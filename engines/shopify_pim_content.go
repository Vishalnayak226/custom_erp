package engines

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 146.5.1 (D13): Shopify gets the approved PIM content.
//
// Behind shopify.publish_pim_content (default off: the payload is exactly what
// 99.3 always sent). On, a publish also carries the approved ProductContent
// (title, description, SEO title and description, tags), the product type, the
// PIM attribute values as custom.<attribute_code> metafields (design values on
// the product, combination - "Variant" - values on the variant), and re-sends
// the pictures when they have changed. Only approved content goes out: no
// approved ProductContent, no content fields; only Active attribute values.

const shopifyPIMContentSetting = "shopify.publish_pim_content"

func init() {
	RegisterSetting(SettingDefinition{
		Key: shopifyPIMContentSetting, Module: "PIM / PIMS", Label: "Shopify gets the approved PIM content", Type: SettingTypeBool, Default: "false",
		Description: "On: a Shopify publish also sends the approved title, description, SEO, tags, product type, attribute values (custom.<attribute code>) and changed pictures. Off: only the design name, description, brand and HSN.",
	})
}

type shopifyPIMContent struct {
	Title, Description, SEOTitle, SEODescription, ProductType string
	Tags                                                      []string
	ProductMF                                                 []ShopifyMetafield
	VariantMF                                                 map[string][]ShopifyMetafield
}

var (
	shopifyHTMLTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	shopifyKeyInvalid = regexp.MustCompile(`[^a-z0-9_.-]+`)
)

// shopifyMetafieldKey makes an attribute code a valid metafield key (3 to 64
// characters of a-z 0-9 _ - .).
func shopifyMetafieldKey(code string) string {
	key := strings.Trim(shopifyKeyInvalid.ReplaceAllString(strings.ToLower(strings.TrimSpace(code)), "_"), "_")
	for len(key) < 3 && key != "" {
		key += "_x"
	}
	if len(key) > 64 {
		key = key[:64]
	}
	return key
}

func shopifyPlainText(s string, max int) string {
	s = strings.Join(strings.Fields(html.UnescapeString(shopifyHTMLTag.ReplaceAllString(s, " "))), " ")
	if len(s) > max {
		s = strings.TrimSpace(s[:max])
	}
	return s
}

