package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Stage 146.5.12: typed product relations go to Shopify as its standard
// product-recommendation metafields, with the rest of the approved PIM content
// (so only when shopify.publish_pim_content is on).
//
// Shopify has two lists: related products (alike - Similar, Alternative,
// Upsell, a better or neighbouring choice) and complementary products (bought
// with it - Complete the look, Cross-sell, Add-on). Only related items that
// are already published to this shop (a shopify_links row gives their
// product GID) can be referenced; the others are left out until they are
// published and this product is published again. Order follows the Order
// field. A product with no such relation gets no metafield, and on a product
// already on the shop that list is deleted (321.1), so recommendations the
// item no longer has do not linger.

const (
	shopifyRecommendationNamespace = "shopify--discovery--product_recommendation"
	shopifyRecommendationMax       = 25 // Shopify's list limit for these
)

var shopifyRelatedTypes = map[string]string{
	"Similar": "related_products", "Alternative": "related_products", "Upsell": "related_products",
	"Complete the look": "complementary_products", "Cross-sell": "complementary_products", "Add-on": "complementary_products",
}

// shopifyRelationMetafields builds the two metafields for a product made of
// owners (its design code and item codes). ownGID is the product itself when
// it is already on Shopify, never listed as its own recommendation. An error
// means the relations could not be read: send none and clear none.
func shopifyRelationMetafields(schema, channel string, owners []string, ownGID string) ([]ShopifyMetafield, error) {
	type rel struct {
		key, item string
		pos       float64
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'relation_type', ''), COALESCE(data->>'related_item', ''), COALESCE(data->>'position', '')
		FROM %s.documents WHERE doctype = 'ProductRelation' AND deleted_at IS NULL AND COALESCE(data->>'status', 'Active') = 'Active'
		  AND data->>'item' = ANY($1)`, schema), "{"+strings.Join(pgArrayEscape(owners), ",")+"}")
	if err != nil {
		return nil, err
	}
	var rels []rel
	for rows.Next() {
		var typ, other, pos string
		if rows.Scan(&typ, &other, &pos) != nil {
			continue
		}
		key, ok := shopifyRelatedTypes[typ]
		if !ok || other == "" {
			continue
		}
		p, _ := strconv.ParseFloat(strings.TrimSpace(pos), 64)
		rels = append(rels, rel{key, other, p})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(rels, func(i, j int) bool {
		if rels[i].pos != rels[j].pos {
			return rels[i].pos < rels[j].pos
		}
		return rels[i].item < rels[j].item
	})
	lists := map[string][]string{}
	seen := map[string]bool{}
	for _, r := range rels {
		if len(lists[r.key]) >= shopifyRecommendationMax {
			continue
		}
		var gid string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT product_gid FROM %s.shopify_links WHERE channel = $1 AND (sku = $2 OR design = $2) AND product_gid <> '' LIMIT 1`, schema),
			channel, r.item).Scan(&gid); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if gid == "" || gid == ownGID || seen[r.key+"|"+gid] {
			continue
		}
		seen[r.key+"|"+gid] = true
		lists[r.key] = append(lists[r.key], gid)
	}
	var out []ShopifyMetafield
	for _, key := range []string{"related_products", "complementary_products"} {
		if len(lists[key]) == 0 {
			continue
		}
		b, _ := json.Marshal(lists[key])
		out = append(out, ShopifyMetafield{Namespace: shopifyRecommendationNamespace, Key: key, Type: "list.product_reference", Value: string(b)})
	}
	return out, nil
}

// shopifyEmptyRelationLists names the recommendation lists a publish does not
// send (321.1); on a product already on the shop they are deleted.
func shopifyEmptyRelationLists(sent []ShopifyMetafield) []ShopifyMetafield {
	has := map[string]bool{}
	for _, m := range sent {
		has[m.Key] = true
	}
	var out []ShopifyMetafield
	for _, key := range []string{"related_products", "complementary_products"} {
		if !has[key] {
			out = append(out, ShopifyMetafield{Namespace: shopifyRecommendationNamespace, Key: key})
		}
	}
	return out
}
