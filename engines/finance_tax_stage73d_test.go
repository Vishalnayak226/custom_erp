package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// Module 3 (Finance & Tax), Stage 73.D follow-ups.

func t73dDoc(t *testing.T, schema, id, doctype, status string, data map[string]interface{}) {
	t.Helper()
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'admin')", id, doctype, raw, status); err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}

func t73dCleanup(schema string, ids ...string) {
	for _, id := range ids {
		db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", id)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
	}
}

func t73dCode(err error) string {
	var verr *ValidationError
	if errors.As(err, &verr) {
		return verr.Code
	}
	return ""
}

// 73.D.11: a foreign-currency bill is refused by Pay with TDS (it posted the
// foreign figure as rupees) and nothing is posted.
func TestStage73D11TDSRefusesForeignCurrencyBill(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const id = "T73D11-VI"
	t73dCleanup(schema, id)
	defer t73dCleanup(schema, id)
	t73dDoc(t, schema, id, "VendorInvoice", "Matched", map[string]interface{}{"invoice_number": id, "vendor_id": "T73D11-V", "invoice_amount": 100, "currency": "USD", "exchange_rate": 83, "base_invoice_amount": 8300})

	_, _, err := PayVendorInvoiceWithTDS(tenantID, id, "194C", "admin")
	if t73dCode(err) != "FIN-0841" {
		t.Fatalf("want FIN-0841 for a USD bill, got %v", err)
	}
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".gl_postings WHERE document_id = $1", id).Scan(&n)
	if n != 0 {
		t.Fatalf("a refused TDS payment posted %d GL rows", n)
	}
}

// 73.D.13: a paid or part-paid bill cannot be matched again (it could flip a
// Paid bill back to Matched / MismatchHold).
func TestStage73D13MatchRefusesPaidBill(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	ids := []string{"T73D13-PAID", "T73D13-PART"}
	t73dCleanup(schema, ids...)
	defer t73dCleanup(schema, ids...)
	t73dDoc(t, schema, "T73D13-PAID", "VendorInvoice", "Paid", map[string]interface{}{"invoice_number": "T73D13-PAID", "invoice_amount": 100, "po_id": "T73D13-PO", "grn_id": "T73D13-GRN"})
	t73dDoc(t, schema, "T73D13-PART", "VendorInvoice", "Matched", map[string]interface{}{"invoice_number": "T73D13-PART", "invoice_amount": 100, "po_id": "T73D13-PO", "grn_id": "T73D13-GRN",
		"payments": `[{"reference":"P1","kind":"payment","amount":40}]`, "outstanding_amount": 60})

	for _, id := range ids {
		if _, err := MatchVendorInvoice(tenantID, id, "", "", 0); t73dCode(err) != "FIN-0842" {
			t.Errorf("%s: want FIN-0842, got %v", id, err)
		}
		var status string
		db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", id).Scan(&status)
		if (id == "T73D13-PAID" && status != "Paid") || (id == "T73D13-PART" && status != "Matched") {
			t.Errorf("%s: status changed to %s", id, status)
		}
	}
}

// 73.D.12: an unpaid (Matched) supplier bill is a forecast outflow on its due
// date - the forecast used to count only Approved bills.
func TestStage73D12ForecastCountsMatchedSupplierBills(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const id = "T73D12-VI"
	t73dCleanup(schema, id)
	defer t73dCleanup(schema, id)
	before, err := GetCashFlowForecast(tenantID, "2031-01-01", 10)
	if err != nil {
		t.Fatalf("forecast: %v", err)
	}
	t73dDoc(t, schema, id, "VendorInvoice", "Matched", map[string]interface{}{"invoice_number": id, "invoice_amount": 1234.5, "due_date": "2031-01-05"})
	after, err := GetCashFlowForecast(tenantID, "2031-01-01", 10)
	if err != nil {
		t.Fatalf("forecast: %v", err)
	}
	if got := after[4]["expected_outflow"].(float64) - before[4]["expected_outflow"].(float64); got != 1234.5 {
		t.Fatalf("a Matched bill due 2031-01-05 added %v to that day's outflow, want 1234.5", got)
	}
}

