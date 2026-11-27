package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
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
	// Type (Stage 75.3.4) is how the screen shows the value: "" text, or
	// "image" (the value is a media id, drawn as a thumbnail), or "doclink" (148.34: opens the
	// document named by <key>_doctype and <key>_id). Total sums the
	// column under the table.
	Type  string `json:"type,omitempty"`
	Total bool   `json:"total,omitempty"`
	// FinanceSees (157.4b): a Sensitive column a finance.statements holder also
	// reads in full, because the report's own endpoint already gives it to them.
	FinanceSees bool `json:"-"`
	// PayrollSees (278.2): a Sensitive column an hr.payroll holder also reads
	// in full - payroll totals, which that role keys and runs.
	PayrollSees bool `json:"-"`
}

// ReportParam describes one input field, rendered generically by the
// frontend (date/text/select input) rather than each report needing its
// own bespoke filter form.
type ReportParam struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"` // "date", "text", "select", "link" (Options = doctype), "segment"
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
	// Stage 73.2.19 / 55.20: filled by ListReportDefinitions from
	// control_totals.go and report_questions.go, never set by a report.
	HasControlTotals bool   `json:"has_control_totals"`
	Question         string `json:"question,omitempty"`
	Description      string `json:"description,omitempty"`
	// Number (Stage 139.2) is the report's permanent number, R1001: filled
	// from reportNumbers (docs/specs/reports/report_numbers.tsv), never set
	// by a report.
	Number string `json:"number,omitempty"`
	// StoreKey (Stage 75.3.3) is the row field carrying the store, for store
	// scope; empty = the first store-named column, "-" = no store dimension.
	StoreKey string `json:"-"`
	// RunPage (Stage 157.4) is an optional paged form of Run: the report's
	// own query takes the page (LIMIT / OFFSET) and returns the true total, so
	// an on-screen run of a big book is not refused at the export cap. Nil =
	// the report is sliced after a capped run.
	RunPage ReportRunPageFunc `json:"-"`
}

// ReportRunPageFunc runs one page of a report and returns that page's rows
// with the total number of rows the filters match.
type ReportRunPageFunc func(tenantID string, params map[string]string, page ReportPage) ([]map[string]interface{}, int, error)

// ReportPage (Stage 157.4) is the page an on-screen run asks for. Set on the
// context with WithReportPage; Total is filled by the run. Scheduled reports
// and dashboards carry no page, so the setting's row limit still applies to
// them. Limit 0 asks for every row, uncut (control totals need them all).
type ReportPage struct {
	Limit  int
	Offset int
	Desc   bool // the report's order reversed; for a ledger, newest first
	Total  int
	// Totals (157.4b) sums the report's Total columns over every row, so the
	// line under a page is the report's total. Nil from a report's own paged
	// query, and a column masked for the role is left out.
	Totals map[string]float64
	// Stores is what a RunPage must cut to when StoreCut is set: a store-limited
	// user's stores. Empty then means no rows.
	Stores   []string
	StoreCut bool
}

// sortedStringKeys gives a report built from a map one order: pages are cut
// from separate runs, so a row must stay where it was between them (157.4b).
func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// maxReportRunRows (157.4b) is the most rows one run may load into memory:
// the rows are all built before any page or CSV is cut from them, and the
// server is small. An admin may raise it with platform.max_sync_report_rows.
// A report with a RunPage loads one page at a time, so on screen it has no
// limit. A report's own SQL stops at LIMIT 20001, this plus one, so the
// runner can tell that it stopped reading.
const maxReportRunRows = 20000

// reportTooBig is REPORT-0161, the honest answer when a run cannot return
// every row.
func reportTooBig(limit int) error {
	message := fmt.Sprintf("It matched more than %d rows.", limit)
	if limit < maxReportRunRows {
		message += fmt.Sprintf(" Export takes up to %d.", maxReportRunRows)
	}
	return &ValidationError{Code: "REPORT-0161", Message: message}
}

// reportReadStopped is REPORT-0161 from inside a report whose read stopped at
// its own LIMIT: whatever it would build from those rows is part of the answer.
func reportReadStopped(limit int) error {
	return &ValidationError{Code: "REPORT-0161", Message: fmt.Sprintf("It read more than %d records.", limit)}
}

type reportPageKey struct{}
type reportExportKey struct{}

