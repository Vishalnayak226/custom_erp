package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

// Ask 170.9: meter-based maintenance.

func cleanupMeterTest(schema string) {
	db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T170M-%' OR (doctype = 'MaintenanceOrder' AND data->>'maintenance_schedule_id' LIKE 'T170M-%')")
}

func meterOrders(t *testing.T, schema, sched string) int {
	t.Helper()
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'MaintenanceOrder' AND data->>'maintenance_schedule_id' = $1", sched).Scan(&n)
	return n
}

func TestMeterReadingBelowTheLastIsRefused(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanupMeterTest(schema)
	t.Cleanup(func() { cleanupMeterTest(schema) })
	insertTestDoc(t, schema, "T170M-R1", "AssetMeterReading", map[string]interface{}{"asset": "T170M-A", "reading_date": "2026-10-01", "reading": 1000, "unit": "km"})
	insertTestDoc(t, schema, "T170M-R2", "AssetMeterReading", map[string]interface{}{"asset": "T170M-A", "reading_date": "2026-11-01", "reading": 1500, "unit": "km"})
	bad := func(date string, reading float64) error {
		return ValidateAssetMeterReadingDocument("default", map[string]interface{}{"asset": "T170M-A", "reading_date": date, "reading": reading, "unit": "km"})
	}
	if validationCode(bad("2026-11-05", 1400)) != "ASSET-1727" {
		t.Fatal("a reading below the last must be refused")
	}
	if validationCode(bad("2026-10-15", 1600)) != "ASSET-1727" {
		t.Fatal("a back-dated reading above a later one must be refused")
	}
	if err := bad("2026-11-05", 1500); err != nil {
		t.Fatalf("an equal reading is fine: %v", err)
	}
	if err := bad("2026-10-15", 1200); err != nil {
		t.Fatalf("between two readings is fine: %v", err)
	}
	// Another asset has its own series.
	if err := ValidateAssetMeterReadingDocument("default", map[string]interface{}{"asset": "T170M-B", "reading_date": "2026-11-05", "reading": 5, "unit": "km"}); err != nil {
		t.Fatalf("other asset: %v", err)
	}
	// Editing a reading is not compared with itself.
	if err := ValidateAssetMeterReadingDocument("default", map[string]interface{}{"id": "T170M-R2", "asset": "T170M-A", "reading_date": "2026-11-01", "reading": 1100, "unit": "km"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
}

func TestMeterIntervalRaisesAnOrderOnceAndRestartsTheCount(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanupMeterTest(schema)
	t.Cleanup(func() { cleanupMeterTest(schema) })
	insertTestDoc(t, schema, "T170M-AS", "Asset", map[string]interface{}{"code": "T170M-AS", "category": "Vehicle"})
	far := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	insertTestDoc(t, schema, "T170M-S1", "MaintenanceSchedule", map[string]interface{}{"code": "T170M-S1", "asset": "T170M-AS", "description": "Every 5000 km",
		"interval_days": 365.0, "interval_meter": 5000.0, "next_due_date": far})
	reading := func(id, date string, v float64) {
		insertTestDoc(t, schema, id, "AssetMeterReading", map[string]interface{}{"asset": "T170M-AS", "reading_date": date, "reading": v, "unit": "km"})
	}
	today := time.Now().Format("2006-01-02")

	runMaintenanceSchedulingForSchema(schema) // no reading yet: nothing to measure
	if meterOrders(t, schema, "T170M-S1") != 0 {
		t.Fatal("no reading, no order")
	}
	reading("T170M-R1", today, 10000)
	runMaintenanceSchedulingForSchema(schema) // first sight: the reading becomes the baseline
	if meterOrders(t, schema, "T170M-S1") != 0 {
		t.Fatal("the first reading must only set the baseline")
	}
	reading("T170M-R2", today, 14000)
	runMaintenanceSchedulingForSchema(schema)
	if meterOrders(t, schema, "T170M-S1") != 0 {
		t.Fatal("4000 of 5000 is not due")
	}
	reading("T170M-R3", today, 15200)
	runMaintenanceSchedulingForSchema(schema)
	runMaintenanceSchedulingForSchema(schema)
	if n := meterOrders(t, schema, "T170M-S1"); n != 1 {
		t.Fatalf("orders after the interval = %d, want exactly 1", n)
	}
	// The count restarts from the reading at that service.
	reading("T170M-R4", today, 19000)
	runMaintenanceSchedulingForSchema(schema)
	if n := meterOrders(t, schema, "T170M-S1"); n != 1 {
		t.Fatalf("3800 since the last service raised another order: %d", n)
	}
	reading("T170M-R5", today, 20200)
	runMaintenanceSchedulingForSchema(schema)
	if n := meterOrders(t, schema, "T170M-S1"); n != 2 {
		t.Fatalf("orders after a second interval = %d, want 2", n)
	}
}

func TestMeterScheduleStillRunsByDateAlone(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanupMeterTest(schema)
	t.Cleanup(func() { cleanupMeterTest(schema) })
	insertTestDoc(t, schema, "T170M-AS2", "Asset", map[string]interface{}{"code": "T170M-AS2", "category": "Vehicle"})
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	insertTestDoc(t, schema, "T170M-S2", "MaintenanceSchedule", map[string]interface{}{"code": "T170M-S2", "asset": "T170M-AS2", "description": "Quarterly",
		"interval_days": 90.0, "next_due_date": yesterday})
	runMaintenanceSchedulingForSchema(schema)
	if n := meterOrders(t, schema, "T170M-S2"); n != 1 {
		t.Fatalf("a date-only schedule raised %d orders", n)
	}
	var next string
	db.DB.QueryRow("SELECT data->>'next_due_date' FROM " + schema + ".documents WHERE id = 'T170M-S2'").Scan(&next)
	if next != time.Now().AddDate(0, 0, 89).Format("2006-01-02") {
		t.Fatalf("next due = %s", next)
	}
}
