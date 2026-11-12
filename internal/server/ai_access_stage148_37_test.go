package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 148.37 (D9): read-only access for AI assistants, module ai_access.
// A security item, so the refusals come first: another store's stock, a write
// with a read token, a disabled user's token, a revoked token, the module off.

type aiFixture struct {
	t        *testing.T
	handler  http.Handler
	admin    string
	seq      int
	suffix   string
	locA     string
	locB     string
	sku      string
	userA    string // Store Manager at locA
	userAMgr string
	cleanup  []func()
}

func newAIFixture(t *testing.T) *aiFixture {
	t.Helper()
	handler, admin := moduleHTTPFixture(t)
	f := &aiFixture{t: t, handler: handler, admin: admin, suffix: stage47UniqueID()}
	f.locA, f.locB = "H37A"+f.suffix, "H37B"+f.suffix
	f.sku = "H37SKU" + f.suffix
	for _, loc := range []string{f.locA, f.locB} {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.inventory_availability (sku, location_code, on_hand, available) VALUES ($1,$2,$3,$3)`,
			f.sku, loc, map[bool]int{true: 7, false: 40}[loc == f.locA]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', (SELECT id FROM tenant_default.users ORDER BY id LIMIT 1))`,
		"ITEM-"+f.sku, fmt.Sprintf(`{"code":%q,"name":"h37 item","hsn_code":"6109"}`, f.sku)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, "ITEM-"+f.sku)
		db.DB.Exec(`DELETE FROM tenant_default.inventory_availability WHERE sku = $1`, f.sku)
		db.DB.Exec(`DELETE FROM tenant_default.module_entitlements WHERE module_key = 'ai_access'`)
		for _, c := range f.cleanup {
			c()
		}
	})
	return f
}

func (f *aiFixture) moduleOn(on bool) {
	f.t.Helper()
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.module_entitlements (module_key, enabled, granted_by) VALUES ('ai_access', $1, 'test')
		ON CONFLICT (module_key) DO UPDATE SET enabled = EXCLUDED.enabled`, on); err != nil {
		f.t.Fatal(err)
	}
}

func (f *aiFixture) do(method, path, bearer, body string, extra map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	f.seq++
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	if strings.HasPrefix(path, publicAPIPathPrefix) && publicAPIMutating(method) {
		req.Header.Set("Idempotency-Key", fmt.Sprintf("h37-%s-%d", f.suffix, f.seq))
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	req.RemoteAddr = fmt.Sprintf("198.22.%d.%d:4000", f.seq/250, f.seq%250+1)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func (f *aiFixture) user(role, location string) (id, token string) {
	f.t.Helper()
	id, cleanup := seedStage47User(f.t, role, location)
	f.cleanup = append(f.cleanup, cleanup)
	return id, stage47Token(id, role, location)
}

type aiClientIssued struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Credential   struct {
		ID string `json:"id"`
	} `json:"credential"`
}

// issue creates an assistant client acting as userID and returns it with a
// fresh access token.
func (f *aiFixture) issue(userID string, scopes ...string) (aiClientIssued, string) {
	f.t.Helper()
	quoted, _ := json.Marshal(scopes)
	rec := f.do(http.MethodPost, "/api/v1/admin/ai-access/clients", f.admin,
		fmt.Sprintf(`{"name":"h37 %s","acting_user":%q,"scopes":%s}`, f.suffix, userID, quoted), nil)
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("issue assistant client: %d %s", rec.Code, rec.Body.String())
	}
	var client aiClientIssued
	if err := json.Unmarshal(rec.Body.Bytes(), &client); err != nil || client.ClientID == "" {
		f.t.Fatalf("issued body: %s", rec.Body.String())
	}
	f.cleanup = append(f.cleanup, func() {
		db.DB.Exec(`DELETE FROM tenant_default.ai_access_clients WHERE credential_id = $1`, client.Credential.ID)
		db.DB.Exec(`DELETE FROM tenant_default.api_oauth_tokens WHERE credential_id = $1`, client.Credential.ID)
		db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id = $1`, client.Credential.ID)
	})
	return client, f.token(client)
}

