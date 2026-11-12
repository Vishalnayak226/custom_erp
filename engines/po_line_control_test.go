package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Stage 148.11 - PO line control: instalments, stop a line, vendor
// confirmation, under / over % read live, full delivery only, auto-close
// within the under tolerance, duplicate challan warning, all-cancelled
// transfer, overdue-lines list.

type poLCFixture struct {
	t        *testing.T
	tenantID string
	schema   string
}

func newPOLCFixture(t *testing.T) *poLCFixture {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	return &poLCFixture{t: t, tenantID: tenantID, schema: schema}
}

func (f *poLCFixture) insert(id, doctype, status string, data map[string]interface{}) {
	f.t.Helper()
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+f.schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
		f.t.Fatalf("insert %s: %v", id, err)
	}
}

func (f *poLCFixture) po(id string, lines ...map[string]interface{}) {
	f.t.Helper()
	raw, _ := json.Marshal(lines)
	f.insert(id, "PurchaseOrder", "Approved", map[string]interface{}{"code": id, "vendor": "LC-V", "location": "LC-LOC", "gst_mode": GSTModeExclusive, "items": string(raw)})
}

func (f *poLCFixture) grn(id, po, sku string, qty, accepted int) map[string]interface{} {
	raw, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": qty, "accepted_qty": accepted}})
	return map[string]interface{}{"code": id, "po_id": po, "vendor": "LC-V", "location": "LC-LOC", "received_items": string(raw)}
}

func (f *poLCFixture) post(id, po, sku string, qty int) {
	f.t.Helper()
	f.insert(id, "GRN", "Posted", f.grn(id, po, sku, qty, qty))
}

func (f *poLCFixture) validate(po, sku string, qty int) string {
	f.t.Helper()
	return lcCode(validateGRNRules(f.tenantID, "", f.grn("LC-NEW", po, sku, qty, qty)))
}

func lcCode(err error) string {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Code
	}
	if err != nil {
		return "ERR:" + err.Error()
	}
	return ""
}

func (f *poLCFixture) base() {
	f.insert("LC-A", "Item", "Active", map[string]interface{}{"code": "LC-A", "name": "Line control A", "hsn_code": "6205", "gst_rate": 5.0})
	f.insert("LC-B", "Item", "Active", map[string]interface{}{"code": "LC-B", "name": "Line control B", "hsn_code": "6205", "gst_rate": 5.0})
	f.insert("LC-V", "Vendor", "Active", map[string]interface{}{"code": "LC-V", "name": "LC Vendor"})
	f.insert("RC-LC", "ReasonCode", "Active", map[string]interface{}{"code": "RC-LC", "category": "Cancellation", "description": "Not needed"})
}

func (f *poLCFixture) setItem(sku string, patch map[string]interface{}) {
	f.t.Helper()
	b, _ := json.Marshal(patch)
	if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET data = data || $1::jsonb WHERE doctype = 'Item' AND id = $2", b, sku); err != nil {
		f.t.Fatalf("set item: %v", err)
	}
}

// A PO with none of the controls set receives exactly as today.
func TestStage14811NoControlReceivesAsToday(t *testing.T) {
	f := newPOLCFixture(t)
	f.base()
	f.po("LC-PO-0", map[string]interface{}{"sku": "LC-A", "qty": 10, "rate": 100.0})
	if got := f.validate("LC-PO-0", "LC-A", 11); got != "PURCHA-0087" {
		t.Fatalf("11 of 10: want PURCHA-0087, got %q", got)
	}
	if got := f.validate("LC-PO-0", "LC-A", 9); got != "" {
		t.Fatalf("9 of 10 is a partial receipt, got %q", got)
	}
	f.post("LC-G0", "LC-PO-0", "LC-A", 9)
	if n, err := AutoClosePOLines(f.tenantID, "LC-PO-0", "system"); err != nil || n != 0 {
		t.Fatalf("no tolerance set: nothing closes, got %d %v", n, err)
	}
	if got := f.validate("LC-PO-0", "LC-A", 2); got != "PURCHA-0087" {
		t.Fatalf("2 against 1 open: want PURCHA-0087, got %q", got)
	}
	if got := f.validate("LC-PO-0", "LC-A", 1); got != "" {
		t.Fatalf("the last unit: %q", got)
	}
}

