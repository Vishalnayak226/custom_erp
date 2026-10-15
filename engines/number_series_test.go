package engines

import (
	"custom_erp/db"
	"errors"
	"testing"
	"time"
)

// Stage 60.1/60.2 - number series shape (suffix, store override, Short Code
// segment), prefix collision refusal, and store identity uniqueness. Runs in
// its own throwaway tenant schema so the shared dev tenant's counters and
// Locations are never touched.
func TestNumberSeriesStoreSegmentSuffixAndOverrides(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		('Location', 'LOC-A', '{"code":"LOC-A","name":"Bandra Store","short_code":"BDR","facility_code":"UC-BANDRA"}', 'Active', 'admin'),
		('Location', 'LOC-B', '{"code":"LOC-B","name":"Andheri Store"}', 'Active', 'admin')`); err != nil {
		t.Fatalf("seed locations: %v", err)
	}

	include := true
	if err := SaveNumberSeries(tenantID, NumberSeries{
		DocType: "PO", Prefix: "PO", Suffix: "X", Separator: "/", PaddingWidth: 5,
		ResetFrequency: "NEVER", ActiveStatus: true,
	}, &include, map[string]StoreSeriesOverride{"LOC-B": {Prefix: "POAN"}}); err != nil {
		t.Fatalf("SaveNumberSeries: %v", err)
	}

	a, err := GenerateSequence(tenantID, "PO", "LOC-A", documentFinancialYear(time.Now()))
	if err != nil {
		t.Fatalf("GenerateSequence A: %v", err)
	}
	if a != "PO/BDR/00001/X" {
		t.Errorf("store A number = %q, want PO/BDR/00001/X (short code segment + suffix)", a)
	}
	b, err := GenerateSequence(tenantID, "PO", "LOC-B", documentFinancialYear(time.Now()))
	if err != nil {
		t.Fatalf("GenerateSequence B: %v", err)
	}
	if b != "POAN/LOC-B/00001/X" {
		t.Errorf("store B number = %q, want POAN/LOC-B/00001/X (store prefix, store code when no short code)", b)
	}

	// Another series may not take a prefix this series already prints.
	var verr *ValidationError
	err = SaveNumberSeries(tenantID, NumberSeries{DocType: "GRN", Prefix: "poan", Separator: "/", PaddingWidth: 5, ResetFrequency: "ANNUAL", ActiveStatus: true}, nil, nil)
	if !errors.As(err, &verr) || verr.Code != "ADMINC-0310" {
		t.Errorf("GRN taking PO's store prefix: err = %v, want ADMINC-0310", err)
	}
	err = SaveNumberSeries(tenantID, NumberSeries{DocType: "PO", Prefix: "PO", Separator: "/", PaddingWidth: 5, ResetFrequency: "ANNUAL", ActiveStatus: true},
		nil, map[string]StoreSeriesOverride{"LOC-A": {Prefix: "GRN"}})
	if !errors.As(err, &verr) || verr.Code != "ADMINC-0310" {
		t.Errorf("PO store override taking GRN's prefix: err = %v, want ADMINC-0310", err)
	}
	err = SaveNumberSeries(tenantID, NumberSeries{DocType: "PO", Prefix: "PO X", Separator: "/", PaddingWidth: 5, ResetFrequency: "ANNUAL", ActiveStatus: true}, nil, nil)
	if !errors.As(err, &verr) || verr.Code != "ADMINC-0311" {
		t.Errorf("prefix with a space: err = %v, want ADMINC-0311", err)
	}

	// Omitted overrides keep the stored ones.
	if err := SaveNumberSeries(tenantID, NumberSeries{DocType: "PO", Prefix: "PO", Separator: "/", PaddingWidth: 5, ResetFrequency: "NEVER", ActiveStatus: true}, nil, nil); err != nil {
		t.Fatalf("re-save PO: %v", err)
	}
	all, _ := ListNumberSeries(tenantID)
	for _, s := range all {
		if s.DocType == "PO" && s.StoreOverrides["LOC-B"].Prefix != "POAN" {
			t.Errorf("a save without store_overrides dropped them: %+v", s.StoreOverrides)
		}
	}

	// Store identity: Short Code may not repeat another store's short code or
	// store code; Facility Code may not repeat.
	err = ValidateMasterDataRules(tenantID, "LOC-B", "Location", map[string]interface{}{"code": "LOC-B", "name": "Andheri Store", "short_code": "bdr"})
	if !errors.As(err, &verr) || verr.Code != "MASTER-0312" {
		t.Errorf("duplicate short code: err = %v, want MASTER-0312", err)
	}
	err = ValidateMasterDataRules(tenantID, "LOC-B", "Location", map[string]interface{}{"code": "LOC-B", "name": "Andheri Store", "short_code": "loc-a"})
	if !errors.As(err, &verr) || verr.Code != "MASTER-0312" {
		t.Errorf("short code equal to another store code: err = %v, want MASTER-0312", err)
	}
	err = ValidateMasterDataRules(tenantID, "LOC-B", "Location", map[string]interface{}{"code": "LOC-B", "name": "Andheri Store", "facility_code": "uc-bandra"})
	if !errors.As(err, &verr) || verr.Code != "MASTER-0313" {
		t.Errorf("duplicate facility code: err = %v, want MASTER-0313", err)
	}
	if err := ValidateMasterDataRules(tenantID, "LOC-A", "Location", map[string]interface{}{"code": "LOC-A", "name": "Bandra Store", "short_code": "BDR", "facility_code": "UC-BANDRA"}); err != nil {
		t.Errorf("re-saving a store with its own codes: %v", err)
	}

	if got := ResolveFacilityCode(schemaName, "uc-bandra"); got != "LOC-A" {
		t.Errorf("ResolveFacilityCode = %q, want LOC-A", got)
	}
	if got := ResolveFacilityCode(schemaName, "LOC-B"); got != "LOC-B" {
		t.Errorf("a store code passes through unchanged, got %q", got)
	}
}
