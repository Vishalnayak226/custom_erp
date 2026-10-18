package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 89.5 - attributes mapped to the item's type (user, 2026-10-18: "red
// color will be attached to necklace. Once I order necklace it will show
// necklace related color. So, This will be a config to turn on and off. If Off
// all attribute will show to all ... By default it will be all ... flexible and
// dynamic with new master created ... Smart as well").
//
// The map lives on the masters themselves - ProductType.attribute_map and
// Category.attribute_map - as "COLOR:Red|Gold;POLISH" (an attribute with no
// values listed offers all of its values). With "Attributes follow the item's
// type" on, an item name offers its type's map, else its category's, else
// everything; off, everything. A new attribute or value created afterwards is
// therefore offered at once wherever nothing restricts it.

// ItemAttributeChoice is one attribute an item can carry, with the values it
// offers and where it is asked.
type ItemAttributeChoice struct {
	Code      string   `json:"code"`
	Label     string   `json:"label"`
	ValueType string   `json:"value_type"`
	Values    []string `json:"values"`
	Item      bool     `json:"item"`
	PO        bool     `json:"po"`
	GRN       string   `json:"grn"`
}

// ItemAttributeSet is what an item name offers, and why.
type ItemAttributeSet struct {
	Attributes []ItemAttributeChoice `json:"attributes"`
	// Source: "all" (mapping off or nothing mapped), "type" or "category".
	Source string `json:"source"`
	// MapOn names the master the map was read from (or would be set on).
	MapOn     string `json:"map_on,omitempty"`
	MapOnName string `json:"map_on_name,omitempty"`
}

// attributeMapEntry is one attribute in a map; Values nil = all of them.
type attributeMapEntry struct {
	Code   string
	Values []string
}

// ParseAttributeMap reads "COLOR:Red|Gold;POLISH".
func parseAttributeMap(s string) []attributeMapEntry {
	var out []attributeMapEntry
	seen := map[string]int{}
	for _, part := range strings.Split(s, ";") {
		code, vals, _ := strings.Cut(part, ":")
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		var values []string
		for _, v := range strings.Split(vals, "|") {
			if v = strings.TrimSpace(v); v != "" {
				values = append(values, v)
			}
		}
		if i, ok := seen[strings.ToUpper(code)]; ok {
			out[i].Values = append(out[i].Values, values...)
			continue
		}
		seen[strings.ToUpper(code)] = len(out)
		out = append(out, attributeMapEntry{Code: code, Values: values})
	}
	return out
}

func formatAttributeMap(entries []attributeMapEntry) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		if len(e.Values) == 0 {
			parts = append(parts, e.Code)
		} else {
			parts = append(parts, e.Code+":"+strings.Join(e.Values, "|"))
		}
	}
	return strings.Join(parts, ";")
}

