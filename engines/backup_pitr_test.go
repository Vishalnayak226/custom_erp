package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
	"time"
)

// Item 290.2: the server watches point-in-time recovery. A server where it was
// never set up stays quiet; a missed weekly base backup raises
// BACKUP_PITR_STALE; a failing archiver raises BACKUP_WAL_ARCHIVE_FAILED.
func TestPITRAlerts(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		st   PITRState
		want []string
	}{
		{"never set up", PITRState{}, nil},
		{"archiving off, archiver failed long ago", PITRState{Failing: true}, nil},
		{"base 2 days old, archiving fine", PITRState{BaseRuns: 3, BaseNewest: now.Add(-48 * time.Hour), ArchiveOn: true}, nil},
		{"base 9 days old", PITRState{BaseRuns: 3, BaseNewest: now.Add(-9 * 24 * time.Hour), ArchiveOn: true}, []string{"BACKUP_PITR_STALE"}},
		{"base runs only failed", PITRState{BaseRuns: 1, ArchiveOn: true}, []string{"BACKUP_PITR_STALE"}},
		{"archiver failing", PITRState{BaseRuns: 1, BaseNewest: now.Add(-time.Hour), ArchiveOn: true, Failing: true}, []string{"BACKUP_WAL_ARCHIVE_FAILED"}},
		{"both", PITRState{BaseRuns: 1, ArchiveOn: true, Failing: true}, []string{"BACKUP_PITR_STALE", "BACKUP_WAL_ARCHIVE_FAILED"}},
	}
	for _, c := range cases {
		got := pitrAlerts(c.st, now)
		if len(got) != len(c.want) {
			t.Errorf("%s: alerts %v, want %v", c.name, got, c.want)
			continue
		}
		for _, code := range c.want {
			msg, ok := got[code]
			if !ok {
				t.Errorf("%s: no %s in %v", c.name, code, got)
			}
			if strings.Contains(msg, "/var/") || strings.Contains(msg, "/opt/") {
				t.Errorf("%s: the alert names a host path: %q", c.name, msg)
			}
		}
	}

	// The real queries: a recorded base backup is found, and pg_stat_archiver reads.
	db.InitDB(testConnStr())
	const host = "T2902-HOST"
	db.DB.Exec(`DELETE FROM public.backup_runs WHERE host = $1`, host)
	defer db.DB.Exec(`DELETE FROM public.backup_runs WHERE host = $1`, host)
	at := time.Now().Add(-time.Minute).Truncate(time.Second)
	if _, err := db.DB.Exec(`INSERT INTO public.backup_runs (scope, outcome, host, started_at, finished_at)
		VALUES ('pitr', 'Failed', $1, $2, $2), ('pitr', 'Success', $1, $2, $2)`, host, at); err != nil {
		t.Fatalf("insert: %v", err)
	}
	st := ReadPITRState()
	if st.BaseRuns < 2 || st.BaseNewest.Before(at.Add(-time.Second)) {
		t.Fatalf("ReadPITRState = %+v, want at least 2 runs and a success at %s", st, at)
	}
	if _, stale := pitrAlerts(st, time.Now())["BACKUP_PITR_STALE"]; stale {
		t.Errorf("a base backup a minute old reads as stale: %+v", st)
	}
}
