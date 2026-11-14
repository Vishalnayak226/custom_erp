package engines

import (
	"encoding/json"
	"fmt"
	"strings"

	"custom_erp/db"
)

// Units per attribute value (168.14) and approval of regulated attribute
// values (168.15).
//
// Unit: a Number attribute may carry a unit (ProductAttributeDef.uom - g, mm,
// ct, ml). The item form shows it as a suffix and an export header names it.
// With pim.check_unit_attribute_values on, the value must be a plain number
// (the unit is not typed with it). An attribute with no unit behaves as before.
//
// Regulated: ProductAttributeDef.regulated = Yes marks an attribute whose value
// a second person must approve before it changes. With
// pim.hold_regulated_attribute_changes on, saving a different value over a stored one
// keeps the stored value and files an AttributeValueChange through the
// maker-checker engine (the 148.10 vendor-change pattern); the decision hook
// applies it once. A first value for an item is not a change and is not held.

const (
	SettingCheckUnitAttributeValues      = "pim.check_unit_attribute_values"
	SettingHoldRegulatedAttributeChanges = "pim.hold_regulated_attribute_changes"
	AttributeValueChangeDoctype          = "AttributeValueChange"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingCheckUnitAttributeValues, Module: "PIM",
		Label: "Check values of attributes that have a unit", Type: SettingTypeBool, Default: "false",
		Description: "When on, a Number attribute that has a unit (g, mm, ml) accepts only a plain number. The unit shows beside the box and in export headers. Off: any text saves as before.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingHoldRegulatedAttributeChanges, Module: "PIM",
		Label: "Regulated attribute changes need approval", Type: SettingTypeBool, Default: "false",
		Description: "When on, changing a stored value of an attribute marked Regulated waits for a second person's approval; the old value stays until then. A first value is saved at once.",
	})
	RegisterApprovalDecisionHook(AttributeValueChangeDoctype, false, func(tenantID, docID, decision, actor string) {
		if decision != "Approved" {
			return
		}
		if err := applyAttributeValueChange(tenantID, docID, actor); err != nil {
			LogSystemError(tenantID, "", "ERROR", "attribute_value_change", fmt.Sprintf("approved change %s not applied: %v", docID, err), "")
		}
	})
}

// attributeDefExtras reads an attribute's unit and whether it is regulated.
func attributeDefExtras(schema, attribute string) (uom string, regulated bool) {
	var raw string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'ProductAttributeDef' AND id = $1 AND deleted_at IS NULL`, schema), attribute).Scan(&raw) != nil {
		return "", false
	}
	m := map[string]interface{}{}
	if json.Unmarshal([]byte(raw), &m) != nil {
		return "", false
	}
	return strings.TrimSpace(pimString(m["uom"])), strings.EqualFold(pimString(m["regulated"]), "Yes")
}

// checkUnitValue refuses a value that is not a plain number for a Number
// attribute with a unit, when the setting is on.
func checkUnitValue(tenantID, schema, attribute, valueType, value string) error {
	if valueType != "Number" || strings.TrimSpace(value) == "" || !GetSettingBool(tenantID, SettingCheckUnitAttributeValues) {
		return nil
	}
	uom, _ := attributeDefExtras(schema, attribute)
	if uom == "" || isPlainNumber(strings.TrimSpace(value)) {
		return nil
	}
	return fmt.Errorf("%q is not a number: type the number only, the unit (%s) is added for you", strings.TrimSpace(value), uom)
}

// exportHeaderWithUnit names the unit in the header of a column that carries a
// Number attribute with one, unless the header already has brackets.
func exportHeaderWithUnit(schema, fieldKey, header string) string {
	if strings.Contains(header, "(") || strings.TrimSpace(fieldKey) == "" {
		return header
	}
	var vt, uom string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'value_type',''), COALESCE(data->>'uom','') FROM %s.documents WHERE doctype = 'ProductAttributeDef' AND deleted_at IS NULL AND lower(id) = lower($1)`, schema), fieldKey).Scan(&vt, &uom) != nil {
		return header
	}
	if vt != "Number" || strings.TrimSpace(uom) == "" {
		return header
	}
	return header + " (" + strings.TrimSpace(uom) + ")"
}

