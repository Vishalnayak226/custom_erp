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

	// --- Inventory ---
	RegisterSetting(SettingDefinition{
		Key: "inventory.reservation_ttl_seconds", Module: "Inventory",
		Label: "Online reservation hold", Type: SettingTypeInt, Default: "86400", Unit: "seconds",
		Min:         settingBound(60),
		Description: "How long an online/channel stock reservation is held before it expires and the stock is released back to available.",
	})

	// --- Security ---
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
		Label: "Default idle-timeout for new users", Type: SettingTypeSelect, Default: "30", Unit: "minutes",
		Options: []SettingOption{
			{Value: "0", Label: "Never"},
			{Value: "15", Label: "15 minutes"},
			{Value: "30", Label: "30 minutes"},
			{Value: "60", Label: "60 minutes"},
			{Value: "120", Label: "120 minutes"},
		},
		Description: "The client-side auto-logout timer a newly created user starts with. Each user can change their own on the Profile screen afterwards.",
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
	// Stage 40.1. Defaults to Exclusive: a vendor's quoted purchase price is
	// normally the taxable value with GST added on top, which is the opposite
	// of this codebase's sale-side rate fields (MRP-style, tax already in).
	// Individual POs override it on the PO screen when a particular vendor
	// quotes landed prices.
	RegisterSetting(SettingDefinition{
		Key: "procurement.po_gst_mode", Module: "Procurement",
		Label: "Purchase Order price GST treatment", Type: SettingTypeSelect, Default: GSTModeExclusive,
		Options: []SettingOption{
			{Value: GSTModeExclusive, Label: "Exclusive - GST is added on top of the purchase price"},
			{Value: GSTModeInclusive, Label: "Inclusive - the purchase price already contains GST"},
		},
		Description: "Whether the Purchase Price entered on a PO line already includes GST. Each PO can override this.",
	})

	// --- Point of Sale ---
	RegisterSetting(SettingDefinition{
		Key: "pos.drawer_variance_tolerance", Module: "Point of Sale",
		Label: "Cash drawer variance tolerance", Type: SettingTypeFloat, Default: "50", Unit: "₹",
		Min:         settingBound(0),
		Description: "Counted-vs-expected cash difference a cashier may close a session with before a written variance reason becomes mandatory (POSOFF-0240).",
	})

	// --- Loyalty (Stage 30.7 additions) ---
	RegisterSetting(SettingDefinition{
		Key: "loyalty.redemption_value_per_point", Module: "Loyalty",
		Label: "Redemption value per point", Type: SettingTypeInt, Default: "1", Unit: "₹ per point",
		Min:         settingBound(1),
		Description: "What one loyalty point is worth in rupees when a customer burns it at checkout. Also drives the outstanding loyalty-liability report.",
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
