package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Stage 89 - the Item master is asked once, at creation, and mostly answered
// by the organisation's own defaults (user, 2026-10-18: "What ever is there in
// item master, It will be some default config for org ... While creating item
// it self it should ask all this. User will never revisit this.").
//
//   - Org defaults (Configuration -> Inventory): traceability and the three
//     shelf-life numbers are copied onto a new Item that leaves them blank;
//     tax treatment is Taxable. Barcode and GRN batch numbering keep inheriting
//     live ("Item type default"), as they always have.
//   - One pick fills the item: the Item Name master (Category + Product Type,
//     with HSN and GST) sets the Item's category, product type, HSN, GST and,
//     when blank, its name. The name order is an org setting; switching it
//     renames the item names that were built from the old order (D2).
//   - Where each field is asked: every Item field and Item Attribute is asked
//     on the Item form, in the PO's add row, at the GRN, or nowhere
//     (ItemFieldRule) - one table in Configuration -> Inventory -> Item fields.

const (
	SettingDefaultTrackingMode        = "inventory.default_tracking_mode"
	SettingDefaultShelfLifeDays       = "inventory.default_shelf_life_days"
	SettingDefaultMinShelfLifeReceipt = "inventory.default_min_shelf_life_on_receipt_days"
	SettingDefaultMinShelfLifePick    = "inventory.default_min_shelf_life_on_pick_days"
	SettingItemNameOrder              = "inventory.item_name_order"
	SettingAttributeTypeMapping       = "inventory.attribute_type_mapping"
	SettingTreatmentSecondAdmin       = "inventory.treatment_correction_second_admin"

	ItemNameOrderCategoryType = "category_type"
	ItemNameOrderTypeCategory = "type_category"

	// The where-each-field-is-asked table: one JSON value in system_settings,
	// edited by its own Configuration page rather than the generic list.
	itemFieldRulesKey = "inventory.item_fields"
)

// itemDefaultNumberFields: the Item fields a new item copies from an org
// default when it leaves them blank.
var itemDefaultNumberFields = []struct{ field, setting string }{
	{"shelf_life_days", SettingDefaultShelfLifeDays},
	{"min_shelf_life_on_receipt_days", SettingDefaultMinShelfLifeReceipt},
	{"min_shelf_life_on_pick_days", SettingDefaultMinShelfLifePick},
}

func itemValueBlank(v interface{}) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// itemNameRecord is an Item Name master with its category and type as names.
type itemNameRecord struct {
	ID, Code, Name           string
	CategoryRef, Category    string
	ProductTypeRef, TypeName string
	HSN                      string
	GST                      interface{}
}

