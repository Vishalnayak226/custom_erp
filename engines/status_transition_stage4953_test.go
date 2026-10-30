package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// 49.5.3 (a): the money / stock / tax doctypes that still failed open are
// strict, and their rule sets cover every status their enum declares, so no
// live document is left with no way on. (b): a reversal is a compensating
// document - the original's rows are never edited away.

// stage4953Matrix is the generic-API rule set each doctype carries, as
// "from>to" with the reason flag, and the statuses only the engine sets.
var stage4953Matrix = map[string]struct {
	rules      map[string]string // "from>to" -> requires_reason_code
	engineOnly []string
}{
	"JournalVoucher":          {approval4953("Draft>Recurring Template:No", "Recurring Template>Draft:No"), []string{"Posted", "Reversed"}},
	"InventoryAdjustment":     {approval4953(), []string{"Posted"}},
	"PurchaseReturn":          {approval4953("Draft>Cancelled:Yes", "Rejected>Cancelled:Yes", "Approved>Cancelled:Yes"), []string{"Posted"}},
	"DeliveryChallan":         {approval4953("Draft>Cancelled:Yes", "Approved>Cancelled:Yes"), []string{"Dispatched", "Partly Returned", "Returned", "Closed"}},
	"MaterialIssue":           {approval4953(), []string{"Issued", "Partly Returned", "Returned"}},
	"IntercompanyTransaction": {approval4953(), []string{"Posted", "Partially Posted"}},
	"ChallanWriteOff":         {approval4953(), []string{"Posted"}},
	"LoyaltyPointAdjustment":  {approval4953(), []string{"Posted"}},
	"IRNCancellation":         {approval4953(), []string{"Applied"}},
	"IRNManualImport":         {approval4953(), []string{"Applied"}},
	"VendorCreditNote":        {rules4953("Draft>Posted:No"), nil},
	"Payslip":                 {rules4953("Draft>Posted:No"), nil},
	"LandedCostVoucher":       {rules4953(), []string{"Applied"}},
}

func rules4953(extra ...string) map[string]string {
	out := map[string]string{}
	for _, e := range extra {
		kv := strings.SplitN(e, ":", 2)
		out[kv[0]] = kv[1]
	}
	return out
}

func approval4953(extra ...string) map[string]string {
	out := rules4953("Draft>Pending Approval:No", "Draft>Approved:No", "Pending Approval>Draft:No", "Pending Approval>Approved:No",
		"Pending Approval>Rejected:Yes", "Rejected>Draft:No", "Rejected>Pending Approval:No", "Approved>Pending Approval:No")
	for k, v := range rules4953(extra...) {
		out[k] = v
	}
	return out
}

