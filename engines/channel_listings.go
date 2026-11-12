package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// Stage 146.4.5: a channel's own product listings. Pull them through the
// connector (only a connector that can list: Shopify today; Amazon, Flipkart,
// WooCommerce and the rest have no listing read, so a pull on them is refused,
// never faked), link them to ERP items by SKU then barcode after a preview,
// and keep a per-listing switch that the stock push honours. Nothing happens
// until someone pulls; an unlinked listing, and a table with no rows, change
// nothing about how stock is sent today.

// ListedProduct is one listing as a channel reports it.
type ListedProduct struct {
	Ref          string // the channel's id for the listing (Shopify: the variant)
	ChannelSKU   string
	Barcode      string
	Title        string
	ProductRef   string
	InventoryRef string
	Price        float64
	CompareAt    float64
}

// ChannelProductLister is the optional connector capability to read what the
// channel lists. A connector without it cannot be pulled.
type ChannelProductLister interface {
	ListListings(ctx context.Context, cred map[string]string) ([]ListedProduct, error)
}

// ListListings reads every Shopify variant as a listing.
func (c shopifySDKConnector) ListListings(ctx context.Context, cred map[string]string) ([]ListedProduct, error) {
	vs, err := c.ListVariants(ctx, cred)
	if err != nil {
		return nil, err
	}
	out := make([]ListedProduct, 0, len(vs))
	for _, v := range vs {
		out = append(out, ListedProduct{Ref: v.VariantGID, ChannelSKU: v.SKU, Barcode: v.Barcode, Title: v.Title,
			ProductRef: v.ProductGID, InventoryRef: v.InventoryItemID, Price: v.Price, CompareAt: v.CompareAt})
	}
	return out, nil
}

// ChannelCanListProducts says whether the platform's connector can list.
func ChannelCanListProducts(platform string) bool {
	c, err := ResolveOmnichannelConnector(platform)
	if err != nil {
		return false
	}
	_, ok := c.(ChannelProductLister)
	return ok
}

// ChannelListing is one stored listing.
type ChannelListing struct {
	Ref        string  `json:"ref"`
	ChannelSKU string  `json:"channel_sku"`
	Barcode    string  `json:"barcode"`
	Title      string  `json:"title"`
	Price      float64 `json:"price"`
	SKU        string  `json:"sku"`
	ItemName   string  `json:"item_name"`
	StockSync  bool    `json:"stock_sync"`
	State      string  `json:"state"`
	PulledAt   string  `json:"pulled_at"`
}

// ListingPullResult is what a pull did.
type ListingPullResult struct {
	Listed int `json:"listed"`
	New    int `json:"new"`
	Gone   int `json:"gone"`
}

func listingErr(code, msg string) error { return &ValidationError{Code: code, Message: msg} }

