package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 73.2.8 - post-sale edit history: a change to an issued sale needs a
// reason at the shared choke point (ValidateTransactionalRules); a Draft, a
// new document, an open till bill or a save that changes nothing does not;
// the saved edit is recorded field by field and listed by the report.
// Own throwaway tenant.
func TestStage73PostSaleEditHistory(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
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
	prior := map[string]interface{}{"code": "SI-1", "customer": "Ravi", "status": "Approved", "total": 1180.0, "notes": "deliver Monday"}
	edited := map[string]interface{}{"code": "SI-1", "customer": "Ravi", "status": "Approved", "total": "1180", "notes": "deliver Tuesday"}

	if err := ValidateTransactionalRules(tenantID, "SalesInvoice", "SI-1", "Approved", prior, edited); code(err) != "SALESP-0616" {
		t.Errorf("an issued invoice edited without a reason is refused: %v", err)
	}
	if err := ValidateTransactionalRules(tenantID, "SalesInvoice", "SI-1", "Draft", prior, edited); err != nil {
		t.Errorf("a Draft is not a post-sale edit: %v", err)
	}
	if err := ValidateTransactionalRules(tenantID, "SalesInvoice", "", "", nil, edited); err != nil {
		t.Errorf("a new invoice needs no reason: %v", err)
	}
	same := map[string]interface{}{"code": "SI-1", "customer": "Ravi ", "status": "Approved", "total": "1180.00", "notes": "deliver Monday", "expected_version": 3.0}
	if err := ValidateTransactionalRules(tenantID, "SalesInvoice", "SI-1", "Approved", prior, same); err != nil {
		t.Errorf("a save that changes nothing (number sent as text, trailing space) needs no reason: %v", err)
	}
	if err := validatePostSaleEdit("POSCart", "CART-1", "Open", prior, edited); err != nil {
		t.Errorf("an open till bill is not issued yet: %v", err)
	}
	if err := validatePostSaleEdit("POSCart", "CART-1", "Paid", prior, edited); code(err) != "SALESP-0616" {
		t.Errorf("a paid till bill needs a reason: %v", err)
	}
	if err := validatePostSaleEdit("Item", "ITEM-1", "Active", prior, edited); err != nil {
		t.Errorf("only sale documents are covered: %v", err)
	}

	edited["edit_reason"] = "   "
	if err := ValidateTransactionalRules(tenantID, "SalesInvoice", "SI-1", "Approved", prior, edited); code(err) != "SALESP-0616" {
		t.Errorf("a blank reason is no reason: %v", err)
	}
	edited["edit_reason"] = "customer asked for Tuesday"
	if err := ValidateTransactionalRules(tenantID, "SalesInvoice", "SI-1", "Approved", prior, edited); err != nil {
		t.Fatalf("with a reason the edit is allowed: %v", err)
	}

	// The handler pops edit_reason before saving and passes it on.
	delete(edited, "edit_reason")
	if err := RecordPostSaleEdit(tenantID, "SalesInvoice", "SI-1", "Approved", prior, edited, "manager1", "customer asked for Tuesday"); err != nil {
		t.Fatalf("RecordPostSaleEdit: %v", err)
	}
	if err := RecordPostSaleEdit(tenantID, "SalesInvoice", "SI-1", "Draft", prior, edited, "manager1", ""); err != nil {
		t.Fatal(err)
	}
	if err := RecordPostSaleEdit(tenantID, "SalesInvoice", "SI-1", "Approved", prior, same, "manager1", "x"); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schemaName + `.documents WHERE doctype = 'DocumentEditHistory'`).Scan(&n)
	if n != 1 {
		t.Errorf("only the real post-sale edit is recorded, got %d records", n)
	}

	_, rows, _, err := RunReport(tenantID, "post-sale-edit-history", "HR/Admin", "admin", map[string]string{"document_id": "SI-1"})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("one changed field gives one row, got %d: %+v", len(rows), rows)
	}
	r := rows[0]
	if r["field"] != "notes" || r["before"] != "deliver Monday" || r["after"] != "deliver Tuesday" ||
		r["edited_by"] != "manager1" || r["reason"] != "customer asked for Tuesday" || r["status"] != "Approved" || r["document_type"] != "Sales Invoice" || r["document_id"] != "SI-1" {
		t.Errorf("report row: %+v", r)
	}
	if _, rows, _, _ = RunReport(tenantID, "post-sale-edit-history", "HR/Admin", "admin", map[string]string{"document_type": "Sales Invoice", "document_id": "si-1"}); len(rows) != 1 {
		t.Errorf("filtered by its label and number: %+v", rows)
	}
	if _, rows, _, _ = RunReport(tenantID, "post-sale-edit-history", "HR/Admin", "admin", map[string]string{"document_type": "Sales Order"}); len(rows) != 0 {
		t.Errorf("the document type filter applies: %+v", rows)
	}
}
