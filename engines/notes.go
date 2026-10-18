package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// outputGSTAccounts are the accounts a sales invoice books its output GST to
// (outputGSTCreditsPaise): CGST, SGST, IGST payable.
var outputGSTAccounts = []string{"2200", "2201", "2202"}

// creditNoteDebitsPaise (Stage 73.D.10) is what a customer credit note takes
// back. Against a sales invoice that booked output GST, the note gives back
// that GST - per account, in proportion to the note's share of the invoice,
// and never more than the invoice booked less what earlier notes on it already
// gave back - and the rest goes to 4150. Before this the whole note debited
// 4150 and the invoice's GST stayed payable, overstating the GST return. A
// note naming no invoice, or one whose invoice booked no output GST (posted
// before the split, foreign, deferred), debits 4150 in full as before. The
// split is stamped on the note (taxable_amount, cgst, sgst, igst).
func creditNoteDebitsPaise(tx *sql.Tx, schema, invoiceID, noteID string, notePaise int64, note map[string]interface{}) (map[string]int64, error) {
	debits := map[string]int64{"4150": notePaise}
	invoiceID = strings.TrimSpace(invoiceID)
	if invoiceID == "" || notePaise <= 0 {
		return debits, nil
	}
	var totalPaise int64
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(debit), 0) FROM %s.gl_postings
		WHERE document_type = 'SalesInvoice' AND document_id = $1 AND account_code = '1300'`, schema), invoiceID).Scan(&totalPaise); err != nil {
		return nil, err
	}
	if totalPaise <= 0 {
		return debits, nil
	}
	remaining := func(account string) (booked, left int64, err error) {
		if err = tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(credit) - SUM(debit), 0) FROM %s.gl_postings
			WHERE document_type = 'SalesInvoice' AND document_id = $1 AND account_code = $2`, schema), invoiceID, account).Scan(&booked); err != nil {
			return 0, 0, err
		}
		var given int64
		if err = tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(g.debit) - SUM(g.credit), 0) FROM %s.gl_postings g
			JOIN %s.documents d ON d.id = g.document_id AND d.doctype = 'CreditNote'
			WHERE g.document_type = 'CreditNote' AND g.account_code = $2 AND d.id <> $3 AND d.data->>'sales_invoice_id' = $1`, schema, schema),
			invoiceID, account, noteID).Scan(&given); err != nil {
			return 0, 0, err
		}
		return booked, booked - given, nil
	}
	var taxPaise int64
	fields := map[string]string{"2200": "cgst", "2201": "sgst", "2202": "igst"}
	for _, account := range outputGSTAccounts {
		booked, left, err := remaining(account)
		if err != nil {
			return nil, err
		}
		if booked <= 0 || left <= 0 {
			continue
		}
		share := int64(math.Round(float64(booked) * float64(notePaise) / float64(totalPaise)))
		if share > left {
			share = left
		}
		if share <= 0 {
			continue
		}
		debits[account] = share
		taxPaise += share
		if note != nil {
			note[fields[account]] = PaiseToRupees(share)
		}
	}
	if taxPaise >= notePaise {
		return map[string]int64{"4150": notePaise}, nil
	}
	debits["4150"] = notePaise - taxPaise
	if note != nil && taxPaise > 0 {
		note["taxable_amount"] = PaiseToRupees(notePaise - taxPaise)
	}
	return debits, nil
}

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
		return 0, postingError(err, "note not marked Posted") // 73.D.14: a coded refusal keeps its code
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

	// Stage 73 / 55.10: a note that names an open (Approved) SalesInvoice -
	// every cancellation note does - first reduces what that customer still
	// owes (Cr 1300), and only the excess is a cash refund (Cr 1100). Before
	// this the whole note credited cash, so the receivable it was raised to
	// reverse stayed open in 1300 for ever. A note with no invoice, or one
	// against an invoice already Paid, is a refund exactly as before.
	appliedToInvoice := false
	if invoiceID, _ := data["sales_invoice_id"].(string); strings.TrimSpace(invoiceID) != "" {
		applied, refunded, ok, aerr := applyCreditNoteToInvoice(tx, schema, tenantID, noteID, strings.TrimSpace(invoiceID), userID, RupeesToPaise(amountF), data)
		if aerr != nil {
			return 0, aerr
		}
		if ok {
			appliedToInvoice = true
			data["applied_to_invoice"] = PaiseToRupees(applied)
			data["refunded_amount"] = PaiseToRupees(refunded)
		}
	}
	if !appliedToInvoice {
		// 73.D.10: a refund note against a Paid invoice still gives back its GST.
		invoiceID, _ := data["sales_invoice_id"].(string)
		debits, derr := creditNoteDebitsPaise(tx, schema, invoiceID, noteID, RupeesToPaise(amountF), data)
		if derr != nil {
			return 0, derr
		}
		credits := map[string]int64{"1100": RupeesToPaise(amountF)}
		if err := PostDoubleEntry(tenantID, "CreditNote", noteID, debits, credits, "", fmt.Sprintf("CreditNote:%s:POST", noteID)); err != nil {
			return 0, postingError(err, "note not marked Posted")
		}
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
