package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 170.1: with finance.gl_source_line on, a sales invoice posts one row
// per account per line, each naming its line, with the same totals; off, it
// posts one row per account with no line. GL Drill-down shows the line.
func TestStage170GLSourceLine(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	ids := []string{"T1701-ON", "T1701-OFF"}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		_ = SetSetting(tenantID, SettingGLSourceLine, "false", "test")
		db.DB.Exec("DELETE FROM "+schema+".system_settings WHERE key = $1", SettingGLSourceLine)
	}
	cleanup()
	defer cleanup()
	insert := func(id string) {
		raw, _ := json.Marshal(map[string]interface{}{"invoice_number": id, "total_amount": 1000, "location": "HO",
			"items": []map[string]interface{}{
				{"sku": "T1701-NOSTOCK-A", "qty": 1, "amount": 300},
				{"sku": "T1701-NOSTOCK-B", "qty": 1, "amount": 700},
			}})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Draft', 'admin')", id, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	type row struct {
		account, line string
		debit, credit int64
	}
	rowsOf := func(id string) []row {
		rs, err := db.DB.Query("SELECT account_code, COALESCE(source_line_id,''), debit, credit FROM "+schema+".gl_postings WHERE document_id = $1 ORDER BY account_code, source_line_id", id)
		if err != nil {
			t.Fatalf("rows: %v", err)
		}
		defer rs.Close()
		var out []row
		for rs.Next() {
			var r row
			rs.Scan(&r.account, &r.line, &r.debit, &r.credit)
			out = append(out, r)
		}
		return out
	}

	// Off (the default): one row per account, no line named.
	insert("T1701-OFF")
	if _, err := PostSalesInvoice(tenantID, "T1701-OFF", "admin"); err != nil {
		t.Fatalf("post with the setting off: %v", err)
	}
	off := rowsOf("T1701-OFF")
	if len(off) != 2 {
		t.Fatalf("setting off: %d rows, want 2 (one per account): %+v", len(off), off)
	}
	for _, r := range off {
		if r.line != "" {
			t.Errorf("setting off: row %+v names a line", r)
		}
	}

	// On: one row per account per line, split 300 / 700, same totals.
	if err := SetSetting(tenantID, SettingGLSourceLine, "true", "test"); err != nil {
		t.Fatalf("turn the setting on: %v", err)
	}
	insert("T1701-ON")
	if _, err := PostSalesInvoice(tenantID, "T1701-ON", "admin"); err != nil {
		t.Fatalf("post with the setting on: %v", err)
	}
	on := rowsOf("T1701-ON")
	if len(on) != 4 {
		t.Fatalf("setting on: %d rows, want 4 (2 accounts x 2 lines): %+v", len(on), on)
	}
	byLine := map[string]int64{}
	var sumD, sumC int64
	for _, r := range on {
		sumD += r.debit
		sumC += r.credit
		if r.account == "1300" {
			byLine[r.line] += r.debit
		}
	}
	if byLine["1"] != 30000 || byLine["2"] != 70000 {
		t.Errorf("receivable by line = %v paise, want 1: 30000, 2: 70000", byLine)
	}
	if sumD != 100000 || sumC != 100000 {
		t.Errorf("totals debit %d / credit %d paise, want 100000 each", sumD, sumC)
	}

	// GL Drill-down shows the line on the invoice's rows.
	book, err := getGLAccountBook(tenantID, "1300")
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range book {
		if r["document_id"] == "T1701-ON" {
			seen[r["source_line_id"].(string)] = true
		}
	}
	if !seen["1"] || !seen["2"] {
		t.Errorf("GL drill-down lines for the invoice = %v, want 1 and 2", seen)
	}
}

// spreadByShares always adds up to the total, the last weighted share taking
// the remainder, and skips lines with no weight.
func TestStage170SpreadByShares(t *testing.T) {
	got := spreadByShares(100, []sourceLineShare{{ID: "1", Weight: 1}, {ID: "2", Weight: 0}, {ID: "3", Weight: 2}})
	if got[0] != 33 || got[1] != 0 || got[2] != 67 {
		t.Errorf("spread = %v, want [33 0 67]", got)
	}
	if got := spreadByShares(100, []sourceLineShare{{ID: "1", Weight: 0}}); got[0] != 0 {
		t.Errorf("no weight: %v, want [0]", got)
	}
}

// glSourceRows is account -> source line -> net (debit - credit) paise over a
// document's rows whose key matches keyLike.
func glSourceRows(t *testing.T, schema, docID, keyLike string) map[string]map[string]int64 {
	t.Helper()
	rs, err := db.DB.Query("SELECT account_code, COALESCE(source_line_id,''), debit - credit FROM "+schema+".gl_postings WHERE document_id = $1 AND idempotency_key LIKE $2", docID, keyLike)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	defer rs.Close()
	out := map[string]map[string]int64{}
	for rs.Next() {
		var acct, line string
		var net int64
		if err := rs.Scan(&acct, &line, &net); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if out[acct] == nil {
			out[acct] = map[string]int64{}
		}
		out[acct][line] += net
	}
	return out
}

