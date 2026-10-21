package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Stage 94 (user, 2026-10-18): goods receipt in steps.
//
//	① Receive -> ② Verify -> ③ Price check -> ④ MRP and posting (per child) -> ⑤ Pay (optional)
//
// The GRN document stays the one record: a receipt in steps is a GRN with
// flow = "steps", status Pending until its last child posts, and every step's
// figures on its received_items lines. Nothing posts until ④, which posts one
// child at a time through the same receipt posting a one-step GRN uses
// (PostGRNReceiptPart), so stock, batches, barcodes, costing and the GL are
// the existing ones. The decisions (docs/product/grn-receive-verify-mrp-plan-
// 2026-10-18.md, approved 2026-10-18) are cited as D1-D10 where they apply.
//
// Line keys the flow adds beside the existing ones (sku, qty, accepted_qty,
// rejected_qty, damaged_qty, po_id, po_rate, invoice_rate, gst_rate, batch_no):
//
//	line_no       fixed at Receive, how every later step names a line
//	received_qty  typed at Receive
//	invoice_qty   the vendor's invoice qty (defaults to received)
//	grn_qty       Verify: what is really in hand (defaults to received)
//	grn_price     Verify: the price after the vendor call (defaults to invoice_rate)
//	excess_qty / excess_action (keep | return) / excess_status (Pending | Approved)
//	prev_price, prev_grn, prev_vendor, prev_date, price_group, child
//	old_mrp, mrp, posted_at, posted_by
//
// qty is kept equal to what is physically in hand - received at Receive, the
// GRN qty from Verify on - so the PO's received count, the stock posting, the
// stickers and the print all read the one figure they always read.

const (
	GRNFlowSteps    = "steps"
	GRNStepReceived = "Received"
	GRNStepVerified = "Verified"
	GRNStepPriced   = "Priced"
	GRNStepPosted   = "Posted"

	GRNGroupNew     = "new"
	GRNGroupSame    = "same"
	GRNGroupChanged = "changed"

	GRNExcessKeep   = "keep"
	GRNExcessReturn = "return"
)

// grnGroupOrder is the order children are shown and posted in; each group has
// a fixed child letter so a child keeps its name when lines move.
var grnGroupOrder = []string{GRNGroupNew, GRNGroupSame, GRNGroupChanged}
var grnGroupChild = map[string]string{GRNGroupNew: "A", GRNGroupSame: "B", GRNGroupChanged: "C"}
var grnGroupLabel = map[string]string{GRNGroupNew: "New item", GRNGroupSame: "Same price", GRNGroupChanged: "Price changed"}

// Settings keys (Configuration > Procurement).
const (
	SettingGRNSamePriceTolerance = "grn.steps.same_price_tolerance"
	SettingGRNBlindCount         = "grn.steps.blind_count"
	SettingGRNMRPMarkup          = "grn.steps.mrp_markup_pct"
	SettingGRNMRPEnding          = "grn.steps.mrp_price_ending"
	SettingGRNVerifyRoles        = "grn.steps.verify_roles"
	SettingGRNPriceRoles         = "grn.steps.price_roles"
	SettingGRNPostRoles          = "grn.steps.post_roles"
	SettingGRNPayRoles           = "grn.steps.pay_roles"
	SettingGRNCorrectRoles       = "grn.steps.correct_roles"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingGRNSamePriceTolerance, Module: "Procurement",
		Label: "Goods receipt: same price within", Type: SettingTypeFloat, Default: "0", Unit: "₹",
		Min:         settingBound(0),
		Description: "At Price check, a line whose GRN price is within this many rupees of the item's previous purchase price goes to Same price. 0 means equal to the paisa.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingGRNBlindCount, Module: "Procurement",
		Label: "Goods receipt: blind count", Type: SettingTypeBool, Default: "false",
		Description: "When on, the person receiving does not see the PO quantity - they count what arrived. Verify still shows it.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingGRNMRPMarkup, Module: "Procurement",
		Label: "Goods receipt: suggested MRP markup", Type: SettingTypeFloat, Default: "100", Unit: "%",
		Min:         settingBound(0),
		Description: "The suggested MRP at the MRP step is cost plus this markup plus GST, rounded to the price ending below. A category's own MRP markup (Setup > Category) wins over this one.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingGRNMRPEnding, Module: "Procurement",
		Label: "Goods receipt: suggested MRP ending", Type: SettingTypeSelect, Default: "99",
		Options: []SettingOption{
			{Value: "99", Label: "₹x99 - 1,099, 1,199"},
			{Value: "49", Label: "₹x49 or ₹x99 - 1,049, 1,099"},
			{Value: "9", Label: "₹x9 - 1,089, 1,099"},
			{Value: "0", Label: "Round up to ₹10"},
			{Value: "none", Label: "Nearest rupee"},
		},
		Description: "How the suggested MRP is rounded up at the MRP step.",
	})
	for _, s := range []struct{ key, step string }{
		{SettingGRNVerifyRoles, "verify (step 2)"},
		{SettingGRNPriceRoles, "price check (step 3)"},
		{SettingGRNPostRoles, "set MRP and post (step 4)"},
		{SettingGRNPayRoles, "pay the vendor (step 5)"},
		{SettingGRNCorrectRoles, "correct after posting"},
	} {
		RegisterSetting(SettingDefinition{
			Key: s.key, Module: "Procurement",
			Label: "Goods receipt: who may " + s.step, Type: SettingTypeString, Default: "",
			Description: "Role names, separated by commas. Blank lets every role that can edit goods receipts do it - a small shop gives every step to one person.",
		})
	}
	RegisterApprovalDecisionHook("GRNExcessApproval", false, grnExcessDecision)
}

// IsGRNStepFlow reports whether a GRN is received in steps.
func IsGRNStepFlow(data map[string]interface{}) bool {
	return strings.EqualFold(strField(data, "flow"), GRNFlowSteps)
}

// GRNStepAllowed answers "may this role do this step" from the step's role
// setting: blank allows every role (the handler has already checked the GRN
// edit right), otherwise the role must be listed. Super Admin always may.
func GRNStepAllowed(tenantID, role, step string) bool {
	if HasFullAccess(role) {
		return true
	}
	key := map[string]string{
		"verify": SettingGRNVerifyRoles, "price": SettingGRNPriceRoles, "post": SettingGRNPostRoles,
		"pay": SettingGRNPayRoles, "correct": SettingGRNCorrectRoles,
	}[step]
	if key == "" {
		return true
	}
	list := strings.TrimSpace(GetSettingString(tenantID, key))
	if list == "" {
		return true
	}
	for _, r := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(r), strings.TrimSpace(role)) {
			return true
		}
	}
	return false
}

// --- small line helpers ----------------------------------------------------

func grnLineNo(l map[string]interface{}) int {
	return int(numFromInterface(l["line_no"]))
}

// grnLineNum reads a number key, falling back to def when the key is unset.
func grnLineNum(l map[string]interface{}, key string, def float64) float64 {
	if lineNumberSet(l, key) {
		return numFromInterface(l[key])
	}
	return def
}

func grnReceivedQty(l map[string]interface{}) float64 {
	return grnLineNum(l, "received_qty", numFromInterface(l["qty"]))
}
func grnInvoiceQty(l map[string]interface{}) float64 {
	return grnLineNum(l, "invoice_qty", grnReceivedQty(l))
}
func grnInvoicePrice(l map[string]interface{}) float64 {
	return grnLineNum(l, "invoice_rate", numFromInterface(l["po_rate"]))
}
func grnGRNQty(l map[string]interface{}) float64 {
	return grnLineNum(l, "grn_qty", grnReceivedQty(l))
}
func grnGRNPrice(l map[string]interface{}) float64 {
	return grnLineNum(l, "grn_price", grnInvoicePrice(l))
}

// grnStepAccepted is what is kept: the GRN qty less rejected and damaged.
func grnStepAccepted(l map[string]interface{}) float64 {
	q := grnGRNQty(l) - numFromInterface(l["rejected_qty"]) - numFromInterface(l["damaged_qty"])
	if q < 0 {
		return 0
	}
	return q
}

func encodeGRNLines(data map[string]interface{}, lines []map[string]interface{}) {
	b, _ := json.Marshal(lines)
	data["received_items"] = string(b)
}