// WithReportPage asks RunReportContext for one page of the report.
func WithReportPage(ctx context.Context, p *ReportPage) context.Context {
	return context.WithValue(ctx, reportPageKey{}, p)
}

// withReportExport marks an export's run: it reads up to maxReportRunRows, not
// the lower on-screen setting, because its rows go to a file.
func withReportExport(ctx context.Context) context.Context {
	return context.WithValue(ctx, reportExportKey{}, true)
}

// slicePage cuts an already-run result to the requested page and records the
// total and the column totals over every row.
func slicePage(rows []map[string]interface{}, columns []ReportColumn, masked bool, p *ReportPage) []map[string]interface{} {
	p.Total = len(rows)
	for _, c := range columns {
		if !c.Total || (masked && c.Sensitive) {
			continue
		}
		if p.Totals == nil {
			p.Totals = map[string]float64{}
		}
		sum := 0.0
		for _, r := range rows {
			sum += numFromInterface(r[c.Key])
		}
		p.Totals[c.Key] = round2(sum)
	}
	if p.Desc {
		rev := make([]map[string]interface{}, len(rows))
		for i, r := range rows {
			rev[len(rows)-1-i] = r
		}
		rows = rev
	}
	if p.Offset >= len(rows) {
		return []map[string]interface{}{}
	}
	end := p.Offset + p.Limit
	if end > len(rows) {
		end = len(rows)
	}
	return rows[p.Offset:end]
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
		def := reportRegistry[id]
		def.HasControlTotals = HasReportControlTotals(id)
		if q, ok := reportQuestions[id]; ok {
			def.Question, def.Description = q.Question, q.Description
		}
		def.Number = reportNumbers[id]
		out = append(out, def)
	}
	return out
}

// ReportAllowedForRole (140.12.10): a finance report - trial balance, ledgers,
// statements, registers - carries the same right as the finance screens
// (finance.statements), so a role refused /finance/trial-balance cannot read
// the same figures through the report runner, an export, a dashboard tile or a
// schedule. Every other report is open to any role with the module.
func ReportAllowedForRole(def ReportDefinition, role string) bool {
	if def.ModuleKey != "finance" {
		return true
	}
	return HasFullAccess(role) || RoleHasCapability(role, "finance.statements")
}

func reportRefused(def ReportDefinition) error {
	return &ValidationError{Code: "GLOBAL-0011", Message: fmt.Sprintf("%s is a finance report - it needs a finance role", def.Label)}
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
	RoleAdmin:        true,
	RoleStoreManager: true,
}

const redactedReportValue = "•••"

func reportColumnMasked(c ReportColumn, role string) bool {
	if !c.Sensitive || reportFullVisibilityRoles[CanonicalRole(role)] {
		return false
	}
	if c.PayrollSees && RoleHasCapability(role, SensitiveCategoryPayroll) {
		return false
	}
	return !c.FinanceSees || !RoleHasCapability(role, "finance.statements")
}

