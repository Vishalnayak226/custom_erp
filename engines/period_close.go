package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 73.1.10: period close with owned tasks and a controlled reopen.
//
// Decision D09: posted documents are corrected only by reversal entries,
// never edited; a closed period reopens only through maker-checker (the
// Accountant role - D09's "Finance Head" - or a Super Admin) with a reason
// and an audit trail, and re-closes through the same task list.
//
//   - Every period has five close tasks (periodCloseTaskDefs). Each needs an
//     owner, and only that owner signs it off - Done, or Not applicable -
//     with written evidence. The live system check is shown beside it and
//     frozen into the task at sign-off; a failing check can still be signed
//     when the evidence explains the difference, because explaining it is
//     the owner's job, not the system's.
//   - CloseAccountingPeriod refuses (FIN-0501) until every task is signed.
//   - A closed period reopens only when a PeriodReopenRequest is approved
//     through the existing approval engine (DecideApproval refuses the
//     requester as approver). Approval reopens the period, starts a new close
//     round and returns every task to Open for re-sign-off; nothing posted is
//     touched - corrections are new reversal entries in the reopened period.
//   - period_close_log keeps every assignment, sign-off, undo, close and
//     reopen; LogAuditEvent records the same actions in the audit trail.

type periodCloseTaskDef struct {
	Key, Title, Hint, Link string
}

var periodCloseTaskDefs = []periodCloseTaskDef{
	{"bank", "Bank reconciliation", "Every bank statement line dated in the period is matched, or sits in the exception queue with an owner and reason.", "#/view/bank-reconciliation"},
	{"stock_valuation", "Stock valuation tie-out", "Inventory Valuation agrees with GL 1200 Inventory Control, or the difference is explained.", "report:inventory-valuation"},
	{"ap_tieout", "Payables (AP) tie-out", "Supplier bills are matched, no bill is in mismatch hold, and GRN Suspense (2100) is explained.", "report:payables-ageing"},
	{"ar_tieout", "Receivables (AR) tie-out", "Receivables Ageing agrees with GL 1300 Accounts Receivable.", "report:receivables-ageing"},
	{"settlement", "Marketplace / POS settlement", "Card, UPI and marketplace payouts for the period are matched to bank lines.", "#/view/bank-reconciliation"},
}

// CanManagePeriodClose is D09's "Finance Head / Admin": a Super Admin, the
// Accountant role template (whose job is period close), or a tenant role
// literally named Finance Head.
func CanManagePeriodClose(role string) bool {
	return HasFullAccess(role) || strings.EqualFold(strings.TrimSpace(role), "Accountant") ||
		strings.EqualFold(strings.TrimSpace(role), "Finance Head")
}

type PeriodCloseTask struct {
	Key           string `json:"key"`
	Title         string `json:"title"`
	Hint          string `json:"hint"`
	Link          string `json:"link"`
	Owner         string `json:"owner"`
	Status        string `json:"status"`
	Evidence      string `json:"evidence"`
	EvidenceRef   string `json:"evidence_ref"`
	CheckPassed   bool   `json:"check_passed"`
	CheckDetail   string `json:"check_detail"`
	CheckSnapshot string `json:"check_snapshot"`
	SignedBy      string `json:"signed_by"`
	SignedAt      string `json:"signed_at"`
	CloseRound    int    `json:"close_round"`
}

type PeriodCloseLogEntry struct {
	CloseRound int    `json:"close_round"`
	Event      string `json:"event"`
	TaskKey    string `json:"task_key"`
	Actor      string `json:"actor"`
	Detail     string `json:"detail"`
	CreatedAt  string `json:"created_at"`
}

type PeriodReopenSummary struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	RequestedBy string `json:"requested_by"`
	Applied     bool   `json:"applied"`
	CreatedAt   string `json:"created_at"`
}

