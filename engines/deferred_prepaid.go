package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Stage 37.6: Deferred revenue, prepaid expense amortisation, recurring
// billing, price-list versioning. Pre-build audit: all four were completely
// absent, and SalesInvoice is lump-sum only (no lines field), so deferred
// revenue is recognised at the whole-invoice level, not per line - a stated
// scope boundary, not an oversight.

// ---------------------------------------------------------------------------
// 37.6.1: Deferred revenue.
// ---------------------------------------------------------------------------

// createDeferredRevenueSchedule is called once, from PostSalesInvoice, right
// after a deferred_revenue-flagged invoice's Dr 1300/Cr 2600 posting commits.
// Created directly Active - see this file's own header for why no separate
// approval is needed here.
func createDeferredRevenueSchedule(tenantID, invoiceID string, totalAmount float64, termMonths int) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	id := NewDocID("DRS")
	docData := map[string]interface{}{
		"id": id, "code": id, "sales_invoice_id": invoiceID,
		"total_amount": totalAmount, "term_months": termMonths,
		"recognized_months": 0, "next_recognition_date": time.Now().AddDate(0, 1, 0).Format("2006-01-02"),
		"status": "Active",
	}
	marshaled, err := json.Marshal(docData)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'DeferredRevenueSchedule', $2, 'Active', 'system')`, schema),
		id, marshaled)
	return err
}

// ---------------------------------------------------------------------------
// 37.6.2: Prepaid expense amortisation - the mirror on the expense side.
// Unlike deferred revenue, this is its own independent decision (a prepaid
// payment made outside the GRN/VendorInvoice 3-way-match flow), so it keeps
// a real Draft -> Pending Approval -> Approved lifecycle.
// ---------------------------------------------------------------------------

func ValidatePrepaidExpenseScheduleDocument(tenantID string, payload map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if accountCode := pimString(payload["expense_account_code"]); accountCode != "" {
		accountType, err := validateGLAccountCodeInSchema(schema, accountCode)
		if err != nil {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Expense Account", Message: err.Error()}
		}
		if accountType != "Expense" {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Expense Account", Message: fmt.Sprintf("account '%s' is a %s account, not an Expense account", accountCode, accountType)}
		}
	}
	if amount, ok := parityNumber(payload["total_amount"]); ok && amount <= 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Total Amount", Message: "total_amount must be greater than zero"}
	}
	if months, ok := parityNumber(payload["term_months"]); ok && months <= 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Term (months)", Message: "term_months must be greater than zero"}
	}
	return nil
}

// postApprovedPrepaidExpenseSchedule books the upfront cash outlay (Dr 1800
// Prepaid Expense / Cr 1100 Cash/Bank) the moment the schedule is approved,
// then starts its monthly recognition clock - the JournalVoucher precedent
// of posting on approval, not on create.
func postApprovedPrepaidExpenseSchedule(tenantID, scheduleID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedPrepaidExpenseSchedule", fmt.Sprintf("schedule %s: %v", scheduleID, err), "")
		return
	}
	data, status, err := fetchDocData(tenantID, "PrepaidExpenseSchedule", scheduleID)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedPrepaidExpenseSchedule", err.Error(), "")
		return
	}
	if status != "Approved" {
		return
	}
	amount, _ := parityNumber(data["total_amount"])
	amountPaise := RupeesToPaise(amount)
	if amountPaise <= 0 {
		LogSystemError(tenantID, "", "ERROR", "postApprovedPrepaidExpenseSchedule", fmt.Sprintf("schedule %s has a non-positive amount, not posted", scheduleID), "")
		return
	}
	if err := PostDoubleEntry(tenantID, "PrepaidExpenseSchedule", scheduleID,
		map[string]int64{"1800": amountPaise}, map[string]int64{"1100": amountPaise},
		"", fmt.Sprintf("PrepaidExpenseSchedule:%s:PAY", scheduleID)); err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedPrepaidExpenseSchedule", fmt.Sprintf("schedule %s: GL posting failed: %v", scheduleID, err), "")
		return
	}
	data["recognized_months"] = 0
	data["next_recognition_date"] = time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	marshaled, err := json.Marshal(data)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedPrepaidExpenseSchedule", fmt.Sprintf("schedule %s posted but could not stamp recognition start: %v", scheduleID, err), "")
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PrepaidExpenseSchedule' AND id = $2`, schema),
		marshaled, scheduleID); err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedPrepaidExpenseSchedule", fmt.Sprintf("schedule %s: failed to stamp recognition start: %v", scheduleID, err), "")
	}
}

