package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stage 73.2.1-73.2.3 - sourcing from an RFQ to a PO (journey J2).
//
//   - 73.2.1: an RFQ asks for item lines (an Item or free text, a UOM and a
//     quantity) and keeps the vendors it was sent to, each with a response
//     status (Invited / Responded / Declined) and a due date.
//   - 73.2.2 (decision D06): a quote prices every line - unit price in the
//     vendor's UOM (converted to the RFQ's), tax, lead time - plus freight,
//     payment terms and how long it is valid. Quotes are compared on their
//     landed total: lines + tax + freight. A vendor's newer quote supersedes
//     their earlier one.
//   - 73.2.3: awarding a quote creates the Draft PO from it, exactly once,
//     with lineage both ways (PO.source_rfq_id / source_quote_id,
//     RFQ.awarded_po_id, VendorQuote.po_id). A second award is refused.

// RFQ invitation states.
const (
	RFQInvited   = "Invited"
	RFQResponded = "Responded"
	RFQDeclined  = "Declined"
)

// RFQLine is one thing an RFQ asks for.
type RFQLine struct {
	LineNo      int     `json:"line_no"`
	Sku         string  `json:"sku,omitempty"`
	Description string  `json:"description"`
	UOM         string  `json:"uom"`
	Qty         float64 `json:"qty"`
}

// RFQInvitation is one vendor the RFQ went to.
type RFQInvitation struct {
	Vendor        string `json:"vendor"`
	VendorName    string `json:"vendor_name,omitempty"`
	Status        string `json:"status"`
	DueDate       string `json:"due_date,omitempty"`
	InvitedAt     string `json:"invited_at,omitempty"`
	RespondedAt   string `json:"responded_at,omitempty"`
	DeclineReason string `json:"decline_reason,omitempty"`
	QuoteID       string `json:"quote_id,omitempty"`
}

// VendorQuoteLine is a vendor's price for one RFQ line.
type VendorQuoteLine struct {
	LineNo       int     `json:"line_no"`
	Sku          string  `json:"sku,omitempty"`
	Description  string  `json:"description,omitempty"`
	UOM          string  `json:"uom"`
	RFQUnitsPer  float64 `json:"rfq_units_per_uom,omitempty"`
	Qty          float64 `json:"qty"`
	UnitPrice    float64 `json:"unit_price"`
	TaxRate      float64 `json:"tax_rate"`
	LeadTimeDays int     `json:"lead_time_days,omitempty"`
	NotQuoted    bool    `json:"not_quoted,omitempty"`
	UnitPriceRFQ float64 `json:"unit_price_rfq_uom"`
	Amount       float64 `json:"amount"`
	Tax          float64 `json:"tax"`
}

// RFQInput is what the RFQ screen saves.
type RFQInput struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	TargetDate  string    `json:"target_date"`
	DueDate     string    `json:"response_due_date"`
	Location    string    `json:"location"`
	Lines       []RFQLine `json:"lines"`
}

// QuoteInput is one vendor response.
type QuoteInput struct {
	RFQID        string            `json:"rfq_id"`
	Vendor       string            `json:"vendor"`
	Lines        []VendorQuoteLine `json:"lines"`
	Freight      float64           `json:"freight_amount"`
	PaymentTerms string            `json:"payment_terms"`
	ValidUntil   string            `json:"valid_until"`
	LeadTimeDays int               `json:"lead_time_days"`
	Notes        string            `json:"notes"`
}

func rfqError(code, format string, a ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, a...)}
}

// decodeJSONField reads a field stored either as a JSON string (how the
// generic document engine keeps arrays) or as a raw array.
func decodeJSONField(raw interface{}, out interface{}) error {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return json.Unmarshal([]byte(v), out)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, out)
	}
}

func rfqLinesOf(data map[string]interface{}) []RFQLine {
	var lines []RFQLine
	_ = decodeJSONField(data["lines"], &lines)
	return lines
}

// rfqInvitationsOf reads the invitations, falling back to the pre-73.2.1
// plain list of invited vendor codes.
func rfqInvitationsOf(data map[string]interface{}) []RFQInvitation {
	var inv []RFQInvitation
	_ = decodeJSONField(data["vendor_invitations"], &inv)
	if len(inv) > 0 {
		return inv
	}
	var codes []string
	_ = decodeJSONField(data["invited_vendors"], &codes)
	for _, c := range codes {
		if c = strings.TrimSpace(c); c != "" {
			inv = append(inv, RFQInvitation{Vendor: c, Status: RFQInvited})
		}
	}
	return inv
}

