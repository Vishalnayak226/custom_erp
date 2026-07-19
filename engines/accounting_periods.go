package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
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

// rejectIfCurrentPeriodClosed is called from inside PostDoubleEntry's own
// transaction so the period check and the postings it guards are atomic.
// Uses the database's own CURRENT_DATE (not app-server time) - the same
// lesson the Stage 14 lockout timezone bug taught: reckon time windows
// against Postgres's clock end-to-end, not a mix of Go and SQL clocks.
func rejectIfCurrentPeriodClosed(tx *sql.Tx, schema string) error {
	var name string
	err := tx.QueryRow(fmt.Sprintf(`
		SELECT period_name FROM %s.accounting_periods
		WHERE status = 'Closed' AND CURRENT_DATE BETWEEN start_date AND end_date
		LIMIT 1`, schema)).Scan(&name)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("cannot post: today falls within closed accounting period '%s'", name)
}
