package engines

import (
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 146.3.14: a bin or a zone can be closed to receipts only (drain it
// while picks continue) or to picks only (freeze picks while receipts
// continue). Put-away, bin moves, the put-away suggestion and pick allocation
// all honour it; who set it and when is kept. No block = exactly as before.

func fbSeed(t *testing.T, tenantID, schema string, insert func(id, doctype, status, createdBy string, data map[string]interface{})) (loc, sku string) {
	t.Helper()
	loc, sku = "FB1", "FB1-SKU"
	insert("ITEM-"+sku, "Item", "Active", "system", map[string]interface{}{"code": sku, "name": "Q"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100)", sku, loc); err != nil {
		t.Fatal(err)
	}
	insert("ZONE-FBZ", "Zone", "Active", "system", map[string]interface{}{"code": "FBZ", "flow_block": "No receipts", "flow_block_reason": "re-slot"})
	insert("ZONE-FBP", "Zone", "Active", "system", map[string]interface{}{"code": "FBP", "flow_block": "No picks", "flow_block_reason": "audit"})
	bin := func(code, zone, block string) {
		m := map[string]interface{}{"bin_code": code, "location": loc, "status": "Active"}
		if zone != "" {
			m["zone"] = zone
		}
		if block != "" {
			m["flow_block"] = block
			m["flow_block_reason"] = "test"
		}
		insert("BIN-"+code, "Bin", "Active", "system", m)
	}
	bin("FB-IN", "", "No receipts")
	bin("FB-OUT", "", "No picks")
	bin("FB-OK", "", "")
	bin("FB-ZIN", "FBZ", "")
	bin("FB-ZOUT", "FBP", "")
	return
}

func TestStage146314ReceiptsBlock(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	_, sku := fbSeed(t, tenantID, schema, insert)

	for _, b := range []string{"FB-IN", "FB-ZIN"} {
		err := PutawayToBin(tenantID, b, sku, 2, "system")
		if err == nil || !strings.Contains(err.Error(), "receipts") {
			t.Fatalf("put-away into %s: want a refusal naming receipts, got %v", b, err)
		}
	}
	for _, b := range []string{"FB-OUT", "FB-OK", "FB-ZOUT"} {
		if err := PutawayToBin(tenantID, b, sku, 2, "system"); err != nil {
			t.Fatalf("put-away into %s (not closed to receipts): %v", b, err)
		}
	}
	// A bin move into a receipts-blocked bin is refused too.
	err := MoveBinStock(tenantID, BinMoveInput{FromBin: "FB-OK", ToBin: "FB-IN", SKU: sku, Qty: 1, UserID: "system", TaskType: "Move"})
	if err == nil || !strings.Contains(err.Error(), "receipts") {
		t.Fatalf("move into a receipts-blocked bin: got %v", err)
	}
	// ...but a move OUT of it (the drain) is allowed.
	if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ('FB-IN', $1, 'FB1', 'Good', 3)", sku); err != nil {
		t.Fatal(err)
	}
	if err := MoveBinStock(tenantID, BinMoveInput{FromBin: "FB-IN", ToBin: "FB-OK", SKU: sku, Qty: 3, UserID: "system", TaskType: "Move"}); err != nil {
		t.Fatalf("draining a receipts-blocked bin: %v", err)
	}
}

func TestStage146314PicksBlock(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	loc, sku := fbSeed(t, tenantID, schema, insert)
	for _, b := range []string{"FB-IN", "FB-OUT", "FB-OK", "FB-ZOUT"} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1, $2, $3, 'Good', 5)", b, sku, loc); err != nil {
			t.Fatal(err)
		}
	}
	got, short, err := allocateFromStockByStrategy(db.DB, tenantID, sku, loc, 100)
	if err != nil {
		t.Fatal(err)
	}
	bins := map[string]int{}
	for _, c := range got {
		bins[c.BinCode] += c.Qty
	}
	if bins["FB-OUT"] != 0 || bins["FB-ZOUT"] != 0 {
		t.Fatalf("picks allocated from a picks-blocked bin or zone: %v", bins)
	}
	if bins["FB-IN"] != 5 || bins["FB-OK"] != 5 || short != 90 {
		t.Fatalf("allocation = %v short %d; want 5 each from FB-IN (drain keeps picking) and FB-OK, short 90 of 100", bins, short)
	}
}

func TestStage146314SuggestionSkipsBlockedBins(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	loc, sku := fbSeed(t, tenantID, schema, insert)
	insert("HB-1", "HomeBin", "Active", "system", map[string]interface{}{"sku": sku, "bin_code": "FB-IN", "priority": 1, "allow_other_items": "Yes", "status": "Active"})
	insert("HB-2", "HomeBin", "Active", "system", map[string]interface{}{"sku": sku, "bin_code": "FB-OK", "priority": 2, "allow_other_items": "Yes", "status": "Active"})
	bin, reason, err := SuggestPutawayBin(tenantID, sku, loc, 3, "")
	if err != nil {
		t.Fatal(err)
	}
	if bin != "FB-OK" || !strings.Contains(reason, "FB-IN") {
		t.Fatalf("suggestion = %q (%s); want FB-OK with FB-IN named as skipped", bin, reason)
	}
}

func TestStage146314StampAndReason(t *testing.T) {
	prior := map[string]interface{}{}
	p := map[string]interface{}{"flow_block": "No picks", "flow_block_reason": "stock take"}
	StampFlowBlock("Bin", prior, p, "alice")
	if p["flow_block_by"] != "alice" || strField(p, "flow_block_at") == "" {
		t.Fatalf("not stamped: %v", p)
	}
	at := p["flow_block_at"]
	// Saved again unchanged: who and when stay.
	p2 := map[string]interface{}{"flow_block": "No picks", "flow_block_reason": "stock take"}
	StampFlowBlock("Bin", p, p2, "bob")
	if p2["flow_block_by"] != "alice" || p2["flow_block_at"] != at {
		t.Fatalf("an unchanged block was re-stamped: %v", p2)
	}
	// Lifted: the stamp goes with it.
	p3 := map[string]interface{}{"flow_block": ""}
	StampFlowBlock("Bin", p, p3, "bob")
	if _, ok := p3["flow_block_by"]; ok && p3["flow_block_by"] != "" {
		t.Fatalf("a lifted block kept its stamp: %v", p3)
	}
	// A block needs a reason.
	if err := validateFlowBlockFields(map[string]interface{}{"flow_block": "No receipts"}); err == nil {
		t.Fatal("a block with no reason was accepted")
	}
	if err := validateFlowBlockFields(map[string]interface{}{"flow_block": "Sideways", "flow_block_reason": "x"}); err == nil {
		t.Fatal("an unknown block kind was accepted")
	}
	if err := validateFlowBlockFields(map[string]interface{}{"code": "Z1"}); err != nil {
		t.Fatalf("no block: %v", err)
	}
	// Other doctypes are untouched.
	p4 := map[string]interface{}{"flow_block": "No picks"}
	StampFlowBlock("Item", nil, p4, "x")
	if _, ok := p4["flow_block_by"]; ok {
		t.Fatal("stamped a non-bin doctype")
	}
}
