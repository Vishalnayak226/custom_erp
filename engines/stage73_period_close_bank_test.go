package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

// signAllCloseTasksForTest gives every close task of the period an owner and
// signs it off, so a test that only cares about the closed state can close.
func signAllCloseTasksForTest(t *testing.T, tenantID, periodID string) {
	t.Helper()
	for _, d := range periodCloseTaskDefs {
		if err := AssignPeriodCloseTask(tenantID, periodID, d.Key, "manager1", "admin", RoleSuperAdmin); err != nil {
			t.Fatalf("assign %s: %v", d.Key, err)
		}
		if err := SignOffPeriodCloseTask(tenantID, periodID, d.Key, "Done", "checked for the test fixture", "", "manager1"); err != nil {
			t.Fatalf("sign %s: %v", d.Key, err)
		}
	}
}

func validationCode(err error) string {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Code
	}
	return ""
}

func testUserID(t *testing.T, schema, username string) string {
	t.Helper()
	var id string
	if err := db.DB.QueryRow("SELECT id FROM "+schema+".users WHERE username = $1", username).Scan(&id); err != nil {
		t.Fatalf("user %s: %v", username, err)
	}
	return id
}

// Stage 73.1.10: close needs every task signed by its owner with evidence; a
// closed period reopens only through an approved request (maker-checker),
// and the next close needs the tasks signed again.
func TestPeriodCloseTasksGateCloseAndReopenNeedsApproval(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	var periodID string
	cleanup := func() {
		if periodID != "" {
			db.DB.Exec("DELETE FROM "+schema+".period_close_log WHERE period_id = $1", periodID)
		}
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE doctype = 'PeriodReopenRequest' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'PeriodReopenRequest' AND data->>'period_name' = 'TEST-PERIOD-7310')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'PeriodReopenRequest' AND data->>'period_name' = 'TEST-PERIOD-7310'")
		db.DB.Exec("DELETE FROM " + schema + ".accounting_periods WHERE period_name = 'TEST-PERIOD-7310'")
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'TestPeriod7310'")
	}
	cleanup()
	defer cleanup()

	today := time.Now()
	periodID, err = CreateAccountingPeriod(tenantID, "TEST-PERIOD-7310", today.AddDate(0, 0, -2).Format("2006-01-02"), today.AddDate(0, 0, 2).Format("2006-01-02"), "system")
	if err != nil {
		t.Fatalf("CreateAccountingPeriod: %v", err)
	}

	// Nothing signed: the close is refused, naming the open tasks.
	if code := validationCode(CloseAccountingPeriod(tenantID, periodID, "system")); code != "FIN-0501" {
		t.Fatalf("close with unsigned tasks: code %q, want FIN-0501", code)
	}
	board, err := GetPeriodCloseBoard(tenantID, periodID, RoleSuperAdmin)
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	if len(board.Tasks) != len(periodCloseTaskDefs) || board.AllSigned || board.Unsigned != len(periodCloseTaskDefs) {
		t.Fatalf("board tasks=%d allSigned=%v unsigned=%d", len(board.Tasks), board.AllSigned, board.Unsigned)
	}
	for _, task := range board.Tasks {
		if task.CheckDetail == "" {
			t.Fatalf("task %s has no live check", task.Key)
		}
	}

	// Ownership rules.
	if code := validationCode(SignOffPeriodCloseTask(tenantID, periodID, "bank", "Done", "statement agreed to the book", "", "manager1")); code != "FIN-0508" {
		t.Fatalf("sign without an owner: %q, want FIN-0508", code)
	}
	if code := validationCode(AssignPeriodCloseTask(tenantID, periodID, "bank", "manager1", "manager1", RoleStoreManager)); code != "FIN-0504" {
		t.Fatalf("assign by a store manager: %q, want FIN-0504", code)
	}
	if code := validationCode(AssignPeriodCloseTask(tenantID, periodID, "bank", "nobody-such", "admin", RoleSuperAdmin)); code != "FIN-0505" {
		t.Fatalf("assign to an unknown user: %q, want FIN-0505", code)
	}
	if err := AssignPeriodCloseTask(tenantID, periodID, "bank", "manager1", "admin", "Accountant"); err != nil {
		t.Fatalf("assign by an Accountant: %v", err)
	}
	if code := validationCode(SignOffPeriodCloseTask(tenantID, periodID, "bank", "Done", "statement agreed", "", "cashier1")); code != "FIN-0508" {
		t.Fatalf("sign by a non-owner: %q, want FIN-0508", code)
	}
	if code := validationCode(SignOffPeriodCloseTask(tenantID, periodID, "bank", "Done", "ok", "", "manager1")); code != "FIN-0507" {
		t.Fatalf("sign with thin evidence: %q, want FIN-0507", code)
	}
	if err := SignOffPeriodCloseTask(tenantID, periodID, "bank", "Done", "statement agreed to the book", "BANK-STMT-OCT", "manager1"); err != nil {
		t.Fatalf("owner sign-off: %v", err)
	}
	// Undo then sign again: both are logged.
	if err := UndoPeriodCloseTaskSignOff(tenantID, periodID, "bank", "late statement line found", "manager1", RoleStoreManager); err != nil {
		t.Fatalf("undo by owner: %v", err)
	}
	signAllCloseTasksForTest(t, tenantID, periodID)

	board, _ = GetPeriodCloseBoard(tenantID, periodID, RoleSuperAdmin)
	if !board.AllSigned {
		t.Fatalf("all tasks signed but board says otherwise")
	}
	for _, task := range board.Tasks {
		if task.SignedBy != "manager1" || task.CheckSnapshot == "" || task.Evidence == "" {
			t.Fatalf("task %s sign-off not recorded: %+v", task.Key, task)
		}
	}
	if err := CloseAccountingPeriod(tenantID, periodID, testUserID(t, schema, "admin")); err != nil {
		t.Fatalf("close with every task signed: %v", err)
	}
	if code := validationCode(PostDoubleEntry(tenantID, "TestPeriod7310", "IN-CLOSED", map[string]int64{"1100": 100}, map[string]int64{"4100": 100}, "", "")); code != "FIN-0260" {
		t.Fatalf("posting into the closed period: %q, want FIN-0260", code)
	}
	if code := validationCode(AssignPeriodCloseTask(tenantID, periodID, "bank", "admin", "admin", RoleSuperAdmin)); code != "FIN-0503" {
		t.Fatalf("task change while closed: %q, want FIN-0503", code)
	}

	// Reopen: role, reason, maker-checker.
	adminID, systemID := testUserID(t, schema, "admin"), testUserID(t, schema, "system")
	if _, err := RequestPeriodReopen(tenantID, periodID, "missed a supplier credit note", "x", "manager1", RoleStoreManager); validationCode(err) != "FIN-0504" {
		t.Fatalf("reopen by a store manager: %v", err)
	}
	if _, err := RequestPeriodReopen(tenantID, periodID, "short", adminID, "admin", RoleSuperAdmin); validationCode(err) != "FIN-0507" {
		t.Fatalf("reopen with a thin reason: %v", err)
	}
	reqID, err := RequestPeriodReopen(tenantID, periodID, "missed a supplier credit note dated in the period", adminID, "admin", RoleSuperAdmin)
	if err != nil {
		t.Fatalf("RequestPeriodReopen: %v", err)
	}
	if _, err := RequestPeriodReopen(tenantID, periodID, "a second request while one is pending", adminID, "admin", RoleSuperAdmin); validationCode(err) != "FIN-0509" {
		t.Fatalf("second pending reopen: %v", err)
	}
	withSelfApproval(t, tenantID, SelfApprovalNobody) // Stage 92: the submitter may not decide
	if err := DecideApproval(tenantID, "PeriodReopenRequest", reqID, adminID, RoleSuperAdmin, "", "Approved", ""); err == nil {
		t.Fatalf("the requester approved their own reopen")
	}
	board, _ = GetPeriodCloseBoard(tenantID, periodID, RoleSuperAdmin)
	if board.Status != "Closed" || board.ReopenOpenID != reqID {
		t.Fatalf("before approval: status %s, pending %q", board.Status, board.ReopenOpenID)
	}
	if err := DecideApproval(tenantID, "PeriodReopenRequest", reqID, systemID, RoleSuperAdmin, "", "Approved", "credit note confirmed"); err != nil {
		t.Fatalf("approve by a second admin: %v", err)
	}
	board, _ = GetPeriodCloseBoard(tenantID, periodID, RoleSuperAdmin)
	if board.Status != "Open" || board.CloseRound != 2 || board.AllSigned || board.ReopenedBy != "system" {
		t.Fatalf("after approval: status %s round %d allSigned %v reopenedBy %q", board.Status, board.CloseRound, board.AllSigned, board.ReopenedBy)
	}
	for _, task := range board.Tasks {
		if task.Status != "Open" || task.Owner != "manager1" {
			t.Fatalf("task %s after reopen: status %s owner %s", task.Key, task.Status, task.Owner)
		}
	}
	events := map[string]int{}
	for _, e := range board.Log {
		events[e.Event]++
	}
	if events["CLOSED"] != 1 || events["REOPEN_REQUESTED"] != 1 || events["REOPENED"] != 1 || events["TASK_RESET"] != len(periodCloseTaskDefs) || events["TASK_UNDONE"] != 1 {
		t.Fatalf("close log events %v", events)
	}
	// Applying again is a no-op, not a second round.
	if err := ApplyApprovedPeriodReopen(tenantID, reqID, ""); err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	board, _ = GetPeriodCloseBoard(tenantID, periodID, RoleSuperAdmin)
	if board.CloseRound != 2 {
		t.Fatalf("re-apply started round %d", board.CloseRound)
	}
	// The reopened period takes a correcting entry, then re-closes only
	// through the tasks again.
	if err := PostDoubleEntry(tenantID, "TestPeriod7310", "REVERSAL", map[string]int64{"4100": 100}, map[string]int64{"1100": 100}, "", ""); err != nil {
		t.Fatalf("posting into the reopened period: %v", err)
	}
	if code := validationCode(CloseAccountingPeriod(tenantID, periodID, adminID)); code != "FIN-0501" {
		t.Fatalf("re-close without re-signing: %q, want FIN-0501", code)
	}
	signAllCloseTasksForTest(t, tenantID, periodID)
	if err := CloseAccountingPeriod(tenantID, periodID, adminID); err != nil {
		t.Fatalf("re-close: %v", err)
	}
}