// PullChannelListings reads the channel's listings and stores them. A listing
// already stored keeps its link and its stock switch; one the channel no
// longer reports is marked Gone, never deleted.
func PullChannelListings(ctx context.Context, tenantID, channel, actor string) (ListingPullResult, error) {
	var res ListingPullResult
	cfg, err := LoadChannelRuntimeConfig(tenantID, channel)
	if err != nil {
		return res, err
	}
	conn, err := ResolveOmnichannelConnector(cfg.Platform)
	if err != nil {
		return res, err
	}
	lister, ok := conn.(ChannelProductLister)
	if !ok {
		return res, listingErr("INTEGR-4800", cfg.Platform+" cannot list its products to the ERP. Link its SKUs one by one on the SKU mapping list.")
	}
	cred, err := LoadConnectorCredential(tenantID, channel, cfg.Platform)
	if err != nil {
		return res, err
	}
	listed, err := lister.ListListings(ctx, cred)
	if err != nil {
		return res, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(fmt.Sprintf(`UPDATE %s.channel_listings SET state = 'Pending' WHERE channel = $1`, schema), channel); err != nil {
		return res, err
	}
	for _, p := range listed {
		if strings.TrimSpace(p.Ref) == "" {
			continue
		}
		var inserted bool
		err = tx.QueryRow(fmt.Sprintf(`INSERT INTO %s.channel_listings (channel, listing_ref, channel_sku, barcode, title, product_ref, inventory_ref, price, compare_at, state, pulled_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'Listed',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)
			ON CONFLICT (channel, listing_ref) DO UPDATE SET channel_sku = EXCLUDED.channel_sku, barcode = EXCLUDED.barcode, title = EXCLUDED.title,
			  product_ref = EXCLUDED.product_ref, inventory_ref = EXCLUDED.inventory_ref, price = EXCLUDED.price, compare_at = EXCLUDED.compare_at,
			  state = 'Listed', pulled_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			RETURNING (xmax = 0)`, schema), channel, p.Ref, strings.TrimSpace(p.ChannelSKU), strings.TrimSpace(p.Barcode), p.Title, p.ProductRef, p.InventoryRef,
			round2(p.Price), round2(p.CompareAt)).Scan(&inserted)
		if err != nil {
			return res, err
		}
		res.Listed++
		if inserted {
			res.New++
		}
	}
	r, err := tx.Exec(fmt.Sprintf(`UPDATE %s.channel_listings SET state = 'Gone', updated_at = CURRENT_TIMESTAMP WHERE channel = $1 AND state = 'Pending'`, schema), channel)
	if err != nil {
		return res, err
	}
	n, _ := r.RowsAffected()
	res.Gone = int(n)
	if err = tx.Commit(); err != nil {
		return res, err
	}
	LogAuditEvent(tenantID, actor, "INTEGRATION", "CHANNEL_LISTINGS_PULLED", fmt.Sprintf("channel %s: %d listed, %d new, %d gone", channel, res.Listed, res.New, res.Gone))
	return res, nil
}

// ListChannelListings is the stored list; filter is "", "linked", "unlinked" or "off".
func ListChannelListings(tenantID, channel, filter string) ([]ChannelListing, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	where := ""
	switch filter {
	case "linked":
		where = " AND l.sku <> ''"
	case "unlinked":
		where = " AND l.sku = ''"
	case "off":
		where = " AND NOT l.stock_sync"
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT l.listing_ref, l.channel_sku, l.barcode, l.title, l.price, l.sku, COALESCE(i.data->>'name',''), l.stock_sync, l.state,
		COALESCE(to_char(l.pulled_at, 'YYYY-MM-DD HH24:MI'), '')
		FROM %s.channel_listings l LEFT JOIN %s.documents i ON i.doctype = 'Item' AND i.id = l.sku AND l.sku <> '' AND i.deleted_at IS NULL
		WHERE l.channel = $1%s ORDER BY l.channel_sku, l.listing_ref LIMIT 5000`, schema, schema, where), channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChannelListing{}
	for rows.Next() {
		var l ChannelListing
		if err := rows.Scan(&l.Ref, &l.ChannelSKU, &l.Barcode, &l.Title, &l.Price, &l.SKU, &l.ItemName, &l.StockSync, &l.State, &l.PulledAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListingLinkRow is one unlinked listing in the link preview.
type ListingLinkRow struct {
	Ref        string `json:"ref"`
	ChannelSKU string `json:"channel_sku"`
	Barcode    string `json:"barcode"`
	Title      string `json:"title"`
	Result     string `json:"result"` // Match, Conflict, Unlinked
	Item       string `json:"item"`
	MatchedBy  string `json:"matched_by"`
	Reason     string `json:"reason"`
}

// ListingLinkPreview is the preview: every row, and the counts.
type ListingLinkPreview struct {
	Rows     []ListingLinkRow `json:"rows"`
	Match    int              `json:"match"`
	Conflict int              `json:"conflict"`
	Unlinked int              `json:"unlinked"`
}

type listingLinkPlan struct {
	row ListingLinkRow
	ref ListedProduct
}

// planListingLinks decides, for every unlinked listing the channel still has,
// the item it matches (SKU first, else barcode) or why it cannot.
func planListingLinks(tenantID, channel string) ([]listingLinkPlan, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT listing_ref, channel_sku, barcode, title, product_ref, inventory_ref, price, compare_at
		FROM %s.channel_listings WHERE channel = $1 AND sku = '' AND state = 'Listed' ORDER BY channel_sku, listing_ref`, schema), channel)
	if err != nil {
		return nil, err
	}
	var plans []listingLinkPlan
	skus, codes := []string{}, []string{}
	for rows.Next() {
		var p ListedProduct
		if err := rows.Scan(&p.Ref, &p.ChannelSKU, &p.Barcode, &p.Title, &p.ProductRef, &p.InventoryRef, &p.Price, &p.CompareAt); err != nil {
			rows.Close()
			return nil, err
		}
		plans = append(plans, listingLinkPlan{ref: p, row: ListingLinkRow{Ref: p.Ref, ChannelSKU: p.ChannelSKU, Barcode: p.Barcode, Title: p.Title}})
		if p.ChannelSKU != "" {
			skus = append(skus, p.ChannelSKU)
		}
		if p.Barcode != "" {
			codes = append(codes, p.Barcode)
		}
	}
	rows.Close()
	if len(plans) == 0 {
		return plans, nil
	}
	// Items by id and by barcode, in one query for the whole batch.
	byID := map[string]bool{}
	byCode := map[string][]string{}
	ir, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'barcode','') FROM %s.documents
		WHERE doctype = 'Item' AND deleted_at IS NULL AND (id = ANY($1) OR (data->>'barcode' <> '' AND data->>'barcode' = ANY($2))) ORDER BY id`, schema),
		pq.Array(skus), pq.Array(codes))
	if err != nil {
		return nil, err
	}
	for ir.Next() {
		var id, code string
		if err := ir.Scan(&id, &code); err != nil {
			ir.Close()
			return nil, err
		}
		byID[id] = true
		if code != "" {
			byCode[code] = append(byCode[code], id)
		}
	}
	ir.Close()
	// What is already tied to a listing or a mapping on this channel.
	linked := map[string]string{}
	lr, err := db.DB.Query(fmt.Sprintf(`SELECT sku, channel_sku FROM %s.channel_listings WHERE channel = $1 AND sku <> ''`, schema), channel)
	if err != nil {
		return nil, err
	}
	for lr.Next() {
		var s, cs string
		_ = lr.Scan(&s, &cs)
		linked[s] = cs
	}
	lr.Close()
	type mapped struct{ chSKU, ext string }
	maps := map[string]mapped{}
	mr, err := db.DB.Query(fmt.Sprintf(`SELECT sku, channel_sku, COALESCE(external_product_id,'') FROM %s.channel_product_mapping WHERE channel = $1`, schema), channel)
	if err != nil {
		return nil, err
	}
	for mr.Next() {
		var s string
		var m mapped
		_ = mr.Scan(&s, &m.chSKU, &m.ext)
		maps[s] = m
	}
	mr.Close()
	shop := isShopifyChannel(tenantID, channel)
	used := map[string]int{}
	for i := range plans {
		p := &plans[i]
		item, by, reason := "", "", ""
		ids := byCode[p.ref.Barcode]
		switch {
		case p.ref.ChannelSKU != "" && byID[p.ref.ChannelSKU]:
			item, by = p.ref.ChannelSKU, "SKU"
			if p.ref.Barcode != "" && len(ids) > 0 && !(len(ids) == 1 && ids[0] == item) {
				reason = "Its SKU and its barcode point to different items."
			}
		case p.ref.Barcode != "" && len(ids) == 1:
			item, by = ids[0], "Barcode"
		case p.ref.Barcode != "" && len(ids) > 1:
			by, reason = "Barcode", "More than one item has this barcode."
		}
		if reason == "" && item != "" {
			if cs, ok := linked[item]; ok {
				reason = "The item is already linked to listing " + cs + "."
			} else if m, ok := maps[item]; ok {
				want := p.ref.ChannelSKU
				if shop || want == "" {
					want = item
				}
				if (m.ext != "" && m.ext != p.ref.Ref) || (m.ext == "" && m.chSKU != want) {
					reason = "The item is already mapped to another channel SKU."
				}
			}
			if reason == "" {
				used[item]++
			}
		}
		p.row.Item, p.row.MatchedBy, p.row.Reason = item, by, reason
		switch {
		case reason != "":
			p.row.Result = "Conflict"
		case item != "":
			p.row.Result = "Match"
		default:
			p.row.Result = "Unlinked"
		}
	}
	// Two listings that want the same item both wait.
	for i := range plans {
		p := &plans[i]
		if p.row.Result == "Match" && used[p.row.Item] > 1 {
			p.row.Result, p.row.Reason = "Conflict", "Another listing matches the same item."
		}
	}
	return plans, nil
}

// PreviewListingLinks shows what a link would do, writing nothing.
func PreviewListingLinks(tenantID, channel string) (ListingLinkPreview, error) {
	out := ListingLinkPreview{Rows: []ListingLinkRow{}}
	plans, err := planListingLinks(tenantID, channel)
	if err != nil {
		return out, err
	}
	for _, p := range plans {
		out.Rows = append(out.Rows, p.row)
		switch p.row.Result {
		case "Match":
			out.Match++
		case "Conflict":
			out.Conflict++
		default:
			out.Unlinked++
		}
	}
	return out, nil
}

// CommitListingLinks links the listings that match (all of them, or only
// those named), re-checked now so a stale preview cannot force a conflict.
// Conflicts and unlinked listings are left as they are. All or nothing.
func CommitListingLinks(tenantID, channel, actor string, refs []string) (linked, skipped int, err error) {
	plans, err := planListingLinks(tenantID, channel)
	if err != nil {
		return 0, 0, err
	}
	only := map[string]bool{}
	for _, r := range refs {
		only[r] = true
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}
	shop := isShopifyChannel(tenantID, channel)
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var shopLinks []ShopifyVariantLink
	var shopDesign []string
	for _, p := range plans {
		if len(only) > 0 && !only[p.ref.Ref] {
			continue
		}
		if p.row.Result != "Match" {
			skipped++
			continue
		}
		if _, err = tx.Exec(fmt.Sprintf(`UPDATE %s.channel_listings SET sku = $3, linked_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE channel = $1 AND listing_ref = $2`, schema),
			channel, p.ref.Ref, p.row.Item); err != nil {
			return 0, 0, err
		}
		chSKU := p.ref.ChannelSKU
		if shop || chSKU == "" {
			chSKU = p.row.Item
		}
		if _, err = tx.Exec(fmt.Sprintf(`INSERT INTO %s.channel_product_mapping (sku,channel,channel_sku,external_product_id,location_code,updated_at) VALUES ($1,$2,$3,$4,'',CURRENT_TIMESTAMP)
			ON CONFLICT (sku,channel) DO UPDATE SET channel_sku=EXCLUDED.channel_sku,external_product_id=EXCLUDED.external_product_id,updated_at=CURRENT_TIMESTAMP`, schema),
			p.row.Item, channel, chSKU, p.ref.Ref); err != nil {
			return 0, 0, err
		}
		if shop {
			var design string
			err = tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'family', ''), data->>'parent_product_code', '') FROM %s.documents WHERE doctype = 'Item' AND id = $1`, schema), p.row.Item).Scan(&design)
			if err != nil && err != sql.ErrNoRows {
				return 0, 0, err
			}
			shopLinks = append(shopLinks, ShopifyVariantLink{SKU: p.row.Item, Barcode: p.ref.Barcode, ProductGID: p.ref.ProductRef, VariantGID: p.ref.Ref, InventoryItemID: p.ref.InventoryRef, Price: p.ref.Price, CompareAt: p.ref.CompareAt})
			shopDesign = append(shopDesign, design)
		}
		linked++
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	for i, v := range shopLinks {
		if err = upsertShopifyLink(schema, channel, shopDesign[i], v); err != nil {
			return linked, skipped, err
		}
	}
	LogAuditEvent(tenantID, actor, "INTEGRATION", "CHANNEL_LISTINGS_LINKED", fmt.Sprintf("channel %s: %d linked by SKU or barcode, %d left", channel, linked, skipped))
	return linked, skipped, nil
}

