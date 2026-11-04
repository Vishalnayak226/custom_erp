package engines

import (
	"encoding/json"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 148.8. The money the tests check is in paise, as gl_postings holds it.

type tc1488 struct{ *mc1481 }

func newTC1488(t *testing.T) *tc1488 {
	t.Helper()
	c := &tc1488{mc1481: newMC1481(t)}
	t.Cleanup(c.sweep) // runs before the fixture's own cleanup
	return c
}

// sweep removes what the engine wrote on its own: the charge entries of this
// test's documents and their postings.
func (c *tc1488) sweep() {
	for _, source := range c.docs {
		rows, err := db.DB.Query("SELECT id FROM "+c.schema+".documents WHERE doctype = 'TradeChargeEntry' AND data->>'source_id' = $1", source)
		if err != nil {
			continue
		}
		var ids []string
		for rows.Next() {
			var id string
			rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+c.schema+".gl_postings WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM "+c.schema+".documents WHERE id = $1", id)
		}
	}
}

func (c *tc1488) code(over map[string]interface{}) string {
	c.t.Helper()
	id := c.id("CODE")
	data := map[string]interface{}{"code": id, "charge_name": id + " name", "payee": "Vendor", "into_cost": "Yes", "status": "Active"}
	for k, v := range over {
		data[k] = v
	}
	c.doc(id, "TradeChargeCode", "Active", data)
	return id
}

func (c *tc1488) rule(code, vendorGroup, itemGroup, method string, rate float64) string {
	c.t.Helper()
	id := c.id("RULE")
	c.doc(id, "TradeChargeRule", "Active", map[string]interface{}{"rule_name": id, "charge_code": code, "vendor_group": vendorGroup,
		"item_group": itemGroup, "method": method, "rate": rate, "status": "Active"})
	return id
}

func (c *tc1488) vendor(group, terms string) string {
	c.t.Helper()
	id := c.id("VEND")
	c.doc(id, "Vendor", "Active", map[string]interface{}{"code": id, "name": id, "vendor_group": group, "freight_terms": terms})
	return id
}

func (c *tc1488) patch(id string, patch map[string]interface{}) {
	c.t.Helper()
	raw, _ := json.Marshal(patch)
	if _, err := db.DB.Exec("UPDATE "+c.schema+".documents SET data = data || $1::jsonb WHERE id = $2", string(raw), id); err != nil {
		c.t.Fatalf("patch %s: %v", id, err)
	}
}

// onDoc types charge rows on a PO, GRN or transfer, the way the form saves them.
func (c *tc1488) onDoc(id string, rows ...map[string]interface{}) {
	c.t.Helper()
	raw, _ := json.Marshal(rows)
	c.patch(id, map[string]interface{}{"charges": string(raw)})
}

func row(code string, amount float64) map[string]interface{} {
	return map[string]interface{}{"charge_code": code, "amount": amount}
}

func (c *tc1488) entries(source string) []map[string]interface{} {
	c.t.Helper()
	rows, err := db.DB.Query("SELECT id, status, data FROM "+c.schema+".documents WHERE doctype = 'TradeChargeEntry' AND data->>'source_id' = $1 ORDER BY id", source)
	if err != nil {
		c.t.Fatalf("entries: %v", err)
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, status string
		var raw []byte
		if err := rows.Scan(&id, &status, &raw); err != nil {
			c.t.Fatalf("entries: %v", err)
		}
		var d map[string]interface{}
		_ = json.Unmarshal(raw, &d)
		d["id"], d["doc_status"] = id, status
		out = append(out, d)
	}
	return out
}

// entry is the one entry of a code on a source.
func (c *tc1488) entry(source, code string) map[string]interface{} {
	c.t.Helper()
	var found map[string]interface{}
	for _, e := range c.entries(source) {
		if e["charge_code"] == code {
			if found != nil {
				c.t.Fatalf("%s has two entries of %s", source, code)
			}
			found = e
		}
	}
	if found == nil {
		c.t.Fatalf("%s has no entry of %s", source, code)
	}
	return found
}

func entryID(e map[string]interface{}) string { return e["id"].(string) }

// entryLine is one SKU's row on an entry.
func entryLine(t *testing.T, e map[string]interface{}, sku string) map[string]interface{} {
	t.Helper()
	lines, _ := e["lines"].([]interface{})
	for _, raw := range lines {
		if l, ok := raw.(map[string]interface{}); ok && l["sku"] == sku {
			return l
		}
	}
	t.Fatalf("entry %v has no line for %s: %v", e["id"], sku, e["lines"])
	return nil
}

func wantShare(t *testing.T, e map[string]interface{}, sku string, want int64) {
	t.Helper()
	if got := int64(numFromInterface(entryLine(t, e, sku)["share"])); got != want {
		t.Errorf("%s share %d paise, want %d", sku, got, want)
	}
}

func wantReason(t *testing.T, e map[string]interface{}, sku, word string) {
	t.Helper()
	l := entryLine(t, e, sku)
	if got := int64(numFromInterface(l["share"])); got != 0 {
		t.Errorf("%s has a share of %d and should have none", sku, got)
	}
	if reason, _ := l["reason"].(string); !strings.Contains(strings.ToLower(reason), strings.ToLower(word)) {
		t.Errorf("%s reason %q does not say %q", sku, reason, word)
	}
}

func (c *tc1488) wantGL(docID, account string, want int64) {
	c.t.Helper()
	if got := c.gl(docID, account); got != want {
		c.t.Errorf("%s on %s: %d paise, want %d", account, docID, got, want)
	}
}

// post raises the PO and GRN, lets set type charges on them, and posts it.
func (c *tc1488) post(lines []mc1481Line, set func(grnID, poID string)) (grnID, poID string) {
	c.t.Helper()
	grnID, poID = c.grnDocs(c.loc, lines)
	if set != nil {
		set(grnID, poID)
	}
	if err := c.postGRN(c.loc, grnID, lines); err != nil {
		c.t.Fatalf("post %s: %v", grnID, err)
	}
	return grnID, poID
}

// purchaseReturn posts a return of qty of sku from the GRN.
func (c *tc1488) purchaseReturn(grnID, sku string, qty float64) (string, *PurchaseReturnPostResult) {
	c.t.Helper()
	ret := c.id("PR")
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "stock_bucket": "Accepted", "qty": qty}})
	vendor := c.vendor("", "")
	p := map[string]interface{}{"grn_id": grnID, "reason": "wrong size", "return_items": string(lines), "vendor_id": vendor, "location": c.loc}
	if err := validatePurchaseReturnRules(c.tenant, "", "", nil, p); err != nil {
		c.t.Fatalf("validate return: %v", err)
	}
	c.doc(ret, "PurchaseReturn", "Draft", p)
	res, err := PostPurchaseReturn(c.tenant, ret, "system", "HQ")
	if err != nil {
		c.t.Fatalf("PostPurchaseReturn: %v", err)
	}
	if res.DebitNoteID != "" {
		c.docs = append(c.docs, res.DebitNoteID)
	}
	return ret, res
}

// billEntries is a carrier's bill for the entries, no GST.
func (c *tc1488) billEntries(carrier, number string, amount float64, ids ...string) *ChargeBillResult {
	c.t.Helper()
	res, err := BillChargeEntries(c.tenant, "system", ChargeBillInput{EntryIDs: ids, VendorID: carrier, InvoiceNumber: number,
		InvoiceDate: BusinessToday(), InvoiceAmount: amount, GSTAmount: &[]float64{0}[0]})
	if err != nil {
		c.t.Fatalf("BillChargeEntries: %v", err)
	}
	c.docs = append(c.docs, res.InvoiceID)
	return res
}

