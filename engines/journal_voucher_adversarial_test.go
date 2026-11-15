package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// Stage 49.5.7 - adversarial invariant tests on a JournalVoucher. A voucher's
// amounts live in its lines (a JSON text field), not in a column, so the
// tamper, replay and race cases are written against the lines.
//
// Already proven elsewhere: one decision per pending voucher
// (TestStage49OneDecisionPerPendingDocument), a reversal raised once under a
// race (TestAtomicityReverseJournalVoucher). This file adds the line-level cases.

const jvAdvPrefix = "TEST-JV4957"

type jvAdvFixture struct {
	t        *testing.T
	tenantID string
	schema   string
}

func newJVAdvFixture(t *testing.T) *jvAdvFixture {
	t.Helper()
	db.InitDB(testConnStr())
	f := &jvAdvFixture{t: t, tenantID: "default"}
	schema, err := db.GetTenantSchema(f.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	f.schema = schema
	clean := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'JournalVoucher' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE '" + jvAdvPrefix + "%')")
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE doctype = 'JournalVoucher' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE '" + jvAdvPrefix + "%')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE '" + jvAdvPrefix + "%'")
	}
	clean()
	t.Cleanup(clean)
	withSelfApproval(t, f.tenantID, SelfApprovalAnyone)
	return f
}

func (f *jvAdvFixture) create(name string, debit, credit int) string {
	f.t.Helper()
	id, err := CreateJournalVoucher(f.tenantID, "2026-07-25", jvAdvPrefix+" "+name,
		[]JournalVoucherLine{{AccountCode: "5100", Debit: float64(debit)}, {AccountCode: "1100", Credit: float64(credit)}}, "admin")
	if err != nil {
		f.t.Fatalf("CreateJournalVoucher: %v", err)
	}
	return id
}

func (f *jvAdvFixture) submit(id string) {
	f.t.Helper()
	if err := SubmitForApproval(f.tenantID, "JournalVoucher", id, "admin", RoleSuperAdmin); err != nil {
		f.t.Fatalf("SubmitForApproval: %v", err)
	}
}

func (f *jvAdvFixture) approve(id string) {
	f.t.Helper()
	if err := DecideApproval(f.tenantID, "JournalVoucher", id, "approver-jv4957", RoleSuperAdmin, "", "Approved", "ok"); err != nil {
		f.t.Fatalf("DecideApproval: %v", err)
	}
}

// setLines is what a record-form save does to a stored voucher: replaces the
// lines text and leaves everything else.
func (f *jvAdvFixture) setLines(id string, lines []JournalVoucherLine) {
	f.t.Helper()
	raw, _ := json.Marshal(lines)
	if _, err := db.DB.Exec(`UPDATE `+f.schema+`.documents SET data = jsonb_set(data, '{lines}', to_jsonb($1::text)) WHERE doctype = 'JournalVoucher' AND id = $2`, string(raw), id); err != nil {
		f.t.Fatal(err)
	}
}