type PeriodCloseBoard struct {
	PeriodID     string                `json:"period_id"`
	PeriodName   string                `json:"period_name"`
	StartDate    string                `json:"start_date"`
	EndDate      string                `json:"end_date"`
	Status       string                `json:"status"`
	CloseRound   int                   `json:"close_round"`
	ClosedBy     string                `json:"closed_by"`
	ClosedAt     string                `json:"closed_at"`
	ReopenedBy   string                `json:"reopened_by"`
	ReopenedAt   string                `json:"reopened_at"`
	Tasks        []PeriodCloseTask     `json:"tasks"`
	AllSigned    bool                  `json:"all_signed"`
	Unsigned     int                   `json:"unsigned"`
	Checks       []PeriodCloseCheck    `json:"checks"`
	Reopens      []PeriodReopenSummary `json:"reopen_requests"`
	Log          []PeriodCloseLogEntry `json:"log"`
	CanManage    bool                  `json:"can_manage"`
	ReopenOpenID string                `json:"reopen_pending_id"`
}

// ensurePeriodCloseTasks inserts any missing task rows for the period's
// current round - a period created before 73.1.10 gets its list on first use.
func ensurePeriodCloseTasks(q interface {
	Exec(string, ...interface{}) (sql.Result, error)
}, schema, periodID string, round int) error {
	for _, d := range periodCloseTaskDefs {
		if _, err := q.Exec(fmt.Sprintf(`
			INSERT INTO %s.period_close_tasks (period_id, task_key, title, close_round)
			VALUES ($1, $2, $3, $4) ON CONFLICT (period_id, task_key) DO NOTHING`, schema),
			periodID, d.Key, d.Title, round); err != nil {
			return err
		}
	}
	return nil
}

func periodCloseTaskDefByKey(key string) (periodCloseTaskDef, bool) {
	for _, d := range periodCloseTaskDefs {
		if d.Key == key {
			return d, true
		}
	}
	return periodCloseTaskDef{}, false
}

func writePeriodCloseLog(q interface {
	Exec(string, ...interface{}) (sql.Result, error)
}, schema, periodID string, round int, event, taskKey, actor, detail string) error {
	_, err := q.Exec(fmt.Sprintf(`
		INSERT INTO %s.period_close_log (period_id, close_round, event, task_key, actor, detail)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6)`, schema), periodID, round, event, taskKey, actor, detail)
	return err
}

