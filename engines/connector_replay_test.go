package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// replayFixtureConnector is a local channel: two pages of orders, the second
// of which times out once.
type replayFixtureConnector struct {
	failPage2 bool
}

func (c *replayFixtureConnector) Descriptor() ConnectorDescriptor {
	return ConnectorDescriptor{Code: "replay-fixture", Kind: "webstore", PullOrders: true}
}
func (c *replayFixtureConnector) ValidateCredentials(m map[string]string) error {
	if m["token"] == "" {
		return errors.New("token missing")
	}
	return nil
}
func (c *replayFixtureConnector) PullOrders(_ context.Context, _ map[string]string, req ConnectorPullRequest) (ConnectorOrderPage, error) {
	order := func(id, sku string) ConnectorOrder {
		return ConnectorOrder{ChannelOrderID: id, CustomerName: "Replay", ShippingAddress: "2 Replay Road, Pune 411001",
			PaymentStatus: "Confirmed", Lines: []ConnectorOrderLine{{ChannelSKU: sku, Quantity: 1, UnitPrice: 100}}}
	}
	switch req.Cursor {
	case "":
		return ConnectorOrderPage{Orders: []ConnectorOrder{order("RPL-1", "RPL-SKU"), order("RPL-2", "RPL-SKU")}, NextCursor: "p2"}, nil
	case "p2":
		if c.failPage2 {
			c.failPage2 = false
			return ConnectorOrderPage{}, errors.New("channel timed out")
		}
		return ConnectorOrderPage{Orders: []ConnectorOrder{order("RPL-3", "RPL-SKU")}}, nil
	}
	return ConnectorOrderPage{}, nil
}
func (c *replayFixtureConnector) PushInventory(context.Context, map[string]string, []ConnectorInventoryUpdate) error {
	return nil
}
func (c *replayFixtureConnector) PushOrderStatus(context.Context, map[string]string, ConnectorStatusUpdate) error {
	return nil
}
func (c *replayFixtureConnector) MapError(err error) string { return "" }
func (c *replayFixtureConnector) PublishProduct(context.Context, map[string]string, ChannelProductPayload) (string, error) {
	return "", nil
}
func (c *replayFixtureConnector) RateLimit() (int, time.Duration) { return 100, time.Minute }

// Stage 55.9: replaying a channel pull - after a mid-sync failure, and again
// from the start - imports every channel order exactly once.
func TestChannelPullReplayImportsEachOrderOnce(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	fixture := &replayFixtureConnector{failPage2: true}
	registerOmnichannelConnector(fixture)
	defer func() {
		omnichannelConnectorMu.Lock()
		delete(omnichannelConnectors, "replay-fixture")
		omnichannelConnectorMu.Unlock()
		delete(connectorRegistry, "replay-fixture")
	}()

	item, _ := json.Marshal(map[string]interface{}{"code": "RPL-SKU", "name": "Replay item"})
	db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('ITEM-RPL', 'Item', $1, 'Active', 'system')", item)
	db.DB.Exec("INSERT INTO " + schema + ".inventory_availability (sku, location_code, on_hand, available) VALUES ('RPL-SKU', 'HO', 50, 50)")
	channel, _ := json.Marshal(map[string]interface{}{"code": "RPL", "platform": "replay-fixture", "location_code": "HO"})
	db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('RPL', 'Channel', $1, 'Active', 'system')", channel)
	if err := SaveChannelCredential(tenantID, "RPL", map[string]string{"token": "local"}); err != nil {
		t.Fatalf("credential: %v", err)
	}
	orders := func() int {
		var n int
		db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'SalesOrder' AND data->>'channel' = 'RPL'").Scan(&n)
		return n
	}
	lastRun := func() string {
		var s string
		db.DB.QueryRow("SELECT status FROM " + schema + ".documents WHERE doctype = 'ChannelSyncRun' AND data->>'channel' = 'RPL' ORDER BY created_at DESC, id DESC LIMIT 1").Scan(&s)
		return s
	}

	processed, _, err := PullChannelOrders(context.Background(), tenantID, "RPL")
	if err == nil || processed != 2 || orders() != 2 || lastRun() != "Failed" {
		t.Fatalf("first pull should import page 1 then fail: processed=%d orders=%d run=%s err=%v", processed, orders(), lastRun(), err)
	}
	processed, _, err = PullChannelOrders(context.Background(), tenantID, "RPL")
	if err != nil || processed != 1 || orders() != 3 || lastRun() != "Success" {
		t.Fatalf("the retry resumes at the failed page: processed=%d orders=%d run=%s err=%v", processed, orders(), lastRun(), err)
	}
	if _, _, err := PullChannelOrders(context.Background(), tenantID, "RPL"); err != nil {
		t.Fatal(err)
	}
	if orders() != 3 {
		t.Fatalf("a full replay must not duplicate orders: %d", orders())
	}
	health, err := GetConnectorHealth(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range health {
		if h.Channel == "RPL" && (!h.CredentialsSet || h.Failures24h != 1 || h.Runs24h != 3) {
			t.Fatalf("health shows the replay history: %+v", h)
		}
	}
}

// Stage 55.0F: a deleted sync run or SKU exception no longer counts on the
// connector row, and a channel with no credentials reads as not connected.
func TestConnectorHealthSkipsDeletedRecords(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	ins := func(id, doctype, status string, data map[string]interface{}, deleted bool) {
		raw, _ := json.Marshal(data)
		del := "NULL"
		if deleted {
			del = "CURRENT_TIMESTAMP"
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, deleted_at) VALUES ($1, $2, $3, $4, 'system', "+del+")", id, doctype, raw, status); err != nil {
			t.Fatal(err)
		}
	}
	ins("CH-DEL", "Channel", "Active", map[string]interface{}{"code": "CH-DEL", "platform": "Shopify"}, false)
	ins("RUN-DEL", "ChannelSyncRun", "Failed", map[string]interface{}{"channel": "CH-DEL", "error": "boom"}, true)
	ins("EXC-DEL", "ChannelSKUException", "Open", map[string]interface{}{"channel": "CH-DEL"}, true)
	health, err := GetConnectorHealth(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range health {
		if h.Channel != "CH-DEL" {
			continue
		}
		if h.LastStatus != "" || h.Failures24h != 0 || h.OpenExceptions != 0 || h.CredentialsSet {
			t.Fatalf("deleted records must not count, and no credentials means not connected: %+v", h)
		}
		return
	}
	t.Fatal("channel CH-DEL missing from connector health")
}