func (f *jvAdvFixture) status(id string) string {
	f.t.Helper()
	_, st, err := fetchJournalVoucher(f.schema, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return st
}

func (f *jvAdvFixture) glSums(id string) (rows, debit, credit int64) {
	f.t.Helper()
	if err := db.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM `+f.schema+`.gl_postings WHERE document_type = 'JournalVoucher' AND document_id = $1`, id).Scan(&rows, &debit, &credit); err != nil {
		f.t.Fatal(err)
	}
	return
}

// A voucher's lines changed while it waits for approval, so they no longer
// balance, never reach the ledger: the approval cannot post it, and it does
// not half-post.
func TestStage4957JournalVoucherUnbalancedLinesNeverPost(t *testing.T) {
	f := newJVAdvFixture(t)
	id := f.create("unbalanced", 500, 500)
	f.submit(id)
	f.setLines(id, []JournalVoucherLine{{AccountCode: "5100", Debit: 500}, {AccountCode: "1100", Credit: 499}})
	f.approve(id)
	if rows, d, c := f.glSums(id); rows != 0 || d != 0 || c != 0 {
		t.Fatalf("an unbalanced voucher reached the ledger: rows=%d debit=%d credit=%d", rows, d, c)
	}
	if st := f.status(id); st == "Posted" {
		t.Fatalf("an unbalanced voucher is Posted")
	}
}

// The generic record API is the other way to put lines on a voucher. The
// shared save check refuses lines that do not balance, that carry a negative or
// two-sided amount, or fewer than two lines, and the stored total is the lines'
// own sum - a client-sent total_amount cannot understate what the lines move
// (the approval slab reads total_amount).
func TestStage4957JournalVoucherGenericSaveChecksLinesAndSetsTotal(t *testing.T) {
	f := newJVAdvFixture(t)
	save := func(lines []JournalVoucherLine, total interface{}) (map[string]interface{}, error) {
		raw, _ := json.Marshal(lines)
		payload := map[string]interface{}{"voucher_date": "2026-07-25", "narration": jvAdvPrefix + " generic", "lines": string(raw), "total_amount": total, "status": "Draft"}
		return payload, ValidateTransactionalRules(f.tenantID, "JournalVoucher", "", "", nil, payload)
	}
	for name, lines := range map[string][]JournalVoucherLine{
		"unbalanced":  {{AccountCode: "5100", Debit: 100}, {AccountCode: "1100", Credit: 99}},
		"negative":    {{AccountCode: "5100", Debit: 100}, {AccountCode: "1100", Credit: 200}, {AccountCode: "1200", Debit: -100}},
		"two sided":   {{AccountCode: "5100", Debit: 100, Credit: 100}, {AccountCode: "1100", Credit: 100}},
		"single line": {{AccountCode: "5100", Debit: 100}},
		"no amount":   {{AccountCode: "5100"}, {AccountCode: "1100"}},
		"no account":  {{Debit: 100}, {AccountCode: "1100", Credit: 100}},
	} {
		if _, err := save(lines, 100); err == nil {
			t.Errorf("%s lines were accepted by the generic save", name)
		}
	}
	payload, err := save([]JournalVoucherLine{{AccountCode: "5100", Debit: 9000000}, {AccountCode: "1100", Credit: 9000000}}, 1)
	if err != nil {
		t.Fatalf("balanced lines refused: %v", err)
	}
	if got, _ := parityNumber(payload["total_amount"]); got != 9000000 {
		t.Errorf("total_amount stayed %v; it must be the lines' own sum 9000000", payload["total_amount"])
	}
}

// Once posted, no line amount may change by a save - one figure, an added
// line, a swapped side, a line removed, or the same lines sent as an array.
func TestStage4957PostedJournalVoucherLineAmountsAreFrozen(t *testing.T) {
	f := newJVAdvFixture(t)
	id := f.create("posted", 700, 700)
	f.submit(id)
	f.approve(id)
	if st := f.status(id); st != "Posted" {
		t.Fatalf("voucher is %s, want Posted", st)
	}
	prior, st, err := fetchJournalVoucher(f.schema, id)
	if err != nil {
		t.Fatal(err)
	}
	for name, lines := range map[string][]JournalVoucherLine{
		"one amount":    {{AccountCode: "5100", Debit: 701}, {AccountCode: "1100", Credit: 700}},
		"both amounts":  {{AccountCode: "5100", Debit: 7000}, {AccountCode: "1100", Credit: 7000}},
		"added line":    {{AccountCode: "5100", Debit: 700}, {AccountCode: "1100", Credit: 700}, {AccountCode: "1200"}},
		"swapped sides": {{AccountCode: "5100", Credit: 700}, {AccountCode: "1100", Debit: 700}},
		"removed line":  {{AccountCode: "5100", Debit: 700}},
		"other account": {{AccountCode: "5100", Debit: 700}, {AccountCode: "1200", Credit: 700}},
	} {
		raw, _ := json.Marshal(lines)
		var arr []interface{}
		_ = json.Unmarshal(raw, &arr)
		for form, v := range map[string]interface{}{"text": string(raw), "array": arr} {
			payload := map[string]interface{}{"lines": v, "status": st}
			err := ValidateTransactionalRules(f.tenantID, "JournalVoucher", id, st, prior, payload)
			if err == nil || !strings.Contains(err.Error(), "posted") {
				t.Errorf("%s (%s): a save changed a posted voucher's lines: %v", name, form, err)
			}
		}
	}
	// The same lines sent back unchanged are not a change.
	same := map[string]interface{}{"lines": prior["lines"], "status": st}
	if err := ValidateTransactionalRules(f.tenantID, "JournalVoucher", id, st, prior, same); err != nil {
		t.Errorf("re-saving unchanged lines was refused: %v", err)
	}
}

// Approving twice, retrying a posted voucher, and eight retries of an approved
// one at once all leave the ledger with the voucher's amounts exactly once.
func TestStage4957JournalVoucherReplayAndRacePostOnce(t *testing.T) {
	f := newJVAdvFixture(t)
	id := f.create("replay", 321, 321)
	f.submit(id)
	f.approve(id)
	if err := DecideApproval(f.tenantID, "JournalVoucher", id, "approver-b-jv4957", RoleSuperAdmin, "", "Approved", "again"); err == nil {
		t.Errorf("a second approval of a decided voucher was accepted")
	}
	if err := RetryPostApprovedJournalVoucher(f.tenantID, id); err == nil {
		t.Errorf("a retry of a Posted voucher was accepted")
	}
	postApprovedJournalVoucher(f.tenantID, id) // the approval hook fired again
	rows, d, c := f.glSums(id)
	if rows != 2 || d != 32100 || c != 32100 {
		t.Fatalf("after replays: rows=%d debit=%d credit=%d paise, want 2 rows of 32100 each side", rows, d, c)
	}

	// An approved voucher whose first post did not happen, retried by eight callers.
	id2 := f.create("race", 654, 654)
	f.submit(id2)
	if _, err := db.DB.Exec(`UPDATE `+f.schema+`.documents SET status = 'Approved', data = jsonb_set(data, '{status}', '"Approved"') WHERE doctype = 'JournalVoucher' AND id = $1`, id2); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = RetryPostApprovedJournalVoucher(f.tenantID, id2)
			postApprovedJournalVoucher(f.tenantID, id2)
		}()
	}
	wg.Wait()
	rows, d, c = f.glSums(id2)
	if rows != 2 || d != 65400 || c != 65400 {
		t.Fatalf("after 8 concurrent posts: rows=%d debit=%d credit=%d paise, want 2 rows of 65400 each side", rows, d, c)
	}
	if st := f.status(id2); st != "Posted" {
		t.Errorf("status %s, want Posted", st)
	}
}

// A voucher and its reversal net to zero in the ledger, each posted balanced.
func TestStage4957JournalVoucherReversalNetsToZero(t *testing.T) {
	f := newJVAdvFixture(t)
	id := f.create("reverse", 1000, 1000)
	f.submit(id)
	f.approve(id)
	revID, err := ReverseJournalVoucher(f.tenantID, id, "admin")
	if err != nil {
		t.Fatal(err)
	}
	f.submit(revID)
	f.approve(revID)
	var net int64
	if err := db.DB.QueryRow(`SELECT COALESCE(SUM(debit),0) - COALESCE(SUM(credit),0) FROM `+f.schema+`.gl_postings WHERE document_type = 'JournalVoucher' AND document_id IN ($1, $2)`, id, revID).Scan(&net); err != nil {
		t.Fatal(err)
	}
	if net != 0 {
		t.Fatalf("original plus reversal leave %d paise in the ledger", net)
	}
	for _, v := range []string{id, revID} {
		if _, d, c := f.glSums(v); d != c {
			t.Errorf("%s posted unbalanced: debit %d credit %d", v, d, c)
		}
	}
	if st := f.status(id); st != "Reversed" {
		t.Errorf("original is %s, want Reversed", st)
	}
}