// ---- The master -----------------------------------------------------------

// Accounts are checked when the code is saved, and through the same door every
// save goes through.
func TestStage1488CodeIsCheckedWhenSaved(t *testing.T) {
	c := newTC1488(t)
	blocked, revenue := "T1488B"+c.run[len(c.run)-6:], "T1488R"+c.run[len(c.run)-6:]
	for _, a := range []struct {
		code, kind string
		blocked    bool
	}{{blocked, "Expense", true}, {revenue, "Revenue", false}} {
		if _, err := db.DB.Exec("INSERT INTO "+c.schema+".gl_accounts (account_code, account_name, account_type, blocked) VALUES ($1, $1, $2, $3)", a.code, a.kind, a.blocked); err != nil {
			t.Fatalf("account: %v", err)
		}
		defer db.DB.Exec("DELETE FROM "+c.schema+".gl_accounts WHERE account_code = $1", a.code)
	}
	good := func(over map[string]interface{}) map[string]interface{} {
		d := map[string]interface{}{"code": "T1488", "charge_name": "Freight", "payee": "Third party", "into_cost": "Yes",
			"clearing_account": "2110", "gst_rate": 18.0, "basis": "Value", "status": "Active"}
		for k, v := range over {
			d[k] = v
		}
		return d
	}
	if err := ValidateDocument(c.tenant, "TradeChargeCode", good(nil)); err != nil {
		t.Fatalf("a good code is refused: %v", err)
	}
	if err := ValidateDocument(c.tenant, "TradeChargeCode", good(map[string]interface{}{"clearing_account": ""})); err != nil {
		t.Errorf("a blank clearing account means 2110: %v", err)
	}
	for _, tc := range []struct {
		name string
		over map[string]interface{}
		want string
	}{
		{"no such account", map[string]interface{}{"into_cost": "No", "gl_account": "T1488-NOPE"}, "FIN-0890"},
		{"a blocked account", map[string]interface{}{"into_cost": "No", "gl_account": blocked}, "FIN-0890"},
		{"not into cost needs an account", map[string]interface{}{"into_cost": "No", "gl_account": ""}, "FIN-0890"},
		{"a clearing account not in the chart", map[string]interface{}{"clearing_account": "T1488-NOPE"}, "FIN-0890"},
		{"a revenue account for a purchase cost", map[string]interface{}{"into_cost": "No", "gl_account": revenue}, "FIN-0891"},
		{"a clearing account that is not a liability", map[string]interface{}{"clearing_account": "1200"}, "FIN-0891"},
		{"no payee", map[string]interface{}{"payee": ""}, "FIN-0892"},
		{"no into cost", map[string]interface{}{"into_cost": ""}, "FIN-0892"},
		{"GST over 100", map[string]interface{}{"gst_rate": 120.0}, "FIN-0893"},
		{"GST below 0", map[string]interface{}{"gst_rate": -1.0}, "FIN-0893"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := t1481Code(ValidateDocument(c.tenant, "TradeChargeCode", good(tc.over))); got != tc.want {
				t.Errorf("code %q, want %s", got, tc.want)
			}
		})
	}
	// A selling code has its own account and needs no payee; a revenue account is right.
	selling := map[string]interface{}{"code": "T1488S", "charge_name": "Delivery", "used_for": "Selling", "gl_account": revenue, "status": "Active"}
	if err := ValidateDocument(c.tenant, "TradeChargeCode", selling); err != nil {
		t.Errorf("a selling code is refused: %v", err)
	}
	// The engine writes entries; nobody saves one by hand.
	if got := t1481Code(ValidateDocument(c.tenant, "TradeChargeEntry", map[string]interface{}{"code": "X", "status": "Posted"})); got != "FIN-0894" {
		t.Errorf("a hand-saved entry: code %q, want FIN-0894", got)
	}
}

func TestStage1488RulesAndRowsAreCheckedWhenSaved(t *testing.T) {
	c := newTC1488(t)
	active, off := c.code(nil), c.code(map[string]interface{}{"status": "Inactive"})
	selling := c.code(map[string]interface{}{"used_for": "Selling"})
	party := c.code(map[string]interface{}{"payee": "Third party"})
	sku := c.item(nil)
	items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 1}})
	doc := func(status string, rows ...map[string]interface{}) map[string]interface{} {
		raw, _ := json.Marshal(rows)
		return map[string]interface{}{"status": status, "items": string(items), "received_items": string(items), "charges": string(raw)}
	}
	for _, tc := range []struct {
		name, doctype string
		doc           map[string]interface{}
		want          string
	}{
		{"a good row", "PurchaseOrder", doc("Draft", row(active, 100)), ""},
		{"no rows", "PurchaseOrder", map[string]interface{}{"status": "Draft"}, ""},
		{"a row by the code's own code field", "GRN", doc("Draft", row(active, 100)), ""},
		{"an unknown code", "PurchaseOrder", doc("Draft", row("T1488-NOPE", 100)), "PURCHA-4980"},
		{"an inactive code on a draft", "PurchaseOrder", doc("Draft", row(off, 100)), "PURCHA-4980"},
		{"an inactive code on an approved PO stays editable", "PurchaseOrder", doc("Approved", row(off, 100)), ""},
		{"a selling code on a purchase", "GRN", doc("Draft", row(selling, 100)), "PURCHA-4981"},
		{"no amount", "PurchaseOrder", doc("Draft", row(active, 0)), "PURCHA-4982"},
		{"a line not on the document", "PurchaseOrder", doc("Draft", map[string]interface{}{"charge_code": active, "amount": 10.0, "only_lines": "T1488-NOPE"}), "PURCHA-4982"},
		{"a line that is on the document", "PurchaseOrder", doc("Draft", map[string]interface{}{"charge_code": active, "amount": 10.0, "exclude_lines": sku}), ""},
		{"a transfer charge the vendor would be paid", "TransferOrder", doc("Draft", row(active, 100)), "PURCHA-4983"},
		{"a transfer charge a carrier is paid", "TransferOrder", doc("Draft", row(party, 100)), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := t1481Code(validateTradeChargeDocument(c.tenant, tc.doctype, tc.doc)); got != tc.want {
				t.Errorf("code %q, want %q", got, tc.want)
			}
		})
	}
	ruleDoc := func(code, method string, rate float64) map[string]interface{} {
		return map[string]interface{}{"rule_name": "R", "charge_code": code, "method": method, "rate": rate, "status": "Active"}
	}
	for _, tc := range []struct {
		name string
		doc  map[string]interface{}
		want string
	}{
		{"a good rule", ruleDoc(active, "Percent", 2), ""},
		{"an inactive code", ruleDoc(off, "Percent", 2), "PURCHA-4980"},
		{"a selling code", ruleDoc(selling, "Percent", 2), "PURCHA-4981"},
		{"no rate", ruleDoc(active, "Fixed", 0), "PURCHA-4984"},
		{"over 100 percent", ruleDoc(active, "Percent", 120), "PURCHA-4984"},
	} {
		t.Run("rule "+tc.name, func(t *testing.T) {
			if got := t1481Code(ValidateDocument(c.tenant, "TradeChargeRule", tc.doc)); got != tc.want {
				t.Errorf("code %q, want %q", got, tc.want)
			}
		})
	}
	// The row check is at the shared exit, so the generic save path runs it.
	_, realPO := c.grnDocs(c.loc, []mc1481Line{{sku: sku, qty: 1, rate: 10}})
	grn := map[string]interface{}{"code": "T1488G", "po_id": realPO, "location": c.loc, "status": "Pending",
		"received_items": string(items), "charges": doc("Draft", row(off, 100))["charges"]}
	if got := t1481Code(ValidateDocument(c.tenant, "GRN", grn)); got != "PURCHA-4980" {
		t.Errorf("a GRN save with an inactive charge: code %q, want PURCHA-4980", got)
	}
}