func maskSensitiveColumns(columns []ReportColumn, rows []map[string]interface{}, role string) []map[string]interface{} {
	if reportFullVisibilityRoles[CanonicalRole(role)] {
		return rows
	}
	var sensitiveKeys []string
	for _, c := range columns {
		if reportColumnMasked(c, role) {
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
	for _, c := range columns {
		if reportColumnMasked(c, role) {
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
// is waiting for are all skipped. Since 283.3 the run's goroutine is bound to
// ctx (db.BindContext), so the abandoned run's own queries are cancelled in
// Postgres too, and reportRunHardTimeout cancels a run that outlives it.
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
	if !ReportAllowedForRole(d, role) {
		return nil, nil, false, reportRefused(d)
	}
	for _, p := range d.Params {
		if p.Required && params[p.Key] == "" {
			return nil, nil, false, fmt.Errorf("param %q is required", p.Key)
		}
	}
	// Stage 75.3.3: a store-limited user sees only their stores.
	storeAccess, storeLimited := reportStoreScope(tenantID, role, userID)
	if storeLimited {
		if err := checkReportStoreParams(d, params, storeAccess); err != nil {
			return nil, nil, false, err
		}
	}
	start := time.Now()
	// 157.4 / 157.4b: an on-screen page and an export read up to the ceiling;
	// scheduled reports and dashboards stay at the setting. A report with its
	// own paged query loads only the page, cut to the user's stores in SQL so
	// paging and the count stay true for a store-limited user.
	page, _ := ctx.Value(reportPageKey{}).(*ReportPage)
	exporting, _ := ctx.Value(reportExportKey{}).(bool)
	onePage := page != nil && page.Limit > 0
	limit := GetSettingInt(tenantID, "platform.max_sync_report_rows")
	if page != nil || exporting {
		limit = max(limit, maxReportRunRows)
	}
	storeKey := ""
	if storeLimited {
		storeKey = reportStoreKey(d)
	}
	sqlPaged := onePage && d.RunPage != nil
	runDef := d
	sqlTotal := 0
	if d.RunPage != nil {
		req := ReportPage{Limit: limit + 1}
		if onePage {
			req = *page
		}
		if storeKey != "" {
			req.Stores, req.StoreCut = reportStoreList(storeAccess), true
		}
		runDef.Run = func(t string, p map[string]string) ([]map[string]interface{}, error) {
			out, total, perr := d.RunPage(t, p, req)
			sqlTotal = total
			return out, perr
		}
	}
	rows, err = runReportRaced(ctx, runDef, tenantID, params)
	if err == errReportRunsBusy {
		return nil, nil, false, err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// The caller is gone. No run log and no REPORT-0162 wrapping: this is
		// not the report failing, it is nobody waiting for it any more.
		return nil, nil, false, err
	}
	var stopped *ValidationError
	if errors.As(err, &stopped) && stopped.Code == "REPORT-0161" {
		return nil, nil, false, stopped // the report's own read stopped short
	}
	if errors.As(err, &stopped) && stopped.Code == "GLOBAL-0002" {
		// 176.1: the report refused a bad filter (a date typed 14-11-2026). That
		// is the caller's input, not the report failing - 422, not 503.
		return nil, nil, false, stopped
	}
	if errors.As(err, &stopped) && stopped.SubFor != "" {
		// 245.2: a refusal that names a field (Listing drift's "choose a
		// channel") is the caller's input too; it keeps its own code.
		return nil, nil, false, stopped
	}
	if err != nil {
		// REPORT-0162 (Stage 25.5): "Report generation failed" - distinct
		// from the unknown-report-id/missing-param checks above (those are
		// caller-input mistakes, GLOBAL-0002-shaped); this is the report's
		// own execution failing (a query error, a bad join), matching the
		// catalog scenario's wording exactly.
		return nil, nil, false, &ValidationError{Code: "REPORT-0162", Message: err.Error()}
	}
	// The report's own SQL stopped reading: a store cut, a count or a page taken
	// from here would be part of the answer.
	if len(rows) == maxReportRunRows+1 {
		return nil, nil, false, reportTooBig(maxReportRunRows)
	}
	if storeKey != "" && d.RunPage == nil {
		rows = filterReportRowsByStore(rows, storeKey, storeAccess)
	}
	if reportID != "report-performance" {
		ranRows := len(rows)
		if sqlPaged {
			ranRows = sqlTotal
		}
		writeReportRunLog(tenantID, reportID, userID, time.Since(start).Milliseconds(), ranRows)
	}
	if !sqlPaged && len(rows) > limit {
		return nil, nil, false, reportTooBig(limit)
	}
	masked = reportMasked(d.Columns, role)
	if sqlPaged {
		page.Total = sqlTotal
	} else if onePage {
		rows = slicePage(rows, d.Columns, masked, page)
	}
	// 49.11.1: a report with a sensitive column read unmasked is a security
	// event, in the caller's own role when a wrapper ran it on their behalf.
	readRole := role
	if real, ok := ctx.Value(reportReadRoleKey{}).(string); ok && real != "" {
		readRole = real
	}
	logSensitiveReportRead(ctx, tenantID, userID, readRole, reportID, d.Columns, len(rows), params)
	rows = maskSensitiveColumns(d.Columns, rows, role)
	rows = maskContactColumns(tenantID, d.Columns, rows, role)
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
	if !acquireReportRun(tenantID) {
		return nil, errReportRunsBusy
	}
	// 283.3: the run's queries stop in Postgres when the caller leaves or the
	// hard timeout passes, and the run slot frees with them.
	runCtx, stopRun := context.WithTimeout(ctx, reportRunHardTimeout)
	defer stopRun()
	done := make(chan runOutcome, 1)
	go func() {
		defer releaseReportRun(tenantID)
		defer db.BindContext(runCtx)()
		defer db.ExemptStatementTimeout(reportRunHardTimeout)() // 49.13.3a: a report keeps its own bound
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

// Stage 49.13.3: a run the caller walked away from (or that passed
// reportRunHardTimeout) keeps its goroutine and pooled connection until its
// query ends - since 283.3 that is when Postgres has cancelled it, but code
// between queries and work on other goroutines still runs on. So every
// LIVE run is counted, abandoned or not, per client and process-wide, and a
// new run past either bound is refused up front instead of piling more
// queries onto a pool that is already busy with ones nobody reads.
const reportRunsMaxTotal = 20 // of db's 50 pooled connections

var errReportRunsBusy = &ValidationError{Code: "GLOBAL-0310",
	Message: "too many reports are still running for this business - try again in a minute, or use Export for a large one"}

var reportRuns = struct {
	sync.Mutex
	perTenant map[string]int
	total     int
}{perTenant: map[string]int{}}

func reportRunsMaxFor(tenantID string) int {
	return max(1, GetSettingInt(tenantID, "platform.per_tenant_max_concurrent_requests")/2)
}

func acquireReportRun(tenantID string) bool {
	limit := reportRunsMaxFor(tenantID)
	reportRuns.Lock()
	defer reportRuns.Unlock()
	if reportRuns.total >= reportRunsMaxTotal || reportRuns.perTenant[tenantID] >= limit {
		return false
	}
	reportRuns.total++
	reportRuns.perTenant[tenantID]++
	return true
}

func releaseReportRun(tenantID string) {
	reportRuns.Lock()
	defer reportRuns.Unlock()
	reportRuns.total--
	if reportRuns.perTenant[tenantID] > 1 {
		reportRuns.perTenant[tenantID]--
	} else {
		delete(reportRuns.perTenant, tenantID)
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
func RunReportDrillDownContext(ctx context.Context, tenantID, reportID, role, rowKey string, params map[string]string, userID ...string) ([]map[string]interface{}, error) {
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
	if !ReportAllowedForRole(def, role) {
		return nil, reportRefused(def)
	}
	if def.DrillDown == nil {
		return nil, fmt.Errorf("report %q has no drill-down", reportID)
	}
	// Stage 75.3.3: the drill-down is cut to the user's stores too - a row
	// key is typed by the caller, so it must not open another store's detail.
	var storeAccess StoreAccess
	storeLimited := false
	if len(userID) > 0 {
		storeAccess, storeLimited = reportStoreScope(tenantID, role, userID[0])
		if storeLimited {
			if err := checkReportStoreParams(def, params, storeAccess); err != nil {
				return nil, err
			}
		}
	}
	wrapped := def
	wrapped.Run = func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
		return def.DrillDown(tenantID, rowKey, params)
	}
	rows, err := runReportRaced(ctx, wrapped, tenantID, params)
	if err != nil {
		return nil, err
	}
	if storeLimited {
		if key := reportStoreKey(def); key != "" {
			if len(rows) > 0 {
				if _, has := rows[0][key]; !has {
					return nil, &ValidationError{Code: "REPORT-0987", Message: "this detail is not broken down by store, and your access covers only some stores"}
				}
			}
			rows = filterReportRowsByStore(rows, key, storeAccess)
		}
	}
	// 134.6 (47.10.1): a drill-down is held to the same row cap as the report.
	if maxRows := GetSettingInt(tenantID, "platform.max_sync_report_rows"); maxRows > 0 && len(rows) > maxRows {
		return nil, &ValidationError{Code: "REPORT-0161", Message: fmt.Sprintf("This detail has %d rows, and the limit is %d.", len(rows), maxRows)}
	}
	return maskContactColumns(tenantID, def.Columns, maskSensitiveColumns(def.Columns, rows, role), role), nil
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
	if !ReportAllowedForRole(def, role) {
		return nil, reportRefused(def)
	}
	if def.DrillDown == nil {
		return nil, fmt.Errorf("report %q has no drill-down", reportID)
	}
	rows, err := def.DrillDown(tenantID, rowKey, params)
	if err != nil {
		return nil, err
	}
	return maskContactColumns(tenantID, def.Columns, maskSensitiveColumns(def.Columns, rows, role), role), nil
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
