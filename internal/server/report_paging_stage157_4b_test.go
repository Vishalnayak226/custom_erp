package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 157.4b: the page params ask for the page and never reach the report as
// filters; a page past the end is not "no data"; totals cover every page.
func TestStage1574bRunReportPages(t *testing.T) {
	db.InitDB(testConnStr())
	var seen map[string]string
	engines.RegisterReport(engines.ReportDefinition{ID: "test-1574b-handler", Label: "Paging", Category: "Inventory", StoreKey: "-",
		Columns: []engines.ReportColumn{{Key: "n", Label: "N"}, {Key: "amount", Label: "Amount", Total: true}},
		Run: func(_ string, params map[string]string) ([]map[string]interface{}, error) {
			seen = params
			rows := make([]map[string]interface{}, 120)
			for i := range rows {
				rows[i] = map[string]interface{}{"n": i, "amount": 2.5}
			}
			return rows, nil
		}})
	type answer struct {
		Rows   []map[string]interface{} `json:"rows"`
		Code   string                   `json:"code"`
		Paging struct {
			Total  int                `json:"total"`
			Limit  int                `json:"limit"`
			Offset int                `json:"offset"`
			Totals map[string]float64 `json:"totals"`
		} `json:"paging"`
	}
	run := func(query string) answer {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/run/test-1574b-handler"+query, nil)
		req.SetPathValue("id", "test-1574b-handler")
		req.Header.Set("Resolved-Tenant-ID", "default")
		req.Header.Set("Resolved-Role", "Super Admin")
		req.Header.Set("Resolved-User-ID", "admin")
		rec := httptest.NewRecorder()
		handleRunReport(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("run = %d: %s", rec.Code, rec.Body.String())
		}
		var out answer
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	got := run("?keep=1&page_size=50&page_offset=100")
	if len(got.Rows) != 20 || got.Paging.Total != 120 || got.Paging.Limit != 50 || got.Paging.Offset != 100 {
		t.Fatalf("last page: %d rows, paging %+v", len(got.Rows), got.Paging)
	}
	if got.Paging.Totals["amount"] != 300 {
		t.Fatalf("totals must cover every page: %+v", got.Paging.Totals)
	}
	if seen["keep"] != "1" || seen["page_size"] != "" || seen["page_offset"] != "" || seen["page_order"] != "" {
		t.Fatalf("the report saw %v", seen)
	}

	if got := run("?page_size=50&page_offset=500"); len(got.Rows) != 0 || got.Code == "REPORT-0160" || got.Paging.Total != 120 {
		t.Fatalf("past the end: code %q, %d rows, paging %+v", got.Code, len(got.Rows), got.Paging)
	}
}
