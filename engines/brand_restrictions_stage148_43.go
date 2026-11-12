package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
)

// Stage 148.43: brand-restricted dealers. A dealer may be limited to a list of
// brands (a Brand master name or code, comma separated) in one of three modes:
// None, Warn (the order is placed and carries a note) or Block (the order is
// refused), optionally between two dates. The check runs where an order's
// lines are first read, CreateSalesOrder, and where a line is added to an open
// order. Setting off, or a customer with no list, is exactly today: no lookup
// beyond the setting, no refusal, no field offered.
const (
	SettingBrandRestrictions = "sales.brand_restrictions"
	brandRestrictedCode      = "SALESR-1400"
)

func init() {
	RegisterSetting(SettingDefinition{Key: SettingBrandRestrictions, Module: "Sales", Label: "Brand-restricted dealers",
		Type: SettingTypeBool, Default: "false",
		Description: "On: a customer can carry a list of allowed brands with a rule of None, Warn or Block, optionally between two dates, and an order line for another brand is warned about or refused. Off: orders ignore brands."})
	if settingGatedFields["Customer"] == nil {
		settingGatedFields["Customer"] = map[string]string{}
	}
	for _, f := range []string{"allowed_brands", "brand_restriction", "brand_restriction_from", "brand_restriction_to"} {
		settingGatedFields["Customer"][f] = SettingBrandRestrictions
	}
	if settingGatedFields["SalesOrder"] == nil {
		settingGatedFields["SalesOrder"] = map[string]string{}
	}
	settingGatedFields["SalesOrder"]["brand_warning"] = SettingBrandRestrictions
}

// brandRestrictionCheck returns a warning text (Warn mode) or a coded refusal
// (Block mode) for the lines a customer is ordering; "" and nil when the
// setting is off, the customer is unknown, unrestricted or outside its dates,
// or every branded line is on the list. An item with no brand never offends.
func brandRestrictionCheck(tenantID, customerID, customerName string, skus []string) (string, error) {
	if !GetSettingBool(tenantID, SettingBrandRestrictions) {
		return "", nil
	}
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		customerID = ResolveCustomerLink(tenantID, customerName)
	}
	if customerID == "" {
		return "", nil
	}
	cust, _, err := fetchDocData(tenantID, "Customer", customerID)
	if err != nil {
		return "", nil
	}
	mode := strings.ToLower(strings.TrimSpace(str(cust, "brand_restriction")))
	if mode != "warn" && mode != "block" {
		return "", nil
	}
	canon := brandCanon(tenantID)
	allowed := map[string]bool{}
	for _, b := range strings.Split(str(cust, "allowed_brands"), ",") {
		if b = strings.TrimSpace(b); b != "" {
			allowed[canon(b)] = true
		}
	}
	if len(allowed) == 0 {
		return "", nil
	}
	today := BusinessToday()
	if from := strings.TrimSpace(str(cust, "brand_restriction_from")); from != "" && today < from {
		return "", nil
	}
	if to := strings.TrimSpace(str(cust, "brand_restriction_to")); to != "" && today > to {
		return "", nil
	}
	var offenders []string
	seen := map[string]bool{}
	for _, sku := range skus {
		item, err := ResolveItemBySKU(tenantID, sku)
		if err != nil || item == nil {
			continue
		}
		brand := strings.TrimSpace(str(item.Data, "brand"))
		if brand == "" || allowed[canon(brand)] || seen[canon(brand)] {
			continue
		}
		seen[canon(brand)] = true
		offenders = append(offenders, brandLabel(tenantID, brand))
	}
	if len(offenders) == 0 {
		return "", nil
	}
	who := firstNonEmpty(str(cust, "name"), customerName)
	text := fmt.Sprintf("%s buys only the brands on their list; not allowed: %s", who, strings.Join(offenders, ", "))
	if mode == "block" {
		return "", &ValidationError{Code: brandRestrictedCode, SubFor: "lines", Message: text}
	}
	return text, nil
}

// brandCanon maps a brand written as a Brand master code or name (any case) to
// one key, so a list written with names matches items written with codes.
func brandCanon(tenantID string) func(string) string {
	alias := map[string]string{}
	if schema, err := db.GetTenantSchema(tenantID); err == nil {
		if rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'code', id), COALESCE(data->>'name', '') FROM %s.documents WHERE doctype = 'Brand' AND deleted_at IS NULL LIMIT 5000`, schema)); err == nil {
			for rows.Next() {
				var code, name string
				if rows.Scan(&code, &name) == nil {
					key := strings.ToLower(strings.TrimSpace(code))
					alias[key] = key
					if n := strings.ToLower(strings.TrimSpace(name)); n != "" {
						alias[n] = key
					}
				}
			}
			rows.Close()
		}
	}
	return func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		if c, ok := alias[s]; ok {
			return c
		}
		return s
	}
}

// brandLabel is the brand as a person reads it: the master's name when the
// item carries a code.
func brandLabel(tenantID, brand string) string {
	if b, _, err := fetchDocData(tenantID, "Brand", brand); err == nil {
		return firstNonEmpty(str(b, "name"), brand)
	}
	return brand
}
