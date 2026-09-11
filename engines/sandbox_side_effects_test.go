package engines

import (
	"custom_erp/db"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// Stage 49.7.6: a sandbox tenant (Stage 38.7) must never reach a real
// external side effect, whatever the server-wide ExternalSideEffectsEnabled()
// posture is. engines/webhook.go's deliverWebhook already proved this for
// outbound webhooks; these two tests close the same gap found while building
// 49.7.6 in engines/password_reset.go (sendPasswordResetEmail) and
// engines/notifications.go (DispatchNotification) - both took a tenantID or
// schema but never actually checked it before making (or queuing) a real
// network call.

// TestSandboxTenantNeverSendsPasswordResetEmail proves the fix by racing a
// real TCP listener against sendPasswordResetEmail: a sandbox tenant must
// never connect to it, and - as the positive control that the listener and
// SMTP configuration are actually wired correctly - a real tenant must.
func TestSandboxTenantNeverSendsPasswordResetEmail(t *testing.T) {
	db.InitDB(testConnStr())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()
	var connections int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&connections, 1)
			conn.Close()
		}
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}

	for _, key := range []string{"SMTP_HOST", "SMTP_PORT", "SMTP_FROM", "ENV", "ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "ERP_DISABLE_EXTERNAL_SIDE_EFFECTS"} {
		old := os.Getenv(key)
		defer os.Setenv(key, old)
	}
	os.Setenv("SMTP_HOST", host)
	os.Setenv("SMTP_PORT", port)
	os.Setenv("SMTP_FROM", "no-reply@test.invalid")
	os.Setenv("ENV", "production") // ExternalSideEffectsEnabled() == true server-wide
	os.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "")
	os.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "")

	sandboxTenantID, sandboxSchema, _, err := ProvisionSandboxTenant("0.1.0-test", 5)
	if err != nil {
		t.Fatalf("ProvisionSandboxTenant: %v", err)
	}
	defer func() {
		db.DB.Exec("DROP SCHEMA IF EXISTS " + sandboxSchema + " CASCADE")
		db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", sandboxTenantID)
	}()

	sendPasswordResetEmail(sandboxTenantID, "someone@test.invalid", "someone", "http://example.invalid/reset?token=x")
	time.Sleep(150 * time.Millisecond)
	if atomic.LoadInt32(&connections) != 0 {
		t.Fatalf("expected a sandbox tenant's password reset to make NO real SMTP connection, but the canary listener was hit %d time(s)", atomic.LoadInt32(&connections))
	}

	// Positive control: the identical call for a real (non-sandbox) tenant
	// must actually attempt delivery, proving the sandbox check above - not a
	// broken listener or SMTP config - is what suppressed the first call.
	sendPasswordResetEmail("default", "someone@test.invalid", "someone", "http://example.invalid/reset?token=x")
	time.Sleep(150 * time.Millisecond)
	if atomic.LoadInt32(&connections) == 0 {
		t.Fatalf("expected a real tenant's password reset to attempt a real SMTP connection to the canary listener, but it was never hit")
	}
}

// TestSandboxTenantNeverSendsAccountRiskNotices covers the two other real
// SMTP call sites in this same file (Stage 49.2.4's
// SendRecoveryEmailChangedNotice and SendPasswordChangedNotice), which
// landed on main after the original sandbox fix above and had the identical
// gap - found and fixed while reconciling this branch onto main.
func TestSandboxTenantNeverSendsAccountRiskNotices(t *testing.T) {
	db.InitDB(testConnStr())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()
	var connections int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&connections, 1)
			conn.Close()
		}
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}

	for _, key := range []string{"SMTP_HOST", "SMTP_PORT", "SMTP_FROM", "ENV", "ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "ERP_DISABLE_EXTERNAL_SIDE_EFFECTS"} {
		old := os.Getenv(key)
		defer os.Setenv(key, old)
	}
	os.Setenv("SMTP_HOST", host)
	os.Setenv("SMTP_PORT", port)
	os.Setenv("SMTP_FROM", "no-reply@test.invalid")
	os.Setenv("ENV", "production")
	os.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "")
	os.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "")

	sandboxTenantID, sandboxSchema, _, err := ProvisionSandboxTenant("0.1.0-test", 5)
	if err != nil {
		t.Fatalf("ProvisionSandboxTenant: %v", err)
	}
	defer func() {
		db.DB.Exec("DROP SCHEMA IF EXISTS " + sandboxSchema + " CASCADE")
		db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", sandboxTenantID)
	}()

	SendRecoveryEmailChangedNotice(sandboxTenantID, "old@test.invalid", "someone", "new@test.invalid")
	SendPasswordChangedNotice(sandboxTenantID, "old@test.invalid", "someone", "your account settings")
	time.Sleep(150 * time.Millisecond)
	if atomic.LoadInt32(&connections) != 0 {
		t.Fatalf("expected a sandbox tenant's account-risk notices to make NO real SMTP connection, but the canary listener was hit %d time(s)", atomic.LoadInt32(&connections))
	}

	// Positive control.
	SendPasswordChangedNotice("default", "old@test.invalid", "someone", "your account settings")
	time.Sleep(150 * time.Millisecond)
	if atomic.LoadInt32(&connections) == 0 {
		t.Fatalf("expected a real tenant's account-risk notice to attempt a real SMTP connection, but it was never hit")
	}
}

