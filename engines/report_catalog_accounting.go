package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Catalog reports - Accounting (Stage 75.3.6, Wave B).
//
// Everything posted is in gl_postings (paise; account, document, cost
// center, department, entity); gl_accounts names the accounts and their
// type. A vendor payment is the vendor invoice's ":PAY" posting out of 1100,
// a customer receipt the sales invoice's ":SETTLE" posting into 1100. The
// payables and receivables are the GRN suspense (2100) and receivables
// (1300) accounts, whose documents name the vendor or customer.

// glRows: one row per GL line in the window (From / To) for the accounts the
// condition picks, oldest first, with a running balance per account.
func glRows(accountCond string) func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	return func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
		q := fmt.Sprintf(`SELECT p.posting_id::text, p.account_code, COALESCE(a.account_name, ''), COALESCE(a.account_type, ''), p.debit, p.credit,
			COALESCE(p.document_type, ''), COALESCE(p.document_id, ''), COALESCE(p.cost_center, ''), COALESCE(p.department, ''), COALESCE(p.entity, ''),
			COALESCE(p.idempotency_key, ''), COALESCE(p.posted_by, ''), p.created_at
			FROM %[1]s.gl_postings p LEFT JOIN %[1]s.gl_accounts a ON a.account_code = p.account_code WHERE %[2]s`, schema, accountCond)
		var args []interface{}
		if v := p["from"]; v != "" {
			args = append(args, v)
			q += fmt.Sprintf(" AND p.created_at >= public.erp_day_start($%d::date)", len(args))
		}
		if v := p["to"]; v != "" {
			args = append(args, v)
			q += fmt.Sprintf(" AND p.created_at < public.erp_day_start($%d::date + 1)", len(args))
		}
		rows, err := dbQuery(q+" ORDER BY p.account_code, p.created_at, p.posting_id LIMIT 50000", args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []*catRow
		balance := map[string]float64{}
		for rows.Next() {
			var id, acct, name, typ, docType, docID, cc, dept, entity, key, by string
			var debit, credit int64
			var at time.Time
			if rows.Scan(&id, &acct, &name, &typ, &debit, &credit, &docType, &docID, &cc, &dept, &entity, &key, &by, &at) != nil {
				continue
			}
			d, c := PaiseToRupees(debit), PaiseToRupees(credit)
			balance[acct] += d - c
			out = append(out, &catRow{ID: id, Created: at, CreatedBy: by, H: map[string]interface{}{"account": acct, "account_name": name,
				"account_type": typ, "debit": d, "credit": c, "doctype": docType, "doc_id": docID, "cost_center": cc, "department": dept,
				"entity": entity, "key": key, "balance": balance[acct]}})
		}
		return out, rows.Err()
	}
}

func glAccountLabel(r *catRow) interface{} {
	return strings.TrimSpace(r.hs("account") + " " + r.hs("account_name"))
}

// glDocStore is the store of the document behind a GL line.
func glDocStore(r *catRow) string {
	d := r.master(r.hs("doctype"), r.hs("doc_id"))
	return firstNonEmpty(catStr(d["location"]), catStr(d["location_code"]), catStr(d["return_location"]))
}

// glParty: the vendor or customer of the document behind a GL line.
func glParty(r *catRow) (vendor, customer string) {
	d := r.master(r.hs("doctype"), r.hs("doc_id"))
	if d == nil {
		return "", ""
	}
	switch r.hs("doctype") {
	case "GRN", "VendorInvoice", "DebitNote", "VendorCreditNote", "PurchaseReturn", "LandedCostVoucher", "Asset":
		return firstNonEmpty(catStr(d["vendor_id"]), catStr(d["vendor"])), ""
	case "SalesInvoice", "CreditNote", "POSCart", "ReturnRequest", "RefundRequest":
		return "", firstNonEmpty(catStr(d["customer_id"]), catStr(d["customer"]))
	}
	return "", ""
}

var glCols = map[string]catFn{
	"Posting Date": func(r *catRow) interface{} { return catDay(r.Created) },
	"Voucher No": func(r *catRow) interface{} {
		return firstNonEmpty(catDocNumber(r, r.hs("doctype"), r.hs("doc_id")), r.ID)
	},
	"Document Type": catH("doctype"),
	"Document No":   func(r *catRow) interface{} { return catDocNumber(r, r.hs("doctype"), r.hs("doc_id")) },
	"GL Account":    glAccountLabel,
	"Cost Center":   func(r *catRow) interface{} { return r.nameOf("CostCenter", r.hs("cost_center")) },
	"Department":    func(r *catRow) interface{} { return r.nameOf("Department", r.hs("department")) },
	"Legal Entity":  func(r *catRow) interface{} { return r.nameOf("LegalEntity", r.hs("entity")) },
	"Debit Amount":  catHN("debit"),
	"Credit Amount": catHN("credit"),
	"Balance":       catHN("balance"),
	"Net Amount":    func(r *catRow) interface{} { return r.hn("credit") - r.hn("debit") },
	"Remarks": func(r *catRow) interface{} {
		d := r.master(r.hs("doctype"), r.hs("doc_id"))
		return firstNonEmpty(catStr(d["narration"]), catStr(d["reason"]), r.hs("key"))
	},
	"User Name": func(r *catRow) interface{} { return r.CreatedBy },
}

