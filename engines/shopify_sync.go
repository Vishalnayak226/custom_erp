package engines

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 99.2-99.7: what the ERP sends Shopify and when, and what it takes
// in. The GraphQL calls are connector_shopify_sdk.go; this file keeps the
// links (shopify_links), the channel's Shopify settings (on its Channel
// record, under "shopify"), and the flows:
//
//   products (99.3)  a design publishes as one product with its variants;
//                    publishing again updates it; an inactive item drops out,
//                    an inactive design is archived.
//   prices (99.3.3)  the channel's price list (else the item's sale price),
//                    less any automatic offer, MRP as compare-at - sent when
//                    it changes, each send logged.
//   stock (99.4)     per Shopify location, the mapped store's sellable stock
//                    less the channel buffer, never below 0 - sent in groups
//                    when it changes, and in full once a day (D3).
//   orders (99.5)    the whole order, imported once; a customer found by
//                    phone or created; edits and cancellations applied until
//                    picking starts, after that the Channel changes queue.
//   back (99.6)      shipping (with carrier and tracking, part shipments),
//                    cancellation and RTO go to Shopify through a job; a
//                    Shopify refund raises an ERP return.
//   payouts (99.7)   Shopify Payments payouts become settlement lines and are
//                    reconciled the marketplace way (fees to 5200).

const (
	shopifyActor         = "shopify"
	shopifyStatusJobType = "shopify.order_status"
)

func init() {
	RegisterJobHandler(shopifyStatusJobType, pushShopifyStatusJob)
}

// ShopifySettings are a Shopify channel's own settings.
type ShopifySettings struct {
	// Locations maps each Shopify location gid to the ERP store it is (D3).
	Locations    map[string]string `json:"locations"`
	PriceList    string            `json:"price_list"`
	SyncProducts bool              `json:"sync_products"`
	SyncPrices   bool              `json:"sync_prices"`
	SyncStock    bool              `json:"sync_stock"`
	SyncOrders   bool              `json:"sync_orders"`
	Shop         string            `json:"shop,omitempty"`
	WebhookURL   string            `json:"webhook_url,omitempty"`
	WebhooksAt   string            `json:"webhooks_registered_at,omitempty"`
	LastFull     string            `json:"last_full_stock_at,omitempty"`
	LastPayouts  string            `json:"last_payouts_at,omitempty"`
	LastPull     string            `json:"last_pull_at,omitempty"`
	// Buffer (D3) is saved on the Channel record's inventory_buffer; nil
	// leaves it as it is.
	Buffer *int `json:"inventory_buffer,omitempty"`
}

func shopifyError(msg string) error { return &ValidationError{Code: "CONN-0994", Message: msg} }

func defaultShopifySettings() ShopifySettings {
	return ShopifySettings{Locations: map[string]string{}, SyncProducts: true, SyncPrices: true, SyncStock: true, SyncOrders: true}
}

// LoadShopifyChannel reads a Shopify channel: its runtime config and its
// Shopify settings. A channel on another platform is refused.
func LoadShopifyChannel(tenantID, channel string) (ChannelRuntimeConfig, ShopifySettings, error) {
	cfg, err := LoadChannelRuntimeConfig(tenantID, channel)
	if err != nil {
		return cfg, ShopifySettings{}, shopifyError(fmt.Sprintf("channel %s is not set up (Setup ▸ PIM ▸ Channel)", channel))
	}
	if !strings.EqualFold(cfg.Platform, "Shopify") {
		return cfg, ShopifySettings{}, shopifyError(fmt.Sprintf("channel %s is not a Shopify channel (its platform is %s)", channel, cfg.Platform))
	}
	s := defaultShopifySettings()
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return cfg, s, err
	}
	var raw sql.NullString
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT data->'shopify' FROM %s.documents WHERE doctype = 'Channel' AND id = $1`, schema), channel).Scan(&raw)
	if raw.Valid && raw.String != "" && raw.String != "null" {
		_ = json.Unmarshal([]byte(raw.String), &s)
	}
	if s.Locations == nil {
		s.Locations = map[string]string{}
	}
	return cfg, s, nil
}

// SaveShopifySettings stores the settings the setup page edits (locations,
// price list, the sync switches); the bookkeeping fields are kept.
func SaveShopifySettings(tenantID, channel string, in ShopifySettings, actor string) error {
	_, cur, err := LoadShopifyChannel(tenantID, channel)
	if err != nil {
		return err
	}
	cur.PriceList = strings.TrimSpace(in.PriceList)
	cur.SyncProducts, cur.SyncPrices, cur.SyncStock, cur.SyncOrders = in.SyncProducts, in.SyncPrices, in.SyncStock, in.SyncOrders
	cur.Locations = map[string]string{}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	for loc, store := range in.Locations {
		store = strings.TrimSpace(store)
		if strings.TrimSpace(loc) == "" || store == "" {
			continue
		}
		var ok bool
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Location' AND id = $1 AND deleted_at IS NULL)`, schema), store).Scan(&ok)
		if !ok {
			return shopifyError(fmt.Sprintf("store %s does not exist", store))
		}
		cur.Locations[loc] = store
	}
	if in.Buffer != nil {
		if *in.Buffer < 0 || *in.Buffer > 100000 {
			return shopifyError("the safety buffer is 0 or more units")
		}
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = jsonb_set(data, '{inventory_buffer}', to_jsonb($2::int)) WHERE doctype = 'Channel' AND id = $1`, schema), channel, *in.Buffer); err != nil {
			return err
		}
	}
	cur.Buffer = nil
	if err := saveShopifySettings(tenantID, channel, cur); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "INTEGRATION", "SHOPIFY_SETTINGS_SAVED", fmt.Sprintf("channel %s: %d location(s), price list %q", channel, len(cur.Locations), cur.PriceList))
	return nil
}

func saveShopifySettings(tenantID, channel string, s ShopifySettings) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(s)
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = jsonb_set(data, '{shopify}', $2::jsonb), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'Channel' AND id = $1`, schema), channel, string(raw))
	return err
}

func shopifyConn() shopifySDKConnector { return shopifySDKConnector{} }

func shopifyCred(tenantID, channel string) (map[string]string, error) {
	cred, err := LoadConnectorCredential(tenantID, channel, "Shopify")
	if err != nil {
		return nil, shopifyError("the Shopify shop address and token are not saved yet: " + err.Error())
	}
	return cred, nil
}

// ShopifyChannels are the client's active Shopify channels.
func ShopifyChannels(tenantID string) []string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Channel' AND deleted_at IS NULL AND status = 'Active'
		AND lower(COALESCE(data->>'platform', '')) = 'shopify' ORDER BY id`, schema))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

func isShopifyChannel(tenantID, channel string) bool {
	if channel == "" {
		return false
	}
	for _, c := range ShopifyChannels(tenantID) {
		if strings.EqualFold(c, channel) {
			return true
		}
	}
	return false
}

// --- links -----------------------------------------------------------------------

// ShopifyLink is one ERP item's place in Shopify.
type ShopifyLink struct {
	SKU             string         `json:"sku"`
	Name            string         `json:"name"`
	Design          string         `json:"design"`
	ProductGID      string         `json:"product_gid"`
	VariantGID      string         `json:"variant_gid"`
	InventoryItemID string         `json:"inventory_item_id"`
	LastPrice       float64        `json:"last_price"`
	LastCompareAt   float64        `json:"last_compare_at"`
	LastStock       map[string]int `json:"last_stock"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func upsertShopifyLink(schema, channel, design string, v ShopifyVariantLink) error {
	_, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.shopify_links (channel, sku, design, product_gid, variant_gid, inventory_item_id, last_price, last_compare_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP)
		ON CONFLICT (channel, sku) DO UPDATE SET design = CASE WHEN EXCLUDED.design <> '' THEN EXCLUDED.design ELSE %s.shopify_links.design END,
		  product_gid = EXCLUDED.product_gid, variant_gid = EXCLUDED.variant_gid, inventory_item_id = EXCLUDED.inventory_item_id,
		  last_price = EXCLUDED.last_price, last_compare_at = EXCLUDED.last_compare_at, updated_at = CURRENT_TIMESTAMP`, schema, schema),
		channel, v.SKU, design, v.ProductGID, v.VariantGID, v.InventoryItemID, round2(v.Price), round2(v.CompareAt))
	return err
}

// ListShopifyLinks is the mapping list (99.3.2), item names included.
func ListShopifyLinks(tenantID, channel string) ([]ShopifyLink, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT l.sku, COALESCE(i.data->>'name', ''), l.design, l.product_gid, l.variant_gid, l.inventory_item_id,
		COALESCE(l.last_price, 0), COALESCE(l.last_compare_at, 0), l.last_stock::text, l.updated_at
		FROM %s.shopify_links l LEFT JOIN %s.documents i ON i.doctype = 'Item' AND i.id = l.sku
		WHERE l.channel = $1 ORDER BY l.design, l.sku`, schema, schema), channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShopifyLink{}
	for rows.Next() {
		var l ShopifyLink
		var stock string
		if err := rows.Scan(&l.SKU, &l.Name, &l.Design, &l.ProductGID, &l.VariantGID, &l.InventoryItemID, &l.LastPrice, &l.LastCompareAt, &stock, &l.UpdatedAt); err != nil {
			return nil, err
		}
		l.LastStock = map[string]int{}
		_ = json.Unmarshal([]byte(stock), &l.LastStock)
		out = append(out, l)
	}
	return out, rows.Err()
}

