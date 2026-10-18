package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stage 73.2.16: the bank reconciliation workbench.
//
// ReconcileBankStatement (bank_reconciliation.go) stays the one-click exact
// pass. Around it:
//
//   - MatchBankLine: a person matches one statement line to one or more GL
//     postings on the bank's GL account. The postings must sum to the line,
//     direction-aware (a statement Credit is a debit in our books). A
//     difference is refused here - post the bank charge or correction first,
//     then match - so a manual match never hides money.
//   - RuleMatchBankStatement: amount +/- a tolerance, a date window and the
//     statement reference. It matches only when exactly one candidate fits;
//     an ambiguous line is left for a person and reported as ambiguous. A
//     tolerance match records its difference on the line, so it stays
//     visible until a correcting entry is posted.
//   - The exception queue: an unmatched line gets an owner and a reason
//     (Exception), and is cleared by a match or - Finance Head / Admin only,
//     with a note - Explained, for a line nothing in the books will ever
//     match (e.g. a bank error the bank will reverse).
//   - Payouts: a marketplace settlement batch (its lines each posted their
//     net payout to 1100) matches one bank credit as a group; a card / UPI
//     acquirer payout clears 1101 / 1102 to the bank, with the acquirer's
//     fee to 5230, and that new bank posting is matched to the line.
//
// A statement line dated inside a closed accounting period is frozen: its
// reconciliation is part of that period's signed close (73.1.10).

type BankStatementLineView struct {
	ID              string            `json:"id"`
	TxnDate         string            `json:"txn_date"`
	Description     string            `json:"description"`
	Reference       string            `json:"reference_number"`
	Amount          float64           `json:"amount"`
	DrCr            string            `json:"dr_cr"`
	MatchStatus     string            `json:"match_status"`
	MatchMethod     string            `json:"match_method"`
	MatchedBy       string            `json:"matched_by"`
	MatchedAt       string            `json:"matched_at"`
	MatchDifference float64           `json:"match_difference"`
	ExceptionOwner  string            `json:"exception_owner"`
	ExceptionReason string            `json:"exception_reason"`
	ExceptionAge    int               `json:"exception_age_days"`
	ResolutionNote  string            `json:"resolution_note"`
	MatchedPostings []BankBookPosting `json:"matched_postings"`
}

type BankBookPosting struct {
	PostingID    string  `json:"posting_id"`
	Date         string  `json:"date"`
	DocumentType string  `json:"document_type"`
	DocumentID   string  `json:"document_id"`
	Debit        float64 `json:"debit"`
	Credit       float64 `json:"credit"`
	PostedBy     string  `json:"posted_by"`
	AmountDiff   float64 `json:"amount_difference,omitempty"`
	DaysApart    int     `json:"days_apart,omitempty"`
}

type MarketplacePayoutSuggestion struct {
	BatchID   string  `json:"settlement_batch_id"`
	Channel   string  `json:"channel"`
	Lines     int     `json:"lines"`
	NetPayout float64 `json:"net_payout"`
	Date      string  `json:"settlement_date"`
}

type BankReconWorkbench struct {
	BankAccountID    string                        `json:"bank_account_id"`
	GLAccount        string                        `json:"gl_account_code"`
	From             string                        `json:"from"`
	To               string                        `json:"to"`
	Lines            []BankStatementLineView       `json:"lines"`
	UnmatchedBook    []BankBookPosting             `json:"unmatched_book"`
	MarketplaceBatch []MarketplacePayoutSuggestion `json:"marketplace_payouts"`
	CardClearing     float64                       `json:"card_clearing_balance"`
	UPIClearing      float64                       `json:"upi_clearing_balance"`
	Counts           map[string]int                `json:"counts"`
	BookBalance      float64                       `json:"book_balance"`
	OpenDifferences  float64                       `json:"open_differences"`
}

type bankLineState struct {
	schema, glAccount, bankAccount string
	data                           map[string]interface{}
	amountPaise                    int64
	drCr, status, txnDate          string
}