func grnChildren(data map[string]interface{}) []GRNChild {
	var out []GRNChild
	if raw := strField(data, "children"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func setGRNChildren(data map[string]interface{}, children []GRNChild) {
	b, _ := json.Marshal(children)
	data["children"] = string(b)
}

// GRNChild is one child GRN: a price-check group that posts on its own (D3).
type GRNChild struct {
	Key      string `json:"key"`
	Group    string `json:"group"`
	Name     string `json:"name"`
	Status   string `json:"status"` // Open | Posted
	PostedAt string `json:"posted_at,omitempty"`
	PostedBy string `json:"posted_by,omitempty"`
}

func grnAnyChildPosted(data map[string]interface{}) bool {
	for _, c := range grnChildren(data) {
		if c.Status == GRNStepPosted {
			return true
		}
	}
	return false
}

// grnInvoiceLineTotals is the vendor's invoice as keyed on the lines:
// invoice qty x invoice price, and GST at each line's rate.
func grnInvoiceLineTotals(lines []map[string]interface{}) (value, gst float64) {
	for _, l := range lines {
		v := grnInvoiceQty(l) * grnInvoicePrice(l)
		value += v
		gst += v * numFromInterface(l["gst_rate"]) / 100
	}
	return round2(value), round2(gst)
}

// grnOurTotals is what we owe for (D1): the accepted qty at the GRN price,
// plus GST. Rejected and damaged units are not owed - they go back to the
// vendor, and the invoice-vs-GRN note already leaves them off the payment.
func grnOurTotals(lines []map[string]interface{}) (value, gst float64) {
	for _, l := range lines {
		v := grnStepAccepted(l) * grnGRNPrice(l)
		value += v
		gst += v * numFromInterface(l["gst_rate"]) / 100
	}
	return round2(value), round2(gst)
}

// grnStampTotals recomputes the header figures every screen and report reads.
func grnStampTotals(data map[string]interface{}, lines []map[string]interface{}) {
	iv, ig := grnInvoiceLineTotals(lines)
	ov, og := grnOurTotals(lines)
	data["invoice_lines_value"] = iv
	data["invoice_lines_gst"] = ig
	data["invoice_lines_total"] = round2(iv + ig)
	data["our_value"] = ov
	data["our_gst"] = og
	data["our_total"] = round2(ov + og)
	typed := numFromInterface(data["vendor_invoice_amount"])
	data["invoice_unexplained"] = round2(typed - (iv + ig))
	data["note_difference"] = round2(typed - (ov + og))
}

// --- ① Receive -------------------------------------------------------------

// PrepareGRNSteps runs on a new GRN received in steps, after PrepareGRNPricing
// has stamped the PO prices: the vendor's invoice number, date and total are
// mandatory, the same vendor invoice cannot be received twice, every line is
// numbered and gets its invoice qty, and the GRN saves as Received - nothing
// posts (94.2).
func PrepareGRNSteps(tenantID, userID string, isCreate bool, payload map[string]interface{}) error {
	if !isCreate || !IsGRNStepFlow(payload) {
		return nil
	}
	payload["flow"] = GRNFlowSteps
	var missing []string
	if strField(payload, "vendor_invoice_no") == "" {
		missing = append(missing, "vendor invoice number")
	}
	if strField(payload, "vendor_invoice_date") == "" {
		missing = append(missing, "invoice date")
	}
	if numFromInterface(payload["vendor_invoice_amount"]) <= 0 {
		missing = append(missing, "invoice total (incl. GST)")
	}
	if len(missing) > 0 {
		return &ValidationError{Code: "GOODSR-0940", SubFor: "Vendor Invoice", Message: "enter the " + strings.Join(missing, ", ") + " from the vendor's invoice"}
	}
	if err := checkDuplicateVendorInvoice(tenantID, "", strField(payload, "vendor"), strField(payload, "vendor_invoice_no")); err != nil {
		return err
	}
	lines := grnLineMaps(payload)
	if len(lines) == 0 {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "Lines", Message: "load a PO or add at least one line"}
	}
	for i, l := range lines {
		l["line_no"] = i + 1
		received := numFromInterface(l["qty"])
		if received < 0 || math.IsNaN(received) {
			return &ValidationError{Code: "GOODSR-0952", SubFor: "Received Qty", Message: fmt.Sprintf("received qty for %s must be zero or more", strField(l, "sku"))}
		}
		l["received_qty"] = received
		if lineNumberSet(l, "invoice_qty") {
			iq := numFromInterface(l["invoice_qty"])
			if iq < 0 || math.IsNaN(iq) {
				return &ValidationError{Code: "GOODSR-0952", SubFor: "Invoice Qty", Message: fmt.Sprintf("invoice qty for %s must be zero or more", strField(l, "sku"))}
			}
			l["invoice_qty"] = iq
		} else {
			l["invoice_qty"] = received
		}
		// At Receive nothing is verified yet: the accepted split is the
		// clerk's, defaulting to everything received.
		if !lineNumberSet(l, "accepted_qty") {
			l["accepted_qty"] = received - numFromInterface(l["rejected_qty"]) - numFromInterface(l["damaged_qty"])
		}
	}
	encodeGRNLines(payload, lines)
	payload["grn_step"] = GRNStepReceived
	payload["status"] = "Pending"
	payload["received_by"] = userID
	payload["received_at"] = time.Now().UTC().Format(time.RFC3339)
	grnStampTotals(payload, lines)
	return nil
}

// checkDuplicateVendorInvoice refuses a second receipt of one vendor invoice
// (94.2): same vendor and invoice number on a GRN that is not cancelled.
func checkDuplicateVendorInvoice(tenantID, grnID, vendor, invoiceNo string) error {
	invoiceNo = strings.TrimSpace(invoiceNo)
	if vendor == "" || invoiceNo == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var other string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'code', id) FROM %s.documents
		WHERE doctype = 'GRN' AND deleted_at IS NULL AND status <> 'Cancelled' AND id <> $3
		  AND data->>'vendor' = $1 AND lower(trim(data->>'vendor_invoice_no')) = lower($2)
		LIMIT 1`, schema), vendor, invoiceNo, grnID).Scan(&other)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return &ValidationError{Code: "GOODSR-0941", SubFor: "Vendor Invoice No", Message: fmt.Sprintf(
		"invoice %s of this vendor is already received on goods receipt %s", invoiceNo, other)}
}

// --- loading and saving a step GRN -------------------------------------------

// stepGRN is a GRN loaded for a step, inside the step's transaction.
type stepGRN struct {
	tx     *sql.Tx
	schema string
	id     string
	status string
	data   map[string]interface{}
	lines  []map[string]interface{}
	before []map[string]interface{} // the lines as loaded, for the change log
	header map[string]interface{}   // header fields as loaded, for the change log
}

var grnLoggedHeader = []string{"vendor_invoice_no", "vendor_invoice_date", "vendor_invoice_amount"}

func loadStepGRN(tenantID, grnID string) (*stepGRN, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	if err := db.SetSearchPath(tx, schema); err != nil {
		tx.Rollback()
		return nil, err
	}
	var dataStr, status string
	err = tx.QueryRow(fmt.Sprintf(`SELECT data, COALESCE(status, '') FROM %s.documents WHERE doctype = 'GRN' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), grnID).Scan(&dataStr, &status)
	if err != nil {
		tx.Rollback()
		if err == sql.ErrNoRows {
			return nil, &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("goods receipt %s not found", grnID)}
		}
		return nil, err
	}
	g := &stepGRN{tx: tx, schema: schema, id: grnID, status: status}
	if err := json.Unmarshal([]byte(dataStr), &g.data); err != nil {
		tx.Rollback()
		return nil, err
	}
	if !IsGRNStepFlow(g.data) {
		tx.Rollback()
		return nil, &ValidationError{Code: "GOODSR-0944", Message: fmt.Sprintf("goods receipt %s was posted in one step - it has no steps to work on", grnID)}
	}
	if status == "Cancelled" {
		tx.Rollback()
		return nil, &ValidationError{Code: "GOODSR-0944", Message: fmt.Sprintf("goods receipt %s is cancelled", grnID)}
	}
	g.lines = grnLineMaps(g.data)
	for _, l := range g.lines {
		c := map[string]interface{}{}
		for k, v := range l {
			c[k] = v
		}
		g.before = append(g.before, c)
	}
	g.header = map[string]interface{}{}
	for _, k := range grnLoggedHeader {
		g.header[k] = g.data[k]
	}
	return g, nil
}

func (g *stepGRN) line(no int) map[string]interface{} {
	for _, l := range g.lines {
		if grnLineNo(l) == no {
			return l
		}
	}
	return nil
}

func (g *stepGRN) step() string { return strField(g.data, "grn_step") }

// requireOpen refuses a step on a GRN whose children have started posting:
// from then on only Correct changes it.
func (g *stepGRN) requireOpen() error {
	if g.step() == GRNStepPosted || grnAnyChildPosted(g.data) {
		return &ValidationError{Code: "GOODSR-0944", Message: fmt.Sprintf(
			"goods receipt %s has started posting - use Correct to change it", firstNonEmpty(strField(g.data, "code"), g.id))}
	}
	return nil
}