// --- 99.3 products and prices ---------------------------------------------------------

// shopifyItemPrice is what Shopify sells an item at: the price list's price
// (else the item's sale price, else its MRP) less any automatic offer, with
// the MRP as the compare-at price.
func shopifyItemPrice(tenantID, priceList string, item map[string]interface{}, sku string) (price, compareAt float64) {
	mrp := numFromInterface(item["mrp"])
	price = numFromInterface(item["sale_price"])
	if priceList != "" {
		if p, ok, err := ResolvePriceForSKU(tenantID, priceList, sku, BusinessToday()); err == nil && ok && p > 0 {
			price = p
		}
	}
	if price <= 0 {
		price = mrp
	}
	if price > 0 {
		if ev, err := EvaluatePOSOffers(tenantID, OfferEvaluationInput{Lines: []OfferCartLine{{Sku: sku, Qty: 1, SalePrice: price}}}); err == nil && ev != nil && len(ev.LineDiscounts) > 0 && ev.LineDiscounts[0] > 0 {
			price = price - ev.LineDiscounts[0]
		}
	}
	price = round2(price)
	if mrp > price {
		compareAt = round2(mrp)
	}
	return price, compareAt
}

func itemActive(item map[string]interface{}) bool {
	st := strings.ToLower(strField(item, "status"))
	return st == "" || st == "active"
}

