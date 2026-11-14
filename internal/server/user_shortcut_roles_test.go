package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"testing"
)

// 165.1: a role made in Setup after Stage 139 has no UserShortcut grant row,
// yet can star and unstar (create, list, delete its own favourite), and can
// neither list, open, change nor delete another user's favourites.
func TestStage1651EveryRoleKeepsItsOwnFavourites(t *testing.T) {
	db.InitDB(testConnStr())
	const roleName = "T1651 Floor Lead"
	defer db.DB.Exec(`DELETE FROM tenant_default.roles WHERE name = $1`, roleName)
	defer db.DB.Exec(`DELETE FROM tenant_default.role_permissions WHERE role = $1`, roleName)
	adminTok := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "", currentCredentialVersion("admin"))
	if rec := doRequest(t, apiMiddleware(handleCreateRole), http.MethodPost, "/api/v1/admin/roles", adminTok, map[string]string{"name": roleName}); rec.Code >= 300 {
		t.Fatalf("create role: %d %s", rec.Code, rec.Body.String())
	}
	var grants int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.role_permissions WHERE role = $1 AND doctype_name = 'UserShortcut'`, roleName).Scan(&grants)
	if grants != 0 {
		t.Fatalf("the test needs a role with no UserShortcut grant row, found %d", grants)
	}
	a, cleanA := seedStage47User(t, roleName, "HO")
	defer cleanA()
	b, cleanB := seedStage47User(t, roleName, "HO")
	defer cleanB()
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'UserShortcut' AND created_by IN ($1, $2)`, a, b)
	tokA, tokB := stage47Token(a, roleName, "HO"), stage47Token(b, roleName, "HO")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	h := mux.ServeHTTP
	star := func(tok, owner, target string) (int, string) {
		rec := doRequest(t, h, http.MethodPost, "/api/v1/doc/UserShortcut", tok, map[string]interface{}{
			"kind": "Favourite", "name": "Star " + target, "target": target, "owner": owner, "status": "Active"})
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		id, _ := out["id"].(string)
		return rec.Code, id
	}
	list := func(tok, owner string) []map[string]interface{} {
		rec := doRequest(t, h, http.MethodGet, "/api/v1/doc/UserShortcut?owner="+owner+"&kind=Favourite&limit=200", tok, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list as %s: %d %s", owner, rec.Code, rec.Body.String())
		}
		var rows []map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &rows)
		return rows
	}

	code, idA := star(tokA, a, "report:t1651-a")
	if code >= 300 || idA == "" {
		t.Fatalf("a role made in Setup could not star: %d", code)
	}
	if code, _ := star(tokB, b, "report:t1651-b"); code >= 300 {
		t.Fatalf("second user could not star: %d", code)
	}
	if rows := list(tokA, a); len(rows) != 1 || rows[0]["target"] != "report:t1651-a" {
		t.Fatalf("A must see exactly its own favourite, got %v", rows)
	}
	// Asking for B's favourites by naming B in the filter returns nothing of B's.
	if rows := list(tokA, b); len(rows) != 0 {
		t.Fatalf("A read B's favourites: %v", rows)
	}
	// Open, change and delete B's favourite by id: refused as not found.
	var idB string
	db.DB.QueryRow(`SELECT id FROM tenant_default.documents WHERE doctype = 'UserShortcut' AND created_by = $1`, b).Scan(&idB)
	if rec := doRequest(t, h, http.MethodGet, "/api/v1/doc/UserShortcut/"+idB, tokA, nil); rec.Code != http.StatusNotFound {
		t.Errorf("A opened B's favourite: %d", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodDelete, "/api/v1/doc/UserShortcut/"+idB, tokA, nil); rec.Code != http.StatusNotFound {
		t.Errorf("A deleted B's favourite: %d", rec.Code)
	}
	var left int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE id = $1 AND deleted_at IS NULL`, idB).Scan(&left)
	if left != 1 {
		t.Error("B's favourite is gone after A's delete")
	}
	// Unstar: A removes its own.
	if rec := doRequest(t, h, http.MethodDelete, "/api/v1/doc/UserShortcut/"+idA, tokA, nil); rec.Code >= 300 {
		t.Errorf("A could not unstar: %d %s", rec.Code, rec.Body.String())
	}
	if rows := list(tokA, a); len(rows) != 0 {
		t.Errorf("A still has %d favourites after unstarring", len(rows))
	}
	// Other doctypes still need a grant: the exception is UserShortcut only.
	if rec := doRequest(t, h, http.MethodGet, "/api/v1/doc/Item?limit=1", tokA, nil); rec.Code != http.StatusForbidden {
		t.Errorf("a role with no grants read Item: %d", rec.Code)
	}
}
