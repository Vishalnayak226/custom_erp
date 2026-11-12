package engines

import (
	"encoding/json"
	"fmt"
	"testing"

	"custom_erp/db"
)

type inspFix struct {
	tenant, schema string
	insert         func(id, doctype, status string, data map[string]interface{})
}

func inspTenant(t *testing.T) inspFix {
	t.Helper()
	tenantID, schema, cleanup, ins := gateEntryTestTenant(t)
	t.Cleanup(cleanup)
	ins("I-GOLD", "Item", "Active", map[string]interface{}{"code": "I-GOLD", "name": "Gold", "category": "Gold", "quality_required": "Yes"})
	ins("I-PLAIN", "Item", "Active", map[string]interface{}{"code": "I-PLAIN", "name": "Plain", "category": "Plain"})
	ins("I-OPT", "Item", "Active", map[string]interface{}{"code": "I-OPT", "name": "Optional", "category": "Plain", "quality_required": "Yes", "qc_mode": qcModeDecide})
	return inspFix{tenantID, schema, ins}
}

func (f inspFix) on(t *testing.T) {
	t.Helper()
	if err := SetSetting(f.tenant, SettingInspectionByRule, "true", "test"); err != nil {
		t.Fatal(err)
	}
}

// receive posts a GRN of the given lines and returns its id.
func (f inspFix) receive(t *testing.T, grn, vendor, loc string, lines ...map[string]interface{}) {
	t.Helper()
	f.insert(grn, "GRN", "Draft", map[string]interface{}{"code": grn, "vendor": vendor, "location": loc})
	items := make([]interface{}, len(lines))
	for i, l := range lines {
		items[i] = l
	}
	if _, err := PostGRNReceiptWithQC(f.tenant, loc, items, "system", grn); err != nil {
		t.Fatalf("receive %s: %v", grn, err)
	}
}

func (f inspFix) stock(t *testing.T, sku, loc string) (onHand, avail, hold int) {
	t.Helper()
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT on_hand, available, COALESCE(qc_hold, 0) FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, f.schema), sku, loc).Scan(&onHand, &avail, &hold); err != nil {
		t.Fatalf("stock %s: %v", sku, err)
	}
	return
}

func (f inspFix) lots(t *testing.T, grn string) []string {
	t.Helper()
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'InspectionLot' AND data->>'grn_id' = $1 ORDER BY id`, f.schema), grn)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		out = append(out, id)
	}
	return out
}

func (f inspFix) journal(t *testing.T, grn string) string {
	t.Helper()
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT account_code, debit, credit FROM %s.gl_postings WHERE document_id = $1 ORDER BY account_code, debit, credit`, f.schema), grn)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	s := ""
	for rows.Next() {
		var a string
		var d, c int
		_ = rows.Scan(&a, &d, &c)
		s += fmt.Sprintf("%s:%d:%d;", a, d, c)
	}
	return s
}

func line(sku string, qty float64) map[string]interface{} {
	return map[string]interface{}{"sku": sku, "qty": qty, "accepted_qty": qty}
}

// Off by default: a flagged item is received exactly as an unflagged one.
func TestStage14833OffIsToday(t *testing.T) {
	f := inspTenant(t)
	f.receive(t, "G-OFF", "V1", "L1", line("I-GOLD", 10), line("I-PLAIN", 10))
	if _, a, h := f.stock(t, "I-GOLD", "L1"); a != 10 || h != 0 {
		t.Errorf("setting off: available %d hold %d, want 10 and 0", a, h)
	}
	if n := len(f.lots(t, "G-OFF")); n != 0 {
		t.Errorf("setting off: %d lots filed", n)
	}
	if err := inspectionBlocksBill(f.tenant, map[string]interface{}{"grn_id": "G-OFF"}); err != nil {
		t.Errorf("setting off: bill blocked: %v", err)
	}
	if _, err := ListInspectionLots(f.tenant, "", ""); err == nil {
		t.Errorf("setting off: lots route should refuse")
	}
	if _, err := RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: "x"}); err == nil {
		t.Errorf("setting off: results route should refuse")
	}
	if fields, _ := GetDocTypeMeta(f.tenant, "Item"); true {
		for _, fl := range fields {
			if fl.Fieldname == "quality_required" || fl.Fieldname == "qc_mode" {
				t.Errorf("setting off: Item meta shows %s", fl.Fieldname)
			}
		}
	}
}

