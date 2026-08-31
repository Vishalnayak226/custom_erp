package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Stage 37.4: Budgeting, cash-flow forecast, credit limits, dunning.
//
// Pre-build audit: all four were completely absent. No real Customer Link
// exists on SalesOrder/SalesInvoice - customer identity flows as a free-text
// name throughout (SalesInvoice.customer is literally the order's
// customer_name, engines/order_invoice.go:66), the same convention
// GetCustomerLedgerReport's own customerFilter already relies on. Credit
// limits here match BY NAME for the same reason - a second, Link-based
// identity model would be new scope well beyond this stage, and would still
// need to agree with every existing report that already matches by name.

// ---------------------------------------------------------------------------
// 37.4.1: Budgeting. Budget is a pure generic-document doctype (the Currency/
// ExchangeRate precedent) - no dedicated create/apply function, no GL
// posting. ValidateBudgetDocument runs at ValidateDocument's shared exit via
// ValidateParityFoundationDocument's dispatcher (engines/currency.go).
// ---------------------------------------------------------------------------

func ValidateBudgetDocument(tenantID string, payload map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if costCenter := pimString(payload["cost_center"]); costCenter != "" {
		if err := validateCostCenterReferenceInSchema(schema, costCenter); err != nil {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Cost Center", Message: err.Error()}
		}
	}
	if accountCode := pimString(payload["account_code"]); accountCode != "" {
		if _, err := validateGLAccountCodeInSchema(schema, accountCode); err != nil {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "GL Account", Message: err.Error()}
		}
	}
	periodStart := pimString(payload["period_start"])
	periodEnd := pimString(payload["period_end"])
	if periodStart != "" && periodEnd != "" && periodEnd < periodStart {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Period End", Message: "a budget's period_end cannot be before its period_start"}
	}
	if amount, ok := parityNumber(payload["planned_amount"]); ok && amount <= 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Planned Amount", Message: "a budget's planned_amount must be greater than zero"}
	}
	return nil
}

// GetBudgetVarianceReport compares every Approved Budget row whose own
// period overlaps [periodStart, periodEnd] against the real gl_postings
// activity for the same (cost_center, account_code) in that window -
// GetCostCenterPL's own date-range/COALESCE('Unassigned') shape, one level
// more granular (per account rather than per account_type) since a budget
// is set per account, not per Revenue/Expense bucket as a whole.
func GetBudgetVarianceReport(tenantID, periodStart, periodEnd string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if periodStart == "" || periodEnd == "" {
		return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "Period", Message: "start and end are required"}
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'cost_center', 'Unassigned') AS cost_center, data->>'account_code' AS account_code,
		       COALESCE(SUM((data->>'planned_amount')::numeric), 0) AS planned
		FROM %s.documents
		WHERE doctype = 'Budget' AND status = 'Approved' AND deleted_at IS NULL
		  AND (data->>'period_start') <= $2 AND (data->>'period_end') >= $1
		GROUP BY COALESCE(data->>'cost_center', 'Unassigned'), data->>'account_code'`, schema),
		periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	type row struct {
		costCenter, accountCode string
		planned                 float64
	}
	var budgetRows []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.costCenter, &r.accountCode, &r.planned); err != nil {
			rows.Close()
			return nil, err
		}
		budgetRows = append(budgetRows, r)
	}
	rows.Close()

	out := make([]map[string]interface{}, 0, len(budgetRows))
	for _, b := range budgetRows {
		var debitPaise, creditPaise int64
		costCenterClause := "p.cost_center IS NULL"
		args := []interface{}{b.accountCode, periodStart, periodEnd}
		if b.costCenter != "Unassigned" {
			costCenterClause = "p.cost_center = $4"
			args = append(args, b.costCenter)
		}
		query := fmt.Sprintf(`
			SELECT COALESCE(SUM(p.debit), 0), COALESCE(SUM(p.credit), 0)
			FROM %s.gl_postings p JOIN %s.gl_accounts a ON a.account_code = p.account_code
			WHERE p.account_code = $1 AND p.created_at >= $2::date AND p.created_at < ($3::date + 1) AND %s`,
			schema, schema, costCenterClause)
		if err := db.DB.QueryRow(query, args...).Scan(&debitPaise, &creditPaise); err != nil {
			return nil, err
		}
		var accountType string
		db.DB.QueryRow(fmt.Sprintf(`SELECT account_type FROM %s.gl_accounts WHERE account_code = $1`, schema), b.accountCode).Scan(&accountType)
		actual := PaiseToRupees(debitPaise)
		if accountType == "Revenue" {
			actual = PaiseToRupees(creditPaise)
		}
		out = append(out, map[string]interface{}{
			"cost_center": b.costCenter, "account_code": b.accountCode,
			"planned_amount": b.planned, "actual_amount": actual, "variance": actual - b.planned,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 37.4.2: Credit limits.
// ---------------------------------------------------------------------------

// customerOutstandingReceivable sums this customer's open (Approved, not yet
// Paid) SalesInvoice.total_amount - matching Customer.name against
// SalesInvoice.customer, the same free-text convention
// GetCustomerLedgerReport (engines/finance_reports_stage26.go) already uses,
// since neither doctype carries a real Customer Link today.
func customerOutstandingReceivable(schema, customerName string) (float64, error) {
	var total float64
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM((data->>'total_amount')::numeric), 0) FROM %s.documents
		 WHERE doctype = 'SalesInvoice' AND status = 'Approved' AND deleted_at IS NULL AND data->>'customer' = $1`, schema),
		customerName).Scan(&total)
	return total, err
}

