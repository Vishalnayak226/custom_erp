package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"time"
)

type AccountingPeriod struct {
	ID         string  `json:"id"`
	PeriodName string  `json:"period_name"`
	StartDate  string  `json:"start_date"`
	EndDate    string  `json:"end_date"`
	Status     string  `json:"status"`
	ClosedBy   *string `json:"closed_by,omitempty"`
	ClosedAt   *string `json:"closed_at,omitempty"`
	CreatedBy  string  `json:"created_by"`
	CreatedAt  string  `json:"created_at"`
}

// CreateAccountingPeriod registers a new Open period. Rejects a date range
// that overlaps any existing period (Open or Closed) - overlapping ranges
// would make "is today inside a closed period" ambiguous.
func CreateAccountingPeriod(tenantID, name, startDate, endDate, userID string) (string, error) {
	// Stage 25 (GLOBAL-0012): dates arrive as ISO "YYYY-MM-DD" strings (same
	// assumption the overlap query below already makes by comparing them
	// directly as SQL date literals), so a plain string compare is enough -
	// no need to parse just to catch a swapped start/end.
	if startDate != "" && endDate != "" && startDate > endDate {
		return "", &ValidationError{Code: "GLOBAL-0012", Message: "Start Date cannot be later than End Date"}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var overlapping string
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT period_name FROM %s.accounting_periods
		WHERE start_date <= $2 AND end_date >= $1
		LIMIT 1`, schema), startDate, endDate).Scan(&overlapping)
	if err == nil {
		return "", fmt.Errorf("date range overlaps existing period '%s'", overlapping)
	} else if err != sql.ErrNoRows {
		return "", err
	}

	var id string
	err = tx.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.accounting_periods (period_name, start_date, end_date, status, created_by)
		VALUES ($1, $2, $3, 'Open', $4) RETURNING id`, schema), name, startDate, endDate, userID).Scan(&id)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	LogAuditEvent(tenantID, userID, "CREATE_ACCOUNTING_PERIOD", "SUCCESS",
		fmt.Sprintf("Created period '%s' (%s to %s)", name, startDate, endDate))
	return id, nil
}

// CloseAccountingPeriod is a one-way transition: Open -> Closed. Once closed,
// PostDoubleEntry rejects new postings dated inside the period; there is no
// reopen path by design, matching the "reversal, never mutation" correction
// model this feature exists to enforce.
func CloseAccountingPeriod(tenantID, periodID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status, name string
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT status, period_name FROM %s.accounting_periods WHERE id = $1 FOR UPDATE`, schema), periodID).Scan(&status, &name)
	if err != nil {
		return fmt.Errorf("period not found: %v", err)
	}
	if status != "Open" {
		return fmt.Errorf("period is already %s", status)
	}

	_, err = tx.Exec(fmt.Sprintf(`
		UPDATE %s.accounting_periods SET status = 'Closed', closed_by = $1, closed_at = CURRENT_TIMESTAMP
		WHERE id = $2`, schema), userID, periodID)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	LogAuditEvent(tenantID, userID, "CLOSE_ACCOUNTING_PERIOD", "SUCCESS", fmt.Sprintf("Closed period '%s'", name))
	return nil
}

func ListAccountingPeriods(tenantID string) ([]AccountingPeriod, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, period_name, start_date, end_date, status, closed_by, closed_at, created_by, created_at
		FROM %s.accounting_periods ORDER BY start_date DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	periods := []AccountingPeriod{}
	for rows.Next() {
		var p AccountingPeriod
		var closedBy, closedAt sql.NullString
		if err := rows.Scan(&p.ID, &p.PeriodName, &p.StartDate, &p.EndDate, &p.Status, &closedBy, &closedAt, &p.CreatedBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		if closedBy.Valid {
			p.ClosedBy = &closedBy.String
		}
		if closedAt.Valid {
			p.ClosedAt = &closedAt.String
		}
		periods = append(periods, p)
	}
	return periods, nil
}

// PeriodCloseCheck is one pass/fail pre-close validation.
type PeriodCloseCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// PeriodCloseChecklist is a guided pre-close review for one accounting
// period (Stage 20.34) - purely read-only validations layered on top of the
// existing CloseAccountingPeriod control above, not new close logic itself.
// A period can still be closed even if ReadyToClose is false; this is
// advisory, surfaced in the UI before the user clicks Close, the same way
// this codebase's other maker-checker/approval screens surface state
// without themselves being the enforcement mechanism.
type PeriodCloseChecklist struct {
	PeriodID     string             `json:"period_id"`
	PeriodName   string             `json:"period_name"`
	Status       string             `json:"status"`
	Checks       []PeriodCloseCheck `json:"checks"`
	ReadyToClose bool               `json:"ready_to_close"`
}

// GetPeriodCloseChecklist runs a fixed set of read-only checks: the trial
// balance is balanced, nothing is stuck awaiting approval, no vendor
// invoice is sitting in a 3-way-match mismatch hold, and no bank statement
// line dated inside the period is still unmatched (Stage 20.26).
func GetPeriodCloseChecklist(tenantID, periodID string) (*PeriodCloseChecklist, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	var name, status, startDate, endDate string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT period_name, status, start_date, end_date FROM %s.accounting_periods WHERE id = $1`, schema),
		periodID).Scan(&name, &status, &startDate, &endDate); err != nil {
		return nil, fmt.Errorf("period not found: %v", err)
	}

	result := &PeriodCloseChecklist{PeriodID: periodID, PeriodName: name, Status: status, ReadyToClose: true}
	addCheck := func(checkName string, passed bool, detail string) {
		result.Checks = append(result.Checks, PeriodCloseCheck{Name: checkName, Passed: passed, Detail: detail})
		if !passed {
			result.ReadyToClose = false
		}
	}

	addCheck("Period is Open", status == "Open", fmt.Sprintf("current status: %s", status))

	tb, err := GetTrialBalance(tenantID)
	if err != nil {
		return nil, err
	}
	balanced, _ := tb["balanced"].(bool)
	statusMsg, _ := tb["status"].(string)
	addCheck("Trial balance is balanced", balanced, statusMsg)

	pending, err := ListPendingApprovals(tenantID, "HR/Admin", "")
	if err != nil {
		return nil, err
	}
	addCheck("No documents awaiting approval", len(pending) == 0,
		fmt.Sprintf("%d document(s) still Pending Approval", len(pending)))

	var mismatchCount int
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'VendorInvoice' AND status = 'MismatchHold'`, schema)).
		Scan(&mismatchCount); err != nil {
		return nil, err
	}
	addCheck("No vendor invoices in mismatch hold", mismatchCount == 0,
		fmt.Sprintf("%d vendor invoice(s) in MismatchHold", mismatchCount))

	var unmatchedBank int
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'BankStatementLine'
		 AND COALESCE(data->>'match_status', 'Unmatched') != 'Matched'
		 AND data->>'txn_date' BETWEEN $1 AND $2`, schema), startDate, endDate).
		Scan(&unmatchedBank); err != nil {
		return nil, err
	}
	addCheck("No unmatched bank statement lines in this period", unmatchedBank == 0,
		fmt.Sprintf("%d bank statement line(s) dated in this period still unmatched", unmatchedBank))

	return result, nil
}

