package engines

import (
	"crypto/hmac"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestExtensionHookTargetURLValidation exercises validateHookTargetURL in
// isolation (no DB) - the gate added to stop a hook from being registered
// with a plain-http:// target_url, which would send a tenant's document
// payload to a 3rd party unencrypted. Plain http:// stays allowed only for
// localhost/loopback so local development against the extension-sdk doesn't
// need a real TLS cert.
func TestExtensionHookTargetURLValidation(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
	}{
		{"https://example.com/hook", false},
		{"http://localhost:8080/hook", false},
		{"http://127.0.0.1:8080/hook", false},
		{"http://[::1]:8080/hook", false},
		{"http://sub.localhost/hook", false},
		{"", true},
		{"://bad", true},
		{"example.com/hook", true},
		{"ftp://example.com/hook", true},
		{"http://example.com/hook", true},
		{"http://evil.example.com.localhost.attacker.io/hook", true},
	}
	for _, c := range cases {
		err := validateHookTargetURL(c.url)
		if c.wantErr && err == nil {
			t.Errorf("validateHookTargetURL(%q): expected an error, got nil", c.url)
		}
		if !c.wantErr && err != nil {
			t.Errorf("validateHookTargetURL(%q): expected no error, got %v", c.url, err)
		}
	}
}

// seedExtensionHook inserts a hook row directly (bypassing
// RegisterExtensionHook, since these tests need to control secret/timeout
// precisely) and returns its id.
func seedExtensionHook(t *testing.T, schema, hookPoint, doctype, targetURL, secret string, timeoutMs int) string {
	t.Helper()
	var id string
	err := db.DB.QueryRow(
		"INSERT INTO "+schema+".extension_hooks (hook_point, doctype, target_url, secret, timeout_ms, created_by) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id",
		hookPoint, doctype, targetURL, secret, timeoutMs, "test",
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed extension hook: %v", err)
	}
	return id
}

func cleanupExtensionHooks(schema, doctype string) {
	db.DB.Exec("DELETE FROM "+schema+".extension_hook_log WHERE hook_id IN (SELECT id FROM "+schema+".extension_hooks WHERE doctype = $1)", doctype)
	db.DB.Exec("DELETE FROM "+schema+".extension_hooks WHERE doctype = $1", doctype)
}

// TestInvokeBeforeSaveHooksSignatureAndPayloadShape confirms the actual
// contract a 3rd-party developer's endpoint relies on: the payload's exact
// key shape (hook_point/doctype/document_id/tenant_id/data), and that
// X-Signature is a real HMAC-SHA256 of the raw body computable independently
// from the hook's own secret - this is what extension-sdk/README.md
// documents and what would silently break for every existing client
// extension if a future change to handleGenericDoc's save path altered it.
func TestInvokeBeforeSaveHooksSignatureAndPayloadShape(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const doctype = "TEST_EXT_DOC_SIG"
	const secret = "test-hmac-secret-signature-check"
	cleanupExtensionHooks(schema, doctype)
	defer cleanupExtensionHooks(schema, doctype)

	var receivedBody []byte
	var receivedSig string
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		receivedSig = r.Header.Get("X-Signature")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hookID := seedExtensionHook(t, schema, "document.before_save", doctype, server.URL, secret, 3000)

	data := map[string]interface{}{"name": "Widget", "price": float64(42)}
	if err := InvokeBeforeSaveHooks(tenantID, doctype, "DOC-001", data); err != nil {
		t.Fatalf("InvokeBeforeSaveHooks: unexpected error: %v", err)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("expected the hook to be called exactly once, got %d", callCount)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("hook received an unparseable payload: %v", err)
	}
	if payload["hook_point"] != "document.before_save" || payload["doctype"] != doctype ||
		payload["document_id"] != "DOC-001" || payload["tenant_id"] != tenantID {
		t.Fatalf("unexpected payload shape: %v", payload)
	}
	gotData, _ := payload["data"].(map[string]interface{})
	if gotData["name"] != "Widget" {
		t.Fatalf("expected data.name=Widget in payload, got %v", gotData)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(receivedBody)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if receivedSig != wantSig {
		t.Fatalf("signature mismatch: got %q want %q", receivedSig, wantSig)
	}

	var logCount int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".extension_hook_log WHERE hook_id = $1", hookID).Scan(&logCount)
	if logCount != 1 {
		t.Fatalf("expected 1 extension_hook_log row for this call, got %d", logCount)
	}
}

