package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"unicode"
)

// Item policy (Stage 72, the user's "Item & Material Master" blueprint,
// 2026-10-17). The blueprint's one rule: do not let a single item-type list
// answer six different questions. Every item carries eight independent
// dimensions, and behaviour is read from those - never from a type's name:
//
//	commercial_class     what it is to the business (descriptive only)
//	stock_tracking       do we keep on-hand quantity by location
//	valuation            does that quantity carry inventory value
//	accounting_treatment where its cost or value posts
//	sellability          how the till may transact it
//	bill_mode            whether the customer's bill shows it
//	capture_method       how the till captures it
//	consumption_method   how quantity leaves inventory
//
// The ItemType master is the reusable profile (blueprint section 5: many items
// to one profile); an Item may override any dimension. Precedence: the Item's
// own value, then its Item Type's, then the meaning the Stage 60 behaviour
// flags (sell at zero price, free with item, backflush, fixed asset) already
// gave that type, then the company default - an ordinary stocked, valuated,
// sellable merchandise line. So a tenant that configures nothing behaves
// exactly as before.

// Canonical dimension values. Select labels ("Promotional / GWP", "Not
// sellable") and these constants both parse - see policyValue.
const (
	ClassMerchandise = "MERCHANDISE"
	ClassAccessory   = "ACCESSORY"
	ClassPromotional = "PROMOTIONAL_GWP"
	ClassPackaging   = "PACKAGING"
	ClassConsumable  = "CONSUMABLE"
	ClassService     = "SERVICE"
	ClassStoredValue = "STORED_VALUE"
	ClassFixedAsset  = "FIXED_ASSET"
	ClassInternalMRO = "INTERNAL_MRO"
	ClassBundleKit   = "BUNDLE_KIT"

	StockTracked    = "TRACKED"
	StockNotTracked = "NOT_TRACKED"

	ValuationValuated    = "VALUATED"
	ValuationNonValuated = "NON_VALUATED"
	ValuationNA          = "NOT_APPLICABLE"

	AccountingInventoryAsset = "INVENTORY_ASSET"
	AccountingExpense        = "EXPENSE"
	AccountingFixedAsset     = "FIXED_ASSET"
	AccountingStoredValue    = "STORED_VALUE_LIABILITY"
	AccountingService        = "SERVICE"

	SellNormal          = "NORMAL_SALE"
	SellPromoOnly       = "PROMO_ONLY"
	SellNot             = "NOT_SELLABLE"
	SellStoredValueOnly = "STORED_VALUE_ISSUE_ONLY"

	BillVisible  = "VISIBLE_LINE"
	BillHidden   = "HIDDEN_INTERNAL"
	BillSeparate = "SEPARATE_RECEIPT"

	CaptureScan   = "SCAN"
	CaptureQuick  = "QUICK_SELECT"
	CaptureAuto   = "AUTO"
	CaptureManual = "MANUAL"
	CaptureNone   = "NONE"

	ConsumeSaleIssue     = "SALE_ISSUE"
	ConsumeAutoBackflush = "AUTO_BACKFLUSH"
	ConsumeManual        = "MANUAL_ISSUE"
	ConsumePeriodic      = "PERIODIC"
	ConsumeProduction    = "PRODUCTION"
	ConsumeNone          = "NONE"
)

// Default GL accounts for expensed consumption (db/migrations_stage72_1_item_policy.sql).
const (
	AccountPackagingExpense   = "5160"
	AccountPromotionalExpense = "5170"
	AccountConsumablesExpense = "5180"
	AccountStoredValueLiab    = "2610"
)

// policyDimension is one of the eight fields, with the values it accepts.
type policyDimension struct {
	field  string
	label  string
	values []string
}