// shopifyOptionOrder puts a design's option names in the client's attribute
// order (the design matrix), the rest by name.
func shopifyOptionOrder(tenantID string, combos []DesignCombination) []string {
	seen := map[string]bool{}
	var names []string
	for _, c := range combos {
		for k := range c.Options {
			if !seen[k] {
				seen[k] = true
				names = append(names, k)
			}
		}
	}
	rank := map[string]int{}
	if m, err := GetDesignMatrix(tenantID); err == nil {
		for i, a := range m.Attributes {
			rank[strings.ToLower(a.Code)], rank[strings.ToLower(a.Label)] = i+1, i+1
		}
	}
	sort.SliceStable(names, func(i, j int) bool {
		ri, rj := rank[strings.ToLower(names[i])], rank[strings.ToLower(names[j])]
		if ri == 0 {
			ri = 1 << 20
		}
		if rj == 0 {
			rj = 1 << 20
		}
		if ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	return names
}

// shopifyImages are a design's pictures then each variant's main one (by
// sku), at most 10 and 5 MB each - the Stage 95 images.
func shopifyImages(tenantID, design string, skus []string) ([]ChannelImage, map[int]string) {
	var imgs []ChannelImage
	forSKU := map[int]string{}
	add := func(owner, sku string, first bool) {
		list, err := ListOwnerImages(tenantID, owner)
		if err != nil {
			return
		}
		for _, m := range list {
			if len(imgs) >= 10 {
				return
			}
			b, ft, name, err := ReadMediaFile(tenantID, m.ID)
			if err != nil || len(b) == 0 || len(b) > 5<<20 {
				continue
			}
			mime := ft
			if !strings.Contains(mime, "/") {
				mime = "image/" + strings.TrimPrefix(strings.ToLower(ft), ".")
			}
			if sku != "" {
				forSKU[len(imgs)] = sku
			}
			imgs = append(imgs, ChannelImage{Filename: firstNonEmpty(name, m.ID+".jpg"), MIMEType: mime, Bytes: b})
			if first {
				return
			}
		}
	}
	if design != "" {
		add(design, "", false)
	}
	for _, s := range skus {
		add(s, s, true)
	}
	return imgs, forSKU
}

// ShopifyPublishResult is what a publish did.
type ShopifyPublishResult struct {
	Products int      `json:"products"`
	Variants int      `json:"variants"`
	Archived int      `json:"archived"`
	Errors   []string `json:"errors"`
	Left     int      `json:"left"` // not yet published, for the next click
}

const shopifyPublishBatch = 50

// PublishToShopify publishes designs (codes may name designs or single
// items) - or, with none named, up to 50 designs and items not on Shopify
// yet, so a big catalogue goes in a few clicks.
func PublishToShopify(ctx context.Context, tenantID, channel string, codes []string, actor string) (ShopifyPublishResult, error) {
	res := ShopifyPublishResult{Errors: []string{}}
	_, s, err := LoadShopifyChannel(tenantID, channel)
	if err != nil {
		return res, err
	}
	cred, err := shopifyCred(tenantID, channel)
	if err != nil {
		return res, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	if len(codes) == 0 {
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT code FROM (
			  SELECT id AS code FROM %[1]s.documents WHERE doctype = 'ProductFamily' AND deleted_at IS NULL AND COALESCE(status, 'Active') = 'Active'
			    AND NOT EXISTS (SELECT 1 FROM %[1]s.shopify_links l WHERE l.channel = $1 AND l.design = documents.id)
			  UNION ALL
			  SELECT id FROM %[1]s.documents WHERE doctype = 'Item' AND deleted_at IS NULL AND COALESCE(status, 'Active') = 'Active'
			    AND COALESCE(data->>'family', '') = '' AND COALESCE(data->>'parent_product_code', '') = ''
			    AND NOT EXISTS (SELECT 1 FROM %[1]s.shopify_links l WHERE l.channel = $1 AND l.sku = documents.id)
			) x ORDER BY code`, schema), channel)
		if err != nil {
			return res, err
		}
		for rows.Next() {
			var c string
			if rows.Scan(&c) == nil {
				codes = append(codes, c)
			}
		}
		rows.Close()
		if len(codes) > shopifyPublishBatch {
			res.Left = len(codes) - shopifyPublishBatch
			codes = codes[:shopifyPublishBatch]
		}
	}
	started := time.Now()
	for _, code := range codes {
		if ctx.Err() != nil {
			break
		}
		n, archived, err := publishShopifyOne(ctx, tenantID, schema, channel, s, cred, code)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", code, err))
			continue
		}
		res.Products++
		res.Variants += n
		if archived {
			res.Archived++
		}
	}
	var runErr error
	if len(res.Errors) > 0 {
		runErr = fmt.Errorf("%s", strings.Join(res.Errors, "; "))
	}
	status := "Success"
	if runErr != nil {
		status = "Failed"
	}
	recordChannelSyncRun(tenantID, channel, "Product Publish", status, res.Products, len(res.Errors), started, runErr)
	LogAuditEvent(tenantID, actor, "INTEGRATION", "SHOPIFY_PUBLISHED", fmt.Sprintf("channel %s: %d product(s), %d variant(s), %d archived, %d failed", channel, res.Products, res.Variants, res.Archived, len(res.Errors)))
	return res, nil
}

// publishShopifyOne sends one design (or one single item) and stores its
// links.
func publishShopifyOne(ctx context.Context, tenantID, schema, channel string, s ShopifySettings, cred map[string]string, code string) (int, bool, error) {
	conn := shopifyConn()
	in := ShopifyProductInput{}
	var combos []DesignCombination
	design := ""
	if d, err := GetDesign(tenantID, code); err == nil && len(d.Combinations) > 0 {
		design = d.Code
		combos = d.Combinations
		in.Title, in.HSN = d.Name, d.HSNCode
		fam, _, _ := fetchDocData(tenantID, "ProductFamily", code)
		in.Description = strField(fam, "description")
		in.Vendor = firstNonEmpty(strField(fam, "brand"), d.Attributes["brand"], d.Attributes["Brand"])
		in.Archived = !itemActive(fam)
		in.OptionOrder = shopifyOptionOrder(tenantID, combos)
	} else {
		item, _, err := fetchDocData(tenantID, "Item", code)
		if err != nil || item == nil {
			return 0, false, fmt.Errorf("no design or item %s", code)
		}
		combos = []DesignCombination{{SKU: code, Name: strField(item, "name"), Barcode: strField(item, "barcode"), Options: map[string]string{}}}
		in.Title, in.HSN, in.Vendor, in.Description = strField(item, "name"), strField(item, "hsn_code"), strField(item, "brand"), strField(item, "description")
		in.Archived = !itemActive(item)
	}
	// The linked product, so publishing again updates it (99.3.2).
	linked := map[string]string{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT sku, product_gid, variant_gid FROM %s.shopify_links WHERE channel = $1 AND (design = $2 OR sku = $2)`, schema), channel, code)
	if err == nil {
		for rows.Next() {
			var sku, p, v string
			if rows.Scan(&sku, &p, &v) == nil {
				in.ProductGID = firstNonEmpty(in.ProductGID, p)
				linked[sku] = v
			}
		}
		rows.Close()
	}
	var skus []string
	for _, c := range combos {
		item, _, _ := fetchDocData(tenantID, "Item", c.SKU)
		if item == nil || !itemActive(item) {
			continue // an inactive item drops out of the product
		}
		price, compare := shopifyItemPrice(tenantID, s.PriceList, item, c.SKU)
		in.Variants = append(in.Variants, ShopifyVariantInput{SKU: c.SKU, Barcode: firstNonEmpty(c.Barcode, strField(item, "barcode")), Price: price, CompareAt: compare,
			WeightGram: numFromInterface(item["weight"]), Options: c.Options, VariantGID: linked[c.SKU]})
		skus = append(skus, c.SKU)
		if in.HSN == "" {
			in.HSN = strField(item, "hsn_code")
		}
		if in.Vendor == "" {
			in.Vendor = strField(item, "brand")
		}
	}
	if len(in.Variants) == 0 {
		if in.ProductGID == "" {
			return 0, false, fmt.Errorf("none of its items is active")
		}
		in.Archived = true // every item inactive: keep the product, archived
		for sku, v := range linked {
			in.Variants = append(in.Variants, ShopifyVariantInput{SKU: sku, VariantGID: v, Price: 0.01})
		}
	}
	firstPublish := in.ProductGID == ""
	productGID, links, err := conn.PublishShopifyProduct(ctx, cred, in)
	if err != nil {
		return 0, false, err
	}
	sent := map[string]ShopifyVariantInput{}
	for _, v := range in.Variants {
		sent[v.SKU] = v
	}
	for _, l := range links {
		// What was sent is what the next price check compares with.
		if v, ok := sent[l.SKU]; ok {
			l.Price, l.CompareAt = v.Price, v.CompareAt
		}
		if err := upsertShopifyLink(schema, channel, design, l); err != nil {
			return 0, false, err
		}
		_ = UpsertChannelSKUMapping(tenantID, ChannelSKUMapping{SKU: l.SKU, Channel: channel, ChannelSKU: l.SKU, ExternalProductID: l.VariantGID})
	}
	// Pictures go up with the first publish only, so they are never doubled.
	if firstPublish {
		imgs, forSKU := shopifyImages(tenantID, design, skus)
		if ids, err := conn.UploadProductImages(ctx, cred, productGID, imgs); err == nil {
			media := map[string]string{}
			for i, id := range ids {
				if sku, ok := forSKU[i]; ok && id != "" {
					for _, l := range links {
						if l.SKU == sku {
							media[l.VariantGID] = id
						}
					}
				}
			}
			if err := conn.AttachVariantImages(ctx, cred, productGID, media); err != nil {
				LogSystemError(tenantID, "", "WARN", "shopify.publish", fmt.Sprintf("product %s: variant pictures not attached yet: %v", productGID, err), "")
			}
		} else if len(imgs) > 0 {
			LogSystemError(tenantID, "", "WARN", "shopify.publish", fmt.Sprintf("product %s: pictures not uploaded: %v", productGID, err), "")
		}
	}
	return len(links), in.Archived, nil
}

// LinkShopifyBySKU links existing Shopify variants to ERP items by SKU, else
// barcode (99.3.2), so a shop that already sells them is not duplicated.
func LinkShopifyBySKU(ctx context.Context, tenantID, channel, actor string) (linked, unmatched int, err error) {
	if _, _, err = LoadShopifyChannel(tenantID, channel); err != nil {
		return 0, 0, err
	}
	cred, err := shopifyCred(tenantID, channel)
	if err != nil {
		return 0, 0, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}
	variants, err := shopifyConn().ListVariants(ctx, cred)
	if err != nil {
		return 0, 0, err
	}
	for _, v := range variants {
		var sku, design string
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(NULLIF(data->>'family', ''), data->>'parent_product_code', '') FROM %s.documents
			WHERE doctype = 'Item' AND deleted_at IS NULL AND (id = $1 OR ($2 <> '' AND data->>'barcode' = $2)) ORDER BY (id = $1) DESC LIMIT 1`, schema), v.SKU, v.Barcode).Scan(&sku, &design)
		if err != nil {
			unmatched++
			continue
		}
		v.SKU = sku
		if err := upsertShopifyLink(schema, channel, design, v); err != nil {
			return linked, unmatched, err
		}
		_ = UpsertChannelSKUMapping(tenantID, ChannelSKUMapping{SKU: sku, Channel: channel, ChannelSKU: sku, ExternalProductID: v.VariantGID})
		linked++
	}
	LogAuditEvent(tenantID, actor, "INTEGRATION", "SHOPIFY_LINKED", fmt.Sprintf("channel %s: %d linked by SKU or barcode, %d not in the ERP", channel, linked, unmatched))
	return linked, unmatched, nil
}

// PushShopifyPrices sends the prices that changed since the last send (all
// of them with force), grouped by product, each send logged (99.3.3).
func PushShopifyPrices(ctx context.Context, tenantID, channel string, force bool) (sent int, err error) {
	started := time.Now()
	defer func() {
		st := "Success"
		if err != nil {
			st = "Failed"
		}
		recordChannelSyncRun(tenantID, channel, "Price Push", st, sent, 0, started, err)
	}()
	_, s, err := LoadShopifyChannel(tenantID, channel)
	if err != nil {
		return 0, err
	}
	cred, err := shopifyCred(tenantID, channel)
	if err != nil {
		return 0, err
	}
	links, err := ListShopifyLinks(tenantID, channel)
	if err != nil {
		return 0, err
	}
	schema, _ := db.GetTenantSchema(tenantID)
	byProduct := map[string][]ShopifyVariantLink{}
	for _, l := range links {
		item, _, _ := fetchDocData(tenantID, "Item", l.SKU)
		if item == nil || !itemActive(item) {
			continue
		}
		price, compare := shopifyItemPrice(tenantID, s.PriceList, item, l.SKU)
		if price <= 0 || (!force && price == l.LastPrice && compare == l.LastCompareAt) {
			continue
		}
		byProduct[l.ProductGID] = append(byProduct[l.ProductGID], ShopifyVariantLink{SKU: l.SKU, VariantGID: l.VariantGID, Price: price, CompareAt: compare})
	}
	conn := shopifyConn()
	for product, vs := range byProduct {
		if err := conn.PushVariantPrices(ctx, cred, product, vs); err != nil {
			return sent, err
		}
		parts := make([]string, len(vs))
		for i, v := range vs {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.shopify_links SET last_price = $3, last_compare_at = $4, updated_at = CURRENT_TIMESTAMP WHERE channel = $1 AND sku = $2`, schema),
				channel, v.SKU, v.Price, v.CompareAt)
			parts[i] = fmt.Sprintf("%s %.2f (MRP %.2f)", v.SKU, v.Price, v.CompareAt)
		}
		sent += len(vs)
		LogAuditEvent(tenantID, shopifyActor, "INTEGRATION", "SHOPIFY_PRICES_SENT", fmt.Sprintf("channel %s: %s", channel, strings.Join(parts, ", ")))
	}
	return sent, nil
}

