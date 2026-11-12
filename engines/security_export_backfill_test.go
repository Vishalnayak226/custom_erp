package engines

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
)

func countDetail(list []string, want string) int {
	n := 0
	for _, s := range list {
		if s == want {
			n++
		}
	}
	return n
}

// 49.11.6: history is replayed in order from a start date, only while the
// export is on, never twice at once, and the state clears when it catches up.
func TestStage49116BackfillReplaysFromDateInOrderAndClears(t *testing.T) {
	action, cleanup := exportRig(t)
	defer cleanup()
	srv, sent := exportReceiver(t)
	if err := SaveSecurityExport("default", "tester49116", srv.URL, exportSecret, true); err != nil {
		t.Fatal(err)
	}
	oldEvent(t, action, "bf-first")
	oldEvent(t, action, "bf-second")
	if _, err := RunSecurityExport("default", time.Now()); err != nil {
		t.Fatal(err)
	}
	if countDetail(sent(), "bf-first") != 1 {
		t.Fatalf("first pass: %v", sent())
	}
	now := time.Now()
	if _, err := StartSecurityExportBackfill("default", "tester49116", now.AddDate(0, 0, 3), now); err == nil {
		t.Fatal("a future start date must be refused")
	}
	if _, err := StartSecurityExportBackfill("default", "tester49116", now.AddDate(0, 0, -(securityExportBackfillMaxDays+5)), now); err == nil {
		t.Fatal("a start date past the bound must be refused")
	}
	rows, err := StartSecurityExportBackfill("default", "tester49116", now.UTC(), now)
	if err != nil || rows < 2 {
		t.Fatalf("start: rows=%d err=%v", rows, err)
	}
	if _, err := StartSecurityExportBackfill("default", "tester49116", now.UTC(), now); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("a second backfill while one runs must be refused: %v", err)
	}
	st, _ := GetSecurityExportStatus("default")
	if st.BackfillFrom == nil || st.BackfillLeft < 2 {
		t.Fatalf("status must show the replay: %+v", st)
	}
	for i := 0; i < 60; i++ {
		n, err := RunSecurityExport("default", time.Now().Add(24*time.Hour+time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	got := sent()
	if countDetail(got, "bf-first") != 2 || countDetail(got, "bf-second") != 2 {
		t.Fatalf("each event must arrive again exactly once: %v", got)
	}
	idx := func(s string) int {
		last := -1
		for i, v := range got {
			if v == s {
				last = i
			}
		}
		return last
	}
	if idx("bf-first") > idx("bf-second") {
		t.Fatal("replay is out of order")
	}
	st, _ = GetSecurityExportStatus("default")
	if st.BackfillFrom != nil || st.BackfillLeft != 0 {
		t.Fatalf("the backfill must clear once caught up: %+v", st)
	}
	if _, err := StartSecurityExportBackfill("default", "tester49116", now.UTC(), now); err != nil {
		t.Fatalf("a new backfill is allowed after one finishes: %v", err)
	}
}

func TestStage49116BackfillNeedsExportOn(t *testing.T) {
	_, cleanup := exportRig(t)
	defer cleanup()
	now := time.Now()
	if _, err := StartSecurityExportBackfill("default", "tester49116", now.UTC(), now); err == nil {
		t.Fatal("backfill with no export configured must be refused")
	}
}

// 49.11.6: a sealed window that is not archived and is short of rows in the
// live table is reported once.
func TestStage49116LiveWindowGapReportedOnce(t *testing.T) {
	action, cleanup := exportRig(t)
	defer cleanup()
	srv, _ := exportReceiver(t)
	if err := SaveSecurityExport("default", "tester49116", srv.URL, exportSecret, true); err != nil {
		t.Fatal(err)
	}
	if _, err := RunSecurityExport("default", time.Now()); err != nil {
		t.Fatal(err)
	}
	oldEvent(t, action, "gap-live")
	var seq int64
	if err := db.DB.QueryRow(`SELECT seq FROM tenant_default.audit_logs WHERE action = $1`, action).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.audit_checkpoints (from_seq, to_seq, row_count, row_digest, signature, sig_version, kind)
		VALUES ($1, $1, 2, 'test-49116', 'test-49116', 'test', 'Periodic')`, seq); err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_checkpoints WHERE row_digest = 'test-49116'`)
	gaps := func() int {
		var n int
		db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.audit_logs WHERE action = 'SECURITY_EXPORT_GAP' AND details LIKE $1`, "%window "+strconv.FormatInt(seq, 10)+" to "+strconv.FormatInt(seq, 10)+"%").Scan(&n)
		return n
	}
	for i := 0; i < 2; i++ {
		if _, err := RunSecurityExport("default", time.Now().Add(time.Duration(i+1)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if gaps() != 1 {
		t.Fatalf("the short window must be reported exactly once, got %d", gaps())
	}
	st, _ := GetSecurityExportStatus("default")
	if !strings.Contains(st.GapNote, "sealed window") {
		t.Fatalf("status must carry the note: %q", st.GapNote)
	}
}
