package engines

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 47.16.4: a posted tax document is reversed, never un-posted. The
// rule sits in validatePostedAmountsFrozen, which every generic save runs
// through (ValidateTransactionalRules), so it holds whatever an
// administrator's status-transition map allows.
func TestStage47164PostedDocumentCannotBeUnpostedBySave(t *testing.T) {
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	invoice := map[string]interface{}{"invoice_number": "SI-1", "total_amount": 1180.0}
	with := func(status string) map[string]interface{} {
		p := map[string]interface{}{"status": status}
		for k, v := range invoice {
			p[k] = v
		}
		return p
	}
	cases := []struct {
		doctype, prior, to string
		refused            bool
	}{
		{"SalesInvoice", "Approved", "Cancelled", true},
		{"SalesInvoice", "Approved", "Draft", true},
		{"SalesInvoice", "Paid", "Cancelled", true},
		{"SalesInvoice", "Approved", "Paid", false},     // a settlement's own status
		{"SalesInvoice", "Approved", "Approved", false}, // an ordinary edit
		{"SalesInvoice", "Draft", "Cancelled", false},   // nothing posted yet
		{"CreditNote", "Posted", "Cancelled", true},
		{"DebitNote", "Posted", "Draft", true},
		{"JournalVoucher", "Posted", "Reversed", true}, // only ReverseJournalVoucher writes it
	}
	for _, c := range cases {
		err := validatePostedAmountsFrozen(c.doctype, "X-1", c.prior, invoice, with(c.to))
		if c.refused && code(err) != "FIN-0844" {
			t.Errorf("%s %s -> %s by a save: %v, want FIN-0844", c.doctype, c.prior, c.to, err)
		}
		if !c.refused && err != nil {
			t.Errorf("%s %s -> %s by a save: refused (%v), want allowed", c.doctype, c.prior, c.to, err)
		}
	}
}

// Stage 47.16.4: the platform's scale test used to begin by deleting the
// client's whole general ledger. It now leaves every existing posting where
// it is, and the books still balance after it.
func TestStage47164ScaleTestNeverDeletesTheLedger(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if err := PostDoubleEntry(tenantID, "JournalVoucher", "JV-KEEP", map[string]int64{"1100": 10000}, map[string]int64{"4100": 10000}, "", "JV-KEEP:POST"); err != nil {
		t.Fatalf("seed posting: %v", err)
	}
	if err := SeedScaleTestData(tenantID, 2, "BAR-SCALE", 50); err != nil {
		t.Fatalf("SeedScaleTestData: %v", err)
	}
	if _, err := RunScaleSimulation(tenantID, 2, 4, "BAR-SCALE", 2); err != nil {
		t.Fatalf("RunScaleSimulation: %v", err)
	}
	var kept int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".gl_postings WHERE document_id = 'JV-KEEP'").Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != 2 {
		t.Fatalf("the journal posted before the scale test has %d of its 2 lines left", kept)
	}
	tb, err := GetTrialBalance(tenantID, allPostingsAsOf())
	if err != nil {
		t.Fatal(err)
	}
	if balanced, _ := tb["balanced"].(bool); !balanced {
		t.Errorf("the books do not balance after the scale test: %+v", tb)
	}
}

// Stage 47.16.4: the control map (docs/security/gst-companies-act-controls.md)
// stays honest - every control row names a test that exists, or says Gap.
func TestStage47164ControlMapNamesLiveTests(t *testing.T) {
	root := filepath.Join("..")
	raw, err := os.ReadFile(filepath.Join(root, "docs", "security", "gst-companies-act-controls.md"))
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string]bool{}
	fn := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	for _, dir := range []string{"engines", "internal", "cmd"} {
		filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, "_test.go") {
				b, _ := os.ReadFile(path)
				for _, m := range fn.FindAllSubmatch(b, -1) {
					defined[string(m[1])] = true
				}
			}
			return nil
		})
	}
	named := regexp.MustCompile("`(Test[A-Za-z0-9_]+)`")
	rows := 0
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "| C") {
			continue // control rows are "| C1 | ..."
		}
		rows++
		tests := named.FindAllStringSubmatch(line, -1)
		if len(tests) == 0 && !strings.Contains(line, "Gap") {
			t.Errorf("control row names no test and is not marked Gap: %s", line)
		}
		for _, m := range tests {
			if !defined[m[1]] {
				t.Errorf("control map names %s, which no test defines", m[1])
			}
		}
	}
	if rows < 10 {
		t.Fatalf("read only %d control rows", rows)
	}
}
