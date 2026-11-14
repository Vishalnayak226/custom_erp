package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

func schedSeedChange(t *testing.T, schema, id string, c map[string]interface{}) {
	t.Helper()
	if _, ok := c["status"]; !ok {
		c["status"] = "Scheduled"
	}
	enc, _ := json.Marshal(c)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ScheduledItemChange', $2, $3, 'system')", id, enc, c["status"]); err != nil {
		t.Fatalf("seed change %s: %v", id, err)
	}
}

func schedState(t *testing.T, schema, id string) (status, reason string) {
	t.Helper()
	if err := db.DB.QueryRow("SELECT COALESCE(data->>'status',''), COALESCE(data->>'failure_reason','') FROM "+schema+".documents WHERE id=$1", id).Scan(&status, &reason); err != nil {
		t.Fatalf("read change %s: %v", id, err)
	}
	return
}

func schedPrice(schema, id string) string {
	var p string
	_ = db.DB.QueryRow("SELECT COALESCE(data->>'sale_price','') FROM "+schema+".documents WHERE id=$1", id).Scan(&p)
	return p
}

func TestScheduledItemChangeAppliesOnTheDay(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	ids := []string{"SC-ITEM1", "SC-ITEM2", "SC-C1", "SC-C2", "SC-C3", "SC-C4"}
	lifecycleCleanup(schema, ids...)
	defer lifecycleCleanup(schema, ids...)
	lifecycleSeedItem(t, schema, "SC-ITEM1", nil)
	lifecycleSeedItem(t, schema, "SC-ITEM2", nil)

	schedSeedChange(t, schema, "SC-C1", map[string]interface{}{"item": "SC-ITEM1", "field": "sale_price", "new_value": "250", "effective_date": "2026-11-20"})
	schedSeedChange(t, schema, "SC-C2", map[string]interface{}{"item": "SC-ITEM2", "field": "mrp", "new_value": "300", "effective_date": "2026-11-20"})
	// A change the item screen would refuse: unknown item. It must fail alone.
	schedSeedChange(t, schema, "SC-C3", map[string]interface{}{"item": "SC-NOPE", "field": "sale_price", "new_value": "5", "effective_date": "2026-11-20"})
	// A cancelled change is never applied.
	schedSeedChange(t, schema, "SC-C4", map[string]interface{}{"item": "SC-ITEM1", "field": "mrp", "new_value": "999", "effective_date": "2026-11-20", "status": "Cancelled"})

	// The day before: nothing moves.
	a, f, err := ApplyDueItemChanges("default", "2026-11-19")
	if err != nil || a != 0 || f != 0 {
		t.Fatalf("before the day: applied=%d failed=%d err=%v", a, f, err)
	}
	if p := schedPrice(schema, "SC-ITEM1"); p != "100" {
		t.Fatalf("price moved early: %s", p)
	}
	// On the day: two apply, one fails, the cancelled one is left.
	a, f, err = ApplyDueItemChanges("default", "2026-11-20")
	if err != nil || a != 2 || f != 1 {
		t.Fatalf("on the day: applied=%d failed=%d err=%v", a, f, err)
	}
	if p := schedPrice(schema, "SC-ITEM1"); p != "250" {
		t.Fatalf("sale_price = %s, want 250", p)
	}
	if st, _ := schedState(t, schema, "SC-C1"); st != "Applied" {
		t.Fatalf("C1 status %s", st)
	}
	if st, why := schedState(t, schema, "SC-C3"); st != "Failed" || why == "" {
		t.Fatalf("C3 should be Failed with a reason, got %s %q", st, why)
	}
	if st, _ := schedState(t, schema, "SC-C4"); st != "Cancelled" {
		t.Fatalf("C4 status %s", st)
	}
	var mrp string
	_ = db.DB.QueryRow("SELECT COALESCE(data->>'mrp','') FROM " + schema + ".documents WHERE id='SC-ITEM2'").Scan(&mrp)
	if mrp != "300" {
		t.Fatalf("the other item's change must still apply: mrp=%s", mrp)
	}
	// Re-run is idempotent: nothing applies again, a later manual price is not reverted.
	_, _ = db.DB.Exec("UPDATE " + schema + ".documents SET data = data || '{\"sale_price\": 400}'::jsonb WHERE id='SC-ITEM1'")
	a, f, err = ApplyDueItemChanges("default", "2026-11-21")
	if err != nil || a != 0 || f != 0 {
		t.Fatalf("re-run: applied=%d failed=%d err=%v", a, f, err)
	}
	if p := schedPrice(schema, "SC-ITEM1"); p != "400" {
		t.Fatalf("re-run reverted the price: %s", p)
	}
}

