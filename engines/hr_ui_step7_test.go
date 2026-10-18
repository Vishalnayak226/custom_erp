package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// 73.D.24: a deleted Employee still linked to a user is not "my record", and
// Employee Self-Service reads the appraisal cycles and training programmes its
// own appraisals and training records point at.
func TestStage73D24HRSelfService(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const user, emp = "u-73d24", "EMP-73D24"
	cleanup := func() { db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", emp) }
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, deleted_at) VALUES ($1, 'Employee', $2, 'Active', 'admin', NOW())",
		emp, `{"code":"EMP-73D24","name":"Left the company","user_id":"u-73d24"}`); err != nil {
		t.Fatalf("deleted employee: %v", err)
	}
	if rec, err := GetMyEmployeeRecord("default", user); err != nil || rec != nil {
		t.Fatalf("a deleted employee must not resolve as the user's record, got %v (err %v)", rec, err)
	}

	tpl, ok := RoleTemplateFor("Employee Self-Service")
	if !ok {
		t.Fatal("Employee Self-Service template missing")
	}
	for _, d := range []string{"AppraisalCycle", "TrainingProgram"} {
		if tpl.Doctypes[d] != AccessRead {
			t.Fatalf("Employee Self-Service: want read on %s, got %v", d, tpl.Doctypes[d])
		}
	}
}

// 73.D.23: a payment file carries a part-paid remainder to the paisa; a run
// executed before the fix (whole rupees only) still prints its amount.
func TestStage73D23PaymentFileToThePaisa(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const vendor, inv, prop = "V-73D23", "VI-73D23", "PP-73D23"
	cleanup := func() { db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2, $3)", vendor, inv, prop) }
	cleanup()
	defer cleanup()
	seed := func(id, doctype, status string, data map[string]interface{}) {
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, b, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": "Paisa Payee", "bank_account_number": "9876543210", "bank_ifsc": "TEST0009999"})
	seed(inv, "VendorInvoice", "Paid", map[string]interface{}{"vendor_id": vendor, "invoice_amount": 1000.50, "status": "Paid"})
	results, _ := json.Marshal([]PaymentProposalResultLine{{InvoiceID: inv, Paid: true, Amount: 501, AmountPaise: 50050}})
	seed(prop, "PaymentProposal", "Executed", map[string]interface{}{"invoice_ids": "[]", "execution_results": json.RawMessage(results)})
	csvText, err := GeneratePaymentFile("default", prop)
	if err != nil {
		t.Fatalf("GeneratePaymentFile: %v", err)
	}
	if !strings.Contains(csvText, ",500.50") {
		t.Fatalf("want the remainder 500.50 to the paisa, got:\n%s", csvText)
	}
}