// ---- The step GRN fixture -------------------------------------------------

func (c *tc1488) stepItem() string {
	c.t.Helper()
	return c.item(map[string]interface{}{"mrp": 199.0, "hsn_code": "6204", "gst_rate": 5.0, "tax_treatment": ""})
}

// stepGRN raises a PO and a receipt in steps with the vendor's typed invoice total.
func (c *tc1488) stepGRN(lines []mc1481Line, invoice float64, set func(poID string)) string {
	c.t.Helper()
	if !grnLineTablesReady(db.DB, c.schema) {
		c.t.Skip("the GRN line tables are not on this database")
	}
	preserveSeries(c.t, c.schema, "DN", "VCN", "GRNBatchSeq", "VendorInvoice", "GRN")
	var ordered, received []map[string]interface{}
	var value float64
	for _, l := range lines {
		ordered = append(ordered, map[string]interface{}{"sku": l.sku, "qty": l.qty, "rate": l.rate})
		received = append(received, map[string]interface{}{"sku": l.sku, "qty": l.qty, "invoice_rate": l.rate, "gst_rate": 5.0, "mrp": 199.0, "hsn_code": "6204"})
		value += float64(l.qty) * l.rate
	}
	po := c.id("PO")
	rawOrdered, _ := json.Marshal(ordered)
	c.doc(po, "PurchaseOrder", "Approved", map[string]interface{}{"code": po, "vendor": c.vendor("", ""), "gst_mode": GSTModeExclusive,
		"total_amount": value, "grand_total": value * 1.05, "items": string(rawOrdered)})
	if set != nil {
		set(po)
	}
	grn := c.id("GRN")
	rawReceived, _ := json.Marshal(received)
	payload := map[string]interface{}{"flow": "steps", "po_ids": po, "location": c.loc, "vendor_invoice_no": grn, "vendor_invoice_date": BusinessToday(),
		"vendor_invoice_amount": invoice, "received_items": string(rawReceived), "code": grn}
	for _, step := range []func() error{
		func() error { return PrepareGRNPricing(c.tenant, true, payload) },
		func() error { return PrepareGRNSteps(c.tenant, "system", true, payload) },
		func() error { return PrepareGRNBatchNumbers(c.tenant, true, payload) },
	} {
		if err := step(); err != nil {
			c.t.Fatalf("prepare: %v", err)
		}
	}
	c.doc(grn, "GRN", strField(payload, "status"), payload)
	SyncGRNLineRows(c.tenant, grn)
	c.t.Cleanup(func() { db.DB.Exec("DELETE FROM "+c.schema+".documents WHERE data->>'grn_id' = $1", grn) })
	return grn
}

// stepPostAll verifies, price checks and posts each child in turn.
func (c *tc1488) stepPostAll(grn string, moves map[string]string) *GRNStepView {
	c.t.Helper()
	if _, err := VerifyGRNStep(c.tenant, "system", "Super Admin", grn, GRNVerifyInput{Reason: "checked"}); err != nil {
		c.t.Fatalf("verify: %v", err)
	}
	view, err := PriceCheckGRNStep(c.tenant, "system", grn, GRNPriceCheckInput{Moves: moves, Reason: "checked"})
	if err != nil {
		c.t.Fatalf("price check: %v", err)
	}
	for _, child := range view.Children {
		if view, err = PostGRNChildStep(c.tenant, "system", "", grn, child.Key, GRNPostInput{Reason: "checked"}); err != nil {
			c.t.Fatalf("post child %s: %v", child.Key, err)
		}
	}
	return view
}

// ---- Nothing set up, nothing changes --------------------------------------

func TestStage1488NoCodesNoChange(t *testing.T) {
	c := newTC1488(t)
	sku := c.item(nil)
	grn, po := c.receive(sku, 10, 100)
	c.wantGL(grn, "1200", 100000)
	c.wantGL(grn, "2100", -100000)
	c.wantBalanced(grn)
	c.wantAvg(sku, 10000, "a receipt with no codes on file")
	if got := c.entries(grn); len(got) != 0 {
		t.Errorf("no code on file, yet %d entries were written", len(got))
	}
	bill := c.bill(po, grn, 1000, nil)
	res, err := MatchVendorInvoice(c.tenant, bill, po, grn, 20)
	if err != nil || res.Status != "Matched" {
		t.Fatalf("match %+v, %v", res, err)
	}
	ret, _ := c.purchaseReturn(grn, sku, 5)
	c.wantGL(ret, "1200", -50000)
	c.wantAvg(sku, 10000, "a return with no codes on file")

	// A code nobody put on a document or a rule changes nothing either.
	c.code(nil)
	sku2 := c.item(nil)
	grn2, _ := c.receive(sku2, 10, 100)
	c.wantGL(grn2, "1200", 100000)
	c.wantAvg(sku2, 10000, "a receipt with a code no one uses")
	if got := c.entries(grn2); len(got) != 0 {
		t.Errorf("a code on no document wrote %d entries", len(got))
	}
}

// ---- Allocation at GRN post -----------------------------------------------

func TestStage1488ValueSplitLandsInCostAtGRN(t *testing.T) {
	c := newTC1488(t)
	code := c.code(nil)
	a, b := c.item(nil), c.item(nil)
	lines := []mc1481Line{{sku: a, qty: 10, rate: 100}, {sku: b, qty: 10, rate: 300}}
	grn, _ := c.post(lines, func(_, po string) { c.onDoc(po, row(code, 400)) })

	e := c.entry(grn, code)
	if e["doc_status"] != "Posted" || int64(numFromInterface(e["amount_paise"])) != 40000 {
		t.Fatalf("entry %+v", e)
	}
	wantShare(t, e, a, 10000)
	wantShare(t, e, b, 30000)
	c.wantAvg(a, 11000, "A with its share of the freight")
	c.wantAvg(b, 33000, "B with its share of the freight")
	id := entryID(e)
	c.wantGL(id, "1200", 40000)
	c.wantGL(id, "2100", -40000) // the vendor is paid: it joins what the vendor is owed
	c.wantBalanced(id)
	// The goods themselves post as they always have.
	c.wantGL(grn, "1200", 400000)
	c.wantGL(grn, "2100", -400000)
}

// A GRN row replaces the PO's for the same code.
func TestStage1488GRNRowOverridesThePO(t *testing.T) {
	c := newTC1488(t)
	code := c.code(nil)
	a := c.item(nil)
	grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(grn, po string) {
		c.onDoc(po, row(code, 400))
		c.onDoc(grn, row(code, 100))
	})
	if got := int64(numFromInterface(c.entry(grn, code)["amount_paise"])); got != 10000 {
		t.Errorf("amount %d, want the GRN's 10000", got)
	}
}

