package engines

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Stage 43.2 - the stale/missing-backup alert.
//
// 26.11.3 found that production had never run a nightly backup at all, and
// nothing noticed, because a job that never fires produces no failure to
// report. These tests cover the states that silence used to look identical to.
func TestBackupFreshness(t *testing.T) {
	t.Run("no backup directory means not applicable, never stale", func(t *testing.T) {
		t.Setenv("BACKUP_DIR", filepath.Join(t.TempDir(), "does-not-exist"))
		fresh := CheckBackupFreshness()
		if fresh.Configured {
			t.Error("a machine with no backup directory must report Configured=false, or every dev box alarms forever")
		}
	})

	t.Run("an empty backup directory is detected as missing", func(t *testing.T) {
		t.Setenv("BACKUP_DIR", t.TempDir())
		fresh := CheckBackupFreshness()
		if !fresh.Configured {
			t.Fatal("an existing directory must count as configured")
		}
		if fresh.Found {
			t.Error("an empty directory must not report a backup as found")
		}
	})

	t.Run("a tenant export does not count as the nightly backup", func(t *testing.T) {
		// 26.1.6's on-demand per-tenant exports land in the same directory.
		// Counting one as "the nightly ran" would mask a dead cron.
		dir := t.TempDir()
		t.Setenv("BACKUP_DIR", dir)
		writeFixture(t, dir, "tenant_acme_20260812T020000Z.dump.enc", time.Now())
		if CheckBackupFreshness().Found {
			t.Error("only custom_erp_* whole-database backups may satisfy the nightly check")
		}
	})

	t.Run("the newest whole-database backup wins", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("BACKUP_DIR", dir)
		old := time.Now().Add(-72 * time.Hour)
		recent := time.Now().Add(-2 * time.Hour)
		writeFixture(t, dir, "custom_erp_20260809T020000Z.dump.enc", old)
		writeFixture(t, dir, "custom_erp_20260812T020000Z.dump.enc", recent)
		fresh := CheckBackupFreshness()
		if !fresh.Found {
			t.Fatal("a whole-database backup must be found")
		}
		if fresh.AgeHours > 3 {
			t.Errorf("age must come from the newest backup, got %.1fh", fresh.AgeHours)
		}
	})
}

// TestBackupStaleAlertReachesTheWebhook is the end-to-end claim that matters:
// a missing nightly backup produces an actual POST to the ops webhook, not
// just an internal state change.
func TestBackupStaleAlertReachesTheWebhook(t *testing.T) {
	received := make(chan string, 1)
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
	t.Setenv("BACKUP_DIR", t.TempDir()) // exists, but holds no backup

	// Reset the throttle so this test is independent of any earlier alert.
	backupAlertState.Lock()
	backupAlertState.lastAlertAt = time.Time{}
	backupAlertState.Unlock()

	checkBackupAge(36 * time.Hour)

	select {
	case text := <-received:
		if text == "" {
			t.Error("the alert payload must carry a message")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a missing nightly backup must alert the ops webhook")
	}

	// And it must not alert again on the next tick - a channel that fires every
	// poll interval is one people mute.
	checkBackupAge(36 * time.Hour)
	select {
	case text := <-received:
		t.Errorf("stale-backup alerts must be throttled to one per window, got a second: %q", text)
	case <-time.After(500 * time.Millisecond):
	}
}

func writeFixture(t *testing.T, dir, name string, modTime time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("setting fixture mtime: %v", err)
	}
}
