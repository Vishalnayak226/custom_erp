package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 127.6: document prints are recorded and listed; a reprint needs its
// reason.
func TestStage127_6DocumentPrintHistory(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if err := RecordDocumentPrint(tenantID, "manager1", DocumentPrint{Txn: "PurchaseOrder", Doctype: "PurchaseOrder", DocID: "PO-1", Number: "PO/HO/0001", Title: "Purchase Order", Action: PrintActionPrint}); err != nil {
		t.Fatalf("record print: %v", err)
	}
	if err := RecordDocumentPrint(tenantID, "manager1", DocumentPrint{Txn: "GRN", Doctype: "GRN", DocID: "GRN-1", Number: "GRN/HO/0007", Action: PrintActionReprint}); err == nil {
		t.Errorf("a reprint without a reason is refused")
	}
	if err := RecordDocumentPrint(tenantID, "cashier1", DocumentPrint{Txn: "GRN", Doctype: "GRN", DocID: "GRN-1", Number: "GRN/HO/0007", Action: PrintActionReprint, ReprintReason: "torn copy"}); err != nil {
		t.Fatalf("record reprint: %v", err)
	}
	if err := RecordDocumentPrint(tenantID, "x", DocumentPrint{Doctype: "GRN", DocID: "GRN-1", Action: "Fax"}); err == nil {
		t.Errorf("an unknown action is refused")
	}
	all, err := ListDocumentPrints(tenantID, DocumentPrintFilter{})
	if err != nil || len(all) != 2 || all[0].Doctype != "GRN" || all[0].ReprintReason != "torn copy" || all[0].PrintedBy != "cashier1" {
		t.Fatalf("history: %v %+v", err, all)
	}
	if rows, _ := ListDocumentPrints(tenantID, DocumentPrintFilter{Doctype: "PurchaseOrder"}); len(rows) != 1 || rows[0].Number != "PO/HO/0001" {
		t.Errorf("filter by doctype: %+v", rows)
	}
	if rows, _ := ListDocumentPrints(tenantID, DocumentPrintFilter{Query: "0007"}); len(rows) != 1 || rows[0].DocID != "GRN-1" {
		t.Errorf("filter by number: %+v", rows)
	}

	// Decided 2026-10-21: only the modules the client has - a module switched
	// off takes its documents out of the Reprint list.
	if err := SetModuleEntitlement(tenantID, "rfq", true, "test"); err != nil {
		t.Fatalf("enable rfq: %v", err)
	}
	if in, err := EnabledModuleDoctypes(tenantID); err != nil || !in["RFQ"] {
		t.Fatalf("rfq on: RFQ listed? %v %v", err, in["RFQ"])
	}
	if err := SetModuleEntitlement(tenantID, "rfq", false, "test"); err != nil {
		t.Fatalf("disable rfq: %v", err)
	}
	if in, _ := EnabledModuleDoctypes(tenantID); in["RFQ"] {
		t.Errorf("rfq off: RFQ must not be listed")
	}
}
