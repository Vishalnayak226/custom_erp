package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Stage 62.5/62.6: creating designs and their combinations from inside the
// PO. A design is a ProductFamily (numbered Design/... since Stage 51.5) and
// each combination is an Item under it (family = parent_product_code = the
// design, variant_option_values = its axis values) - the model
// PrepareItemVariantCode already builds codes from. The PO screen creates
// them through the ordinary record API, so numbering, variant codes, barcode
// policy and the duplicate-combination check all apply unchanged; this file
// only answers the read-side questions that screen asks.

// Design number modes (setting procurement.po_design_number_mode).
const (
	DesignNumberModeNew   = "new"
	DesignNumberModeMatch = "match"
)

// DesignAttribute is one attribute the org defines for its products
// (ProductAttributeDef). A Select attribute offers its options as values to
// pick - one value is a design attribute, several make it an axis whose
// values multiply into combinations; any other type is typed once per design.
type DesignAttribute struct {
	Code      string   `json:"code"`
	Label     string   `json:"label"`
	ValueType string   `json:"value_type"`
	Options   []string `json:"options,omitempty"`
	Group     string   `json:"group,omitempty"`
}

// DesignMatrix is everything the PO's "Add design" panel starts from.
type DesignMatrix struct {
	Attributes []DesignAttribute `json:"attributes"`
	Mode       string            `json:"design_number_mode"`
	// AttributesClosed (Stage 81): the operator set this client's attributes
	// up from the library, so the panel offers no "new attribute" box.
	AttributesClosed bool `json:"attributes_closed"`
}

// GetDesignMatrix lists every Active attribute, Select ones first, in label
// order, with the tenant's design-number mode.
func GetDesignMatrix(tenantID string) (DesignMatrix, error) {
	out := DesignMatrix{Mode: DesignNumberMode(tenantID), Attributes: []DesignAttribute{}}
	out.AttributesClosed = TenantHasLibraryAttributes(tenantID)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'ProductAttributeDef' AND deleted_at IS NULL AND COALESCE(status, '') <> 'Inactive'`, schema))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return out, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil || strField(d, "status") == "Inactive" {
			continue
		}
		// Product information is PIM's (Stage 81); a design is made of variants.
		if strField(d, "used_for") == AttributeUsedForProduct {
			continue
		}
		a := DesignAttribute{
			Code: firstNonEmpty(strField(d, "code"), id), Label: firstNonEmpty(strField(d, "label"), strField(d, "code"), id),
			ValueType: firstNonEmpty(strField(d, "value_type"), "Data"), Group: strField(d, "group"),
		}
		if a.ValueType == "Select" {
			for _, o := range strings.Split(strField(d, "value_options"), ",") {
				if o = strings.TrimSpace(o); o != "" {
					a.Options = append(a.Options, o)
				}
			}
		}
		out.Attributes = append(out.Attributes, a)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	sort.SliceStable(out.Attributes, func(i, j int) bool {
		si, sj := out.Attributes[i].ValueType == "Select", out.Attributes[j].ValueType == "Select"
		if si != sj {
			return si
		}
		return strings.ToLower(out.Attributes[i].Label) < strings.ToLower(out.Attributes[j].Label)
	})
	return out, nil
}

// DesignNumberMode is the tenant's choice: a new design number on every "Add
// design" (fast fashion, where designs rarely repeat exactly), or reuse an
// existing design whose name and every design attribute match.
func DesignNumberMode(tenantID string) string {
	if GetSettingString(tenantID, "procurement.po_design_number_mode") == DesignNumberModeMatch {
		return DesignNumberModeMatch
	}
	return DesignNumberModeNew
}

// designAttributesString is the stored, order-independent form of a design's
// attributes ("fabric:cotton;fit:slim") - normalizeVariantOptions, the same
// normaliser the duplicate-combination check uses.
func designAttributesString(attrs map[string]string) string {
	parts := make([]string, 0, len(attrs))
	for k, v := range attrs {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			parts = append(parts, k+":"+v)
		}
	}
	return normalizeVariantOptions(strings.Join(parts, ";"))
}

// FindMatchingDesign returns the existing design with the same name and the
// same design attributes, or "" when there is none. Combination values are not
// compared: a matched design takes new combinations alongside its old ones.
func FindMatchingDesign(tenantID, name string, attrs map[string]string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	want := designAttributesString(attrs)
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'code', id), COALESCE(data->>'design_attributes', '') FROM %s.documents
		WHERE doctype = 'ProductFamily' AND deleted_at IS NULL AND lower(trim(data->>'name')) = lower($1)
		ORDER BY created_at, id`, schema), name)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, code, stored string
		if err := rows.Scan(&id, &code, &stored); err != nil {
			return "", err
		}
		if normalizeVariantOptions(stored) == want {
			return code, nil
		}
	}
	return "", rows.Err()
}