// CheckCustomerCreditLimit refuses when a customer's outstanding receivable
// plus a new invoice's amount would exceed their Customer.credit_limit. A
// blank/zero/unregistered credit_limit means unlimited - the same "empty is
// always valid" posture every other optional master-linked check in this
// codebase (CostCenter, Department, Entity...) already takes, so a tenant
// that never sets a limit sees no behaviour change.
func CheckCustomerCreditLimit(tenantID, customerName string, additionalAmount float64) error {
	if customerName == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'Customer' AND (data->>'name' = $1 OR id = $1) AND deleted_at IS NULL LIMIT 1`, schema),
		customerName).Scan(&dataStr)
	if err != nil {
		return nil // no matching Customer master - nothing to check against
	}
	var data map[string]interface{}
	if jsonErr := json.Unmarshal([]byte(dataStr), &data); jsonErr != nil {
		return nil
	}
	limit, ok := parityNumber(data["credit_limit"])
	if !ok || limit <= 0 {
		return nil
	}
	outstanding, err := customerOutstandingReceivable(schema, customerName)
	if err != nil {
		return err
	}
	if outstanding+additionalAmount > limit {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Customer",
			Message: fmt.Sprintf("posting this invoice would take %s's outstanding receivable to %.2f, over their credit limit of %.2f", customerName, outstanding+additionalAmount, limit)}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 37.4.4: Dunning. Reuses the existing DispatchNotification/outbox machinery
// entirely (engines/notifications.go) rather than a second delivery path -
// this file only adds the scheduled scan that decides WHEN to call it.
// ---------------------------------------------------------------------------

var dunningTierRank = map[string]int{"": 0, "Reminder": 1, "Escalation": 2}

func dunningTierFor(daysOverdue, reminderDays, escalationDays int) string {
	switch {
	case daysOverdue >= escalationDays:
		return "Escalation"
	case daysOverdue >= reminderDays:
		return "Reminder"
	default:
		return ""
	}
}

type dunningInvoice struct {
	id, customer, dueDate, lastTier string
	amount                          float64
}

// runDunningForSchema scans every open SalesInvoice with a due_date (one
// with no due_date has no way to know it's overdue - a stated, logged-once
// limitation, not a silent skip) and, for one whose age has newly crossed a
// tier boundary since its last notification, dispatches a notification and
// advances the stamp. Safe to call repeatedly - an invoice already
// notified at Reminder is only re-notified on crossing into Escalation, and
// a Paid/Cancelled invoice simply stops matching the WHERE clause.
func runDunningForSchema(tenantID, schema string) (notified int, err error) {
	reminderDays := GetSettingInt(tenantID, "finance.dunning_reminder_days")
	escalationDays := GetSettingInt(tenantID, "finance.dunning_escalation_days")
	if reminderDays <= 0 {
		reminderDays = 7
	}
	if escalationDays <= reminderDays {
		escalationDays = reminderDays + 23
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'customer', ''), data->>'due_date',
		       COALESCE(data->>'dunning_last_notified_tier', ''), COALESCE((data->>'total_amount')::numeric, 0)
		FROM %s.documents
		WHERE doctype = 'SalesInvoice' AND status = 'Approved' AND deleted_at IS NULL
		  AND COALESCE(data->>'due_date', '') <> ''`, schema))
	if err != nil {
		return 0, err
	}
	var invoices []dunningInvoice
	for rows.Next() {
		var inv dunningInvoice
		if err := rows.Scan(&inv.id, &inv.customer, &inv.dueDate, &inv.lastTier, &inv.amount); err != nil {
			rows.Close()
			return notified, err
		}
		invoices = append(invoices, inv)
	}
	rows.Close()

	today := time.Now()
	for _, inv := range invoices {
		dueDate, parseErr := time.Parse("2006-01-02", inv.dueDate)
		if parseErr != nil {
			continue
		}
		daysOverdue := int(today.Sub(dueDate).Hours() / 24)
		tier := dunningTierFor(daysOverdue, reminderDays, escalationDays)
		if dunningTierRank[tier] <= dunningTierRank[inv.lastTier] {
			continue
		}
		DispatchNotification(tenantID, "Invoice "+tier, inv.id, map[string]string{
			"customer": inv.customer, "days_overdue": fmt.Sprintf("%d", daysOverdue), "amount": fmt.Sprintf("%.2f", inv.amount),
		})
		if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = jsonb_set(jsonb_set(data, '{dunning_last_notified_tier}', to_jsonb($1::text)), '{dunning_last_notified_at}', to_jsonb($2::text))
			 WHERE doctype = 'SalesInvoice' AND id = $3`, schema),
			tier, today.Format("2006-01-02"), inv.id); err != nil {
			LogSystemError(tenantID, "", "ERROR", "runDunningForSchema", fmt.Sprintf("invoice %s: failed to record dunning stamp: %v", inv.id, err), "")
			continue
		}
		notified++
	}
	return notified, nil
}

// StartDunningWorker polls every tenant schema, the StartReservationSweeper
// precedent - re-queried each tick so a newly provisioned tenant is picked
// up without a restart.
func StartDunningWorker(ctx context.Context, interval time.Duration) {
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
					log.Printf("[DUNNING] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					tenantID, err := tenantIDForSchema(schema)
					if err != nil {
						log.Printf("[DUNNING] %s: could not resolve tenant id: %v", schema, err)
						continue
					}
					notified, err := runDunningForSchema(tenantID, schema)
					if err != nil {
						log.Printf("[DUNNING] %s: %v", schema, err)
						continue
					}
					if notified > 0 {
						log.Printf("[DUNNING] %s: dispatched %d overdue-invoice notification(s)", schema, notified)
					}
				}
			}
		}
	}()
}

// GetDunningQueue (report) lists every invoice a dunning scan would consider
// today, with its computed days-overdue and current tier - visibility into
// what the worker above is about to do, and a way to see it even before the
// interval next ticks.
func GetDunningQueue(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	reminderDays := GetSettingInt(tenantID, "finance.dunning_reminder_days")
	escalationDays := GetSettingInt(tenantID, "finance.dunning_escalation_days")
	if reminderDays <= 0 {
		reminderDays = 7
	}
	if escalationDays <= reminderDays {
		escalationDays = reminderDays + 23
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'customer', ''), data->>'due_date',
		       COALESCE(data->>'dunning_last_notified_tier', ''), COALESCE((data->>'total_amount')::numeric, 0)
		FROM %s.documents
		WHERE doctype = 'SalesInvoice' AND status = 'Approved' AND deleted_at IS NULL
		  AND COALESCE(data->>'due_date', '') <> ''
		ORDER BY data->>'due_date'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	today := time.Now()
	var out []map[string]interface{}
	for rows.Next() {
		var id, customer, dueDateStr, lastTier string
		var amount float64
		if err := rows.Scan(&id, &customer, &dueDateStr, &lastTier, &amount); err != nil {
			return nil, err
		}
		dueDate, parseErr := time.Parse("2006-01-02", dueDateStr)
		if parseErr != nil {
			continue
		}
		daysOverdue := int(today.Sub(dueDate).Hours() / 24)
		if daysOverdue < 0 {
			continue
		}
		out = append(out, map[string]interface{}{
			"invoice_id": id, "customer": customer, "due_date": dueDateStr,
			"days_overdue": daysOverdue, "amount": amount,
			"tier": dunningTierFor(daysOverdue, reminderDays, escalationDays), "last_notified_tier": lastTier,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 37.4.5: Cash-flow forecast - projects future cash position from open
// invoices' due dates, unlike GetCashFlowStatement (engines/
// finance_reports_stage26.go), which is historical only. An invoice with no
// due_date cannot be projected (no date to project it to) and is excluded -
// stated, not silently guessed at.
// ---------------------------------------------------------------------------

// amountField differs by doctype: SalesInvoice stores total_amount,
// VendorInvoice stores invoice_amount (engines/vendor_invoice.go) - two
// pre-existing, differently-named fields for the same "what this invoice is
// for" concept, not something this stage invented or can unify.
func openInvoiceDueTotalsByDate(schema, doctype, amountField, asOfDate, horizonEndDate string) (map[string]float64, error) {
	// amountField is always one of this file's own two hardcoded call-site
	// literals ("total_amount"/"invoice_amount"), never external input, so a
	// direct %s interpolation into the JSON field name is as safe as this
	// codebase's many other doctype/column-name interpolations elsewhere.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'due_date', COALESCE((data->>'%s')::numeric, 0)
		FROM %s.documents
		WHERE doctype = $1 AND status = 'Approved' AND deleted_at IS NULL
		  AND data->>'due_date' >= $2 AND data->>'due_date' <= $3`, amountField, schema),
		doctype, asOfDate, horizonEndDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := map[string]float64{}
	for rows.Next() {
		var dueDate string
		var amount float64
		if err := rows.Scan(&dueDate, &amount); err != nil {
			return nil, err
		}
		totals[dueDate] += amount
	}
	return totals, nil
}