func TestStage1488BasisSharesSumExactly(t *testing.T) {
	t.Run("qty, the last line takes the rounding", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(map[string]interface{}{"basis": "Qty"})
		a, b, d := c.item(nil), c.item(nil), c.item(nil)
		lines := []mc1481Line{{sku: a, qty: 1, rate: 100}, {sku: b, qty: 1, rate: 500}, {sku: d, qty: 1, rate: 900}}
		grn, _ := c.post(lines, func(_, po string) { c.onDoc(po, row(code, 100)) })
		e := c.entry(grn, code)
		// Lines post in SKU order and a, b, d were minted in that order.
		wantShare(t, e, a, 3333)
		wantShare(t, e, b, 3333)
		wantShare(t, e, d, 3334)
		c.wantGL(entryID(e), "1200", 10000)
	})
	t.Run("value, thirds of an uneven charge", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(nil)
		a, b, d := c.item(nil), c.item(nil), c.item(nil)
		lines := []mc1481Line{{sku: a, qty: 3, rate: 100}, {sku: b, qty: 3, rate: 100}, {sku: d, qty: 3, rate: 100}}
		grn, _ := c.post(lines, func(_, po string) { c.onDoc(po, row(code, 100.01)) })
		e := c.entry(grn, code)
		var sum int64
		for _, s := range []string{a, b, d} {
			sum += int64(numFromInterface(entryLine(t, e, s)["share"]))
		}
		if sum != 10001 {
			t.Errorf("shares sum to %d, want exactly 10001", sum)
		}
		c.wantGL(entryID(e), "1200", 10001)
	})
	t.Run("weight, and a line with no weight says so", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(map[string]interface{}{"basis": "Weight"})
		a, b, d := c.item(nil), c.item(nil), c.item(nil)
		lines := []mc1481Line{
			{sku: a, qty: 1, rate: 100, extra: map[string]interface{}{"actual_weight": 10.0, "weight_uom": "g"}},
			{sku: b, qty: 1, rate: 100, extra: map[string]interface{}{"actual_weight": 0.03, "weight_uom": "kg"}},
			{sku: d, qty: 1, rate: 100},
		}
		grn, _ := c.post(lines, func(_, po string) { c.onDoc(po, row(code, 100)) })
		e := c.entry(grn, code)
		wantShare(t, e, a, 2500)
		wantShare(t, e, b, 7500)
		wantReason(t, e, d, "weight")
		c.wantGL(entryID(e), "1200", 10000)
	})
	t.Run("weight, and no line has any", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(map[string]interface{}{"basis": "Weight"})
		a := c.item(nil)
		grn, _ := c.post([]mc1481Line{{sku: a, qty: 1, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 100)) })
		e := c.entry(grn, code)
		if e["doc_status"] != "Skipped" || !strings.Contains(strings.ToLower(e["skip_reason"].(string)), "weight") {
			t.Errorf("entry %+v", e)
		}
		c.wantGL(entryID(e), "1200", 0)
		c.wantAvg(a, 10000, "a charge with nowhere to go")
	})
}

func TestStage1488NotIntoCostPostsToItsAccount(t *testing.T) {
	c := newTC1488(t)
	code := c.code(map[string]interface{}{"into_cost": "No", "gl_account": "5300"})
	a := c.item(nil)
	grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 50)) })
	e := c.entry(grn, code)
	c.wantGL(entryID(e), "5300", 5000)
	c.wantGL(entryID(e), "2100", -5000)
	c.wantGL(entryID(e), "1200", 0)
	c.wantAvg(a, 10000, "a charge that does not go into cost")
	wantShare(t, e, a, 5000)
}

// ---- Rules and freight terms ----------------------------------------------

func TestStage1488RulesPostStraightToLines(t *testing.T) {
	c := newTC1488(t)
	code := c.code(nil)
	group, cat, other := "G"+c.run, "C"+c.run, "D"+c.run
	c.rule(code, group, cat, "Percent", 2) // the most specific
	c.rule(code, group, "", "Percent", 5)
	c.rule(code, "", "", "Percent", 9)
	v := c.vendor(group, "")
	a, b := c.item(map[string]interface{}{"category": cat}), c.item(map[string]interface{}{"category": other})
	lines := []mc1481Line{{sku: a, qty: 10, rate: 100}, {sku: b, qty: 10, rate: 100}}
	grn, _ := c.post(lines, func(_, po string) { c.patch(po, map[string]interface{}{"vendor": v}) })
	e := c.entry(grn, code)
	wantShare(t, e, a, 2000) // 2% of 1000
	wantShare(t, e, b, 5000) // 5% of 1000
	c.wantAvg(a, 10200, "A at 2%")
	c.wantAvg(b, 10500, "B at 5%")
	c.wantGL(entryID(e), "1200", 7000)
}

func TestStage1488RuleMethods(t *testing.T) {
	t.Run("per unit", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(nil)
		c.rule(code, "", "", "Per unit", 3)
		a := c.item(nil)
		grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, nil)
		wantShare(t, c.entry(grn, code), a, 3000)
	})
	t.Run("fixed spreads by the code's basis", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(map[string]interface{}{"basis": "Qty"})
		c.rule(code, "", "", "Fixed", 100)
		a, b := c.item(nil), c.item(nil)
		grn, _ := c.post([]mc1481Line{{sku: a, qty: 1, rate: 100}, {sku: b, qty: 3, rate: 900}}, nil)
		e := c.entry(grn, code)
		wantShare(t, e, a, 2500)
		wantShare(t, e, b, 7500)
	})
	t.Run("a row on the document beats the rule", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(nil)
		c.rule(code, "", "", "Percent", 50)
		a := c.item(nil)
		grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 10)) })
		wantShare(t, c.entry(grn, code), a, 1000)
	})
}

// A rule that does not reach a line is no entry at all, and the line says why.
func TestStage1488LineWithNoRuleSaysWhy(t *testing.T) {
	c := newTC1488(t)
	code := c.code(nil)
	group, cat := "G"+c.run, "C"+c.run
	c.rule(code, "NOT"+group, "", "Percent", 2)
	v := c.vendor(group, "")
	a := c.item(map[string]interface{}{"category": cat})
	grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.patch(po, map[string]interface{}{"vendor": v}) })
	if got := c.entries(grn); len(got) != 0 {
		t.Fatalf("no rule reached it, yet %d entries", len(got))
	}
	view, err := GRNChargeLines(c.tenant, grn)
	if err != nil || len(view) != 1 {
		t.Fatalf("GRNChargeLines: %v %+v", err, view)
	}
	why := strings.Join(view[0].Reasons, ";")
	if !strings.Contains(why, group) || !strings.Contains(why, cat) {
		t.Errorf("why %q names neither the group nor the category", why)
	}
}

func TestStage1488GRNChargeLinesShowsShares(t *testing.T) {
	c := newTC1488(t)
	code := c.code(map[string]interface{}{"scope": "All lines"})
	a, b := c.item(nil), c.item(nil)
	grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}, {sku: b, qty: 10, rate: 100}}, func(_, po string) {
		c.onDoc(po, map[string]interface{}{"charge_code": code, "amount": 50.0, "exclude_lines": b})
	})
	view, err := GRNChargeLines(c.tenant, grn)
	if err != nil || len(view) != 2 {
		t.Fatalf("GRNChargeLines: %v %+v", err, view)
	}
	for _, l := range view {
		switch l.SKU {
		case a:
			if len(l.Charges) != 1 || l.Charges[0].Paise != 5000 {
				t.Errorf("A's charges %+v", l.Charges)
			}
		case b:
			if len(l.Charges) != 0 || !strings.Contains(strings.ToLower(strings.Join(l.Reasons, ";")), "left out") {
				t.Errorf("B %+v", l)
			}
		}
	}
}