var policyDimensions = []policyDimension{
	{"commercial_class", "Commercial class", []string{ClassMerchandise, ClassAccessory, ClassPromotional, ClassPackaging, ClassConsumable, ClassService, ClassStoredValue, ClassFixedAsset, ClassInternalMRO, ClassBundleKit}},
	{"stock_tracking", "Stock tracking", []string{StockTracked, StockNotTracked}},
	{"valuation", "Valuation", []string{ValuationValuated, ValuationNonValuated, ValuationNA}},
	{"accounting_treatment", "Accounting treatment", []string{AccountingInventoryAsset, AccountingExpense, AccountingFixedAsset, AccountingStoredValue, AccountingService}},
	{"sellability", "Sellability at POS", []string{SellNormal, SellPromoOnly, SellNot, SellStoredValueOnly}},
	{"bill_mode", "Customer bill", []string{BillVisible, BillHidden, BillSeparate}},
	{"capture_method", "POS capture", []string{CaptureScan, CaptureQuick, CaptureAuto, CaptureManual, CaptureNone}},
	{"consumption_method", "Consumption", []string{ConsumeSaleIssue, ConsumeAutoBackflush, ConsumeManual, ConsumePeriodic, ConsumeProduction, ConsumeNone}},
}

// ItemPolicy is an item's resolved behaviour.
type ItemPolicy struct {
	Class          string `json:"commercial_class"`
	StockTracking  string `json:"stock_tracking"`
	Valuation      string `json:"valuation"`
	Accounting     string `json:"accounting_treatment"`
	Sellability    string `json:"sellability"`
	BillMode       string `json:"bill_mode"`
	Capture        string `json:"capture_method"`
	Consumption    string `json:"consumption_method"`
	ExpenseAccount string `json:"expense_account"`
	Profile        string `json:"profile"`
	// Source says where each dimension came from: "item", "item_type",
	// "behaviour_flags" or "default" - what Item -> Resolved policy shows.
	Source map[string]string `json:"source"`
}

// Tracked reports whether the item keeps on-hand quantity.
func (p ItemPolicy) Tracked() bool { return p.StockTracking != StockNotTracked }

// Valuated reports whether on-hand quantity carries inventory value, so an
// issue posts its cost out of Inventory (1200).
func (p ItemPolicy) Valuated() bool { return p.Tracked() && p.Valuation == ValuationValuated }

