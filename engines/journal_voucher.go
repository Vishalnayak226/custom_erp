package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// Stage 26.6.4: Journal Voucher - manual GL entry, reversal, recurring
// templates. Posts through the existing PostDoubleEntry (engines/finance.go),
// gated by the existing maker-checker approval engine (engines/approval.go)
// via a seeded approval_rules row (db/migrations_stage26_6_finance_tax_close.sql)
// rather than posting on create - a manual GL entry is exactly the kind of
// action that engine exists to gate.

// JournalVoucherLine is one debit-or-credit line. A line is never both -
// validateJournalVoucherLines rejects that.
type JournalVoucherLine struct {
	AccountCode string `json:"account_code"`
	Debit       int    `json:"debit,omitempty"`
	Credit      int    `json:"credit,omitempty"`
}

func validateJournalVoucherLines(lines []JournalVoucherLine) (totalAmount int, err error) {
	if len(lines) < 2 {
		return 0, fmt.Errorf("a journal voucher needs at least two lines")
	}
	totalDebit, totalCredit := 0, 0
	for _, l := range lines {
		if l.AccountCode == "" {
			return 0, fmt.Errorf("every line needs an account_code")
		}
		if l.Debit < 0 || l.Credit < 0 {
			return 0, fmt.Errorf("debit/credit cannot be negative")
		}
		if l.Debit > 0 && l.Credit > 0 {
			return 0, fmt.Errorf("line for account %s cannot have both a debit and a credit", l.AccountCode)
		}
		totalDebit += l.Debit
		totalCredit += l.Credit
	}
	if totalDebit != totalCredit {
		return 0, fmt.Errorf("unbalanced journal voucher: total debit (%d) must equal total credit (%d)", totalDebit, totalCredit)
	}
	if totalDebit == 0 {
		return 0, fmt.Errorf("journal voucher has no amount")
	}
	return totalDebit, nil
}

// JournalVoucherOptions carries the voucher's optional whole-voucher
// cost_center/department (Stage 26.6.8) - a variadic param on
// CreateJournalVoucher so every existing call site (including this
// package's own tests) needs zero changes.
type JournalVoucherOptions struct {
	CostCenter string
	Department string
	// Stage 37.2.1: which LegalEntity's book this voucher belongs to -
	// the same optional whole-voucher dimension as CostCenter/Department,
	// flowing into PostDoubleEntry via PostingOptions.Entity.
	Entity string
	// Stage 37.7.2: which Project this voucher's cost/revenue belongs to -
	// the same optional whole-voucher dimension, flowing into
	// PostDoubleEntry via PostingOptions.Project.
	Project string
}

