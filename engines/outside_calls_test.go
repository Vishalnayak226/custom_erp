package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Stage 49.8.7a: connectors, couriers and extension hooks obey the same
// switches as email and webhooks - off in development (loopback excepted),
// on in production, stopped by the emergency switch - and each client has
// its own Outside calls setting.

// outsideCallsEnv sets the three server switches for one test.
func outsideCallsEnv(t *testing.T, env, enable, disable string) {
	t.Helper()
	t.Setenv("ENV", env)
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", enable)
	t.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", disable)
}

func countingServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func wantConn0225(t *testing.T, label string, err error, reasonPart string) {
	t.Helper()
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "CONN-0225" || !strings.Contains(verr.Message, reasonPart) || !strings.Contains(verr.Message, "nothing was sent") {
		t.Fatalf("%s: want CONN-0225 naming %q, got %v", label, reasonPart, err)
	}
}

func notRefused(t *testing.T, label string, err error) {
	t.Helper()
	var verr *ValidationError
	if errors.As(err, &verr) && verr.Code == "CONN-0225" {
		t.Fatalf("%s: the call was refused by a switch, it should have gone: %v", label, err)
	}
}

// outsideCallsTenant provisions a client of its own, so its setting touches
// no shared data.
func outsideCallsTenant(t *testing.T) (tenantID, schema string) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema = uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	return tenantID, schema
}

func TestOutsideCallsConnectorSwitches(t *testing.T) {
	const key = "outside-calls-49-8-7a"
	resetBreaker(key)
	t.Cleanup(func() { resetBreaker(key) })
	var hits atomic.Int32
	local := countingServer(t, &hits)
	// TEST-NET-1: never answers. A call that is let through fails on its
	// own short timeout; one that is refused never tries.
	const remote = "https://192.0.2.1:9/api"
	ctx := context.Background()

	// Development: a call to another machine is refused, a read too.
	outsideCallsEnv(t, "", "", "")
	_, _, err := doConnectorRequest(ctx, 300*time.Millisecond, http.MethodPost, remote, nil, []byte(`{}`), key)
	wantConn0225(t, "development write", err, "off in development")
	_, _, err = doConnectorRequest(ctx, 300*time.Millisecond, http.MethodGet, remote, nil, nil, key)
	wantConn0225(t, "development read", err, "off in development")
	// ... and loopback is not an outside call.
	if status, _, err := doConnectorRequest(ctx, 2*time.Second, http.MethodPost, local.URL, nil, []byte(`{}`), key); err != nil || status != http.StatusOK || hits.Load() != 1 {
		t.Fatalf("development, loopback: want it to go, got %d %v (hits %d)", status, err, hits.Load())
	}
	if status, _, err := doConnectorRequest(ctx, 2*time.Second, http.MethodPost, strings.Replace(local.URL, "127.0.0.1", "localhost", 1), nil, []byte(`{}`), key); err != nil || status != http.StatusOK || hits.Load() != 2 {
		t.Fatalf("development, localhost: want it to go, got %d %v (hits %d)", status, err, hits.Load())
	}

	// ERP_ENABLE_EXTERNAL_SIDE_EFFECTS=1 opts a development run in.
	outsideCallsEnv(t, "", "1", "")
	_, _, err = doConnectorRequest(ctx, 300*time.Millisecond, http.MethodPost, remote, nil, []byte(`{}`), key)
	notRefused(t, "development opted in", err)
	resetBreaker(key)

	// Production: the call goes (and fails only on the unreachable address).
	outsideCallsEnv(t, "production", "", "")
	_, _, err = doConnectorRequest(ctx, 300*time.Millisecond, http.MethodPost, remote, nil, []byte(`{}`), key)
	if err == nil {
		t.Fatal("production: the unreachable address should have failed on the network")
	}
	notRefused(t, "production", err)
	resetBreaker(key)

	// The emergency switch beats production: writes stop, loopback too;
	// reads still go, for reconciliation.
	outsideCallsEnv(t, "production", "", "1")
	_, _, err = doConnectorRequest(ctx, 2*time.Second, http.MethodPost, local.URL, nil, []byte(`{}`), key)
	wantConn0225(t, "emergency switch", err, "emergency switch")
	if hits.Load() != 2 {
		t.Fatalf("emergency switch: the write reached the server (hits %d)", hits.Load())
	}
	if status, _, err := doConnectorRequest(ctx, 2*time.Second, http.MethodGet, local.URL, nil, nil, key); err != nil || status != http.StatusOK || hits.Load() != 3 {
		t.Fatalf("emergency switch: a read still goes: %d %v (hits %d)", status, err, hits.Load())
	}
	// ... and in development too.
	outsideCallsEnv(t, "", "1", "1")
	_, _, err = doConnectorRequest(ctx, 2*time.Second, http.MethodPost, local.URL, nil, []byte(`{}`), key)
	wantConn0225(t, "emergency switch, opted-in development", err, "emergency switch")

	// The banner says which of the three it is.
	for _, c := range []struct{ env, enable, disable, want string }{
		{"", "", "", "SIMULATED"}, {"production", "", "", "LIVE"}, {"production", "", "1", "KILLED"},
	} {
		outsideCallsEnv(t, c.env, c.enable, c.disable)
		if b := EnvironmentBanner(); !strings.HasPrefix(b, c.want) || !strings.Contains(b, "connector") {
			t.Fatalf("banner for ENV=%q disable=%q: %q", c.env, c.disable, b)
		}
	}
}

