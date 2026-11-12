package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// TestStage14643BackorderListFeedsBuying: unallocated order lines are listed
// per SKU and location, net of what is on its way, and "Send to buying" makes
// open supply proposals; approving them gives a Draft PO, never a raised one.
func TestStage14643BackorderListFeedsBuying(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		loc = "S14643-LOC"
		sA  = "S14643-A" // 8 waiting, nothing coming
		sB  = "S14643-B" // 5 waiting, 5 on an open PO
		sC  = "S14643-C" // 6 waiting, 2 in stock
	)
	ids := []string{"S14643-L1", "S14643-L2", "S14643-L3", "S14643-L4", "S14643-L5", "S14643-PO1", "S14643-V"}
	clean := func() {
		for _, id := range ids {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'SupplyProposal' AND data->>'location' = '" + loc + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'PurchaseOrder' AND data->>'source_proposal_ids' LIKE 'SP%' AND data->>'vendor' = 'S14643-V'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE location_code = '" + loc + "'")
	}
	clean()
	defer clean()
	put := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 2, 2)", sC, loc); err != nil {
		t.Fatalf("stock: %v", err)
	}
	put("S14643-L1", "SalesOrderLine", "Pending", map[string]interface{}{"sku": sA, "qty": 5, "location_code": loc, "order_id": "SO-1"})
	put("S14643-L2", "SalesOrderLine", "Pending", map[string]interface{}{"sku": sA, "qty": 3, "location_code": loc, "order_id": "SO-2"})
	put("S14643-L3", "SalesOrderLine", "Pending", map[string]interface{}{"sku": sB, "qty": 5, "location_code": loc, "order_id": "SO-3"})
	put("S14643-L4", "SalesOrderLine", "Pending", map[string]interface{}{"sku": sC, "qty": 6, "location_code": loc, "order_id": "SO-4"})
	put("S14643-L5", "SalesOrderLine", "Allocated", map[string]interface{}{"sku": sA, "qty": 99, "location_code": loc, "order_id": "SO-5"})
	put("S14643-V", "Vendor", "Active", map[string]interface{}{"code": "S14643-V", "name": "Back order vendor", "state": "Maharashtra"})
	items, _ := json.Marshal([]map[string]interface{}{{"sku": sB, "qty": 5}})
	put("S14643-PO1", "PurchaseOrder", "Approved", map[string]interface{}{"items": string(items), "target_warehouse": loc, "vendor": "V"})

	rows, err := ListBackorders(tenantID, loc)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	by := map[string]BackorderRow{}
	for _, r := range rows {
		by[r.Sku] = r
	}
	if r := by[sA]; r.Waiting != 8 || r.Orders != 2 || r.ToBuy != 8 {
		t.Fatalf("A: want waiting 8 / 2 orders / buy 8 (an allocated line is not waiting), got %+v", r)
	}
	if r := by[sB]; r.Waiting != 5 || r.OnOrder != 5 || r.ToBuy != 0 {
		t.Fatalf("B: an open PO covers it, want buy 0, got %+v", r)
	}
	if r := by[sC]; r.ToBuy != 4 {
		t.Fatalf("C: 6 waiting less 2 in stock, want buy 4, got %+v", r)
	}

	// The report carries the same rows.
	_, rep, _, err := RunReport(tenantID, "backorder-list", "Super Admin", "admin", map[string]string{"location_code": loc})
	if err != nil || len(rep) != 3 {
		t.Fatalf("report: %v rows=%d", err, len(rep))
	}

	// Send to buying: proposals for A and C only (B is covered).
	res, err := GenerateSupplyProposals(tenantID, "admin", loc, "backorder", 7, 0)
	if err != nil || res.Created != 2 {
		t.Fatalf("send to buying: %v %+v", err, res)
	}
	again, _ := GenerateSupplyProposals(tenantID, "admin", loc, "backorder", 7, 0)
	if again.Created != 0 || again.Refreshed != 2 {
		t.Fatalf("a second send must refresh, not duplicate: %+v", again)
	}
	props, _ := ListSupplyProposals(tenantID, loc, ProposalOpen)
	var idA string
	for _, p := range props {
		if p.Source != "Back order" {
			t.Fatalf("source: %+v", p)
		}
		if p.Sku == sA {
			idA = p.ID
			if p.SuggestedQty != 8 {
				t.Fatalf("A proposal qty: %+v", p)
			}
		}
	}
	po, err := ApproveSupplyProposals(tenantID, "admin", "HQ", ApproveProposalsInput{IDs: []string{idA}, Action: ProposalPurchase, Vendor: "S14643-V"})
	if err != nil || len(po.Documents) != 1 {
		t.Fatalf("approve: %v %+v", err, po)
	}
	if _, st, _ := fetchDocData(tenantID, "PurchaseOrder", po.Documents[0]); st != "Draft" {
		t.Fatalf("a back-order PO is raised as Draft for the buyer, got %s", st)
	}
	_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", po.Documents[0])
}
