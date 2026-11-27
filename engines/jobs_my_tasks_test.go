package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 297.3: the Tasks bar lists the caller's own long tasks and nobody else's,
// with rows done and the time left, and lets the caller cancel only their own.
func TestMyTasksListsOnlyTheCallersOwnTasks(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const taskType, sideType = "test-2973-task", "test-2973-side"
	ensureTestJobHandler(taskType, sideType)
	RegisterUserTask(taskType, "Test task")

	stamp := time.Now().UnixNano()
	me, other := fmt.Sprintf("t2973-me-%d", stamp), fmt.Sprintf("t2973-other-%d", stamp)
	for _, u := range []string{me, other} {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.users (id, username, password_hash, role, status, location_code)
			VALUES ($1, $1, 'x', $2, 'Active', 'HO') ON CONFLICT (id) DO NOTHING`, schema), u, RoleSuperAdmin); err != nil {
			t.Fatalf("seed user %s: %v", u, err)
		}
	}
	defer func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.async_jobs WHERE actor IN ($1, $2)`, schema), me, other)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE created_by IN ($1, $2)`, schema), me, other)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.users WHERE id IN ($1, $2)`, schema), me, other)
	}()

	mine, err := EnqueueUserTask(WithJobActor(context.Background(), me), tenantID, taskType, "Import · Item · items.csv", map[string]interface{}{}, "")
	if err != nil {
		t.Fatalf("EnqueueUserTask (mine): %v", err)
	}
	theirs, err := EnqueueUserTask(WithJobActor(context.Background(), other), tenantID, taskType, "Their import", map[string]interface{}{}, "")
	if err != nil {
		t.Fatalf("EnqueueUserTask (theirs): %v", err)
	}
	// A side effect the user never asked to wait for (a notification, a
	// webhook) is queued under their name too, but is not a task.
	side, err := EnqueueJobContext(WithJobActor(context.Background(), me), tenantID, sideType, map[string]interface{}{}, "")
	if err != nil {
		t.Fatalf("EnqueueJobContext (side): %v", err)
	}

	var reportID, reportLabel string
	for id, def := range reportRegistry {
		reportID, reportLabel = id, def.Label
		break
	}
	exportID := fmt.Sprintf("t2973-export-%d", stamp)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'ReportExportJob', $2, 'Pending', $3)`, schema), exportID, fmt.Sprintf(`{"report_id": %q}`, reportID), me); err != nil {
		t.Fatalf("seed export: %v", err)
	}

	list, err := ListMyTasks(tenantID, me)
	if err != nil {
		t.Fatalf("ListMyTasks: %v", err)
	}
	byID := map[string]MyTask{}
	for _, task := range list {
		byID[task.ID] = task
	}
	if len(list) != 2 {
		t.Fatalf("expected my import and my export only, got %d: %+v", len(list), list)
	}
	if _, ok := byID["job:"+theirs]; ok {
		t.Fatalf("another user's task is in my list")
	}
	if _, ok := byID["job:"+side]; ok {
		t.Fatalf("a side-effect job is in my tasks list")
	}
	got := byID["job:"+mine]
	if got.Label != "Import · Item · items.csv" || got.State != "queued" || got.Kind != "job" {
		t.Fatalf("my queued import: %+v", got)
	}
	exp := byID["export:"+exportID]
	if exp.State != "running" || exp.Pct != -1 || exp.Label != "Export · "+reportLabel {
		t.Fatalf("my running export: %+v", exp)
	}

	t.Run("ReportJobProgress records the counts, renews the lease and the list shows the time left", func(t *testing.T) {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.async_jobs SET status = 'Leased', leased_by = 'test',
			leased_until = CURRENT_TIMESTAMP + INTERVAL '10 seconds', started_at = CURRENT_TIMESTAMP - INTERVAL '20 seconds' WHERE id = $1`, schema), mine); err != nil {
			t.Fatalf("lease: %v", err)
		}
		if err := ReportJobProgress(schema, mine, 50, 200); err != nil {
			t.Fatalf("ReportJobProgress: %v", err)
		}
		var pct int
		var leaseLeft float64
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT progress_pct, EXTRACT(EPOCH FROM leased_until - CURRENT_TIMESTAMP) FROM %s.async_jobs WHERE id = $1`, schema), mine).Scan(&pct, &leaseLeft); err != nil {
			t.Fatalf("reload: %v", err)
		}
		if pct != 25 {
			t.Fatalf("expected 25%% for 50 of 200, got %d", pct)
		}
		if leaseLeft < 240 {
			t.Fatalf("a job reporting progress must keep its lease (5 min), %0.f s left", leaseLeft)
		}
		list, err := ListMyTasks(tenantID, me)
		if err != nil {
			t.Fatalf("ListMyTasks: %v", err)
		}
		for _, task := range list {
			if task.ID != "job:"+mine {
				continue
			}
			if task.State != "running" || task.Done != 50 || task.Total != 200 || task.Pct != 25 {
				t.Fatalf("running import: %+v", task)
			}
			// 50 rows in 20 s: 150 left is about 60 s.
			if task.EtaSec < 50 || task.EtaSec > 75 {
				t.Fatalf("expected about 60 s left, got %d", task.EtaSec)
			}
			return
		}
		t.Fatalf("my running import is missing from the list")
	})

	t.Run("CancelMyJob cancels only the caller's own task", func(t *testing.T) {
		if err := CancelMyJob(tenantID, me, theirs); err != ErrMyTaskNotFound {
			t.Fatalf("cancelling another user's task: expected not found, got %v", err)
		}
		if err := CancelMyJob(tenantID, me, side); err != ErrMyTaskNotFound {
			t.Fatalf("cancelling a side-effect job: expected not found, got %v", err)
		}
		if err := CancelMyJob(tenantID, "", mine); err != ErrMyTaskNotFound {
			t.Fatalf("cancelling with no user: expected not found, got %v", err)
		}
		var status string
		db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.async_jobs WHERE id = $1`, schema), theirs).Scan(&status)
		if status != "Pending" {
			t.Fatalf("another user's task was touched: %s", status)
		}
		if err := CancelMyJob(tenantID, me, mine); err != nil {
			t.Fatalf("cancelling my own task: %v", err)
		}
		if err := CancelMyJob(tenantID, me, mine); err == nil || !strings.Contains(err.Error(), "finished") {
			t.Fatalf("cancelling a finished task: expected 'already finished', got %v", err)
		}
		list, _ := ListMyTasks(tenantID, me)
		for _, task := range list {
			if task.ID == "job:"+mine && task.State != "cancelled" {
				t.Fatalf("expected my task cancelled, got %s", task.State)
			}
		}
	})
}

