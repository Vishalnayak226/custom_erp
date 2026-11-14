package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

func seedStaffVendor(t *testing.T, schema, doctype, id string, data map[string]interface{}) {
	t.Helper()
	raw, _ := json.Marshal(data)
	db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,'Active','system')", id, doctype, raw); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func subjectField(t *testing.T, schema, id, field string) string {
	t.Helper()
	var v string
	if err := db.DB.QueryRow("SELECT COALESCE(data->>$2,'') FROM "+schema+".documents WHERE id = $1", id, field).Scan(&v); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return v
}

func wantValidationCode(t *testing.T, err error, code string) {
	t.Helper()
	if ve, ok := err.(*ValidationError); !ok || ve.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}

func TestAnonymizeEmployeeKeepsPayrollAndTaxFields(t *testing.T) {
	tenant, schema := retentionTestSetup(t)
	id := "I170-EMP-1"
	seedStaffVendor(t, schema, "Employee", id, map[string]interface{}{"name": "Asha Rao", "status": "Exited", "date_of_exit": "2020-01-31",
		"phone": "9811111111", "email": "asha@example.com", "pan": "ABCDE1234F", "bank_account_no": "123456789", "bank_ifsc": "HDFC0000001", "code": "E-9"})
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + id + "'")
	if err := AnonymizeStaffOrVendor(tenant, "Employee", id, "tester", "asked"); err != nil {
		t.Fatalf("anonymise: %v", err)
	}
	for _, f := range []string{"phone", "email"} {
		if v := subjectField(t, schema, id, f); v != "" {
			t.Errorf("%s survived anonymisation: %q", f, v)
		}
	}
	if subjectField(t, schema, id, "name") == "Asha Rao" {
		t.Error("the name survived")
	}
	for f, want := range map[string]string{"pan": "ABCDE1234F", "bank_account_no": "123456789", "bank_ifsc": "HDFC0000001", "code": "E-9"} {
		if v := subjectField(t, schema, id, f); v != want {
			t.Errorf("statutory field %s = %q, want it kept (%q)", f, v, want)
		}
	}
	exp, err := ExportStaffVendorData(tenant, "Employee", id)
	if err != nil || exp["pan"] != "ABCDE1234F" {
		t.Errorf("export: %v %v", exp, err)
	}
}

func TestAnonymizeEmployeeRefusals(t *testing.T) {
	tenant, schema := retentionTestSetup(t)
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'I170-EMP-%'")
	seedStaffVendor(t, schema, "Employee", "I170-EMP-ACT", map[string]interface{}{"name": "A", "status": "Active", "phone": "1"})
	wantValidationCode(t, AnonymizeStaffOrVendor(tenant, "Employee", "I170-EMP-ACT", "t", "r"), "SEC-1706")
	seedStaffVendor(t, schema, "Employee", "I170-EMP-HOLD", map[string]interface{}{"name": "B", "status": "Exited", "phone": "1", "legal_hold": true, "legal_hold_reason": "case 7"})
	wantValidationCode(t, AnonymizeStaffOrVendor(tenant, "Employee", "I170-EMP-HOLD", "t", "r"), "SEC-1705")
	if subjectField(t, schema, "I170-EMP-HOLD", "phone") != "1" {
		t.Error("a held employee was changed")
	}
	wantValidationCode(t, AnonymizeStaffOrVendor(tenant, "Employee", "I170-EMP-NONE", "t", "r"), "SEC-1709")
}

func TestAnonymizeVendorRefusesOpenOrderThenKeepsTaxFields(t *testing.T) {
	tenant, schema := retentionTestSetup(t)
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'I170-V%'")
	seedStaffVendor(t, schema, "Vendor", "I170-VEND", map[string]interface{}{"name": "Acme Traders", "status": "Active", "contact_person": "Ravi", "phone": "9822222222",
		"email": "v@example.com", "gstin": "27AAAPL1234C1ZV", "pan": "AAAPL1234C", "bank_account_number": "55512345"})
	seedStaffVendor(t, schema, "PurchaseOrder", "I170-VPO", map[string]interface{}{"vendor": "I170-VEND"})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Approved' WHERE id = 'I170-VPO'")
	wantValidationCode(t, AnonymizeStaffOrVendor(tenant, "Vendor", "I170-VEND", "t", "r"), "SEC-1708")
	if subjectField(t, schema, "I170-VEND", "phone") != "9822222222" {
		t.Error("a refused vendor was changed")
	}
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Closed' WHERE id = 'I170-VPO'")
	if err := AnonymizeStaffOrVendor(tenant, "Vendor", "I170-VEND", "t", "r"); err != nil {
		t.Fatalf("anonymise: %v", err)
	}
	for _, f := range []string{"phone", "email", "contact_person"} {
		if v := subjectField(t, schema, "I170-VEND", f); v != "" {
			t.Errorf("vendor %s survived: %q", f, v)
		}
	}
	for f, want := range map[string]string{"gstin": "27AAAPL1234C1ZV", "pan": "AAAPL1234C", "bank_account_number": "55512345"} {
		if v := subjectField(t, schema, "I170-VEND", f); v != want {
			t.Errorf("vendor %s = %q, want kept", f, v)
		}
	}
}