// --- 99.4 stock ------------------------------------------------------------------------

// shopifyStockFor is what Shopify should show at each mapped location: the
// store's sellable stock less the channel buffer, never below 0.
func shopifyStockFor(tenantID, sku string, s ShopifySettings, buffer int) map[string]int {
	out := map[string]int{}
	for loc, store := range s.Locations {
		q, err := ComputeSellableSKUATS(tenantID, sku, store)
		if err != nil {
			continue
		}
		q -= buffer
		if q < 0 {
			q = 0
		}
		out[loc] = q
	}
	return out
}

// PushShopifyStock sends stock that changed since the last send, grouped
// (every quantity with full, as the nightly run does; only skus when given,
// as "Send now" does).
func PushShopifyStock(ctx context.Context, tenantID, channel string, full bool, skus []string) (sent int, err error) {
	started := time.Now()
	defer func() {
		st := "Success"
		if err != nil {
			st = "Failed"
		}
		recordChannelSyncRun(tenantID, channel, "Inventory Push", st, sent, 0, started, err)
	}()
	cfg, s, err := LoadShopifyChannel(tenantID, channel)
	if err != nil {
		return 0, err
	}
	if len(s.Locations) == 0 {
		return 0, shopifyError("map at least one Shopify location to a store first")
	}
	cred, err := shopifyCred(tenantID, channel)
	if err != nil {
		return 0, err
	}
	links, err := ListShopifyLinks(tenantID, channel)
	if err != nil {
		return 0, err
	}
	only := map[string]bool{}
	for _, k := range skus {
		only[k] = true
	}
	schema, _ := db.GetTenantSchema(tenantID)
	var updates []ConnectorInventoryUpdate
	newStock := map[string]map[string]int{}
	for _, l := range links {
		if l.InventoryItemID == "" || (len(only) > 0 && !only[l.SKU]) {
			continue
		}
		want := shopifyStockFor(tenantID, l.SKU, s, cfg.InventoryBuffer)
		changed := false
		for loc, q := range want {
			if last, ok := l.LastStock[loc]; full || len(only) > 0 || !ok || last != q {
				updates = append(updates, ConnectorInventoryUpdate{ChannelSKU: l.SKU, ProductID: l.InventoryItemID, LocationCode: loc, Quantity: q})
				changed = true
			}
		}
		if changed {
			newStock[l.SKU] = want
		}
	}
	if len(updates) == 0 {
		return 0, nil
	}
	if err := shopifyConn().PushInventory(ctx, cred, updates); err != nil {
		return 0, err
	}
	for sku, st := range newStock {
		raw, _ := json.Marshal(st)
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.shopify_links SET last_stock = $3::jsonb, updated_at = CURRENT_TIMESTAMP WHERE channel = $1 AND sku = $2`, schema), channel, sku, string(raw))
	}
	if full {
		s.LastFull = time.Now().UTC().Format(time.RFC3339)
		_ = saveShopifySettings(tenantID, channel, s)
	}
	return len(updates), nil
}

// ShopifyMismatch is one item at one location where Shopify and the ERP
// disagree (99.4.2).
type ShopifyMismatch struct {
	SKU      string `json:"sku"`
	Name     string `json:"name"`
	Location string `json:"location"` // Shopify location gid
	Store    string `json:"store"`
	ERP      int    `json:"erp"`
	Shopify  int    `json:"shopify"`
}

// ShopifyStockMismatch reads what Shopify shows and compares it with what
// the ERP would send now.
func ShopifyStockMismatch(ctx context.Context, tenantID, channel string) ([]ShopifyMismatch, error) {
	cfg, s, err := LoadShopifyChannel(tenantID, channel)
	if err != nil {
		return nil, err
	}
	cred, err := shopifyCred(tenantID, channel)
	if err != nil {
		return nil, err
	}
	links, err := ListShopifyLinks(tenantID, channel)
	if err != nil {
		return nil, err
	}
	var ids []string
	bySKU := map[string]ShopifyLink{}
	itemSKU := map[string]string{}
	for _, l := range links {
		if l.InventoryItemID != "" {
			ids = append(ids, l.InventoryItemID)
			bySKU[l.SKU] = l
			itemSKU[l.InventoryItemID] = l.SKU
		}
	}
	levels, err := shopifyConn().ReadStockLevels(ctx, cred, ids)
	if err != nil {
		return nil, err
	}
	shown := map[string]int{}
	for _, lv := range levels {
		shown[itemSKU[lv.InventoryItemID]+"\x00"+lv.LocationID] = lv.Available
	}
	out := []ShopifyMismatch{}
	for sku, l := range bySKU {
		for loc, q := range shopifyStockFor(tenantID, sku, s, cfg.InventoryBuffer) {
			if sh := shown[sku+"\x00"+loc]; sh != q {
				out = append(out, ShopifyMismatch{SKU: sku, Name: l.Name, Location: loc, Store: s.Locations[loc], ERP: q, Shopify: sh})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SKU+out[i].Location < out[j].SKU+out[j].Location })
	return out, nil
}

// --- 99.5 orders in ----------------------------------------------------------------------

// shopifyGST splits tax lines into CGST / SGST / IGST (Shopify titles them
// so for an Indian store) and anything else.
func shopifyGST(tax []ShopifyTaxLine) map[string]float64 {
	out := map[string]float64{}
	for _, t := range tax {
		key := "other_tax"
		switch u := strings.ToUpper(t.Title); {
		case strings.Contains(u, "CGST"):
			key = "cgst"
		case strings.Contains(u, "SGST"), strings.Contains(u, "UTGST"):
			key = "sgst"
		case strings.Contains(u, "IGST"):
			key = "igst"
		}
		out[key] = round2(out[key] + t.Amount)
	}
	return out
}

// shopifyCustomer finds the customer by phone, or makes one (Stage 85's
// one-number-one-customer rule); a contact erased under the privacy rules
// is never re-created (Stage 101).
func shopifyCustomer(tenantID, store string, o ShopifyOrder) string {
	phone := firstNonEmpty(o.Phone, o.Ship.Phone, o.Bill.Phone)
	if strings.TrimSpace(phone) == "" {
		return ""
	}
	ref, found, national, err := LookupCustomerByPhone(tenantID, phone)
	if err != nil {
		return ""
	}
	if found {
		return ref.ID
	}
	if blocked, _ := CRMErasedContactBlocked(tenantID, national, o.Email); blocked {
		return ""
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	addr := o.Bill
	if addr.Address1 == "" {
		addr = o.Ship
	}
	name := strings.TrimSpace(o.FirstName + " " + o.LastName)
	payload := map[string]interface{}{"name": firstNonEmpty(name, addr.Name, "Shopify customer"), "phone": national, "email": o.Email, "status": "Active",
		"address": strings.TrimSpace(addr.Address1), "address_line2": addr.Address2, "city": addr.City, "state": addr.Province, "pincode": addr.Zip,
		"walk_in_store": store, "source": "Shopify", "consent_sms": false, "consent_whatsapp": false}
	if err := PrepareMasterNumber(tenantID, store, "Customer", true, payload); err != nil || strField(payload, "id") == "" {
		payload["id"] = NewDocID("CUST")
		payload["code"] = payload["id"]
	}
	id := strField(payload, "id")
	raw, _ := json.Marshal(payload)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2, 'Active', 'system')`, schema), id, raw); err != nil {
		return ""
	}
	LogAuditEvent(tenantID, shopifyActor, "CRM", "CUSTOMER_CREATED", fmt.Sprintf("Customer %s from Shopify order %s", id, o.Number))
	return id
}

