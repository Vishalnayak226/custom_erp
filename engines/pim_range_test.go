package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Assortment / ranging, markdown plans and listing drift, each on its own
// throwaway tenant schema.

type rangeFixture struct {
	t        *testing.T
	tenantID string
	schema   string
}

func newRangeFixture(t *testing.T) *rangeFixture {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	return &rangeFixture{t: t, tenantID: tenantID, schema: schema}
}

func (f *rangeFixture) insert(id, doctype string, data map[string]interface{}) {
	f.t.Helper()
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+f.schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
		f.t.Fatalf("insert %s: %v", id, err)
	}
}

func (f *rangeFixture) item(code, category string, price, mrp float64) {
	f.insert(code, "Item", map[string]interface{}{"code": code, "name": code + " name", "category": category, "sale_price": price, "mrp": mrp,
		"hsn_code": "7113", "tax_treatment": "Exempt", "status": "Active"})
}

func (f *rangeFixture) store(code string) {
	f.insert(code, "Location", map[string]interface{}{"code": code, "name": code, "type": "Store", "sellable": "Yes"})
}

func (f *rangeFixture) rangeRow(id string, data map[string]interface{}) {
	if _, ok := data["status"]; !ok {
		data["status"] = "Active"
	}
	f.insert(id, "Assortment", data)
}

func (f *rangeFixture) mode(m string) {
	f.t.Helper()
	if err := SetSetting(f.tenantID, AssortmentSettingKey, m, "test"); err != nil {
		f.t.Fatalf("SetSetting: %v", err)
	}
}

func errCode(err error) string {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Code
	}
	return ""
}

func poPayload(store string, skus ...string) map[string]interface{} {
	lines := []map[string]interface{}{}
	for _, s := range skus {
		lines = append(lines, map[string]interface{}{"sku": s, "qty": 1})
	}
	raw, _ := json.Marshal(lines)
	return map[string]interface{}{"location": store, "items": string(raw)}
}

func transferPayload(to string, skus ...string) map[string]interface{} {
	lines := []map[string]interface{}{}
	for _, s := range skus {
		lines = append(lines, map[string]interface{}{"sku": s, "qty": 1})
	}
	raw, _ := json.Marshal(lines)
	return map[string]interface{}{"to_warehouse": to, "items": string(raw)}
}

