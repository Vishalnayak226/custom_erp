package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
)

// Stage 146.5.13: PIM small slices - attribute fill rate, brand vocabulary
// check, and the multi-SKU paste split used by the workbench search.

// maxSKUPaste bounds how many codes one paste search takes.
const maxSKUPaste = 500

// ParseSKUPaste splits pasted text (commas, semicolons, tabs, spaces, new
// lines) into unique codes in first-seen order, at most maxSKUPaste.
func ParseSKUPaste(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ';' || r == '\t' || r == ' ' || r == '\n' || r == '\r'
	})
	seen := map[string]bool{}
	out := []string{}
	for _, f := range fields {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
		if len(out) >= maxSKUPaste {
			break
		}
	}
	return out
}

// AttributeFillRate lists, per attribute, how many active items carry a
// non-empty base value.
func AttributeFillRate(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var total int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype='Item' AND deleted_at IS NULL AND COALESCE(status,'') <> 'Inactive'`, schema)).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT d.id, COALESCE(d.data->>'label', d.id),
		(SELECT COUNT(DISTINCT v.data->>'item') FROM %[1]s.documents v
		  JOIN %[1]s.documents i ON i.doctype='Item' AND i.id = v.data->>'item' AND i.deleted_at IS NULL AND COALESCE(i.status,'') <> 'Inactive'
		  WHERE v.doctype='ProductAttributeValue' AND v.deleted_at IS NULL
		    AND v.data->>'attribute' = d.id AND COALESCE(v.data->>'locale','') = '' AND COALESCE(v.data->>'channel','') = ''
		    AND COALESCE(v.data->>'value','') <> '')
		FROM %[1]s.documents d WHERE d.doctype='ProductAttributeDef' AND d.deleted_at IS NULL ORDER BY d.id LIMIT 1000`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, label string
		var filled int
		if err := rows.Scan(&id, &label, &filled); err != nil {
			return nil, err
		}
		pct := 0.0
		if total > 0 {
			pct = float64(int(float64(filled)/float64(total)*1000+0.5)) / 10
		}
		out = append(out, map[string]interface{}{"attribute": id, "label": label, "items": total, "filled": filled, "missing": total - filled, "fill_pct": pct})
	}
	return out, rows.Err()
}

// BrandVocabularyCheck lists the brand values written on items that are not
// spelt exactly like a Brand master name or code, with the master spelling
// they most likely meant.
func BrandVocabularyCheck(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	master := map[string]string{} // normalised -> master name
	exact := map[string]bool{}
	mrows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'code',''), COALESCE(data->>'name','') FROM %s.documents WHERE doctype='Brand' AND deleted_at IS NULL`, schema))
	if err != nil {
		return nil, err
	}
	for mrows.Next() {
		var code, name string
		if mrows.Scan(&code, &name) == nil {
			canon := name
			if canon == "" {
				canon = code
			}
			exact[code], exact[name] = true, true
			master[strings.ToLower(strings.TrimSpace(code))] = canon
			master[strings.ToLower(strings.TrimSpace(name))] = canon
		}
	}
	mrows.Close()
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT BTRIM(data->>'brand'), COUNT(*) FROM %s.documents
		WHERE doctype='Item' AND deleted_at IS NULL AND BTRIM(COALESCE(data->>'brand','')) <> '' GROUP BY 1 ORDER BY 1 LIMIT 2000`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var v string
		var n int
		if err := rows.Scan(&v, &n); err != nil {
			return nil, err
		}
		if exact[v] {
			continue
		}
		out = append(out, map[string]interface{}{"brand_value": v, "items": n, "suggestion": master[strings.ToLower(v)]})
	}
	return out, rows.Err()
}

func init() {
	RegisterReport(ReportDefinition{ID: "pim-attribute-fill-rate", Label: "Attribute fill rate", Category: "PIM", StoreKey: "-",
		Columns: []ReportColumn{{Key: "attribute", Label: "Attribute"}, {Key: "label", Label: "Label"}, {Key: "items", Label: "Items"},
			{Key: "filled", Label: "Filled"}, {Key: "missing", Label: "Missing"}, {Key: "fill_pct", Label: "Filled %"}},
		Run: func(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
			return AttributeFillRate(tenantID)
		}})
	RegisterReport(ReportDefinition{ID: "pim-brand-vocabulary", Label: "Brand vocabulary check", Category: "PIM", StoreKey: "-",
		Columns: []ReportColumn{{Key: "brand_value", Label: "Brand on items"}, {Key: "items", Label: "Items"}, {Key: "suggestion", Label: "Brand master spelling"}},
		Run: func(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
			return BrandVocabularyCheck(tenantID)
		}})
}

// ItemLiveOn lists the channels an item is published on (the latest publish
// log row per channel says Published), with the channel's page address built
// from Channel.item_url_template when it has one.
func ItemLiveOn(tenantID, itemCode string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT l.channel_code, COALESCE(l.external_id,''), COALESCE(to_char(l.created_at,'YYYY-MM-DD HH24:MI'),''),
		COALESCE((SELECT c.data->>'item_url_template' FROM %[1]s.documents c WHERE c.doctype='Channel' AND c.id = l.channel_code AND c.deleted_at IS NULL),'')
		FROM (SELECT DISTINCT ON (channel_code) * FROM %[1]s.pim_publish_log WHERE item_code = $1 ORDER BY channel_code, created_at DESC, id DESC) l
		WHERE l.status = 'Published' ORDER BY l.channel_code`, schema), itemCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var ch, ext, at, tpl string
		if err := rows.Scan(&ch, &ext, &at, &tpl); err != nil {
			return nil, err
		}
		url := ""
		if t := strings.TrimSpace(tpl); strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
			url = strings.NewReplacer("{item_code}", itemCode, "{external_id}", ext).Replace(t)
		}
		out = append(out, map[string]interface{}{"channel": ch, "external_id": ext, "published_at": at, "url": url})
	}
	return out, rows.Err()
}
