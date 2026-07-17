package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"time"
)

// ValidateExpenseClaimControls implements MB 16.2's "Controls" for a new
// claim: a date window (not in the future, not stale) and a duplicate-bill
// check. Mandatory attachment and a hard amount limit aren't implemented -
// this codebase has no file-upload infrastructure to attach a bill to, and
// the amount limit is already enforced by routing through the amount-slab
// approval engine (Stage 13.8) rather than a separate hard cutoff.
func ValidateExpenseClaimControls(tenantID string, payload map[string]interface{}) error {
	expenseDateStr, _ := payload["expense_date"].(string)
	expenseDate, err := time.Parse("2006-01-02", expenseDateStr)
	if err != nil {
		return fmt.Errorf("expense_date must be a valid YYYY-MM-DD date")
	}
	if expenseDate.After(time.Now()) {
		return fmt.Errorf("expense date cannot be in the future")
	}
	if time.Since(expenseDate) > 90*24*time.Hour {
		return fmt.Errorf("expense date is more than 90 days old - outside the claim window")
	}

	employeeID, _ := payload["employee_id"].(string)
	category, _ := payload["category"].(string)
	amount, _ := payload["amount"].(float64)

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var count int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.documents
		WHERE doctype = 'ExpenseClaim' AND data->>'employee_id' = $1 AND data->>'category' = $2
		AND data->>'expense_date' = $3 AND (data->>'amount')::numeric = $4`, schema),
		employeeID, category, expenseDateStr, amount).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("a claim for this employee/category/date/amount already exists - possible duplicate bill")
	}
	return nil
}

func fetchExpenseClaim(tenantID, claimID string) (data map[string]interface{}, status string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'ExpenseClaim' AND id = $1`, schema), claimID).Scan(&dataStr, &status)
	if err != nil {
		return nil, "", err
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", err
	}
	return data, status, nil
}

func saveExpenseClaimStatus(tenantID, claimID, newStatus string, data map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data["status"] = newStatus
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ExpenseClaim' AND id = $3`, schema), marshaled, newStatus, claimID)
	return err
}

// VerifyExpenseClaim implements the "Finance Verification" stage: a
// Manager-Approved claim (via the Stage 13.8 approval engine) gets a
// second, independent check before payment. Restricted to HR/Admin - this
// codebase has no distinct Finance role (same reasoning as Stage 13.3's
// MFA role mapping).
func VerifyExpenseClaim(tenantID, claimID string) error {
	data, status, err := fetchExpenseClaim(tenantID, claimID)
	if err != nil {
		return fmt.Errorf("claim not found: %v", err)
	}
	if status != "Approved" {
		return fmt.Errorf("only a manager-approved claim can be finance-verified (current status: %s)", status)
	}
	return saveExpenseClaimStatus(tenantID, claimID, "Verified", data)
}

// PayExpenseClaim implements "Payment -> Accounting": posts a balanced GL
// entry (Debit Employee Expense + GST Input Credit if eligible, Credit
// Cash/Bank) and marks the claim Paid. advance_adjusted is returned as
// informational context (matching MB 16.2's "payable amount" field) but is
// NOT netted into the GL entry - this codebase has no advance-issuance
// ledger tracking a prior debit to net it against, and fabricating a credit
// with no matching earlier entry would be accounting-incorrect. Advance
// issue/usage/balance tracking (MB 16.2's "Advance" requirement) is
// explicitly out of scope for this pass.
func PayExpenseClaim(tenantID, claimID string) (payableAmount int, err error) {
	data, status, err := fetchExpenseClaim(tenantID, claimID)
	if err != nil {
		return 0, fmt.Errorf("claim not found: %v", err)
	}
	if status != "Verified" {
		return 0, fmt.Errorf("only a finance-verified claim can be paid (current status: %s)", status)
	}

	amount := 0
	if v, ok := data["amount"].(float64); ok {
		amount = int(v)
	}
	gstAmount := 0
	if v, ok := data["gst_amount"].(float64); ok {
		gstAmount = int(v)
	}
	advanceAdjusted := 0
	if v, ok := data["advance_adjusted"].(float64); ok {
		advanceAdjusted = int(v)
	}
	if amount <= 0 {
		return 0, fmt.Errorf("claim amount must be positive to pay")
	}

	debits := map[string]int{"5400": amount}
	if gstAmount > 0 {
		debits["1500"] = gstAmount
	}
	credits := map[string]int{"1100": amount + gstAmount}
	if err := PostDoubleEntry(tenantID, "ExpenseClaim", claimID, debits, credits); err != nil {
		return 0, fmt.Errorf("failed to post expense payment GL entry: %v", err)
	}

	if err := saveExpenseClaimStatus(tenantID, claimID, "Paid", data); err != nil {
		return 0, err
	}
	return amount + gstAmount - advanceAdjusted, nil
}
