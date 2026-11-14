package engines

import (
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"custom_erp/db"
)

// Stage 170.4: the bank's acknowledgement of a payment file.
//
// The bank answers a payment file with a CSV: payment reference (the invoice id
// the file carried), UTR, status and reason. A paid row records the UTR
// (RecordPaymentUTR); a rejected row is kept with its reason and raises a bank
// exception for someone to deal with. Nothing is reversed on a rejection - the
// ledger is left as it is until a person decides. A reference the proposal does
// not hold is listed, not fatal; the same answer sent twice is skipped.

// PaymentAckRow is what happened to one row of the bank's file.
type PaymentAckRow struct {
	Row       int    `json:"row"`
	Reference string `json:"payment_reference"`
	Status    string `json:"status"` // Paid | Rejected
	UTR       string `json:"utr,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Result    string `json:"result"` // Recorded | Skipped | Unknown reference | Error
	Detail    string `json:"detail,omitempty"`
	Exception string `json:"exception_line_id,omitempty"`
}

// PaymentAckResult is the whole file's outcome.
type PaymentAckResult struct {
	Paid     int             `json:"paid"`
	Rejected int             `json:"rejected"`
	Skipped  int             `json:"skipped"`
	Unknown  int             `json:"unknown"`
	Errors   int             `json:"errors"`
	Rows     []PaymentAckRow `json:"rows"`
}

func ackStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "paid", "success", "successful", "processed", "credited", "completed", "settled":
		return "Paid"
	case "rejected", "failed", "failure", "returned", "declined", "reversed":
		return "Rejected"
	}
	return ""
}

// ImportPaymentBankResponse reads the bank's response CSV for an Executed
// payment proposal. bankAccountID is the account the exception lines of
// rejected payments are filed on.
func ImportPaymentBankResponse(tenantID, proposalID, bankAccountID, content, actor string) (*PaymentAckResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr, status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'PaymentProposal' AND id = $1`, schema), proposalID).Scan(&dataStr, &status); err != nil {
		return nil, importErr("FIN-1706", "payment proposal not found")
	}
	if status != "Executed" {
		return nil, importErr("FIN-1706", fmt.Sprintf("a bank response can only be loaded for an Executed proposal (this one is %s)", status))
	}
	if _, err := bankAccountGLAccount(schema, bankAccountID); err != nil {
		return nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, fmt.Errorf("proposal %s has corrupt stored data: %v", proposalID, err)
	}
	raw, _ := json.Marshal(data["execution_results"])
	var results []PaymentProposalResultLine
	_ = json.Unmarshal(raw, &results)
	known := map[string]PaymentProposalResultLine{}
	for _, r := range results {
		if r.Paid {
			known[r.InvoiceID] = r
		}
	}

	r := csv.NewReader(strings.NewReader(content))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		return nil, importErr("FIN-1706", "the file is empty or not a CSV")
	}
	idx := csvHeaderIndex(header)
	refCol := -1
	for _, n := range []string{"payment_reference", "reference", "invoice_id", "payment_ref"} {
		if i, ok := idx[n]; ok {
			refCol = i
			break
		}
	}
	if refCol < 0 {
		return nil, importErr("FIN-1707", "the file needs a payment_reference column")
	}
	if _, ok := idx["status"]; !ok {
		return nil, importErr("FIN-1707", "the file needs a status column")
	}

	res := &PaymentAckResult{Rows: []PaymentAckRow{}}
	row := 1
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		row++
		if err != nil {
			res.Errors++
			res.Rows = append(res.Rows, PaymentAckRow{Row: row, Result: "Error", Detail: "unreadable row"})
			continue
		}
		if len(strings.Join(rec, "")) == 0 {
			continue
		}
		ref := ""
		if refCol < len(rec) {
			ref = strings.TrimSpace(rec[refCol])
		}
		out := PaymentAckRow{Row: row, Reference: ref, UTR: pickCol(idx, rec, "utr", "utr_number", "bank_reference"),
			Reason: pickCol(idx, rec, "reason", "remarks", "failure_reason")}
		out.Status = ackStatus(pickCol(idx, rec, "status"))
		switch {
		case ref == "":
			out.Result, out.Detail = "Error", "no payment reference"
		case out.Status == "":
			out.Result, out.Detail = "Error", "status not understood"
		default:
			ackOne(tenantID, schema, proposalID, bankAccountID, actor, known, &out)
		}
		switch out.Result {
		case "Recorded":
			if out.Status == "Paid" {
				res.Paid++
			} else {
				res.Rejected++
			}
		case "Skipped":
			res.Skipped++
		case "Unknown reference":
			res.Unknown++
		default:
			res.Errors++
		}
		res.Rows = append(res.Rows, out)
	}
	if len(res.Rows) == 0 {
		return nil, importErr("FIN-1706", "the file has no rows")
	}
	LogAuditEvent(tenantID, actor, "PAYMENT_BANK_RESPONSE", "SUCCESS", fmt.Sprintf("Bank response for %s: %d paid, %d rejected, %d skipped, %d unknown, %d errors", proposalID, res.Paid, res.Rejected, res.Skipped, res.Unknown, res.Errors))
	return res, nil
}

