package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Stage 55.20: scheduled reports and dashboard digests people can see and
// control, and a report's recent runs.
//
// ScheduledReport and DashboardDigest are still created and edited through the
// generic doc API (Setup > Reports); this file is the read and control layer
// over them. A schedule belongs to whoever created it: they (or a Super Admin)
// see it, pause or resume it, and run it now. "Next run" is the worker's own
// answer - scheduleDueSQL decides "due", advanceScheduleDate where it moves
// next - and Run now calls the worker's per-schedule step, so the screen
// cannot drift from what the worker does. Recipients are shown as a count and
// masked addresses only; the full list stays on the record itself.

// reportScheduleDoctypes maps the view's kind to the doctype its worker reads.
var reportScheduleDoctypes = map[string]string{"report": "ScheduledReport", "digest": "DashboardDigest"}

// ErrReportScheduleNotFound is one answer for a schedule that does not exist,
// is deleted, or belongs to someone else - the same rule as an export job.
var ErrReportScheduleNotFound = errors.New("schedule not found")

// ReportScheduleBusyError is Run now refused because the schedule is running
// or has only just run.
type ReportScheduleBusyError struct{ Reason string }

func (e *ReportScheduleBusyError) Error() string { return e.Reason }

// reportScheduleRunGap is the shortest gap between two runs of one schedule
// that Run now allows, so a double click delivers once.
const reportScheduleRunGap = time.Minute

// ReportScheduleView is one row of the Schedules tab.
type ReportScheduleView struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"` // "report" or "digest"
	Owner          string   `json:"owner"`
	Target         string   `json:"target"`       // report id, or dashboard layout id
	TargetLabel    string   `json:"target_label"` // report label, or layout name
	Frequency      string   `json:"frequency"`
	RunAsRole      string   `json:"run_as_role"`
	Status         string   `json:"status"`   // "Active" or "Paused" (any other stored status as-is)
	NextRun        string   `json:"next_run"` // the worker's next_run_date
	Due            bool     `json:"due"`      // the worker runs it on its next tick
	FollowingRun   string   `json:"following_run,omitempty"`
	LastRunAt      string   `json:"last_run_at"`
	LastResult     string   `json:"last_result"` // "ok", "error", "running", or "" when never run
	LastError      string   `json:"last_error,omitempty"`
	LastRows       *int     `json:"last_rows,omitempty"`
	RecipientCount int      `json:"recipient_count"`
	Recipients     []string `json:"recipients"` // masked, e.g. a***@x.com
	WebhookHost    string   `json:"webhook_host,omitempty"`
}