func glSource(cond string) *catSource {
	return &catSource{Label: "GL line", Rows: glRows(cond), StoreOf: glDocStore, Cols: glCols}
}

// trialRows: per account, the balance before the period, the period's
// debits and credits and the closing balance.
func trialRows(typeCond string) func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	return func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
		from, to := strings.TrimSpace(p["from"]), strings.TrimSpace(p["to"])
		if from == "" {
			from = "1900-01-01"
		}
		if to == "" {
			to = BusinessToday()
		}
		rows, err := dbQuery(fmt.Sprintf(`SELECT a.account_code, a.account_name, a.account_type,
			COALESCE(SUM(p.debit - p.credit) FILTER (WHERE p.created_at < public.erp_day_start($1::date)), 0),
			COALESCE(SUM(p.debit) FILTER (WHERE p.created_at >= public.erp_day_start($1::date) AND p.created_at < public.erp_day_start($2::date + 1)), 0),
			COALESCE(SUM(p.credit) FILTER (WHERE p.created_at >= public.erp_day_start($1::date) AND p.created_at < public.erp_day_start($2::date + 1)), 0)
			FROM %[1]s.gl_accounts a LEFT JOIN %[1]s.gl_postings p ON p.account_code = a.account_code
			WHERE %[2]s GROUP BY 1, 2, 3 ORDER BY 1`, schema, typeCond), from, to)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []*catRow
		for rows.Next() {
			var acct, name, typ string
			var open, debit, credit int64
			if rows.Scan(&acct, &name, &typ, &open, &debit, &credit) != nil {
				continue
			}
			if open == 0 && debit == 0 && credit == 0 {
				continue
			}
			o, d, c := PaiseToRupees(open), PaiseToRupees(debit), PaiseToRupees(credit)
			out = append(out, &catRow{ID: acct, H: map[string]interface{}{"account": acct, "account_name": name, "account_type": typ,
				"opening": o, "debit": d, "credit": c, "balance": o + d - c}})
		}
		return out, rows.Err()
	}
}

var catTrial = &catSource{Label: "GL account", Rows: trialRows("TRUE"), StoreField: "-", Cols: mergeCols(glCols, map[string]catFn{
	"Opening Stock Qty": catHN("opening"), // the catalog's heading for the opening balance
	"From Date":         func(r *catRow) interface{} { return r.p["from"] },
	"To Date":           func(r *catRow) interface{} { return firstNonEmpty(r.p["to"], BusinessToday()) },
})}

var catBalanceSheet = &catSource{Label: "GL account", Rows: func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	asOf := map[string]string{"from": "1900-01-01", "to": firstNonEmpty(p["to"], BusinessToday())}
	rows, err := trialRows("a.account_type IN ('Asset', 'Liability', 'Equity')")(tenantID, schema, asOf)
	for _, r := range rows {
		if catStr(r.H["account_type"]) != "Asset" {
			r.H["balance"] = -catNum(r.H["balance"]) // liabilities and equity read as credit balances
		}
	}
	return rows, err
}, StoreField: "-", Cols: glCols}

// --- payables and receivables -------------------------------------------------

func agingBucket(due string) string {
	t, err := time.ParseInLocation("2006-01-02", catDay(due), time.Local)
	if err != nil {
		return "No due date"
	}
	days := time.Since(t).Hours() / 24
	switch {
	case days <= 0:
		return "Not due"
	case days <= 30:
		return "1-30 days"
	case days <= 60:
		return "31-60 days"
	case days <= 90:
		return "61-90 days"
	}
	return "Over 90 days"
}

func invoiceOutstanding(r *catRow, total string) float64 {
	if _, has := r.H["outstanding_amount"]; has {
		return r.hn("outstanding_amount")
	}
	if strings.EqualFold(r.Status, "Paid") {
		return 0
	}
	return r.hn(total) - r.hn("paid_amount")
}

var vendorInvoiceCols = map[string]catFn{
	"Posting Date":      func(r *catRow) interface{} { return r.date() },
	"Vendor Invoice No": catH("invoice_number"),
	"Vendor Code":       func(r *catRow) interface{} { return firstNonEmpty(r.hs("vendor_id"), r.hs("vendor")) },
	"Vendor Name": func(r *catRow) interface{} {
		return r.nameOf("Vendor", firstNonEmpty(r.hs("vendor_id"), r.hs("vendor")))
	},
	"Vendor GSTIN": func(r *catRow) interface{} {
		return r.master("Vendor", firstNonEmpty(r.hs("vendor_id"), r.hs("vendor")))["gstin"]
	},
	"Taxable Amount":     catHN("taxable_amount"),
	"GST Amount":         catHN("gst_amount"),
	"Net Amount":         catHN("invoice_amount"),
	"Due Date":           catHD("due_date"),
	"Outstanding Amount": func(r *catRow) interface{} { return invoiceOutstanding(r, "invoice_amount") },
	"Aging Bucket":       func(r *catRow) interface{} { return agingBucket(r.hs("due_date")) },
	"Payment Status":     func(r *catRow) interface{} { return r.Status },
}