func TestAssortmentRange(t *testing.T) {
	f := newRangeFixture(t)
	f.store("RNG-A")
	f.store("RNG-FREE")
	f.item("RING-1", "Rings", 1000, 1200)
	f.item("CHAIN-1", "Chains", 2000, 2400)
	f.rangeRow("ASRT-1", map[string]interface{}{"store": "RNG-A", "category": "Rings"})

	// Off, the default: nothing is checked, even with rows on the store.
	if err := validateAssortmentRange(f.tenantID, "PurchaseOrder", nil, poPayload("RNG-A", "CHAIN-1")); err != nil {
		t.Fatalf("setting off must not refuse: %v", err)
	}

	f.mode(AssortmentBlock)
	if err := validateAssortmentRange(f.tenantID, "PurchaseOrder", nil, poPayload("RNG-A", "RING-1")); err != nil {
		t.Errorf("an item in the category range was refused: %v", err)
	}
	err := validateAssortmentRange(f.tenantID, "PurchaseOrder", nil, poPayload("RNG-A", "RING-1", "CHAIN-1"))
	if errCode(err) != assortmentCodePO || !strings.Contains(err.Error(), "CHAIN-1") {
		t.Errorf("an item outside the range on a PO: err = %v, want %s naming CHAIN-1", err, assortmentCodePO)
	}
	// Through the shared save check, not only the helper.
	if err := ValidateTransactionalRules(f.tenantID, "PurchaseOrder", "", "", nil, poPayload("RNG-A", "CHAIN-1")); errCode(err) != assortmentCodePO {
		t.Errorf("ValidateTransactionalRules did not run the range check: %v", err)
	}
	// A store with no rows is unaffected.
	if err := validateAssortmentRange(f.tenantID, "PurchaseOrder", nil, poPayload("RNG-FREE", "CHAIN-1")); err != nil {
		t.Errorf("a store with no range rows was checked: %v", err)
	}
	// A save that leaves the lines alone is not re-judged.
	prior := poPayload("RNG-A", "CHAIN-1")
	if err := validateAssortmentRange(f.tenantID, "PurchaseOrder", prior, poPayload("RNG-A", "CHAIN-1")); err != nil {
		t.Errorf("an unchanged line list was re-judged: %v", err)
	}
	// Transfers into the store.
	if err := validateAssortmentRange(f.tenantID, "TransferOrder", nil, transferPayload("RNG-A", "CHAIN-1")); errCode(err) != assortmentCodeTransfer {
		t.Errorf("transfer into a ranged store: err = %v, want %s", err, assortmentCodeTransfer)
	}
	if err := validateAssortmentRange(f.tenantID, "TransferOrder", nil, transferPayload("RNG-FREE", "CHAIN-1")); err != nil {
		t.Errorf("transfer into an unranged store refused: %v", err)
	}

	// Item row admits that item; a date window that has not started or has ended does not.
	f.rangeRow("ASRT-2", map[string]interface{}{"store": "RNG-A", "item": "CHAIN-1", "valid_from": "2999-01-01"})
	if err := validateAssortmentRange(f.tenantID, "PurchaseOrder", nil, poPayload("RNG-A", "CHAIN-1")); errCode(err) != assortmentCodePO {
		t.Errorf("a future-dated row admitted the item: %v", err)
	}
	f.rangeRow("ASRT-3", map[string]interface{}{"store": "RNG-A", "item": "CHAIN-1", "valid_to": "2000-01-01"})
	if err := validateAssortmentRange(f.tenantID, "PurchaseOrder", nil, poPayload("RNG-A", "CHAIN-1")); errCode(err) != assortmentCodePO {
		t.Errorf("an ended row admitted the item: %v", err)
	}
	f.rangeRow("ASRT-4", map[string]interface{}{"store": "RNG-A", "item": "CHAIN-1", "valid_from": "2000-01-01", "valid_to": "2999-01-01"})
	if err := validateAssortmentRange(f.tenantID, "PurchaseOrder", nil, poPayload("RNG-A", "CHAIN-1")); err != nil {
		t.Errorf("an in-window row did not admit the item: %v", err)
	}

	// The store is matched by id or code, whichever the row used.
	f.rangeRow("ASRT-5", map[string]interface{}{"store": "rng-free-code", "item": "RING-1"})
	f.store("RNG-B")
	f.rangeRow("ASRT-6", map[string]interface{}{"store": "RNG-B", "status": "Inactive", "item": "RING-1"})
	if err := validateAssortmentRange(f.tenantID, "PurchaseOrder", nil, poPayload("RNG-B", "CHAIN-1")); err != nil {
		t.Errorf("an Inactive row made the store ranged: %v", err)
	}

	// Warn: no refusal, a message for the save response, and a Logged exception.
	f.mode(AssortmentWarn)
	if err := validateAssortmentRange(f.tenantID, "PurchaseOrder", nil, poPayload("RNG-A", "RING-1", "SKU-NONE")); err != nil {
		t.Errorf("warn mode refused: %v", err)
	}
	msg := AssortmentWarnDocument(f.tenantID, "PurchaseOrder", "PO-RNG-1", nil, poPayload("RNG-A", "SKU-NONE"))
	if !strings.Contains(msg, "SKU-NONE") {
		t.Errorf("warn mode gave no message: %q", msg)
	}
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + f.schema + `.documents WHERE doctype = 'ItemException' AND data->>'source_id' = 'PO-RNG-1'`).Scan(&n)
	if n != 1 {
		t.Errorf("warn mode should leave one item exception, got %d", n)
	}
	if msg := AssortmentWarnDocument(f.tenantID, "PurchaseOrder", "PO-RNG-2", nil, poPayload("RNG-FREE", "SKU-NONE")); msg != "" {
		t.Errorf("warn on an unranged store: %q", msg)
	}
}