// ListReportSchedules returns the caller's own schedules and digests, or every
// one for a Super Admin, scheduled reports first and newest first.
func ListReportSchedules(tenantID, userID, role string) ([]ReportScheduleView, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	// scheduleDueSQL's columns are unqualified; they resolve to d, the only
	// table in the outer FROM.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, doctype, data, COALESCE(status, ''), COALESCE(created_by, ''),
		       COALESCE((`+scheduleDueSQL+`), false),
		       COALESCE((SELECT l.data->>'name' FROM %s.documents l
		                 WHERE l.doctype = 'DashboardLayout' AND l.deleted_at IS NULL
		                   AND l.id = d.data->>'dashboard_layout_id'), '')
		FROM %s.documents d
		WHERE doctype IN ('ScheduledReport', 'DashboardDigest') AND deleted_at IS NULL
		  AND ($2 OR created_by = $3)
		ORDER BY doctype DESC, created_at DESC
		LIMIT 500`, schema, schema), today, HasFullAccess(role), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportScheduleView{}
	for rows.Next() {
		var id, doctype, dataStr, status, owner, layoutName string
		var due bool
		if err := rows.Scan(&id, &doctype, &dataStr, &status, &owner, &due, &layoutName); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		str := func(k string) string { s, _ := data[k].(string); return strings.TrimSpace(s) }
		v := ReportScheduleView{
			ID: id, Owner: owner, Frequency: str("frequency"), RunAsRole: str("requested_role"),
			Status: status, NextRun: str("next_run_date"), Due: due, LastRunAt: str("last_run_at"),
		}
		if v.Frequency == "" {
			v.Frequency = "Daily" // advanceScheduleDate's own default
		}
		if status == "Inactive" {
			v.Status = "Paused"
		}
		if doctype == "DashboardDigest" {
			v.Kind, v.Target, v.TargetLabel = "digest", str("dashboard_layout_id"), layoutName
		} else {
			v.Kind, v.Target = "report", str("report_id")
			if def, ok := reportRegistry[v.Target]; ok {
				v.TargetLabel = def.Label
			}
		}
		if due {
			v.FollowingRun = advanceScheduleDate(v.NextRun, v.Frequency)
		}
		switch last := str("last_run_status"); {
		case last == "":
		case last == "Delivered":
			v.LastResult = "ok"
		case last == "Running":
			v.LastResult = "running"
		default:
			v.LastResult, v.LastError = "error", strings.TrimPrefix(last, "Failed: ")
		}
		if n, ok := data["last_run_rows"].(float64); ok {
			rowsRun := int(n)
			v.LastRows = &rowsRun
		}
		v.Recipients = []string{}
		for _, addr := range splitRecipientList(str("recipient_email")) {
			v.Recipients = append(v.Recipients, maskEmailAddress(addr))
		}
		v.RecipientCount = len(v.Recipients)
		v.WebhookHost = webhookHostOnly(str("webhook_url"))
		out = append(out, v)
	}
	return out, rows.Err()
}

// splitRecipientList splits a recipient field on commas, semicolons and
// whitespace - the field is free text, so any of those may separate addresses.
func splitRecipientList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || unicode.IsSpace(r) })
}

// maskEmailAddress keeps an address's first character and its domain:
// "anita@example.com" -> "a***@example.com". Anything that is not an address
// shows nothing of itself.
func maskEmailAddress(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at <= 0 {
		return "***"
	}
	_, size := utf8.DecodeRuneInString(addr)
	return addr[:size] + "***" + addr[at:]
}

// webhookHostOnly is a webhook URL's scheme and host - its path and query can
// carry a token, so they are never shown.
func webhookHostOnly(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "webhook set"
	}
	return u.Scheme + "://" + u.Host
}

// SetReportSchedulePaused pauses (status Inactive - the worker only ever
// selects Active) or resumes one schedule. Owner or Super Admin only.
func SetReportSchedulePaused(tenantID, kind, id, userID, role string, paused bool) error {
	doctype, ok := reportScheduleDoctypes[kind]
	if !ok {
		return ErrReportScheduleNotFound
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	status, action := "Active", "REPORT_SCHEDULE_RESUME"
	if paused {
		status, action = "Inactive", "REPORT_SCHEDULE_PAUSE"
	}
	res, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET status = $1::text, data = jsonb_set(data, '{status}', to_jsonb($1::text)),
		       updated_at = CURRENT_TIMESTAMP, version = version + 1
		WHERE doctype = $2 AND id = $3 AND deleted_at IS NULL AND ($4 OR created_by = $5)`, schema),
		status, doctype, id, HasFullAccess(role), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrReportScheduleNotFound
	}
	LogAuditEvent(tenantID, userID, action, "SUCCESS", doctype+" "+id)
	return nil
}

// ReportScheduleRun is a claimed Run now, ready to run.
type ReportScheduleRun struct {
	schema, tenantID, doctype, id, runBy string
	data                                 map[string]interface{}
}