func TestStage1488FreightTerms(t *testing.T) {
	freight := func(c *tc1488) string { return c.code(map[string]interface{}{"charge_kind": "Freight"}) }
	t.Run("CIF: the vendor bears it", func(t *testing.T) {
		c := newTC1488(t)
		code := freight(c)
		c.rule(code, "", "", "Percent", 2)
		v := c.vendor("", "CIF")
		a := c.item(nil)
		grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.patch(po, map[string]interface{}{"vendor": v}) })
		e := c.entry(grn, code)
		if e["doc_status"] != "Skipped" || !strings.Contains(e["skip_reason"].(string), "CIF") {
			t.Errorf("entry %+v", e)
		}
		c.wantGL(entryID(e), "1200", 0)
		c.wantAvg(a, 10000, "freight the vendor bears")
	})
	t.Run("FOB: we bear it", func(t *testing.T) {
		c := newTC1488(t)
		code := freight(c)
		c.rule(code, "", "", "Percent", 2)
		v := c.vendor("", "FOB")
		a := c.item(nil)
		grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.patch(po, map[string]interface{}{"vendor": v}) })
		wantShare(t, c.entry(grn, code), a, 2000)
	})
	t.Run("the PO's own freight term beats the vendor's", func(t *testing.T) {
		c := newTC1488(t)
		code := freight(c)
		c.rule(code, "", "", "Percent", 2)
		v := c.vendor("", "FOB")
		a := c.item(nil)
		grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) {
			c.patch(po, map[string]interface{}{"vendor": v, "freight_by": "Vendor"})
		})
		if e := c.entry(grn, code); e["doc_status"] != "Skipped" {
			t.Errorf("entry %+v", e)
		}
	})
	t.Run("a typed row is honoured under CIF", func(t *testing.T) {
		c := newTC1488(t)
		code := freight(c)
		v := c.vendor("", "CIF")
		a := c.item(nil)
		grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) {
			c.patch(po, map[string]interface{}{"vendor": v})
			c.onDoc(po, row(code, 10))
		})
		wantShare(t, c.entry(grn, code), a, 1000)
	})
	t.Run("a charge that is neither freight nor insurance ignores the term", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(nil)
		c.rule(code, "", "", "Percent", 2)
		v := c.vendor("", "CIF")
		a := c.item(nil)
		grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.patch(po, map[string]interface{}{"vendor": v}) })
		wantShare(t, c.entry(grn, code), a, 2000)
	})
}

// ---- Scope and repeat -----------------------------------------------------

func TestStage1488ScopeSaysWhy(t *testing.T) {
	t.Run("stocked lines only", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(map[string]interface{}{"scope": "Stocked lines only"})
		a := c.item(nil)
		b := c.item(map[string]interface{}{"stock_tracking": StockNotTracked})
		grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}, {sku: b, qty: 10, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 100)) })
		e := c.entry(grn, code)
		wantShare(t, e, a, 10000)
		wantReason(t, e, b, "stocked")
		if note, _ := e["note"].(string); !strings.Contains(strings.ToLower(note), "stocked") {
			t.Errorf("note %q leaves the reason out", note)
		}
	})
	t.Run("a line left out", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(nil)
		a, b := c.item(nil), c.item(nil)
		grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}, {sku: b, qty: 10, rate: 100}}, func(_, po string) {
			c.onDoc(po, map[string]interface{}{"charge_code": code, "amount": 100.0, "exclude_lines": b})
		})
		e := c.entry(grn, code)
		wantShare(t, e, a, 10000)
		wantReason(t, e, b, "left out")
	})
	t.Run("only the lines named", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(nil)
		a, b := c.item(nil), c.item(nil)
		grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}, {sku: b, qty: 10, rate: 100}}, func(_, po string) {
			c.onDoc(po, map[string]interface{}{"charge_code": code, "amount": 100.0, "only_lines": b})
		})
		e := c.entry(grn, code)
		wantShare(t, e, b, 10000)
		wantReason(t, e, a, "not named")
	})
}

func TestStage1488RepeatOnceOrEvery(t *testing.T) {
	twoGRNs := func(c *tc1488, code string) (first, second, sku string) {
		sku = c.item(nil)
		lines := []mc1481Line{{sku: sku, qty: 5, rate: 100}}
		first, po := c.grnDocs(c.loc, lines)
		c.onDoc(po, row(code, 50))
		if err := c.postGRN(c.loc, first, lines); err != nil {
			t.Fatalf("first: %v", err)
		}
		second = c.id("GRN")
		raw, _ := json.Marshal([]map[string]interface{}{lines[0].posted()})
		c.doc(second, "GRN", "Received", map[string]interface{}{"code": second, "po_id": po, "location": c.loc, "received_items": string(raw)})
		if err := c.postGRN(c.loc, second, lines); err != nil {
			t.Fatalf("second: %v", err)
		}
		return first, second, sku
	}
	t.Run("once", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(nil) // blank repeat means once
		first, second, _ := twoGRNs(c, code)
		c.wantGL(entryID(c.entry(first, code)), "1200", 5000)
		e := c.entry(second, code)
		if e["doc_status"] != "Skipped" || !strings.Contains(e["skip_reason"].(string), first) {
			t.Errorf("second entry %+v does not name the first GRN", e)
		}
		c.wantGL(entryID(e), "1200", 0)
	})
	t.Run("every receipt", func(t *testing.T) {
		c := newTC1488(t)
		code := c.code(map[string]interface{}{"repeat_on": "Every receipt"})
		first, second, _ := twoGRNs(c, code)
		c.wantGL(entryID(c.entry(first, code)), "1200", 5000)
		c.wantGL(entryID(c.entry(second, code)), "1200", 5000)
	})
}

// ---- Retries --------------------------------------------------------------

func TestStage1488RepeatedGRNPostPostsOnce(t *testing.T) {
	c := newTC1488(t)
	code := c.code(nil)
	a := c.item(nil)
	lines := []mc1481Line{{sku: a, qty: 10, rate: 100}}
	grn, _ := c.post(lines, func(_, po string) { c.onDoc(po, row(code, 100)) })
	id := entryID(c.entry(grn, code))
	if err := c.postGRN(c.loc, grn, lines); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if got := len(c.entries(grn)); got != 1 {
		t.Errorf("%d entries after a repeat, want 1", got)
	}
	c.wantGL(id, "1200", 10000)
	c.wantGL(id, "2100", -10000)
}

// ---- Other ways in --------------------------------------------------------

// The GRN steps come through the same posting point. Vendor billed the charge on
// its own invoice, so the steps must not raise a debit note for it.
func TestStage1488StepGRNGetsTheSameCharge(t *testing.T) {
	c := newTC1488(t)
	code := c.code(nil)
	sku := c.stepItem()
	grn := c.stepGRN([]mc1481Line{{sku: sku, qty: 10, rate: 100}}, 1150, func(po string) { c.onDoc(po, row(code, 100)) })
	view := c.stepPostAll(grn, nil)
	e := c.entry(grn, code)
	wantShare(t, e, sku, 10000)
	c.wantAvg(sku, 11000, "a step GRN")
	c.wantGL(entryID(e), "1200", 10000)
	c.wantGL(entryID(e), "2100", -10000)
	if view.DebitNoteID != "" {
		t.Errorf("the charge on the vendor's invoice raised debit note %s", view.DebitNoteID)
	}
	if view.RoundOff != 0 || view.Difference != 0 {
		t.Errorf("the charge reads as a difference: round off %v, difference %v", view.RoundOff, view.Difference)
	}
}

