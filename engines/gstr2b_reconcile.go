package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// Ask 170.36: GSTR-2B import and match. The month's GSTR-2B JSON (docdata
// b2b / cdnr) is stored as Gstr2bLine rows, one per supplier document, and
// each is matched to a vendor bill by supplier GSTIN and normalised invoice
// number, then (when the number differs) by amount and date within the
// tolerances below. Statuses: Matched, Mismatch (amount, tax or date),
// In 2B not in books, In books not in 2B. Re-importing a month replaces its
// lines and keeps a manual decision where the line is unchanged.

const (
	Gstr2bLineDoctype = "Gstr2bLine"

	SettingGSTR2BAmountTolerance   = "finance.gstr2b_amount_tolerance"
	SettingGSTR2BDateToleranceDays = "finance.gstr2b_date_tolerance_days"

	Gstr2bMatched   = "Matched"
	Gstr2bMismatch  = "Mismatch"
	Gstr2bNotInBook = "In 2B not in books"
	Gstr2bNotIn2B   = "In books not in 2B"

	gstr2bBadFileCode  = "TAXGST-1710" // not a GSTR-2B file
	gstr2bBadMonthCode = "TAXGST-1711" // month missing / not the file's month
	gstr2bDecisionCode = "TAXGST-1712" // decision not allowed / line not found
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingGSTR2BAmountTolerance, Module: "Finance",
		Label: "GSTR-2B match: amount tolerance", Type: SettingTypeFloat, Default: "1", Unit: "rupees",
		Description: "Ask 170.36: a vendor bill within this many rupees of its GSTR-2B line (invoice value and tax) counts as Matched.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingGSTR2BDateToleranceDays, Module: "Finance",
		Label: "GSTR-2B match: date tolerance", Type: SettingTypeInt, Default: "3", Unit: "days",
		Description: "Ask 170.36: a bill whose number differs from the 2B line still matches when supplier, amount and date (within this many days) agree.",
	})
}

var gstr2bZeroRun = regexp.MustCompile(`(^|\D)0+(\d)`)

// NormalizeInvoiceNumber upper-cases a supplier invoice number, drops
// everything but letters and digits, and drops zeros that lead a run of
// digits: "inv/0012" and "INV-12" are the same number.
func NormalizeInvoiceNumber(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return gstr2bZeroRun.ReplaceAllString(b.String(), "$1$2")
}

// GSTR2BImportResult is what an import reports.
type GSTR2BImportResult struct {
	Month    string         `json:"month"`
	Imported int            `json:"imported"`
	Kept     int            `json:"kept_decisions"`
	Summary  map[string]int `json:"summary"`
}

type gstr2bFileLine struct {
	gstin, name, docType, number, date     string
	taxable, igst, cgst, sgst, cess, total float64
}

func (l gstr2bFileLine) tax() float64 { return round2(l.igst + l.cgst + l.sgst + l.cess) }
func (l gstr2bFileLine) hash() string {
	return fmt.Sprintf("%s|%s|%s|%s|%.2f|%.2f", l.gstin, NormalizeInvoiceNumber(l.number), l.docType, l.date, l.total, l.tax())
}

