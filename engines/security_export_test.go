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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// exportRig points the export at a local receiver and cleans up after itself.
func exportRig(t *testing.T) (action string, cleanup func()) {
	t.Helper()
	db.InitDB(testConnStr())
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "1")
	prevPriv, prevCheck := outboundAllowPrivate, securityExportCheckURL
	outboundAllowPrivate = true
	securityExportCheckURL = func(string) error { return nil }
	action = "TEST_49116_" + stage4911Suffix()
	return action, func() {
		outboundAllowPrivate, securityExportCheckURL = prevPriv, prevCheck
		db.DB.Exec(`DELETE FROM tenant_default.security_export_config`)
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1 OR action LIKE 'SECURITY_EXPORT_%'`, action)
		db.DB.Exec(`DELETE FROM tenant_default.audit_archives WHERE file_path = 'test-49116'`)
	}
}

func oldEvent(t *testing.T, action string, details string) {
	t.Helper()
	LogAuditEvent("default", "tester49116", action, "SUCCESS", details)
	if _, err := db.DB.Exec(`UPDATE tenant_default.audit_logs SET created_at = (now() AT TIME ZONE 'UTC') - interval '5 minutes' WHERE action = $1 OR action LIKE 'SECURITY_EXPORT_%'`, action); err != nil {
		t.Fatal(err)
	}
}

const exportSecret = "export-test-key-1234" // under 24 chars, the secret scanner's floor; the save needs 16

// 49.11.6: a batch is signed, carries only this client's events, moves the
// cursor only after a 2xx, and a failing destination keeps the cursor and backs off.
func TestStage49116ExportBatchSignedCursorOnlyOnSuccess(t *testing.T) {
	action, cleanup := exportRig(t)
	defer cleanup()
	var status atomic.Int32
	status.Store(500)
	var gotBody []byte
	var gotSig, gotTS string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig, gotTS = r.Header.Get("X-Webhook-Signature"), r.Header.Get("X-Webhook-Timestamp")
		w.WriteHeader(int(status.Load()))
	}))
	defer srv.Close()

	if err := SaveSecurityExport("default", "tester49116", srv.URL, exportSecret, true); err != nil {
		t.Fatalf("save: %v", err)
	}
	oldEvent(t, action, "password=hunter2 something happened")

	now := time.Now()
	if n, err := RunSecurityExport("default", now); err == nil || n != 0 {
		t.Fatalf("a 500 must be an error with nothing counted, got n=%d err=%v", n, err)
	}
	st, _ := GetSecurityExportStatus("default")
	if st.Failures != 1 || st.ExportedTotal != 0 || st.LastError == "" || st.NextAttemptAt == nil {
		t.Fatalf("failure not recorded with backoff: %+v", st)
	}
	// Inside the backoff window nothing is attempted.
	if n, err := RunSecurityExport("default", now.Add(time.Second)); n != 0 || err != nil {
		t.Fatalf("inside backoff: n=%d err=%v", n, err)
	}

	status.Store(200)
	n, err := RunSecurityExport("default", now.Add(2*time.Hour))
	if err != nil || n < 1 {
		t.Fatalf("after recovery: n=%d err=%v", n, err)
	}
	mac := hmac.New(sha256.New, []byte(exportSecret))
	mac.Write(gotBody)
	if gotSig != "sha256="+hex.EncodeToString(mac.Sum(nil)) || gotTS == "" {
		t.Fatal("the batch is not signed with the saved secret")
	}
	var payload struct {
		Format string `json:"format"`
		Tenant string `json:"tenant"`
		Events []struct {
			Action  string `json:"action"`
			Details string `json:"details"`
		} `json:"events"`
	}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	// the receiver sees the webhook envelope; accept either direct or wrapped data
	if payload.Format == "" {
		var wrapped struct {
			Data json.RawMessage `json:"payload"`
		}
		_ = json.Unmarshal(gotBody, &wrapped)
		_ = json.Unmarshal(wrapped.Data, &payload)
	}
	found := false
	for _, e := range payload.Events {
		if e.Action == action {
			found = true
			if strings.Contains(e.Details, "hunter2") {
				t.Fatal("a secret reached the destination")
			}
		}
	}
	if !found || payload.Tenant != "default" {
		t.Fatalf("event not in the batch for this client: %+v", payload)
	}
	// Cursor moved: a second pass sends nothing.
	if n, _ := RunSecurityExport("default", now.Add(3*time.Hour)); n != 0 {
		t.Fatalf("cursor did not advance, resent %d", n)
	}
	st, _ = GetSecurityExportStatus("default")
	if st.Failures != 0 || st.ExportedTotal < 1 || st.HasSecret != true {
		t.Fatalf("status after success: %+v", st)
	}
	raw, _ := json.Marshal(st)
	if strings.Contains(string(raw), exportSecret) {
		t.Fatal("the secret is in the status")
	}
}

// Events newer than the settle delay wait for the next pass.
func TestStage49116ExportWaitsForSettleDelay(t *testing.T) {
	action, cleanup := exportRig(t)
	defer cleanup()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	if err := SaveSecurityExport("default", "tester49116", srv.URL, exportSecret, true); err != nil {
		t.Fatal(err)
	}
	LogAuditEvent("default", "tester49116", action, "SUCCESS", "fresh")
	if n, _ := RunSecurityExport("default", time.Now()); n != 0 {
		t.Fatalf("a row younger than the settle delay was sent (%d)", n)
	}
}

// Archived-before-sent events are reported, not skipped quietly.
func TestStage49116ExportReportsArchiveGap(t *testing.T) {
	_, cleanup := exportRig(t)
	defer cleanup()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	if err := SaveSecurityExport("default", "tester49116", srv.URL, exportSecret, true); err != nil {
		t.Fatal(err)
	}
	var cur int64
	db.DB.QueryRow(`SELECT cursor_seq FROM tenant_default.security_export_config WHERE id = 1`).Scan(&cur)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.audit_archives (checkpoint_id, from_seq, to_seq, row_count, row_digest, file_path, file_sha256, manifest_signature, sig_version, compressed_bytes)
		VALUES (gen_random_uuid(), $1, $2, 5, 'd', 'test-49116', 's', 'm', 'v1', 1)`, cur+1, cur+5000000); err != nil {
		t.Fatal(err)
	}
	_, _ = RunSecurityExport("default", time.Now())
	st, _ := GetSecurityExportStatus("default")
	if !strings.Contains(st.GapNote, "archived before they were sent") {
		t.Fatalf("gap not reported: %q", st.GapNote)
	}
	var n int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.audit_logs WHERE action = 'SECURITY_EXPORT_GAP'`).Scan(&n)
	if n < 1 {
		t.Fatal("no SECURITY_EXPORT_GAP audit row")
	}
}

// Off by default; refused without outside calls; sandbox never; secret required.
func TestStage49116ExportRefusalsAndDefaults(t *testing.T) {
	_, cleanup := exportRig(t)
	defer cleanup()
	st, err := GetSecurityExportStatus("default")
	if err != nil || st.Enabled || st.URL != "" {
		t.Fatalf("export must be off by default: %+v %v", st, err)
	}
	if err := SaveSecurityExport("default", "t", "https://example.com/x", "", true); err == nil {
		t.Fatal("enabling without a signing secret must be refused")
	}
	if err := SaveSecurityExport("default", "t", "https://example.com/x", "short", true); err == nil {
		t.Fatal("a short secret must be refused")
	}
	// the real address guard rejects http and credentials
	securityExportCheckURL = validateWebhookURL
	if err := ValidateSecurityExportURL("http://example.com/x"); err == nil {
		t.Fatal("plain http must be refused")
	}
	if err := ValidateSecurityExportURL("https://user:pw@example.com/x"); err == nil {
		t.Fatal("credentials in the address must be refused")
	}
	// outside calls frozen: nothing is sent
	securityExportCheckURL = func(string) error { return nil }
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(200) }))
	defer srv.Close()
	if err := SaveSecurityExport("default", "t", srv.URL, exportSecret, true); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "")
	t.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "1")
	if _, err := TestSecurityExport("default", "t"); err == nil {
		t.Fatal("the test send must be refused while outside calls are off")
	}
	_, _ = RunSecurityExport("default", time.Now())
	if hits.Load() != 0 {
		t.Fatal("something was sent while outside calls were off")
	}
}

// exportReceiver records every batch it is sent.
func exportReceiver(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p struct {
			Events []struct {
				Details string `json:"details"`
			} `json:"events"`
			Payload struct {
				Events []struct {
					Details string `json:"details"`
				} `json:"events"`
			} `json:"payload"`
		}
		_ = json.Unmarshal(body, &p)
		mu.Lock()
		for _, e := range append(p.Events, p.Payload.Events...) {
			got = append(got, e.Details)
		}
		mu.Unlock()
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string{}, got...)
	}
}

func exportHas(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// A row inside the settle delay holds back everything after it: the cursor only
// moves forward, so passing it would lose it for good.
func TestStage49116ExportFreshRowHoldsBackLaterRows(t *testing.T) {
	action, cleanup := exportRig(t)
	defer cleanup()
	srv, sent := exportReceiver(t)
	if err := SaveSecurityExport("default", "tester49116", srv.URL, exportSecret, true); err != nil {
		t.Fatal(err)
	}
	LogAuditEvent("default", "tester49116", action, "SUCCESS", "fresh-first")
	oldEvent(t, action+"-later", "old-second")
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = $1`, action+"-later")
	db.DB.Exec(`UPDATE tenant_default.audit_logs SET created_at = now() AT TIME ZONE 'UTC' WHERE action = $1`, action)
	if _, err := RunSecurityExport("default", time.Now()); err != nil {
		t.Fatal(err)
	}
	if exportHas(sent(), "old-second") || exportHas(sent(), "fresh-first") {
		t.Fatalf("sent past a row still settling: %v", sent())
	}
	if _, err := RunSecurityExport("default", time.Now().Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got := sent()
	if !exportHas(got, "fresh-first") || !exportHas(got, "old-second") {
		t.Fatalf("both must arrive once settled: %v", got)
	}
}