// usernameForUserID resolves a user id to its username for readable history;
// the id itself when the user cannot be found.
func usernameForUserID(schema, userID string) string {
	var name string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT username FROM %s.users WHERE id = $1`, schema), userID).Scan(&name); err != nil || name == "" {
		return userID
	}
	return name
}

// periodCloseTaskCheck is the live system check shown beside one task. For
// the tie-outs it is the report's own control totals (73.2.19), read now -
// the board says so, because current stock and open items are live figures.
func periodCloseTaskCheck(tenantID, schema, key, startDate, endDate string) (bool, string) {
	switch key {
	case "bank":
		var unmatched, exceptions int
		if err := db.DB.QueryRow(fmt.Sprintf(`
			SELECT COUNT(*) FILTER (WHERE COALESCE(data->>'match_status', 'Unmatched') NOT IN ('Matched', 'Exception')),
			       COUNT(*) FILTER (WHERE data->>'match_status' = 'Exception')
			FROM %s.documents WHERE doctype = 'BankStatementLine' AND data->>'txn_date' BETWEEN $1 AND $2`, schema),
			startDate, endDate).Scan(&unmatched, &exceptions); err != nil {
			return false, "could not read bank statement lines: " + err.Error()
		}
		return unmatched == 0, fmt.Sprintf("%d statement line(s) in the period still unmatched; %d in the exception queue", unmatched, exceptions)
	case "stock_valuation":
		rows, err := GetInventoryValuation(tenantID)
		if err != nil {
			return false, "could not run Inventory Valuation: " + err.Error()
		}
		c, err := inventoryValuationControlTotals(tenantID, nil, rows)
		if err != nil {
			return false, "could not compute control totals: " + err.Error()
		}
		return c.Tied, controlTotalsSentence(c)
	case "ap_tieout":
		var holds int
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'VendorInvoice' AND status = 'MismatchHold'`, schema)).Scan(&holds); err != nil {
			return false, "could not read supplier bills: " + err.Error()
		}
		var grnSuspensePaise int64
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(credit), 0) - COALESCE(SUM(debit), 0) FROM %s.gl_postings
			WHERE account_code = '2100' AND created_at < ($1::date + 1)`, schema), endDate).Scan(&grnSuspensePaise)
		return holds == 0, fmt.Sprintf("%d supplier bill(s) in mismatch hold; GRN Suspense 2100 at period end: %.2f", holds, PaiseToRupees(grnSuspensePaise))
	case "ar_tieout":
		buckets, err := GetReceivablesAgeingReport(tenantID)
		if err != nil {
			return false, "could not run Receivables Ageing: " + err.Error()
		}
		rows, err := structsToRows(buckets)
		if err != nil {
			return false, err.Error()
		}
		c, err := receivablesAgeingControlTotals(tenantID, nil, rows)
		if err != nil {
			return false, "could not compute control totals: " + err.Error()
		}
		return c.Tied, controlTotalsSentence(c)
	case "settlement":
		open, detail := unmatchedSettlementPayouts(schema, startDate, endDate)
		return open == 0, detail
	}
	return false, "unknown task"
}

func controlTotalsSentence(c *ReportControlTotals) string {
	ledger := "no ledger tie"
	if c.LedgerTotal != nil {
		ledger = fmt.Sprintf("ledger %.2f", *c.LedgerTotal)
	}
	verdict := "tied"
	if !c.Tied {
		verdict = "NOT tied"
	}
	return fmt.Sprintf("%s - detail %.2f, summary %.2f, %s (%s)", verdict, c.DetailTotal, c.SummaryTotal, ledger, c.CutOff)
}

// GetPeriodCloseBoard is the close screen: tasks with live checks, the
// advisory checklist, reopen requests and the history.
func GetPeriodCloseBoard(tenantID, periodID, role string) (*PeriodCloseBoard, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	b := &PeriodCloseBoard{PeriodID: periodID, CanManage: CanManagePeriodClose(role)}
	var closedBy, closedAt, reopenedBy, reopenedAt sql.NullString
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT period_name, start_date::text, end_date::text, status, close_round, closed_by, to_char(closed_at, 'YYYY-MM-DD HH24:MI'), reopened_by, to_char(reopened_at, 'YYYY-MM-DD HH24:MI')
		FROM %s.accounting_periods WHERE id = $1`, schema), periodID).Scan(
		&b.PeriodName, &b.StartDate, &b.EndDate, &b.Status, &b.CloseRound, &closedBy, &closedAt, &reopenedBy, &reopenedAt); err != nil {
		return nil, &ValidationError{Code: "FIN-0502", Message: "accounting period not found"}
	}
	if closedBy.Valid {
		b.ClosedBy = usernameForUserID(schema, closedBy.String)
	}
	b.ClosedAt, b.ReopenedBy, b.ReopenedAt = closedAt.String, reopenedBy.String, reopenedAt.String
	if err := ensurePeriodCloseTasks(db.DB, schema, periodID, b.CloseRound); err != nil {
		return nil, err
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT task_key, COALESCE(owner, ''), status, COALESCE(evidence, ''), COALESCE(evidence_ref, ''),
		       COALESCE(check_snapshot, ''), COALESCE(signed_by, ''), COALESCE(to_char(signed_at, 'YYYY-MM-DD HH24:MI'), ''), close_round
		FROM %s.period_close_tasks WHERE period_id = $1`, schema), periodID)
	if err != nil {
		return nil, err
	}
	byKey := map[string]PeriodCloseTask{}
	for rows.Next() {
		var t PeriodCloseTask
		if err := rows.Scan(&t.Key, &t.Owner, &t.Status, &t.Evidence, &t.EvidenceRef, &t.CheckSnapshot, &t.SignedBy, &t.SignedAt, &t.CloseRound); err != nil {
			rows.Close()
			return nil, err
		}
		byKey[t.Key] = t
	}
	rows.Close()
	b.AllSigned = true
	for _, d := range periodCloseTaskDefs {
		t := byKey[d.Key]
		t.Key, t.Title, t.Hint, t.Link = d.Key, d.Title, d.Hint, d.Link
		t.CheckPassed, t.CheckDetail = periodCloseTaskCheck(tenantID, schema, d.Key, b.StartDate, b.EndDate)
		if t.Status == "Open" || t.Status == "" {
			b.AllSigned = false
			b.Unsigned++
		}
		b.Tasks = append(b.Tasks, t)
	}

	if checklist, err := GetPeriodCloseChecklist(tenantID, periodID); err == nil {
		b.Checks = checklist.Checks
	}

	reopenRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, status, COALESCE(data->>'reason', ''), COALESCE(data->>'requested_by', created_by),
		       COALESCE((data->>'applied')::boolean, false), to_char(created_at, 'YYYY-MM-DD HH24:MI')
		FROM %s.documents WHERE doctype = 'PeriodReopenRequest' AND data->>'period_id' = $1
		ORDER BY created_at DESC`, schema), periodID)
	if err != nil {
		return nil, err
	}
	for reopenRows.Next() {
		var s PeriodReopenSummary
		if err := reopenRows.Scan(&s.ID, &s.Status, &s.Reason, &s.RequestedBy, &s.Applied, &s.CreatedAt); err != nil {
			reopenRows.Close()
			return nil, err
		}
		if s.Status == "Pending Approval" || (s.Status == "Approved" && !s.Applied) {
			b.ReopenOpenID = s.ID
		}
		b.Reopens = append(b.Reopens, s)
	}
	reopenRows.Close()

	logRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT close_round, event, COALESCE(task_key, ''), actor, COALESCE(detail, ''), to_char(created_at, 'YYYY-MM-DD HH24:MI')
		FROM %s.period_close_log WHERE period_id = $1 ORDER BY created_at DESC, id DESC LIMIT 200`, schema), periodID)
	if err != nil {
		return nil, err
	}
	defer logRows.Close()
	for logRows.Next() {
		var e PeriodCloseLogEntry
		if err := logRows.Scan(&e.CloseRound, &e.Event, &e.TaskKey, &e.Actor, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		b.Log = append(b.Log, e)
	}
	if b.Reopens == nil {
		b.Reopens = []PeriodReopenSummary{}
	}
	if b.Log == nil {
		b.Log = []PeriodCloseLogEntry{}
	}
	return b, nil
}

// lockOpenPeriod locks the period row and refuses unless it is Open.
func lockOpenPeriod(tx *sql.Tx, schema, periodID string) (name string, round int, err error) {
	var status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT period_name, status, close_round FROM %s.accounting_periods WHERE id = $1 FOR UPDATE`, schema),
		periodID).Scan(&name, &status, &round); err != nil {
		return "", 0, &ValidationError{Code: "FIN-0502", Message: "accounting period not found"}
	}
	if status != "Open" {
		return "", 0, &ValidationError{Code: "FIN-0503", Message: fmt.Sprintf("period '%s' is %s - close tasks change only while it is Open; request a reopen first", name, status)}
	}
	return name, round, nil
}

