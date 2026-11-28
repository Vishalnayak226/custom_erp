package server

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// 316.1: the bill pairing, on the cases of minn's November file.
func TestBulkPurchaseBillsPairLikeAPerson(t *testing.T) {
	row := func(n int, vendor, bill string) *bpLine {
		return &bpLine{Row: n, Vendor: vendor, Invoice: bill, Qty: 1, Rate: 100}
	}
	bill := func(n int, vendor, no, date string) *bpBillIn {
		return &bpBillIn{Row: n, Vendor: vendor, Invoice: no, Date: date, Total: 100}
	}
	ahm := bill(3, "Ahm crt", "ahm-12339", "2026-10-02")
	jsk := bill(5, "jai shree krishna", "SA4733 PO", "2026-10-02")
	zevi := bill(8, "zevi", "zevi-1", "2026-10-01")
	gagan := bill(11, "gagan", "gagan01", "2026-10-03")
	thofa := bill(12, "thofa", "thofa260026", "2026-10-03")
	fahion := bill(13, "fahion jewellery", "fahion jewelleryo1", "2026-10-03")
	ornista := bill(14, "fahion jewellery", "ornista01", "2026-10-03")
	ball1 := bill(15, "ballerina", "ballerina01", "2026-10-03")
	ball2 := bill(17, "ballerina", "ballerina02", "2026-10-02")
	twice := bill(20, "Ahm crt", "AHM-12339", "2026-10-02")
	blk := bill(21, "blockco", "inv101", "2026-10-04")
	bills := []*bpBillIn{ahm, jsk, zevi, gagan, thofa, fahion, ornista, ball1, ball2, twice, blk}

	r2, r3, r4 := row(2, "Ahm crt", "ahm-12339"), row(3, "Ahm crt", "SE/2526/PL/3568"), row(4, "AHM CRT", "ahm-12339")
	j1 := row(120, "jai shree krishna", "sa4733po")
	z1 := row(378, "zevi", "zevi-1")
	t1, t2 := row(409, "tanush", "zevi-1"), row(410, "tanush", "zevi-1")
	g := []*bpLine{row(548, "gagan", "gagan01"), row(549, "gagan", "gagan02"), row(550, "gagan", "gagan03"),
		row(551, "gagan", "gagan04"), row(552, "gagan", "gagan05"), row(553, "gagan", "gagan05")}
	f1, f2 := row(643, "fahion jewellery", "gagan82"), row(644, "fahion jewellery", "gagan82")
	o1 := row(662, "ornista", "ornista01")
	b1, b2 := row(663, "ballerina", "ballerinapo01"), row(664, "ballerina", "ballerinapo01")
	b3 := row(696, "ballerina", "ballerina02")
	k1, k2 := row(900, "blockco", "inv101"), row(901, "blockco", "inv101")
	k3, k4 := row(902, "blockco", "inv102"), row(903, "blockco", "inv102")
	ordered := row(950, "tanush", "")
	lines := append([]*bpLine{r2, r3, r4, j1, z1, t1, t2}, g...)
	lines = append(lines, f1, f2, o1, b1, b2, b3, k1, k2, k3, k4, ordered)

	p := bpPairBills(bills, lines, "2026-11-28")
	on := func(l *bpLine, want *bpBillIn) {
		t.Helper()
		if p.bill[l] != want {
			got := "none"
			if b := p.bill[l]; b != nil {
				got = b.Vendor + " " + b.Invoice
			}
			t.Errorf("row %d (%s %s) is on %s, want %s %s", l.Row, l.Vendor, l.Invoice, got, want.Vendor, want.Invoice)
		}
	}
	t.Run("as typed, whatever the case of the vendor; a bill listed twice gets nothing", func(t *testing.T) {
		on(r2, ahm)
		on(r4, ahm)
		on(z1, zevi)
		on(b3, ball2)
		on(k1, blk)
		for l, b := range p.bill {
			if b == twice {
				t.Errorf("row %d is on the bill listed twice", l.Row)
			}
		}
	})
	t.Run("the same letters and digits are one bill", func(t *testing.T) {
		on(j1, jsk)
		if !strings.Contains(jsk.note, "1 row(s) with bill no sa4733po") {
			t.Errorf("note %q", jsk.note)
		}
	})
	t.Run("Excel's count-up is one bill", func(t *testing.T) {
		for _, l := range g {
			on(l, gagan)
		}
		if !strings.Contains(gagan.note, "5 row(s) with bill no gagan02, gagan03, gagan04 ...") {
			t.Errorf("note %q", gagan.note)
		}
		if p.note[g[1]] != "received on bill gagan01" {
			t.Errorf("row note %q", p.note[g[1]])
		}
	})
	t.Run("a bill under the wrong vendor goes to the vendor whose rows carry it", func(t *testing.T) {
		on(o1, ornista)
		if ornista.Vendor != "ornista" || !strings.Contains(ornista.note, "under fahion jewellery") {
			t.Errorf("vendor %q note %q", ornista.Vendor, ornista.note)
		}
	})
	t.Run("the vendor's one bill no row names takes its other rows", func(t *testing.T) {
		on(f1, fahion)
		on(f2, fahion)
		on(b1, ball1)
		on(b2, ball1)
	})
	t.Run("a bill with no rows at all is left to say so", func(t *testing.T) {
		for l, b := range p.bill {
			if b == thofa {
				t.Errorf("row %d is on thofa's bill", l.Row)
			}
		}
		if p.bill[ordered] != nil {
			t.Errorf("a row with no bill no is ordered, not received")
		}
	})
	t.Run("what is left is a bill of its own, at its rows", func(t *testing.T) {
		if len(p.made) != 3 {
			for _, b := range p.made {
				t.Logf("made %s %s", b.Vendor, b.Invoice)
			}
			t.Fatalf("made %d bills, want 3 (Ahm crt SE/2526/PL/3568, tanush zevi-1, blockco inv102)", len(p.made))
		}
		se, tz, k := p.made[0], p.made[1], p.made[2]
		on(r3, se)
		on(t1, tz)
		on(t2, tz)
		on(k3, k)
		on(k4, k)
		if !se.fromRows || se.Row != 0 || se.Date != "2026-10-02" || se.note != "not on the Bills sheet: made from row 3" {
			t.Errorf("Ahm crt's own bill %+v", *se)
		}
		if tz.Date != "2026-10-03" || tz.note != "not on the Bills sheet: made from rows 409-410 - zevi-1 is zevi's bill no" {
			t.Errorf("tanush's own bill date %q note %q", tz.Date, tz.note)
		}
		if !strings.Contains(p.note[t1], "is not on the Bills sheet") {
			t.Errorf("row note %q", p.note[t1])
		}
	})
	t.Run("a made bill with no bill date in the file is dated today", func(t *testing.T) {
		q := bpPairBills(nil, []*bpLine{row(2, "x", "a1")}, "2026-11-28")
		if len(q.made) != 1 || q.made[0].Date != "2026-11-28" {
			t.Fatalf("made %+v", q.made)
		}
	})
}