// A writer still open may hold the next seq: the walk stops at the hole and the
// row arrives after the commit.
func TestStage49116ExportStopsAtAWriterStillOpen(t *testing.T) {
	action, cleanup := exportRig(t)
	defer cleanup()
	srv, sent := exportReceiver(t)
	if err := SaveSecurityExport("default", "tester49116", srv.URL, exportSecret, true); err != nil {
		t.Fatal(err)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO tenant_default.audit_logs (user_id, action, status, details, created_at)
		VALUES ('tester49116', $1, 'SUCCESS', 'held-open', (now() AT TIME ZONE 'UTC') - interval '5 minutes')`, action); err != nil {
		t.Fatal(err)
	}
	oldEvent(t, action, "committed-later")
	if _, err := RunSecurityExport("default", time.Now()); err != nil {
		t.Fatal(err)
	}
	if exportHas(sent(), "committed-later") {
		t.Fatalf("walked past an open writer: %v", sent())
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := RunSecurityExport("default", time.Now()); err != nil {
		t.Fatal(err)
	}
	got := sent()
	if !exportHas(got, "held-open") || !exportHas(got, "committed-later") {
		t.Fatalf("both must arrive after the commit: %v", got)
	}
}

// A hole left by a rollback is walked past once no writer is open.
func TestStage49116ExportWalksPastARolledBackSeq(t *testing.T) {
	action, cleanup := exportRig(t)
	defer cleanup()
	srv, sent := exportReceiver(t)
	if err := SaveSecurityExport("default", "tester49116", srv.URL, exportSecret, true); err != nil {
		t.Fatal(err)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO tenant_default.audit_logs (user_id, action, status, details) VALUES ('tester49116', $1, 'SUCCESS', 'rolled-back')`, action); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	oldEvent(t, action, "after-the-hole")
	if _, err := RunSecurityExport("default", time.Now()); err != nil {
		t.Fatal(err)
	}
	if !exportHas(sent(), "after-the-hole") {
		t.Fatalf("a rollback hole stopped the export: %v", sent())
	}
}

