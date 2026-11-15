package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// 197.3: the Pick worklist follows the store picked in the top bar, as Pack and
// Ship do (Stage 106.6) - a ?location_code= cannot widen it - and shows every
// store, or the one asked for, under "All stores".
func TestPickQueueFollowsTopBarStore(t *testing.T) {
	db.InitDB(testConnStr())
	cleanup := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype IN ('FulfillmentTask', 'SalesOrder') AND id LIKE 'T1973-%'`)
	}
	cleanup()
	defer cleanup()
	// Expedite puts these tasks at the head of the queue (capped, oldest first) on a busy test database.
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ('T1973-ORDER', 'SalesOrder', '{"priority":"Expedite"}', 'Reserved', 'admin')`); err != nil {
		t.Fatalf("order: %v", err)
	}
	for _, s := range []struct{ id, loc string }{{"T1973-TASK-A", "T1973-STORE-A"}, {"T1973-TASK-B", "T1973-STORE-B"}} {
		data := `{"order_id":"T1973-ORDER","location_code":"` + s.loc + `","items":"[]"}`
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'FulfillmentTask', $2, 'Pending', 'admin')`, s.id, data); err != nil {
			t.Fatalf("task: %v", err)
		}
	}
	queue := func(storeView, asked string) []string {
		r := httptest.NewRequest("GET", "/api/v1/oms/pick-queue?location_code="+asked, nil)
		r.Header.Set("Resolved-Tenant-ID", "default")
		r.Header.Set("Resolved-Role", engines.RoleStoreManager)
		r.Header.Set("Resolved-Location", "T1973-STORE-A")
		r.Header.Set("Resolved-Store-View", storeView)
		w := httptest.NewRecorder()
		handlePickQueue(w, r)
		var out struct {
			Tasks []map[string]interface{} `json:"tasks"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
			t.Fatalf("pick-queue %q: %d %s", storeView, w.Code, w.Body.String())
		}
		ids := []string{}
		for _, task := range out.Tasks {
			if id, _ := task["task_id"].(string); len(id) > 6 && id[:6] == "T1973-" {
				ids = append(ids, id)
			}
		}
		return ids
	}
	if ids := queue(engines.StoreViewStore, "T1973-STORE-B"); len(ids) != 1 || ids[0] != "T1973-TASK-A" {
		t.Errorf("following the top bar: %v, want only T1973-TASK-A", ids)
	}
	if ids := queue("", ""); len(ids) != 2 {
		t.Errorf("All stores: %v, want both tasks", ids)
	}
	if ids := queue("", "T1973-STORE-B"); len(ids) != 1 || ids[0] != "T1973-TASK-B" {
		t.Errorf("All stores asking for B: %v, want only T1973-TASK-B", ids)
	}
}
