package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 146.3.12 (D23): one allocation upload (store, sku, qty rows) makes one
// Draft transfer per store. The dry run shows per-row errors and what the
// source cannot cover; a run ID refuses a second upload; two further actions
// submit the Drafts and then bulk-approve them through the ordinary transfer
// approval, so the maker is never the checker by accident.
func TestStage146312StoreAllocationUpload(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	const src, s1, s2, skuA, skuB = "ALLOC-SRC", "ALLOC-S1", "ALLOC-S2", "ALLOC-A", "ALLOC-B"
	for _, l := range []string{src, s1, s2} {
		insert("LOC-"+l, "Location", "Active", "system", map[string]interface{}{"code": l, "name": l, "type": "Store"})
	}
	for _, s := range []string{skuA, skuB} {
		insert("ITEM-"+s, "Item", "Active", "system", map[string]interface{}{"code": s, "name": s, "standard_cost": 10.0})
	}
	for sku, n := range map[string]int{skuA: 10, skuB: 5} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, $3, $3)", sku, src, n); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".approval_rules (doctype, min_amount, max_amount, required_role) VALUES ('TransferOrder', 0, NULL, 'Store Manager') ON CONFLICT (doctype, min_amount) DO NOTHING"); err != nil {
		t.Fatal(err)
	}
	code := func(err error) string {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Code
		}
		return ""
	}
	count := func(run string) (n int) {
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'TransferOrder' AND data->>'allocation_run_id' = $1", run).Scan(&n)
		return
	}
	req := AllocationRequest{RunID: "RUN-1", FromWarehouse: src, Purpose: "New store"}
	good := "Store,SKU,Qty\n" + s1 + "," + skuA + ",5\n" + s2 + "," + skuA + ",8\n" + s1 + "," + skuB + ",2\n" + s1 + "," + skuA + ",1\n"

	// Dry run: nothing is made; duplicate store + SKU rows are summed; the
	// source is 4 short on skuA (14 wanted of 10).
	res, err := RunStoreAllocation(tenantID, req, strings.NewReader(good), "maker", true)
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("dry run: %v %+v", err, res)
	}
	if count("RUN-1") != 0 || len(res.Stores) != 2 {
		t.Fatalf("dry run made %d transfers, shows %d stores; want 0 and 2", count("RUN-1"), len(res.Stores))
	}
	if len(res.Shortfalls) != 1 || res.Shortfalls[0].Sku != skuA || res.Shortfalls[0].Wanted != 14 || res.Shortfalls[0].Short != 4 {
		t.Fatalf("shortfalls = %+v; want %s wanted 14, short 4", res.Shortfalls, skuA)
	}

	// Bad rows: per-row errors with the file's row number; a real run makes nothing.
	bad := "store,sku,qty\n" + s1 + "," + skuA + ",3\nNOWHERE," + skuA + ",3\n" + s2 + ",NO-SUCH-SKU,3\n" + s2 + "," + skuA + ",0\n" + src + "," + skuA + ",1\n"
	res, err = RunStoreAllocation(tenantID, AllocationRequest{RunID: "RUN-BAD", FromWarehouse: src}, strings.NewReader(bad), "maker", true)
	if err != nil || len(res.Errors) != 4 || res.Errors[0].RowNumber != 3 {
		t.Fatalf("bad rows: %v %+v; want 4 row errors starting at row 3", err, res.Errors)
	}
	if _, err = RunStoreAllocation(tenantID, AllocationRequest{RunID: "RUN-BAD", FromWarehouse: src}, strings.NewReader(bad), "maker", false); code(err) != "STOCKT-0488" || count("RUN-BAD") != 0 {
		t.Fatalf("a real run with bad rows must make nothing (STOCKT-0488), got %v / %d", err, count("RUN-BAD"))
	}
	if _, err = RunStoreAllocation(tenantID, AllocationRequest{FromWarehouse: src}, strings.NewReader(good), "maker", true); code(err) != "STOCKT-0488" {
		t.Fatalf("a missing run ID should be STOCKT-0488, got %v", err)
	}
	if _, err = RunStoreAllocation(tenantID, AllocationRequest{RunID: "RUN-X", FromWarehouse: src}, strings.NewReader("a,b\n1,2\n"), "maker", true); code(err) != "STOCKT-0488" {
		t.Fatalf("a file without store/sku/qty columns should be STOCKT-0488, got %v", err)
	}

	// Real run: one Draft per store, carrying the run and the purpose.
	res, err = RunStoreAllocation(tenantID, req, strings.NewReader(good), "maker", false)
	if err != nil || len(res.Created) != 2 || count("RUN-1") != 2 {
		t.Fatalf("real run: %v %+v (%d made)", err, res, count("RUN-1"))
	}
	var items, purpose, status string
	if err := db.DB.QueryRow("SELECT data->>'items', data->>'purpose', status FROM "+schema+".documents WHERE id = $1", res.Created[0]).Scan(&items, &purpose, &status); err != nil {
		t.Fatal(err)
	}
	if status != "Draft" || purpose != "New store" || !strings.Contains(items, skuA) {
		t.Fatalf("draft = %s %q %q", status, purpose, items)
	}
	// The run ID refuses a second upload - dry run or real.
	for _, dry := range []bool{true, false} {
		if _, err := RunStoreAllocation(tenantID, req, strings.NewReader(good), "maker", dry); code(err) != "STOCKT-0487" {
			t.Fatalf("duplicate run (dry=%v) should be STOCKT-0487, got %v", dry, err)
		}
	}

	// One bulk approve: nothing to approve until submitted; the maker cannot
	// check; a checker approves the whole run in one action.
	if _, _, err := DecideAllocationRun(tenantID, "RUN-1", "approve", "checker", "Store Manager", src); code(err) != "STOCKT-0492" {
		t.Fatalf("approve before submit should be STOCKT-0492, got %v", err)
	}
	if _, _, err := DecideAllocationRun(tenantID, "RUN-NONE", "submit", "maker", "Store Manager", src); code(err) != "STOCKT-0492" {
		t.Fatalf("unknown run should be STOCKT-0492, got %v", err)
	}
	ok, failed, err := DecideAllocationRun(tenantID, "RUN-1", "submit", "maker", "Store Manager", src)
	if err != nil || len(ok) != 2 || len(failed) != 0 {
		t.Fatalf("submit: %v ok=%v failed=%v", err, ok, failed)
	}
	ok, failed, err = DecideAllocationRun(tenantID, "RUN-1", "approve", "checker", "Store Manager", src)
	if err != nil || len(ok) != 2 || len(failed) != 0 {
		t.Fatalf("approve: %v ok=%v failed=%v", err, ok, failed)
	}
	run, err := GetAllocationRun(tenantID, "RUN-1")
	if err != nil || len(run) != 2 {
		t.Fatalf("run = %+v, %v", run, err)
	}
	for _, r := range run {
		if r.Status != "Approved" || r.Lines == 0 {
			t.Fatalf("run transfer %+v; want Approved", r)
		}
	}
}