// 73.D.15: a record-form save cannot capitalise or dispose an asset (both post
// to the ledger only through their own actions); other edits still save.
func TestStage73D15AssetStatusOnlyThroughItsActions(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	cases := []struct {
		prior, to string
		want      string
	}{
		{"Capitalised", "Disposed", "ASSET-0843"},
		{"Draft", "Capitalised", "ASSET-0843"},
		{"", "Capitalised", "ASSET-0843"},
		{"Capitalised", "Capitalised", ""},
		{"Draft", "Draft", ""},
	}
	for _, c := range cases {
		payload := map[string]interface{}{"status": c.to, "custodian": "T73D15", "reason_code": "T73D15"}
		err := validateAssetStatusWrite(c.prior, payload)
		if got := t73dCode(err); got != c.want {
			t.Errorf("%q -> %q: want %q, got %v", c.prior, c.to, c.want, err)
		}
	}
	if err := ValidateTransactionalRules(tenantID, "Asset", "T73D15-A", "Capitalised", map[string]interface{}{"status": "Capitalised"},
		map[string]interface{}{"status": "Disposed", "reason_code": "T73D15"}); t73dCode(err) != "ASSET-0843" {
		t.Errorf("the shared validator let a record-form disposal through: %v", err)
	}
}

// 73.D.16: a backdated journal voucher's GL rows carry the voucher's date, so
// period reports put it in its own month, not the month it was approved.
func TestStage73D16BackdatedVoucherPostsOnItsDate(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'JournalVoucher' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' = 'TEST-JV73D16')")
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE doctype = 'JournalVoucher' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' = 'TEST-JV73D16')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' = 'TEST-JV73D16'")
	}
	cleanup()
	defer cleanup()
	id, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-JV73D16", []JournalVoucherLine{{AccountCode: "5100", Debit: 73}, {AccountCode: "1100", Credit: 73}}, "manager1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := SubmitForApproval(tenantID, "JournalVoucher", id, "manager1", "Store Manager"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := DecideApproval(tenantID, "JournalVoucher", id, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	var rows int
	var dates []string
	r, err := db.DB.Query("SELECT created_at::date::text FROM "+schema+".gl_postings WHERE document_type = 'JournalVoucher' AND document_id = $1", id)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer r.Close()
	for r.Next() {
		var d string
		r.Scan(&d)
		dates = append(dates, d)
		rows++
	}
	if rows != 2 {
		t.Fatalf("want 2 GL rows, got %d", rows)
	}
	for _, d := range dates {
		if d != "2026-07-25" {
			t.Errorf("a voucher dated 2026-07-25 posted on %s", d)
		}
	}
}

// 73.D.17 (module 4): the global scopes a role template declares apply - HR
// Manager works on everyone's HR records, an Accountant across stores - and a
// role whose template declares none stays confined.
func TestStage73D17TemplateGlobalScopesApply(t *testing.T) {
	cases := []struct {
		role      string
		dimension ScopeDimension
		doctype   string
		want      bool
	}{
		{"HR Manager", ScopeSelf, "Grievance", true},
		{"HR Manager", ScopeSelf, "Appraisal", true},
		{"HR Manager", ScopeLocation, "Attendance", true},
		{"Accountant", ScopeLocation, "ExpenseClaim", true},
		{"Employee Self-Service", ScopeSelf, "Leave", false},
		{"Cashier", ScopeSelf, "Leave", false},
		{"Store Manager", ScopeSelf, "Leave", true},
	}
	for _, c := range cases {
		if got := (SessionScope{Role: c.role}).HasGlobalScope(c.dimension, c.doctype); got != c.want {
			t.Errorf("%s / %s / %s: global = %v, want %v", c.role, c.dimension, c.doctype, got, c.want)
		}
	}
}