// Under / over % come from the item, change on the line, and are read live.
func TestStage14811ToleranceFromItemLineAndLive(t *testing.T) {
	f := newPOLCFixture(t)
	f.base()
	f.po("LC-PO-1", map[string]interface{}{"sku": "LC-A", "qty": 10, "rate": 100.0})
	f.setItem("LC-A", map[string]interface{}{"over_delivery_pct": 20})
	if got := f.validate("LC-PO-1", "LC-A", 12); got != "" {
		t.Fatalf("item over 20%%: 12 of 10 should pass, got %q", got)
	}
	if got := f.validate("LC-PO-1", "LC-A", 13); got != "PURCHA-0087" {
		t.Fatalf("item over 20%%: 13 of 10, got %q", got)
	}
	// Read live: the item changes after the PO was written.
	f.setItem("LC-A", map[string]interface{}{"over_delivery_pct": 30})
	if got := f.validate("LC-PO-1", "LC-A", 13); got != "" {
		t.Fatalf("item raised to 30%% after the PO: 13 should pass, got %q", got)
	}
	// The line overrides the item.
	if err := SetPOLineControl(f.tenantID, "LC-PO-1", "buyer", 1, map[string]interface{}{"over_pct": 5}); err != nil {
		t.Fatalf("set over on the line: %v", err)
	}
	if got := f.validate("LC-PO-1", "LC-A", 11); got != "PURCHA-0087" {
		t.Fatalf("line over 5%%: 11 of 10, got %q", got)
	}
	if got := f.validate("LC-PO-1", "LC-A", 10); got != "" {
		t.Fatalf("line over 5%%: 10 of 10, got %q", got)
	}
	if err := SetPOLineControl(f.tenantID, "LC-PO-1", "buyer", 1, map[string]interface{}{"over_pct": 150}); lcCode(err) != "PURCHA-4987" {
		t.Fatalf("150%% is not a percentage, got %v", err)
	}
	// Clearing the line goes back to the item.
	if err := SetPOLineControl(f.tenantID, "LC-PO-1", "buyer", 1, map[string]interface{}{"over_pct": nil}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := f.validate("LC-PO-1", "LC-A", 13); got != "" {
		t.Fatalf("cleared line goes back to the item's 30%%, got %q", got)
	}
}

// Receiving at least (100 - under %) closes the rest.
func TestStage14811UnderToleranceClosesTheRest(t *testing.T) {
	f := newPOLCFixture(t)
	f.base()
	f.po("LC-PO-2", map[string]interface{}{"sku": "LC-A", "qty": 10, "rate": 100.0}, map[string]interface{}{"sku": "LC-B", "qty": 10, "rate": 50.0})
	f.setItem("LC-A", map[string]interface{}{"under_delivery_pct": 10})
	f.post("LC-G2", "LC-PO-2", "LC-A", 8)
	if n, _ := AutoClosePOLines(f.tenantID, "LC-PO-2", "system"); n != 0 {
		t.Fatalf("8 of 10 is below 90%%: nothing closes, got %d", n)
	}
	f.post("LC-G2B", "LC-PO-2", "LC-A", 1)
	n, err := AutoClosePOLines(f.tenantID, "LC-PO-2", "system")
	if err != nil || n != 1 {
		t.Fatalf("9 of 10 is 90%%: line 1 closes, got %d %v", n, err)
	}
	if got := f.validate("LC-PO-2", "LC-A", 1); got != "PURCHA-0087" {
		t.Fatalf("the closed rest takes no more, got %q", got)
	}
	rec, err := PurchaseOrderReconciliation(f.tenantID, "LC-PO-2")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range rec.Lines {
		if l.Sku == "LC-A" && (l.Ordered != 10 || l.Open != 0 || l.ShortClosed != 1) {
			t.Fatalf("reconciliation of the closed line: %+v", l)
		}
		if l.Sku == "LC-B" && l.Open != 10 {
			t.Fatalf("line 2 untouched: %+v", l)
		}
	}
	// Line B is still open, so the PO is not fully received.
	if full, _ := purchaseOrderFullyReceived(f.tenantID, "LC-PO-2", ""); full {
		t.Fatal("line B is still open")
	}
	f.post("LC-G2C", "LC-PO-2", "LC-B", 10)
	if full, _ := purchaseOrderFullyReceived(f.tenantID, "LC-PO-2", ""); !full {
		t.Fatal("A closed within tolerance and B in full: the PO is complete")
	}
}

// A stopped line takes no receipt; the PO is complete without it.
func TestStage14811StopLine(t *testing.T) {
	f := newPOLCFixture(t)
	f.base()
	f.po("LC-PO-3", map[string]interface{}{"sku": "LC-A", "qty": 10, "rate": 100.0}, map[string]interface{}{"sku": "LC-B", "qty": 4, "rate": 50.0})
	if err := StopPOLine(f.tenantID, "LC-PO-3", "buyer", 1, "", "x"); lcCode(err) != "PURCHA-4988" {
		t.Fatalf("a stop needs a reason, got %v", err)
	}
	if err := StopPOLine(f.tenantID, "LC-PO-3", "buyer", 1, "RC-LC", "vendor discontinued"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if got := f.validate("LC-PO-3", "LC-A", 1); got != "PURCHA-4985" {
		t.Fatalf("a stopped line: want PURCHA-4985, got %q", got)
	}
	if got := f.validate("LC-PO-3", "LC-B", 4); got != "" {
		t.Fatalf("the other line still receives, got %q", got)
	}
	f.post("LC-G3", "LC-PO-3", "LC-B", 4)
	if full, _ := purchaseOrderFullyReceived(f.tenantID, "LC-PO-3", ""); !full {
		t.Fatal("the stopped line no longer holds the PO open")
	}
	// A stopped line also refuses the step-flow path: its qty is excess.
	g := &stepGRN{id: "LC-STEP", data: map[string]interface{}{"vendor": "LC-V", "po_id": "LC-PO-3"},
		lines: []map[string]interface{}{{"line_no": 1.0, "sku": "LC-A", "po_id": "LC-PO-3", "grn_qty": 2.0}}}
	ex, err := grnExcessByLine(f.tenantID, g)
	if err != nil || ex[1] != 2 {
		t.Fatalf("step flow: stopped line's 2 units are excess, got %v %v", ex, err)
	}
}

func TestStage14811FullDeliveryOnly(t *testing.T) {
	f := newPOLCFixture(t)
	f.base()
	f.po("LC-PO-4", map[string]interface{}{"sku": "LC-A", "qty": 10, "rate": 100.0, "full_delivery_only": true})
	if got := f.validate("LC-PO-4", "LC-A", 6); got != "PURCHA-4986" {
		t.Fatalf("part delivery of a full-delivery line: want PURCHA-4986, got %q", got)
	}
	if got := f.validate("LC-PO-4", "LC-A", 10); got != "" {
		t.Fatalf("the full delivery: %q", got)
	}
	f.setItem("LC-A", map[string]interface{}{"under_delivery_pct": 10})
	if got := f.validate("LC-PO-4", "LC-A", 9); got != "" {
		t.Fatalf("90%% counts as full with 10%% under: %q", got)
	}
	if got := f.validate("LC-PO-4", "LC-A", 8); got != "PURCHA-4986" {
		t.Fatalf("80%% is not full, got %q", got)
	}
	// Step flow: Verify reads the same rule.
	g := &stepGRN{id: "LC-STEP4", data: map[string]interface{}{"vendor": "LC-V", "po_id": "LC-PO-4"},
		lines: []map[string]interface{}{{"line_no": 1.0, "sku": "LC-A", "po_id": "LC-PO-4", "grn_qty": 5.0}}}
	if got := lcCode(grnFullDeliveryCheck(f.tenantID, g)); got != "PURCHA-4986" {
		t.Fatalf("step flow part delivery: want PURCHA-4986, got %q", got)
	}
}

func TestStage14811InstalmentsAndOverdueList(t *testing.T) {
	f := newPOLCFixture(t)
	f.base()
	f.po("LC-PO-5", map[string]interface{}{"sku": "LC-A", "qty": 10, "rate": 100.0})
	bad := []map[string]interface{}{{"qty": 4, "date": "2026-10-01"}, {"qty": 4, "date": "2026-11-01"}}
	if err := SetPOLineControl(f.tenantID, "LC-PO-5", "buyer", 1, map[string]interface{}{"instalments": bad}); lcCode(err) != "PURCHA-4987" {
		t.Fatalf("instalments must add up to the line: got %v", err)
	}
	ok := []map[string]interface{}{{"qty": 4, "date": "2026-10-01"}, {"qty": 6, "date": "2026-11-01"}}
	if err := SetPOLineControl(f.tenantID, "LC-PO-5", "buyer", 1, map[string]interface{}{
		"instalments": ok, "confirmation_no": "VC-77", "confirmation_date": "2026-09-20"}); err != nil {
		t.Fatalf("set instalments: %v", err)
	}
	rec, _, _ := fetchDocData(f.tenantID, "PurchaseOrder", "LC-PO-5")
	if s := fmt.Sprint(rec["items"]); !strings.Contains(s, "VC-77") || !strings.Contains(s, "2026-11-01") {
		t.Fatalf("instalments and confirmation are stored on the line: %s", s)
	}
	// The PO stays open until all instalments are in.
	f.post("LC-G5", "LC-PO-5", "LC-A", 4)
	if full, _ := purchaseOrderFullyReceived(f.tenantID, "LC-PO-5", ""); full {
		t.Fatal("first instalment in, second not: the PO is open")
	}
	rows, err := POOverdueLines(f.tenantID, "2026-11-10")
	if err != nil {
		t.Fatal(err)
	}
	var mine []map[string]interface{}
	for _, r := range rows {
		if r["po"] == "LC-PO-5" {
			mine = append(mine, r)
		}
	}
	if len(mine) != 1 || mine[0]["due"] != "2026-11-01" || numFromInterface(mine[0]["pending"]) != 6 || numFromInterface(mine[0]["days_late"]) != 9 {
		t.Fatalf("overdue rows: %v", mine)
	}
	rows, _ = POOverdueLines(f.tenantID, "2026-10-20")
	for _, r := range rows {
		if r["po"] == "LC-PO-5" {
			t.Fatalf("the first instalment came in and the second is not due yet: %v", r)
		}
	}
	// Close rest from the list.
	closed, err := ClosePOLineRest(f.tenantID, "LC-PO-5", "buyer", 1, "RC-LC", "")
	if err != nil || closed != 6 {
		t.Fatalf("close rest: %v %v", closed, err)
	}
	if full, _ := purchaseOrderFullyReceived(f.tenantID, "LC-PO-5", ""); !full {
		t.Fatal("rest closed: the PO is complete")
	}
	rows, _ = POOverdueLines(f.tenantID, "2026-11-10")
	for _, r := range rows {
		if r["po"] == "LC-PO-5" {
			t.Fatalf("a closed line is not overdue: %v", r)
		}
	}
	// The report is registered and runs through the framework.
	if _, ok := reportRegistry["po-overdue-lines"]; !ok {
		t.Fatal("report po-overdue-lines is not registered")
	}
}

// The control survives a PO amendment that rebuilds the lines from the form.
func TestStage14811AmendmentKeepsControl(t *testing.T) {
	f := newPOLCFixture(t)
	f.base()
	f.po("LC-PO-6", map[string]interface{}{"sku": "LC-A", "qty": 10, "rate": 100.0, "over_pct": 7, "confirmation_no": "KEEP-1"})
	prior, _, _ := fetchDocData(f.tenantID, "PurchaseOrder", "LC-PO-6")
	newItems := `[{"sku":"LC-A","qty":12,"rate":100}]`
	payload := map[string]interface{}{"items": newItems, "amendment_reason": "more", "vendor": "LC-V"}
	if err := validatePurchaseOrderEditRules(f.tenantID, "LC-PO-6", "Approved", prior, payload); err != nil {
		t.Fatalf("amend: %v", err)
	}
	if s := fmt.Sprint(payload["items"]); !strings.Contains(s, "KEEP-1") || !strings.Contains(s, `"over_pct"`) {
		t.Fatalf("line control must be carried over: %s", s)
	}
}

func TestStage14811DuplicateChallanWarning(t *testing.T) {
	f := newPOLCFixture(t)
	f.base()
	prior := map[string]interface{}{"code": "LC-GC1", "vendor": "LC-V", "vendor_challan_no": "CH-9", "vendor_challan_date": BusinessToday()}
	f.insert("LC-GC1", "GRN", "Posted", prior)
	if got := VendorChallanSeenThisYear(f.tenantID, "LC-GC2", "LC-V", " ch-9 ", BusinessToday()); got != "LC-GC1" {
		t.Fatalf("same vendor and challan this year: want LC-GC1, got %q", got)
	}
	if got := VendorChallanSeenThisYear(f.tenantID, "LC-GC1", "LC-V", "CH-9", BusinessToday()); got != "" {
		t.Fatalf("the receipt itself is not a duplicate, got %q", got)
	}
	if got := VendorChallanSeenThisYear(f.tenantID, "LC-GC2", "LC-V", "CH-9", "2019-01-05"); got != "" {
		t.Fatalf("an older year is a different challan series, got %q", got)
	}
	if got := VendorChallanSeenThisYear(f.tenantID, "LC-GC2", "OTHER-V", "CH-9", BusinessToday()); got != "" {
		t.Fatalf("another vendor, got %q", got)
	}
}

func TestStage14811TransferLinesAndTolerance(t *testing.T) {
	f := newPOLCFixture(t)
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	const from, to = "LC-TF-A", "LC-TF-B"
	for _, loc := range []string{from, to} {
		f.insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": loc})
	}
	f.insert("RC-LC", "ReasonCode", "Active", map[string]interface{}{"code": "RC-LC", "category": "Cancellation"})
	for _, sku := range []string{"LC-T1", "LC-T2"} {
		f.insert("ITEM-"+sku, "Item", "Active", map[string]interface{}{"code": sku, "name": sku, "standard_cost": 10})
		exec("INSERT INTO "+f.schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)", sku, from)
	}
	order := func(id string, lines ...map[string]interface{}) {
		items, _ := json.Marshal(lines)
		f.insert(id, "TransferOrder", "Approved", map[string]interface{}{"transfer_number": id, "from_warehouse": from, "to_warehouse": to, "status": "Approved", "items": string(items)})
	}

	// Some lines cancelled: the transfer goes on with the rest.
	order("LC-TO-1", map[string]interface{}{"sku": "LC-T1", "qty": 5}, map[string]interface{}{"sku": "LC-T2", "qty": 5})
	if err := CancelTransferLines(f.tenantID, "LC-TO-1", "buyer", "", []string{"LC-T1"}); lcCode(err) != "PURCHA-4989" {
		t.Fatalf("cancelling lines needs a reason, got %v", err)
	}
	if err := CancelTransferLines(f.tenantID, "LC-TO-1", "buyer", "RC-LC", []string{"LC-T1"}); err != nil {
		t.Fatalf("cancel one line: %v", err)
	}
	if _, st := lcDoc(t, f, "LC-TO-1"); st != "Approved" {
		t.Fatalf("one line left: still Approved, got %s", st)
	}
	if err := DispatchTransferOrder(f.tenantID, "LC-TO-1", "system"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var inTransit int
	db.DB.QueryRow("SELECT COALESCE(SUM(in_transit),0) FROM " + f.schema + ".inventory_availability WHERE sku = 'LC-T1'").Scan(&inTransit)
	if inTransit != 0 {
		t.Fatalf("a cancelled line is not dispatched, in transit = %d", inTransit)
	}
	// All lines cancelled: the transfer shows Cancelled.
	order("LC-TO-2", map[string]interface{}{"sku": "LC-T1", "qty": 5}, map[string]interface{}{"sku": "LC-T2", "qty": 5})
	if err := CancelTransferLines(f.tenantID, "LC-TO-2", "buyer", "RC-LC", []string{"LC-T1", "LC-T2"}); err != nil {
		t.Fatalf("cancel all: %v", err)
	}
	if _, st := lcDoc(t, f, "LC-TO-2"); st != "Cancelled" {
		t.Fatalf("every line cancelled: want Cancelled, got %s", st)
	}

	// Under tolerance on the receiving side: 9 of 10 within the item's 10%
	// closes without a shortage reason; without it the reason is demanded.
	f.setItem("ITEM-LC-T1", map[string]interface{}{"under_delivery_pct": 10, "over_delivery_pct": 20})
	order("LC-TO-3", map[string]interface{}{"sku": "LC-T1", "qty": 10})
	if err := DispatchTransferOrder(f.tenantID, "LC-TO-3", "system"); err != nil {
		t.Fatalf("dispatch 3: %v", err)
	}
	if _, err := ReceiveTransferOrderLines(f.tenantID, "LC-TO-3", "system", TransferReceiptRequest{Lines: []TransferReceiptLine{{Sku: "LC-T1", Qty: 13, ExcessQty: 3}}}); lcCode(err) != "PURCHA-4990" {
		t.Fatalf("excess beyond over 20%%: want PURCHA-4990, got %v", err)
	}
	if _, err := ReceiveTransferOrderLines(f.tenantID, "LC-TO-3", "system", TransferReceiptRequest{Lines: []TransferReceiptLine{{Sku: "LC-T1", Qty: 9}}}); err != nil {
		t.Fatalf("9 of 10 within the 10%% under tolerance: %v", err)
	}
	if _, st := lcDoc(t, f, "LC-TO-3"); st != "Received" {
		t.Fatalf("transfer 3 should be Received, got %s", st)
	}
	order("LC-TO-4", map[string]interface{}{"sku": "LC-T2", "qty": 10})
	if err := DispatchTransferOrder(f.tenantID, "LC-TO-4", "system"); err != nil {
		t.Fatalf("dispatch 4: %v", err)
	}
	if _, err := ReceiveTransferOrderLines(f.tenantID, "LC-TO-4", "system", TransferReceiptRequest{Lines: []TransferReceiptLine{{Sku: "LC-T2", Qty: 9}}}); lcCode(err) != "TRN-0259" {
		t.Fatalf("no tolerance: the shortage still needs its reason, got %v", err)
	}
}

func lcDoc(t *testing.T, f *poLCFixture, id string) (map[string]interface{}, string) {
	t.Helper()
	d, st, err := fetchDocData(f.tenantID, "TransferOrder", id)
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return d, st
}

// Free units (148.7) are owed on the line, so they count before it closes.
func TestStage14811FreeUnitsAreDueOnTheLine(t *testing.T) {
	f := newPOLCFixture(t)
	f.base()
	f.po("LC-PO-F", map[string]interface{}{"sku": "LC-A", "qty": 10, "free_qty": 2, "rate": 100.0})
	f.setItem("LC-A", map[string]interface{}{"under_delivery_pct": 10})
	f.post("LC-GF1", "LC-PO-F", "LC-A", 10)
	if n, _ := AutoClosePOLines(f.tenantID, "LC-PO-F", "system"); n != 0 {
		t.Fatalf("10 of 12 is below 90%%: nothing closes, got %d", n)
	}
	if full, _ := purchaseOrderFullyReceived(f.tenantID, "LC-PO-F", ""); full {
		t.Fatal("the 2 free units are still to come")
	}
	f.post("LC-GF2", "LC-PO-F", "LC-A", 1)
	n, err := AutoClosePOLines(f.tenantID, "LC-PO-F", "system")
	if err != nil || n != 1 {
		t.Fatalf("11 of 12 is over 90%%: the last unit closes, got %d %v", n, err)
	}
	if full, _ := purchaseOrderFullyReceived(f.tenantID, "LC-PO-F", ""); !full {
		t.Fatal("nothing is owed after the close")
	}
}
