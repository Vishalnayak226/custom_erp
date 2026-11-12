package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Stage 47.12.6: the synthetic, resettable sandbox and the rehearsal gate.

func sandboxTestEnv(t *testing.T) {
	t.Helper()
	synthTestEnv(t)
	// A server that WOULD send: the sandbox must still stop every call.
	t.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "")
	t.Setenv("ENV", "production")
	invalidateSandboxFlag()
}

func dropSandboxForTest(tenantID, schema string) {
	db.DB.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", tenantID)
	invalidateSandboxFlag()
}

func TestSandboxResetRefusesAnythingThatIsNotASandbox(t *testing.T) {
	sandboxTestEnv(t)

	for _, id := range []string{"default", "no_such_tenant", ""} {
		if _, err := ResetSandboxData(id, "tester", "must refuse"); err == nil {
			t.Errorf("ResetSandboxData(%q) did not refuse", id)
		}
		if _, err := ResetAndReseedSandbox(id, "tester", "must refuse", SandboxSeed{}); err == nil {
			t.Errorf("ResetAndReseedSandbox(%q) did not refuse", id)
		}
		if _, err := SeedSandboxTenant(id, SandboxSeed{}); err == nil {
			t.Errorf("SeedSandboxTenant(%q) did not refuse", id)
		}
	}

	// A sandbox whose flag is switched off is a real client: refused, data untouched.
	id, schema, _, err := ProvisionSandboxTenant("0.0-test", 5)
	if err != nil {
		t.Fatal(err)
	}
	defer dropSandboxForTest(id, schema)
	if _, err := db.DB.Exec(`UPDATE public.tenants SET is_sandbox = FALSE WHERE tenant_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	invalidateSandboxFlag()
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.users (username, password_hash, role) VALUES ('keepme','x','Cashier') ON CONFLICT DO NOTHING`, schema)); err != nil {
		t.Logf("seed user (best effort): %v", err)
	}
	if _, err := ResetSandboxData(id, "tester", "must refuse"); err == nil || !strings.Contains(err.Error(), "not a sandbox") {
		t.Fatalf("a client with the sandbox flag off was not refused: %v", err)
	}
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.users`, schema)).Scan(&n)
	if n == 0 {
		t.Error("the refused reset removed logins")
	}

	// And the flag cannot be switched back on for a client that has been live
	// more than an hour (migration trigger): no way to turn a real client into
	// a reset target.
	if _, err := db.DB.Exec(`UPDATE public.tenants SET provisioned_at = CURRENT_TIMESTAMP - INTERVAL '3 hours' WHERE tenant_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE public.tenants SET is_sandbox = TRUE WHERE tenant_id = $1`, id); err == nil {
		t.Error("the database let an older client be flagged a sandbox")
	}
}

func TestSandboxNeverReceivesACopyOfAClient(t *testing.T) {
	sandboxTestEnv(t)
	if err := CopyTenantDataIntoSandbox("default", "anything"); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("copy into a sandbox was not refused: %v", err)
	}
}

func TestSandboxStopsOutsideCallsRecordsThemAndResets(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a sandbox of 60 bills")
	}
	sandboxTestEnv(t)

	var hits int32
	canary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(200)
	}))
	defer canary.Close()

	id, pw, first, err := CreateSyntheticSandbox("0.0-test", 5, SandboxSeed{Seed: 11})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	schema := first.Schema
	defer dropSandboxForTest(id, schema)
	if pw == "" || first.Counts["bills"] == 0 {
		t.Fatalf("create gave password %q, counts %v", pw, first.Counts)
	}
	if sb, _ := IsSandboxTenant(id); !sb {
		t.Fatal("not flagged a sandbox")
	}
	var docs int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid'`, schema)).Scan(&docs)
	if docs == 0 {
		t.Fatal("the seed made no sale")
	}

	// A seeded sandbox is not seeded twice.
	if _, err := SeedSandboxTenant(id, SandboxSeed{Seed: 11}); err == nil {
		t.Error("seeding a sandbox that already holds records did not refuse")
	}

	// An outside call under the sandbox never reaches the canary.
	ctx := WithOutsideCallsTenant(context.Background(), id)
	_, _, err = doConnectorRequest(ctx, 2*time.Second, "POST", canary.URL+"/orders", nil, []byte("{}"), "sandbox-test")
	if err == nil {
		t.Error("the outside call was not refused")
	}
	// A client that is not a sandbox, under the same server posture, does reach it (positive control).
	if _, _, err := doConnectorRequest(WithOutsideCallsTenant(context.Background(), "default"), 2*time.Second, "POST", canary.URL+"/orders", nil, []byte("{}"), "sandbox-test-control"); err != nil {
		t.Logf("control call refused (client policy): %v", err)
	} else if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("control call did not arrive, hits=%d", hits)
	}
	if h := atomic.LoadInt32(&hits); h > 1 {
		t.Errorf("sandbox call reached the canary: hits=%d", h)
	}

	counts, err := SandboxEffectCounts(id)
	if err != nil || counts["connector"] == 0 {
		t.Fatalf("suppressed call not recorded: %v %v", counts, err)
	}
	rows, _ := ListSandboxEffects(id, 10)
	if len(rows) == 0 || !strings.Contains(rows[0].Detail, "POST") {
		t.Errorf("record lacks the method: %+v", rows)
	}

	// Reset and re-seed: effects cleared, same seed gives the same data.
	second, err := ResetAndReseedSandbox(id, "tester", "rehearsal over", SandboxSeed{Seed: 11})
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	// The audit trail is kept across a reset (as for a real client), so the
	// whole-schema checksum differs; the business data must be the same.
	for _, k := range []string{"bills", "items", "returns", "bins", "gl_postings"} {
		if second.Counts[k] != first.Counts[k] {
			t.Errorf("same seed, %s: %d before the reset, %d after", k, first.Counts[k], second.Counts[k])
		}
	}
	if c, _ := SandboxEffectCounts(id); len(c) != 0 {
		t.Errorf("effects survived the reset: %v", c)
	}
	if sb, _ := IsSandboxTenant(id); !sb {
		t.Error("lost the sandbox flag in the reset")
	}
}

