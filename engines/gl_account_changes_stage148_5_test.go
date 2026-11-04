package engines

import (
	"errors"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 148.5: the chart of accounts maintained from the app, through the
// approval engine.

// stage1485Change files a GLAccountChange, sends it for approval and has an
// admin approve it, as the screens do.
func stage1485Change(t *testing.T, tenantID string, insert func(id, doctype, status, createdBy string, data map[string]interface{}), id string, data map[string]interface{}) {
	t.Helper()
	if err := validateGLAccountChange(tenantID, data); err != nil {
		t.Fatalf("%s should save: %v", id, err)
	}
	data["code"], data["status"] = id, "Draft"
	insert(id, GLAccountChangeDoctype, "Draft", "accountant1", data)
	if err := SubmitForApproval(tenantID, GLAccountChangeDoctype, id, "accountant1", "Accountant"); err != nil {
		t.Fatalf("submit %s: %v", id, err)
	}
	if err := DecideApproval(tenantID, GLAccountChangeDoctype, id, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatalf("approve %s: %v", id, err)
	}
}

func stage1485Account(t *testing.T, schema, code string) (name, typ, parent string, blocked, noManual, found bool) {
	t.Helper()
	var p *string
	err := db.DB.QueryRow("SELECT account_name, account_type, parent_code, blocked, no_manual_posting FROM "+schema+".gl_accounts WHERE account_code = $1", code).
		Scan(&name, &typ, &p, &blocked, &noManual)
	if p != nil {
		parent = *p
	}
	return name, typ, parent, blocked, noManual, err == nil
}

func TestStage1485AccountAddedAfterApproval(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)

	// Nothing is added before the approval.
	add := map[string]interface{}{"action": "Add", "account_code": "7150", "account_name": "Packaging Expense", "account_type": "Expense", "parent_code": "5100"}
	if err := validateGLAccountChange(tenantID, add); err != nil {
		t.Fatalf("a valid add: %v", err)
	}
	add["code"], add["status"] = "GLC-1485-A", "Draft"
	insert("GLC-1485-A", GLAccountChangeDoctype, "Draft", "accountant1", add)
	if err := SubmitForApproval(tenantID, GLAccountChangeDoctype, "GLC-1485-A", "accountant1", "Accountant"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, found := stage1485Account(t, schema, "7150"); found {
		t.Fatalf("the account exists before the change was approved")
	}
	if err := DecideApproval(tenantID, GLAccountChangeDoctype, "GLC-1485-A", "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	name, typ, parent, blocked, noManual, found := stage1485Account(t, schema, "7150")
	if !found || name != "Packaging Expense" || typ != "Expense" || parent != "5100" || blocked || noManual {
		t.Fatalf("approved add: %q %q parent %q blocked %v noManual %v found %v", name, typ, parent, blocked, noManual, found)
	}
	// It can be posted to like any account, and it shows in the trial balance with its parent.
	if err := PostDoubleEntry(tenantID, "Test", "T1485-1", map[string]int64{"7150": 5000}, map[string]int64{"1100": 5000}, "", "T1485-1"); err != nil {
		t.Fatalf("post to the new account: %v", err)
	}
	tb, err := GetTrialBalance(tenantID, BusinessToday())
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, b := range tb["balances"].([]AccountBalance) {
		if b.Code == "7150" {
			seen = b.Parent == "5100" && b.Debit == 50
		}
	}
	if !seen {
		t.Fatalf("trial balance does not show 7150 under 5100 with its balance")
	}

	// Rename and flag through a Change; the code never changes.
	stage1485Change(t, tenantID, insert, "GLC-1485-B", map[string]interface{}{"action": "Change", "account_code": "7150", "account_name": "Packing Costs", "no_manual_posting": "Yes", "clear_parent": "Yes"})
	name, _, parent, _, noManual, _ = stage1485Account(t, schema, "7150")
	if name != "Packing Costs" || parent != "" || !noManual {
		t.Fatalf("approved change: %q parent %q noManual %v", name, parent, noManual)
	}

	// Refused at save: a reused code, a missing parent, a loop, an unknown action.
	for label, bad := range map[string]map[string]interface{}{
		"reused code":    {"action": "Add", "account_code": "4100", "account_name": "Again", "account_type": "Revenue"},
		"missing parent": {"action": "Change", "account_code": "7150", "parent_code": "9999"},
		"own parent":     {"action": "Change", "account_code": "7150", "parent_code": "7150"},
		"unknown":        {"action": "Delete", "account_code": "7150"},
		"no account":     {"action": "Block", "account_code": "9998"},
	} {
		var ve *ValidationError
		if err := validateGLAccountChange(tenantID, bad); !errors.As(err, &ve) || ve.Code != "FIN-0873" {
			t.Errorf("%s should be refused with FIN-0873, got %v", label, err)
		}
	}
	stage1485Change(t, tenantID, insert, "GLC-1485-C", map[string]interface{}{"action": "Change", "account_code": "5100", "parent_code": "7150"})
	var ve *ValidationError
	if err := validateGLAccountChange(tenantID, map[string]interface{}{"action": "Change", "account_code": "7150", "parent_code": "5100"}); !errors.As(err, &ve) {
		t.Errorf("a parent loop should be refused, got %v", err)
	}
}

func TestStage1485ControlAccountRefusesManualJournal(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	stage1485Change(t, tenantID, insert, "GLC-1485-D", map[string]interface{}{"action": "Change", "account_code": "1300", "no_manual_posting": "Yes"})
	if _, _, _, _, noManual, _ := stage1485Account(t, schema, "1300"); !noManual {
		t.Fatalf("the flag was not set on 1300")
	}
	lines := []JournalVoucherLine{{AccountCode: "1300", Debit: 100}, {AccountCode: "4100", Credit: 100}}
	_, err := CreateJournalVoucher(tenantID, BusinessToday(), "manual to a control account", lines, "manager1")
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != "FIN-0872" || !strings.Contains(err.Error(), "1300") {
		t.Fatalf("a manual journal on a control account must be refused with FIN-0872, got %v", err)
	}
	// Other accounts still take a manual journal; a system posting still reaches 1300.
	if _, err := CreateJournalVoucher(tenantID, BusinessToday(), "manual elsewhere", []JournalVoucherLine{{AccountCode: "5100", Debit: 100}, {AccountCode: "1100", Credit: 100}}, "manager1"); err != nil {
		t.Fatalf("a manual journal on ordinary accounts: %v", err)
	}
	if err := PostDoubleEntry(tenantID, "SalesInvoice", "SI-1485", map[string]int64{"1300": 10000}, map[string]int64{"4100": 10000}, "", "SI-1485:POST"); err != nil {
		t.Fatalf("a system posting must still reach a control account: %v", err)
	}
	// Lifting the flag lets the manual journal through again.
	stage1485Change(t, tenantID, insert, "GLC-1485-E", map[string]interface{}{"action": "Change", "account_code": "1300", "no_manual_posting": "No"})
	if _, err := CreateJournalVoucher(tenantID, BusinessToday(), "manual after lifting", lines, "manager1"); err != nil {
		t.Fatalf("after the flag is lifted: %v", err)
	}
}

// The flag has to hold at posting too: a voucher saved before the flag was set
// goes to the ledger through postJournalVoucherTx, not CreateJournalVoucher. A
// reversal of a voucher already posted still gets through, or it could not be undone.
func TestStage1485ControlAccountRefusesAJournalSavedBeforeTheFlag(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	withSelfApproval(t, tenantID, SelfApprovalAnyone)
	lines := []JournalVoucherLine{{AccountCode: "1300", Debit: 100}, {AccountCode: "4100", Credit: 100}}
	posted, err := CreateJournalVoucher(tenantID, BusinessToday(), "posted before the flag", lines, "manager1")
	if err != nil {
		t.Fatal(err)
	}
	if err := SubmitForApproval(tenantID, "JournalVoucher", posted, "manager1", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(tenantID, "JournalVoucher", posted, "approver-1485", RoleSuperAdmin, "", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	waiting, err := CreateJournalVoucher(tenantID, BusinessToday(), "saved before the flag", lines, "manager1")
	if err != nil {
		t.Fatal(err)
	}
	stage1485Change(t, tenantID, insert, "GLC-1485-J", map[string]interface{}{"action": "Change", "account_code": "1300", "no_manual_posting": "Yes"})

	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	data, _, err := lockJournalVoucherTx(tx, schema, waiting)
	if err != nil {
		t.Fatal(err)
	}
	err = postJournalVoucherTx(tx, tenantID, schema, waiting, data)
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != "FIN-0872" {
		t.Fatalf("a journal saved before the flag must be refused at posting with FIN-0872, got %v", err)
	}
	tx.Rollback()

	if _, err := ReverseJournalVoucher(tenantID, posted, "manager1"); err != nil {
		t.Fatalf("a posted journal must still reverse onto a control account: %v", err)
	}
}

func TestStage1485BlockedAccountRefusesPosting(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	if err := PostDoubleEntry(tenantID, "Test", "T1485-2", map[string]int64{"1100": 7000}, map[string]int64{"4100": 7000}, "", "T1485-2"); err != nil {
		t.Fatal(err)
	}
	stage1485Change(t, tenantID, insert, "GLC-1485-F", map[string]interface{}{"action": "Block", "account_code": "4100"})
	if _, _, _, blocked, _, _ := stage1485Account(t, schema, "4100"); !blocked {
		t.Fatalf("4100 was not blocked")
	}
	err := PostDoubleEntry(tenantID, "Test", "T1485-3", map[string]int64{"1100": 100}, map[string]int64{"4100": 100}, "", "T1485-3")
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != "FIN-0870" {
		t.Fatalf("posting to a blocked account must fail with FIN-0870, got %v", err)
	}
	if n := len(stage1484Posted(t, schema, "Test", "T1485-3")); n != 0 {
		t.Fatalf("a refused posting left %d GL rows", n)
	}
	// History and balance stay readable; a cancellation can still undo an old document.
	tb, err := GetTrialBalance(tenantID, BusinessToday())
	if err != nil {
		t.Fatal(err)
	}
	var credit float64
	var flagged bool
	for _, b := range tb["balances"].([]AccountBalance) {
		if b.Code == "4100" {
			credit, flagged = b.Credit, b.Blocked
		}
	}
	if credit != 70 || !flagged {
		t.Fatalf("the blocked account's balance %v / flag %v", credit, flagged)
	}
	if err := PostDoubleEntry(tenantID, "Test", "T1485-2", map[string]int64{"4100": 7000}, map[string]int64{"1100": 7000}, "", "Test:T1485-2:CANCEL"); err != nil {
		t.Fatalf("a cancellation must reach a blocked account: %v", err)
	}
	// There is no delete action: the change request offers Add, Change, Block and Unblock only.
	if err := validateGLAccountChange(tenantID, map[string]interface{}{"action": "Delete", "account_code": "4100"}); err == nil {
		t.Fatalf("a Delete request was accepted")
	}
	// Unblocking lets it take postings again.
	stage1485Change(t, tenantID, insert, "GLC-1485-G", map[string]interface{}{"action": "Unblock", "account_code": "4100"})
	if err := PostDoubleEntry(tenantID, "Test", "T1485-4", map[string]int64{"1100": 100}, map[string]int64{"4100": 100}, "", "T1485-4"); err != nil {
		t.Fatalf("after unblocking: %v", err)
	}
	// A missing account is refused clearly too.
	if err := PostDoubleEntry(tenantID, "Test", "T1485-5", map[string]int64{"1100": 100}, map[string]int64{"4999": 100}, "", "T1485-5"); err == nil || !strings.Contains(err.Error(), "not in the chart") {
		t.Fatalf("a missing account: %v", err)
	}
}