func insertTestDoc(t *testing.T, schema, id, doctype string, data map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, string(b)); err != nil {
		t.Fatalf("insert %s %s: %v", doctype, id, err)
	}
}

func bankLineStatus(t *testing.T, schema, id string) (status, method string, diff float64) {
	t.Helper()
	var dataStr string
	if err := db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE doctype = 'BankStatementLine' AND id = $1", id).Scan(&dataStr); err != nil {
		t.Fatalf("line %s: %v", id, err)
	}
	var d map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &d)
	status, _ = d["match_status"].(string)
	method, _ = d["match_method"].(string)
	return status, method, numFromInterface(d["match_difference"])
}

func glPostingID(t *testing.T, schema, docType, docID, account string) string {
	t.Helper()
	var id string
	if err := db.DB.QueryRow("SELECT posting_id FROM "+schema+".gl_postings WHERE document_type = $1 AND document_id = $2 AND account_code = $3 LIMIT 1",
		docType, docID, account).Scan(&id); err != nil {
		t.Fatalf("posting %s/%s/%s: %v", docType, docID, account, err)
	}
	return id
}

// Stage 73.2.16: manual, rule and payout matching, the exception queue and
// the closed-period freeze.
func TestBankReconciliationWorkbenchMatchesExceptionsAndPayouts(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	var periodID string
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type IN ('TestBank7316') OR (document_type = 'BankStatementLine' AND document_id LIKE 'T7316-%') OR (document_type = 'MarketplaceSettlementLine' AND document_id LIKE 'T7316-%')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T7316-%'")
		if periodID != "" {
			db.DB.Exec("DELETE FROM "+schema+".period_close_log WHERE period_id = $1", periodID)
		}
		db.DB.Exec("DELETE FROM " + schema + ".accounting_periods WHERE period_name = 'TEST-PERIOD-7316'")
		db.DB.Exec("DELETE FROM " + schema + ".gl_accounts WHERE account_code = '1199'")
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".gl_accounts (account_code, account_name, account_type) VALUES ('1199', 'Test Bank 7316', 'Asset')"); err != nil {
		t.Fatalf("gl account: %v", err)
	}
	today := time.Now().Format("2006-01-02")
	insertTestDoc(t, schema, "T7316-BANK", "BankAccount", map[string]interface{}{"bank_name": "Test", "account_number": "7316", "gl_account_code": "1199"})
	post := func(docID string, debits, credits map[string]int64) {
		t.Helper()
		if err := PostDoubleEntry(tenantID, "TestBank7316", docID, debits, credits, "", ""); err != nil {
			t.Fatalf("post %s: %v", docID, err)
		}
	}
	post("T7316-A", map[string]int64{"1199": 100000}, map[string]int64{"4100": 100000})
	post("T7316-REF-XYZ", map[string]int64{"1199": 50000}, map[string]int64{"4100": 50000})
	post("T7316-S1", map[string]int64{"1199": 25000}, map[string]int64{"4100": 25000})
	post("T7316-S2", map[string]int64{"1199": 25000}, map[string]int64{"4100": 25000})
	line := func(id, drCr string, amount float64, ref, date string) {
		insertTestDoc(t, schema, id, "BankStatementLine", map[string]interface{}{
			"bank_account": "T7316-BANK", "txn_date": date, "amount": amount, "dr_cr": drCr, "reference_number": ref, "match_status": "Unmatched"})
	}
	line("T7316-L1", "Credit", 1000, "", today)
	line("T7316-L2", "Credit", 495, "REF-XYZ", today)
	line("T7316-L3", "Credit", 500, "", today)
	line("T7316-L4", "Credit", 777, "", today)

	pA := glPostingID(t, schema, "TestBank7316", "T7316-A", "1199")
	pS1 := glPostingID(t, schema, "TestBank7316", "T7316-S1", "1199")
	pS2 := glPostingID(t, schema, "TestBank7316", "T7316-S2", "1199")
	revenueLeg := glPostingID(t, schema, "TestBank7316", "T7316-A", "4100")

	// Manual: refused on another account, on a short total; one-to-one and
	// many-to-one both match.
	if code := validationCode(MatchBankLine(tenantID, "T7316-L1", []string{revenueLeg}, "", "manager1")); code != "FIN-0512" {
		t.Fatalf("match to another account's entry: %q", code)
	}
	if code := validationCode(MatchBankLine(tenantID, "T7316-L3", []string{pS1}, "", "manager1")); code != "FIN-0513" {
		t.Fatalf("match short of the line amount: %q", code)
	}
	if err := MatchBankLine(tenantID, "T7316-L1", []string{pA}, "", "manager1"); err != nil {
		t.Fatalf("one-to-one match: %v", err)
	}
	if err := MatchBankLine(tenantID, "T7316-L3", []string{pS1, pS2}, "two counter deposits", "manager1"); err != nil {
		t.Fatalf("many-to-one match: %v", err)
	}
	if code := validationCode(MatchBankLine(tenantID, "T7316-L4", []string{pA}, "", "manager1")); code != "FIN-0512" {
		t.Fatalf("re-using a matched entry: %q", code)
	}

	// Rule: 495 against the 500 entry only within tolerance, found by reference.
	if res, err := RuleMatchBankStatement(tenantID, "T7316-BANK", BankMatchRule{AmountTolerance: 0, DateWindowDays: 2, UseReference: true}, "manager1"); err != nil || res.Matched != 0 {
		t.Fatalf("zero-tolerance rule pass: %+v %v", res, err)
	}
	res, err := RuleMatchBankStatement(tenantID, "T7316-BANK", BankMatchRule{AmountTolerance: 10, DateWindowDays: 2, UseReference: true}, "manager1")
	if err != nil || res.Matched != 1 || res.WithDifference != 1 {
		t.Fatalf("tolerance rule pass: %+v %v", res, err)
	}
	if status, method, diff := bankLineStatus(t, schema, "T7316-L2"); status != "Matched" || method != "rule:reference" || diff != -5 {
		t.Fatalf("L2 after rule pass: %s %s %v", status, method, diff)
	}

	// Exception queue: owner + reason, explained only by Finance Head / Admin.
	if code := validationCode(RaiseBankException(tenantID, "T7316-L4", "", "unknown deposit", "manager1")); code != "FIN-0515" {
		t.Fatalf("exception without owner: %q", code)
	}
	if err := RaiseBankException(tenantID, "T7316-L4", "manager1", "unknown deposit - asked the bank", "manager1"); err != nil {
		t.Fatalf("raise exception: %v", err)
	}
	if code := validationCode(ExplainBankException(tenantID, "T7316-L4", "bank will reverse its own error", "manager1", RoleStoreManager)); code != "FIN-0504" {
		t.Fatalf("explain by a store manager: %q", code)
	}
	if err := ExplainBankException(tenantID, "T7316-L4", "bank will reverse its own error next week", "admin", RoleSuperAdmin); err != nil {
		t.Fatalf("explain: %v", err)
	}
	wb, err := GetBankReconciliationWorkbench(tenantID, "T7316-BANK", "", "")
	if err != nil {
		t.Fatalf("workbench: %v", err)
	}
	if wb.Counts["Matched"] != 3 || wb.Counts["Explained"] != 1 || wb.OpenDifferences != -5 {
		t.Fatalf("workbench counts %v, open differences %v", wb.Counts, wb.OpenDifferences)
	}
	// Unmatch, with a reason, frees the entries again.
	if err := UnmatchBankLine(tenantID, "T7316-L3", "wrong deposits paired", "manager1"); err != nil {
		t.Fatalf("unmatch: %v", err)
	}
	if cands, err := BankLineCandidates(tenantID, "T7316-L3"); err != nil || len(cands) < 2 {
		t.Fatalf("candidates after unmatch: %v %v", cands, err)
	}

	// Card payout: clears 1101 to the bank with the fee to 5230, once.
	post("T7316-CARD", map[string]int64{"1101": 200000}, map[string]int64{"4100": 200000})
	line("T7316-L5", "Credit", 1960, "", today)
	if err := MatchPOSPayout(tenantID, "T7316-L5", "1101", 40, "manager1"); err != nil {
		t.Fatalf("card payout: %v", err)
	}
	var bankDr, feeDr, clearingCr int64
	db.DB.QueryRow("SELECT COALESCE(SUM(debit) FILTER (WHERE account_code = '1199'), 0), COALESCE(SUM(debit) FILTER (WHERE account_code = '5230'), 0), COALESCE(SUM(credit) FILTER (WHERE account_code = '1101'), 0) FROM "+schema+".gl_postings WHERE document_type = 'BankStatementLine' AND document_id = 'T7316-L5'").Scan(&bankDr, &feeDr, &clearingCr)
	if bankDr != 196000 || feeDr != 4000 || clearingCr != 200000 {
		t.Fatalf("payout posting bank %d fee %d clearing %d", bankDr, feeDr, clearingCr)
	}
	if status, method, _ := bankLineStatus(t, schema, "T7316-L5"); status != "Matched" || method != "payout:card" {
		t.Fatalf("L5: %s %s", status, method)
	}
	if code := validationCode(MatchPOSPayout(tenantID, "T7316-L5", "1101", 40, "manager1")); code != "FIN-0512" {
		t.Fatalf("second payout match: %q", code)
	}

	// Marketplace batch: two settlement lines' payouts match one bank credit.
	insertTestDoc(t, schema, "T7316-BANK1100", "BankAccount", map[string]interface{}{"bank_name": "Test", "account_number": "1100", "gl_account_code": "1100"})
	for i, amt := range []int64{30000, 20000} {
		id := fmt.Sprintf("T7316-MSL%d", i)
		insertTestDoc(t, schema, id, "MarketplaceSettlementLine", map[string]interface{}{"channel": "TestMart", "settlement_batch_id": "T7316-BATCH", "settlement_date": today})
		if err := PostDoubleEntry(tenantID, "MarketplaceSettlementLine", id, map[string]int64{"1100": amt}, map[string]int64{"1300": amt}, "", ""); err != nil {
			t.Fatalf("settlement posting: %v", err)
		}
	}
	insertTestDoc(t, schema, "T7316-L6", "BankStatementLine", map[string]interface{}{"bank_account": "T7316-BANK1100", "txn_date": today, "amount": 450, "dr_cr": "Credit", "match_status": "Unmatched"})
	if code := validationCode(MatchMarketplacePayout(tenantID, "T7316-L6", "T7316-BATCH", "manager1")); code != "FIN-0513" {
		t.Fatalf("batch vs wrong amount: %q", code)
	}
	db.DB.Exec("UPDATE " + schema + ".documents SET data = jsonb_set(data, '{amount}', '500') WHERE id = 'T7316-L6'")
	if err := MatchMarketplacePayout(tenantID, "T7316-L6", "T7316-BATCH", "manager1"); err != nil {
		t.Fatalf("batch payout: %v", err)
	}

	// Closed period: the line's reconciliation is frozen.
	old := time.Now().AddDate(-3, 0, 0)
	periodID, err = CreateAccountingPeriod(tenantID, "TEST-PERIOD-7316", old.Format("2006-01-02"), old.AddDate(0, 0, 5).Format("2006-01-02"), "system")
	if err != nil {
		t.Fatalf("old period: %v", err)
	}
	signAllCloseTasksForTest(t, tenantID, periodID)
	if err := CloseAccountingPeriod(tenantID, periodID, "system"); err != nil {
		t.Fatalf("close old period: %v", err)
	}
	line("T7316-L7", "Credit", 250, "", old.AddDate(0, 0, 1).Format("2006-01-02"))
	if code := validationCode(MatchBankLine(tenantID, "T7316-L7", []string{pS1}, "", "manager1")); code != "FIN-0511" {
		t.Fatalf("match in a closed period: %q, want FIN-0511", code)
	}
}
