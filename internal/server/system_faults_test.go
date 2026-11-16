package server

import (
	"encoding/json"
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

// TestSessionRefusalIsNotLoggedAsFault covers 206.1: an expired session wrote
// one "Session expired" row per call the page made (eight on minn). A 401 is
// the sign-in module's answer, not a fault; a 5xx is still written.
func TestSessionRefusalIsNotLoggedAsFault(t *testing.T) {
	db.InitDB(testConnStr())
	correlationID := newTestCorrelationID(t)
	defer db.DB.Exec(`DELETE FROM tenant_default.system_error_logs WHERE correlation_id = $1::uuid`, correlationID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/permissions", nil)
	req.Header.Set("Resolved-Tenant-ID", "default")
	req.Header.Set("Resolved-Correlation-ID", correlationID)
	rec := httptest.NewRecorder()
	writeAPIError(rec, req, "GLOBAL-0009", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GLOBAL-0009 must still answer 401, got %d", rec.Code)
	}
	writeAPIErrorGeneric(httptest.NewRecorder(), req, http.StatusUnauthorized, "Current password is incorrect")
	if msgs := loggedMessages(t, correlationID); len(msgs) != 0 {
		t.Fatalf("a 401 must not be written to System Errors, got %q", msgs)
	}

	writeAPIErrorGeneric(httptest.NewRecorder(), req, http.StatusInternalServerError, "boom")
	if msgs := loggedMessages(t, correlationID); len(msgs) != 1 || !strings.HasPrefix(msgs[0], "[GLOBAL-0302] ") {
		t.Fatalf("a 500 must still be written as a fault, got %q", msgs)
	}
}

type listedFault struct {
	LogID        string `json:"log_id"`
	ModuleSource string `json:"module_source"`
	ErrorMessage string `json:"error_message"`
	Count        int    `json:"count"`
	FirstSeen    string `json:"first_seen"`
	CreatedAt    string `json:"created_at"`
}

func listOpenFaults(t *testing.T, module string) []listedFault {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs/system", nil)
	req.Header.Set("Resolved-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	handleSystemLogs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list system errors: %d %s", rec.Code, rec.Body.String())
	}
	var all []listedFault
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var out []listedFault
	for _, f := range all {
		if f.ModuleSource == module {
			out = append(out, f)
		}
	}
	return out
}

func resolveFault(t *testing.T, logID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/logs/system/resolve", strings.NewReader(`{"log_id":"`+logID+`"}`))
	req.Header.Set("Resolved-Tenant-ID", "default")
	req.Header.Set("Resolved-User-ID", "admin")
	req.Header.Set("Resolved-Username", "admin")
	rec := httptest.NewRecorder()
	handleResolveSystemFault(rec, req)
	return rec
}

// TestSystemErrorsIsAFixList covers 206.2 and 206.3: System Errors lists open
// faults only, one row per fault with its count; Resolve closes every open
// occurrence, and the fault comes back by itself if it happens again.
func TestSystemErrorsIsAFixList(t *testing.T) {
	db.InitDB(testConnStr())
	module := "TEST206-" + newTestCorrelationID(t)[:8]
	defer db.DB.Exec(`DELETE FROM tenant_default.system_error_logs WHERE module_source = $1`, module)
	seed := func(msg, age string) {
		t.Helper()
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.system_error_logs (severity, module_source, error_message, stack_trace, created_at)
			VALUES ('Critical', $1, $2, 'trace', NOW() - $3::interval)`, module, msg, age); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	fault := "[GLOBAL-0302] boom | GET /api/v1/test206"
	seed(fault, "2 minutes")
	seed(fault, "0 seconds")
	seed("[GLOBAL-0009] Session expired | GET /api/v1/test206", "0 seconds")

	got := listOpenFaults(t, module)
	if len(got) != 1 || got[0].ErrorMessage != fault || got[0].Count != 2 {
		t.Fatalf("want the fault once with count 2 and no refusal, got %+v", got)
	}
	if got[0].FirstSeen >= got[0].CreatedAt {
		t.Fatalf("first seen %s must be before last seen %s", got[0].FirstSeen, got[0].CreatedAt)
	}

	if rec := resolveFault(t, got[0].LogID); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"resolved":2`) {
		t.Fatalf("resolve: %d %s", rec.Code, rec.Body.String())
	}
	if got := listOpenFaults(t, module); len(got) != 0 {
		t.Fatalf("a resolved fault must leave the list, got %+v", got)
	}

	seed(fault, "0 seconds")
	if got := listOpenFaults(t, module); len(got) != 1 || got[0].Count != 1 {
		t.Fatalf("a new occurrence must open the fault again with count 1, got %+v", got)
	}

	if rec := resolveFault(t, "00000000-0000-0000-0000-000000000000"); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown log id must be 404, got %d", rec.Code)
	}
	if rec := resolveFault(t, "not-a-uuid"); rec.Code != http.StatusNotFound {
		t.Fatalf("a malformed log id must be 404, got %d", rec.Code)
	}
	if rec := resolveFault(t, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("a missing log id must be 400, got %d", rec.Code)
	}
}

// TestResolveSystemErrorIsSuperAdminOnly: Resolve is the owner's fix list
// (admin.patch); an Admin or an Auditor (who can read the list) cannot.
func TestResolveSystemErrorIsSuperAdminOnly(t *testing.T) {
	const pattern = "POST /api/v1/logs/system/resolve"
	for role, want := range map[string]bool{engines.RoleSuperAdmin: true, "Admin": false, "Auditor": false} {
		ok, _, found := checkRouteCapability(pattern, role)
		if !found {
			t.Fatalf("%s has no capability entry", pattern)
		}
		if ok != want {
			t.Errorf("%s as %s: allowed=%v, want %v", pattern, role, ok, want)
		}
	}
}
