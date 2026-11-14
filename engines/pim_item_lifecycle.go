package engines

import (
	"fmt"
	"strings"
	"time"
)

// Product lifecycle. An Item carries an optional stage (Launch, Active,
// Phase-out, End of life), a launch date and a substitute item. No stage means
// Active, so items that exist today behave exactly as before.
//
// Phase-out and End of life items cannot go on a new PO line: the check is in
// validatePurchaseOrderEditRules, which every PO save runs. At the till and on
// an order the new setting decides: off, an End of life item or a not yet
// launched item sells with a warning; on, it is refused.

const (
	SettingLifecycleBlockEOLSale = "pim.lifecycle_block_eol_sale"

	LifecycleLaunch   = "Launch"
	LifecycleActive   = "Active"
	LifecyclePhaseOut = "Phase-out"
	LifecycleEOL      = "End of life"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingLifecycleBlockEOLSale, Module: "PIM",
		Label: "Refuse End of life and not yet launched items at the till", Type: SettingTypeBool, Default: "false",
		Description: "Off: the till and a new order sell an End of life item, or a Launch item before its launch date, and show a warning naming the substitute. On: they are refused. A new PO never takes a Phase-out or End of life item, whatever this says.",
	})
}

// itemLifecycleStage is the item's stage; blank means Active.
func itemLifecycleStage(item map[string]interface{}) string {
	s := strings.TrimSpace(pimString(item["lifecycle_stage"]))
	if s == "" {
		return LifecycleActive
	}
	return s
}

func lifecycleSubstituteHint(item map[string]interface{}) string {
	if sub := strings.TrimSpace(pimString(item["substitute_item"])); sub != "" {
		return " - use " + sub + " instead"
	}
	return ""
}

func lifecycleLabel(item map[string]interface{}, sku string) string {
	if c := strings.TrimSpace(pimString(item["code"])); c != "" {
		return c
	}
	return sku
}

// validateItemLifecycleFields runs on every Item save (validateDocumentWith).
func validateItemLifecycleFields(data map[string]interface{}) error {
	sub := strings.TrimSpace(pimString(data["substitute_item"]))
	if sub != "" {
		for _, own := range []string{pimString(data["code"]), pimString(data["id"])} {
			if own != "" && strings.EqualFold(own, sub) {
				return &ValidationError{Code: "PIM-9201", SubFor: "Substitute item", Message: "an item cannot be its own substitute"}
			}
		}
	}
	if d := strings.TrimSpace(pimString(data["launch_date"])); d != "" {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return &ValidationError{Code: "PIM-9201", SubFor: "Launch date", Message: fmt.Sprintf("%q is not a date (YYYY-MM-DD)", d)}
		}
	}
	return nil
}

// validatePOLifecycle refuses a PO line for a Phase-out or End of life item.
// Only a sku that is new to the stored PO is checked, so a PO already holding
// the item can still be received, amended or closed.
func validatePOLifecycle(tenantID string, priorData, payload map[string]interface{}) error {
	newItems, _ := payload["items"].(string)
	if strings.TrimSpace(newItems) == "" {
		return nil
	}
	lines, err := parsePOLineMaps(newItems)
	if err != nil || len(lines) == 0 {
		return nil
	}
	had := map[string]bool{}
	if priorData != nil {
		old, _ := parsePOLineMaps(strField(priorData, "items"))
		for _, l := range old {
			had[strings.ToLower(strField(l, "sku"))] = true
		}
	}
	for _, l := range lines {
		sku := strField(l, "sku")
		if sku == "" || had[strings.ToLower(sku)] {
			continue
		}
		it, err := ResolveItemBySKU(tenantID, sku)
		if err != nil || it == nil {
			continue
		}
		switch itemLifecycleStage(it.Data) {
		case LifecyclePhaseOut, LifecycleEOL:
			return &ValidationError{Code: "PIM-9200", SubFor: "items", Message: fmt.Sprintf(
				"%s is %s and cannot go on a new purchase order%s", lifecycleLabel(it.Data, sku), strings.ToLower(itemLifecycleStage(it.Data)), lifecycleSubstituteHint(it.Data))}
		}
	}
	return nil
}

// lifecycleSale judges selling an item today: a warning to show, or a refusal
// when the setting is on. Both empty means nothing to say.
func lifecycleSale(tenantID, sku string, item map[string]interface{}, today string) (string, error) {
	name := lifecycleLabel(item, sku)
	var msg, code string
	switch itemLifecycleStage(item) {
	case LifecycleEOL:
		msg, code = fmt.Sprintf("%s is End of life%s", name, lifecycleSubstituteHint(item)), "PIM-9202"
	case LifecycleLaunch:
		if d := strings.TrimSpace(pimString(item["launch_date"])); d != "" && d > today {
			msg, code = fmt.Sprintf("%s launches on %s and cannot be sold before then", name, d), "PIM-9203"
		}
	}
	if msg == "" {
		return "", nil
	}
	if GetSettingBool(tenantID, SettingLifecycleBlockEOLSale) {
		return "", &ValidationError{Code: code, SubFor: sku, Message: msg}
	}
	return msg, nil
}
