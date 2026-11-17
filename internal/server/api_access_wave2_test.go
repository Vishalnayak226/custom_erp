package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// 205.14-205.22: actions, reports, webhooks, docs, share link, attention, fix.

func TestPublicActionsTable(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range publicActions {
		id := a.Module + "/" + a.Action
		if seen[id] {
			t.Errorf("action %s is listed twice", id)
		}
		seen[id] = true
		if a.Level != "read" && a.Level != "write" {
			t.Errorf("action %s has level %q", id, a.Level)
		}
		if a.Summary == "" || a.Body == "" || a.Handler == nil {
			t.Errorf("action %s lacks a summary, body note or handler", id)
		}
		if _, _, found := checkRouteCapability(a.Pattern, engines.RoleSuperAdmin); !found {
			t.Errorf("action %s: pattern %q has no entry in route_capabilities.go", id, a.Pattern)
		}
		switch a.Module {
		case "finance", "pos":
			t.Errorf("action %s: money and period actions stay off the list", id)
		}
	}
}

func TestAPIAccessWave2(t *testing.T) {
	f := newAPIAccessFixture(t)
	create := func(body map[string]interface{}) (id, key string, raw string) {
		rec := f.do(http.MethodPost, "/api/v1/api-access/keys", f.adminToken, body, nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create key: %d %s", rec.Code, rec.Body.String())
		}
		var c struct {
			Credential struct {
				ID string `json:"id"`
			} `json:"credential"`
			APIKey string `json:"api_key"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &c)
		t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id = $1`, c.Credential.ID) })
		return c.Credential.ID, c.APIKey, rec.Body.String()
	}
	id, key, raw := create(map[string]interface{}{"name": "t205w2 key", "modules": map[string]string{f.module: "write"}, "expires": "30d"})
	if !strings.Contains(raw, `"base_url"`) || !strings.Contains(raw, `"tenant_code"`) {
		t.Errorf("create answer lacks base_url or tenant_code: %s", raw)
	}
	keyPath := "/api/v1/api-access/keys/" + id

	t.Run("get key returns limits and the limits route works for the holder", func(t *testing.T) {
		rec := f.do(http.MethodGet, keyPath, f.adminToken, nil, nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"limits"`) {
			t.Fatalf("get key: %d %s", rec.Code, rec.Body.String())
		}
		rec = f.do(http.MethodPut, keyPath+"/limits", f.adminToken, map[string]int{"rate_limit_per_minute": 30, "daily_quota": 500}, nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"rate_limit_per_minute":30`) {
			t.Fatalf("set limits: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("the key spec is 3.1, complete for its modules, and every operation names its errors", func(t *testing.T) {
		rec := f.do(http.MethodGet, keyPath+"/openapi.json", f.adminToken, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("spec: %d %s", rec.Code, rec.Body.String())
		}
		var spec map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
			t.Fatal(err)
		}
		if spec["openapi"] != "3.1.0" {
			t.Errorf("openapi = %v", spec["openapi"])
		}
		servers, _ := spec["servers"].([]interface{})
		if len(servers) == 0 || servers[0].(map[string]interface{})["url"] == "" {
			t.Errorf("servers[0].url missing")
		}
		paths := spec["paths"].(map[string]interface{})
		for _, route := range publicAPIV1Routes() {
			if route.ModuleKey == "core" || route.ModuleKey == "reports" {
				if _, ok := paths[route.Path]; !ok {
					t.Errorf("route %s %s is not in the spec", route.Method, route.Path)
				}
			}
		}
		comps := spec["components"].(map[string]interface{})["schemas"].(map[string]interface{})
		mods, _ := engines.APIAccessModules("default", engines.RoleSuperAdmin)
		for _, m := range mods {
			if m.Key != f.module || !m.InPlan {
				continue
			}
			for _, d := range m.Doctypes {
				if _, ok := comps["Record_"+d.Doctype]; !ok {
					t.Errorf("document type %s of %s is not documented", d.Doctype, m.Key)
				}
			}
		}
		for p, item := range paths {
			for method, op := range item.(map[string]interface{}) {
				if _, ok := op.(map[string]interface{})["x-errors"]; !ok {
					t.Errorf("%s %s has no x-errors", method, p)
				}
			}
		}
		// A module the key does not hold is not in its spec.
		if _, ok := comps["Record_"+f.other]; ok {
			t.Errorf("%s is documented though the key does not hold its module", f.other)
		}
		if rec := f.do(http.MethodGet, keyPath+"/postman.json", f.adminToken, nil, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "collection/v2.1.0") {
			t.Errorf("postman: %d", rec.Code)
		}
		if rec := f.do(http.MethodGet, "/api/v1/api-access/openapi.json", f.adminToken, nil, nil); rec.Code != http.StatusOK {
			t.Errorf("company spec: %d", rec.Code)
		}
	})

	t.Run("share link works until withdrawn, and every bad token looks the same", func(t *testing.T) {
		rec := f.do(http.MethodPost, keyPath+"/share-docs", f.adminToken, map[string]int{"days": 7}, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("share: %d %s", rec.Code, rec.Body.String())
		}
		var out struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		token := strings.TrimPrefix(out.URL, "/api-docs.html?t=")
		if token == out.URL || token == "" {
			t.Fatalf("share url = %q", out.URL)
		}
		docs := f.do(http.MethodGet, "/api/public/v1/docs/"+token, "", nil, nil)
		if docs.Code != http.StatusOK || !strings.Contains(docs.Body.String(), `"x-tenant-code"`) {
			t.Fatalf("docs: %d %s", docs.Code, docs.Body.String()[:min(200, docs.Body.Len())])
		}
		if strings.Contains(docs.Body.String(), key) || strings.Contains(docs.Body.String(), "whsec_") {
			t.Fatal("the shared docs carry a secret")
		}
		bad := f.do(http.MethodGet, "/api/public/v1/docs/default.deadbeef", "", nil, nil)
		if f.do(http.MethodDelete, keyPath+"/share-docs", f.adminToken, nil, nil).Code != http.StatusOK {
			t.Fatal("withdraw failed")
		}
		gone := f.do(http.MethodGet, "/api/public/v1/docs/"+token, "", nil, nil)
		if gone.Code != http.StatusNotFound || bad.Code != http.StatusNotFound || gone.Body.String() != bad.Body.String() {
			t.Errorf("withdrawn %d %q vs bad %d %q", gone.Code, gone.Body.String(), bad.Code, bad.Body.String())
		}
		// Revoke kills a fresh link too.
		rid, _, _ := create(map[string]interface{}{"name": "t205w2 revoke", "modules": map[string]string{f.module: "read"}, "expires": "30d"})
		rec = f.do(http.MethodPost, "/api/v1/api-access/keys/"+rid+"/share-docs", f.adminToken, map[string]int{"days": 1}, nil)
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		tok2 := strings.TrimPrefix(out.URL, "/api-docs.html?t=")
		if f.do(http.MethodGet, "/api/public/v1/docs/"+tok2, "", nil, nil).Code != http.StatusOK {
			t.Fatal("fresh link should work")
		}
		f.do(http.MethodDelete, "/api/v1/api-access/keys/"+rid, f.adminToken, nil, nil)
		if r := f.do(http.MethodGet, "/api/public/v1/docs/"+tok2, "", nil, nil); r.Code != http.StatusNotFound {
			t.Errorf("link after revoke: %d", r.Code)
		}
	})

	t.Run("webhooks: subscribe, list without secret, bad address refused, remove", func(t *testing.T) {
		rec := f.do(http.MethodPost, keyPath+"/webhooks", f.adminToken, map[string]interface{}{"url": "http://127.0.0.1/hook"}, nil)
		if rec.Code < 400 {
			t.Fatalf("a private address must be refused: %d %s", rec.Code, rec.Body.String())
		}
		rec = f.do(http.MethodPost, keyPath+"/webhooks", f.adminToken, map[string]interface{}{"url": "https://example.com/hook", "events": []string{f.module + ".PurchaseOrder.submitted"}}, nil)
		if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "whsec_") {
			t.Fatalf("subscribe: %d %s", rec.Code, rec.Body.String())
		}
		var hook struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &hook)
		rec = f.do(http.MethodPost, keyPath+"/webhooks", f.adminToken, map[string]interface{}{"url": "https://example.com/x", "events": []string{"nomodule.Nothing.created"}}, nil)
		if rec.Code < 400 {
			t.Errorf("an event of a module the key lacks must be refused: %d", rec.Code)
		}
		rec = f.do(http.MethodGet, keyPath+"/webhooks", f.adminToken, nil, nil)
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "whsec_") || !strings.Contains(rec.Body.String(), hook.ID) {
			t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
		}
		if rec = f.do(http.MethodGet, keyPath+"/webhooks/deliveries", f.adminToken, nil, nil); rec.Code != http.StatusOK {
			t.Errorf("deliveries: %d %s", rec.Code, rec.Body.String())
		}
		if rec = f.do(http.MethodDelete, keyPath+"/webhooks/"+hook.ID, f.adminToken, nil, nil); rec.Code != http.StatusOK {
			t.Errorf("delete: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("attention is an array and fix refuses what the asker does not hold", func(t *testing.T) {
		rec := f.do(http.MethodGet, "/api/v1/api-access/attention", f.adminToken, nil, nil)
		if rec.Code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(rec.Body.String()), "[") {
			t.Fatalf("attention: %d %s", rec.Code, rec.Body.String())
		}
		if rec = f.do(http.MethodPost, keyPath+"/fix", f.adminToken, map[string]string{"module": "no_such_module", "level": "read"}, nil); rec.Code < 400 {
			t.Errorf("fix of an unknown module: %d", rec.Code)
		}
		if rec = f.do(http.MethodPost, keyPath+"/fix", f.adminToken, map[string]string{"module": f.module, "level": "bogus"}, nil); rec.Code < 400 {
			t.Errorf("fix with a bad level: %d", rec.Code)
		}
	})

	t.Run("actions and reports answer with their own codes", func(t *testing.T) {
		if rec := f.do(http.MethodGet, "/api/public/v1/actions", key, nil, nil); rec.Code != http.StatusOK {
			t.Errorf("actions list: %d %s", rec.Code, rec.Body.String())
		}
		rec := f.do(http.MethodPost, "/api/public/v1/actions/oms/no-such-action", key, map[string]string{}, map[string]string{"Idempotency-Key": "t205w2-a1"})
		if rec.Code != http.StatusNotFound || apiAccessErrCode(rec) != engines.APIKeyActionUnknown {
			t.Errorf("unknown action: %d %s", rec.Code, rec.Body.String())
		}
		if rec := f.do(http.MethodGet, "/api/public/v1/reports", key, nil, nil); rec.Code != http.StatusOK {
			t.Errorf("reports list: %d %s", rec.Code, rec.Body.String())
		}
		rec = f.do(http.MethodGet, "/api/public/v1/reports/no-such-report", key, nil, nil)
		if rec.Code != http.StatusNotFound || apiAccessErrCode(rec) != engines.APIKeyReportUnknown {
			t.Errorf("unknown report: %d %s", rec.Code, rec.Body.String())
		}
		// A listed action in a module the key does not hold is refused, and the
		// refusal carries its fix and docs link.
		if a := publicActions[0]; a.Module != f.module {
			rec = f.do(http.MethodPost, "/api/public/v1/actions/"+a.Module+"/"+a.Action, key, map[string]string{"id": "x"}, map[string]string{"Idempotency-Key": "t205w2-a2"})
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"fix"`) || !strings.Contains(rec.Body.String(), `"docs_url"`) {
				t.Errorf("action outside the key's modules: %d %s", rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("a developer link opens once", func(t *testing.T) {
		rec := f.do(http.MethodPost, "/api/v1/api-access/keys", f.adminToken, map[string]interface{}{
			"name": "t205w2 link", "modules": map[string]string{f.module: "read"}, "expires": "30d", "deliver": "developer_link",
		}, nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("link key: %d %s", rec.Code, rec.Body.String())
		}
		var c struct {
			Credential struct {
				ID string `json:"id"`
			} `json:"credential"`
			Link struct {
				URL string `json:"url"`
			} `json:"developer_link"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &c)
		t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id = $1`, c.Credential.ID) })
		token := strings.TrimPrefix(c.Link.URL, "/api-claim.html?t=")
		if token == c.Link.URL || token == "" {
			t.Fatalf("developer link url = %q", c.Link.URL)
		}
		first := f.do(http.MethodPost, "/api/public/v1/developer-links/"+token+"/claim", "", map[string]string{}, nil)
		if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"api_key"`) || !strings.Contains(first.Body.String(), `"tenant_code"`) {
			t.Fatalf("first claim: %d %s", first.Code, first.Body.String())
		}
		second := f.do(http.MethodPost, "/api/public/v1/developer-links/"+token+"/claim", "", map[string]string{}, nil)
		if second.Code != http.StatusNotFound {
			t.Errorf("second claim: %d %s", second.Code, second.Body.String())
		}
	})

	t.Run("a role without api.share cannot reach the new routes", func(t *testing.T) {
		cid, cleanup := seedStage47User(t, "Cashier", "HO")
		t.Cleanup(cleanup)
		tok := stage47Token(cid, "Cashier", "HO")
		for _, p := range []string{"/api/v1/api-access/attention", keyPath + "/webhooks", keyPath + "/openapi.json", keyPath + "/share-docs"} {
			if rec := f.do(http.MethodGet, p, tok, nil, nil); rec.Code != http.StatusForbidden {
				t.Errorf("cashier on %s: %d", p, rec.Code)
			}
		}
	})
}
