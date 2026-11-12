package server

import (
	"bufio"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Stage 49.4.6 (b): the generic list's sort / filter vocabulary is an
// allowlist. The one documented sort (recent) works, an unknown sort is
// refused, a filter is one equality on a declared field, and a filter name
// that is not a field of the doctype - an operator suffix, a column guess, a
// SQL fragment - answers 422 instead of being matched to nothing.
func TestStage4946ListSortAndFilterAllowlist(t *testing.T) {
	db.InitDB(testConnStr())
	mux := contractMux()
	userID, cleanup := seedContractUser(t, engines.RoleAdmin, "HO")
	defer cleanup()
	token := engines.SignToken(userID, userID, engines.RoleAdmin, "default", "HO", 1)
	ip := mfaTestIP()
	call := func(query string) (int, string) {
		rec := doRequestFromIP(t, mux.ServeHTTP, http.MethodGet, "/api/v1/doc/Department"+query, token, nil, ip)
		return rec.Code, rec.Body.String()
	}
	for query, want := range map[string]int{
		"?limit=1":                   http.StatusOK,
		"?sort=recent&limit=1":       http.StatusOK,
		"?code=D-4946&limit=1":       http.StatusOK, // a declared field, one equality
		"?status=Active&limit=1":     http.StatusOK, // the record's own status
		"?q=sales&q_fields=name":     http.StatusOK,
		"?sort=name":                 http.StatusUnprocessableEntity, // not a documented order
		"?sort=id%20desc":            http.StatusUnprocessableEntity,
		"?code__gt=1":                http.StatusUnprocessableEntity, // no operators
		"?nosuchfield=1":             http.StatusUnprocessableEntity, // not a field of Department
		"?data-%3E%3E%27x%27=1":      http.StatusUnprocessableEntity, // not a name at all
		"?1code=1":                   http.StatusUnprocessableEntity,
		"?code=a&code=b":             http.StatusBadRequest, // 49.4.4a: a repeated parameter is ambiguous
		"?name%3Bdrop%20table%20x=1": http.StatusUnprocessableEntity,
	} {
		code, body := call(query)
		if code != want {
			t.Errorf("GET /api/v1/doc/Department%s: %d %s, want %d", query, code, strings.TrimSpace(body), want)
		}
		if code >= 400 && !strings.Contains(body, `"error"`) {
			t.Errorf("GET /api/v1/doc/Department%s: a refusal must be the JSON envelope, got %s", query, strings.TrimSpace(body))
		}
	}
}

// Stage 49.4.6 (c): parser differentials. Each shape a proxy, Go's parser,
// PostgreSQL and a browser could read differently gets one consistent,
// documented answer - never a 5xx, always the JSON envelope. The outcome
// table is docs/security/input-validation.md; this test is what keeps it
// true.
func TestStage4946ParserDifferentialsAnswerConsistently(t *testing.T) {
	db.InitDB(testConnStr())
	mux := contractMux()
	userID, cleanup := seedContractUser(t, engines.RoleAdmin, "HO")
	defer cleanup()
	token := engines.SignToken(userID, userID, engines.RoleAdmin, "default", "HO", 1)
	suffix := contractUniqueID()
	prefix := "DIFF-4946-" + suffix
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'Customer' AND id LIKE $1`, prefix+"%")

	srv := httptest.NewServer(mux)
	defer srv.Close()

	type answer struct {
		status int
		code   string
		body   string
	}
	parse := func(status int, body string) answer {
		var env struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal([]byte(body), &env)
		return answer{status, env.Code, body}
	}
	// raw sends the request bytes exactly as written, so the server's own
	// parser - not Go's client - decides what a header combination means.
	raw := func(req string) answer {
		conn, err := net.Dial("tcp", srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := io.WriteString(conn, req); err != nil {
			t.Fatal(err)
		}
		// The request is complete as written: half-close so a server waiting
		// for bytes a Content-Length promised sees EOF instead of a hang, and
		// never wait on an answer for more than the envelope's own deadline.
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("%q: %v", req, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return parse(resp.StatusCode, string(b))
	}
	post := func(path, body string, extra ...string) answer {
		req := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: erp.test\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nConnection: close\r\n", path, token)
		for _, h := range extra {
			req += h + "\r\n"
		}
		if len(extra) == 0 {
			req += fmt.Sprintf("Content-Length: %d\r\n", len(body))
		}
		return raw(req + "\r\n" + body)
	}
	get := func(target string) answer {
		return raw(fmt.Sprintf("GET %s HTTP/1.1\r\nHost: erp.test\r\nAuthorization: Bearer %s\r\nConnection: close\r\n\r\n", target, token))
	}
	customer := func(id string, fields string) string {
		return fmt.Sprintf(`{"id":%q,"code":%q,"name":"Differential %s","status":"Active"%s}`, id, id, id, fields)
	}

	var rows []string
	record := func(name string, a answer, wantStatus int, wantCode string) {
		rows = append(rows, fmt.Sprintf("%-46s -> %d %s", name, a.status, a.code))
		if a.status >= 500 {
			t.Errorf("%s: %d %s", name, a.status, strings.TrimSpace(a.body))
		}
		if a.status >= 400 && !strings.Contains(a.body, `"error"`) {
			t.Errorf("%s: a refusal must be the JSON envelope, got %s", name, strings.TrimSpace(a.body))
		}
		if wantStatus != 0 && a.status != wantStatus {
			t.Errorf("%s: %d %s, want %d %s", name, a.status, strings.TrimSpace(a.body), wantStatus, wantCode)
		}
		if wantCode != "" && a.code != wantCode {
			t.Errorf("%s: code %s, want %s", name, a.code, wantCode)
		}
	}

	// A plain, well-formed save is the reference every differential is held to.
	plain := post("/api/v1/doc/Customer", customer(prefix+"-PLAIN", `,"credit_limit":150.5`))
	record("plain JSON save", plain, http.StatusOK, "")

	// Duplicate keys: Go's decoder would keep the last; the envelope refuses.
	record("duplicate key in one object", post("/api/v1/doc/Customer", `{"id":"`+prefix+`-DUP","code":"a","code":"b","name":"x","status":"Active"}`), http.StatusBadRequest, "GLOBAL-0307")

	// A number as a string and as a float for an amount field: both are one
	// value to the validator, and both are stored as the same number.
	asString := post("/api/v1/doc/Customer", customer(prefix+"-STR", `,"credit_limit":"150.5"`))
	record("amount as a JSON string", asString, plain.status, "")
	var storedPlain, storedString string
	db.DB.QueryRow(`SELECT data->>'credit_limit' FROM tenant_default.documents WHERE id = $1`, prefix+"-PLAIN").Scan(&storedPlain)
	db.DB.QueryRow(`SELECT data->>'credit_limit' FROM tenant_default.documents WHERE id = $1`, prefix+"-STR").Scan(&storedString)
	if asString.status == http.StatusOK && storedPlain != storedString {
		t.Errorf("an amount sent as %q is stored as %q, as a number it is stored as %q", "150.5", storedString, storedPlain)
	}
	record("amount with a unit (12abc)", post("/api/v1/doc/Customer", customer(prefix+"-UNIT", `,"credit_limit":"12abc"`)), http.StatusUnprocessableEntity, "GLOBAL-0002")

	// Leading + and leading zeros are not JSON numbers: refused as malformed,
	// not read as 5 and 5.
	record("number with a leading plus", post("/api/v1/doc/Customer", customer(prefix+"-PLUS", `,"credit_limit":+5`)), http.StatusBadRequest, "GLOBAL-0307")
	record("number with leading zeros", post("/api/v1/doc/Customer", customer(prefix+"-ZERO", `,"credit_limit":05`)), http.StatusBadRequest, "GLOBAL-0307")
	// Magnitude: beyond 2^53-1 a float64 silently rounds; the envelope refuses.
	record("number 1e308", post("/api/v1/doc/Customer", customer(prefix+"-BIG", `,"credit_limit":1e308`)), http.StatusBadRequest, "GLOBAL-0307")
	record("number 1e400 (overflows float64)", post("/api/v1/doc/Customer", customer(prefix+"-OVER", `,"credit_limit":1e400`)), http.StatusBadRequest, "GLOBAL-0307")
	record("number -1 for an amount", post("/api/v1/doc/Customer", customer(prefix+"-NEG", `,"credit_limit":-1`)), 0, "")

	// Transfer-Encoding: chunked together with a Content-Length that
	// disagrees - request smuggling's raw material. The server reads the
	// chunked body and ignores the length, so the answer is the plain save's.
	chunkedBody := customer(prefix+"-CHUNK", "")
	chunked := post("/api/v1/doc/Customer", fmt.Sprintf("%x\r\n%s\r\n0\r\n\r\n", len(chunkedBody), chunkedBody),
		"Transfer-Encoding: chunked", "Content-Length: 5")
	record("chunked body with a Content-Length", chunked, plain.status, "")
	// Content-Length longer than the body: the read cannot complete.
	record("Content-Length longer than the body", post("/api/v1/doc/Customer", `{"code":"x"}`, "Content-Length: 400"), http.StatusBadRequest, "")

	// URL-encoded and raw UTF-8 in a query: the same search, the same answer.
	encoded := get("/api/v1/doc/Customer?q=%E0%A4%95&limit=1")
	rawUnicode := get("/api/v1/doc/Customer?q=क&limit=1")
	record("query value URL-encoded", encoded, http.StatusOK, "")
	record("query value as raw UTF-8", rawUnicode, encoded.status, "")
	if encoded.status == http.StatusOK && encoded.body != rawUnicode.body {
		t.Errorf("the encoded and the raw query answer differently:\n%s\n%s", encoded.body, rawUnicode.body)
	}

	// %2F in a path segment stays one segment (an id holding a slash), and
	// a dot-dot segment is an id too - neither walks the route table.
	record("path segment with %2F", get("/api/v1/doc/Customer/"+prefix+"%2FX"), http.StatusNotFound, "GLOBAL-0004")
	record("path segment with ..%2F..", get("/api/v1/doc/Customer/..%2F..%2Fetc"), http.StatusNotFound, "GLOBAL-0004")
	record("path with a raw dot-dot segment", get("/api/v1/doc/Customer/../Customer/"+prefix+"-PLAIN"), 0, "")

	t.Logf("parser differentials (docs/security/input-validation.md):\n%s", strings.Join(rows, "\n"))
}
