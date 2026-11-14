package engines

import (
	"testing"

	"custom_erp/db"
)

func dqRow(t *testing.T, tenant, recordID, code string) (DataQualityRow, bool) {
	t.Helper()
	rows, err := ListDataQualityIssues(tenant, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if r.RecordID == recordID {
			return r, true
		}
	}
	return DataQualityRow{}, false
}

func dqCount(t *testing.T, tenant, recordID string) int {
	t.Helper()
	rows, _ := ListDataQualityIssues(tenant, "")
	n := 0
	for _, r := range rows {
		if r.RecordID == recordID {
			n++
		}
	}
	return n
}

func TestDataQualityScanLifecycle(t *testing.T) {
	tenant, schema := retentionTestSetup(t)
	defer func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'I170-DQ-%'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'DataQualityIssue' AND data->>'record_id' LIKE 'I170-DQ-%'")
	}()
	seedStaffVendor(t, schema, "Vendor", "I170-DQ-V1", map[string]interface{}{"name": "Dq Vendor", "gstin": "NOT-A-GSTIN", "pan": "BADPAN"})
	if _, err := ScanDataQuality(tenant, "tester"); err != nil {
		t.Fatalf("scan: %v", err)
	}
	first := dqCount(t, tenant, "I170-DQ-V1")
	if first == 0 {
		t.Fatal("a vendor with a bad GSTIN and PAN raised nothing")
	}
	// Rescan: no duplicate open rows.
	if _, err := ScanDataQuality(tenant, "tester"); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if n := dqCount(t, tenant, "I170-DQ-V1"); n != first {
		t.Errorf("rescan duplicated rows: %d -> %d", first, n)
	}
	// Ignore needs a reason.
	rows, _ := ListDataQualityIssues(tenant, "Open")
	var id string
	for _, r := range rows {
		if r.RecordID == "I170-DQ-V1" {
			id = r.ID
			break
		}
	}
	wantValidationCode(t, SetDataQualityStatus(tenant, id, "Ignored", "", "tester"), "MASTER-1700")
	wantValidationCode(t, SetDataQualityStatus(tenant, id, "Bogus", "", "tester"), "MASTER-1701")
	wantValidationCode(t, SetDataQualityStatus(tenant, "DQI-nothere", "Fixed", "", "tester"), "MASTER-1702")
	if err := SetDataQualityStatus(tenant, id, "Ignored", "known supplier", "tester"); err != nil {
		t.Fatalf("ignore: %v", err)
	}
	ScanDataQuality(tenant, "tester")
	if r, _ := dqRow(t, tenant, "I170-DQ-V1", ""); r.ID == id && r.Status != "Ignored" {
		t.Errorf("a rescan changed an ignored row to %s", r.Status)
	}
	// Fix the data: a rescan closes the open rows.
	db.DB.Exec(`UPDATE ` + schema + `.documents SET data = data || '{"gstin":"","pan":""}'::jsonb WHERE id = 'I170-DQ-V1'`)
	ScanDataQuality(tenant, "tester")
	open, _ := ListDataQualityIssues(tenant, "Open")
	for _, r := range open {
		if r.RecordID == "I170-DQ-V1" {
			t.Errorf("problem fixed in the data but %s is still open", r.Problem)
		}
	}
	// The problem comes back: a Fixed row reopens.
	db.DB.Exec(`UPDATE ` + schema + `.documents SET data = data || '{"pan":"BADPAN"}'::jsonb WHERE id = 'I170-DQ-V1'`)
	ScanDataQuality(tenant, "tester")
	open, _ = ListDataQualityIssues(tenant, "Open")
	back := false
	for _, r := range open {
		if r.RecordID == "I170-DQ-V1" {
			back = true
		}
	}
	if !back {
		t.Error("a returned problem did not reopen")
	}
}

// The queue leaves Stage 72's exception queue alone.
func TestDataQualityScanLeavesItemExceptionAlone(t *testing.T) {
	tenant, schema := retentionTestSetup(t)
	var before int
	db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.documents WHERE doctype = 'ItemException'`).Scan(&before)
	ScanDataQuality(tenant, "tester")
	var after int
	db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.documents WHERE doctype = 'ItemException'`).Scan(&after)
	if before != after {
		t.Errorf("ItemException rows changed %d -> %d", before, after)
	}
}
