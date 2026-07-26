package engines

import (
	"bytes"
	"custom_erp/db"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 26.6.5: payment-file (bank-file) generation for an Executed
// PaymentProposal (Stage 20.27, engines/payment_proposal.go) + a
// duplicate-UTR check once the bank returns one.

// GeneratePaymentFile builds a generic NEFT/RTGS-style bulk-upload CSV
// (beneficiary name/account/IFSC/amount) from an Executed proposal's Paid
// invoice lines - each invoice's Vendor supplies the bank details
// (bank_account_number/bank_ifsc, already-existing Vendor fields). A real
// bank portal's exact column format varies by bank; this is a generic
// shape a human can adapt/paste from, not a specific bank's proprietary
// format.
func GeneratePaymentFile(tenantID, proposalID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var dataStr, status string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'PaymentProposal' AND id = $1`, schema), proposalID).
		Scan(&dataStr, &status); err != nil {
		return "", fmt.Errorf("payment proposal not found: %v", err)
	}
	if status != "Executed" {
		return "", fmt.Errorf("a payment file can only be generated for an Executed proposal (current status: %s)", status)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return "", fmt.Errorf("proposal %s has corrupt stored data: %v", proposalID, err)
	}
	resultsRaw, err := json.Marshal(data["execution_results"])
	if err != nil {
		return "", err
	}
	var results []PaymentProposalResultLine
	if err := json.Unmarshal(resultsRaw, &results); err != nil {
		return "", fmt.Errorf("could not parse proposal's execution_results: %v", err)
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"invoice_id", "beneficiary_name", "account_number", "ifsc", "amount"}); err != nil {
		return "", err
	}
	for _, r := range results {
		if !r.Paid {
			continue
		}
		name, account, ifsc, err := resolveVendorBankDetails(schema, r.InvoiceID)
		if err != nil {
			return "", err
		}
		if err := w.Write([]string{r.InvoiceID, name, account, ifsc, fmt.Sprintf("%d", r.Amount)}); err != nil {
			return "", err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func resolveVendorBankDetails(schema, invoiceID string) (name, account, ifsc string, err error) {
	var invDataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1`, schema), invoiceID).Scan(&invDataStr); err != nil {
		return "", "", "", fmt.Errorf("vendor invoice %s not found: %v", invoiceID, err)
	}
	var invData map[string]interface{}
	if err := json.Unmarshal([]byte(invDataStr), &invData); err != nil {
		return "", "", "", fmt.Errorf("vendor invoice %s has corrupt stored data: %v", invoiceID, err)
	}
	vendorID, _ := invData["vendor_id"].(string)

	var vendorDataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'Vendor' AND id = $1`, schema), vendorID).Scan(&vendorDataStr); err != nil {
		return "", "", "", fmt.Errorf("vendor %s (invoice %s) not found: %v", vendorID, invoiceID, err)
	}
	var vendorData map[string]interface{}
	if err := json.Unmarshal([]byte(vendorDataStr), &vendorData); err != nil {
		return "", "", "", fmt.Errorf("vendor %s has corrupt stored data: %v", vendorID, err)
	}
	name, _ = vendorData["name"].(string)
	account, _ = vendorData["bank_account_number"].(string)
	ifsc, _ = vendorData["bank_ifsc"].(string)
	if account == "" || ifsc == "" {
		return "", "", "", fmt.Errorf("vendor %s has no bank_account_number/bank_ifsc configured - cannot include invoice %s in a payment file", vendorID, invoiceID)
	}
	return name, account, ifsc, nil
}

// PaymentUTREntry is one recorded bank UTR against a proposal's invoice.
type PaymentUTREntry struct {
	InvoiceID  string `json:"invoice_id"`
	UTR        string `json:"utr"`
	RecordedAt string `json:"recorded_at"`
}

// RecordPaymentUTR attaches the bank-returned UTR for one invoice of an
// Executed proposal. The UNIQUE constraint on payment_utr_log.utr is the
// actual duplicate-UTR check - the same bank transaction reference can
// never be recorded against two different payments, which would otherwise
// silently double-count one real bank transfer as two.
func RecordPaymentUTR(tenantID, proposalID, invoiceID, utr string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if utr == "" {
		return fmt.Errorf("utr is required")
	}
	var status string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'PaymentProposal' AND id = $1`, schema), proposalID).Scan(&status); err != nil {
		return fmt.Errorf("payment proposal not found: %v", err)
	}
	if status != "Executed" {
		return fmt.Errorf("a UTR can only be recorded against an Executed proposal (current status: %s)", status)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.payment_utr_log (proposal_id, invoice_id, utr) VALUES ($1, $2, $3)`, schema),
		proposalID, invoiceID, utr); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return fmt.Errorf("UTR %q has already been recorded against another payment - each bank transaction reference must be unique", utr)
		}
		return err
	}
	return nil
}

// ListPaymentUTRs returns every UTR recorded against one proposal.
func ListPaymentUTRs(tenantID, proposalID string) ([]PaymentUTREntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT invoice_id, utr, recorded_at::text FROM %s.payment_utr_log WHERE proposal_id = $1 ORDER BY recorded_at ASC`, schema), proposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PaymentUTREntry{}
	for rows.Next() {
		var e PaymentUTREntry
		if err := rows.Scan(&e.InvoiceID, &e.UTR, &e.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
