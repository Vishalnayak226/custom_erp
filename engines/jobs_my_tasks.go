package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
)

// 297.3: a progress bar for every long task.
//
// "My tasks" is the signed-in user's own long work - an import, a report
// export, an image copy - wherever it runs, so the top bar can show it with a
// bar while the user works on other screens. Two sources feed it, each read
// as it already is: async_jobs rows the user queued (actor = the user's id,
// stamped by apiMiddleware), and the user's ReportExportJob documents.
//
// Only job types registered with RegisterUserTask are listed. Most jobs a
// request queues are side effects the user never asked for (a notification,
// a webhook, a push to a marketplace); those stay on Admin › Jobs only.

// MyTask is one row of the tasks list.
type MyTask struct {
	ID         string                 `json:"id"`    // "job:<id>" or "export:<id>"
	Kind       string                 `json:"kind"`  // job | export
	Type       string                 `json:"type"`  // the job type, or the report id
	Label      string                 `json:"label"` // what the user sees
	State      string                 `json:"state"` // queued | running | done | failed | cancelled
	Pct        int                    `json:"pct"`   // -1: running, how far is not known
	Done       int64                  `json:"done,omitempty"`
	Total      int64                  `json:"total,omitempty"`
	EtaSec     int64                  `json:"eta_sec,omitempty"`
	Error      string                 `json:"error,omitempty"`
	Result     map[string]interface{} `json:"result,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	FinishedAt *time.Time             `json:"finished_at,omitempty"`
}

var userTaskTypes = struct {
	sync.RWMutex
	labels map[string]string
}{labels: map[string]string{}}

// RegisterUserTask marks a job type as work a user starts and waits for, with
// the name the tasks list shows when the job has no label of its own.
func RegisterUserTask(jobType, label string) {
	userTaskTypes.Lock()
	userTaskTypes.labels[jobType] = label
	userTaskTypes.Unlock()
}

func init() {
	// A scheduled MRP change is not listed: it waits days for its date, and
	// one applied now runs inside the request.
	RegisterUserTask(imageDriveCopyJobType, "Copy images")
}

// EnqueueUserTask queues a job a user waits for, with the label the tasks
// list shows ("Import · Item · items.csv"). The job is stamped with ctx's
// actor like any other, which is what puts it in that user's list.
func EnqueueUserTask(ctx context.Context, tenantID, jobType, label string, payload map[string]interface{}, idempotencyKey string) (string, error) {
	id, err := EnqueueJobContext(ctx, tenantID, jobType, payload, idempotencyKey)
	if err != nil || strings.TrimSpace(label) == "" {
		return id, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return id, err
	}
	label = truncateRunes(strings.TrimSpace(label), 200)
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.async_jobs SET label = $1 WHERE id = $2 AND label = ''`, schema), label, id)
	return id, err
}

// myTasksWindow is how long a finished task stays in the list: long enough
// to come back to after lunch, short enough that the list stays a few rows.
const myTasksWindow = 24 * time.Hour

const myTasksMax = 20