// ---------------------------------------------------------------------------
// Shared amortisation worker for 37.6.1 (DeferredRevenueSchedule, Dr 2600 /
// Cr 4100) and 37.6.2 (PrepaidExpenseSchedule, Dr <its expense account> /
// Cr 1800) - one monthly recognition tick each, sharing the "1/term_months
// per month, last month absorbs the paise remainder" arithmetic and the
// recognized_months/next_recognition_date bookkeeping, since the two are the
// same mechanism on opposite sides of the balance sheet.
// ---------------------------------------------------------------------------

type amortizationSchedule struct {
	id                           string
	totalAmount                  float64
	termMonths, recognizedMonths int
	nextRecognitionDate          string
	linkedID, expenseAccountCode string // sales_invoice_id for deferred revenue, expense_account_code for prepaid
}

func loadDueAmortizationSchedules(schema, doctype, linkedIDField string) ([]amortizationSchedule, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE((data->>'total_amount')::numeric, 0), COALESCE((data->>'term_months')::int, 0),
		       COALESCE((data->>'recognized_months')::int, 0), COALESCE(data->>'next_recognition_date', ''),
		       COALESCE(data->>'%s', '')
		FROM %s.documents
		WHERE doctype = $1 AND status IN ('Active', 'Approved') AND deleted_at IS NULL
		  AND COALESCE(data->>'next_recognition_date', '') <> '' AND data->>'next_recognition_date' <= $2`,
		linkedIDField, schema), doctype, time.Now().Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []amortizationSchedule
	for rows.Next() {
		var s amortizationSchedule
		if err := rows.Scan(&s.id, &s.totalAmount, &s.termMonths, &s.recognizedMonths, &s.nextRecognitionDate, &s.linkedID); err != nil {
			return nil, err
		}
		if s.termMonths > 0 && s.recognizedMonths < s.termMonths {
			out = append(out, s)
		}
	}
	return out, nil
}

// monthlyRecognitionPaise splits totalAmount into termMonths installments,
// the last one absorbing the paise remainder - ConvertPostingToFunctional's
// own "largest/last line takes the remainder" technique.
func monthlyRecognitionPaise(totalAmount float64, termMonths, recognizedMonths int) int64 {
	totalPaise := RupeesToPaise(totalAmount)
	perMonth := totalPaise / int64(termMonths)
	if recognizedMonths == termMonths-1 {
		return totalPaise - perMonth*int64(recognizedMonths)
	}
	return perMonth
}

func advanceAmortizationSchedule(schema, doctype, scheduleID string, newRecognizedMonths, termMonths int) error {
	status := "Active"
	if doctype == "PrepaidExpenseSchedule" {
		status = "Approved"
	}
	if newRecognizedMonths >= termMonths {
		status = "Completed"
	}
	_, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET
			data = jsonb_set(jsonb_set(data, '{recognized_months}', to_jsonb($1::int)), '{next_recognition_date}', to_jsonb($2::text)),
			status = $3, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $4 AND id = $5`, schema),
		newRecognizedMonths, time.Now().AddDate(0, 1, 0).Format("2006-01-02"), status, doctype, scheduleID)
	return err
}

func runDeferredRevenueRecognition(tenantID, schema string) (recognized int, err error) {
	schedules, err := loadDueAmortizationSchedules(schema, "DeferredRevenueSchedule", "sales_invoice_id")
	if err != nil {
		return 0, err
	}
	for _, s := range schedules {
		amountPaise := monthlyRecognitionPaise(s.totalAmount, s.termMonths, s.recognizedMonths)
		if amountPaise <= 0 {
			continue
		}
		if err := PostDoubleEntry(tenantID, "DeferredRevenueSchedule", s.id,
			map[string]int64{"2600": amountPaise}, map[string]int64{"4100": amountPaise},
			"", fmt.Sprintf("DeferredRevenueSchedule:%s:RECOGNIZE:%d", s.id, s.recognizedMonths)); err != nil {
			LogSystemError(tenantID, "", "ERROR", "runDeferredRevenueRecognition", fmt.Sprintf("schedule %s: %v", s.id, err), "")
			continue
		}
		if err := advanceAmortizationSchedule(schema, "DeferredRevenueSchedule", s.id, s.recognizedMonths+1, s.termMonths); err != nil {
			LogSystemError(tenantID, "", "ERROR", "runDeferredRevenueRecognition", fmt.Sprintf("schedule %s posted but failed to advance: %v", s.id, err), "")
			continue
		}
		recognized++
	}
	return recognized, nil
}

