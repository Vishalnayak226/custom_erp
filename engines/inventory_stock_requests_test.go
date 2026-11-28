package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Items 324.1-324.2 on a provisioned throwaway client: the sourcing engine
// ranks sources on spare stock, distance and kind, leaves promised stock
// alone and joins lines into shipments; a request is accepted, accepted with
// less or rejected only at its source; auto requests skip what is already
// asked for; a built-in reason passes where reason codes are required.
func TestStockRequestSourcingAndFlow(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	const req, wh, far, keep = "LOC-SR-REQ", "LOC-SR-WH", "LOC-SR-FAR", "LOC-SR-KEEP"
	loc := func(id, typ, city, state string) {
		data, _ := json.Marshal(map[string]interface{}{"code": id, "name": id, "type": typ, "city": city, "state": state})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", id, data)
	}
	loc(req, "Store", "Pune", "Maharashtra")
	loc(wh, "Warehouse", "Pune", "Maharashtra")
	loc(far, "Store", "Mumbai", "Maharashtra")
	loc(keep, "Store", "Pune", "Maharashtra")
	for _, sku := range []string{"SR-A", "SR-B", "SR-C", "SR-D", "SR-E"} {
		data, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "Item " + sku, "standard_cost": 10})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+sku, data)
	}
	stock := func(sku, at string, qty int) {
		exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, $3, $3)", sku, at, qty)
	}
	reorder := func(sku, at string, safety int) {
		data, _ := json.Marshal(map[string]interface{}{"item": sku, "location_code": at, "lead_time_days": 7, "safety_stock_qty": safety})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReorderPointConfig', $2, 'Active', 'system')",
			"RPC-"+sku+"-"+at, data)
	}
	stock("SR-A", req, 1)
	stock("SR-A", wh, 10)
	stock("SR-A", far, 50)
	stock("SR-A", keep, 10)
	reorder("SR-A", keep, 8) // KEEP holds 8 for its own sales: 2 spare
	stock("SR-B", wh, 30)
	stock("SR-C", far, 6)
	stock("SR-C", keep, 6)
	stock("SR-D", far, 20)
	exec("INSERT INTO " + schema + ".approval_rules (doctype, min_amount, max_amount, required_role) VALUES ('TransferOrder', 0, NULL, 'Store Manager') ON CONFLICT (doctype, min_amount) DO NOTHING")

	option := func(l SourcedLine, at string) *SourceOption {
		for i := range l.Options {
			if l.Options[i].Location == at {
				return &l.Options[i]
			}
		}
		return nil
	}
	planOf := func(l SourcedLine) string {
		var parts []string
		for _, p := range l.Plan {
			parts = append(parts, fmt.Sprintf("%s:%g", p.From, p.Qty))
		}
		return strings.Join(parts, ",")
	}

	t.Run("ranks sources on spare stock, distance and kind", func(t *testing.T) {
		plan, err := SuggestRequestSources(tenantID, req, []StockRequestLineIn{{SKU: "SR-A", Qty: 12}})
		if err != nil {
			t.Fatalf("SuggestRequestSources: %v", err)
		}
		l := plan.Lines[0]
		if l.HaveHere != 1 || l.Name != "Item SR-A" {
			t.Fatalf("line: have here %g, name %q", l.HaveHere, l.Name)
		}
		var order []string
		for _, o := range l.Options {
			order = append(order, o.Location)
		}
		if strings.Join(order, ",") != wh+","+far+","+keep {
			t.Fatalf("ranking %v, want the same-city warehouse, then the same-state store, then the store keeping its stock", order)
		}
		if o := option(l, keep); o.Keeps != 8 || o.Spare != 2 {
			t.Fatalf("KEEP keeps %g spare %g, want 8 and 2", o.Keeps, o.Spare)
		}
		if o := option(l, far); o.DistanceNote != "same state" {
			t.Fatalf("FAR distance note %q", o.DistanceNote)
		}
		if why := strings.Join(option(l, wh).Why, "|"); !strings.Contains(why, "same city") || !strings.Contains(why, "warehouse") {
			t.Fatalf("WH why %q", why)
		}
		// 10 from the warehouse; the last 2 from the store whose 2 spare fill it.
		if got := planOf(l); got != wh+":10,"+keep+":2" || l.Short != 0 {
			t.Fatalf("plan %s short %g", got, l.Short)
		}
	})

	t.Run("stock already promised is not spare", func(t *testing.T) {
		items, _ := json.Marshal([]map[string]interface{}{{"sku": "SR-A", "qty": 4}})
		data, _ := json.Marshal(map[string]interface{}{"from_warehouse": wh, "to_warehouse": far, "items": string(items)})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('TO-SR-PROMISED', 'TransferOrder', $1, 'Draft', 'system')", data)
		defer exec("DELETE FROM " + schema + ".documents WHERE id = 'TO-SR-PROMISED'")
		plan, err := SuggestRequestSources(tenantID, req, []StockRequestLineIn{{SKU: "SR-A", Qty: 12}})
		if err != nil {
			t.Fatalf("SuggestRequestSources: %v", err)
		}
		if o := option(plan.Lines[0], wh); o == nil || o.Promised != 4 || o.Spare != 6 {
			t.Fatalf("WH option %+v, want 4 promised and 6 spare", o)
		}
	})

	t.Run("a line joins a source already sending", func(t *testing.T) {
		alone, err := SuggestRequestSources(tenantID, req, []StockRequestLineIn{{SKU: "SR-C", Qty: 4}})
		if err != nil {
			t.Fatalf("SuggestRequestSources: %v", err)
		}
		if got := planOf(alone.Lines[0]); got != keep+":4" {
			t.Fatalf("SR-C alone from %s, want the same-city store", got)
		}
		both, err := SuggestRequestSources(tenantID, req, []StockRequestLineIn{{SKU: "SR-C", Qty: 4}, {SKU: "SR-D", Qty: 10}})
		if err != nil {
			t.Fatalf("SuggestRequestSources: %v", err)
		}
		if got := planOf(both.Lines[0]); got != far+":4" || len(both.Shipments) != 1 {
			t.Fatalf("SR-C with SR-D from %s, %d shipments; want one shipment from %s", got, len(both.Shipments), far)
		}
	})

	t.Run("accept, accept less and reject only at the source", func(t *testing.T) {
		res, err := CreateStockRequest(tenantID, "maker", "Store Manager", req, StockRequestInput{
			Reason: "Advance order", Note: "wedding order", Lines: []StockRequestLineIn{{SKU: "SR-B", Qty: 4, From: wh}}})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if res.RequestNo == "" || len(res.Transfers) != 1 || res.Transfers[0].Status != "Pending Approval" || res.Transfers[0].From != wh {
			t.Fatalf("create result %+v", res)
		}
		id := res.Transfers[0].ID
		if err := AcceptStockRequest(tenantID, id, "maker", "Store Manager", req, nil); code(err) != "STOCKT-0494" {
			t.Fatalf("the asking store must not accept its own request, got %v", err)
		}
		if err := AcceptStockRequest(tenantID, id, "checker", "Store Manager", wh, []StockRequestLineIn{{SKU: "SR-B", Qty: 5}}); code(err) != "STOCKT-0494" {
			t.Fatalf("sending more than asked must be refused, got %v", err)
		}
		if err := AcceptStockRequest(tenantID, id, "checker", "Store Manager", wh, []StockRequestLineIn{{SKU: "SR-B", Qty: 0}}); code(err) != "STOCKT-0494" {
			t.Fatalf("accepting nothing must point to reject, got %v", err)
		}
		if err := AcceptStockRequest(tenantID, id, "checker", "Store Manager", wh, []StockRequestLineIn{{SKU: "SR-B", Qty: 3}}); err != nil {
			t.Fatalf("accept less: %v", err)
		}
		data, status, err := fetchDocData(tenantID, "TransferOrder", id)
		if err != nil || status != "Approved" || data["accepted_less"] != true {
			t.Fatalf("after accept less: %s %v %v", status, data["accepted_less"], err)
		}
		lines, _ := parseTransferItems(data["items"].(string))
		var raw []map[string]interface{}
		_ = json.Unmarshal([]byte(data["items"].(string)), &raw)
		if len(lines) != 1 || lines[0].Qty != 3 || numFromInterface(raw[0]["requested_qty"]) != 4 {
			t.Fatalf("lines after accept less: %+v %v", lines, raw)
		}
		if err := RejectStockRequest(tenantID, id, "checker", "Store Manager", wh, "too late"); code(err) != "STOCKT-0494" {
			t.Fatalf("an accepted request must not be rejected, got %v", err)
		}

		res2, err := CreateStockRequest(tenantID, "maker", "Store Manager", req, StockRequestInput{
			Reason: "Customer order", Lines: []StockRequestLineIn{{SKU: "SR-B", Qty: 2, From: wh}}})
		if err != nil {
			t.Fatalf("create second: %v", err)
		}
		id2 := res2.Transfers[0].ID
		if err := RejectStockRequest(tenantID, id2, "checker", "Store Manager", wh, " "); code(err) != "APPROV-0159" {
			t.Fatalf("a reject needs a reason, got %v", err)
		}
		if err := RejectStockRequest(tenantID, id2, "other", "Store Manager", far, "no"); code(err) != "STOCKT-0494" {
			t.Fatalf("another store must not reject, got %v", err)
		}
		if err := RejectStockRequest(tenantID, id2, "checker", "Store Manager", wh, "saving it for a launch"); err != nil {
			t.Fatalf("reject: %v", err)
		}
		mine, err := ListStockRequests(tenantID, "Store Manager", req, "mine", "", "")
		if err != nil {
			t.Fatalf("list mine: %v", err)
		}
		seen := map[string]StockRequestRow{}
		for _, r := range mine {
			seen[r.ID] = r
		}
		if r := seen[id2]; r.Status != "Rejected" || r.RejectedReason != "saving it for a launch" {
			t.Fatalf("rejected row %+v", r)
		}
		if r := seen[id]; r.Reason != "Advance order" || r.Note != "wedding order" || !r.AcceptedLess || r.Units != 3 {
			t.Fatalf("accepted row %+v", r)
		}
		incoming, err := ListStockRequests(tenantID, "Store Manager", wh, "incoming", "", "open")
		if err != nil {
			t.Fatalf("list incoming: %v", err)
		}
		if len(incoming) != 1 || incoming[0].ID != id {
			t.Fatalf("open incoming at WH %+v, want only the accepted one", incoming)
		}
	})

	t.Run("a built-in reason passes where codes are required", func(t *testing.T) {
		if err := SetSetting(tenantID, SettingReasonCodesRequired, "true", "system"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		defer SetSetting(tenantID, SettingReasonCodesRequired, "false", "system")
		if _, err := CreateStockRequest(tenantID, "maker", "Store Manager", req, StockRequestInput{
			Reason: "my own words", Lines: []StockRequestLineIn{{SKU: "SR-B", Qty: 1, From: wh}}}); code(err) != "MASTER-0975" {
			t.Fatalf("typed text must need a code here, got %v", err)
		}
		if _, err := CreateStockRequest(tenantID, "maker", "Store Manager", req, StockRequestInput{
			Reason: "advance ORDER", Lines: []StockRequestLineIn{{SKU: "SR-B", Qty: 1, From: wh}}}); err != nil {
			t.Fatalf("a built-in reason must pass: %v", err)
		}
	})

	t.Run("auto asks for what is short, once", func(t *testing.T) {
		stock("SR-E", req, 0)
		reorder("SR-E", req, 6)
		stock("SR-E", wh, 20)
		out, err := RunAutoStockRequests(tenantID, req, "maker", "Store Manager", req)
		if err != nil || len(out) != 1 {
			t.Fatalf("auto run: %v %+v", err, out)
		}
		if out[0].Request == nil || len(out[0].Request.Transfers) != 1 || out[0].Request.Transfers[0].From != wh || out[0].Request.Transfers[0].Units != 6 {
			t.Fatalf("auto result %+v (%s)", out[0].Request, out[0].Message)
		}
		data, _, _ := fetchDocData(tenantID, "TransferOrder", out[0].Request.Transfers[0].ID)
		if data["request_origin"] != "Auto" || data["request_reason"] != "Replenishment" {
			t.Fatalf("auto request origin %v reason %v", data["request_origin"], data["request_reason"])
		}
		again, err := RunAutoStockRequests(tenantID, req, "maker", "Store Manager", req)
		if err != nil || len(again) != 1 || again[0].Request != nil || again[0].Short != 0 {
			t.Fatalf("second auto run must ask for nothing: %v %+v", err, again)
		}
		if _, err := RunAutoStockRequests(tenantID, req, "x", "Store Manager", wh); code(err) != "STOCKT-0494" {
			t.Fatalf("another store must not run auto for this one, got %v", err)
		}
	})
}
