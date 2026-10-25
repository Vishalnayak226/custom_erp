package engines

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func stage4911Suffix() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func stage4911UUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// Stage 49.11.2: an audit event carries the request's correlation id, never a
// secret or a whole payload, and its signature covers what was stored.
func TestStage49112AuditEventCarriesCorrelationAndIsRedacted(t *testing.T) {
	db.InitDB(testConnStr())
	action := "TEST_49112_" + stage4911Suffix()
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1`, action)
	corr := "corr-49112-" + stage4911Suffix()
	ctx := WithCorrelationID(context.Background(), corr)
	LogAuditEventCtx(ctx, "default", "tester49112", action, "SUCCESS", "password=hunter2 "+strings.Repeat("x", 20000))

	var details, gotCorr, sig string
	var at time.Time
	if err := db.DB.QueryRow(`SELECT details, COALESCE(correlation_id, ''), signature, created_at FROM tenant_default.audit_logs WHERE action = $1`, action).
		Scan(&details, &gotCorr, &sig, &at); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if gotCorr != corr {
		t.Fatalf("correlation id %q, want %q", gotCorr, corr)
	}
	if strings.Contains(details, "hunter2") || !strings.Contains(details, "[REDACTED]") {
		t.Fatalf("a secret reached the audit log: %.80s", details)
	}
	if len(details) > maxAuditDetailBytes {
		t.Fatalf("details %d bytes, bound is %d", len(details), maxAuditDetailBytes)
	}
	if want := SignAuditRow("default", "tester49112", action, "SUCCESS", details, "", "", corr, at); sig != want {
		t.Fatal("the signature does not cover the stored row (correlation id or redacted details)")
	}
}

// Stage 49.11.3: one correlation id finds the audit row and the system error
// row of the same request - referenced, not copied.
func TestStage49113InvestigationTrailJoinsEvidence(t *testing.T) {
	db.InitDB(testConnStr())
	corr := stage4911UUID()
	action := "TEST_49113_" + stage4911Suffix()
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1`, action)
	defer db.DB.Exec(`DELETE FROM tenant_default.system_error_logs WHERE module_source = 'test.49113'`)
	LogAuditEventCtx(WithCorrelationID(context.Background(), corr), "default", "tester49113", action, "SUCCESS", "the business event")
	LogSystemError("default", corr, "ERROR", "test.49113", "the failure behind it", "")

	trail, err := InvestigationTrail("default", corr)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]bool{}
	for _, e := range trail {
		sources[e.Source] = true
	}
	if !sources["audit"] || !sources["system error"] {
		t.Fatalf("trail sources %v, want the audit row and the system error", sources)
	}
	if _, err := InvestigationTrail("default", ""); err == nil {
		t.Fatal("an empty correlation id must be refused, not list everything")
	}
}

