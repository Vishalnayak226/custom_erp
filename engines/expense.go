package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"time"
)

// ValidateExpenseClaimControls implements MB 16.2's "Controls" for a new
// claim: a date window (not in the future, not stale) and a duplicate-bill
// check. A hard amount limit isn't implemented - it is already enforced by
// routing through the amount-slab approval engine (Stage 13.8). Receipts are
// attached after the claim is saved (Stage 55.15b, engines/attachments.go)
// and are deliberately not mandatory: the claims list shows "no receipt" so
// an approver or AP can ask for one, rather than payment being blocked.
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

	// Stage 55.15: the amounts must make sense together before anything else.
	gst := numFromInterface(payload["gst_amount"])
	advance := numFromInterface(payload["advance_adjusted"])
	if amount <= 0 {
		return fmt.Errorf("amount must be more than zero")
	}
	if gst < 0 || gst > amount {
		return fmt.Errorf("GST amount must be between 0 and the claim amount")
	}
	if advance < 0 || advance > amount+gst {
		return fmt.Errorf("advance adjusted cannot be more than the claim total (amount + GST)")
	}

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
func PayExpenseClaim(tenantID, claimID string) (payableAmount float64, err error) {
	data, status, err := fetchExpenseClaim(tenantID, claimID)
	if err != nil {
		return 0, fmt.Errorf("claim not found: %v", err)
	}
	if status != "Verified" {
		return 0, fmt.Errorf("only a finance-verified claim can be paid (current status: %s)", status)
	}

	// Stage 132.17: in paise - int(rupees) dropped the paise, so a claim of
	// 1,250.75 was paid and booked as 1,250 (Guide found it).
	var amount, gstAmount, advanceAdjusted int64
	if v, ok := data["amount"].(float64); ok {
		amount = RupeesToPaise(v)
	}
	if v, ok := data["gst_amount"].(float64); ok {
		gstAmount = RupeesToPaise(v)
	}
	if v, ok := data["advance_adjusted"].(float64); ok {
		advanceAdjusted = RupeesToPaise(v)
	}
	if amount <= 0 {
		return 0, fmt.Errorf("claim amount must be positive to pay")
	}
	if advanceAdjusted < 0 || advanceAdjusted > amount+gstAmount {
		return 0, fmt.Errorf("advance adjusted (%.2f) must be between 0 and the claim total (%.2f)", PaiseToRupees(advanceAdjusted), PaiseToRupees(amount+gstAmount))
	}

	debits := map[string]int64{"5400": amount}
	if gstAmount > 0 {
		debits["1500"] = gstAmount
	}
	// Stage 55.15: the cash leaving the bank is what the employee is actually
	// reimbursed - the claim less any advance already paid to them - and the
	// advance itself is cleared from 1600 Staff Loans & Advances, where an
	// advance is booked (DisburseEmployeeLoan). Crediting 1100 with the whole
	// claim overstated the payment by the advance and left 1600 uncleared.
	credits := map[string]int64{"1100": amount + gstAmount - advanceAdjusted}
	if advanceAdjusted > 0 {
		credits["1600"] = advanceAdjusted
	}
	// Stage 37.7.3: a real, adjacent gap found while wiring Project onto
	// this same call - ExpenseClaim.department (a field this doctype has
	// had since Stage 1) was never actually passed as PostingOptions, so it
	// has never reached gl_postings.department. Fixed here since it is the
	// identical call site.
	department, _ := data["department"].(string)
	// Stage 93.9: a claim saved before the department was picked holds free
	// text, which the posting would refuse; an unknown one is left to the
	// claimant's own department (93.10) instead of blocking the payment.
	if resolved, err := ResolveDepartmentReference(tenantID, department); err == nil {
		department = resolved
	} else {
		department = ""
	}
	project, _ := data["project"].(string)
	if err := PostDoubleEntry(tenantID, "ExpenseClaim", claimID, debits, credits, "", fmt.Sprintf("ExpenseClaim:%s:PAY", claimID),
		PostingOptions{Department: department, Project: project}); err != nil {
		return 0, fmt.Errorf("failed to post expense payment GL entry: %v", err)
	}

	if err := saveExpenseClaimStatus(tenantID, claimID, "Paid", data); err != nil {
		return 0, err
	}
	return PaiseToRupees(amount + gstAmount - advanceAdjusted), nil
}
