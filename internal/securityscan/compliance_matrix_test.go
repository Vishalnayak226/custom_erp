package securityscan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Stage 47.16.1: the India compliance control matrix
// (docs/security/compliance-control-matrix.json) stays complete, versioned and
// honest - every evidence test and file it cites exists, every row has an
// owner and a review date, and the companion page lists every row.

type complianceMatrix struct {
	Version       int    `json:"version"`
	Status        string `json:"status"`
	Owner         string `json:"owner"`
	EffectiveFrom string `json:"effective_from"`
	Disclaimer    string `json:"disclaimer"`
	Changelog     []struct {
		Version int    `json:"version"`
		Date    string `json:"date"`
		Change  string `json:"change"`
	} `json:"changelog"`
	Rows []complianceRow `json:"rows"`
}

type complianceRow struct {
	ID             string   `json:"id"`
	Jurisdiction   string   `json:"jurisdiction"`
	Rule           string   `json:"rule"`
	Trigger        string   `json:"trigger"`
	Responsibility string   `json:"responsibility"`
	CustomerDuty   string   `json:"customer_duty"`
	ControlTypes   []string `json:"control_types"`
	Control        string   `json:"control"`
	Implementation []string `json:"implementation"`
	Evidence       []string `json:"evidence"`
	Retention      string   `json:"retention"`
	Owner          string   `json:"owner"`
	Status         string   `json:"status"`
	Gap            string   `json:"gap"`
	LastReviewed   string   `json:"last_reviewed"`
	ReviewBy       string   `json:"review_by"`
}

const complianceMatrixPath = "docs/security/compliance-control-matrix.json"

var (
	complianceIDRe     = regexp.MustCompile(`^CM-[A-Z]{2}-\d{3}$`)
	complianceTestRe   = regexp.MustCompile(`^Test[A-Za-z0-9_]+$`)
	complianceOwners   = set("security-owner", "privacy-lead", "finance-controller", "gst-practitioner", "statutory-auditor", "platform-operator")
	complianceResp     = set("product", "customer", "shared")
	complianceControls = set("preventive", "detective", "corrective")
	complianceStatuses = set("implemented", "partial", "gap", "needs-decision")
)

func set(v ...string) map[string]bool {
	m := map[string]bool{}
	for _, s := range v {
		m[s] = true
	}
	return m
}

func loadComplianceMatrix(t *testing.T) complianceMatrix {
	t.Helper()
	return loadComplianceMatrixFrom(t, filepath.Join(repoRoot, complianceMatrixPath))
}

func loadComplianceMatrixFrom(t *testing.T, path string) complianceMatrix {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var m complianceMatrix
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("%s does not parse as the matrix shape: %v", path, err)
	}
	return m
}

// complianceReviewOverdue is whether a row's review_by date has passed: the
// row is due on that day and overdue from the next. One rule for the build's
// OVERDUE log and the release gate below.
func complianceReviewOverdue(reviewBy string, today time.Time) bool {
	due, err := time.Parse("2006-01-02", reviewBy)
	return err == nil && today.After(due.AddDate(0, 0, 1))
}

// repoTestNames is every func TestXxx declared in a _test.go file of this
// repository, so a matrix row can cite a test by name and be held to it.
func repoTestNames(t *testing.T) map[string]bool {
	t.Helper()
	declRe := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	names := map[string]bool{}
	err := filepath.Walk(repoRoot, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			if skippedDirs[fi.Name()] && fi.Name() != "docs" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, m := range declRe.FindAllSubmatch(data, -1) {
			names[string(m[1])] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestComplianceMatrixIsCompleteAndItsEvidenceExists(t *testing.T) {
	m := loadComplianceMatrix(t)
	if m.Version < 1 || m.Owner == "" || strings.TrimSpace(m.Disclaimer) == "" || m.Status == "" {
		t.Fatal("the matrix needs a version, an owner, a status and its not-a-certification disclaimer")
	}
	if len(m.Changelog) == 0 || m.Changelog[len(m.Changelog)-1].Version != m.Version {
		t.Fatalf("the last changelog entry must be version %d - raise version and log the change together", m.Version)
	}
	if _, err := time.Parse("2006-01-02", m.EffectiveFrom); err != nil {
		t.Errorf("effective_from %q is not a date", m.EffectiveFrom)
	}
	if len(m.Rows) == 0 {
		t.Fatal("the matrix has no rows")
	}
	tests := repoTestNames(t)
	if len(tests) < 500 {
		t.Fatalf("found only %d test functions - the walk is not reading this repository", len(tests))
	}
	today := time.Now()
	seen := map[string]bool{}
	for _, r := range m.Rows {
		where := r.ID
		if !complianceIDRe.MatchString(r.ID) || seen[r.ID] {
			t.Errorf("row id %q is malformed or repeated", r.ID)
		}
		seen[r.ID] = true
		if r.Jurisdiction == "" || !strings.Contains(r.ID, "-"+r.Jurisdiction+"-") {
			t.Errorf("%s: jurisdiction %q does not match the id", where, r.Jurisdiction)
		}
		for name, v := range map[string]string{"rule": r.Rule, "trigger": r.Trigger, "control": r.Control, "retention": r.Retention} {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: %s is empty", where, name)
			}
		}
		if !complianceResp[r.Responsibility] {
			t.Errorf("%s: responsibility %q must be product, customer or shared", where, r.Responsibility)
		}
		if r.Responsibility != "product" && strings.TrimSpace(r.CustomerDuty) == "" {
			t.Errorf("%s: a %s row must say what the customer does (customer_duty)", where, r.Responsibility)
		}
		if len(r.ControlTypes) == 0 {
			t.Errorf("%s: no control type", where)
		}
		for _, c := range r.ControlTypes {
			if !complianceControls[c] {
				t.Errorf("%s: control type %q", where, c)
			}
		}
		if !complianceOwners[r.Owner] {
			t.Errorf("%s: owner %q is not one of the matrix roles", where, r.Owner)
		}
		if !complianceStatuses[r.Status] {
			t.Errorf("%s: status %q", where, r.Status)
		}
		if (r.Status == "implemented") == (strings.TrimSpace(r.Gap) != "") {
			t.Errorf("%s: an implemented row has no gap, every other row names its gap (status %q, gap %q)", where, r.Status, r.Gap)
		}
		if r.Status != "gap" && len(r.Implementation) == 0 {
			t.Errorf("%s: a %s row names the code that implements it", where, r.Status)
		}
		if len(r.Evidence) == 0 {
			t.Errorf("%s: no evidence", where)
		}
		for _, p := range r.Implementation {
			if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(p))); err != nil {
				t.Errorf("%s: implementation %s does not exist", where, p)
			}
		}
		for _, e := range r.Evidence {
			switch {
			case complianceTestRe.MatchString(e):
				if !tests[e] {
					t.Errorf("%s: evidence test %s is not declared in any _test.go file", where, e)
				}
			default:
				if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(e))); err != nil {
					t.Errorf("%s: evidence %s is neither a test name nor an existing file", where, e)
				}
			}
		}
		last, err1 := time.Parse("2006-01-02", r.LastReviewed)
		due, err2 := time.Parse("2006-01-02", r.ReviewBy)
		switch {
		case err1 != nil || err2 != nil:
			t.Errorf("%s: last_reviewed %q / review_by %q must be YYYY-MM-DD dates", where, r.LastReviewed, r.ReviewBy)
		case !due.After(last) || due.After(last.AddDate(1, 0, 0)):
			t.Errorf("%s: review_by must fall after last_reviewed and within a year of it", where)
		case complianceReviewOverdue(r.ReviewBy, today):
			// Logged, not failed: an overdue review stops the release (gate
			// G4e, the test below), never a developer's build (47.16.1a).
			t.Logf("OVERDUE %s: review_by %s has passed - review the row and move both dates", where, r.ReviewBy)
		}
	}
}

