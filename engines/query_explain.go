package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
)

// Stage 47.9.3d (approved 2026-11-15, ask 191): EXPLAIN (ANALYZE, BUFFERS)
// for one statement the platform operator pastes on the Control Panel - the
// evidence the slow-query table cannot give on its own. As approved:
// operator-only (the route), one read-only SELECT, in a read-only transaction
// with a timeout. On top of that the transaction is always rolled back, so a
// session setting a function might make is undone; the statement is prepared,
// so the server refuses a second one; functions that act outside the
// transaction (signals, advisory locks, files, dynamic SQL) are refused by
// name; and only one plan runs at a time. The plan comes back, never rows,
// and a failure that could quote a stored value (a failed cast does) comes
// back as its SQLSTATE only.

// explainTimeout bounds one plan.
const explainTimeout = 10 * time.Second

// maxExplainBytes bounds the statement.
const maxExplainBytes = 20000

// QueryPlan is one EXPLAIN (ANALYZE, BUFFERS) result.
type QueryPlan struct {
	Tenant string   `json:"tenant"`
	Plan   []string `json:"plan"`
	TookMs int64    `json:"took_ms"`
}

// ExplainRefusal is a statement the action will not run, or a failure the
// operator can act on; its text is safe to show.
type ExplainRefusal struct{ msg string }

func (e *ExplainRefusal) Error() string { return e.msg }

func refuseExplain(format string, a ...interface{}) error {
	return &ExplainRefusal{msg: fmt.Sprintf(format, a...)}
}

var (
	explainStart = regexp.MustCompile(`(?is)^\s*(select|with|table|values)\b`)
	// Functions whose effect is outside the transaction, or that run SQL
	// handed to them as text. Matched with identifier quotes removed.
	explainDenied = regexp.MustCompile(`(?i)\b(pg_terminate_backend|pg_cancel_backend|pg_reload_conf|pg_rotate_logfile\w*|pg_read_file|pg_read_binary_file|pg_ls_\w+|pg_stat_reset\w*|pg_stat_statements_reset|pg_advisory\w*|pg_try_advisory\w*|pg_sleep\w*|pg_switch_wal|pg_promote|pg_create_\w+|pg_drop_\w+|pg_replication_\w+|pg_logical_\w+|pg_file_\w+|pg_logdir_ls|pg_notify|pg_export_snapshot|set_config|lo_\w+|dblink\w*|query_to_xml\w*|cursor_to_xml\w*|xpath\w*)\s*\(`)
	explainParam  = regexp.MustCompile(`\$\d+`)
	explainMasked = regexp.MustCompile(`(?i)\btenant_\?\.`)
	explainOne    sync.Mutex
)

// checkExplainStatement returns the statement to run, or a refusal.
func checkExplainStatement(stmt string) (string, error) {
	s := strings.TrimSpace(stmt)
	s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
	switch {
	case s == "":
		return "", refuseExplain("Paste a SELECT statement")
	case len(s) > maxExplainBytes:
		return "", refuseExplain("The statement is longer than %d characters", maxExplainBytes)
	case !explainStart.MatchString(s):
		return "", refuseExplain("Only a SELECT statement can be explained")
	case strings.Contains(s, ";"):
		return "", refuseExplain("One statement only")
	case strings.Contains(s, "--") || strings.Contains(s, "/*"):
		return "", refuseExplain("Remove the comments first")
	case strings.Contains(strings.ToUpper(s), `U&"`) || strings.Contains(strings.ToUpper(s), `U&'`):
		return "", refuseExplain("Unicode-escaped names are not accepted")
	case explainParam.MatchString(s):
		return "", refuseExplain("Put a real value in place of each $n first")
	}
	if m := explainDenied.FindString(strings.ReplaceAll(s, `"`, "")); m != "" {
		return "", refuseExplain("%s is not allowed here", strings.TrimSpace(strings.TrimSuffix(m, "(")))
	}
	return s, nil
}

// ExplainStatement runs EXPLAIN (ANALYZE, BUFFERS) on stmt in tenantID's
// schema. A slow-query fingerprint's masked schema (tenant_?.) is read as that
// client's.
func ExplainStatement(ctx context.Context, tenantID, stmt string) (*QueryPlan, error) {
	s, err := checkExplainStatement(stmt)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	s = explainMasked.ReplaceAllString(s, pq.QuoteIdentifier(schema)+".")
	if !explainOne.TryLock() {
		return nil, refuseExplain("Another plan is running - try again when it ends")
	}
	defer explainOne.Unlock()

	ctx, cancel := context.WithTimeout(ctx, explainTimeout+5*time.Second)
	defer cancel()
	tx, err := db.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() // never committed
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`SET LOCAL statement_timeout = %d`, explainTimeout.Milliseconds())); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL search_path TO `+pq.QuoteIdentifier(schema)+`, public`); err != nil {
		return nil, err
	}
	start := time.Now()
	prep, err := tx.PrepareContext(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) `+s)
	if err != nil {
		return nil, explainFailure(err)
	}
	defer prep.Close()
	rows, err := prep.QueryContext(ctx)
	if err != nil {
		return nil, explainFailure(err)
	}
	defer rows.Close()
	plan := &QueryPlan{Tenant: tenantID}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		plan.Plan = append(plan.Plan, line)
	}
	if err := rows.Err(); err != nil {
		return nil, explainFailure(err)
	}
	plan.TookMs = time.Since(start).Milliseconds()
	return plan, nil
}

// explainFailure turns a database error into text that cannot carry a stored
// value: syntax and naming errors keep their message, the rest their code.
func explainFailure(err error) error {
	var pe *pq.Error
	if !errors.As(err, &pe) {
		return err
	}
	code := string(pe.Code)
	switch {
	case code == "57014":
		return refuseExplain("Stopped after %d s", int(explainTimeout.Seconds()))
	case code == "25006":
		return refuseExplain("Only a read-only statement can be explained")
	case strings.HasPrefix(code, "42"):
		return refuseExplain("%s", pe.Message)
	}
	return refuseExplain("The statement failed (SQLSTATE %s)", code)
}
