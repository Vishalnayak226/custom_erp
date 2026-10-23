package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 130.9 (72.D b, user decision 2026-10-22: only tax / policy fields are
// effective-dated, not the whole item). An item can carry one scheduled tax
// change - "New tax from" a date, with the new GST rate and, if it changes
// too, the new HSN code. From that date every sale and purchase is taxed at
// the new values (GetItemTaxInfo reads them), and the hourly worker folds
// them into the item's own fields, keeping what it replaced in
// tax_change_history and the audit log. Until then the item's current rate
// applies, so a change can be entered ahead of the government's date.

const (
	itemTaxChangeFrom = "tax_change_from"
	itemTaxChangeRate = "tax_change_gst_rate"
	itemTaxChangeHSN  = "tax_change_hsn"
)

// scheduledTaxChange is an item's change, when it has one in force today.
func scheduledTaxChange(data map[string]interface{}, today string) (rate float64, hsn string, ok bool) {
	from := strings.TrimSpace(strField(data, itemTaxChangeFrom))
	if from == "" || from > today {
		return 0, "", false
	}
	rate = numFromInterface(data[itemTaxChangeRate])
	hsn = strings.TrimSpace(strField(data, itemTaxChangeHSN))
	return rate, hsn, rate > 0 || hsn != ""
}

// withTaxChangeInForce is data with a change in force applied, for
// GetItemTaxInfo; data itself is not touched.
func withTaxChangeInForce(data map[string]interface{}) map[string]interface{} {
	rate, hsn, ok := scheduledTaxChange(data, time.Now().Format("2006-01-02"))
	if !ok {
		return data
	}
	out := make(map[string]interface{}, len(data))
	for k, v := range data {
		out[k] = v
	}
	if rate > 0 {
		out["gst_rate"] = rate
	}
	if hsn != "" {
		out["hsn_code"] = hsn
	}
	return out
}

// validateScheduledTaxChange checks the three fields on an item save.
func validateScheduledTaxChange(payload map[string]interface{}) error {
	from := strings.TrimSpace(strField(payload, itemTaxChangeFrom))
	rateSet := strings.TrimSpace(fmt.Sprintf("%v", payload[itemTaxChangeRate])) != "" && payload[itemTaxChangeRate] != nil
	hsn := strings.TrimSpace(strField(payload, itemTaxChangeHSN))
	if from == "" {
		if rateSet || hsn != "" {
			return &ValidationError{Code: "MASTER-1306", SubFor: "New tax from", Message: "give the date the new GST rate / HSN applies from"}
		}
		return nil
	}
	if _, err := time.Parse("2006-01-02", from); err != nil {
		return &ValidationError{Code: "MASTER-1306", SubFor: "New tax from", Message: fmt.Sprintf("%q is not a date (YYYY-MM-DD)", from)}
	}
	if rateSet && numFromInterface(payload[itemTaxChangeRate]) <= 0 {
		return &ValidationError{Code: "MASTER-1306", SubFor: "New GST rate", Message: "the new GST rate must be more than 0 - an item that becomes untaxed changes its Tax Treatment instead"}
	}
	if !rateSet && hsn == "" {
		return &ValidationError{Code: "MASTER-1306", SubFor: "New GST rate", Message: "say what changes on that date: the new GST rate, the new HSN code, or both"}
	}
	if hsn != "" && len(hsn) != 4 && len(hsn) != 6 && len(hsn) != 8 {
		return &ValidationError{Code: "MASTER-1306", SubFor: "New HSN code", Message: fmt.Sprintf("HSN code %q must be 4, 6 or 8 digits", hsn)}
	}
	return nil
}

// ApplyDueTaxChanges folds every change in force into its item's own fields,
// for one client. Returns how many items changed.
func ApplyDueTaxChanges(schema string) (int, error) {
	today := time.Now().Format("2006-01-02")
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = 'Item' AND deleted_at IS NULL AND COALESCE(data->>'tax_change_from', '') <> '' AND data->>'tax_change_from' <= $1`, schema), today)
	if err != nil {
		return 0, err
	}
	type due struct {
		id   string
		data map[string]interface{}
	}
	var list []due
	for rows.Next() {
		var d due
		var raw string
		if err := rows.Scan(&d.id, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		if json.Unmarshal([]byte(raw), &d.data) == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	applied := 0
	for _, d := range list {
		rate, hsn, ok := scheduledTaxChange(d.data, today)
		entry := map[string]interface{}{"from": strField(d.data, itemTaxChangeFrom), "old_gst_rate": d.data["gst_rate"], "old_hsn_code": d.data["hsn_code"], "applied_at": time.Now().Format(time.RFC3339)}
		patch := map[string]interface{}{itemTaxChangeFrom: "", itemTaxChangeRate: nil, itemTaxChangeHSN: ""}
		if ok && rate > 0 {
			patch["gst_rate"], entry["gst_rate"] = rate, rate
		}
		if ok && hsn != "" {
			patch["hsn_code"], entry["hsn_code"] = hsn, hsn
		}
		var history []interface{}
		if raw := strField(d.data, "tax_change_history"); raw != "" {
			_ = json.Unmarshal([]byte(raw), &history)
		}
		history = append(history, entry)
		h, _ := json.Marshal(history)
		patch["tax_change_history"] = string(h)
		b, _ := json.Marshal(patch)
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb, updated_at = CURRENT_TIMESTAMP, updated_by = 'system',
			edit_reason = 'scheduled tax change' WHERE doctype = 'Item' AND id = $1`, schema), d.id, string(b)); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

// StartItemTaxChangeWorker applies due tax changes on every client each
// interval (and once at start).
func StartItemTaxChangeWorker(ctx context.Context, interval time.Duration) {
	run := func() {
		if db.DB == nil {
			return
		}
		schemas, err := listTenantSchemas()
		if err != nil {
			return
		}
		for _, schema := range schemas {
			if n, err := ApplyDueTaxChanges(schema); err != nil {
				log.Printf("[ITEM-TAX] %s: %v", schema, err)
			} else if n > 0 {
				log.Printf("[ITEM-TAX] %s: %d item(s) moved to their new GST rate / HSN", schema, n)
			}
		}
	}
	go func() {
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() { // 134.7: one server runs each sweep
					continue
				}
				run()
			}
		}
	}()
}