// Stage 47.16.1a: an overdue review fails the release, not the build. An
// ordinary `go test` skips this; Release gate G4e runs it with
// ERP_COMPLIANCE_REVIEW_GATE=1 and refuses to ship while any row's review_by
// has passed. ERP_COMPLIANCE_MATRIX points it at another copy of the matrix,
// so the gate can be tried on a temporary one.
func TestComplianceMatrixReviewsAreNotOverdue(t *testing.T) {
	if os.Getenv("ERP_COMPLIANCE_REVIEW_GATE") != "1" {
		t.Skip("release gate G4e only (ERP_COMPLIANCE_REVIEW_GATE=1)")
	}
	path := os.Getenv("ERP_COMPLIANCE_MATRIX")
	if path == "" {
		path = filepath.Join(repoRoot, complianceMatrixPath)
	}
	m := loadComplianceMatrixFrom(t, path)
	if len(m.Rows) == 0 {
		t.Fatalf("%s has no rows", path)
	}
	today := time.Now()
	for _, r := range m.Rows {
		if _, err := time.Parse("2006-01-02", r.ReviewBy); err != nil {
			t.Errorf("%s: review_by %q is not a YYYY-MM-DD date", r.ID, r.ReviewBy)
		} else if complianceReviewOverdue(r.ReviewBy, today) {
			t.Errorf("OVERDUE %s: review_by %s has passed - %s reviews the row and moves last_reviewed and review_by", r.ID, r.ReviewBy, r.Owner)
		}
	}
}

// The overdue rule itself, both ways and on its boundary.
func TestComplianceReviewOverdueRule(t *testing.T) {
	today := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	for reviewBy, want := range map[string]bool{
		"2026-10-27": true,  // passed
		"2026-10-28": true,  // passed yesterday
		"2026-10-29": false, // due today
		"2027-01-28": false, // in the future
		"not-a-date": false, // malformed: the completeness test reports it
	} {
		if got := complianceReviewOverdue(reviewBy, today); got != want {
			t.Errorf("review_by %s on 2026-10-29: overdue %v, want %v", reviewBy, got, want)
		}
	}
}

func TestComplianceMatrixPageListsEveryRowAndTheVersion(t *testing.T) {
	m := loadComplianceMatrix(t)
	page, err := os.ReadFile(filepath.Join(repoRoot, "docs/security/compliance-control-matrix.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)
	if want := fmt.Sprintf("**version %d**", m.Version); !strings.Contains(text, want) {
		t.Errorf("compliance-control-matrix.md must name the matrix's current %s", want)
	}
	for _, r := range m.Rows {
		row := regexp.MustCompile(`(?m)^\| ` + regexp.QuoteMeta(r.ID) + ` \|.*\| ` + regexp.QuoteMeta(r.Responsibility) + ` \| ` + regexp.QuoteMeta(r.Status) + ` \|$`)
		if !row.MatchString(strings.ReplaceAll(text, "\r\n", "\n")) {
			t.Errorf("compliance-control-matrix.md has no table row for %s showing %s / %s", r.ID, r.Responsibility, r.Status)
		}
	}
}
