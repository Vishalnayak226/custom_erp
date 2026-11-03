package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

func t14631Doc(t *testing.T, schema, id, doctype, status string, m map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(m)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, b, status); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func t14631Cleanup(schema, prefix string) {
	db.DB.Exec("DELETE FROM "+schema+".documents WHERE id LIKE $1", prefix+"%")
	db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' LIKE $1", prefix+"%")
	db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'WarehouseTask' AND data->>'item' LIKE $1", prefix+"%")
	db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku LIKE $1", prefix+"%")
	db.DB.Exec("DELETE FROM "+schema+".bin_stock_batch WHERE sku LIKE $1", prefix+"%")
	db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku LIKE $1", prefix+"%")
}

// Stage 146.3.1: a bin mix rule per Zone with a Bin override.
func TestStage14631BinMixRules(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const loc = "T14631-LOC"
	skus := []string{"T14631-A", "T14631-B"}
	t14631Cleanup(schema, "T14631-")
	defer t14631Cleanup(schema, "T14631-")

	doc := func(id, doctype, status string, m map[string]interface{}) {
		t14631Doc(t, schema, id, doctype, status, m)
	}
	for _, s := range skus {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100)", s, loc); err != nil {
			t.Fatalf("seed availability: %v", err)
		}
	}
	doc("T14631-Z-SINGLE", "Zone", "Active", map[string]interface{}{"code": "T14631-ZS", "bin_mix_rule": "Single item"})
	doc("T14631-Z-LOT", "Zone", "Active", map[string]interface{}{"code": "T14631-ZL", "bin_mix_rule": "Single item single lot"})
	doc("T14631-Z-MIX", "Zone", "Active", map[string]interface{}{"code": "T14631-ZM"})
	bin := func(code, zone, rule string) {
		m := map[string]interface{}{"bin_code": code, "location": loc, "zone": zone, "status": "Active"}
		if rule != "" {
			m["bin_mix_rule"] = rule
		}
		doc(code, "Bin", "Active", m)
	}
	bin("T14631-S1", "T14631-ZS", "")
	bin("T14631-S2", "T14631-ZS", "Mixed") // bin override beats the zone
	bin("T14631-L1", "T14631-ZL", "")
	bin("T14631-M1", "T14631-ZM", "")
	bin("T14631-M2", "", "Single item") // bin rule with no zone
	bin("T14631-FROM", "T14631-ZM", "")

	// Single item: a second SKU is refused by putaway, naming both.
	if err := PutawayToBin(tenantID, "T14631-S1", skus[0], 5, "system"); err != nil {
		t.Fatalf("first item into a single-item bin: %v", err)
	}
	if err := PutawayToBin(tenantID, "T14631-S1", skus[0], 3, "system"); err != nil {
		t.Fatalf("same item again: %v", err)
	}
	err = PutawayToBin(tenantID, "T14631-S1", skus[1], 2, "system")
	if err == nil || !strings.Contains(err.Error(), skus[0]) || !strings.Contains(err.Error(), skus[1]) {
		t.Fatalf("second item in a single-item bin: want a refusal naming both SKUs, got %v", err)
	}
	// The bin override makes it Mixed again.
	if err := PutawayToBin(tenantID, "T14631-S2", skus[0], 1, "system"); err != nil {
		t.Fatal(err)
	}
	if err := PutawayToBin(tenantID, "T14631-S2", skus[1], 1, "system"); err != nil {
		t.Fatalf("bin override Mixed in a single-item zone: %v", err)
	}
	// Default (no rule) is Mixed.
	if err := PutawayToBin(tenantID, "T14631-M1", skus[0], 1, "system"); err != nil {
		t.Fatal(err)
	}
	if err := PutawayToBin(tenantID, "T14631-M1", skus[1], 1, "system"); err != nil {
		t.Fatalf("default Mixed refused: %v", err)
	}
	// A bin rule works without a zone.
	if err := PutawayToBin(tenantID, "T14631-M2", skus[0], 1, "system"); err != nil {
		t.Fatal(err)
	}
	if err := PutawayToBin(tenantID, "T14631-M2", skus[1], 1, "system"); err == nil {
		t.Fatalf("bin-level single-item rule not enforced")
	}

	// Move: the same check at the other choke point.
	if err := PutawayToBin(tenantID, "T14631-FROM", skus[1], 4, "system"); err != nil {
		t.Fatal(err)
	}
	err = MoveBinStock(tenantID, BinMoveInput{FromBin: "T14631-FROM", ToBin: "T14631-S1", SKU: skus[1], Qty: 1, UserID: "system"})
	if err == nil || !strings.Contains(err.Error(), skus[0]) || !strings.Contains(err.Error(), skus[1]) {
		t.Fatalf("move of a second item into a single-item bin: got %v", err)
	}
	if err := MoveBinStock(tenantID, BinMoveInput{FromBin: "T14631-FROM", ToBin: "T14631-S2", SKU: skus[1], Qty: 1, UserID: "system"}); err != nil {
		t.Fatalf("move into a Mixed bin: %v", err)
	}

	// Single item single lot: a second lot of the same SKU is refused.
	if err := PutawayToBin(tenantID, "T14631-L1", skus[0], 6, "system"); err != nil {
		t.Fatal(err)
	}
	db.DB.Exec("INSERT INTO "+schema+".bin_stock_batch (bin_code, sku, batch_no, condition, location_code, qty) VALUES ('T14631-L1', $1, 'LOT1', 'Good', $2, 3)", skus[0], loc)
	if err := t14631Mix(tenantID, "T14631-L1", skus[0], "LOT2"); err == nil || !strings.Contains(err.Error(), "LOT1") || !strings.Contains(err.Error(), "LOT2") {
		t.Fatalf("second lot in a single-lot bin: got %v", err)
	}
	if err := t14631Mix(tenantID, "T14631-L1", skus[0], "LOT1"); err != nil {
		t.Fatalf("same lot: %v", err)
	}
	if err := t14631Mix(tenantID, "T14631-L1", skus[1], ""); err == nil {
		t.Fatalf("second item in a single-lot bin must be refused")
	}
}

