package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

// Ask 170.8: the cost of a maintenance order.

type mcost170 struct {
	*mc1481
	asset, sku string
}

func newMCost170(t *testing.T, on bool) *mcost170 {
	m := newMC1481(t)
	v := "false"
	if on {
		v = "true"
	}
	if err := SetSetting("default", SettingMaintenanceCosting, v, "t1708"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetSetting("default", SettingMaintenanceCosting, "false", "t1708") })
	c := &mcost170{mc1481: m}
	c.asset = m.id("MA")
	m.doc(c.asset, "Asset", "Capitalised", map[string]interface{}{"code": c.asset, "category": "Test", "status": "Capitalised"})
	c.sku = m.item(nil)
	m.receive(c.sku, 10, 100)
	return c
}

// inProgress raises an order and starts it.
func (c *mcost170) inProgress() string {
	c.t.Helper()
	id, err := CreateMaintenanceOrder(c.tenant, c.asset, "Service", time.Now().Format("2006-01-02"), "", "system")
	if err != nil {
		c.t.Fatalf("create: %v", err)
	}
	c.docs = append(c.docs, id)
	if err := StartMaintenanceOrderBy(c.tenant, id, "system"); err != nil {
		c.t.Fatalf("start: %v", err)
	}
	return id
}

func (c *mcost170) glRows(order string) int {
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+c.schema+".gl_postings WHERE document_type = 'MaintenanceOrder' AND document_id = $1", order).Scan(&n)
	return n
}

func TestMaintenanceCostOffRecordsButPostsNothing(t *testing.T) {
	c := newMCost170(t, false)
	id := c.inProgress()
	err := CompleteMaintenanceOrderWith(c.tenant, id, MaintenanceCompletion{Notes: "done", Hours: 2, Rate: 500, Downtime: 4,
		Parts: []MaintenancePart{{Sku: c.sku, Qty: 3, Location: c.loc}}}, "system")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	d, status, _ := fetchDocData(c.tenant, "MaintenanceOrder", id)
	if status != "Completed" || numFromInterface(d["labour_hours"]) != 2 || numFromInterface(d["labour_rate"]) != 500 || numFromInterface(d["downtime_hours"]) != 4 {
		t.Fatalf("the figures are always recorded: %s %v", status, d)
	}
	if _, has := d["cost_posted"]; has || c.glRows(id) != 0 || c.onHand(c.sku) != 10 {
		t.Fatalf("setting off: posted %v, gl rows %d, on hand %v", d["cost_posted"], c.glRows(id), c.onHand(c.sku))
	}
}

func TestMaintenanceCostOnIssuesPartsAndPostsOnce(t *testing.T) {
	c := newMCost170(t, true)
	id := c.inProgress()
	in := MaintenanceCompletion{Notes: "done", Hours: 2, Rate: 500, Parts: []MaintenancePart{{Sku: c.sku, Qty: 3, Location: c.loc}}}
	if err := CompleteMaintenanceOrderWith(c.tenant, id, in, "system"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got := c.onHand(c.sku); got != 7 {
		t.Fatalf("on hand = %v, want 7", got)
	}
	parts, labour := -c.gl(id, "1200"), -c.gl(id, "5315")
	if parts <= 0 || labour != 100000 {
		t.Fatalf("credits: inventory %d, labour %d", parts, labour)
	}
	if got := c.gl(id, "5314"); got != parts+labour {
		t.Fatalf("expense %d != inventory %d + labour %d", got, parts, labour)
	}
	c.wantBalanced(id)
	// Completing again is refused and posts nothing more.
	if err := CompleteMaintenanceOrderWith(c.tenant, id, in, "system"); err == nil {
		t.Fatal("a Completed order must not complete again")
	}
	if c.onHand(c.sku) != 7 || -c.gl(id, "5315") != 100000 {
		t.Fatal("a second completion posted again")
	}
}

func TestMaintenanceCostRollsBackWhenStockIsShortOrStoreMissing(t *testing.T) {
	c := newMCost170(t, true)
	id := c.inProgress()
	err := CompleteMaintenanceOrderWith(c.tenant, id, MaintenanceCompletion{Notes: "done", Hours: 1, Rate: 100,
		Parts: []MaintenancePart{{Sku: c.sku, Qty: 50, Location: c.loc}}}, "system")
	if validationCode(err) != "ASSET-1728" {
		t.Fatalf("short stock: %v", err)
	}
	_, status, _ := fetchDocData(c.tenant, "MaintenanceOrder", id)
	if status != "InProgress" || c.glRows(id) != 0 || c.onHand(c.sku) != 10 {
		t.Fatalf("not rolled back: %s, gl %d, on hand %v", status, c.glRows(id), c.onHand(c.sku))
	}
	err = CompleteMaintenanceOrderWith(c.tenant, id, MaintenanceCompletion{Notes: "done",
		Parts: []MaintenancePart{{Sku: c.sku, Qty: 1}}}, "system")
	if validationCode(err) != "ASSET-1728" {
		t.Fatalf("no store: %v", err)
	}
	// Labour only: no parts, no stock movement.
	if err := CompleteMaintenanceOrderWith(c.tenant, id, MaintenanceCompletion{Notes: "done", Hours: 1, Rate: 250}, "system"); err != nil {
		t.Fatalf("labour only: %v", err)
	}
	if -c.gl(id, "5315") != 25000 || c.onHand(c.sku) != 10 {
		t.Fatalf("labour only: %d", -c.gl(id, "5315"))
	}
}
