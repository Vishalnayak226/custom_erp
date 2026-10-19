package server

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 104: the default client's Control Panel showed 36 "errors" in 24
// hours and none was a server fault - 24 were reports with no rows, the rest
// messages users were shown (a missing field, an expired session, a refused
// self-approval).

// TestClientErrorCodesSplitRejectionsFromFaults covers 104.2's code list:
// the 2xx/4xx catalog codes are rejections, the 5xx ones stay faults, and
// every code has the "[A-Z]+-[0-9]+" shape engines' SQL reads it back by.
func TestClientErrorCodesSplitRejectionsFromFaults(t *testing.T) {
	shape := regexp.MustCompile(`^[A-Z]+-[0-9]+$`)
	for code := range errorCatalog {
		if !shape.MatchString(code) {
			t.Errorf("catalog code %q does not match %s - the fault filter could not read it", code, shape)
		}
	}

	codes := clientErrorCodes()
	if !sort.StringsAreSorted(codes) {
		t.Error("clientErrorCodes is not sorted")
	}
	in := map[string]bool{}
	for _, c := range codes {
		in[c] = true
	}
	// The codes behind the 36, plus an extension row added by another init.
	for _, c := range []string{"REPORT-0160", "GLOBAL-0001", "GLOBAL-0002", "GLOBAL-0004", "GLOBAL-0006", "GLOBAL-0009", "GLOBAL-0019", "META-0198", "GLOBAL-0303"} {
		if !in[c] {
			t.Errorf("%s answers 2xx/4xx and must count as a rejection", c)
		}
	}
	for _, c := range []string{"GLOBAL-0302", "GLOBAL-0010", "GLOBAL-0308", "REPORT-0162"} {
		if in[c] {
			t.Errorf("%s is a 5xx fault and must still count as an error", c)
		}
	}
	if engines.ClientErrorCodes == nil || len(engines.ClientErrorCodes()) != len(codes) {
		t.Fatal("engines.ClientErrorCodes is not wired to the catalog")
	}
}

func newTestCorrelationID(t *testing.T) string {
	t.Helper()
	var id string
	if err := db.DB.QueryRow(`SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatalf("gen_random_uuid: %v", err)
	}
	return id
}

func loggedMessages(t *testing.T, correlationID string) []string {
	t.Helper()
	rows, err := db.DB.Query(`SELECT error_message FROM tenant_default.system_error_logs WHERE correlation_id = $1::uuid`, correlationID)
	if err != nil {
		t.Fatalf("read system_error_logs: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

// TestLoggedCatalogErrorNamesItsRequest covers 104.3: five of the 36 could
// not be traced because the row did not say which request was refused.
func TestLoggedCatalogErrorNamesItsRequest(t *testing.T) {
	db.InitDB(testConnStr())
	correlationID := newTestCorrelationID(t)
	defer db.DB.Exec(`DELETE FROM tenant_default.system_error_logs WHERE correlation_id = $1::uuid`, correlationID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item/STAGE104-NOPE?q=secret-ish", nil)
	req.Header.Set("Resolved-Tenant-ID", "default")
	req.Header.Set("Resolved-Correlation-ID", correlationID)
	writeAPIError(httptest.NewRecorder(), req, "GLOBAL-0004", "")

	msgs := loggedMessages(t, correlationID)
	if len(msgs) != 1 {
		t.Fatalf("want one logged row, got %d: %q", len(msgs), msgs)
	}
	if !strings.HasPrefix(msgs[0], "[GLOBAL-0004] ") || !strings.HasSuffix(msgs[0], " | GET /api/v1/doc/Item/STAGE104-NOPE") {
		t.Fatalf("logged row must keep its code and name the request, got %q", msgs[0])
	}
	if strings.Contains(msgs[0], "secret-ish") {
		t.Fatalf("the query string must not be logged, got %q", msgs[0])
	}
}

// TestEmptyReportIsNotLoggedAsError covers 104.1: a report with no rows for
// its filters is a normal answer - the REPORT-0160 note stays on the
// response, and nothing is written to the error log.
func TestEmptyReportIsNotLoggedAsError(t *testing.T) {
	db.InitDB(testConnStr())
	correlationID := newTestCorrelationID(t)
	defer db.DB.Exec(`DELETE FROM tenant_default.system_error_logs WHERE correlation_id = $1::uuid`, correlationID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/stock-ledger/run?sku=STAGE104-NO-SUCH-SKU", nil)
	req.SetPathValue("id", "stock-ledger")
	req.Header.Set("Resolved-Tenant-ID", "default")
	req.Header.Set("Resolved-Role", "Super Admin")
	req.Header.Set("Resolved-User-ID", "admin")
	req.Header.Set("Resolved-Correlation-ID", correlationID)
	rec := httptest.NewRecorder()
	handleRunReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("an empty report is a normal answer, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"REPORT-0160"`) {
		t.Fatalf("the no-data note must stay on the response, got %s", rec.Body.String())
	}
	if msgs := loggedMessages(t, correlationID); len(msgs) != 0 {
		t.Fatalf("an empty report must not be logged as an error, got %q", msgs)
	}
}
