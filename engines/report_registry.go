package engines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Stage 20 Track B.4 (20.35): a generic, reusable report framework
// replacing the previous shape (one hardcoded Go function + one hardcoded
// frontend render function per report, engines/reports.go). Deliberately
// NOT a full ad hoc SQL query builder - this ERP's scope doesn't need
// user-authored arbitrary SQL (an injection-risk feature this codebase's
// lightweight/solid principle argues against); instead every report is a
// fixed, developer-defined Go function registered once, with its
// parameters/columns described as data so ONE frontend screen can drive
// any of them - adding a new report from here on is "register a function,"
// not "write a new endpoint + a new render function."

// ReportColumn describes one output column - drives the generic frontend
// table and (via Sensitive) column-level masking (Stage 20.39).
type ReportColumn struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Sensitive bool   `json:"sensitive"`
}

// ReportParam describes one input field, rendered generically by the
// frontend (date/text/select input) rather than each report needing its
// own bespoke filter form.
type ReportParam struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"` // "date", "text", "select"
	Options  string `json:"options,omitempty"`
	Required bool   `json:"required"`
}

// ReportRunFunc executes a report and returns generic rows keyed by each
// ReportColumn's Key.
type ReportRunFunc func(tenantID string, params map[string]string) ([]map[string]interface{}, error)

// ReportDrillDownFunc returns the transaction-level rows behind one summary
// row (Stage 20.38) - optional per report.
type ReportDrillDownFunc func(tenantID string, rowKey string, params map[string]string) ([]map[string]interface{}, error)

// ReportDefinition is one catalog entry. Existing reports (engines/reports.go
// et al.) are wrapped here as-is rather than rewritten; every Stage 20.40
// catalog addition is registered the same way.
type ReportDefinition struct {
	ID           string              `json:"id"`
	Label        string              `json:"label"`
	Category     string              `json:"category"`
	ModuleKey    string              `json:"module_key"`
	Columns      []ReportColumn      `json:"columns"`
	Params       []ReportParam       `json:"params"`
	HasDrillDown bool                `json:"has_drill_down"`
	Run          ReportRunFunc       `json:"-"`
	DrillDown    ReportDrillDownFunc `json:"-"`
}

var reportRegistry = map[string]ReportDefinition{}
var reportRegistryOrder []string

// RegisterReport adds a definition to the catalog. Called only from this
// package's own init() functions (engines/report_definitions.go) - a
// duplicate ID is a build-time programmer error, so it panics rather than
// silently overwriting or returning a runtime error nothing would check.
func RegisterReport(def ReportDefinition) {
	if _, exists := reportRegistry[def.ID]; exists {
		panic(fmt.Sprintf("report %q already registered", def.ID))
	}
	def.HasDrillDown = def.DrillDown != nil
	if def.ModuleKey == "" {
		def.ModuleKey = ModuleForReportCategory(def.Category)
	}
	reportRegistry[def.ID] = def
	reportRegistryOrder = append(reportRegistryOrder, def.ID)
}

// ListReportDefinitions returns the catalog in registration order, for the
// frontend's generic report picker.
func ListReportDefinitions() []ReportDefinition {
	out := make([]ReportDefinition, 0, len(reportRegistryOrder))
	for _, id := range reportRegistryOrder {
		out = append(out, reportRegistry[id])
	}
	return out
}

// reportFullVisibilityRoles see every column in full; any other role gets
// Sensitive-flagged columns redacted in place (Stage 20.39, matching the
// master plan's own Loophole Matrix control for "report export leaks
// customer or finance data"). Redacted in place, not omitted, so the
// column set the frontend renders stays identical regardless of who asked.
// Stage 40.3: keyed on the canonical name, and every lookup runs the role
// through CanonicalRole first, so the legacy "HR/Admin" still resolves here.
var reportFullVisibilityRoles = map[string]bool{
	RoleSuperAdmin:   true,
	RoleStoreManager: true,
}

const redactedReportValue = "•••"

func maskSensitiveColumns(columns []ReportColumn, rows []map[string]interface{}, role string) []map[string]interface{} {
	if reportFullVisibilityRoles[CanonicalRole(role)] {
		return rows
	}
	var sensitiveKeys []string
	for _, c := range columns {
		if c.Sensitive {
			sensitiveKeys = append(sensitiveKeys, c.Key)
		}
	}
	if len(sensitiveKeys) == 0 {
		return rows
	}
	for _, row := range rows {
		for _, k := range sensitiveKeys {
			if _, ok := row[k]; ok {
				row[k] = redactedReportValue
			}
		}
	}
	return rows
}

