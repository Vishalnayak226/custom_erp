package engines

// Stage 47.13.2: optional help text on a doctype field - a few words saying
// what the less obvious fields mean. It rides on FieldMeta.Help, shows as the
// field's tooltip on the form, and is the "definition" of the field's column
// in the import column guide. A field with no entry here behaves as before.
// Additive: filling more fields is only adding lines.
var fieldHelp = map[string]map[string]string{
	"Item": {
		"hsn_code":            "Tax classification code, 4 to 8 digits; sets the GST rate",
		"tracking_mode":       "Whether stock is tracked by batch, serial number, both or neither",
		"mrp":                 "Maximum retail price printed on the pack; a sale cannot go above it",
		"family":              "The product family (design) this item is a variant of",
		"barcode":             "The code scanned at the till; leave blank to generate one",
		"gtin":                "Global trade item number, if the maker printed one",
		"standard_cost":       "Planned cost per unit, used when no receipt cost exists",
		"tax_treatment":       "Taxable, or exempt / nil-rated / zero-rated (then the rate must be 0)",
		"parent_product_code": "The item this one is a variant of, if any",
		"free_with":           "The item that is given free with this one",
		"item_type":           "The item type whose defaults this item follows",
		"shelf_life_days":     "Days the goods stay sellable after they are made",
	},
	"Vendor": {
		"gstin":     "15-character GST number; leave blank for an unregistered supplier",
		"bank_ifsc": "11-character bank branch code, used when paying the supplier",
		"status":    "Inactive suppliers cannot be chosen on new orders",
	},
	"Customer": {
		"price_list_code": "The price list used for this customer instead of the default",
		"credit_limit":    "Most the customer may owe on credit; 0 means no credit",
		"loyalty_tier":    "Loyalty level; normally set by the points earned",
		"merged_into":     "Set when this record was merged into another customer",
		"walk_in":         "Yes for the shared walk-in record used for anonymous sales",
		"consent_sms":     "Whether the customer agreed to receive SMS",
	},
	"PurchaseOrder": {
		"items":                  "Lines as JSON: item, qty, purchase price and optional MRP",
		"gst_mode":               "Whether line prices include GST (inclusive) or not (exclusive)",
		"interstate_override":    "Tick to force interstate tax when the supplier's state is unclear",
		"payment_term":           "The payment term that sets the due date",
		"place_of_supply":        "The state taxed as the place of supply",
		"expected_delivery_date": "The date the supplier promised to deliver",
		"target_warehouse":       "The warehouse the goods are to be received into",
	},
}