func (f *aiFixture) token(client aiClientIssued) string {
	f.t.Helper()
	f.seq++
	form := url.Values{"grant_type": {"client_credentials"}}
	req := httptest.NewRequest(http.MethodPost, engines.OAuthTokenEndpointPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Tenant-ID", "default")
	req.SetBasicAuth(url.QueryEscape(client.ClientID), url.QueryEscape(client.ClientSecret))
	req.RemoteAddr = fmt.Sprintf("198.23.%d.%d:4000", f.seq/250, f.seq%250+1)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	var out engines.OAuthAccessToken
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.AccessToken == "" {
		f.t.Fatalf("token exchange: %d %s", rec.Code, rec.Body.String())
	}
	return out.AccessToken
}

func (f *aiFixture) stockPath(location string) string {
	p := "/api/public/v1/ai/stock?sku=" + url.QueryEscape(f.sku)
	if location != "" {
		p += "&location=" + url.QueryEscape(location)
	}
	return p
}

// The module off: nothing is reachable, nothing is issued, the old public API
// is unchanged.
func TestStage14837ModuleOffMeansOff(t *testing.T) {
	f := newAIFixture(t)
	var present int
	var def bool
	if err := db.DB.QueryRow(`SELECT count(*), COALESCE(bool_or(default_enabled), false) FROM public.modules WHERE module_key = 'ai_access'`).Scan(&present, &def); err != nil || present != 1 || def {
		t.Fatalf("ai_access must be registered and default off: present=%d default=%v err=%v", present, def, err)
	}
	var rows int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.module_entitlements WHERE module_key = 'ai_access'`).Scan(&rows)
	if rows != 0 {
		t.Fatalf("the migration must not write an entitlement row for any client, found %d", rows)
	}
	userID, _ := f.user(engines.RoleStoreManager, f.locA)

	// Off: issuing and listing are refused.
	rec := f.do(http.MethodPost, "/api/v1/admin/ai-access/clients", f.admin, fmt.Sprintf(`{"name":"x","acting_user":%q,"scopes":["ai:read"]}`, userID), nil)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "SAAS-0191") {
		t.Fatalf("issue with the module off: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodGet, "/api/v1/admin/ai-access/clients", f.admin, "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("list with the module off: %d %s", rec.Code, rec.Body.String())
	}

	// On, then off again: a token issued while it was on stops working.
	f.moduleOn(true)
	_, token := f.issue(userID, "ai:read")
	if rec := f.do(http.MethodGet, f.stockPath(f.locA), token, "", nil); rec.Code != 200 {
		t.Fatalf("stock with the module on: %d %s", rec.Code, rec.Body.String())
	}
	f.moduleOn(false)
	for _, p := range []string{f.stockPath(f.locA), "/api/public/v1/ai/orders/NOPE/status", "/api/public/v1/ai/reports"} {
		if rec := f.do(http.MethodGet, p, token, "", nil); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "SAAS-0191") {
			t.Fatalf("%s with the module off: %d %s", p, rec.Code, rec.Body.String())
		}
	}
	// The person's submit route is switched off too.
	if rec := f.do(http.MethodPost, "/api/v1/orders/SO-NOPE/submit-assistant-draft", f.admin, `{}`, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("submit route with the module off: %d", rec.Code)
	}
}

// Another store's stock is not readable, by asking for it or by leaving the
// store out.
func TestStage14837OtherStoreStockRefused(t *testing.T) {
	f := newAIFixture(t)
	f.moduleOn(true)
	userID, _ := f.user(engines.RoleStoreManager, f.locA)
	_, token := f.issue(userID, "ai:read")

	rec := f.do(http.MethodGet, f.stockPath(f.locB), token, "", nil)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "SEC-0301") {
		t.Fatalf("another store's stock by location: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), f.locB) && strings.Contains(rec.Body.String(), `"available_to_sell":40`) {
		t.Fatalf("the refusal leaked the other store's figure: %s", rec.Body.String())
	}
	rec = f.do(http.MethodGet, f.stockPath(""), token, "", nil)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), f.locB) || !strings.Contains(rec.Body.String(), f.locA) {
		t.Fatalf("no location must list only the user's own store: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodGet, f.stockPath(f.locA), token, "", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"available_to_sell":7`) {
		t.Fatalf("own store: %d %s", rec.Code, rec.Body.String())
	}
	// A user whose role sees every store gets every store.
	adminID, _ := f.user(engines.RoleStoreManager, f.locA)
	if _, err := db.DB.Exec(`UPDATE tenant_default.users SET store_access = 'All' WHERE id = $1`, adminID); err != nil {
		t.Fatal(err)
	}
	_, adminTok := f.issue(adminID, "ai:read")
	if rec := f.do(http.MethodGet, f.stockPath(""), adminTok, "", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), f.locB) {
		t.Fatalf("an all-stores user must see both: %d %s", rec.Code, rec.Body.String())
	}
}