// The archive run can skip a failed range and take a later one first: live rows
// before it are sent, and only then is the archived range reported.
func TestStage49116ExportLaterArchiveDoesNotLoseLiveRows(t *testing.T) {
	action, cleanup := exportRig(t)
	defer cleanup()
	srv, sent := exportReceiver(t)
	if err := SaveSecurityExport("default", "tester49116", srv.URL, exportSecret, true); err != nil {
		t.Fatal(err)
	}
	oldEvent(t, action, "live-before-archive")
	var last int64
	db.DB.QueryRow(`SELECT MAX(seq) FROM tenant_default.audit_logs`).Scan(&last)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.audit_archives (checkpoint_id, from_seq, to_seq, row_count, row_digest, file_path, file_sha256, manifest_signature, sig_version, compressed_bytes)
		VALUES (gen_random_uuid(), $1, $2, 5, 'd', 'test-49116', 's', 'm', 'v1', 1)`, last+1000, last+2000); err != nil {
		t.Fatal(err)
	}
	if _, err := RunSecurityExport("default", time.Now()); err != nil {
		t.Fatal(err)
	}
	st, _ := GetSecurityExportStatus("default")
	if !exportHas(sent(), "live-before-archive") || st.GapNote != "" {
		t.Fatalf("live rows first, no note yet: sent %v note %q", sent(), st.GapNote)
	}
	if _, err := RunSecurityExport("default", time.Now()); err != nil {
		t.Fatal(err)
	}
	st, _ = GetSecurityExportStatus("default")
	if !strings.Contains(st.GapNote, "archived before they were sent") {
		t.Fatalf("the range is reported once nothing live is before it: %q", st.GapNote)
	}
}

// Saving while on keeps the place, even at cursor 0.
func TestStage49116ResaveKeepsTheCursor(t *testing.T) {
	_, cleanup := exportRig(t)
	defer cleanup()
	if err := SaveSecurityExport("default", "tester49116", "https://logs.example/in", exportSecret, true); err != nil {
		t.Fatal(err)
	}
	db.DB.Exec(`UPDATE tenant_default.security_export_config SET cursor_seq = 0 WHERE id = 1`)
	if err := SaveSecurityExport("default", "tester49116", "https://logs.example/in2", "", true); err != nil {
		t.Fatal(err)
	}
	var cur int64
	db.DB.QueryRow(`SELECT cursor_seq FROM tenant_default.security_export_config WHERE id = 1`).Scan(&cur)
	if cur != 0 {
		t.Fatalf("a re-save while on must not jump the cursor to %d: rows would be lost", cur)
	}
}

// A token in the destination's query string stays out of the saved error.
func TestStage49116ExportErrorDropsTheAddressQuery(t *testing.T) {
	action, cleanup := exportRig(t)
	defer cleanup()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL + "/in?token=sekret-query-token"
	srv.Close()
	if err := SaveSecurityExport("default", "tester49116", addr, exportSecret, true); err != nil {
		t.Fatal(err)
	}
	oldEvent(t, action, "x")
	_, err := RunSecurityExport("default", time.Now())
	if err == nil || strings.Contains(err.Error(), "sekret-query-token") {
		t.Fatalf("err = %v", err)
	}
	st, _ := GetSecurityExportStatus("default")
	if st.LastError == "" || strings.Contains(st.LastError, "sekret-query-token") {
		t.Fatalf("last_error = %q", st.LastError)
	}
}