func vendorInvoiceStore(r *catRow) string {
	if g := r.master("GRN", r.hs("grn_id")); g != nil {
		return catStr(g["location"])
	}
	return catStr(r.master("PurchaseOrder", r.hs("po_id"))["location"])
}

var catVendorInvoices = &catSource{Label: "Vendor Invoice", Doctype: "VendorInvoice", StoreOf: vendorInvoiceStore,
	Statuses: []string{"Draft", "Pending Approval", "Approved", "Partially Paid", "Paid", "Cancelled"}, Cols: vendorInvoiceCols}

var salesInvoiceAcctCols = map[string]catFn{
	"Posting Date":     func(r *catRow) interface{} { return r.date() },
	"Sales Invoice No": func(r *catRow) interface{} { return r.number() },
	"Customer ID":      func(r *catRow) interface{} { return firstNonEmpty(r.hs("customer_id"), r.hs("customer")) },
	"Customer Name": func(r *catRow) interface{} {
		return r.nameOf("Customer", firstNonEmpty(r.hs("customer_id"), r.hs("customer")))
	},
	"Customer GSTIN":     func(r *catRow) interface{} { return r.master("Customer", r.hs("customer_id"))["gstin"] },
	"Due Date":           catHD("due_date"),
	"Outstanding Amount": func(r *catRow) interface{} { return invoiceOutstanding(r, "total_amount") },
	"Aging Bucket":       func(r *catRow) interface{} { return agingBucket(r.hs("due_date")) },
	"Status":             func(r *catRow) interface{} { return r.Status },
}

var catSalesInvoiceAcct = &catSource{Label: "Sales Invoice", Doctype: "SalesInvoice", Where: "status NOT IN ('Draft', 'Cancelled')",
	Number: "invoice_number", Amount: "total_amount", Taxable: "taxable_amount", Tax: "total_tax",
	Statuses: []string{"Approved", "Posted", "Partially Paid", "Paid", "Overdue"}, Cols: salesInvoiceAcctCols}

// The party ledgers: GRN suspense (2100) and receivables (1300) lines.
func partyLedgerRows(account string) func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	return glRows("p.account_code = '" + account + "'")
}

var partyLedgerCols = mergeCols(glCols, map[string]catFn{
	"Vendor Code":   func(r *catRow) interface{} { v, _ := glParty(r); return v },
	"Vendor Name":   func(r *catRow) interface{} { v, _ := glParty(r); return r.nameOf("Vendor", v) },
	"Customer ID":   func(r *catRow) interface{} { _, c := glParty(r); return c },
	"Customer Name": func(r *catRow) interface{} { _, c := glParty(r); return r.nameOf("Customer", c) },
})

var catVendorLedger = &catSource{Label: "Vendor ledger", Rows: partyLedgerRows("2100"), StoreOf: glDocStore, Cols: partyLedgerCols}
var catCustomerLedger = &catSource{Label: "Customer ledger", Rows: partyLedgerRows("1300"), StoreOf: glDocStore, Cols: partyLedgerCols}

// partyRunningBalance re-runs each party's balance in date order.
func partyRunningBalance(key string) func(rows []map[string]interface{}) {
	return func(rows []map[string]interface{}) {
		sort.SliceStable(rows, func(i, j int) bool {
			if a, b := catStr(rows[i][key]), catStr(rows[j][key]); a != b {
				return a < b
			}
			return catStr(rows[i]["Posting Date"]) < catStr(rows[j]["Posting Date"])
		})
		bal := map[string]float64{}
		for _, row := range rows {
			k := catStr(row[key])
			bal[k] += catNum(row["Debit Amount"]) - catNum(row["Credit Amount"])
			row["Balance"] = math.Round(bal[k]*100) / 100
		}
	}
}

// --- payments, receipts, proposals --------------------------------------------

var catPayments = &catSource{Label: "Vendor payment", Rows: glRows("p.document_type = 'VendorInvoice' AND p.account_code = '1100' AND p.credit > 0 AND p.idempotency_key LIKE '%:PAY%'"),
	StoreOf: func(r *catRow) string {
		inv := r.master("VendorInvoice", r.hs("doc_id"))
		if g := r.master("GRN", catStr(inv["grn_id"])); g != nil {
			return catStr(g["location"])
		}
		return ""
	},
	Cols: mergeCols(partyLedgerCols, map[string]catFn{
		"Payment Date": func(r *catRow) interface{} { return catDay(r.Created) },
		"Voucher No":   func(r *catRow) interface{} { return r.hs("key") },
		"Bank Account": glAccountLabel,
		"Net Amount":   catHN("credit"),
		"Reference No": func(r *catRow) interface{} {
			if i := strings.Index(r.hs("key"), ":PAY:"); i >= 0 {
				return r.hs("key")[i+5:]
			}
			return paymentUTR(r)
		},
		"Payment Status": func(r *catRow) interface{} { return r.master("VendorInvoice", r.hs("doc_id"))["_status"] },
	})}

