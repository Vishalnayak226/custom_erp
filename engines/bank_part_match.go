package engines

import (
	"database/sql"
	"fmt"
	"strings"

	"custom_erp/db"
)

// Stage 170.2: part-match a bank line.
//
// MatchBankLine pairs a statement line with book entries that sum to it
// exactly. Real banks do not always: one payment clears several statement
// lines, one deposit covers several receipts. MatchBankLineParts pairs an
// amount of each chosen book entry with the line; what is left on either side
// stays open and matchable. The line shows "Part Matched" until its open amount
// reaches zero, then "Matched". A book entry whose parts reach its full side is
// marked matched to the line that completed it, so every existing report that
// reads matched_statement_line_id sees it as matched. UnmatchBankLine releases
// the line's parts and reopens what they took. The auto and rule matchers never
// take a book entry that has parts.

// BankPart is one book entry and the amount of it matched to a line.
type BankPart struct {
	PostingID   string  `json:"posting_id"`
	AmountPaise int64   `json:"amount_paise"`
	Amount      float64 `json:"amount"` // rupees; used when AmountPaise is 0
}

// noBankParts is the SQL condition "this gl_postings row has no part match".
func noBankParts(schema string) string {
	return fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM %s.bank_match_parts bmp WHERE bmp.posting_id = gl_postings.posting_id)`, schema)
}

func postingPartsPaise(tx *sql.Tx, schema, postingID string) (int64, error) {
	var used int64
	err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(amount_paise), 0) FROM %s.bank_match_parts WHERE posting_id = $1`, schema), postingID).Scan(&used)
	return used, err
}

func linePartsPaise(tx *sql.Tx, schema, lineID string) (int64, error) {
	var used int64
	err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(amount_paise), 0) FROM %s.bank_match_parts WHERE statement_line_id = $1`, schema), lineID).Scan(&used)
	return used, err
}

// releaseBankParts deletes a line's parts and reopens every book entry they
// touched (an entry marked matched is reopened; its other parts stay).
func releaseBankParts(tx *sql.Tx, schema, lineID string) error {
	rows, err := tx.Query(fmt.Sprintf(`DELETE FROM %s.bank_match_parts WHERE statement_line_id = $1 RETURNING posting_id::text`, schema), lineID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.gl_postings SET matched_statement_line_id = NULL WHERE posting_id = $1 AND matched_statement_line_id IS NOT NULL`, schema), id); err != nil {
			return err
		}
	}
	return nil
}

// MatchBankLineParts matches part of a statement line to part of one or more
// book entries. The amounts together must not exceed the line's open amount,
// nor any entry's open amount. Repeat calls add more parts.
func MatchBankLineParts(tenantID, lineID string, parts []BankPart, note, actor string) error {
	if len(parts) == 0 {
		return &ValidationError{Code: "FIN-1700", Message: "choose at least one book entry and an amount"}
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
		return &ValidationError{Code: "FIN-1703", Message: fmt.Sprintf("this line is already %s - unmatch it first", line.status)}
	}
	already, err := linePartsPaise(tx, schema, lineID)
	if err != nil {
		return err
	}
	var add int64
	seen := map[string]bool{}
	for _, p := range parts {
		amt := p.AmountPaise
		if amt == 0 {
			amt = RupeesToPaise(p.Amount)
		}
		if p.PostingID == "" || amt <= 0 {
			return &ValidationError{Code: "FIN-1700", Message: "each part needs a book entry and an amount above zero"}
		}
		if seen[p.PostingID] {
			return &ValidationError{Code: "FIN-1700", Message: "a book entry is listed twice"}
		}
		seen[p.PostingID] = true
		var account string
		var debit, credit int64
		var matched sql.NullString
		if err := tx.QueryRow(fmt.Sprintf(`SELECT account_code, debit, credit, matched_statement_line_id FROM %s.gl_postings WHERE posting_id::text = $1 FOR UPDATE`, schema),
			p.PostingID).Scan(&account, &debit, &credit, &matched); err != nil {
			return &ValidationError{Code: "FIN-1702", Message: fmt.Sprintf("book entry %s not found", p.PostingID)}
		}
		if account != line.glAccount {
			return &ValidationError{Code: "FIN-1702", Message: fmt.Sprintf("book entry %s is on account %s, not this bank's %s", p.PostingID, account, line.glAccount)}
		}
		if matched.Valid && matched.String != "" {
			return &ValidationError{Code: "FIN-1702", Message: fmt.Sprintf("book entry %s is already matched to statement line %s", p.PostingID, matched.String)}
		}
		side := bookSidePaise(line.drCr, debit, credit)
		if side == 0 {
			return &ValidationError{Code: "FIN-1702", Message: fmt.Sprintf("book entry %s is on the wrong side for a statement %s", p.PostingID, line.drCr)}
		}
		used, err := postingPartsPaise(tx, schema, p.PostingID)
		if err != nil {
			return err
		}
		if amt > side-used {
			return &ValidationError{Code: "FIN-1702", Message: fmt.Sprintf("book entry %s has only %.2f open", p.PostingID, PaiseToRupees(side-used))}
		}
		add += amt
	}
	if already+add > line.amountPaise {
		return &ValidationError{Code: "FIN-1701", Message: fmt.Sprintf("the parts total %.2f but only %.2f of this statement line is open",
			PaiseToRupees(add), PaiseToRupees(line.amountPaise-already))}
	}
	for _, p := range parts {
		amt := p.AmountPaise
		if amt == 0 {
			amt = RupeesToPaise(p.Amount)
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.bank_match_parts (statement_line_id, posting_id, amount_paise, matched_by, note) VALUES ($1, $2::uuid, $3, $4, $5)`, schema),
			lineID, p.PostingID, amt, actor, strings.TrimSpace(note)); err != nil {
			return err
		}
		// A book entry whose parts now reach its whole side is matched.
		var debit, credit int64
		if err := tx.QueryRow(fmt.Sprintf(`SELECT debit, credit FROM %s.gl_postings WHERE posting_id::text = $1`, schema), p.PostingID).Scan(&debit, &credit); err != nil {
			return err
		}
		used, err := postingPartsPaise(tx, schema, p.PostingID)
		if err != nil {
			return err
		}
		if used >= bookSidePaise(line.drCr, debit, credit) {
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.gl_postings SET matched_statement_line_id = $1 WHERE posting_id::text = $2`, schema), lineID, p.PostingID); err != nil {
				return err
			}
		}
	}
	total := already + add
	open := line.amountPaise - total
	patch := map[string]interface{}{"matched_amount": PaiseToRupees(total), "open_amount": PaiseToRupees(open)}
	if open == 0 {
		for k, v := range matchedPatch("part", actor, 0) {
			patch[k] = v
		}
	} else {
		patch["match_status"], patch["match_method"], patch["matched_by"] = "Part Matched", "part", actor
	}
	if n := strings.TrimSpace(note); n != "" {
		patch["resolution_note"] = n
	}
	if err := patchBankLine(tx, schema, lineID, patch); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "BANK_LINE_PART_MATCHED", "SUCCESS", fmt.Sprintf("Statement line %s part-matched %.2f, %.2f open", lineID, PaiseToRupees(total), PaiseToRupees(open)))
	return nil
}
