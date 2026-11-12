package engines

import (
	"context"
	"crypto/x509"
	"custom_erp/db"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Stage 49.11.2: a security event written for a request carries its source
// address, a device summary and the policy it ran under; the signature still
// covers exactly what it covered before; a background write carries none.
func TestStage49112SecurityEventCarriesSourceDeviceAndPolicy(t *testing.T) {
	db.InitDB(testConnStr())
	action := "TEST_49112S_" + stage4911Suffix()
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1`, action)
	corr := "corr-49112s-" + stage4911Suffix()
	ctx := WithCorrelationID(context.Background(), corr)
	ctx = WithRequestSource(ctx, " 203.0.113.9 ", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	ctx = WithRequestPolicy(ctx, "admin.audit-logs", "pabc123def456")
	LogAuditEventCtx(ctx, "default", "tester49112s", action, "SUCCESS", "with a source")
	LogAuditEvent("default", "system", action, "BACKGROUND", "no request")

	var ip, device, capability, policy, details, sig, gotCorr string
	var at time.Time
	if err := db.DB.QueryRow(`SELECT COALESCE(source_ip, ''), COALESCE(device, ''), COALESCE(capability, ''), COALESCE(policy_version, ''),
		details, signature, COALESCE(correlation_id, ''), created_at FROM tenant_default.audit_logs WHERE action = $1 AND status = 'SUCCESS'`, action).
		Scan(&ip, &device, &capability, &policy, &details, &sig, &gotCorr, &at); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if ip != "203.0.113.9" || device != "Chrome on Windows" || capability != "admin.audit-logs" || policy != "pabc123def456" {
		t.Fatalf("source %q, device %q, capability %q, policy %q", ip, device, capability, policy)
	}
	if want := SignAuditRow("default", "tester49112s", action, "SUCCESS", details, "", "", gotCorr, at); sig != want {
		t.Fatal("the signature changed meaning: it must cover the row as before, not the clearable address")
	}
	var bgIP, bgDevice *string
	if err := db.DB.QueryRow(`SELECT source_ip, device FROM tenant_default.audit_logs WHERE action = $1 AND status = 'BACKGROUND'`, action).Scan(&bgIP, &bgDevice); err != nil {
		t.Fatal(err)
	}
	if bgIP != nil || bgDevice != nil {
		t.Fatal("a background write recorded a source it does not have")
	}
	// A value that is not an address is not stored as one.
	if src := RequestSourceFromContext(WithRequestSource(context.Background(), "not-an-ip", "")); src.Address != "" {
		t.Fatalf("stored %q as an address", src.Address)
	}
}

// The device summary is a family and a platform - never versions, never the
// raw string.
func TestStage49112DeviceSummary(t *testing.T) {
	for ua, want := range map[string]string{
		"": "",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0":               "Edge on Windows",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1": "Safari on iOS",
		"Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Mobile Safari/537.36":                      "Chrome on Android",
		"Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0":                                                                  "Firefox on Linux",
		"curl/8.4.0":                    "curl",
		"Shopify-Captain-Hook":          "Shopify",
		"something nobody has heard of": "Other",
	} {
		if got := DeviceSummary(ua); got != want {
			t.Errorf("DeviceSummary(%.40q) = %q, want %q", ua, got, want)
		}
		if got := DeviceSummary(ua); len(got) > maxDeviceBytes || strings.ContainsAny(got, "0123456789") {
			t.Errorf("DeviceSummary(%.40q) = %q: too long or carries a version", ua, got)
		}
	}
}

// The address and device are registered personal data with a purpose and a
// retention, on columns that exist, and the retention sweep clears them on
// old events - leaving the event, its signature and recent addresses alone.
func TestStage49112SourceAndDeviceAreRegisteredAndCleared(t *testing.T) {
	db.InitDB(testConnStr())
	for _, key := range PersonalDataSystemColumns() {
		row := personalDataSystemColumns[key]
		parts := strings.SplitN(key, ".", 2)
		var exists bool
		if err := db.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'tenant_default' AND table_name = $1 AND column_name = $2)`,
			parts[0], parts[1]).Scan(&exists); err != nil || !exists {
			t.Errorf("%s is registered but tenant_default has no such column (%v)", key, err)
		}
		if row.Purpose == "" || row.Retention == "" || row.Readers == "" || !personalDataKinds[row.Kind] || !personalDataBases[row.LegalBasis] || row.LegalBasis == BasisNotPersonal {
			t.Errorf("%s: incomplete register row %+v", key, row)
		}
	}
	if len(personalDataSystemColumns) < 2 {
		t.Fatal("the address and the device must both be registered")
	}

	action := "TEST_49112R_" + stage4911Suffix()
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1`, action)
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = 'PERSONAL_DATA_RETENTION' AND details LIKE '%(49.11.2)%'`)
	old := utcStamp(time.Now().AddDate(0, 0, -(auditSourceRetentionDays + 5)))
	recent := utcStamp(time.Now().AddDate(0, 0, -(auditSourceRetentionDays - 5)))
	for _, at := range []string{old, recent} {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.audit_logs (user_id, action, status, details, created_at, signature, sig_version, source_ip, device)
			VALUES ('tester', $1, 'SUCCESS', 'x', $2::timestamp, 'sig-kept', 'v2-hmac-sha256', '198.51.100.20', 'Chrome on Windows')`, action, at); err != nil {
			t.Fatal(err)
		}
	}
	cleared, err := SweepPersonalFieldRetention("default")
	if err != nil {
		t.Fatal(err)
	}
	if cleared["audit_logs.source_ip"] < 1 {
		t.Fatalf("the sweep cleared nothing: %v", cleared)
	}
	rows, err := db.DB.Query(`SELECT created_at < $2::timestamp, source_ip IS NULL, device IS NULL, signature FROM tenant_default.audit_logs WHERE action = $1`, action, recent)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var isOld, ipGone, deviceGone bool
		var sig string
		if err := rows.Scan(&isOld, &ipGone, &deviceGone, &sig); err != nil {
			t.Fatal(err)
		}
		n++
		if isOld != ipGone || isOld != deviceGone {
			t.Errorf("old=%v: address cleared %v, device cleared %v", isOld, ipGone, deviceGone)
		}
		if sig != "sig-kept" {
			t.Error("the sweep touched the event itself")
		}
	}
	if n != 2 {
		t.Fatalf("%d rows, want 2 - the sweep must clear fields, never delete events", n)
	}
}

// Stage 49.11.4: the webhook and stock-adjustment rules fire at their
// threshold and not one row below it; look-alikes do not count.
func TestStage49114WebhookAndStockRulesCrossTheirThresholds(t *testing.T) {
	now := time.Now()
	for _, key := range []string{"webhook_signature_surge", "webhook_replay_surge", "stock_adjustment_burst"} {
		rule := stage4954Rule(t, key)
		action, status := rule.actions[len(rule.actions)-1], "SUCCESS"
		if len(rule.statuses) > 0 {
			status = rule.statuses[0]
		}
		in := now.Add(-time.Duration(rule.WindowMinutes) * time.Minute / 2)
		rows := []watchedAuditRow{
			{actor: "u1", action: action, status: "OTHER", at: in},                                                         // another outcome
			{actor: "u1", action: action, status: status, at: now.Add(-time.Duration(rule.WindowMinutes+5) * time.Minute)}, // too old
			{actor: "u1", action: "SOMETHING_ELSE", status: status, at: in},
		}
		if len(rule.statuses) == 0 {
			rows = rows[1:] // a rule with no status filter counts any outcome
		}
		for i := 0; i < rule.Threshold-1; i++ {
			rows = append(rows, watchedAuditRow{actor: "u1", action: action, status: status, at: in})
		}
		if rule.byActor {
			rows = append(rows, watchedAuditRow{actor: "u2", action: action, status: status, at: in}) // someone else's
		}
		top := func() int {
			best := 0
			for _, h := range countWatchedRows(rows, rule, now) {
				best = max(best, h.Count)
			}
			return best
		}
		if got := top(); got >= rule.Threshold {
			t.Errorf("%s: %d counted one row below its threshold of %d", key, got, rule.Threshold)
		}
		rows = append(rows, watchedAuditRow{actor: "u1", action: action, status: status, at: in})
		if got := top(); got != rule.Threshold {
			t.Errorf("%s: %d counted at its threshold, want %d", key, got, rule.Threshold)
		}
	}
	if r := stage4954Rule(t, "stock_adjustment_burst"); !containsString(r.actions, "POST_CYCLE_COUNT_ADJUSTMENT") {
		t.Error("a posted cycle-count variance moves stock and must count toward the adjustment burst")
	}
}

// Stage 49.11.4: a webhook refusal is a signed row naming the source and the
// address, and a flood is capped per client and outcome per window.
func TestStage49114WebhookEventsAreRecordedAndCapped(t *testing.T) {
	db.InitDB(testConnStr())
	src := "shopify:t49114" + stage4911Suffix()
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1 AND details = $2`, InboundWebhookAction, "source="+src)
	ctx := WithRequestSource(context.Background(), "192.0.2.44", "Shopify-Captain-Hook")
	NoteInboundWebhookEvent(ctx, "default", src, WebhookSignatureRefused)
	NoteInboundWebhookEvent(ctx, "default", src, "SOMETHING_ELSE") // not an outcome it records
	var n int
	var ip string
	if err := db.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(source_ip), '') FROM tenant_default.audit_logs WHERE action = $1 AND details = $2 AND status = $3 AND signature IS NOT NULL`,
		InboundWebhookAction, "source="+src, WebhookSignatureRefused).Scan(&n, &ip); err != nil {
		t.Fatal(err)
	}
	if n != 1 || ip != "192.0.2.44" {
		t.Fatalf("%d signed rows from %q, want 1 from 192.0.2.44", n, ip)
	}

	at := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	allowed := 0
	for i := 0; i < webhookEventRowCap+25; i++ {
		if webhookEventAllowed("cap-test", WebhookReplayed, at) {
			allowed++
		}
	}
	if allowed != webhookEventRowCap {
		t.Fatalf("%d rows allowed in one window, cap is %d", allowed, webhookEventRowCap)
	}
	if !webhookEventAllowed("cap-test", WebhookReplayed, at.Add(webhookEventWindow)) {
		t.Fatal("the next window must start a fresh count")
	}
}

// Stage 49.11.7: the certificate's expiry is read from the handshake - also
// when it does not verify - and an address inside the network is refused by
// the outbound guard.
func TestStage49117CertificateExpiryIsRead(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, srv.Listener.Addr().String())
	}
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	want := srv.Certificate().NotAfter.UTC()
	now := time.Now()

	st := checkCertificate(context.Background(), "example.com", dial, roots, now)
	if st.Problem != "" || st.NotAfter == nil || !st.NotAfter.Equal(want) || st.DaysLeft <= certExpiryAlertDays || st.Expiring {
		t.Fatalf("trusted certificate: %+v", st)
	}
	soon := checkCertificate(context.Background(), "example.com", dial, roots, want.Add(-5*24*time.Hour))
	if !soon.Expiring || soon.DaysLeft != 5 {
		t.Fatalf("five days before expiry: %+v", soon)
	}
	untrusted := checkCertificate(context.Background(), "example.com", dial, nil, now)
	if !strings.HasPrefix(untrusted.Problem, "not trusted") || untrusted.NotAfter == nil || !untrusted.NotAfter.Equal(want) {
		t.Fatalf("an untrusted certificate must still show its date: %+v", untrusted)
	}
	if !outboundAllowPrivate {
		inside := checkCertificate(context.Background(), "localhost", outboundDial, nil, now)
		if inside.NotAfter != nil || !strings.Contains(inside.Problem, "non-public") {
			t.Fatalf("the guard must refuse an address inside the network: %+v", inside)
		}
	}
}

// Stage 49.11.7: a client's backup is the later of the whole-database nightly
// and an export of its own schema; older than the limit, or none, is stale.
func TestStage49117ClientBackupFreshness(t *testing.T) {
	db.InitDB(testConnStr())
	own := "tenant_bk49117_" + stage4911Suffix()
	if _, err := db.DB.Exec(`INSERT INTO public.backup_runs (finished_at, scope, tenant_schema, outcome) VALUES (NOW() - INTERVAL '2 hours', 'tenant', $1, 'Success')`, own); err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec(`DELETE FROM public.backup_runs WHERE tenant_schema = $1`, own)
	now := time.Now()
	nightly := now.Add(-40 * time.Hour)
	clients := []ControlPanelClient{{Schema: own}, {Schema: "tenant_bk49117_none"}}
	fillClientBackups(clients, &nightly, now)
	if clients[0].LastBackupAt == nil || now.Sub(*clients[0].LastBackupAt) > 3*time.Hour || clients[0].BackupStale {
		t.Fatalf("own export 2h ago: %+v", clients[0])
	}
	if clients[1].LastBackupAt == nil || !clients[1].LastBackupAt.Equal(nightly) || !clients[1].BackupStale {
		t.Fatalf("only a 40h-old nightly: %+v", clients[1])
	}
	none := []ControlPanelClient{{Schema: "tenant_bk49117_none"}}
	fillClientBackups(none, nil, now)
	if none[0].LastBackupAt != nil || !none[0].BackupStale {
		t.Fatalf("no backup at all must read stale: %+v", none[0])
	}
}
