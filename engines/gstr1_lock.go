package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Stage 148.25 (D8): lock a month after GST filing.
//
// Setting finance.lock_after_gstr1 (off). Once an accountant marks a month's
// GSTR-1 filed, that month's sales documents are locked: the sale's own
// postings (an invoice posting, its cost of goods, its cancellation) are
// refused. A credit or debit note, a receipt and a return are not sales
// documents here, so a filed month is still corrected the way GST wants.
// Unmarking is an admin action with a reason, kept in the audit log.
//
// A period can also be put On Hold: the same sales postings are refused for
// that period's dates, with no filing and no setting involved; Release puts
// it back to Open. Closing works from Open or On Hold.
//
// The check runs from PostDoubleEntryTx, the one choke point every posting
// already goes through, right after the closed-period check.

const (
	SettingLockAfterGSTR1 = "finance.lock_after_gstr1"

	gstr1LockedCode = "FIN-0970" // sales posting in a month whose GSTR-1 is filed
	periodHoldCode  = "FIN-0971" // sales posting in a period On Hold
	gstr1MarkCode   = "FIN-0972" // mark / unmark / hold / release refused
)

var gstr1MonthRe = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// isSalesDocumentPosting is true for the sale's own postings: a SalesInvoice's
// POST, COGS and CANCEL, and a POS sale's SALE_* lines. Receipts (SETTLE),
// set-offs, credit notes and returns are not.
func isSalesDocumentPosting(docType, postingKey string) bool {
	switch docType {
	case "SalesInvoice":
		return strings.HasSuffix(postingKey, ":POST") || strings.HasSuffix(postingKey, ":COGS") || strings.HasSuffix(postingKey, ":CANCEL")
	case "POSCart":
		return strings.Contains(postingKey, ":SALE_")
	}
	return false
}

// checkSalesMonthLockedTx refuses a sales posting dated in a filed month (when
// the setting is on) or in a period On Hold.
func checkSalesMonthLockedTx(tx *sql.Tx, schema, docType, docID, postingKey, transactionDate string) error {
	if !isSalesDocumentPosting(docType, postingKey) {
		return nil
	}
	date := strings.TrimSpace(transactionDate)
	if date == "" && docType == "SalesInvoice" {
		var d sql.NullString
		_ = tx.QueryRow(fmt.Sprintf(`SELECT data->>'invoice_date' FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1`, schema), docID).Scan(&d)
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(d.String)); err == nil {
			date = strings.TrimSpace(d.String)
		}
	}
	if date == "" {
		date = BusinessToday()
	}
	var name string
	err := tx.QueryRow(fmt.Sprintf(`SELECT period_name FROM %s.accounting_periods WHERE status = 'On Hold' AND $1::date BETWEEN start_date AND end_date LIMIT 1`, schema), date).Scan(&name)
	if err == nil {
		return &ValidationError{Code: periodHoldCode, Message: fmt.Sprintf("period '%s' is On Hold for sales", name)}
	}
	if err != sql.ErrNoRows {
		return err
	}
	if !GetSettingBoolForSchema(schema, SettingLockAfterGSTR1) {
		return nil
	}
	var month string
	err = tx.QueryRow(fmt.Sprintf(`SELECT to_char(month_start, 'Mon YYYY') FROM %s.gstr1_filed_months
		WHERE month_start = date_trunc('month', $1::date)::date AND unmarked_at IS NULL`, schema), date).Scan(&month)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return &ValidationError{Code: gstr1LockedCode, Message: fmt.Sprintf("%s is locked: its GSTR-1 is filed", month)}
}

// GSTR1FiledMonth is one row of the filed-month list.
type GSTR1FiledMonth struct {
	Month        string `json:"month"`
	FiledBy      string `json:"filed_by"`
	FiledAt      string `json:"filed_at"`
	Locked       bool   `json:"locked"`
	UnmarkedBy   string `json:"unmarked_by,omitempty"`
	UnmarkedAt   string `json:"unmarked_at,omitempty"`
	UnmarkReason string `json:"unmark_reason,omitempty"`
}

