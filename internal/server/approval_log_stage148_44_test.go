package server

import (
	"custom_erp/db"
	"net/http"
	"testing"
)

// Stage 148.44: the approval history of a document that has its own role row
// (or none) answers a role with full access, whether the document exists yet
// or not; a role without read stays refused.
func TestStage14844ApprovalLogForFullAccessRole(t *testing.T) {
	db.InitDB(testConnStr())
	const id = "T14844-ITEM::en"
	cleanup := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
		db.DB.Exec(`DELETE FROM tenant_default.approval_log WHERE document_id = $1`, id)
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'ProductContent', '{"product_id":"T14844-ITEM","language":"en","title":"T14844"}', 'Draft', 'admin')`, id); err != nil {
		t.Fatalf("seed content: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.approval_log (doctype, document_id, action, actor_user_id, actor_role)
		VALUES ('ProductContent', $1, 'Submitted', 'admin', 'Super Admin')`, id); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	sa, c1 := seedStage47User(t, "Super Admin", "HO")
	defer c1()
	noRead, c2 := seedStage47User(t, "Employee Self-Service", "HO")
	defer c2()
	handler := apiMiddleware(handleApprovalLog)
	q := "/api/v1/approval/log?doctype=ProductContent&document_id="
	for _, c := range []struct {
		name, path, tok string
		want            int
	}{
		{"super admin, content exists", q + "T14844-ITEM%3A%3Aen", stage47Token(sa, "Super Admin", "HO"), http.StatusOK},
		{"super admin, no content for the language yet", q + "T14844-ITEM%3A%3Afr", stage47Token(sa, "Super Admin", "HO"), http.StatusOK},
		{"role without read", q + "T14844-ITEM%3A%3Aen", stage47Token(noRead, "Employee Self-Service", "HO"), http.StatusForbidden},
	} {
		rec := doRequest(t, handler, http.MethodGet, c.path, c.tok, nil)
		if rec.Code != c.want {
			t.Errorf("%s: status %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body.String())
		}
	}
}
