package engines

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// Stage 81 (user, 2026-10-18): "I will create variant for the client and it
// will be set for the client but disablable by client ... Like for jewellery
// color, polish, size, stone color. I will share a master with predefined
// value. All attribute value deletable and attributes disableable."
//
// attribute_library.json is the operator's industry product-attribute master
// (10 industries, ~670 attributes, ~190 dropdown value sets), generated from
// the workbook by cmd/genattrlib and shipped in the binary like the HSN
// master. It is a library to copy from, never read at transaction time: what
// a client actually has lives in its own ProductAttributeDef records, which
// the client can switch off and whose values it can trim. Nothing here is a
// compliance claim - the workbook says so itself.

//go:embed attribute_library.json
var attributeLibraryJSON []byte

// LibraryIndustry is one industry of the attribute library.
type LibraryIndustry struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Categories string `json:"categories,omitempty"`
	Focus      string `json:"focus,omitempty"`
}

// LibraryAttribute is one attribute definition from the library. Variant is
// the workbook's "Variant Driver" (Yes / No / Category dependent) and Scope
// its "Value Scope" (Product, Variant, Lot, Serial, Component, ...).
type LibraryAttribute struct {
	ID         string `json:"id"`
	Industry   string `json:"industry"`
	Group      string `json:"group"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ValueSet   string `json:"value_set,omitempty"`
	UOM        string `json:"uom,omitempty"`
	Mandatory  string `json:"mandatory,omitempty"`
	Variant    string `json:"variant"`
	Scope      string `json:"scope"`
	Categories string `json:"categories,omitempty"`
	Facet      string `json:"facet,omitempty"`
	Note       string `json:"note,omitempty"`
}

// LibraryValue is one dropdown value of a value set.
type LibraryValue struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type attributeLibraryFile struct {
	Source     string                    `json:"source"`
	Industries []LibraryIndustry         `json:"industries"`
	Attributes []LibraryAttribute        `json:"attributes"`
	ValueSets  map[string][]LibraryValue `json:"value_sets"`
}

var (
	attributeLibraryOnce sync.Once
	attributeLibrary     attributeLibraryFile
)

func loadAttributeLibrary() *attributeLibraryFile {
	attributeLibraryOnce.Do(func() {
		_ = json.Unmarshal(attributeLibraryJSON, &attributeLibrary)
	})
	return &attributeLibrary
}

// LibraryCoreIndustry is the workbook's code for the attributes every
// industry shares (identity, units, logistics, traceability, governance).
const LibraryCoreIndustry = "CORE"

// libraryCoveredByItem names the shared attributes this ERP already carries as
// a field of its own, so the library never adds a second copy of them as an
// attribute. The value is where the user finds it.
var libraryCoveredByItem = map[string]string{
	"CORE_PRODUCT_MASTER_ID":         "Item: Code (or its design)",
	"CORE_VARIANT_SKU":               "Item: Code",
	"CORE_DISPLAY_NAME":              "Item: Name",
	"CORE_ITEM_KIND":                 "Item: Item Type",
	"CORE_CATEGORY_ID":               "Item: Category",
	"CORE_GTIN":                      "Item: GTIN",
	"CORE_COUNTRY_OF_ORIGIN":         "Item: Country of Origin",
	"CORE_NET_WEIGHT":                "Item: Weight",
	"CORE_SHELF_LIFE_DAYS":           "Item: Shelf Life (days)",
	"CORE_STORAGE_CLASS":             "Item: Temperature Class",
	"CORE_HAZARD_CLASSIFICATION":     "Item: Hazmat Class",
	"CORE_TRACEABILITY_POLICY":       "Item: Traceability",
	"CORE_HSN_HS_TARIFF_CODE":        "Item: HSN Code",
	"CORE_TAX_CLASSIFICATION":        "Item: Tax Treatment and GST Rate",
	"CORE_INVENTORY_VALUATION_CLASS": "Item: Valuation",
	"CORE_BATCH_LOT_NUMBER":          "Goods Receipt: batch number",
	"CORE_SERIAL_UNIQUE_UNIT_NUMBER": "Serial Number master",
	"CORE_MANUFACTURING_DATE":        "Goods Receipt: manufacturing date",
	"CORE_EXPIRY_DATE":               "Goods Receipt: expiry date",
}

// LibraryAttributeUse is what the library suggests an attribute is for in
// this ERP, from the workbook's own Variant Driver and Value Scope columns:
//   - "variant": it tells one sellable combination from another (the
//     workbook marks it a variant driver) - offered as a variant option on the
//     Item, in PO designs and on prints;
//   - "variant-candidate": a variant driver only for some categories - not
//     ticked by default, the operator decides per client (polish, stone colour);
//   - "product": descriptive product information - PIM;
//   - "lot": belongs to a batch or serial unit, not to the item - not copied;
//   - "covered": the ERP already has its own field for it - not copied.
func LibraryAttributeUse(a LibraryAttribute) string {
	if _, ok := libraryCoveredByItem[a.ID]; ok {
		return "covered"
	}
	scope := strings.ToLower(a.Scope)
	if scope == "lot" || scope == "serial" {
		return "lot"
	}
	switch strings.ToLower(a.Variant) {
	case "yes":
		return "variant"
	case "category dependent":
		return "variant-candidate"
	}
	return "product"
}

// LibraryCoveredBy says where the ERP already keeps a shared attribute, or "".
func LibraryCoveredBy(id string) string { return libraryCoveredByItem[id] }

// LibraryIndustries lists the library's industries in workbook order.
func LibraryIndustries() []LibraryIndustry {
	return append([]LibraryIndustry(nil), loadAttributeLibrary().Industries...)
}

// LibraryAttributesFor returns an industry's own attributes followed by the
// shared CORE ones, in workbook order. An unknown industry returns only CORE.
func LibraryAttributesFor(industry string) []LibraryAttribute {
	lib := loadAttributeLibrary()
	industry = strings.ToUpper(strings.TrimSpace(industry))
	var own, core []LibraryAttribute
	for _, a := range lib.Attributes {
		switch a.Industry {
		case industry:
			own = append(own, a)
		case LibraryCoreIndustry:
			core = append(core, a)
		}
	}
	if industry == LibraryCoreIndustry {
		return core
	}
	return append(own, core...)
}

// LibraryAttributeByID finds one attribute definition.
func LibraryAttributeByID(id string) (LibraryAttribute, bool) {
	for _, a := range loadAttributeLibrary().Attributes {
		if a.ID == id {
			return a, true
		}
	}
	return LibraryAttribute{}, false
}

// LibraryValues returns a value set's dropdown values in the workbook's order.
func LibraryValues(valueSet string) []LibraryValue {
	return append([]LibraryValue(nil), loadAttributeLibrary().ValueSets[valueSet]...)
}

// LibraryValueType maps the workbook's data type onto ProductAttributeDef's
// Value Type (Data, Number, Select, Date).
func LibraryValueType(dataType string) string {
	switch strings.ToLower(strings.TrimSpace(dataType)) {
	case "enum", "multienum":
		return "Select"
	case "decimal", "integer":
		return "Number"
	case "date":
		return "Date"
	}
	return "Data"
}