func TestAssortmentSaleAndChannel(t *testing.T) {
	f := newRangeFixture(t)
	f.store("RNG-S")
	f.item("RING-1", "Rings", 1000, 1200)
	f.item("CHAIN-1", "Chains", 2000, 2400)
	f.rangeRow("ASRT-1", map[string]interface{}{"store": "RNG-S", "category": "Rings"})
	f.rangeRow("ASRT-2", map[string]interface{}{"channel": "shop-x", "category": "Rings"})
	quote := func(cart, sku string) error {
		_, err := ResolvePOSQuote(f.tenantID, QuoteRequest{CartNumber: cart, Location: "RNG-S", Lines: []QuoteLineRequest{{Sku: sku, Qty: 1}}})
		return err
	}

	f.mode(AssortmentOff)
	if err := quote("C0", "CHAIN-1"); err != nil {
		t.Fatalf("off: sale refused: %v", err)
	}
	f.mode(AssortmentBlock)
	if err := quote("C1", "RING-1"); err != nil {
		t.Errorf("in-range sale refused: %v", err)
	}
	if err := quote("C2", "CHAIN-1"); errCode(err) != assortmentCodeSale {
		t.Errorf("out-of-range sale: err = %v, want %s", err, assortmentCodeSale)
	}
	f.mode(AssortmentWarn)
	if err := quote("C3", "CHAIN-1"); err != nil {
		t.Errorf("warn mode refused a sale: %v", err)
	}
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + f.schema + `.documents WHERE doctype = 'ItemException' AND data->>'source_id' = 'C3'`).Scan(&n)
	if n != 1 {
		t.Errorf("warn sale should leave one item exception, got %d", n)
	}

	f.mode(AssortmentBlock)
	if err := assortmentChannelGate(f.tenantID, "shop-x", []string{"CHAIN-1"}); errCode(err) != assortmentCodeChannel {
		t.Errorf("channel order outside range: %v", err)
	}
	if err := assortmentChannelGate(f.tenantID, "shop-x", []string{"RING-1"}); err != nil {
		t.Errorf("channel order inside range refused: %v", err)
	}
	if err := assortmentChannelGate(f.tenantID, "other-shop", []string{"CHAIN-1"}); err != nil {
		t.Errorf("a channel with no rows was checked: %v", err)
	}

	// Another tenant never sees these rows.
	other := newRangeFixture(t)
	other.store("RNG-S")
	other.item("CHAIN-1", "Chains", 2000, 2400)
	other.mode(AssortmentBlock)
	if _, err := ResolvePOSQuote(other.tenantID, QuoteRequest{CartNumber: "C9", Location: "RNG-S", Lines: []QuoteLineRequest{{Sku: "CHAIN-1", Qty: 1}}}); err != nil {
		t.Errorf("another tenant was ranged by this tenant's rows: %v", err)
	}
}