func runPrepaidExpenseRecognition(tenantID, schema string) (recognized int, err error) {
	schedules, err := loadDueAmortizationSchedules(schema, "PrepaidExpenseSchedule", "expense_account_code")
	if err != nil {
		return 0, err
	}
	for _, s := range schedules {
		if s.linkedID == "" {
			continue
		}
		amountPaise := monthlyRecognitionPaise(s.totalAmount, s.termMonths, s.recognizedMonths)
		if amountPaise <= 0 {
			continue
		}
		if err := PostDoubleEntry(tenantID, "PrepaidExpenseSchedule", s.id,
			map[string]int64{s.linkedID: amountPaise}, map[string]int64{"1800": amountPaise},
			"", fmt.Sprintf("PrepaidExpenseSchedule:%s:RECOGNIZE:%d", s.id, s.recognizedMonths)); err != nil {
			LogSystemError(tenantID, "", "ERROR", "runPrepaidExpenseRecognition", fmt.Sprintf("schedule %s: %v", s.id, err), "")
			continue
		}
		if err := advanceAmortizationSchedule(schema, "PrepaidExpenseSchedule", s.id, s.recognizedMonths+1, s.termMonths); err != nil {
			LogSystemError(tenantID, "", "ERROR", "runPrepaidExpenseRecognition", fmt.Sprintf("schedule %s posted but failed to advance: %v", s.id, err), "")
			continue
		}
		recognized++
	}
	return recognized, nil
}

// StartAmortizationWorker polls every tenant schema, the StartDunningWorker/
// StartReservationSweeper precedent.
func StartAmortizationWorker(ctx context.Context, interval time.Duration) {
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
					log.Printf("[AMORTIZATION] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					tenantID, err := tenantIDForSchema(schema)
					if err != nil {
						log.Printf("[AMORTIZATION] %s: could not resolve tenant id: %v", schema, err)
						continue
					}
					deferred, err := runDeferredRevenueRecognition(tenantID, schema)
					if err != nil {
						log.Printf("[AMORTIZATION] %s (deferred revenue): %v", schema, err)
					}
					prepaid, err := runPrepaidExpenseRecognition(tenantID, schema)
					if err != nil {
						log.Printf("[AMORTIZATION] %s (prepaid expense): %v", schema, err)
					}
					if deferred+prepaid > 0 {
						log.Printf("[AMORTIZATION] %s: recognized %d deferred-revenue and %d prepaid-expense installment(s)", schema, deferred, prepaid)
					}
				}
			}
		}
	}()
}

// ---------------------------------------------------------------------------
// 37.6.3: Recurring billing. The CreateRecurringJournalTemplate/
// StartRecurringJournalWorker shape (engines/journal_voucher.go), spawning a
// Draft SalesInvoice instead of a Draft JournalVoucher. The contract itself
// needs no approval workflow - each spawned invoice still goes through
// PostSalesInvoice's own credit-limit gate (37.4.2) when a human posts it.
// ---------------------------------------------------------------------------

var recurringBillingFrequencies = map[string]bool{"Monthly": true, "Quarterly": true, "Yearly": true}

func ValidateRecurringSalesContractDocument(tenantID string, payload map[string]interface{}) error {
	if freq := pimString(payload["billing_frequency"]); freq != "" && !recurringBillingFrequencies[freq] {
		return &ValidationError{Code: "META-0199", SubFor: "Billing Frequency", Message: "billing_frequency must be one of Monthly, Quarterly, Yearly"}
	}
	if amount, ok := parityNumber(payload["amount"]); ok && amount <= 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Amount Per Cycle", Message: "amount must be greater than zero"}
	}
	return nil
}

func advanceBillingDate(dateStr, frequency string) (string, error) {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return "", err
	}
	switch frequency {
	case "Monthly":
		t = t.AddDate(0, 1, 0)
	case "Quarterly":
		t = t.AddDate(0, 3, 0)
	case "Yearly":
		t = t.AddDate(1, 0, 0)
	default:
		return "", fmt.Errorf("unknown billing_frequency %q", frequency)
	}
	return t.Format("2006-01-02"), nil
}