// gstr2bDate turns the portal's dd-mm-yyyy (or an ISO date) into yyyy-mm-dd.
func gstr2bDate(s string) string {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"02-01-2006", "2006-01-02", "02/01/2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

// parseGSTR2B reads the file: docdata.b2b[].inv[] and docdata.cdnr[].nt[].
func parseGSTR2B(raw []byte) (lines []gstr2bFileLine, period string, err error) {
	var root map[string]interface{}
	if json.Unmarshal(raw, &root) != nil {
		return nil, "", &ValidationError{Code: gstr2bBadFileCode, Message: "the file is not valid JSON"}
	}
	data, _ := root["data"].(map[string]interface{})
	if data == nil {
		data = root
	}
	docdata, _ := data["docdata"].(map[string]interface{})
	if docdata == nil {
		return nil, "", &ValidationError{Code: gstr2bBadFileCode, Message: "the file has no docdata"}
	}
	period = strField(data, "rtnprd")
	each := func(key, inner, docType, numKey string) {
		parties, _ := docdata[key].([]interface{})
		for _, p := range parties {
			pm, _ := p.(map[string]interface{})
			docs, _ := pm[inner].([]interface{})
			for _, d := range docs {
				dm, _ := d.(map[string]interface{})
				if dm == nil {
					continue
				}
				kind := docType
				// 170.36: a cdnr note says whether it is a credit or a debit note.
				if key == "cdnr" && strings.EqualFold(firstNonEmpty(strField(dm, "typ"), strField(dm, "ntty")), "D") {
					kind = "Debit Note"
				}
				l := gstr2bFileLine{gstin: strings.ToUpper(strField(pm, "ctin")), name: strField(pm, "trdnm"), docType: kind,
					number: strField(dm, numKey), date: gstr2bDate(strField(dm, "dt")),
					taxable: numFromInterface(dm["txval"]), igst: numFromInterface(dm["igst"]), cgst: numFromInterface(dm["cgst"]),
					sgst: numFromInterface(dm["sgst"]), cess: numFromInterface(dm["cess"]), total: numFromInterface(dm["val"])}
				if l.gstin != "" && l.number != "" {
					lines = append(lines, l)
				}
			}
		}
	}
	each("b2b", "inv", "Invoice", "inum")
	each("cdnr", "nt", "Credit Note", "ntnum")
	if len(lines) == 0 {
		return nil, period, &ValidationError{Code: gstr2bBadFileCode, Message: "no invoices in the file"}
	}
	return lines, period, nil
}

// gstr2bBook is a vendor bill or vendor note in the books. kind is the 2B
// document it answers to: a bill is an "Invoice"; our DebitNote (the payable
// goes down) answers the supplier's "Credit Note"; our VendorCreditNote (the
// payable goes up) answers the supplier's "Debit Note". gstin is "" when the
// vendor has no GSTIN in the books (170.36).
type gstr2bBook struct {
	id, kind, gstin, number, date string
	total, tax                    float64
	hasTax                        bool
	used                          bool
}

// gstr2bNoteKind maps a vendor note doctype to the 2B document it answers to.
var gstr2bNoteKind = map[string]string{"DebitNote": "Credit Note", "VendorCreditNote": "Debit Note"}

func loadBooksForMonth(schema, month string) ([]*gstr2bBook, error) {
	first, last, err := gstr1Month(month)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT d.id, d.data, COALESCE(v.data->>'gstin', '')
		FROM %[1]s.documents d
		LEFT JOIN %[1]s.documents v ON v.doctype = 'Vendor' AND v.deleted_at IS NULL AND v.id = d.data->>'vendor_id'
		WHERE d.doctype = 'VendorInvoice' AND d.deleted_at IS NULL AND d.status NOT IN ('Draft', 'Cancelled', 'Rejected')
		  AND d.data->>'invoice_date' BETWEEN $1 AND $2`, schema), first.Format("2006-01-02"), last.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*gstr2bBook
	for rows.Next() {
		var id, gstin string
		var raw []byte
		if err := rows.Scan(&id, &raw, &gstin); err != nil {
			return nil, err
		}
		var m map[string]interface{}
		_ = json.Unmarshal(raw, &m)
		b := &gstr2bBook{id: id, kind: "Invoice", gstin: strings.ToUpper(strings.TrimSpace(gstin)),
			number: firstNonEmpty(strField(m, "invoice_number"), id), date: strField(m, "invoice_date"),
			total: numFromInterface(m["invoice_amount"])}
		for _, k := range []string{"gst_total", "gst_amount"} {
			if v := numFromInterface(m[k]); v > 0 {
				b.tax, b.hasTax = v, true
				break
			}
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	notes, err := loadVendorNotesForMonth(schema, first, last)
	return append(out, notes...), err
}

// loadVendorNotesForMonth reads the vendor notes posted in the month. A note
// has no date field of its own, so it is dated by its first GL posting.
func loadVendorNotesForMonth(schema string, first, last time.Time) ([]*gstr2bBook, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH posted AS (
			SELECT document_type, document_id, MIN(created_at) AS at FROM %[1]s.gl_postings
			 WHERE document_type IN ('DebitNote', 'VendorCreditNote') GROUP BY document_type, document_id)
		SELECT d.id, d.doctype, d.data, COALESCE(v.data->>'gstin', ''), public.erp_local_date(p.at)::text
		FROM posted p
		JOIN %[1]s.documents d ON d.id = p.document_id AND d.doctype = p.document_type
		LEFT JOIN %[1]s.documents v ON v.doctype = 'Vendor' AND v.deleted_at IS NULL AND v.id = d.data->>'vendor_id'
		WHERE d.deleted_at IS NULL AND d.status NOT IN ('Draft', 'Cancelled', 'Rejected')
		  AND p.at >= public.erp_day_start($1::date) AND p.at < public.erp_day_start($2::date + 1)`, schema),
		first.Format("2006-01-02"), last.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*gstr2bBook
	for rows.Next() {
		var id, doctype, gstin, day string
		var raw []byte
		if err := rows.Scan(&id, &doctype, &raw, &gstin, &day); err != nil {
			return nil, err
		}
		var m map[string]interface{}
		_ = json.Unmarshal(raw, &m)
		total := numFromInterface(m["total_with_gst"])
		if total == 0 {
			total = round2(numFromInterface(m["amount"]) + numFromInterface(m["gst_amount"]))
		}
		b := &gstr2bBook{id: id, kind: gstr2bNoteKind[doctype], gstin: strings.ToUpper(strings.TrimSpace(gstin)),
			number: firstNonEmpty(strField(m, "note_number"), id), date: day, total: total}
		if v := numFromInterface(m["gst_amount"]); v > 0 {
			b.tax, b.hasTax = v, true
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func dayGap(a, b string) float64 {
	ta, e1 := time.Parse("2006-01-02", a)
	tb, e2 := time.Parse("2006-01-02", b)
	if e1 != nil || e2 != nil {
		return 0
	}
	return math.Abs(ta.Sub(tb).Hours() / 24)
}

// gstr2bJudge compares a 2B line with a bill.
func gstr2bJudge(l gstr2bFileLine, b *gstr2bBook, amtTol, dateTol float64) (status, reason string) {
	var why []string
	if math.Abs(l.total-b.total) > amtTol {
		why = append(why, fmt.Sprintf("amount: 2B %.2f, books %.2f", l.total, b.total))
	}
	if b.hasTax && math.Abs(l.tax()-b.tax) > amtTol {
		why = append(why, fmt.Sprintf("tax: 2B %.2f, books %.2f", l.tax(), b.tax))
	}
	if l.date != "" && b.date != "" && dayGap(l.date, b.date) > dateTol {
		why = append(why, fmt.Sprintf("date: 2B %s, books %s", l.date, b.date))
	}
	if len(why) > 0 {
		return Gstr2bMismatch, strings.Join(why, "; ")
	}
	return Gstr2bMatched, ""
}

type gstr2bStored struct {
	id   string
	line gstr2bFileLine
}

// ImportGSTR2B stores the month's 2B lines (replacing the month's earlier
// ones, keeping a decision on a line that did not change) and matches them.
func ImportGSTR2B(tenantID, month string, raw []byte, userID string) (*GSTR2BImportResult, error) {
	month = strings.TrimSpace(month)
	first, _, err := gstr1Month(month)
	if err != nil {
		return nil, &ValidationError{Code: gstr2bBadMonthCode, Message: "give the month as YYYY-MM"}
	}
	lines, period, err := parseGSTR2B(raw)
	if err != nil {
		return nil, err
	}
	if len(period) == 6 && period != first.Format("012006") {
		return nil, &ValidationError{Code: gstr2bBadMonthCode, Message: fmt.Sprintf("the file is for %s, not %s", period, first.Format("012006"))}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Decisions on the month's earlier lines, keyed by what the line says.
	kept := map[string][2]string{}
	rows, err := tx.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
		AND data->>'month' = $2 AND data->>'source' = '2B' AND COALESCE(data->>'decision', '') <> ''`, schema), Gstr2bLineDoctype, month)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var b []byte
		var m map[string]interface{}
		if rows.Scan(&b) == nil && json.Unmarshal(b, &m) == nil {
			kept[strField(m, "line_hash")] = [2]string{strField(m, "decision"), strField(m, "decision_note")}
		}
	}
	rows.Close()
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET deleted_at = CURRENT_TIMESTAMP WHERE doctype = $1 AND deleted_at IS NULL AND data->>'month' = $2`, schema), Gstr2bLineDoctype, month); err != nil {
		return nil, err
	}
	nKept := 0
	for _, l := range lines {
		data := map[string]interface{}{
			"month": month, "source": "2B", "supplier_gstin": l.gstin, "supplier_name": l.name, "doc_type": l.docType,
			"doc_number": l.number, "doc_key": NormalizeInvoiceNumber(l.number), "doc_date": l.date, "taxable_value": l.taxable,
			"igst": l.igst, "cgst": l.cgst, "sgst": l.sgst, "cess": l.cess, "total": l.total, "tax": l.tax(), "line_hash": l.hash(),
			"status": Gstr2bNotInBook,
		}
		if k, ok := kept[l.hash()]; ok {
			data["decision"], data["decision_note"] = k[0], k[1]
			nKept++
		}
		b, _ := json.Marshal(data)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, $5)`, schema),
			NewDocID("G2B"), Gstr2bLineDoctype, b, Gstr2bNotInBook, userID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	summary, err := ReconcileGSTR2B(tenantID, month, userID)
	if err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "GSTR2B_IMPORT", "SUCCESS", fmt.Sprintf("Imported %d GSTR-2B lines for %s", len(lines), month))
	return &GSTR2BImportResult{Month: month, Imported: len(lines), Kept: nKept, Summary: summary}, nil
}

// ReconcileGSTR2B re-matches the month's stored 2B lines with the vendor bills
// as they are now, and rewrites the "In books not in 2B" lines. A manual
// decision stays where it is.
func ReconcileGSTR2B(tenantID, month, userID string) (map[string]int, error) {
	if _, _, err := gstr1Month(month); err != nil {
		return nil, &ValidationError{Code: gstr2bBadMonthCode, Message: "give the month as YYYY-MM"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	books, err := loadBooksForMonth(schema, month)
	if err != nil {
		return nil, err
	}
	amtTol, dateTol := GetSettingFloat(tenantID, SettingGSTR2BAmountTolerance), float64(GetSettingInt(tenantID, SettingGSTR2BDateToleranceDays))
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
		AND data->>'month' = $2 AND data->>'source' = '2B' ORDER BY id`, schema), Gstr2bLineDoctype, month)
	if err != nil {
		return nil, err
	}
	var stored []gstr2bStored
	for rows.Next() {
		var id string
		var b []byte
		var m map[string]interface{}
		if rows.Scan(&id, &b) != nil || json.Unmarshal(b, &m) != nil {
			continue
		}
		stored = append(stored, gstr2bStored{id: id, line: gstr2bFileLine{
			gstin: strField(m, "supplier_gstin"), name: strField(m, "supplier_name"), docType: strField(m, "doc_type"), number: strField(m, "doc_number"),
			date: strField(m, "doc_date"), taxable: numFromInterface(m["taxable_value"]), igst: numFromInterface(m["igst"]), cgst: numFromInterface(m["cgst"]),
			sgst: numFromInterface(m["sgst"]), cess: numFromInterface(m["cess"]), total: numFromInterface(m["total"])}})
	}
	rows.Close()

	type result struct{ status, reason, bill string }
	results := make([]result, len(stored))
	for i := range results {
		results[i] = result{status: Gstr2bNotInBook}
	}
	// Pass 1: supplier + normalised number. Pass 2: supplier, amount and date.
	// Pass 3: a vendor with no GSTIN in the books, by number. Each pass pairs a
	// 2B line only with a book of the same kind (bill, credit or debit note).
	for i, s := range stored {
		for _, b := range books {
			if !b.used && b.kind == s.line.docType && b.gstin == s.line.gstin && NormalizeInvoiceNumber(b.number) == NormalizeInvoiceNumber(s.line.number) {
				b.used = true
				st, why := gstr2bJudge(s.line, b, amtTol, dateTol)
				results[i] = result{st, why, b.id}
				break
			}
		}
	}
	for i, s := range stored {
		if results[i].bill != "" {
			continue
		}
		for _, b := range books {
			if b.used || b.kind != s.line.docType || b.gstin != s.line.gstin || math.Abs(s.line.total-b.total) > amtTol {
				continue
			}
			if s.line.date != "" && b.date != "" && dayGap(s.line.date, b.date) > dateTol {
				continue
			}
			b.used = true
			st, why := gstr2bJudge(s.line, b, amtTol, dateTol)
			if why == "" {
				why = fmt.Sprintf("matched on amount and date; ours is numbered %s", b.number)
			}
			results[i] = result{st, why, b.id}
			break
		}
	}
	for i, s := range stored {
		if results[i].bill != "" {
			continue
		}
		for _, b := range books {
			if b.used || b.gstin != "" || b.kind != s.line.docType || NormalizeInvoiceNumber(b.number) != NormalizeInvoiceNumber(s.line.number) {
				continue
			}
			b.used = true
			why := fmt.Sprintf("the vendor has no GSTIN in the books; the 2B gives %s", s.line.gstin)
			if _, diff := gstr2bJudge(s.line, b, amtTol, dateTol); diff != "" {
				why += "; " + diff
			}
			results[i] = result{Gstr2bMismatch, why, b.id}
			break
		}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	summary := map[string]int{Gstr2bMatched: 0, Gstr2bMismatch: 0, Gstr2bNotInBook: 0, Gstr2bNotIn2B: 0}
	for i, s := range stored {
		r := results[i]
		patch, _ := json.Marshal(map[string]interface{}{"status": r.status, "reason": r.reason, "bill_id": r.bill})
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3 AND doctype = $4`, schema),
			patch, r.status, s.id, Gstr2bLineDoctype); err != nil {
			return nil, err
		}
		summary[r.status]++
	}
	// Bills the 2B does not carry: rewritten each run.
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET deleted_at = CURRENT_TIMESTAMP WHERE doctype = $1 AND deleted_at IS NULL
		AND data->>'month' = $2 AND data->>'source' = 'Books'`, schema), Gstr2bLineDoctype, month); err != nil {
		return nil, err
	}
	for _, b := range books {
		if b.used || b.gstin == "" { // an unregistered vendor is not on a 2B
			continue
		}
		why := "the supplier has not reported this bill"
		if b.kind != "Invoice" {
			why = "the supplier has not reported this note"
		}
		data := map[string]interface{}{"month": month, "source": "Books", "supplier_gstin": b.gstin, "doc_type": b.kind, "doc_number": b.number,
			"doc_key": NormalizeInvoiceNumber(b.number), "doc_date": b.date, "total": b.total, "tax": b.tax, "bill_id": b.id, "status": Gstr2bNotIn2B,
			"reason": why}
		raw, _ := json.Marshal(data)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, $5)`, schema),
			NewDocID("G2B"), Gstr2bLineDoctype, raw, Gstr2bNotIn2B, userID); err != nil {
			return nil, err
		}
		summary[Gstr2bNotIn2B]++
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return summary, nil
}

// SetGSTR2BDecision records what the accountant decided about a line.
func SetGSTR2BDecision(tenantID, lineID, decision, note, userID string) error {
	switch decision {
	case "", "Accepted", "Rejected", "Query raised":
	default:
		return &ValidationError{Code: gstr2bDecisionCode, Message: "the decision must be Accepted, Rejected or Query raised"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	patch, _ := json.Marshal(map[string]interface{}{"decision": decision, "decision_note": note, "decided_by": userID})
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $2 AND id = $3 AND deleted_at IS NULL`, schema), patch, Gstr2bLineDoctype, lineID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &ValidationError{Code: gstr2bDecisionCode, Message: "that line was not found"}
	}
	LogAuditEvent(tenantID, userID, "GSTR2B_DECISION", "SUCCESS", fmt.Sprintf("Line %s: %s", lineID, decision))
	return nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "gstr2b-reconciliation", Label: "GSTR-2B reconciliation", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "status", Label: "Status"}, {Key: "supplier_gstin", Label: "Supplier GSTIN"}, {Key: "supplier_name", Label: "Supplier"},
			{Key: "doc_type", Label: "Type"}, {Key: "doc_number", Label: "Number"}, {Key: "doc_date", Label: "Date"},
			{Key: "total", Label: "Value"}, {Key: "tax", Label: "Tax"},
			{Key: "bill_id", Label: "Bill / note"}, {Key: "reason", Label: "Why"}, {Key: "decision", Label: "Decision"},
		},
		Params: []ReportParam{
			{Key: "month", Label: "Month (YYYY-MM)", Type: "text", Required: true},
			{Key: "status", Label: "Status", Type: "text"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			if _, _, err := gstr1Month(params["month"]); err != nil {
				return nil, &ValidationError{Code: gstr2bBadMonthCode, Message: "give the month as YYYY-MM"}
			}
			schema, err := db.GetTenantSchema(tenantID)
			if err != nil {
				return nil, err
			}
			rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL AND data->>'month' = $2
				AND ($3 = '' OR status = $3) ORDER BY status, data->>'supplier_gstin', data->>'doc_number'`, schema), Gstr2bLineDoctype, params["month"], params["status"])
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := []map[string]interface{}{}
			for rows.Next() {
				var id string
				var b []byte
				var m map[string]interface{}
				if rows.Scan(&id, &b) == nil && json.Unmarshal(b, &m) == nil {
					m["id"] = id
					out = append(out, m)
				}
			}
			return out, rows.Err()
		},
	})
}