func TestAssortmentRecordBulkAndReport(t *testing.T) {
	f := newRangeFixture(t)
	f.store("RNG-1")
	f.store("RNG-2")
	f.item("RING-1", "Rings", 1000, 1200)
	f.item("RING-2", "Rings", 1100, 1300)
	f.item("CHAIN-1", "Chains", 2000, 2400)

	bad := []map[string]interface{}{
		{"store": "A", "channel": "B", "item": "X"},
		{"item": "X"},
		{"store": "A"},
		{"store": "A", "item": "X", "category": "Y"},
		{"store": "A", "item": "X", "valid_from": "2026-02-01", "valid_to": "2026-01-01"},
		{"store": "A", "item": "X", "valid_from": "01/02/2026"},
	}
	for i, p := range bad {
		if errCode(ValidateAssortmentRecord(p)) != assortmentCodeShape {
			t.Errorf("bad record %d was accepted: %v", i, p)
		}
	}
	if err := ValidateAssortmentRecord(map[string]interface{}{"store": "A", "item": "X", "valid_from": "2026-01-01"}); err != nil {
		t.Errorf("a good record was refused: %v", err)
	}

	res, err := BulkAddAssortment(f.tenantID, "tester", BulkAssortmentInput{Stores: []string{"RNG-1", "RNG-2"}, Category: "Rings"})
	if err != nil || res.Added != 2 || res.Skipped != 0 {
		t.Fatalf("bulk add: %+v %v", res, err)
	}
	res, err = BulkAddAssortment(f.tenantID, "tester", BulkAssortmentInput{Stores: []string{"RNG-1", "RNG-2"}, Category: "Rings"})
	if err != nil || res.Added != 0 || res.Skipped != 2 {
		t.Fatalf("a repeat bulk add must skip: %+v %v", res, err)
	}
	if _, err := BulkAddAssortment(f.tenantID, "tester", BulkAssortmentInput{Category: "Rings"}); errCode(err) != assortmentCodeBulk {
		t.Errorf("bulk add with no target: %v", err)
	}
	if _, err := BulkAddAssortment(f.tenantID, "tester", BulkAssortmentInput{Stores: []string{"RNG-1"}, Category: "Rings", Items: []string{"X"}}); errCode(err) != assortmentCodeBulk {
		t.Errorf("bulk add with both a category and items: %v", err)
	}
	if _, err := BulkAddAssortment(f.tenantID, "tester", BulkAssortmentInput{Stores: []string{"RNG-1"}, Items: []string{"CHAIN-1"}, ValidFrom: "2026-02-01", ValidTo: "2026-01-01"}); errCode(err) != assortmentCodeShape {
		t.Errorf("bulk add with a bad window: %v", err)
	}
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + f.schema + `.documents WHERE doctype = 'Assortment'`).Scan(&n)
	if n != 2 {
		t.Errorf("a refused bulk add must write nothing; rows = %d", n)
	}

	rows, err := runRangeByStoreReport(f.tenantID, map[string]string{"store": "RNG-1"})
	if err != nil || len(rows) != 2 {
		t.Fatalf("report for RNG-1: %d rows, %v (want RING-1 and RING-2)", len(rows), err)
	}
	all, _ := runRangeByStoreReport(f.tenantID, map[string]string{})
	if len(all) != 4 {
		t.Errorf("report for all stores: %d rows, want 4", len(all))
	}
}