func bankAccountGLAccount(schema, bankAccountID string) (string, error) {
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'BankAccount' AND id = $1`, schema), bankAccountID).Scan(&dataStr); err != nil {
		return "", &ValidationError{Code: "FIN-0510", Message: "bank account not found"}
	}
	var acc map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &acc); err != nil {
		return "", err
	}
	code, _ := acc["gl_account_code"].(string)
	if code == "" {
		return "", &ValidationError{Code: "ADMINC-0035", Message: fmt.Sprintf("bank account %s has no gl_account_code configured", bankAccountID)}
	}
	return code, nil
}

// lockBankLine locks one statement line and its bank account's GL code, and
// refuses a line dated in a closed period.
func lockBankLine(tx *sql.Tx, schema, lineID string) (*bankLineState, error) {
	var dataStr string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'BankStatementLine' AND id = $1 FOR UPDATE`, schema), lineID).Scan(&dataStr); err != nil {
		return nil, &ValidationError{Code: "FIN-0510", Message: "bank statement line not found"}
	}
	s := &bankLineState{schema: schema}
	if err := json.Unmarshal([]byte(dataStr), &s.data); err != nil {
		return nil, fmt.Errorf("statement line %s is unreadable: %v", lineID, err)
	}
	s.bankAccount, _ = s.data["bank_account"].(string)
	s.drCr, _ = s.data["dr_cr"].(string)
	s.status, _ = s.data["match_status"].(string)
	if s.status == "" {
		s.status = "Unmatched"
	}
	s.txnDate = fmt.Sprintf("%v", s.data["txn_date"])
	s.amountPaise = RupeesToPaise(numFromInterface(s.data["amount"]))
	code, err := bankAccountGLAccount(schema, s.bankAccount)
	if err != nil {
		return nil, err
	}
	s.glAccount = code
	var closed string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT period_name FROM %s.accounting_periods WHERE status = 'Closed' AND $1::date BETWEEN start_date AND end_date LIMIT 1`, schema),
		s.txnDate).Scan(&closed); err == nil {
		return nil, &ValidationError{Code: "FIN-0511", Message: fmt.Sprintf("this line is dated in closed period '%s' - its reconciliation is part of that signed close; request a reopen first", closed)}
	}
	return s, nil
}

func patchBankLine(tx *sql.Tx, schema, lineID string, patch map[string]interface{}) error {
	b, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'BankStatementLine' AND id = $2`, schema), string(b), lineID)
	return err
}

func matchedPatch(method, actor string, differencePaise int64) map[string]interface{} {
	return map[string]interface{}{
		"match_status": "Matched", "match_method": method, "matched_by": actor,
		"matched_at": time.Now().Format("2006-01-02 15:04:05"), "match_difference": PaiseToRupees(differencePaise),
	}
}

// bookSidePaise is the side of a bank-GL posting that a statement line of
// direction drCr corresponds to.
func bookSidePaise(drCr string, debit, credit int64) int64 {
	if drCr == "Credit" {
		return debit
	}
	return credit
}