// A step GRN posts child by child. The charge goes out once across them and the
// parts add up to the paisa.
func TestStage1488StepGRNChildrenShareExactly(t *testing.T) {
	c := newTC1488(t)
	code := c.code(nil)
	a, b := c.stepItem(), c.stepItem()
	lines := []mc1481Line{{sku: a, qty: 10, rate: 100}, {sku: b, qty: 20, rate: 100}}
	grn := c.stepGRN(lines, 3250.01, func(po string) { c.onDoc(po, row(code, 100.01)) })
	c.stepPostAll(grn, map[string]string{b: GRNGroupSame})
	var total, debit int64
	seen := map[string]bool{}
	for _, e := range c.entries(grn) {
		total += RupeesToPaise(numFromInterface(e["amount"]))
		debit += c.gl(entryID(e), "1200")
		seen[entryID(e)] = true
	}
	if len(seen) != 2 {
		t.Errorf("%d entries, want one per child", len(seen))
	}
	if total != 10001 || debit != 10001 {
		t.Errorf("the children carried %d paise and stock took %d, want 10001", total, debit)
	}
}

// A row typed on a later receipt is charged, even on a code that says once.
func TestStage1488RowOnTheSecondGRNIsCharged(t *testing.T) {
	c := newTC1488(t)
	code := c.code(map[string]interface{}{"repeat_on": "Once"})
	sku := c.item(nil)
	lines := []mc1481Line{{sku: sku, qty: 5, rate: 100}}
	first, po := c.grnDocs(c.loc, lines)
	c.onDoc(first, row(code, 50))
	if err := c.postGRN(c.loc, first, lines); err != nil {
		t.Fatalf("first: %v", err)
	}
	second := c.id("GRN")
	raw, _ := json.Marshal([]map[string]interface{}{lines[0].posted()})
	c.doc(second, "GRN", "Received", map[string]interface{}{"code": second, "po_id": po, "location": c.loc, "received_items": string(raw)})
	c.onDoc(second, row(code, 70))
	if err := c.postGRN(c.loc, second, lines); err != nil {
		t.Fatalf("second: %v", err)
	}
	wantShare(t, c.entry(first, code), sku, 5000)
	wantShare(t, c.entry(second, code), sku, 7000)
}

func TestStage1488InterStoreFreightAddsToReceivingStore(t *testing.T) {
	c := newTC1488(t)
	code := c.code(map[string]interface{}{"payee": "Third party", "basis": "Qty"})
	sku := c.item(nil)
	to := "L1488B-" + c.run
	c.doc(to, "Location", "Active", map[string]interface{}{"code": to, "name": to, "type": "Store", "sellable": "Yes"})
	c.receive(sku, 10, 100)
	toID := c.id("TO")
	items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 4}})
	c.doc(toID, "TransferOrder", "Approved", map[string]interface{}{"id": toID, "transfer_number": toID, "from_warehouse": c.loc, "to_warehouse": to, "status": "Approved", "items": string(items)})
	c.onDoc(toID, row(code, 100))
	if err := DispatchTransferOrder(c.tenant, toID, "system"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	c.wantAvg(sku, 10000, "dispatch carries no charge")
	req := TransferReceiptRequest{ReceiptKey: "k-" + c.run, Lines: []TransferReceiptLine{{Sku: sku, Qty: 4}}}
	if _, err := ReceiveTransferOrderLines(c.tenant, toID, "system", req); err != nil {
		t.Fatalf("receive: %v", err)
	}
	e := c.entry(toID, code)
	if e["location"] != to || e["source_doctype"] != "TransferOrder" || e["doc_status"] != "Estimated" {
		t.Errorf("entry %+v", e)
	}
	id := entryID(e)
	c.wantGL(id, "1200", 10000)
	c.wantGL(id, "2110", -10000)
	c.wantAvg(sku, 11000, "Rs 100 on 10 in hand") // company-wide average, D1
	// The same delivery again.
	if _, err := ReceiveTransferOrderLines(c.tenant, toID, "system", req); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	c.wantGL(id, "1200", 10000)
	if got := len(c.entries(toID)); got != 1 {
		t.Errorf("%d entries after a repeat", got)
	}
	// The carrier's bill clears it like any other.
	carrier := c.vendor("", "")
	c.billEntries(carrier, "TO-"+c.run, 100, id)
	c.wantGL(id, "2110", 0)
}

func TestStage1488PieceOwnCostIncludesTheCharge(t *testing.T) {
	c := newTC1488(t)
	code := c.code(nil)
	sku := c.item(map[string]interface{}{"tracking_mode": TrackingSerial})
	s1, s2 := c.id("S"), c.id("S")
	lines := []mc1481Line{{sku: sku, qty: 2, rate: 100, extra: map[string]interface{}{"serial_numbers": []interface{}{s1, s2}}}}
	c.post(lines, func(_, po string) { c.onDoc(po, row(code, 20)) })
	for _, s := range []string{s1, s2} {
		if got := numFromInterface(c.pieceData("SerialNumber", "serial_no", s)["receipt_cost_paise"]); got != 11000 {
			t.Errorf("piece %s own cost %v, want 11000 with its share of the charge", s, got)
		}
	}
	if v := c.valuation(sku); v["total_value"] != 220.0 {
		t.Errorf("valuation %+v, want Rs 220", v)
	}
}

// ---- The carrier's bill ---------------------------------------------------

// estimate parks Rs 100 in clearing on 10 units, of which 4 are then sold.
func (c *tc1488) estimate(t *testing.T) (entry, sku, cart string) {
	t.Helper()
	code := c.code(map[string]interface{}{"payee": "Third party"})
	sku = c.item(nil)
	grn, _ := c.post([]mc1481Line{{sku: sku, qty: 10, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 100)) })
	e := c.entry(grn, code)
	if e["doc_status"] != "Estimated" {
		t.Fatalf("entry %+v", e)
	}
	entry = entryID(e)
	c.wantGL(entry, "1200", 10000)
	c.wantGL(entry, "2110", -10000)
	c.wantGL(entry, "2100", 0)
	c.wantAvg(sku, 11000, "the estimate is in cost")
	cart, cost := c.mustSell(sku, 4, nil)
	if cost != 440 {
		t.Fatalf("sold at %v, want 440", cost)
	}
	return entry, sku, cart
}

func (c *tc1488) entryByID(id string) (map[string]interface{}, string) {
	c.t.Helper()
	var raw []byte
	var status string
	if err := db.DB.QueryRow("SELECT data, status FROM "+c.schema+".documents WHERE id = $1", id).Scan(&raw, &status); err != nil {
		c.t.Fatalf("entry %s: %v", id, err)
	}
	var d map[string]interface{}
	_ = json.Unmarshal(raw, &d)
	return d, status
}

