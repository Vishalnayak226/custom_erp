package engines

import (
	"testing"
	"time"
)

func promiseDay(s string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", s, BusinessLocation())
	return t
}

// Stage 148.27: the promise date is stock today, else confirmed arrivals, plus
// courier days, skipping closed days, with a cut-off hour. Reads only.
func TestStage14827PromiseCompute(t *testing.T) {
	stock := []LocationATS{{LocationCode: "Mumbai", ATS: 2}}
	arr := []promiseArrival{{Day: "2026-11-03", Qty: 5, Doc: "PO-123", Kind: "Purchase order"}}
	in := promiseInput{Qty: 5, Stock: stock, Arrivals: arr, Now: promiseDay("2026-11-02 10:00"), CutoffHour: -1}
	got := computePromise(in)
	if got.Date != "2026-11-03" || got.Reason != "2 in Mumbai, 3 on PO-123 due 3 Nov" {
		t.Fatalf("stock + PO: %+v", got)
	}
	// Enough stock ships today; courier days add, closed days do not count.
	in = promiseInput{Qty: 2, Stock: stock, Now: promiseDay("2026-11-06 10:00"), CourierDays: 2, CutoffHour: -1, Closed: parseClosedDays("Sun")}
	if got = computePromise(in); got.Date != "2026-11-09" { // Fri + 2 open days: Sat, skip Sun, Mon
		t.Fatalf("courier: %+v", got)
	}
	// After the cut-off the dispatch is the next open day.
	in = promiseInput{Qty: 2, Stock: stock, Now: promiseDay("2026-11-06 16:00"), CutoffHour: 15, Closed: parseClosedDays("Sat, Sun")}
	if got = computePromise(in); got.Date != "2026-11-09" {
		t.Fatalf("cut-off: %+v", got)
	}
	// Short with nothing due: no date, and the reason says so.
	in = promiseInput{Qty: 5, Stock: stock, Now: promiseDay("2026-11-02 10:00"), CutoffHour: -1}
	if got = computePromise(in); got.Date != "" || got.Reason != "2 in Mumbai, short 3, none due" {
		t.Fatalf("short: %+v", got)
	}
	if h := cutoffFor("HO=15, HO/Shopify=12", "HO", "Shopify"); h != 12 {
		t.Fatalf("store/channel cut-off: %d", h)
	}
	if h := cutoffFor("HO=15", "HO", "Shopify"); h != 15 {
		t.Fatalf("store cut-off: %d", h)
	}
	if h := cutoffFor("", "HO", "Shopify"); h != -1 {
		t.Fatalf("empty: %d", h)
	}
}

func TestStage14827PromiseOnOrderAndLate(t *testing.T) {
	tenantID, _, cleanup, insert := gateEntryTestTenant(t)
	defer cleanup()
	insert("C-P1", "Customer", "Active", map[string]interface{}{"name": "Pat"})
	insert("SO-P1", "SalesOrder", "Open", map[string]interface{}{"customer_id": "C-P1"})
	insert("SO-P1-L1", "SalesOrderLine", "Reserved", map[string]interface{}{"order_id": "SO-P1", "sku": "SKU-P", "qty": 2, "line_status": "Reserved", "promise_date": "2020-01-01", "promise_reason": "x"})
	insert("SO-P1-L2", "SalesOrderLine", "Reserved", map[string]interface{}{"order_id": "SO-P1", "sku": "SKU-P", "qty": 1, "line_status": "Reserved", "promise_date": "2999-01-01"})
	insert("SO-P1-L3", "SalesOrderLine", "Dispatched", map[string]interface{}{"order_id": "SO-P1", "sku": "SKU-P", "qty": 1, "line_status": "Dispatched", "promise_date": "2020-01-01"})
	rows, err := LatePromiseLines(tenantID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("late: %v %v", rows, err)
	}
	if rows[0]["sku"] != "SKU-P" || rows[0]["customer_name"] != "Pat" || rows[0]["days_late"].(int) < 1000 {
		t.Fatalf("late row: %v", rows[0])
	}
	// Off by default: no promise.
	if p, err := PromiseForLine(tenantID, "SKU-P", 1, "", ""); err != nil || p.Date != "" || p.Reason != "" {
		t.Fatalf("off: %+v %v", p, err)
	}
}

// 73.4.2.1: an order keeps its own promise - its latest line's - and none
// when a line cannot be promised; off, the order carries no promise at all.
func TestStage73421OrderPromise(t *testing.T) {
	if p := OrderPromise([]string{"A", "B"}, []PromiseResult{{}, {}}); p != (PromiseResult{}) {
		t.Fatalf("off: %+v", p)
	}
	if p := OrderPromise([]string{"A", "B"}, []PromiseResult{{Date: "2026-11-14", Reason: "in stock"}, {Date: "2026-11-20", Reason: "PO"}}); p.Date != "2026-11-20" {
		t.Fatalf("latest line: %+v", p)
	}
	if p := OrderPromise([]string{"A", "B"}, []PromiseResult{{Date: "2026-11-14"}, {Reason: "short 2, none due"}}); p.Date != "" || p.Reason != "no date for B" {
		t.Fatalf("a line with no date: %+v", p)
	}

	f := newCreditFixture(t)
	off := f.order("Pat", "", 2)
	if f.field("SalesOrder", off, "promise_date") != "" || f.field("SalesOrder", off, "promise_reason") != "" {
		t.Fatal("setting off: the order must carry no promise")
	}
	if err := SetSetting(f.tenantID, SettingPromiseDates, "true", "admin"); err != nil {
		t.Fatal(err)
	}
	on := f.order("Pat", "", 2)
	line := f.field("SalesOrderLine", on+"-L1", "promise_date")
	if line == "" || f.field("SalesOrder", on, "promise_date") != line {
		t.Fatalf("order promise %q must be the line's %q", f.field("SalesOrder", on, "promise_date"), line)
	}
}
