package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stage 148.19: a vendor's bill with no PO (rent, repairs, freight, services).
//
// Off by default (finance.expense_bills). On, an expense bill is a VendorInvoice
// with bill_kind "Expense", made by CreateExpenseBill and never by a plain save.
// Its lines are by expense account (or the account the vendor group's
// "expense_bill" posting rule names), each with GST (eligible or blocked) and an
// optional cost centre. It books on entry:
//
//	Dr expense account (+ blocked GST), Dr 1500 eligible GST / Cr 2100 payable
//
// one journal per cost centre. From then on it is an ordinary bill: pay, part
// pay, pay with TDS, proposals, ageing and holds work off payment_basis_paise
// (2100 only) exactly as for any bill. A bill over the VendorInvoice approval
// slab is held Pending Approval and books when a checker approves it. A PO can be
// linked later; that changes nothing in the books.

const (
	SettingExpenseBills     = "finance.expense_bills"
	PostingEventExpenseBill = "expense_bill"

	expenseBillKind = "Expense"

	expBillOffCode   = "FIN-0930" // the setting is off
	expBillInputCode = "FIN-0931" // vendor, number, date or lines not usable
	expBillLineCode  = "FIN-0932" // a line's account, amount, rate or credit choice not usable
	expBillHandCode  = "FIN-0933" // typed in or changed by a plain save
	expBillStateCode = "FIN-0934" // not in a state for that step (link, cancel)
	expBillPostCode  = "FIN-0935" // not booked yet, so it cannot be paid
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingExpenseBills, Module: "Finance",
		Label: "Bills without a PO", Type: SettingTypeBool, Default: "false",
		Description: "A vendor's bill for rent, repairs or services can be entered by expense account, with GST, cost centre and TDS, and paid like any bill.",
	})
	postingEvents = append(postingEvents, PostingEventExpenseBill)
}

type ExpenseBillLine struct {
	Description string  `json:"description"`
	Account     string  `json:"account"`
	Amount      float64 `json:"amount"` // before GST
	GSTRate     float64 `json:"gst_rate"`
	ITC         string  `json:"itc"` // Eligible or Blocked; blank takes the HSN row's, then the vendor's
	HSN         string  `json:"hsn"`
	CostCenter  string  `json:"cost_center"`
}

type ExpenseBillInput struct {
	VendorID      string            `json:"vendor_id"`
	InvoiceNumber string            `json:"invoice_number"`
	InvoiceDate   string            `json:"invoice_date"`
	Location      string            `json:"location"`
	LinkedPO      string            `json:"linked_po_id"`
	Narration     string            `json:"narration"`
	Lines         []ExpenseBillLine `json:"lines"`
}

type ExpenseBillResult struct {
	InvoiceID    string  `json:"id"`
	Status       string  `json:"status"`
	Amount       float64 `json:"amount"`
	GST          float64 `json:"gst_credit"`
	BlockedGST   float64 `json:"gst_blocked"`
	ApprovalRole string  `json:"approval_role,omitempty"`
	Replayed     bool    `json:"replayed,omitempty"`
}

func expBillRefused(code, sub, format string, args ...interface{}) error {
	return &ValidationError{Code: code, SubFor: sub, Message: fmt.Sprintf(format, args...)}
}

func isExpenseBill(data map[string]interface{}) bool {
	return strField(data, "bill_kind") == expenseBillKind
}

// expenseBillGST is all the GST on an expense bill, eligible and blocked.
func expenseBillGST(data map[string]interface{}, amount float64) float64 {
	gst := numFromInterface(data["gst_total"])
	if gst <= 0 || gst >= amount {
		return 0
	}
	return gst
}

// expenseLineRow is one line as stored: paise throughout.
type expenseLineRow struct {
	Description string  `json:"description"`
	Account     string  `json:"account"`
	Net         int64   `json:"net_paise"`
	Tax         int64   `json:"tax_paise"`
	Rate        float64 `json:"gst_rate"`
	Blocked     bool    `json:"blocked"`
	CostCenter  string  `json:"cost_center,omitempty"`
}