// createJournalVoucherInSchema is the schema-level primitive shared by
// CreateJournalVoucher (tenantID-facing) and the recurring worker below
// (which already has the schema from its own tenant-schema scan and has no
// tenantID to re-derive it from).
func createJournalVoucherInSchema(schema, voucherDate, narration string, lines []JournalVoucherLine, userID string, opts JournalVoucherOptions) (string, error) {
	totalAmount, err := validateJournalVoucherLines(lines)
	if err != nil {
		return "", err
	}
	if voucherDate == "" {
		return "", fmt.Errorf("voucher_date is required")
	}
	if narration == "" {
		return "", fmt.Errorf("narration is required")
	}
	if err := validateCostCenterReferenceInSchema(schema, opts.CostCenter); err != nil {
		return "", err
	}
	if err := validateDepartmentReferenceInSchema(schema, opts.Department); err != nil {
		return "", err
	}
	if err := validateLegalEntityReferenceInSchema(schema, opts.Entity); err != nil {
		return "", err
	}
	if err := validateProjectReferenceInSchema(schema, opts.Project); err != nil {
		return "", err
	}

	linesJSON, err := json.Marshal(lines)
	if err != nil {
		return "", err
	}
	voucherID := NewDocID("JV")
	docData := map[string]interface{}{
		"id": voucherID, "code": voucherID, "voucher_number": voucherID,
		"voucher_date": voucherDate, "narration": narration,
		"lines": string(linesJSON), "total_amount": totalAmount, "status": "Draft",
		"cost_center": opts.CostCenter, "department": opts.Department, "entity": opts.Entity, "project": opts.Project,
	}
	// Stage 76.11: a company-level series (no store segment, migration 76_11).
	// The recurring worker reaches here with only the schema.
	if tenantID, err := tenantIDForSchema(schema); err == nil && tenantID != "" {
		issueNumberInto(docData, tenantID, "JournalVoucher", voucherID, "")
	}
	marshaled, err := json.Marshal(docData)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'JournalVoucher', $2, 'Draft', $3)`, schema),
		voucherID, marshaled, userID); err != nil {
		return "", err
	}
	return voucherID, nil
}

// CreateJournalVoucher creates a Draft JournalVoucher. It must be routed
// through the existing SubmitForApproval/DecideApproval before it posts -
// see postApprovedJournalVoucher, hooked into DecideApproval the same way
// Stage 26.4.6's ProductContent snapshot is.
func CreateJournalVoucher(tenantID, voucherDate, narration string, lines []JournalVoucherLine, userID string, opts ...JournalVoucherOptions) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var opt JournalVoucherOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	return createJournalVoucherInSchema(schema, voucherDate, narration, lines, userID, opt)
}

func fetchJournalVoucher(schema, voucherID string) (data map[string]interface{}, status string, err error) {
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'JournalVoucher' AND id = $1`, schema), voucherID).
		Scan(&dataStr, &status); err != nil {
		return nil, "", fmt.Errorf("journal voucher not found: %v", err)
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", fmt.Errorf("journal voucher %s has corrupt stored data: %v", voucherID, err)
	}
	return data, status, nil
}

func journalVoucherLinesFromData(data map[string]interface{}) ([]JournalVoucherLine, error) {
	linesRaw, _ := data["lines"].(string)
	var lines []JournalVoucherLine
	if err := json.Unmarshal([]byte(linesRaw), &lines); err != nil {
		return nil, fmt.Errorf("could not parse journal voucher's lines: %v", err)
	}
	return lines, nil
}

func updateJournalVoucherData(schema, voucherID, status string, data map[string]interface{}) error {
	return updateJournalVoucherDataIn(db.DB, schema, voucherID, status, data)
}

// updateJournalVoucherDataIn is updateJournalVoucherData on a given
// connection or transaction.
func updateJournalVoucherDataIn(q interface {
	Exec(string, ...interface{}) (sql.Result, error)
}, schema, voucherID, status string, data map[string]interface{}) error {
	data["status"] = status
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = q.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'JournalVoucher' AND id = $3`, schema),
		marshaled, status, voucherID)
	return err
}

// lockJournalVoucherTx is fetchJournalVoucher under the voucher's row lock
// (Stage 135.21).
func lockJournalVoucherTx(tx *sql.Tx, schema, voucherID string) (data map[string]interface{}, status string, err error) {
	var dataStr string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'JournalVoucher' AND id = $1 FOR UPDATE`, schema), voucherID).
		Scan(&dataStr, &status); err != nil {
		return nil, "", fmt.Errorf("journal voucher not found: %v", err)
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", fmt.Errorf("journal voucher %s has corrupt stored data: %v", voucherID, err)
	}
	return data, status, nil
}

// postApprovedJournalVoucher posts an Approved JournalVoucher's lines
// through PostDoubleEntry and moves it to Posted. Called from
// engines.DecideApproval - best-effort in the sense that it doesn't block
// the approval decision itself (already recorded by the time this runs),
// but unlike the ProductContent snapshot this failure IS a real
// data-integrity gap (an "Approved" voucher that never reached the GL), so
// it's logged loudly via LogSystemError rather than silently skipped.
//
// Stage 135.21: the voucher is read under its row lock and posted and marked
// Posted in one transaction, so this and a retry (or two retries) at once
// cannot both post - the second waits, then finds it Posted.
func postApprovedJournalVoucher(tenantID, voucherID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedJournalVoucher", fmt.Sprintf("voucher %s: %v", voucherID, err), "")
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedJournalVoucher", fmt.Sprintf("voucher %s: %v", voucherID, err), "")
		return
	}
	defer tx.Rollback()
	data, status, err := lockJournalVoucherTx(tx, schema, voucherID)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedJournalVoucher", err.Error(), "")
		return
	}
	if status != "Approved" {
		return // a retry posted it first
	}
	if err := postJournalVoucherTx(tx, tenantID, schema, voucherID, data); err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedJournalVoucher", err.Error(), "")
		return
	}
	if err := tx.Commit(); err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedJournalVoucher", fmt.Sprintf("voucher %s: %v", voucherID, err), "")
	}
}