// A read token reaches no write route anywhere, and a draft-only token reads
// nothing.
func TestStage14837ReadTokenCannotReachAWriteRoute(t *testing.T) {
	f := newAIFixture(t)
	f.moduleOn(true)
	userID, _ := f.user(engines.RoleStoreManager, f.locA)
	_, readTok := f.issue(userID, "ai:read")
	body := fmt.Sprintf(`{"customer_name":"X","lines":[{"sku":%q,"qty":1}]}`, f.sku)
	var before int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.documents WHERE doctype = 'SalesOrder' AND data->>'made_by_assistant' = 'true'`).Scan(&before)

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/public/v1/ai/sales-order-drafts"},
		{http.MethodPost, "/api/public/v1/notifications/x/delivery-status"},
		{http.MethodPost, "/api/public/v1/orders/x/status"},
		{http.MethodGet, "/api/public/v1/items?limit=1"},
	} {
		rec := f.do(c.method, c.path, readTok, body, nil)
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s with a read token: %d %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
	// Writing methods on the read routes are not there either.
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if rec := f.do(m, f.stockPath(f.locA), readTok, `{}`, nil); rec.Code == 200 || rec.Code == 201 {
			t.Fatalf("%s on a read route answered %d", m, rec.Code)
		}
	}
	// A read token is not a human session: the person's routes refuse it.
	if rec := f.do(http.MethodPost, "/api/v1/orders", readTok, body, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a read token on the app's order route: %d, want 401", rec.Code)
	}
	if rec := f.do(http.MethodPost, "/api/v1/orders/SO-X/submit-assistant-draft", readTok, `{}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a read token on submit: %d, want 401", rec.Code)
	}
	var after int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.documents WHERE doctype = 'SalesOrder' AND data->>'made_by_assistant' = 'true'`).Scan(&after)
	if after != before {
		t.Fatalf("a read token created %d assistant drafts", after-before)
	}

	// Draft-only: cannot read.
	_, draftTok := f.issue(userID, "ai:draft")
	if rec := f.do(http.MethodGet, f.stockPath(f.locA), draftTok, "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("a draft-only token reading stock: %d", rec.Code)
	}
	// Only the two scopes exist for an assistant; anything else is refused at issue.
	rec := f.do(http.MethodPost, "/api/v1/admin/ai-access/clients", f.admin,
		fmt.Sprintf(`{"name":"bad","acting_user":%q,"scopes":["ai:read","orders:write"]}`, userID), nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an assistant client with an ordinary write scope must be refused: %d %s", rec.Code, rec.Body.String())
	}
	// The ordinary credential route cannot make an assistant scope or a user binding.
	rec = f.do(http.MethodPost, "/api/v1/admin/api-credentials", f.admin, `{"name":"bad","scopes":["ai:read"],"auth_mode":"oauth_client"}`, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("the ordinary credential route accepted an ai scope: %d %s", rec.Code, rec.Body.String())
	}
}

func TestStage14837DisabledUserTokenDies(t *testing.T) {
	f := newAIFixture(t)
	f.moduleOn(true)
	userID, _ := f.user(engines.RoleStoreManager, f.locA)
	_, token := f.issue(userID, "ai:read")
	if rec := f.do(http.MethodGet, f.stockPath(f.locA), token, "", nil); rec.Code != 200 {
		t.Fatalf("before disabling: %d", rec.Code)
	}
	// Warm the live-user cache, then disable without invalidating it: the
	// assistant must still be refused on its very next call.
	engines.ResolveLiveUserState("default", userID)
	if _, err := db.DB.Exec(`UPDATE tenant_default.users SET status = 'Inactive' WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	rec := f.do(http.MethodGet, f.stockPath(f.locA), token, "", nil)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "SEC-0300") {
		t.Fatalf("a disabled user's token: %d %s", rec.Code, rec.Body.String())
	}
	// Issuing for a disabled or unknown user is refused too.
	rec = f.do(http.MethodPost, "/api/v1/admin/ai-access/clients", f.admin, fmt.Sprintf(`{"name":"x","acting_user":%q,"scopes":["ai:read"]}`, userID), nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("issue for a disabled user: %d", rec.Code)
	}
	rec = f.do(http.MethodPost, "/api/v1/admin/ai-access/clients", f.admin, `{"name":"x","acting_user":"no-such-user-h37","scopes":["ai:read"]}`, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("issue for an unknown user: %d", rec.Code)
	}
}

