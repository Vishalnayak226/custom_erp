package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
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

	linesJSON, err := json.Marshal(lines)
	if err != nil {
		return "", err
	}
	voucherID := fmt.Sprintf("JV-%d", time.Now().UnixNano())
	docData := map[string]interface{}{
		"id": voucherID, "code": voucherID, "voucher_number": voucherID,
		"voucher_date": voucherDate, "narration": narration,
		"lines": string(linesJSON), "total_amount": totalAmount, "status": "Draft",
		"cost_center": opts.CostCenter, "department": opts.Department,
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
	data["status"] = status
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'JournalVoucher' AND id = $3`, schema),
		marshaled, status, voucherID)
	return err
}

// postApprovedJournalVoucher posts an Approved JournalVoucher's lines
// through PostDoubleEntry and moves it to Posted. Called from
// engines.DecideApproval - best-effort in the sense that it doesn't block
// the approval decision itself (already recorded by the time this runs),
// but unlike the ProductContent snapshot this failure IS a real
// data-integrity gap (an "Approved" voucher that never reached the GL), so
// it's logged loudly via LogSystemError rather than silently skipped.
func postApprovedJournalVoucher(tenantID, voucherID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedJournalVoucher", fmt.Sprintf("voucher %s: %v", voucherID, err), "")
		return
	}
	data, _, err := fetchJournalVoucher(schema, voucherID)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedJournalVoucher", err.Error(), "")
		return
	}
	lines, err := journalVoucherLinesFromData(data)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedJournalVoucher", fmt.Sprintf("voucher %s: %v", voucherID, err), "")
		return
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
	opt := PostingOptions{CostCenter: costCenter, Department: department}
	if err := PostDoubleEntry(tenantID, "JournalVoucher", voucherID, debits, credits, voucherDate, fmt.Sprintf("JournalVoucher:%s:POST", voucherID), opt); err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedJournalVoucher", fmt.Sprintf("voucher %s approved but GL post failed: %v", voucherID, err), "")
		return
	}
	if err := updateJournalVoucherData(schema, voucherID, "Posted", data); err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedJournalVoucher", fmt.Sprintf("voucher %s posted to GL but status update failed: %v", voucherID, err), "")
	}
}

// RetryPostApprovedJournalVoucher re-attempts posting an Approved voucher
// that previously failed to post - most commonly, Stage 26.6.6's closed-
// period block, where postApprovedJournalVoucher's first attempt (fired
// automatically from DecideApproval) failed and left the voucher stuck in
// Approved rather than Posted. A caller retries once the blocking condition
// is resolved (e.g. a BackdatedPostingRequest for this exact voucher gets
// Approved) - only valid while still Approved, so it can't be used to
// re-post an already-Posted voucher a second time.
func RetryPostApprovedJournalVoucher(tenantID, voucherID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, status, err := fetchJournalVoucher(schema, voucherID)
	if err != nil {
		return err
	}
	if status != "Approved" {
		return fmt.Errorf("only an Approved (not yet Posted) journal voucher can be retried (current status: %s)", status)
	}
	postApprovedJournalVoucher(tenantID, voucherID)
	_, newStatus, err := fetchJournalVoucher(schema, voucherID)
	if err != nil {
		return err
	}
	if newStatus != "Posted" {
		return fmt.Errorf("posting is still blocked (status: %s) - check for a required BackdatedPostingRequest approval", newStatus)
	}
	return nil
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
	newID, err := createJournalVoucherInSchema(schema, time.Now().Format("2006-01-02"), narration, reversed, userID, JournalVoucherOptions{CostCenter: costCenter, Department: department})
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

	data["reversed_by"] = newID
	if err := updateJournalVoucherData(schema, voucherID, "Reversed", data); err != nil {
		return newID, err
	}
	return newID, nil
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

	today := time.Now().Format("2006-01-02")
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
		if _, err := createJournalVoucherInSchema(schema, nextRunDate, narration, lines, "system", JournalVoucherOptions{CostCenter: costCenter, Department: department}); err != nil {
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