func setRFQInvitations(data map[string]interface{}, inv []RFQInvitation) {
	b, _ := json.Marshal(inv)
	data["vendor_invitations"] = string(b)
	codes := make([]string, 0, len(inv))
	for _, i := range inv {
		codes = append(codes, i.Vendor)
	}
	cb, _ := json.Marshal(codes)
	data["invited_vendors"] = string(cb) // the pre-73.2.1 list, kept in step
}

// normalizeRFQLines checks and completes the lines (an Item fills in its own
// description), and returns the header summary.
func normalizeRFQLines(tenantID string, lines []RFQLine) ([]RFQLine, string, float64, error) {
	if len(lines) == 0 {
		return nil, "", 0, rfqError("RFQ-0551", "an RFQ needs at least one line - an item or a description, a unit and a quantity")
	}
	total := 0.0
	for i := range lines {
		l := &lines[i]
		l.LineNo = i + 1
		l.Sku, l.Description, l.UOM = strings.TrimSpace(l.Sku), strings.TrimSpace(l.Description), strings.TrimSpace(l.UOM)
		if l.Sku != "" {
			item, _, err := fetchDocData(tenantID, "Item", l.Sku)
			if err != nil {
				return nil, "", 0, rfqError("RFQ-0551", "line %d: item %s was not found", l.LineNo, l.Sku)
			}
			if code := str(item, "code"); code != "" {
				l.Sku = code
			}
			if l.Description == "" {
				l.Description = firstNonEmpty(str(item, "name"), l.Sku)
			}
		}
		if l.Description == "" {
			return nil, "", 0, rfqError("RFQ-0551", "line %d needs an item or a description", l.LineNo)
		}
		if l.UOM == "" {
			l.UOM = "Nos"
		}
		if !(l.Qty > 0) {
			return nil, "", 0, rfqError("RFQ-0551", "line %d (%s) needs a quantity above zero", l.LineNo, l.Description)
		}
		total += l.Qty
	}
	summary := lines[0].Description
	if len(lines) > 1 {
		summary = fmt.Sprintf("%s (+%d more)", summary, len(lines)-1)
	}
	return lines, summary, total, nil
}

func vendorNameOf(tenantID, vendor string) (string, bool) {
	v, status, err := fetchDocData(tenantID, "Vendor", vendor)
	if err != nil {
		return "", false
	}
	if status == "Inactive" || status == "Blocked" {
		return str(v, "name"), false
	}
	return firstNonEmpty(str(v, "name"), vendor), true
}

// SaveRFQ creates an RFQ, or updates a Draft one, from item lines.
func SaveRFQ(tenantID, userID, storeCode string, in RFQInput) (string, error) {
	lines, summary, total, err := normalizeRFQLines(tenantID, in.Lines)
	if err != nil {
		return "", err
	}
	if in.Location != "" {
		loc, err := ResolveLocationReference(tenantID, in.Location)
		if err != nil {
			return "", rfqError("RFQ-0551", "deliver-to location: %v", err)
		}
		in.Location = loc
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	linesJSON, _ := json.Marshal(lines)
	fields := map[string]interface{}{
		"lines": string(linesJSON), "description": firstNonEmpty(strings.TrimSpace(in.Description), summary), "quantity": total,
		"target_date": in.TargetDate, "response_due_date": in.DueDate, "location": in.Location,
	}
	if strings.TrimSpace(in.ID) == "" {
		if strings.TrimSpace(storeCode) == "" {
			storeCode = "HQ"
		}
		id, err := GenerateSequence(tenantID, "RFQ", storeCode, documentFinancialYear(time.Now()))
		if err != nil {
			return "", err
		}
		fields["code"], fields["status"] = id, "Draft"
		b, _ := json.Marshal(fields)
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'RFQ', $2, 'Draft', $3)`, schema), id, b, userID); err != nil {
			return "", err
		}
		LogAuditEvent(tenantID, userID, "RFQ_CREATED", "SUCCESS", fmt.Sprintf("RFQ %s with %d line(s)", id, len(lines)))
		return id, nil
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	data, status, err := lockDocTx(tx, schema, "RFQ", in.ID)
	if err != nil {
		return "", err
	}
	if status != "Draft" {
		return "", rfqError("RFQ-0552", "RFQ %s is %s - its lines can only change while it is a Draft", in.ID, status)
	}
	for k, v := range fields {
		data[k] = v
	}
	if err := writeDocTx(tx, schema, "RFQ", in.ID, data, status); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "RFQ_UPDATED", "SUCCESS", fmt.Sprintf("RFQ %s lines updated (%d)", in.ID, len(lines)))
	return in.ID, nil
}

// lockDocTx / writeDocTx read and write one document under its row lock.
func lockDocTx(tx *sql.Tx, schema, doctype, id string) (map[string]interface{}, string, error) {
	var raw, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, COALESCE(status, '') FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL FOR UPDATE`, schema),
		doctype, id).Scan(&raw, &status); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", fmt.Errorf("%s %s not found", doctype, id)
		}
		return nil, "", err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, "", err
	}
	return data, status, nil
}