// MatchBankLine matches a line to postings that sum to it exactly.
func MatchBankLine(tenantID, lineID string, postingIDs []string, note, actor string) error {
	if len(postingIDs) == 0 {
		return &ValidationError{Code: "FIN-0512", Message: "choose at least one book entry to match this line to"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	line, err := lockBankLine(tx, schema, lineID)
	if err != nil {
		return err
	}
	if line.status == "Matched" || line.status == "Explained" {
		return &ValidationError{Code: "FIN-0512", Message: fmt.Sprintf("this line is already %s - unmatch it first", line.status)}
	}
	var total int64
	for _, pid := range postingIDs {
		var account string
		var debit, credit int64
		var matched sql.NullString
		if err := tx.QueryRow(fmt.Sprintf(`SELECT account_code, debit, credit, matched_statement_line_id FROM %s.gl_postings WHERE posting_id = $1 FOR UPDATE`, schema),
			pid).Scan(&account, &debit, &credit, &matched); err != nil {
			return &ValidationError{Code: "FIN-0512", Message: fmt.Sprintf("book entry %s not found", pid)}
		}
		if account != line.glAccount {
			return &ValidationError{Code: "FIN-0512", Message: fmt.Sprintf("book entry %s is on account %s, not this bank's %s", pid, account, line.glAccount)}
		}
		if matched.Valid && matched.String != "" {
			return &ValidationError{Code: "FIN-0512", Message: fmt.Sprintf("book entry %s is already matched to statement line %s", pid, matched.String)}
		}
		side := bookSidePaise(line.drCr, debit, credit)
		if side == 0 {
			return &ValidationError{Code: "FIN-0512", Message: fmt.Sprintf("book entry %s is on the wrong side: a statement %s is a %s in the books", pid, line.drCr, map[string]string{"Credit": "debit", "Debit": "credit"}[line.drCr])}
		}
		total += side
	}
	if total != line.amountPaise {
		return &ValidationError{Code: "FIN-0513", Message: fmt.Sprintf("the chosen entries total %.2f but the statement line is %.2f (difference %.2f) - post the bank charge or correction first, then match",
			PaiseToRupees(total), PaiseToRupees(line.amountPaise), PaiseToRupees(line.amountPaise-total))}
	}
	for _, pid := range postingIDs {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.gl_postings SET matched_statement_line_id = $1 WHERE posting_id = $2`, schema), lineID, pid); err != nil {
			return err
		}
	}
	patch := matchedPatch("manual", actor, 0)
	if strings.TrimSpace(note) != "" {
		patch["resolution_note"] = strings.TrimSpace(note)
	}
	if err := patchBankLine(tx, schema, lineID, patch); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "BANK_LINE_MATCHED", "SUCCESS", fmt.Sprintf("Statement line %s matched manually to %d book entr(ies)", lineID, len(postingIDs)))
	return nil
}

// UnmatchBankLine undoes a match (any method) with a reason. A POS payout's
// own posting stays - it is real money movement; only the pairing is undone.
func UnmatchBankLine(tenantID, lineID, reason, actor string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return &ValidationError{Code: "FIN-0512", Message: "say why this match is being undone"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	line, err := lockBankLine(tx, schema, lineID)
	if err != nil {
		return err
	}
	if line.status != "Matched" && line.status != "Explained" {
		return &ValidationError{Code: "FIN-0512", Message: "this line is not matched"}
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.gl_postings SET matched_statement_line_id = NULL WHERE matched_statement_line_id = $1`, schema), lineID); err != nil {
		return err
	}
	if err := patchBankLine(tx, schema, lineID, map[string]interface{}{
		"match_status": "Unmatched", "match_method": "", "matched_by": "", "matched_at": "", "match_difference": 0,
		"resolution_note": "unmatched by " + actor + ": " + reason,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "BANK_LINE_UNMATCHED", "SUCCESS", fmt.Sprintf("Statement line %s unmatched: %s", lineID, reason))
	return nil
}

// BankMatchRule is one rule pass's settings.
type BankMatchRule struct {
	AmountTolerance float64 `json:"amount_tolerance"`
	DateWindowDays  int     `json:"date_window_days"`
	UseReference    bool    `json:"use_reference"`
}

type BankRuleMatchResult struct {
	Matched        int      `json:"matched"`
	WithDifference int      `json:"matched_with_difference"`
	Ambiguous      []string `json:"ambiguous_lines"`
	NoCandidate    []string `json:"no_candidate_lines"`
	Skipped        []string `json:"skipped_lines"`
}

// RuleMatchBankStatement runs the rule pass over one bank account's open
// lines (Unmatched or Exception, not in a closed period).
func RuleMatchBankStatement(tenantID, bankAccountID string, rule BankMatchRule, actor string) (*BankRuleMatchResult, error) {
	if rule.AmountTolerance < 0 || rule.AmountTolerance > 10000 {
		return nil, &ValidationError{Code: "FIN-0514", Message: "amount tolerance must be between 0 and 10,000"}
	}
	if rule.DateWindowDays < 0 || rule.DateWindowDays > 31 {
		return nil, &ValidationError{Code: "FIN-0514", Message: "date window must be between 0 and 31 days"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	glAccount, err := bankAccountGLAccount(schema, bankAccountID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	type lineRow struct {
		id, drCr, ref string
		date          time.Time
		amount        int64
	}
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT d.id, d.data FROM %s.documents d
		WHERE d.doctype = 'BankStatementLine' AND d.data->>'bank_account' = $1
		  AND COALESCE(d.data->>'match_status', 'Unmatched') IN ('Unmatched', 'Exception')
		  AND NOT EXISTS (SELECT 1 FROM %s.accounting_periods p WHERE p.status = 'Closed'
		                  AND (d.data->>'txn_date')::date BETWEEN p.start_date AND p.end_date)
		ORDER BY d.data->>'txn_date', d.id FOR UPDATE OF d`, schema, schema), bankAccountID)
	if err != nil {
		return nil, err
	}
	var lines []lineRow
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			rows.Close()
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(dataStr), &d) != nil {
			continue
		}
		l := lineRow{id: id, amount: RupeesToPaise(numFromInterface(d["amount"]))}
		l.drCr, _ = d["dr_cr"].(string)
		l.ref, _ = d["reference_number"].(string)
		l.date, _ = time.ParseInLocation("2006-01-02", fmt.Sprintf("%v", d["txn_date"]), time.Local)
		lines = append(lines, l)
	}
	rows.Close()

	type bookRow struct {
		id, docID     string
		debit, credit int64
		at            time.Time
	}
	postRows, err := tx.Query(fmt.Sprintf(`
		SELECT posting_id, document_id, debit, credit, created_at FROM %s.gl_postings
		WHERE account_code = $1 AND matched_statement_line_id IS NULL FOR UPDATE`, schema), glAccount)
	if err != nil {
		return nil, err
	}
	var book []bookRow
	for postRows.Next() {
		var b bookRow
		if err := postRows.Scan(&b.id, &b.docID, &b.debit, &b.credit, &b.at); err != nil {
			postRows.Close()
			return nil, err
		}
		book = append(book, b)
	}
	postRows.Close()

	tolerance := RupeesToPaise(rule.AmountTolerance)
	used := map[int]bool{}
	res := &BankRuleMatchResult{Ambiguous: []string{}, NoCandidate: []string{}, Skipped: []string{}}
	for _, l := range lines {
		if l.drCr != "Credit" && l.drCr != "Debit" {
			res.Skipped = append(res.Skipped, l.id)
			continue
		}
		var cands []int
		for i, b := range book {
			if used[i] {
				continue
			}
			side := bookSidePaise(l.drCr, b.debit, b.credit)
			if side == 0 || absInt64(side-l.amount) > tolerance {
				continue
			}
			if !l.date.IsZero() {
				// Whole calendar days between the posting's date and the
				// statement date (created_at is wall-clock server time).
				postDay := time.Date(b.at.Year(), b.at.Month(), b.at.Day(), 0, 0, 0, 0, time.Local)
				if int(math.Round(math.Abs(postDay.Sub(l.date).Hours()/24))) > rule.DateWindowDays {
					continue
				}
			}
			cands = append(cands, i)
		}
		pick, method := -1, ""
		if rule.UseReference && strings.TrimSpace(l.ref) != "" {
			ref := strings.ToLower(strings.TrimSpace(l.ref))
			var byRef []int
			for _, i := range cands {
				doc := strings.ToLower(book[i].docID)
				if strings.Contains(doc, ref) || strings.Contains(ref, doc) {
					byRef = append(byRef, i)
				}
			}
			if len(byRef) == 1 {
				pick, method = byRef[0], "rule:reference"
			}
		}
		if pick < 0 && len(cands) == 1 {
			pick, method = cands[0], "rule:amount-date"
		}
		if pick < 0 && len(cands) > 1 {
			var exact []int
			for _, i := range cands {
				if bookSidePaise(l.drCr, book[i].debit, book[i].credit) == l.amount {
					exact = append(exact, i)
				}
			}
			if len(exact) == 1 {
				pick, method = exact[0], "rule:exact-amount"
			}
		}
		if pick < 0 {
			if len(cands) == 0 {
				res.NoCandidate = append(res.NoCandidate, l.id)
			} else {
				res.Ambiguous = append(res.Ambiguous, fmt.Sprintf("%s (%d candidates)", l.id, len(cands)))
			}
			continue
		}
		used[pick] = true
		b := book[pick]
		diff := l.amount - bookSidePaise(l.drCr, b.debit, b.credit)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.gl_postings SET matched_statement_line_id = $1 WHERE posting_id = $2`, schema), l.id, b.id); err != nil {
			return nil, err
		}
		if err := patchBankLine(tx, schema, l.id, matchedPatch(method, actor, diff)); err != nil {
			return nil, err
		}
		res.Matched++
		if diff != 0 {
			res.WithDifference++
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, actor, "BANK_RULE_MATCH", "SUCCESS", fmt.Sprintf("Rule match on bank account %s (tolerance %.2f, window %d days, reference %v): %d matched (%d with a difference), %d ambiguous, %d without a candidate",
		bankAccountID, rule.AmountTolerance, rule.DateWindowDays, rule.UseReference, res.Matched, res.WithDifference, len(res.Ambiguous), len(res.NoCandidate)))
	return res, nil
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// RaiseBankException puts a line in the exception queue with an owner.
func RaiseBankException(tenantID, lineID, owner, reason, actor string) error {
	owner, reason = strings.TrimSpace(owner), strings.TrimSpace(reason)
	if owner == "" || reason == "" {
		return &ValidationError{Code: "FIN-0515", Message: "an exception needs an owner and a reason"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var active bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.users WHERE username = $1 AND COALESCE(status, 'Active') = 'Active')`, schema), owner).Scan(&active); err != nil {
		return err
	}
	if !active {
		return &ValidationError{Code: "FIN-0515", Message: fmt.Sprintf("'%s' is not an active user", owner)}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	line, err := lockBankLine(tx, schema, lineID)
	if err != nil {
		return err
	}
	if line.status == "Matched" || line.status == "Explained" {
		return &ValidationError{Code: "FIN-0515", Message: fmt.Sprintf("this line is already %s", line.status)}
	}
	if err := patchBankLine(tx, schema, lineID, map[string]interface{}{
		"match_status": "Exception", "exception_owner": owner, "exception_reason": reason,
		"exception_raised_at": time.Now().Format("2006-01-02"), "exception_raised_by": actor,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "BANK_EXCEPTION_RAISED", "SUCCESS", fmt.Sprintf("Statement line %s raised as an exception, owner %s: %s", lineID, owner, reason))
	return nil
}

// ExplainBankException closes an exception that nothing in the books will
// match. Finance Head / Admin only, with a note; it stays reported.
func ExplainBankException(tenantID, lineID, note, actor, actorRole string) error {
	if !CanManagePeriodClose(actorRole) {
		return &ValidationError{Code: "FIN-0504", Message: "only an Accountant or a Super Admin can close an exception as explained"}
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) < 10 {
		return &ValidationError{Code: "FIN-0515", Message: "explain why nothing in the books will match this line (at least 10 characters)"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	line, err := lockBankLine(tx, schema, lineID)
	if err != nil {
		return err
	}
	if line.status != "Exception" {
		return &ValidationError{Code: "FIN-0515", Message: "only a line in the exception queue can be explained - raise it first, with its owner and reason"}
	}
	patch := matchedPatch("explained", actor, line.amountPaise)
	patch["match_status"] = "Explained"
	patch["resolution_note"] = note
	if err := patchBankLine(tx, schema, lineID, patch); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "BANK_EXCEPTION_EXPLAINED", "SUCCESS", fmt.Sprintf("Statement line %s closed as explained: %s", lineID, note))
	return nil
}

// marketplaceBatchPostings are the unmatched bank-side payout postings of
// one marketplace settlement batch.
func marketplaceBatchPostings(q interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}, schema, glAccount, batchID string) ([]string, int64, error) {
	rows, err := q.Query(fmt.Sprintf(`
		SELECT p.posting_id, p.debit FROM %s.gl_postings p
		JOIN %s.documents d ON d.doctype = 'MarketplaceSettlementLine' AND d.id = p.document_id
		WHERE p.document_type = 'MarketplaceSettlementLine' AND p.account_code = $1 AND p.debit > 0
		  AND p.matched_statement_line_id IS NULL AND d.data->>'settlement_batch_id' = $2`, schema, schema), glAccount, batchID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var ids []string
	var total int64
	for rows.Next() {
		var id string
		var debit int64
		if err := rows.Scan(&id, &debit); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
		total += debit
	}
	return ids, total, rows.Err()
}

// MatchMarketplacePayout matches a whole settlement batch's net payout to one
// bank credit line.
func MatchMarketplacePayout(tenantID, lineID, batchID, actor string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	line, err := lockBankLine(tx, schema, lineID)
	if err != nil {
		return err
	}
	if line.status == "Matched" || line.status == "Explained" {
		return &ValidationError{Code: "FIN-0512", Message: fmt.Sprintf("this line is already %s", line.status)}
	}
	if line.drCr != "Credit" {
		return &ValidationError{Code: "FIN-0516", Message: "a marketplace payout is money in - match it to a Credit line"}
	}
	ids, total, err := marketplaceBatchPostings(tx, schema, line.glAccount, strings.TrimSpace(batchID))
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return &ValidationError{Code: "FIN-0516", Message: fmt.Sprintf("settlement batch %s has no unmatched payout posted to account %s - reconcile the settlement file first (OMS > Settlement), or check it posted to this bank's GL account", batchID, line.glAccount)}
	}
	if total != line.amountPaise {
		return &ValidationError{Code: "FIN-0513", Message: fmt.Sprintf("batch %s paid out %.2f across %d line(s) but the bank credit is %.2f (difference %.2f)",
			batchID, PaiseToRupees(total), len(ids), PaiseToRupees(line.amountPaise), PaiseToRupees(line.amountPaise-total))}
	}
	for _, id := range ids {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.gl_postings SET matched_statement_line_id = $1 WHERE posting_id = $2`, schema), lineID, id); err != nil {
			return err
		}
	}
	if err := patchBankLine(tx, schema, lineID, matchedPatch("payout:marketplace:"+batchID, actor, 0)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "BANK_PAYOUT_MATCHED", "SUCCESS", fmt.Sprintf("Statement line %s matched to marketplace settlement batch %s (%d payout lines)", lineID, batchID, len(ids)))
	return nil
}

// MatchPOSPayout books a card / UPI acquirer payout from its clearing account
// (1101 card, 1102 UPI) to the bank and matches it to the bank credit:
// Dr bank GL (amount received) + Dr 5230 (acquirer fee) / Cr clearing.
func MatchPOSPayout(tenantID, lineID, clearingAccount string, fee float64, actor string) error {
	if clearingAccount != "1101" && clearingAccount != "1102" {
		return &ValidationError{Code: "FIN-0516", Message: "choose the card (1101) or UPI (1102) clearing account"}
	}
	if fee < 0 {
		return &ValidationError{Code: "FIN-0516", Message: "the acquirer fee cannot be negative"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	// Validate under the line lock, then post (PostDoubleEntry runs its own
	// transaction), then pair. The posting key makes a retry after a failed
	// pairing find the same posting instead of booking the payout twice.
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	line, err := lockBankLine(tx, schema, lineID)
	if err != nil {
		tx.Rollback()
		return err
	}
	if line.status == "Matched" || line.status == "Explained" {
		tx.Rollback()
		return &ValidationError{Code: "FIN-0512", Message: fmt.Sprintf("this line is already %s", line.status)}
	}
	if line.drCr != "Credit" {
		tx.Rollback()
		return &ValidationError{Code: "FIN-0516", Message: "a card / UPI payout is money in - match it to a Credit line"}
	}
	key := fmt.Sprintf("BankStatementLine:%s:POS_PAYOUT", lineID)
	var alreadyPosted bool
	if err := tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.gl_postings WHERE idempotency_key = $1)`, schema), key).Scan(&alreadyPosted); err != nil {
		tx.Rollback()
		return err
	}
	feePaise := RupeesToPaise(fee)
	if !alreadyPosted {
		var clearingPaise int64
		if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(debit), 0) - COALESCE(SUM(credit), 0) FROM %s.gl_postings WHERE account_code = $1`, schema),
			clearingAccount).Scan(&clearingPaise); err != nil {
			tx.Rollback()
			return err
		}
		if line.amountPaise+feePaise > clearingPaise {
			tx.Rollback()
			return &ValidationError{Code: "FIN-0513", Message: fmt.Sprintf("the payout plus fee (%.2f) is more than the %.2f still in clearing account %s - check the date range or the fee",
				PaiseToRupees(line.amountPaise+feePaise), PaiseToRupees(clearingPaise), clearingAccount)}
		}
	}
	tx.Rollback()

	debits := map[string]int64{line.glAccount: line.amountPaise}
	if feePaise > 0 {
		debits["5230"] = feePaise
	}
	credits := map[string]int64{clearingAccount: line.amountPaise + feePaise}
	if err := PostDoubleEntry(tenantID, "BankStatementLine", lineID, debits, credits, line.txnDate, key); err != nil {
		return err
	}

	tx2, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx2.Rollback()
	if _, err := lockBankLine(tx2, schema, lineID); err != nil {
		return err
	}
	var postingID string
	if err := tx2.QueryRow(fmt.Sprintf(`SELECT posting_id FROM %s.gl_postings WHERE idempotency_key = $1 AND account_code = $2 AND debit > 0 LIMIT 1`, schema),
		key, line.glAccount).Scan(&postingID); err != nil {
		return fmt.Errorf("payout posted but its bank entry was not found: %v", err)
	}
	if _, err := tx2.Exec(fmt.Sprintf(`UPDATE %s.gl_postings SET matched_statement_line_id = $1 WHERE posting_id = $2`, schema), lineID, postingID); err != nil {
		return err
	}
	method := map[string]string{"1101": "payout:card", "1102": "payout:upi"}[clearingAccount]
	patch := matchedPatch(method, actor, 0)
	patch["payout_fee"] = fee
	if err := patchBankLine(tx2, schema, lineID, patch); err != nil {
		return err
	}
	if err := tx2.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "BANK_PAYOUT_MATCHED", "SUCCESS", fmt.Sprintf("Statement line %s booked as a %s payout from %s (fee %.2f) and matched", lineID, method, clearingAccount, fee))
	return nil
}