func (r expenseLineRow) debit() int64 {
	if r.Blocked {
		return r.Net + r.Tax
	}
	return r.Net
}

func expenseLineChoice(tenantID string, vendorDoc map[string]interface{}, l ExpenseBillLine) string {
	choice := strings.TrimSpace(l.ITC)
	if choice == "" && GetSettingBool(tenantID, SettingITC) {
		if hsn := NormalizeHSN(l.HSN); hsn != "" {
			choice, _ = itcDocData(tenantID, "HSNCode", HSNCatalogID(hsn))["itc"].(string)
		}
		if choice == "" {
			choice = strField(vendorDoc, "itc")
		}
	}
	return choice
}

// expensePostingGroups spreads the lines over one journal per cost centre.
type expenseGroup struct {
	CostCenter string
	Debits     map[string]int64
	Gross      int64
}

func expenseGroups(rows []expenseLineRow) []expenseGroup {
	by := map[string]*expenseGroup{}
	for _, r := range rows {
		g := by[r.CostCenter]
		if g == nil {
			g = &expenseGroup{CostCenter: r.CostCenter, Debits: map[string]int64{}}
			by[r.CostCenter] = g
		}
		g.Debits[r.Account] += r.debit()
		if !r.Blocked && r.Tax > 0 {
			g.Debits["1500"] += r.Tax
		}
		g.Gross += r.Net + r.Tax
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]expenseGroup, 0, len(keys))
	for _, k := range keys {
		out = append(out, *by[k])
	}
	return out
}

func expenseRowsOf(data map[string]interface{}) []expenseLineRow {
	raw, _ := json.Marshal(data["expense_lines"])
	var rows []expenseLineRow
	_ = json.Unmarshal(raw, &rows)
	return rows
}

// postExpenseBillTx books the bill: one balanced journal per cost centre.
func postExpenseBillTx(tx *sql.Tx, tenantID, schema, billID, date string, rows []expenseLineRow) error {
	for i, g := range expenseGroups(rows) {
		err := PostDoubleEntryTx(tx, tenantID, schema, "VendorInvoice", billID, g.Debits, map[string]int64{"2100": g.Gross},
			date, fmt.Sprintf("VendorInvoice:%s:EXPENSE:%d", billID, i+1), PostingOptions{CostCenter: g.CostCenter})
		if err != nil {
			return err
		}
	}
	return nil
}

func expenseAccountOK(tx queryRower, schema, code string) error {
	var typ string
	var blocked bool
	err := tx.QueryRow(fmt.Sprintf(`SELECT account_type, blocked FROM %s.gl_accounts WHERE account_code = $1`, schema), code).Scan(&typ, &blocked)
	if err == sql.ErrNoRows {
		return expBillRefused(expBillLineCode, "Account", "account %s is not in the chart of accounts", code)
	}
	if err != nil {
		return err
	}
	if typ != "Expense" {
		return expBillRefused(expBillLineCode, "Account", "account %s is not an expense account", code)
	}
	if blocked {
		return expBillRefused(expBillLineCode, "Account", "account %s is blocked", code)
	}
	return nil
}