func TestMyTaskStateAndTimeLeft(t *testing.T) {
	for _, c := range []struct {
		status  string
		started bool
		want    string
	}{
		{"Pending", false, "queued"},
		{"Pending", true, "running"}, // between retries
		{"Leased", true, "running"},
		{"Succeeded", true, "done"},
		{"Cancelled", false, "cancelled"},
		{"DeadLettered", true, "failed"},
		{"Quarantined", true, "failed"},
	} {
		if got := jobTaskState(c.status, c.started); got != c.want {
			t.Errorf("%s started=%v: got %s, want %s", c.status, c.started, got, c.want)
		}
	}
	now := time.Now()
	started := sql.NullTime{Time: now.Add(-30 * time.Second), Valid: true}
	if got := taskEta(100, 400, started, now); got != 90 {
		t.Errorf("100 of 400 in 30 s: expected 90 s left, got %d", got)
	}
	if got := taskEta(100, 400, sql.NullTime{Time: now.Add(-2 * time.Second), Valid: true}, now); got != 0 {
		t.Errorf("under 5 s of run time is too little to say, got %d", got)
	}
	if got := taskEta(0, 400, started, now); got != 0 {
		t.Errorf("nothing done yet is too little to say, got %d", got)
	}
	if got := taskEta(10, 0, started, now); got != 0 {
		t.Errorf("no total: no time left, got %d", got)
	}
}