func TestOutsideCallsPerClientSwitch(t *testing.T) {
	tenantID, schema := outsideCallsTenant(t)
	if err := SetSetting(tenantID, SettingOutsideCalls, "false", "test"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	outsideCallsEnv(t, "production", "", "")

	if ok, reason := OutsideCallsAllowed(schema); ok || !strings.Contains(reason, "off for this client") {
		t.Fatalf("client Off: OutsideCallsAllowed = %v %q", ok, reason)
	}
	if ok, reason := OutsideCallsAllowedForTenant(tenantID); ok || reason == "" {
		t.Fatalf("client Off, by tenant id: OutsideCallsAllowedForTenant = %v %q", ok, reason)
	}
	if ok, _ := OutsideCallsAllowed("tenant_default"); !ok {
		t.Fatal("another client (default) must not be switched off with this one")
	}

	const key = "outside-calls-client-49-8-7a"
	resetBreaker(key)
	t.Cleanup(func() { resetBreaker(key) })
	var hits atomic.Int32
	local := countingServer(t, &hits)

	// A connector or courier write for this client is refused, through either
	// way a context names the client (API request: tenant id; job: schema).
	for label, ctx := range map[string]context.Context{
		"request": WithOutsideCallsTenant(context.Background(), tenantID),
		"job":     withOutsideCallsSchema(context.Background(), schema),
	} {
		_, _, err := doConnectorRequest(ctx, 2*time.Second, http.MethodPost, local.URL, nil, []byte(`{}`), key)
		wantConn0225(t, "client Off, "+label, err, "off for this client")
	}
	if hits.Load() != 0 {
		t.Fatalf("client Off: a write reached the server (hits %d)", hits.Load())
	}
	// Its reads still go, for reconciliation.
	if status, _, err := doConnectorRequest(WithOutsideCallsTenant(context.Background(), tenantID), 2*time.Second, http.MethodGet, local.URL, nil, nil, key); err != nil || status != http.StatusOK || hits.Load() != 1 {
		t.Fatalf("client Off: a read still goes: %d %v (hits %d)", status, err, hits.Load())
	}
	// Another client's write goes.
	if status, _, err := doConnectorRequest(WithOutsideCallsTenant(context.Background(), "default"), 2*time.Second, http.MethodPost, local.URL, nil, []byte(`{}`), key); err != nil || status != http.StatusOK || hits.Load() != 2 {
		t.Fatalf("another client: want the write to go, got %d %v (hits %d)", status, err, hits.Load())
	}

	// A webhook delivery for this client is simulated, with the reason; the
	// default client's same delivery is not simulated (it reaches the
	// address check, which refuses loopback).
	job := Job{ID: "outside-calls-test", JobType: "webhook_delivery", Payload: map[string]interface{}{"url": local.URL, "event_name": "order.created", "event_payload": map[string]interface{}{}}}
	res, err := deliverWebhook(context.Background(), schema, job)
	if err != nil || res["simulated"] != true || !strings.Contains(res["reason"].(string), "off for this client") {
		t.Fatalf("client Off: webhook want simulated, got %v %v", res, err)
	}
	res, _ = deliverWebhook(context.Background(), "tenant_default", job)
	if res["simulated"] == true {
		t.Fatalf("another client: webhook must not be simulated: %v", res)
	}

	// A notification webhook (an order event routed to the client's own
	// system) is logged as not sent, with the reason.
	tmpl, _ := json.Marshal(map[string]interface{}{"code": "NT-OUTSIDE", "event": "Order Cancelled", "channel": "Email", "subject": "", "body_template": "Order {{order_id}}", "status": "Active"})
	cfg, _ := json.Marshal(map[string]interface{}{"code": "NCC-OUTSIDE", "channel": "Email", "webhook_url": local.URL, "status": "Active"})
	for id, data := range map[string][]byte{"NT-OUTSIDE": tmpl, "NCC-OUTSIDE": cfg} {
		doctype := "NotificationTemplate"
		if id == "NCC-OUTSIDE" {
			doctype = "NotificationChannelConfig"
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, data); err != nil {
			t.Fatalf("seed %s: %v", doctype, err)
		}
	}
	DispatchNotification(tenantID, "Order Cancelled", "SO-OUTSIDE", nil)
	var status, detail string
	if err := db.DB.QueryRow("SELECT data->>'dispatch_status', data->>'response_detail' FROM "+schema+".documents WHERE doctype = 'NotificationLog' AND data->>'template_id' = 'NT-OUTSIDE' ORDER BY created_at DESC LIMIT 1").Scan(&status, &detail); err != nil {
		t.Fatalf("NotificationLog: %v", err)
	}
	if status != "Skipped-SideEffectsOff" || !strings.Contains(detail, "off for this client") {
		t.Fatalf("client Off: notification want Skipped-SideEffectsOff naming the client switch, got %q %q", status, detail)
	}
	if hits.Load() != 2 {
		t.Fatalf("client Off: the webhook or notification reached the server (hits %d)", hits.Load())
	}

	// Back on, the client's write goes again.
	if err := SetSetting(tenantID, SettingOutsideCalls, "true", "test"); err != nil {
		t.Fatalf("SetSetting on: %v", err)
	}
	if status, _, err := doConnectorRequest(WithOutsideCallsTenant(context.Background(), tenantID), 2*time.Second, http.MethodPost, local.URL, nil, []byte(`{}`), key); err != nil || status != http.StatusOK {
		t.Fatalf("client back On: want the write to go, got %d %v", status, err)
	}
}

func TestOutsideCallsExtensionHooks(t *testing.T) {
	db.InitDB(testConnStr())
	const doctype = "TEST_EXT_DOC_OUTSIDE_CALLS"
	var hits atomic.Int32
	local := countingServer(t, &hits)
	cleanupExtensionHooks("tenant_default", doctype)
	t.Cleanup(func() { cleanupExtensionHooks("tenant_default", doctype) })

	wantRefused := func(label string, err error, reasonPart string) {
		t.Helper()
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != "EXT-0289" || !strings.Contains(verr.SubFor, reasonPart) {
			t.Fatalf("%s: want the save stopped with EXT-0289 naming %q, got %v", label, reasonPart, err)
		}
	}

	// Development: a hook on another machine is not called and the save is
	// stopped (refused before any DNS lookup of the .invalid name).
	outsideCallsEnv(t, "", "", "")
	remoteID := seedExtensionHook(t, "tenant_default", "document.before_save", doctype, "https://hook.invalid/check", "s", 2000)
	wantRefused("development", InvokeBeforeSaveHooks("default", doctype, "DOC-OC-1", map[string]interface{}{}), "off in development")
	if _, err := db.DB.Exec("DELETE FROM tenant_default.extension_hooks WHERE id = $1", remoteID); err != nil {
		t.Fatal(err)
	}
	// A local hook (the extension SDK on the developer's machine) is called.
	seedExtensionHook(t, "tenant_default", "document.before_save", doctype, local.URL, "s", 2000)
	if err := InvokeBeforeSaveHooks("default", doctype, "DOC-OC-2", map[string]interface{}{}); err != nil || hits.Load() != 1 {
		t.Fatalf("development, loopback hook: want it called, got %v (hits %d)", err, hits.Load())
	}

	// Production calls it.
	outsideCallsEnv(t, "production", "", "")
	if err := InvokeBeforeSaveHooks("default", doctype, "DOC-OC-3", map[string]interface{}{}); err != nil || hits.Load() != 2 {
		t.Fatalf("production: want the hook called, got %v (hits %d)", err, hits.Load())
	}

	// The emergency switch beats production.
	outsideCallsEnv(t, "production", "", "1")
	wantRefused("emergency switch", InvokeBeforeSaveHooks("default", doctype, "DOC-OC-4", map[string]interface{}{}), "emergency switch")
	if hits.Load() != 2 {
		t.Fatalf("emergency switch: the hook was called (hits %d)", hits.Load())
	}

	// A client whose Outside calls are off: its hook is not called, another
	// client's is; an after_save hook logs the refusal.
	outsideCallsEnv(t, "production", "", "")
	tenantID, schema := outsideCallsTenant(t)
	if err := SetSetting(tenantID, SettingOutsideCalls, "false", "test"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	seedExtensionHook(t, schema, "document.before_save", doctype, local.URL, "s", 2000)
	afterID := seedExtensionHook(t, schema, "document.after_save", doctype, local.URL, "s", 2000)
	wantRefused("client Off", InvokeBeforeSaveHooks(tenantID, doctype, "DOC-OC-5", map[string]interface{}{}), "off for this client")
	if err := InvokeBeforeSaveHooks("default", doctype, "DOC-OC-6", map[string]interface{}{}); err != nil || hits.Load() != 3 {
		t.Fatalf("another client: want its hook called, got %v (hits %d)", err, hits.Load())
	}
	InvokeAfterSaveHooksAsync(tenantID, doctype, "DOC-OC-7", map[string]interface{}{})
	var logged string
	for i := 0; i < 40; i++ {
		_ = db.DB.QueryRow("SELECT COALESCE(error, '') FROM "+schema+".extension_hook_log WHERE hook_id = $1 ORDER BY called_at DESC LIMIT 1", afterID).Scan(&logged)
		if logged != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(logged, "off for this client") {
		t.Fatalf("client Off: after_save want the refusal logged, got %q", logged)
	}
	if hits.Load() != 3 {
		t.Fatalf("client Off: a hook reached the server (hits %d)", hits.Load())
	}
}
