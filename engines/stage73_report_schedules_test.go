package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 55.20: scheduled reports and digests people can see and control.
func TestStage73ReportSchedules(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	ctx := context.Background()
	started := time.Now().Add(-time.Second)
	prefix := fmt.Sprintf("RS73-%d-", time.Now().UnixNano())
	mgrSched, cashSched, digestID := prefix+"MGR", prefix+"CASH", prefix+"DIG"
	const reportID = "exception-failed-syncs"
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	later := time.Now().AddDate(0, 0, 5).Format("2006-01-02")

	layoutID, err := SaveDashboardLayout(tenantID, "manager1", prefix+"Layout", "", []DashboardTileSpec{{ReportID: reportID, Title: "Failed syncs"}})
	if err != nil {
		t.Fatalf("SaveDashboardLayout: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id LIKE $1 OR id = $2", prefix+"%", layoutID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'ReportRunLog' AND data->>'report_id' = $1 AND created_at >= $2", reportID, started)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".integration_event_outbox WHERE payload::text LIKE $1", "%"+prefix+"%")
	})

	seed := func(id, doctype, owner string, data map[string]interface{}) {
		t.Helper()
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', $4)", id, doctype, b, owner); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed(mgrSched, "ScheduledReport", "manager1", map[string]interface{}{
		"report_id": reportID, "frequency": "Daily", "requested_role": "Store Manager", "status": "Active",
		"recipient_email": "anita@example.com, ops.team@example.org", "webhook_url": "https://hooks.example.com/T0/secret?token=abc",
		"next_run_date": yesterday,
	})
	seed(cashSched, "ScheduledReport", "cashier1", map[string]interface{}{
		"report_id": reportID, "frequency": "Weekly", "requested_role": "Cashier", "status": "Active",
		"recipient_email": "till@example.com", "next_run_date": later,
	})
	seed(digestID, "DashboardDigest", "manager1", map[string]interface{}{
		"dashboard_layout_id": layoutID, "frequency": "Weekly", "requested_role": "Store Manager", "status": "Active",
		"recipient_email": "boss@example.com", "next_run_date": later,
	})

	find := func(list []ReportScheduleView, id string) *ReportScheduleView {
		for i := range list {
			if list[i].ID == id {
				return &list[i]
			}
		}
		return nil
	}
	list := func(user, role string) []ReportScheduleView {
		t.Helper()
		out, err := ListReportSchedules(tenantID, user, role)
		if err != nil {
			t.Fatalf("ListReportSchedules(%s): %v", user, err)
		}
		return out
	}
	readData := func(id string) map[string]interface{} {
		t.Helper()
		var s string
		if err := db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id = $1", id).Scan(&s); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		var d map[string]interface{}
		_ = json.Unmarshal([]byte(s), &d)
		return d
	}
	setField := func(id, key, value string) {
		t.Helper()
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, $2, to_jsonb($3::text)) WHERE id = $1", id, "{"+key+"}", value); err != nil {
			t.Fatalf("set %s.%s: %v", id, key, err)
		}
	}

	t.Run("OwnerSeesOwnOthersDoNotSuperAdminSeesAll", func(t *testing.T) {
		mgr := list("manager1", "Store Manager")
		if find(mgr, mgrSched) == nil || find(mgr, digestID) == nil {
			t.Fatalf("manager1 should see their schedule and digest")
		}
		if find(mgr, cashSched) != nil {
			t.Fatalf("manager1 must not see cashier1's schedule")
		}
		cash := list("cashier1", "Cashier")
		if find(cash, cashSched) == nil || find(cash, mgrSched) != nil || find(cash, digestID) != nil {
			t.Fatalf("cashier1 should see only their own schedule")
		}
		admin := list("admin", "Super Admin")
		if find(admin, mgrSched) == nil || find(admin, cashSched) == nil || find(admin, digestID) == nil {
			t.Fatalf("Super Admin should see every schedule")
		}
		d := find(admin, digestID)
		if d.Kind != "digest" || d.TargetLabel != prefix+"Layout" || d.Owner != "manager1" {
			t.Errorf("digest row wrong: %+v", d)
		}
		if s := find(admin, mgrSched); s.Kind != "report" || s.TargetLabel == "" || s.Owner != "manager1" {
			t.Errorf("schedule row wrong: %+v", s)
		}
	})

	t.Run("RecipientsAreMasked", func(t *testing.T) {
		for _, who := range [][2]string{{"admin", "Super Admin"}, {"manager1", "Store Manager"}} {
			s := find(list(who[0], who[1]), mgrSched)
			if s.RecipientCount != 2 || strings.Join(s.Recipients, ",") != "a***@example.com,o***@example.org" {
				t.Errorf("%s: recipients not masked: %d %v", who[0], s.RecipientCount, s.Recipients)
			}
			if s.WebhookHost != "https://hooks.example.com" {
				t.Errorf("%s: webhook should be host only, got %q", who[0], s.WebhookHost)
			}
			b, _ := json.Marshal(s)
			if strings.Contains(string(b), "anita") || strings.Contains(string(b), "token=abc") || strings.Contains(string(b), "secret") {
				t.Errorf("%s: full delivery details leaked: %s", who[0], b)
			}
		}
	})

	t.Run("NextRunMatchesTheWorker", func(t *testing.T) {
		admin := list("admin", "Super Admin")
		due, notDue, digest := find(admin, mgrSched), find(admin, cashSched), find(admin, digestID)
		if !due.Due || due.NextRun != yesterday || due.FollowingRun != advanceScheduleDate(yesterday, "Daily") {
			t.Fatalf("due schedule view wrong: %+v", due)
		}
		if notDue.Due || notDue.NextRun != later || digest.Due {
			t.Fatalf("future schedules must not be due: %+v / %+v", notDue, digest)
		}
		processScheduledReports(ctx, schema)
		processDashboardDigests(ctx, schema)
		ran := readData(mgrSched)
		if ran["last_run_at"] == nil || ran["next_run_date"] != due.FollowingRun {
			t.Fatalf("worker should have run the due schedule and moved it to %s: %v", due.FollowingRun, ran)
		}
		if readData(cashSched)["last_run_at"] != nil || readData(digestID)["last_run_at"] != nil {
			t.Fatalf("worker ran a schedule the view said was not due")
		}
		after := find(list("manager1", "Store Manager"), mgrSched)
		if after.LastRunAt == "" || after.LastResult == "" || after.Due {
			t.Errorf("view after the run: %+v", after)
		}
		if after.LastResult == "ok" && after.LastRows == nil {
			t.Errorf("a delivered run should show its row count: %+v", after)
		}
	})

	t.Run("PauseStopsTheWorker", func(t *testing.T) {
		setField(mgrSched, "next_run_date", yesterday)
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = data - 'last_run_at' WHERE id = $1", mgrSched); err != nil {
			t.Fatal(err)
		}
		if err := SetReportSchedulePaused(tenantID, "report", mgrSched, "cashier1", "Cashier", true); !errors.Is(err, ErrReportScheduleNotFound) {
			t.Fatalf("another user must not pause it, got %v", err)
		}
		if err := SetReportSchedulePaused(tenantID, "report", mgrSched, "manager1", "Store Manager", true); err != nil {
			t.Fatalf("owner pause: %v", err)
		}
		if s := find(list("manager1", "Store Manager"), mgrSched); s.Status != "Paused" || s.Due {
			t.Fatalf("paused schedule should show Paused and not due: %+v", s)
		}
		processScheduledReports(ctx, schema)
		if readData(mgrSched)["last_run_at"] != nil {
			t.Fatalf("worker ran a paused schedule")
		}
		if err := SetReportSchedulePaused(tenantID, "report", mgrSched, "admin", "Super Admin", false); err != nil {
			t.Fatalf("Super Admin resume: %v", err)
		}
		if s := find(list("manager1", "Store Manager"), mgrSched); s.Status != "Active" || !s.Due {
			t.Fatalf("resumed schedule should be due again: %+v", s)
		}
		processScheduledReports(ctx, schema)
		if readData(mgrSched)["last_run_at"] == nil {
			t.Fatalf("worker did not run the resumed schedule")
		}
	})

	t.Run("RunNowRecordsARunOnce", func(t *testing.T) {
		if _, err := ClaimReportScheduleRun(tenantID, "report", mgrSched, "cashier1", "Cashier"); !errors.Is(err, ErrReportScheduleNotFound) {
			t.Fatalf("another user must not run it, got %v", err)
		}
		var busy *ReportScheduleBusyError
		if _, err := ClaimReportScheduleRun(tenantID, "report", mgrSched, "manager1", "Store Manager"); !errors.As(err, &busy) {
			t.Fatalf("a schedule that just ran must not run again within a minute, got %v", err)
		}
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = data - 'last_run_at' WHERE id = $1", mgrSched); err != nil {
			t.Fatal(err)
		}
		nextBefore := readData(mgrSched)["next_run_date"]
		var logsBefore int
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'ReportRunLog' AND data->>'report_id' = $1 AND data->>'user_id' = 'manager1'", reportID).Scan(&logsBefore)

		run, err := ClaimReportScheduleRun(tenantID, "report", mgrSched, "manager1", "Store Manager")
		if err != nil {
			t.Fatalf("owner run now: %v", err)
		}
		if _, err := ClaimReportScheduleRun(tenantID, "report", mgrSched, "admin", "Super Admin"); !errors.As(err, &busy) {
			t.Fatalf("a second click while running must be refused, got %v", err)
		}
		if s := find(list("manager1", "Store Manager"), mgrSched); s.LastResult != "running" {
			t.Errorf("claimed schedule should show running: %+v", s)
		}
		run.Run(ctx)
		d := readData(mgrSched)
		if d["last_run_status"] != "Delivered" || d["last_run_rows"] == nil {
			t.Fatalf("run now should deliver and record rows: %v", d)
		}
		if d["next_run_date"] != nextBefore {
			t.Errorf("run now must not move a future next run: was %v now %v", nextBefore, d["next_run_date"])
		}
		var logsAfter int
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'ReportRunLog' AND data->>'report_id' = $1 AND data->>'user_id' = 'manager1'", reportID).Scan(&logsAfter)
		if logsAfter != logsBefore+1 {
			t.Fatalf("run now should log one run by manager1: %d -> %d", logsBefore, logsAfter)
		}

		mine, err := ListRecentReportRuns(tenantID, reportID, "manager1", "Store Manager", 50)
		if err != nil || len(mine) == 0 || mine[0].RunBy != "manager1" {
			t.Fatalf("manager1's recent runs: %v %+v", err, mine)
		}
		for _, r := range mine {
			if r.RunBy != "manager1" {
				t.Fatalf("a non-admin must see only their own runs, got %+v", r)
			}
		}
		theirs, _ := ListRecentReportRuns(tenantID, reportID, "cashier1", "Cashier", 50)
		for _, r := range theirs {
			if r.RunBy != "cashier1" {
				t.Fatalf("cashier1 saw someone else's run: %+v", r)
			}
		}
		all, _ := ListRecentReportRuns(tenantID, reportID, "admin", "Super Admin", 50)
		sawWorker, sawManager := false, false
		for _, r := range all {
			sawWorker = sawWorker || r.RunBy == ""
			sawManager = sawManager || r.RunBy == "manager1"
		}
		if !sawWorker || !sawManager {
			t.Fatalf("Super Admin should see the worker's and manager1's runs: %+v", all)
		}

		// A Super Admin may run someone else's digest; a future next run stays put.
		digestRun, err := ClaimReportScheduleRun(tenantID, "digest", digestID, "admin", "Super Admin")
		if err != nil {
			t.Fatalf("Super Admin run digest: %v", err)
		}
		digestRun.Run(ctx)
		dd := readData(digestID)
		if dd["last_run_status"] != "Delivered" || dd["last_run_rows"] == nil || dd["next_run_date"] != later {
			t.Fatalf("digest run now: %v", dd)
		}
	})

	t.Run("DeletedScheduleIsNeitherListedNorRun", func(t *testing.T) {
		setField(mgrSched, "next_run_date", yesterday)
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET deleted_at = CURRENT_TIMESTAMP, data = data - 'last_run_at' WHERE id = $1", mgrSched); err != nil {
			t.Fatal(err)
		}
		if find(list("admin", "Super Admin"), mgrSched) != nil {
			t.Fatalf("a deleted schedule must not be listed")
		}
		processScheduledReports(ctx, schema)
		if readData(mgrSched)["last_run_at"] != nil {
			t.Fatalf("the worker ran a deleted schedule")
		}
		if _, err := ClaimReportScheduleRun(tenantID, "report", mgrSched, "manager1", "Store Manager"); !errors.Is(err, ErrReportScheduleNotFound) {
			t.Fatalf("a deleted schedule must not run now, got %v", err)
		}
	})
}