// ListMyTasks is the user's own tasks: every one still running, and those
// finished in the last day - running first, then newest first, at most 20.
func ListMyTasks(tenantID, userID string) ([]MyTask, error) {
	out := []MyTask{}
	if strings.TrimSpace(userID) == "" {
		return out, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	userTaskTypes.RLock()
	types := make([]string, 0, len(userTaskTypes.labels))
	labels := make(map[string]string, len(userTaskTypes.labels))
	for t, l := range userTaskTypes.labels {
		types = append(types, t)
		labels[t] = l
	}
	userTaskTypes.RUnlock()
	since := time.Now().Add(-myTasksWindow)

	jobs, err := listMyJobTasks(schema, userID, types, labels, since)
	if err != nil {
		return nil, err
	}
	out = append(out, jobs...)
	exports, err := listMyExportTasks(schema, userID, since)
	if err != nil {
		return nil, err
	}
	out = append(out, exports...)

	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := taskActive(out[i].State), taskActive(out[j].State)
		if ai != aj {
			return ai
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > myTasksMax {
		out = out[:myTasksMax]
	}
	return out, nil
}

func taskActive(state string) bool { return state == "queued" || state == "running" }

func listMyJobTasks(schema, userID string, types []string, labels map[string]string, since time.Time) ([]MyTask, error) {
	out := []MyTask{}
	if len(types) == 0 {
		return out, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, job_type, label, status, progress_pct, progress_done, progress_total, started_at,
		       COALESCE(result::text, ''), last_error, created_at, completed_at
		FROM %s.async_jobs
		WHERE actor = $1 AND job_type = ANY($2)
		  AND (status IN ('Pending', 'Leased') OR created_at >= $3)
		ORDER BY created_at DESC LIMIT $4`, schema), userID, pq.Array(types), since, myTasksMax)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		var t MyTask
		var id, status, resultText, lastError string
		var started, completed sql.NullTime
		if err := rows.Scan(&id, &t.Type, &t.Label, &status, &t.Pct, &t.Done, &t.Total, &started,
			&resultText, &lastError, &t.CreatedAt, &completed); err != nil {
			return nil, err
		}
		t.ID, t.Kind = "job:"+id, "job"
		if t.Label == "" {
			t.Label = labels[t.Type]
		}
		t.State = jobTaskState(status, started.Valid)
		if completed.Valid {
			c := completed.Time
			t.FinishedAt = &c
		}
		if resultText != "" {
			_ = json.Unmarshal([]byte(resultText), &t.Result)
		}
		switch t.State {
		case "running":
			if t.Total == 0 && t.Pct == 0 {
				t.Pct = -1
			}
			t.EtaSec = taskEta(t.Done, t.Total, started, now)
		case "done":
			t.Pct = 100
		case "failed":
			t.Error = firstLine(lastError, 200)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// jobTaskState maps the runner's status taxonomy to the four a user needs.
// A Pending job that has started before is between retries: still running.
func jobTaskState(status string, started bool) string {
	switch status {
	case "Pending":
		if started {
			return "running"
		}
		return "queued"
	case "Leased":
		return "running"
	case "Succeeded":
		return "done"
	case "Cancelled":
		return "cancelled"
	default: // Failed is retried (Pending again); DeadLettered, Quarantined
		return "failed"
	}
}

// taskEta is the time left at the rate so far, or 0 while too little has
// run to say (under 5 seconds, or nothing done yet).
func taskEta(done, total int64, started sql.NullTime, now time.Time) int64 {
	if !started.Valid || done <= 0 || total <= done {
		return 0
	}
	elapsed := now.Sub(started.Time)
	if elapsed < 5*time.Second {
		return 0
	}
	left := time.Duration(float64(elapsed) * float64(total-done) / float64(done))
	return int64(left.Round(time.Second) / time.Second)
}

func listMyExportTasks(schema, userID string, since time.Time) ([]MyTask, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'report_id', ''), status, created_at, COALESCE(updated_at, created_at)
		FROM %s.documents
		WHERE doctype = 'ReportExportJob' AND created_by = $1 AND (status = 'Pending' OR created_at >= $2)
		ORDER BY created_at DESC LIMIT $3`, schema), userID, since, myTasksMax)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MyTask{}
	for rows.Next() {
		var t MyTask
		var id, status string
		var updated time.Time
		if err := rows.Scan(&id, &t.Type, &status, &t.CreatedAt, &updated); err != nil {
			return nil, err
		}
		t.ID, t.Kind = "export:"+id, "export"
		t.Label = "Export"
		if def, ok := reportRegistry[t.Type]; ok {
			t.Label = "Export · " + def.Label
		}
		switch status {
		case "Pending":
			// The export worker runs a report in one query: how far it is
			// cannot be known, so the bar moves without a figure.
			t.State, t.Pct = "running", -1
		case "Completed":
			t.State, t.Pct = "done", 100
		case "Failed":
			t.State, t.Error = "failed", "Narrow the filters and export again."
		default: // Cancelled, Revoked
			t.State = "cancelled"
		}
		if t.State != "running" {
			u := updated
			t.FinishedAt = &u
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func firstLine(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return truncateRunes(strings.TrimSpace(s), max)
}

// ErrMyTaskNotFound answers a cancel for a task that is not the caller's.
var ErrMyTaskNotFound = fmt.Errorf("task not found")

// CancelMyJob stops a job the caller queued, through CancelJob, so a running
// handler is told to stop. A job of another user, or of a type that is not a
// user task, answers not found - never cancelled.
func CancelMyJob(tenantID, userID, jobID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var actor, jobType string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT actor, job_type FROM %s.async_jobs WHERE id = $1`, schema), jobID).Scan(&actor, &jobType); err != nil {
		return ErrMyTaskNotFound
	}
	userTaskTypes.RLock()
	_, isTask := userTaskTypes.labels[jobType]
	userTaskTypes.RUnlock()
	if userID == "" || actor != userID || !isTask {
		return ErrMyTaskNotFound
	}
	if err := CancelJob(tenantID, jobID); err != nil {
		return fmt.Errorf("this task has already finished")
	}
	return nil
}
