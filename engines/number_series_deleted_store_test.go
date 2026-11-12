package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
	"time"
)

// Stage 115.2: a deleted store whose code cannot sit in a GST number (here a
// Location series number, slashes and all) must not block saving a series -
// it issues no new number, and the form cannot show it to fix. A live store
// with the same code still blocks, and the message names it.
func TestSeriesSaveIgnoresDeletedStoreSample(t *testing.T) {
	s := NumberSeries{DocType: "StockMove", Prefix: "SM", PaddingWidth: 4, ResetFrequency: "ANNUAL", YearFormat: "FY_COMPACT", ActiveStatus: true, IncludeStore: true}
	junk := seriesStore{Code: "Location/HQ/2026/000001", Segment: "Location/HQ/2026/000001", Name: "Old Test Store"}
	ho := seriesStore{Code: "HO", Segment: "HO", Name: "Head Office"}

	deleted := junk
	deleted.Deleted = true
	if err := validateNumberSeries(s, nil, nil, nil, []seriesStore{ho, deleted}, "IN"); err != nil {
		t.Fatalf("a deleted store blocked the save: %v", err)
	}

	err := validateNumberSeries(s, nil, nil, nil, []seriesStore{ho, junk}, "IN")
	if err == nil {
		t.Fatal("a live store printing an invalid GST number was let through")
	}
	if !strings.Contains(err.Error(), "Old Test Store") {
		t.Errorf("the message should name the store, got: %v", err)
	}

	// Its code stays taken: a live store may not print it.
	clash := seriesStore{Code: "NEW", Segment: "NEW", Name: "New Store"}
	if err := validateNumberSeries(s, nil, map[string]StoreSeriesOverride{"NEW": {Code: "HO"}}, nil, []seriesStore{ho, clash, deleted}, "IN"); err == nil {
		t.Error("two stores printing HO were let through")
	}
}

// Stage 115.1: the year can go at the end; issuing and the save checks share
// one layout (joinNumberParts), so the sample the form checks is the number
// the series prints.
func TestNumberLayoutYearAtEnd(t *testing.T) {
	cases := []struct {
		name, prefix, store, period, suffix, sep, pos, want string
	}{
		{"year before, as before", "SM", "HO", "2627", "", "", "", "SMHO26270001"},
		{"year at the end", "SM", "HO", "2627", "", "", YearPositionEnd, "SMHO00012627"},
		{"year at the end, after the suffix", "PO", "HO", "26-27", "X", "/", "end", "PO/HO/0001/X/26-27"},
		{"no year", "SM", "HO", "", "", "/", YearPositionEnd, "SM/HO/0001"},
		{"no store", "SM", "", "2627", "", "-", YearPositionEnd, "SM-0001-2627"},
	}
	for _, c := range cases {
		if got := joinNumberParts(c.prefix, c.store, c.period, "0001", c.suffix, c.sep, c.pos); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	s := NumberSeries{DocType: "StockMove", Prefix: "SM", PaddingWidth: 4, ResetFrequency: "ANNUAL", YearFormat: "FY_COMPACT", ActiveStatus: true, IncludeStore: true, YearPosition: YearPositionEnd}
	ho := []seriesStore{{Code: "HO", Segment: "HO", Name: "Head Office"}}
	if got := seriesSampleNumbers(s, nil, ho); len(got) != 1 || !strings.HasPrefix(got[0].Number, "SMHO0001") {
		t.Errorf("sample with the year at the end: %+v", got)
	}
	if err := validateNumberSeries(s, nil, nil, nil, ho, "IN"); err != nil {
		t.Errorf("year at the end refused: %v", err)
	}
	s.YearPosition = "MIDDLE"
	if err := validateNumberSeries(s, nil, nil, nil, ho, "IN"); err == nil {
		t.Error("an unknown year position was accepted")
	}
}

// Stage 115 end to end on a real tenant: a deleted store with a Location
// series code does not block the save, and a series with the year at the end
// issues its numbers that way.
func TestNumberSeriesDeletedStoreAndYearAtEndDB(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by, deleted_at) VALUES
		('Location', 'HO', '{"code":"HO","name":"HO"}', 'Active', 'admin', NULL),
		('Location', 'Location/HQ/2026/000001', '{"code":"Location/HQ/2026/000001","name":"Old store"}', 'Active', 'admin', NOW())`); err != nil {
		t.Fatalf("seed locations: %v", err)
	}
	fy := documentFinancialYear(time.Now())
	compact := strings.ReplaceAll(fy, "-", "")
	include := true
	if err := SaveNumberSeries(tenantID, NumberSeries{DocType: "ASN", Prefix: "ASN", Separator: "", PaddingWidth: 5, ResetFrequency: "ANNUAL", ActiveStatus: true, YearFormat: "FY_COMPACT", YearPosition: "end"},
		&include, map[string]StoreSeriesOverride{}); err != nil {
		t.Fatalf("save with a deleted store and the year at the end: %v", err)
	}
	all, err := ListNumberSeries(tenantID)
	if err != nil {
		t.Fatalf("ListNumberSeries: %v", err)
	}
	for _, s := range all {
		if s.DocType == "ASN" && s.YearPosition != YearPositionEnd {
			t.Errorf("year_position stored as %q, want END", s.YearPosition)
		}
	}
	if got, err := GenerateSequence(tenantID, "ASN", "HO", fy); err != nil || got != "ASNHO00001"+compact {
		t.Errorf("HO's number = %q (%v), want ASNHO00001%s", got, err, compact)
	}
}