// CreateExpenseBill enters a bill with no PO. The caller has checked the role.
func CreateExpenseBill(tenantID, userID string, in ExpenseBillInput) (*ExpenseBillResult, error) {
	if !GetSettingBool(tenantID, SettingExpenseBills) {
		return nil, expBillRefused(expBillOffCode, "", "Bills without a PO are switched off.")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	vendor, number := strings.TrimSpace(in.VendorID), strings.TrimSpace(in.InvoiceNumber)
	switch {
	case vendor == "":
		return nil, expBillRefused(expBillInputCode, "Vendor", "Choose the vendor.")
	case number == "":
		return nil, expBillRefused(expBillInputCode, "Invoice number", "Enter the vendor's bill number.")
	case len(in.Lines) == 0:
		return nil, expBillRefused(expBillInputCode, "Lines", "Add at least one line.")
	}
	billDate := strings.TrimSpace(in.InvoiceDate)
	if billDate == "" {
		billDate = BusinessToday()
	} else if _, err := time.Parse("2006-01-02", billDate); err != nil {
		return nil, expBillRefused(expBillInputCode, "Invoice date", "Use a date like 2026-11-04.")
	}
	vendorDoc, _, err := fetchDocData(tenantID, "Vendor", vendor)
	if err != nil || vendorDoc == nil {
		return nil, expBillRefused(expBillInputCode, "Vendor", "That vendor is not on file.")
	}
	if in.LinkedPO != "" {
		if err := checkExpenseBillPO(tenantID, vendor, vendorDoc, in.LinkedPO); err != nil {
			return nil, err
		}
	}

	sum := sha256.Sum256([]byte(vendor + "|" + strings.ToLower(number)))
	billID := "VI-EXP-" + hex.EncodeToString(sum[:])[:20]

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// The same bill again is the same bill.
	var other string
	err = tx.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents
		WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL AND status <> 'Cancelled' AND data->>'vendor_id' = $1
		  AND lower(trim(data->>'invoice_number')) = lower($2) LIMIT 1`, schema), vendor, number).Scan(&other)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		if other == billID {
			res := &ExpenseBillResult{InvoiceID: billID, Replayed: true}
			if d, st, e := fetchDocData(tenantID, "VendorInvoice", billID); e == nil {
				res.Status, res.Amount = st, numFromInterface(d["invoice_amount"])
			}
			return res, nil
		}
		return nil, expBillRefused(expBillInputCode, "Invoice number", "This vendor's bill %s is already entered.", number)
	}

	group := strField(vendorDoc, "vendor_group")
	rows := make([]expenseLineRow, 0, len(in.Lines))
	var gross, gstEligible, gstBlocked int64
	for i, l := range in.Lines {
		if l.Amount <= 0 {
			return nil, expBillRefused(expBillLineCode, "Amount", "Line %d needs an amount above nought.", i+1)
		}
		if l.GSTRate < 0 || l.GSTRate > 100 {
			return nil, expBillRefused(expBillLineCode, "GST rate", "Line %d: the GST rate must be 0 to 100.", i+1)
		}
		choice := expenseLineChoice(tenantID, vendorDoc, l)
		if choice != "" && choice != itcEligible && choice != itcBlocked {
			return nil, expBillRefused(expBillLineCode, "GST credit", "Line %d: the GST credit must be Eligible or Blocked.", i+1)
		}
		account := strings.TrimSpace(l.Account)
		if account == "" {
			ctx := PostingContext{VendorGroup: group, CostCenter: l.CostCenter}
			account = ResolvePostingAccounts(tenantID, PostingEventExpenseBill, ctx, "", "").Debit
		}
		if account == "" {
			return nil, expBillRefused(expBillLineCode, "Account", "Line %d needs an expense account.", i+1)
		}
		if err := expenseAccountOK(tx, schema, account); err != nil {
			return nil, err
		}
		if err := validateCostCenterReference(tenantID, strings.TrimSpace(l.CostCenter)); err != nil {
			return nil, expBillRefused(expBillLineCode, "Cost centre", "Line %d: that cost centre is not set up.", i+1)
		}
		net := RupeesToPaise(l.Amount)
		tax := int64(math.Round(float64(net) * l.GSTRate / 100))
		row := expenseLineRow{Description: strings.TrimSpace(l.Description), Account: account, Net: net, Tax: tax,
			Rate: l.GSTRate, Blocked: choice == itcBlocked, CostCenter: strings.TrimSpace(l.CostCenter)}
		rows = append(rows, row)
		gross += net + tax
		if row.Blocked {
			gstBlocked += tax
		} else {
			gstEligible += tax
		}
	}

	doc := map[string]interface{}{
		"id": billID, "code": billID, "bill_kind": expenseBillKind, "source_doctype": "ExpenseBill",
		"invoice_number": number, "vendor_id": vendor, "invoice_date": billDate,
		"invoice_amount": PaiseToRupees(gross), "gst_amount": PaiseToRupees(gstEligible),
		"gst_total": PaiseToRupees(gstEligible + gstBlocked), "itc_blocked_amount": PaiseToRupees(gstBlocked),
		"taxable_amount": PaiseToRupees(gross - gstEligible),
		"expense_lines":  rows, "narration": strings.TrimSpace(in.Narration),
		"payment_basis_paise": map[string]int64{"2100": gross}, "payment_basis_exact": true,
		"financial_year": documentFinancialYear(time.Now()),
	}
	if in.Location != "" {
		doc["location"] = in.Location
	}
	if in.LinkedPO != "" {
		doc["linked_po_id"] = in.LinkedPO
	}
	if due := VendorBillDueDate(tenantID, in.LinkedPO, vendor, billDate, billDate); due != "" {
		doc["due_date"] = due
	}
	role, err := RequiredApproverRoleForAmount(tenantID, "VendorInvoice", PaiseToRupees(gross))
	if err != nil {
		return nil, err
	}
	status := "Matched"
	if role != "" {
		status = "Pending Approval"
		doc["expense_pending"] = true
	} else {
		doc["expense_posted"] = "Yes"
	}
	doc["status"] = status
	issueNumberInto(doc, tenantID, "VendorInvoice", billID, "", in.Location)
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorInvoice', $2, $3, $4)`, schema),
		billID, raw, status, userID); err != nil {
		return nil, fmt.Errorf("the bill could not be saved: %w", err)
	}
	if role != "" {
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.approval_log (doctype, document_id, action, actor_user_id, actor_role, amount, comment)
			VALUES ('VendorInvoice', $1, 'Submitted', $2, '', $3, 'Bill without a PO')`, schema), billID, userID, PaiseToRupees(gross)); err != nil {
			return nil, err
		}
	} else if err := postExpenseBillTx(tx, tenantID, schema, billID, billDate, rows); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "EXPENSE_BILL_ENTERED", "SUCCESS", fmt.Sprintf("Bill %s of %s for %.2f (%s)", number, vendor, PaiseToRupees(gross), status))
	return &ExpenseBillResult{InvoiceID: billID, Status: status, Amount: PaiseToRupees(gross), GST: PaiseToRupees(gstEligible),
		BlockedGST: PaiseToRupees(gstBlocked), ApprovalRole: role}, nil
}

// finalizeExpenseBillApproval books a bill a checker has approved. It is
// FinalizeVendorInvoiceOverridePayment's branch for a bill waiting on its
// approval, and it moves no money: the bill is then an ordinary Matched bill.
func finalizeExpenseBillApproval(tx *sql.Tx, tenantID, schema, billID string, data map[string]interface{}) error {
	rows := expenseRowsOf(data)
	date := strField(data, "invoice_date")
	if err := postExpenseBillTx(tx, tenantID, schema, billID, date, rows); err != nil {
		return err
	}
	delete(data, "expense_pending")
	data["expense_posted"] = "Yes"
	data["status"] = "Matched"
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Matched', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $2`, schema), raw, billID)
	return err
}