// postJournalVoucherTx is the posting itself, in the caller's transaction
// holding the voucher's row lock: the lines to the GL, the rows to the
// voucher's date, the voucher to Posted and the voucher it reverses to
// Reversed. Any failure returns an error and leaves the whole post undone.
func postJournalVoucherTx(tx *sql.Tx, tenantID, schema, voucherID string, data map[string]interface{}) error {
	lines, err := journalVoucherLinesFromData(data)
	if err != nil {
		return fmt.Errorf("voucher %s: %v", voucherID, err)
	}

	debits := map[string]int{}
	credits := map[string]int{}
	for _, l := range lines {
		if l.Debit > 0 {
			debits[l.AccountCode] += l.Debit
		}
		if l.Credit > 0 {
			credits[l.AccountCode] += l.Credit
		}
	}
	voucherDate, _ := data["voucher_date"].(string)
	costCenter, _ := data["cost_center"].(string)
	department, _ := data["department"].(string)
	entity, _ := data["entity"].(string)
	project, _ := data["project"].(string)
	opt := PostingOptions{CostCenter: costCenter, Department: department, Entity: entity, Project: project}

	// Stage 37.1.2: a voucher entered in another currency posts functional
	// amounts to the ledger and carries its own amounts alongside. The line
	// values above are in the voucher's currency - what the person typed - so
	// they are converted here rather than at the line editor, keeping one
	// definition of "what the ledger holds" for every posting path.
	currency := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", data["currency"])))
	functional := FunctionalCurrency(tenantID)
	if currency != "" && currency != "<NIL>" && currency != functional {
		rate, ok := parityNumber(data["exchange_rate"])
		if !ok || rate <= 0 {
			return fmt.Errorf("voucher %s is in %s but carries no usable exchange rate; not posted", voucherID, currency)
		}
		debits, credits, opt.TransactionDebits, opt.TransactionCredits = ConvertPostingToFunctional(debits, credits, rate)
		opt.Currency = currency
		opt.ExchangeRate = rate
	}
	postingKey := fmt.Sprintf("JournalVoucher:%s:POST", voucherID)
	// Stage 73.D.16: a backdated voucher belongs to its own date. Period
	// reports read the GL row's created_at, which is the posting moment, so a
	// voucher dated last month landed in this month. The rows take the
	// voucher's date (keeping their time of day), as depreciation's rows take
	// their month's end (asset_depreciation.go). A future date is left alone.
	// Stage 47.16.4b: written as the rows are inserted - gl_postings refuses
	// to change a posted row's date afterwards.
	if _, perr := time.Parse("2006-01-02", voucherDate); perr == nil && voucherDate < BusinessToday() {
		opt.PostedAt = voucherDate
	}
	if err := PostDoubleEntryTx(tx, tenantID, schema, "JournalVoucher", voucherID, PaiseMap(debits), PaiseMap(credits), voucherDate, postingKey, opt); err != nil {
		return fmt.Errorf("voucher %s approved but GL post failed: %v", voucherID, err)
	}
	if err := updateJournalVoucherDataIn(tx, schema, voucherID, "Posted", data); err != nil {
		return fmt.Errorf("voucher %s not posted - status update failed: %v", voucherID, err)
	}
	// Stage 132.17: a reversal that has posted marks the voucher it reverses.
	if orig, _ := data["reversed_from"].(string); orig != "" {
		if od, ost, err := lockJournalVoucherTx(tx, schema, orig); err == nil && ost == "Posted" {
			if err := updateJournalVoucherDataIn(tx, schema, orig, "Reversed", od); err != nil {
				return fmt.Errorf("reversal %s not posted - %s could not be marked Reversed: %v", voucherID, orig, err)
			}
		}
	}
	return nil
}

