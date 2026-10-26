package engines

import (
	"custom_erp/db"
	"errors"
	"strings"
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
		DocType: "PO", Prefix: "PO", Suffix: "X", Separator: "/", PaddingWidth: 3,
		ResetFrequency: "NEVER", ActiveStatus: true,
	}, &include, map[string]StoreSeriesOverride{"LOC-B": {Prefix: "POAN"}}); err != nil {
		t.Fatalf("SaveNumberSeries: %v", err)
	}

	a, err := GenerateSequence(tenantID, "PO", "LOC-A", documentFinancialYear(time.Now()))
	if err != nil {
		t.Fatalf("GenerateSequence A: %v", err)
	}
	if a != "PO/BDR/001/X" {
		t.Errorf("store A number = %q, want PO/BDR/001/X (short code segment + suffix)", a)
	}
	b, err := GenerateSequence(tenantID, "PO", "LOC-B", documentFinancialYear(time.Now()))
	if err != nil {
		t.Fatalf("GenerateSequence B: %v", err)
	}
	if b != "POAN/LOC-B/001/X" {
		t.Errorf("store B number = %q, want POAN/LOC-B/001/X (an older store prefix, store code when no short code)", b)
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
	// Stage 132.20: a deleted store's Short Code stays reserved (its documents
	// carry it), and the refusal says it is a deleted store's.
	if _, err := db.DB.Exec("UPDATE " + schemaName + ".documents SET deleted_at = CURRENT_TIMESTAMP WHERE id = 'LOC-A'"); err != nil {
		t.Fatalf("soft-delete LOC-A: %v", err)
	}
	err = ValidateMasterDataRules(tenantID, "LOC-B", "Location", map[string]interface{}{"code": "LOC-B", "name": "Andheri Store", "short_code": "bdr"})
	if !errors.As(err, &verr) || verr.Code != "MASTER-0312" || !strings.Contains(verr.Message, "deleted store") {
		t.Errorf("a deleted store's short code: err = %v, want MASTER-0312 naming a deleted store", err)
	}
	if _, err := db.DB.Exec("UPDATE " + schemaName + ".documents SET deleted_at = NULL WHERE id = 'LOC-A'"); err != nil {
		t.Fatalf("restore LOC-A: %v", err)
	}

	if got := ResolveFacilityCode(schemaName, "uc-bandra"); got != "LOC-A" {
		t.Errorf("ResolveFacilityCode = %q, want LOC-A", got)
	}
	if got := ResolveFacilityCode(schemaName, "LOC-B"); got != "LOC-B" {
		t.Errorf("a store code passes through unchanged, got %q", got)
	}
}

// Stage 86.2 - the year format prints only the segment (the counter bucket
// stays the financial year), and a monthly START_* format prints the
// calendar month so January to March read right.
func TestFormatPeriodSegment(t *testing.T) {
	jul := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	jan := time.Date(2027, time.January, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		format, reset, fy string
		now               time.Time
		want              string
	}{
		{"", "ANNUAL", "26-27", jul, "26-27"},
		{"FY_COMPACT", "ANNUAL", "26-27", jul, "2627"},
		{"FY_LONG", "ANNUAL", "26-27", jul, "2026-27"},
		{"START_YY", "ANNUAL", "26-27", jul, "26"},
		{"START_YYYY", "ANNUAL", "26-27", jul, "2026"},
		{"", "MONTHLY", "26-27", jul, "26-27-07"},
		{"FY_COMPACT", "MONTHLY", "26-27", jul, "262707"},
		{"FY_LONG", "MONTHLY", "26-27", jul, "2026-27-07"},
		{"START_YY", "MONTHLY", "26-27", jan, "2701"},
		{"START_YYYY", "MONTHLY", "26-27", jan, "202701"},
		// A GRN batch date is not a financial year - printed as given.
		{"FY_COMPACT", "ANNUAL", "261015", jul, "261015"},
		{"", "MONTHLY", "26", jul, "26-07"},
	}
	for _, tc := range cases {
		if got := formatPeriodSegment(tc.format, tc.reset, tc.fy, tc.now); got != tc.want {
			t.Errorf("formatPeriodSegment(%q, %q, %q) = %q, want %q", tc.format, tc.reset, tc.fy, got, tc.want)
		}
	}
}