// SetListingStockSync turns the stock push for one listing on or off.
func SetListingStockSync(tenantID, channel, ref string, on bool, actor string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var sku, chSKU string
	err = db.DB.QueryRow(fmt.Sprintf(`UPDATE %s.channel_listings SET stock_sync = $3, updated_at = CURRENT_TIMESTAMP WHERE channel = $1 AND listing_ref = $2 RETURNING sku, channel_sku`, schema),
		channel, ref, on).Scan(&sku, &chSKU)
	if err == sql.ErrNoRows {
		return listingErr("INTEGR-4801", "That listing is not on the list. Pull the channel's listings first.")
	}
	if err != nil {
		return err
	}
	word := "on"
	if !on {
		word = "off"
	}
	LogAuditEvent(tenantID, actor, "INTEGRATION", "CHANNEL_LISTING_STOCK_SYNC", fmt.Sprintf("channel %s: stock sync %s for listing %s", channel, word, chSKU))
	return nil
}

// listingStockOff is the ERP items whose listing on this channel has the stock
// push switched off. Empty when nobody pulled or switched anything off.
func listingStockOff(tenantID, channel string) map[string]bool {
	out := map[string]bool{}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT sku FROM %s.channel_listings WHERE channel = $1 AND NOT stock_sync AND sku <> ''`, schema), channel)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			out[s] = true
		}
	}
	return out
}
