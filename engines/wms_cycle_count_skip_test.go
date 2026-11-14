package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"
)

// TestItemLeftOutOfCycleCounts (146.3.16): an Item marked "Leave out of cycle
// counts" = Yes drops out of the count plan the screens show, matched by code
// or by barcode, while the plan itself (which putaway and slotting read for
// velocity tiers) still carries it.
func TestItemLeftOutOfCycleCounts(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	run := fmt.Sprintf("%d", time.Now().UnixNano())
	location := "WH-SKIPCC-" + run
	byCode := "SKU-SKIPCC-CODE-" + run   // flagged, stock keyed by item code
	byBarcode := "BC-SKIPCC-" + run      // flagged, stock keyed by barcode
	counted := "SKU-SKIPCC-COUNT-" + run // not flagged
	itemIDs := []string{"ITEM-SKIPCC-1-" + run, "ITEM-SKIPCC-2-" + run, "ITEM-SKIPCC-3-" + run}

	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE location_code = $1", location)
		for _, id := range itemIDs {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	})

	seedItem := func(id string, data map[string]interface{}) {
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", id, b); err != nil {
			t.Fatalf("seed item %s: %v", id, err)
		}
	}
	seedItem(itemIDs[0], map[string]interface{}{"code": byCode, "name": "Carry bag", "skip_cycle_count": "Yes"})
	seedItem(itemIDs[1], map[string]interface{}{"code": "PROP-" + run, "barcode": byBarcode, "name": "Display stand", "skip_cycle_count": "Yes"})
	seedItem(itemIDs[2], map[string]interface{}{"code": counted, "name": "Shirt", "skip_cycle_count": "No"})
	for _, sku := range []string{byCode, byBarcode, counted} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 5, 5)", sku, location); err != nil {
			t.Fatalf("seed availability %s: %v", sku, err)
		}
	}

	skus := func(plan []ABCCycleCountSuggestion) []string {
		out := []string{}
		for _, s := range plan {
			out = append(out, s.Sku)
		}
		sort.Strings(out)
		return out
	}

	full, err := GetABCCycleCountPlan(tenantID, location, 0, 0, 0)
	if err != nil {
		t.Fatalf("GetABCCycleCountPlan: %v", err)
	}
	if got := skus(full); len(got) != 3 {
		t.Fatalf("the plan putaway and slotting read must keep every SKU on hand, got %v", got)
	}

	shown := WithoutItemsLeftOutOfCycleCounts(tenantID, full)
	if got := skus(shown); len(got) != 1 || got[0] != counted {
		t.Fatalf("expected only %s left to count, got %v", counted, got)
	}

	classPlan, err := GetCycleCountPlan(tenantID, location)
	if err != nil {
		t.Fatalf("GetCycleCountPlan: %v", err)
	}
	if got := skus(WithoutItemsLeftOutOfCycleCounts(tenantID, classPlan)); len(got) != 1 || got[0] != counted {
		t.Fatalf("expected the cycle-class plan to leave out the flagged items too, got %v", got)
	}

	// Turning the box back to No puts the item back in the plan.
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{skip_cycle_count}', '\"No\"') WHERE id = $1", itemIDs[0]); err != nil {
		t.Fatalf("unflag: %v", err)
	}
	if got := skus(WithoutItemsLeftOutOfCycleCounts(tenantID, full)); len(got) != 2 {
		t.Fatalf("expected %s back in the plan after the box is No, got %v", byCode, got)
	}

	if out := WithoutItemsLeftOutOfCycleCounts(tenantID, nil); out != nil {
		t.Fatalf("an empty plan stays empty, got %v", out)
	}
}