func shopifyTagList(csv string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range strings.Split(csv, ",") {
		t = strings.TrimSpace(t)
		if t == "" || len(t) > 255 || seen[strings.ToLower(t)] || len(out) >= 250 {
			continue
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
	}
	return out
}

// loadShopifyPIMContent reads what a Shopify publish of this design may carry
// from the PIM. owners are the design code (when it is a design) then its
// item codes.
func loadShopifyPIMContent(tenantID, schema, channel, design string, skus []string) (*shopifyPIMContent, error) {
	pc := &shopifyPIMContent{VariantMF: map[string][]ShopifyMetafield{}}
	locale := "en"
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'default_locale', ''), 'en') FROM %s.documents WHERE doctype = 'Channel' AND id = $1`, schema), channel).Scan(&locale)

	// Approved content: the design's, else the first combination that has some.
	owners := []string{}
	if design != "" {
		owners = append(owners, design)
	}
	owners = append(owners, skus...)
	for _, owner := range owners {
		var title, shortDesc, longDesc, seoTitle, tags string
		err := db.DB.QueryRow(fmt.Sprintf(`
			SELECT COALESCE(data->>'title', ''), COALESCE(data->>'short_desc', ''), COALESCE(data->>'long_desc', ''),
			       COALESCE(data->>'seo_title', ''), COALESCE(data->>'tags', '')
			FROM %s.documents WHERE doctype = 'ProductContent' AND deleted_at IS NULL AND data->>'product_id' = $1
			  AND data->>'language' = $2 AND status = 'Approved'
			ORDER BY updated_at DESC LIMIT 1`, schema), owner, locale).Scan(&title, &shortDesc, &longDesc, &seoTitle, &tags)
		if err != nil {
			continue
		}
		pc.Title = strings.TrimSpace(title)
		pc.Description = firstNonEmpty(strings.TrimSpace(longDesc), strings.TrimSpace(shortDesc))
		pc.SEOTitle = shopifyPlainText(seoTitle, 70)
		pc.SEODescription = shopifyPlainText(firstNonEmpty(shortDesc, longDesc), 320)
		pc.Tags = shopifyTagList(tags)
		break
	}

	// Product type: the channel's name for the item's category, else the category.
	if len(skus) > 0 {
		if item, _, _ := fetchDocData(tenantID, "Item", skus[0]); item != nil {
			if cat := strField(item, "category"); cat != "" {
				mapped := ""
				_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'channel_category', '') FROM %s.documents
					WHERE doctype = 'ChannelCategoryMap' AND deleted_at IS NULL AND data->>'channel' = $1 AND data->>'erp_category' = $2 LIMIT 1`, schema), channel, cat).Scan(&mapped)
				pc.ProductType = firstNonEmpty(mapped, cat)
			}
		}
	}

	// Attribute values as metafields.
	type attrDef struct{ key, level string }
	defs := map[string]attrDef{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'code', ''), COALESCE(data->>'used_for', '') FROM %s.documents
		WHERE doctype = 'ProductAttributeDef' AND deleted_at IS NULL AND COALESCE(status, 'Active') = 'Active'`, schema))
	if err != nil {
		return pc, err
	}
	for rows.Next() {
		var id, code, usedFor string
		if rows.Scan(&id, &code, &usedFor) == nil {
			d := attrDef{key: shopifyMetafieldKey(firstNonEmpty(code, id)), level: "product"}
			if usedFor == "Variant" {
				d.level = "variant"
			}
			defs[id] = d
			if code != "" {
				defs[code] = d
			}
		}
	}
	rows.Close()
	if len(defs) == 0 {
		return pc, nil
	}
	type cell struct{ value, locale, channel string }
	values := map[string]map[string][]cell{} // owner -> attribute key -> candidates
	vrows, err := db.DB.Query(fmt.Sprintf(`SELECT data->>'item', data->>'attribute', COALESCE(data->>'value', ''), COALESCE(data->>'locale', ''), COALESCE(data->>'channel', '')
		FROM %s.documents WHERE doctype = 'ProductAttributeValue' AND deleted_at IS NULL AND COALESCE(status, 'Active') = 'Active'
		  AND data->>'item' = ANY($1)`, schema), "{"+strings.Join(pgArrayEscape(owners), ",")+"}")
	if err != nil {
		return pc, err
	}
	for vrows.Next() {
		var item, attr string
		var c cell
		if vrows.Scan(&item, &attr, &c.value, &c.locale, &c.channel) == nil {
			d, ok := defs[attr]
			if !ok || strings.TrimSpace(c.value) == "" {
				continue
			}
			if (c.locale != "" && c.locale != locale) || (c.channel != "" && c.channel != channel) {
				continue
			}
			if values[item] == nil {
				values[item] = map[string][]cell{}
			}
			values[item][d.key] = append(values[item][d.key], c)
		}
	}
	vrows.Close()
	// the same priority ResolveAttributeValue uses: locale match, then channel match
	pick := func(cands []cell) string {
		best, bestScore := "", -1
		for _, c := range cands {
			score := 0
			if c.locale == locale {
				score += 2
			}
			if c.channel == channel {
				score++
			}
			if score > bestScore {
				best, bestScore = c.value, score
			}
		}
		return strings.Join(strings.Fields(best), " ")
	}
	level := map[string]string{}
	for _, d := range defs {
		level[d.key] = d.level
	}
	keys := make([]string, 0, len(level))
	for k := range level {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if level[key] == "variant" {
			for _, sku := range skus {
				if v := pick(values[sku][key]); v != "" {
					pc.VariantMF[sku] = append(pc.VariantMF[sku], ShopifyMetafield{Key: key, Value: v})
				}
			}
			continue
		}
		for _, owner := range owners { // the design's value, else the first item that has one
			if v := pick(values[owner][key]); v != "" {
				pc.ProductMF = append(pc.ProductMF, ShopifyMetafield{Key: key, Value: v})
				break
			}
		}
	}
	return pc, nil
}

func pgArrayEscape(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
	}
	return out
}

// ensureShopifyMetafieldDefs creates, once, the custom.<key> definitions this
// publish uses. A refusal (for example the app lacks the definitions scope) is
// noted and the publish goes on: the metafield is still set without one.
func ensureShopifyMetafieldDefs(ctx context.Context, tenantID, channel string, cred map[string]string, productKeys, variantKeys []string) {
	_, cur, err := LoadShopifyChannel(tenantID, channel)
	if err != nil {
		return
	}
	have := map[string]bool{}
	for _, d := range cur.MetafieldDefs {
		have[d] = true
	}
	changed := false
	for _, group := range []struct {
		owner string
		keys  []string
	}{{"PRODUCT", productKeys}, {"PRODUCTVARIANT", variantKeys}} {
		missing := []string{}
		for _, k := range group.keys {
			if !have[group.owner+":"+k] {
				missing = append(missing, k)
			}
		}
		if len(missing) == 0 {
			continue
		}
		made, err := shopifyConn().EnsureMetafieldDefinitions(ctx, cred, group.owner, missing)
		for _, k := range made {
			cur.MetafieldDefs = append(cur.MetafieldDefs, group.owner+":"+k)
			changed = true
		}
		if err != nil {
			LogSystemError(tenantID, "", "WARN", "shopify.publish", fmt.Sprintf("channel %s: metafield definitions not created: %v", channel, err), "")
		}
	}
	if changed {
		_ = saveShopifySettings(tenantID, channel, cur)
	}
}

// shopifyImagesHash fingerprints the pictures a design and its combinations
// have, so a change is noticed (a picture added, replaced, reordered or removed).
func shopifyImagesHash(tenantID, design string, skus []string) string {
	h := sha256.New()
	add := func(owner string) {
		list, err := ListOwnerImages(tenantID, owner)
		if err != nil {
			return
		}
		for _, m := range list {
			fmt.Fprintf(h, "%s|%s|%s|%d;", owner, m.ID, m.Checksum, m.VersionNo)
		}
	}
	if design != "" {
		add(design)
	}
	for _, s := range skus {
		add(s)
	}
	return hex.EncodeToString(h.Sum(nil))
}
