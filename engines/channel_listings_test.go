package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// A connector that can list, and records the stock it is sent. It stands in
// for a marketplace: no live channel is called by these tests.
type listingDouble146 struct {
	mu     *sync.Mutex
	listed *[]ListedProduct
	pushed *[][]ConnectorInventoryUpdate
}

func (listingDouble146) Descriptor() ConnectorDescriptor {
	return ConnectorDescriptor{Code: "Listing146Double", Kind: "marketplace", PushInventory: true}
}
func (listingDouble146) RateLimit() (int, time.Duration)             { return 100, time.Minute }
func (listingDouble146) ValidateCredentials(map[string]string) error { return nil }
func (listingDouble146) PublishProduct(context.Context, map[string]string, ChannelProductPayload) (string, error) {
	return "", nil
}
func (listingDouble146) PullOrders(context.Context, map[string]string, ConnectorPullRequest) (ConnectorOrderPage, error) {
	return ConnectorOrderPage{}, nil
}
func (c listingDouble146) PushInventory(_ context.Context, _ map[string]string, u []ConnectorInventoryUpdate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.pushed = append(*c.pushed, u)
	return nil
}
func (listingDouble146) PushOrderStatus(context.Context, map[string]string, ConnectorStatusUpdate) error {
	return nil
}
func (listingDouble146) MapError(err error) string { return DefaultConnectorErrorCode(err) }
func (c listingDouble146) ListListings(context.Context, map[string]string) ([]ListedProduct, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ListedProduct{}, *c.listed...), nil
}

func stage14645Insert(t *testing.T, schema, id, doctype string, data map[string]interface{}) {
	t.Helper()
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
		t.Fatal(err)
	}
}