func writeDocTx(tx *sql.Tx, schema, doctype, id string, data map[string]interface{}, status string) error {
	data["status"] = status
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = $3 AND id = $4`, schema),
		b, status, doctype, id)
	return err
}

// InviteRFQVendors adds vendors to an open RFQ with a response due date.
func InviteRFQVendors(tenantID, rfqID, userID, dueDate string, vendors []string) ([]RFQInvitation, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	data, status, err := lockDocTx(tx, schema, "RFQ", rfqID)
	if err != nil {
		return nil, err
	}
	if status != "Draft" && status != "Sent" {
		return nil, rfqError("RFQ-0552", "RFQ %s is %s - vendors can be invited only while it is open", rfqID, status)
	}
	inv := rfqInvitationsOf(data)
	now := time.Now().UTC().Format(time.RFC3339)
	due := firstNonEmpty(strings.TrimSpace(dueDate), str(data, "response_due_date"))
	for _, v := range vendors {
		if v = strings.TrimSpace(v); v == "" {
			continue
		}
		name, ok := vendorNameOf(tenantID, v)
		if !ok {
			return nil, rfqError("RFQ-0553", "vendor %s is not an active vendor", v)
		}
		dup := false
		for i := range inv {
			if strings.EqualFold(inv[i].Vendor, v) {
				dup = true
				if inv[i].Status == RFQDeclined { // asked again
					inv[i].Status, inv[i].DeclineReason, inv[i].DueDate, inv[i].InvitedAt = RFQInvited, "", due, now
				}
			}
		}
		if !dup {
			inv = append(inv, RFQInvitation{Vendor: v, VendorName: name, Status: RFQInvited, DueDate: due, InvitedAt: now})
		}
	}
	setRFQInvitations(data, inv)
	if err := writeDocTx(tx, schema, "RFQ", rfqID, data, status); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "RFQ_VENDORS_INVITED", "SUCCESS", fmt.Sprintf("RFQ %s invited %v (due %s)", rfqID, vendors, orDash(due)))
	return inv, nil
}

// DeclineRFQ records that an invited vendor will not quote.
func DeclineRFQ(tenantID, rfqID, vendor, reason, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockDocTx(tx, schema, "RFQ", rfqID)
	if err != nil {
		return err
	}
	if status != "Draft" && status != "Sent" {
		return rfqError("RFQ-0552", "RFQ %s is %s", rfqID, status)
	}
	inv := rfqInvitationsOf(data)
	found := false
	for i := range inv {
		if strings.EqualFold(inv[i].Vendor, vendor) {
			if inv[i].Status == RFQResponded {
				return rfqError("RFQ-0553", "%s has already quoted on RFQ %s", vendor, rfqID)
			}
			inv[i].Status, inv[i].DeclineReason, inv[i].RespondedAt = RFQDeclined, strings.TrimSpace(reason), time.Now().UTC().Format(time.RFC3339)
			found = true
		}
	}
	if !found {
		return rfqError("RFQ-0553", "%s was not invited to RFQ %s", vendor, rfqID)
	}
	setRFQInvitations(data, inv)
	if err := writeDocTx(tx, schema, "RFQ", rfqID, data, status); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "RFQ_VENDOR_DECLINED", "SUCCESS", fmt.Sprintf("RFQ %s: %s declined%s", rfqID, vendor, reasonSuffix(reason)))
	return nil
}

// uomFactor is how many RFQ units one quoted unit holds: 1 for the same unit,
// else the vendor's stated figure, else the item's UOMConversion master.
func uomFactor(tenantID, sku, quoteUOM, rfqUOM string, stated float64) (float64, bool) {
	if strings.EqualFold(strings.TrimSpace(quoteUOM), strings.TrimSpace(rfqUOM)) || strings.TrimSpace(quoteUOM) == "" {
		return 1, true
	}
	if stated > 0 {
		return stated, true
	}
	if sku == "" {
		return 0, false
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, false
	}
	var factor float64
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(data->>'factor', '')::numeric, 0) FROM %s.documents
		 WHERE doctype = 'UOMConversion' AND COALESCE(status, 'Active') = 'Active' AND data->>'item' = $1
		   AND lower(data->>'from_uom') = lower($2) AND lower(data->>'to_uom') = lower($3) LIMIT 1`, schema),
		sku, quoteUOM, rfqUOM).Scan(&factor); err == nil && factor > 0 {
		return factor, true
	}
	return 0, false
}

