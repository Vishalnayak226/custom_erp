package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Effective-dated product changes. A ScheduledItemChange row is one field of
// one item, its new value and the day it takes effect. A sweep applies the due
// rows at the start of the business day (BusinessToday, the same local day the
// other day-based jobs use) through BulkUpdateDocuments, the item update path
// that runs every save rule. A refused change becomes Failed with its reason
// and the others still apply. Applied and Failed rows are audited and are
// never edited again.

const (
	ScheduledItemChangeDoctype = "ScheduledItemChange"
	schedChangeScheduled       = "Scheduled"
	schedChangeApplied         = "Applied"
	schedChangeCancelled       = "Cancelled"
	schedChangeFailed          = "Failed"
	schedChangeAttribute       = "attribute"
)

// scheduledItemChangeFields is the allow-list: selling price, MRP, purchase
// price (the Standard Cost), the tax and HSN fields, and attribute values.
var scheduledItemChangeFields = map[string]string{
	"sale_price": "number", "mrp": "number", "standard_cost": "number",
	"gst_rate": "percent", "hsn_code": "hsn", "tax_treatment": "text",
	schedChangeAttribute: "text",
}

var schedHSNRe = regexp.MustCompile(`^\d{4}(\d{2}(\d{2})?)?$`)

func schedFail(sub, format string, a ...interface{}) error {
	return &ValidationError{Code: "PIM-9210", SubFor: sub, Message: fmt.Sprintf(format, a...)}
}

