package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func TestBulkUpdateDocumentsIsAtomicAndResetsApproval(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
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