// AssignPeriodCloseTask names a task's owner (Finance Head / Admin only).
func AssignPeriodCloseTask(tenantID, periodID, taskKey, owner, actor, actorRole string) error {
	if !CanManagePeriodClose(actorRole) {
		return &ValidationError{Code: "FIN-0504", Message: "only an Accountant or an Admin can assign close tasks"}
	}
	def, ok := periodCloseTaskDefByKey(taskKey)
	if !ok {
		return &ValidationError{Code: "FIN-0502", Message: fmt.Sprintf("unknown close task %q", taskKey)}
	}
	owner = strings.TrimSpace(owner)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var active bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.users WHERE username = $1 AND COALESCE(status, 'Active') = 'Active')`, schema), owner).Scan(&active); err != nil {
		return err
	}
	if owner == "" || !active {
		return &ValidationError{Code: "FIN-0505", Message: fmt.Sprintf("'%s' is not an active user - choose the person who will sign this task off", owner)}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	name, round, err := lockOpenPeriod(tx, schema, periodID)
	if err != nil {
		return err
	}
	if err := ensurePeriodCloseTasks(tx, schema, periodID, round); err != nil {
		return err
	}
	var status, previous string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT status, COALESCE(owner, '') FROM %s.period_close_tasks WHERE period_id = $1 AND task_key = $2 FOR UPDATE`, schema),
		periodID, taskKey).Scan(&status, &previous); err != nil {
		return err
	}
	if status != "Open" {
		return &ValidationError{Code: "FIN-0506", Message: fmt.Sprintf("'%s' is already signed off - undo the sign-off before changing its owner", def.Title)}
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.period_close_tasks SET owner = $1 WHERE period_id = $2 AND task_key = $3`, schema), owner, periodID, taskKey); err != nil {
		return err
	}
	detail := fmt.Sprintf("owner set to %s", owner)
	if previous != "" {
		detail = fmt.Sprintf("owner changed from %s to %s", previous, owner)
	}
	if err := writePeriodCloseLog(tx, schema, periodID, round, "TASK_ASSIGNED", taskKey, actor, detail); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "PERIOD_CLOSE_TASK_ASSIGNED", "SUCCESS", fmt.Sprintf("Period '%s' task '%s': %s", name, def.Title, detail))
	return nil
}

// SignOffPeriodCloseTask records the owner's sign-off with evidence.
// status is "Done" or "Not applicable".
func SignOffPeriodCloseTask(tenantID, periodID, taskKey, status, evidence, evidenceRef, actor string) error {
	def, ok := periodCloseTaskDefByKey(taskKey)
	if !ok {
		return &ValidationError{Code: "FIN-0502", Message: fmt.Sprintf("unknown close task %q", taskKey)}
	}
	if status != "Done" && status != "Not applicable" {
		return &ValidationError{Code: "FIN-0507", Message: "sign off as Done or Not applicable"}
	}
	evidence = strings.TrimSpace(evidence)
	if len([]rune(evidence)) < 10 {
		return &ValidationError{Code: "FIN-0507", Message: "write what you checked and what it showed (at least 10 characters) - the evidence is what the sign-off rests on"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	name, round, err := lockOpenPeriod(tx, schema, periodID)
	if err != nil {
		return err
	}
	var startDate, endDate string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT start_date::text, end_date::text FROM %s.accounting_periods WHERE id = $1`, schema), periodID).Scan(&startDate, &endDate); err != nil {
		return err
	}
	if err := ensurePeriodCloseTasks(tx, schema, periodID, round); err != nil {
		return err
	}
	var current, owner string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT status, COALESCE(owner, '') FROM %s.period_close_tasks WHERE period_id = $1 AND task_key = $2 FOR UPDATE`, schema),
		periodID, taskKey).Scan(&current, &owner); err != nil {
		return err
	}
	if current != "Open" {
		return &ValidationError{Code: "FIN-0506", Message: fmt.Sprintf("'%s' is already signed off", def.Title)}
	}
	if owner == "" {
		return &ValidationError{Code: "FIN-0508", Message: fmt.Sprintf("'%s' has no owner yet - an Accountant or Super Admin assigns one first", def.Title)}
	}
	if owner != actor {
		return &ValidationError{Code: "FIN-0508", Message: fmt.Sprintf("'%s' is owned by %s - only the owner signs it off", def.Title, owner)}
	}
	passed, detail := periodCloseTaskCheck(tenantID, schema, taskKey, startDate, endDate)
	snapshot := "check passed: " + detail
	if !passed {
		snapshot = "check NOT passed at sign-off: " + detail
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.period_close_tasks SET status = $1, evidence = $2, evidence_ref = NULLIF($3, ''), check_snapshot = $4,
		       signed_by = $5, signed_at = CURRENT_TIMESTAMP, close_round = $6
		WHERE period_id = $7 AND task_key = $8`, schema),
		status, evidence, strings.TrimSpace(evidenceRef), snapshot, actor, round, periodID, taskKey); err != nil {
		return err
	}
	if err := writePeriodCloseLog(tx, schema, periodID, round, "TASK_SIGNED", taskKey, actor,
		fmt.Sprintf("%s - %s | %s", status, evidence, snapshot)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "PERIOD_CLOSE_TASK_SIGNED", "SUCCESS", fmt.Sprintf("Period '%s' task '%s' signed %s", name, def.Title, status))
	return nil
}