func runRecurringBillingForSchema(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'RecurringSalesContract' AND status = 'Active' AND deleted_at IS NULL`, schema))
	if err != nil {
		log.Printf("[RECURRING-BILLING] Failed to list contracts in schema %s: %v", schema, err)
		return
	}
	type row struct{ id, data string }
	var contracts []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.data); err == nil {
			contracts = append(contracts, r)
		}
	}
	rows.Close()

	today := time.Now().Format("2006-01-02")
	for _, c := range contracts {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(c.data), &data); err != nil {
			log.Printf("[RECURRING-BILLING] Skipping corrupt contract %s in schema %s: %v", c.id, schema, err)
			continue
		}
		nextBillingDate, _ := data["next_billing_date"].(string)
		frequency, _ := data["billing_frequency"].(string)
		if nextBillingDate == "" || frequency == "" || nextBillingDate > today {
			continue
		}
		customer, _ := data["customer"].(string)
		description, _ := data["description"].(string)
		amount, _ := parityNumber(data["amount"])

		invoiceID := NewDocID("INV")
		invoiceData := map[string]interface{}{
			"id": invoiceID, "code": invoiceID, "invoice_number": invoiceID,
			"customer": customer, "total_amount": amount,
			"recurring_contract_id": c.id, "status": "Draft",
		}
		_ = description
		marshaled, err := json.Marshal(invoiceData)
		if err != nil {
			log.Printf("[RECURRING-BILLING] Failed to marshal invoice for contract %s: %v", c.id, err)
			continue
		}
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Draft', 'system')`, schema),
			invoiceID, marshaled); err != nil {
			log.Printf("[RECURRING-BILLING] Failed to create invoice for contract %s: %v", c.id, err)
			continue
		}

		newNextBillingDate, err := advanceBillingDate(nextBillingDate, frequency)
		if err != nil {
			log.Printf("[RECURRING-BILLING] Failed to advance next_billing_date for contract %s: %v", c.id, err)
			continue
		}
		if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = jsonb_set(data, '{next_billing_date}', to_jsonb($1::text)), updated_at = CURRENT_TIMESTAMP WHERE doctype = 'RecurringSalesContract' AND id = $2`, schema),
			newNextBillingDate, c.id); err != nil {
			log.Printf("[RECURRING-BILLING] Failed to advance contract %s to %s: %v", c.id, newNextBillingDate, err)
		}
	}
}

// StartRecurringBillingWorker mirrors StartRecurringJournalWorker exactly
// (engines/journal_voucher.go).
func StartRecurringBillingWorker(ctx context.Context, interval time.Duration) {
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
					log.Printf("[RECURRING-BILLING] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					runRecurringBillingForSchema(schema)
				}
			}
		}
	}()
}

// ---------------------------------------------------------------------------
// 37.6.4: Price-list versioning - ExchangeRate's own effective-dated-row
// pattern (Stage 37.1.1), not a snapshot-table mechanism: each version is
// its own full, immutable-once-superseded document.
// ---------------------------------------------------------------------------

func ValidatePriceListVersionDocument(tenantID string, payload map[string]interface{}) error {
	effectiveFrom := pimString(payload["effective_from"])
	effectiveTo := pimString(payload["effective_to"])
	if effectiveFrom != "" && effectiveTo != "" && effectiveTo < effectiveFrom {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Effective To", Message: "effective_to cannot be before effective_from"}
	}
	return nil
}

// postApprovedPriceListVersion closes any other Active version of the SAME
// price_list_code whose own window is still open-ended, setting its
// effective_to to the day before this version's effective_from - the
// "versioning" behaviour itself: approving a new version supersedes the old
// one rather than leaving two open-ended Active versions to disagree.
func postApprovedPriceListVersion(tenantID, versionID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedPriceListVersion", fmt.Sprintf("version %s: %v", versionID, err), "")
		return
	}
	data, status, err := fetchDocData(tenantID, "PriceListVersion", versionID)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedPriceListVersion", err.Error(), "")
		return
	}
	if status != "Approved" {
		return
	}
	priceListCode, _ := data["price_list_code"].(string)
	effectiveFrom, _ := data["effective_from"].(string)
	if priceListCode == "" || effectiveFrom == "" {
		return
	}
	dayBefore, err := time.Parse("2006-01-02", effectiveFrom)
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedPriceListVersion", fmt.Sprintf("version %s: bad effective_from %q", versionID, effectiveFrom), "")
		return
	}
	supersededTo := dayBefore.AddDate(0, 0, -1).Format("2006-01-02")
	if _, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET
			data = jsonb_set(jsonb_set(data, '{effective_to}', to_jsonb($1::text)), '{status}', to_jsonb('Superseded'::text)),
			status = 'Superseded', updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'PriceListVersion' AND id <> $2 AND status = 'Approved'
		  AND data->>'price_list_code' = $3
		  AND COALESCE(data->>'effective_to', '') = ''`, schema),
		supersededTo, versionID, priceListCode); err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedPriceListVersion", fmt.Sprintf("version %s: failed to supersede prior open-ended versions: %v", versionID, err), "")
	}
}

