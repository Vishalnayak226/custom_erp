package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// TestSupplyProposalsStage73218 runs 73.2.18 on a provisioned throwaway
// client: reorder and MRP suggestions become proposals (regenerating
// refreshes, never duplicates), the planner approves some into a PO, a
// transfer and a production order - each once, with lineage both ways - and
// rejects one with a reason.
func TestSupplyProposalsStage73218(t *testing.T) {
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
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	const wh, store = "SP-WH", "SP-STORE"
	for _, l := range []string{wh, store} {
		insert(l, "Location", "Active", map[string]interface{}{"code": l, "name": l})
	}
	for _, i := range []struct {
		sku  string
		cost float64
	}{{"SP-TEE", 120}, {"SP-CAP", 60}, {"SP-KIT", 0}, {"SP-CLOTH", 15}} {
		insert("ITEM-"+i.sku, "Item", "Active", map[string]interface{}{"code": i.sku, "name": i.sku + " item", "standard_cost": i.cost})
	}
	insert("SP-VEND", "Vendor", "Active", map[string]interface{}{"code": "SP-VEND", "name": "Proposal Vendor", "state": "Maharashtra"}) // 49.5.6b
	insert("BOM-SP-KIT", "BOM", "Active", map[string]interface{}{"parent_item": "SP-KIT", "is_default": "Yes",
		"components": `[{"sku":"SP-CLOTH","qty":2}]`})
	stock("SP-TEE", wh, 2)     // safety 10 -> reorder 8
	stock("SP-CAP", wh, 4)     // safety 10 -> reorder 6
	stock("SP-KIT", wh, 0)     // safety 10 -> produce 10, needs 20 cloth
	stock("SP-CLOTH", wh, 5)   // 15 short
	stock("SP-TEE", store, 50) // a store with surplus tees

	res, err := GenerateSupplyProposals(tenantID, "planner", wh, "reorder", 7, 10)
	if err != nil || res.Created < 2 {
		t.Fatalf("reorder proposals: %v %+v", err, res)
	}
	again, err := GenerateSupplyProposals(tenantID, "planner", wh, "reorder", 7, 10)
	if err != nil || again.Created != 0 || again.Refreshed != res.Created {
		t.Fatalf("regenerating must refresh, not duplicate: %v %+v (first %+v)", err, again, res)
	}
	if _, err := GenerateSupplyProposals(tenantID, "planner", wh, "mrp", 7, 10); err != nil {
		t.Fatalf("mrp proposals: %v", err)
	}
	list, err := ListSupplyProposals(tenantID, wh, ProposalOpen)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	by := map[string]SupplyProposal{}
	for _, p := range list {
		by[p.Source+"|"+p.Sku] = p
	}
	tee, cap, kit, cloth := by["Reorder|SP-TEE"], by["Reorder|SP-CAP"], by["MRP|SP-KIT"], by["MRP component|SP-CLOTH"]
	if tee.SuggestedQty != 8 || cap.SuggestedQty != 6 || kit.SuggestedQty != 10 || kit.BOMID != "BOM-SP-KIT" || cloth.SuggestedQty != 15 || cloth.ForItem != "SP-KIT" {
		t.Fatalf("proposals: tee %+v cap %+v kit %+v cloth %+v", tee, cap, kit, cloth)
	}

	// Purchase: tees (bumped to 10) and cloth on one PO to the vendor.
	if _, err := ApproveSupplyProposals(tenantID, "planner", "HQ", ApproveProposalsInput{IDs: []string{tee.ID}, Action: ProposalPurchase}); code(err) != "PLAN-0557" {
		t.Fatalf("purchase without a vendor: want PLAN-0557, got %v", err)
	}
	po, err := ApproveSupplyProposals(tenantID, "planner", "HQ", ApproveProposalsInput{IDs: []string{tee.ID, cloth.ID}, Action: ProposalPurchase,
		Vendor: "SP-VEND", Quantities: map[string]float64{tee.ID: 10}})
	if err != nil || po.Doctype != "PurchaseOrder" || len(po.Documents) != 1 {
		t.Fatalf("approve purchase: %v %+v", err, po)
	}
	poData, poStatus, _ := fetchDocData(tenantID, "PurchaseOrder", po.Documents[0])
	var lines []POLineInput
	json.Unmarshal([]byte(str(poData, "items")), &lines)
	if poStatus != "Draft" || str(poData, "vendor") != "SP-VEND" || str(poData, "location") != wh || len(lines) != 2 {
		t.Fatalf("PO %s: %s %v", po.Documents[0], poStatus, poData)
	}
	got := map[string]POLineInput{}
	for _, l := range lines {
		got[l.SKU] = l
	}
	if got["SP-TEE"].Qty != 10 || got["SP-TEE"].Rate != 120 || got["SP-CLOTH"].Qty != 15 || got["SP-CLOTH"].Rate != 15 {
		t.Fatalf("PO lines %+v", lines)
	}
	if str(poData, "source_proposal_ids") == "" {
		t.Fatalf("PO lacks its proposals")
	}
	// Once only.
	if _, err := ApproveSupplyProposals(tenantID, "planner", "HQ", ApproveProposalsInput{IDs: []string{tee.ID}, Action: ProposalPurchase, Vendor: "SP-VEND"}); code(err) != "PLAN-0557" {
		t.Fatalf("approving a converted proposal again: want PLAN-0557, got %v", err)
	}

	// Transfer: caps from the store.
	if _, err := ApproveSupplyProposals(tenantID, "planner", "HQ", ApproveProposalsInput{IDs: []string{cap.ID}, Action: ProposalTransfer, FromLocation: wh}); code(err) != "PLAN-0557" {
		t.Fatalf("transfer from itself: want PLAN-0557, got %v", err)
	}
	to, err := ApproveSupplyProposals(tenantID, "planner", "HQ", ApproveProposalsInput{IDs: []string{cap.ID}, Action: ProposalTransfer, FromLocation: store})
	if err != nil || to.Doctype != "TransferOrder" {
		t.Fatalf("approve transfer: %v %+v", err, to)
	}
	toData, _, _ := fetchDocData(tenantID, "TransferOrder", to.Documents[0])
	if str(toData, "from_warehouse") != store || str(toData, "to_warehouse") != wh || str(toData, "items") != `[{"sku":"SP-CAP","qty":6}]` {
		t.Fatalf("transfer %v", toData)
	}

	// Production: the kit on its BOM; a bought item cannot be produced.
	if _, err := ApproveSupplyProposals(tenantID, "planner", "HQ", ApproveProposalsInput{IDs: []string{kit.ID}, Action: ProposalProduction}); err != nil {
		t.Fatalf("approve production: %v", err)
	}
	kitAfter, _ := ListSupplyProposals(tenantID, wh, ProposalConverted)
	var pro string
	for _, p := range kitAfter {
		if p.ID == kit.ID {
			pro = p.TargetID
		}
	}
	proData, proStatus, err := fetchDocData(tenantID, "ProductionOrder", pro)
	if err != nil || proStatus != "Draft" || str(proData, "bom_id") != "BOM-SP-KIT" || numFromInterface(proData["quantity"]) != 10 || str(proData, "source_proposal_ids") != kit.ID {
		t.Fatalf("production order %s: %v %s %v", pro, err, proStatus, proData)
	}

	// Reject needs a reason; a regenerated run brings back only what is still short.
	if err := RejectSupplyProposals(tenantID, "planner", "", []string{kit.ID}); code(err) != "PLAN-0557" {
		t.Fatalf("reject without a reason: want PLAN-0557, got %v", err)
	}
	// The reorder run also proposed buying the kit and cloth; MRP covers those.
	open, _ := ListSupplyProposals(tenantID, wh, ProposalOpen)
	var dup []string
	for _, p := range open {
		dup = append(dup, p.ID)
	}
	if err := RejectSupplyProposals(tenantID, "planner", "covered by the MRP proposals", dup); err != nil {
		t.Fatalf("reject reorder duplicates: %v", err)
	}
	open, _ = ListSupplyProposals(tenantID, wh, ProposalOpen)
	if len(open) != 0 {
		t.Fatalf("every proposal was decided, %d still open", len(open))
	}
	GenerateSupplyProposals(tenantID, "planner", wh, "reorder", 7, 10)
	open, _ = ListSupplyProposals(tenantID, wh, ProposalOpen)
	var ids []string
	for _, p := range open {
		ids = append(ids, p.ID)
	}
	if len(ids) == 0 {
		t.Fatalf("stock is still short until the PO arrives - a new run should propose again")
	}
	if err := RejectSupplyProposals(tenantID, "planner", "already on order", ids); err != nil {
		t.Fatalf("reject: %v", err)
	}
	rejected, _ := ListSupplyProposals(tenantID, wh, ProposalRejected)
	if len(rejected) < len(ids) || rejected[0].Reason != "already on order" {
		t.Fatalf("rejected %+v", rejected)
	}
}