func TestMarkdownPlans(t *testing.T) {
	f := newRangeFixture(t)
	f.store("MD-1")
	f.store("MD-2")
	f.item("RING-1", "Rings", 1000, 1200)
	f.item("RING-2", "Rings", 500, 600)
	f.item("CHAIN-1", "Chains", 2000, 2400)
	plan := func(id string, d map[string]interface{}) {
		if _, ok := d["status"]; !ok {
			d["status"] = MarkdownScheduled
		}
		f.insert(id, "MarkdownPlan", d)
	}
	const day = "2026-06-15"
	price := func(sku, store string, when string, base float64) (float64, bool) {
		return MarkdownPrice(f.tenantID, sku, store, when, base)
	}

	plan("MD-P1", map[string]interface{}{"name": "Ring sale", "category": "Rings", "percent_off": 10, "start_date": "2026-06-10", "end_date": "2026-06-20"})
	if _, ok := price("RING-1", "MD-1", "2026-06-09", 1000); ok {
		t.Errorf("a markdown applied before it starts")
	}
	if p, ok := price("RING-1", "MD-1", "2026-06-10", 1000); !ok || p != 900 {
		t.Errorf("first day: %v %v, want 900", p, ok)
	}
	if p, ok := price("RING-2", "MD-1", "2026-06-20", 500); !ok || p != 450 {
		t.Errorf("last day: %v %v, want 450", p, ok)
	}
	if _, ok := price("RING-1", "MD-1", "2026-06-21", 1000); ok {
		t.Errorf("a markdown applied after it ended")
	}
	if _, ok := price("CHAIN-1", "MD-1", day, 2000); ok {
		t.Errorf("a markdown reached an item outside its category")
	}

	// Store scope: the plan applies at MD-1 only.
	plan("MD-P2", map[string]interface{}{"name": "Chain clearance", "items": "CHAIN-1", "fixed_price": 1500, "stores": "MD-1", "start_date": "2026-06-01", "end_date": "2026-06-30"})
	if p, ok := price("CHAIN-1", "MD-1", day, 2000); !ok || p != 1500 {
		t.Errorf("store in scope: %v %v", p, ok)
	}
	if _, ok := price("CHAIN-1", "MD-2", day, 2000); ok {
		t.Errorf("store outside the scope got the markdown")
	}
	if _, ok := price("CHAIN-1", "", day, 2000); ok {
		t.Errorf("a store-limited plan applied with no store")
	}

	// Overlap: the lowest price wins.
	plan("MD-P3", map[string]interface{}{"name": "Deeper", "category": "Rings", "percent_off": 25, "start_date": "2026-06-12", "end_date": "2026-06-18"})
	if p, _ := price("RING-1", "MD-1", day, 1000); p != 750 {
		t.Errorf("overlap: %v, want the lower 750", p)
	}
	// A fixed price above the base is no markdown.
	plan("MD-P4", map[string]interface{}{"name": "Up", "items": "RING-2", "fixed_price": 900, "start_date": "2026-06-01", "end_date": "2026-06-30"})
	if p, _ := price("RING-2", "MD-1", day, 500); p != 375 {
		t.Errorf("a fixed price above the base changed the result: %v", p)
	}

	// Draft and Cancelled never apply.
	plan("MD-P5", map[string]interface{}{"name": "Draft", "items": "CHAIN-1", "percent_off": 90, "start_date": "2026-06-01", "end_date": "2026-06-30", "status": MarkdownDraft})
	plan("MD-P6", map[string]interface{}{"name": "Cancelled", "items": "CHAIN-1", "percent_off": 90, "start_date": "2026-06-01", "end_date": "2026-06-30", "status": MarkdownCancelled})
	if p, _ := price("CHAIN-1", "MD-1", day, 2000); p != 1500 {
		t.Errorf("a draft or cancelled plan applied: %v", p)
	}
	if _, err := db.DB.Exec(`UPDATE ` + f.schema + `.documents SET data = jsonb_set(data, '{status}', '"Cancelled"') WHERE id = 'MD-P2'`); err != nil {
		t.Fatal(err)
	}
	if _, ok := price("CHAIN-1", "MD-1", day, 2000); ok {
		t.Errorf("cancelling the plan did not take the markdown off")
	}

	// States as the calendar shows them.
	for _, c := range []struct{ status, from, to, want string }{
		{MarkdownScheduled, "2026-06-20", "2026-06-30", MarkdownScheduled},
		{MarkdownScheduled, "2026-06-10", "2026-06-30", MarkdownRunning},
		{MarkdownScheduled, "2026-06-01", "2026-06-10", MarkdownEnded},
		{MarkdownDraft, "2026-06-10", "2026-06-30", MarkdownDraft},
		{MarkdownCancelled, "2026-06-10", "2026-06-30", MarkdownCancelled},
	} {
		if got := MarkdownPlanState(c.status, c.from, c.to, day); got != c.want {
			t.Errorf("state of %+v = %s", c, got)
		}
	}

	// The item master is never rewritten.
	var sale string
	_ = db.DB.QueryRow(`SELECT data->>'sale_price' FROM ` + f.schema + `.documents WHERE id = 'RING-1'`).Scan(&sale)
	if sale != "1000" {
		t.Errorf("the item master price was changed: %s", sale)
	}
}

