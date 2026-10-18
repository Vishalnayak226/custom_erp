package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Stage 65.3/65.4 (user, 2026-10-16): a design in the PO hangs off an item
// name - Category + Product Type ("Necklaces" + "Antique") holding the HSN
// and GST every design under it inherits, so they are typed once. Its
// variants come from the PIM attributes, which the PO may now extend inline.

// Masters created inline from the PO are numbered by PrepareMasterNumber
// (engines/master_codes.go), like every other master since Stage 76.5.

// PrepareItemNameFields fills what the user should not have to type twice:
// an ItemName with no name is named "<Category> <Product Type>", and a design
// (ProductFamily) under an item name takes that item name's HSN and GST rate
// where its own are blank.
func PrepareItemNameFields(tenantID, doctype string, payload map[string]interface{}) error {
	if doctype != "ItemName" && doctype != "ProductFamily" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	switch doctype {
	case "ItemName":
		if strings.TrimSpace(strField(payload, "name")) != "" {
			return nil
		}
		name := strings.TrimSpace(masterDisplayName(schema, "Category", strField(payload, "category")) + " " +
			masterDisplayName(schema, "ProductType", strField(payload, "product_type")))
		if name != "" {
			payload["name"] = name
		}
	case "ProductFamily":
		itemName := strings.TrimSpace(strField(payload, "item_name"))
		if itemName == "" {
			return nil
		}
		blankGST := payload["gst_rate"] == nil || strings.TrimSpace(fmt.Sprint(payload["gst_rate"])) == ""
		if strings.TrimSpace(strField(payload, "hsn_code")) != "" && !blankGST {
			return nil
		}
		var raw string
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents
			WHERE doctype = 'ItemName' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), itemName).Scan(&raw)
		if err == sql.ErrNoRows {
			return nil // the Link check reports an unknown item name
		}
		if err != nil {
			return err
		}
		var in map[string]interface{}
		if json.Unmarshal([]byte(raw), &in) != nil {
			return nil
		}
		if strings.TrimSpace(strField(payload, "hsn_code")) == "" && strField(in, "hsn_code") != "" {
			payload["hsn_code"] = strField(in, "hsn_code")
		}
		if blankGST && in["gst_rate"] != nil {
			payload["gst_rate"] = in["gst_rate"]
		}
	}
	return nil
}

// masterDisplayName is a master record's name, looked up by id or code; the
// value itself when there is no such record.
func masterDisplayName(schema, doctype, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	var name sql.NullString
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'name' FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND (id = $2 OR data->>'code' = $2) LIMIT 1`, schema), doctype, ref).Scan(&name)
	if err != nil || strings.TrimSpace(name.String) == "" {
		return ref
	}
	return strings.TrimSpace(name.String)
}

var attributeCodeRE = regexp.MustCompile(`[^A-Za-z0-9]+`)

// AddDesignAttributeValue adds a value to a Select product attribute - or,
// given a label no attribute has, creates that attribute with the value - so
// a buyer can add a colour or a new attribute from the PO instead of going to
// PIM first. A value the attribute already has (any case) is not added twice.
func AddDesignAttributeValue(tenantID, userID, code, label, value string) (DesignAttribute, error) {
	code, label, value = strings.TrimSpace(code), strings.TrimSpace(label), strings.TrimSpace(value)
	if value == "" || strings.Contains(value, ",") {
		return DesignAttribute{}, &ValidationError{Code: "PIM-0321", Message: "Enter one value, without commas."}
	}
	if code == "" && label == "" {
		return DesignAttribute{}, &ValidationError{Code: "PIM-0321", Message: "Name the attribute the value belongs to."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return DesignAttribute{}, err
	}
	var id, raw string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data::text FROM %s.documents
		WHERE doctype = 'ProductAttributeDef' AND deleted_at IS NULL
		  AND (($1 <> '' AND (lower(id) = lower($1) OR lower(data->>'code') = lower($1)))
		    OR ($1 = '' AND lower(data->>'label') = lower($2)))
		ORDER BY created_at LIMIT 1`, schema), code, label).Scan(&id, &raw)
	if err != nil && err != sql.ErrNoRows {
		return DesignAttribute{}, err
	}

	if err == sql.ErrNoRows {
		if label == "" {
			return DesignAttribute{}, &ValidationError{Code: "PIM-0321", Message: fmt.Sprintf("There is no product attribute %q.", code)}
		}
		// Stage 81: once the operator has set this client's attributes up from
		// the library, a PO adds values to them but never a new attribute.
		if TenantHasLibraryAttributes(tenantID) {
			return DesignAttribute{}, &ValidationError{Code: "PIM-0691", Message: fmt.Sprintf("%q is not one of your product attributes. Pick one of the attributes shown, or ask your administrator to add it under PIM -> Attribute Definitions.", label)}
		}
		newCode := strings.ToUpper(strings.Trim(attributeCodeRE.ReplaceAllString(label, "_"), "_"))
		if newCode == "" {
			newCode = "ATTR"
		}
		base := newCode
		for n := 2; ; n++ {
			var taken bool
			if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.documents WHERE id = $1)`, schema), newCode).Scan(&taken); err != nil {
				return DesignAttribute{}, err
			}
			if !taken {
				break
			}
			newCode = fmt.Sprintf("%s_%d", base, n)
		}
		data, _ := json.Marshal(map[string]interface{}{
			"code": newCode, "label": label, "value_type": "Select", "value_options": value, "status": "Active",
		})
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, 'ProductAttributeDef', $2::jsonb, 'Active', $3)`, schema), newCode, string(data), userID); err != nil {
			return DesignAttribute{}, err
		}
		LogAuditEvent(tenantID, userID, "DESIGN_ATTRIBUTE_CREATED", "SUCCESS", fmt.Sprintf("%s (%s) with value %s, from a purchase order", newCode, label, value))
		return DesignAttribute{Code: newCode, Label: label, ValueType: "Select", Options: []string{value}}, nil
	}

	var d map[string]interface{}
	if json.Unmarshal([]byte(raw), &d) != nil {
		return DesignAttribute{}, fmt.Errorf("product attribute %s is unreadable", id)
	}
	attr := DesignAttribute{
		Code: firstNonEmpty(strField(d, "code"), id), Label: firstNonEmpty(strField(d, "label"), strField(d, "code"), id),
		ValueType: firstNonEmpty(strField(d, "value_type"), "Data"), Group: strField(d, "group"),
	}
	if attr.ValueType != "Select" {
		return DesignAttribute{}, &ValidationError{Code: "PIM-0321", Message: fmt.Sprintf("%s is typed, not chosen from a list - type its value on the design instead.", attr.Label)}
	}
	for _, o := range strings.Split(strField(d, "value_options"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			attr.Options = append(attr.Options, o)
			if strings.EqualFold(o, value) {
				return attr, nil // already there
			}
		}
	}
	attr.Options = append(attr.Options, value)
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents
		SET data = jsonb_set(data, '{value_options}', to_jsonb($2::text)), updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND doctype = 'ProductAttributeDef'`, schema), id, strings.Join(attr.Options, ",")); err != nil {
		return DesignAttribute{}, err
	}
	LogAuditEvent(tenantID, userID, "DESIGN_ATTRIBUTE_VALUE_ADDED", "SUCCESS", fmt.Sprintf("%s: %s, from a purchase order", attr.Code, value))
	return attr, nil
}