// paymentUTR is the bank reference recorded for the invoice (payment_utr_log).
func paymentUTR(r *catRow) string {
	m, ok := r.lk.extra["utr"].(map[string]string)
	if !ok {
		m = map[string]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT invoice_id, utr FROM %s.payment_utr_log ORDER BY recorded_at`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var inv, utr string
				if rows.Scan(&inv, &utr) == nil {
					m[inv] = utr
				}
			}
			rows.Close()
		}
		r.lk.extra["utr"] = m
	}
	return m[r.hs("doc_id")]
}

var catReceipts = &catSource{Label: "Customer receipt", Rows: glRows("p.document_type = 'SalesInvoice' AND p.account_code = '1100' AND p.debit > 0 AND p.idempotency_key LIKE '%:SETTLE%'"),
	StoreOf: glDocStore,
	Cols: mergeCols(partyLedgerCols, map[string]catFn{
		"Payment Date": func(r *catRow) interface{} { return catDay(r.Created) },
		"Voucher No":   func(r *catRow) interface{} { return r.hs("key") },
		"Bank Account": glAccountLabel,
		"Net Amount":   catHN("debit"),
		"Reference No": func(r *catRow) interface{} {
			if i := strings.Index(r.hs("key"), ":SETTLE:"); i >= 0 {
				return r.hs("key")[i+8:]
			}
			return nil
		},
		"Status": func(r *catRow) interface{} { return r.master("SalesInvoice", r.hs("doc_id"))["_status"] },
	})}

func proposalInvoices(r *catRow) []string { return catLineListStrings(r.hs("invoice_ids")) }

var catProposals = &catSource{Label: "Payment proposal", Doctype: "PaymentProposal", Number: "proposal_number", StoreField: "-",
	Statuses: []string{"Draft", "Pending Approval", "Approved", "Paid", "Rejected"},
	Cols: map[string]catFn{
		"Payment Proposal No": func(r *catRow) interface{} { return r.number() },
		"Posting Date":        func(r *catRow) interface{} { return r.date() },
		"Vendor Code": func(r *catRow) interface{} {
			var out []string
			for _, id := range proposalInvoices(r) {
				inv := r.master("VendorInvoice", id)
				if v := firstNonEmpty(catStr(inv["vendor_id"]), catStr(inv["vendor"])); v != "" && !containsString(out, v) {
					out = append(out, v)
				}
			}
			return strings.Join(out, ", ")
		},
		"Vendor Name": func(r *catRow) interface{} {
			var out []string
			for _, id := range proposalInvoices(r) {
				inv := r.master("VendorInvoice", id)
				if v := r.nameOf("Vendor", firstNonEmpty(catStr(inv["vendor_id"]), catStr(inv["vendor"]))); v != "" && !containsString(out, v) {
					out = append(out, v)
				}
			}
			return strings.Join(out, ", ")
		},
		"Proposed Payment Amount": catHN("total_amount"),
		"Approval Date":           func(r *catRow) interface{} { return r.approval()[1] },
	}}

// financePendingRows: finance documents waiting for an approval.
var financeApprovalDoctypes = []string{"JournalVoucher", "VendorInvoice", "PaymentProposal", "ExpenseClaim", "IntercompanyTransaction",
	"BackdatedPostingRequest", "PeriodReopenRequest", "IRNCancellation", "CreditNote", "DebitNote", "VendorCreditNote", "LandedCostVoucher", "Asset"}

func financePendingRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	rows, err := dbQuery(fmt.Sprintf(`SELECT id, doctype, data, created_at, COALESCE(created_by, '') FROM %s.documents
		WHERE doctype = ANY($1) AND status = 'Pending Approval' AND deleted_at IS NULL ORDER BY created_at LIMIT 20000`, schema),
		pqStringArray(financeApprovalDoctypes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := approvalRoles(schema)
	var out []*catRow
	for rows.Next() {
		var id, doctype, by string
		var raw []byte
		var at time.Time
		if rows.Scan(&id, &doctype, &raw, &at, &by) != nil {
			continue
		}
		h := map[string]interface{}{}
		if l := catLineList("[" + string(raw) + "]"); len(l) == 1 {
			h = l[0]
		}
		h["_doctype"], h["_pending_with"] = doctype, roles[doctype]
		out = append(out, &catRow{ID: id, Status: "Pending Approval", Created: at, CreatedBy: by, H: h})
	}
	return out, rows.Err()
}

// approvalRoles: who approves each document type (approval_rules).
func approvalRoles(schema string) map[string]string {
	out := map[string]string{}
	rows, err := dbQuery(fmt.Sprintf(`SELECT doctype, string_agg(DISTINCT required_role, ' / ') FROM %s.approval_rules GROUP BY 1`, schema))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var d, r string
		if rows.Scan(&d, &r) == nil {
			out[d] = r
		}
	}
	return out
}

var catFinancePending = &catSource{Label: "Finance approval", Rows: financePendingRows, StoreOf: func(r *catRow) string { return r.hs("location") },
	Cols: map[string]catFn{
		"Document Type": catH("_doctype"),
		"Document No": func(r *catRow) interface{} {
			for _, f := range []string{"voucher_number", "invoice_number", "proposal_number", "note_number", "code"} {
				if v := r.hs(f); v != "" {
					return v
				}
			}
			return r.ID
		},
		"Document Date": func(r *catRow) interface{} { return catDay(r.Created) },
		"Vendor Name": func(r *catRow) interface{} {
			if v := firstNonEmpty(r.hs("vendor_id"), r.hs("vendor")); v != "" {
				return r.nameOf("Vendor", v)
			}
			return nil
		},
		"Amount": func(r *catRow) interface{} {
			return firstPositive(r.hn("total_amount"), r.hn("invoice_amount"), r.hn("amount"), r.hn("cost"))
		},
		"Approval Level": catConst("1"),
		"Pending With":   catH("_pending_with"),
		"Ageing Days":    func(r *catRow) interface{} { return math.Floor(r.since(r.Created).Hours() / 24) },
	}}

// --- vouchers, notes, bank, TDS, assets ---------------------------------------

var catJVLines = &catSource{Label: "Journal Voucher", Doctype: "JournalVoucher", Lines: "lines", Number: "voucher_number", StoreField: "-", DateField: "voucher_date",
	Statuses: []string{"Draft", "Pending Approval", "Approved", "Posted", "Reversed", "Rejected"},
	Cols: map[string]catFn{
		"Posting Date": func(r *catRow) interface{} { return r.date() },
		"Voucher No":   func(r *catRow) interface{} { return r.number() },
		"GL Account":   func(r *catRow) interface{} { return r.ls("account_code") + " " + jvAccountName(r) },
		"Cost Center": func(r *catRow) interface{} {
			return r.nameOf("CostCenter", firstNonEmpty(r.ls("cost_center"), r.hs("cost_center")))
		},
		"Debit Amount":  catLN("debit"),
		"Credit Amount": catLN("credit"),
		"Remarks":       func(r *catRow) interface{} { return firstNonEmpty(r.ls("narration"), r.hs("narration")) },
	}}

func jvAccountName(r *catRow) string {
	m, ok := r.lk.extra["accounts"].(map[string]string)
	if !ok {
		m = map[string]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT account_code, account_name FROM %s.gl_accounts`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var c, n string
				if rows.Scan(&c, &n) == nil {
					m[c] = n
				}
			}
			rows.Close()
		}
		r.lk.extra["accounts"] = m
	}
	return m[r.ls("account_code")]
}

