package server

import (
	"custom_erp/db"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDataHealerRoutesFollowTheDataHealRights (ask 207): every signed-in
// user reads the heal list (the strip needs it) but only DataHeal update may
// run one; DataHeal read sees the records; extension tokens are refused
// everywhere. Only refusals and reads, so the shared tenant is not healed.
func TestDataHealerRoutesFollowTheDataHealRights(t *testing.T) {
	db.InitDB(testConnStr())
	call := func(handler http.HandlerFunc, method, path, role, purpose, id, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Resolved-Tenant-ID", "default")
		r.Header.Set("Resolved-Role", role)
		r.Header.Set("Resolved-Username", "heal-test")
		if purpose != "" {
			r.Header.Set("Resolved-Purpose", purpose)
		}
		if id != "" {
			r.SetPathValue("id", id)
		}
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}

	for role, wantRun := range map[string]bool{"Cashier": false, "Store Manager": false, "Super Admin": true} {
		w := call(handleListHeals, "GET", "/api/v1/heals", role, "", "", "")
		var out struct {
			Heals  []map[string]interface{} `json:"heals"`
			CanRun bool                     `json:"can_run"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("%s list: %d %s", role, w.Code, w.Body.String())
		}
		if len(out.Heals) == 0 {
			t.Errorf("%s list: no heals", role)
		}
		if out.CanRun != wantRun {
			t.Errorf("%s can_run %v, want %v", role, out.CanRun, wantRun)
		}
	}

	for role, want := range map[string]int{"Cashier": 403, "Store Manager": 200} {
		if w := call(handleHealRecords, "GET", "/api/v1/heals/item-alias/records", role, "", "item-alias", ""); w.Code != want {
			t.Errorf("%s records: %d %s, want %d", role, w.Code, w.Body.String(), want)
		}
	}
	for _, role := range []string{"Cashier", "Store Manager"} {
		if w := call(handleRunHeal, "POST", "/api/v1/heals/item-alias/run", role, "", "item-alias", `{}`); w.Code != 403 {
			t.Errorf("%s run: %d %s, want 403", role, w.Code, w.Body.String())
		}
	}
	if w := call(handleRunHeal, "POST", "/api/v1/heals/no-such-heal/run", "Super Admin", "", "no-such-heal", `{}`); w.Code != 404 || !strings.Contains(w.Body.String(), "MASTER-1703") {
		t.Errorf("unknown heal: %d %s, want 404 MASTER-1703", w.Code, w.Body.String())
	}
	if w := call(handleRunHeal, "POST", "/api/v1/heals/item-alias/run", "Super Admin", "", "item-alias", `{"typed":`); w.Code != 422 {
		t.Errorf("unreadable body: %d %s, want 422", w.Code, w.Body.String())
	}
	for name, h := range map[string]http.HandlerFunc{"list": handleListHeals, "records": handleHealRecords, "run": handleRunHeal} {
		if w := call(h, "POST", "/api/v1/heals/item-alias/run", "Super Admin", "extension", "item-alias", `{}`); w.Code != 403 {
			t.Errorf("extension token %s: %d %s, want 403", name, w.Code, w.Body.String())
		}
	}

	// Item 201.4: Past names is Super Admin only. Reads and refusals only.
	for role, want := range map[string]bool{"Store Manager": false, "Super Admin": true} {
		var out struct {
			CanRename bool `json:"can_rename"`
		}
		if w := call(handleListHeals, "GET", "/api/v1/heals", role, "", "", ""); json.Unmarshal(w.Body.Bytes(), &out) != nil || out.CanRename != want {
			t.Errorf("%s can_rename %v, want %v", role, out.CanRename, want)
		}
	}
	form := `{"old":"No Such Item 201-4","new":"Still None 201-4","confirm":"YES"}`
	for _, role := range []string{"Cashier", "Store Manager"} {
		for name, h := range map[string]http.HandlerFunc{"preview": handlePastRenamePreview, "apply": handlePastRenameApply} {
			if w := call(h, "POST", "/api/v1/heals/past-names/"+name, role, "", "", form); w.Code != 403 {
				t.Errorf("%s past-names %s: %d %s, want 403", role, name, w.Code, w.Body.String())
			}
		}
	}
	if w := call(handlePastRenamePreview, "POST", "/api/v1/heals/past-names/preview", "Super Admin", "extension", "", form); w.Code != 403 {
		t.Errorf("extension token past-names: %d %s, want 403", w.Code, w.Body.String())
	}
	if w := call(handlePastRenamePreview, "POST", "/api/v1/heals/past-names/preview", "Super Admin", "", "", form); w.Code != 200 || !strings.Contains(w.Body.String(), `"documents":0`) {
		t.Errorf("Super Admin preview: %d %s, want 200 with nothing found", w.Code, w.Body.String())
	}
	if w := call(handlePastRenamePreview, "POST", "/api/v1/heals/past-names/preview", "Super Admin", "", "", `{"old":"A","new":"A"}`); w.Code != 422 || !strings.Contains(w.Body.String(), "MASTER-1705") {
		t.Errorf("same name: %d %s, want 422 MASTER-1705", w.Code, w.Body.String())
	}
	if w := call(handlePastRenameApply, "POST", "/api/v1/heals/past-names/apply", "Super Admin", "", "", `{"old":"No Such Item 201-4","new":"B"}`); w.Code != 422 || !strings.Contains(w.Body.String(), "MASTER-1707") {
		t.Errorf("apply without YES: %d %s, want 422 MASTER-1707", w.Code, w.Body.String())
	}
}