// expenseBillPayBlock refuses to pay an expense bill the engine has not booked:
// it must say so and have its own journal behind it, so a bill brought in by a
// CSV import with the flag typed cannot clear a payable nobody raised.
func expenseBillPayBlock(tenantID string, bill map[string]interface{}) error {
	if !isExpenseBill(bill) {
		return nil
	}
	refuse := &ValidationError{Code: expBillPostCode, Message: "This bill is not booked yet."}
	id := strField(bill, "id")
	if strField(bill, "expense_posted") != "Yes" || id == "" {
		return refuse
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.gl_postings WHERE document_type = 'VendorInvoice' AND document_id = $1 AND idempotency_key LIKE $2`, schema),
		id, "VendorInvoice:"+id+":EXPENSE:%").Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return refuse
	}
	return nil
}

func checkExpenseBillPO(tenantID, vendorID string, vendorDoc map[string]interface{}, poID string) error {
	po, _, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err != nil || po == nil {
		return expBillRefused(expBillStateCode, "PO", "That PO is not on file.")
	}
	pv := firstNonEmpty(strField(po, "vendor_id"), strField(po, "vendor"))
	if pv != vendorID && pv != strField(vendorDoc, "code") {
		return expBillRefused(expBillStateCode, "PO", "That PO is for another vendor.")
	}
	return nil
}

// LinkExpenseBillToPO ties a bill with no PO to its PO afterwards. The books
// do not change.
func LinkExpenseBillToPO(tenantID, userID, billID, poID string) error {
	if !GetSettingBool(tenantID, SettingExpenseBills) {
		return expBillRefused(expBillOffCode, "", "Bills without a PO are switched off.")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	bill, status, err := fetchDocData(tenantID, "VendorInvoice", billID)
	if err != nil || bill == nil || !isExpenseBill(bill) {
		return expBillRefused(expBillStateCode, "Bill", "That is not a bill without a PO.")
	}
	if status == "Cancelled" {
		return expBillRefused(expBillStateCode, "Bill", "A cancelled bill cannot be linked.")
	}
	vendor := strField(bill, "vendor_id")
	vendorDoc, _, _ := fetchDocData(tenantID, "Vendor", vendor)
	if err := checkExpenseBillPO(tenantID, vendor, vendorDoc, poID); err != nil {
		return err
	}
	if err := mergeDocData(schema, "VendorInvoice", billID, map[string]interface{}{"linked_po_id": poID}); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "EXPENSE_BILL_LINKED", "SUCCESS", fmt.Sprintf("Bill %s linked to a PO", strField(bill, "invoice_number")))
	return nil
}

// reverseExpenseBill takes back an expense bill's own journals when it is
// cancelled or deleted. handled is true for an expense bill, whether or not it
// had booked anything.
func reverseExpenseBill(tenantID, billID string) (handled bool, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var raw, status string
	err = tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), billID).Scan(&raw, &status)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var data map[string]interface{}
	if json.Unmarshal([]byte(raw), &data) != nil || !isExpenseBill(data) {
		return false, nil
	}
	if vendorInvoiceHasPayments(data) || status == "Paid" || status == "Approved" {
		return true, expBillRefused(expBillStateCode, "Bill", "This bill has a payment or a decision on it.")
	}
	if strField(data, "expense_posted") != "Yes" {
		return true, nil
	}
	rows, err := tx.Query(fmt.Sprintf(`SELECT idempotency_key, account_code, debit, credit, COALESCE(cost_center, '')
		FROM %s.gl_postings WHERE document_type = 'VendorInvoice' AND document_id = $1 AND idempotency_key LIKE $2 ORDER BY idempotency_key, account_code`, schema),
		billID, "VendorInvoice:"+billID+":EXPENSE:%")
	if err != nil {
		return true, err
	}
	type leg struct {
		debits, credits map[string]int64
		cc              string
	}
	byKey := map[string]*leg{}
	var keys []string
	for rows.Next() {
		var key, account, cc string
		var debit, credit int64
		if err := rows.Scan(&key, &account, &debit, &credit, &cc); err != nil {
			rows.Close()
			return true, err
		}
		if strings.HasSuffix(key, ":CANCEL") {
			continue
		}
		l := byKey[key]
		if l == nil {
			l = &leg{debits: map[string]int64{}, credits: map[string]int64{}, cc: cc}
			byKey[key] = l
			keys = append(keys, key)
		}
		// The reverse swaps the sides.
		if debit > 0 {
			l.credits[account] += debit
		}
		if credit > 0 {
			l.debits[account] += credit
		}
	}
	rows.Close()
	for _, key := range keys {
		l := byKey[key]
		if err := PostDoubleEntryTx(tx, tenantID, schema, "VendorInvoice", billID, l.debits, l.credits, BusinessToday(), key+":CANCEL", PostingOptions{CostCenter: l.cc}); err != nil {
			return true, err
		}
	}
	data["expense_posted"] = "Reversed"
	out, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $2`, schema), out, billID); err != nil {
		return true, err
	}
	return true, tx.Commit()
}