func findChannelOrder(schema, channel, channelOrderID string) (id, status string) {
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(status, '') FROM %s.documents WHERE doctype = 'SalesOrder' AND deleted_at IS NULL
		AND data->>'channel' = $1 AND data->>'channel_order_id' = $2 ORDER BY created_at LIMIT 1`, schema), channel, channelOrderID).Scan(&id, &status)
	return id, status
}

// ImportShopifyOrder takes a Shopify order in (or applies its changes, if it
// is in already) and says what it did.
func ImportShopifyOrder(ctx context.Context, tenantID, channel string, o ShopifyOrder) (orderID, action string, err error) {
	cfg, s, err := LoadShopifyChannel(tenantID, channel)
	if err != nil {
		return "", "", err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	if id, status := findChannelOrder(schema, channel, o.ID); id != "" {
		action, err := applyShopifyOrderChange(ctx, tenantID, schema, channel, id, status, o)
		return id, action, err
	}
	if o.CancelledAt != nil {
		return "", "skipped (cancelled before it reached the ERP)", nil
	}
	// D5: the store mapped to the location Shopify chose ships it.
	store := cfg.LocationCode
	if cred, err := shopifyCred(tenantID, channel); err == nil {
		if loc, err := shopifyConn().AssignedLocation(ctx, cred, o.ID); err == nil && s.Locations[loc] != "" {
			store = s.Locations[loc]
		}
	}
	if store == "" {
		for _, st := range s.Locations {
			store = st
			break
		}
	}
	lines := make([]SalesOrderLineInput, 0, len(o.Lines))
	for _, l := range o.Lines {
		if l.Qty <= 0 {
			continue
		}
		unit := l.UnitPrice
		if l.Discount > 0 {
			unit = round2((l.UnitPrice*float64(l.Qty) - l.Discount) / float64(l.Qty))
		}
		lines = append(lines, SalesOrderLineInput{SKU: l.SKU, Qty: l.Qty, UnitPrice: unit})
	}
	name := strings.TrimSpace(o.FirstName + " " + o.LastName)
	orderID, err = ImportChannelSalesOrder(tenantID, ChannelOrderInput{Channel: channel, ChannelOrderID: o.ID, CustomerName: firstNonEmpty(name, o.Ship.Name),
		CustomerPhone: firstNonEmpty(o.Phone, o.Ship.Phone), ShippingAddress: o.Ship.Line(), ShippingState: o.Ship.Province, PaymentStatus: shopifyPaymentStatus(o),
		PreferredLocation: store, NoSplit: cfg.NoSplit, Lines: lines})
	if err != nil {
		return "", "", err
	}
	customer := shopifyCustomer(tenantID, store, o)
	gst := shopifyGST(o.Tax)
	extra := map[string]interface{}{
		"shopify_order_number": o.Number, "shopify_order_gid": o.GID, "customer_email": o.Email, "shipping_charge": round2(o.Shipping),
		"order_discount": round2(o.Discount), "payment_gateway": strings.Join(o.Gateways, ", "), "cod": o.COD(), "channel_financial_status": o.Financial,
		"billing_address": o.Bill.Line(), "billing_state": o.Bill.Province, "place_of_supply_state": firstNonEmpty(o.Ship.Province, o.Bill.Province),
		"tax_cgst": gst["cgst"], "tax_sgst": gst["sgst"], "tax_igst": gst["igst"], "tax_other": gst["other_tax"],
		"channel_updated_at": o.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if customer != "" {
		extra["customer_id"] = customer
		RecordCRMEventForTenant(tenantID, customer, CRMEventOrderPlaced, orderID, map[string]interface{}{"channel": channel, "order": o.Number, "total": o.Total})
		if o.CheckoutToken != "" { // the cart was bought: its journey ends (Stage 101)
			RecordCRMEventForTenant(tenantID, customer, CRMEventCartPurchased, o.CheckoutToken, map[string]interface{}{"cart": o.CheckoutToken, "order": orderID})
		}
	}
	// An ERP voucher code on the order takes its one use here; one the shop
	// honoured although it was no longer usable waits in Channel changes.
	var voucherProblems []string
	for _, code := range o.DiscountCodes {
		if p := RedeemCRMVoucherForChannelOrder(tenantID, code, customer, o.CheckoutToken, orderID); p != "" {
			voucherProblems = append(voucherProblems, p)
		}
	}
	if len(voucherProblems) > 0 {
		extra["channel_change_pending"] = strings.Join(voucherProblems, "; ")
	}
	_ = mergeSalesOrderData(schema, orderID, extra)
	return orderID, "imported", nil
}

// applyShopifyOrderChange applies a Shopify edit, cancellation or payment to
// an order already in: until picking starts it is made on the order; after,
// it waits in the Channel changes queue for a person (99.5.2).
func applyShopifyOrderChange(ctx context.Context, tenantID, schema, channel, orderID, status string, o ShopifyOrder) (string, error) {
	var lastSeen string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'channel_updated_at', '') FROM %s.documents WHERE id = $1`, schema), orderID).Scan(&lastSeen)
	if seen, err := time.Parse(time.RFC3339, lastSeen); err == nil && !o.UpdatedAt.IsZero() && !o.UpdatedAt.Truncate(time.Second).After(seen) {
		return "unchanged", nil
	}
	mark := func(fields map[string]interface{}) {
		fields["channel_updated_at"] = o.UpdatedAt.UTC().Format(time.RFC3339)
		_ = mergeSalesOrderData(schema, orderID, fields)
	}
	pending := func(what string) (string, error) {
		mark(map[string]interface{}{"channel_change_pending": what})
		return "to the Channel changes queue: " + what, nil
	}
	if status == "Cancelled" || status == "Closed" {
		mark(map[string]interface{}{})
		return "unchanged (the order is " + strings.ToLower(status) + ")", nil
	}
	if o.CancelledAt != nil {
		reason := shopifyCancelReason(schema)
		if reason == "" {
			return pending("cancelled in Shopify - no active Cancellation reason code to cancel it with")
		}
		if err := CancelOrder(tenantID, orderID, reason, shopifyActor); err != nil {
			return pending("cancelled in Shopify, but here it could not be: " + err.Error())
		}
		mark(map[string]interface{}{"channel_financial_status": o.Financial})
		return "cancelled", nil
	}
	action := "updated"
	// Payment: a pending order now paid is released.
	var payStatus, hold string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'payment_status', ''), COALESCE(data->>'hold_reason', '') FROM %s.documents WHERE id = $1`, schema), orderID).Scan(&payStatus, &hold)
	if np := shopifyPaymentStatus(o); np != payStatus && (np == "Confirmed" || np == "COD") {
		mark(map[string]interface{}{"payment_status": np, "channel_financial_status": o.Financial})
		if hold == HoldPaymentPending {
			_ = ReleaseOrderHold(tenantID, orderID)
		}
		action = "payment " + strings.ToLower(np)
	}
	// Lines: what Shopify now asks for against what the order holds.
	want := map[string]int{}
	price := map[string]float64{}
	for _, l := range o.Lines {
		if l.Qty > 0 {
			want[l.SKU] += l.Qty
			price[l.SKU] = l.UnitPrice
			if l.Discount > 0 {
				price[l.SKU] = round2((l.UnitPrice*float64(l.Qty) - l.Discount) / float64(l.Qty))
			}
		}
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'sku', ''), COALESCE(data->>'qty', '0') FROM %s.documents
		WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL AND data->>'order_id' = $1 AND COALESCE(data->>'line_status', '') <> 'Cancelled'`, schema), orderID)
	if err != nil {
		return action, err
	}
	have := map[string]int{}
	lineOf := map[string]string{}
	for rows.Next() {
		var id, sku, q string
		if rows.Scan(&id, &sku, &q) == nil {
			n := int(numFromInterface(q))
			have[sku] += n
			lineOf[sku] = id
		}
	}
	rows.Close()
	var changes []OrderLineChange
	var described []string
	for sku, q := range want {
		if have[sku] != q {
			p := price[sku]
			changes = append(changes, OrderLineChange{LineID: lineOf[sku], SKU: sku, Qty: q, UnitPrice: &p})
			described = append(described, fmt.Sprintf("%s %d → %d", sku, have[sku], q))
		}
	}
	for sku, q := range have {
		if _, ok := want[sku]; !ok && q > 0 {
			changes = append(changes, OrderLineChange{LineID: lineOf[sku], SKU: sku, Remove: true})
			described = append(described, fmt.Sprintf("%s removed", sku))
		}
	}
	sort.Strings(described)
	if len(changes) > 0 {
		if shopifyPickingStarted(schema, orderID) {
			return pending("edited in Shopify after picking started: " + strings.Join(described, ", "))
		}
		if _, err := EditOrderLines(tenantID, orderID, changes, "Changed in Shopify", shopifyActor); err != nil {
			return pending("edited in Shopify, but the order could not take it: " + err.Error())
		}
		action = "lines changed: " + strings.Join(described, ", ")
	}
	mark(map[string]interface{}{})
	return action, nil
}