// RegulatedHoldRefusal is for the doors that cannot hold a change (the CSV
// sheet import and a delete): they refuse a change to a stored regulated
// value, saying where to make it. A first value and an unchanged value pass.
func RegulatedHoldRefusal(tenantID, item, attribute, locale, channel, newValue string) error {
	if !GetSettingBool(tenantID, SettingHoldRegulatedAttributeChanges) {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if _, regulated := attributeDefExtras(schema, attribute); !regulated {
		return nil
	}
	var stored string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'value','') FROM %s.documents WHERE doctype = 'ProductAttributeValue' AND id = $1 AND deleted_at IS NULL`, schema), attributeValueID(item, attribute, locale, channel)).Scan(&stored) != nil {
		return nil
	}
	if strings.TrimSpace(stored) == strings.TrimSpace(newValue) {
		return nil
	}
	return &ValidationError{Code: "PIM-9306", SubFor: "Value", Message: fmt.Sprintf("%s is regulated: change it on the item's page, where it waits for approval.", attribute)}
}

// HoldRegulatedAttributeChange is the save door's guard. prior is the stored
// ProductAttributeValue (nil: a first value, not held) and payload the one
// about to be written. It reports whether the change was held, after putting
// the stored value back into payload and filing the request.
func HoldRegulatedAttributeChange(tenantID string, prior, payload map[string]interface{}, userID, role string) (bool, error) {
	if prior == nil || payload == nil || !GetSettingBool(tenantID, SettingHoldRegulatedAttributeChanges) {
		return false, nil
	}
	attribute, item := strField(payload, "attribute"), strField(payload, "item")
	oldV, newV := strings.TrimSpace(vendorChangeText(prior["value"])), strings.TrimSpace(vendorChangeText(payload["value"]))
	if attribute == "" || item == "" || oldV == newV {
		return false, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	if _, regulated := attributeDefExtras(schema, attribute); !regulated {
		return false, nil
	}
	id := NewDocID("AVC")
	doc := map[string]interface{}{
		"code": id, "item": item, "attribute": attribute, "locale": strField(payload, "locale"), "channel": strField(payload, "channel"),
		"old_value": oldV, "new_value": newV, "requested_by": userID, "status": "Draft",
		"summary": fmt.Sprintf("%s · %s: %s → %s", item, attribute, blankWord(oldV), blankWord(newV)),
	}
	b, _ := json.Marshal(doc)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'AttributeValueChange', $2, 'Draft', $3)`, schema), id, b, userID); err != nil {
		return false, err
	}
	if err := SubmitForApproval(tenantID, AttributeValueChangeDoctype, id, userID, role); err != nil {
		_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = 'AttributeValueChange' AND id = $1`, schema), id)
		return false, err
	}
	payload["value"] = prior["value"]
	LogAuditEvent(tenantID, userID, "ATTRIBUTE_VALUE_CHANGE_REQUESTED", "SUCCESS", fmt.Sprintf("%s %s waits for approval (%s)", item, attribute, id))
	return true, nil
}

func blankWord(s string) string {
	if s == "" {
		return "blank"
	}
	return s
}

// applyAttributeValueChange writes an approved request onto the value once
// (applied_at marks it) and records the edit in the audit log.
func applyAttributeValueChange(tenantID, docID, actor string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1 AND doctype = 'AttributeValueChange' FOR UPDATE`, schema), docID).Scan(&raw); err != nil {
		return err
	}
	var req map[string]interface{}
	if err := json.Unmarshal(raw, &req); err != nil {
		return err
	}
	if strField(req, "applied_at") != "" {
		return nil
	}
	valID := attributeValueID(strField(req, "item"), strField(req, "attribute"), strField(req, "locale"), strField(req, "channel"))
	var vraw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1 AND doctype = 'ProductAttributeValue' AND deleted_at IS NULL FOR UPDATE`, schema), valID).Scan(&vraw); err != nil {
		return err
	}
	var val map[string]interface{}
	if err := json.Unmarshal(vraw, &val); err != nil {
		return err
	}
	prior := map[string]interface{}{}
	for k, v := range val {
		prior[k] = v
	}
	val["value"] = strField(req, "new_value")
	vb, _ := json.Marshal(val)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND doctype = 'ProductAttributeValue'`, schema), vb, valID); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('applied_at', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')) WHERE id = $1`, schema), docID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogDocumentEdit(tenantID, strField(req, "requested_by"), "ProductAttributeValue", valID, prior, val, "approved change "+docID+" by "+actor)
	return nil
}

// CheckAttributeValueDelete is the delete door for a ProductAttributeValue: a
// delete blanks the value, so it answers to the same two rules as a blank save
// (a category's Required attribute, a Regulated attribute's approval).
func CheckAttributeValueDelete(tenantID, id string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var raw []byte
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'ProductAttributeValue' AND id = $1 AND deleted_at IS NULL`, schema), id).Scan(&raw) != nil {
		return nil
	}
	var m map[string]interface{}
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	if err := validateBlankedRequiredValue(tenantID, m); err != nil {
		return err
	}
	return RegulatedHoldRefusal(tenantID, strField(m, "item"), strField(m, "attribute"), strField(m, "locale"), strField(m, "channel"), "")
}
