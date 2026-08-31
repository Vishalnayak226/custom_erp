package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

func TestBulkUpdateDocumentsIsAtomicAndResetsApproval(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}

	const itemID = "PIM-BULK-ITEM"
	const firstID = "PIM-BULK-ITEM::en"
	const secondID = "PIM-BULK-ITEM::hi"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".approval_log WHERE document_id IN ($1, $2)", firstID, secondID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2, $3)", itemID, firstID, secondID)
	}
	cleanup()
	defer cleanup()

	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		encoded, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("marshal %s: %v", id, err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, encoded, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert(itemID, "Item", "Active", map[string]interface{}{"code": itemID, "name": "Bulk edit fixture"})
	insert(firstID, "ProductContent", "Approved", map[string]interface{}{
		"code": firstID, "product_id": itemID, "language": "en", "title": "Original EN", "status": "Approved",
	})
	insert(secondID, "ProductContent", "Approved", map[string]interface{}{
		"code": secondID, "product_id": itemID, "language": "hi", "title": "Original HI", "status": "Approved",
	})

	updated, err := BulkUpdateDocuments("default", "ProductContent", []string{secondID, firstID}, "title", "Bulk title", "system", "HR/Admin")
	if err != nil {
		t.Fatalf("bulk update approved content: %v", err)
	}
	if len(updated) != 2 || updated[0] != firstID || updated[1] != secondID {
		t.Fatalf("updated ids = %#v, want sorted fixture ids", updated)
	}

	for _, id := range []string{firstID, secondID} {
		var raw, status string
		if err := db.DB.QueryRow("SELECT data, status FROM "+schema+".documents WHERE id = $1", id).Scan(&raw, &status); err != nil {
			t.Fatalf("read updated %s: %v", id, err)
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &data)
		if data["title"] != "Bulk title" || data["status"] != "Pending Approval" || status != "Pending Approval" {
			t.Errorf("%s did not receive title + reapproval reset: data=%#v status=%s", id, data, status)
		}
	}
	var resetCount int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".approval_log WHERE document_id IN ($1, $2) AND action = 'Modified'", firstID, secondID).Scan(&resetCount); err != nil {
		t.Fatalf("count approval resets: %v", err)
	}
	if resetCount != 2 {
		t.Errorf("approval reset audit rows = %d, want 2", resetCount)
	}

	// A validation failure occurs before any write. The empty mandatory language
	// must leave both previously-updated titles intact rather than partially
	// applying one row before rejecting the next.
	if _, err := BulkUpdateDocuments("default", "ProductContent", []string{firstID, secondID}, "language", "", "system", "HR/Admin"); err == nil {
		t.Fatal("expected mandatory-field rejection from bulk edit")
	}
	var title string
	if err := db.DB.QueryRow("SELECT data->>'title' FROM "+schema+".documents WHERE id = $1", firstID).Scan(&title); err != nil {
		t.Fatalf("read title after rejected batch: %v", err)
	}
	if title != "Bulk title" {
		t.Errorf("rejected batch partially changed document: title=%q", title)
	}
	if _, err := BulkUpdateDocuments("default", "ProductContent", []string{firstID}, "code", "new-code", "system", "HR/Admin"); err == nil {
		t.Fatal("expected immutable code field to be rejected")
	}
}