func TestStage14837RevokedTokenDies(t *testing.T) {
	f := newAIFixture(t)
	f.moduleOn(true)
	userID, _ := f.user(engines.RoleStoreManager, f.locA)
	client, token := f.issue(userID, "ai:read")
	if rec := f.do(http.MethodGet, f.stockPath(f.locA), token, "", nil); rec.Code != 200 {
		t.Fatalf("before revoking: %d", rec.Code)
	}
	// The list shows it with its user and revokes through the ordinary route.
	rec := f.do(http.MethodGet, "/api/v1/admin/ai-access/clients", f.admin, "", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), client.ClientID) || !strings.Contains(rec.Body.String(), userID) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodDelete, "/api/v1/admin/api-credentials/"+client.Credential.ID, f.admin, "", nil); rec.Code != 200 {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodGet, f.stockPath(f.locA), token, "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked client's token: %d, want 401", rec.Code)
	}
	// A rotated assistant client keeps the user it was tied to (review fix).
	client2, _ := f.issue(userID, "ai:read")
	rot := f.do(http.MethodPost, "/api/v1/admin/api-credentials/"+client2.Credential.ID+"/rotate", f.admin, `{}`, nil)
	if rot.Code != 200 {
		t.Fatalf("rotate: %d %s", rot.Code, rot.Body.String())
	}
	var rotated aiClientIssued
	json.Unmarshal(rot.Body.Bytes(), &rotated)
	f.cleanup = append(f.cleanup, func() {
		db.DB.Exec(`DELETE FROM tenant_default.api_oauth_tokens WHERE credential_id = $1`, rotated.Credential.ID)
		db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id = $1`, rotated.Credential.ID)
	})
	if rec := f.do(http.MethodGet, f.stockPath(f.locA), f.token(rotated), "", nil); rec.Code != 200 {
		t.Fatalf("a rotated assistant must keep acting as its user: %d %s", rec.Code, rec.Body.String())
	}
	var links int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.ai_access_clients WHERE credential_id = $1 AND acting_user = $2`, rotated.Credential.ID, userID).Scan(&links)
	if links != 1 {
		t.Fatalf("the rotated credential has %d links to the user, want 1", links)
	}
	f.cleanup = append(f.cleanup, func() {
		db.DB.Exec(`DELETE FROM tenant_default.ai_access_clients WHERE credential_id = $1`, rotated.Credential.ID)
	})
}