// policyKey folds a label or constant to letters and digits, lower case:
// "Promotional / GWP", "promotional_gwp" and "PROMOTIONAL_GWP" all become
// "promotionalgwp".
func policyKey(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(v) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// policyValue maps a stored label to the dimension's constant; "" means the
// value inherits (blank, "Item type default", anything unrecognised).
func policyValue(dim policyDimension, raw interface{}) string {
	s, _ := raw.(string)
	k := policyKey(s)
	if k == "" {
		return ""
	}
	for _, v := range dim.values {
		if policyKey(v) == k {
			return v
		}
	}
	return ""
}

// policyLabel is how a constant reads on screen and in messages.
func policyLabel(v string) string {
	switch v {
	case ClassPromotional:
		return "Promotional / GWP"
	case ClassInternalMRO:
		return "Internal / MRO"
	case ClassBundleKit:
		return "Bundle / Kit"
	case SellStoredValueOnly:
		return "Stored-value issue only"
	case BillHidden:
		return "Hidden (internal)"
	case ValuationNonValuated:
		return "Non-valuated"
	}
	s := strings.ToLower(strings.ReplaceAll(v, "_", " "))
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// defaultItemPolicy is the company default: an ordinary merchandise line.
func defaultItemPolicy() map[string]string {
	return map[string]string{
		"commercial_class":     ClassMerchandise,
		"stock_tracking":       StockTracked,
		"valuation":            ValuationValuated,
		"accounting_treatment": AccountingInventoryAsset,
		"sellability":          SellNormal,
		"bill_mode":            BillVisible,
		"capture_method":       CaptureScan,
		"consumption_method":   ConsumeSaleIssue,
	}
}

// legacyPolicy is what a type's Stage 60 behaviour flags already meant, for a
// type whose dimension fields were never filled in (a tenant-made type, or a
// row from before this Stage's migration).
func legacyPolicy(r ItemTypeRules) map[string]string {
	out := map[string]string{}
	switch {
	case r.FixedAsset:
		// A GRN already raises fixed assets as Asset records, never as stock.
		out["commercial_class"] = ClassFixedAsset
		out["stock_tracking"] = StockNotTracked
		out["accounting_treatment"] = AccountingFixedAsset
		out["sellability"] = SellNot
		out["consumption_method"] = ConsumeNone
	case r.SellAtZeroPrice:
		out["commercial_class"] = ClassPromotional
		out["sellability"] = SellPromoOnly
	}
	if r.Backflush {
		out["consumption_method"] = ConsumeAutoBackflush
	}
	return out
}

// ItemPolicyFor resolves an Item's behaviour from its own record and its type.
func ItemPolicyFor(tenantID string, itemData map[string]interface{}) ItemPolicy {
	typeName := ItemTypeName(itemData)
	rules := ItemTypeRulesFor(tenantID, typeName)
	legacy := legacyPolicy(rules)
	defaults := defaultItemPolicy()
	p := ItemPolicy{Profile: rules.Name, Source: map[string]string{}}
	set := func(field, v string) {
		switch field {
		case "commercial_class":
			p.Class = v
		case "stock_tracking":
			p.StockTracking = v
		case "valuation":
			p.Valuation = v
		case "accounting_treatment":
			p.Accounting = v
		case "sellability":
			p.Sellability = v
		case "bill_mode":
			p.BillMode = v
		case "capture_method":
			p.Capture = v
		case "consumption_method":
			p.Consumption = v
		}
	}
	for _, dim := range policyDimensions {
		switch {
		case policyValue(dim, itemData[dim.field]) != "":
			set(dim.field, policyValue(dim, itemData[dim.field]))
			p.Source[dim.field] = "item"
		case rules.Policy[dim.field] != "":
			set(dim.field, rules.Policy[dim.field])
			p.Source[dim.field] = "item_type"
		case legacy[dim.field] != "":
			set(dim.field, legacy[dim.field])
			p.Source[dim.field] = "behaviour_flags"
		default:
			set(dim.field, defaults[dim.field])
			p.Source[dim.field] = "default"
		}
	}
	// Not tracked has no on-hand to value, whatever the profile says.
	if p.StockTracking == StockNotTracked {
		p.Valuation = ValuationNA
	}
	p.ExpenseAccount = strings.TrimSpace(pimString(itemData["expense_account"]))
	if p.ExpenseAccount == "" {
		p.ExpenseAccount = rules.ExpenseAccount
	}
	if p.ExpenseAccount == "" {
		p.ExpenseAccount = defaultExpenseAccount(p.Class)
	}
	return p
}

// defaultExpenseAccount is where an expensed issue of this class posts when
// neither the item nor its profile names an account.
func defaultExpenseAccount(class string) string {
	switch class {
	case ClassPackaging:
		return AccountPackagingExpense
	case ClassPromotional:
		return AccountPromotionalExpense
	}
	return AccountConsumablesExpense
}

// ItemPolicyForSKU resolves by SKU code, barcode or id. ok is false when no
// Item matches - callers keep their existing behaviour for an unknown SKU.
func ItemPolicyForSKU(tenantID, sku string) (ItemPolicy, map[string]interface{}, bool) {
	item, err := ResolveItemBySKU(tenantID, sku)
	if err != nil || item == nil {
		return ItemPolicy{}, nil, false
	}
	return ItemPolicyFor(tenantID, item.Data), item.Data, true
}

// policyFromRecord reads the eight dimension fields of an ItemType record.
func policyFromRecord(data map[string]interface{}) map[string]string {
	out := map[string]string{}
	for _, dim := range policyDimensions {
		if v := policyValue(dim, data[dim.field]); v != "" {
			out[dim.field] = v
		}
	}
	return out
}

// ValidateItemPolicyCompatibility applies the blueprint's section 15.2 rules
// to an Item or ItemType being saved. It reads only what the record itself
// sets (an Item over its type), so a profile is checked on its own terms and
// an item is checked as it will actually behave.
func ValidateItemPolicyCompatibility(tenantID, doctype string, payload map[string]interface{}) error {
	var p ItemPolicy
	if doctype == "ItemType" {
		dims := policyFromRecord(payload)
		defaults := defaultItemPolicy()
		for field, v := range defaults {
			if dims[field] == "" {
				dims[field] = v
			}
		}
		p = ItemPolicy{Class: dims["commercial_class"], StockTracking: dims["stock_tracking"], Valuation: dims["valuation"],
			Accounting: dims["accounting_treatment"], Sellability: dims["sellability"], BillMode: dims["bill_mode"],
			Capture: dims["capture_method"], Consumption: dims["consumption_method"]}
		if p.StockTracking == StockNotTracked && policyValue(policyDimensions[2], payload["valuation"]) == "" {
			p.Valuation = ValuationNA
		}
	} else {
		p = ItemPolicyFor(tenantID, payload)
		if v := policyValue(policyDimensions[2], payload["valuation"]); v != "" {
			p.Valuation = v // judge what the user typed, not the folded value
		}
	}
	fail := func(code, sub, msg string) error {
		return &ValidationError{Code: code, SubFor: sub, Message: msg}
	}
	if p.StockTracking == StockNotTracked && p.Valuation == ValuationValuated {
		return fail("MASTER-0402", "valuation", "an item that is not stock tracked has no on-hand to value - set Valuation to Not applicable, or Stock tracking to Tracked")
	}
	if p.StockTracking == StockTracked && p.Valuation == ValuationNA {
		return fail("MASTER-0402", "valuation", "a stock-tracked item is either Valuated or Non-valuated - Not applicable is only for items that are not tracked")
	}
	if p.Valuation == ValuationValuated && p.Accounting != AccountingInventoryAsset {
		return fail("MASTER-0401", "accounting_treatment", fmt.Sprintf("valuated stock is an inventory asset - set Accounting treatment to Inventory asset, or Valuation to Non-valuated (it is %s)", policyLabel(p.Accounting)))
	}
	if p.Accounting == AccountingFixedAsset && p.Class != ClassFixedAsset {
		return fail("MASTER-0403", "commercial_class", "only a Fixed Asset item can carry Fixed asset accounting - it is raised as an Asset on receipt, never held as stock for sale")
	}
	if (p.Class == ClassStoredValue) != (p.Sellability == SellStoredValueOnly) {
		return fail("MASTER-0404", "sellability", "a Stored Value item (gift card / voucher) is issued only through Gift Card -> Issue, and only a Stored Value item may use Stored-value issue only")
	}
	if p.Class == ClassStoredValue && p.Accounting != AccountingStoredValue {
		return fail("MASTER-0404", "accounting_treatment", "a Stored Value item posts to the stored-value liability - set Accounting treatment to Stored value liability")
	}
	if (p.Consumption == ConsumeAutoBackflush || p.Consumption == ConsumePeriodic) && p.StockTracking == StockNotTracked {
		return fail("MASTER-0405", "consumption_method", fmt.Sprintf("%s consumption takes stock out of a store, so the item must be stock tracked", policyLabel(p.Consumption)))
	}
	if acct := strings.TrimSpace(pimString(payload["expense_account"])); acct != "" {
		if err := requireGLAccount(tenantID, acct); err != nil {
			return fail("MASTER-0406", "expense_account", err.Error())
		}
	}
	return nil
}

// requireGLAccount refuses an account code the tenant's chart does not have.
func requireGLAccount(tenantID, code string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var exists bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.gl_accounts WHERE account_code = $1)`, schema), code).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("GL account %s is not in the chart of accounts", code)
	}
	return nil
}

// guardPostedItemPolicyChange is the blueprint's section 7.2 rule (ITM-018):
// stock tracking, valuation and accounting treatment cannot be changed freely
// once an item has stock history - the quantities and values already posted
// were posted under the old treatment. A successor item is the clean way.
func guardPostedItemPolicyChange(tenantID, docID string, payload map[string]interface{}) error {
	if strings.TrimSpace(docID) == "" {
		return nil
	}
	old, _, err := fetchDocData(tenantID, "Item", docID)
	if err != nil || old == nil {
		return nil
	}
	before, after := ItemPolicyFor(tenantID, old), ItemPolicyFor(tenantID, payload)
	if !postingTreatmentChanged(before, after) {
		return nil
	}
	sku := strings.TrimSpace(pimString(old["code"]))
	if sku == "" {
		sku = docID
	}
	if !skusHaveStockHistory(tenantID, sku) {
		return nil
	}
	return &ValidationError{Code: "MASTER-0407", SubFor: "item_type", Message: fmt.Sprintf(
		"%s already has stock history, so its stock tracking, valuation and accounting treatment cannot change (%s) - create a new item for the new treatment and stop using this one",
		itemLabel(old, sku), describeTreatmentChange(before, after))}
}

// guardPostedProfileChange applies the same rule to an Item Type: changing a
// profile's treatment changes every item that uses it.
func guardPostedProfileChange(tenantID, docID string, payload map[string]interface{}) error {
	if strings.TrimSpace(docID) == "" {
		return nil
	}
	old, _, err := fetchDocData(tenantID, "ItemType", docID)
	if err != nil || old == nil {
		return nil
	}
	resolve := func(data map[string]interface{}) ItemPolicy {
		rules := ItemTypeRules{Name: docID, Policy: policyFromRecord(data),
			SellAtZeroPrice: yesValue(data["sell_at_zero_price"]), Backflush: yesValue(data["backflush"]), FixedAsset: yesValue(data["fixed_asset"])}
		return policyFromRules(rules)
	}
	before, after := resolve(old), resolve(payload)
	if !postingTreatmentChanged(before, after) {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	var used bool
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT EXISTS (
		  SELECT 1 FROM %[1]s.documents i
		  JOIN %[1]s.documents s ON s.doctype = 'StockLedgerEntry' AND s.data->>'item_id' = i.data->>'code'
		  WHERE i.doctype = 'Item' AND i.deleted_at IS NULL
		    AND lower(CASE WHEN COALESCE(i.data->>'item_type', '') IN ('', 'Stock') THEN 'Product' ELSE i.data->>'item_type' END) = lower($1))`, schema),
		docID).Scan(&used)
	if !used {
		return nil
	}
	return &ValidationError{Code: "MASTER-0407", SubFor: "stock_tracking", Message: fmt.Sprintf(
		"items of type %s already have stock history, so the type's stock tracking, valuation and accounting treatment cannot change (%s) - create a new item type for the new treatment",
		docID, describeTreatmentChange(before, after))}
}