func TestScheduledItemChangeValidation(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	ids := []string{"SC-V-ITEM", "SC-V1", "SC-V2", "SC-V3"}
	lifecycleCleanup(schema, ids...)
	defer lifecycleCleanup(schema, ids...)
	lifecycleSeedItem(t, schema, "SC-V-ITEM", nil)
	future := "2099-01-05"
	mk := func(over map[string]interface{}) map[string]interface{} {
		p := map[string]interface{}{"item": "SC-V-ITEM", "field": "sale_price", "new_value": "10", "effective_date": future}
		for k, v := range over {
			p[k] = v
		}
		return p
	}
	code := func(err error) string {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Code
		}
		if err == nil {
			return ""
		}
		return err.Error()
	}
	if c := code(ValidateScheduledItemChangeDocument("default", mk(nil))); c != "" {
		t.Fatalf("a good change must pass: %s", c)
	}
	for name, p := range map[string]map[string]interface{}{
		"field off the list": mk(map[string]interface{}{"field": "name"}),
		"past date":          mk(map[string]interface{}{"effective_date": "2020-01-01"}),
		"bad hsn":            mk(map[string]interface{}{"field": "hsn_code", "new_value": "12"}),
		"negative price":     mk(map[string]interface{}{"new_value": "-1"}),
		"unknown item":       mk(map[string]interface{}{"item": "SC-NOPE"}),
		"attribute missing":  mk(map[string]interface{}{"field": "attribute"}),
	} {
		if c := code(ValidateScheduledItemChangeDocument("default", p)); c != "PIM-9210" {
			t.Fatalf("%s: code %q, want PIM-9210", name, c)
		}
	}

	// Two changes to the same field and day are refused; another day is fine.
	schedSeedChange(t, schema, "SC-V1", mk(nil))
	if c := code(ValidateScheduledItemChangeDocument("default", mk(nil))); c != "PIM-9211" {
		t.Fatalf("clash: code %q", c)
	}
	if c := code(ValidateScheduledItemChangeDocument("default", mk(map[string]interface{}{"effective_date": "2099-01-06"}))); c != "" {
		t.Fatalf("another day must pass: %s", c)
	}
	if c := code(ValidateScheduledItemChangeDocument("default", mk(map[string]interface{}{"field": "mrp"}))); c != "" {
		t.Fatalf("another field must pass: %s", c)
	}
	// Editing the Scheduled row itself is not a clash with itself.
	if c := code(ValidateScheduledItemChangeDocument("default", mk(map[string]interface{}{"id": "SC-V1", "new_value": "12"}))); c != "" {
		t.Fatalf("editing itself: %s", c)
	}
	// Cancel works on a Scheduled row, and a cancelled or applied row is fixed.
	if c := code(ValidateScheduledItemChangeDocument("default", mk(map[string]interface{}{"id": "SC-V1", "status": "Cancelled"}))); c != "" {
		t.Fatalf("cancel: %s", c)
	}
	schedSeedChange(t, schema, "SC-V2", mk(map[string]interface{}{"status": "Applied", "field": "mrp"}))
	if c := code(ValidateScheduledItemChangeDocument("default", mk(map[string]interface{}{"id": "SC-V2", "status": "Cancelled"}))); c != "PIM-9212" {
		t.Fatalf("cancel of an applied change: code %q", c)
	}
	// Wrong role: only Super Admin can schedule; a Store Manager only reads.
	var create bool
	if err := db.DB.QueryRow(fmt.Sprintf("SELECT allow_create FROM %s.role_permissions WHERE role='Store Manager' AND doctype_name='ScheduledItemChange'", schema)).Scan(&create); err != nil || create {
		t.Fatalf("Store Manager must not create scheduled changes: %v %v", create, err)
	}
}
