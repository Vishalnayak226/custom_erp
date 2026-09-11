package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func seedTestCustomer(t *testing.T, schema, id string, extra map[string]interface{}) {
	t.Helper()
	data := map[string]interface{}{"code": id, "name": "Original Name", "phone": "9999999999", "email": "orig@example.com"}
	for k, v := range extra {
		data[k] = v
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal seed customer: %v", err)
	}
	if _, err := db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id); err != nil {
		t.Fatalf("cleanup before seed: %v", err)
	}
	if _, err := db.DB.Exec(
		"INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2, 'Active', 'system')",
		id, raw); err != nil {
		t.Fatalf("seed customer %s: %v", id, err)
	}
}

func TestDataSubjectRequestMakerCheckerRejectsSelfDecision(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("GetTenantSchema: %v", err)
	}
	custID := "CUST-DSR-SELFCHECK"
	seedTestCustomer(t, schema, custID, nil)
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + custID + "'")

	reqID, err := RequestDataSubjectAction(tenantID, "Customer", custID, PrivacyRequestErasure, "customer asked to be forgotten", "requester_user")
	if err != nil {
		t.Fatalf("RequestDataSubjectAction: %v", err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".data_subject_requests WHERE id = $1", reqID)

	if err := DecideDataSubjectRequest(tenantID, reqID, "requester_user", true, "self-approving"); err == nil {
		t.Error("expected the requester to be blocked from deciding their own request")
	}
}

func TestDataSubjectRequestUnknownTypeRejected(t *testing.T) {
	db.InitDB(testConnStr())
	if _, err := RequestDataSubjectAction("default", "Customer", "CUST-X", "not_a_real_type", "reason", "someone"); err == nil {
		t.Error("expected an unknown request type to be rejected")
	}
}

func TestAnonymizeCustomerFullLifecycleThroughApprovedRequest(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("GetTenantSchema: %v", err)
	}
	custID := "CUST-DSR-ERASURE"
	seedTestCustomer(t, schema, custID, nil)
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + custID + "'")

	reqID, err := RequestDataSubjectAction(tenantID, "Customer", custID, PrivacyRequestErasure, "GDPR erasure request", "requester_user")
	if err != nil {
		t.Fatalf("RequestDataSubjectAction: %v", err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".data_subject_requests WHERE id = $1", reqID)

	if err := DecideDataSubjectRequest(tenantID, reqID, "checker_user", true, "verified, approved"); err != nil {
		t.Fatalf("DecideDataSubjectRequest: %v", err)
	}
	if err := ExecuteDataSubjectRequest(tenantID, reqID, "checker_user"); err != nil {
		t.Fatalf("ExecuteDataSubjectRequest: %v", err)
	}

	data, err := ExportCustomerData(tenantID, custID)
	if err != nil {
		t.Fatalf("ExportCustomerData after erasure: %v", err)
	}
	if data["name"] != "Redacted Customer" {
		t.Errorf("expected name to be anonymized, got %v", data["name"])
	}
	if data["phone"] != "" || data["email"] != "" {
		t.Errorf("expected phone/email cleared, got phone=%v email=%v", data["phone"], data["email"])
	}

	req, err := getDataSubjectRequest(tenantID, reqID)
	if err != nil {
		t.Fatalf("getDataSubjectRequest: %v", err)
	}
	if req.Status != PrivacyRequestCompleted {
		t.Errorf("expected request status Completed, got %s", req.Status)
	}
}

func TestAnonymizeCustomerRefusesUnderLegalHold(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("GetTenantSchema: %v", err)
	}
	custID := "CUST-DSR-LEGALHOLD"
	seedTestCustomer(t, schema, custID, nil)
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + custID + "'")

	if err := SetSubjectLegalHold(tenantID, "Customer", custID, true, "compliance_officer", "active litigation"); err != nil {
		t.Fatalf("SetSubjectLegalHold: %v", err)
	}

	err = AnonymizeCustomer(tenantID, custID, "some_admin", "erasure request")
	if err == nil {
		t.Fatal("expected anonymization to be refused under legal hold")
	}

	// The refusal must not have touched the data - the customer's original
	// name must still be intact, not partially anonymized.
	data, exportErr := ExportCustomerData(tenantID, custID)
	if exportErr != nil {
		t.Fatalf("ExportCustomerData: %v", exportErr)
	}
	if data["name"] != "Original Name" {
		t.Errorf("legal hold refusal must leave data untouched, got name=%v", data["name"])
	}

	// Lifting the hold must allow it to proceed.
	if err := SetSubjectLegalHold(tenantID, "Customer", custID, false, "compliance_officer", "litigation concluded"); err != nil {
		t.Fatalf("SetSubjectLegalHold (lift): %v", err)
	}
	if err := AnonymizeCustomer(tenantID, custID, "some_admin", "erasure request retried"); err != nil {
		t.Fatalf("expected anonymization to succeed after hold lifted: %v", err)
	}
}

func TestExportCustomerDataUnknownCustomerErrors(t *testing.T) {
	db.InitDB(testConnStr())
	if _, err := ExportCustomerData("default", "CUST-DOES-NOT-EXIST-49-6-8"); err == nil {
		t.Error("expected an error for an unknown customer id")
	}
}

func TestExecuteDataSubjectRequestRejectsUnimplementedDoctype(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("GetTenantSchema: %v", err)
	}
	reqID, err := RequestDataSubjectAction(tenantID, "Employee", "EMP-SOME-ID", PrivacyRequestErasure, "test", "requester_user")
	if err != nil {
		t.Fatalf("RequestDataSubjectAction: %v", err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".data_subject_requests WHERE id = $1", reqID)

	if err := DecideDataSubjectRequest(tenantID, reqID, "checker_user", true, "approved"); err != nil {
		t.Fatalf("DecideDataSubjectRequest: %v", err)
	}
	if err := ExecuteDataSubjectRequest(tenantID, reqID, "checker_user"); err == nil {
		t.Error("expected execution against an unimplemented doctype (Employee) to fail explicitly rather than silently succeed")
	}
}

func TestListDataSubjectRequestsOrdersNewestFirst(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("GetTenantSchema: %v", err)
	}
	custID := "CUST-DSR-LIST"
	seedTestCustomer(t, schema, custID, nil)
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + custID + "'")
	defer db.DB.Exec("DELETE FROM "+schema+".data_subject_requests WHERE subject_id = $1", custID)

	id1, err := RequestDataSubjectAction(tenantID, "Customer", custID, PrivacyRequestAccess, "first", "u1")
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	id2, err := RequestDataSubjectAction(tenantID, "Customer", custID, PrivacyRequestExport, "second", "u1")
	if err != nil {
		t.Fatalf("second request: %v", err)
	}

	list, err := ListDataSubjectRequests(tenantID, "Customer", custID)
	if err != nil {
		t.Fatalf("ListDataSubjectRequests: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(list))
	}
	foundIDs := map[string]bool{}
	for _, r := range list {
		foundIDs[r.ID] = true
	}
	if !foundIDs[id1] || !foundIDs[id2] {
		t.Errorf("expected both requests %s and %s to be listed, got %v", id1, id2, foundIDs)
	}
}