// UndoPeriodCloseTaskSignOff returns a signed task to Open while the period
// is still Open (the owner, or Finance Head / Admin), with a reason.
func UndoPeriodCloseTaskSignOff(tenantID, periodID, taskKey, reason, actor, actorRole string) error {
	def, ok := periodCloseTaskDefByKey(taskKey)
	if !ok {
		return &ValidationError{Code: "FIN-0502", Message: fmt.Sprintf("unknown close task %q", taskKey)}
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return &ValidationError{Code: "FIN-0507", Message: "say why the sign-off is being undone"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	name, round, err := lockOpenPeriod(tx, schema, periodID)
	if err != nil {
		return err
	}
	var current, owner string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT status, COALESCE(owner, '') FROM %s.period_close_tasks WHERE period_id = $1 AND task_key = $2 FOR UPDATE`, schema),
		periodID, taskKey).Scan(&current, &owner); err != nil {
		return &ValidationError{Code: "FIN-0502", Message: "close task not found"}
	}
	if current == "Open" {
		return &ValidationError{Code: "FIN-0506", Message: fmt.Sprintf("'%s' is not signed off", def.Title)}
	}
	if actor != owner && !CanManagePeriodClose(actorRole) {
		return &ValidationError{Code: "FIN-0508", Message: "only the task owner, an Accountant or a Super Admin can undo a sign-off"}
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.period_close_tasks SET status = 'Open', evidence = NULL, evidence_ref = NULL, check_snapshot = NULL,
		       signed_by = NULL, signed_at = NULL WHERE period_id = $1 AND task_key = $2`, schema), periodID, taskKey); err != nil {
		return err
	}
	if err := writePeriodCloseLog(tx, schema, periodID, round, "TASK_UNDONE", taskKey, actor, reason); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "PERIOD_CLOSE_TASK_UNDONE", "SUCCESS", fmt.Sprintf("Period '%s' task '%s' sign-off undone: %s", name, def.Title, reason))
	return nil
}

