package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"testing"
	"time"
)

// Stage 49.10.4 - authenticated DAST as a test.
//
// Every /api route in the route table is driven through the real route table
// and middleware (registerRoutes) as each privilege tier - no token, Cashier,
// Admin, Super Admin - with the payloads a scanner sends by default: malformed
// JSON, an empty body, numeric extremes in every numeric-looking field, SQL
// injection and XSS strings in every path and query parameter, dot-dot path
// segments, and a 2 MB string. The contract is the same for every route: never
// a 5xx, never a panic trace, a database error or a file path in the body, and
// every refusal is the JSON error envelope. A 5xx or a leak it finds is a bug
// in that handler (49.8.8 found a server-killing panic this way).
//
// Bounded on purpose: the payload set is fixed, the 2 MB body goes to a sample
// of POST routes as Admin only, and -short skips the whole sweep.

var dastLeakPattern = regexp.MustCompile(`(?i)goroutine \d+|runtime error|panic:|\bpq: |\.go:\d+|[A-Z]:\\[A-Za-z]|/home/|/usr/local|/var/lib|sql: |syntax error at or near|pg_catalog|stack trace`)

// dastSkipRoutes are the commands a sweep must not fire against a shared
// test database: they re-shape the client or the platform (packages, module
// and feature switches, users and roles, patches, extension hooks, sandbox
// resets, export freezes, archive restores) and so would change what the
// rest of the suite runs against. Each is covered by its own test.
var dastSkipRoutes = regexp.MustCompile(`/admin/(patch|extension|sandbox-tenants|security/freeze-exports|audit-logs/archive$|audit-logs/archives/[^/]+/restore|tenant/package|modules|features|users|roles|role-permissions|field-permissions|settings|retention|provision|tenants|control-panel|platform|data-console|privacy)|/debug/panic|/ops/|/backup|/restore|/shutdown|/drain|/migrate|/approval/rules|/auth/|/login|/logout|/mfa`)