// RetryPostApprovedJournalVoucher re-attempts posting an Approved voucher
// that previously failed to post - most commonly, Stage 26.6.6's closed-
// period block, where postApprovedJournalVoucher's first attempt (fired
// automatically from DecideApproval) failed and left the voucher stuck in
// Approved rather than Posted. A caller retries once the blocking condition
// is resolved (e.g. a BackdatedPostingRequest for this exact voucher gets
// Approved) - only valid while still Approved, so it can't be used to
// re-post an already-Posted voucher a second time. The status is checked
// under the voucher's row lock in the posting's own transaction (Stage
// 135.21), so of two retries at once one posts and the other is refused.
func RetryPostApprovedJournalVoucher(tenantID, voucherID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockJournalVoucherTx(tx, schema, voucherID)
	if err != nil {
		return err
	}
	if status != "Approved" {
		return fmt.Errorf("only an Approved (not yet Posted) journal voucher can be retried (current status: %s)", status)
	}
	if err := postJournalVoucherTx(tx, tenantID, schema, voucherID, data); err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedJournalVoucher", err.Error(), "")
		return fmt.Errorf("posting is still blocked (status: %s) - check for a required BackdatedPostingRequest approval", status)
	}
	return tx.Commit()
}

// ReverseJournalVoucher creates a new Draft JournalVoucher with every line's
// debit/credit swapped, linked back via reversed_from. Deliberately routed
// through the same Submit/Approve/Post cycle as any new voucher rather than
// posting immediately - reusing the existing machinery rather than adding a
// second, unreviewed posting path for reversals specifically.
func ReverseJournalVoucher(tenantID, voucherID, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	data, status, err := fetchJournalVoucher(schema, voucherID)
	if err != nil {
		return "", err
	}
	if status != "Posted" {
		return "", fmt.Errorf("only a Posted journal voucher can be reversed (current status: %s)", status)
	}
	if by := journalVoucherReversalPending(schema, data); by != "" {
		return "", fmt.Errorf("%s is already being reversed by %s - approve or reject that one first", voucherID, by)
	}
	lines, err := journalVoucherLinesFromData(data)
	if err != nil {
		return "", err
	}
	reversed := make([]JournalVoucherLine, len(lines))
	for i, l := range lines {
		reversed[i] = JournalVoucherLine{AccountCode: l.AccountCode, Debit: l.Credit, Credit: l.Debit}
	}

	narration := fmt.Sprintf("Reversal of %s", voucherID)
	costCenter, _ := data["cost_center"].(string)
	department, _ := data["department"].(string)
	entity, _ := data["entity"].(string)
	project, _ := data["project"].(string)
	newID, err := createJournalVoucherInSchema(schema, BusinessToday(), narration, reversed, userID, JournalVoucherOptions{CostCenter: costCenter, Department: department, Entity: entity, Project: project})
	if err != nil {
		return "", err
	}

	newData, _, err := fetchJournalVoucher(schema, newID)
	if err != nil {
		return newID, err
	}
	newData["reversed_from"] = voucherID
	if err := updateJournalVoucherData(schema, newID, "Draft", newData); err != nil {
		return newID, err
	}

	// Stage 132.17: the voucher stays Posted - its amounts are still in the
	// GL - until the reversal itself posts (postApprovedJournalVoucher marks
	// it Reversed then). Marked Reversed here, a reversal left in Draft or
	// rejected showed the voucher reversed while its amounts stood, and it
	// could not be reversed again.
	data["reversed_by"] = newID
	if err := updateJournalVoucherData(schema, voucherID, "Posted", data); err != nil {
		return newID, err
	}
	return newID, nil
}

// journalVoucherReversalPending names the live reversal of a voucher, if one
// exists: one raised and not yet rejected or cancelled.
func journalVoucherReversalPending(schema string, data map[string]interface{}) string {
	by, _ := data["reversed_by"].(string)
	if by == "" {
		return ""
	}
	if _, st, err := fetchJournalVoucher(schema, by); err != nil || st == "Rejected" || st == "Cancelled" {
		return ""
	}
	return by
}

var recurringJournalFrequencies = map[string]bool{"Daily": true, "Weekly": true, "Monthly": true, "Yearly": true}