// ClaimReportScheduleRun checks the caller may run this schedule now and
// marks it Running, under a row lock so two clicks cannot both claim it. A
// schedule that is already running, or ran less than a minute ago, is
// refused. Owner or Super Admin only; a paused schedule may still be run now.
func ClaimReportScheduleRun(tenantID, kind, id, userID, role string) (*ReportScheduleRun, error) {
	doctype, ok := reportScheduleDoctypes[kind]
	if !ok {
		return nil, ErrReportScheduleNotFound
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var dataStr, owner string
	err = tx.QueryRow(fmt.Sprintf(`SELECT data, COALESCE(created_by, '') FROM %s.documents
		WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL FOR UPDATE`, schema), doctype, id).Scan(&dataStr, &owner)
	if err == sql.ErrNoRows {
		return nil, ErrReportScheduleNotFound
	}
	if err != nil {
		return nil, err
	}
	if owner != userID && !HasFullAccess(role) {
		return nil, ErrReportScheduleNotFound
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil || data == nil {
		return nil, fmt.Errorf("this schedule's saved settings cannot be read - open and save it again")
	}
	if last, _ := data["last_run_at"].(string); last != "" {
		if t, perr := time.Parse(time.RFC3339, last); perr == nil {
			since := time.Since(t)
			if lastStatus, _ := data["last_run_status"].(string); lastStatus == "Running" && since >= 0 && since < reportRunHardTimeout {
				return nil, &ReportScheduleBusyError{"this schedule is already running - wait for it to finish"}
			}
			if since >= 0 && since < reportScheduleRunGap {
				return nil, &ReportScheduleBusyError{"this schedule ran less than a minute ago - wait a minute before running it again"}
			}
		}
	}
	data["last_run_at"] = time.Now().Format(time.RFC3339)
	data["last_run_status"] = "Running"
	marshaled, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = $2 AND id = $3`, schema),
		marshaled, doctype, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "REPORT_SCHEDULE_RUN_NOW", "SUCCESS", doctype+" "+id)
	return &ReportScheduleRun{schema: schema, tenantID: tenantID, doctype: doctype, id: id, runBy: userID, data: data}, nil
}

// Run delivers the claimed schedule through the worker's own per-schedule
// step and advances it exactly as a scheduled run would.
func (r *ReportScheduleRun) Run(ctx context.Context) {
	if r.doctype == "DashboardDigest" {
		runDashboardDigest(ctx, r.schema, r.tenantID, r.id, r.runBy, r.data)
		return
	}
	runScheduledReport(ctx, r.schema, r.tenantID, r.id, r.runBy, r.data)
}

// ReportRunEntry is one row of a report's recent runs.
type ReportRunEntry struct {
	RunBy      string `json:"run_by"` // "" = the schedule worker
	RunAt      string `json:"run_at"`
	DurationMs int64  `json:"duration_ms"`
	RowCount   int64  `json:"row_count"`
}

// ListRecentReportRuns reads ReportRunLog for one report, newest first. A
// Super Admin sees every run, including the schedule worker's; anyone else
// sees only their own.
func ListRecentReportRuns(tenantID, reportID, userID, role string, limit int) ([]ReportRunEntry, error) {
	def, ok := reportRegistry[reportID]
	if !ok {
		return nil, fmt.Errorf("unknown report %q", reportID)
	}
	if err := RequireModules(tenantID, "reports", def.ModuleKey); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// created_at is a timestamp without time zone holding the session zone's
	// wall clock; ::timestamptz reads it in that zone, so run_at is a real
	// instant rather than local time mislabelled as UTC.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'user_id', ''), created_at::timestamptz,
		       COALESCE((data->>'duration_ms')::numeric, 0)::bigint, COALESCE((data->>'row_count')::numeric, 0)::bigint
		FROM %s.documents
		WHERE doctype = 'ReportRunLog' AND data->>'report_id' = $1 AND ($2 OR data->>'user_id' = $3)
		ORDER BY created_at DESC LIMIT $4`, schema), reportID, HasFullAccess(role), userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportRunEntry{}
	for rows.Next() {
		var e ReportRunEntry
		var at time.Time
		if err := rows.Scan(&e.RunBy, &at, &e.DurationMs, &e.RowCount); err != nil {
			return nil, err
		}
		e.RunAt = at.Format(time.RFC3339)
		out = append(out, e)
	}
	return out, rows.Err()
}