// ResolvePriceForSKU finds the Approved PriceListVersion for priceListCode
// whose effective window contains asOfDate and returns that SKU's price -
// ResolveExchangeRate's own findExchangeRate query shape (engines/currency.go).
func ResolvePriceForSKU(tenantID, priceListCode, sku, asOfDate string) (price float64, found bool, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, false, err
	}
	if asOfDate == "" {
		asOfDate = time.Now().Format("2006-01-02")
	}
	// 'Superseded' is included deliberately: it means "no longer the open-
	// ended current version," not "was never valid" - a date that falls
	// within a since-superseded version's own (now-truncated) effective
	// window must still resolve to what was really priced then, e.g. for a
	// backdated document. Only Draft/Pending Approval/Rejected are excluded.
	var itemsRaw string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data->>'items' FROM %s.documents
		WHERE doctype = 'PriceListVersion' AND deleted_at IS NULL AND status IN ('Approved', 'Superseded')
		  AND data->>'price_list_code' = $1
		  AND data->>'effective_from' <= $2
		  AND (COALESCE(data->>'effective_to','') = '' OR data->>'effective_to' >= $2)
		ORDER BY data->>'effective_from' DESC LIMIT 1`, schema),
		priceListCode, asOfDate).Scan(&itemsRaw)
	if err != nil {
		return 0, false, nil
	}
	var items []map[string]interface{}
	if err := json.Unmarshal([]byte(itemsRaw), &items); err != nil {
		return 0, false, nil
	}
	for _, item := range items {
		if pimString(item["sku"]) == sku {
			p, _ := parityNumber(item["price"])
			return p, true, nil
		}
	}
	return 0, false, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "deferred-revenue-roll-forward", Label: "Deferred Revenue Roll-Forward", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "schedule_id", Label: "Schedule"}, {Key: "sales_invoice_id", Label: "Invoice"},
			{Key: "total_amount", Label: "Total", Sensitive: true}, {Key: "recognized_months", Label: "Recognized Months"},
			{Key: "term_months", Label: "Term Months"}, {Key: "recognized_amount", Label: "Recognized", Sensitive: true},
			{Key: "remaining_amount", Label: "Remaining", Sensitive: true}, {Key: "status", Label: "Status"},
		},
		Params: []ReportParam{},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return listAmortizationSchedules(tenantID, "DeferredRevenueSchedule", "sales_invoice_id")
		},
	})
	RegisterReport(ReportDefinition{
		ID: "prepaid-expense-roll-forward", Label: "Prepaid Expense Roll-Forward", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "schedule_id", Label: "Schedule"}, {Key: "sales_invoice_id", Label: "Expense Account"},
			{Key: "total_amount", Label: "Total", Sensitive: true}, {Key: "recognized_months", Label: "Recognized Months"},
			{Key: "term_months", Label: "Term Months"}, {Key: "recognized_amount", Label: "Recognized", Sensitive: true},
			{Key: "remaining_amount", Label: "Remaining", Sensitive: true}, {Key: "status", Label: "Status"},
		},
		Params: []ReportParam{},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return listAmortizationSchedules(tenantID, "PrepaidExpenseSchedule", "expense_account_code")
		},
	})
}

func listAmortizationSchedules(tenantID, doctype, linkedIDField string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'%s', ''), COALESCE((data->>'total_amount')::numeric, 0),
		       COALESCE((data->>'recognized_months')::int, 0), COALESCE((data->>'term_months')::int, 0), status
		FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL ORDER BY created_at DESC`, linkedIDField, schema), doctype)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, linkedID, status string
		var total float64
		var recognizedMonths, termMonths int
		if err := rows.Scan(&id, &linkedID, &total, &recognizedMonths, &termMonths, &status); err != nil {
			return nil, err
		}
		recognizedAmount := 0.0
		if termMonths > 0 {
			recognizedAmount = total * float64(recognizedMonths) / float64(termMonths)
		}
		out = append(out, map[string]interface{}{
			"schedule_id": id, "sales_invoice_id": linkedID, "total_amount": total,
			"recognized_months": recognizedMonths, "term_months": termMonths,
			"recognized_amount": recognizedAmount, "remaining_amount": total - recognizedAmount, "status": status,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}