func TestStage1488BillAboveTheEstimate(t *testing.T) {
	c := newTC1488(t)
	id, sku, cart := c.estimate(t)
	cogs := c.gl(cart, "5100")
	carrier := c.vendor("", "")
	res := c.billEntries(carrier, "B-"+c.run, 200, id)
	// Rs 100 more on 10 units: 6 here, 4 sold.
	c.wantGL(id, "2110", 0)
	c.wantGL(id, "2100", -20000)
	c.wantGL(id, "1200", 10000+6000)
	c.wantGL(id, "5195", 4000)
	c.wantBalanced(id)
	c.wantAvg(sku, 12000, "6 in hand carry their Rs 60")
	c.wantGL(cart, "5100", cogs) // the sale is not re-costed
	d, status := c.entryByID(id)
	if status != "Trued up" || d["bill_id"] != res.InvoiceID || numFromInterface(d["open_paise"]) != 0 {
		t.Errorf("entry %s %+v", status, d)
	}
	// Retried: same bill, nothing new.
	again := c.billEntries(carrier, "B-"+c.run, 200, id)
	if again.InvoiceID != res.InvoiceID || !again.Replayed {
		t.Errorf("repeat %+v", again)
	}
	c.wantGL(id, "2100", -20000)
	c.wantGL(id, "5195", 4000)
	c.wantAvg(sku, 12000, "a repeated true-up")
	// A different bill for the same entry is refused.
	_, err := BillChargeEntries(c.tenant, "system", ChargeBillInput{EntryIDs: []string{id}, VendorID: carrier, InvoiceNumber: "B2-" + c.run, InvoiceDate: BusinessToday(), InvoiceAmount: 300})
	if got := t1481Code(err); got != "FIN-0897" {
		t.Errorf("a second bill: code %q, want FIN-0897", got)
	}
}

func TestStage1488BillBelowTheEstimate(t *testing.T) {
	c := newTC1488(t)
	id, sku, cart := c.estimate(t)
	cogs := c.gl(cart, "5100")
	c.billEntries(c.vendor("", ""), "B-"+c.run, 50, id)
	c.wantGL(id, "2110", 0)
	c.wantGL(id, "2100", -5000)
	c.wantGL(id, "1200", 10000-3000)
	c.wantGL(id, "5195", -2000)
	c.wantBalanced(id)
	c.wantAvg(sku, 10500, "6 in hand give back their Rs 30")
	c.wantGL(cart, "5100", cogs)
}

func TestStage1488BillAtTheEstimateMovesOnlyTheClearing(t *testing.T) {
	c := newTC1488(t)
	id, sku, _ := c.estimate(t)
	c.billEntries(c.vendor("", ""), "B-"+c.run, 100, id)
	c.wantGL(id, "2110", 0)
	c.wantGL(id, "2100", -10000)
	c.wantGL(id, "1200", 10000)
	c.wantGL(id, "5195", 0)
	c.wantAvg(sku, 11000, "no difference")
}

// One bill for two receipts: split by what each estimated, last takes the rounding.
func TestStage1488OneBillTwoEntries(t *testing.T) {
	c := newTC1488(t)
	code := c.code(map[string]interface{}{"payee": "Third party"})
	a, b := c.item(nil), c.item(nil)
	grnA, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 100)) })
	grnB, _ := c.post([]mc1481Line{{sku: b, qty: 10, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 200)) })
	ea, eb := entryID(c.entry(grnA, code)), entryID(c.entry(grnB, code))
	c.billEntries(c.vendor("", ""), "B-"+c.run, 330, ea, eb)
	c.wantGL(ea, "2110", 0)
	c.wantGL(eb, "2110", 0)
	if got := c.gl(ea, "2100") + c.gl(eb, "2100"); got != -33000 {
		t.Errorf("2100 %d, want the whole bill", got)
	}
	c.wantBalanced(ea)
	c.wantBalanced(eb)
}

func TestStage1488BillRefusals(t *testing.T) {
	c := newTC1488(t)
	mine := c.vendor("", "")
	named := c.code(map[string]interface{}{"payee": "Third party", "carrier": mine})
	paid := c.code(nil)
	a := c.item(nil)
	grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.onDoc(po, row(named, 100), row(paid, 100)) })
	open, vendorPaid := entryID(c.entry(grn, named)), entryID(c.entry(grn, paid))
	call := func(id, vendor, number string, amount float64) string {
		_, err := BillChargeEntries(c.tenant, "system", ChargeBillInput{EntryIDs: []string{id}, VendorID: vendor, InvoiceNumber: number, InvoiceDate: BusinessToday(), InvoiceAmount: amount})
		return t1481Code(err)
	}
	if got := call(vendorPaid, mine, "R1-"+c.run, 100); got != "FIN-0895" {
		t.Errorf("a vendor-paid entry: code %q", got)
	}
	if got := call(open, mine, "R2-"+c.run, 0); got != "FIN-0896" {
		t.Errorf("no amount: code %q", got)
	}
	if got := call(open, mine, "", 100); got != "FIN-0896" {
		t.Errorf("no number: code %q", got)
	}
	if got := call(open, c.vendor("", ""), "R3-"+c.run, 100); got != "FIN-0898" {
		t.Errorf("another carrier: code %q", got)
	}
	// The number is already on an ordinary bill of this carrier.
	other := c.id("VI")
	c.doc(other, "VendorInvoice", "Draft", map[string]interface{}{"invoice_number": "R4-" + c.run, "vendor_id": mine, "invoice_amount": 1.0, "financial_year": "T1488"})
	if got := call(open, mine, "R4-"+c.run, 100); got != "FIN-0899" {
		t.Errorf("a used number: code %q", got)
	}
	c.wantGL(open, "2110", -10000)
}

// The carrier's bill pays through the ordinary path and leaves 2100 clear.
func TestStage1488CarrierBillPays(t *testing.T) {
	c := newTC1488(t)
	code := c.code(map[string]interface{}{"payee": "Third party", "gst_rate": 18.0})
	a := c.item(nil)
	grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 100)) })
	id := entryID(c.entry(grn, code))
	carrier := c.vendor("", "")
	res, err := BillChargeEntries(c.tenant, "system", ChargeBillInput{EntryIDs: []string{id}, VendorID: carrier, InvoiceNumber: "P-" + c.run,
		InvoiceDate: BusinessToday(), InvoiceAmount: 118}) // GST from the code's rate
	if err != nil {
		t.Fatalf("bill: %v", err)
	}
	c.docs = append(c.docs, res.InvoiceID)
	c.wantGL(id, "2110", 0)
	c.wantGL(id, "2100", -10000) // GST is not a cost
	c.wantGL(id, "1200", 10000)
	d, status := c.entryByID(res.InvoiceID)
	if status != "Matched" || numFromInterface(d["gst_amount"]) != 18 || numFromInterface(d["taxable_amount"]) != 100 {
		t.Fatalf("bill %s %+v", status, d)
	}
	if d["grn_id"] != nil || d["po_id"] != nil {
		t.Errorf("a carrier's bill must not look like the goods bill: %+v", d)
	}
	if _, _, err := PayVendorInvoice(c.tenant, res.InvoiceID, "system", "Super Admin", ""); err != nil {
		t.Fatalf("pay: %v", err)
	}
	c.wantGL(res.InvoiceID, "2100", 10000)
	c.wantGL(res.InvoiceID, "1500", 1800)
	c.wantGL(res.InvoiceID, "1100", -11800)
}

// ---- The vendor's own bill ------------------------------------------------

