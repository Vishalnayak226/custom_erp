package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 146.3.13 (D22): the transfer fill policy. Off (default) = Exact for
// every transfer, as today. On = replenishment ships what is there, a new
// store / launch allocation waits until every line can be filled, a store or
// the transfer itself can override, and a minimum fill % refuses a thin ship.
func TestStage146313TransferFillPolicy(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const sku, from, to = "SKU-S146313", "WH-S146313-FROM", "WH-S146313-TO"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'TO-S146313-%' OR id = 'LOC-S146313-TO'")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".stock_ledger WHERE item_id = $1", sku)
	}
	cleanup()
	t.Cleanup(cleanup)
	t.Cleanup(func() { _ = SetSetting(tenantID, TransferFillPolicySetting, "false", "system") })
	setOn := func(on bool) {
		v := "false"
		if on {
			v = "true"
		}
		if err := SetSetting(tenantID, TransferFillPolicySetting, v, "system"); err != nil {
			t.Fatalf("set: %v", err)
		}
	}
	stock := func(n int) {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, $3, $3)", sku, from, n); err != nil {
			t.Fatalf("stock: %v", err)
		}
	}
	transfer := func(id string, qty int, extra map[string]interface{}) {
		items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": qty}})
		m := map[string]interface{}{"id": id, "transfer_number": id, "from_warehouse": from, "to_warehouse": to, "status": "Approved", "items": string(items)}
		for k, v := range extra {
			m[k] = v
		}
		data, _ := json.Marshal(m)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'TransferOrder', $2, 'Approved', 'system')", id, data); err != nil {
			t.Fatalf("seed transfer: %v", err)
		}
	}
	state := func(id string) (status string, inTransit, onHand int, data map[string]interface{}) {
		var raw []byte
		_ = db.DB.QueryRow("SELECT status, data FROM "+schema+".documents WHERE id = $1", id).Scan(&status, &raw)
		_ = json.Unmarshal(raw, &data)
		_ = db.DB.QueryRow("SELECT COALESCE(in_transit,0), on_hand FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, from).Scan(&inTransit, &onHand)
		return
	}
	code := func(err error) string {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Code
		}
		return ""
	}
	// A reason code for the short close: use one the receipts already accept.
	reason := seedTransferReasonCode(t, tenantID, schema)

	// Off: a short transfer fails exactly as before, whatever its fields say.
	stock(6)
	transfer("TO-S146313-OFF", 10, map[string]interface{}{"fill_policy": "Ship what is there"})
	err := DispatchTransferOrder(tenantID, "TO-S146313-OFF", "system")
	if err == nil || code(err) != "STOCKT-0112" {
		t.Fatalf("off: short dispatch should fail with STOCKT-0112, got %v", err)
	}

	setOn(true)
	// Replenishment (no purpose) defaults to Ship what is there.
	if p, _ := resolveTransferFill(tenantID, schema, map[string]interface{}{"to_warehouse": to}); p.Policy != FillShipWhatIsThere {
		t.Fatalf("default policy = %q, want Ship what is there", p.Policy)
	}
	if p, _ := resolveTransferFill(tenantID, schema, map[string]interface{}{"to_warehouse": to, "purpose": "New store"}); p.Policy != FillComplete {
		t.Fatalf("new store policy = %q, want Complete", p.Policy)
	}

	// Ship what is there: ships 6 of 10, needs a reason for the 4 closed.
	transfer("TO-S146313-SHIP", 10, nil)
	err = DispatchTransferOrderOpts(tenantID, "TO-S146313-SHIP", "system", DispatchOptions{})
	if code(err) != "STOCKT-0484" {
		t.Fatalf("short close without a reason should be STOCKT-0484, got %v", err)
	}
	if st, _, oh, _ := state("TO-S146313-SHIP"); st != "Approved" || oh != 6 {
		t.Fatalf("refused dispatch must move nothing: status %s on_hand %d", st, oh)
	}
	if err := DispatchTransferOrderOpts(tenantID, "TO-S146313-SHIP", "system", DispatchOptions{ShortReasonCode: reason}); err != nil {
		t.Fatalf("ship what is there: %v", err)
	}
	st, inTransit, onHand, data := state("TO-S146313-SHIP")
	if st != "Dispatched" || inTransit != 6 || onHand != 0 {
		t.Fatalf("after short ship: status %s in_transit %d on_hand %d, want Dispatched 6 0", st, inTransit, onHand)
	}
	if sc, _ := data["short_closed"].(string); !strings.Contains(sc, `"short":4`) || !strings.Contains(sc, reason) {
		t.Fatalf("short_closed = %q, want the 4 units and the reason", sc)
	}
	if di, _ := data["dispatched_items"].(string); !strings.Contains(di, `"qty":6`) {
		t.Fatalf("dispatched_items = %q, want qty 6", di)
	}

	// A fully covered transfer ships whole and needs no reason.
	stock(10)
	transfer("TO-S146313-FULL", 10, nil)
	if err := DispatchTransferOrder(tenantID, "TO-S146313-FULL", "system"); err != nil {
		t.Fatalf("full cover: %v", err)
	}

	// Minimum fill %: 6 of 10 is 60%, below 80% - refused, nothing moves.
	stock(6)
	transfer("TO-S146313-MIN", 10, map[string]interface{}{"min_fill_pct": 80})
	err = DispatchTransferOrderOpts(tenantID, "TO-S146313-MIN", "system", DispatchOptions{ShortReasonCode: reason})
	if code(err) != "STOCKT-0489" {
		t.Fatalf("below minimum fill should be STOCKT-0489, got %v", err)
	}
	if st, _, oh, _ := state("TO-S146313-MIN"); st != "Approved" || oh != 6 {
		t.Fatalf("below-minimum refusal moved stock: %s %d", st, oh)
	}
	// Nothing in stock at all: nothing to ship.
	stock(0)
	transfer("TO-S146313-NONE", 10, nil)
	if code(DispatchTransferOrderOpts(tenantID, "TO-S146313-NONE", "system", DispatchOptions{ShortReasonCode: reason})) != "STOCKT-0489" {
		t.Fatalf("zero cover should be STOCKT-0489")
	}

	// Complete: waits while any line is short, ships when all can be filled.
	stock(6)
	transfer("TO-S146313-COMP", 10, map[string]interface{}{"purpose": "New store"})
	err = DispatchTransferOrderOpts(tenantID, "TO-S146313-COMP", "system", DispatchOptions{ShortReasonCode: reason})
	if code(err) != "STOCKT-0490" {
		t.Fatalf("complete with a short line should be STOCKT-0490, got %v", err)
	}
	if st, _, oh, _ := state("TO-S146313-COMP"); st != "Approved" || oh != 6 {
		t.Fatalf("waiting transfer moved stock: %s %d", st, oh)
	}
	fc, err := TransferFillCheck(tenantID, "TO-S146313-COMP")
	if err != nil || fc.Policy != FillComplete || fc.CanShip || len(fc.Lines) != 1 || fc.Lines[0].Short != 4 {
		t.Fatalf("fill check = %+v, %v; want Complete, cannot ship, 4 short", fc, err)
	}
	stock(10)
	if err := DispatchTransferOrder(tenantID, "TO-S146313-COMP", "system"); err != nil {
		t.Fatalf("complete once stocked: %v", err)
	}

	// The store's own setting beats the purpose default; the transfer's beats the store's.
	locData, _ := json.Marshal(map[string]interface{}{"code": to, "name": "S146313 store", "type": "Store", "transfer_fill_policy": "Exact"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('LOC-S146313-TO', 'Location', $1, 'Active', 'system')", locData); err != nil {
		t.Fatalf("seed location: %v", err)
	}
	stock(6)
	transfer("TO-S146313-STORE", 10, nil)
	if err := DispatchTransferOrder(tenantID, "TO-S146313-STORE", "system"); code(err) != "STOCKT-0112" {
		t.Fatalf("store set to Exact should fail as today (STOCKT-0112), got %v", err)
	}
	transfer("TO-S146313-OVER", 10, map[string]interface{}{"fill_policy": "Ship what is there"})
	if err := DispatchTransferOrderOpts(tenantID, "TO-S146313-OVER", "system", DispatchOptions{ShortReasonCode: reason}); err != nil {
		t.Fatalf("transfer override of the store: %v", err)
	}
}

// seedTransferReasonCode makes (or reuses) an active Transfer Discrepancy
// reason code for the short-close tests and removes it afterwards.
func seedTransferReasonCode(t *testing.T, tenantID, schema string) string {
	t.Helper()
	const code = "RC-S146313-SHORT"
	_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + code + "'")
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system')",
		code, `{"code":"`+code+`","description":"short","category":"`+TransferDiscrepancyReasonCategory+`"}`); err != nil {
		t.Fatalf("seed reason: %v", err)
	}
	t.Cleanup(func() { _, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + code + "'") })
	return code
}