// save writes the GRN, its line rows and the change log, and commits.
func (g *stepGRN) save(tenantID, userID, step, reason string) error {
	defer g.tx.Rollback()
	encodeGRNLines(g.data, g.lines)
	grnStampTotals(g.data, g.lines)
	if s := strField(g.data, "status"); s != "" {
		g.status = s
	}
	b, err := json.Marshal(g.data)
	if err != nil {
		return err
	}
	if _, err := g.tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'GRN' AND id = $3`, g.schema),
		b, g.status, g.id); err != nil {
		return err
	}
	if grnLineTablesReady(g.tx, g.schema) {
		if err := logGRNChanges(g.tx, g.schema, g.id, step, userID, reason, g.header, g.data, g.before, g.lines); err != nil {
			return err
		}
		if err := writeGRNLineRows(g.tx, g.schema, g.id, g.data, g.lines); err != nil {
			return err
		}
	}
	return g.tx.Commit()
}

// grnLineTablesReady: the two Stage 94 tables exist (a database migrated
// before this stage has none; the flow still works, reports stay empty).
func grnLineTablesReady(q queryRower, schema string) bool {
	var ok bool
	_ = q.QueryRow(`SELECT to_regclass($1) IS NOT NULL AND to_regclass($2) IS NOT NULL`, schema+".grn_line", schema+".grn_line_change").Scan(&ok)
	return ok
}

// --- grn_line rows and the change log (94.1) -------------------------------

type grnExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func nullNum(l map[string]interface{}, key string) interface{} {
	if lineNumberSet(l, key) {
		return numFromInterface(l[key])
	}
	return nil
}

func nullStr(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func nullTime(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

// writeGRNLineRows rewrites a GRN's grn_line rows from its document - the one
// place they are written, so they can never drift from the GRN.
func writeGRNLineRows(ex grnExecer, schema, grnID string, data map[string]interface{}, lines []map[string]interface{}) error {
	if _, err := ex.Exec(fmt.Sprintf(`DELETE FROM %s.grn_line WHERE grn_id = $1`, schema), grnID); err != nil {
		return err
	}
	children := map[string]GRNChild{}
	for _, c := range grnChildren(data) {
		children[c.Key] = c
	}
	for i, l := range lines {
		no := grnLineNo(l)
		if no == 0 {
			no = i + 1
		}
		child := strField(l, "child")
		c := children[child]
		childStatus := c.Status
		postedAt, postedBy := strField(l, "posted_at"), strField(l, "posted_by")
		if !IsGRNStepFlow(data) && strField(data, "status") != "Cancelled" {
			// A one-step GRN posted when it was saved.
			postedAt = firstNonEmpty(postedAt, strField(data, "posted_at"))
		}
		_, err := ex.Exec(fmt.Sprintf(`INSERT INTO %s.grn_line (
			grn_id, line_no, grn_code, flow, vendor, location, vendor_invoice_no, po_id, sku,
			po_qty, po_rate, received_qty, invoice_qty, invoice_price, grn_qty, grn_price, gst_rate,
			rejected_qty, damaged_qty, rejection_reason, damage_reason, excess_qty, excess_action,
			prev_price, prev_grn, prev_vendor, prev_date, price_group, child_key, child_name,
			old_mrp, new_mrp, batch_no, step, child_status,
			received_by, received_at, verified_by, verified_at, priced_by, priced_at, posted_by, posted_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,
			        $31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,NOW())`, schema),
			grnID, no, nullStr(firstNonEmpty(strField(data, "code"), grnID)), nullStr(strField(data, "flow")),
			nullStr(strField(data, "vendor")), nullStr(strField(data, "location")), nullStr(strField(data, "vendor_invoice_no")),
			nullStr(firstNonEmpty(strField(l, "po_id"), strField(data, "po_id"))), strField(l, "sku"),
			nullNum(l, "ordered_qty"), nullNum(l, "po_rate"), grnReceivedQty(l), grnInvoiceQty(l), round2(grnInvoicePrice(l)),
			grnGRNQty(l), round2(grnGRNPrice(l)), nullNum(l, "gst_rate"),
			numFromInterface(l["rejected_qty"]), numFromInterface(l["damaged_qty"]),
			nullStr(strField(l, "rejection_reason")), nullStr(strField(l, "damage_reason")),
			nullNum(l, "excess_qty"), nullStr(strField(l, "excess_action")),
			nullNum(l, "prev_price"), nullStr(strField(l, "prev_grn")), nullStr(strField(l, "prev_vendor")), nullTime(strField(l, "prev_date")),
			nullStr(strField(l, "price_group")), nullStr(child), nullStr(c.Name),
			nullNum(l, "old_mrp"), nullNum(l, "mrp"), nullStr(strField(l, "batch_no")),
			nullStr(firstNonEmpty(strField(data, "grn_step"), map[bool]string{true: GRNStepPosted}[postedAt != ""])), nullStr(childStatus),
			nullStr(strField(data, "received_by")), nullTime(strField(data, "received_at")),
			nullStr(strField(data, "verified_by")), nullTime(strField(data, "verified_at")),
			nullStr(strField(data, "priced_by")), nullTime(strField(data, "priced_at")),
			nullStr(postedBy), nullTime(postedAt))
		if err != nil {
			return err
		}
	}
	return nil
}

// grnLoggedFields are the line figures whose every change is logged.
var grnLoggedFields = []string{"received_qty", "invoice_qty", "invoice_rate", "grn_qty", "grn_price",
	"rejected_qty", "damaged_qty", "excess_action", "child", "mrp"}

func grnFieldText(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case string:
		return strings.TrimSpace(x)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func logGRNChanges(ex grnExecer, schema, grnID, step, userID, reason string, header, data map[string]interface{}, before, after []map[string]interface{}) error {
	write := func(lineNo interface{}, sku, field, oldV, newV string) error {
		_, err := ex.Exec(fmt.Sprintf(`INSERT INTO %s.grn_line_change (grn_id, line_no, sku, step, field, old_value, new_value, reason, changed_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, schema), grnID, lineNo, nullStr(sku), step, field, nullStr(oldV), nullStr(newV), nullStr(reason), nullStr(userID))
		return err
	}
	for _, k := range grnLoggedHeader {
		o, n := grnFieldText(header[k]), grnFieldText(data[k])
		if o != n && !numbersEqualText(o, n) {
			if err := write(nil, "", k, o, n); err != nil {
				return err
			}
		}
	}
	old := map[int]map[string]interface{}{}
	for _, l := range before {
		old[grnLineNo(l)] = l
	}
	for _, l := range after {
		prev := old[grnLineNo(l)]
		if prev == nil {
			continue
		}
		for _, k := range grnLoggedFields {
			o, n := grnFieldText(prev[k]), grnFieldText(l[k])
			if o == n || numbersEqualText(o, n) {
				continue
			}
			if err := write(grnLineNo(l), strField(l, "sku"), k, o, n); err != nil {
				return err
			}
		}
	}
	return nil
}

func numbersEqualText(a, b string) bool {
	x, e1 := strconv.ParseFloat(a, 64)
	y, e2 := strconv.ParseFloat(b, 64)
	return e1 == nil && e2 == nil && math.Abs(x-y) < 0.0005
}

// SyncGRNLineRows rewrites a GRN's grn_line rows outside any step - every GRN
// save calls it, so a one-step GRN is in the reports too.
func SyncGRNLineRows(tenantID, grnID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || !grnLineTablesReady(db.DB, schema) {
		return
	}
	data, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		return
	}
	lines := grnLineMaps(data)
	if !IsGRNStepFlow(data) && strField(data, "posted_at") == "" {
		// A one-step GRN posts as it is saved, which is when this runs.
		data["posted_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	if err := writeGRNLineRows(db.DB, schema, grnID, data, lines); err != nil {
		LogSystemError(tenantID, "", "WARN", "SyncGRNLineRows", fmt.Sprintf("GRN %s: line rows not written: %v", grnID, err), "")
	}
}

// --- edit Receive (Back to step 1) -----------------------------------------

// GRNReceiveEdit changes what was keyed at Receive, before posting.
type GRNReceiveEdit struct {
	InvoiceNo     *string  `json:"vendor_invoice_no"`
	InvoiceDate   *string  `json:"vendor_invoice_date"`
	InvoiceAmount *float64 `json:"vendor_invoice_amount"`
	Lines         []struct {
		LineNo       int      `json:"line_no"`
		ReceivedQty  *float64 `json:"received_qty"`
		InvoiceQty   *float64 `json:"invoice_qty"`
		InvoicePrice *float64 `json:"invoice_price"`
	} `json:"lines"`
	Reason string `json:"reason"`
}

// SaveGRNReceive applies a Receive edit. The receipt goes back to Received,
// so the figures are verified again from what changed.
func SaveGRNReceive(tenantID, userID, grnID string, in GRNReceiveEdit) (*GRNStepView, error) {
	g, err := loadStepGRN(tenantID, grnID)
	if err != nil {
		return nil, err
	}
	if err := g.requireOpen(); err != nil {
		g.tx.Rollback()
		return nil, err
	}
	if in.InvoiceNo != nil {
		no := strings.TrimSpace(*in.InvoiceNo)
		if no == "" {
			g.tx.Rollback()
			return nil, &ValidationError{Code: "GOODSR-0940", SubFor: "Vendor Invoice No", Message: "enter the vendor invoice number"}
		}
		if err := checkDuplicateVendorInvoice(tenantID, grnID, strField(g.data, "vendor"), no); err != nil {
			g.tx.Rollback()
			return nil, err
		}
		g.data["vendor_invoice_no"] = no
	}
	if in.InvoiceDate != nil {
		if strings.TrimSpace(*in.InvoiceDate) == "" {
			g.tx.Rollback()
			return nil, &ValidationError{Code: "GOODSR-0940", SubFor: "Invoice Date", Message: "enter the invoice date"}
		}
		g.data["vendor_invoice_date"] = strings.TrimSpace(*in.InvoiceDate)
	}
	if in.InvoiceAmount != nil {
		if *in.InvoiceAmount <= 0 || math.IsNaN(*in.InvoiceAmount) {
			g.tx.Rollback()
			return nil, &ValidationError{Code: "GOODSR-0940", SubFor: "Invoice Total", Message: "enter the invoice total (incl. GST)"}
		}
		g.data["vendor_invoice_amount"] = round2(*in.InvoiceAmount)
	}
	for _, e := range in.Lines {
		l := g.line(e.LineNo)
		if l == nil {
			g.tx.Rollback()
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Lines", Message: fmt.Sprintf("line %d is not on this receipt", e.LineNo)}
		}
		for _, f := range []struct {
			v   *float64
			key string
		}{{e.ReceivedQty, "received_qty"}, {e.InvoiceQty, "invoice_qty"}, {e.InvoicePrice, "invoice_rate"}} {
			if f.v == nil {
				continue
			}
			if *f.v < 0 || math.IsNaN(*f.v) || math.IsInf(*f.v, 0) {
				g.tx.Rollback()
				return nil, &ValidationError{Code: "GOODSR-0952", SubFor: f.key, Message: fmt.Sprintf("line %d: %s must be zero or more", e.LineNo, strings.ReplaceAll(f.key, "_", " "))}
			}
			l[f.key] = round2(*f.v)
		}
		if e.ReceivedQty != nil && g.step() == GRNStepReceived {
			l["qty"] = *e.ReceivedQty
			l["accepted_qty"] = math.Max(0, *e.ReceivedQty-numFromInterface(l["rejected_qty"])-numFromInterface(l["damaged_qty"]))
		}
	}
	if g.step() != GRNStepReceived {
		// Verified figures were checked against the old Receive figures.
		g.data["grn_step"] = GRNStepReceived
		delete(g.data, "children")
	}
	if err := g.save(tenantID, userID, GRNStepReceived, in.Reason); err != nil {
		return nil, err
	}
	return GetGRNStepView(tenantID, grnID)
}

// --- ② Verify ---------------------------------------------------------------

// GRNVerifyLine is one line's verified figures. Nil keeps the default (or the
// value saved at an earlier Verify).
type GRNVerifyLine struct {
	LineNo          int      `json:"line_no"`
	GRNQty          *float64 `json:"grn_qty"`
	GRNPrice        *float64 `json:"grn_price"`
	RejectedQty     *float64 `json:"rejected_qty"`
	RejectionReason *string  `json:"rejection_reason"`
	DamagedQty      *float64 `json:"damaged_qty"`
	DamageReason    *string  `json:"damage_reason"`
	ExcessAction    string   `json:"excess_action"`
	// Stage 74.1.1: the reasons picked from the reason codes.
	RejectionReasonCode *string `json:"rejection_reason_code"`
	DamageReasonCode    *string `json:"damage_reason_code"`
}

