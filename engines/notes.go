package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// PostDebitNote transitions a Draft DebitNote to Posted and books the GL
// reversal (Stage 20.32): a debit note issued to a vendor reduces what we
// owe them (debit 2100 GRN Suspense/Payable) and reduces the recognized
// purchase cost via the new contra-expense account 5150, rather than
// reusing 1200/5100 - those carry a sale-specific COGS meaning elsewhere
// (PostSalesFinanceBooking) that a purchase-side adjustment shouldn't disturb.
func PostDebitNote(tenantID, noteID, userID string) (amount int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return 0, err
	}

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'DebitNote' AND id = $1 FOR UPDATE`, schema),
		noteID).Scan(&dataStr, &status); err != nil {
		return 0, fmt.Errorf("debit note not found: %v", err)
	}
	if status == "Posted" {
		return 0, fmt.Errorf("debit note is already Posted")
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, err
	}
	amountF := numFromInterface(data["amount"])
	if amountF <= 0 {
		return 0, fmt.Errorf("amount must be positive to post")
	}
	amount = int(amountF)

	debits := map[string]int64{"2100": RupeesToPaise(amountF)}
	credits := map[string]int64{"5150": RupeesToPaise(amountF)}
	if err := PostDoubleEntry(tenantID, "DebitNote", noteID, debits, credits, "", fmt.Sprintf("DebitNote:%s:POST", noteID)); err != nil {
		return 0, fmt.Errorf("GL posting failed, note not marked Posted: %v", err)
	}

	data["status"] = "Posted"
	updatedBytes, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Posted', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'DebitNote' AND id = $2`, schema),
		updatedBytes, noteID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	LogAuditEvent(tenantID, userID, "POST_DEBIT_NOTE", "SUCCESS", fmt.Sprintf("Posted debit note %s amount=%d", noteID, amount))
	return amount, nil
}

// PostCreditNote transitions a Draft CreditNote to Posted and books the GL
// reversal: a credit note issued to a customer gives back recognized
// revenue (debit the new contra-revenue account 4150, matching DebitNote's
// use of 5150 on the purchase side) and pays cash back out (credit 1100),
// modeling a direct refund - the same cash-refund philosophy this
// codebase's POS returns already use (engines.ProcessReturnAnywhere), not
// an open-receivable adjustment.
func PostCreditNote(tenantID, noteID, userID string) (amount int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return 0, err
	}

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'CreditNote' AND id = $1 FOR UPDATE`, schema),
		noteID).Scan(&dataStr, &status); err != nil {
		return 0, fmt.Errorf("credit note not found: %v", err)
	}
	if status == "Posted" {
		return 0, fmt.Errorf("credit note is already Posted")
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, err
	}
	amountF := numFromInterface(data["amount"])
	if amountF <= 0 {
		return 0, fmt.Errorf("amount must be positive to post")
	}
	amount = int(amountF)

	debits := map[string]int64{"4150": RupeesToPaise(amountF)}
	credits := map[string]int64{"1100": RupeesToPaise(amountF)}
	if err := PostDoubleEntry(tenantID, "CreditNote", noteID, debits, credits, "", fmt.Sprintf("CreditNote:%s:POST", noteID)); err != nil {
		return 0, fmt.Errorf("GL posting failed, note not marked Posted: %v", err)
	}

	data["status"] = "Posted"
	updatedBytes, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Posted', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'CreditNote' AND id = $2`, schema),
		updatedBytes, noteID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	LogAuditEvent(tenantID, userID, "POST_CREDIT_NOTE", "SUCCESS", fmt.Sprintf("Posted credit note %s amount=%d", noteID, amount))
	return amount, nil
}