func shopifyPickingStarted(schema, orderID string) bool {
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'FulfillmentTask' AND deleted_at IS NULL AND data->>'order_id' = $1
		AND status IN ('Picking', 'Picked', 'Packed', 'Dispatched')`, schema), orderID).Scan(&n)
	return n > 0
}

// shopifyCancelReason is the Cancellation reason a Shopify cancellation is
// recorded with: the customer one if the client has it, else the first.
func shopifyCancelReason(schema string) string {
	var code string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'ReasonCode' AND deleted_at IS NULL AND COALESCE(status, 'Active') = 'Active'
		AND data->>'category' = 'Cancellation' ORDER BY (upper(id) LIKE '%%CUSTOMER%%') DESC, id LIMIT 1`, schema)).Scan(&code)
	return code
}

// ClearChannelChange marks a Channel changes queue row done.
func ClearChannelChange(tenantID, orderID, actor string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data - 'channel_change_pending', updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'SalesOrder' AND id = $1 AND COALESCE(data->>'channel_change_pending', '') <> ''`, schema), orderID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return shopifyError(fmt.Sprintf("order %s has no channel change waiting", orderID))
	}
	LogAuditEvent(tenantID, actor, "OMS", "CHANNEL_CHANGE_DONE", "order "+orderID)
	return nil
}

// PullShopifyOrders is the 15-minute pull (D4): every order updated since
// the last pull, new ones imported, changed ones applied.
func PullShopifyOrders(ctx context.Context, tenantID, channel string) (processed, failed int, err error) {
	started := time.Now()
	defer func() {
		st := "Success"
		if err != nil {
			st = "Failed"
		}
		recordChannelSyncRun(tenantID, channel, "Order Pull", st, processed, failed, started, err)
	}()
	_, s, err := LoadShopifyChannel(tenantID, channel)
	if err != nil {
		return 0, 0, err
	}
	cred, err := shopifyCred(tenantID, channel)
	if err != nil {
		return 0, 0, err
	}
	since := time.Now().Add(-24 * time.Hour)
	if t, err := time.Parse(time.RFC3339, s.LastPull); err == nil {
		since = t.Add(-2 * time.Minute) // a small overlap; import is idempotent
	}
	cursor := ""
	for page := 0; page < 40; page++ {
		orders, next, err := shopifyConn().PullShopifyOrders(ctx, cred, since, cursor)
		if err != nil {
			return processed, failed, err
		}
		for _, o := range orders {
			if _, _, err := ImportShopifyOrder(ctx, tenantID, channel, o); err != nil {
				failed++
				LogSystemError(tenantID, "", "WARN", "shopify.pull", fmt.Sprintf("order %s: %v", o.Number, err), "")
				continue
			}
			processed++
		}
		if next == "" {
			break
		}
		cursor = next
	}
	_, s2, _ := LoadShopifyChannel(tenantID, channel)
	s2.LastPull = started.UTC().Format(time.RFC3339)
	_ = saveShopifySettings(tenantID, channel, s2)
	return processed, failed, nil
}

// --- webhooks --------------------------------------------------------------------------

// HandleShopifyWebhook takes one verified webhook (99.2.2). The caller has
// checked the HMAC with the channel's webhook secret.
func HandleShopifyWebhook(ctx context.Context, tenantID, channel, topic string, body []byte) (string, error) {
	switch strings.ToLower(strings.ReplaceAll(topic, "_", "/")) {
	case "orders/create", "orders/updated", "orders/cancelled", "orders/paid", "orders/edited":
		o, err := ParseShopifyOrderWebhook(body)
		if err != nil {
			return "", err
		}
		_, action, err := ImportShopifyOrder(ctx, tenantID, channel, o)
		return action, err
	case "refunds/create":
		return handleShopifyRefund(tenantID, channel, body)
	case "checkouts/create", "checkouts/update":
		return handleShopifyCheckout(tenantID, body)
	case "fulfillments/create":
		return "noted (shipping is sent from the ERP)", nil
	case "app/uninstalled":
		schema, err := db.GetTenantSchema(tenantID)
		if err == nil {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || '{"shopify_uninstalled": true}'::jsonb WHERE doctype = 'Channel' AND id = $1`, schema), channel)
		}
		LogAuditEvent(tenantID, shopifyActor, "INTEGRATION", "SHOPIFY_APP_UNINSTALLED", "channel "+channel)
		return "the app was removed from the shop", nil
	}
	return "ignored", nil
}

// handleShopifyCheckout records a web-shop cart for a customer the ERP
// knows (by phone, else email) - Stage 101's "cart left on the web shop"
// journey starts on it and ends when the same cart is bought. A checkout
// already completed, or with no known customer, is ignored.
func handleShopifyCheckout(tenantID string, body []byte) (string, error) {
	var c struct {
		Token       string  `json:"token"`
		Email       string  `json:"email"`
		Phone       string  `json:"phone"`
		CompletedAt *string `json:"completed_at"`
		URL         string  `json:"abandoned_checkout_url"`
		Customer    *struct {
			Phone string `json:"phone"`
			Email string `json:"email"`
		} `json:"customer"`
		DiscountCodes []struct {
			Code string `json:"code"`
		} `json:"discount_codes"`
	}
	if err := json.Unmarshal(body, &c); err != nil || c.Token == "" {
		return "", fmt.Errorf("not a Shopify checkout")
	}
	if c.CompletedAt != nil && *c.CompletedAt != "" {
		return "ignored (completed)", nil
	}
	phone, email := c.Phone, c.Email
	if c.Customer != nil {
		phone, email = firstNonEmpty(phone, c.Customer.Phone), firstNonEmpty(email, c.Customer.Email)
	}
	customer := ""
	if phone != "" {
		if ref, found, _, err := LookupCustomerByPhone(tenantID, phone); err == nil && found {
			customer = ref.ID
		}
	}
	if customer == "" && email != "" {
		if schema, err := db.GetTenantSchema(tenantID); err == nil {
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Customer' AND deleted_at IS NULL AND lower(data->>'email') = lower($1) ORDER BY created_at LIMIT 1`, schema), email).Scan(&customer)
		}
	}
	// An ERP voucher code typed at the checkout is held for it, so the till
	// cannot take the same single-use code meanwhile (Stage 101.11.2); the
	// paid order uses it up. A code the ERP does not know is the shop's own.
	for _, d := range c.DiscountCodes {
		_, _ = ReserveCRMVoucher(tenantID, d.Code, VoucherChannelOnline, c.Token, customer)
	}
	if customer == "" {
		return "ignored (no known customer)", nil
	}
	RecordCRMEventForTenant(tenantID, customer, CRMEventCartAbandoned, c.Token, map[string]interface{}{"cart": c.Token, "item.link": c.URL})
	return "cart noted", nil
}

