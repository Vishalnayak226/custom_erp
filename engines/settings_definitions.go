package engines

import "fmt"

// Stage 28.1: the first-wave setting definitions. Each maps a value that was
// previously a hardcoded constant to a tenant-editable setting whose Default
// equals the old constant, so behavior is unchanged until an admin edits it.
// Registration order = the order the admin Settings screen renders within each
// module; the screen groups by the Module field.
//
// This is the single, growing home for every future config: adding one is a
// RegisterSetting call here plus a GetSetting* read where the value is used.

// settingBound builds a Min/Max bound. float64 so one helper serves both int
// and float settings - a whole-number literal converts implicitly.
func settingBound(n float64) *float64 { return &n }

func init() {
	// --- Loyalty ---
	RegisterSetting(SettingDefinition{
		Key: "loyalty.point_expiry_days", Module: "Loyalty",
		Label: "Point expiry", Type: SettingTypeInt, Default: "365", Unit: "days",
		Min:         settingBound(1),
		Description: "How long an earned loyalty-point lot stays valid before the expiry sweep burns it.",
	})
	RegisterSetting(SettingDefinition{
		Key: "loyalty.rupees_per_point", Module: "Loyalty",
		Label: "Spend per point earned", Type: SettingTypeInt, Default: "100", Unit: "₹ per point",
		Min:         settingBound(1),
		Description: "Net sale amount a customer must spend to earn one loyalty point.",
	})
	RegisterSetting(SettingDefinition{
		Key: "loyalty.recompute_tier_on_earn", Module: "Loyalty",
		Label: "Recompute customer tier on each earn", Type: SettingTypeBool, Default: "true",
		Description: "When on, a customer's loyalty tier is re-evaluated against lifetime spend every time they earn points.",
	})
	// Stage 73.3.1: what a tier is earned on.
	RegisterSetting(SettingDefinition{
		Key: "loyalty.tier_window_days", Module: "Loyalty",
		Label: "Tier spend counted over the last", Type: SettingTypeInt, Default: "0", Unit: "days (0 = all time)",
		Min:         settingBound(0),
		Description: "A customer's tier is set by what they spent (net of offers and refunds). 0 counts all time, so a tier is never lost; 365 counts the last year, so spend that ages out can lower it - after the tier's grace days.",
	})
	// Stage 73.2.12: how points are accounted for.
	RegisterSetting(SettingDefinition{
		Key: "loyalty.liability_accounting", Module: "Loyalty",
		Label: "Account for loyalty points", Type: SettingTypeSelect, Default: "redemption",
		Options: []SettingOption{
			{Value: "redemption", Label: "As an expense when they are redeemed (5250)"},
			{Value: "accrue", Label: "As a liability when they are earned (2630 / 5260)"},
		},
		Description: "Accrual books the value of points earned as Loyalty Points Expense and a Loyalty Points Liability, releases the liability as points are redeemed, given back or expire, and lets the Loyalty Points Movement report reconcile it to the ledger. Turning it on does not book points already outstanding - post that opening balance by journal.",
	})
	// Stage 73.1.4: redemption controls on every path that burns points.
	RegisterSetting(SettingDefinition{
		Key: "loyalty.redemption_requires_otp", Module: "Loyalty",
		Label: "Customer confirms every redemption with a one-time code", Type: SettingTypeBool, Default: "false",
		Description: "When on, points can only be redeemed - at the till or through the API - after the customer reads back a one-time code sent to them. Needs a Loyalty Redemption OTP notification template. Redemptions at or above the LoyaltyRedemptionRequest approval rule also need a manager's approval.",
	})
	// Stage 73.1.3: points a return gives back.
	RegisterSetting(SettingDefinition{
		Key: "loyalty.restored_points_grace_days", Module: "Loyalty",
		Label: "Returned points stay valid for at least", Type: SettingTypeInt, Default: "30", Unit: "days",
		Min:         settingBound(0),
		Description: "When a returned item was paid for with points, those points go back to the customer. They keep the latest expiry the customer's points had at the sale, but never less than this many days from the return.",
	})
	// Stage 55.5a: second-person approval of returns and refunds - off until
	// a business turns it on.
	RegisterSetting(SettingDefinition{
		Key: "returns.second_person_approval", Module: "OMS",
		Label: "A second person approves returns and refunds", Type: SettingTypeBool, Default: "false",
		Description: "When on, the person who raised a return cannot approve it or approve its refund - someone else must. Receiving, inspecting and paying the refund out are unchanged, and an exchange at the till is still approved at the counter. Off: anyone with Returns can approve, as before.",
	})

	// --- Inventory ---
	RegisterSetting(SettingDefinition{
		Key: "inventory.reservation_ttl_seconds", Module: "Inventory",
		Label: "Online reservation hold", Type: SettingTypeInt, Default: "86400", Unit: "seconds",
		Min:         settingBound(60),
		Description: "How long an online/channel stock reservation is held before it expires and the stock is released back to available.",
	})
	// Stage 57.7: barcode policy - see engines/barcode_policy.go.
	RegisterSetting(SettingDefinition{
		Key: "inventory.barcode_generate_at", Module: "Inventory",
		Label: "Generate barcodes at", Type: SettingTypeSelect, Default: "sku_create",
		Options: []SettingOption{
			{Value: "sku_create", Label: "SKU creation - every new Item gets its barcode when saved"},
			{Value: "grn", Label: "Goods receipt - an Item gets its barcode the first time it is received"},
			{Value: "off", Label: "Off - barcodes are only scanned, typed or generated by hand"},
		},
		Description: "When a SKU's permanent barcode is issued automatically. A barcode you type or scan yourself is always kept.",
	})
	RegisterSetting(SettingDefinition{
		Key: "inventory.barcode_per_receipt_date", Module: "Inventory",
		Label: "New barcode for each receipt date", Type: SettingTypeSelect, Default: "no",
		Options: []SettingOption{
			{Value: "no", Label: "No - the same barcode for a SKU every time"},
			{Value: "yes", Label: "Yes - each day a SKU is received gets its own barcode (printed on that receipt's stickers)"},
		},
		Description: "For stock you age or rotate by arrival date. Every dated barcode still scans as its SKU, and is listed under Setup > Item Barcode.",
	})
	// Stage 61.6: GRN batch numbers - see engines/wms_receiving.go.
	RegisterSetting(SettingDefinition{
		Key: "inventory.grn_batch_number", Module: "Inventory",
		Label: "GRN batch numbers", Type: SettingTypeSelect, Default: GRNBatchPerGRNSKU,
		Options: []SettingOption{
			{Value: GRNBatchPerGRNSKU, Label: "One per SKU on each GRN - every line of a SKU on one receipt shares it"},
			{Value: GRNBatchPerLine, Label: "One per GRN line - each line gets its own"},
			{Value: GRNBatchPerSKUDay, Label: "One per SKU per day - every receipt of a SKU on one day shares it"},
			{Value: GRNBatchOff, Label: "Off - only batch numbers typed on the GRN"},
		},
		Description: "How a GRN line left without a batch number gets one. An Item Type or an Item can choose differently. A typed number is always kept, and a batch-tracked item always needs the lot from the carton. The number's look is the GRNBatchSeq number series.",
	})
	// Stage 89: what a new Item starts with (engines/item_setup.go). Copied
	// onto the item when it is created, so changing one later moves only the
	// items made after - a tracked item's mode never shifts under its stock.
	RegisterSetting(SettingDefinition{
		Key: SettingDefaultTrackingMode, Module: "Inventory",
		Label: "Traceability for new items", Type: SettingTypeSelect, Default: "None",
		Options: []SettingOption{
			{Value: "None", Label: "None - quantity only"},
			{Value: "Batch", Label: "Batch - every receipt carries a lot"},
			{Value: "Serial", Label: "Serial - every piece has its own number"},
			{Value: "Batch and Serial", Label: "Batch and serial"},
		},
		Description: "What a new item is set to unless it chooses otherwise. Changing this does not change items that already exist.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingDefaultShelfLifeDays, Module: "Inventory",
		Label: "Shelf life for new items", Type: SettingTypeInt, Default: "0", Unit: "days", Min: settingBound(0),
		Description: "The standard shelf life a new item starts with. 0 = no shelf life. Each item can change its own.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingDefaultMinShelfLifeReceipt, Module: "Inventory",
		Label: "Minimum shelf life left on receipt, for new items", Type: SettingTypeInt, Default: "0", Unit: "days", Min: settingBound(0),
		Description: "A GRN refuses stock with less life left than this. 0 = no check. Each item can change its own.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingDefaultMinShelfLifePick, Module: "Inventory",
		Label: "Minimum shelf life left when picked, for new items", Type: SettingTypeInt, Default: "0", Unit: "days", Min: settingBound(0),
		Description: "Picking skips stock with less life left than this. 0 = no check. Each item can change its own.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingItemNameOrder, Module: "Inventory",
		Label: "Item name order", Type: SettingTypeSelect, Default: ItemNameOrderCategoryType,
		Options: []SettingOption{
			{Value: ItemNameOrderCategoryType, Label: "Category + Type - Antique Necklace"},
			{Value: ItemNameOrderTypeCategory, Label: "Type + Category - Necklace Antique"},
		},
		Description: "How an item name is built from its category and type. Switching renames every item name still built the old way, and the designs and items named from it; switching back renames them back. A name typed by hand and bills already printed are not changed.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingAttributeTypeMapping, Module: "Inventory",
		Label: "Attributes follow the item's type", Type: SettingTypeBool, Default: "false",
		Description: "Off: every attribute and value is offered for every item. On: an item, a PO line and a GRN line offer only the attributes and values mapped to the item's type (or its category) - set on each Product Type and Category. A type with nothing mapped still offers everything.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingTreatmentSecondAdmin, Module: "Inventory",
		Label: "A second admin approves item treatment corrections", Type: SettingTypeBool, Default: "false",
		Description: "Off: an admin's correction of an item's stock tracking, valuation or accounting (after it has stock) applies once they confirm it. On: it waits for another admin's approval.",
	})

	// --- Security ---
	RegisterSetting(SettingDefinition{
		Key: SettingSelfApproval, Module: "Security",
		Label: "Who can approve their own documents", Type: SettingTypeSelect, Default: SelfApprovalAnyone,
		Options: []SettingOption{
			{Value: SelfApprovalAnyone, Label: "Anyone who can approve it"},
			{Value: SelfApprovalAdmins, Label: "Admins only"},
			{Value: SelfApprovalNobody, Label: "Nobody - someone else always decides"},
		},
		Description: "Whether the person who submitted a document (a PO, a requisition, a voucher...) can approve or reject it themselves. They still need the role the approval rule names. Each one is marked \"decided by the submitter\" in the approval history and the audit log. A Super Admin's password reset and a second-admin treatment correction always need a second admin.",
	})
	RegisterSetting(SettingDefinition{
		Key: "security.loyalty_otp_validity_minutes", Module: "Security",
		Label: "Loyalty redemption OTP validity", Type: SettingTypeInt, Default: "5", Unit: "minutes",
		Min: settingBound(1), Max: settingBound(60),
		Description: "How long a one-time password for a secured loyalty redemption stays valid before it must be re-issued.",
	})
	RegisterSetting(SettingDefinition{
		Key: "security.loyalty_max_redemptions_per_day", Module: "Security",
		Label: "Max loyalty redemptions per customer per day", Type: SettingTypeInt, Default: "5",
		Min:         settingBound(1),
		Description: "Daily cap on customer-initiated loyalty redemption attempts - a lightweight velocity/fraud guard.",
	})
	RegisterSetting(SettingDefinition{
		Key: "security.default_idle_timeout_minutes", Module: "Security",
		Label: "Default idle-timeout for new users", Type: SettingTypeSelect, Default: "720", Unit: "minutes",
		Options: []SettingOption{
			{Value: "0", Label: "Never"},
			{Value: "15", Label: "15 minutes"},
			{Value: "30", Label: "30 minutes"},
			{Value: "60", Label: "60 minutes"},
			{Value: "120", Label: "120 minutes"},
			{Value: "720", Label: "12 hours"},
		},
		Description: "The client-side auto-logout timer a newly created user starts with - 12 hours by default, so a working shift is never cut short. Each user can change their own on the Profile screen afterwards.",
	})

	registerStage282Settings()
}

