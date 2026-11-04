package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
)

// Stage 36.7.3: related products ("Find Duplicates and Related Products" in
// Unbxd's own feature naming - 26.4.2 already built the duplicate half of
// that pair). Relatedness here is deliberately simple and explainable
// rather than a similarity-score black box: two products are related when
// they sit in the same ProductFamily and share one or more attribute
// values (color, material, whatever the family's own attribute set
// defines) - the same "family/attribute overlap" the plan spec names.

// RelatedProduct is one candidate the query below found, ranked by how
// many attribute values it shares with the item being looked up.
type RelatedProduct struct {
	ItemCode         string `json:"item_code"`
	Name             string `json:"name"`
	SharedAttributes int    `json:"shared_attributes"`
}

// FindRelatedProducts (36.7.3) returns up to `limit` other Active items in
// the same ProductFamily as itemCode, ordered by the number of attribute
// values they share with it (most shared first, item code as a
// deterministic tiebreaker). Only global attribute values are compared
// (locale/channel blank) - a locale- or channel-scoped override is a
// presentation detail, not evidence that two products are the same kind of
// thing, and comparing every scoped variant as well would inflate the
// count without saying anything more about relatedness. An item with no
// family, or no attribute values yet, has nothing to compare against and
// returns an empty (not an error) result - the same "nothing to do" vs.
// "something went wrong" distinction the rest of this codebase draws.
func FindRelatedProducts(tenantID, itemCode string, limit int) ([]RelatedProduct, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	var family string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'family', '') FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1`, schema),
		itemCode).Scan(&family)
	if err != nil {
		return nil, fmt.Errorf("item %q not found", itemCode)
	}
	if family == "" {
		return []RelatedProduct{}, nil
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT other.data->>'code', COALESCE(other.data->>'name', ''), COUNT(*) AS shared
		FROM %s.documents self_attr
		JOIN %s.documents other_attr
			ON other_attr.doctype = 'ProductAttributeValue'
			AND other_attr.data->>'attribute' = self_attr.data->>'attribute'
			AND other_attr.data->>'value' = self_attr.data->>'value'
			AND COALESCE(other_attr.data->>'locale', '') = ''
			AND COALESCE(other_attr.data->>'channel', '') = ''
			AND other_attr.data->>'item' != $1
		JOIN %s.documents other
			ON other.doctype = 'Item'
			AND other.data->>'code' = other_attr.data->>'item'
			AND other.data->>'family' = $2
			AND other.status = 'Active' AND other.deleted_at IS NULL
		WHERE self_attr.doctype = 'ProductAttributeValue'
			AND self_attr.data->>'item' = $1
			AND COALESCE(self_attr.data->>'locale', '') = ''
			AND COALESCE(self_attr.data->>'channel', '') = ''
			AND COALESCE(self_attr.data->>'value', '') != ''
		GROUP BY other_attr.data->>'item', other.data->>'code', other.data->>'name'
		ORDER BY shared DESC, other.data->>'code' ASC
		LIMIT $3`, schema, schema, schema), itemCode, family, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RelatedProduct{}
	for rows.Next() {
		var rp RelatedProduct
		if err := rows.Scan(&rp.ItemCode, &rp.Name, &rp.SharedAttributes); err != nil {
			return nil, err
		}
		out = append(out, rp)
	}
	return out, rows.Err()
}

// Stage 146.5.12: typed product relationships. A ProductRelation row says
// that RelatedItem is a Similar / Complete the look / Alternative / Upsell /
// Cross-sell / Add-on product for Item. The pair is directional: A upsells to
// B does not make B an upsell of A.

// ValidateProductRelationDocument is the ValidateDocument hook: no item
// related to itself, and no second row for the same item, type and related item.
func ValidateProductRelationDocument(tenantID string, payload map[string]interface{}) error {
	item, other, typ := strField(payload, "item"), strField(payload, "related_item"), strField(payload, "relation_type")
	if item == "" || other == "" || typ == "" {
		return nil // the mandatory check reports it
	}
	if strings.EqualFold(item, other) {
		return &ValidationError{Code: "PIM-0981", SubFor: "Related Item", Message: "an item cannot be related to itself"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var dup bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents
		WHERE doctype = 'ProductRelation' AND deleted_at IS NULL AND id <> $4
		  AND data->>'item' = $1 AND data->>'related_item' = $2 AND data->>'relation_type' = $3)`, schema),
		item, other, typ, strField(payload, "id")).Scan(&dup); err != nil {
		return err
	}
	if dup {
		return &ValidationError{Code: "PIM-0982", SubFor: "Related Item", Message: fmt.Sprintf("%s is already a %s of %s", other, typ, item)}
	}
	return nil
}
