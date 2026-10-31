package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// Stage 145 (user, 2026-10-31: "Need Design No, Combinn ID/SKU, barcode and
// item name should be small Ex- Antique Necklace. Othere Attribute should show
// as column."): the inventory screens show a stocked SKU by its design instead
// of the long name every attribute value is appended to.

// StockItem is what the inventory screens show for one stocked SKU.
type StockItem struct {
	Design   string // the design's id, or the SKU for an item with none - what "Designs" counts
	DesignNo string // the design's number or code
	Name     string // short: the Item Name master (Category + Product Type), else the design's name, else the item's own
	FullName string
	Barcode  string
	Category string
	Attrs    map[string]string // attribute code -> value: the design's, overlaid by the combination's
}

// loadStockItems describes each SKU in three reads - the items, their designs,
// and the Item Name / Category masters they name - for a page or a whole store
// at once. A SKU with no Item record is described by its code alone.
func loadStockItems(tenantID, schema string, skus []string) (map[string]*StockItem, error) {
	out := make(map[string]*StockItem, len(skus))
	for _, s := range skus {
		out[s] = &StockItem{Design: s, Name: s, FullName: s, Attrs: map[string]string{}}
	}
	if len(skus) == 0 {
		return out, nil
	}
	// The stock row's SKU is the Item's id; an older item's may be its code.
	itemByID, itemByCode := map[string]map[string]interface{}{}, map[string]map[string]interface{}{}
	if err := eachDocData(fmt.Sprintf(`SELECT id, data::text FROM %s.documents
		WHERE doctype = 'Item' AND deleted_at IS NULL AND (id = ANY($1) OR data->>'code' = ANY($1))`, schema), skus,
		func(id string, d map[string]interface{}) {
			itemByID[id] = d
			if c := strField(d, "code"); c != "" {
				itemByCode[c] = d
			}
		}); err != nil {
		return nil, err
	}
	item := func(sku string) map[string]interface{} {
		if d := itemByID[sku]; d != nil {
			return d
		}
		return itemByCode[sku]
	}
	familyRef := func(d map[string]interface{}) string {
		return firstNonEmpty(strField(d, "family"), strField(d, "parent_product_code"))
	}

	refs := []string{}
	for _, s := range skus {
		if d := item(s); d != nil {
			if f := familyRef(d); f != "" {
				refs = append(refs, f)
			}
		}
	}
	famByRef := map[string]map[string]interface{}{}
	famID := map[string]string{}
	if len(refs) > 0 {
		if err := eachDocData(fmt.Sprintf(`SELECT id, data::text FROM %s.documents
			WHERE doctype = 'ProductFamily' AND deleted_at IS NULL AND (id = ANY($1) OR data->>'code' = ANY($1))`, schema), refs,
			func(id string, d map[string]interface{}) {
				famByRef[id], famID[id] = d, id
				if c := strField(d, "code"); c != "" && famByRef[c] == nil {
					famByRef[c], famID[c] = d, id
				}
			}); err != nil {
			return nil, err
		}
	}

	// The Item Name and Category masters, by id or code.
	masterRefs := []string{}
	for _, s := range skus {
		if d := item(s); d != nil {
			masterRefs = append(masterRefs, strField(d, "item_name"), strField(d, "category"), strField(famByRef[familyRef(d)], "item_name"))
		}
	}
	names := map[string]string{} // "<doctype>|<id or code>" -> name
	if len(masterRefs) > 0 {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT doctype, id, COALESCE(data->>'code', ''), COALESCE(data->>'name', '') FROM %s.documents
			WHERE doctype IN ('ItemName', 'Category') AND deleted_at IS NULL AND (id = ANY($1) OR data->>'code' = ANY($1))`, schema), pq.Array(masterRefs))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var doctype, id, code, name string
			if err := rows.Scan(&doctype, &id, &code, &name); err != nil {
				rows.Close()
				return nil, err
			}
			if name = strings.TrimSpace(name); name != "" {
				names[doctype+"|"+id] = name
				if code != "" {
					names[doctype+"|"+code] = name
				}
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// ItemAttributeValues reads each design's attributes from this cache, so
	// it never goes back to the database here.
	designAttrs := map[string]map[string]string{}
	for ref, f := range famByRef {
		designAttrs[ref] = parseOptionPairs(strField(f, "design_attributes"))
	}
	for _, s := range skus {
		d := item(s)
		if d == nil {
			continue
		}
		st := out[s]
		ref := familyRef(d)
		fam := famByRef[ref]
		if ref != "" && designAttrs[ref] == nil {
			designAttrs[ref] = map[string]string{} // a design that is gone has no attributes
		}
		short := ""
		for _, r := range []string{strField(d, "item_name"), strField(fam, "item_name")} {
			if n := names["ItemName|"+r]; r != "" && n != "" {
				short = n
				break
			}
		}
		st.FullName = firstNonEmpty(strField(d, "name"), s)
		st.Name = firstNonEmpty(short, strField(fam, "name"), st.FullName)
		st.Barcode = strField(d, "barcode")
		if c := strField(d, "category"); c != "" {
			st.Category = firstNonEmpty(names["Category|"+c], c)
		}
		if ref != "" {
			st.Design = firstNonEmpty(famID[ref], ref)
			st.DesignNo = firstNonEmpty(strField(fam, "design_number"), strField(fam, "code"), ref)
		}
		st.Attrs = ItemAttributeValues(tenantID, d, designAttrs)
	}
	return out, nil
}

// eachDocData runs a read of (id, data::text) with one text-array argument
// and hands each row's data to fn.
func eachDocData(query string, arg []string, fn func(id string, data map[string]interface{})) error {
	rows, err := db.DB.Query(query, pq.Array(arg))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) == nil {
			fn(id, d)
		}
	}
	return rows.Err()
}

// InventoryStockPage is Inventory ▸ Stock on Hand (Stage 145.3): one page of
// current stock, each SKU described by its design, plus the attribute columns
// - every active Item Attribute in the client's order, so the columns stay the
// same from page to page, then any other attribute this page's items carry.
func InventoryStockPage(tenantID string, f CurrentStockFilter) ([]map[string]interface{}, int, []AttributeColumn, error) {
	rows, total, err := GetCurrentStockPage(tenantID, f)
	if err != nil {
		return nil, 0, nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, 0, nil, err
	}
	skus, seen := []string{}, map[string]bool{}
	for _, r := range rows {
		if s, _ := r["sku"].(string); !seen[s] {
			seen[s] = true
			skus = append(skus, s)
		}
	}
	info, err := loadStockItems(tenantID, schema, skus)
	if err != nil {
		return nil, 0, nil, err
	}
	lines := make([]map[string]string, len(rows))
	for i, r := range rows {
		s, _ := r["sku"].(string)
		lines[i] = info[s].Attrs
	}
	present := OrderAttributeColumns(tenantID, lines) // also gives each line's keys the attribute's own code
	cols, have := []AttributeColumn{}, map[string]bool{}
	if m, err := GetDesignMatrix(tenantID); err == nil {
		for _, a := range m.Attributes {
			if lc := strings.ToLower(a.Code); !have[lc] {
				have[lc] = true
				cols = append(cols, AttributeColumn{Code: a.Code, Label: a.Label})
			}
		}
	}
	for _, c := range present {
		if lc := strings.ToLower(c.Code); !have[lc] {
			have[lc] = true
			cols = append(cols, c)
		}
	}
	for i, r := range rows {
		s, _ := r["sku"].(string)
		it := info[s]
		r["design"], r["design_no"], r["item_name"], r["full_name"] = it.Design, it.DesignNo, it.Name, it.FullName
		r["barcode"], r["category"] = it.Barcode, it.Category
		attrs := lines[i]
		if attrs == nil {
			attrs = map[string]string{}
		}
		r["attributes"] = attrs
	}
	return rows, total, cols, nil
}