// rejectIfCurrentPeriodClosed is called from inside PostDoubleEntry's own
// transaction so the period check and the postings it guards are atomic.
//
// 24.6: previously always checked CURRENT_DATE, so a document posted with a
// backdated transaction date into an already-closed period sailed through
// untouched - period closure was non-binding for backdated entries.
// transactionDate (YYYY-MM-DD) now lets a caller check its own document
// date instead; empty string preserves the exact original CURRENT_DATE
// behavior. As of this pass no document type in this codebase actually
// carries an independent, user-editable transaction date that reaches this
// function (every current PostDoubleEntry call site posts at the moment of
// the action itself, so "today" and "the document's date" are the same
// thing) - this wires the mechanism through so a future backdated-entry
// flow (e.g. a dated SalesInvoice/VendorInvoice field) is covered
// automatically rather than needing another pass through this function.
// Uses the database's own date comparison (not app-server time) either way -
// the same lesson the Stage 14 lockout timezone bug taught: reckon time
// windows against Postgres's clock end-to-end, not a mix of Go and SQL
// clocks.
func rejectIfCurrentPeriodClosed(tx *sql.Tx, schema, docType, docID, transactionDate string) error {
	var name string
	var err error
	if transactionDate == "" {
		err = tx.QueryRow(fmt.Sprintf(`
			SELECT period_name FROM %s.accounting_periods
			WHERE status = 'Closed' AND CURRENT_DATE BETWEEN start_date AND end_date
			LIMIT 1`, schema)).Scan(&name)
	} else {
		err = tx.QueryRow(fmt.Sprintf(`
			SELECT period_name FROM %s.accounting_periods
			WHERE status = 'Closed' AND $1::date BETWEEN start_date AND end_date
			LIMIT 1`, schema), transactionDate).Scan(&name)
	}
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	// Stage 26.6.6: a signed-off BackdatedPostingRequest lets this specific
	// (docType, docID, transactionDate) posting through the closed period
	// anyway, instead of a blanket rejection - the override path this
	// function's own comment above (24.6) said a future backdated-entry
	// flow would need once one existed (Stage 26.6.4's JournalVoucher is
	// the first caller that actually passes a real, user-editable
	// transactionDate here).
	if approved, approvedErr := isBackdatedPostingApproved(tx, schema, docType, docID, transactionDate); approvedErr == nil && approved {
		return nil
	}

	// FIN-0260 (Stage 26.6.10, revisited from Stage 25.9): this is the
	// single choke point every PostDoubleEntry call site already routes
	// through, so coding the error here propagates it to all of them at
	// once - no per-call-site changes needed.
	return &ValidationError{Code: "FIN-0260", Message: fmt.Sprintf("cannot post: transaction date falls within closed accounting period '%s'", name)}
}

// isBackdatedPostingApproved checks for an Approved BackdatedPostingRequest
// matching this exact posting - docID may be "" for callers that don't yet
// have a document id at posting time, in which case there's nothing to
// match against and no override is possible (not an error, just no match).
func isBackdatedPostingApproved(tx *sql.Tx, schema, docType, docID, transactionDate string) (bool, error) {
	if docID == "" {
		return false, nil
	}
	effectiveDate := transactionDate
	if effectiveDate == "" {
		effectiveDate = time.Now().Format("2006-01-02")
	}
	var count int
	err := tx.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.documents
		WHERE doctype = 'BackdatedPostingRequest' AND status = 'Approved'
		  AND data->>'target_doctype' = $1 AND data->>'target_document_id' = $2 AND data->>'transaction_date' = $3`, schema),
		docType, docID, effectiveDate).Scan(&count)
	return count > 0, err
}