// priceQuoteLines checks a quote against its RFQ's lines and works out each
// line's money in the RFQ's unit (D06). Returns subtotal and tax.
func priceQuoteLines(tenantID string, rfqLines []RFQLine, in []VendorQuoteLine) ([]VendorQuoteLine, float64, float64, int, error) {
	byNo := map[int]VendorQuoteLine{}
	for _, q := range in {
		byNo[q.LineNo] = q
	}
	out := make([]VendorQuoteLine, 0, len(rfqLines))
	var subtotal, tax float64
	quoted := 0
	for _, rl := range rfqLines {
		q, ok := byNo[rl.LineNo]
		if !ok {
			q = VendorQuoteLine{LineNo: rl.LineNo, NotQuoted: true}
		}
		q.Sku, q.Description, q.Qty = rl.Sku, rl.Description, rl.Qty
		if q.UOM = strings.TrimSpace(q.UOM); q.UOM == "" {
			q.UOM = rl.UOM
		}
		if q.NotQuoted || q.UnitPrice == 0 {
			q.NotQuoted, q.UnitPrice, q.UnitPriceRFQ, q.Amount, q.Tax = true, 0, 0, 0, 0
			out = append(out, q)
			continue
		}
		if q.UnitPrice < 0 || q.TaxRate < 0 || q.TaxRate > 40 || q.LeadTimeDays < 0 {
			return nil, 0, 0, 0, rfqError("RFQ-0554", "line %d (%s): unit price, tax rate (0-40%%) and lead time cannot be negative or out of range", rl.LineNo, rl.Description)
		}
		factor, ok := uomFactor(tenantID, rl.Sku, q.UOM, rl.UOM, q.RFQUnitsPer)
		if !ok {
			return nil, 0, 0, 0, rfqError("RFQ-0554", "line %d (%s) is quoted per %s but asked per %s - say how many %s one %s holds", rl.LineNo, rl.Description, q.UOM, rl.UOM, rl.UOM, q.UOM)
		}
		q.RFQUnitsPer = factor
		q.UnitPriceRFQ = round2(q.UnitPrice / factor)
		q.Amount = round2(q.UnitPriceRFQ * rl.Qty)
		q.Tax = round2(q.Amount * q.TaxRate / 100)
		subtotal += q.Amount
		tax += q.Tax
		quoted++
		out = append(out, q)
	}
	if quoted == 0 {
		return nil, 0, 0, 0, rfqError("RFQ-0554", "a quote must price at least one line")
	}
	return out, round2(subtotal), round2(tax), quoted, nil
}