// KeepExpenseBillMarks is a plain save's half of "an expense bill is the
// engine's": a new bill cannot be typed in as one, and an edit cannot change
// what the engine booked.
func KeepExpenseBillMarks(tenantID, billID string, payload map[string]interface{}) error {
	forged := payload["bill_kind"] == expenseBillKind || payload["expense_lines"] != nil || payload["source_doctype"] == "ExpenseBill"
	if billID == "" {
		if forged {
			return expBillRefused(expBillHandCode, "", "Bills without a PO are entered on their own page.")
		}
		return nil
	}
	prior, pstatus, err := fetchDocData(tenantID, "VendorInvoice", billID)
	if err != nil || prior == nil || !isExpenseBill(prior) {
		if forged {
			return expBillRefused(expBillHandCode, "", "Bills without a PO are entered on their own page.")
		}
		return nil
	}
	if strField(payload, "status") == "Cancelled" && (vendorInvoiceHasPayments(prior) || pstatus == "Paid" || pstatus == "Approved") {
		return expBillRefused(expBillStateCode, "Bill", "This bill has a payment or a decision on it.")
	}
	for _, key := range []string{"bill_kind", "source_doctype", "expense_lines", "expense_pending", "expense_posted", "linked_po_id",
		"vendor_id", "invoice_number", "invoice_date", "invoice_amount", "taxable_amount", "gst_amount", "gst_total", "itc_blocked_amount",
		"payment_basis_paise", "payment_basis_exact", "id", "code"} {
		delete(payload, key)
		if v, ok := prior[key]; ok {
			payload[key] = v
		}
	}
	return nil
}