// TestInvokeBeforeSaveHooksBlocksOnNon2xx confirms the core safety property
// a before_save hook exists for: a non-2xx response from the 3rd-party
// endpoint must block the save (return a non-nil error), not let it proceed
// with an unreviewed value.
func TestInvokeBeforeSaveHooksBlocksOnNon2xx(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const doctype = "TEST_EXT_DOC_REJECT"
	cleanupExtensionHooks(schema, doctype)
	defer cleanupExtensionHooks(schema, doctype)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer server.Close()

	seedExtensionHook(t, schema, "document.before_save", doctype, server.URL, "secret", 3000)

	err = InvokeBeforeSaveHooks(tenantID, doctype, "DOC-002", map[string]interface{}{})
	if err == nil {
		t.Fatalf("expected a rejecting before_save hook to block the save")
	}
	verr, ok := err.(*ValidationError)
	if !ok || verr.Code != "EXT-0289" {
		t.Fatalf("expected an EXT-0289 ValidationError, got %v (%T)", err, err)
	}
}

// TestInvokeBeforeSaveHooksBlocksOnTimeout confirms a hung/slow 3rd-party
// endpoint blocks the save (rather than silently letting it through) and
// that the caller gets control back promptly - the panic/hang isolation
// callHookWithRecovery's own doc comment describes.
func TestInvokeBeforeSaveHooksBlocksOnTimeout(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const doctype = "TEST_EXT_DOC_TIMEOUT"
	cleanupExtensionHooks(schema, doctype)
	defer cleanupExtensionHooks(schema, doctype)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	seedExtensionHook(t, schema, "document.before_save", doctype, server.URL, "secret", 100)

	start := time.Now()
	err = InvokeBeforeSaveHooks(tenantID, doctype, "DOC-003", map[string]interface{}{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected a timed-out before_save hook to block the save")
	}
	verr, ok := err.(*ValidationError)
	if !ok || verr.Code != "EXT-0290" {
		t.Fatalf("expected an EXT-0290 ValidationError, got %v (%T)", err, err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("expected the call to return shortly after its own 100ms timeout, took %v", elapsed)
	}
}

// TestInvokeBeforeSaveHooksNoMatchIsNoop confirms the documented common-case
// fast path: no hooks registered for a doctype returns immediately with no
// error and no network call.
func TestInvokeBeforeSaveHooksNoMatchIsNoop(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	if err := InvokeBeforeSaveHooks("default", "TEST_EXT_DOC_NEVER_REGISTERED", "DOC-005", map[string]interface{}{}); err != nil {
		t.Fatalf("expected no matching hooks to be a no-op, got error: %v", err)
	}
}

// TestInvokeAfterSaveHooksAsyncDoesNotBlockCaller confirms the async design
// contract: handleGenericDoc's save path must never wait on an after_save
// hook, even a slow/hung one - it fires in the background and the caller
// gets control back immediately.
func TestInvokeAfterSaveHooksAsyncDoesNotBlockCaller(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const doctype = "TEST_EXT_DOC_ASYNC"
	cleanupExtensionHooks(schema, doctype)
	defer cleanupExtensionHooks(schema, doctype)

	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		select {
		case called <- struct{}{}:
		default:
		}
	}))
	defer server.Close()

	seedExtensionHook(t, schema, "document.after_save", doctype, server.URL, "secret", 3000)

	start := time.Now()
	InvokeAfterSaveHooksAsync(tenantID, doctype, "DOC-004", map[string]interface{}{})
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Fatalf("expected InvokeAfterSaveHooksAsync to return immediately, took %v", elapsed)
	}

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected the after_save hook to eventually be called in the background")
	}
}