// ValidateScheduledItemChangeDocument is the ValidateDocument hook. A saved
// change that is no longer Scheduled is fixed; a Scheduled one can be edited
// or cancelled; a new one must be Scheduled, for an existing item, a field on
// the allow-list, a valid value, a date from today on, and no second change to
// the same field and day.
func ValidateScheduledItemChangeDocument(tenantID string, payload map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	id := strField(payload, "id")
	stored := ""
	if id != "" {
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'status','') FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema),
			ScheduledItemChangeDoctype, id).Scan(&stored)
		if stored != "" && stored != schedChangeScheduled {
			return &ValidationError{Code: "PIM-9212", SubFor: "Status", Message: fmt.Sprintf("this change is %s and can no longer be edited", strings.ToLower(stored))}
		}
	}
	status := strField(payload, "status")
	if status == "" {
		status = schedChangeScheduled
		payload["status"] = status
	}
	if stored == "" && status != schedChangeScheduled {
		return schedFail("Status", "a new change starts as Scheduled")
	}
	if status == schedChangeCancelled {
		return nil
	}
	if status != schedChangeScheduled {
		return schedFail("Status", "a change is applied or failed by the daily job, not by hand")
	}

	field := strField(payload, "field")
	kind, ok := scheduledItemChangeFields[field]
	if !ok {
		return schedFail("Field", "%q cannot be scheduled - pick selling price, MRP, purchase price, a tax or HSN field, or an attribute", field)
	}
	attr := strField(payload, "attribute")
	if field == schedChangeAttribute && attr == "" {
		return schedFail("Attribute", "pick the attribute whose value changes")
	}
	if field != schedChangeAttribute && attr != "" {
		return schedFail("Attribute", "leave Attribute empty unless the field is attribute")
	}
	if _, err := ResolveItemBySKU(tenantID, strField(payload, "item")); err != nil {
		return schedFail("Item", "item %q was not found", strField(payload, "item"))
	}
	val := strings.TrimSpace(fmt.Sprint(payload["new_value"]))
	if payload["new_value"] == nil || val == "" {
		return schedFail("New value", "give the new value")
	}
	switch kind {
	case "number":
		if n, err := strconv.ParseFloat(val, 64); err != nil || n < 0 {
			return schedFail("New value", "%q is not an amount of 0 or more", val)
		}
	case "percent":
		if n, err := strconv.ParseFloat(val, 64); err != nil || n < 0 || n > 100 {
			return schedFail("New value", "%q is not a rate between 0 and 100", val)
		}
	case "hsn":
		if !schedHSNRe.MatchString(val) {
			return schedFail("New value", "HSN code %q must be 4, 6 or 8 digits", val)
		}
	}
	date := strField(payload, "effective_date")
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return schedFail("Effective date", "%q is not a date (YYYY-MM-DD)", date)
	}
	if stored == "" && d.Format("2006-01-02") < BusinessToday() {
		return schedFail("Effective date", "%s is already past - pick today or a later day", date)
	}
	var clash bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND id <> $2 AND COALESCE(data->>'status','') = 'Scheduled'
		  AND lower(data->>'item') = lower($3) AND data->>'field' = $4 AND COALESCE(data->>'attribute','') = $5
		  AND data->>'effective_date' = $6)`, schema),
		ScheduledItemChangeDoctype, id, strField(payload, "item"), field, attr, date).Scan(&clash); err != nil {
		return err
	}
	if clash {
		return &ValidationError{Code: "PIM-9211", SubFor: "Effective date", Message: fmt.Sprintf("%s already has a scheduled change on %s", field, date)}
	}
	return nil
}

// schedChangeValue turns the typed text into what the item field stores.
func schedChangeValue(field, text string) interface{} {
	switch scheduledItemChangeFields[field] {
	case "number", "percent":
		if n, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil {
			return n
		}
	}
	return strings.TrimSpace(text)
}

// ApplyDueItemChanges applies every Scheduled change due on or before today
// for one client. today is "YYYY-MM-DD" in the business time zone. A change
// that is not Scheduled any more is skipped, so a second run does nothing.
func ApplyDueItemChanges(tenantID, today string) (applied, failed int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND COALESCE(data->>'status','') = 'Scheduled'
		  AND data->>'effective_date' <= $2
		ORDER BY data->>'effective_date', created_at, id`, schema), ScheduledItemChangeDoctype, today)
	if err != nil {
		return 0, 0, err
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
			return 0, 0, err
		}
		if json.Unmarshal([]byte(raw), &d.data) == nil {
			list = append(list, d)
		}
	}
	rows.Close()

	for _, d := range list {
		cause := applyOneItemChange(tenantID, d.data)
		patch := map[string]interface{}{"applied_at": time.Now().UTC().Format(time.RFC3339)}
		action, outcome := "item.scheduled_change.applied", "Success"
		if cause != nil {
			patch["status"], patch["failure_reason"] = schedChangeFailed, truncateText(cause.Error(), 300)
			action, outcome = "item.scheduled_change.failed", "Failure"
			failed++
		} else {
			patch["status"] = schedChangeApplied
			applied++
		}
		b, _ := json.Marshal(patch)
		res, uerr := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $3::jsonb, status = $4, updated_at = CURRENT_TIMESTAMP, updated_by = 'system'
			WHERE doctype = $1 AND id = $2 AND COALESCE(data->>'status','') = 'Scheduled'`, schema),
			ScheduledItemChangeDoctype, d.id, string(b), fmt.Sprint(patch["status"]))
		if uerr != nil {
			return applied, failed, uerr
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // someone cancelled it meanwhile
		}
		LogAuditEvent(tenantID, "system", action, outcome, fmt.Sprintf("%s %s %s -> %s (effective %s) %v", ScheduledItemChangeDoctype, d.id,
			strField(d.data, "item")+"/"+firstNonEmpty(strField(d.data, "attribute"), strField(d.data, "field")), strField(d.data, "new_value"), strField(d.data, "effective_date"), cause))
	}
	return applied, failed, nil
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// applyOneItemChange writes the change through the normal update path.
func applyOneItemChange(tenantID string, c map[string]interface{}) error {
	item, err := ResolveItemBySKU(tenantID, strField(c, "item"))
	if err != nil {
		return fmt.Errorf("item %q was not found", strField(c, "item"))
	}
	field := strField(c, "field")
	if _, ok := scheduledItemChangeFields[field]; !ok {
		return fmt.Errorf("field %q is not allowed", field)
	}
	if field == schedChangeAttribute {
		code := firstNonEmpty(strField(item.Data, "code"), item.ID)
		_, err = BulkUpdateDocuments(tenantID, "ProductAttributeValue", []string{code + "::" + strField(c, "attribute")}, "value", strField(c, "new_value"), "system", RoleSuperAdmin)
		return err
	}
	_, err = BulkUpdateDocuments(tenantID, "Item", []string{item.ID}, field, schedChangeValue(field, strField(c, "new_value")), "system", RoleSuperAdmin)
	return err
}

// StartScheduledItemChangeWorker applies due changes on every client each
// interval (and once at start); one server runs each sweep.
func StartScheduledItemChangeWorker(ctx context.Context, interval time.Duration) {
	run := func() {
		if db.DB == nil {
			return
		}
		schemas, err := sweepTenantSchemas("pim")
		if err != nil {
			return
		}
		today := BusinessToday()
		for _, schema := range schemas {
			tenantID := schemaToTenantID(schema)
			if tenantID == "" {
				continue
			}
			if a, f, err := ApplyDueItemChanges(tenantID, today); err != nil {
				log.Printf("[ITEM-SCHEDULE] %s: %v", schema, err)
			} else if a+f > 0 {
				log.Printf("[ITEM-SCHEDULE] %s: %d change(s) applied, %d failed", schema, a, f)
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
				if IsSchedulerLeader() {
					run()
				}
			}
		}
	}()
}