// On: the flagged item's lot is held (qc_hold, not available) in the same
// post; the journal is the one an unflagged receipt makes; an unflagged item is untouched.
func TestStage14833FlaggedLotIsHeld(t *testing.T) {
	f := inspTenant(t)
	f.on(t)
	f.receive(t, "G-HOLD", "V1", "L1", line("I-GOLD", 10), line("I-PLAIN", 10))
	on, a, h := f.stock(t, "I-GOLD", "L1")
	if on != 10 || a != 0 || h != 10 {
		t.Errorf("held lot: on_hand %d available %d qc_hold %d, want 10/0/10", on, a, h)
	}
	if _, a, h := f.stock(t, "I-PLAIN", "L1"); a != 10 || h != 0 {
		t.Errorf("unflagged item: available %d hold %d, want 10/0", a, h)
	}
	lots := f.lots(t, "G-HOLD")
	if len(lots) != 1 {
		t.Fatalf("lots = %v, want one", lots)
	}
	d, status, _ := fetchDocData(f.tenant, InspectionLotDoctype, lots[0])
	if status != lotPending || numFromInterface(d["qty"]) != 10 || numFromInterface(d["sample_size"]) != 1 || strField(d, "reason") != "Item" {
		t.Errorf("lot = %v status %s", d, status)
	}
	var moves int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1 AND data->>'to_status' = 'QC-Hold' AND data->>'from_status' = 'Available'`, f.schema), lots[0]).Scan(&moves)
	if moves != 1 {
		t.Errorf("stock ledger status moves = %d, want 1", moves)
	}
	// The journal is the one an unflagged receipt makes: a flagged and an unflagged
	// GRN of the same quantity and rate post identical lines, and only the
	// available / qc_hold split of the stock differs.
	cost := func(grn, sku string) {
		raw, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 4, "accepted_qty": 4, "invoice_rate": 250}})
		f.insert(grn, "GRN", "Draft", map[string]interface{}{"code": grn, "vendor": "V2", "location": "L2", "received_items": string(raw)})
		if _, err := PostGRNReceiptWithQC(f.tenant, "L2", []interface{}{map[string]interface{}{"sku": sku, "qty": 4, "accepted_qty": 4}}, "system", grn); err != nil {
			t.Fatal(err)
		}
	}
	cost("G-JF", "I-GOLD")
	cost("G-JP", "I-PLAIN")
	jf, jp := f.journal(t, "G-JF"), f.journal(t, "G-JP")
	if jf == "" {
		t.Fatalf("flagged receipt posted no journal; the comparison would prove nothing")
	}
	if jf != jp {
		t.Errorf("journal differs: flagged %q, unflagged %q", jf, jp)
	}
	if on, a, h := f.stock(t, "I-GOLD", "L2"); on != 4 || a != 0 || h != 4 {
		t.Errorf("flagged buckets %d/%d/%d, want 4/0/4", on, a, h)
	}
	if on, a, h := f.stock(t, "I-PLAIN", "L2"); on != 4 || a != 4 || h != 0 {
		t.Errorf("unflagged buckets %d/%d/%d, want 4/4/0", on, a, h)
	}
	if err := inspectionBlocksBill(f.tenant, map[string]interface{}{"grn_id": "G-HOLD"}); err == nil {
		t.Errorf("a pending lot should hold the bill")
	}
	// A replay of the same receipt part files no second lot.
	if _, err := PostGRNReceiptWithQC(f.tenant, "L1", []interface{}{line("I-GOLD", 10)}, "system", "G-HOLD"); err != nil {
		t.Fatal(err)
	}
	if n := len(f.lots(t, "G-HOLD")); n != 1 {
		t.Errorf("replay: %d lots", n)
	}
}

// Warehouse may decide: the lot is still held and only Waive, with a reason, lets
// it go. A flag on the receipt line skips nothing.
func TestStage14833WarehouseMayDecide(t *testing.T) {
	f := inspTenant(t)
	f.on(t)
	skip := line("I-OPT", 4)
	skip["skip_qc"] = "Yes"
	f.receive(t, "G-OPT", "V1", "L1", skip)
	if _, a, h := f.stock(t, "I-OPT", "L1"); a != 0 || h != 4 {
		t.Errorf("a flag on the line skipped the check: available %d hold %d", a, h)
	}
	mand := line("I-GOLD", 4)
	mand["skip_qc"] = "Yes"
	f.receive(t, "G-MAND", "V1", "L1", mand)
	if _, a, h := f.stock(t, "I-GOLD", "L1"); a != 0 || h != 4 {
		t.Errorf("mandatory ignores skip: available %d hold %d", a, h)
	}
}

// With the Quality module off the Inspect screen is gone: a receipt holds
// nothing, and a bill waits for no lot.
func TestStage14833QualityModuleOffHoldsNothing(t *testing.T) {
	f := inspTenant(t)
	f.on(t)
	f.receive(t, "G-QON", "V1", "L1", line("I-GOLD", 4))
	bill := map[string]interface{}{"grn_id": "G-QON"}
	if err := inspectionBlocksBill(f.tenant, bill); err == nil {
		t.Fatal("module on: a bill for a pending lot should wait")
	}
	if err := SetModuleEntitlement(f.tenant, "quality", false, "test"); err != nil {
		t.Fatal(err)
	}
	if err := inspectionBlocksBill(f.tenant, bill); err != nil {
		t.Errorf("module off: bill still waits: %v", err)
	}
	f.receive(t, "G-QOFF", "V1", "L1", line("I-PLAIN", 4), line("I-GOLD", 2))
	if n := len(f.lots(t, "G-QOFF")); n != 0 {
		t.Errorf("module off: %d lots filed", n)
	}
	if _, a, h := f.stock(t, "I-PLAIN", "L1"); a != 4 || h != 0 {
		t.Errorf("module off: available %d hold %d", a, h)
	}
}

// Vendor rules: a new vendor is checked; a vendor over the reject rate is checked; clean lots in a row lower the sample.
func TestStage14833VendorRules(t *testing.T) {
	f := inspTenant(t)
	f.on(t)
	f.insert("R-NEW", "InspectionRule", "Active", map[string]interface{}{"code": "R-NEW", "rule_kind": "New vendor", "status": "Active"})
	f.insert("R-REJ", "InspectionRule", "Active", map[string]interface{}{"code": "R-REJ", "rule_kind": "Vendor reject rate", "threshold": 10, "status": "Active"})
	f.insert("R-SKIP", "InspectionRule", "Active", map[string]interface{}{"code": "R-SKIP", "rule_kind": "Skip lot", "threshold": 2, "sample_pct": 2, "status": "Active"})

	f.receive(t, "G-N1", "V-NEW", "L1", line("I-PLAIN", 100))
	if _, _, h := f.stock(t, "I-PLAIN", "L1"); h != 100 {
		t.Fatalf("new vendor: hold %d, want 100", h)
	}
	// Second lot from the same vendor: not new, no rule fires.
	lots := f.lots(t, "G-N1")
	if _, err := RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: lots[0], Lines: []InspectionResultLine{{Result: "Pass", Qty: 100}}}); err != nil {
		t.Fatal(err)
	}
	f.receive(t, "G-N2", "V-NEW", "L1", line("I-PLAIN", 50))
	if n := len(f.lots(t, "G-N2")); n != 0 {
		t.Errorf("known vendor with no history of rejects: %d lots", n)
	}
	// A vendor whose past lots failed 30% is checked.
	f.insert("IQL-OLD", "InspectionLot", "Rejected", map[string]interface{}{"vendor": "V-BAD", "qty": 100, "failed_qty": 30, "status": "Rejected"})
	f.insert("G-PRIOR", "GRN", "Posted", map[string]interface{}{"vendor": "V-BAD"})
	f.receive(t, "G-BAD", "V-BAD", "L1", line("I-PLAIN", 20))
	if n := len(f.lots(t, "G-BAD")); n != 1 {
		t.Fatalf("reject-rate vendor: %d lots, want 1", n)
	}
	// Two clean lots in a row: sample falls to the rule's 2%.
	for i, g := range []string{"G-C1", "G-C2"} {
		f.insert(fmt.Sprintf("IQL-C%d", i), "InspectionLot", "Released", map[string]interface{}{"vendor": "V-GOOD", "qty": 100, "failed_qty": 0, "status": "Released"})
		_ = g
	}
	f.insert("G-PG", "GRN", "Posted", map[string]interface{}{"vendor": "V-GOOD"})
	f.receive(t, "G-GOOD", "V-GOOD", "L1", line("I-GOLD", 100))
	d, _, _ := fetchDocData(f.tenant, InspectionLotDoctype, f.lots(t, "G-GOOD")[0])
	if numFromInterface(d["sample_size"]) != 2 {
		t.Errorf("skip-lot sample = %v, want 2", d["sample_size"])
	}
}

// Results: a blank result is a Fail, lines total the lot, the acceptance % decides, stock follows.
func TestStage14833Results(t *testing.T) {
	f := inspTenant(t)
	f.on(t)
	f.insert("PLAN-G", "InspectionPlan", "Active", map[string]interface{}{"code": "PLAN-G", "item": "I-GOLD", "status": "Active", "applies_to": "Incoming", "acceptance_pct": 90,
		"test_parameters": []interface{}{map[string]interface{}{"parameter_name": "Karat", "spec": "22K", "min": 21.5, "max": 22.5}}})
	f.insert("BATCH-1", "Batch", "Active", map[string]interface{}{"item": "I-GOLD", "batch_no": "B1", "status": "Active"})
	mk := func(grn string, batch string) string {
		l := line("I-GOLD", 10)
		l["batch_no"] = batch
		f.receive(t, grn, "V1", "L1", l)
		return f.lots(t, grn)[0]
	}
	id := mk("G-R1", "")
	// Short total refused; stock unmoved.
	if _, err := RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: id, Lines: []InspectionResultLine{{Result: "Pass", Qty: 9}}}); err == nil {
		t.Fatalf("lines totalling 9 of 10 must be refused")
	}
	if _, a, h := f.stock(t, "I-GOLD", "L1"); a != 0 || h != 10 {
		t.Errorf("refused result moved stock: %d/%d", a, h)
	}
	// A blank result counts as Fail: 9 pass + 1 blank = 90% = accepted, part released.
	res, err := RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: id, Lines: []InspectionResultLine{{Result: "Pass", Qty: 9}, {Qty: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != lotPart || res.Failed != 1 || res.Release != 9 {
		t.Errorf("result = %+v", res)
	}
	if on, a, h := f.stock(t, "I-GOLD", "L1"); on != 10 || a != 9 || h != 1 {
		t.Errorf("part release: %d/%d/%d, want on 10, available 9, hold 1", on, a, h)
	}
	if _, err := RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: id, Lines: []InspectionResultLine{{Result: "Pass", Qty: 10}}}); err == nil {
		t.Errorf("a decided lot cannot be decided again")
	}
	// Below the acceptance %: the whole lot stays held. A reading outside the limits fails its line.
	id2 := mk("G-R2", "B1")
	res, err = RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: id2, Karat: "22", Weight: "12.5", Lines: []InspectionResultLine{
		{Result: "Pass", Qty: 8, Readings: map[string]float64{"Karat": 22}}, {Result: "Pass", Qty: 2, Readings: map[string]float64{"Karat": 18}}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != lotRejected || res.Release != 0 || res.Failed != 2 {
		t.Errorf("reject result = %+v", res)
	}
	if _, a, h := f.stock(t, "I-GOLD", "L1"); a != 9 || h != 11 {
		t.Errorf("rejected lot: available %d hold %d, want 9 and 11 (10 failed lot held, none sold)", a, h)
	}
	b, _ := GetBatch(f.tenant, "I-GOLD", "B1")
	if b == nil || b.Attributes["karat"] != "22" || b.Attributes["weight"] != "12.5" {
		t.Errorf("batch attributes = %+v", b)
	}
	// Clean pass releases the whole lot, and the bill is free.
	id3 := mk("G-R3", "")
	if _, err := RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: id3, Lines: []InspectionResultLine{{Result: "Pass", Qty: 10}}}); err != nil {
		t.Fatal(err)
	}
	if _, a, _ := f.stock(t, "I-GOLD", "L1"); a != 19 {
		t.Errorf("full pass: available %d, want 19", a)
	}
	if err := inspectionBlocksBill(f.tenant, map[string]interface{}{"grn_id": "G-R3"}); err != nil {
		t.Errorf("released lot still holds the bill: %v", err)
	}
	// Waive: only where the warehouse may decide, and with a reason.
	l := line("I-OPT", 6)
	f.receive(t, "G-W", "V1", "L1", l)
	wid := f.lots(t, "G-W")[0]
	if _, err := RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: wid, Waive: true}); err == nil {
		t.Errorf("waive without a reason must be refused")
	}
	if _, err := RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: wid, Waive: true, Note: "supplier certificate"}); err != nil {
		t.Fatal(err)
	}
	if _, a, h := f.stock(t, "I-OPT", "L1"); a != 6 || h != 0 {
		t.Errorf("waived: %d/%d", a, h)
	}
	gid := mk("G-R4", "")
	if _, err := RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: gid, Waive: true, Note: "x"}); err == nil {
		t.Errorf("a mandatory check cannot be waived")
	}
}

func TestStage14833TypedInLotRefused(t *testing.T) {
	if err := validateVendorMoneyDocument(InspectionLotDoctype); err == nil {
		t.Errorf("a typed-in lot must be refused")
	}
}

// A failed lot keeps its units in QC hold and the vendor bill waits; Scrap (a real
// stock adjustment) or Release (with a note) lets them leave, so a bill never dead-ends.
func TestStage14833HeldUnitsHoldTheBillUntilDisposed(t *testing.T) {
	f := inspTenant(t)
	f.on(t)
	f.insert("L1", "Location", "Active", map[string]interface{}{"code": "L1", "name": "Store 1", "status": "Active"})
	f.insert("PLAN-G", "InspectionPlan", "Active", map[string]interface{}{"code": "PLAN-G", "item": "I-GOLD", "status": "Active", "applies_to": "Incoming", "acceptance_pct": 90})
	// A new client now starts with the starter reason codes (163.1), IA-DAMAGE
	// among them; add it only where the client was made without them.
	var haveDamage bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'ReasonCode' AND id = 'IA-DAMAGE')`, f.schema)).Scan(&haveDamage); err != nil {
		t.Fatal(err)
	}
	if !haveDamage {
		f.insert("IA-DAMAGE", "ReasonCode", "Active", map[string]interface{}{"code": "IA-DAMAGE", "category": ReasonInventoryAdjust, "status": "Active"})
	}
	fail := func(grn string, failedOf int) string {
		// Costed lines, so the scrap adjustment has a value to journal.
		raw, _ := json.Marshal([]map[string]interface{}{{"sku": "I-GOLD", "qty": 10, "accepted_qty": 10, "invoice_rate": 250}})
		f.insert(grn, "GRN", "Draft", map[string]interface{}{"code": grn, "vendor": "V1", "location": "L1", "received_items": string(raw)})
		if _, err := PostGRNReceiptWithQC(f.tenant, "L1", []interface{}{line("I-GOLD", 10)}, "system", grn); err != nil {
			t.Fatal(err)
		}
		id := f.lots(t, grn)[0]
		if _, err := RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: id, Lines: []InspectionResultLine{{Result: "Pass", Qty: float64(10 - failedOf)}, {Result: "Fail", Qty: float64(failedOf)}}}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	bill := func(grn string) map[string]interface{} { return map[string]interface{}{"grn_id": grn} }

	// Rejected: all 10 held, bill blocked, listed as Held.
	id := fail("G-H1", 10)
	if err := inspectionBlocksBill(f.tenant, bill("G-H1")); err == nil {
		t.Fatal("a rejected lot must hold the bill")
	}
	held, err := ListInspectionLots(f.tenant, "", "Held")
	if err != nil || len(held) != 1 || held[0].ID != id {
		t.Fatalf("held list = %+v (%v)", held, err)
	}
	// Refusals leave everything as it was.
	if _, err := DisposeInspectionHeld(f.tenant, "u", "HR/Admin", "", InspectionDisposeRequest{LotID: id, Outcome: "Release"}); err == nil {
		t.Error("release without a note should refuse")
	}
	if _, err := DisposeInspectionHeld(f.tenant, "u", "HR/Admin", "L9", InspectionDisposeRequest{LotID: id, Outcome: "Release", Note: "x"}); err == nil {
		t.Error("another store's lot should refuse")
	}
	if _, err := DisposeInspectionHeld(f.tenant, "u", "Cashier", "", InspectionDisposeRequest{LotID: id, Outcome: "Release", Note: "x"}); err == nil {
		t.Error("a cashier may not dispose")
	}
	// Scrap: the stock really leaves (on hand falls, a negative ledger entry and a
	// stock adjustment exist), the hold clears and the bill is free.
	onB, _, holdB := f.stock(t, "I-GOLD", "L1")
	res, err := DisposeInspectionHeld(f.tenant, "u", "HR/Admin", "L1", InspectionDisposeRequest{LotID: id, Outcome: "Scrap", ReasonCode: "IA-DAMAGE"})
	if err != nil || res.AdjustmentID == "" || res.Qty != 10 {
		t.Fatalf("scrap: %+v %v", res, err)
	}
	onA, _, holdA := f.stock(t, "I-GOLD", "L1")
	if j := f.journal(t, res.AdjustmentID); j == "" {
		t.Error("scrap of a costed item posted no journal")
	}
	if onA != onB-10 || holdA != holdB-10 {
		t.Errorf("scrap: on_hand %d->%d hold %d->%d, want both down by 10", onB, onA, holdB, holdA)
	}
	var outs int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = 'I-GOLD' AND (data->>'qty')::numeric < 0`, f.schema)).Scan(&outs)
	if outs == 0 {
		t.Error("scrap wrote no negative stock-ledger entry")
	}
	if err := inspectionBlocksBill(f.tenant, bill("G-H1")); err != nil {
		t.Errorf("bill still blocked after scrap: %v", err)
	}
	if _, err := DisposeInspectionHeld(f.tenant, "u", "HR/Admin", "L1", InspectionDisposeRequest{LotID: id, Outcome: "Scrap", ReasonCode: "IA-DAMAGE"}); err == nil {
		t.Error("a disposed lot cannot be disposed twice")
	}

	// Scrap under an approval rule: nothing is ever free. The units stay held, the
	// bill waits, a second try is refused, and approving closes the lot.
	idA := fail("G-HA", 10)
	if _, err := UpsertApprovalRule(f.tenant, InventoryAdjustmentDoctype, 0, nil, RoleStoreManager, nil); err != nil {
		t.Fatal(err)
	}
	_, availA, holdB4 := f.stock(t, "I-GOLD", "L1")
	pend, err := DisposeInspectionHeld(f.tenant, "u", "HR/Admin", "L1", InspectionDisposeRequest{LotID: idA, Outcome: "Scrap", ReasonCode: "IA-DAMAGE"})
	if err != nil || pend.Status != "Pending Approval" {
		t.Fatalf("scrap under a rule: %+v %v", pend, err)
	}
	if _, a, h := f.stock(t, "I-GOLD", "L1"); a != availA || h != holdB4 {
		t.Errorf("waiting scrap moved stock: available %d->%d hold %d->%d", availA, a, holdB4, h)
	}
	if err := inspectionBlocksBill(f.tenant, bill("G-HA")); err == nil {
		t.Error("the bill must wait while the write-off waits")
	}
	if _, err := DisposeInspectionHeld(f.tenant, "u", "HR/Admin", "L1", InspectionDisposeRequest{LotID: idA, Outcome: "Release", Note: "x"}); err == nil {
		t.Error("a lot with a waiting write-off cannot also be released")
	}
	if err := DecideApproval(f.tenant, InventoryAdjustmentDoctype, pend.AdjustmentID, "checker", RoleStoreManager, "L1", "Approved", ""); err != nil {
		t.Fatal(err)
	}
	if _, a, h := f.stock(t, "I-GOLD", "L1"); a != availA || h != holdB4-10 {
		t.Errorf("approved scrap: available %d->%d hold %d->%d, want available unchanged and hold down 10", availA, a, holdB4, h)
	}
	if err := inspectionBlocksBill(f.tenant, bill("G-HA")); err != nil {
		t.Errorf("bill still blocked after the approved scrap: %v", err)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.approval_rules WHERE doctype = 'InventoryAdjustment'`, f.schema)); err != nil {
		t.Fatal(err)
	}

	// Part released: 9 sellable, 1 held; the bill waits for that 1, and Release frees it.
	id2 := fail("G-H2", 1)
	if err := inspectionBlocksBill(f.tenant, bill("G-H2")); err == nil {
		t.Fatal("one held unit must still hold the bill")
	}
	if _, a, h := f.stock(t, "I-GOLD", "L1"); a != 9 || h != 1 {
		t.Errorf("part release buckets: available %d hold %d, want 9/1", a, h)
	}
	if _, err := DisposeInspectionHeld(f.tenant, "u", "HR/Admin", "L1", InspectionDisposeRequest{LotID: id2, Outcome: "Release", Note: "re-tested at the vendor, passes"}); err != nil {
		t.Fatal(err)
	}
	if _, a, h := f.stock(t, "I-GOLD", "L1"); a != 10 || h != 0 {
		t.Errorf("released buckets: available %d hold %d, want 10/0", a, h)
	}
	if err := inspectionBlocksBill(f.tenant, bill("G-H2")); err != nil {
		t.Errorf("bill still blocked after release: %v", err)
	}
	if held, _ := ListInspectionLots(f.tenant, "", "Held"); len(held) != 0 {
		t.Errorf("held list not empty after disposal: %+v", held)
	}
}