func TestMarkdownAtTillAndOrders(t *testing.T) {
	f := newRangeFixture(t)
	f.store("MD-1")
	f.item("RING-1", "Rings", 1000, 1200)
	f.insert("MD-T1", "MarkdownPlan", map[string]interface{}{"name": "Now", "items": "RING-1", "percent_off": 20, "status": MarkdownScheduled,
		"start_date": "2000-01-01", "end_date": "2999-12-31", "stores": "MD-1"})
	q, err := ResolvePOSQuote(f.tenantID, QuoteRequest{CartNumber: "MDC1", Location: "MD-1", Lines: []QuoteLineRequest{{Sku: "RING-1", Qty: 1}}})
	if err != nil || len(q.Lines) != 1 || q.Lines[0].ReferencePrice != 800 {
		t.Fatalf("till reference price with a markdown: %v %+v", err, q)
	}
	f.store("MD-2")
	q, err = ResolvePOSQuote(f.tenantID, QuoteRequest{CartNumber: "MDC2", Location: "MD-2", Lines: []QuoteLineRequest{{Sku: "RING-1", Qty: 1}}})
	if err != nil || q.Lines[0].ReferencePrice != 1000 {
		t.Errorf("a store outside the plan paid the markdown price: %v %+v", err, q.Lines)
	}

	f.insert("MD-T2", "MarkdownPlan", map[string]interface{}{"name": "All", "items": "RING-1", "percent_off": 30, "status": MarkdownScheduled,
		"start_date": "2000-01-01", "end_date": "2999-12-31"})
	lines := []SalesOrderLineInput{{SKU: "RING-1", Qty: 1}, {SKU: "RING-1", Qty: 1, UnitPrice: 950}}
	fillMarkdownRates(f.tenantID, lines)
	if lines[0].UnitPrice != 700 {
		t.Errorf("a blank order rate should take the marked-down price, got %v", lines[0].UnitPrice)
	}
	if lines[1].UnitPrice != 950 {
		t.Errorf("an entered order rate must stay, got %v", lines[1].UnitPrice)
	}
	if p, _ := storeAppReferencePrice(f.tenantID, "MD-1", map[string]interface{}{"sale_price": 1000.0}, "RING-1"); p != 700 {
		t.Errorf("store app price = %v, want 700", p)
	}
	if p, _ := shopifyItemPrice(f.tenantID, "", map[string]interface{}{"sale_price": 1000.0, "mrp": 1200.0}, "RING-1"); p != 700 {
		t.Errorf("channel price = %v, want 700", p)
	}
}

