package engines

import (
	"fmt"
	"math"
	"strings"

	"custom_erp/db"
)

// Digital-shelf drift: where a channel listing no longer matches the item
// master. Only stored data is compared (the last listing pull, and the payload
// of the last publish), so the report makes no outside call. A listing the
// ERP cannot see the stock or the image count of is not judged on those: the
// Shopify listing read carries title, price, compare-at price and state only.
//
// Kinds: Title, Price, MRP, Status (listing gone while the item is active, or
// listed while the item is not), Images (the item has a different number of
// images than the last publish sent).

// Error codes PIM-9260 to PIM-9269.
const listingDriftCodeChannel = "PIM-9260"

// ListingDriftRow is one difference on one listing.
type ListingDriftRow struct {
	Channel    string `json:"channel"`
	ListingRef string `json:"listing_ref"`
	SKU        string `json:"sku"`
	ItemName   string `json:"item_name"`
	Kind       string `json:"kind"`
	Listing    string `json:"listing"`
	Master     string `json:"master"`
	CanPublish bool   `json:"can_publish"` // a republish carries it (titles and images, not prices)
}

func sameText(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}

func sameMoney(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func rupees(v float64) string { return fmt.Sprintf("%.2f", v) }

// ChannelDrift compares the channel's stored listings that are linked to an
// item with the item master.
func ChannelDrift(tenantID, channel string) ([]ListingDriftRow, error) {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return nil, &ValidationError{Code: listingDriftCodeChannel, SubFor: "channel", Message: "choose a channel"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rs, err := db.DB.Query(fmt.Sprintf(`SELECT listing_ref, sku, title, price, compare_at, state FROM %s.channel_listings
		WHERE channel = $1 AND sku <> '' ORDER BY sku, listing_ref LIMIT 5000`, schema), channel)
	if err != nil {
		return nil, err
	}
	type stored struct {
		ref, sku, title, state string
		price, compareAt       float64
	}
	var listings []stored
	for rs.Next() {
		var l stored
		if err := rs.Scan(&l.ref, &l.sku, &l.title, &l.price, &l.compareAt, &l.state); err != nil {
			rs.Close()
			return nil, err
		}
		listings = append(listings, l)
	}
	rs.Close()
	if err := rs.Err(); err != nil {
		return nil, err
	}

	day := BusinessToday()
	out := []ListingDriftRow{}
	for _, l := range listings {
		res, rerr := ResolveItemBySKU(tenantID, l.sku)
		if rerr != nil {
			continue // the linked item is gone: nothing to compare against
		}
		item := res.Data
		name := strField(item, "name")
		add := func(kind, listing, master string, publish bool) {
			out = append(out, ListingDriftRow{Channel: channel, ListingRef: l.ref, SKU: l.sku, ItemName: name, Kind: kind, Listing: listing, Master: master, CanPublish: publish})
		}
		active := itemActive(item)
		switch {
		case l.state == "Gone" && active:
			add("Status", "Gone from the channel", "Active", true)
		case l.state == "Listed" && !active:
			add("Status", "Listed", "Not active", false)
		}
		if l.state != "Listed" {
			continue
		}
		if name != "" && !sameText(l.title, name) {
			add("Title", l.title, name, true)
		}
		mrp := numFromInterface(item["mrp"])
		want := numFromInterface(item["sale_price"])
		if want <= 0 {
			want = mrp
		}
		if p, ok := MarkdownPrice(tenantID, l.sku, "", day, want); ok {
			want = p
		}
		if want > 0 && !sameMoney(l.price, want) {
			add("Price", rupees(l.price), rupees(want), false)
		}
		wantCompare := 0.0
		if mrp > want {
			wantCompare = mrp
		}
		if want > 0 && !sameMoney(l.compareAt, wantCompare) {
			add("MRP", rupees(l.compareAt), rupees(wantCompare), false)
		}
		if d, derr := PreviewChannelDiff(tenantID, l.sku, channel); derr == nil && d.HasPriorSnapshot {
			for _, f := range d.Fields {
				if f.Field == "image_count" && f.Changed {
					add("Images", f.Old+" sent", f.New+" now", true)
				}
			}
		}
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "listing-drift", Label: "Listing drift", Category: "PIM", ModuleKey: "pim",
		Columns: []ReportColumn{
			{Key: "channel", Label: "Channel"},
			{Key: "sku", Label: "Item"},
			{Key: "item_name", Label: "Name"},
			{Key: "kind", Label: "Differs"},
			{Key: "listing", Label: "Listing"},
			{Key: "master", Label: "Item master"},
		},
		Params: []ReportParam{{Key: "channel", Label: "Channel", Type: "text", Required: true}}, // 245.2: asked before the run
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := ChannelDrift(tenantID, params["channel"])
			if err != nil {
				return nil, err
			}
			out := make([]map[string]interface{}, 0, len(rows))
			for _, r := range rows {
				out = append(out, map[string]interface{}{"channel": r.Channel, "sku": r.SKU, "item_name": r.ItemName, "kind": r.Kind, "listing": r.Listing, "master": r.Master})
			}
			return out, nil
		},
	})
}