// requireSignedCloseTasks is CloseAccountingPeriod's gate (inside its
// transaction): every task of the current round signed.
func requireSignedCloseTasks(tx *sql.Tx, schema, periodID string, round int) error {
	if err := ensurePeriodCloseTasks(tx, schema, periodID, round); err != nil {
		return err
	}
	rows, err := tx.Query(fmt.Sprintf(`SELECT title FROM %s.period_close_tasks WHERE period_id = $1 AND status = 'Open' ORDER BY id`, schema), periodID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var open []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return err
		}
		open = append(open, title)
	}
	if len(open) > 0 {
		return &ValidationError{Code: "FIN-0501", Message: fmt.Sprintf("cannot close: %d close task(s) not signed off - %s", len(open), strings.Join(open, ", "))}
	}
	return nil
}

// RequestPeriodReopen raises a PeriodReopenRequest for a Closed period and
// routes it to approval. The requester can never approve it.
func RequestPeriodReopen(tenantID, periodID, reason, actorUserID, actorUsername, actorRole string) (string, error) {
	if !CanManagePeriodClose(actorRole) {
		return "", &ValidationError{Code: "FIN-0504", Message: "only an Accountant or an Admin can request a period reopen"}
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) < 10 {
		return "", &ValidationError{Code: "FIN-0507", Message: "say why the period must reopen and what will be corrected (at least 10 characters)"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var name, status string
	var round int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT period_name, status, close_round FROM %s.accounting_periods WHERE id = $1`, schema), periodID).Scan(&name, &status, &round); err != nil {
		return "", &ValidationError{Code: "FIN-0502", Message: "accounting period not found"}
	}
	if status != "Closed" {
		return "", &ValidationError{Code: "FIN-0509", Message: fmt.Sprintf("period '%s' is %s - only a closed period can be reopened", name, status)}
	}
	var pending int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'PeriodReopenRequest'
		AND data->>'period_id' = $1 AND (status = 'Pending Approval' OR (status = 'Approved' AND COALESCE((data->>'applied')::boolean, false) = false))`, schema),
		periodID).Scan(&pending); err != nil {
		return "", err
	}
	if pending > 0 {
		return "", &ValidationError{Code: "FIN-0509", Message: fmt.Sprintf("a reopen request for '%s' is already awaiting a decision", name)}
	}
	docID := NewDocID("PRR")
	payload, err := json.Marshal(map[string]interface{}{
		"period_id": periodID, "period_name": name, "reason": reason, "requested_by": actorUsername, "status": "Draft",
	})
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'PeriodReopenRequest', $2, 'Draft', $3)`, schema),
		docID, payload, actorUserID); err != nil {
		return "", err
	}
	if err := SubmitForApproval(tenantID, "PeriodReopenRequest", docID, actorUserID, actorRole); err != nil {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Failed' WHERE doctype = 'PeriodReopenRequest' AND id = $1`, schema), docID)
		return "", err
	}
	_ = writePeriodCloseLog(db.DB, schema, periodID, round, "REOPEN_REQUESTED", "", actorUsername, fmt.Sprintf("%s: %s", docID, reason))
	LogAuditEvent(tenantID, actorUserID, "PERIOD_REOPEN_REQUESTED", "SUCCESS", fmt.Sprintf("Reopen of period '%s' requested (%s): %s", name, docID, reason))
	return docID, nil
}

