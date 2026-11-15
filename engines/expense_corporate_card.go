package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Ask 176 item 176.3: corporate cards.
//
// A CorporateCard is held by one employee. Its statement lines
// (CardStatementLine) are typed or loaded with the record list's CSV import,
// and every save - and every imported line, through the import's
// post-commit step - is matched to an expense claim:
//
//   - a claim typed on the line must be the card holder's, for the same total
//     (amount plus GST), not Draft or Rejected, and held by no other line;
//   - with none typed, the line takes the holder's one claim of the same
//     total dated within 7 days of the charge that no other line holds.
//
// Two or more such claims leave the line Unmatched rather than guess; the
// "Card lines not matched" report lists those lines with the claims that
// could fit, so someone picks one.

const (
	CorporateCardDoctype     = "CorporateCard"
	CardStatementLineDoctype = "CardStatementLine"
	corporateCardCodeInvalid = "EXP-1705"
	cardLineCodeInvalid      = "EXP-1706"
	cardLineMatchWindowDays  = 7
)

var cardLast4Pattern = regexp.MustCompile(`^[0-9]{4}$`)

func init() {
	RegisterReport(ReportDefinition{
		ID: "card-lines-unmatched", Label: "Card lines not matched", Category: "Expenses",
		Columns: []ReportColumn{
			{Key: "card", Label: "Card"}, {Key: "holder", Label: "Card holder"},
			{Key: "txn_date", Label: "Date"}, {Key: "description", Label: "Description"},
			{Key: "amount", Label: "Amount", Sensitive: true, FinanceSees: true, Total: true},
			{Key: "possible_claims", Label: "Claims that fit"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return UnmatchedCardLines(tenantID)
		},
	})
}

// validateCorporateCardSave: only the last 4 digits are kept (never the full
// number), and the card has a holder.
func validateCorporateCardSave(tenantID string, payload map[string]interface{}) error {
	bad := func(msg string) error { return &ValidationError{Code: corporateCardCodeInvalid, Message: msg} }
	if !cardLast4Pattern.MatchString(strField(payload, "last4")) {
		return bad("Enter only the last 4 digits of the card.")
	}
	if _, _, err := fetchDocData(tenantID, "Employee", strField(payload, "employee_id")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return bad("Pick the card holder.")
		}
		return err
	}
	return nil
}

// validateCardStatementLineSave checks a statement line and matches it,
// setting matched_claim and the status (Matched / Unmatched) on payload.
// docID is the line's own id ("" on a create), so it never counts as the
// other line holding a claim.
func validateCardStatementLineSave(tenantID, docID string, payload map[string]interface{}) error {
	bad := func(msg string) error { return &ValidationError{Code: cardLineCodeInvalid, Message: msg} }
	card, _, err := fetchDocData(tenantID, CorporateCardDoctype, strField(payload, "card"))
	if errors.Is(err, sql.ErrNoRows) {
		return bad("Pick the card.")
	}
	if err != nil {
		return err
	}
	txnDate, err := time.Parse("2006-01-02", strField(payload, "txn_date"))
	if err != nil {
		return bad("Enter the date of the charge.")
	}
	if strField(payload, "description") == "" {
		return bad("Give the description from the statement.")
	}
	amount := RupeesToPaise(numFromInterface(payload["amount"]))
	if amount <= 0 {
		return bad("The amount must be above 0.")
	}
	holder := strField(card, "employee_id")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	if typed := strField(payload, "matched_claim"); typed != "" {
		claim, status, err := fetchDocData(tenantID, "ExpenseClaim", typed)
		if errors.Is(err, sql.ErrNoRows) {
			return bad("The claim was not found.")
		}
		if err != nil {
			return err
		}
		if status == "Draft" || status == "Rejected" {
			return bad("That claim is a draft or was rejected.")
		}
		same, err := sameDocRef(tenantID, "Employee", holder, strField(claim, "employee_id"))
		if err != nil {
			return err
		}
		if !same {
			return bad("That claim is not the card holder's.")
		}
		if claimTotalPaise(claim) != amount {
			return bad("That claim's total is not the amount on the line.")
		}
		claimID, err := docRefID(tenantID, "ExpenseClaim", typed)
		if err != nil {
			return err
		}
		held, err := cardLineHoldingClaim(schema, claimID, strField(claim, "code"), docID)
		if err != nil {
			return err
		}
		if held != "" {
			return bad("That claim is already on another card line.")
		}
		payload["status"] = "Matched"
		return nil
	}

	candidates, err := cardLineCandidates(tenantID, schema, holder, amount, txnDate, docID)
	if err != nil {
		return err
	}
	if len(candidates) == 1 {
		payload["matched_claim"] = candidates[0].id
		payload["status"] = "Matched"
	} else {
		payload["status"] = "Unmatched"
	}
	return nil
}

// MatchCardStatementLine runs the same check and match on a line that is
// already stored - the CSV import's post-commit step, as the import writes
// rows without the record form's checks. A line that fails the check is kept
// as Unmatched and the reason is returned for the import to report.
func MatchCardStatementLine(tenantID, id string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var raw string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents
		WHERE id = $1 AND doctype = 'CardStatementLine' AND deleted_at IS NULL`, schema), id).Scan(&raw); err != nil {
		return err
	}
	data := map[string]interface{}{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return err
	}
	checkErr := validateCardStatementLineSave(tenantID, id, data)
	if checkErr != nil {
		var verr *ValidationError
		if !errors.As(checkErr, &verr) {
			return checkErr
		}
		data["status"] = "Unmatched"
	}
	out, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND doctype = 'CardStatementLine'`, schema), string(out), strField(data, "status"), id); err != nil {
		return err
	}
	return checkErr
}