// A release that finds the units gone from QC hold refuses: the lot must not
// say Released while no stock moved.
func TestStage14833HoldGoneRefuses(t *testing.T) {
	f := inspTenant(t)
	f.on(t)
	f.insert("L1", "Location", "Active", map[string]interface{}{"code": "L1", "name": "Store 1", "status": "Active"})
	f.insert("PLAN-G", "InspectionPlan", "Active", map[string]interface{}{"code": "PLAN-G", "item": "I-GOLD", "status": "Active", "applies_to": "Incoming", "acceptance_pct": 90})
	f.receive(t, "G-GONE", "V1", "L1", line("I-GOLD", 10))
	id := f.lots(t, "G-GONE")[0]
	empty := func() {
		if _, err := db.DB.Exec("UPDATE " + f.schema + ".inventory_availability SET qc_hold = 0 WHERE sku = 'I-GOLD' AND location_code = 'L1'"); err != nil {
			t.Fatal(err)
		}
	}
	restore := func() {
		if _, err := db.DB.Exec("UPDATE " + f.schema + ".inventory_availability SET qc_hold = 10 WHERE sku = 'I-GOLD' AND location_code = 'L1'"); err != nil {
			t.Fatal(err)
		}
	}
	empty()
	if _, err := RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: id, Lines: []InspectionResultLine{{Result: "Pass", Qty: 10}}}); err == nil {
		t.Fatal("a pass with nothing in QC hold must refuse")
	}
	restore()
	if _, err := RecordInspectionResult(f.tenant, "u", InspectionResultRequest{LotID: id, Lines: []InspectionResultLine{{Result: "Fail", Qty: 10}}}); err != nil {
		t.Fatalf("fail: %v", err)
	}
	empty()
	if _, err := DisposeInspectionHeld(f.tenant, "u", "HR/Admin", "", InspectionDisposeRequest{LotID: id, Outcome: "Release", Note: "x"}); err == nil {
		t.Fatal("a release with nothing in QC hold must refuse")
	}
	restore()
}