// ListGSTR1FiledMonths is the newest first list of months ever marked.
func ListGSTR1FiledMonths(tenantID string) ([]GSTR1FiledMonth, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT to_char(month_start, 'YYYY-MM'), filed_by,
		to_char(filed_at AT TIME ZONE public.erp_business_tz(), 'YYYY-MM-DD HH24:MI'), unmarked_at IS NULL,
		COALESCE(unmarked_by, ''), COALESCE(to_char(unmarked_at AT TIME ZONE public.erp_business_tz(), 'YYYY-MM-DD HH24:MI'), ''), COALESCE(unmark_reason, '')
		FROM %s.gstr1_filed_months ORDER BY month_start DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GSTR1FiledMonth{}
	for rows.Next() {
		var m GSTR1FiledMonth
		if err := rows.Scan(&m.Month, &m.FiledBy, &m.FiledAt, &m.Locked, &m.UnmarkedBy, &m.UnmarkedAt, &m.UnmarkReason); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MarkGSTR1Filed records that a finished month's GSTR-1 is filed. It works with
// the setting off too (the mark is a record); the lock bites only when on.
func MarkGSTR1Filed(tenantID, userID, month string) error {
	month = strings.TrimSpace(month)
	if !gstr1MonthRe.MatchString(month) {
		return &ValidationError{Code: gstr1MarkCode, Message: "choose a month"}
	}
	if month >= BusinessToday()[:7] {
		return &ValidationError{Code: gstr1MarkCode, Message: "only a month that has ended can be marked"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.gstr1_filed_months (month_start, filed_by) VALUES (($1 || '-01')::date, $2)
		ON CONFLICT (month_start) DO UPDATE SET filed_by = EXCLUDED.filed_by, filed_at = CURRENT_TIMESTAMP,
			unmarked_by = NULL, unmarked_at = NULL, unmark_reason = NULL
		WHERE %s.gstr1_filed_months.unmarked_at IS NOT NULL`, schema, schema), month, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &ValidationError{Code: gstr1MarkCode, Message: "that month is already marked"}
	}
	LogAuditEvent(tenantID, userID, "GSTR1_MARKED_FILED", "SUCCESS", "GSTR-1 for "+month+" marked filed")
	return nil
}

// UnmarkGSTR1Filed lifts the lock on a month. The reason is required and, with
// who and when, stays on the row and in the audit log.
func UnmarkGSTR1Filed(tenantID, userID, month, reason string) error {
	month, reason = strings.TrimSpace(month), strings.TrimSpace(reason)
	if !gstr1MonthRe.MatchString(month) {
		return &ValidationError{Code: gstr1MarkCode, Message: "choose a month"}
	}
	if reason == "" {
		return &ValidationError{Code: gstr1MarkCode, Message: "say why"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.gstr1_filed_months SET unmarked_by = $2, unmarked_at = CURRENT_TIMESTAMP, unmark_reason = $3
		WHERE month_start = ($1 || '-01')::date AND unmarked_at IS NULL`, schema), month, userID, reason)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &ValidationError{Code: gstr1MarkCode, Message: "that month is not marked"}
	}
	LogAuditEvent(tenantID, userID, "GSTR1_UNMARKED", "SUCCESS", "GSTR-1 mark for "+month+" removed: "+reason)
	return nil
}

// SetAccountingPeriodHold moves a period Open -> On Hold (hold true) or back
// (hold false). A Closed period is reopened through its own request, not here.
func SetAccountingPeriodHold(tenantID, userID, periodID string, hold bool) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	from, to, event := "On Hold", "Open", "RELEASE_ACCOUNTING_PERIOD"
	if hold {
		from, to, event = "Open", "On Hold", "HOLD_ACCOUNTING_PERIOD"
	}
	var name string
	err = db.DB.QueryRow(fmt.Sprintf(`UPDATE %s.accounting_periods SET status = $2 WHERE id = $1 AND status = $3 RETURNING period_name`, schema), periodID, to, from).Scan(&name)
	if err == sql.ErrNoRows {
		return &ValidationError{Code: gstr1MarkCode, Message: "the period is not " + from}
	}
	if err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, event, "SUCCESS", fmt.Sprintf("Period '%s' is now %s", name, to))
	return nil
}

// SettingSetOffClearingAccount names the account a customer-vendor set-off posts
// through (Stage 148.23). The admin picks it; blank refuses set-offs.
const SettingSetOffClearingAccount = "finance.setoff_clearing_account"
