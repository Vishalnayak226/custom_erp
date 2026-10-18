package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
	"time"
)

// Stage 75.1.6: BACKUP_OFFSITE_STALE fires when off-box copies are expected
// and the newest is missing or older than 26 h, and stays quiet where no
// bucket is configured and none was ever recorded.
func TestStage7516OffsiteBackupFreshness(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		fresh BackupFreshness
		alert bool
	}{
		{"no bucket, none recorded", BackupFreshness{}, false},
		{"expected, none copied", BackupFreshness{OffsiteExpected: true}, true},
		{"expected, copied 30h ago", BackupFreshness{OffsiteExpected: true, OffsiteFound: true, OffsiteNewest: now.Add(-30 * time.Hour), OffsiteAgeHours: 30}, true},
		{"expected, copied 2h ago", BackupFreshness{OffsiteExpected: true, OffsiteFound: true, OffsiteNewest: now.Add(-2 * time.Hour), OffsiteAgeHours: 2}, false},
	}
	for _, c := range cases {
		msg := offsiteBackupStaleMessage(c.fresh)
		if (msg != "") != c.alert {
			t.Errorf("%s: alert = %q", c.name, msg)
		}
		if strings.Contains(msg, "/opt/") {
			t.Errorf("%s: the alert names a host path: %q", c.name, msg)
		}
	}

	// The real query: a recorded off-box success is found.
	db.InitDB(testConnStr())
	const host = "T7516-HOST"
	db.DB.Exec(`DELETE FROM public.backup_runs WHERE host = $1`, host)
	defer db.DB.Exec(`DELETE FROM public.backup_runs WHERE host = $1`, host)
	if _, err := db.DB.Exec(`INSERT INTO public.backup_runs (scope, outcome, host, started_at, finished_at, offsite_key, offsite_outcome)
		VALUES ('database', 'Success', $1, now() - interval '1 hour', now() - interval '1 hour', 'T7516/database/x.dump.enc', 'Success')`, host); err != nil {
		t.Fatalf("seed: %v", err)
	}
	fresh := CheckBackupFreshness()
	if !fresh.OffsiteExpected || !fresh.OffsiteFound || fresh.OffsiteAgeHours > 2 {
		t.Fatalf("a recorded off-box success was not seen: %+v", fresh)
	}
}
