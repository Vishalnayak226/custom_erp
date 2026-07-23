package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"
)

// BankReconcileResult reports what a reconciliation pass matched, plus
// whatever is left outstanding on each side for a human to chase down.
type BankReconcileResult struct {
	Matched             int      `json:"matched"`
	UnmatchedStatement  []string `json:"unmatched_statement_lines"`
	UnmatchedGLPostings []string `json:"unmatched_gl_postings"`
}

type bankStatementLineRow struct {
	ID          string
	TxnDate     time.Time
	Amount      float64
	DrCr        string
	MatchStatus string
}

// ReconcileBankStatement matches Unmatched BankStatementLine documents for
// one BankAccount against gl_postings on that account's own GL code that
// haven't already been matched (gl_postings.matched_statement_line_id IS
// NULL). Matching is exact-amount, direction-aware (a bank-statement Credit
// - money coming into the account - corresponds to a debit in our own
// books, and vice versa, standard double-entry convention for an asset
// account), within a +/-3 day window of the statement line's txn_date -
// wide enough to absorb bank clearing lag without matching unrelated
// transactions weeks apart. Deliberately not fuzzy on amount: an
// off-by-a-rupee "close enough" match would silently paper over a real
// discrepancy, which is exactly what reconciliation exists to catch.
func ReconcileBankStatement(tenantID, bankAccountID, userID string) (*BankReconcileResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	var bankAccData string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'BankAccount' AND id = $1`, schema), bankAccountID).Scan(&bankAccData); err != nil {
		return nil, fmt.Errorf("bank account not found: %v", err)
	}
	var bankAcc map[string]interface{}
	if err := json.Unmarshal([]byte(bankAccData), &bankAcc); err != nil {
		return nil, err
	}
	glAccountCode, _ := bankAcc["gl_account_code"].(string)
	if glAccountCode == "" {
		// ADMINC-0035 (Stage 25.5): "GL mapping missing" - exact scenario
		// match for a BankAccount with no gl_account_code to reconcile
		// gl_postings against.
		return nil, &ValidationError{Code: "ADMINC-0035", Message: fmt.Sprintf("bank account %s has no gl_account_code configured", bankAccountID)}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}

	// Load every statement line for this bank account not already Matched.
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'BankStatementLine' AND data->>'bank_account' = $1
		  AND COALESCE(data->>'match_status', 'Unmatched') != 'Matched'`, schema), bankAccountID)
	if err != nil {
		return nil, err
	}
	var lines []bankStatementLineRow
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			rows.Close()
			return nil, err
		}
		var d map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
			// 24.18: reading from a nil map wouldn't panic, but silently
			// including a corrupt line as a zero-value txn_date/amount could
			// produce a spurious match (or a spurious non-match) in a
			// financial reconciliation - skip it instead.
			log.Printf("[BANK-RECONCILE] corrupt BankStatementLine %s: %v", id, err)
			continue
		}
		txnDate, _ := time.Parse("2006-01-02", fmt.Sprintf("%v", d["txn_date"]))
		amount := numFromInterface(d["amount"])
		drCr, _ := d["dr_cr"].(string)
		lines = append(lines, bankStatementLineRow{ID: id, TxnDate: txnDate, Amount: amount, DrCr: drCr})
	}
	rows.Close()

	// Load every not-yet-matched gl_posting on this account.
	postingRows, err := tx.Query(fmt.Sprintf(`
		SELECT posting_id, debit, credit, created_at FROM %s.gl_postings
		WHERE account_code = $1 AND matched_statement_line_id IS NULL`, schema), glAccountCode)
	if err != nil {
		return nil, err
	}
	type postingRow struct {
		ID        string
		Debit     int
		Credit    int
		CreatedAt time.Time
	}
	var postings []postingRow
	for postingRows.Next() {
		var p postingRow
		if err := postingRows.Scan(&p.ID, &p.Debit, &p.Credit, &p.CreatedAt); err != nil {
			postingRows.Close()
			return nil, err
		}
		postings = append(postings, p)
	}
	postingRows.Close()

	usedPosting := make(map[int]bool)
	result := &BankReconcileResult{}

	for _, line := range lines {
		matchedIdx := -1
		for i, p := range postings {
			if usedPosting[i] {
				continue
			}
			// Bank statement Credit (money in) <-> our books' debit to the
			// asset account; Debit (money out) <-> our books' credit.
			var bookAmount int
			if line.DrCr == "Credit" {
				bookAmount = p.Debit
			} else {
				bookAmount = p.Credit
			}
			if bookAmount == 0 {
				continue
			}
			if math.Abs(float64(bookAmount)-line.Amount) > 0.01 {
				continue
			}
			if !line.TxnDate.IsZero() {
				diff := p.CreatedAt.Sub(line.TxnDate)
				if diff < -72*time.Hour || diff > 72*time.Hour {
					continue
				}
			}
			matchedIdx = i
			break
		}
		if matchedIdx == -1 {
			result.UnmatchedStatement = append(result.UnmatchedStatement, line.ID)
			continue
		}
		usedPosting[matchedIdx] = true
		p := postings[matchedIdx]

		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.gl_postings SET matched_statement_line_id = $1 WHERE posting_id = $2`, schema),
			line.ID, p.ID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = jsonb_set(data, '{match_status}', '"Matched"'), updated_at = CURRENT_TIMESTAMP
			 WHERE doctype = 'BankStatementLine' AND id = $1`, schema), line.ID); err != nil {
			return nil, err
		}
		result.Matched++
	}

	for i, p := range postings {
		if !usedPosting[i] {
			result.UnmatchedGLPostings = append(result.UnmatchedGLPostings, p.ID)
		}
	}
	if result.UnmatchedStatement == nil {
		result.UnmatchedStatement = []string{}
	}
	if result.UnmatchedGLPostings == nil {
		result.UnmatchedGLPostings = []string{}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "BANK_RECONCILE", "SUCCESS",
		fmt.Sprintf("Reconciled bank account %s: %d matched, %d statement lines and %d GL postings still outstanding",
			bankAccountID, result.Matched, len(result.UnmatchedStatement), len(result.UnmatchedGLPostings)))
	return result, nil
}