func t14631Mix(tenantID, bin, sku, batch string) error {
	schema, _ := db.GetTenantSchema(tenantID)
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}
	return enforceBinMixRule(tx, schema, bin, sku, batch)
}

// Stage 146.3.1: home bins - priority order, fallback with a reason, "allow other items".
func TestStage14631HomeBins(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const loc = "T146312-LOC"
	const sku, other = "T146312-A", "T146312-B"
	t14631Cleanup(schema, "T146312-")
	defer t14631Cleanup(schema, "T146312-")
	doc := func(id, doctype, status string, m map[string]interface{}) {
		t14631Doc(t, schema, id, doctype, status, m)
	}
	for _, s := range []string{sku, other} {
		db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100)", s, loc)
	}
	bin := func(code, opState string, capacity int) {
		m := map[string]interface{}{"bin_code": code, "location": loc, "status": "Active", "bin_status": opState}
		if capacity > 0 {
			m["capacity"] = capacity
		}
		doc(code, "Bin", "Active", m)
	}
	bin("T146312-H1", "Full", 0)
	bin("T146312-H2", "", 4)
	bin("T146312-H3", "", 0)
	bin("T146312-OTHER", "", 0)

	// No home bins: nothing changes - the suggestion is today's.
	got, _, err := SuggestPutawayBin(tenantID, sku, loc, 3, "")
	if err != nil || got != "" {
		t.Fatalf("no home bins: want no suggestion, got %q %v", got, err)
	}

	home := func(id, binCode string, prio int, allowOthers string) {
		doc(id, "HomeBin", "Active", map[string]interface{}{"sku": sku, "bin_code": binCode, "priority": prio, "allow_other_items": allowOthers, "status": "Active"})
	}
	home("T146312-HB1", "T146312-H1", 1, "Yes")
	home("T146312-HB2", "T146312-H2", 2, "No")
	home("T146312-HB3", "T146312-H3", 3, "Yes")

	// H1 is Full: falls back to H2 and says why.
	bc, reason, err := SuggestPutawayBin(tenantID, sku, loc, 3, "")
	if err != nil || bc != "T146312-H2" || !strings.Contains(reason, "T146312-H1") || !strings.Contains(reason, "Full") {
		t.Fatalf("want H2 with H1 Full reason, got %q %q %v", bc, reason, err)
	}
	// Qty over H2's capacity: falls to H3.
	bc, reason, _ = SuggestPutawayBin(tenantID, sku, loc, 6, "")
	if bc != "T146312-H3" || !strings.Contains(reason, "T146312-H2") {
		t.Fatalf("want H3 (H2 has no room), got %q %q", bc, reason)
	}
	// All home bins unusable: today's behaviour, with the reason kept.
	db.DB.Exec("UPDATE " + schema + ".documents SET data = data || '{\"bin_status\":\"Blocked\"}' WHERE id = 'T146312-H3'")
	bc, reason, _ = SuggestPutawayBin(tenantID, sku, loc, 6, "")
	if bc != "" || !strings.Contains(reason, "home bin") {
		t.Fatalf("want no suggestion with a home-bin reason, got %q %q", bc, reason)
	}
	db.DB.Exec("UPDATE " + schema + ".documents SET data = data || '{\"bin_status\":\"\"}' WHERE id = 'T146312-H3'")

	// "Allow other items" = No: H2 is kept for its own SKU.
	if err := PutawayToBin(tenantID, "T146312-H2", other, 1, "system"); err == nil || !strings.Contains(err.Error(), sku) {
		t.Fatalf("other item into a reserved home bin: got %v", err)
	}
	if err := PutawayToBin(tenantID, "T146312-H2", sku, 1, "system"); err != nil {
		t.Fatalf("own item into its home bin: %v", err)
	}
	if err := PutawayToBin(tenantID, "T146312-H3", other, 1, "system"); err != nil {
		t.Fatalf("other item where others are allowed: %v", err)
	}
	if err := PutawayToBin(tenantID, "T146312-OTHER", other, 1, "system"); err != nil {
		t.Fatalf("plain bin: %v", err)
	}
	list, err := HomeBinsForSKU(tenantID, sku, loc)
	if err != nil || len(list) != 3 || list[0].BinCode != "T146312-H1" {
		t.Fatalf("HomeBinsForSKU = %+v %v", list, err)
	}
}