// maxSyncReportRows (REPORT-0161, Stage 25.5) caps how many rows a
// synchronous report run (handleRunReport) will return inline - a report
// this heavy should go through the existing async export job
// (CreateReportExportJob) instead. Chosen generously (an ordinary report
// screen render is nowhere near this) rather than tuned to any real observed
// slowdown.
//
// BLD-046 correction: RunReport applies this cap unconditionally (see
// below), including to the async export/scheduled-report/dashboard-digest
// callers - it does not actually exempt them the way an earlier version of
// this comment claimed. That is deliberate as of this pass (an async export
// over the cap fails with a clear REPORT-0285/"Failed: ..." rather than
// silently materializing an unbounded row set), not a bug to fix; a report
// that legitimately needs more rows than this needs a narrower filter or a
// dedicated bulk-export path, not an unbounded one.
const maxSyncReportRows = 5000

// reportMasked reports whether maskSensitiveColumns would actually redact
// anything for this role/column set - shared by RunReport (REPORT-0287
// annotation) and RunReportDrillDown so both stay consistent.
func reportMasked(columns []ReportColumn, role string) bool {
	if reportFullVisibilityRoles[CanonicalRole(role)] {
		return false
	}
	for _, c := range columns {
		if c.Sensitive {
			return true
		}
	}
	return false
}

// RunReport looks up a registered report, validates required params, runs
// it, and applies column masking for the requesting role. masked reports
// REPORT-0287 (Info, non-blocking) so the caller can annotate its response;
// it is never itself a reason to fail the request.
//
// 26.10.7: also times the run and logs it via writeReportRunLog - the
// prerequisite 26.10.6 (dedicated BI data mart/read replica) was itself
// deferred pending, since that item's own gate is "only once real
// report-query load is measured," not a decision that's already been made.
// RunReport is the pre-BLD-046 entry point, kept so the many callers that
// legitimately have no request to abandon (startup warmers, tests) stay
// unchanged. It delegates to RunReportContext with a background context.
func RunReport(tenantID, reportID, role, userID string, params map[string]string) (def *ReportDefinition, rows []map[string]interface{}, masked bool, err error) {
	return RunReportContext(context.Background(), tenantID, reportID, role, userID, params)
}

// RunReportContext is the BLD-046 cancellation-aware form. When ctx is
// cancelled - an abandoned HTTP request, a cancelled async job, a shutting
// down worker - it returns ctx.Err() promptly instead of finishing the run.
//
// Deliberately NOT implemented by threading ctx through ReportRunFunc: that
// would change the signature of all 97 registered reports while their own
// db.DB.Query calls still ignored it, so it would add churn across the
// catalog without cancelling a single statement. Instead the one choke
// point every report already runs through races the run against ctx, so the
// caller is released immediately and - the dominant cost for a large report
// - the row cap check, masking and JSON serialization of a payload nobody
// is waiting for are all skipped. The abandoned run's own query is left to
// finish on its pooled connection and its result is discarded; it is
// time-bounded by reportRunHardTimeout rather than running forever.
func RunReportContext(ctx context.Context, tenantID, reportID, role, userID string, params map[string]string) (def *ReportDefinition, rows []map[string]interface{}, masked bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, false, err
	}
	d, ok := reportRegistry[reportID]
	if !ok {
		return nil, nil, false, fmt.Errorf("unknown report %q", reportID)
	}
	if err := RequireModules(tenantID, "reports", d.ModuleKey); err != nil {
		return nil, nil, false, err
	}
	for _, p := range d.Params {
		if p.Required && params[p.Key] == "" {
			return nil, nil, false, fmt.Errorf("param %q is required", p.Key)
		}
	}
	start := time.Now()
	rows, err = runReportRaced(ctx, d, tenantID, params)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// The caller is gone. No run log and no REPORT-0162 wrapping: this is
		// not the report failing, it is nobody waiting for it any more.
		return nil, nil, false, err
	}
	if err != nil {
		// REPORT-0162 (Stage 25.5): "Report generation failed" - distinct
		// from the unknown-report-id/missing-param checks above (those are
		// caller-input mistakes, GLOBAL-0002-shaped); this is the report's
		// own execution failing (a query error, a bad join), matching the
		// catalog scenario's wording exactly.
		return nil, nil, false, &ValidationError{Code: "REPORT-0162", Message: err.Error()}
	}
	if reportID != "report-performance" {
		writeReportRunLog(tenantID, reportID, userID, time.Since(start).Milliseconds(), len(rows))
	}
	if maxRows := GetSettingInt(tenantID, "platform.max_sync_report_rows"); len(rows) > maxRows {
		return nil, nil, false, &ValidationError{Code: "REPORT-0161", Message: fmt.Sprintf("this report matched %d rows, over the %d-row synchronous limit - narrow the filters or use Export instead", len(rows), maxRows)}
	}
	masked = reportMasked(d.Columns, role)
	rows = maskSensitiveColumns(d.Columns, rows, role)
	return &d, rows, masked, nil
}