// Stage 86.2 - a store's own code replaces only the store segment; the year
// format and a blank separator print as set; two stores may not print one
// code; and no document series may print more than 16 characters.
func TestNumberSeriesStoreCodeYearFormatAndGSTLength(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		('Location', 'HO', '{"code":"HO","name":"HO"}', 'Active', 'admin'),
		('Location', 'JAYANAGAR', '{"code":"JAYANAGAR","name":"Jayanagar","short_code":"JAY"}', 'Active', 'admin')`); err != nil {
		t.Fatalf("seed locations: %v", err)
	}
	fy := documentFinancialYear(time.Now())
	compact := strings.ReplaceAll(fy, "-", "")
	include := true
	var verr *ValidationError

	// The screenshot's series with one more digit: the longest store's
	// ASN/JAY/26-27/000001 is 20 characters - refused, naming that store's number.
	err := SaveNumberSeries(tenantID, NumberSeries{DocType: "ASN", Prefix: "ASN", Separator: "/", PaddingWidth: 6, ResetFrequency: "ANNUAL", ActiveStatus: true}, &include, map[string]StoreSeriesOverride{})
	if !errors.As(err, &verr) || verr.Code != "ADMINC-0311" || !strings.Contains(verr.Message, "ASN/JAY/"+fy+"/000001") || !strings.Contains(verr.Message, "16") {
		t.Fatalf("a 20-character series: err = %v, want ADMINC-0311 naming ASN/JAY/%s/000001 and 16", err, fy)
	}
	// Stage 126.4: an existing client's seeded series as it is
	// (ASN/JAY/26-27/00001, 19 characters, made before the 16-character
	// rule) saves when nothing in its number changes - a status change is
	// not refused. Since 130.6 a fresh client's ASN ships short, so the
	// legacy shape is put back here first.
	if _, err := db.DB.Exec(`UPDATE ` + schemaName + `.prefix_configs SET prefix = 'ASN', separator = '/', padding_width = 5, year_format = '', include_store = TRUE WHERE doc_type = 'ASN'`); err != nil {
		t.Fatalf("legacy ASN series: %v", err)
	}
	if err := SaveNumberSeries(tenantID, NumberSeries{DocType: "ASN", Prefix: "ASN", Separator: "/", PaddingWidth: 5, ResetFrequency: "ANNUAL", ActiveStatus: false}, &include, map[string]StoreSeriesOverride{}); err != nil {
		t.Fatalf("status-only save of the seeded ASN series: %v", err)
	}

	// The store's own code changes only its segment; 2627 with no separator fits.
	if err := SaveNumberSeries(tenantID, NumberSeries{DocType: "ASN", Prefix: "ASN", Separator: "", PaddingWidth: 5, ResetFrequency: "ANNUAL", ActiveStatus: true, YearFormat: "FY_COMPACT"},
		&include, map[string]StoreSeriesOverride{"HO": {Code: "HQ"}}); err != nil {
		t.Fatalf("save ASN with a store code and 2627: %v", err)
	}
	if got, err := GenerateSequence(tenantID, "ASN", "HO", fy); err != nil || got != "ASNHQ"+compact+"00001" {
		t.Errorf("HO's number = %q (%v), want ASNHQ%s00001", got, err, compact)
	}
	if got, err := GenerateSequence(tenantID, "ASN", "JAYANAGAR", fy); err != nil || got != "ASNJAY"+compact+"00001" {
		t.Errorf("Jayanagar's number = %q (%v), want ASNJAY%s00001 (its Short Code)", got, err, compact)
	}

	// Changing the year format mid-year carries the count on: same bucket.
	if err := SaveNumberSeries(tenantID, NumberSeries{DocType: "ASN", Prefix: "ASN", Separator: "/", PaddingWidth: 5, ResetFrequency: "ANNUAL", ActiveStatus: true, YearFormat: "START_YY"},
		&include, map[string]StoreSeriesOverride{"HO": {Code: "HQ"}}); err != nil {
		t.Fatalf("save ASN with the year as 26: %v", err)
	}
	if got, err := GenerateSequence(tenantID, "ASN", "JAYANAGAR", fy); err != nil || got != "ASN/JAY/"+fy[:2]+"/00002" {
		t.Errorf("after the format change = %q (%v), want ASN/JAY/%s/00002", got, err, fy[:2])
	}

	// No year, no separator: one continuous series.
	if err := SaveNumberSeries(tenantID, NumberSeries{DocType: "ASN", Prefix: "ASN", Separator: "", PaddingWidth: 5, ResetFrequency: "NEVER", ActiveStatus: true},
		&include, map[string]StoreSeriesOverride{}); err != nil {
		t.Fatalf("save ASN with no year and no separator: %v", err)
	}
	if got, err := GenerateSequence(tenantID, "ASN", "JAYANAGAR", fy); err != nil || got != "ASNJAY00001" {
		t.Errorf("no year, no separator = %q (%v), want ASNJAY00001", got, err)
	}

	// Two stores may not print one code.
	err = SaveNumberSeries(tenantID, NumberSeries{DocType: "ASN", Prefix: "ASN", Separator: "", PaddingWidth: 5, ResetFrequency: "NEVER", ActiveStatus: true},
		&include, map[string]StoreSeriesOverride{"HO": {Code: "jay"}})
	if !errors.As(err, &verr) || verr.Code != "ADMINC-0311" {
		t.Errorf("HO taking Jayanagar's code: err = %v, want ADMINC-0311", err)
	}
	err = SaveNumberSeries(tenantID, NumberSeries{DocType: "ASN", Prefix: "ASN", Separator: "/", PaddingWidth: 5, ResetFrequency: "ANNUAL", ActiveStatus: true, YearFormat: "FY_WEEKLY"}, &include, nil)
	if !errors.As(err, &verr) || verr.Code != "ADMINC-0311" {
		t.Errorf("an unknown year format: err = %v, want ADMINC-0311", err)
	}

	// GST characters: "/", "-" or no separator; capitals, digits and "-" in
	// the parts; never a leading 0 - though the running number may start with
	// one (ASNHQ00001).
	for name, series := range map[string]NumberSeries{
		"a dot separator": {DocType: "ASN", Prefix: "ASN", Separator: ".", PaddingWidth: 5, ResetFrequency: "NEVER", ActiveStatus: true},
		"a leading 0":     {DocType: "ASN", Prefix: "0AS", Separator: "", PaddingWidth: 5, ResetFrequency: "NEVER", ActiveStatus: true},
		"an underscore":   {DocType: "ASN", Prefix: "AS_N", Separator: "", PaddingWidth: 5, ResetFrequency: "NEVER", ActiveStatus: true},
	} {
		if err := SaveNumberSeries(tenantID, series, &include, map[string]StoreSeriesOverride{}); !errors.As(err, &verr) || verr.Code != "ADMINC-0311" {
			t.Errorf("%s: err = %v, want ADMINC-0311", name, err)
		}
	}
	// Typed in lower case, saved in capitals.
	if err := SaveNumberSeries(tenantID, NumberSeries{DocType: "ASN", Prefix: "asn", Separator: "", PaddingWidth: 5, ResetFrequency: "NEVER", ActiveStatus: true},
		&include, map[string]StoreSeriesOverride{"HO": {Code: "hq"}}); err != nil {
		t.Fatalf("save a lower-case series: %v", err)
	}

	// The format is kept on the series and listed back.
	all, err := ListNumberSeries(tenantID)
	if err != nil {
		t.Fatalf("ListNumberSeries: %v", err)
	}
	for _, s := range all {
		if s.DocType == "ASN" && (s.Separator != "" || s.ResetFrequency != "NEVER" || s.Prefix != "ASN" || s.StoreOverrides["HO"].Code != "HQ") {
			t.Errorf("ASN listed as %+v, want ASN in capitals, store code HQ, a blank separator and NEVER", s)
		}
	}

	// A store whose own code breaks the rule is named; its Code in number fixes it.
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		('Location', 'loc_x', '{"code":"loc_x","name":"Old Store"}', 'Active', 'admin')`); err != nil {
		t.Fatalf("seed a store: %v", err)
	}
	err = SaveNumberSeries(tenantID, NumberSeries{DocType: "ASN", Prefix: "ASN", Separator: "", PaddingWidth: 5, ResetFrequency: "NEVER", ActiveStatus: true}, &include, nil)
	if !errors.As(err, &verr) || verr.Code != "ADMINC-0311" || !strings.Contains(verr.Message, "loc_x") {
		t.Errorf("a store printing loc_x: err = %v, want ADMINC-0311 naming loc_x", err)
	}
	if err := SaveNumberSeries(tenantID, NumberSeries{DocType: "ASN", Prefix: "ASN", Separator: "", PaddingWidth: 5, ResetFrequency: "NEVER", ActiveStatus: true},
		&include, map[string]StoreSeriesOverride{"HO": {Code: "HQ"}, "loc_x": {Code: "OLD"}}); err != nil {
		t.Errorf("the same with the store's own code OLD: %v", err)
	}

	// A counter mints digits, not a document number - not held to 16.
	if err := SaveNumberSeries(tenantID, NumberSeries{DocType: "GRNBatchSeq", Prefix: "BATCHNO", Separator: "/", PaddingWidth: 8, ResetFrequency: "ANNUAL", ActiveStatus: true}, &include, nil); err != nil {
		t.Errorf("a long GRN batch series: %v, want it saved", err)
	}
}