type GRNVerifyInput struct {
	Lines        []GRNVerifyLine `json:"lines"`
	ExcessReason string          `json:"excess_reason"`
	Reason       string          `json:"reason"`
}

// VerifyGRNStep saves step ② (94.3): GRN qty and price per line, rejected and
// damaged with reasons, the invoice total checked against the invoice lines
// (D5), and goods beyond the PO decided Keep or Return (D6).
func VerifyGRNStep(tenantID, userID, role, grnID string, in GRNVerifyInput) (*GRNStepView, error) {
	g, err := loadStepGRN(tenantID, grnID)
	if err != nil {
		return nil, err
	}
	if err := g.requireOpen(); err != nil {
		g.tx.Rollback()
		return nil, err
	}
	excessReq, err := verifyGRNLines(tenantID, g, in)
	if err != nil {
		g.tx.Rollback()
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	g.data["grn_step"] = GRNStepVerified
	g.data["verified_by"] = userID
	g.data["verified_at"] = now
	delete(g.data, "children")
	if err := g.save(tenantID, userID, GRNStepVerified, in.Reason); err != nil {
		return nil, err
	}
	if excessReq != nil {
		if err := raiseGRNExcessApproval(tenantID, userID, role, grnID, excessReq, in.ExcessReason); err != nil {
			return nil, err
		}
	}
	return GetGRNStepView(tenantID, grnID)
}

// grnExcessRequest is what Keep asks an approver to accept.
type grnExcessRequest struct {
	Lines []map[string]interface{}
	Value float64
}

func verifyGRNLines(tenantID string, g *stepGRN, in GRNVerifyInput) (*grnExcessRequest, error) {
	byNo := map[int]GRNVerifyLine{}
	for _, v := range in.Lines {
		byNo[v.LineNo] = v
	}
	for _, l := range g.lines {
		v := byNo[grnLineNo(l)]
		set := func(p *float64, key, label string) error {
			if p == nil {
				return nil
			}
			if *p < 0 || math.IsNaN(*p) || math.IsInf(*p, 0) {
				return &ValidationError{Code: "GOODSR-0952", SubFor: label, Message: fmt.Sprintf("line %d (%s): %s must be zero or more", grnLineNo(l), strField(l, "sku"), strings.ToLower(label))}
			}
			l[key] = round2(*p)
			return nil
		}
		if err := set(v.GRNQty, "grn_qty", "GRN Qty"); err != nil {
			return nil, err
		}
		if err := set(v.GRNPrice, "grn_price", "GRN Price"); err != nil {
			return nil, err
		}
		// rejected_qty holds the clerk's own rejects plus any excess sent
		// back (excess_returned); Verify edits the clerk's own part.
		own := math.Max(0, numFromInterface(l["rejected_qty"])-numFromInterface(l["excess_returned"]))
		delete(l, "excess_returned")
		l["rejected_qty"] = own
		if err := set(v.RejectedQty, "rejected_qty", "Rejected Qty"); err != nil {
			return nil, err
		}
		if err := set(v.DamagedQty, "damaged_qty", "Damaged Qty"); err != nil {
			return nil, err
		}
		if v.RejectionReason != nil {
			l["rejection_reason"] = strings.TrimSpace(*v.RejectionReason)
		}
		if v.DamageReason != nil {
			l["damage_reason"] = strings.TrimSpace(*v.DamageReason)
		}
		if v.RejectionReasonCode != nil {
			l["rejection_reason_code"] = strings.TrimSpace(*v.RejectionReasonCode)
		}
		if v.DamageReasonCode != nil {
			l["damage_reason_code"] = strings.TrimSpace(*v.DamageReasonCode)
		}
		if !lineNumberSet(l, "grn_qty") {
			l["grn_qty"] = grnReceivedQty(l)
		}
		if !lineNumberSet(l, "grn_price") {
			l["grn_price"] = round2(grnInvoicePrice(l))
		}
		if err := applyLineReason(tenantID, l, "rejected_qty", "rejection_reason", "rejection_reason_code", ReasonGRNRejection); err != nil {
			return nil, err
		}
		if err := applyLineReason(tenantID, l, "damaged_qty", "damage_reason", "damage_reason_code", ReasonGRNDamage); err != nil {
			return nil, err
		}
		rej, dmg, qty := numFromInterface(l["rejected_qty"]), numFromInterface(l["damaged_qty"]), grnGRNQty(l)
		if rej+dmg > qty+1e-9 {
			return nil, &ValidationError{Code: "GOODSR-0097", Message: fmt.Sprintf("line %d (%s): rejected (%v) plus damaged (%v) cannot exceed the GRN qty (%v)", grnLineNo(l), strField(l, "sku"), rej, dmg, qty)}
		}
		if rej > 0 && strField(l, "rejection_reason") == "" {
			return nil, &ValidationError{Code: "GOODSR-0090", Message: fmt.Sprintf("line %d (%s): give the reason for the %v rejected", grnLineNo(l), strField(l, "sku"), rej)}
		}
		if dmg > 0 && strField(l, "damage_reason") == "" {
			return nil, &ValidationError{Code: "GOODSR-0096", Message: fmt.Sprintf("line %d (%s): give the reason for the %v damaged", grnLineNo(l), strField(l, "sku"), dmg)}
		}
		l["qty"] = qty
		l["accepted_qty"] = grnStepAccepted(l)
		l["excess_action"] = strings.ToLower(strings.TrimSpace(firstNonEmpty(v.ExcessAction, strField(l, "excess_action"))))
	}

	// D5: the typed invoice total must be explained by the invoice lines
	// (GST cannot be split on a lump sum).
	if c := grnInvoiceCheck(g.lines, numFromInterface(g.data["vendor_invoice_amount"])); c != nil {
		return nil, &ValidationError{Code: "GOODSR-0942", SubFor: "Invoice Total", Message: c.Message}
	}

	// D6: goods beyond the PO (plus the vendor's tolerance) or not on it.
	excess, err := grnExcessByLine(tenantID, g)
	if err != nil {
		return nil, err
	}
	var undecided []string
	req := &grnExcessRequest{}
	for _, l := range g.lines {
		ex := excess[grnLineNo(l)]
		if ex <= 1e-9 {
			delete(l, "excess_qty")
			delete(l, "excess_status")
			if strField(l, "excess_action") != "" {
				l["excess_action"] = ""
			}
			continue
		}
		l["excess_qty"] = ex
		switch strField(l, "excess_action") {
		case GRNExcessReturn:
			l["rejected_qty"] = round2(numFromInterface(l["rejected_qty"]) + ex)
			l["excess_returned"] = ex
			if r := strField(l, "rejection_reason"); r == "" {
				l["rejection_reason"] = "Excess over PO - return to vendor"
			}
			l["accepted_qty"] = grnStepAccepted(l)
			delete(l, "excess_status")
		case GRNExcessKeep:
			if strField(l, "excess_status") != "Approved" {
				l["excess_status"] = "Pending"
				req.Lines = append(req.Lines, map[string]interface{}{"line_no": grnLineNo(l), "sku": strField(l, "sku"), "excess_qty": ex, "grn_price": grnGRNPrice(l)})
				req.Value += ex * grnGRNPrice(l)
			}
		default:
			undecided = append(undecided, fmt.Sprintf("line %d (%s, %v beyond the PO)", grnLineNo(l), strField(l, "sku"), ex))
		}
	}
	if len(undecided) > 0 {
		return nil, &ValidationError{Code: "GOODSR-0943", SubFor: "Excess", Message: "choose Keep (asks for approval) or Return to vendor for " + strings.Join(undecided, ", ")}
	}
	if len(req.Lines) == 0 {
		return nil, nil
	}
	req.Value = round2(req.Value)
	return req, nil
}

// GRNInvoiceCheck says, in words anyone on the receiving desk follows, why
// the typed invoice total and the invoice lines do not agree (D5), and which
// box on Receive to fix: the total, or one line's invoice qty or price. The
// Verify refusal and the screen both read it, so they always say the same.
type GRNInvoiceCheck struct {
	Typed    float64 `json:"typed"`
	LinesSum float64 `json:"lines_total"`
	Gap      float64 `json:"gap"` // typed - lines
	LineNo   int     `json:"line_no,omitempty"`
	SKU      string  `json:"sku,omitempty"`
	Field    string  `json:"field"` // invoice_qty | invoice_price | total
	Message  string  `json:"message"`
}

// grnInvoiceCheck is nil when the total and its lines agree within Rs 1.
// The likely line is one whose qty is off by whole units, else the largest.
func grnInvoiceCheck(lines []map[string]interface{}, typed float64) *GRNInvoiceCheck {
	iv, ig := grnInvoiceLineTotals(lines)
	sum := round2(iv + ig)
	gap := round2(typed - sum)
	if math.Abs(gap) <= 1 {
		return nil
	}
	c := &GRNInvoiceCheck{Typed: round2(typed), LinesSum: sum, Gap: gap, Field: "total"}
	more := "more"
	if gap < 0 {
		more = "less"
	}
	head := fmt.Sprintf("The bill's total is %.2f, but the items on it add up to %.2f - the total is %.2f %s than the items.",
		c.Typed, sum, math.Abs(gap), more)
	for _, l := range lines {
		unit := grnInvoicePrice(l) * (1 + numFromInterface(l["gst_rate"])/100)
		if unit <= 0 {
			continue
		}
		n := gap / unit
		if r := math.Round(n); r != 0 && math.Abs(n-r) < 0.01 {
			c.LineNo, c.SKU, c.Field = grnLineNo(l), strField(l, "sku"), "invoice_qty"
			c.Message = fmt.Sprintf("%s That is exactly %v unit(s) of line %d (%s). Check the invoice qty on line %d against the bill, or the total you typed.",
				head, math.Abs(r), c.LineNo, c.SKU, c.LineNo)
			return c
		}
	}
	best, bestV := -1, 0.0
	for i, l := range lines {
		if v := grnInvoiceQty(l) * grnInvoicePrice(l); v > bestV {
			best, bestV = i, v
		}
	}
	if best < 0 {
		c.Message = head + " Check the total you typed and every line's invoice qty and price."
		return c
	}
	l := lines[best]
	// The per-unit hint is in the terms the line's price box shows: incl.
	// GST for a line of a GST-inclusive PO (Stage 110.1), else before GST.
	per, terms := 0.0, "before GST"
	if q := grnInvoiceQty(l); q > 0 {
		per = gap / (q * (1 + numFromInterface(l["gst_rate"])/100))
		if truthy(l["gst_inclusive"]) {
			per, terms = gap/q, "incl. GST"
		}
	}
	c.LineNo, c.SKU, c.Field = grnLineNo(l), strField(l, "sku"), "invoice_price"
	c.Message = fmt.Sprintf("%s First check the total you typed against the bill. If the total is right, check the invoice price on line %d (%s): about %.2f %s per unit (%s) would make them match.",
		head, c.LineNo, c.SKU, math.Abs(round2(per)), more, terms)
	return c
}

// grnExcessByLine is each line's qty beyond what its PO still had open plus
// the vendor's over-receipt tolerance (ReceiptValidationRule, the existing
// setting), or its whole accepted qty when the item is not on the PO.
func grnExcessByLine(tenantID string, g *stepGRN) (map[int]float64, error) {
	out := map[int]float64{}
	pct, allowUnexpected, err := getReceiptValidationRule(tenantID, strField(g.data, "vendor"))
	if err != nil {
		return nil, err
	}
	type key struct{ po, sku string }
	groups := map[key][]map[string]interface{}{}
	var order []key
	headerPO := strField(g.data, "po_id")
	for _, l := range g.lines {
		k := key{firstNonEmpty(strField(l, "po_id"), headerPO), strField(l, "sku")}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], l)
	}
	ordered := map[string]map[string]float64{}
	before := map[string]map[string]float64{}
	for _, k := range order {
		ls := groups[k]
		var kept float64
		for _, l := range ls {
			kept += numFromInterface(l["grn_qty"]) - numFromInterface(l["rejected_qty"]) - numFromInterface(l["damaged_qty"])
		}
		if k.po == "" {
			if !allowUnexpected {
				out[grnLineNo(ls[len(ls)-1])] = round2(kept)
			}
			continue
		}
		if _, ok := ordered[k.po]; !ok {
			o, err := fetchPOItemQuantities(tenantID, k.po)
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
			ordered[k.po] = o
			b, err := fetchGRNAcceptedQuantities(tenantID, k.po, g.id)
			if err != nil {
				return nil, err
			}
			before[k.po] = b
		}
		orderedQty, onPO := ordered[k.po][k.sku]
		var allowed float64
		if onPO {
			allowed = orderedQty*(1+pct/100) - before[k.po][k.sku]
		} else if allowUnexpected {
			continue
		}
		if ex := round2(kept - math.Max(0, allowed)); ex > 1e-9 {
			// The excess sits on the SKU's last line, where a Return takes it.
			out[grnLineNo(ls[len(ls)-1])] = ex
		}
	}
	return out, nil
}