type cardClaimRef struct{ id, code string }

// claimTotalPaise is what the employee spent on a claim: amount plus GST.
func claimTotalPaise(claim map[string]interface{}) int64 {
	return RupeesToPaise(numFromInterface(claim["amount"])) + RupeesToPaise(numFromInterface(claim["gst_amount"]))
}

// cardLineHoldingClaim is the id of a live card line other than exceptLine
// that names the claim (by id or code), "" when none does.
func cardLineHoldingClaim(schema, claimID, claimCode, exceptLine string) (string, error) {
	var id string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents
		WHERE doctype = 'CardStatementLine' AND deleted_at IS NULL AND id <> $1
		  AND data->>'matched_claim' <> '' AND data->>'matched_claim' IN ($2, $3) LIMIT 1`, schema),
		exceptLine, claimID, claimCode).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// cardLineCandidates are the card holder's claims a line could be: the same
// total, not Draft or Rejected, dated within the match window of the charge,
// and held by no other line.
func cardLineCandidates(tenantID, schema, holderRef string, amountPaise int64, txnDate time.Time, exceptLine string) ([]cardClaimRef, error) {
	if holderRef == "" {
		return nil, nil
	}
	holderID, err := docRefID(tenantID, "Employee", holderRef)
	if err != nil {
		return nil, err
	}
	holderCode := holderRef
	if emp, _, err := fetchDocData(tenantID, "Employee", holderRef); err == nil && strField(emp, "code") != "" {
		holderCode = strField(emp, "code")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT c.id, COALESCE(NULLIF(c.data->>'code', ''), c.id)
		  FROM %[1]s.documents c
		 WHERE c.doctype = 'ExpenseClaim' AND c.deleted_at IS NULL AND c.status NOT IN ('Draft', 'Rejected')
		   AND c.data->>'employee_id' IN ($1, $2, $3)
		   AND ROUND((COALESCE(NULLIF(c.data->>'amount', '')::numeric, 0)
		            + COALESCE(NULLIF(c.data->>'gst_amount', '')::numeric, 0)) * 100) = $4
		   AND (CASE WHEN c.data->>'expense_date' ~ '^\d{4}-\d{2}-\d{2}$' THEN (c.data->>'expense_date')::date END)
		       BETWEEN $5::date - $6::int AND $5::date + $6::int
		   AND NOT EXISTS (SELECT 1 FROM %[1]s.documents l
		                    WHERE l.doctype = 'CardStatementLine' AND l.deleted_at IS NULL AND l.id <> $7
		                      AND l.data->>'matched_claim' <> '' AND l.data->>'matched_claim' IN (c.id, c.data->>'code'))
		 ORDER BY c.data->>'expense_date', c.id`, schema),
		holderID, holderCode, holderRef, amountPaise, txnDate.Format("2006-01-02"), cardLineMatchWindowDays, exceptLine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []cardClaimRef
	for rows.Next() {
		var c cardClaimRef
		if err := rows.Scan(&c.id, &c.code); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UnmatchedCardLines lists every Unmatched card line with the claims that
// could fit it (none, or more than one - one would have matched).
func UnmatchedCardLines(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT l.id, COALESCE(c.data->>'code', l.data->>'card', ''), COALESCE(c.data->>'employee_id', ''),
		       COALESCE(l.data->>'txn_date', ''), COALESCE(l.data->>'description', ''),
		       COALESCE(NULLIF(l.data->>'amount', '')::numeric, 0)::float8
		  FROM %[1]s.documents l
		  LEFT JOIN %[1]s.documents c ON c.doctype = 'CorporateCard' AND c.deleted_at IS NULL
		       AND (c.id = l.data->>'card' OR c.data->>'code' = l.data->>'card')
		 WHERE l.doctype = 'CardStatementLine' AND l.deleted_at IS NULL AND l.status <> 'Matched'
		 ORDER BY l.data->>'txn_date', l.id`, schema))
	if err != nil {
		return nil, err
	}
	type line struct {
		id, card, holder, date, desc string
		amount                       float64
	}
	var lines []line
	for rows.Next() {
		var l line
		if err := rows.Scan(&l.id, &l.card, &l.holder, &l.date, &l.desc, &l.amount); err != nil {
			rows.Close()
			return nil, err
		}
		lines = append(lines, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	for _, l := range lines {
		var fits []string
		if d, err := time.Parse("2006-01-02", l.date); err == nil {
			cands, err := cardLineCandidates(tenantID, schema, l.holder, RupeesToPaise(l.amount), d, l.id)
			if err != nil {
				return nil, err
			}
			for _, c := range cands {
				fits = append(fits, c.code)
			}
		}
		out = append(out, map[string]interface{}{
			"card": l.card, "holder": l.holder, "txn_date": l.date, "description": l.desc,
			"amount": round2(l.amount), "possible_claims": strings.Join(fits, ", "),
		})
	}
	return out, nil
}
