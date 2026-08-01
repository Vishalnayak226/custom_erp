package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// PaymentProposalResultLine reports what happened to one invoice inside a
// proposal's execution.
type PaymentProposalResultLine struct {
	InvoiceID string `json:"invoice_id"`
	Paid      bool   `json:"paid"`
	Amount    int    `json:"amount,omitempty"`
	Error     string `json:"error,omitempty"`
}

// CreatePaymentProposal groups multiple Matched VendorInvoices into one
// Draft payment run (Stage 20.27), extending Stage 17.8's one-at-a-time
// PayVendorInvoice rather than replacing it - execution below still calls
// PayVendorInvoice per invoice, just batched. total_amount is always
// server-computed from the invoices' own invoice_amount, never client-supplied.
func CreatePaymentProposal(tenantID string, invoiceIDs []string, userID string) (proposalID string, totalAmount int, err error) {
	if len(invoiceIDs) == 0 {
		return "", 0, fmt.Errorf("at least one invoice_id is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, err
	}

	total := 0
	for _, invID := range invoiceIDs {
		var dataStr, status string
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT data, status FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1`, schema), invID).
			Scan(&dataStr, &status); err != nil {
			return "", 0, fmt.Errorf("vendor invoice %s not found: %v", invID, err)
		}
		if status != "Matched" {
			return "", 0, fmt.Errorf("vendor invoice %s is not Matched (status: %s) - only Matched invoices can join a payment proposal", invID, status)
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			// 24.18: a corrupt invoice's amount would otherwise silently
			// read as 0 (nil map read, not a panic) and understate
			// total_amount for the whole proposal - a real financial
			// correctness risk, not just a display glitch. Reject instead.
			return "", 0, fmt.Errorf("vendor invoice %s has corrupt stored data: %v", invID, err)
		}
		total += int(numFromInterface(data["invoice_amount"]))
	}

	idsJSON, err := json.Marshal(invoiceIDs)
	if err != nil {
		return "", 0, err
	}
	proposalID = NewDocID("PROP")
	docData := map[string]interface{}{
		"id":              proposalID,
		"code":            proposalID,
		"proposal_number": proposalID,
		"invoice_ids":     string(idsJSON),
		"total_amount":    total,
		"status":          "Draft",
	}
	marshaled, err := json.Marshal(docData)
	if err != nil {
		return "", 0, err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'PaymentProposal', $2, 'Draft', $3)`, schema),
		proposalID, marshaled, userID)
	if err != nil {
		return "", 0, err
	}
	LogAuditEvent(tenantID, userID, "CREATE_PAYMENT_PROPOSAL", "SUCCESS",
		fmt.Sprintf("Created payment proposal %s for %d invoices, total %d", proposalID, len(invoiceIDs), total))
	return proposalID, total, nil
}

// ExecutePaymentProposal pays every invoice in a Draft proposal via the
// existing PayVendorInvoice, one invoice at a time. Each invoice pay is
// already its own atomic transaction (PayVendorInvoice locks and commits
// per-invoice) - this does not attempt to wrap the whole batch in one
// larger transaction, so a failure partway through is reported per-invoice
// rather than rolling back invoices already paid. The proposal always moves
// to Executed once run (not retryable as a whole), with the full per-line
// outcome stored on the document for audit visibility.
func ExecutePaymentProposal(tenantID, proposalID, userID string) ([]PaymentProposalResultLine, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr, status string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'PaymentProposal' AND id = $1`, schema), proposalID).
		Scan(&dataStr, &status); err != nil {
		return nil, fmt.Errorf("payment proposal not found: %v", err)
	}
	if status != "Draft" {
		return nil, fmt.Errorf("payment proposal is not Draft (status: %s) - already executed", status)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, err
	}
	idsStr, _ := data["invoice_ids"].(string)
	var invoiceIDs []string
	if err := json.Unmarshal([]byte(idsStr), &invoiceIDs); err != nil {
		return nil, fmt.Errorf("could not parse proposal's invoice_ids: %v", err)
	}

	var results []PaymentProposalResultLine
	totalPaid := 0
	for _, invID := range invoiceIDs {
		// No override_reason - CreatePaymentProposal already required every
		// invoice to be Matched, so this should never hit 24.11's
		// approval-routing branch; pendingApproval is checked anyway rather
		// than assumed, in case an invoice's status changed out from under
		// the proposal between creation and execution.
		amount, pendingApproval, err := PayVendorInvoice(tenantID, invID, userID, "", "")
		if err != nil {
			results = append(results, PaymentProposalResultLine{InvoiceID: invID, Paid: false, Error: err.Error()})
			continue
		}
		if pendingApproval {
			results = append(results, PaymentProposalResultLine{InvoiceID: invID, Paid: false, Error: "invoice is no longer Matched and requires override approval"})
			continue
		}
		totalPaid += amount
		results = append(results, PaymentProposalResultLine{InvoiceID: invID, Paid: true, Amount: amount})
	}

	resultsJSON, err := json.Marshal(results)
	if err != nil {
		return nil, err
	}
	data["status"] = "Executed"
	data["total_paid"] = totalPaid
	data["execution_results"] = json.RawMessage(resultsJSON)
	updatedBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Executed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PaymentProposal' AND id = $2`, schema),
		updatedBytes, proposalID); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "EXECUTE_PAYMENT_PROPOSAL", "SUCCESS",
		fmt.Sprintf("Executed payment proposal %s: %d/%d invoices paid, total %d", proposalID, totalPaid, len(invoiceIDs), totalPaid))
	return results, nil
}