var catNotesAll = &catSource{Label: "Note", Rows: func(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	var out []*catRow
	for _, s := range []*catSource{catDebitNotes, catVendorCreditNotes, catCreditNotes} {
		rs, err := s.docRows(schema, p, false)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			r.H["_type"] = s.Label
			out = append(out, r)
		}
	}
	return out, nil
}, StoreOf: func(r *catRow) string {
	if r.hs("_type") == "Credit Note" {
		return creditNoteStore(r)
	}
	return noteStore(r)
},
	Cols: map[string]catFn{
		"Document Type":  catH("_type"),
		"Document No":    func(r *catRow) interface{} { return firstNonEmpty(r.hs("note_number"), r.ID) },
		"Vendor Code":    catH("vendor_id"),
		"Customer ID":    catH("customer_id"),
		"Taxable Amount": catHN("amount"),
		"GST Amount":     catHN("gst_amount"),
		"Net Amount": func(r *catRow) interface{} {
			return firstPositive(r.hn("total_with_gst"), r.hn("amount")+r.hn("gst_amount"))
		},
	}}

var catBankLines = &catSource{Label: "Bank statement line", Doctype: "BankStatementLine", StoreField: "-", DateField: "txn_date",
	Cols: map[string]catFn{
		"Posting Date": func(r *catRow) interface{} { return r.date() },
		"Bank Account": func(r *catRow) interface{} {
			b := r.master("BankAccount", r.hs("bank_account"))
			return strings.TrimSpace(firstNonEmpty(catStr(b["bank_name"]), r.hs("bank_account")) + " " + catStr(b["account_number"]))
		},
		"Bank Statement Ref": catH("reference_number"),
		"Voucher No":         func(r *catRow) interface{} { return bankMatchedVoucher(r) },
		"Reference No":       catH("reference_number"),
		"Debit Amount": func(r *catRow) interface{} {
			if strings.EqualFold(r.hs("dr_cr"), "Dr") || strings.EqualFold(r.hs("dr_cr"), "Debit") {
				return r.hn("amount")
			}
			return 0.0
		},
		"Credit Amount": func(r *catRow) interface{} {
			if strings.EqualFold(r.hs("dr_cr"), "Dr") || strings.EqualFold(r.hs("dr_cr"), "Debit") {
				return 0.0
			}
			return r.hn("amount")
		},
		"Reconciliation Status": func(r *catRow) interface{} { return firstNonEmpty(r.hs("match_status"), r.Status) },
		"Remarks": func(r *catRow) interface{} {
			return firstNonEmpty(r.hs("exception_reason"), r.hs("resolution_note"), r.hs("description"))
		},
	}}