// policyFromRules resolves a profile on its own (no item overrides).
func policyFromRules(rules ItemTypeRules) ItemPolicy {
	legacy := legacyPolicy(rules)
	dims := defaultItemPolicy()
	for field := range dims {
		if v := rules.Policy[field]; v != "" {
			dims[field] = v
		} else if v := legacy[field]; v != "" {
			dims[field] = v
		}
	}
	p := ItemPolicy{Class: dims["commercial_class"], StockTracking: dims["stock_tracking"], Valuation: dims["valuation"],
		Accounting: dims["accounting_treatment"], Sellability: dims["sellability"], BillMode: dims["bill_mode"],
		Capture: dims["capture_method"], Consumption: dims["consumption_method"]}
	if p.StockTracking == StockNotTracked {
		p.Valuation = ValuationNA
	}
	return p
}

func postingTreatmentChanged(a, b ItemPolicy) bool {
	return a.StockTracking != b.StockTracking || a.Valuation != b.Valuation || a.Accounting != b.Accounting
}

func describeTreatmentChange(a, b ItemPolicy) string {
	var parts []string
	if a.StockTracking != b.StockTracking {
		parts = append(parts, fmt.Sprintf("stock tracking %s -> %s", policyLabel(a.StockTracking), policyLabel(b.StockTracking)))
	}
	if a.Valuation != b.Valuation {
		parts = append(parts, fmt.Sprintf("valuation %s -> %s", policyLabel(a.Valuation), policyLabel(b.Valuation)))
	}
	if a.Accounting != b.Accounting {
		parts = append(parts, fmt.Sprintf("accounting %s -> %s", policyLabel(a.Accounting), policyLabel(b.Accounting)))
	}
	return strings.Join(parts, ", ")
}

// skusHaveStockHistory reports whether any stock ledger entry names the SKU.
func skusHaveStockHistory(tenantID, sku string) bool {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var exists bool
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1)`, schema), sku).Scan(&exists)
	return exists
}

// RefuseNonStockItems is the blueprint's ITM-002 (Stage 72.9): an item that
// keeps no stock (service, non-stock expense, virtual kit) cannot appear on a
// stock transaction. verb completes "... cannot be <verb>".
func RefuseNonStockItems(tenantID string, skus []string, verb string) error {
	for _, sku := range skus {
		policy, item, ok := ItemPolicyForSKU(tenantID, strings.TrimSpace(sku))
		if ok && !policy.Tracked() {
			return &ValidationError{Code: "INVENT-0421", SubFor: sku, Message: fmt.Sprintf(
				"%s keeps no stock (%s, Stock tracking: Not tracked), so it cannot be %s", itemLabel(item, sku), policyLabel(policy.Class), verb)}
		}
	}
	return nil
}