// Stage 49.11.4 / 49.11.5 / 49.11.8 - the detection drill: inject the known
// events for every rule, run the sweep, and require every rule to fire once
// with its owner and first response; a second sweep inside the dedup window
// fires nothing. A rule that does not fire is a failed control.
func TestStage49118DetectionDrillCoversEveryRule(t *testing.T) {
	db.InitDB(testConnStr())
	const tenant = "default"
	s := stage4911Suffix()
	actor := "drill49118-" + s
	start := time.Now()
	disabledID := "__drill49118_" + s
	cleanup := func() {
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE user_id IN ($1, $2) OR details LIKE $3 OR details = $4`,
			actor, disabledID, "%drill49118-"+s+"%", "Set user "+disabledID+" status to Inactive")
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1 AND created_at > $2::timestamp`, SecurityAlertAction, utcStamp(start.Add(-time.Second)))
		db.DB.Exec(`DELETE FROM tenant_default.system_error_logs WHERE error_message LIKE $1`, "%drill49118-"+s+"%")
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, disabledID)
	}
	cleanup()
	defer cleanup()
	// Per-client rules dedup on subject "client": clear their recent alerts so
	// the drill measures this run, as a fresh client would.
	db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1 AND details LIKE 'rule=% subject=client:%'`, SecurityAlertAction)
	db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1 AND details LIKE 'rule=% subject=audit evidence:%'`, SecurityAlertAction)

	for _, rule := range SecurityDetectionRules() {
		if rule.special != nil {
			continue
		}
		status := "SUCCESS"
		if len(rule.statuses) > 0 {
			status = rule.statuses[0]
		}
		for i := 0; i < rule.Threshold; i++ {
			LogAuditEvent(tenant, actor, rule.actions[0], status, "drill49118-"+s)
		}
	}
	// disabled_user_activity: disabled by an admin, then still acting.
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code)
		VALUES ($1, $1, 'x', $2, 'Cashier', 'Inactive', 'HO')`, disabledID, disabledID+"@drill.invalid"); err != nil {
		t.Fatalf("seed disabled user: %v", err)
	}
	LogAuditEvent(tenant, "admin", "USER_MANAGEMENT", "USER_STATUS_CHANGED", "Set user "+disabledID+" status to Inactive")
	time.Sleep(5 * time.Millisecond)
	LogAuditEvent(tenant, disabledID, "TEST_DISABLED_ACTION", "SUCCESS", "drill49118-"+s)
	// audit_verify_failed: the verifier's own failure row.
	LogSystemError(tenant, "", "CRITICAL", "audit.verify", "drill49118-"+s+" evidence FAILED verification", "")
	// audit_checkpoint_overdue: the first signed row no checkpoint covers is
	// four hours old. Rows are written in time order, so the check reads the
	// first unsealed row by seq; a stand-in checkpoint just below the injected
	// row makes it that row whatever other tests have checkpointed.
	var unsealedSeq int64
	if err := db.DB.QueryRow(`INSERT INTO tenant_default.audit_logs (user_id, action, status, details, created_at, signature, sig_version)
		VALUES ($1, 'TEST_UNSEALED', 'SUCCESS', 'drill49118-'||$2, $3::timestamp, 'drill', 'v1') RETURNING seq`,
		actor, s, utcStamp(time.Now().Add(-4*time.Hour))).Scan(&unsealedSeq); err != nil {
		t.Fatalf("seed unsealed row: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.audit_checkpoints (from_seq, to_seq, row_count, row_digest, signature, sig_version)
		VALUES ($1, $1, 0, $2, 'drill', 'v1')`, unsealedSeq-1, "drill49118-"+s); err != nil {
		t.Fatalf("seed stand-in checkpoint: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_checkpoints WHERE row_digest = $1`, "drill49118-"+s)

	began := time.Now()
	fired, err := RunSecurityDetections(tenant, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(began)
	got := map[string]SecurityDetection{}
	for _, d := range fired {
		got[d.Rule] = d
	}
	for _, rule := range SecurityDetectionRules() {
		d, ok := got[rule.Key]
		if !ok {
			t.Errorf("rule %s did not fire on its own injected events - a missing detection is a failed control", rule.Key)
			continue
		}
		if !strings.Contains(d.Details, "Owner: "+rule.Owner) || !strings.Contains(d.Details, "First response:") || !strings.Contains(d.Details, "Runbook:") {
			t.Errorf("rule %s fired without owner / first response / runbook: %s", rule.Key, d.Details)
		}
		if d.Severity != rule.Severity {
			t.Errorf("rule %s severity %s, want %s", rule.Key, d.Severity, rule.Severity)
		}
	}
	t.Logf("drill: %d/%d rules fired in %s", len(got), len(SecurityDetectionRules()), took)

	again, err := RunSecurityDetections(tenant, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range again {
		if strings.Contains(d.Subject, actor) || d.Subject == "client" || d.Subject == "audit evidence" || d.Subject == disabledID {
			t.Errorf("rule %s fired again for %s inside its dedup window", d.Rule, d.Subject)
		}
	}
}

// A count below the threshold, or outside the window, does not fire.
func TestStage49114BelowThresholdOrOldEventsDoNotFire(t *testing.T) {
	now := time.Now()
	rule := SecurityDetectionRule{Key: "x", Threshold: 3, WindowMinutes: 10, actions: []string{"A"}, byActor: true}
	rows := []watchedAuditRow{
		{actor: "u1", action: "A", at: now.Add(-time.Minute)},
		{actor: "u1", action: "A", at: now.Add(-2 * time.Minute)},
		{actor: "u1", action: "A", at: now.Add(-30 * time.Minute)}, // outside the window
		{actor: "u2", action: "B", at: now.Add(-time.Minute)},      // another action
	}
	for _, h := range countWatchedRows(rows, rule, now) {
		if h.Count >= rule.Threshold {
			t.Fatalf("%s counted %d, only 2 are in the window", h.Subject, h.Count)
		}
	}
}

// Stage 49.12.3: sign out everywhere voids the user's tokens (credential
// version bump), needs a reason, and is audited; the export freeze is a
// setting with a reason, undone the same way.
func TestStage49123EmergencyControls(t *testing.T) {
	db.InitDB(testConnStr())
	id := "__em49123_" + stage4911Suffix()
	defer db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id)
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE details LIKE $1`, "%"+id+"%")
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code)
		VALUES ($1, $1, 'x', $2, 'Cashier', 'Active', 'HO')`, id, id+"@em.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := SignOutUserEverywhere("default", id, "admin", ""); err != ErrEmergencyReasonRequired {
		t.Fatalf("no reason: %v, want ErrEmergencyReasonRequired", err)
	}
	var before, after int
	db.DB.QueryRow(`SELECT credential_version FROM tenant_default.users WHERE id = $1`, id).Scan(&before)
	if _, err := SignOutUserEverywhere("default", id, "admin", "laptop stolen "+id); err != nil {
		t.Fatal(err)
	}
	db.DB.QueryRow(`SELECT credential_version FROM tenant_default.users WHERE id = $1`, id).Scan(&after)
	if after != before+1 {
		t.Fatalf("credential_version %d -> %d, want +1", before, after)
	}
	var audited int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.audit_logs WHERE status = 'SESSIONS_REVOKED' AND details LIKE $1`, "%"+id+"%").Scan(&audited)
	if audited != 1 {
		t.Fatalf("%d SESSIONS_REVOKED rows, want 1", audited)
	}

	defer SetSetting("default", SettingFreezeExports, "false", "test")
	if err := SetExportFreeze("default", true, "admin", "suspected leak "+id); err != nil {
		t.Fatal(err)
	}
	if !GetSettingBool("default", SettingFreezeExports) {
		t.Fatal("the freeze did not take")
	}
	if err := SetExportFreeze("default", false, "admin", "all clear "+id); err != nil {
		t.Fatal(err)
	}
	if GetSettingBool("default", SettingFreezeExports) {
		t.Fatal("undoing the freeze did not take")
	}
	db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = 'SECURITY_CONTROL' AND details LIKE $1`, "%"+id+"%")
}