// BankLineCandidates lists unmatched book entries a line could match, best
// first: exact amount, then nearest amount, then nearest date.
func BankLineCandidates(tenantID, lineID string) ([]BankBookPosting, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'BankStatementLine' AND id = $1`, schema), lineID).Scan(&dataStr); err != nil {
		return nil, &ValidationError{Code: "FIN-0510", Message: "bank statement line not found"}
	}
	var d map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
		return nil, err
	}
	bankAccount, _ := d["bank_account"].(string)
	glAccount, err := bankAccountGLAccount(schema, bankAccount)
	if err != nil {
		return nil, err
	}
	drCr, _ := d["dr_cr"].(string)
	amount := RupeesToPaise(numFromInterface(d["amount"]))
	txnDate, _ := time.ParseInLocation("2006-01-02", fmt.Sprintf("%v", d["txn_date"]), time.Local)
	book, err := unmatchedBookPostings(schema, glAccount, 500)
	if err != nil {
		return nil, err
	}
	var out []BankBookPosting
	for _, b := range book {
		side := RupeesToPaise(b.Debit)
		if drCr != "Credit" {
			side = RupeesToPaise(b.Credit)
		}
		if side == 0 {
			continue
		}
		b.AmountDiff = PaiseToRupees(amount - side)
		if len(b.Date) < 10 {
			continue
		}
		if at, err := time.ParseInLocation("2006-01-02", b.Date[:10], time.Local); err == nil && !txnDate.IsZero() {
			b.DaysApart = int(math.Abs(at.Sub(txnDate).Hours() / 24))
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := math.Abs(out[i].AmountDiff), math.Abs(out[j].AmountDiff)
		if ai != aj {
			return ai < aj
		}
		return out[i].DaysApart < out[j].DaysApart
	})
	if len(out) > 20 {
		out = out[:20]
	}
	if out == nil {
		out = []BankBookPosting{}
	}
	return out, nil
}

func unmatchedBookPostings(schema, glAccount string, limit int) ([]BankBookPosting, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT posting_id, created_at::text, document_type, document_id, debit, credit, COALESCE(posted_by, '')
		FROM %s.gl_postings WHERE account_code = $1 AND matched_statement_line_id IS NULL
		ORDER BY created_at DESC LIMIT $2`, schema), glAccount, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BankBookPosting
	for rows.Next() {
		var b BankBookPosting
		var debit, credit int64
		if err := rows.Scan(&b.PostingID, &b.Date, &b.DocumentType, &b.DocumentID, &debit, &credit, &b.PostedBy); err != nil {
			return nil, err
		}
		b.Debit, b.Credit = PaiseToRupees(debit), PaiseToRupees(credit)
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetBankReconciliationWorkbench is the screen: statement lines in the date
// range with their state, the unmatched book side, and payout suggestions.
func GetBankReconciliationWorkbench(tenantID, bankAccountID, from, to string) (*BankReconWorkbench, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	glAccount, err := bankAccountGLAccount(schema, bankAccountID)
	if err != nil {
		return nil, err
	}
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}
	if from == "" {
		if t, err := time.Parse("2006-01-02", to); err == nil {
			from = t.AddDate(0, -3, 0).Format("2006-01-02")
		}
	}
	w := &BankReconWorkbench{BankAccountID: bankAccountID, GLAccount: glAccount, From: from, To: to, Counts: map[string]int{}}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'BankStatementLine' AND data->>'bank_account' = $1
		  AND (data->>'txn_date' BETWEEN $2 AND $3 OR COALESCE(data->>'match_status', 'Unmatched') IN ('Unmatched', 'Exception'))
		ORDER BY data->>'txn_date' DESC, id`, schema), bankAccountID, from, to)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			rows.Close()
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(dataStr), &d) != nil {
			continue
		}
		str := func(k string) string {
			if v, ok := d[k]; ok && v != nil {
				return fmt.Sprintf("%v", v)
			}
			return ""
		}
		l := BankStatementLineView{
			ID: id, TxnDate: str("txn_date"), Description: str("description"), Reference: str("reference_number"),
			Amount: numFromInterface(d["amount"]), DrCr: str("dr_cr"), MatchStatus: str("match_status"),
			MatchMethod: str("match_method"), MatchedBy: str("matched_by"), MatchedAt: str("matched_at"),
			MatchDifference: numFromInterface(d["match_difference"]), ExceptionOwner: str("exception_owner"),
			ExceptionReason: str("exception_reason"), ResolutionNote: str("resolution_note"),
		}
		if l.MatchStatus == "" {
			l.MatchStatus = "Unmatched"
		}
		if raised := str("exception_raised_at"); raised != "" && l.MatchStatus == "Exception" {
			if t, err := time.ParseInLocation("2006-01-02", raised, time.Local); err == nil {
				l.ExceptionAge = int(now.Sub(t).Hours() / 24)
			}
		}
		if l.MatchStatus == "Matched" && l.MatchDifference != 0 {
			w.OpenDifferences += l.MatchDifference
		}
		w.Counts[l.MatchStatus]++
		w.Lines = append(w.Lines, l)
	}
	rows.Close()

	// The book entries each matched line is paired with, for drill-back.
	if len(w.Lines) > 0 {
		pr, err := db.DB.Query(fmt.Sprintf(`
			SELECT matched_statement_line_id, posting_id, created_at::text, document_type, document_id, debit, credit, COALESCE(posted_by, '')
			FROM %s.gl_postings WHERE account_code = $1 AND matched_statement_line_id IS NOT NULL
			  AND matched_statement_line_id IN (SELECT id FROM %s.documents WHERE doctype = 'BankStatementLine' AND data->>'bank_account' = $2)`, schema, schema),
			glAccount, bankAccountID)
		if err != nil {
			return nil, err
		}
		byLine := map[string][]BankBookPosting{}
		for pr.Next() {
			var lineID string
			var b BankBookPosting
			var debit, credit int64
			if err := pr.Scan(&lineID, &b.PostingID, &b.Date, &b.DocumentType, &b.DocumentID, &debit, &credit, &b.PostedBy); err != nil {
				pr.Close()
				return nil, err
			}
			b.Debit, b.Credit = PaiseToRupees(debit), PaiseToRupees(credit)
			byLine[lineID] = append(byLine[lineID], b)
		}
		pr.Close()
		for i := range w.Lines {
			w.Lines[i].MatchedPostings = byLine[w.Lines[i].ID]
			if w.Lines[i].MatchedPostings == nil {
				w.Lines[i].MatchedPostings = []BankBookPosting{}
			}
		}
	}
	if w.Lines == nil {
		w.Lines = []BankStatementLineView{}
	}

	if w.UnmatchedBook, err = unmatchedBookPostings(schema, glAccount, 200); err != nil {
		return nil, err
	}
	if w.UnmatchedBook == nil {
		w.UnmatchedBook = []BankBookPosting{}
	}

	batchRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT d.data->>'settlement_batch_id', COALESCE(MAX(d.data->>'channel'), ''), COUNT(*), COALESCE(SUM(p.debit), 0), COALESCE(MAX(d.data->>'settlement_date'), '')
		FROM %s.gl_postings p JOIN %s.documents d ON d.doctype = 'MarketplaceSettlementLine' AND d.id = p.document_id
		WHERE p.document_type = 'MarketplaceSettlementLine' AND p.account_code = $1 AND p.debit > 0 AND p.matched_statement_line_id IS NULL
		GROUP BY d.data->>'settlement_batch_id' ORDER BY 5 DESC LIMIT 100`, schema, schema), glAccount)
	if err != nil {
		return nil, err
	}
	for batchRows.Next() {
		var s MarketplacePayoutSuggestion
		var paise int64
		if err := batchRows.Scan(&s.BatchID, &s.Channel, &s.Lines, &paise, &s.Date); err != nil {
			batchRows.Close()
			return nil, err
		}
		s.NetPayout = PaiseToRupees(paise)
		w.MarketplaceBatch = append(w.MarketplaceBatch, s)
	}
	batchRows.Close()
	if w.MarketplaceBatch == nil {
		w.MarketplaceBatch = []MarketplacePayoutSuggestion{}
	}

	var card, upi, bookBal int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(debit - credit) FILTER (WHERE account_code = '1101'), 0),
		COALESCE(SUM(debit - credit) FILTER (WHERE account_code = '1102'), 0),
		COALESCE(SUM(debit - credit) FILTER (WHERE account_code = $1 AND created_at < ($2::date + 1)), 0)
		FROM %s.gl_postings WHERE account_code IN ('1101', '1102', $1)`, schema), glAccount, to).Scan(&card, &upi, &bookBal)
	w.CardClearing, w.UPIClearing, w.BookBalance = PaiseToRupees(card), PaiseToRupees(upi), PaiseToRupees(bookBal)
	w.OpenDifferences = roundRupees(w.OpenDifferences)
	return w, nil
}

// unmatchedSettlementPayouts is the period-close "settlement" check: payout
// postings on any bank GL in the period not yet matched to a bank line, and
// what is still sitting in card / UPI clearing at period end.
func unmatchedSettlementPayouts(schema, startDate, endDate string) (int, string) {
	var batches int
	var card, upi int64
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(DISTINCT d.data->>'settlement_batch_id')
		FROM %s.gl_postings p JOIN %s.documents d ON d.doctype = 'MarketplaceSettlementLine' AND d.id = p.document_id
		WHERE p.document_type = 'MarketplaceSettlementLine' AND p.account_code = '1100' AND p.debit > 0
		  AND p.matched_statement_line_id IS NULL AND p.created_at >= $1::date AND p.created_at < ($2::date + 1)`, schema, schema),
		startDate, endDate).Scan(&batches); err != nil {
		return 1, "could not read marketplace payouts: " + err.Error()
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(debit - credit) FILTER (WHERE account_code = '1101'), 0),
		COALESCE(SUM(debit - credit) FILTER (WHERE account_code = '1102'), 0)
		FROM %s.gl_postings WHERE account_code IN ('1101', '1102') AND created_at < ($1::date + 1)`, schema), endDate).Scan(&card, &upi)
	open := batches
	if card != 0 {
		open++
	}
	if upi != 0 {
		open++
	}
	return open, fmt.Sprintf("%d marketplace payout batch(es) in the period not matched to a bank line; card clearing 1101 at period end %.2f, UPI clearing 1102 %.2f",
		batches, PaiseToRupees(card), PaiseToRupees(upi))
}