// bankMatchedVoucher is the GL document a statement line was matched to.
func bankMatchedVoucher(r *catRow) string {
	m, ok := r.lk.extra["bankmatch"].(map[string]string)
	if !ok {
		m = map[string]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT matched_statement_line_id, document_type || ' ' || document_id FROM %s.gl_postings
			WHERE COALESCE(matched_statement_line_id, '') <> ''`, r.lk.schema))
		if err == nil {
			for rows.Next() {
				var line, doc string
				if rows.Scan(&line, &doc) == nil {
					m[line] = doc
				}
			}
			rows.Close()
		}
		r.lk.extra["bankmatch"] = m
	}
	return m[r.ID]
}

var catTDS = &catSource{Label: "TDS", Rows: glRows("p.account_code = '2300'"), StoreOf: glDocStore,
	Cols: mergeCols(partyLedgerCols, map[string]catFn{
		"Taxable Amount": func(r *catRow) interface{} {
			d := r.master(r.hs("doctype"), r.hs("doc_id"))
			return firstPositive(catNum(d["taxable_amount"]), catNum(d["invoice_amount"]), catNum(d["amount"]))
		},
		"GST %": func(r *catRow) interface{} { // the TDS rate the deduction works out to
			d := r.master(r.hs("doctype"), r.hs("doc_id"))
			base := firstPositive(catNum(d["taxable_amount"]), catNum(d["invoice_amount"]), catNum(d["amount"]))
			return catPct(r.hn("credit")-r.hn("debit"), base)
		},
		"Net Amount": func(r *catRow) interface{} { return r.hn("credit") - r.hn("debit") },
		"Status":     func(r *catRow) interface{} { return r.master(r.hs("doctype"), r.hs("doc_id"))["_status"] },
	})}

var catAssets = &catSource{Label: "Asset", Doctype: "Asset", DateField: "acquisition_date", SKU: "item_code", Number: "code",
	Statuses: []string{"Draft", "Active", "Capitalised", "Disposed", "Retired"},
	Cols: map[string]catFn{
		"Item Name": func(r *catRow) interface{} {
			return firstNonEmpty(catStr(r.item()["name"]), r.hs("category"), r.hs("serial_number"))
		},
		"Cost Center": func(r *catRow) interface{} {
			return r.nameOf("CostCenter", firstNonEmpty(r.hs("cost_center"), catStr(r.store()["cost_center"])))
		},
		"Purchase Price": catHN("cost"),
		"Debit Amount":   func(r *catRow) interface{} { return assetDepreciation(r, true) },
		"Balance":        func(r *catRow) interface{} { return r.hn("cost") - assetDepreciation(r, false) },
	}}

// assetDepreciation: depreciation posted for the asset (5310), in the
// period or to date.
func assetDepreciation(r *catRow, inPeriod bool) float64 {
	key := "dep:all"
	if inPeriod {
		key = "dep:period"
	}
	m, ok := r.lk.extra[key].(map[string]float64)
	if !ok {
		m = map[string]float64{}
		q := fmt.Sprintf(`SELECT document_id, COALESCE(SUM(debit), 0) FROM %s.gl_postings WHERE document_type = 'Asset' AND account_code = '5310'`, r.lk.schema)
		var args []interface{}
		if inPeriod {
			if v := r.p["from"]; v != "" {
				args = append(args, v)
				q += fmt.Sprintf(" AND created_at >= public.erp_day_start($%d::date)", len(args))
			}
			if v := r.p["to"]; v != "" {
				args = append(args, v)
				q += fmt.Sprintf(" AND created_at < public.erp_day_start($%d::date + 1)", len(args))
			}
		}
		rows, err := dbQuery(q+" GROUP BY 1", args...)
		if err == nil {
			for rows.Next() {
				var id string
				var paise int64
				if rows.Scan(&id, &paise) == nil {
					m[id] = PaiseToRupees(paise)
				}
			}
			rows.Close()
		}
		r.lk.extra[key] = m
	}
	return m[r.ID]
}

// --- budgets, intercompany, audit trail, period close ----------------------------

var catBudgets = &catSource{Label: "Budget", Doctype: "Budget", StoreField: "-",
	Cols: map[string]catFn{
		"From Date": catHD("period_start"),
		"To Date":   catHD("period_end"),
		"Department": func(r *catRow) interface{} {
			return r.nameOf("Department", catStr(r.master("CostCenter", r.hs("cost_center"))["department"]))
		},
		"Cost Center":   func(r *catRow) interface{} { return r.nameOf("CostCenter", r.hs("cost_center")) },
		"GL Account":    func(r *catRow) interface{} { return r.hs("account_code") },
		"Target Amount": catHN("planned_amount"),
		"Net Amount":    func(r *catRow) interface{} { return budgetActual(r) },
		"Amount Variance": func(r *catRow) interface{} {
			return budgetActual(r) - r.hn("planned_amount")
		},
		"Achievement %": func(r *catRow) interface{} { return catPct(budgetActual(r), r.hn("planned_amount")) },
	}}

// budgetActual is what was posted to the budget's account and cost center
// in its period (debits less credits: spend).
func budgetActual(r *catRow) float64 {
	cache, ok := r.lk.extra["budgetactual"].(map[string]float64)
	if !ok {
		cache = map[string]float64{}
		r.lk.extra["budgetactual"] = cache
	}
	if v, ok := cache[r.ID]; ok {
		return v
	}
	var paise int64
	rows, err := dbQuery(fmt.Sprintf(`SELECT COALESCE(SUM(debit - credit), 0) FROM %s.gl_postings WHERE account_code = $1
		AND ($2 = '' OR cost_center = $2) AND created_at >= public.erp_day_start($3::date) AND created_at < public.erp_day_start($4::date + 1)`, r.lk.schema),
		r.hs("account_code"), r.hs("cost_center"), firstNonEmpty(catDay(r.hv("period_start")), "1900-01-01"),
		firstNonEmpty(catDay(r.hv("period_end")), BusinessToday()))
	if err == nil {
		if rows.Next() {
			_ = rows.Scan(&paise)
		}
		rows.Close()
	}
	cache[r.ID] = PaiseToRupees(paise)
	return cache[r.ID]
}

var catIntercompany = &catSource{Label: "Intercompany", Rows: glRows("p.account_code IN ('1700', '2500')"), StoreField: "-",
	Cols: mergeCols(glCols, map[string]catFn{
		"Legal Entity": func(r *catRow) interface{} {
			e := r.hs("entity")
			if e == "" {
				d := r.master("IntercompanyTransaction", r.hs("doc_id"))
				if r.hs("account") == "1700" {
					e = catStr(d["from_entity"])
				} else {
					e = catStr(d["to_entity"])
				}
			}
			return r.nameOf("LegalEntity", e)
		},
		"Reconciliation Status": func(r *catRow) interface{} {
			if s := catStr(r.master("IntercompanyTransaction", r.hs("doc_id"))["_status"]); s != "" {
				return s
			}
			return "Posted"
		},
	}),
}

var accountingAuditDoctypes = []string{"JournalVoucher", "VendorInvoice", "SalesInvoice", "PaymentProposal", "CreditNote", "DebitNote",
	"VendorCreditNote", "Asset", "Budget", "IntercompanyTransaction", "BankStatementLine", "LandedCostVoucher", "ExpenseClaim"}

var catAccountingAudit = &catSource{Label: "Accounting change", Rows: auditChangeRows(accountingAuditDoctypes, nil), StoreField: "-",
	Cols: map[string]catFn{
		"Audit ID":      func(r *catRow) interface{} { return r.ID },
		"Posting Date":  func(r *catRow) interface{} { return catDay(r.Created) },
		"Voucher No":    func(r *catRow) interface{} { return catDocNumber(r, r.hs("doctype"), r.hs("doc_id")) },
		"Document Type": catH("doctype"),
		"Document No":   func(r *catRow) interface{} { return catDocNumber(r, r.hs("doctype"), r.hs("doc_id")) },
		"Edit Action": func(r *catRow) interface{} {
			return map[bool]string{true: "Added", false: "Changed"}[r.hs("old") == ""]
		},
		"Field Changed": catH("field"),
		"Old Value":     catH("old"),
		"New Value":     catH("new"),
		"Edited At":     func(r *catRow) interface{} { return catStamp(r.Created) },
	}}

func periodCloseRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	rows, err := dbQuery(fmt.Sprintf(`SELECT t.id::text, COALESCE(t.title, t.task_key), COALESCE(t.owner, ''), COALESCE(t.status, ''), COALESCE(t.evidence, ''),
		COALESCE(t.signed_by, ''), COALESCE(to_char(t.signed_at, 'YYYY-MM-DD HH24:MI'), ''), to_char(a.start_date, 'YYYY-MM-DD'), to_char(a.end_date, 'YYYY-MM-DD'),
		COALESCE(a.period_name, '') FROM %[1]s.period_close_tasks t JOIN %[1]s.accounting_periods a ON a.id = t.period_id
		ORDER BY a.start_date DESC, t.id`, schema))
	if err != nil {
		return nil, nil // no period close on this client yet
	}
	defer rows.Close()
	var out []*catRow
	for rows.Next() {
		var id, title, owner, status, evidence, signedBy, signedAt, start, end, period string
		if rows.Scan(&id, &title, &owner, &status, &evidence, &signedBy, &signedAt, &start, &end, &period) != nil {
			continue
		}
		if (p["from"] != "" && end < p["from"]) || (p["to"] != "" && start > p["to"]) {
			continue
		}
		out = append(out, &catRow{ID: id, Status: status, CreatedBy: owner, H: map[string]interface{}{"title": title, "evidence": evidence,
			"signed_by": signedBy, "signed_at": signedAt, "start": start, "end": end, "period": period}})
	}
	return out, rows.Err()
}

var catPeriodClose = &catSource{Label: "Close task", Rows: periodCloseRows, StoreField: "-",
	Cols: map[string]catFn{
		"From Date":     catH("start"),
		"To Date":       catH("end"),
		"Legal Entity":  func(r *catRow) interface{} { return r.hs("period") },
		"Document Type": catH("title"),
		"Approved By":   catH("signed_by"),
		"Approved At":   catH("signed_at"),
		"Remarks":       catH("evidence"),
	}}

var _ = catWire("cat-general-ledger", &catReport{Src: glSource("TRUE")})
var _ = catWire("cat-trial-balance", &catReport{Src: catTrial})
var _ = catWire("cat-profit-and-loss-statement", &catReport{Src: glSource("a.account_type IN ('Revenue', 'Expense')"),
	Group: []string{"From Date", "To Date", "GL Account", "Department", "Cost Center"}})
var _ = catWire("cat-balance-sheet", &catReport{Src: catBalanceSheet, NoDates: false})
var _ = catWire("cat-cash-flow-statement", &catReport{Src: glSource("p.account_code IN ('1100', '1101', '1102')"),
	Group: []string{"From Date", "To Date", "GL Account"},
	Cols: map[string]catFn{"GL Account": func(r *catRow) interface{} {
		return cashFlowActivity(r.hs("doctype")) + " - " + catStr(glAccountLabel(r))
	}, "Net Amount": func(r *catRow) interface{} { return r.hn("debit") - r.hn("credit") }}})
var _ = catWire("cat-ap-aging", &catReport{Src: catAPOpen, NoDates: true})

// Approved = posted and not yet fully settled - the receivables account's basis.
var _ = catWire("cat-ar-aging", &catReport{Src: catSalesInvoiceAcct, NoDates: true,
	Keep: func(r *catRow) bool { return r.Status == "Approved" && invoiceOutstanding(r, "total_amount") > 0.004 }})
var _ = catWire("cat-vendor-ledger", &catReport{Src: catVendorLedger, PostAll: partyRunningBalance("Vendor Code")})
var _ = catWire("cat-customer-ledger", &catReport{Src: catCustomerLedger, PostAll: partyRunningBalance("Customer ID")})
var _ = catWire("cat-vendor-invoice-register", &catReport{Src: catVendorInvoices})
var _ = catWire("cat-payment-proposal-register", &catReport{Src: catProposals})
var _ = catWire("cat-finance-approval-pending-report", &catReport{Src: catFinancePending, NoDates: true})
var _ = catWire("cat-sales-invoice-accounting-register", &catReport{Src: catSalesInvoiceAcct})
var _ = catWire("cat-payment-register", &catReport{Src: catPayments})
var _ = catWire("cat-receipt-register", &catReport{Src: catReceipts})
var _ = catWire("cat-journal-voucher-register", &catReport{Src: catJVLines})
var _ = catWire("cat-debit-credit-note-register", &catReport{Src: catNotesAll})
var _ = catWire("cat-bank-reconciliation-report", &catReport{Src: catBankLines})
var _ = catWire("cat-tds-register", &catReport{Src: catTDS})
var _ = catWire("cat-fixed-asset-register", &catReport{Src: catAssets})
var _ = catWire("cat-depreciation-schedule", &catReport{Src: catAssets})
var _ = catWire("cat-cost-center-p-and-l", &catReport{Src: glSource("a.account_type IN ('Revenue', 'Expense')"),
	Group: []string{"From Date", "To Date", "Department", "Cost Center", "GL Account"}})
var _ = catWire("cat-budget-vs-actual", &catReport{Src: catBudgets, NoDates: true})
var _ = catWire("cat-intercompany-reconciliation", &catReport{Src: catIntercompany})
var _ = catWire("cat-accounting-audit-trail", &catReport{Src: catAccountingAudit})
var _ = catWire("cat-period-close-checklist", &catReport{Src: catPeriodClose})

// apOpenRows: the payables subledger's open items (payables_subledger.go) -
// each goods receipt still owed for, with the bills booked against it; the
// same items GRN suspense (2100) carries, so the ageing ties to it.
func apOpenRows(tenantID, schema string, p map[string]string) ([]*catRow, error) {
	items, err := payablesOpenItems(dbQueryer{}, schema, time.Time{})
	if err != nil {
		return nil, err
	}
	var out []*catRow
	for _, it := range items {
		if it.Outstanding == 0 {
			continue
		}
		out = append(out, &catRow{ID: it.Key, Created: it.ReceivedOn, H: map[string]interface{}{"vendor": it.Vendor, "grn": it.GRNID,
			"bills": strings.Join(it.Bills, ", "), "due": it.DueDate, "outstanding": PaiseToRupees(it.Outstanding), "bucket": it.Bucket,
			"source": it.SourceType + " " + it.SourceID}})
	}
	return out, nil
}

// dbQueryer lets the shared pool stand in where a transaction is taken.
type dbQueryer struct{}

func (dbQueryer) Query(q string, args ...interface{}) (*sql.Rows, error) {
	return db.DB.Query(q, args...)
}
func (dbQueryer) QueryRow(q string, args ...interface{}) *sql.Row { return db.DB.QueryRow(q, args...) }

var catAPOpen = &catSource{Label: "Payable", Rows: apOpenRows, StoreOf: func(r *catRow) string {
	return catStr(r.master("GRN", r.hs("grn"))["location"])
},
	Cols: map[string]catFn{
		"Vendor Code": catH("vendor"),
		"Vendor Name": func(r *catRow) interface{} { return r.nameOf("Vendor", r.hs("vendor")) },
		"Vendor Invoice No": func(r *catRow) interface{} {
			return firstNonEmpty(r.hs("bills"), "Not billed yet ("+r.hs("source")+")")
		},
		"Posting Date":       func(r *catRow) interface{} { return catDay(r.Created) },
		"Due Date":           catH("due"),
		"Outstanding Amount": catHN("outstanding"),
		"Aging Bucket":       catH("bucket"),
		"Status": func(r *catRow) interface{} {
			if r.hn("outstanding") < 0 {
				return "Over-cleared"
			}
			if r.hs("bills") == "" {
				return "Received, not billed"
			}
			return "Billed, unpaid"
		},
	}}