func TestStage49104AuthenticatedDAST(t *testing.T) {
	if testing.Short() {
		t.Skip("the DAST sweep sends several thousand requests; skipped under -short")
	}
	db.InitDB(testConnStr())
	routes, err := RouteAccessTable("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	registerRoutes()
	mux := http.DefaultServeMux
	t.Cleanup(func() { http.DefaultServeMux = previous })

	// One Active user per tier, in the default client.
	suffix := contractUniqueID()
	tiers := []struct{ name, token string }{{"no token", ""}}
	for _, role := range []string{engines.RoleCashier, engines.RoleAdmin, engines.RoleSuperAdmin} {
		id := "dast-" + strings.ToLower(strings.ReplaceAll(role, " ", "-")) + "-" + suffix
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code) VALUES ($1, $1, 'x', $2, $3, 'Active', 'HO')`, id, id+"@dast.invalid", role); err != nil {
			t.Fatalf("seed %s: %v", role, err)
		}
		t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id) })
		tiers = append(tiers, struct{ name, token string }{role, engines.SignToken(id, id, role, "default", "HO", 1)})
	}
	engines.ResetLiveUserStateCache()

	const sqli = `' OR '1'='1' --`
	const xss = `<script>alert(1)</script>`
	param := regexp.MustCompile(`\{[^}]*\}`)
	fill := func(pattern, value string) string {
		return param.ReplaceAllStringFunc(pattern, func(m string) string {
			if strings.HasSuffix(m, "...}") {
				return url.PathEscape(value) + "/" + url.PathEscape(value)
			}
			return url.PathEscape(value)
		})
	}
	hostileQuery := "?" + url.Values{
		"q": {xss}, "id": {sqli}, "limit": {"-1"}, "offset": {"1e308"}, "from": {sqli}, "to": {"../../etc/passwd"},
		"status": {`<svg/onload=1>`}, "location": {`');--`}, "ids": {"9223372036854775808"}, "date": {"0000-00-00"},
		"doctype": {"../x"}, "sort": {";drop table x"}, "page": {"99999999999999999999"}, "days": {"-1"}, "code": {"‮"},
	}.Encode()
	extremes := map[string]interface{}{
		"amount": 1e308, "qty": -1, "quantity": 9007199254740993.0, "limit": -1, "offset": 1e308, "rate": -1, "price": 1e308,
		"total": -1, "id": sqli, "version": -1, "expected_version": 1e308, "page": -1, "days": 1e308, "count": -1,
		"items":    []interface{}{map[string]interface{}{"sku": xss, "qty": -1, "price": 1e308}},
		"lines":    []interface{}{map[string]interface{}{"qty": 1e308, "amount": -1}},
		"location": "../x", "code": "‮", "status": xss, "doctype": "../../x", "document_id": "%00",
		"name": xss, "email": sqli, "from_date": "9999-99-99", "to_date": sqli, "percent": 1e308, "discount": -1,
	}
	extremesBody, _ := json.Marshal(extremes)
	huge := []byte(`{"name":"` + strings.Repeat("A", 2<<20+100) + `"}`)

	type payload struct {
		name string
		path string
		body []byte
	}
	payloadsFor := func(rt RouteAccess, method string, tier string, i int) []payload {
		if method == http.MethodGet || method == http.MethodHead {
			return []payload{
				{"sqli/xss in path and query", fill(rt.Path, sqli) + hostileQuery, nil},
				{"dot-dot path and extreme paging", fill(rt.Path, "../../etc/passwd") + "?limit=99999999999999999999&offset=1e308&q=%00&sort=%3Bdrop", nil},
			}
		}
		out := []payload{
			{"malformed JSON", fill(rt.Path, "x1"), []byte(`{"a":`)},
			{"empty body", fill(rt.Path, "x1"), nil},
			{"numeric extremes and sqli/xss", fill(rt.Path, sqli) + hostileQuery, extremesBody},
		}
		if tier == engines.RoleAdmin && i%10 == 0 {
			out = append(out, payload{"2 MB string", fill(rt.Path, "x1"), huge})
		}
		return out
	}

	var ipCounter int
	nextIP := func() string {
		ipCounter++
		return fmt.Sprintf("10.%d.%d.%d", (ipCounter>>16)&255, (ipCounter>>8)&255, ipCounter&255)
	}
	type finding struct{ kind, where string }
	var findings []finding
	seen := map[string]bool{}
	note := func(kind, route, tier, payloadName string, code int, body string) {
		key := kind + "|" + route
		if seen[key] {
			return
		}
		seen[key] = true
		body = strings.TrimSpace(body)
		if len(body) > 160 {
			body = body[:160] + "..."
		}
		findings = append(findings, finding{kind, fmt.Sprintf("%s as %s (%s): %d %s", route, tier, payloadName, code, body)})
	}
	serve := func(method, target string, body []byte, token string) (code int, respBody string, contentType string, panicked string) {
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		req.RemoteAddr = nextIP() + ":4444"
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		func() {
			defer func() {
				if p := recover(); p != nil {
					panicked = fmt.Sprintf("%v\n%s", p, debug.Stack())
				}
			}()
			mux.ServeHTTP(rec, req)
		}()
		return rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"), panicked
	}

	start := time.Now()
	requests, skipped := 0, 0
	for i, rt := range routes {
		if dastSkipRoutes.MatchString(rt.Path) {
			skipped++
			continue
		}
		method := rt.Method
		if method == "ANY" {
			method = http.MethodGet
		}
		for _, tier := range tiers {
			for _, p := range payloadsFor(rt, method, tier.name, i) {
				requests++
				began := time.Now()
				code, body, ctype, panicked := serve(method, p.path, p.body, tier.token)
				if took := time.Since(began); took > 10*time.Second {
					t.Logf("slow: %s as %s (%s) took %s", rt.Pattern, tier.name, p.name, took.Round(time.Millisecond))
				}
				switch {
				case panicked != "":
					note("panic escaped the middleware", rt.Pattern, tier.name, p.name, code, panicked)
				case code >= 500:
					note("5xx", rt.Pattern, tier.name, p.name, code, body)
				case dastLeakPattern.MatchString(body):
					note("leak in body", rt.Pattern, tier.name, p.name, code, body)
				case code >= 400 && !strings.Contains(body, `"error"`) && !strings.Contains(body, `"code"`):
					note("refusal is not the JSON envelope", rt.Pattern, tier.name, p.name, code, body+" ["+ctype+"]")
				}
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].kind+findings[i].where < findings[j].kind+findings[j].where })
	for _, f := range findings {
		t.Errorf("%s: %s", f.kind, f.where)
	}
	if requests < 3000 {
		t.Fatalf("only %d requests were sent - the route reading has broken", requests)
	}
	t.Logf("DAST: %d routes (%d skipped by policy), %d tiers, %d requests in %s; findings: %d", len(routes)-skipped, skipped, len(tiers), requests, time.Since(start).Round(time.Second), len(findings))
}

// The choke point the sweep's findings were fixed at (writeAPIErrorGeneric):
// driver text is never shown, a missing row is 404, a Postgres data exception
// is 422, and any other internal failure stays a 500.
func TestStage49104DriverTextNeverReachesTheClient(t *testing.T) {
	for message, want := range map[string]int{
		`pq: invalid input syntax for type uuid: "x1" (22P02)`:        http.StatusUnprocessableEntity,
		`pq: invalid byte sequence for encoding "UTF8": 0x00 (22021)`: http.StatusUnprocessableEntity,
		`pq: OFFSET must not be negative (2201X)`:                     http.StatusUnprocessableEntity,
		`item not found: sql: no rows in result set`:                  http.StatusNotFound,
		`pq: connection reset`:                                        http.StatusInternalServerError,
		`pq: duplicate key value violates unique constraint (23505)`:  http.StatusInternalServerError,
	} {
		rec := httptest.NewRecorder()
		writeAPIErrorGeneric(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil), http.StatusInternalServerError, message)
		if rec.Code != want {
			t.Errorf("%q: %d, want %d", message, rec.Code, want)
		}
		if body := rec.Body.String(); dastLeakPattern.MatchString(body) || !strings.Contains(body, `"code"`) {
			t.Errorf("%q: the body leaks or is not the envelope: %s", message, body)
		}
	}
	// A 4xx keeps its own curated message, and loses only driver text.
	rec := httptest.NewRecorder()
	writeAPIErrorGeneric(rec, httptest.NewRequest(http.MethodPost, "/api/v1/x", nil), http.StatusUnprocessableEntity, "Field 'report_id' is required")
	if !strings.Contains(rec.Body.String(), "Field 'report_id' is required") {
		t.Errorf("a curated 4xx message was replaced: %s", rec.Body.String())
	}
}