func TestStage4953PostingDoctypesAreStrictAndCovered(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var seeded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.documents WHERE id = 'STR-JournalVoucher-Draft-Pending Approval')`).Scan(&seeded)
	if !seeded {
		t.Skip("db/migrations_stage130_16_3_strict_posting_transitions.sql (Stage 49.5.3) has not been applied")
	}
	names := make([]string, 0, len(stage4953Matrix))
	for n := range stage4953Matrix {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, doctype := range names {
		want := stage4953Matrix[doctype]
		var strict bool
		var options string
		if err := db.DB.QueryRow(`SELECT m.strict_status_transitions, f.options FROM `+schema+`.doctype_meta m JOIN `+schema+`.doctype_fields f ON f.doctype_name = m.name AND f.fieldname = 'status' WHERE m.name = $1`, doctype).Scan(&strict, &options); err != nil {
			t.Fatalf("%s: %v", doctype, err)
		}
		if !strict {
			t.Errorf("%s is not strict", doctype)
		}
		// The rule set in the database is exactly the matrix.
		rows, err := db.DB.Query(`SELECT data->>'from_status', data->>'to_status', COALESCE(data->>'requires_reason_code', 'No') FROM `+schema+`.documents
			WHERE doctype = 'StatusTransitionRule' AND status = 'Active' AND deleted_at IS NULL AND data->>'entity' = $1`, doctype)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for rows.Next() {
			var from, to, reason string
			_ = rows.Scan(&from, &to, &reason)
			got[from+">"+to] = reason
		}
		rows.Close()
		for k, v := range want.rules {
			if got[k] != v {
				t.Errorf("%s: rule %s wants reason=%s, got %q", doctype, k, v, got[k])
			}
		}
		for k := range got {
			if _, ok := want.rules[k]; !ok {
				t.Errorf("%s: unexpected rule %s", doctype, k)
			}
		}
		// Every declared status is covered: a rule in or out, engine-only,
		// or Draft (the create status) when the document's own action moves
		// it out (LandedCostVoucher: Draft -> Applied by Apply only).
		covered := map[string]bool{}
		for k := range want.rules {
			ft := strings.SplitN(k, ">", 2)
			covered[ft[0]], covered[ft[1]] = true, true
		}
		for _, s := range want.engineOnly {
			covered[s] = true
		}
		if len(want.engineOnly) > 0 {
			covered["Draft"] = true
		}
		for _, s := range strings.Split(options, ",") {
			s = strings.TrimSpace(s)
			if s != "" && !covered[s] {
				t.Errorf("%s: declared status %q has no rule and is not engine-only - a document there would be stuck", doctype, s)
			}
		}
		// The engine applies it: every rule passes (with a reason where one
		// is needed), an unlisted move and a missing reason are refused.
		for k, reason := range want.rules {
			ft := strings.SplitN(k, ">", 2)
			payload := map[string]interface{}{}
			if reason == "Yes" {
				if err := ValidateStatusTransition("default", doctype, ft[0], ft[1], payload); err == nil {
					t.Errorf("%s %s without a reason must be refused", doctype, k)
				}
				payload["reason_code"] = "TEST-4953"
			}
			if err := ValidateStatusTransition("default", doctype, ft[0], ft[1], payload); err != nil {
				t.Errorf("%s %s: %v", doctype, k, err)
			}
		}
		for _, s := range want.engineOnly {
			err := ValidateStatusTransition("default", doctype, "Draft", s, map[string]interface{}{})
			var verr *ValidationError
			if !errors.As(err, &verr) || verr.Code != "GLOBAL-0019" {
				t.Errorf("%s: a plain save Draft -> %s must be refused with GLOBAL-0019, got %v", doctype, s, err)
			}
		}
	}
}

// TestStage4953UnruledStatusDoctypesListed names the doctypes that still
// fail open (a Select status, not strict, no rule) and proves none of them
// is a posting doctype any more; the list itself is the report's "left".
func TestStage4953UnruledStatusDoctypesListed(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.DB.Query(`SELECT m.name, f.options FROM ` + schema + `.doctype_meta m JOIN ` + schema + `.doctype_fields f ON f.doctype_name = m.name AND f.fieldname = 'status' AND f.fieldtype = 'Select'
		WHERE NOT m.strict_status_transitions AND NOT EXISTS (SELECT 1 FROM ` + schema + `.documents d WHERE d.doctype = 'StatusTransitionRule' AND d.deleted_at IS NULL AND d.data->>'entity' = m.name)
		ORDER BY m.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var open []string
	for rows.Next() {
		var name, options string
		_ = rows.Scan(&name, &options)
		if len(strings.Split(options, ",")) > 1 {
			open = append(open, name)
		}
		if _, posting := stage4953Matrix[name]; posting {
			t.Errorf("%s still fails open", name)
		}
	}
	t.Logf("fail-open doctypes with a multi-status Select (%d): %s", len(open), strings.Join(open, ", "))
}