// handleShopifyRefund turns a Shopify refund into an ERP return through the
// returns path (Customer Return against the order's invoice); before the
// order is invoiced there is nothing to return, so a person decides.
func handleShopifyRefund(tenantID, channel string, body []byte) (string, error) {
	var r struct {
		ID          json.Number `json:"id"`
		OrderID     json.Number `json:"order_id"`
		Restock     bool        `json:"restock"`
		Note        string      `json:"note"`
		RefundLines []struct {
			Quantity int `json:"quantity"`
			LineItem struct {
				SKU string `json:"sku"`
			} `json:"line_item"`
		} `json:"refund_line_items"`
	}
	if err := json.Unmarshal(body, &r); err != nil || r.OrderID.String() == "" {
		return "", fmt.Errorf("not a Shopify refund")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	orderID, _ := findChannelOrder(schema, channel, r.OrderID.String())
	if orderID == "" {
		return "ignored (the order is not in the ERP)", nil
	}
	var items []ReturnItemInput
	for _, l := range r.RefundLines {
		if l.Quantity > 0 && l.LineItem.SKU != "" {
			items = append(items, ReturnItemInput{SKU: l.LineItem.SKU, Qty: l.Quantity})
		}
	}
	if len(items) == 0 {
		_ = mergeSalesOrderData(schema, orderID, map[string]interface{}{"channel_change_pending": "a money-only refund in Shopify (no items) - record it on the order's credit"})
		return "to the Channel changes queue (money-only refund)", nil
	}
	var invoice, store string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'SalesInvoice' AND deleted_at IS NULL AND data->>'sales_order_id' = $1 ORDER BY created_at LIMIT 1`, schema), orderID).Scan(&invoice)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'location_code', '') FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 LIMIT 1`, schema), orderID).Scan(&store)
	if invoice == "" {
		_ = mergeSalesOrderData(schema, orderID, map[string]interface{}{"channel_change_pending": fmt.Sprintf("refunded in Shopify before it shipped (%d item lines) - cancel or edit the order", len(items))})
		return "to the Channel changes queue (refund before shipping)", nil
	}
	res, err := CreateReturnRequestCommand(tenantID, "Customer Return", store, invoice, "", shopifyActor, "shopify-refund-"+r.ID.String(), "", items, nil)
	if err != nil {
		_ = mergeSalesOrderData(schema, orderID, map[string]interface{}{"channel_change_pending": "refunded in Shopify, but the return could not be raised: " + err.Error()})
		return "to the Channel changes queue", nil
	}
	return "return " + res.ReturnRequestID + " raised", nil
}

// --- 99.6 status back to Shopify --------------------------------------------------------

// queueShopifyStatus queues a status for a Shopify channel's order; any
// other channel's order is left alone.
func queueShopifyStatus(tenantID, schema, orderID, status, carrier, tracking string, items []ConnectorStatusLine, key string) {
	var channel, ref string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'channel', ''), COALESCE(data->>'channel_order_id', '') FROM %s.documents WHERE doctype = 'SalesOrder' AND id = $1`, schema), orderID).Scan(&channel, &ref)
	if ref == "" || !isShopifyChannel(tenantID, channel) {
		return
	}
	payload := map[string]interface{}{"tenant_id": tenantID, "channel": channel, "order_id": orderID, "channel_order_id": ref, "status": status,
		"carrier": carrier, "tracking": tracking}
	if len(items) > 0 {
		raw, _ := json.Marshal(items)
		payload["items"] = string(raw)
	}
	if _, err := EnqueueJob(tenantID, shopifyStatusJobType, payload, "shopify:"+orderID+":"+key); err != nil {
		LogSystemError(tenantID, "", "WARN", "shopify.status", fmt.Sprintf("order %s: %s not queued: %v", orderID, status, err), "")
	}
}

// queueShopifyShipments sends each dispatched task of an order once, with
// its lines and its booking's carrier and tracking (part shipments too).
func queueShopifyShipments(tenantID, schema, orderID string) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT t.id, COALESCE(t.data->'items', '[]'::jsonb)::text,
		COALESCE(b.data->>'carrier', ''), COALESCE(NULLIF(b.data->>'tracking_number', ''), b.data->>'awb_number', '')
		FROM %s.documents t LEFT JOIN LATERAL (SELECT data FROM %s.documents x WHERE x.doctype = 'LogisticsBooking' AND x.deleted_at IS NULL
		  AND (x.data->>'fulfillment_task_id' = t.id OR (COALESCE(x.data->>'fulfillment_task_id', '') = '' AND x.data->>'order_id' = $1))
		  ORDER BY (x.data->>'fulfillment_task_id' = t.id) DESC, x.created_at DESC LIMIT 1) b ON TRUE
		WHERE t.doctype = 'FulfillmentTask' AND t.deleted_at IS NULL AND t.data->>'order_id' = $1 AND t.status = 'Dispatched'`, schema, schema), orderID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var taskID, itemsRaw, carrier, tracking string
		if rows.Scan(&taskID, &itemsRaw, &carrier, &tracking) != nil {
			continue
		}
		var lines []struct {
			SKU string  `json:"sku"`
			Qty float64 `json:"qty"`
		}
		_ = json.Unmarshal([]byte(itemsRaw), &lines)
		items := []ConnectorStatusLine{}
		for _, l := range lines {
			if l.SKU != "" && l.Qty > 0 {
				items = append(items, ConnectorStatusLine{OrderItemID: l.SKU, Quantity: int(l.Qty)})
			}
		}
		queueShopifyStatus(tenantID, schema, orderID, "Shipped", carrier, tracking, items, "ship:"+taskID)
	}
}

// queueShopifyRTO marks a booking's order RTO in Shopify.
func queueShopifyRTO(tenantID, schema, bookingID string) {
	var orderID string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'order_id', '') FROM %s.documents WHERE doctype = 'LogisticsBooking' AND id = $1`, schema), bookingID).Scan(&orderID)
	if orderID != "" {
		queueShopifyStatus(tenantID, schema, orderID, "RTO", "", "", nil, "rto:"+bookingID)
	}
}

// pushShopifyStatusJob is the job: sent, simulated (sandbox, or side effects
// off) or retried by the runner; the order keeps the last attempt.
func pushShopifyStatusJob(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
	str := func(k string) string { v, _ := job.Payload[k].(string); return v }
	tenantID, orderID, status := str("tenant_id"), str("order_id"), str("status")
	record := func(st, detail string) {
		_ = mergeSalesOrderData(schema, orderID, map[string]interface{}{"shopify_push": map[string]interface{}{
			"status": status, "result": st, "error": detail, "at": time.Now().UTC().Format(time.RFC3339), "attempts": job.Attempts + 1}})
	}
	ok, reason := OutsideCallsAllowed(schema) // 49.8.7a
	if sandbox, _ := IsSandboxSchema(schema); sandbox || !ok {
		record("Simulated", firstNonEmpty(reason, "a sandbox client")+" - nothing was sent")
		return map[string]interface{}{"simulated": true}, nil
	}
	var items []ConnectorStatusLine
	_ = json.Unmarshal([]byte(str("items")), &items)
	err := PushChannelOrderStatus(ctx, tenantID, str("channel"), ConnectorStatusUpdate{ChannelOrderID: str("channel_order_id"), Status: status,
		Carrier: str("carrier"), TrackingNumber: str("tracking"), Items: items})
	if err != nil {
		record("Failed", err.Error())
		return nil, err
	}
	record("Sent", "")
	return map[string]interface{}{"sent": true}, nil
}

// --- 99.7 payouts ----------------------------------------------------------------------

// SyncShopifyPayouts brings Shopify Payments payouts in as settlement lines
// (one per order in each payout; Shopify's fee as the commission) and runs
// the marketplace reconciliation, which posts them and matches the orders;
// the payout then matches its bank line in Bank Reconciliation.
func SyncShopifyPayouts(ctx context.Context, tenantID, channel, actor string) (lines int, err error) {
	started := time.Now()
	defer func() {
		st := "Success"
		if err != nil {
			st = "Failed"
		}
		recordChannelSyncRun(tenantID, channel, "Payout Sync", st, lines, 0, started, err)
	}()
	_, s, err := LoadShopifyChannel(tenantID, channel)
	if err != nil {
		return 0, err
	}
	cred, err := shopifyCred(tenantID, channel)
	if err != nil {
		return 0, err
	}
	since := time.Now().AddDate(0, 0, -30)
	if t, err := time.Parse(time.RFC3339, s.LastPayouts); err == nil {
		since = t.AddDate(0, 0, -3)
	}
	payouts, err := shopifyConn().ReadPayouts(ctx, cred, since)
	if err != nil {
		return 0, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	for _, p := range payouts {
		if p.Status != "paid" && p.Status != "in_transit" {
			continue
		}
		for i, l := range p.Lines {
			if l.OrderID == "" {
				continue // reserves and adjustments: not an order's money
			}
			id := fmt.Sprintf("SHOPIFY-%s-%s-%d", p.ID, l.OrderID, i)
			raw, _ := json.Marshal(map[string]interface{}{"code": id, "channel": channel, "channel_order_id": l.OrderID, "settlement_batch_id": "SHOPIFY-" + p.ID,
				"settlement_date": p.IssuedAt.Format("2006-01-02"), "gross_amount": round2(l.Amount), "commission": round2(l.Fee), "shipping_fee": 0, "other_fee": 0,
				"tds": 0, "tcs": 0, "net_payout": round2(l.Net), "match_status": "Unmatched", "source": "Shopify Payments", "shopify_type": l.Type, "shopify_order": l.Order})
			res, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'MarketplaceSettlementLine', $2, 'Active', 'system')
				ON CONFLICT DO NOTHING`, schema), id, raw)
			if err != nil {
				return lines, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				lines++
			}
		}
	}
	if lines > 0 {
		if _, err := ReconcileMarketplaceSettlements(tenantID, firstNonEmpty(actor, shopifyActor)); err != nil {
			return lines, err
		}
	}
	_, s2, _ := LoadShopifyChannel(tenantID, channel)
	s2.LastPayouts = started.UTC().Format(time.RFC3339)
	_ = saveShopifySettings(tenantID, channel, s2)
	return lines, nil
}

