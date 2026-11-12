package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 55.5a - second-person approval of returns and refunds. Off (the
// default) the person who raised a return may approve it and its refund, as
// before. On, they are refused (SALESR-0617) and a colleague is not; an
// approver who cannot be identified is refused; the till's exchange path,
// which approves at the counter, is unaffected. Own throwaway tenant.
func TestStage555aSecondPersonApproval(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	put := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system') ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, status = EXCLUDED.status", id, doctype, raw, status); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	ret := func(id string) {
		put(id, "ReturnRequest", "Requested", map[string]interface{}{"code": id, "status": "Requested", "requested_by": "cashier1", "request_type": "Customer Return", "items": []interface{}{}})
	}
	refund := func(id, returnID string) {
		put(id, "RefundRequest", "Pending", map[string]interface{}{"code": id, "status": "Pending", "return_request_id": returnID, "amount": 900.0})
	}
	code := func(err error) string {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Code
		}
		if err != nil {
			return "other: " + err.Error()
		}
		return ""
	}

	// Off: unchanged.
	ret("RR-SP-1")
	refund("RF-SP-1", "RR-SP-1")
	if err := ApproveReturnRequest(tenantID, "RR-SP-1", "cashier1"); err != nil {
		t.Fatalf("off: the raiser may approve the return: %v", err)
	}
	if err := ApproveRefundRequest(tenantID, "RF-SP-1", "cashier1"); err != nil {
		t.Fatalf("off: the raiser may approve the refund: %v", err)
	}

	if err := SetSetting(tenantID, "returns.second_person_approval", "true", "tester"); err != nil {
		t.Fatal(err)
	}
	ret("RR-SP-2")
	refund("RF-SP-2", "RR-SP-2")
	if err := ApproveReturnRequest(tenantID, "RR-SP-2", "cashier1"); code(err) != "SALESR-0617" {
		t.Errorf("on: the raiser approving the return must be refused, got %v", err)
	}
	if err := ApproveReturnRequest(tenantID, "RR-SP-2", "CASHIER1 "); code(err) != "SALESR-0617" {
		t.Errorf("on: the same user in other case / spacing is still the same person, got %v", err)
	}
	if err := ApproveReturnRequest(tenantID, "RR-SP-2", ""); code(err) != "SALESR-0617" {
		t.Errorf("on: an unidentified approver must be refused, got %v", err)
	}
	if err := ApproveRefundRequest(tenantID, "RF-SP-2", "cashier1"); code(err) != "SALESR-0617" {
		t.Errorf("on: the raiser approving the refund must be refused, got %v", err)
	}
	if err := ApproveReturnRequest(tenantID, "RR-SP-2", "manager1"); err != nil {
		t.Errorf("on: a colleague approves the return: %v", err)
	}
	if err := ApproveRefundRequest(tenantID, "RF-SP-2", "manager1"); err != nil {
		t.Errorf("on: a colleague approves the refund: %v", err)
	}
	var approver string
	_ = db.DB.QueryRow(`SELECT data->>'approved_by' FROM ` + schemaName + `.documents WHERE id = 'RR-SP-2'`).Scan(&approver)
	if approver != "manager1" {
		t.Errorf("the approval records who approved: %q", approver)
	}

	// The till's exchange approves at the counter, setting or not.
	ret("RR-SP-3")
	refund("RF-SP-3", "RR-SP-3")
	if err := approveReturnRequest(tenantID, "RR-SP-3", "cashier1", false); err != nil {
		t.Errorf("exchange path: %v", err)
	}
	if err := approveRefundRequest(tenantID, "RF-SP-3", "cashier1", false); err != nil {
		t.Errorf("exchange path refund: %v", err)
	}
}
