package engines

// Stage 28.1: the first-wave setting definitions. Each maps a value that was
// previously a hardcoded constant to a tenant-editable setting whose Default
// equals the old constant, so behavior is unchanged until an admin edits it.
// Registration order = the order the admin Settings screen renders within each
// module; the screen groups by the Module field.
//
// This is the single, growing home for every future config: adding one is a
// RegisterSetting call here plus a GetSetting* read where the value is used.

func settingBound(n int) *int { return &n }

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
}
