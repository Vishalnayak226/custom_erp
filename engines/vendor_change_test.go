package engines

import (
	"encoding/json"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 148.10 - vendor changes wait for approval: a change to a listed field
// (setting procurement.vendor_approval_fields) keeps the live value, files a
// VendorChange request, pauses payments to the vendor, and is applied when a
// checker approves; the CSV import is held the same way; a new vendor is not.

func (f *creditFixture) loadVendor(id string) map[string]interface{} {
	f.t.Helper()
	var raw []byte
	if err := db.DB.QueryRow("SELECT data FROM "+f.schema+".documents WHERE doctype = 'Vendor' AND id = $1", id).Scan(&raw); err != nil {
		f.t.Fatalf("load vendor %s: %v", id, err)
	}
	var d map[string]interface{}
	_ = json.Unmarshal(raw, &d)
	return d
}

func (f *creditFixture) pendingChange(vendorID string) string {
	var id string
	_ = db.DB.QueryRow("SELECT id FROM "+f.schema+".documents WHERE doctype = 'VendorChange' AND status = 'Pending Approval' AND data->>'vendor_id' = $1", vendorID).Scan(&id)
	return id
}

func TestStage14810ListedChangeWaitsAndPausesPayments(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("V-1", map[string]interface{}{"bank_account_number": "111", "credit_limit": 1000.0})
	f.bill("B-1", "V-1", 1000, BusinessToday())

	prior := f.loadVendor("V-1")
	payload := map[string]interface{}{}
	for k, v := range prior {
		payload[k] = v
	}
	payload["bank_account_number"] = "999"
	payload["credit_limit"] = 5000.0
	payload["name"] = "Acme Renamed" // not a listed field: applies at once
	held, err := HoldVendorChanges(f.tenantID, "V-1", prior, payload, "maker", "Accountant")
	if err != nil {
		t.Fatalf("HoldVendorChanges: %v", err)
	}
	if len(held) != 2 {
		t.Fatalf("held fields = %v, want bank_account_number and credit_limit", held)
	}
	if payload["bank_account_number"] != "111" || payload["credit_limit"] != 1000.0 || payload["name"] != "Acme Renamed" {
		t.Fatalf("live values must stay and the unlisted edit must go through, payload = %v", payload)
	}
	id := f.pendingChange("V-1")
	if id == "" {
		t.Fatal("no VendorChange request is waiting")
	}
	var before, after map[string]interface{}
	_ = json.Unmarshal([]byte(f.field("VendorChange", id, "old_values")), &before)
	_ = json.Unmarshal([]byte(f.field("VendorChange", id, "new_values")), &after)
	if before["bank_account_number"] != "111" || after["bank_account_number"] != "999" || after["credit_limit"] != 5000.0 {
		t.Fatalf("before / after view wrong: before=%v after=%v", before, after)
	}

	// payments to the vendor pause while the change waits
	if _, _, err := PayVendorInvoice(f.tenantID, "B-1", "u1", "Accountant", ""); err == nil || !strings.Contains(err.Error(), "waiting for approval") {
		t.Fatalf("payment while a change waits: want the pause refusal, got %v", err)
	}

	// the maker cannot approve their own change when self-approval is off
	withSelfApproval(t, f.tenantID, SelfApprovalNobody)
	if err := DecideApproval(f.tenantID, VendorChangeDoctype, id, "maker", "Accountant", "", "Approved", "mine"); err == nil {
		t.Fatal("the maker approved their own change with self-approval off")
	}
	if got := f.loadVendor("V-1")["bank_account_number"]; got != "111" {
		t.Fatalf("a refused approval changed the vendor: %v", got)
	}
	// a second person can
	if err := DecideApproval(f.tenantID, VendorChangeDoctype, id, "checker", "Accountant", "", "Approved", "ok"); err != nil {
		t.Fatalf("checker approval: %v", err)
	}
	v := f.loadVendor("V-1")
	if v["bank_account_number"] != "999" || v["credit_limit"] != 5000.0 {
		t.Fatalf("approved change not applied: %v", v)
	}
	if f.field("VendorChange", id, "applied_at") == "" {
		t.Fatal("applied_at not stamped")
	}
	// the edit shows in the audit log, so the bank-then-paid alert still sees it
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".audit_logs WHERE action = 'EDIT_Vendor' AND details LIKE '%bank_account_number%' AND details LIKE '%V-1%'").Scan(&n)
	if n == 0 {
		t.Fatal("no EDIT_Vendor audit row for the approved bank change")
	}
	// payments resume
	if _, _, err := PayVendorInvoice(f.tenantID, "B-1", "u1", "Accountant", ""); err != nil {
		t.Fatalf("payment after the change was decided: %v", err)
	}
}