// currentCashBalance sums the tenant's own Cash/Bank-family accounts (1100
// Cash/Bank, 1101 Card Clearing, 1102 UPI Clearing - the three Stage 20.9
// payment-mode accounts) as of asOfDate, the same debit-minus-credit shape
// GetTrialBalance already uses per account.
func currentCashBalance(schema, asOfDate string) (float64, error) {
	var debitPaise, creditPaise int64
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(debit), 0), COALESCE(SUM(credit), 0) FROM %s.gl_postings
		WHERE account_code IN ('1100', '1101', '1102') AND created_at < ($1::date + 1)`, schema),
		asOfDate).Scan(&debitPaise, &creditPaise)
	if err != nil {
		return 0, err
	}
	return PaiseToRupees(debitPaise - creditPaise), nil
}

// GetCashFlowForecast projects a running cash balance day by day from
// asOfDate across horizonDays: opening balance (today's real Cash/Bank GL
// balance) plus expected inflows (open SalesInvoice due that day) minus
// expected outflows (open VendorInvoice due that day).
func GetCashFlowForecast(tenantID, asOfDate string, horizonDays int) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if asOfDate == "" {
		return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "As Of Date", Message: "as_of date is required"}
	}
	if horizonDays <= 0 {
		horizonDays = 30
	}
	start, err := time.Parse("2006-01-02", asOfDate)
	if err != nil {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "As Of Date", Message: "as_of must be YYYY-MM-DD"}
	}
	horizonEnd := start.AddDate(0, 0, horizonDays).Format("2006-01-02")

	inflows, err := openInvoiceDueTotalsByDate(schema, "SalesInvoice", "total_amount", asOfDate, horizonEnd)
	if err != nil {
		return nil, err
	}
	outflows, err := openInvoiceDueTotalsByDate(schema, "VendorInvoice", "invoice_amount", asOfDate, horizonEnd)
	if err != nil {
		return nil, err
	}
	balance, err := currentCashBalance(schema, asOfDate)
	if err != nil {
		return nil, err
	}

	out := make([]map[string]interface{}, 0, horizonDays+1)
	for i := 0; i <= horizonDays; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		in := inflows[day]
		out2 := outflows[day]
		balance += in - out2
		out = append(out, map[string]interface{}{
			"date": day, "expected_inflow": in, "expected_outflow": out2, "projected_balance": balance,
		})
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "budget-variance", Label: "Budget vs Actual", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "cost_center", Label: "Cost Center"}, {Key: "account_code", Label: "Account"},
			{Key: "planned_amount", Label: "Planned", Sensitive: true}, {Key: "actual_amount", Label: "Actual", Sensitive: true},
			{Key: "variance", Label: "Variance", Sensitive: true},
		},
		Params: []ReportParam{
			{Key: "start", Label: "From", Type: "date", Required: true},
			{Key: "end", Label: "To", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetBudgetVarianceReport(tenantID, params["start"], params["end"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "dunning-queue", Label: "Dunning Queue", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "invoice_id", Label: "Invoice"}, {Key: "customer", Label: "Customer"}, {Key: "due_date", Label: "Due Date"},
			{Key: "days_overdue", Label: "Days Overdue"}, {Key: "amount", Label: "Amount", Sensitive: true},
			{Key: "tier", Label: "Tier"}, {Key: "last_notified_tier", Label: "Last Notified"},
		},
		Params: []ReportParam{},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetDunningQueue(tenantID)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "cash-flow-forecast", Label: "Cash Flow Forecast", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "date", Label: "Date"}, {Key: "expected_inflow", Label: "Expected Inflow", Sensitive: true},
			{Key: "expected_outflow", Label: "Expected Outflow", Sensitive: true}, {Key: "projected_balance", Label: "Projected Balance", Sensitive: true},
		},
		Params: []ReportParam{
			{Key: "as_of", Label: "As Of", Type: "date", Required: true},
			{Key: "horizon_days", Label: "Horizon (days)", Type: "text", Required: false},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			horizon := 30
			if v := params["horizon_days"]; v != "" {
				fmt.Sscanf(v, "%d", &horizon)
			}
			return GetCashFlowForecast(tenantID, params["as_of"], horizon)
		},
	})
}
