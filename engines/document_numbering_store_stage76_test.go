package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 76.8: a generated number comes from the document's own store (a PO's
// billing store, a transfer's from-store), not the maker's session store, and
// a value that is not a store never reaches a number.
func TestStage768NumberFromDocumentStore(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const store = "T768S"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".sequence_counters WHERE store_code = $1", store)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Location' AND id = $1", store)
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'admin')",
		store, `{"code":"T768S","name":"Stage 76.8 test store"}`); err != nil {
		t.Fatalf("scratch store: %v", err)
	}

	if got := DocumentStoreCode(tenantID, "", "NOT-A-STORE-768", store); got != store {
		t.Fatalf("DocumentStoreCode: want %s, got %q", store, got)
	}
	if got := DocumentStoreCode(tenantID, "NOT-A-STORE-768"); got != "" {
		t.Fatalf("DocumentStoreCode for a non-store: want empty, got %q", got)
	}

	po := map[string]interface{}{"vendor": "V1", "location": store}
	if err := PrepareDocumentNumber(tenantID, "HQ", "PurchaseOrder", true, po); err != nil {
		t.Fatalf("PO: %v", err)
	}
	if id, _ := po["id"].(string); !strings.Contains(id, "/"+store+"/") {
		t.Fatalf("a PO billed to %s raised from an HQ session: want %s in the number, got %q", store, store, id)
	}

	to := map[string]interface{}{"from_warehouse": store, "to_warehouse": "HQ"}
	if err := PrepareDocumentNumber(tenantID, "HQ", "TransferOrder", true, to); err != nil {
		t.Fatalf("TO: %v", err)
	}
	if id, _ := to["transfer_number"].(string); !strings.Contains(id, "/"+store+"/") {
		t.Fatalf("a transfer from %s: want %s in the number, got %q", store, store, id)
	}

	stray := map[string]interface{}{"vendor": "V1", "location": "NOT-A-STORE-768"}
	if err := PrepareDocumentNumber(tenantID, store, "PurchaseOrder", true, stray); err != nil {
		t.Fatalf("PO with a non-store location: %v", err)
	}
	if id, _ := stray["id"].(string); strings.Contains(id, "NOT-A-STORE") || !strings.Contains(id, "/"+store+"/") {
		t.Fatalf("a location that is not a store must fall back to the session store %s, got %q", store, id)
	}

	none := map[string]interface{}{"vendor": "V1"}
	if err := PrepareDocumentNumber(tenantID, store, "PurchaseOrder", true, none); err != nil {
		t.Fatalf("PO with no store: %v", err)
	}
	if id, _ := none["id"].(string); !strings.Contains(id, "/"+store+"/") {
		t.Fatalf("a PO naming no store keeps the session store %s, got %q", store, id)
	}
}
