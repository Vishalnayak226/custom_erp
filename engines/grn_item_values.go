package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"custom_erp/db"
)

// Stage 89.4.3 - fields asked at the GRN (user, 2026-10-18: "Weight and ask in
// GRN to fill", "Purity Ask in GRN, It will hidable in GRN as well. Give in
// config"). Configuration -> Inventory -> Item fields says which Item fields
// and attributes a GRN line asks, and whether a line may be saved without them.
// A line carries them as item_values {field: value}; posting keeps them on the
// line's batch (D3), so the batch's stickers print that receipt's own weight
// and purity, not the item master's.

// GRNAskedField is one field or attribute a GRN line asks.
type GRNAskedField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

// GRNAskedFields lists what a GRN line asks, in the Item fields order.
func GRNAskedFields(tenantID string) ([]GRNAskedField, error) {
	rules, err := ItemFieldRules(tenantID)
	if err != nil {
		return nil, err
	}
	var out []GRNAskedField
	for _, r := range rules {
		if r.Hidden || r.GRN == GRNAskOff || (r.Kind == "attribute" && r.LinkedField != "") {
			continue
		}
		out = append(out, GRNAskedField{Key: r.Key, Label: r.Label, Required: r.GRN == GRNAskRequired})
	}
	return out, nil
}

// grnLineItemValues reads a line's item_values as text.
func grnLineItemValues(line map[string]interface{}) map[string]string {
	raw, ok := line["item_values"].(map[string]interface{})
	if !ok {
		return nil
	}
	out := map[string]string{}
	for k, v := range raw {
		var s string
		switch t := v.(type) {
		case string:
			s = strings.TrimSpace(t)
		case float64:
			s = strconv.FormatFloat(t, 'f', -1, 64)
		case bool:
			s = strconv.FormatBool(t)
		}
		if s != "" {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// PrepareGRNItemValues keeps each line's item_values to the fields the GRN
// asks and refuses a line missing a required one (MASTER-0891). Runs with the
// GRN's other prepare steps, before the document is written.
func PrepareGRNItemValues(tenantID string, payload map[string]interface{}) error {
	raw, ok := payload["received_items"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	var lines []map[string]interface{}
	if json.Unmarshal([]byte(raw), &lines) != nil {
		return nil // the create handler reports malformed received_items itself
	}
	asked, err := GRNAskedFields(tenantID)
	if err != nil {
		return err
	}
	if len(asked) == 0 {
		changed := false
		for _, l := range lines {
			if _, has := l["item_values"]; has {
				delete(l, "item_values")
				changed = true
			}
		}
		if changed {
			out, _ := json.Marshal(lines)
			payload["received_items"] = string(out)
		}
		return nil
	}
	for _, l := range lines {
		values := grnLineItemValues(l)
		kept := map[string]interface{}{}
		var missing []string
		accepted := numFromInterface(l["accepted_qty"])
		if _, has := l["accepted_qty"]; !has {
			accepted = numFromInterface(l["qty"])
		}
		for _, f := range asked {
			if v := values[f.Key]; v != "" {
				kept[f.Key] = v
			} else if f.Required && accepted > 0 {
				missing = append(missing, f.Label)
			}
		}
		if len(missing) > 0 {
			return &ValidationError{Code: "MASTER-0891", Message: fmt.Sprintf("%s needs %s on this GRN.", strField(l, "sku"), strings.Join(missing, ", "))}
		}
		if len(kept) > 0 {
			l["item_values"] = kept
		} else {
			delete(l, "item_values")
		}
	}
	out, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	payload["received_items"] = string(out)
	return nil
}

// batchItemValues is what a receipt recorded on one batch of an item.
func batchItemValues(schema, item, batchNo string) map[string]string {
	if strings.TrimSpace(batchNo) == "" {
		return nil
	}
	var raw sql.NullString
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'attributes' FROM %s.documents
		WHERE doctype = 'Batch' AND deleted_at IS NULL AND data->>'item' = $1 AND data->>'batch_no' = $2
		ORDER BY created_at DESC LIMIT 1`, schema), item, batchNo).Scan(&raw); err != nil || raw.String == "" {
		return nil
	}
	out := map[string]string{}
	if json.Unmarshal([]byte(raw.String), &out) != nil {
		return nil
	}
	return out
}