func TestMarkdownPlanRecord(t *testing.T) {
	good := map[string]interface{}{"name": "A", "items": "X", "percent_off": 10, "start_date": "2026-01-01", "end_date": "2026-01-31"}
	if err := ValidateMarkdownPlan(good); err != nil {
		t.Fatalf("a good plan was refused: %v", err)
	}
	mut := func(k string, v interface{}) map[string]interface{} {
		m := map[string]interface{}{}
		for a, b := range good {
			m[a] = b
		}
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
		return m
	}
	for name, p := range map[string]map[string]interface{}{
		"no name":          mut("name", nil),
		"no scope":         mut("items", nil),
		"both scopes":      mut("category", "Rings"),
		"no discount":      mut("percent_off", nil),
		"both discounts":   mut("fixed_price", 100),
		"percent 100":      mut("percent_off", 100),
		"end before start": mut("end_date", "2025-12-31"),
		"bad date":         mut("start_date", "1/1/2026"),
	} {
		if errCode(ValidateMarkdownPlan(p)) != markdownCodeShape {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestListingDrift(t *testing.T) {
	f := newRangeFixture(t)
	f.item("RING-1", "Rings", 1000, 1200)
	f.item("RING-2", "Rings", 500, 600)
	f.item("RING-3", "Rings", 700, 700)
	f.insert("RING-4", "Item", map[string]interface{}{"code": "RING-4", "name": "RING-4 name", "sale_price": 300.0, "mrp": 300.0, "status": "Inactive"})
	f.item("RING-5", "Rings", 800, 900)
	ins := func(ref, sku, title string, price, compare float64, state string) {
		if _, err := db.DB.Exec(`INSERT INTO `+f.schema+`.channel_listings (channel, listing_ref, channel_sku, title, price, compare_at, sku, state) VALUES ('shop-x',$1,$1,$2,$3,$4,$5,$6)`,
			ref, title, price, compare, sku, state); err != nil {
			t.Fatalf("listing %s: %v", ref, err)
		}
	}

	rows, err := ChannelDrift(f.tenantID, "shop-x")
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty state: %v rows=%d", err, len(rows))
	}
	if _, err := ChannelDrift(f.tenantID, ""); errCode(err) != listingDriftCodeChannel {
		t.Errorf("no channel: %v", err)
	}
	// 245.2: through the report runner the refusal keeps its code (422), not
	// REPORT-0162 "could not be generated" (503); with no channel at all the
	// runner asks for it before the report runs.
	if _, _, _, err := RunReportContext(context.Background(), f.tenantID, "listing-drift", RoleSuperAdmin, "", map[string]string{"channel": "  "}); errCode(err) != listingDriftCodeChannel {
		t.Errorf("blank channel through the runner: %v", err)
	}
	if _, _, _, err := RunReportContext(context.Background(), f.tenantID, "listing-drift", RoleSuperAdmin, "", map[string]string{}); err == nil || errCode(err) == "REPORT-0162" {
		t.Errorf("no channel through the runner: %v", err)
	}

	ins("L-OK", "RING-1", "ring-1  NAME", 1000, 1200, "Listed") // matches, case and spacing ignored
	ins("L-TITLE", "RING-2", "Old name", 500, 600, "Listed")    // title
	ins("L-PRICE", "RING-3", "RING-3 name", 650, 0, "Listed")   // price
	ins("L-MRP", "RING-5", "RING-5 name", 800, 950, "Listed")   // MRP
	ins("L-OFF", "RING-4", "RING-4 name", 300, 0, "Listed")     // item inactive, still listed
	ins("L-NOSKU", "", "Unlinked", 1, 0, "Listed")              // not linked: not judged
	rows, err = ChannelDrift(f.tenantID, "shop-x")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.ListingRef+":"+r.Kind] = r.Listing + "|" + r.Master
	}
	want := map[string]string{
		"L-TITLE:Title": "Old name|RING-2 name",
		"L-PRICE:Price": "650.00|700.00",
		"L-MRP:MRP":     "950.00|900.00",
		"L-OFF:Status":  "Listed|Not active",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("drift %s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected drift rows: %v", got)
	}

	// Gone from the channel while the item is active; republish is on offer.
	ins("L-GONE", "RING-1", "x", 0, 0, "Gone")
	rows, _ = ChannelDrift(f.tenantID, "shop-x")
	found := false
	for _, r := range rows {
		if r.ListingRef == "L-GONE" && r.Kind == "Status" && r.CanPublish {
			found = true
		}
	}
	if !found {
		t.Errorf("a Gone listing of an active item was not reported: %+v", rows)
	}

	// A running markdown lowers the expected price.
	f.insert("MD-D1", "MarkdownPlan", map[string]interface{}{"name": "n", "items": "RING-3", "percent_off": 50, "status": MarkdownScheduled, "start_date": "2000-01-01", "end_date": "2999-12-31"})
	ins("L-MD", "RING-3", "RING-3 name", 350, 0, "Listed")
	rows, _ = ChannelDrift(f.tenantID, "shop-x")
	for _, r := range rows {
		if r.ListingRef == "L-MD" && r.Kind == "Price" {
			t.Errorf("a listing at the marked-down price was reported: %+v", r)
		}
	}

	// Another tenant sees none of it.
	other := newRangeFixture(t)
	rows, err = ChannelDrift(other.tenantID, "shop-x")
	if err != nil || len(rows) != 0 {
		t.Errorf("another tenant saw drift: %v %d", err, len(rows))
	}
}