func TestStage1488VendorBillCarriesTheCharge(t *testing.T) {
	c := newTC1488(t)
	code := c.code(map[string]interface{}{"gst_rate": 18.0})
	a := c.item(nil)
	grn, po := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 100)) })
	id := entryID(c.entry(grn, code))

	// Goods alone is short of what is owed.
	short := c.bill(po, grn, 1000, nil)
	if res, err := MatchVendorInvoice(c.tenant, short, po, grn, 1); err != nil || res.Status == "Matched" {
		t.Errorf("goods alone matched: %+v %v", res, err)
	}
	c.patch(short, map[string]interface{}{"status": "Cancelled"})
	if _, err := db.DB.Exec("UPDATE "+c.schema+".documents SET status = 'Cancelled' WHERE id = $1", short); err != nil {
		t.Fatal(err)
	}

	// Goods 1000 + charge 100 + its GST 18.
	bill := c.bill(po, grn, 1118, nil)
	res, err := MatchVendorInvoice(c.tenant, bill, po, grn, 1)
	if err != nil || res.Status != "Matched" {
		t.Fatalf("match: %+v %v", res, err)
	}
	d, _ := c.entryByID(bill)
	if numFromInterface(d["gst_amount"]) != 18 || numFromInterface(d["taxable_amount"]) != 1100 {
		t.Errorf("gst %v taxable %v", d["gst_amount"], d["taxable_amount"])
	}
	c.wantAvg(a, 11000, "the charge is not a price difference")
	c.wantGL(bill, "5195", 0)
	if _, _, err := PayVendorInvoice(c.tenant, bill, "system", "Super Admin", ""); err != nil {
		t.Fatalf("pay: %v", err)
	}
	if got := c.gl(grn, "2100") + c.gl(id, "2100") + c.gl(bill, "2100"); got != 0 {
		t.Errorf("2100 left %d after the vendor is paid", got)
	}
	c.wantGL(bill, "1500", 1800)
}

// ---- Returns --------------------------------------------------------------

func TestStage1488ReturnReversesPayableCharge(t *testing.T) {
	c := newTC1488(t)
	code := c.code(nil)
	a := c.item(nil)
	grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 200)) })
	id := entryID(c.entry(grn, code))
	c.wantAvg(a, 12000, "charged")
	ret, res := c.purchaseReturn(grn, a, 5)
	c.wantGL(ret, "1200", -50000) // the goods post as they always have
	c.wantGL(id, "1200", 20000-10000)
	c.wantGL(id, "5150", 10000)
	c.wantBalanced(id)
	c.wantAvg(a, 12000, "half went back at what it cost")
	if v := c.valuation(a); v["total_value"] != 600.0 {
		t.Errorf("valuation %+v, want Rs 600", v)
	}
	// What the vendor owes back includes half the charge.
	if res.DebitNoteID == "" {
		t.Fatal("no debit note")
	}
	c.wantGL(res.DebitNoteID, "2100", 60000)
	wantRet := func(want int64) {
		t.Helper()
		d, _ := c.entryByID(id)
		if got := int64(numFromInterface(d["returned_paise"])); got != want {
			t.Errorf("returned %d, want %d", got, want)
		}
	}
	wantRet(10000)

	// A retried post of the return is refused and posts nothing.
	if _, err := PostPurchaseReturn(c.tenant, ret, "system", "HQ"); t1481Code(err) != "PURCHA-0119" {
		t.Errorf("repeat: %v", err)
	}
	c.wantGL(id, "1200", 10000)

	// Below the guard the reversal itself still runs once.
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, c.schema); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := reverseReceiptChargesTx(tx, c.tenant, c.schema, ret, grn, []PurchaseReturnLine{{SKU: a, Qty: 5, StockBucket: PurchaseReturnBucketAccepted}}); err != nil {
		t.Fatalf("direct repeat: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	c.wantGL(id, "1200", 10000)
	wantRet(10000)

	// The rest goes back: the entry is done.
	c.purchaseReturn(grn, a, 5)
	c.wantGL(id, "1200", 0)
	c.wantGL(id, "5150", 20000)
	wantRet(20000)
	if _, status := c.entryByID(id); status != "Reversed" {
		t.Errorf("status %s, want Reversed", status)
	}
}

func TestStage1488ReturnOfAnOpenEstimateBeforeTheBill(t *testing.T) {
	c := newTC1488(t)
	code := c.code(map[string]interface{}{"payee": "Third party"})
	a := c.item(nil)
	grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 100)) })
	id := entryID(c.entry(grn, code))
	_, res := c.purchaseReturn(grn, a, 5)
	c.wantGL(id, "2110", -5000)
	c.wantGL(id, "1200", 5000)
	c.wantAvg(a, 11000, "half went back with its share")
	d, _ := c.entryByID(id)
	if numFromInterface(d["open_paise"]) != 5000 {
		t.Errorf("open %v, want 5000", d["open_paise"])
	}
	if res.DebitNoteID != "" {
		c.wantGL(res.DebitNoteID, "2100", 50000) // the goods only: the carrier is not the vendor
	}
	// The carrier still bills the whole journey. Clearing ends at nought and the
	// extra lands on the 5 still here.
	c.billEntries(c.vendor("", ""), "B-"+c.run, 100, id)
	c.wantGL(id, "2110", 0)
	c.wantGL(id, "2100", -10000)
	c.wantGL(id, "1200", 5000+5000)
	c.wantGL(id, "5195", 0)
	c.wantAvg(a, 12000, "5 in hand carry the whole Rs 100")
	c.wantBalanced(id)
}

func TestStage1488ReturnAfterTheBillGoesToVariance(t *testing.T) {
	c := newTC1488(t)
	code := c.code(map[string]interface{}{"payee": "Third party"})
	a := c.item(nil)
	grn, _ := c.post([]mc1481Line{{sku: a, qty: 10, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 100)) })
	id := entryID(c.entry(grn, code))
	c.billEntries(c.vendor("", ""), "B-"+c.run, 100, id)
	c.purchaseReturn(grn, a, 5)
	c.wantGL(id, "1200", 10000-5000)
	c.wantGL(id, "5195", 5000) // the carrier is paid; that freight is a loss now
	c.wantGL(id, "2110", 0)
	c.wantAvg(a, 11000, "half went back with its share")
	c.wantBalanced(id)
}

// Pro rata keeps its rounding across returns: the parts sum to the share exactly.
func TestStage1488ReturnsInPartsSumToTheShare(t *testing.T) {
	c := newTC1488(t)
	code := c.code(nil)
	a := c.item(nil)
	grn, _ := c.post([]mc1481Line{{sku: a, qty: 3, rate: 100}}, func(_, po string) { c.onDoc(po, row(code, 100)) })
	id := entryID(c.entry(grn, code))
	c.purchaseReturn(grn, a, 1)
	c.wantGL(id, "1200", 10000-3333)
	c.purchaseReturn(grn, a, 2)
	c.wantGL(id, "1200", 0)
	c.wantGL(id, "5150", 10000)
}

// A return from a receipt with no charge on it behaves as ever, even with codes
// on file.
func TestStage1488ReturnWithoutChargeUnchanged(t *testing.T) {
	c := newTC1488(t)
	c.code(nil)
	a := c.item(nil)
	grn, _ := c.receive(a, 10, 100)
	ret, res := c.purchaseReturn(grn, a, 5)
	c.wantGL(ret, "1200", -50000)
	c.wantGL(ret, "5150", 50000)
	if res.DebitNoteID != "" {
		c.wantGL(res.DebitNoteID, "2100", 50000)
	}
	c.wantAvg(a, 10000, "a plain return")
}