// --- the schedule ------------------------------------------------------------------------

// RunShopifyScheduled is one channel's turn of the channel sync worker:
// orders, changed prices and stock every interval; full stock and payouts
// once a day.
func RunShopifyScheduled(ctx context.Context, tenantID, channel string) {
	_, s, err := LoadShopifyChannel(tenantID, channel)
	if err != nil {
		return
	}
	if ok, _ := HasChannelCredential(tenantID, channel); !ok {
		return
	}
	if s.SyncOrders {
		_, _, _ = PullShopifyOrders(ctx, tenantID, channel)
	}
	if s.SyncPrices {
		_, _ = PushShopifyPrices(ctx, tenantID, channel, false)
	}
	if s.SyncStock && len(s.Locations) > 0 {
		last, _ := time.Parse(time.RFC3339, s.LastFull)
		_, _ = PushShopifyStock(ctx, tenantID, channel, time.Since(last) > 20*time.Hour, nil)
	}
	if last, _ := time.Parse(time.RFC3339, s.LastPayouts); time.Since(last) > 20*time.Hour {
		_, _ = SyncShopifyPayouts(ctx, tenantID, channel, shopifyActor)
	}
}

// ShopifyOverview is the setup page's state of a channel.
type ShopifyOverview struct {
	Channel        string          `json:"channel"`
	Name           string          `json:"name"`
	Settings       ShopifySettings `json:"settings"`
	Buffer         int             `json:"inventory_buffer"`
	Store          string          `json:"store"`
	CredentialsSet bool            `json:"credentials_set"`
	ShopDomain     string          `json:"shop_domain"`
	WebhookSecret  bool            `json:"webhook_secret_set"`
	Links          int             `json:"links"`
	Products       int             `json:"products"`
	Runs           []ShopifyRun    `json:"runs"`
	Pending        int             `json:"channel_changes"`
}

// ShopifyRun is one recent sync run.
type ShopifyRun struct {
	Operation string `json:"operation"`
	Status    string `json:"status"`
	Processed int    `json:"processed"`
	Failed    int    `json:"failed"`
	Error     string `json:"error"`
	At        string `json:"at"`
}

// GetShopifyOverview reads a Shopify channel for its setup page; no secret
// leaves the server.
func GetShopifyOverview(tenantID, channel string) (ShopifyOverview, error) {
	cfg, s, err := LoadShopifyChannel(tenantID, channel)
	if err != nil {
		return ShopifyOverview{}, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ShopifyOverview{}, err
	}
	ov := ShopifyOverview{Channel: channel, Settings: s, Buffer: cfg.InventoryBuffer, Store: cfg.LocationCode, Runs: []ShopifyRun{}}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'name', id) FROM %s.documents WHERE doctype = 'Channel' AND id = $1`, schema), channel).Scan(&ov.Name)
	ov.CredentialsSet, _ = HasChannelCredential(tenantID, channel)
	if ov.CredentialsSet {
		if cred, err := getChannelCredential(tenantID, channel); err == nil {
			ov.ShopDomain = cred["shop_domain"]
			ov.WebhookSecret = cred["webhook_secret"] != ""
		}
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*), COUNT(DISTINCT product_gid) FROM %s.shopify_links WHERE channel = $1`, schema), channel).Scan(&ov.Links, &ov.Products)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'SalesOrder' AND deleted_at IS NULL AND data->>'channel' = $1
		AND COALESCE(data->>'channel_change_pending', '') <> ''`, schema), channel).Scan(&ov.Pending)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'operation', ''), COALESCE(data->>'status', ''), COALESCE((data->>'processed')::int, 0),
		COALESCE((data->>'failed')::int, 0), COALESCE(data->>'error', ''), to_char(created_at, 'YYYY-MM-DD HH24:MI')
		FROM %s.documents WHERE doctype = 'ChannelSyncRun' AND data->>'channel' = $1 ORDER BY created_at DESC LIMIT 12`, schema), channel)
	if err == nil {
		for rows.Next() {
			var r ShopifyRun
			if rows.Scan(&r.Operation, &r.Status, &r.Processed, &r.Failed, &r.Error, &r.At) == nil {
				ov.Runs = append(ov.Runs, r)
			}
		}
		rows.Close()
	}
	return ov, nil
}

// ChannelCredentialFields are a channel's saved credential fields, for a
// form that keeps what it is not asked to change.
func ChannelCredentialFields(tenantID, channel string) (map[string]string, error) {
	return getChannelCredential(tenantID, channel)
}

// CheckShopifyChannel tests the connection and remembers the shop's name.
func CheckShopifyChannel(ctx context.Context, tenantID, channel string, cred map[string]string) (ShopifyCheck, error) {
	chk, err := shopifyConn().CheckShopify(ctx, cred)
	if err != nil {
		return chk, err
	}
	if _, s, err := LoadShopifyChannel(tenantID, channel); err == nil && s.Shop != chk.Shop {
		s.Shop = chk.Shop
		_ = saveShopifySettings(tenantID, channel, s)
	}
	return chk, nil
}

// RegisterShopifyWebhooks points the shop's webhooks at callback and notes
// when (99.2.2); "Register again" calls it again.
func RegisterShopifyWebhooks(ctx context.Context, tenantID, channel, callback, actor string) ([]string, error) {
	_, s, err := LoadShopifyChannel(tenantID, channel)
	if err != nil {
		return nil, err
	}
	cred, err := shopifyCred(tenantID, channel)
	if err != nil {
		return nil, err
	}
	if cred["webhook_secret"] == "" {
		return nil, shopifyError("save the webhook secret (the app's API secret key) first - without it every webhook is refused")
	}
	topics, err := shopifyConn().RegisterWebhooks(ctx, cred, callback)
	if err != nil {
		return topics, err
	}
	s.WebhookURL, s.WebhooksAt = callback, time.Now().UTC().Format(time.RFC3339)
	_ = saveShopifySettings(tenantID, channel, s)
	LogAuditEvent(tenantID, actor, "INTEGRATION", "SHOPIFY_WEBHOOKS_REGISTERED", fmt.Sprintf("channel %s: %s", channel, strings.Join(topics, ", ")))
	return topics, nil
}