// Stage 49.12.4: the forensic pack holds the window's audit rows with their
// signatures, a manifest whose SHA-256s match the files, a manifest
// signature, and refuses an over-long window.
func TestStage49124ForensicPackIsManifestedAndBounded(t *testing.T) {
	db.InitDB(testConnStr())
	action := "TEST_49124_" + stage4911Suffix()
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1 OR (action = 'EVIDENCE_EXPORTED' AND user_id = 'tester49124')`, action)
	LogAuditEvent("default", "tester49124", action, "SUCCESS", "in the window")
	from, to := time.Now().Add(-time.Hour), time.Now().Add(time.Minute)

	if _, _, err := BuildForensicPack("default", from, from.Add(40*24*time.Hour), "tester49124"); err != ErrForensicWindow {
		t.Fatalf("a 40-day window: %v, want ErrForensicWindow", err)
	}
	pack, manifest, err := BuildForensicPack("default", from, to, "tester49124")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(pack), int64(len(pack)))
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string][]byte{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		contents[f.Name] = b
	}
	if !bytes.Contains(contents["audit_logs.jsonl"], []byte(action)) {
		t.Fatal("the window's audit row is not in the pack")
	}
	for name, want := range manifest.Files {
		sum := sha256Hex(contents[name])
		if sum != want {
			t.Fatalf("%s: manifest says %s, file hashes to %s", name, want, sum)
		}
	}
	var onDisk ForensicManifest
	if err := json.Unmarshal(contents["manifest.json"], &onDisk); err != nil {
		t.Fatal(err)
	}
	if string(contents["manifest.sig"]) != SignForensicManifest(contents["manifest.json"]) {
		t.Fatal("manifest.sig does not verify")
	}
	var exported int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.audit_logs WHERE action = 'EVIDENCE_EXPORTED' AND user_id = 'tester49124'`).Scan(&exported)
	if exported != 1 {
		t.Fatalf("%d EVIDENCE_EXPORTED rows, want 1 (collecting evidence is itself evidence)", exported)
	}
}
