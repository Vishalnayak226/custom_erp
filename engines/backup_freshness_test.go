package engines

import (
	"custom_erp/db"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Stage 43.2 - the stale/missing-backup alert, reading Stage 56.9's
// public.backup_runs record instead of a backup folder.
//
// 26.11.3 found that production had never run a nightly backup at all, and
// nothing noticed, because a job that never fires produces no failure to
// report. These tests cover the states that silence used to look identical to.

const backupTestHost = "backup-freshness-test"

// backupRunsTestDB connects and clears this test's own rows before and after.
// It skips if the shared database holds rows something else recorded (a real
// backup.sh run against the dev database), because every case below depends on
// knowing exactly which runs exist.
func backupRunsTestDB(t *testing.T) {
	t.Helper()
	db.InitDB(testConnStr())
	clear := func() { db.DB.Exec(`DELETE FROM public.backup_runs WHERE host = $1`, backupTestHost) }
	clear()
	t.Cleanup(clear)
	var foreign int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM public.backup_runs WHERE host <> $1`, backupTestHost).Scan(&foreign); err != nil {
		t.Fatalf("public.backup_runs is not readable - apply db/migrations_stage56_9_backup_runs.sql: %v", err)
	}
	if foreign > 0 {
		t.Skipf("public.backup_runs holds %d run(s) not written by this test; these cases need a table with no other runs", foreign)
	}
}

func recordBackupRun(t *testing.T, scope, outcome string, finished time.Time) {
	t.Helper()
	var tenant interface{}
	if scope == "tenant" {
		tenant = "tenant_acme"
	}
	if _, err := db.DB.Exec(`
		INSERT INTO public.backup_runs (scope, tenant_schema, outcome, file_name, size_bytes, duration_ms, host, started_at, finished_at)
		VALUES ($1, $2, $3, 'custom_erp_fixture.dump.enc', 1048576, 4200, $4, $5, $5)`,
		scope, tenant, outcome, backupTestHost, finished); err != nil {
		t.Fatalf("recording fixture run: %v", err)
	}
}

func TestBackupFreshness(t *testing.T) {
	backupRunsTestDB(t)

	t.Run("missing outside production is not applicable, never stale", func(t *testing.T) {
		t.Setenv("ENV", "development")
		fresh := CheckBackupFreshness()
		if fresh.Configured || fresh.Found {
			t.Errorf("a dev box with no recorded backup must be not-applicable, got configured=%v found=%v", fresh.Configured, fresh.Found)
		}
		if findings := backupRecordFindings(); len(findings) != 0 {
			t.Errorf("no backup finding is expected outside production, got %v", findings)
		}
	})

	t.Run("missing in production is a loud finding, not silence", func(t *testing.T) {
		t.Setenv("ENV", "production")
		fresh := CheckBackupFreshness()
		if !fresh.Configured || fresh.Found {
			t.Fatalf("production with no recorded backup must be configured-but-missing, got configured=%v found=%v", fresh.Configured, fresh.Found)
		}
		findings := backupRecordFindings()
		if len(findings) != 1 || findings[0].ID != "SB-026" {
			t.Fatalf("production with no recorded backup must raise SB-026, got %v", findings)
		}
		found := false
		for _, f := range ProductionSecurityBaseline() {
			found = found || f.ID == "SB-026"
		}
		if !found {
			t.Error("SB-026 must reach the boot-time baseline that is logged at every start")
		}
	})

	t.Run("a tenant export does not count as the nightly backup", func(t *testing.T) {
		t.Setenv("ENV", "production")
		recordBackupRun(t, "tenant", "Success", time.Now())
		if CheckBackupFreshness().Found {
			t.Error("only scope 'database' runs may satisfy the nightly check (26.1.6)")
		}
	})

	t.Run("a failed run does not count, and is reported", func(t *testing.T) {
		t.Setenv("ENV", "development")
		recordBackupRun(t, "database", "Failed", time.Now())
		fresh := CheckBackupFreshness()
		if fresh.Found {
			t.Error("a failed run must not satisfy the freshness check")
		}
		if !fresh.Configured || fresh.LastOutcome != "Failed" {
			t.Errorf("a recorded failed run proves a job exists and must be reported, got configured=%v last=%q", fresh.Configured, fresh.LastOutcome)
		}
	})

	t.Run("fresh: the newest successful whole-database run wins", func(t *testing.T) {
		t.Setenv("ENV", "production")
		recordBackupRun(t, "database", "Success", time.Now().Add(-72*time.Hour))
		recordBackupRun(t, "database", "Success", time.Now().Add(-2*time.Hour))
		fresh := CheckBackupFreshness()
		if !fresh.Found {
			t.Fatal("a whole-database run must be found")
		}
		if fresh.AgeHours < 1.9 || fresh.AgeHours > 3 {
			t.Errorf("age must come from the newest run (~2h), got %.1fh", fresh.AgeHours)
		}
		if fresh.SizeBytes != 1048576 || fresh.DurationMS != 4200 {
			t.Errorf("size and duration must come from the record, got %d bytes / %d ms", fresh.SizeBytes, fresh.DurationMS)
		}
		if findings := backupRecordFindings(); len(findings) != 0 {
			t.Errorf("a fresh backup must clear SB-026, got %v", findings)
		}
	})
}

// TestBackupStaleAlertReachesTheWebhook is the end-to-end claim that matters:
// a stale or missing nightly backup produces an actual POST to the ops
// webhook, not just an internal state change.
func TestBackupStaleAlertReachesTheWebhook(t *testing.T) {
	backupRunsTestDB(t)
	received := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		select {
		case received <- payload.Text:
		default:
		}
	}))
	defer srv.Close()

	t.Setenv("OPS_ALERT_WEBHOOK_URL", srv.URL)
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "1") // Stage 47.0.5 Gate 0: opt this real-send test in deliberately
	resetThrottle := func() {
		backupAlertState.Lock()
		backupAlertState.lastAlertAt = time.Time{}
		backupAlertState.Unlock()
	}
	expectAlert := func(want string) {
		t.Helper()
		select {
		case text := <-received:
			if !strings.Contains(text, want) {
				t.Errorf("alert must say %q, got %q", want, text)
			}
			if strings.Contains(text, "fixture.dump.enc") {
				t.Errorf("the external alert must not carry the backup file name: %q", text)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("expected an ops alert containing %q", want)
		}
	}

	// Missing, production: nothing recorded at all.
	t.Setenv("ENV", "production")
	resetThrottle()
	checkBackupAge(36 * time.Hour)
	expectAlert("no successful whole-database backup is recorded")

	// Stale: the only success is 72h old and the latest run failed.
	recordBackupRun(t, "database", "Success", time.Now().Add(-72*time.Hour))
	recordBackupRun(t, "database", "Failed", time.Now().Add(-1*time.Hour))
	resetThrottle()
	checkBackupAge(36 * time.Hour)
	expectAlert("latest recorded run FAILED")

	// And it must not alert again on the next tick - a channel that fires every
	// poll interval is one people mute.
	checkBackupAge(36 * time.Hour)
	select {
	case text := <-received:
		t.Errorf("stale-backup alerts must be throttled to one per window, got a second: %q", text)
	case <-time.After(500 * time.Millisecond):
	}

	// Fresh: no alert at all.
	recordBackupRun(t, "database", "Success", time.Now().Add(-1*time.Minute))
	resetThrottle()
	checkBackupAge(36 * time.Hour)
	select {
	case text := <-received:
		t.Errorf("a fresh backup must not alert, got %q", text)
	case <-time.After(500 * time.Millisecond):
	}
}