func ackOne(tenantID, schema, proposalID, bankAccountID, actor string, known map[string]PaymentProposalResultLine, out *PaymentAckRow) {
	pay, ok := known[out.Reference]
	if !ok {
		out.Result, out.Detail = "Unknown reference", "this proposal has no paid line with that reference"
		return
	}
	var prev, prevUTR sql.NullString
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT status, utr FROM %s.payment_bank_response WHERE proposal_id = $1 AND invoice_id = $2`, schema), proposalID, out.Reference).Scan(&prev, &prevUTR)
	if err == nil {
		if prev.String == out.Status {
			out.Result, out.Detail = "Skipped", "already recorded"
		} else {
			out.Result, out.Detail = "Error", fmt.Sprintf("already recorded as %s - the bank now says %s; settle it by hand", prev.String, out.Status)
		}
		return
	}
	if err != sql.ErrNoRows {
		out.Result, out.Detail = "Error", err.Error()
		return
	}
	if out.Status == "Paid" {
		if strings.TrimSpace(out.UTR) == "" {
			out.Result, out.Detail = "Error", "a paid row needs a UTR"
			return
		}
		if err := RecordPaymentUTR(tenantID, proposalID, out.Reference, out.UTR); err != nil {
			out.Result, out.Detail = "Error", err.Error()
			return
		}
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.payment_bank_response (proposal_id, invoice_id, status, utr, recorded_by) VALUES ($1, $2, 'Paid', $3, $4)`, schema),
			proposalID, out.Reference, out.UTR, actor); err != nil {
			out.Result, out.Detail = "Error", err.Error()
			return
		}
		out.Result = "Recorded"
		return
	}
	// Rejected: keep the reason and put a line in the exception queue. The
	// ledger is not touched.
	reason := strings.TrimSpace(out.Reason)
	if reason == "" {
		reason = "rejected by the bank"
	}
	sum := sha256.Sum256([]byte(proposalID + "|" + out.Reference))
	lineID := "BSL-REJ-" + strings.ToUpper(hex.EncodeToString(sum[:6]))
	amount := PaiseToRupees(pay.AmountPaise)
	if pay.AmountPaise == 0 {
		amount = float64(pay.Amount)
	}
	stub := map[string]interface{}{
		"bank_account": bankAccountID, "txn_date": BusinessToday(), "description": "Payment rejected by bank: " + reason,
		"reference_number": out.Reference, "amount": amount, "dr_cr": "Debit", "match_status": "Unmatched", "payment_proposal": proposalID,
	}
	b, _ := json.Marshal(stub)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'BankStatementLine', $2, 'Active', $3) ON CONFLICT (id) DO NOTHING`, schema),
		lineID, string(b), actor); err != nil {
		out.Result, out.Detail = "Error", err.Error()
		return
	}
	if err := RaiseBankException(tenantID, lineID, actor, fmt.Sprintf("Payment %s in %s rejected: %s", out.Reference, proposalID, reason), actor); err != nil {
		out.Result, out.Detail = "Error", err.Error()
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.payment_bank_response (proposal_id, invoice_id, status, reason, exception_line_id, recorded_by) VALUES ($1, $2, 'Rejected', $3, $4, $5)`, schema),
		proposalID, out.Reference, reason, lineID, actor); err != nil {
		out.Result, out.Detail = "Error", err.Error()
		return
	}
	out.Result, out.Exception, out.Reason = "Recorded", lineID, reason
}
