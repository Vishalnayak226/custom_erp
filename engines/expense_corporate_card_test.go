package engines

import (
	"strings"
	"testing"
)

// Ask 176.3: corporate cards - the card's checks, and a statement line
// matched to the holder's claim, typed or found, never guessed.

func cardFix176(t *testing.T) *wsFix {
	t.Helper()
	f := g170Fix(t)
	f.insert("EMP-H", "Employee", "Active", "system", map[string]interface{}{"code": "EMP-H", "name": "H", "status": "Active"})
	f.insert("CARD-1", "CorporateCard", "Active", "system", map[string]interface{}{"code": "CARD-1", "last4": "4242", "employee_id": "EMP-G"})
	claim := func(id, status, emp, date string, amount, gst float64) {
		f.insert(id, "ExpenseClaim", status, "system", map[string]interface{}{"code": id, "employee_id": emp, "expense_date": date, "amount": amount, "gst_amount": gst})
	}
	claim("CLM-1", "Approved", "EMP-G", "2026-11-10", 1000, 180) // one fit for a 1180 charge
	claim("CLM-2", "Approved", "EMP-G", "2026-11-01", 500, 0)    // two fits for a 500 charge
	claim("CLM-3", "Verified", "EMP-G", "2026-11-03", 500, 0)
	claim("CLM-4", "Approved", "EMP-G", "2026-10-01", 700, 0) // outside the 7-day window
	claim("CLM-5", "Draft", "EMP-G", "2026-11-12", 300, 0)    // a draft never fits
	claim("CLM-6", "Approved", "EMP-G", "2026-11-12", 250, 0)
	claim("CLM-H", "Approved", "EMP-H", "2026-11-12", 400, 0) // another employee's
	return f
}

func cardLine176(over map[string]interface{}) map[string]interface{} {
	p := map[string]interface{}{"card": "CARD-1", "txn_date": "2026-11-12", "description": "Hotel", "amount": 1180.0}
	for k, v := range over {
		p[k] = v
	}
	return p
}

func TestStage1763CorporateCardKeepsOnlyTheLast4AndAHolder(t *testing.T) {
	f := cardFix176(t)
	for _, p := range []map[string]interface{}{
		{"last4": "12345", "employee_id": "EMP-G"},
		{"last4": "12a4", "employee_id": "EMP-G"},
		{"last4": "", "employee_id": "EMP-G"},
		{"last4": "1234", "employee_id": "EMP-NOPE"},
	} {
		wantCode(t, ValidateTransactionalRules(f.tenantID, "CorporateCard", "", "", nil, p), "EXP-1705")
	}
	if err := ValidateTransactionalRules(f.tenantID, "CorporateCard", "", "", nil, map[string]interface{}{"last4": "1234", "employee_id": "EMP-G"}); err != nil {
		t.Fatalf("a good card was refused: %v", err)
	}
}

func TestStage1763LineMatchesTheHoldersOneClaimOrStaysUnmatched(t *testing.T) {
	f := cardFix176(t)
	for name, over := range map[string]map[string]interface{}{
		"no card":        {"card": "CARD-NOPE"},
		"no date":        {"txn_date": ""},
		"no description": {"description": ""},
		"zero amount":    {"amount": 0.0},
	} {
		t.Run(name, func(t *testing.T) {
			wantCode(t, ValidateTransactionalRules(f.tenantID, "CardStatementLine", "", "", nil, cardLine176(over)), "EXP-1706")
		})
	}

	one := cardLine176(nil)
	if err := ValidateTransactionalRules(f.tenantID, "CardStatementLine", "", "", nil, one); err != nil {
		t.Fatal(err)
	}
	if one["status"] != "Matched" || one["matched_claim"] != "CLM-1" {
		t.Fatalf("one fitting claim: %v", one)
	}
	f.insert("L-1", "CardStatementLine", "Matched", "system", one)

	// The same charge again: CLM-1 is on L-1 now, so nothing fits.
	again := cardLine176(nil)
	if err := ValidateTransactionalRules(f.tenantID, "CardStatementLine", "", "", nil, again); err != nil || again["status"] != "Unmatched" {
		t.Fatalf("a held claim was matched twice: %v %v", err, again)
	}
	// L-1 re-saved keeps its own claim.
	resave := cardLine176(map[string]interface{}{"matched_claim": "CLM-1"})
	if err := ValidateTransactionalRules(f.tenantID, "CardStatementLine", "L-1", "Matched", one, resave); err != nil || resave["status"] != "Matched" {
		t.Fatalf("the line holding the claim was refused it: %v %v", err, resave)
	}

	for name, line := range map[string]map[string]interface{}{
		"two fit":          cardLine176(map[string]interface{}{"txn_date": "2026-11-02", "amount": 500.0}),
		"outside window":   cardLine176(map[string]interface{}{"amount": 700.0}),
		"draft claim only": cardLine176(map[string]interface{}{"amount": 300.0}),
		"other employee":   cardLine176(map[string]interface{}{"amount": 400.0}),
	} {
		if err := ValidateTransactionalRules(f.tenantID, "CardStatementLine", "", "", nil, line); err != nil || line["status"] != "Unmatched" || strField(line, "matched_claim") != "" {
			t.Fatalf("%s: want Unmatched, got %v %v", name, err, line)
		}
	}
}

