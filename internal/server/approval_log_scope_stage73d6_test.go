package server

import (
	"custom_erp/db"
	"net/http"
	"testing"
)

// Stage 73.D.6: GET /api/v1/approval/log answers only to someone who may read
// the document itself - the same read rule as opening it.
func TestStage73D6ApprovalLogFollowsTheDocumentReadRule(t *testing.T) {
	db.InitDB(testConnStr())
	const po = "T73D6-PO"
	db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, po)
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, po)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'PurchaseOrder', '{"po_number":"T73D6-PO","location":"HO","vendor":"T73D6-V"}', 'Pending Approval', 'admin')`, po); err != nil {
		t.Fatalf("seed PO: %v", err)
	}

	sameStore, cleanupA := seedStage47User(t, "Store Manager", "HO")
	defer cleanupA()
	otherStore, cleanupB := seedStage47User(t, "Store Manager", "T73D6-JAY")
	defer cleanupB()
	noRead, cleanupC := seedStage47User(t, "Employee Self-Service", "HO")
	defer cleanupC()

	handler := apiMiddleware(handleApprovalLog)
	path := "/api/v1/approval/log?doctype=PurchaseOrder&document_id=" + po
	cases := []struct {
		name, user, role, location string
		want                       int
	}{
		{"store manager at the PO's store", sameStore, "Store Manager", "HO", http.StatusOK},
		{"store manager at another store", otherStore, "Store Manager", "T73D6-JAY", http.StatusForbidden},
		{"a role that cannot read POs", noRead, "Employee Self-Service", "HO", http.StatusForbidden},
	}
	for _, c := range cases {
		rec := doRequest(t, handler, http.MethodGet, path, stage47Token(c.user, c.role, c.location), nil)
		if rec.Code != c.want {
			t.Errorf("%s: status %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body.String())
		}
	}
	missing := doRequest(t, handler, http.MethodGet, "/api/v1/approval/log?doctype=PurchaseOrder&document_id=T73D6-NOPE", stage47Token(sameStore, "Store Manager", "HO"), nil)
	if missing.Code != http.StatusForbidden {
		t.Errorf("a missing document: status %d, want %d (no hint which ids exist)", missing.Code, http.StatusForbidden)
	}
}