// ExpenseBillRow is one line of the list.
type ExpenseBillRow struct {
	ID            string  `json:"id"`
	InvoiceNumber string  `json:"invoice_number"`
	Vendor        string  `json:"vendor"`
	InvoiceDate   string  `json:"invoice_date"`
	DueDate       string  `json:"due_date"`
	Amount        float64 `json:"amount"`
	GSTCredit     float64 `json:"gst_credit"`
	GSTBlocked    float64 `json:"gst_blocked"`
	Status        string  `json:"status"`
	Outstanding   float64 `json:"outstanding"`
	PO            string  `json:"po"`
}

func ListExpenseBills(tenantID string) ([]ExpenseBillRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT d.id, COALESCE(d.data->>'invoice_number',''), COALESCE(v.data->>'name', d.data->>'vendor_id', ''),
		       COALESCE(d.data->>'invoice_date',''), COALESCE(d.data->>'due_date',''),
		       COALESCE(NULLIF(d.data->>'invoice_amount','')::numeric,0)::float8,
		       COALESCE(NULLIF(d.data->>'gst_amount','')::numeric,0)::float8,
		       COALESCE(NULLIF(d.data->>'itc_blocked_amount','')::numeric,0)::float8,
		       d.status, COALESCE(NULLIF(d.data->>'outstanding_amount','')::numeric, -1)::float8, COALESCE(d.data->>'linked_po_id','')
		  FROM %s.documents d
		  LEFT JOIN %s.documents v ON v.doctype = 'Vendor' AND v.id = d.data->>'vendor_id' AND v.deleted_at IS NULL
		 WHERE d.doctype = 'VendorInvoice' AND d.deleted_at IS NULL AND d.data->>'bill_kind' = 'Expense'
		 ORDER BY d.created_at DESC LIMIT 500`, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExpenseBillRow{}
	for rows.Next() {
		var r ExpenseBillRow
		if err := rows.Scan(&r.ID, &r.InvoiceNumber, &r.Vendor, &r.InvoiceDate, &r.DueDate, &r.Amount, &r.GSTCredit, &r.GSTBlocked, &r.Status, &r.Outstanding, &r.PO); err != nil {
			return nil, err
		}
		if r.Outstanding < 0 {
			r.Outstanding = r.Amount
			if r.Status == "Paid" || r.Status == "Cancelled" || r.Status == "Pending Approval" {
				r.Outstanding = 0
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ExpenseAccountChoice is one expense account a line can be booked to.
type ExpenseAccountChoice struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func ListExpenseAccounts(tenantID string) ([]ExpenseAccountChoice, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT account_code, account_name FROM %s.gl_accounts
		WHERE account_type = 'Expense' AND NOT blocked ORDER BY account_code`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExpenseAccountChoice{}
	for rows.Next() {
		var c ExpenseAccountChoice
		if err := rows.Scan(&c.Code, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