// raiseGRNExcessApproval sends Keep to the approval engine (D6) as a
// GRNExcessApproval record; grnExcessDecision acts on the answer.
func raiseGRNExcessApproval(tenantID, userID, role, grnID string, req *grnExcessRequest, reason string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	grn, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		return err
	}
	// One open request per GRN: a second Verify replaces the first.
	if old := strField(grn, "excess_approval_id"); old != "" {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Rejected', data = data || '{"status":"Rejected","superseded":true}'::jsonb, updated_at = CURRENT_TIMESTAMP
			WHERE doctype = 'GRNExcessApproval' AND id = $1 AND status IN ('Draft','Pending Approval')`, schema), old)
	}
	id := NewDocID("GEXA")
	linesJSON, _ := json.Marshal(req.Lines)
	doc := map[string]interface{}{
		"code": id, "grn_id": grnID, "vendor": strField(grn, "vendor"),
		"excess_lines": string(linesJSON), "excess_value": req.Value, "amount": req.Value,
		"reason": strings.TrimSpace(reason), "requested_by": userID, "status": "Draft",
	}
	b, _ := json.Marshal(doc)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'GRNExcessApproval', $2, 'Draft', $3)`, schema), id, b, userID); err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = jsonb_set(data, '{excess_approval_id}', to_jsonb($1::text)), updated_at = CURRENT_TIMESTAMP WHERE doctype = 'GRN' AND id = $2`, schema), id, grnID); err != nil {
		return err
	}
	if err := SubmitForApproval(tenantID, "GRNExcessApproval", id, userID, role); err != nil {
		return fmt.Errorf("the excess was saved as Keep, but could not be sent for approval: %w", err)
	}
	LogAuditEvent(tenantID, userID, "GRN_EXCESS_KEEP_REQUESTED", "SUCCESS", fmt.Sprintf("GRN %s: keeping goods beyond the PO worth %.2f sent for approval (%s)", grnID, req.Value, id))
	return nil
}

// grnExcessDecision is the approval hook: Approved releases the kept excess
// for posting; Rejected turns it into Return to vendor.
func grnExcessDecision(tenantID, docID, decision, actorUserID string) {
	req, _, err := fetchDocData(tenantID, "GRNExcessApproval", docID)
	if err != nil {
		return
	}
	grnID := strField(req, "grn_id")
	g, err := loadStepGRN(tenantID, grnID)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "grnExcessDecision", fmt.Sprintf("GRN %s: excess decision %s on %s not applied: %v", grnID, decision, docID, err), "")
		return
	}
	if strField(g.data, "excess_approval_id") != docID {
		g.tx.Rollback()
		return // superseded by a later Verify
	}
	approved := strings.EqualFold(decision, "Approved") || strings.EqualFold(decision, "Approve")
	for _, l := range g.lines {
		if strField(l, "excess_status") != "Pending" {
			continue
		}
		if approved {
			l["excess_status"] = "Approved"
			continue
		}
		ex := numFromInterface(l["excess_qty"])
		l["excess_action"] = GRNExcessReturn
		l["rejected_qty"] = round2(numFromInterface(l["rejected_qty"]) + ex)
		l["excess_returned"] = ex
		if strField(l, "rejection_reason") == "" {
			l["rejection_reason"] = "Excess over PO not approved - return to vendor"
		}
		l["accepted_qty"] = grnStepAccepted(l)
		delete(l, "excess_status")
	}
	if err := g.save(tenantID, actorUserID, "Excess "+decision, "Excess approval "+docID+": "+decision); err != nil {
		LogSystemError(tenantID, "", "ERROR", "grnExcessDecision", fmt.Sprintf("GRN %s: excess decision not saved: %v", grnID, err), "")
	}
}

// --- ③ Price check ------------------------------------------------------------

// GRNPriceCheckInput regroups lines and renames children. Moves are by SKU:
// a SKU's lines always sit in one child, so its stock posts once.
type GRNPriceCheckInput struct {
	Moves   map[string]string `json:"moves"` // sku -> group (new | same | changed)
	Names   map[string]string `json:"names"` // group -> child name
	Regroup bool              `json:"regroup"`
	Reason  string            `json:"reason"`
}

// PriceCheckGRNStep saves step ③ (94.4): each line's GRN price against the
// item's previous purchase price (D4) sorts it into New item, Same price or
// Price changed (D3), one child GRN each; lines can be moved and children
// renamed.
func PriceCheckGRNStep(tenantID, userID, grnID string, in GRNPriceCheckInput) (*GRNStepView, error) {
	g, err := loadStepGRN(tenantID, grnID)
	if err != nil {
		return nil, err
	}
	if err := g.requireOpen(); err != nil {
		g.tx.Rollback()
		return nil, err
	}
	if s := g.step(); s != GRNStepVerified && s != GRNStepPriced {
		g.tx.Rollback()
		return nil, &ValidationError{Code: "GOODSR-0944", Message: "verify the receipt first (step 2)"}
	}
	applyGRNPriceCheck(tenantID, g, in)
	g.data["grn_step"] = GRNStepPriced
	g.data["priced_by"] = userID
	g.data["priced_at"] = time.Now().UTC().Format(time.RFC3339)
	if err := g.save(tenantID, userID, GRNStepPriced, in.Reason); err != nil {
		return nil, err
	}
	return GetGRNStepView(tenantID, grnID)
}

func applyGRNPriceCheck(tenantID string, g *stepGRN, in GRNPriceCheckInput) {
	// Same means equal to the paisa unless the org allows a tolerance.
	tol := math.Max(GetSettingFloat(tenantID, SettingGRNSamePriceTolerance), 0.004)
	// The SKU's group: worst of its lines (changed > new > same), so a SKU
	// never splits across children.
	rank := map[string]int{GRNGroupSame: 0, GRNGroupNew: 1, GRNGroupChanged: 2}
	skuGroup := map[string]string{}
	for _, l := range g.lines {
		sku := strField(l, "sku")
		prev, ok := grnPreviousPrice(g.tx, g.schema, sku, g.id)
		group := GRNGroupNew
		if ok {
			l["prev_price"] = prev.Price
			l["prev_grn"] = prev.GRN
			l["prev_vendor"] = prev.Vendor
			l["prev_date"] = prev.Date
			group = GRNGroupSame
			if math.Abs(grnGRNPrice(l)-prev.Price) > tol {
				group = GRNGroupChanged
			}
		} else {
			for _, k := range []string{"prev_price", "prev_grn", "prev_vendor", "prev_date"} {
				delete(l, k)
			}
		}
		if cur, seen := skuGroup[sku]; !seen || rank[group] > rank[cur] {
			skuGroup[sku] = group
		}
	}
	// price_group is what the prices say; moved_to is where a person put the
	// SKU, kept across later checks until Regroup clears it.
	for _, l := range g.lines {
		sku := strField(l, "sku")
		l["price_group"] = skuGroup[sku]
		if in.Regroup {
			delete(l, "moved_to")
		}
		if moved, ok := in.Moves[sku]; ok && grnGroupChild[moved] != "" {
			l["moved_to"] = moved
		}
		group := skuGroup[sku]
		if m := strField(l, "moved_to"); grnGroupChild[m] != "" {
			group = m
		}
		l["child"] = grnGroupChild[group]
	}
	old := map[string]GRNChild{}
	for _, c := range grnChildren(g.data) {
		old[c.Group] = c
	}
	var children []GRNChild
	for _, group := range grnGroupOrder {
		key := grnGroupChild[group]
		used := false
		for _, l := range g.lines {
			if strField(l, "child") == key {
				used = true
				break
			}
		}
		if !used {
			continue
		}
		name := firstNonEmpty(strings.TrimSpace(in.Names[group]), old[group].Name, grnGroupLabel[group])
		children = append(children, GRNChild{Key: key, Group: group, Name: name, Status: "Open"})
	}
	setGRNChildren(g.data, children)
}

// grnPrevPrice is an item's previous purchase price (D4).
type grnPrevPrice struct {
	Price  float64
	GRN    string
	Vendor string
	Date   string
}

// grnPreviousPrice is the last posted GRN price of a SKU from any vendor: the
// grn_line rows first, then - for receipts posted before Stage 94 - the
// latest posted GRN document carrying the SKU, at its GRN, invoice or PO rate.
func grnPreviousPrice(q queryRower, schema, sku, excludeGRN string) (grnPrevPrice, bool) {
	var p grnPrevPrice
	if grnLineTablesReady(q, schema) {
		var price sql.NullFloat64
		var code, vendor sql.NullString
		var at sql.NullTime
		err := q.QueryRow(fmt.Sprintf(`SELECT grn_price, grn_code, vendor, posted_at FROM %s.grn_line
			WHERE sku = $1 AND grn_id <> $2 AND posted_at IS NOT NULL AND grn_price IS NOT NULL
			ORDER BY posted_at DESC, grn_id DESC LIMIT 1`, schema), sku, excludeGRN).Scan(&price, &code, &vendor, &at)
		if err == nil && price.Valid {
			p = grnPrevPrice{Price: round2(price.Float64), GRN: code.String, Vendor: vendor.String}
			if at.Valid {
				p.Date = at.Time.Format("2006-01-02")
			}
			return p, true
		}
	}
	rows, err := q.Query(fmt.Sprintf(`SELECT id, data, created_at FROM %s.documents
		WHERE doctype = 'GRN' AND status = 'Approved' AND deleted_at IS NULL AND id <> $2
		  AND position($1 in COALESCE(data->>'received_items', '')) > 0
		ORDER BY created_at DESC LIMIT 5`, schema), `"sku":"`+sku+`"`, excludeGRN)
	if err != nil {
		return p, false
	}
	defer rows.Close()
	for rows.Next() {
		var id, dataStr string
		var at time.Time
		if rows.Scan(&id, &dataStr, &at) != nil {
			continue
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(dataStr), &data) != nil {
			continue
		}
		for _, l := range grnLineMaps(data) {
			if strField(l, "sku") != sku {
				continue
			}
			price := 0.0
			for _, k := range []string{"grn_price", "invoice_rate", "po_rate"} {
				if lineNumberSet(l, k) {
					price = numFromInterface(l[k])
					break
				}
			}
			if price <= 0 {
				continue
			}
			return grnPrevPrice{Price: round2(price), GRN: firstNonEmpty(strField(data, "code"), id), Vendor: strField(data, "vendor"), Date: at.Format("2006-01-02")}, true
		}
	}
	return p, false
}

// --- the read model every step screen renders --------------------------------

// GRNStepLineView is one line with every step's figures.
type GRNStepLineView struct {
	LineNo       int      `json:"line_no"`
	SKU          string   `json:"sku"`
	ItemName     string   `json:"item_name"`
	POID         string   `json:"po_id"`
	POOpenQty    *float64 `json:"po_open_qty,omitempty"`
	PORate       *float64 `json:"po_rate,omitempty"`
	ReceivedQty  float64  `json:"received_qty"`
	InvoiceQty   float64  `json:"invoice_qty"`
	InvoicePrice float64  `json:"invoice_price"`
	GRNQty       float64  `json:"grn_qty"`
	GRNPrice     float64  `json:"grn_price"`
	GSTRate      float64  `json:"gst_rate"`
	// GSTInclusive: the line's PO prices include GST, so the screen shows
	// and takes its prices that way (Stage 110.1); the figures here stay ex-GST.
	GSTInclusive    bool     `json:"gst_inclusive,omitempty"`
	RejectedQty     float64  `json:"rejected_qty"` // the clerk's own rejects
	ExcessReturned  float64  `json:"excess_returned,omitempty"`
	RejectionReason string   `json:"rejection_reason,omitempty"`
	RejectionCode   string   `json:"rejection_reason_code,omitempty"`
	DamageCode      string   `json:"damage_reason_code,omitempty"`
	DamagedQty      float64  `json:"damaged_qty"`
	DamageReason    string   `json:"damage_reason,omitempty"`
	AcceptedQty     float64  `json:"accepted_qty"`
	InvoiceValue    float64  `json:"invoice_value"` // incl. GST
	OurValue        float64  `json:"our_value"`     // incl. GST
	QtyDiff         float64  `json:"qty_diff"`      // GRN qty - invoice qty
	PriceDiff       float64  `json:"price_diff"`    // GRN price - invoice price
	ExcessQty       float64  `json:"excess_qty,omitempty"`
	ExcessAction    string   `json:"excess_action,omitempty"`
	ExcessStatus    string   `json:"excess_status,omitempty"`
	PrevPrice       *float64 `json:"prev_price,omitempty"`
	PrevGRN         string   `json:"prev_grn,omitempty"`
	PrevVendor      string   `json:"prev_vendor,omitempty"`
	PrevDate        string   `json:"prev_date,omitempty"`
	PriceChangePct  *float64 `json:"price_change_pct,omitempty"`
	PriceGroup      string   `json:"price_group,omitempty"`
	Child           string   `json:"child,omitempty"`
	CurrentMRP      float64  `json:"current_mrp"`
	MRP             float64  `json:"mrp,omitempty"`
	SuggestedMRP    float64  `json:"suggested_mrp"`
	// SuggestedMRPRule names the MRP rule that made the suggestion; "" = the
	// markup (Stage 114.6).
	SuggestedMRPRule string `json:"suggested_mrp_rule,omitempty"`
	NeedsMRP         bool   `json:"needs_mrp"`
	BatchNo          string `json:"batch_no,omitempty"`
	Posted           bool   `json:"posted"`
	GSTWarning       string `json:"gst_warning,omitempty"`
}

// GRNStepView is a step GRN as its screens show it.
type GRNStepView struct {
	ID                 string            `json:"id"`
	Code               string            `json:"code"`
	Step               string            `json:"step"`
	Status             string            `json:"status"`
	Vendor             string            `json:"vendor"`
	VendorName         string            `json:"vendor_name"`
	Location           string            `json:"location"`
	POIDs              []string          `json:"po_ids"`
	InvoiceNo          string            `json:"vendor_invoice_no"`
	InvoiceDate        string            `json:"vendor_invoice_date"`
	InvoiceTotal       float64           `json:"vendor_invoice_amount"`
	InvoiceLinesValue  float64           `json:"invoice_lines_value"`
	InvoiceLinesGST    float64           `json:"invoice_lines_gst"`
	InvoiceLinesTotal  float64           `json:"invoice_lines_total"`
	Unexplained        float64           `json:"invoice_unexplained"`
	OurValue           float64           `json:"our_value"`
	OurGST             float64           `json:"our_gst"`
	OurTotal           float64           `json:"our_total"`
	Difference         float64           `json:"difference"` // invoice total - our total
	NoteKind           string            `json:"note_kind,omitempty"`
	NoteAmount         float64           `json:"note_amount"`
	PriceDiffTotal     float64           `json:"price_diff_total"`
	QtyDiffTotal       float64           `json:"qty_diff_total"`
	RoundOff           float64           `json:"round_off"`
	Lines              []GRNStepLineView `json:"lines"`
	Children           []GRNChild        `json:"children"`
	ExcessPending      bool              `json:"excess_pending"`
	ExcessApprovalID   string            `json:"excess_approval_id,omitempty"`
	DebitNoteID        string            `json:"debit_note_id,omitempty"`
	VendorCreditNoteID string            `json:"vendor_credit_note_id,omitempty"`
	VendorBillID       string            `json:"vendor_bill_id,omitempty"`
	VendorBillStatus   string            `json:"vendor_bill_status,omitempty"`
	NetPayable         float64           `json:"net_payable"`
	PaidAmount         float64           `json:"paid_amount"`
	PaymentStatus      string            `json:"payment_status,omitempty"`
	Payments           []GRNPayment      `json:"payments"`
	VendorTDSSection   string            `json:"vendor_tds_section,omitempty"`
	QuickPath          bool              `json:"quick_path"`
	QuickPathReasons   []string          `json:"quick_path_reasons,omitempty"`
	BlindCount         bool              `json:"blind_count"`
	Warnings           []string          `json:"warnings,omitempty"`
	InvoiceCheck       *GRNInvoiceCheck  `json:"invoice_check,omitempty"`
	Changes            []GRNLineChange   `json:"changes"`
	ReceivedBy         string            `json:"received_by,omitempty"`
	VerifiedBy         string            `json:"verified_by,omitempty"`
	PricedBy           string            `json:"priced_by,omitempty"`
	Allowed            map[string]bool   `json:"allowed,omitempty"`
}

// GRNLineChange is one row of the correction log.
type GRNLineChange struct {
	LineNo    int    `json:"line_no,omitempty"`
	SKU       string `json:"sku,omitempty"`
	Step      string `json:"step"`
	Field     string `json:"field"`
	Old       string `json:"old"`
	New       string `json:"new"`
	Reason    string `json:"reason,omitempty"`
	By        string `json:"by,omitempty"`
	ChangedAt string `json:"changed_at"`
}

// GetGRNStepView builds the read model for a step GRN.
func GetGRNStepView(tenantID, grnID string) (*GRNStepView, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	data, status, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("goods receipt %s not found", grnID)}
		}
		return nil, err
	}
	if !IsGRNStepFlow(data) {
		return nil, &ValidationError{Code: "GOODSR-0944", Message: fmt.Sprintf("goods receipt %s was posted in one step", grnID)}
	}
	lines := grnLineMaps(data)
	grnStampTotals(data, lines)
	v := &GRNStepView{
		ID: grnID, Code: firstNonEmpty(strField(data, "code"), grnID), Step: strField(data, "grn_step"), Status: status,
		Vendor: strField(data, "vendor"), Location: strField(data, "location"), POIDs: GRNPOIDs(data),
		InvoiceNo: strField(data, "vendor_invoice_no"), InvoiceDate: strField(data, "vendor_invoice_date"),
		InvoiceTotal:      numFromInterface(data["vendor_invoice_amount"]),
		InvoiceLinesValue: numFromInterface(data["invoice_lines_value"]), InvoiceLinesGST: numFromInterface(data["invoice_lines_gst"]),
		InvoiceLinesTotal: numFromInterface(data["invoice_lines_total"]), Unexplained: numFromInterface(data["invoice_unexplained"]),
		OurValue: numFromInterface(data["our_value"]), OurGST: numFromInterface(data["our_gst"]), OurTotal: numFromInterface(data["our_total"]),
		Difference: numFromInterface(data["note_difference"]), Children: grnChildren(data),
		ExcessApprovalID: strField(data, "excess_approval_id"), DebitNoteID: strField(data, "debit_note_id"),
		VendorCreditNoteID: strField(data, "vendor_credit_note_id"), VendorBillID: strField(data, "vendor_bill_id"),
		PaidAmount: numFromInterface(data["paid_amount"]), PaymentStatus: strField(data, "payment_status"),
		Payments: grnPayments(data), BlindCount: GetSettingBool(tenantID, SettingGRNBlindCount),
		ReceivedBy: strField(data, "received_by"), VerifiedBy: strField(data, "verified_by"), PricedBy: strField(data, "priced_by"),
	}
	if v.Children == nil {
		v.Children = []GRNChild{}
	}
	if vd, _, err := fetchDocData(tenantID, "Vendor", v.Vendor); err == nil {
		v.VendorName = strField(vd, "name")
		v.VendorTDSSection = strField(vd, "tds_section")
	}
	if math.Abs(v.Difference) >= 0.005 {
		v.NoteKind = "DebitNote"
		if v.Difference < 0 {
			v.NoteKind = "VendorCreditNote"
		}
		v.NoteAmount = round2(math.Abs(v.Difference))
	}
	v.RoundOff = v.Unexplained
	if v.Step == "Received" {
		v.InvoiceCheck = grnInvoiceCheck(grnLineMaps(data), v.InvoiceTotal)
	}

	// Open PO qty per PO and SKU, for the PO column.
	openQty := map[string]map[string]float64{}
	poOpen := func(po, sku string) *float64 {
		if po == "" {
			return nil
		}
		if _, ok := openQty[po]; !ok {
			o, _ := fetchPOItemQuantities(tenantID, po)
			b, _ := fetchGRNAcceptedQuantities(tenantID, po, grnID)
			m := map[string]float64{}
			for s, q := range o {
				m[s] = math.Max(0, q-b[s])
			}
			openQty[po] = m
		}
		if q, ok := openQty[po][sku]; ok {
			return &q
		}
		return nil
	}
	vendorState, storeState := "", ""
	if vd, _, err := fetchDocData(tenantID, "Vendor", v.Vendor); err == nil {
		vendorState = strings.ToLower(strField(vd, "state"))
	}
	if ld, _, err := fetchDocData(tenantID, "Location", v.Location); err == nil {
		storeState = strings.ToLower(strField(ld, "state"))
	}
	if vendorState != "" && storeState != "" {
		if vendorState == storeState {
			v.Warnings = append(v.Warnings, "Vendor and store are in the same state: the invoice should charge CGST + SGST.")
		} else {
			v.Warnings = append(v.Warnings, "Vendor and store are in different states: the invoice should charge IGST.")
		}
	}
	posted := map[string]bool{}
	for _, c := range v.Children {
		posted[c.Key] = c.Status == GRNStepPosted
	}
	markup := GetSettingFloat(tenantID, SettingGRNMRPMarkup)
	ending := GetSettingString(tenantID, SettingGRNMRPEnding)
	for _, l := range lines {
		sku := strField(l, "sku")
		item, _, _ := fetchDocData(tenantID, "Item", sku)
		lv := GRNStepLineView{
			LineNo: grnLineNo(l), SKU: sku, ItemName: strField(item, "name"),
			POID:        firstNonEmpty(strField(l, "po_id"), strField(data, "po_id")),
			ReceivedQty: grnReceivedQty(l), InvoiceQty: grnInvoiceQty(l), InvoicePrice: round2(grnInvoicePrice(l)),
			GRNQty: grnGRNQty(l), GRNPrice: round2(grnGRNPrice(l)), GSTRate: numFromInterface(l["gst_rate"]),
			GSTInclusive:   truthy(l["gst_inclusive"]),
			RejectedQty:    math.Max(0, numFromInterface(l["rejected_qty"])-numFromInterface(l["excess_returned"])),
			ExcessReturned: numFromInterface(l["excess_returned"]), RejectionReason: strField(l, "rejection_reason"),
			RejectionCode: strField(l, "rejection_reason_code"), DamageCode: strField(l, "damage_reason_code"),
			DamagedQty: numFromInterface(l["damaged_qty"]), DamageReason: strField(l, "damage_reason"),
			AcceptedQty: grnStepAccepted(l), ExcessQty: numFromInterface(l["excess_qty"]),
			ExcessAction: strField(l, "excess_action"), ExcessStatus: strField(l, "excess_status"),
			PrevGRN: strField(l, "prev_grn"), PrevVendor: strField(l, "prev_vendor"), PrevDate: strField(l, "prev_date"),
			PriceGroup: strField(l, "price_group"), Child: strField(l, "child"),
			CurrentMRP: numFromInterface(item["mrp"]), MRP: numFromInterface(l["mrp"]), BatchNo: strField(l, "batch_no"),
		}
		lv.POOpenQty = poOpen(lv.POID, sku)
		if lineNumberSet(l, "po_rate") {
			r := numFromInterface(l["po_rate"])
			lv.PORate = &r
		}
		gstF := 1 + lv.GSTRate/100
		lv.InvoiceValue = round2(lv.InvoiceQty * lv.InvoicePrice * gstF)
		lv.OurValue = round2(lv.AcceptedQty * lv.GRNPrice * gstF)
		lv.QtyDiff = round2(lv.GRNQty - lv.InvoiceQty)
		lv.PriceDiff = round2(lv.GRNPrice - lv.InvoicePrice)
		v.QtyDiffTotal += (lv.InvoiceQty - lv.AcceptedQty) * lv.InvoicePrice * gstF
		v.PriceDiffTotal += lv.AcceptedQty * (lv.InvoicePrice - lv.GRNPrice) * gstF
		if lineNumberSet(l, "prev_price") {
			p := numFromInterface(l["prev_price"])
			lv.PrevPrice = &p
		} else if v.Step == GRNStepReceived || v.Step == GRNStepVerified {
			// Shown before the price check runs, so Verify and the quick
			// path can already see it; saved at step 3.
			if prev, ok := grnPreviousPrice(db.DB, schema, sku, grnID); ok {
				p := prev.Price
				lv.PrevPrice = &p
				lv.PrevGRN, lv.PrevVendor, lv.PrevDate = prev.GRN, prev.Vendor, prev.Date
			}
		}
		if lv.PrevPrice != nil && *lv.PrevPrice > 0 {
			pct := round2((lv.GRNPrice - *lv.PrevPrice) / *lv.PrevPrice * 100)
			lv.PriceChangePct = &pct
		}
		if itemRate := numFromInterface(item["gst_rate"]); item != nil && lineNumberSet(item, "gst_rate") && math.Abs(itemRate-lv.GSTRate) >= 0.005 {
			lv.GSTWarning = fmt.Sprintf("Invoice GST %v%% but the item's HSN rate is %v%%", lv.GSTRate, itemRate)
		}
		lv.Posted = lv.Child != "" && posted[lv.Child]
		lv.NeedsMRP = grnLineNeedsMRP(tenantID, sku)
		lv.SuggestedMRP, lv.SuggestedMRPRule = suggestedMRPFor(tenantID, item, lv.GRNPrice, lv.GSTRate, grnLineWeight(l, item), markup, ending)
		if lv.ExcessStatus == "Pending" {
			v.ExcessPending = true
		}
		v.Lines = append(v.Lines, lv)
	}
	v.QtyDiffTotal = round2(v.QtyDiffTotal)
	v.PriceDiffTotal = round2(v.PriceDiffTotal)
	v.QuickPath, v.QuickPathReasons = grnQuickPathCheck(v, math.Max(GetSettingFloat(tenantID, SettingGRNSamePriceTolerance), 0.004))
	v.NetPayable = grnNetPayable(tenantID, data)
	if v.VendorBillID != "" {
		if _, st, err := fetchDocData(tenantID, "VendorInvoice", v.VendorBillID); err == nil {
			v.VendorBillStatus = st
		}
	}
	v.Changes = readGRNChanges(schema, grnID)
	return v, nil
}

// grnLineNeedsMRP: every stocked, sellable line needs an MRP before it posts;
// a fixed asset or an item that keeps no stock does not.
func grnLineNeedsMRP(tenantID, sku string) bool {
	if asset, _ := isFixedAssetSKU(tenantID, sku); asset {
		return false
	}
	if policy, _, known := ItemPolicyForSKU(tenantID, sku); known && !policy.Tracked() {
		return false
	}
	return true
}

// suggestedMRP is cost x (1 + markup) x (1 + GST), rounded up to the price
// ending. The item's category markup (Category.mrp_markup_pct) wins over the
// org-wide one.
func suggestedMRP(tenantID string, item map[string]interface{}, cost, gstRate, markup float64, ending string) float64 {
	if cost <= 0 {
		return 0
	}
	if cat := strField(item, "category"); cat != "" {
		if m, ok := categoryMRPMarkup(tenantID, cat); ok {
			markup = m
		}
	}
	return roundUpToEnding(cost*(1+markup/100)*(1+gstRate/100), ending)
}

func categoryMRPMarkup(tenantID, category string) (float64, bool) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, false
	}
	var raw sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'mrp_markup_pct' FROM %s.documents
		WHERE doctype = 'Category' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1 OR lower(data->>'name') = lower($1))
		LIMIT 1`, schema), category).Scan(&raw)
	if err != nil || !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return 0, false
	}
	v, perr := strconv.ParseFloat(strings.TrimSpace(raw.String), 64)
	return v, perr == nil && v >= 0
}