// masterAttributeMap reads a ProductType / Category record's map.
func masterAttributeMap(schema, doctype, ref string) (id, name, raw string, ok bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", "", false
	}
	var m, n sql.NullString
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, data->>'name', data->>'attribute_map' FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND (id = $2 OR data->>'code' = $2 OR lower(data->>'name') = lower($2))
		ORDER BY (id = $2) DESC LIMIT 1`, schema), doctype, ref).Scan(&id, &n, &m)
	if err != nil {
		return "", "", "", false
	}
	return id, n.String, strings.TrimSpace(m.String), true
}

// ItemAttributesFor is what an item name offers (itemNameRef may be blank: all).
func ItemAttributesFor(tenantID, itemNameRef string) (*ItemAttributeSet, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	attrs, err := activeItemAttributes(schema)
	if err != nil {
		return nil, err
	}
	rules, err := ItemFieldRules(tenantID)
	if err != nil {
		return nil, err
	}
	placed := map[string]ItemFieldRule{}
	for _, r := range rules {
		placed[r.Key] = r
	}
	set := &ItemAttributeSet{Source: "all", Attributes: []ItemAttributeChoice{}}
	var entries []attributeMapEntry
	if rec, ok, err := loadItemNameRecord(schema, itemNameRef); err != nil {
		return nil, err
	} else if ok {
		if id, name, raw, found := masterAttributeMap(schema, "ProductType", rec.ProductTypeRef); found {
			set.MapOn, set.MapOnName = "ProductType:"+id, name
			if raw != "" && GetSettingBool(tenantID, SettingAttributeTypeMapping) {
				entries, set.Source = parseAttributeMap(raw), "type"
			}
		}
		if set.Source == "all" {
			if id, name, raw, found := masterAttributeMap(schema, "Category", rec.CategoryRef); found {
				if set.MapOn == "" {
					set.MapOn, set.MapOnName = "Category:"+id, name
				}
				if raw != "" && GetSettingBool(tenantID, SettingAttributeTypeMapping) {
					entries, set.Source = parseAttributeMap(raw), "category"
					set.MapOn, set.MapOnName = "Category:"+id, name
				}
			}
		}
	}
	byCode := map[string]attributeMapEntry{}
	for _, e := range entries {
		byCode[strings.ToUpper(e.Code)] = e
	}
	for _, a := range attrs {
		rule := placed["attr:"+a.Code]
		if rule.Hidden {
			continue
		}
		choice := ItemAttributeChoice{Code: a.Code, Label: a.Label, ValueType: a.ValueType, Values: a.Values, Item: rule.Item, PO: rule.PO, GRN: rule.GRN}
		if set.Source != "all" {
			e, listed := byCode[strings.ToUpper(a.Code)]
			if !listed {
				continue
			}
			if len(e.Values) > 0 && a.ValueType == "Select" {
				var kept []string
				for _, v := range a.Values {
					for _, want := range e.Values {
						if strings.EqualFold(v, want) {
							kept = append(kept, v)
							break
						}
					}
				}
				choice.Values = kept
			}
		}
		if choice.Values == nil {
			choice.Values = []string{}
		}
		set.Attributes = append(set.Attributes, choice)
	}
	return set, nil
}

// AttributeMapSuggestion is what existing items of a type (or category)
// already use - "where to map", worked out from the data (89.5.4).
type AttributeMapSuggestion struct {
	Code   string               `json:"code"`
	Label  string               `json:"label"`
	Items  int                  `json:"items"`
	Values []AttributeValueUsed `json:"values"`
}

// AttributeValueUsed is one value and how many items carry it.
type AttributeValueUsed struct {
	Value string `json:"value"`
	Items int    `json:"items"`
}

// SuggestAttributeMap reads the variant options of every item whose item name
// is of this Product Type (or Category).
func SuggestAttributeMap(tenantID, doctype, ref string) ([]AttributeMapSuggestion, error) {
	if doctype != "ProductType" && doctype != "Category" {
		return nil, &ValidationError{Code: "MASTER-0890", Message: "Attributes are mapped on a Product Type or a Category."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	id, _, _, ok := masterAttributeMap(schema, doctype, ref)
	if !ok {
		return []AttributeMapSuggestion{}, nil
	}
	field := "product_type"
	if doctype == "Category" {
		field = "category"
	}
	var code sql.NullString
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'code' FROM %s.documents WHERE id = $1`, schema), id).Scan(&code)
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH names AS (
			SELECT id, data->>'code' AS code FROM %[1]s.documents
			 WHERE doctype = 'ItemName' AND deleted_at IS NULL AND data->>'%[2]s' IN ($1, $2)),
		designs AS (
			SELECT d.id, d.data->>'code' AS code FROM %[1]s.documents d
			 WHERE d.doctype = 'ProductFamily' AND d.deleted_at IS NULL
			   AND d.data->>'item_name' IN (SELECT id FROM names UNION SELECT code FROM names))
		SELECT i.data->>'variant_option_values' FROM %[1]s.documents i
		 WHERE i.doctype = 'Item' AND i.deleted_at IS NULL AND COALESCE(i.data->>'variant_option_values', '') <> ''
		   AND (i.data->>'item_name' IN (SELECT id FROM names UNION SELECT code FROM names)
		        OR i.data->>'family' IN (SELECT id FROM designs UNION SELECT code FROM designs))`, schema, field), id, code.String)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type tally struct {
		items  int
		values map[string]int
	}
	counts := map[string]*tally{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		for _, pair := range strings.Split(raw, ";") {
			k, v, ok := strings.Cut(pair, ":")
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			if !ok || k == "" || v == "" {
				continue
			}
			t := counts[strings.ToUpper(k)]
			if t == nil {
				t = &tally{values: map[string]int{}}
				counts[strings.ToUpper(k)] = t
			}
			t.items++
			t.values[v]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	attrs, err := activeItemAttributes(schema)
	if err != nil {
		return nil, err
	}
	var out []AttributeMapSuggestion
	for _, a := range attrs {
		t := counts[strings.ToUpper(a.Code)]
		if t == nil {
			continue
		}
		s := AttributeMapSuggestion{Code: a.Code, Label: a.Label, Items: t.items}
		for v, n := range t.values {
			s.Values = append(s.Values, AttributeValueUsed{Value: v, Items: n})
		}
		sort.Slice(s.Values, func(i, j int) bool {
			if s.Values[i].Items != s.Values[j].Items {
				return s.Values[i].Items > s.Values[j].Items
			}
			return s.Values[i].Value < s.Values[j].Value
		})
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Items > out[j].Items })
	return out, nil
}

// MapAttributeValueToItemName adds a value that was just created for an item
// (from its form, a PO or a GRN) to the map it was offered under, so the next
// item of that type offers it too. Only when mapping is on and the map lists
// that attribute's values one by one; "all values" maps need nothing.
func MapAttributeValueToItemName(tenantID, userID, itemNameRef, code, value string) error {
	if !GetSettingBool(tenantID, SettingAttributeTypeMapping) || strings.TrimSpace(itemNameRef) == "" || strings.TrimSpace(value) == "" {
		return nil
	}
	set, err := ItemAttributesFor(tenantID, itemNameRef)
	if err != nil || set.Source == "all" {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	doctype, id, _ := strings.Cut(set.MapOn, ":")
	var raw sql.NullString
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'attribute_map' FROM %s.documents WHERE id = $1`, schema), id).Scan(&raw); err != nil {
		return err
	}
	entries := parseAttributeMap(raw.String)
	changed := false
	for i, e := range entries {
		if !strings.EqualFold(e.Code, code) || len(e.Values) == 0 {
			continue
		}
		for _, v := range e.Values {
			if strings.EqualFold(v, value) {
				return nil
			}
		}
		entries[i].Values = append(entries[i].Values, value)
		changed = true
	}
	if !changed {
		return nil
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = jsonb_set(data, '{attribute_map}', to_jsonb($2::text)),
		updated_at = CURRENT_TIMESTAMP, version = version + 1 WHERE id = $1`, schema), id, formatAttributeMap(entries)); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "ATTRIBUTE_MAP_VALUE_ADDED", "SUCCESS", fmt.Sprintf("%s %s: %s %s", doctype, id, code, value))
	return nil
}

// attributeChoiceCodes indexes an item name's attributes by code - the PO
// asks only those set to be asked in the PO.
func attributeChoiceCodes(set *ItemAttributeSet, po bool) map[string]ItemAttributeChoice {
	out := map[string]ItemAttributeChoice{}
	for _, a := range set.Attributes {
		if po && !a.PO {
			continue
		}
		out[strings.ToUpper(a.Code)] = a
	}
	return out
}

// CreateAttributeListFromField turns a free-text Item field into a list
// (89.3.1): an Item Attribute named like the field, holding every value items
// already carry in it (and a Select field's options), linked to the field. The
// items keep their values; from then on the field is a dropdown of that list.
func CreateAttributeListFromField(tenantID, userID, by, field string) (ItemFieldRule, error) {
	rules, err := ItemFieldRules(tenantID)
	if err != nil {
		return ItemFieldRule{}, err
	}
	var rule *ItemFieldRule
	for i := range rules {
		if rules[i].Kind == "field" && rules[i].Key == field {
			rule = &rules[i]
		}
	}
	if rule == nil || rule.Fixed || (rule.Type != "Data" && rule.Type != "Select") {
		return ItemFieldRule{}, &ValidationError{Code: "MASTER-0890", Message: fmt.Sprintf("%q is not a field that can be chosen from a list.", field)}
	}
	if rule.List != "" {
		return ItemFieldRule{}, &ValidationError{Code: "MASTER-0890", Message: fmt.Sprintf("%s is already chosen from the %s list.", rule.Label, rule.List)}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ItemFieldRule{}, err
	}
	var values []string
	seen := map[string]bool{}
	add := func(v string) {
		if v = strings.TrimSpace(strings.ReplaceAll(v, ",", " ")); v != "" && !seen[strings.ToLower(v)] {
			seen[strings.ToLower(v)] = true
			values = append(values, v)
		}
	}
	var options sql.NullString
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT options FROM %s.doctype_fields WHERE doctype_name = 'Item' AND fieldname = $1`, schema), field).Scan(&options)
	if rule.Type == "Select" {
		for _, o := range strings.Split(options.String, ",") {
			add(o)
		}
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT trim(data->>$1) FROM %s.documents
		WHERE doctype = 'Item' AND deleted_at IS NULL AND COALESCE(trim(data->>$1), '') <> '' ORDER BY 1 LIMIT 1000`, schema), field)
	if err != nil {
		return ItemFieldRule{}, err
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return ItemFieldRule{}, err
		}
		add(v)
	}
	rows.Close()
	label := strings.TrimSpace(strings.SplitN(rule.Label, "/", 2)[0])
	code := strings.ToUpper(strings.Trim(attributeCodeRE.ReplaceAllString(label, "_"), "_"))
	if code == "" {
		code = "ATTR"
	}
	base := code
	for n := 2; ; n++ {
		var taken bool
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.documents WHERE id = $1 OR (doctype = 'ItemAttribute' AND data->>'code' = $2))`, schema), itemAttributeID(code), code).Scan(&taken); err != nil {
			return ItemFieldRule{}, err
		}
		if !taken {
			break
		}
		code = fmt.Sprintf("%s_%d", base, n)
	}
	data, _ := json.Marshal(map[string]interface{}{
		"code": code, "label": label, "value_type": "Select", "value_options": strings.Join(values, ","), "status": "Active",
	})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'ItemAttribute', $2::jsonb, 'Active', $3)`, schema), itemAttributeID(code), string(data), userID); err != nil {
		return ItemFieldRule{}, err
	}
	saved := loadSavedItemFieldRules(tenantID)
	sr := saved[field]
	sr.List = code
	saved[field] = sr
	raw, _ := json.Marshal(saved)
	if err := storeRawSetting(tenantID, itemFieldRulesKey, string(raw), by); err != nil {
		return ItemFieldRule{}, err
	}
	LogAuditEvent(tenantID, userID, "ITEM_FIELD_LIST_CREATED", "SUCCESS", fmt.Sprintf("%s -> Item Attribute %s with %d values", field, code, len(values)))
	rule.List = code
	return *rule, nil
}

// GetDesignMatrixFor is the matrix a PO offers for one item name: only the
// attributes set to be asked in the PO (Configuration -> Item fields), with
// the item name's type map applied when the org maps attributes to types. The
// unfiltered GetDesignMatrix still orders the PO's printed attribute columns.
func GetDesignMatrixFor(tenantID, itemName string) (DesignMatrix, error) {
	m, err := GetDesignMatrix(tenantID)
	if err != nil {
		return m, err
	}
	set, err := ItemAttributesFor(tenantID, itemName)
	if err != nil {
		return m, err
	}
	offered := attributeChoiceCodes(set, true)
	kept := make([]DesignAttribute, 0, len(m.Attributes))
	for _, a := range m.Attributes {
		c, ok := offered[strings.ToUpper(a.Code)]
		if !ok {
			continue
		}
		if set.Source != "all" && a.ValueType == "Select" {
			a.Options = c.Values
		}
		kept = append(kept, a)
	}
	m.Attributes = kept
	return m, nil
}
