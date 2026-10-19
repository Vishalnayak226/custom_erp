package engines

import (
	"strings"
	"testing"
)

// Stage 121.2: a "/" in a document series' prefix, suffix or store code saves
// - the user's PO series, separator None and suffix "/", read POHO00001/2627
// on the form and was refused by the server. GST's whole-number rule still
// decides: a number may not start with "/". A counter keeps its old characters.
func TestSeriesSlashInPrefixSuffixAndStoreCode(t *testing.T) {
	ho := []seriesStore{{Code: "HO", Segment: "HO", Name: "Head Office"}}
	po := NumberSeries{DocType: "PO", Prefix: "PO", Suffix: "/", Separator: "", PaddingWidth: 5, ResetFrequency: "ANNUAL",
		YearFormat: "FY_COMPACT", YearPosition: YearPositionEnd, ActiveStatus: true, IncludeStore: true}
	if got := seriesSampleNumbers(po, nil, ho); len(got) != 1 || !strings.HasPrefix(got[0].Number, "POHO00001/") {
		t.Fatalf("sample = %+v, want POHO00001/<year>", got)
	}
	if err := validateNumberSeries(po, nil, nil, nil, ho, "IN"); err != nil {
		t.Errorf("suffix \"/\" refused: %v", err)
	}

	withSlash := po
	withSlash.Prefix, withSlash.Suffix = "PO/", ""
	if err := validateNumberSeries(withSlash, nil, nil, nil, ho, "IN"); err != nil {
		t.Errorf("prefix \"PO/\" refused: %v", err)
	}
	if err := validateNumberSeries(po, nil, map[string]StoreSeriesOverride{"HO": {Code: "H/O", Suffix: "/A"}}, nil, ho, "IN"); err != nil {
		t.Errorf("store code \"H/O\" and suffix \"/A\" refused: %v", err)
	}
	if err := validateNumberSeries(po, nil, nil, nil, ho, "AE"); err != nil {
		t.Errorf("suffix \"/\" refused outside India: %v", err)
	}

	leading := po
	leading.Prefix = "/PO"
	if err := validateNumberSeries(leading, nil, nil, nil, ho, "IN"); err == nil {
		t.Error("a number starting with \"/\" was let through - GST does not allow it")
	}
	spaced := po
	spaced.Prefix = "PO X"
	if err := validateNumberSeries(spaced, nil, nil, nil, ho, "IN"); err == nil {
		t.Error("a prefix with a space was let through")
	}

	batch := NumberSeries{DocType: "GRNBatchSeq", Prefix: "B/", PaddingWidth: 4, ResetFrequency: "ANNUAL", ActiveStatus: true}
	if err := validateNumberSeries(batch, nil, nil, nil, nil, "IN"); err == nil {
		t.Error("a counter took \"/\" - its characters were not meant to change")
	}
}