// roundUpToEnding rounds a price up to the next price ending.
func roundUpToEnding(p float64, ending string) float64 {
	if p <= 0 {
		return 0
	}
	switch ending {
	case "none":
		return WholeRupeeMRP(p) // Stage 94.15: an MRP is a whole rupee
	case "0":
		return math.Ceil(p/10-1e-9) * 10
	case "9":
		return math.Ceil((p+1)/10-1e-9)*10 - 1
	case "49":
		return math.Ceil((p+1)/50-1e-9)*50 - 1
	default: // "99"
		return math.Ceil((p+1)/100-1e-9)*100 - 1
	}
}

// grnQuickPathCheck: everything matches - received = invoice = open PO qty,
// invoice price = PO price, the total agrees with the lines, every price is
// Same and every MRP is there - so one Post all runs every step (94.10).
func grnQuickPathCheck(v *GRNStepView, samePriceTol float64) (bool, []string) {
	if v.Step != GRNStepReceived && v.Step != GRNStepVerified && v.Step != GRNStepPriced {
		return false, nil
	}
	var why []string
	if math.Abs(v.Unexplained) > 1 {
		why = append(why, "the invoice total does not agree with its lines")
	}
	for _, l := range v.Lines {
		tag := fmt.Sprintf("line %d (%s)", l.LineNo, l.SKU)
		switch {
		case l.POOpenQty == nil || l.PORate == nil:
			why = append(why, tag+" is not on the PO")
		case math.Abs(l.ReceivedQty-l.InvoiceQty) > 1e-9 || math.Abs(l.InvoiceQty-*l.POOpenQty) > 1e-9:
			why = append(why, tag+": received, invoiced and PO qty differ")
		case math.Abs(l.InvoicePrice-*l.PORate) >= 0.005:
			why = append(why, tag+": the invoice price is not the PO price")
		}
		if l.RejectedQty > 0 || l.DamagedQty > 0 || l.ExcessReturned > 0 {
			why = append(why, tag+" has rejected or damaged units")
		}
		if l.NeedsMRP && l.CurrentMRP <= 0 {
			why = append(why, tag+" has no MRP yet")
		}
		if l.PrevPrice == nil {
			why = append(why, tag+" is bought for the first time")
		} else if math.Abs(l.GRNPrice-*l.PrevPrice) > samePriceTol {
			why = append(why, tag+": the price differs from the last purchase")
		}
	}
	return len(why) == 0, why
}