func stage14645Ready(t *testing.T) string {
	t.Helper()
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var ok bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'channel_listings')`, schema).Scan(&ok)
	if !ok {
		t.Skip("needs migrations_stage146_4_5_channel_listings.sql - run erp-server -migrate on this database")
	}
	return schema
}

func TestStage14645ListingsPullLinkAndStockSwitch(t *testing.T) {
	schema := stage14645Ready(t)
	const ch, store = "CH-14645", "ST-14645"
	items := []string{"S14645-RED", "S14645-BLUE", "S14645-GREEN", "S14645-DUPA", "S14645-DUPB", "S14645-TWIN"}
	clean := func() {
		db.DB.Exec("DELETE FROM "+schema+".channel_listings WHERE channel = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".channel_product_mapping WHERE channel = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".channel_credentials WHERE channel_code = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE location_code = $1", store)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = ANY($1) OR id IN ($2, $3) OR (doctype = 'ChannelSyncRun' AND data->>'channel' = $3)", "{"+strings.Join(items, ",")+"}", store, ch)
	}
	clean()
	defer clean()
	stage14645Insert(t, schema, store, "Location", map[string]interface{}{"code": store, "name": "Store 14645", "type": "Store", "status": "Active"})
	stage14645Insert(t, schema, ch, "Channel", map[string]interface{}{"code": ch, "name": "Test marketplace", "platform": "Listing146Double", "location_code": store, "sync_interval_minutes": 15})
	bc := map[string]string{"S14645-BLUE": "BC-BLUE-45", "S14645-GREEN": "BC-GREEN-45", "S14645-DUPA": "BC-DUPE-45", "S14645-DUPB": "BC-DUPE-45", "S14645-TWIN": "BC-TWIN-45"}
	for _, id := range items {
		stage14645Insert(t, schema, id, "Item", map[string]interface{}{"code": id, "name": id + " name", "barcode": bc[id], "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", id, store); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveChannelCredential("default", ch, map[string]string{"token": "t"}); err != nil {
		t.Fatal(err)
	}
	listed := []ListedProduct{
		{Ref: "L1", ChannelSKU: "S14645-RED", Title: "Red"},
		{Ref: "L2", ChannelSKU: "MKT-2", Barcode: "BC-BLUE-45", Title: "Blue"},
		{Ref: "L3", ChannelSKU: "S14645-GREEN", Barcode: "BC-DUPE-45", Title: "Green"},
		{Ref: "L4", ChannelSKU: "MKT-4", Barcode: "BC-DUPE-45"},
		{Ref: "L5", ChannelSKU: "NOPE-1", Title: "Nobody"},
		{Ref: "L6", ChannelSKU: "MKT-6", Barcode: "BC-TWIN-45"},
		{Ref: "L7", ChannelSKU: "MKT-7", Barcode: "BC-TWIN-45"},
	}
	var pushed [][]ConnectorInventoryUpdate
	registerOmnichannelConnector(listingDouble146{mu: &sync.Mutex{}, listed: &listed, pushed: &pushed})
	ctx := context.Background()

	// Nothing is stored until someone pulls, and the stock push is as before.
	if l, _ := ListChannelListings("default", ch, ""); len(l) != 0 {
		t.Fatalf("nothing pulled yet: %v", l)
	}
	// A channel whose connector cannot list is refused, never faked.
	if ChannelCanListProducts("Amazon") || ChannelCanListProducts("Flipkart") || !ChannelCanListProducts("Shopify") || !ChannelCanListProducts("Listing146Double") {
		t.Errorf("only a connector with a listing read can list: Amazon=%v Flipkart=%v Shopify=%v", ChannelCanListProducts("Amazon"), ChannelCanListProducts("Flipkart"), ChannelCanListProducts("Shopify"))
	}

	res, err := PullChannelListings(ctx, "default", ch, "tester")
	if err != nil || res.Listed != 7 || res.New != 7 || res.Gone != 0 {
		t.Fatalf("pull: %+v %v", res, err)
	}
	// A preview writes nothing; it names matches, conflicts and unlinked.
	pv, err := PreviewListingLinks("default", ch)
	if err != nil || pv.Match != 2 || pv.Conflict != 4 || pv.Unlinked != 1 {
		t.Fatalf("preview counts: %+v %v", pv, err)
	}
	by := map[string]ListingLinkRow{}
	for _, r := range pv.Rows {
		by[r.Ref] = r
	}
	if r := by["L1"]; r.Result != "Match" || r.Item != "S14645-RED" || r.MatchedBy != "SKU" {
		t.Errorf("L1 matches by SKU: %+v", r)
	}
	if r := by["L2"]; r.Result != "Match" || r.Item != "S14645-BLUE" || r.MatchedBy != "Barcode" {
		t.Errorf("L2 matches by barcode: %+v", r)
	}
	for _, ref := range []string{"L3", "L4", "L6", "L7"} {
		if by[ref].Result != "Conflict" || by[ref].Reason == "" {
			t.Errorf("%s is a conflict with a reason: %+v", ref, by[ref])
		}
	}
	if by["L5"].Result != "Unlinked" {
		t.Errorf("L5 matches nothing: %+v", by["L5"])
	}
	var maps int
	_ = db.DB.QueryRow("SELECT count(*) FROM "+schema+".channel_product_mapping WHERE channel = $1", ch).Scan(&maps)
	if maps != 0 {
		t.Fatalf("a preview writes nothing: %d mappings", maps)
	}
	// The commit links the matches only; the rest stay as they were.
	linked, skipped, err := CommitListingLinks("default", ch, "tester", nil)
	if err != nil || linked != 2 || skipped != 5 {
		t.Fatalf("commit: %d linked, %d skipped, %v", linked, skipped, err)
	}
	if again, _, _ := CommitListingLinks("default", ch, "tester", nil); again != 0 {
		t.Errorf("a second commit links nothing new: %d", again)
	}
	ms, _ := ListChannelSKUMappings("default", ch)
	if len(ms) != 2 {
		t.Fatalf("two mappings: %v", ms)
	}
	for _, m := range ms {
		if m.SKU == "S14645-BLUE" && (m.ChannelSKU != "MKT-2" || m.ExternalProductID != "L2") {
			t.Errorf("the mapping carries the channel's own SKU and listing: %+v", m)
		}
	}

	// Default is "syncs": both linked items go out.
	skus := func() map[string]int {
		m := map[string]int{}
		if n := len(pushed); n > 0 {
			for _, u := range pushed[n-1] {
				m[u.ChannelSKU] = u.Quantity
			}
		}
		return m
	}
	if _, _, err := SyncChannelInventory(ctx, "default", ch); err != nil {
		t.Fatal(err)
	}
	if got := skus(); len(got) != 2 || got["S14645-RED"] != 10 || got["MKT-2"] != 10 {
		t.Fatalf("by default both listings sync: %v", got)
	}
	// Switch one off: the push leaves it out, the other still goes.
	if err := SetListingStockSync("default", ch, "L1", false, "tester"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SyncChannelInventory(ctx, "default", ch); err != nil {
		t.Fatal(err)
	}
	if got := skus(); len(got) != 1 || got["MKT-2"] != 10 {
		t.Fatalf("a listing switched off is not pushed: %v", got)
	}
	if off, _ := ListChannelListings("default", ch, "off"); len(off) != 1 || off[0].SKU != "S14645-RED" || off[0].StockSync {
		t.Errorf("the off filter lists it: %v", off)
	}
	if err := SetListingStockSync("default", ch, "L1", true, "tester"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SyncChannelInventory(ctx, "default", ch); err != nil {
		t.Fatal(err)
	}
	if got := skus(); len(got) != 2 {
		t.Fatalf("switched back on, it goes again: %v", got)
	}
	if err := SetListingStockSync("default", ch, "NO-SUCH", false, "tester"); err == nil {
		t.Errorf("an unknown listing is refused")
	} else if ve, ok := err.(*ValidationError); !ok || ve.Code != "INTEGR-4801" {
		t.Errorf("the refusal carries its code: %v", err)
	}

	// A second pull keeps links and switches; a listing the channel dropped is Gone.
	_ = SetListingStockSync("default", ch, "L2", false, "tester")
	listed = listed[:4]
	res, err = PullChannelListings(ctx, "default", ch, "tester")
	if err != nil || res.Listed != 4 || res.New != 0 || res.Gone != 3 {
		t.Fatalf("second pull: %+v %v", res, err)
	}
	all, _ := ListChannelListings("default", ch, "linked")
	if len(all) != 2 {
		t.Fatalf("links survive a pull: %v", all)
	}
	for _, l := range all {
		if l.Ref == "L2" && l.StockSync {
			t.Errorf("the switch survives a pull: %+v", l)
		}
	}
}

func TestStage14645ChannelThatCannotListIsRefused(t *testing.T) {
	schema := stage14645Ready(t)
	const ch = "CH-14645-AMZ"
	clean := func() {
		db.DB.Exec("DELETE FROM "+schema+".channel_credentials WHERE channel_code = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", ch)
	}
	clean()
	defer clean()
	stage14645Insert(t, schema, ch, "Channel", map[string]interface{}{"code": ch, "platform": "Amazon", "sync_interval_minutes": 15})
	_ = SaveChannelCredential("default", ch, map[string]string{"seller_id": "x"})
	_, err := PullChannelListings(context.Background(), "default", ch, "tester")
	ve, ok := err.(*ValidationError)
	if !ok || ve.Code != "INTEGR-4800" {
		t.Fatalf("Amazon cannot list: %v", err)
	}
	if l, _ := ListChannelListings("default", ch, ""); len(l) != 0 {
		t.Errorf("nothing is invented: %v", l)
	}
}

func TestStage14645ShopifyListingsFeedTheShopifyLinksAndStockSwitch(t *testing.T) {
	schema := stage14645Ready(t)
	_, cred := withFakeShopify(t)
	const ch, store, sku = "SHOP-14645", "ST-14645S", "KURTA-RED-M"
	clean := func() {
		db.DB.Exec("DELETE FROM "+schema+".channel_listings WHERE channel = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".shopify_links WHERE channel = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".channel_product_mapping WHERE channel = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".channel_credentials WHERE channel_code = $1", ch)
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE location_code = $1", store)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2, $3) OR (doctype = 'ChannelSyncRun' AND data->>'channel' = $3)", store, sku, ch)
	}
	clean()
	defer clean()
	ctx := context.Background()
	stage14645Insert(t, schema, store, "Location", map[string]interface{}{"code": store, "name": "Web store", "type": "Store", "status": "Active"})
	stage14645Insert(t, schema, ch, "Channel", map[string]interface{}{"code": ch, "platform": "Shopify", "location_code": store, "sync_interval_minutes": 15})
	stage14645Insert(t, schema, sku, "Item", map[string]interface{}{"code": sku, "name": "Kurta red M", "barcode": "890000000001", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", sku, store); err != nil {
		t.Fatal(err)
	}
	if err := SaveChannelCredential("default", ch, map[string]string{"shop_domain": cred["shop_domain"], "access_token": cred["access_token"], "webhook_secret": "w"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveShopifySettings("default", ch, ShopifySettings{Locations: map[string]string{"gid://shopify/Location/1": store}}, "tester"); err != nil {
		t.Fatal(err)
	}
	res, err := PullChannelListings(ctx, "default", ch, "tester")
	if err != nil || res.Listed != 1 {
		t.Fatalf("Shopify lists: %+v %v", res, err)
	}
	if l, _ := ListChannelListings("default", ch, ""); len(l) != 1 || l[0].ChannelSKU != sku || l[0].SKU != "" {
		t.Fatalf("pulled, not linked yet: %v", l)
	}
	if n, err := PushShopifyStock(ctx, "default", ch, true, nil); err != nil || n != 0 {
		t.Fatalf("an unlinked listing is not pushed: %d %v", n, err)
	}
	if linked, _, err := CommitListingLinks("default", ch, "tester", nil); err != nil || linked != 1 {
		t.Fatalf("commit: %d %v", linked, err)
	}
	links, _ := ListShopifyLinks("default", ch)
	if len(links) != 1 || links[0].SKU != sku || links[0].InventoryItemID != "gid://shopify/InventoryItem/41" {
		t.Fatalf("the Shopify link carries the variant: %v", links)
	}
	if n, err := PushShopifyStock(ctx, "default", ch, true, nil); err != nil || n != 1 {
		t.Fatalf("linked, it goes by default: %d %v", n, err)
	}
	if err := SetListingStockSync("default", ch, "gid://shopify/ProductVariant/31", false, "tester"); err != nil {
		t.Fatal(err)
	}
	if n, err := PushShopifyStock(ctx, "default", ch, true, nil); err != nil || n != 0 {
		t.Fatalf("switched off, the Shopify push leaves it out: %d %v", n, err)
	}
	if n, err := PushShopifyStock(ctx, "default", ch, true, []string{sku}); err != nil || n != 0 {
		t.Fatalf("even a send-now for that item: %d %v", n, err)
	}
	_ = SetListingStockSync("default", ch, "gid://shopify/ProductVariant/31", true, "tester")
	if n, err := PushShopifyStock(ctx, "default", ch, true, nil); err != nil || n != 1 {
		t.Fatalf("on again, it goes: %d %v", n, err)
	}
}