func TestStage14810RejectedChangeKeepsLiveValues(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("V-2", map[string]interface{}{"gstin": "29ABCDE1234F1Z5"})
	prior := f.loadVendor("V-2")
	payload := map[string]interface{}{"gstin": "27ABCDE1234F1Z5"}
	if _, err := HoldVendorChanges(f.tenantID, "V-2", prior, payload, "maker", "Accountant"); err != nil {
		t.Fatal(err)
	}
	id := f.pendingChange("V-2")
	if err := DecideApproval(f.tenantID, VendorChangeDoctype, id, "checker", "Accountant", "", "Rejected", "wrong GSTIN"); err != nil {
		t.Fatal(err)
	}
	if got := f.loadVendor("V-2")["gstin"]; got != "29ABCDE1234F1Z5" {
		t.Fatalf("a rejected change altered the vendor: %v", got)
	}
	if f.pendingChange("V-2") != "" {
		t.Fatal("a decided request still counts as waiting")
	}
}

func TestStage14810NewVendorAndEmptyListAreNotHeld(t *testing.T) {
	f := newCreditFixture(t)
	// a brand-new vendor (no prior record) is not held
	payload := map[string]interface{}{"bank_account_number": "555"}
	if held, err := HoldVendorChanges(f.tenantID, "V-NEW", nil, payload, "maker", "Accountant"); err != nil || len(held) != 0 || payload["bank_account_number"] != "555" {
		t.Fatalf("new vendor: held=%v err=%v payload=%v", held, err, payload)
	}
	f.vendor("V-3", map[string]interface{}{"bank_account_number": "111"})
	// an unchanged value (same text, number typed as text) is not a change
	same := map[string]interface{}{"bank_account_number": " 111 "}
	if held, _ := HoldVendorChanges(f.tenantID, "V-3", f.loadVendor("V-3"), same, "maker", "Accountant"); len(held) != 0 {
		t.Fatalf("an unchanged value was held: %v", held)
	}
	// an empty list applies every edit at once, as before this item
	if err := SetSetting(f.tenantID, SettingVendorApprovalFields, "", "test"); err != nil {
		t.Fatal(err)
	}
	edit := map[string]interface{}{"bank_account_number": "222"}
	if held, err := HoldVendorChanges(f.tenantID, "V-3", f.loadVendor("V-3"), edit, "maker", "Accountant"); err != nil || len(held) != 0 || edit["bank_account_number"] != "222" {
		t.Fatalf("empty list: held=%v err=%v payload=%v", held, err, edit)
	}
	if f.pendingChange("V-3") != "" {
		t.Fatal("an empty list filed a request")
	}
}

func TestStage14810CSVImportIsHeldTheSameWay(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("V-4", map[string]interface{}{"bank_account_number": "111", "bank_ifsc": "HDFC0000001"})
	csv := "id,code,name,state,bank_account_number\nV-4,V-4,Vendor V-4,Karnataka,777\nV-5,V-5,Vendor V-5,Karnataka,888\n"
	res, err := BulkImportCSV(f.tenantID, "Vendor", strings.NewReader(csv), "maker", "", false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if got := f.loadVendor("V-4")["bank_account_number"]; got != "111" {
		t.Fatalf("an import rewrote a listed field live: %v (result %+v)", got, res)
	}
	if f.pendingChange("V-4") == "" {
		t.Fatalf("the import filed no request (result %+v)", res)
	}
	// the new vendor in the same file is created with its bank as given
	if got := f.loadVendor("V-5")["bank_account_number"]; got != "888" {
		t.Fatalf("a new vendor from an import must not wait: %v", got)
	}
}