// TestStage4953PostedGRNIsNotCancelledBySave: a save cannot take an
// Approved (posted) goods receipt back out of the accounts - the purchase
// return is its reversal - while a pending receipt still cancels and a
// posted one can still be edited.
func TestStage4953PostedGRNIsNotCancelledBySave(t *testing.T) {
	data := map[string]interface{}{"po_id": "PO-1", "received_items": "[]"}
	with := func(status string) map[string]interface{} {
		return map[string]interface{}{"status": status, "po_id": "PO-1", "received_items": "[]", "reason_code": "X"}
	}
	for _, to := range []string{"Cancelled", "Draft", "Rejected"} {
		err := validatePostedAmountsFrozen("GRN", "GRN-1", "Approved", data, with(to))
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != "FIN-0844" {
			t.Errorf("GRN Approved -> %s by a save: %v, want FIN-0844", to, err)
		}
	}
	if err := validatePostedAmountsFrozen("GRN", "GRN-1", "Pending", data, with("Cancelled")); err != nil {
		t.Errorf("a pending receipt still cancels: %v", err)
	}
	if err := validatePostedAmountsFrozen("GRN", "GRN-1", "Approved", data, map[string]interface{}{"status": "Approved", "po_id": "PO-1", "note": "edited"}); err != nil {
		t.Errorf("an ordinary edit of a posted receipt: %v", err)
	}
}

