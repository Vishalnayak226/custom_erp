package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Stage 146.5.11: attribute types and the check of a value against its
// definition. ProductAttributeDef.value_type gains Yes / No, Multi-select, Long
// text and URL beside Data, Number, Select and Date. The check runs from
// ValidateParityFoundationDocument, the one exit every save, import and bulk
// edit shares.
//
// The four new types are always checked: nothing has them until someone picks
// one. The old types (Number, Select, Date) are checked only once
// pim.check_legacy_attribute_values is on, so a value that saves today keeps
// saving. Either way a value is checked only when it changes: a stored value
// that would now fail still loads, and saving it again unchanged is allowed.

// SettingCheckLegacyAttributeValues holds Number, Select and Date values to their definition.
const SettingCheckLegacyAttributeValues = "pim.check_legacy_attribute_values"

const (
	AttrTypeYesNo       = "Yes / No"
	AttrTypeMultiSelect = "Multi-select"
	AttrTypeLongText    = "Long text"
	AttrTypeURL         = "URL"
)

// checkAttributeValueType returns the value as it will be stored (a Yes / No
// and a Multi-select are written in one canonical form) or the reason it is
// refused. legacy says whether Number, Select and Date are held too.
func checkAttributeValueType(valueType, options, value string, legacy bool, dateOrder string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return value, nil
	}
	switch valueType {
	case AttrTypeYesNo:
		switch strings.ToLower(value) {
		case "yes", "y", "true", "1":
			return "Yes", nil
		case "no", "n", "false", "0":
			return "No", nil
		}
		return value, fmt.Errorf("%q is not Yes or No", value)
	case AttrTypeMultiSelect:
		allowed := splitAttrOptions(options)
		picked := map[string]bool{}
		for _, p := range strings.Split(value, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			canon := p
			if len(allowed) > 0 {
				canon = ""
				for _, a := range allowed {
					if strings.EqualFold(a, p) {
						canon = a
						break
					}
				}
				if canon == "" {
					return value, fmt.Errorf("%q is not one of: %s", p, strings.Join(allowed, ", "))
				}
			}
			picked[canon] = true
		}
		out := make([]string, 0, len(picked))
		for k := range picked {
			out = append(out, k)
		}
		sort.Strings(out)
		return strings.Join(out, ","), nil
	case AttrTypeURL:
		u, err := url.Parse(value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.ContainsAny(value, " \t\r\n") {
			return value, fmt.Errorf("%q is not a web address starting http:// or https://", value)
		}
		return value, nil
	case AttrTypeLongText:
		return value, nil
	}
	if !legacy {
		return value, nil
	}
	switch valueType {
	case "Number":
		if !isPlainNumber(value) {
			return value, fmt.Errorf("%q is not a number", value)
		}
	case "Date":
		iso, err := ParseDateInOrder(value, dateOrder)
		if err != nil {
			return value, err
		}
		return iso, nil
	case "Select":
		if allowed := splitAttrOptions(options); len(allowed) > 0 {
			for _, a := range allowed {
				if a == value {
					return value, nil
				}
			}
			return value, fmt.Errorf("%q is not one of: %s", value, strings.Join(allowed, ", "))
		}
	}
	return value, nil
}

func splitAttrOptions(options string) []string {
	var out []string
	for _, o := range strings.Split(options, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	return out
}

// attributeDefType reads the type and options of one attribute definition.
func attributeDefType(schema, attribute string) (valueType, options string, found bool) {
	var raw string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'ProductAttributeDef' AND id = $1 AND deleted_at IS NULL`, schema), attribute).Scan(&raw) != nil {
		return "", "", false
	}
	m := map[string]interface{}{}
	if json.Unmarshal([]byte(raw), &m) != nil {
		return "", "", false
	}
	return pimString(m["value_type"]), pimString(m["value_options"]), true
}

// CheckAttributeValue checks and normalises one value against its attribute's
// definition. An unknown attribute is left to the Link check.
func CheckAttributeValue(tenantID, attribute, value string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return value, err
	}
	vt, opts, ok := attributeDefType(schema, attribute)
	if !ok {
		return value, nil
	}
	norm, cerr := checkAttributeValueType(vt, opts, value, GetSettingBool(tenantID, SettingCheckLegacyAttributeValues), ClientDateOrder(tenantID))
	if cerr != nil {
		return value, &ValidationError{Code: "PIM-0980", SubFor: "Value", Message: fmt.Sprintf("Value for %s (%s): %v", attribute, vt, cerr)}
	}
	return norm, nil
}

// ValidateProductAttributeValueDocument is the ValidateDocument hook for a
// ProductAttributeValue: a changed value must fit its attribute's type.
func ValidateProductAttributeValueDocument(tenantID string, payload map[string]interface{}) error {
	attribute := strField(payload, "attribute")
	value := strings.TrimSpace(pimString(payload["value"]))
	if attribute == "" {
		return nil
	}
	if err := StewardshipGuard(tenantID, payload); err != nil { // 47.13.4: a blank wipes a value, so it is held too
		return err
	}
	if value == "" {
		return nil
	}
	norm, err := CheckAttributeValue(tenantID, attribute, value)
	if err != nil {
		// Only a changed value is held: the stored row says what it holds now.
		if schema, sErr := db.GetTenantSchema(tenantID); sErr == nil {
			id := attributeValueID(strField(payload, "item"), attribute, strField(payload, "locale"), strField(payload, "channel"))
			var stored string
			if db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'value','') FROM %s.documents WHERE doctype = 'ProductAttributeValue' AND id = $1 AND deleted_at IS NULL`, schema), id).Scan(&stored) == nil && strings.TrimSpace(stored) == value {
				return nil
			}
		}
		return err
	}
	if norm != value {
		payload["value"] = norm
	}
	return nil
}
