package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 126.4 (the user: "after typing the values it is not showing or not
// allowing to save"): a record code is checked as it prints - no store, no
// year - and a barcode counter's shape cannot be edited into a broken one.
func TestSeriesRecordCodeSampleAndBarcodeShape(t *testing.T) {
	stores := []seriesStore{{Code: "HO", Segment: "HO", Name: "Head Office"}, {Code: "BLRWAREHOUSE", Segment: "BLRWAREHOUSE", Name: "BLR Warehouse"}}
	vendor := NumberSeries{DocType: "Vendor", Prefix: "VENDOR", Separator: "/", PaddingWidth: 6, ResetFrequency: "ANNUAL",
		ActiveStatus: true, IncludeStore: true, RecordCode: true}
	if got := seriesSampleNumbers(vendor, nil, stores); len(got) != 1 || got[0].Number != "VENDOR/000001" {
		t.Fatalf("record code sample = %+v, want the one number VENDOR/000001 (no store, no year)", got)
	}
	if err := validateNumberSeries(vendor, nil, nil, nil, stores, "IN"); err != nil {
		t.Errorf("a 13-character vendor code was refused for a store and year it never prints: %v", err)
	}

	barcode := NumberSeries{DocType: "PIMBarcodeSeq", Prefix: "", Separator: "", PaddingWidth: 9, ResetFrequency: "NEVER",
		ActiveStatus: true, StoreOverrides: map[string]StoreSeriesOverride{}}
	off := barcode
	off.ActiveStatus = false
	if err := validateNumberSeries(off, &barcode, map[string]StoreSeriesOverride{}, nil, stores, "IN"); err != nil {
		t.Errorf("switching a barcode counter off was refused: %v", err)
	}
	shorter := barcode
	shorter.PaddingWidth = 8
	if err := validateNumberSeries(shorter, &barcode, map[string]StoreSeriesOverride{}, nil, stores, "IN"); err == nil || !strings.Contains(err.Error(), "barcode counter") {
		t.Errorf("an 8-digit barcode counter was let through: %v", err)
	}
	yearly := barcode
	yearly.ResetFrequency = "ANNUAL"
	if err := validateNumberSeries(yearly, &barcode, map[string]StoreSeriesOverride{}, nil, stores, "IN"); err == nil {
		t.Error("a barcode counter moved to a yearly restart was let through - it would repeat barcodes")
	}
}

// A series saved on the screen is confirmed, so a master's first record no
// longer replaces it with the suggestion; its Restart / Store stay as it
// counts; a store's code in a counter is saved in capitals.
func TestSeriesSavedOnScreenIsKept(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		('Location', 'HO', '{"code":"HO","name":"HO"}', 'Active', 'admin')`); err != nil {
		t.Fatalf("seed location: %v", err)
	}
	include := true
	if err := SaveNumberSeries(tenantID, NumberSeries{DocType: "Vendor", Prefix: "VEN", Separator: "", PaddingWidth: 4, ResetFrequency: "ANNUAL", ActiveStatus: true}, &include, map[string]StoreSeriesOverride{}); err != nil {
		t.Fatalf("save the Vendor series VEN/4: %v", err)
	}
	ms, err := GetMasterCodeSeries(tenantID, "Vendor")
	if err != nil || !ms.Configured || ms.Next != "VEN0001" {
		t.Fatalf("after the screen's save: %+v (%v), want configured, next VEN0001", ms, err)
	}
	if err := ensureMasterSeries(tenantID, "Vendor", nil); err != nil {
		t.Fatalf("first vendor's ensureMasterSeries: %v", err)
	}
	if code, err := GenerateSequence(tenantID, "Vendor", "", ""); err != nil || code != "VEN0001" {
		t.Errorf("first vendor code = %q (%v), want VEN0001 - the series typed on the screen", code, err)
	}
	all, _ := ListNumberSeries(tenantID)
	for _, s := range all {
		if s.DocType == "Vendor" && (s.Prefix != "VEN" || s.PaddingWidth != 4 || s.ResetFrequency != "NEVER" || s.IncludeStore || !s.RecordCode) {
			t.Errorf("Vendor series = %+v, want VEN, 4 digits, never, no store, a record code", s)
		}
	}

	// A counter's lower-case store code is saved in capitals, not refused.
	if err := SaveNumberSeries(tenantID, NumberSeries{DocType: "GRNBatchSeq", Prefix: "bt", Separator: "-", PaddingWidth: 4, ResetFrequency: "ANNUAL", ActiveStatus: true},
		&include, map[string]StoreSeriesOverride{"HO": {Code: "ho2"}}); err != nil {
		t.Fatalf("GRN batch series with store code ho2: %v", err)
	}
	all, _ = ListNumberSeries(tenantID)
	for _, s := range all {
		if s.DocType == "GRNBatchSeq" && (s.Prefix != "bt" || s.StoreOverrides["HO"].Code != "HO2") {
			t.Errorf("GRN batch series = %+v, want prefix bt kept and store code HO2", s)
		}
	}
}
