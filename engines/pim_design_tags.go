package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"strings"
)

// Stage 146.5.5 (tags part): add or remove tags on the content of many
// products in one go. Targets are a product group's members and / or designs
// (the design item and its variants). The tags are those of the product's
// content in one language (en unless asked), the same tags feeds and exports
// read. The write goes through BulkUpdateDocuments, so an edit meets every
// guard a hand edit meets: per-document validation, field permissions, and
// re-approval on edit for approved content.

// ItemTagRequest names the products and the tags.
type ItemTagRequest struct {
	Group    string   `json:"group"`
	Designs  []string `json:"designs"`
	Action   string   `json:"action"` // add | remove
	Tags     string   `json:"tags"`   // comma-separated
	Language string   `json:"language"`
}

// ItemTagResult counts what a bulk tag change did.
type ItemTagResult struct {
	Products  int      `json:"products"`
	Changed   int      `json:"changed"`
	Unchanged int      `json:"unchanged"`
	NoContent int      `json:"no_content"`
	Codes     []string `json:"codes"`
}

// tagList splits a comma-separated tag string, trimmed, without blanks.
func tagList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// applyTags returns the new comma-separated tags after adding or removing
// (case-insensitively) and whether anything changed.
func applyTags(current, action string, tags []string) (string, bool) {
	have := tagList(current)
	switch action {
	case "add":
		seen := map[string]bool{}
		for _, h := range have {
			seen[strings.ToLower(h)] = true
		}
		changed := false
		for _, t := range tags {
			if k := strings.ToLower(t); !seen[k] {
				seen[k] = true
				have = append(have, t)
				changed = true
			}
		}
		if !changed {
			return current, false
		}
		return strings.Join(have, ","), true
	default: // remove
		drop := map[string]bool{}
		for _, t := range tags {
			drop[strings.ToLower(t)] = true
		}
		kept := make([]string, 0, len(have))
		for _, h := range have {
			if !drop[strings.ToLower(h)] {
				kept = append(kept, h)
			}
		}
		if len(kept) == len(have) {
			return current, false
		}
		return strings.Join(kept, ","), true
	}
}

// BulkTagItems adds or removes tags on the content of the chosen products.
func BulkTagItems(tenantID, user, role string, req ItemTagRequest) (*ItemTagResult, error) {
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "add" && action != "remove" {
		return nil, templateErr("PIM-0970", "Action", "choose add or remove")
	}
	tags := tagList(req.Tags)
	if len(tags) == 0 {
		return nil, templateErr("PIM-0970", "Tags", "enter at least one tag")
	}
	lang := strings.TrimSpace(req.Language)
	if lang == "" {
		lang = "en"
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	set := map[string]bool{}
	if g := strings.TrimSpace(req.Group); g != "" {
		members, gErr := ResolvePIMProductGroupItemCodes(tenantID, g)
		if gErr != nil {
			return nil, gErr
		}
		for _, m := range members {
			set[m] = true
		}
	}
	for _, d := range req.Designs {
		if d = strings.TrimSpace(d); d == "" {
			continue
		}
		if ok, _ := verifyDocumentExists(tenantID, "Item", d); !ok {
			return nil, templateErr("PIM-0961", "Design", fmt.Sprintf("design %q is not an item", d))
		}
		set[d] = true
		rows, qerr := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Item' AND deleted_at IS NULL
			AND data->>'parent_product_code' = $1`, schema), d)
		if qerr != nil {
			return nil, qerr
		}
		for rows.Next() {
			var v string
			if rows.Scan(&v) == nil {
				set[v] = true
			}
		}
		rows.Close()
	}
	if len(set) == 0 {
		return nil, templateErr("PIM-0959", "Designs", "choose a product group or at least one design")
	}
	if max := maxPIMBulkEditDocumentsFor(tenantID); len(set) > max {
		return nil, templateErr("PIM-0962", "Designs", fmt.Sprintf("%d products chosen; one bulk change takes at most %d", len(set), max))
	}
	items := make([]string, 0, len(set))
	for it := range set {
		items = append(items, it)
	}
	sort.Strings(items)

	res := &ItemTagResult{Products: len(items), Codes: []string{}}
	byNew := map[string][]string{} // new tags -> content ids
	owner := map[string]string{}   // content id -> item
	for _, it := range items {
		var id, cur string
		qerr := db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(data->>'tags','') FROM %s.documents
			WHERE doctype = 'ProductContent' AND deleted_at IS NULL AND data->>'product_id' = $1
			AND COALESCE(NULLIF(data->>'language',''),'en') = $2 ORDER BY id LIMIT 1`, schema), it, lang).Scan(&id, &cur)
		if qerr != nil {
			res.NoContent++
			continue
		}
		next, changed := applyTags(cur, action, tags)
		if !changed {
			res.Unchanged++
			continue
		}
		byNew[next] = append(byNew[next], id)
		owner[id] = it
	}
	keys := make([]string, 0, len(byNew))
	for k := range byNew {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ids := byNew[k]
		if _, uErr := BulkUpdateDocuments(tenantID, "ProductContent", ids, "tags", k, user, role); uErr != nil {
			return res, uErr
		}
		for _, id := range ids {
			res.Changed++
			res.Codes = append(res.Codes, owner[id])
			LogAuditEvent(tenantID, user, "pim.item_tag."+action, "Success", fmt.Sprintf("%s: %s", owner[id], strings.Join(tags, ",")))
		}
	}
	sort.Strings(res.Codes)
	return res, nil
}
