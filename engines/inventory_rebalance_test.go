package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// Item 273.2: the rebalance source proposes a transfer from the location with
// the most to spare before anything is bought. A store keeps its own reorder
// point plus one lead time of its sales; a switched-off location gives
// nothing; two short stores never both take the same spare stock; approving
// without a source makes one Draft transfer per suggested store.
func TestRebalanceProposesMovesBeforeBuying(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	stock := func(sku, loc string, qty int) {
		t.Helper()
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, $3, $3)", sku, loc, qty); err != nil {
			t.Fatalf("stock: %v", err)
		}
	}
	sold := func(id, loc, sku string, qty int) {
		insert(id, "POSCart", "Paid", map[string]interface{}{"location": loc, "items": []map[string]interface{}{{"sku": sku, "qty": qty}}})
	}
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	const a, b, c, d, w = "RB-A", "RB-B", "RB-C", "RB-D", "RB-W"
	for _, l := range []string{a, b, c, d, w} {
		st := "Active"
		if l == w {
			st = "Inactive"
		}
		insert(l, "Location", st, map[string]interface{}{"code": l, "name": l, "state": "Maharashtra"})
	}
	for _, sku := range []string{"RB-TEE", "RB-CAP", "RB-MUG"} {
		insert("ITEM-"+sku, "Item", "Active", map[string]interface{}{"code": sku, "name": sku + " item", "standard_cost": 50})
	}
	sold("RB-S1", a, "RB-TEE", 60) // A sells 2 a day: reorder point 14
	sold("RB-S2", b, "RB-TEE", 30) // B sells 1 a day: keeps 14
	sold("RB-S3", a, "RB-CAP", 30) // reorder point 7, nowhere to take it from
	sold("RB-S4", a, "RB-MUG", 15) // reorder point 4
	sold("RB-S5", d, "RB-TEE", 30) // D sells 1 a day: reorder point 7
	stock("RB-TEE", a, 2)
	stock("RB-TEE", b, 20)  // spares 6
	stock("RB-TEE", c, 10)  // sells nothing: spares 10
	stock("RB-TEE", w, 100) // switched off
	stock("RB-TEE", d, 0)
	stock("RB-CAP", a, 0)
	stock("RB-MUG", a, 0)
	stock("RB-MUG", b, 3) // ties with C: the lower code wins
	stock("RB-MUG", c, 3)

	moves, err := RebalanceSuggestions(tenantID, a, 7, 0)
	if err != nil {
		t.Fatalf("RebalanceSuggestions: %v", err)
	}
	got := map[string]RebalanceLine{}
	for _, m := range moves {
		got[m.Sku] = m
	}
	if len(moves) != 2 || got["RB-TEE"].From != c || got["RB-TEE"].Qty != 10 || got["RB-TEE"].Short != 12 ||
		got["RB-MUG"].From != b || got["RB-MUG"].Qty != 3 {
		t.Fatalf("moves %+v", moves)
	}

	res, err := GenerateSupplyProposals(tenantID, "planner", a, "rebalance", 7, 0)
	if err != nil || res.Created != 2 {
		t.Fatalf("rebalance proposals: %v %+v", err, res)
	}
	if again, err := GenerateSupplyProposals(tenantID, "planner", a, "rebalance", 7, 0); err != nil || again.Created != 0 || again.Refreshed != 2 {
		t.Fatalf("regenerating must refresh, not duplicate: %v %+v", err, again)
	}
	list, _ := ListSupplyProposals(tenantID, a, ProposalOpen)
	by := map[string]SupplyProposal{}
	for _, p := range list {
		by[p.Sku] = p
	}
	tee, mug := by["RB-TEE"], by["RB-MUG"]
	if tee.Source != "Rebalance" || tee.Action != ProposalTransfer || tee.FromLocation != c || tee.FromAvailable != 10 || tee.SuggestedQty != 10 ||
		mug.FromLocation != b || mug.SuggestedQty != 3 {
		t.Fatalf("proposals tee %+v mug %+v", tee, mug)
	}

	// D is short too, but C's spare tees are already A's: D takes B's.
	if _, err := GenerateSupplyProposals(tenantID, "planner", d, "rebalance", 7, 0); err != nil {
		t.Fatalf("rebalance at D: %v", err)
	}
	dl, _ := ListSupplyProposals(tenantID, d, ProposalOpen)
	if len(dl) != 1 || dl[0].FromLocation != b || dl[0].SuggestedQty != 6 {
		t.Fatalf("D proposals %+v", dl)
	}
	if _, err := ApproveSupplyProposals(tenantID, "planner", "HQ", ApproveProposalsInput{IDs: []string{dl[0].ID}, Action: ProposalTransfer, FromLocation: d}); code(err) != "PLAN-0557" {
		t.Fatalf("transfer from itself: want PLAN-0557, got %v", err)
	}

	// A reorder proposal names no source: a transfer needs one chosen.
	if _, err := GenerateSupplyProposals(tenantID, "planner", a, "reorder", 7, 0); err != nil {
		t.Fatalf("reorder proposals: %v", err)
	}
	list, _ = ListSupplyProposals(tenantID, a, ProposalOpen)
	var capID string
	for _, p := range list {
		if p.Source == "Reorder" && p.Sku == "RB-CAP" {
			capID = p.ID
		}
	}
	if _, err := ApproveSupplyProposals(tenantID, "planner", "HQ", ApproveProposalsInput{IDs: []string{capID}, Action: ProposalTransfer}); code(err) != "PLAN-0557" {
		t.Fatalf("transfer with no source: want PLAN-0557, got %v", err)
	}

	// Approved with no source chosen: one Draft transfer per suggested store.
	out, err := ApproveSupplyProposals(tenantID, "planner", "HQ", ApproveProposalsInput{IDs: []string{tee.ID, mug.ID}, Action: ProposalTransfer})
	if err != nil || out.Doctype != "TransferOrder" || len(out.Documents) != 2 {
		t.Fatalf("approve: %v %+v", err, out)
	}
	for _, id := range out.Documents {
		data, st, err := fetchDocData(tenantID, "TransferOrder", id)
		if err != nil || st != "Draft" || str(data, "to_warehouse") != a {
			t.Fatalf("transfer %s: %v %s %v", id, err, st, data)
		}
		var lines []transferLine
		_ = json.Unmarshal([]byte(str(data, "items")), &lines)
		from := str(data, "from_warehouse")
		if len(lines) != 1 || (from == c && (lines[0].Sku != "RB-TEE" || lines[0].Qty != 10)) || (from == b && (lines[0].Sku != "RB-MUG" || lines[0].Qty != 3)) || (from != b && from != c) {
			t.Fatalf("transfer %s from %s: %+v", id, from, lines)
		}
	}
	conv, _ := ListSupplyProposals(tenantID, a, ProposalConverted)
	for _, p := range conv {
		if p.TargetDoctype != "TransferOrder" || p.FromLocation == "" {
			t.Fatalf("converted proposal %+v", p)
		}
	}

	// The approved tees have not left C: D still cannot take them.
	if _, err := GenerateSupplyProposals(tenantID, "planner", d, "rebalance", 7, 0); err != nil {
		t.Fatalf("rebalance at D again: %v", err)
	}
	if dl, _ = ListSupplyProposals(tenantID, d, ProposalOpen); len(dl) != 1 || dl[0].FromLocation != b {
		t.Fatalf("D after approval %+v", dl)
	}
}