// TestBulkUpdateDocumentsRejectsRestrictedFieldForRole (Stage 36.7.6): the
// generic single-document update path has always refused a field a role's
// field_permissions row marks not-writable; this bulk path edits the same
// documents through a different door and, before this stage, had never been
// taught the same restriction - a Cashier blocked from editing Item GST
// rate one screen at a time could still change it for a hundred items at
// once via Group Actions bulk edit. Exercises db/migrations_stage16_field_
// permissions.sql's own real demo policy (Cashier cannot write Item.gst_
// rate) rather than a synthetic fixture, and proves the guard is
// role-scoped, not a blanket block, by having HR/Admin succeed on the exact
// same field right after. (The same policy also names Item.cost_price, but
// that field is never declared in doctype_fields for this doctype - a
// pre-existing, unrelated gap in the demo policy itself, not exercised
// here.)
func TestBulkUpdateDocumentsRejectsRestrictedFieldForRole(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	const itemID = "PIM-BULK-PERM-ITEM"
	cleanup := func() { _, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", itemID) }
	cleanup()
	defer cleanup()

	encoded, _ := json.Marshal(map[string]interface{}{
		"code": itemID, "name": "Bulk permission fixture", "barcode": "8901234500019",
		"hsn_code": "1006", "gst_rate": 5,
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", itemID, encoded); err != nil {
		t.Fatalf("insert fixture item: %v", err)
	}

	if _, err := BulkUpdateDocuments("default", "Item", []string{itemID}, "gst_rate", 12, "system", "Cashier"); err == nil {
		t.Fatal("expected Cashier's bulk edit of Item.gst_rate to be refused")
	} else if !strings.Contains(err.Error(), "gst_rate") {
		t.Errorf("refusal should name the restricted field, got: %v", err)
	}
	var gstRate float64
	if err := db.DB.QueryRow("SELECT COALESCE((data->>'gst_rate')::numeric, 0) FROM "+schema+".documents WHERE id = $1", itemID).Scan(&gstRate); err != nil {
		t.Fatalf("read gst_rate after refused edit: %v", err)
	}
	if gstRate != 5 {
		t.Errorf("gst_rate = %v after refused Cashier edit, want unchanged 5", gstRate)
	}

	if _, err := BulkUpdateDocuments("default", "Item", []string{itemID}, "gst_rate", 12, "system", "HR/Admin"); err != nil {
		t.Fatalf("HR/Admin (no field restriction configured) should be able to bulk edit gst_rate: %v", err)
	}
}

// TestBulkImportCSVRejectsRestrictedFieldForRole (Stage 36.7.6): the same
// gap as above, on the CSV import path - importBatch now runs every row's
// field set through RejectRestrictedFieldWrites before ValidateDocument, so
// a role blocked from writing a field cannot push it through in bulk via
// upload either. A scratch field_permissions row scopes this to a role/
// field/doctype combination not used by any other test, cleaned up after.
func TestBulkImportCSVRejectsRestrictedFieldForRole(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	const itemID = "PIM-IMPORT-PERM-ITEM"
	const contentID = "PIM-IMPORT-PERM-CONTENT"
	const role = "PIMImportPermTestRole"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2)", itemID, contentID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".field_permissions WHERE role = $1", role)
	}
	cleanup()
	defer cleanup()

	itemEncoded, _ := json.Marshal(map[string]interface{}{
		"code": itemID, "name": "Import permission fixture", "barcode": "8901234500026",
		"hsn_code": "1006", "gst_rate": 5,
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", itemID, itemEncoded); err != nil {
		t.Fatalf("insert fixture item: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".field_permissions (role, doctype_name, fieldname, allow_read, allow_write) VALUES ($1, 'ProductContent', 'title', TRUE, FALSE)", role); err != nil {
		t.Fatalf("insert scratch field_permissions row: %v", err)
	}

	csv := "code,product_id,language,title\n" + contentID + "," + itemID + ",en,Imported Title\n"
	res, err := BulkImportCSV("default", "ProductContent", strings.NewReader(csv), "system", role, false)
	if err != nil {
		t.Fatalf("BulkImportCSV: %v", err)
	}
	if res.SuccessRows != 0 || res.FailedRows != 1 {
		t.Fatalf("expected 0 success / 1 failure, got %d/%d (%v)", res.SuccessRows, res.FailedRows, res.Errors)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Message, "title") {
		t.Errorf("row error should name the restricted field, got: %#v", res.Errors)
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE id = '" + contentID + "'").Scan(&count); err != nil {
		t.Fatalf("count content rows: %v", err)
	}
	if count != 0 {
		t.Error("a restricted-field row should not have been written")
	}

	// A role with no restriction on this field imports the identical row
	// successfully, proving the block is role-scoped rather than a change
	// in ProductContent's own validation.
	res2, err := BulkImportCSV("default", "ProductContent", strings.NewReader(csv), "system", "HR/Admin", false)
	if err != nil {
		t.Fatalf("BulkImportCSV (unrestricted role): %v", err)
	}
	if res2.SuccessRows != 1 || res2.FailedRows != 0 {
		t.Fatalf("expected 1 success / 0 failures for unrestricted role, got %d/%d (%v)", res2.SuccessRows, res2.FailedRows, res2.Errors)
	}
}