// 316.1 against a real database: a file typed like minn's - a bill under the
// wrong vendor, a bill no Excel counted up, a bill total that is not its
// rows', a bill not on the Bills sheet - is received and posted bill by bill,
// and the same file again makes nothing twice.
func TestBulkPurchaseBillsReceiveAsTyped(t *testing.T) {
	c := swTestCaller(t)
	probe, err := newSWRun(c, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := probe.resolveLink("Location", "HO"); err != nil || id == "" {
		t.Skip("the test database has no store HO")
	}
	storeHasState(t, "HO")
	sfx := fmt.Sprint(time.Now().UnixNano() % 1e9)
	va, vb, category := "BPA Vendor "+sfx, "BPB Vendor "+sfx, "BPBCat"+sfx
	schema, _ := db.GetTenantSchema("default")
	var res *bpResult
	t.Cleanup(func() { bpTestCleanup(schema, category, []string{va, vb}, res) })
	row := func(vendor, ref, colour, qty, bill string) []string {
		return []string{"", ref, vendor, "HO", "", "Earring", category, "7117", "3", colour, "", qty, "90", "349", "", bill, "", "", ""}
	}
	file := swBook(t, "", map[string][][]string{
		"Vendors": {{"Check", "Vendor Name *", "State *", "City", "GSTIN", "Phone", "Email"},
			{"", va, "Maharashtra", "Mumbai", "", "", ""}, {"", vb, "Maharashtra", "Mumbai", "", "", ""}},
		"Purchase": {bpTestPurchaseHead,
			row(va, "BPA-"+sfx, "Red", "1", "a-01"),
			row(va, "BPA-"+sfx, "Blue", "2", "a-02"),
			row(va, "BPA-"+sfx, "Green", "1", "lost-1"),
			row(vb, "BPB-"+sfx, "Red", "2", "b-1"),
		},
		"Bills": {{"Check", "Vendor Invoice No *", "Vendor *", "Invoice Date *", "Invoice Total (₹) *"},
			{"", "a-01", va, "46345", "300"},
			{"", "b-1", va, "46345", "180"},
		},
	}, "Vendors", "Purchase", "Bills")
	res = bpRunFile(t, c, file, true)
	if res.Totals.Failed != 0 || res.Totals.Errors != 0 || len(res.Bills) != 3 {
		for _, b := range res.Bills {
			t.Logf("bill %s %s: %s %s %s", b.Vendor, b.Invoice, b.Action, b.Result, b.Message)
		}
		t.Fatalf("totals %+v, %d bills (want 3, nothing failed)", res.Totals, len(res.Bills))
	}
	got := map[string]*bpBillOut{}
	for _, b := range res.Bills {
		got[b.Invoice] = b
		if b.Result != "done" || b.Step != engines.GRNStepPosted || b.GRNID == "" {
			t.Errorf("bill %s of %s: result %q, step %q, GRN %q - %s", b.Invoice, b.Vendor, b.Result, b.Step, b.GRNID, b.Message)
		}
	}
	if a := got["a-01"]; a == nil || a.Lines != 2 || a.Total != 270 || !strings.Contains(a.Message, "row(s) with bill no a-02") ||
		!strings.Contains(a.Message, "settle the 30 with the vendor") {
		t.Errorf("the counted-up bill with a total of 300 = %+v", a)
	}
	if b := got["b-1"]; b == nil || b.Vendor != vb || b.Lines != 1 || !strings.Contains(b.Message, "under "+va) {
		t.Errorf("the bill under the wrong vendor = %+v", b)
	}
	if l := got["lost-1"]; l == nil || l.Vendor != va || l.Date != "2026-11-19" || l.Total != 90 || !strings.Contains(l.Message, "not on the Bills sheet") {
		t.Errorf("the bill not on the Bills sheet = %+v", l)
	}

	again := bpRunFile(t, c, file, true)
	for _, b := range again.Bills {
		if b.Action != "existing" || b.Result != "done" || b.GRNID != got[b.Invoice].GRNID {
			t.Errorf("the same file again, bill %s: %s %s %s (GRN %s, first time %s)", b.Invoice, b.Action, b.Result, b.Message, b.GRNID, got[b.Invoice].GRNID)
		}
	}
	if again.Totals.NewPOs != 0 || len(again.Bills) != 3 {
		t.Errorf("the same file again made %d POs and has %d bills", again.Totals.NewPOs, len(again.Bills))
	}
}

// bpTestCleanup removes what an applied upload made: its GRNs and their
// postings, stock and bills, its POs, combinations, designs and vendors.
func bpTestCleanup(schema, category string, vendors []string, res *bpResult) {
	exec := func(q string, args ...interface{}) { _, _ = db.DB.Exec(fmt.Sprintf(q, schema), args...) }
	var skus []string
	if res != nil {
		for _, b := range res.Bills {
			if b.GRNID == "" {
				continue
			}
			exec(`DELETE FROM %[1]s.gl_postings WHERE document_id IN (SELECT id FROM %[1]s.documents WHERE data->>'source_doc_id' = $1 OR data->>'grn_id' = $1)`, b.GRNID)
			exec(`DELETE FROM %[1]s.approval_log WHERE document_id IN (SELECT id FROM %[1]s.documents WHERE data->>'source_doc_id' = $1 OR data->>'grn_id' = $1)`, b.GRNID)
			exec(`DELETE FROM %[1]s.documents WHERE doctype IN ('DebitNote','VendorCreditNote','VendorInvoice','GRNExcessApproval') AND (data->>'source_doc_id' = $1 OR data->>'grn_id' = $1)`, b.GRNID)
			exec(`DELETE FROM %[1]s.gl_postings WHERE document_id = $1`, b.GRNID)
			exec(`DELETE FROM %[1]s.documents WHERE doctype = 'StockLedgerEntry' AND (data->>'voucher_id' = $1 OR data->>'voucher_id' LIKE $1 || '-C%%')`, b.GRNID)
			exec(`DELETE FROM %[1]s.grn_line WHERE grn_id = $1`, b.GRNID)
			exec(`DELETE FROM %[1]s.grn_line_change WHERE grn_id = $1`, b.GRNID)
			exec(`DELETE FROM %[1]s.documents WHERE id = $1`, b.GRNID)
		}
		for _, po := range res.POs {
			for _, l := range po.Lines {
				if l.SKU != "" {
					skus = append(skus, l.SKU)
				}
			}
			if po.POID != "" {
				exec(`DELETE FROM %[1]s.approval_log WHERE document_id = $1`, po.POID)
				exec(`DELETE FROM %[1]s.documents WHERE id = $1`, po.POID)
			}
		}
	}
	for _, sku := range skus {
		exec(`DELETE FROM %[1]s.item_cost WHERE item_code = $1`, sku)
		exec(`DELETE FROM %[1]s.inventory_availability WHERE sku = $1`, sku)
		exec(`DELETE FROM %[1]s.documents WHERE doctype IN ('Batch','StockLedgerEntry') AND (data->>'item' = $1 OR data->>'item_id' = $1)`, sku)
	}
	exec(`DELETE FROM %[1]s.documents WHERE doctype = 'Item' AND data->>'category' = $1`, category)
	for _, v := range vendors {
		exec(`DELETE FROM %[1]s.documents WHERE doctype = 'ProductFamily' AND data->>'vendor' IN (SELECT id FROM %[1]s.documents WHERE doctype = 'Vendor' AND data->>'name' = $1)`, v)
		exec(`DELETE FROM %[1]s.documents WHERE doctype = 'Vendor' AND data->>'name' = $1`, v)
	}
	exec(`DELETE FROM %[1]s.documents WHERE doctype = 'ItemName' AND data->>'name' LIKE '%%' || $1 || '%%'`, category)
	exec(`DELETE FROM %[1]s.documents WHERE doctype = 'Category' AND data->>'name' = $1`, category)
}

func TestBulkPurchaseBillTotalIsItsRows(t *testing.T) {
	if got, note := bpBillTotal(100, 100.6); got != 100 || note != "" {
		t.Errorf("within Rs 1: %v %q", got, note)
	}
	if got, note := bpBillTotal(43920, 43620); got != 43620 || note != "bill total 43920, its rows 43620: received at the rows - settle the 300 with the vendor" {
		t.Errorf("swc: %v %q", got, note)
	}
	if got, _ := bpBillTotal(20600, 33041); got != 33041 {
		t.Errorf("gagan: %v", got)
	}
	if got := bpRowsText([]int{770, 3, 769}); got != "rows 3, 769-770" {
		t.Errorf("rows text %q", got)
	}
	if bpJoin("a", "") != "a" || bpJoin("", "b") != "b" || bpJoin("a", "b") != "a; b" {
		t.Errorf("join")
	}
}

// 316.2: a run saved before 237.6 kept no rows to fix.
func TestBulkPurchaseOldRunRowsToFix(t *testing.T) {
	res := &bpResult{
		POs: []*bpPOOut{
			{Ref: "ahm-oct- 2026", Vendor: "Ahm crt", Lines: []*bpLineOut{{Row: 2, Bill: "ahm-12339"}, {Row: 3, Bill: "SE/2526/PL/3568", Message: "bill SE/2526/PL/3568 is not on the Bills sheet - this row is ordered, not received"}}},
			{Ref: "delhi", Vendor: "delhi imported 1", Lines: []*bpLineOut{{Row: 98, Bill: "E-5548"}, {Row: 99}}},
			{Ref: "inv", Vendor: "invco", Lines: []*bpLineOut{{Row: 5, Bill: "i-1"}, {Row: 6, Bill: "i-1"}}},
		},
		Bills: []*bpBillOut{
			{Row: 3, Vendor: "Ahm crt", Invoice: "ahm-12339", Action: "error", Message: "the bill total 32076 and its rows (32553) differ by -477"},
			{Row: 4, Vendor: "delhi imported 1", Invoice: "E-5548", Action: "existing", Result: "done"},
			{Row: 5, Vendor: "invco", Invoice: "i-1", Action: "error", Message: "row 5 has Invoice Date 2026-10-01 and row 6 has 2026-10-02"},
		},
	}
	fix := bpRunFix(res)
	var got []string
	for _, f := range fix {
		got = append(got, f.Sheet+" "+strconv.Itoa(f.Row))
	}
	want := "purchase 2,purchase 3,purchase 5,purchase 6,bills 3"
	if strings.Join(got, ",") != want {
		t.Fatalf("rows to fix %v, want %s", got, want)
	}
	if !strings.Contains(fix[0].Reason, "bill ahm-12339 not received: the bill total") {
		t.Errorf("reason %q", fix[0].Reason)
	}
	res.Fix = []bpFixRow{{Sheet: "bills", Row: 9}}
	if f := bpRunFix(res); len(f) != 1 || f[0].Row != 9 {
		t.Errorf("a run with its own rows to fix keeps them: %+v", f)
	}
}
