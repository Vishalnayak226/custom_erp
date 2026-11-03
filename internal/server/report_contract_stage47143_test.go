package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 47.14.3: a report run - what a report-sourced dashboard widget and an
// executive tile read - carries the same contract a dataset answer does:
// period and store scope from the run's own filters, refreshed = the run's
// time, basis from the report.
func TestStage47143ReportRunCarriesAContract(t *testing.T) {
	db.InitDB(testConnStr())
	run := func(query string) map[string]interface{} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/run/stock-ledger"+query, nil)
		req.SetPathValue("id", "stock-ledger")
		req.Header.Set("Resolved-Tenant-ID", "default")
		req.Header.Set("Resolved-Role", "Super Admin")
		req.Header.Set("Resolved-User-ID", "admin")
		rec := httptest.NewRecorder()
		handleRunReport(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("run = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Contract map[string]interface{} `json:"contract"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Contract == nil {
			t.Fatalf("no contract on the run: %s", rec.Body.String())
		}
		return body.Contract
	}

	before := time.Now().Add(-2 * time.Second)
	c := run("?from=2026-10-01&to=2026-10-31&location=HO")
	if c["period"] != "2026-10-01 to 2026-10-31" {
		t.Errorf("period = %v, want the run's dates", c["period"])
	}
	if c["scope"] != "Store HO" {
		t.Errorf("scope = %v, want the run's store", c["scope"])
	}
	at, err := time.Parse(time.RFC3339, c["as_of"].(string))
	if err != nil || at.Before(before) || at.After(time.Now().Add(2*time.Second)) {
		t.Errorf("as_of = %v is not the run's time", c["as_of"])
	}
	for _, key := range []string{"definition", "basis", "basis_note"} {
		if v, _ := c[key].(string); strings.TrimSpace(v) == "" {
			t.Errorf("contract.%s is empty", key)
		}
	}
	if c["basis"] != "Stock ledger" {
		t.Errorf("basis = %v, want the stock ledger for an Inventory report", c["basis"])
	}

	// No filters: the contract says so rather than staying silent.
	c = run("")
	if c["period"] != "All dates" || c["scope"] != "All stores" {
		t.Errorf("unfiltered contract = %v / %v", c["period"], c["scope"])
	}
}