// loadItemNameRecord finds an Item Name by id, code or (for an import that
// names it) its name.
func loadItemNameRecord(schema, ref string) (itemNameRecord, bool, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return itemNameRecord{}, false, nil
	}
	var id, raw string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, data::text FROM %s.documents
		WHERE doctype = 'ItemName' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1 OR lower(data->>'name') = lower($1))
		ORDER BY (id = $1) DESC, (data->>'code' = $1) DESC LIMIT 1`, schema), ref).Scan(&id, &raw)
	if err == sql.ErrNoRows {
		return itemNameRecord{}, false, nil
	}
	if err != nil {
		return itemNameRecord{}, false, err
	}
	var d map[string]interface{}
	if json.Unmarshal([]byte(raw), &d) != nil {
		return itemNameRecord{}, false, nil
	}
	rec := itemNameRecord{
		ID: id, Code: firstNonEmpty(strField(d, "code"), id), Name: strings.TrimSpace(strField(d, "name")),
		CategoryRef: strField(d, "category"), ProductTypeRef: strField(d, "product_type"),
		HSN: strings.TrimSpace(strField(d, "hsn_code")), GST: d["gst_rate"],
	}
	rec.Category = masterDisplayName(schema, "Category", rec.CategoryRef)
	rec.TypeName = masterDisplayName(schema, "ProductType", rec.ProductTypeRef)
	return rec, true, nil
}

// ItemDefaultsMemo keeps one import's lookups (a category's name, an item name,
// a design's item name, the field rules), so a 20,000-row file asks the
// database once per distinct value instead of once per row.
type ItemDefaultsMemo struct {
	schema    string
	org       *orgItemDefaults
	names     map[string]string
	itemNames map[string]itemNameMemo
	families  map[string]string
	rules     []ItemFieldRule
	rulesErr  error
	rulesRead bool
}

type itemNameMemo struct {
	rec itemNameRecord
	ok  bool
}

// orgItemDefaults is what a new item copies from the organisation.
type orgItemDefaults struct {
	tracking string
	numbers  map[string]int
}

func loadOrgItemDefaults(schema string) *orgItemDefaults {
	d := &orgItemDefaults{tracking: strings.TrimSpace(GetSettingStringForSchema(schema, SettingDefaultTrackingMode)), numbers: map[string]int{}}
	for _, n := range itemDefaultNumberFields {
		d.numbers[n.field] = GetSettingIntForSchema(schema, n.setting)
	}
	return d
}

// schemaFor and orgDefaults resolve once per import (every tenant-schema
// lookup is a query), and each time without a memo.
func (m *ItemDefaultsMemo) schemaFor(tenantID string) (string, error) {
	if m != nil && m.schema != "" {
		return m.schema, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err == nil && m != nil {
		m.schema = schema
	}
	return schema, err
}

func (m *ItemDefaultsMemo) orgDefaults(schema string) *orgItemDefaults {
	if m == nil {
		return loadOrgItemDefaults(schema)
	}
	if m.org == nil {
		m.org = loadOrgItemDefaults(schema)
	}
	return m.org
}

// NewItemDefaultsMemo starts an empty memo for one import.
func NewItemDefaultsMemo() *ItemDefaultsMemo {
	return &ItemDefaultsMemo{names: map[string]string{}, itemNames: map[string]itemNameMemo{}, families: map[string]string{}}
}

func (m *ItemDefaultsMemo) displayName(schema, doctype, ref string) string {
	if m == nil {
		return masterDisplayName(schema, doctype, ref)
	}
	k := doctype + "|" + ref
	if v, ok := m.names[k]; ok {
		return v
	}
	v := masterDisplayName(schema, doctype, ref)
	m.names[k] = v
	return v
}

func (m *ItemDefaultsMemo) itemName(schema, ref string) (itemNameRecord, bool, error) {
	if m == nil {
		return loadItemNameRecord(schema, ref)
	}
	if v, ok := m.itemNames[ref]; ok {
		return v.rec, v.ok, nil
	}
	rec, ok, err := loadItemNameRecord(schema, ref)
	if err != nil {
		return rec, ok, err
	}
	m.itemNames[ref] = itemNameMemo{rec, ok}
	return rec, ok, nil
}

func (m *ItemDefaultsMemo) familyItemName(schema, fam string) string {
	if m != nil {
		if v, ok := m.families[fam]; ok {
			return v
		}
	}
	var in sql.NullString
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'item_name' FROM %s.documents
		WHERE doctype = 'ProductFamily' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), fam).Scan(&in)
	v := strings.TrimSpace(in.String)
	if m != nil {
		m.families[fam] = v
	}
	return v
}

func (m *ItemDefaultsMemo) fieldRules(tenantID string) ([]ItemFieldRule, error) {
	if m == nil {
		return ItemFieldRules(tenantID)
	}
	if !m.rulesRead {
		m.rules, m.rulesErr = ItemFieldRules(tenantID)
		m.rulesRead = true
	}
	return m.rules, m.rulesErr
}

// PrepareItemDefaults fills what an Item takes from its item name and from the
// organisation's defaults. Runs on every Item save before validation (the
// fields it fills feed mandatory checks).
func PrepareItemDefaults(tenantID string, isCreate bool, payload map[string]interface{}) error {
	return PrepareItemDefaultsMemo(tenantID, isCreate, payload, nil)
}

// PrepareItemDefaultsMemo is PrepareItemDefaults with an import's memo.
func PrepareItemDefaultsMemo(tenantID string, isCreate bool, payload map[string]interface{}, memo *ItemDefaultsMemo) error {
	schema, err := memo.schemaFor(tenantID)
	if err != nil {
		return err
	}
	// A PO's design items arrive with their design, which knows the item name.
	if strings.TrimSpace(strField(payload, "item_name")) == "" {
		if fam := firstNonEmpty(strField(payload, "family"), strField(payload, "parent_product_code")); fam != "" {
			if v := memo.familyItemName(schema, fam); v != "" {
				payload["item_name"] = v
			}
		}
	}
	if ref := strings.TrimSpace(strField(payload, "item_name")); ref != "" {
		rec, ok, err := memo.itemName(schema, ref)
		if err != nil {
			return err
		}
		if ok {
			payload["item_name"] = rec.ID
			payload["category"] = rec.Category
			payload["product_type"] = rec.TypeName
			if strings.TrimSpace(strField(payload, "hsn_code")) == "" && rec.HSN != "" {
				payload["hsn_code"] = rec.HSN
			}
			if itemValueBlank(payload["gst_rate"]) && !itemValueBlank(rec.GST) {
				payload["gst_rate"] = rec.GST
			}
			if strings.TrimSpace(strField(payload, "name")) == "" && rec.Name != "" {
				payload["name"] = rec.Name
			}
		}
	} else if cat := strings.TrimSpace(strField(payload, "category")); cat != "" {
		// A category given by its master's code (a PO design item) reads as its name,
		// which is what prints and stickers show.
		payload["category"] = memo.displayName(schema, "Category", cat)
	}
	// A field chosen from an attribute's list (Color from COLOR) that is blank
	// takes the item's variant value for it - a PO's design item carries its
	// colour in its variant options, and prints read the field.
	if variant := strings.TrimSpace(strField(payload, "variant_option_values")); variant != "" {
		values := map[string]string{}
		for _, pair := range strings.Split(variant, ";") {
			if k, v, ok := strings.Cut(pair, ":"); ok && strings.TrimSpace(v) != "" {
				values[strings.ToUpper(strings.TrimSpace(k))] = strings.TrimSpace(v)
			}
		}
		if rules, err := memo.fieldRules(tenantID); err == nil {
			for _, r := range rules {
				if r.Kind == "field" && r.List != "" && itemValueBlank(payload[r.Key]) && values[strings.ToUpper(r.List)] != "" {
					payload[r.Key] = values[strings.ToUpper(r.List)]
				}
			}
		}
	}
	if !isCreate {
		return nil
	}
	if strings.TrimSpace(strField(payload, "tax_treatment")) == "" {
		payload["tax_treatment"] = TaxTreatmentTaxable
	}
	org := memo.orgDefaults(schema)
	if strings.TrimSpace(strField(payload, "tracking_mode")) == "" {
		if org.tracking != "" && org.tracking != "None" {
			payload["tracking_mode"] = org.tracking
		}
	}
	for _, d := range itemDefaultNumberFields {
		if itemValueBlank(payload[d.field]) {
			if n := org.numbers[d.field]; n > 0 {
				payload[d.field] = n
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Item name order (D2): "Antique Necklace" or "Necklace Antique".
// ---------------------------------------------------------------------------

// ItemNameOrder is the org's order for a name built from category and type.
func ItemNameOrder(tenantID string) string {
	if GetSettingString(tenantID, SettingItemNameOrder) == ItemNameOrderTypeCategory {
		return ItemNameOrderTypeCategory
	}
	return ItemNameOrderCategoryType
}

func composeItemName(order, category, productType string) string {
	parts := []string{strings.TrimSpace(category), strings.TrimSpace(productType)}
	if order == ItemNameOrderTypeCategory {
		parts[0], parts[1] = parts[1], parts[0]
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// renamedFrom gives name with its leading oldPrefix (a whole word run) swapped
// for newPrefix, or "" when name does not start with it.
func renamedFrom(name, oldPrefix, newPrefix string) string {
	trimmed := strings.TrimSpace(name)
	if oldPrefix == "" || len(trimmed) < len(oldPrefix) || !strings.EqualFold(trimmed[:len(oldPrefix)], oldPrefix) {
		return ""
	}
	rest := trimmed[len(oldPrefix):]
	if rest != "" && rest[0] != ' ' {
		return ""
	}
	return newPrefix + rest
}

// ItemNameOrderChange is what switching the order renamed.
type ItemNameOrderChange struct {
	ItemNames int `json:"item_names"`
	Designs   int `json:"designs"`
	Items     int `json:"items"`
}

// ApplyItemNameOrder renames, in one transaction, every item name that still
// reads in the from-order (a name someone typed by hand is left alone), and
// the designs and items named from it. Bills already printed keep the name
// they printed. Called when the setting changes (settingChangeHooks).
func ApplyItemNameOrder(tenantID, from, to, by string) (ItemNameOrderChange, error) {
	var out ItemNameOrderChange
	if from == to {
		return out, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	type rename struct{ id, code, oldName, newName string }
	var renames []rename
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text FROM %s.documents WHERE doctype = 'ItemName' AND deleted_at IS NULL`, schema))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return out, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		cat := masterDisplayName(schema, "Category", strField(d, "category"))
		typ := masterDisplayName(schema, "ProductType", strField(d, "product_type"))
		oldName, newName := composeItemName(from, cat, typ), composeItemName(to, cat, typ)
		if oldName == newName || !strings.EqualFold(strings.TrimSpace(strField(d, "name")), oldName) {
			continue
		}
		renames = append(renames, rename{id, firstNonEmpty(strField(d, "code"), id), oldName, newName})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(renames) == 0 {
		return out, nil
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	setName := func(id, name string) error {
		_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = jsonb_set(data, '{name}', to_jsonb($2::text)),
			updated_at = CURRENT_TIMESTAMP, version = version + 1 WHERE id = $1`, schema), id, name)
		return err
	}
	renameFrom := func(query string, args []interface{}, r rename, count *int) ([]string, error) {
		rs, err := tx.Query(query, args...)
		if err != nil {
			return nil, err
		}
		type hit struct{ id, code, name string }
		var hits []hit
		for rs.Next() {
			var h hit
			var code, name sql.NullString
			if err := rs.Scan(&h.id, &code, &name); err != nil {
				rs.Close()
				return nil, err
			}
			h.code, h.name = code.String, name.String
			hits = append(hits, h)
		}
		rs.Close()
		var refs []string
		for _, h := range hits {
			refs = append(refs, h.id)
			if h.code != "" {
				refs = append(refs, h.code)
			}
			if n := renamedFrom(h.name, r.oldName, r.newName); n != "" {
				if err := setName(h.id, n); err != nil {
					return nil, err
				}
				*count++
			}
		}
		return refs, nil
	}
	for _, r := range renames {
		if err := setName(r.id, r.newName); err != nil {
			return out, err
		}
		out.ItemNames++
		designs, err := renameFrom(fmt.Sprintf(`SELECT id, data->>'code', data->>'name' FROM %s.documents
			WHERE doctype = 'ProductFamily' AND deleted_at IS NULL AND data->>'item_name' IN ($1, $2)`, schema),
			[]interface{}{r.id, r.code}, r, &out.Designs)
		if err != nil {
			return out, err
		}
		refs := append([]string{r.id, r.code}, designs...)
		if _, err := renameFrom(fmt.Sprintf(`SELECT id, data->>'code', data->>'name' FROM %s.documents
			WHERE doctype = 'Item' AND deleted_at IS NULL
			  AND (data->>'item_name' = ANY($1) OR data->>'family' = ANY($1) OR data->>'parent_product_code' = ANY($1))`, schema),
			[]interface{}{pq.Array(refs)}, r, &out.Items); err != nil {
			return out, err
		}
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	LogAuditEvent(tenantID, by, "ITEM_NAME_ORDER_CHANGED", "SUCCESS", fmt.Sprintf("%s -> %s: renamed %d item names, %d designs, %d items", from, to, out.ItemNames, out.Designs, out.Items))
	return out, nil
}

func init() {
	settingChangeHooks[SettingItemNameOrder] = func(tenantID, from, to, by string) error {
		if from != ItemNameOrderTypeCategory {
			from = ItemNameOrderCategoryType
		}
		_, err := ApplyItemNameOrder(tenantID, from, to, by)
		return err
	}
}

// ---------------------------------------------------------------------------
// What kind of item (89.7): the Item Type profiles in plain words, and what an
// item of each kind inherits.
// ---------------------------------------------------------------------------

// itemKindWords are the shipped profiles as a person would answer "what kind
// of item is this?"; a tenant's own types read "<name> - <description>".
var itemKindWords = map[string]string{
	"product":               "Normal product - bought and sold",
	"accessory":             "Accessory - sold alongside a main product",
	"packaging":             "Packaging used with a sale",
	"gift":                  "Free gift - given at zero price",
	"free with item":        "Free with another item",
	"gift card":             "Gift card - stored value",
	"service":               "Service - no stock kept",
	"consumable - periodic": "Consumable - counted and expensed periodically",
	"non-stock expense":     "Bought and expensed - no stock kept",
	"non-asset":             "Stock used in the business, not sold as merchandise",
	"backflush":             "Used up when production completes",
	"fixed asset":           "Fixed asset - bought, never sold",
	"virtual kit":           "Kit sold as one item",
}

// ItemKind is one Item Type as the Item form offers it.
type ItemKind struct {
	Code        string            `json:"code"`
	Label       string            `json:"label"`
	Description string            `json:"description,omitempty"`
	Inherits    map[string]string `json:"inherits"`
}

// itemKindInherits is what an Item of this type that sets nothing itself gets,
// per inheriting field, as the form shows it.
func itemKindInherits(tenantID, code string) map[string]string {
	item := map[string]interface{}{"item_type": code}
	p := ItemPolicyFor(tenantID, item)
	out := map[string]string{
		"commercial_class":     policyLabel(p.Class),
		"stock_tracking":       policyLabel(p.StockTracking),
		"valuation":            policyLabel(p.Valuation),
		"accounting_treatment": policyLabel(p.Accounting),
		"sellability":          policyLabel(p.Sellability),
		"bill_mode":            policyLabel(p.BillMode),
		"capture_method":       policyLabel(p.Capture),
		"consumption_method":   policyLabel(p.Consumption),
		"expense_account":      p.ExpenseAccount,
	}
	switch EffectiveBarcodeGenerateAt(tenantID, item) {
	case BarcodeAtSKUCreate:
		out["barcode_generate_at"] = "SKU creation"
	case BarcodeAtGRN:
		out["barcode_generate_at"] = "Goods receipt"
	default:
		out["barcode_generate_at"] = "Off"
	}
	out["barcode_per_receipt_date"] = "Fixed per SKU"
	if EffectiveBarcodePerReceiptDate(tenantID, item) {
		out["barcode_per_receipt_date"] = "New per receipt date"
	}
	switch EffectiveGRNBatchMode(tenantID, item) {
	case GRNBatchPerGRNSKU:
		out["grn_batch_number"] = "One per SKU on each GRN"
	case GRNBatchPerLine:
		out["grn_batch_number"] = "One per GRN line"
	case GRNBatchPerSKUDay:
		out["grn_batch_number"] = "One per SKU per day"
	default:
		out["grn_batch_number"] = "Off"
	}
	return out
}

// ListItemKinds is every active Item Type, Product first.
func ListItemKinds(tenantID string) ([]ItemKind, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text FROM %s.documents
		WHERE doctype = 'ItemType' AND deleted_at IS NULL AND COALESCE(data->>'status', 'Active') <> 'Inactive'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var kinds []ItemKind
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		code := firstNonEmpty(strField(d, "code"), id)
		desc := strings.TrimSpace(strField(d, "description"))
		label := itemKindWords[strings.ToLower(code)]
		if label == "" {
			label = code
			if desc != "" {
				label += " - " + desc
			}
		}
		kinds = append(kinds, ItemKind{Code: code, Label: label, Description: desc})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(kinds) == 0 {
		kinds = append(kinds, ItemKind{Code: ItemTypeProduct, Label: itemKindWords["product"]})
	}
	sort.SliceStable(kinds, func(i, j int) bool {
		pi, pj := strings.EqualFold(kinds[i].Code, ItemTypeProduct), strings.EqualFold(kinds[j].Code, ItemTypeProduct)
		if pi != pj {
			return pi
		}
		return kinds[i].Label < kinds[j].Label
	})
	for i := range kinds {
		kinds[i].Inherits = itemKindInherits(tenantID, kinds[i].Code)
	}
	return kinds, nil
}

// ---------------------------------------------------------------------------
// Where each field is asked (89.4).
// ---------------------------------------------------------------------------

// GRN asking levels.
const (
	GRNAskOff      = ""
	GRNAskOptional = "optional"
	GRNAskRequired = "required"
)

// ItemFieldRule is where one Item field or Item Attribute is asked. Key is the
// Item field name, or "attr:<code>" for an attribute.
type ItemFieldRule struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Kind   string `json:"kind"` // "field" | "attribute"
	Type   string `json:"type,omitempty"`
	Item   bool   `json:"item"`
	PO     bool   `json:"po"`
	GRN    string `json:"grn"`
	Hidden bool   `json:"hidden"`
	// Fixed: always asked on the Item form - what identifies, prices and taxes it.
	Fixed bool `json:"fixed,omitempty"`
	// List (a field): the Item Attribute whose values it is chosen from - a
	// free-text Color / Polish / Size becomes a dropdown of its own master (89.3).
	List string `json:"list,omitempty"`
	// LinkedField (an attribute): the Item field that carries it, so a form
	// asks it once, through that field.
	LinkedField string `json:"linked_field,omitempty"`
	// Options: the choices a form offers for it (a Select field's options, its
	// list's values, an attribute's values) - what the GRN line shows.
	Options []string `json:"options,omitempty"`
}

// itemFixedFields are asked on every Item form and nowhere else.
var itemFixedFields = map[string]bool{
	"code": true, "name": true, "item_type": true, "item_name": true, "category": true, "product_type": true,
	"hsn_code": true, "gst_rate": true, "tax_treatment": true, "barcode": true, "family": true,
	"parent_product_code": true, "variant_option_values": true, "sale_price": true, "mrp": true, "standard_cost": true,
}

// itemGRNDefaultFields are, unless configured otherwise, asked at the GRN and
// not on the Item: what differs piece by piece (user: "Weight and ask in GRN to
// fill", "Purity Ask in GRN").
var itemGRNDefaultFields = map[string]bool{
	"gross_weight": true, "net_weight": true, "stone_weight": true, "purity_karat": true,
}

// defaultHiddenItemFields are the Item fields that start hidden: a profile's
// free-text "type", which the item name's Product Type replaces. Its data is
// kept, and Configuration -> Inventory -> Item fields shows it again. Fields
// are never hidden for belonging to another industry's profile: a client set
// to the wrong industry would lose the very fields it uses.
func defaultHiddenItemFields() map[string]bool {
	return map[string]bool{"type": true}
}

// libraryReplacedItemFields (81.6, user decision 2026-10-22) are a jewellery
// profile's free-text variant fields. Once the org's attribute library has
// the same attribute, the field starts hidden and the attribute is asked on
// its own - one key for the value, not two. Data is kept, and a rule the org
// saved for the field always wins.
var libraryReplacedItemFields = map[string]bool{"color": true, "polish": true, "size": true, "stone_type": true}

type savedItemFieldRule struct {
	Item   *bool  `json:"item,omitempty"`
	PO     *bool  `json:"po,omitempty"`
	GRN    string `json:"grn"`
	Hidden bool   `json:"hidden,omitempty"`
	// List: an attribute code, "-" for "typed, not from a list", "" = matched by name.
	List string `json:"list,omitempty"`
}

// attributeMatchKey folds a field or attribute name for matching: letters
// only, and the British spellings ("Colour") read as the field names do.
func attributeMatchKey(s string) string {
	k := policyKey(s)
	k = strings.ReplaceAll(k, "colour", "color")
	return k
}

// matchAttributeForField is the attribute a field's values come from when the
// org has not said: same code or label ("color" ~ COLOR / Colour), or a label
// the field's starts with ("Polish / Plating" ~ Polish).
func matchAttributeForField(field, label string, attrs []itemAttributeDef) string {
	fk, lk := attributeMatchKey(field), attributeMatchKey(label)
	for _, a := range attrs {
		ak, al := attributeMatchKey(a.Code), attributeMatchKey(a.Label)
		if ak == fk || al == fk || al == lk || ak == lk {
			return a.Code
		}
	}
	for _, a := range attrs {
		if al := attributeMatchKey(a.Label); len(al) >= 4 && strings.HasPrefix(lk, al) {
			return a.Code
		}
	}
	return ""
}

func loadSavedItemFieldRules(tenantID string) map[string]savedItemFieldRule {
	saved := map[string]savedItemFieldRule{}
	if raw := strings.TrimSpace(rawSetting(tenantID, itemFieldRulesKey)); raw != "" {
		_ = json.Unmarshal([]byte(raw), &saved)
	}
	return saved
}

// ItemFieldRules is every Item field and active Item Attribute with where it is
// asked: what the org saved, else the default.
func ItemFieldRules(tenantID string) ([]ItemFieldRule, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	saved := loadSavedItemFieldRules(tenantID)
	hiddenByDefault := defaultHiddenItemFields()
	attrs, err := activeItemAttributes(schema)
	if err != nil {
		return nil, err
	}
	var out []ItemFieldRule
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT fieldname, label, fieldtype, COALESCE(options, '') FROM %s.doctype_fields
		WHERE doctype_name = 'Item' ORDER BY display_order, fieldname`, schema))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r ItemFieldRule
		var options string
		if err := rows.Scan(&r.Key, &r.Label, &r.Type, &options); err != nil {
			rows.Close()
			return nil, err
		}
		r.Kind = "field"
		r.Fixed = itemFixedFields[r.Key]
		r.Item = !itemGRNDefaultFields[r.Key]
		if itemGRNDefaultFields[r.Key] {
			r.GRN = GRNAskOptional
		}
		r.Hidden = hiddenByDefault[r.Key] && !r.Fixed
		s, hasSaved := saved[r.Key]
		if hasSaved && !r.Fixed {
			if s.Item != nil {
				r.Item = *s.Item
			}
			r.GRN, r.Hidden = s.GRN, s.Hidden
		}
		if r.Hidden {
			r.Item, r.GRN = false, GRNAskOff
		}
		if !r.Fixed && (r.Type == "Data" || r.Type == "Select") {
			switch {
			case hasSaved && s.List == "-":
			case hasSaved && s.List != "":
				r.List = s.List
			default:
				r.List = matchAttributeForField(r.Key, r.Label, attrs)
			}
		}
		if !hasSaved && libraryReplacedItemFields[r.Key] && r.List != "" {
			r.Hidden, r.Item, r.GRN = true, false, GRNAskOff
		}
		if r.Type == "Select" {
			for _, o := range strings.Split(options, ",") {
				if o = strings.TrimSpace(o); o != "" {
					r.Options = append(r.Options, o)
				}
			}
		}
		if r.List != "" {
			for _, a := range attrs {
				if strings.EqualFold(a.Code, r.List) {
					r.Options = a.Values
				}
			}
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	linked := map[string]string{}
	for _, r := range out {
		// A hidden field no longer carries its attribute, so the attribute
		// is asked on its own rather than not at all.
		if r.List != "" && !r.Hidden {
			linked[strings.ToUpper(r.List)] = r.Key
		}
	}
	for _, a := range attrs {
		r := ItemFieldRule{Key: "attr:" + a.Code, Label: a.Label, Kind: "attribute", Type: a.ValueType, Item: true, PO: true,
			LinkedField: linked[strings.ToUpper(a.Code)], Options: a.Values}
		if s, ok := saved[r.Key]; ok {
			if s.Item != nil {
				r.Item = *s.Item
			}
			if s.PO != nil {
				r.PO = *s.PO
			}
			r.GRN, r.Hidden = s.GRN, s.Hidden
		}
		if r.Hidden {
			r.Item, r.PO, r.GRN = false, false, GRNAskOff
		}
		out = append(out, r)
	}
	return out, nil
}