func wantSourceLines(t *testing.T, what string, got map[string]map[string]int64, acct string, want map[string]int64) {
	t.Helper()
	for line, v := range want {
		if got[acct][line] != v {
			t.Errorf("%s: %s line %q = %d paise, want %d (all: %v)", what, acct, line, got[acct][line], v, got[acct])
		}
	}
	if len(got[acct]) != len(want) {
		t.Errorf("%s: %s has lines %v, want exactly %v", what, acct, got[acct], want)
	}
}

// 170.1 gap: with the setting on, a GRN's receipt, a till sale (revenue, cost,
// exempt reclass), a sales invoice's cost and a bill without a PO each name the line every row came
// from, and still balance.
func TestStage170SourceLinesOnGRNPOSAndExpenseBills(t *testing.T) {
	e := newExp19(t)
	if err := SetSetting(e.tenant, SettingGLSourceLine, "true", "test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = SetSetting(e.tenant, SettingGLSourceLine, "false", "test")
		db.DB.Exec("DELETE FROM "+e.schema+".system_settings WHERE key = $1", SettingGLSourceLine)
	})
	a, b := e.item(nil), e.item(nil)

	// GRN: line 1 is 2 x 100, line 2 is 1 x 50.
	lines := []mc1481Line{{sku: a, qty: 2, rate: 100}, {sku: b, qty: 1, rate: 50}}
	grn, _ := e.grnDocs(e.loc, lines)
	if err := e.postGRN(e.loc, grn, lines); err != nil {
		t.Fatalf("receive: %v", err)
	}
	g := glSourceRows(t, e.schema, grn, "GRN:%RECEIPT_COST%")
	wantSourceLines(t, "GRN", g, "1200", map[string]int64{"1": 20000, "2": 5000})
	wantSourceLines(t, "GRN", g, "2100", map[string]int64{"1": -20000, "2": -5000})
	e.wantBalanced(grn)

	// Till sale: line 1 sells A for 1000, line 2 sells B for 3000 (both exempt).
	cart := e.id("CART")
	e.doc(cart, "POSCart", "Processing", map[string]interface{}{"location": e.loc, "payment_mode": "Cash", "gst_breakdown": map[string]interface{}{"exempt_amount": 4000.0}, "items": []map[string]interface{}{
		{"sku": a, "qty": 1, "sale_price": 1000.0}, {"sku": b, "qty": 1, "sale_price": 3000.0}}})
	if _, _, err := FinalizePOSCheckout(e.tenant, cart, ""); err != nil {
		t.Fatalf("sell: %v", err)
	}
	rev := glSourceRows(t, e.schema, cart, "%:SALE_REVENUE")
	wantSourceLines(t, "revenue", rev, "4100", map[string]int64{"1": -100000, "2": -300000})
	wantSourceLines(t, "revenue", rev, "1100", map[string]int64{"1": 100000, "2": 300000})
	cogs := glSourceRows(t, e.schema, cart, "%:SALE_COGS")
	wantSourceLines(t, "cost", cogs, "5100", map[string]int64{"1": 10000, "2": 5000})
	ex := glSourceRows(t, e.schema, cart, "%:SALE_EXEMPT")
	wantSourceLines(t, "exempt", ex, "4100", map[string]int64{"1": 100000, "2": 300000})
	e.wantBalanced(cart)

	// Sales invoice: line 1 is a service, line 2 issues the last A from stock,
	// so its cost names line 2.
	inv := e.id("SINV")
	e.doc(inv, "SalesInvoice", "Draft", map[string]interface{}{"invoice_number": inv, "total_amount": 1500.0, "location": e.loc, "items": []map[string]interface{}{
		{"sku": inv + "-SVC", "qty": 1, "amount": 300.0}, {"sku": a, "qty": 1, "amount": 1200.0}}})
	if _, err := PostSalesInvoice(e.tenant, inv, "admin"); err != nil {
		t.Fatalf("invoice: %v", err)
	}
	ic := glSourceRows(t, e.schema, inv, "%:COGS")
	wantSourceLines(t, "invoice cost", ic, "5100", map[string]int64{"2": 10000})
	wantSourceLines(t, "invoice cost", ic, "1200", map[string]int64{"2": -10000})
	e.wantBalanced(inv)

	// Bill without a PO: each row is its own line, blocked GST in line 2's cost.
	cc := e.costCentre("SL")
	bill := e.create(e.input("SL",
		ExpenseBillLine{Account: "5100", Amount: 1000, GSTRate: 18, ITC: "Eligible", CostCenter: cc},
		ExpenseBillLine{Account: "5200", Amount: 500, GSTRate: 18, ITC: "Blocked", CostCenter: cc}))
	x := glSourceRows(t, e.schema, bill, "%:EXPENSE:%")
	wantSourceLines(t, "bill", x, "5100", map[string]int64{"1": 100000})
	wantSourceLines(t, "bill", x, "5200", map[string]int64{"2": 59000})
	wantSourceLines(t, "bill", x, "1500", map[string]int64{"1": 18000})
	wantSourceLines(t, "bill", x, "2100", map[string]int64{"1": -118000, "2": -59000})
	e.wantBalanced(bill)
}