// registerStage282Settings is Stage 30.7's sweep: every remaining hardcoded
// operational constant found across the engines, moved here so the
// Configuration screen is the single place any of them is changed. Each
// Default is byte-for-byte the constant it replaced, so an untouched tenant
// behaves exactly as before.
//
// Every key registered here is read at its consuming site via GetSetting* on
// each use (never captured once at startup), which is what makes an edit take
// effect immediately everywhere without a restart.
func registerStage282Settings() {
	// --- Sales & Returns ---
	RegisterSetting(SettingDefinition{
		Key: "sales.return_window_days", Module: "Sales & Returns",
		Label: "Sales return window", Type: SettingTypeInt, Default: "30", Unit: "days",
		Min:         settingBound(0),
		Description: "How many days after the original sale a customer return is still accepted (SALESR-0129). 0 disables returns entirely.",
	})

	// --- Procurement ---
	RegisterSetting(SettingDefinition{
		Key: "procurement.vendor_invoice_tolerance_percent", Module: "Procurement",
		Label: "Vendor invoice 3-way match tolerance", Type: SettingTypeFloat, Default: "2", Unit: "%",
		Min: settingBound(0), Max: settingBound(100),
		Description: "How far a vendor invoice may differ from the PO/GRN value before it is held for review instead of auto-matched.",
	})
	RegisterSetting(SettingDefinition{
		Key: "procurement.po_edit_window_days", Module: "Procurement",
		Label: "Purchase Order edit window", Type: SettingTypeInt, Default: "0", Unit: "days",
		Min:         settingBound(0),
		Description: "How many days after creation a Purchase Order may still be edited. 0 means no time limit (the existing approval and post-receipt rules still apply).",
	})
	RegisterSetting(SettingDefinition{
		Key: "procurement.grn_edit_window_days", Module: "Procurement",
		Label: "GRN edit window", Type: SettingTypeInt, Default: "0", Unit: "days",
		Min:         settingBound(0),
		Description: "How many days after creation a Goods Receipt Note may still be edited. 0 means no time limit.",
	})
	// Day limits (the user, 2026-10-17): read by po_auto_cancel.go and
	// purchase_return.go.
	RegisterSetting(SettingDefinition{
		Key: "procurement.po_auto_cancel_days", Module: "Procurement",
		Label: "Purchase Order auto-cancel", Type: SettingTypeInt, Default: "0", Unit: "days",
		Min:         settingBound(0),
		Description: "How many days after approval an Approved Purchase Order stays open. Then, checked every hour: with nothing received it is cancelled; partly received, its open balance is short-closed (rejected goods go on a draft purchase return). Already-older POs are handled on the first check. 0 = never.",
	})
	RegisterSetting(SettingDefinition{
		Key: "procurement.purchase_return_window_days", Module: "Procurement",
		Label: "Purchase return window", Type: SettingTypeInt, Default: "0", Unit: "days",
		Min:         settingBound(0),
		Description: "How many days after a goods receipt its accepted stock may still be returned to the vendor (PURCHA-0620). Rejected and damaged goods can always be returned. 0 = no limit.",
	})
	// Stage 40.1; default Inclusive since Stage 110.2 (user, 2026-10-19): the
	// price typed on a PO line already has GST in it. Individual POs override
	// it on the PO screen when a particular vendor quotes before GST. POs
	// saved earlier on "Tenant default" keep Exclusive (ResolvePOGSTMode).
	RegisterSetting(SettingDefinition{
		Key: "procurement.po_gst_mode", Module: "Procurement",
		Label: "Purchase Order price GST treatment", Type: SettingTypeSelect, Default: GSTModeInclusive,
		Options: []SettingOption{
			{Value: GSTModeExclusive, Label: "Exclusive - GST is added on top of the purchase price"},
			{Value: GSTModeInclusive, Label: "Inclusive - the purchase price already contains GST"},
		},
		Description: "Whether the Purchase Price entered on a PO line already includes GST. Each PO can override this.",
	})
	// Stage 130.7 (120.5): the company the app header and tab title name.
	RegisterSetting(SettingDefinition{
		Key: "platform.company_name", Module: "Platform",
		Label: "Company name in the header", Type: SettingTypeString, Default: "",
		Description: "Shown beside the Wholeops logo and in the browser tab's title, so everyone sees whose ERP this is. Blank uses the brand name on prints; with neither, nothing is shown.",
	})
	// Stage 63 (engines/print_documents.go): the brand every designed print carries.
	RegisterSetting(SettingDefinition{
		Key: "print.brand_name", Module: "Printing",
		Label: "Brand name on prints", Type: SettingTypeString, Default: "",
		Description: "Printed at the top of every PO, GRN, transfer and debit / credit note - even on a template that does not place it. Blank uses the legal entity's name of the store the print is generated from.",
	})
	// Stage 62.6 (engines/po_design.go).
	RegisterSetting(SettingDefinition{
		Key: "procurement.po_design_number_mode", Module: "Procurement",
		Label: "Design number when a PO adds an item name's design", Type: SettingTypeSelect, Default: DesignNumberModeNew,
		Options: []SettingOption{
			{Value: DesignNumberModeNew, Label: "Always new design number - each design added gets a new design number"},
			{Value: DesignNumberModeMatch, Label: "Inherit from existing - the PO vendor's design with the same name and every design attribute is brought back, with its combinations"},
		},
		Description: "What a Purchase Order does with the design number when an item name's design is added with the design number left blank. Inherit searches only the PO vendor's designs (a design with no vendor still matches). Typing an existing design number always loads that design, whichever is chosen.",
	})

	// --- Point of Sale ---
	RegisterSetting(SettingDefinition{
		Key: "pos.drawer_variance_tolerance", Module: "Point of Sale",
		Label: "Cash drawer variance tolerance", Type: SettingTypeFloat, Default: "50", Unit: "₹",
		Min:         settingBound(0),
		Description: "Counted-vs-expected cash difference a cashier may close a session with before a written variance reason becomes mandatory (POSOFF-0240).",
	})
	// Stage 47.2 (audit A-02). Assisted is the default deliberately: every
	// tenant on this product today prices entirely at the till - Item.sale_price
	// did not exist before this stage - so defaulting to strict would stop all
	// of them selling on the next deploy. Assisted still closes the A-02 hole
	// for any item the tenant HAS priced (master price wins absolutely, the
	// client's figure is ignored) and routes an unpriceable line through
	// discount approval instead of letting it through unchecked. Strict is the
	// end state, reachable per tenant once its catalogue carries prices.
	RegisterSetting(SettingDefinition{
		Key: "pos.pricing_mode", Module: "Point of Sale",
		Label: "POS pricing authority", Type: SettingTypeSelect, Default: PricingModeAssisted,
		Options: []SettingOption{
			{Value: PricingModeAssisted, Label: "Assisted - server price wins; an unpriced item may be keyed in, and needs approval"},
			{Value: PricingModeStrict, Label: "Strict - refuse to sell any item that has no server-side price"},
		},
		Description: "Where a POS line's price comes from. In both modes a price the server can resolve (price list, or the item's Sale Price/MRP) overrides anything the till sends. Strict additionally refuses to sell an item nothing on the server prices.",
	})
	// Stage 85.C4: high-value bills. Blank = the home country's rule
	// (engines/pos_customers.go countryHighValueDefaults); 0 = no rule.
	// Stage 85.E7: a bill discount above this % needs the cashier's reason.
	RegisterSetting(SettingDefinition{
		Key: "pos.manual_discount.reason_above_pct", Module: "Point of Sale",
		Label: "Discount needs a reason above", Type: SettingTypeFloat, Default: "5", Unit: "%",
		Min:         settingBound(0),
		Description: "A bill discount the cashier gives above this percentage needs a reason, printed nowhere and kept on the bill. Above an approval slab (Approval Rules, POSCart) it also waits for a manager, as before.",
	})
	RegisterSetting(SettingDefinition{
		Key: "pos.high_value.address_from", Module: "Point of Sale",
		Label: "Buyer's address needed from (bill total)", Type: SettingTypeFloat, Default: "", Optional: true, Min: settingBound(0),
		Description: "A bill of this total or more needs the buyer's address before it completes. Blank uses your country's rule (India: 50,000 to a buyer with no GSTIN, CGST Rule 46(e); UK and Germany: 250; UAE: 10,000). 0 turns it off.",
	})
	RegisterSetting(SettingDefinition{
		Key: "pos.high_value.pan_above", Module: "Point of Sale",
		Label: "PAN needed above (bill total)", Type: SettingTypeFloat, Default: "", Optional: true, Min: settingBound(0),
		Description: "A bill above this total needs the buyer's PAN, or a signed Form 97 when they have none. Blank uses your country's rule (India: 2,00,000, Income-tax Rules r.159). 0 turns it off.",
	})
	RegisterSetting(SettingDefinition{
		Key: "pos.high_value.cash_limit", Module: "Point of Sale",
		Label: "Cash refused from (per bill)", Type: SettingTypeFloat, Default: "", Optional: true, Min: settingBound(0),
		Description: "Cash of this much or more on one bill is refused; the rest is taken by card or UPI. Blank uses your country's rule (India: 2,00,000, s.269ST). 0 turns it off.",
	})
	RegisterSetting(SettingDefinition{
		Key: "pos.default_price_list", Module: "Point of Sale",
		Label: "Default price list", Type: SettingTypeString, Default: "",
		Description: "The PriceListVersion price_list_code the POS prices from when the customer has no contract price list of their own. Leave empty to price from the item master (Sale Price, then MRP).",
	})

	// --- Loyalty (Stage 30.7 additions) ---
	RegisterSetting(SettingDefinition{
		Key: "loyalty.redemption_value_per_point", Module: "Loyalty",
		Label: "Redemption value per point", Type: SettingTypeInt, Default: "1", Unit: "₹ per point",
		Min:         settingBound(1),
		Description: "What one loyalty point is worth in rupees when a customer burns it at checkout. Also drives the outstanding loyalty-liability report.",
	})

	// Stage 47.5.1 (audit A-05). Single-owner is the SUPPORTED configuration,
	// not merely the default. Stage 59.6: mixed owners is supported too -
	// orders, pick tasks, allocation, availability and every bin movement
	// carry the owner (engines/wms_owner_isolation.go), so one client's order
	// is only ever filled from that client's stock. The pre-59 value
	// "mixed_unsupported" still reads as mixed.
	RegisterSetting(SettingDefinition{
		Key: StockOwnershipModeSetting, Module: "Warehouse",
		Label: "Stock ownership model", Type: SettingTypeSelect, Default: OwnershipSingleOwner,
		Options: []SettingOption{
			{Value: OwnershipSingleOwner, Label: "One owner per warehouse"},
			{Value: OwnershipMixed, Label: "Several owners per warehouse (stock kept apart per owner)"},
		},
		Description: "Whether a warehouse may hold stock for more than one 3PL owner (client). With several owners, each order names its owner and is picked only from that owner's stock; putaway, moves and condition changes keep each owner's units apart, and Reports → Owner Stock Exceptions lists anything that no longer adds up.",
	})

	// --- Manufacturing ---
	RegisterSetting(SettingDefinition{
		Key: "manufacturing.max_bom_explosion_depth", Module: "Manufacturing",
		Label: "Maximum BOM nesting depth", Type: SettingTypeInt, Default: "10", Unit: "levels",
		Min: settingBound(1), Max: settingBound(50),
		Description: "How deep a bill of materials may nest before explosion aborts. Also the circular-reference guard, so keep it modest.",
	})
	RegisterSetting(SettingDefinition{
		Key: "manufacturing.production_cost_variance_tolerance_percent", Module: "Manufacturing",
		Label: "Production cost variance tolerance", Type: SettingTypeFloat, Default: "10", Unit: "%",
		Min: settingBound(0), Max: settingBound(100),
		Description: "How far actual production cost may drift from standard cost before the order is flagged as a cost variance.",
	})
	RegisterSetting(SettingDefinition{
		Key: "manufacturing.default_lead_time_days", Module: "Manufacturing",
		Label: "Default MRP lead time", Type: SettingTypeInt, Default: "7", Unit: "days",
		Min:         settingBound(0),
		Description: "Lead time MRP assumes for an item that has no explicit lead time of its own.",
	})

	// --- HR & Payroll ---
	RegisterSetting(SettingDefinition{
		Key: "hr.esi_wage_ceiling", Module: "HR & Payroll",
		Label: "ESI wage ceiling", Type: SettingTypeFloat, Default: "21000", Unit: "₹ gross/month",
		Min:         settingBound(0),
		Description: "Gross monthly wage at or below which ESI is deducted. Mirrors India's statutory ceiling - editable here so a statutory revision needs no code change.",
	})

	// --- CRM ---
	RegisterSetting(SettingDefinition{
		Key: "crm.churn_days", Module: "CRM",
		Label: "Customer churn threshold", Type: SettingTypeInt, Default: "90", Unit: "days",
		Min:         settingBound(1),
		Description: "Days without a purchase after which a customer counts as churned in lifetime-value and RFM analytics.",
	})
	RegisterSetting(SettingDefinition{
		Key: "crm.default_lapsed_days", Module: "CRM",
		Label: "Default lapsed-customer threshold", Type: SettingTypeInt, Default: "90", Unit: "days",
		Min:         settingBound(1),
		Description: "Fallback inactivity period a Lapsed-Customer campaign uses when the campaign itself does not specify one.",
	})

	// --- Warehouse ---
	RegisterSetting(SettingDefinition{
		Key: "wms.cycle_count_tier_a_interval_days", Module: "Warehouse",
		Label: "Cycle count interval - A items", Type: SettingTypeInt, Default: "30", Unit: "days",
		Min:         settingBound(1),
		Description: "How often fast-moving (A-tier) SKUs are proposed for a cycle count.",
	})
	RegisterSetting(SettingDefinition{
		Key: "wms.cycle_count_tier_b_interval_days", Module: "Warehouse",
		Label: "Cycle count interval - B items", Type: SettingTypeInt, Default: "60", Unit: "days",
		Min:         settingBound(1),
		Description: "How often medium-moving (B-tier) SKUs are proposed for a cycle count.",
	})
	RegisterSetting(SettingDefinition{
		Key: "wms.cycle_count_tier_c_interval_days", Module: "Warehouse",
		Label: "Cycle count interval - C items", Type: SettingTypeInt, Default: "90", Unit: "days",
		Min:         settingBound(1),
		Description: "How often slow-moving (C-tier) SKUs are proposed for a cycle count.",
	})
	RegisterSetting(SettingDefinition{
		Key: "wms.productivity_threshold_minutes", Module: "Warehouse",
		Label: "Task productivity alert threshold", Type: SettingTypeFloat, Default: "120", Unit: "minutes",
		Min:         settingBound(1),
		Description: "How long a warehouse task may take before it is surfaced as a productivity outlier.",
	})

	// --- PIM ---
	RegisterSetting(SettingDefinition{
		Key: "pim.max_bulk_edit_documents", Module: "PIM",
		Label: "Maximum documents per bulk edit", Type: SettingTypeInt, Default: "100", Unit: "documents",
		Min: settingBound(1), Max: settingBound(1000),
		Description: "Upper bound on how many products one bulk-edit operation may change at once.",
	})
	RegisterSetting(SettingDefinition{
		Key: "pim.thumbnail_max_dim", Module: "PIM",
		Label: "Product thumbnail size", Type: SettingTypeInt, Default: "200", Unit: "pixels",
		Min: settingBound(32), Max: settingBound(2000),
		Description: "Longest edge of a generated product-image thumbnail. Applies to newly uploaded images.",
	})

	// --- Security (Stage 30.7 additions) ---
	RegisterSetting(SettingDefinition{
		Key: "security.session_token_ttl_hours", Module: "Security",
		Label: "Session token lifetime", Type: SettingTypeInt, Default: "24", Unit: "hours",
		Min: settingBound(1), Max: settingBound(720),
		Description: "How long an issued login token stays valid. Long enough to cover a shift, short enough that a leaked token expires. Overridden by the JWT_EXPIRY_HOURS environment variable only while this setting is left at its default.",
	})
	RegisterSetting(SettingDefinition{
		Key: "security.password_reset_ttl_minutes", Module: "Security",
		Label: "Password-reset link validity", Type: SettingTypeInt, Default: "30", Unit: "minutes",
		Min: settingBound(1), Max: settingBound(1440),
		Description: "How long a password-reset link remains usable after it is emailed.",
	})
	RegisterSetting(SettingDefinition{
		Key: "security.account_lockout_threshold", Module: "Security",
		Label: "Failed logins before lockout", Type: SettingTypeInt, Default: "10", Unit: "attempts",
		Min: settingBound(1), Max: settingBound(100),
		Description: "Consecutive failed password attempts that lock an account. Deliberately more permissive than the per-IP rate limiter, which already stops single-source bursts.",
	})
	RegisterSetting(SettingDefinition{
		Key: "security.account_lockout_duration_minutes", Module: "Security",
		Label: "Account lockout duration", Type: SettingTypeInt, Default: "15", Unit: "minutes",
		Min: settingBound(1), Max: settingBound(1440),
		Description: "How long an account stays locked after hitting the failed-login threshold.",
	})
	RegisterSetting(SettingDefinition{
		Key: "security.totp_skew_steps", Module: "Security",
		Label: "Two-factor clock-drift tolerance", Type: SettingTypeInt, Default: "1", Unit: "× 30s steps",
		Min: settingBound(0), Max: settingBound(10),
		Description: "How far a TOTP code may be out of step with server time and still be accepted. 1 tolerates ±30 seconds of drift. Raising this widens the window an intercepted code stays usable.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingMFARememberDays, Module: "Security",
		Label: "Remember a device for", Type: SettingTypeInt, Default: "30", Unit: "days",
		Min: settingBound(0), Max: settingBound(90),
		Description: "Stage 125.8: after a correct authenticator code, Admin and Super Admin may tick \"Remember this device\"; that browser then skips the code (never the password) for this many days. A password change, a 2FA reset or a new authenticator ends it. 0 turns the option off and makes every remembered device ask for the code again. Lowering it shortens devices already remembered.",
	})
	RegisterSetting(SettingDefinition{
		Key: "security.password_min_length", Module: "Security",
		Label: "Minimum password length", Type: SettingTypeInt, Default: "12", Unit: "characters",
		Min: settingBound(8), Max: settingBound(128),
		Description: "Stage 49.2.2: shortest password ValidatePasswordStrength accepts for a self-service change, a reset completion or an admin-created account. Does not retroactively affect an existing password shorter than this.",
	})

	// --- Platform ---
	// These are safety limits rather than business policy: configurable (so
	// nothing is hardcoded) but each carries a registry Min/Max that
	// validateSettingValue enforces, so a mistyped value is rejected at save
	// time instead of destabilizing the server.
	RegisterSetting(SettingDefinition{
		Key: "platform.default_list_limit", Module: "Platform",
		Label: "Default list page size", Type: SettingTypeInt, Default: "500", Unit: "rows",
		Min: settingBound(1), Max: settingBound(5000),
		Description: "How many rows a list endpoint returns when the caller requests no explicit limit, so a list can never be unbounded.",
	})
	RegisterSetting(SettingDefinition{
		Key: "platform.max_list_limit", Module: "Platform",
		Label: "Maximum list page size", Type: SettingTypeInt, Default: "1000", Unit: "rows",
		Min: settingBound(1), Max: settingBound(10000),
		Description: "Hard cap on the page size a caller may explicitly request. Raising it materially increases per-request memory.",
	})
	RegisterSetting(SettingDefinition{
		Key: "platform.per_tenant_max_concurrent_requests", Module: "Platform",
		Label: "Concurrent requests per tenant", Type: SettingTypeInt, Default: "15", Unit: "requests",
		Min: settingBound(1), Max: settingBound(500),
		Description: "How many requests one tenant may have in flight at once before further ones are shed, so a single tenant cannot starve the others.",
	})
	RegisterSetting(SettingDefinition{
		Key: "platform.import_batch_rows", Module: "Platform",
		Label: "CSV import batch size", Type: SettingTypeInt, Default: "500", Unit: "rows",
		Min: settingBound(50), Max: settingBound(10000),
		Description: "How many CSV rows share one database transaction during a bulk import. Larger batches import faster but hold locks longer.",
	})
	RegisterSetting(SettingDefinition{
		Key: "platform.max_sync_report_rows", Module: "Platform",
		Label: "Maximum rows for an on-screen report", Type: SettingTypeInt, Default: "5000", Unit: "rows",
		Min: settingBound(100), Max: settingBound(100000),
		Description: "Row count above which a report must be exported rather than rendered synchronously (REPORT-0161).",
	})
	RegisterSetting(SettingDefinition{
		Key: "platform.max_import_rows", Module: "Platform",
		Label: "Maximum CSV import rows", Type: SettingTypeInt, Default: "20000", Unit: "rows",
		Min: settingBound(100), Max: settingBound(1000000),
		Description: "Row count above which a bulk CSV/template import is rejected outright rather than partially processed (DATAIM-0189).",
	})
	RegisterSetting(SettingDefinition{
		Key: "platform.report_export_retention_days", Module: "Platform",
		Label: "Report export job retention", Type: SettingTypeInt, Default: "7", Unit: "days",
		Min: settingBound(1), Max: settingBound(365),
		Description: "How long a completed or failed report export job's stored CSV is kept before being swept, so an export nobody downloads doesn't grow the documents table forever.",
	})
	RegisterSetting(SettingDefinition{
		Key: "platform.field_max_length", Module: "Platform",
		Label: "Default maximum field length", Type: SettingTypeInt, Default: "10000", Unit: "characters",
		Min: settingBound(100), Max: settingBound(1000000),
		Description: "Blanket per-field character cap applied to any document field that does not declare its own stricter limit.",
	})
	// Stage 34.3. Default 0 = the undercut worker does nothing, so a tenant
	// that never loads competitor prices is never alerted about them; a buyer
	// opts in by setting a threshold. Bounded at 90 because "a competitor is
	// 95% cheaper" is a data-entry error, not a pricing signal.
	RegisterSetting(SettingDefinition{
		Key: "market.undercut_threshold_pct", Module: "Platform",
		Label: "Competitor undercut alert threshold", Type: SettingTypeFloat, Default: "0", Unit: "%",
		Min: settingBound(0), Max: settingBound(90),
		Description: "Alert when the cheapest competitor observation is at least this far below our last transacted price. 0 disables competitor undercut alerts.",
	})
	// Stage 38.3/38.5/38.9. These are the tenant-wide defaults; an individual
	// credential may override the two budgets, so pinning one noisy integration
	// never means lowering the ceiling for every other key.
	// Stage 37.1.2. The currency every ledger amount, report and approval slab
	// is denominated in. Changing it does NOT re-translate existing postings -
	// it declares what they were always in - so it is a provisioning-time
	// decision, not an operational one.
	RegisterSetting(SettingDefinition{
		Key: SettingKeyFunctionalCurrency, Module: "Finance",
		Label: "Functional (reporting) currency", Type: SettingTypeString, Default: "INR",
		Description: "The ISO currency code every ledger posting, report and approval threshold is expressed in. Documents may be transacted in other currencies and are converted to this one at their exchange rate.",
	})
	// Stage 37.1.4. Which rate a period-end revaluation quotes open foreign
	// balances at. Closing is what every accounting standard names for a
	// balance-sheet item and is the default; it is configurable because a
	// tenant whose rate feed only publishes Spot must still be able to close a
	// period rather than be unable to revalue at all.
	RegisterSetting(SettingDefinition{
		Key: SettingKeyRevaluationRateType, Module: "Finance",
		Label: "FX revaluation rate type", Type: SettingTypeSelect, Default: "Closing",
		Options: []SettingOption{
			{Value: "Closing", Label: "Closing"},
			{Value: "Spot", Label: "Spot"},
			{Value: "Average", Label: "Average"},
		},
		Description: "The exchange-rate type used to restate open foreign-currency receivables and payables at a period end. Changing it does not alter revaluations already posted.",
	})
	RegisterSetting(SettingDefinition{
		Key: "platform.public_api_rate_limit_per_minute", Module: "Platform",
		Label: "Public API requests per minute per credential", Type: SettingTypeInt, Default: "120", Unit: "requests",
		Min: settingBound(1), Max: settingBound(100000),
		Description: "Burst budget for one public API credential. Exceeding it returns 429 with Retry-After; it does not affect internal application requests.",
	})
	RegisterSetting(SettingDefinition{
		Key: "platform.public_api_daily_quota", Module: "Platform",
		Label: "Public API daily quota per credential", Type: SettingTypeInt, Default: "50000", Unit: "requests",
		Min: settingBound(1), Max: settingBound(10000000),
		Description: "How many public API calls one credential may make between midnights (server time). Counted from the API traffic log, so it survives a restart.",
	})
	RegisterSetting(SettingDefinition{
		Key: "platform.public_api_log_retention_days", Module: "Platform",
		Label: "Public API traffic log retention", Type: SettingTypeInt, Default: "30", Unit: "days",
		Min: settingBound(1), Max: settingBound(365),
		Description: "How long per-credential API traffic rows are kept before the sweeper deletes them. Must exceed one day or the daily quota loses its history.",
	})
	RegisterSetting(SettingDefinition{
		Key: "platform.public_api_idempotency_retention_hours", Module: "Platform",
		Label: "Idempotency key retention", Type: SettingTypeInt, Default: "24", Unit: "hours",
		Min: settingBound(1), Max: settingBound(720),
		Description: "How long a completed public API idempotency key can still replay its stored response. After this window the same key is treated as a new request.",
	})
	RegisterSetting(SettingDefinition{
		Key: "platform.job_runner_retention_days", Module: "Platform",
		Label: "Async job history retention", Type: SettingTypeInt, Default: "30", Unit: "days",
		Min: settingBound(1), Max: settingBound(365),
		Description: "How long a completed, dead-lettered or cancelled async job (Stage 38.6) stays visible in the job history before the hourly sweeper deletes it. Jobs still Pending or Leased are never swept.",
	})

	// --- OMS Settlement Reconciliation (Stage 35.8) ---
	RegisterSetting(SettingDefinition{
		Key: "oms.settlement_variance_tolerance", Module: "OMS",
		Label: "Settlement auto-match tolerance", Type: SettingTypeFloat, Default: "2.00", Unit: "₹",
		Min:         settingBound(0),
		Description: "A settlement line whose gross amount differs from the matched order's invoiced value by no more than this is auto-matched and posted; a larger gap is held as a Variance for an operator to dispute or write off.",
	})
	// Stage 88.3.1 (D2): channel orders wait at Pending unless this is on.
	RegisterSetting(SettingDefinition{
		Key: "oms.accept_channel_orders", Module: "OMS",
		Label: "Accept channel orders automatically", Type: SettingTypeBool, Default: "false",
		Description: "Off: a marketplace or website order whose stock is reserved waits on OMS ▸ Pending Orders until someone accepts or rejects it, and the answer is sent to the channel. On: it is accepted as it arrives and goes straight to Pick. Orders keyed in by staff always skip Pending.",
	})
	RegisterSetting(SettingDefinition{
		Key: "oms.settlement_aging_days", Module: "OMS",
		Label: "Unsettled order aging threshold", Type: SettingTypeInt, Default: "7", Unit: "days",
		Min:         settingBound(1),
		Description: "How many days after a marketplace order ships it is flagged on the Unsettled Settlements report if no settlement line has matched it yet.",
	})

	// --- Dunning (Stage 37.4.4) ---
	RegisterSetting(SettingDefinition{
		Key: "finance.dunning_reminder_days", Module: "Finance",
		Label: "Dunning reminder threshold", Type: SettingTypeInt, Default: "7", Unit: "days",
		Min:         settingBound(1),
		Description: "An Approved SalesInvoice with a due_date this many days in the past gets a Reminder notification.",
	})
	RegisterSetting(SettingDefinition{
		Key: "finance.dunning_escalation_days", Module: "Finance",
		Label: "Dunning escalation threshold", Type: SettingTypeInt, Default: "30", Unit: "days",
		Min:         settingBound(1),
		Description: "An Approved SalesInvoice this many days past due_date gets an Escalation notification instead of a Reminder.",
	})

	// BLD-052: queue saturation thresholds are tenant-tunable operational
	// guardrails. Defaults are intentionally conservative and affect alerts
	// only; they never reject accepted work or alter retry/idempotency behavior.
	RegisterSetting(SettingDefinition{
		Key: "ops.async_job_queue_depth_alert", Module: "Platform",
		Label: "Async job queue alert depth", Type: SettingTypeInt, Default: "100", Unit: "ready jobs",
		Min: settingBound(1), Max: settingBound(100000),
		Description: "Alert operations when this tenant has at least this many due, pending async jobs.",
	})
	RegisterSetting(SettingDefinition{
		Key: "ops.async_job_queue_wait_seconds_alert", Module: "Platform",
		Label: "Async job queue oldest-age alert", Type: SettingTypeInt, Default: "600", Unit: "seconds",
		Min: settingBound(30), Max: settingBound(86400),
		Description: "Alert operations when the oldest due, pending async job has waited this many seconds.",
	})

	registerLocalizationSettings()
}