// RecordVendorQuote records one vendor's response to an open RFQ. A vendor
// who was not invited is invited on the way (status Responded); a vendor's
// earlier quote on the same RFQ is superseded.
func RecordVendorQuote(tenantID, userID, storeCode string, in QuoteInput) (string, error) {
	in.Vendor = strings.TrimSpace(in.Vendor)
	if in.Vendor == "" {
		return "", rfqError("RFQ-0553", "a quote needs the vendor")
	}
	vendorName, ok := vendorNameOf(tenantID, in.Vendor)
	if !ok {
		return "", rfqError("RFQ-0553", "vendor %s is not an active vendor", in.Vendor)
	}
	if in.Freight < 0 || in.LeadTimeDays < 0 {
		return "", rfqError("RFQ-0554", "freight and lead time cannot be negative")
	}
	valid, okDate := parseTraceDate(in.ValidUntil)
	if !okDate {
		return "", rfqError("RFQ-0554", "say until when the quote is valid")
	}
	if valid.Before(today()) {
		return "", rfqError("RFQ-0554", "the quote's validity (%s) is already over", in.ValidUntil)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(storeCode) == "" {
		storeCode = "HQ"
	}
	quoteID, err := GenerateSequence(tenantID, "QTN", storeCode, documentFinancialYear(time.Now()))
	if err != nil {
		return "", err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	rfq, status, err := lockDocTx(tx, schema, "RFQ", in.RFQID)
	if err != nil {
		return "", err
	}
	if status != "Draft" && status != "Sent" {
		return "", rfqError("RFQ-0552", "RFQ %s is %s - quotes can be recorded only while it is open", in.RFQID, status)
	}
	rfqLines := rfqLinesOf(rfq)
	if len(rfqLines) == 0 {
		return "", rfqError("RFQ-0551", "RFQ %s has no item lines yet - add them before recording quotes", in.RFQID)
	}
	lines, subtotal, tax, quoted, err := priceQuoteLines(tenantID, rfqLines, in.Lines)
	if err != nil {
		return "", err
	}
	landed := round2(subtotal + tax + in.Freight)
	leadTime := in.LeadTimeDays
	for _, l := range lines {
		if l.LeadTimeDays > leadTime {
			leadTime = l.LeadTimeDays
		}
	}
	linesJSON, _ := json.Marshal(lines)
	quote := map[string]interface{}{
		"code": quoteID, "rfq_id": in.RFQID, "vendor": in.Vendor, "vendor_name": vendorName,
		"lines": string(linesJSON), "subtotal": subtotal, "tax_total": tax, "freight_amount": round2(in.Freight),
		"landed_total": landed, "quoted_price": landed, "lead_time_days": leadTime,
		"payment_terms": strings.TrimSpace(in.PaymentTerms), "valid_until": valid.Format(isoDate),
		"lines_quoted": quoted, "lines_total": len(rfqLines), "notes": strings.TrimSpace(in.Notes), "status": "Submitted",
	}
	b, _ := json.Marshal(quote)
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorQuote', $2, 'Submitted', $3)`, schema),
		quoteID, b, userID); err != nil {
		return "", err
	}
	// A newer quote from the same vendor supersedes the earlier one.
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET status = 'Superseded', data = data || jsonb_build_object('status', 'Superseded', 'superseded_by', $3::text), updated_at = CURRENT_TIMESTAMP
		 WHERE doctype = 'VendorQuote' AND data->>'rfq_id' = $1 AND lower(data->>'vendor') = lower($2) AND status = 'Submitted' AND id <> $3`, schema),
		in.RFQID, in.Vendor, quoteID); err != nil {
		return "", err
	}
	inv := rfqInvitationsOf(rfq)
	now := time.Now().UTC().Format(time.RFC3339)
	found := false
	for i := range inv {
		if strings.EqualFold(inv[i].Vendor, in.Vendor) {
			inv[i].Status, inv[i].RespondedAt, inv[i].QuoteID, inv[i].DeclineReason = RFQResponded, now, quoteID, ""
			found = true
		}
	}
	if !found {
		inv = append(inv, RFQInvitation{Vendor: in.Vendor, VendorName: vendorName, Status: RFQResponded, InvitedAt: now, RespondedAt: now, QuoteID: quoteID})
	}
	setRFQInvitations(rfq, inv)
	if status == "Draft" {
		status = "Sent" // a response means it went out
	}
	if err := writeDocTx(tx, schema, "RFQ", in.RFQID, rfq, status); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "VENDOR_QUOTE_RECORDED", "SUCCESS", fmt.Sprintf("Quote %s from %s on RFQ %s: landed %.2f (%d/%d lines)", quoteID, in.Vendor, in.RFQID, landed, quoted, len(rfqLines)))
	return quoteID, nil
}

// RFQComparisonQuote is one quote in the comparison.
type RFQComparisonQuote struct {
	QuoteID      string            `json:"quote_id"`
	Number       string            `json:"number"`
	Vendor       string            `json:"vendor"`
	VendorName   string            `json:"vendor_name"`
	Status       string            `json:"status"`
	Subtotal     float64           `json:"subtotal"`
	Tax          float64           `json:"tax_total"`
	Freight      float64           `json:"freight_amount"`
	Landed       float64           `json:"landed_total"`
	LeadTimeDays int               `json:"lead_time_days"`
	PaymentTerms string            `json:"payment_terms,omitempty"`
	ValidUntil   string            `json:"valid_until"`
	Expired      bool              `json:"expired"`
	LinesQuoted  int               `json:"lines_quoted"`
	Complete     bool              `json:"complete"`
	Lines        []VendorQuoteLine `json:"lines"`
	// LandedUnit is, per line, the landed cost of one RFQ unit: price + tax +
	// its value share of freight.
	LandedUnit  map[int]float64 `json:"landed_unit"`
	Recommended bool            `json:"recommended"`
}

// RFQComparison is the side-by-side view (D06: compared on landed total).
type RFQComparison struct {
	RFQID       string               `json:"rfq_id"`
	Number      string               `json:"number"`
	Status      string               `json:"status"`
	Location    string               `json:"location,omitempty"`
	Lines       []RFQLine            `json:"lines"`
	Invitations []RFQInvitation      `json:"invitations"`
	Quotes      []RFQComparisonQuote `json:"quotes"`
	BestByLine  map[int]string       `json:"best_by_line"`
	AwardedPO   string               `json:"awarded_po_id,omitempty"`
	AwardedQt   string               `json:"awarded_quote_id,omitempty"`
}

// GetRFQComparison builds the comparison: live quotes (Submitted / Selected),
// cheapest complete and valid landed total recommended, and the cheapest
// landed unit cost per line.
func GetRFQComparison(tenantID, rfqID string) (*RFQComparison, error) {
	rfq, status, err := fetchDocData(tenantID, "RFQ", rfqID)
	if err != nil {
		return nil, fmt.Errorf("RFQ %s not found", rfqID)
	}
	out := &RFQComparison{RFQID: rfqID, Number: firstNonEmpty(str(rfq, "code"), rfqID), Status: status,
		Location: str(rfq, "location"), Lines: rfqLinesOf(rfq), Invitations: rfqInvitationsOf(rfq),
		BestByLine: map[int]string{}, AwardedPO: str(rfq, "awarded_po_id"), AwardedQt: str(rfq, "awarded_quote_id")}
	for i := range out.Invitations {
		if out.Invitations[i].VendorName == "" {
			out.Invitations[i].VendorName, _ = vendorNameOf(tenantID, out.Invitations[i].Vendor)
		}
	}
	quotes, err := GetVendorQuotesForRFQ(tenantID, rfqID)
	if err != nil {
		return nil, err
	}
	bestUnit := map[int]float64{}
	recommended := -1
	for _, q := range quotes {
		st := str(q, "status")
		if st != "Submitted" && st != "Selected" {
			continue
		}
		c := RFQComparisonQuote{QuoteID: str(q, "id"), Number: firstNonEmpty(str(q, "code"), str(q, "id")), Vendor: str(q, "vendor"),
			VendorName: str(q, "vendor_name"), Status: st, Subtotal: numFromInterface(q["subtotal"]), Tax: numFromInterface(q["tax_total"]),
			Freight: numFromInterface(q["freight_amount"]), Landed: numFromInterface(q["landed_total"]),
			LeadTimeDays: int(numFromInterface(q["lead_time_days"])), PaymentTerms: str(q, "payment_terms"),
			ValidUntil: str(q, "valid_until"), LandedUnit: map[int]float64{}}
		if c.VendorName == "" {
			c.VendorName, _ = vendorNameOf(tenantID, c.Vendor)
		}
		_ = decodeJSONField(q["lines"], &c.Lines)
		if len(c.Lines) == 0 { // a pre-73.2.2 quote: one price, nothing to compare per line
			c.Landed = numFromInterface(q["quoted_price"])
		}
		if d, ok := parseTraceDate(c.ValidUntil); ok && d.Before(today()) {
			c.Expired = true
		}
		for _, l := range c.Lines {
			if l.NotQuoted || l.Qty <= 0 {
				continue
			}
			c.LinesQuoted++
			share := 0.0
			if c.Subtotal > 0 {
				share = c.Freight * l.Amount / c.Subtotal
			}
			unit := round2((l.Amount + l.Tax + share) / l.Qty)
			c.LandedUnit[l.LineNo] = unit
			if !c.Expired {
				if best, ok := bestUnit[l.LineNo]; !ok || unit < best {
					bestUnit[l.LineNo] = unit
					out.BestByLine[l.LineNo] = c.QuoteID
				}
			}
		}
		c.Complete = len(out.Lines) > 0 && c.LinesQuoted == len(out.Lines)
		out.Quotes = append(out.Quotes, c)
	}
	sort.SliceStable(out.Quotes, func(i, j int) bool { return out.Quotes[i].Landed < out.Quotes[j].Landed })
	for i, c := range out.Quotes {
		if c.Complete && !c.Expired && (recommended < 0 || c.Landed < out.Quotes[recommended].Landed) {
			recommended = i
		}
	}
	if recommended >= 0 {
		out.Quotes[recommended].Recommended = true
	}
	return out, nil
}

// RFQAwardResult is what an award produced.
type RFQAwardResult struct {
	POID     string   `json:"po_id"`
	QuoteID  string   `json:"quote_id"`
	LeftOut  []string `json:"left_out,omitempty"`
	Replayed bool     `json:"replayed,omitempty"`
	PONumber string   `json:"po_number"`
	POTotal  float64  `json:"po_total"`
	Freight  float64  `json:"freight_amount"`
	Location string   `json:"location,omitempty"`
}

// AwardRFQ turns the chosen quote into a Draft PO - once (73.2.3). The PO
// carries the quote's lines that have an item, at the quote's price per RFQ
// unit, its freight, terms and validity, and points back to the RFQ and
// quote; the RFQ and quote point to it. The winner becomes Selected, the
// other live quotes Rejected, the RFQ Closed. A second award returns the
// first PO's id with Replayed for the same quote, and is refused for another.
// AwardOptions: where the goods go, the item for a free-text line (by line
// number), and lines deliberately left off the PO.
type AwardOptions struct {
	Location  string         `json:"location"`
	LineItems map[int]string `json:"line_items"`
	LeaveOut  []int          `json:"leave_out"`
}

func AwardRFQ(tenantID, rfqID, quoteID, userID, storeCode string, opts AwardOptions) (*RFQAwardResult, error) {
	location := opts.Location
	leaveOut := map[int]bool{}
	for _, n := range opts.LeaveOut {
		leaveOut[n] = true
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(storeCode) == "" {
		storeCode = "HQ"
	}
	// The number is taken before the transaction, as every converter here
	// does; a refusal below leaves a gap in the series, never a duplicate.
	poID, err := GenerateSequence(tenantID, "PO", storeCode, documentFinancialYear(time.Now()))
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rfq, rfqStatus, err := lockDocTx(tx, schema, "RFQ", rfqID)
	if err != nil {
		return nil, err
	}
	if prior := str(rfq, "awarded_po_id"); prior != "" {
		if str(rfq, "awarded_quote_id") == quoteID {
			return &RFQAwardResult{POID: prior, QuoteID: quoteID, Replayed: true, PONumber: prior}, nil
		}
		return nil, rfqError("RFQ-0555", "RFQ %s was already awarded - purchase order %s was created from quote %s", rfqID, prior, str(rfq, "awarded_quote_id"))
	}
	if rfqStatus != "Sent" && rfqStatus != "Closed" && rfqStatus != "Draft" {
		return nil, rfqError("RFQ-0552", "RFQ %s is %s", rfqID, rfqStatus)
	}
	quote, qStatus, err := lockDocTx(tx, schema, "VendorQuote", quoteID)
	if err != nil {
		return nil, err
	}
	if str(quote, "rfq_id") != rfqID {
		return nil, rfqError("RFQ-0555", "quote %s is not on RFQ %s", quoteID, rfqID)
	}
	if qStatus != "Submitted" && qStatus != "Selected" {
		return nil, rfqError("RFQ-0555", "quote %s is %s - only a live quote can be awarded", quoteID, qStatus)
	}
	if rfqStatus == "Closed" && qStatus != "Selected" {
		return nil, rfqError("RFQ-0552", "RFQ %s was closed without choosing this quote", rfqID)
	}
	if d, ok := parseTraceDate(str(quote, "valid_until")); ok && d.Before(today()) {
		return nil, rfqError("RFQ-0556", "quote %s expired on %s - ask the vendor to re-quote", quoteID, str(quote, "valid_until"))
	}
	var lines []VendorQuoteLine
	_ = decodeJSONField(quote["lines"], &lines)
	if len(lines) == 0 {
		return nil, rfqError("RFQ-0556", "quote %s has no priced lines to put on a purchase order (it was recorded before quotes had lines) - record the vendor's quote again", quoteID)
	}
	type poLine struct {
		SKU  string  `json:"sku"`
		Qty  int     `json:"qty"`
		Rate float64 `json:"rate"`
	}
	var items []poLine
	res := &RFQAwardResult{POID: poID, QuoteID: quoteID, PONumber: poID}
	for _, l := range lines {
		if l.NotQuoted {
			res.LeftOut = append(res.LeftOut, fmt.Sprintf("line %d %s (not quoted)", l.LineNo, l.Description))
			continue
		}
		if leaveOut[l.LineNo] {
			res.LeftOut = append(res.LeftOut, fmt.Sprintf("line %d %s (left out)", l.LineNo, l.Description))
			continue
		}
		if l.Sku == "" {
			if mapped := strings.TrimSpace(opts.LineItems[l.LineNo]); mapped != "" {
				item, _, err := fetchDocData(tenantID, "Item", mapped)
				if err != nil {
					return nil, rfqError("RFQ-0556", "line %d: item %s was not found", l.LineNo, mapped)
				}
				l.Sku = firstNonEmpty(str(item, "code"), mapped)
			}
		}
		if l.Sku == "" {
			return nil, rfqError("RFQ-0556", "line %d (%s) has no item - choose its item for the purchase order, or leave the line out", l.LineNo, l.Description)
		}
		if l.Qty != math.Trunc(l.Qty) {
			return nil, rfqError("RFQ-0556", "line %d (%s) asks for %v - a purchase order line needs whole units", l.LineNo, l.Description, l.Qty)
		}
		items = append(items, poLine{SKU: l.Sku, Qty: int(l.Qty), Rate: l.UnitPriceRFQ})
		res.POTotal += l.Amount
	}
	if len(items) == 0 {
		return nil, rfqError("RFQ-0556", "quote %s prices none of the RFQ's lines", quoteID)
	}
	res.POTotal = round2(res.POTotal)
	res.Freight = numFromInterface(quote["freight_amount"])
	loc := firstNonEmpty(strings.TrimSpace(location), str(rfq, "location"))
	res.Location = loc
	itemsJSON, _ := json.Marshal(items)
	vendor := str(quote, "vendor")
	po := map[string]interface{}{
		"code": poID, "po_number": poID, "vendor": vendor, "vendor_id": vendor,
		"location": loc, "target_warehouse": loc, "items": string(itemsJSON), "total_amount": res.POTotal,
		"gst_mode": GSTModeExclusive, "freight_amount": res.Freight, "payment_terms": str(quote, "payment_terms"),
		"quote_valid_until": str(quote, "valid_until"), "source_rfq_id": rfqID, "source_quote_id": quoteID,
		"source_requisition_id": str(rfq, "source_requisition_id"), "status": "Draft",
	}
	pb, _ := json.Marshal(po)
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'PurchaseOrder', $2, 'Draft', $3)`, schema),
		poID, pb, userID); err != nil {
		return nil, err
	}
	quote["po_id"] = poID
	if err := writeDocTx(tx, schema, "VendorQuote", quoteID, quote, "Selected"); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET status = 'Rejected', data = data || '{"status":"Rejected"}'::jsonb, updated_at = CURRENT_TIMESTAMP
		 WHERE doctype = 'VendorQuote' AND data->>'rfq_id' = $1 AND id <> $2 AND status = 'Submitted'`, schema), rfqID, quoteID); err != nil {
		return nil, err
	}
	rfq["awarded_po_id"], rfq["awarded_quote_id"] = poID, quoteID
	rfq["awarded_by"], rfq["awarded_at"] = userID, time.Now().UTC().Format(time.RFC3339)
	if err := writeDocTx(tx, schema, "RFQ", rfqID, rfq, "Closed"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "RFQ_AWARDED", "SUCCESS", fmt.Sprintf(
		"RFQ %s awarded to %s (quote %s) -> Draft PO %s, %d line(s), %.2f + freight %.2f%s",
		rfqID, vendor, quoteID, poID, len(items), res.POTotal, res.Freight, map[bool]string{true: fmt.Sprintf("; left out: %s", strings.Join(res.LeftOut, "; ")), false: ""}[len(res.LeftOut) > 0]))
	return res, nil
}

// validateVendorQuoteRules keeps generic saves of a quote honest: its
// selection is the award's to make, and an awarded quote is final.
func validateVendorQuoteRules(priorStatus string, payload map[string]interface{}) error {
	status := resolveWrittenStatus(payload)
	if status == "Selected" && priorStatus != "Selected" {
		return rfqError("RFQ-0555", "a quote is selected by awarding it on the RFQ screen, which also creates its purchase order")
	}
	if priorStatus == "Selected" {
		return rfqError("RFQ-0555", "an awarded quote cannot be changed")
	}
	return nil
}

// validateRFQEditRules: an awarded RFQ is final.
func validateRFQEditRules(priorData map[string]interface{}) error {
	if priorData != nil && str(priorData, "awarded_po_id") != "" {
		return rfqError("RFQ-0555", "RFQ was awarded (purchase order %s) and cannot be changed", str(priorData, "awarded_po_id"))
	}
	return nil
}