// DesignCombination is one existing Item under a design.
type DesignCombination struct {
	SKU     string            `json:"sku"`
	Name    string            `json:"name"`
	Barcode string            `json:"barcode,omitempty"`
	Options map[string]string `json:"options"`
}

// DesignDetail is an existing design and its combinations, for "paste a
// design number": the PO always takes that same design, and new combinations
// can be added next to the ones it already has.
type DesignDetail struct {
	Code         string              `json:"code"`
	Name         string              `json:"name"`
	Attributes   map[string]string   `json:"attributes"`
	HSNCode      string              `json:"hsn_code,omitempty"`
	GSTRate      float64             `json:"gst_rate,omitempty"`
	MRP          float64             `json:"mrp,omitempty"`
	Rate         float64             `json:"purchase_rate,omitempty"`
	Combinations []DesignCombination `json:"combinations"`
}

func parseOptionPairs(raw string) map[string]string {
	out := map[string]string{}
	for _, p := range strings.Split(raw, ";") {
		kv := strings.SplitN(p, ":", 2)
		k := strings.TrimSpace(kv[0])
		if k == "" {
			continue
		}
		v := ""
		if len(kv) == 2 {
			v = strings.TrimSpace(kv[1])
		}
		out[k] = v
	}
	return out
}

// GetDesign loads a design by its number (code or id) with every Item under
// it. HSN/GST/MRP/rate come from the design record, falling back to its first
// combination for a design made before Stage 62.
func GetDesign(tenantID, code string) (DesignDetail, error) {
	code = strings.TrimSpace(code)
	fam, _, err := fetchDocData(tenantID, "ProductFamily", code)
	if err != nil {
		return DesignDetail{}, &ValidationError{Code: "GLOBAL-0004", SubFor: "Design No", Message: fmt.Sprintf("design %s was not found", code)}
	}
	designCode := firstNonEmpty(strField(fam, "code"), code)
	out := DesignDetail{
		Code: designCode, Name: strField(fam, "name"),
		Attributes: parseOptionPairs(strField(fam, "design_attributes")),
		HSNCode:    strField(fam, "hsn_code"), GSTRate: numFromInterface(fam["gst_rate"]),
		MRP: numFromInterface(fam["mrp"]), Rate: numFromInterface(fam["purchase_rate"]),
		Combinations: []DesignCombination{},
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'Item' AND deleted_at IS NULL AND (data->>'family' = $1 OR data->>'parent_product_code' = $1)
		ORDER BY created_at, id`, schema), designCode)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return out, err
		}
		var it map[string]interface{}
		if json.Unmarshal([]byte(raw), &it) != nil {
			continue
		}
		out.Combinations = append(out.Combinations, DesignCombination{
			SKU: firstNonEmpty(strField(it, "code"), id), Name: strField(it, "name"), Barcode: strField(it, "barcode"),
			Options: parseOptionPairs(strField(it, "variant_option_values")),
		})
		if out.HSNCode == "" {
			out.HSNCode = strField(it, "hsn_code")
			out.GSTRate = numFromInterface(it["gst_rate"])
		}
		if out.MRP == 0 {
			out.MRP = numFromInterface(it["mrp"])
		}
		if out.Rate == 0 {
			out.Rate = numFromInterface(it["standard_cost"])
		}
	}
	return out, rows.Err()
}

// PrepareDesignNumber numbers a new design (ProductFamily) that arrives with
// no code, from its own "Design/..." series (Stage 51.5's prefix row) - the
// same series and counter the master screen's code would have come from. A
// code the caller typed, or an explicit id (the API's upsert form), is kept.
func PrepareDesignNumber(tenantID, location string, isCreate bool, payload map[string]interface{}) error {
	if !isCreate || strings.TrimSpace(strField(payload, "code")) != "" || strings.TrimSpace(strField(payload, "id")) != "" {
		return nil
	}
	return PrepareMasterNumber(tenantID, location, "ProductFamily", isCreate, payload)
}