// SettingKeyDefaultCountry is the tenant's home country. It is referenced by
// name from engines/phone.go rather than typed as a literal at each consuming
// site, so the key can never drift between where it is declared and where it
// is read.
const SettingKeyDefaultCountry = "localization.default_country"

// registerLocalizationSettings (Stage 41) introduces the tenant's country as
// a first-class setting. Everything phone-shaped in the product derives from
// it: how many digits a number must have, which trunk/dial prefixes are
// stripped when cleaning, and what counts as a foreign order.
//
// The option list is generated from engines/phone.go's own country table
// instead of being retyped here, so adding a country is one row in that table
// and nothing else - the same "register once, drive generically" posture the
// rest of this registry uses.
func registerLocalizationSettings() {
	countries := PhoneCountries()
	opts := make([]SettingOption, 0, len(countries))
	for _, c := range countries {
		opts = append(opts, SettingOption{
			Value: c.ISO2,
			Label: fmt.Sprintf("%s (+%s, %s)", c.Name, c.DialCode, c.LengthLabel()),
		})
	}
	RegisterSetting(SettingDefinition{
		Key: SettingKeyDefaultCountry, Module: "Localization",
		Label: "Home country", Type: SettingTypeSelect, Default: DefaultPhoneCountry,
		Options: opts,
		Description: "The country this business operates from. Sets the phone-number length every module validates against " +
			"(POS, Customer master, HR, vendors) and the dial code stripped when cleaning imported data. Orders arriving " +
			"from other countries are still accepted and are tagged with their own country for targeting.",
	})
}