func readGRNChanges(schema, grnID string) []GRNLineChange {
	out := []GRNLineChange{}
	if !grnLineTablesReady(db.DB, schema) {
		return out
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(line_no, 0), COALESCE(sku, ''), step, field, COALESCE(old_value, ''), COALESCE(new_value, ''),
		COALESCE(reason, ''), COALESCE(changed_by, ''), changed_at FROM %s.grn_line_change WHERE grn_id = $1 ORDER BY changed_at DESC, id DESC LIMIT 200`, schema), grnID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c GRNLineChange
		var at time.Time
		if rows.Scan(&c.LineNo, &c.SKU, &c.Step, &c.Field, &c.Old, &c.New, &c.Reason, &c.By, &at) == nil {
			c.ChangedAt = at.UTC().Format(time.RFC3339)
			out = append(out, c)
		}
	}
	return out
}

// --- the inbox (list first) ----------------------------------------------------

// GRNInboxRow is one GRN on the inbox tabs.
type GRNInboxRow struct {
	ID            string  `json:"id"`
	Code          string  `json:"code"`
	Vendor        string  `json:"vendor"`
	InvoiceNo     string  `json:"vendor_invoice_no"`
	InvoiceTotal  float64 `json:"vendor_invoice_amount"`
	Step          string  `json:"step"`
	Status        string  `json:"status"`
	Tab           string  `json:"tab"`
	PaymentStatus string  `json:"payment_status,omitempty"`
	ExcessPending bool    `json:"excess_pending,omitempty"`
	AgeHours      float64 `json:"age_hours"`
	CreatedAt     string  `json:"created_at"`
}

// GRNInboxTab places a GRN on a tab: To verify, To price, Ready to pay, Done.
func GRNInboxTab(data map[string]interface{}, status string) string {
	if status == "Cancelled" {
		return "done"
	}
	if !IsGRNStepFlow(data) {
		return "done"
	}
	switch strField(data, "grn_step") {
	case GRNStepReceived:
		return "verify"
	case GRNStepVerified, GRNStepPriced:
		return "price"
	case GRNStepPosted:
		if s := strField(data, "payment_status"); s == "Paid" {
			return "done"
		}
		return "pay"
	}
	return "done"
}

// ListGRNInbox lists step GRNs by tab with each tab's count and oldest age.
func ListGRNInbox(tenantID string) ([]GRNInboxRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// The age is worked out in the database: documents.created_at is the
	// session's local time, so comparing it with Go's clock would skew it.
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, COALESCE(status, ''), created_at,
		EXTRACT(EPOCH FROM (LOCALTIMESTAMP - created_at))::float8 / 3600
		FROM %s.documents
		WHERE doctype = 'GRN' AND deleted_at IS NULL AND data->>'flow' = 'steps'
		ORDER BY created_at DESC LIMIT 500`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GRNInboxRow{}
	for rows.Next() {
		var id, dataStr, status string
		var at time.Time
		var ageHours float64
		if err := rows.Scan(&id, &dataStr, &status, &at, &ageHours); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(dataStr), &data) != nil {
			continue
		}
		pending := false
		for _, l := range grnLineMaps(data) {
			if strField(l, "excess_status") == "Pending" {
				pending = true
			}
		}
		out = append(out, GRNInboxRow{
			ID: id, Code: firstNonEmpty(strField(data, "code"), id), Vendor: strField(data, "vendor"),
			InvoiceNo: strField(data, "vendor_invoice_no"), InvoiceTotal: numFromInterface(data["vendor_invoice_amount"]),
			Step: strField(data, "grn_step"), Status: status, Tab: GRNInboxTab(data, status),
			PaymentStatus: strField(data, "payment_status"), ExcessPending: pending,
			AgeHours: math.Round(math.Max(ageHours, 0)*10) / 10, CreatedAt: at.Format("2006-01-02T15:04:05"),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, rows.Err()
}

// OpenPOsForVendor is Receive's tick-list: the vendor's approved POs that
// still have qty open, with lines, open qty and value.
type OpenPOSummary struct {
	ID        string  `json:"id"`
	Code      string  `json:"code"`
	Date      string  `json:"date"`
	Lines     int     `json:"lines"`
	OpenQty   float64 `json:"open_qty"`
	OpenValue float64 `json:"open_value"`
	Location  string  `json:"location,omitempty"`
}

func OpenPOsForVendor(tenantID, vendor string) ([]OpenPOSummary, error) {
	vendor = strings.TrimSpace(vendor)
	out := []OpenPOSummary{}
	if vendor == "" {
		return out, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'po_number', data->>'code', id), created_at,
		COALESCE(data->>'target_warehouse', data->>'location', '') FROM %s.documents
		WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL AND status = 'Approved'
		  AND (data->>'vendor' = $1 OR data->>'vendor_id' = $1)
		ORDER BY created_at LIMIT 200`, schema), vendor)
	if err != nil {
		return nil, err
	}
	type poRow struct {
		id, code, loc string
		at            time.Time
	}
	var pos []poRow
	for rows.Next() {
		var p poRow
		if err := rows.Scan(&p.id, &p.code, &p.at, &p.loc); err != nil {
			rows.Close()
			return nil, err
		}
		pos = append(pos, p)
	}
	rows.Close()
	for _, p := range pos {
		ordered, err := fetchPOItemQuantities(tenantID, p.id)
		if err != nil || len(ordered) == 0 {
			continue
		}
		received, err := fetchGRNAcceptedQuantities(tenantID, p.id, "")
		if err != nil {
			return nil, err
		}
		rates, _, _ := poRatesBySKU(tenantID, p.id)
		s := OpenPOSummary{ID: p.id, Code: p.code, Date: p.at.Format("2006-01-02"), Location: p.loc}
		for sku, q := range ordered {
			open := q - received[sku]
			if open <= 1e-9 {
				continue
			}
			s.Lines++
			s.OpenQty += open
			s.OpenValue += open * rates[sku].Rate
		}
		if s.Lines == 0 {
			continue
		}
		s.OpenValue = round2(s.OpenValue)
		out = append(out, s)
	}
	return out, nil
}