// ApplyApprovedPeriodReopen reopens the period an Approved PeriodReopenRequest
// names. Called by DecideApproval's approval hook, and again from the close
// board if that first attempt failed. Idempotent: an already applied request
// is a no-op.
func ApplyApprovedPeriodReopen(tenantID, requestID, approverUserID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'PeriodReopenRequest' AND id = $1 FOR UPDATE`, schema),
		requestID).Scan(&dataStr, &status); err != nil {
		return &ValidationError{Code: "FIN-0502", Message: "reopen request not found"}
	}
	if status != "Approved" {
		return &ValidationError{Code: "FIN-0509", Message: fmt.Sprintf("the reopen request is %s, not Approved", status)}
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	if applied, _ := data["applied"].(bool); applied {
		return nil
	}
	periodID, _ := data["period_id"].(string)
	reason, _ := data["reason"].(string)
	requestedBy, _ := data["requested_by"].(string)
	var name, periodStatus string
	var round int
	if err := tx.QueryRow(fmt.Sprintf(`SELECT period_name, status, close_round FROM %s.accounting_periods WHERE id = $1 FOR UPDATE`, schema),
		periodID).Scan(&name, &periodStatus, &round); err != nil {
		return &ValidationError{Code: "FIN-0502", Message: "the period this request names no longer exists"}
	}
	approver := approverUserID
	if approverUserID == "" {
		// The retry path is called without an approver; take it from the
		// approval log.
		_ = tx.QueryRow(fmt.Sprintf(`SELECT actor_user_id FROM %s.approval_log WHERE doctype = 'PeriodReopenRequest' AND document_id = $1 AND action = 'Approved' ORDER BY id DESC LIMIT 1`, schema), requestID).Scan(&approver)
	}
	approverName := usernameForUserID(schema, approver)
	newRound := round
	if periodStatus == "Closed" {
		newRound = round + 1
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.accounting_periods SET status = 'Open', close_round = $1, closed_by = NULL, closed_at = NULL,
			       reopened_by = $2, reopened_at = CURRENT_TIMESTAMP WHERE id = $3`, schema), newRound, approverName, periodID); err != nil {
			return err
		}
		// Every task returns to Open for the next round; what each one said
		// at the previous close stays in the log.
		taskRows, err := tx.Query(fmt.Sprintf(`SELECT task_key, status, COALESCE(signed_by, ''), COALESCE(evidence, '') FROM %s.period_close_tasks WHERE period_id = $1`, schema), periodID)
		if err != nil {
			return err
		}
		type prior struct{ key, status, by, evidence string }
		var priors []prior
		for taskRows.Next() {
			var p prior
			if err := taskRows.Scan(&p.key, &p.status, &p.by, &p.evidence); err != nil {
				taskRows.Close()
				return err
			}
			priors = append(priors, p)
		}
		taskRows.Close()
		for _, p := range priors {
			if err := writePeriodCloseLog(tx, schema, periodID, round, "TASK_RESET", p.key, approverName,
				fmt.Sprintf("round %d closed with %s by %s: %s", round, p.status, p.by, p.evidence)); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.period_close_tasks SET status = 'Open', evidence = NULL, evidence_ref = NULL, check_snapshot = NULL,
			       signed_by = NULL, signed_at = NULL, close_round = $1 WHERE period_id = $2`, schema), newRound, periodID); err != nil {
			return err
		}
		if err := writePeriodCloseLog(tx, schema, periodID, newRound, "REOPENED", "", approverName,
			fmt.Sprintf("%s requested by %s, approved by %s: %s", requestID, requestedBy, approverName, reason)); err != nil {
			return err
		}
	}
	data["applied"] = true
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PeriodReopenRequest' AND id = $2`, schema), marshaled, requestID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if periodStatus == "Closed" {
		LogAuditEvent(tenantID, approver, "PERIOD_REOPENED", "SUCCESS",
			fmt.Sprintf("Period '%s' reopened by approved request %s (requested by %s): %s - round %d begins", name, requestID, requestedBy, reason, newRound))
	}
	return nil
}