// TestSandboxTenantNeverDispatchesNotificationWebhook proves the same fix in
// DispatchNotification: a sandbox tenant's Active NotificationTemplate +
// NotificationChannelConfig must never actually be POSTed, and the
// NotificationLog must say so (Skipped-Sandbox) rather than claim "Sent".
func TestSandboxTenantNeverDispatchesNotificationWebhook(t *testing.T) {
	db.InitDB(testConnStr())

	for _, key := range []string{"ENV", "ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "ERP_DISABLE_EXTERNAL_SIDE_EFFECTS"} {
		old := os.Getenv(key)
		defer os.Setenv(key, old)
	}
	os.Setenv("ENV", "production")
	os.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "")
	os.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "")

	var called int32
	canary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer canary.Close()

	sandboxTenantID, sandboxSchema, _, err := ProvisionSandboxTenant("0.1.0-test", 5)
	if err != nil {
		t.Fatalf("ProvisionSandboxTenant: %v", err)
	}
	defer func() {
		db.DB.Exec("DROP SCHEMA IF EXISTS " + sandboxSchema + " CASCADE")
		db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", sandboxTenantID)
	}()

	tmplData, _ := json.Marshal(map[string]interface{}{
		"code": "NT-TEST-SANDBOX", "event": "Order Cancelled", "channel": "Email",
		"subject": "", "body_template": "Order {{order_id}} was cancelled", "status": "Active",
	})
	if _, err := db.DB.Exec("INSERT INTO "+sandboxSchema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'NotificationTemplate', $2, 'Active', 'system')", "NT-TEST-SANDBOX", tmplData); err != nil {
		t.Fatalf("seed NotificationTemplate: %v", err)
	}
	cfgData, _ := json.Marshal(map[string]interface{}{
		"code": "NCC-TEST-SANDBOX", "channel": "Email", "webhook_url": canary.URL, "status": "Active",
	})
	if _, err := db.DB.Exec("INSERT INTO "+sandboxSchema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'NotificationChannelConfig', $2, 'Active', 'system')", "NCC-TEST-SANDBOX", cfgData); err != nil {
		t.Fatalf("seed NotificationChannelConfig: %v", err)
	}

	DispatchNotification(sandboxTenantID, "Order Cancelled", "SO-SANDBOX-TEST", nil)
	time.Sleep(150 * time.Millisecond)

	if atomic.LoadInt32(&called) != 0 {
		t.Fatalf("expected a sandbox tenant's notification dispatch to make NO real HTTP call, but the canary server was hit")
	}
	var status string
	if err := db.DB.QueryRow("SELECT data->>'dispatch_status' FROM " + sandboxSchema + ".documents WHERE doctype = 'NotificationLog' AND data->>'template_id' = 'NT-TEST-SANDBOX' ORDER BY created_at DESC LIMIT 1").Scan(&status); err != nil {
		t.Fatalf("query NotificationLog: %v", err)
	}
	if status != "Skipped-Sandbox" {
		t.Fatalf("expected dispatch_status Skipped-Sandbox, got %q", status)
	}
}