// Orders: status only for orders the user's stores can see; reports with the
// user's own role and store cut.
func TestStage14837OrderStatusAndReportsUseTheUsersRights(t *testing.T) {
	f := newAIFixture(t)
	f.moduleOn(true)
	userID, _ := f.user(engines.RoleStoreManager, f.locA)
	_, token := f.issue(userID, "ai:read")

	mkOrder := func(loc string) string {
		id := engines.NewDocID("SO")
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1,'SalesOrder',$2,'Reserved',(SELECT id FROM tenant_default.users ORDER BY id LIMIT 1))`,
			id, fmt.Sprintf(`{"order_status":"Reserved","customer_name":"h37 cust"}`)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1,'SalesOrderLine',$2,'Reserved',(SELECT id FROM tenant_default.users ORDER BY id LIMIT 1))`,
			id+"-L1", fmt.Sprintf(`{"order_id":%q,"sku":%q,"qty":1,"location_code":%q,"line_status":"Reserved"}`, id, f.sku, loc)); err != nil {
			t.Fatal(err)
		}
		f.cleanup = append(f.cleanup, func() {
			db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id IN ($1,$2)`, id, id+"-L1")
		})
		return id
	}
	mine, theirs := mkOrder(f.locA), mkOrder(f.locB)
	if rec := f.do(http.MethodGet, "/api/public/v1/ai/orders/"+mine+"/status", token, "", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Reserved") {
		t.Fatalf("own store's order: %d %s", rec.Code, rec.Body.String())
	}
	rec := f.do(http.MethodGet, "/api/public/v1/ai/orders/"+theirs+"/status", token, "", nil)
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "Reserved") {
		t.Fatalf("another store's order: %d %s", rec.Code, rec.Body.String())
	}
	// Not-found and not-yours look the same.
	missing := f.do(http.MethodGet, "/api/public/v1/ai/orders/SO-NO-SUCH-H37/status", token, "", nil)
	if missing.Code != rec.Code {
		t.Fatalf("a missing order must look like a hidden one: %d vs %d", missing.Code, rec.Code)
	}

	// A finance report needs a finance role; a Store Manager is refused.
	finance := ""
	for _, d := range engines.ListReportDefinitions() {
		if d.ModuleKey == "finance" {
			finance = d.ID
			break
		}
	}
	if finance == "" {
		t.Fatal("no finance report registered")
	}
	cashierID, _ := f.user(engines.RoleCashier, f.locA)
	_, cashierToken := f.issue(cashierID, "ai:read")
	token = cashierToken
	rec = f.do(http.MethodGet, "/api/public/v1/ai/reports/"+finance, token, "", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a finance report for a non-finance role: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodGet, "/api/public/v1/ai/reports", token, "", nil); rec.Code != 200 || strings.Contains(rec.Body.String(), `"`+finance+`"`) {
		t.Fatalf("the report list must hold only what the role may run: %d", rec.Code)
	}
	if rec := f.do(http.MethodGet, "/api/public/v1/ai/reports/no-such-report", token, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown report: %d", rec.Code)
	}
}

// The one write: a sales order draft. It cannot post, reserve or approve, is
// marked as an assistant's, and a person submits it.
func TestStage14837DraftIsMarkedAndNeverPosts(t *testing.T) {
	f := newAIFixture(t)
	f.moduleOn(true)
	userID, userTok := f.user(engines.RoleStoreManager, f.locA)
	_, token := f.issue(userID, "ai:read", "ai:draft")
	var reservedBefore int
	db.DB.QueryRow(`SELECT reserved FROM tenant_default.inventory_availability WHERE sku = $1 AND location_code = $2`, f.sku, f.locA).Scan(&reservedBefore)

	// Fields the assistant must not be able to set are ignored.
	body := fmt.Sprintf(`{"customer_name":"Asha","shipping_address":"1 Main Rd","lines":[{"sku":%q,"qty":2,"unit_price":150}],
		"order_status":"Reserved","acceptance_status":"Accepted","accepted_by":"boss","made_by_assistant":false,"status":"Released"}`, f.sku)
	rec := f.do(http.MethodPost, "/api/public/v1/ai/sales-order-drafts", token, body, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create draft: %d %s", rec.Code, rec.Body.String())
	}
	var made struct {
		OrderID string `json:"order_id"`
		Status  string `json:"order_status"`
	}
	json.Unmarshal(rec.Body.Bytes(), &made)
	f.cleanup = append(f.cleanup, func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1 OR data->>'order_id' = $1 OR data->>'submitted_from_draft' = $1`, made.OrderID)
	})
	if made.OrderID == "" || made.Status != "Draft" {
		t.Fatalf("draft body: %s", rec.Body.String())
	}
	var status string
	var data string
	db.DB.QueryRow(`SELECT status, data::text FROM tenant_default.documents WHERE id = $1 AND doctype = 'SalesOrder'`, made.OrderID).Scan(&status, &data)
	for _, want := range []string{`"order_status": "Draft"`, `"made_by_assistant": true`, `"assistant_user": "` + userID + `"`} {
		if !strings.Contains(data, want) {
			t.Fatalf("draft record lacks %s: %s", want, data)
		}
	}
	if status != "Draft" || strings.Contains(data, `"accepted_by"`) || strings.Contains(data, `Accepted`) {
		t.Fatalf("the assistant forged a server-set field: status=%s %s", status, data)
	}
	var reservedAfter, tasks int
	db.DB.QueryRow(`SELECT reserved FROM tenant_default.inventory_availability WHERE sku = $1 AND location_code = $2`, f.sku, f.locA).Scan(&reservedAfter)
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.documents WHERE doctype = 'FulfillmentTask' AND data->>'order_id' = $1`, made.OrderID).Scan(&tasks)
	if reservedAfter != reservedBefore || tasks != 0 {
		t.Fatalf("a draft moved stock or made a task: reserved %d->%d tasks %d", reservedBefore, reservedAfter, tasks)
	}
	// Bad input: no lines, a quantity below one, an unknown item.
	for _, bad := range []string{`{"customer_name":"A","lines":[]}`, fmt.Sprintf(`{"lines":[{"sku":%q,"qty":0}]}`, f.sku), `{"lines":[{"sku":"NO-SUCH-SKU-H37","qty":1}]}`} {
		if rec := f.do(http.MethodPost, "/api/public/v1/ai/sales-order-drafts", token, bad, nil); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("bad draft %s: %d %s", bad, rec.Code, rec.Body.String())
		}
	}
	// The draft shows in its status route for the same user, as a draft.
	if rec := f.do(http.MethodGet, "/api/public/v1/ai/orders/"+made.OrderID+"/status", token, "", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Draft") {
		t.Fatalf("draft status: %d %s", rec.Code, rec.Body.String())
	}

	// Another store's manager cannot submit it; the assistant cannot submit it.
	_, otherTok := f.user(engines.RoleStoreManager, f.locB)
	if rec := f.do(http.MethodPost, "/api/v1/orders/"+made.OrderID+"/submit-assistant-draft", otherTok, `{}`, nil); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("another store submitting: %d %s", rec.Code, rec.Body.String())
	}
	var stillDraft string
	db.DB.QueryRow(`SELECT status FROM tenant_default.documents WHERE id = $1`, made.OrderID).Scan(&stillDraft)
	if stillDraft != "Draft" {
		t.Fatalf("another store's submit changed the draft to %s", stillDraft)
	}
	// The person submits it: a real order, stock reserved, the draft closed and linked.
	rec = f.do(http.MethodPost, "/api/v1/orders/"+made.OrderID+"/submit-assistant-draft", userTok, `{}`, nil)
	if rec.Code != 200 {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}
	var sub struct {
		OrderID string `json:"order_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &sub)
	f.cleanup = append(f.cleanup, func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1 OR data->>'order_id' = $1 OR id LIKE $1 || '-L%'`, sub.OrderID)
	})
	var subStatus, subData string
	db.DB.QueryRow(`SELECT status, data::text FROM tenant_default.documents WHERE id = $1 AND doctype = 'SalesOrder'`, sub.OrderID).Scan(&subStatus, &subData)
	if sub.OrderID == "" || sub.OrderID == made.OrderID || subStatus == "Draft" || !strings.Contains(subData, `"submitted_from_draft": "`+made.OrderID+`"`) ||
		!strings.Contains(subData, `"submitted_by": "`+userID+`"`) {
		t.Fatalf("submitted order: id=%s status=%s %s", sub.OrderID, subStatus, subData)
	}
	var closed string
	db.DB.QueryRow(`SELECT data->>'order_status' FROM tenant_default.documents WHERE id = $1`, made.OrderID).Scan(&closed)
	if closed != "Closed" {
		t.Fatalf("the draft after submit is %q, want Closed", closed)
	}
	if rec := f.do(http.MethodPost, "/api/v1/orders/"+made.OrderID+"/submit-assistant-draft", userTok, `{}`, nil); rec.Code == 200 {
		t.Fatalf("a draft submitted twice")
	}
	// An ordinary order is not an assistant draft.
	if rec := f.do(http.MethodPost, "/api/v1/orders/"+sub.OrderID+"/submit-assistant-draft", userTok, `{}`, nil); rec.Code == 200 {
		t.Fatalf("a non-draft order went through submit")
	}
}

// Every call is audited with the user and the client; the budget applies.
func TestStage14837CallsAreAuditedAndRateLimited(t *testing.T) {
	f := newAIFixture(t)
	f.moduleOn(true)
	userID, _ := f.user(engines.RoleStoreManager, f.locA)
	client, token := f.issue(userID, "ai:read")
	if rec := f.do(http.MethodGet, f.stockPath(f.locA), token, "", nil); rec.Code != 200 {
		t.Fatalf("call: %d", rec.Code)
	}
	f.do(http.MethodGet, f.stockPath(f.locB), token, "", nil) // refused, still audited
	var n int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.audit_logs WHERE action LIKE 'AI_ACCESS%' AND details LIKE '%' || $1 || '%' AND details LIKE '%' || $2 || '%'`,
		client.ClientID, userID).Scan(&n)
	if n < 2 {
		t.Fatalf("expected an audit row per call (allowed and refused) naming client and user, got %d", n)
	}
	one := 1
	if err := engines.SetPublicAPICredentialLimits("default", client.Credential.ID, &one, nil); err != nil {
		t.Fatal(err)
	}
	engines.ResetPublicAPICredentialBudget(client.Credential.ID)
	f.do(http.MethodGet, f.stockPath(f.locA), token, "", nil)
	if rec := f.do(http.MethodGet, f.stockPath(f.locA), token, "", nil); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the second call in the minute: %d, want 429", rec.Code)
	}
}
