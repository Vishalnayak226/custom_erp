package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Stage 146.5.5: design-level attribute values. A design is the parent Item
// its variants name in parent_product_code. A ProductAttributeValue on the
// design is the value every variant inherits until it has its own. Reading the
// inheritance is OFF until pim.design_values_fallback is turned on, so a
// client that never opts in sees exactly what it saw before.

// SettingDesignValuesFallback turns on the variant -> design read.
const SettingDesignValuesFallback = "pim.design_values_fallback"

// maxDesignGridRows bounds one grid page.
const maxDesignGridRows = 500

// designCodeOf returns the design an item belongs to: its parent Item, or
// "" when it has none.
func designCodeOf(schema, itemCode string) string {
	var parent string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'parent_product_code', '') FROM %s.documents
		WHERE doctype = 'Item' AND id = $1 AND deleted_at IS NULL`, schema), itemCode).Scan(&parent)
	return strings.TrimSpace(parent)
}

// DesignValueRequest says which designs get which value. Designs are Item
// codes; Group is a product group whose members are mapped to their designs
// (a member with no parent counts as its own design). Value "" clears.
type DesignValueRequest struct {
	Group     string   `json:"group"`
	Designs   []string `json:"designs"`
	Attribute string   `json:"attribute"`
	Value     string   `json:"value"`
}

// DesignValueResult counts what a bulk set did.
type DesignValueResult struct {
	Designs   int      `json:"designs"`
	Written   int      `json:"written"`
	Unchanged int      `json:"unchanged"`
	Codes     []string `json:"codes"`
}

// SetDesignAttributeValues writes (or clears) one attribute on many designs in
// a single transaction. Nothing is written if any design or the attribute is
// refused, or if more designs are named than pim.max_bulk_edit_documents.
func SetDesignAttributeValues(tenantID, user string, req DesignValueRequest) (*DesignValueResult, error) {
	attribute := strings.TrimSpace(req.Attribute)
	if attribute == "" {
		return nil, templateErr("PIM-0960", "Attribute", "choose an attribute")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if ok, _ := verifyDocumentExists(tenantID, "ProductAttributeDef", attribute); !ok {
		return nil, templateErr("PIM-0960", "Attribute", fmt.Sprintf("attribute %q does not exist", attribute))
	}

	set := map[string]bool{}
	for _, d := range req.Designs {
		if d = strings.TrimSpace(d); d != "" {
			set[d] = true
		}
	}
	if g := strings.TrimSpace(req.Group); g != "" {
		members, gErr := ResolvePIMProductGroupItemCodes(tenantID, g)
		if gErr != nil {
			return nil, gErr
		}
		for _, m := range members {
			if p := designCodeOf(schema, m); p != "" {
				set[p] = true
			} else {
				set[m] = true
			}
		}
	}
	if len(set) == 0 {
		return nil, templateErr("PIM-0959", "Designs", "choose a product group or at least one design")
	}
	if max := maxPIMBulkEditDocumentsFor(tenantID); len(set) > max {
		return nil, templateErr("PIM-0962", "Designs", fmt.Sprintf("%d designs chosen; one bulk set takes at most %d", len(set), max))
	}
	designs := make([]string, 0, len(set))
	for d := range set {
		designs = append(designs, d)
	}
	sort.Strings(designs)
	for _, d := range designs {
		if ok, _ := verifyDocumentExists(tenantID, "Item", d); !ok {
			return nil, templateErr("PIM-0961", "Design", fmt.Sprintf("design %q is not an item", d))
		}
	}

	value := strings.TrimSpace(req.Value)
	if value != "" { // 146.5.11: the value fits the attribute's type
		norm, cErr := CheckAttributeValue(tenantID, attribute, value)
		if cErr != nil {
			return nil, cErr
		}
		value = norm
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}
	res := &DesignValueResult{Designs: len(designs), Codes: []string{}}
	for _, d := range designs {
		id := attributeValueID(d, attribute, "", "")
		var raw string
		qerr := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'ProductAttributeValue' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), id).Scan(&raw)
		exists := qerr == nil
		old := ""
		if exists {
			m := map[string]interface{}{}
			_ = json.Unmarshal([]byte(raw), &m)
			old = strings.TrimSpace(pimString(m["value"]))
		}
		if old == value {
			res.Unchanged++
			continue
		}
		body, _ := json.Marshal(map[string]interface{}{"item": d, "attribute": attribute, "value": value})
		if exists {
			_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $2, updated_at = NOW() WHERE doctype = 'ProductAttributeValue' AND id = $1`, schema), id, body)
		} else {
			_, err = tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents(id, doctype, data, status, created_by) VALUES($1,'ProductAttributeValue',$2,'Active',$3)`, schema), id, body, user)
		}
		if err != nil {
			return nil, err
		}
		res.Written++
		res.Codes = append(res.Codes, d)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, d := range res.Codes {
		LogAuditEvent(tenantID, user, "pim.design_value.set", "Success", fmt.Sprintf("%s.%s = %q", d, attribute, value))
	}
	return res, nil
}

// DesignValueRow is one design in the grid.
type DesignValueRow struct {
	Design   string            `json:"design"`
	Name     string            `json:"name"`
	Variants int               `json:"variants"`
	Values   map[string]string `json:"values"`
}

// DesignValueGrid lists the designs behind a product group (or, with no
// group, every design that has variants) with the design's own value for each
// attribute asked for. Bounded at maxDesignGridRows.
func DesignValueGrid(tenantID, group string, attributes []string) ([]DesignValueRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var designs []string
	if g := strings.TrimSpace(group); g != "" {
		members, gErr := ResolvePIMProductGroupItemCodes(tenantID, g)
		if gErr != nil {
			return nil, gErr
		}
		seen := map[string]bool{}
		for _, m := range members {
			d := designCodeOf(schema, m)
			if d == "" {
				d = m
			}
			if !seen[d] {
				seen[d] = true
				designs = append(designs, d)
			}
		}
		sort.Strings(designs)
	} else {
		rows, qerr := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT data->>'parent_product_code' FROM %s.documents
			WHERE doctype = 'Item' AND deleted_at IS NULL AND COALESCE(data->>'parent_product_code','') <> ''
			ORDER BY 1 LIMIT %d`, schema, maxDesignGridRows))
		if qerr != nil {
			return nil, qerr
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			if rows.Scan(&d) == nil {
				designs = append(designs, d)
			}
		}
	}
	if len(designs) > maxDesignGridRows {
		designs = designs[:maxDesignGridRows]
	}
	out := make([]DesignValueRow, 0, len(designs))
	for _, d := range designs {
		row := DesignValueRow{Design: d, Values: map[string]string{}}
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'name','') FROM %s.documents WHERE doctype='Item' AND id=$1`, schema), d).Scan(&row.Name)
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype='Item' AND deleted_at IS NULL AND data->>'parent_product_code' = $1`, schema), d).Scan(&row.Variants)
		for _, a := range attributes {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			var v string
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'value','') FROM %s.documents WHERE doctype='ProductAttributeValue' AND id=$1 AND deleted_at IS NULL`, schema), attributeValueID(d, a, "", "")).Scan(&v)
			row.Values[a] = v
		}
		out = append(out, row)
	}
	return out, nil
}