func TestStage4953ReversalsAreCompensatingEvents(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	glRows := func(id string) string {
		rows, err := db.DB.Query(`SELECT posting_id::text, account_code, debit, credit FROM `+schema+`.gl_postings WHERE document_type = 'JournalVoucher' AND document_id = $1 ORDER BY posting_id`, id)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var pid, acct string
			var dr, cr int64
			_ = rows.Scan(&pid, &acct, &dr, &cr)
			out = append(out, fmt.Sprintf("%s|%s|%d|%d", pid, acct, dr, cr))
		}
		return strings.Join(out, ";")
	}

	// A posted journal voucher: its reversal is a new voucher with the lines
	// swapped; the original keeps every GL row and its lines.
	cleanupJV := func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.gl_postings WHERE document_type = 'JournalVoucher' AND document_id IN (SELECT id FROM ` + schema + `.documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-4953%')`)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.documents WHERE doctype = 'JournalVoucher' AND (data->>'narration' LIKE 'TEST-4953%' OR data->>'narration' LIKE 'Reversal of %' AND data->>'reversed_from' IN (SELECT id FROM ` + schema + `.documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-4953%'))`)
	}
	cleanupJV()
	t.Cleanup(cleanupJV)
	jv, err := CreateJournalVoucher("default", "2026-07-25", "TEST-4953 reversal", []JournalVoucherLine{{AccountCode: "5100", Debit: 4953}, {AccountCode: "1100", Credit: 4953}}, "manager1")
	if err != nil {
		t.Fatal(err)
	}
	if err := SubmitForApproval("default", "JournalVoucher", jv, "manager1", "Store Manager"); err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval("default", "JournalVoucher", jv, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	before := glRows(jv)
	if before == "" {
		t.Fatal("the voucher posted no GL rows")
	}
	origData, _, _ := fetchJournalVoucher(schema, jv)
	origLines, _ := json.Marshal(origData["lines"])
	rev, err := ReverseJournalVoucher("default", jv, "manager1")
	if err != nil {
		t.Fatal(err)
	}
	if err := SubmitForApproval("default", "JournalVoucher", rev, "manager1", "Store Manager"); err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval("default", "JournalVoucher", rev, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	if after := glRows(jv); after != before {
		t.Fatalf("the original voucher's GL rows changed:\n%s\n%s", before, after)
	}
	data, status, _ := fetchJournalVoucher(schema, jv)
	lines, _ := json.Marshal(data["lines"])
	if status != "Reversed" || string(lines) != string(origLines) || data["reversed_by"] != rev {
		t.Fatalf("original after reversal: status %s reversed_by %v lines unchanged %v", status, data["reversed_by"], string(lines) == string(origLines))
	}
	var revDr, revCr int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM `+schema+`.gl_postings WHERE document_type = 'JournalVoucher' AND document_id = $1`, rev).Scan(&revDr, &revCr)
	if revDr != 495300 || revCr != 495300 {
		t.Fatalf("the reversal posted %d / %d, want 495300 each", revDr, revCr)
	}
	if glRows(rev) == before {
		t.Fatal("the reversal reused the original's rows")
	}

	// A posted inventory adjustment: its reversal is a new adjustment with
	// the opposite quantity; the original keeps its lines and quantities.
	const sku, loc, bin = "T4953-SKU", "T4953-LOC", "T4953-BIN"
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", schema), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cleanupIA := func() {
		for _, q := range []string{
			`DELETE FROM {s}.documents WHERE doctype = 'InventoryAdjustment' AND data->>'location' = '` + loc + `'`,
			`DELETE FROM {s}.documents WHERE id IN ('` + sku + `', '` + loc + `')`,
			`DELETE FROM {s}.inventory_availability WHERE sku = '` + sku + `'`,
			`DELETE FROM {s}.bin_stock WHERE sku = '` + sku + `'`,
			`DELETE FROM {s}.approval_rules WHERE doctype = 'InventoryAdjustment'`,
		} {
			_, _ = db.DB.Exec(strings.ReplaceAll(q, "{s}", schema))
		}
	}
	cleanupIA()
	t.Cleanup(cleanupIA)
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', '{"code":"`+sku+`","name":"4953 item","cost_price":40}', 'Active', 'system')`, sku)
	exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', '{"code":"`+loc+`","name":"4953 store"}', 'Active', 'system')`, loc)
	exec(`INSERT INTO {s}.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)`, sku, loc)
	exec(`INSERT INTO {s}.bin_stock (bin_code, sku, location_code, qty, condition) VALUES ($1, $2, $3, 10, 'Good')`, bin, sku, loc)
	onHand := func() int {
		var n int
		_ = db.DB.QueryRow(`SELECT on_hand FROM `+schema+`.inventory_availability WHERE sku = $1 AND location_code = $2`, sku, loc).Scan(&n)
		return n
	}
	first, status, err := CreateInventoryAdjustment("default", InventoryAdjustmentInput{Location: loc, Lines: []InventoryAdjustmentLine{{SKU: sku, Qty: -2, ReasonCode: "IA-DAMAGE", Bin: bin}}}, "maker", RoleStoreManager)
	if err != nil || status != "Posted" {
		t.Fatalf("create: %s %v", status, err)
	}
	if onHand() != 8 {
		t.Fatalf("after the adjustment: %d on hand", onHand())
	}
	var raw []byte
	_ = db.DB.QueryRow(`SELECT data FROM `+schema+`.documents WHERE id = $1`, first).Scan(&raw)
	var origIA map[string]interface{}
	_ = json.Unmarshal(raw, &origIA)
	revIA, status, err := ReverseInventoryAdjustment("default", first, "counted wrong", "maker", RoleStoreManager)
	if err != nil || status != "Posted" || revIA == first {
		t.Fatalf("reverse: %s %s %v", revIA, status, err)
	}
	if onHand() != 10 {
		t.Fatalf("after the reversal: %d on hand, want 10", onHand())
	}
	var rawAfter []byte
	var statusAfter string
	_ = db.DB.QueryRow(`SELECT data, status FROM `+schema+`.documents WHERE id = $1`, first).Scan(&rawAfter, &statusAfter)
	var afterIA map[string]interface{}
	_ = json.Unmarshal(rawAfter, &afterIA)
	if afterIA["reversed_by"] != revIA || statusAfter != "Posted" {
		t.Fatalf("the original is marked reversed_by=%v and stays Posted (%s)", afterIA["reversed_by"], statusAfter)
	}
	delete(afterIA, "reversed_by")
	delete(afterIA, "reversal_reason")
	for k := range origIA {
		a, _ := json.Marshal(origIA[k])
		b, _ := json.Marshal(afterIA[k])
		if string(a) != string(b) {
			t.Errorf("the original adjustment's %s was edited: %s -> %s", k, a, b)
		}
	}
	var revRaw []byte
	_ = db.DB.QueryRow(`SELECT data FROM `+schema+`.documents WHERE id = $1`, revIA).Scan(&revRaw)
	var revData map[string]interface{}
	_ = json.Unmarshal(revRaw, &revData)
	revLines := jsonLines(revData["items"]) // lines are stored as encoded JSON under "items"
	if revData["reversal_of"] != first || len(revLines) != 1 {
		t.Fatalf("the reversal document: reversal_of=%v lines=%d", revData["reversal_of"], len(revLines))
	}
	if q := numFromInterface(revLines[0]["qty"]); q != 2 {
		t.Fatalf("the reversal's line qty = %v, want +2", revLines[0]["qty"])
	}
}