// SaveItemFieldRules stores where each field is asked. Fixed fields and
// unknown keys are ignored; a GRN level must be blank, optional or required.
func SaveItemFieldRules(tenantID, by string, rules []ItemFieldRule) error {
	current, err := ItemFieldRules(tenantID)
	if err != nil {
		return err
	}
	known := map[string]ItemFieldRule{}
	for _, r := range current {
		known[r.Key] = r
	}
	saved := map[string]savedItemFieldRule{}
	for _, r := range rules {
		k, ok := known[r.Key]
		if !ok || k.Fixed {
			continue
		}
		switch r.GRN {
		case GRNAskOff, GRNAskOptional, GRNAskRequired:
		default:
			return &ValidationError{Code: "MASTER-0890", Message: fmt.Sprintf("%s: asked at the GRN must be off, optional or required", k.Label)}
		}
		item, po := r.Item, r.PO
		s := savedItemFieldRule{Item: &item, GRN: r.GRN, Hidden: r.Hidden, List: strings.TrimSpace(r.List)}
		if k.Kind == "attribute" {
			s.PO = &po
		}
		saved[r.Key] = s
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	if err := storeRawSetting(tenantID, itemFieldRulesKey, string(raw), by); err != nil {
		return err
	}
	LogAuditEvent(tenantID, by, "ITEM_FIELD_RULES_SAVED", "SUCCESS", fmt.Sprintf("%d fields and attributes", len(saved)))
	return nil
}

// storeRawSetting writes a system_settings row that is not a registered
// (generic-page) setting, keeping the settings cache honest.
func storeRawSetting(tenantID, key, value, by string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.system_settings (key, value, updated_at, updated_by)
		VALUES ($1, $2, now(), $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now(), updated_by = EXCLUDED.updated_by`, schema),
		key, value, by); err != nil {
		return err
	}
	settingsCacheMu.Lock()
	delete(settingsCache, settingsCacheKey(schema, key))
	settingsCacheMu.Unlock()
	return nil
}

// itemAttributeDef is one active Item Attribute.
type itemAttributeDef struct {
	ID, Code, Label, ValueType string
	Values                     []string
}

func activeItemAttributes(schema string) ([]itemAttributeDef, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text FROM %s.documents
		WHERE doctype = 'ItemAttribute' AND deleted_at IS NULL AND COALESCE(data->>'status', 'Active') <> 'Inactive'
		ORDER BY created_at, id`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []itemAttributeDef
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		a := itemAttributeDef{ID: id, Code: firstNonEmpty(strField(d, "code"), id), ValueType: firstNonEmpty(strField(d, "value_type"), "Data")}
		a.Label = firstNonEmpty(strField(d, "label"), a.Code)
		for _, v := range strings.Split(strField(d, "value_options"), ",") {
			if v = strings.TrimSpace(v); v != "" {
				a.Values = append(a.Values, v)
			}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// The read model every item screen loads once (GET /api/v1/inventory/item-setup).
// ---------------------------------------------------------------------------

// ItemSetup is what the Item form, the PO and the GRN need to ask an item's
// fields the org's way.
type ItemSetup struct {
	Defaults         map[string]string `json:"defaults"`
	Kinds            []ItemKind        `json:"kinds"`
	DefaultKind      string            `json:"default_kind"`
	NameOrder        string            `json:"name_order"`
	Fields           []ItemFieldRule   `json:"fields"`
	AttributeMapping bool              `json:"attribute_mapping"`
	SecondAdmin      bool              `json:"second_admin"`
	// Company: what an Item Type's "Client default" means today (89.7.3).
	Company map[string]string `json:"company"`
}

// GetItemSetup assembles the read model.
func GetItemSetup(tenantID string) (*ItemSetup, error) {
	kinds, err := ListItemKinds(tenantID)
	if err != nil {
		return nil, err
	}
	fields, err := ItemFieldRules(tenantID)
	if err != nil {
		return nil, err
	}
	defaults := map[string]string{
		"tax_treatment": TaxTreatmentTaxable,
		"tracking_mode": firstNonEmpty(GetSettingString(tenantID, SettingDefaultTrackingMode), "None"),
	}
	for _, d := range itemDefaultNumberFields {
		if n := GetSettingInt(tenantID, d.setting); n > 0 {
			defaults[d.field] = strconv.Itoa(n)
		}
	}
	company := map[string]string{"barcode_generate_at": "Off", "barcode_per_receipt_date": "Fixed per SKU", "grn_batch_number": "Off"}
	switch BarcodeGenerateAt(tenantID) {
	case BarcodeAtSKUCreate:
		company["barcode_generate_at"] = "SKU creation"
	case BarcodeAtGRN:
		company["barcode_generate_at"] = "Goods receipt"
	}
	if BarcodePerReceiptDate(tenantID) {
		company["barcode_per_receipt_date"] = "New per receipt date"
	}
	switch GRNBatchMode(tenantID) {
	case GRNBatchPerGRNSKU:
		company["grn_batch_number"] = "One per SKU on each GRN"
	case GRNBatchPerLine:
		company["grn_batch_number"] = "One per GRN line"
	case GRNBatchPerSKUDay:
		company["grn_batch_number"] = "One per SKU per day"
	}
	return &ItemSetup{
		Defaults: defaults, Kinds: kinds, DefaultKind: kinds[0].Code, NameOrder: ItemNameOrder(tenantID),
		Fields: fields, AttributeMapping: GetSettingBool(tenantID, SettingAttributeTypeMapping),
		SecondAdmin: GetSettingBool(tenantID, SettingTreatmentSecondAdmin), Company: company,
	}, nil
}