// CreateRecurringJournalTemplate creates a JournalVoucher in the special
// "Recurring Template" status - never itself posted, it's the source
// StartRecurringJournalWorker copies from to create a fresh Draft instance
// each time next_run_date arrives.
func CreateRecurringJournalTemplate(tenantID, narration, frequency, nextRunDate string, lines []JournalVoucherLine, userID string, opts ...JournalVoucherOptions) (string, error) {
	if !recurringJournalFrequencies[frequency] {
		return "", fmt.Errorf("recurring_frequency must be one of Daily, Weekly, Monthly, Yearly")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var opt JournalVoucherOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	voucherID, err := createJournalVoucherInSchema(schema, nextRunDate, narration, lines, userID, opt)
	if err != nil {
		return "", err
	}
	data, _, err := fetchJournalVoucher(schema, voucherID)
	if err != nil {
		return voucherID, err
	}
	data["recurring_frequency"] = frequency
	data["next_run_date"] = nextRunDate
	if err := updateJournalVoucherData(schema, voucherID, "Recurring Template", data); err != nil {
		return voucherID, err
	}
	return voucherID, nil
}

func advanceRecurringDate(dateStr, frequency string) (string, error) {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return "", err
	}
	switch frequency {
	case "Daily":
		t = t.AddDate(0, 0, 1)
	case "Weekly":
		t = t.AddDate(0, 0, 7)
	case "Monthly":
		t = t.AddDate(0, 1, 0)
	case "Yearly":
		t = t.AddDate(1, 0, 0)
	default:
		return "", fmt.Errorf("unknown recurring_frequency %q", frequency)
	}
	return t.Format("2006-01-02"), nil
}

func runRecurringJournalsForSchema(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'JournalVoucher' AND status = 'Recurring Template'`, schema))
	if err != nil {
		log.Printf("[RECURRING-JV] Failed to list templates in schema %s: %v", schema, err)
		return
	}
	type row struct{ id, data string }
	var templates []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.data); err == nil {
			templates = append(templates, r)
		}
	}
	rows.Close()

	today := BusinessToday()
	for _, t := range templates {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(t.data), &data); err != nil {
			log.Printf("[RECURRING-JV] Skipping corrupt template %s in schema %s: %v", t.id, schema, err)
			continue
		}
		nextRunDate, _ := data["next_run_date"].(string)
		frequency, _ := data["recurring_frequency"].(string)
		if nextRunDate == "" || frequency == "" || nextRunDate > today {
			continue
		}
		lines, err := journalVoucherLinesFromData(data)
		if err != nil {
			log.Printf("[RECURRING-JV] Skipping template %s: %v", t.id, err)
			continue
		}
		narration, _ := data["narration"].(string)
		costCenter, _ := data["cost_center"].(string)
		department, _ := data["department"].(string)
		entity, _ := data["entity"].(string)
		project, _ := data["project"].(string)
		if _, err := createJournalVoucherInSchema(schema, nextRunDate, narration, lines, "system", JournalVoucherOptions{CostCenter: costCenter, Department: department, Entity: entity, Project: project}); err != nil {
			log.Printf("[RECURRING-JV] Failed to spawn instance from template %s: %v", t.id, err)
			continue
		}
		newNextRunDate, err := advanceRecurringDate(nextRunDate, frequency)
		if err != nil {
			log.Printf("[RECURRING-JV] Failed to advance next_run_date for template %s: %v", t.id, err)
			continue
		}
		data["next_run_date"] = newNextRunDate
		if err := updateJournalVoucherData(schema, t.id, "Recurring Template", data); err != nil {
			log.Printf("[RECURRING-JV] Failed to advance template %s to %s: %v", t.id, newNextRunDate, err)
		}
	}
}

// StartRecurringJournalWorker polls every tenant schema (re-queried each
// tick, same convention as StartOutboxWorker) for Recurring Template
// JournalVouchers whose next_run_date has arrived.
func StartRecurringJournalWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() { // 134.7: one server runs each sweep
					continue
				}
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[RECURRING-JV] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					runRecurringJournalsForSchema(schema)
				}
			}
		}
	}()
}