func TestConnectorRehearsalIsAWarningUnlessSettingOn(t *testing.T) {
	db.InitDB(testConnStr())
	invalidateSandboxFlag()
	defer SetSetting("default", SettingRequireRehearsal, "false", "test")

	db.DB.Exec(`DELETE FROM public.sandbox_effect_log WHERE kind = 'connector'`)
	count := func(action string) int {
		var n int
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.audit_logs WHERE action = $1`, outsideCallsSchemaOf("default")), action).Scan(&n)
		return n
	}
	warned, refused := count("CONNECTOR_ENABLED_WITHOUT_REHEARSAL"), count("CONNECTOR_ENABLE_REFUSED")

	if err := SetSetting("default", SettingRequireRehearsal, "false", "test"); err != nil {
		t.Fatal(err)
	}
	if err := checkConnectorRehearsal("default", "channel", "shopify", "tester"); err != nil {
		t.Fatalf("default Off must not block: %v", err)
	}
	if count("CONNECTOR_ENABLED_WITHOUT_REHEARSAL") != warned+1 {
		t.Error("no recorded warning")
	}

	if err := SetSetting("default", SettingRequireRehearsal, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if err := checkConnectorRehearsal("default", "channel", "shopify", "tester"); err == nil {
		t.Fatal("On must refuse an un-rehearsed connector")
	}
	if count("CONNECTOR_ENABLE_REFUSED") != refused+1 {
		t.Error("no recorded refusal")
	}

	// A sandbox rehearsal in the window lifts the refusal.
	if _, err := db.DB.Exec(`INSERT INTO public.sandbox_effect_log (tenant_id, kind, effect, detail)
		SELECT tenant_id, 'connector', 'test', 'rehearsal' FROM public.tenants WHERE tenant_id = 'default'`); err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec(`DELETE FROM public.sandbox_effect_log WHERE effect = 'test'`)
	if err := checkConnectorRehearsal("default", "channel", "shopify", "tester"); err != nil {
		t.Errorf("a rehearsed connector was refused: %v", err)
	}
}