// reportRunHardTimeout caps how long a single report run may occupy a
// goroutine and a pooled connection. It exists so an abandoned run (whose
// caller has already been released by RunReportContext) cannot leak
// indefinitely, and so a pathological report still fails instead of hanging
// a worker forever. Generous on purpose: this is a backstop against a
// runaway query, not the row/time budget a report is expected to meet -
// platform.max_sync_report_rows remains the user-facing bound.
const reportRunHardTimeout = 5 * time.Minute

// runReportRaced runs def.Run on its own goroutine and returns as soon as
// either the run finishes or ctx is done, whichever comes first. The
// goroutine is never blocked on the send (buffered channel of 1), so an
// abandoned run always finishes, releases its connection and exits rather
// than parking forever on an unread channel.
func runReportRaced(ctx context.Context, def ReportDefinition, tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	type runOutcome struct {
		rows []map[string]interface{}
		err  error
	}
	done := make(chan runOutcome, 1)
	go func() {
		defer func() {
			// A panic in one report must not take the process down through a
			// goroutine nobody is recovering on, which is a new failure mode
			// introduced by running it off the caller's stack.
			if p := recover(); p != nil {
				done <- runOutcome{nil, fmt.Errorf("report %s panicked: %v", def.ID, p)}
			}
		}()
		rows, err := def.Run(tenantID, params)
		done <- runOutcome{rows, err}
	}()
	timeout := time.NewTimer(reportRunHardTimeout)
	defer timeout.Stop()
	select {
	case out := <-done:
		return out.rows, out.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timeout.C:
		return nil, context.DeadlineExceeded
	}
}

// IsCancellation reports whether err is an abandoned-caller signal rather
// than a real failure. The one predicate every BLD-046 path (report, export,
// import, job) and the HTTP error writers share, so none of them logs a
// client walking away as a server error or burns a retry attempt on it.
func IsCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// RunReportDrillDownContext is the cancellation-aware form of
// RunReportDrillDown, racing the drill-down the same way RunReportContext
// races the main run.
func RunReportDrillDownContext(ctx context.Context, tenantID, reportID, role, rowKey string, params map[string]string) ([]map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	def, ok := reportRegistry[reportID]
	if !ok {
		return nil, fmt.Errorf("unknown report %q", reportID)
	}
	if err := RequireModules(tenantID, "reports", def.ModuleKey); err != nil {
		return nil, err
	}
	if def.DrillDown == nil {
		return nil, fmt.Errorf("report %q has no drill-down", reportID)
	}
	wrapped := def
	wrapped.Run = func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
		return def.DrillDown(tenantID, rowKey, params)
	}
	rows, err := runReportRaced(ctx, wrapped, tenantID, params)
	if err != nil {
		return nil, err
	}
	return maskSensitiveColumns(def.Columns, rows, role), nil
}

// RunReportDrillDown runs a registered report's drill-down function (Stage
// 20.38), masking the same way RunReport does.
func RunReportDrillDown(tenantID, reportID, role, rowKey string, params map[string]string) ([]map[string]interface{}, error) {
	def, ok := reportRegistry[reportID]
	if !ok {
		return nil, fmt.Errorf("unknown report %q", reportID)
	}
	if err := RequireModules(tenantID, "reports", def.ModuleKey); err != nil {
		return nil, err
	}
	if def.DrillDown == nil {
		return nil, fmt.Errorf("report %q has no drill-down", reportID)
	}
	rows, err := def.DrillDown(tenantID, rowKey, params)
	if err != nil {
		return nil, err
	}
	return maskSensitiveColumns(def.Columns, rows, role), nil
}

// structsToRows converts a slice of any JSON-taggable struct (or a single
// struct, wrapped in a 1-element result) into generic rows keyed by its own
// json tags - one universal adapter so existing typed report functions
// (e.g. []SalesRegisterEntry, *GSTReturnSummary) register without a
// bespoke converter each.
func structsToRows(v interface{}) ([]map[string]interface{}, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	trimmed := len(b) > 0 && b[0] == '{'
	if trimmed {
		var row map[string]interface{}
		if err := json.Unmarshal(b, &row); err != nil {
			return nil, err
		}
		return []map[string]interface{}{row}, nil
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []map[string]interface{}{}
	}
	return rows, nil
}