func TestStage1763TypedClaimMustFitTheLine(t *testing.T) {
	f := cardFix176(t)
	f.insert("L-1", "CardStatementLine", "Matched", "system", cardLine176(map[string]interface{}{"matched_claim": "CLM-1", "status": "Matched"}))
	for name, over := range map[string]map[string]interface{}{
		"not found":      {"matched_claim": "CLM-NOPE"},
		"draft":          {"matched_claim": "CLM-5", "amount": 300.0},
		"other employee": {"matched_claim": "CLM-H", "amount": 400.0},
		"wrong total":    {"matched_claim": "CLM-2", "amount": 501.0},
		"held elsewhere": {"matched_claim": "CLM-1"},
	} {
		t.Run(name, func(t *testing.T) {
			wantCode(t, ValidateTransactionalRules(f.tenantID, "CardStatementLine", "", "", nil, cardLine176(over)), "EXP-1706")
		})
	}
	// Picking one of two fits by hand settles it - and the date window does
	// not apply to a claim someone picked.
	picked := cardLine176(map[string]interface{}{"matched_claim": "CLM-4", "amount": 700.0})
	if err := ValidateTransactionalRules(f.tenantID, "CardStatementLine", "", "", nil, picked); err != nil || picked["status"] != "Matched" {
		t.Fatalf("a claim picked by hand: %v %v", err, picked)
	}
}

func TestStage1763ImportedLinesAreMatchedAndTheReportListsTheRest(t *testing.T) {
	f := cardFix176(t)
	// The CSV import writes rows as Active without the form's checks.
	f.insert("L-I1", "CardStatementLine", "Active", "system", cardLine176(map[string]interface{}{"amount": 250.0}))
	f.insert("L-I2", "CardStatementLine", "Active", "system", cardLine176(map[string]interface{}{"txn_date": "2026-11-02", "amount": 500.0}))
	f.insert("L-I3", "CardStatementLine", "Active", "system", cardLine176(map[string]interface{}{"description": ""}))

	if err := MatchCardStatementLine(f.tenantID, "L-I1"); err != nil {
		t.Fatal(err)
	}
	if st, d := f.docStatus("CardStatementLine", "L-I1"); st != "Matched" || d["matched_claim"] != "CLM-6" {
		t.Fatalf("imported line: %s %v", st, d)
	}
	if err := MatchCardStatementLine(f.tenantID, "L-I2"); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.docStatus("CardStatementLine", "L-I2"); st != "Unmatched" {
		t.Fatalf("two fits were guessed: %s", st)
	}
	wantCode(t, MatchCardStatementLine(f.tenantID, "L-I3"), "EXP-1706")
	if st, _ := f.docStatus("CardStatementLine", "L-I3"); st != "Unmatched" {
		t.Fatalf("a line failing the check: %s", st)
	}

	rows, err := UnmatchedCardLines(f.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want L-I2 and L-I3, got %v", rows)
	}
	var two map[string]interface{}
	for _, r := range rows {
		if r["amount"] == 500.0 {
			two = r
		}
	}
	if two == nil || two["possible_claims"] != "CLM-2, CLM-3" || two["holder"] != "EMP-G" || two["card"] != "CARD-1" {
		t.Fatalf("the report row for two fits: %v", rows)
	}
	for _, r := range rows {
		if strings.Contains(strField(r, "possible_claims"), "CLM-6") {
			t.Fatalf("a matched claim offered again: %v", r)
		}
	}
}
