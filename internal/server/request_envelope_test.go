package server

import (
	"bytes"
	"context"
	"custom_erp/db"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Stage 49.4.4 - the shared request envelope. One test per limit, each driven
// through apiMiddleware itself so the wiring is proven as well as the rule,
// plus large legitimate payloads that must still pass.

func TestValidateJSONEnvelopeLimits(t *testing.T) {
	nest := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	list := func(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("0,", n), ",") + "]" }
	cases := []struct {
		name, body string
		ok         bool
	}{
		{"ordinary document", `{"name":"x","lines":[{"sku":"A","qty":2}],"total":10.5}`, true},
		{"depth at the limit", nest(envelopeMaxJSONDepth), true},
		{"depth over the limit", nest(envelopeMaxJSONDepth + 1), false},
		{"list at the limit", list(envelopeMaxJSONArray), true},
		{"list over the limit", list(envelopeMaxJSONArray + 1), false},
		{"string at the limit", `{"a":"` + strings.Repeat("x", envelopeMaxJSONString) + `"}`, true},
		{"string over the limit", `{"a":"` + strings.Repeat("x", envelopeMaxJSONString+1) + `"}`, false},
		{"duplicate key", `{"qty":1,"qty":100}`, false},
		{"duplicate key spelled with an escape", `{"qty":1,"q\u0074y":100}`, false},
		{"duplicate key in a nested object", `{"lines":[{"sku":"A","sku":"B"}]}`, false},
		{"same key in sibling objects is fine", `{"lines":[{"sku":"A"},{"sku":"B"}]}`, true},
		{"case-distinct keys are fine (SKU-keyed maps)", `{"copies_override":{"ab1":1,"AB1":2}}`, true},
		{"two top-level values", `{"a":1}{"a":2}`, false},
		{"trailing garbage", `{"a":1} x`, false},
		{"malformed", `{"a":}`, false},
		{"truncated", `{"a":[1,2`, false},
		// 49.4.4a numeric bounds: the largest real values pass.
		{"paise, epoch ms, 13-digit barcode, negatives and decimals", `{"total_paise":50000000000,"at":1760160000000,"barcode":8901234567890,"adj":-60,"rate":0.18,"qty":1e3}`, true},
		{"number at the exact-integer limit", `{"n":9007199254740991}`, true},
		{"negative number at the limit", `{"n":-9007199254740991}`, true},
		{"number one past the limit", `{"n":9007199254740992}`, false},
		{"number that rounds onto the limit", `{"n":9007199254740993}`, false},
		{"large number in exponent form", `{"n":1e300}`, false},
		{"number beyond float64", `{"n":1e400}`, false},
		{"number literal at the length limit", `{"n":0.` + strings.Repeat("1", envelopeMaxJSONNumberLen-2) + `}`, true},
		{"number literal over the length limit", `{"n":0.` + strings.Repeat("1", envelopeMaxJSONNumberLen-1) + `}`, false},
		{"over-limit number inside a list", `{"lines":[1,2,99999999999999999999]}`, false},
	}
	for _, c := range cases {
		problem := validateJSONEnvelope([]byte(c.body), envelopeMaxJSONString)
		if c.ok && problem != "" {
			t.Errorf("%s: rejected a legitimate body: %s", c.name, problem)
		}
		if !c.ok && problem == "" {
			t.Errorf("%s: accepted a body the envelope must reject", c.name)
		}
		if strings.Contains(problem, "qty") || strings.Contains(problem, "sku") {
			t.Errorf("%s: the rejection quotes submitted content: %s", c.name, problem)
		}
	}
}

// envelopeSession logs in a disposable non-MFA user so requests reach the
// handler behind apiMiddleware's session check.
func envelopeSession(t *testing.T) string {
	t.Helper()
	db.InitDB(testConnStr())
	id := fmt.Sprintf("envelope-test-%d", time.Now().UnixNano())
	pw := randomTestPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status)
		VALUES ($1, $1, $2, $3, 'Store Manager', 'Active')`, id, string(hash), id+"@envelope.test"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id) })
	return plainSessionToken(t, id, pw, mfaTestIP())
}

type envelopeProbe struct {
	reached  bool
	deadline time.Duration
	body     []byte
}

// probeHandler stands in for a real handler: it records that it ran, the
// deadline it inherited and the body it was handed.
func (p *envelopeProbe) handler(w http.ResponseWriter, r *http.Request) {
	p.reached = true
	if dl, ok := r.Context().Deadline(); ok {
		p.deadline = time.Until(dl)
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, `{"error":"multipart"}`, http.StatusUnprocessableEntity)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, `{"error":"file"}`, http.StatusUnprocessableEntity)
			return
		}
		rows, err := csv.NewReader(file).ReadAll()
		if err != nil {
			http.Error(w, `{"error":"csv"}`, http.StatusUnprocessableEntity)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"rows": len(rows) - 1})
		return
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(r.Body)
	p.body = buf.Bytes()
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func envelopeDo(t *testing.T, token string, req *http.Request) (*httptest.ResponseRecorder, *envelopeProbe) {
	t.Helper()
	req.RemoteAddr = mfaTestIP() + ":40000"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	probe := &envelopeProbe{}
	rec := httptest.NewRecorder()
	apiMiddleware(probe.handler)(rec, req)
	return rec, probe
}

func jsonReq(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// assertEnvelopeRejection checks status, catalog code, a correlation id that
// matches the response header, and that nothing internal leaked.
func assertEnvelopeRejection(t *testing.T, name string, rec *httptest.ResponseRecorder, probe *envelopeProbe, status int, code string) {
	t.Helper()
	if probe.reached {
		t.Errorf("%s: the handler ran - the envelope must reject before any handler", name)
	}
	if rec.Code != status {
		t.Errorf("%s: status %d, want %d (body %s)", name, rec.Code, status, rec.Body.String())
	}
	var body struct {
		Code          string `json:"code"`
		CorrelationID string `json:"correlation_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != code {
		t.Errorf("%s: code %q, want %q", name, body.Code, code)
	}
	if body.CorrelationID == "" || body.CorrelationID != rec.Header().Get("X-Correlation-ID") {
		t.Errorf("%s: the rejection must carry the request's correlation id (body %q, header %q)", name, body.CorrelationID, rec.Header().Get("X-Correlation-ID"))
	}
	for _, leak := range []string{"goroutine", "panic", "pq:", "SELECT ", ".go:", `C:\`, "/opt/", "/home/"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("%s: the rejection leaks %q: %s", name, leak, rec.Body.String())
		}
	}
}

func TestRequestEnvelopeThroughAPIMiddleware(t *testing.T) {
	token := envelopeSession(t)

	t.Run("method outside the allowlist", func(t *testing.T) {
		rec, probe := envelopeDo(t, token, httptest.NewRequest("TRACE", "/api/v1/doc/Item", nil))
		assertEnvelopeRejection(t, "TRACE", rec, probe, http.StatusMethodNotAllowed, "GLOBAL-0303")
		if rec.Header().Get("Allow") == "" {
			t.Error("a 405 must say which methods are allowed")
		}
	})

	t.Run("content type outside the allowlist", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/Item", strings.NewReader(`{"a":1}`))
		req.Header.Set("Content-Type", "text/plain")
		rec, probe := envelopeDo(t, token, req)
		assertEnvelopeRejection(t, "text/plain", rec, probe, http.StatusUnsupportedMediaType, "GLOBAL-0304")
	})

	t.Run("compressed body", func(t *testing.T) {
		req := jsonReq(http.MethodPost, "/api/v1/doc/Item", `{"a":1}`)
		req.Header.Set("Content-Encoding", "gzip")
		rec, probe := envelopeDo(t, token, req)
		assertEnvelopeRejection(t, "gzip", rec, probe, http.StatusUnsupportedMediaType, "GLOBAL-0304")
	})

	t.Run("body over the cap", func(t *testing.T) {
		big := `{"a":"` + strings.Repeat("x", int(envelopeMaxBody)) + `"}`
		rec, probe := envelopeDo(t, token, jsonReq(http.MethodPost, "/api/v1/doc/Item", big))
		assertEnvelopeRejection(t, "2 MiB+ JSON", rec, probe, http.StatusRequestEntityTooLarge, "GLOBAL-0305")

		// Unknown length (chunked) is cut off by the reader, not trusted.
		chunked := jsonReq(http.MethodPost, "/api/v1/doc/Item", big)
		chunked.ContentLength = -1
		rec, probe = envelopeDo(t, token, chunked)
		assertEnvelopeRejection(t, "chunked 2 MiB+ JSON", rec, probe, http.StatusRequestEntityTooLarge, "GLOBAL-0305")
	})

	t.Run("headers over the cap", func(t *testing.T) {
		req := jsonReq(http.MethodGet, "/api/v1/doc/Item", "")
		req.Header.Set("X-Padding", strings.Repeat("p", envelopeMaxHeaderBytes))
		rec, probe := envelopeDo(t, token, req)
		assertEnvelopeRejection(t, "large header", rec, probe, http.StatusRequestHeaderFieldsTooLarge, "GLOBAL-0306")

		req = jsonReq(http.MethodGet, "/api/v1/doc/Item", "")
		for i := 0; i <= envelopeMaxHeaderCount; i++ {
			req.Header.Set(fmt.Sprintf("X-Field-%d", i), "v")
		}
		rec, probe = envelopeDo(t, token, req)
		assertEnvelopeRejection(t, "many headers", rec, probe, http.StatusRequestHeaderFieldsTooLarge, "GLOBAL-0306")
	})

	t.Run("JSON depth, list length, string length and duplicate keys", func(t *testing.T) {
		bodies := map[string]string{
			"depth":     strings.Repeat(`{"a":`, envelopeMaxJSONDepth+1) + "1" + strings.Repeat("}", envelopeMaxJSONDepth+1),
			"list":      `{"ids":[` + strings.TrimSuffix(strings.Repeat("1,", envelopeMaxJSONArray+1), ",") + `]}`,
			"string":    `{"note":"` + strings.Repeat("x", envelopeMaxJSONString+1) + `"}`,
			"duplicate": `{"qty":1,"qty":100}`,
			"number":    `{"qty":9007199254740993}`,
			"numberlen": `{"qty":0.` + strings.Repeat("5", envelopeMaxJSONNumberLen) + `}`,
		}
		for name, body := range bodies {
			rec, probe := envelopeDo(t, token, jsonReq(http.MethodPost, "/api/v1/doc/Item", body))
			assertEnvelopeRejection(t, name, rec, probe, http.StatusBadRequest, "GLOBAL-0307")
		}
	})

	t.Run("repeated query parameter", func(t *testing.T) {
		for name, path := range map[string]string{
			"tenant_id twice":         "/api/v1/doc/Item?tenant_id=default&tenant_id=other",
			"encoded second spelling": "/api/v1/doc/Item?page=1&p%61ge=2",
		} {
			rec, probe := envelopeDo(t, token, httptest.NewRequest(http.MethodGet, path, nil))
			assertEnvelopeRejection(t, name, rec, probe, http.StatusBadRequest, "GLOBAL-0307")
			if strings.Contains(rec.Body.String(), "other") {
				t.Errorf("%s: the rejection echoes a submitted value: %s", name, rec.Body.String())
			}
		}
		// Distinct names still pass.
		rec, probe := envelopeDo(t, token, httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item?page=1&search=a", nil))
		if !probe.reached || rec.Code == http.StatusBadRequest {
			t.Errorf("distinct query parameters must reach the handler, got %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("no Content-Type is read as JSON and still bounded", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/Item", strings.NewReader(`{"name":"no type"}`))
		rec, probe := envelopeDo(t, token, req)
		if !probe.reached || rec.Code != http.StatusOK {
			t.Errorf("a JSON body without a Content-Type must pass, got %d %s", rec.Code, rec.Body.String())
		}
		req = httptest.NewRequest(http.MethodPost, "/api/v1/doc/Item", strings.NewReader(`{"qty":1,"qty":2}`))
		rec, probe = envelopeDo(t, token, req)
		assertEnvelopeRejection(t, "untyped duplicate key", rec, probe, http.StatusBadRequest, "GLOBAL-0307")
	})

	t.Run("deadline reaches the handler and the body survives validation", func(t *testing.T) {
		body := `{"name":"deadline probe","qty":3}`
		rec, probe := envelopeDo(t, token, jsonReq(http.MethodPost, "/api/v1/doc/Item", body))
		if !probe.reached || rec.Code != http.StatusOK {
			t.Fatalf("a legitimate request must reach the handler, got %d %s", rec.Code, rec.Body.String())
		}
		if probe.deadline <= 0 || probe.deadline > envelopeDeadline {
			t.Errorf("handler deadline %s, want within (0, %s]", probe.deadline, envelopeDeadline)
		}
		if string(probe.body) != body {
			t.Errorf("the handler must receive the exact bytes sent (webhook signatures depend on it), got %q", probe.body)
		}
	})
}

// TestRequestDeadlineIsNotReportedAsAbandoned: found live on 2026-10-09 - a
// 20,000-row import preview ran past the envelope deadline and came back as an
// empty 499 "client went away", though the user was still waiting.
func TestRequestDeadlineIsNotReportedAsAbandoned(t *testing.T) {
	db.InitDB(testConnStr())
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pim/import/Item/preview", nil).WithContext(expired)
	req.Header.Set("Resolved-Correlation-ID", "corr-deadline")
	rec := httptest.NewRecorder()
	writeEngineError(rec, req, context.DeadlineExceeded, http.StatusUnprocessableEntity)
	if rec.Code != http.StatusGatewayTimeout || !strings.Contains(rec.Body.String(), "GLOBAL-0308") {
		t.Errorf("an expired request deadline must answer 504 GLOBAL-0308, got %d %s", rec.Code, rec.Body.String())
	}

	gone, cancelGone := context.WithCancel(context.Background())
	cancelGone()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/pim/import/Item/preview", nil).WithContext(gone)
	rec = httptest.NewRecorder()
	writeEngineError(rec, req, context.Canceled, http.StatusUnprocessableEntity)
	if rec.Code != clientClosedRequestStatus {
		t.Errorf("a client that left must still be treated as abandoned (499), got %d", rec.Code)
	}
}

// TestRequestEnvelopePassesLargeLegitimatePayloads is the guard against the
// envelope breaking a real screen: each payload is the largest of its kind
// the product actually produces.
func TestRequestEnvelopePassesLargeLegitimatePayloads(t *testing.T) {
	token := envelopeSession(t)

	t.Run("20,000-row CSV bulk import (the platform.max_import_rows default)", func(t *testing.T) {
		var csvBuf bytes.Buffer
		w := csv.NewWriter(&csvBuf)
		_ = w.Write([]string{"item_code", "name", "description", "category", "brand", "hsn_code", "uom", "mrp", "selling_price", "barcode"})
		for i := 0; i < 20000; i++ {
			_ = w.Write([]string{fmt.Sprintf("ENV-ITEM-%05d", i), fmt.Sprintf("Cotton Shirt Slim Fit %d", i),
				"Regular wash, 100% cotton, mid-weight fabric", "Apparel/Shirts", "House Brand", "62052000",
				"PCS", "1499.00", "1199.00", fmt.Sprintf("8901234%06d", i)})
		}
		w.Flush()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, _ := mw.CreateFormFile("file", "items.csv")
		_, _ = part.Write(csvBuf.Bytes())
		_ = mw.Close()
		if body.Len() <= int(envelopeMaxBody) {
			t.Fatalf("fixture is only %d bytes; it must exceed the 2 MiB default to prove the import route's allowance", body.Len())
		}

		send := func(path string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body.Bytes()))
			req.Header.Set("Content-Type", mw.FormDataContentType())
			rec, _ := envelopeDo(t, token, req)
			return rec
		}
		rec := send("/api/v1/import/Item")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"rows":20000`) {
			t.Fatalf("a %d-byte, 20,000-row import must pass the envelope intact, got %d %s", body.Len(), rec.Code, rec.Body.String())
		}
		// The same body on an ordinary route is refused - the import route's
		// own allowance is what lets it through, not a loosened default.
		if rec := send("/api/v1/doc/Item"); rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("the same body on a default route must hit the 2 MiB cap, got %d", rec.Code)
		}
	})

	t.Run("Sticker Studio design with three maximum-size logos", func(t *testing.T) {
		// 400 KB is sticker-studio.js's per-logo cap; base64 makes it ~546 KB
		// of data URL, and templateToDoc puts every logo in the one design string.
		logo := "data:image/png;base64," + strings.Repeat("A", 400*1024*4/3)
		design, _ := json.Marshal(map[string]interface{}{"v": 2, "settings": map[string]int{"dpi": 203},
			"assets": map[string]string{"a1": logo, "a2": logo, "a3": logo}})
		doc, _ := json.Marshal(map[string]interface{}{"code": "ENV-STK", "name": "Envelope", "label_width_mm": 50,
			"label_height_mm": 25, "elements": "[]", "design": string(design)})
		rec, probe := envelopeDo(t, token, jsonReq(http.MethodPost, "/api/v1/doc/StickerTemplate", string(doc)))
		if !probe.reached || rec.Code != http.StatusOK {
			t.Fatalf("a %d-byte three-logo sticker design must pass, got %d %s", len(doc), rec.Code, rec.Body.String())
		}
		if rec, _ := envelopeDo(t, token, jsonReq(http.MethodPost, "/api/v1/doc/Item", string(doc))); rec.Code != http.StatusBadRequest {
			t.Errorf("a %d-char string on an ordinary route must hit the 1 MiB string bound, got %d", len(design), rec.Code)
		}
	})

	t.Run("purchase order with 2,000 lines", func(t *testing.T) {
		lines := make([]map[string]interface{}, 2000)
		for i := range lines {
			lines[i] = map[string]interface{}{"item_code": fmt.Sprintf("ENV-ITEM-%05d", i), "qty": 12, "rate": 1199.5,
				"uom": "PCS", "tax_rate": 12, "hsn_code": "62052000", "description": "Cotton Shirt Slim Fit"}
		}
		doc, _ := json.Marshal(map[string]interface{}{"vendor": "V-ENV", "location_code": "WH-1", "items": lines})
		rec, probe := envelopeDo(t, token, jsonReq(http.MethodPost, "/api/v1/doc/PurchaseOrder", string(doc)))
		if !probe.reached || rec.Code != http.StatusOK {
			t.Fatalf("a %d-byte 2,000-line PO must pass, got %d %s", len(doc), rec.Code, rec.Body.String())
		}
	})
}

// TestRepeatedQueryParam covers the 49.4.4a duplicate-query model: any name
// given twice is ambiguous unless the route declares it as a list.
func TestRepeatedQueryParam(t *testing.T) {
	cases := []struct {
		query      string
		repeatable []string
		want       bool
	}{
		{"", nil, false},
		{"page=1&search=a&limit=50", nil, false},
		{"tenant_id=a&tenant_id=b", nil, true},
		{"a=&a=", nil, true},
		{"a&a", nil, true},
		{"page=1&p%61ge=2", nil, true}, // same name once decoded
		{"a=1&a=%zz", nil, true},       // an undecodable value still counts
		{"Page=1&page=2", nil, false},  // case-sensitive, like JSON keys
		{"a=1&&b=2&", nil, false},      // empty pairs are not names
		{"status=a&status=b", []string{"status"}, false},
		{"status=a&status=b&page=1&page=2", []string{"status"}, true},
	}
	for _, c := range cases {
		if got := repeatedQueryParam(c.query, c.repeatable); got != c.want {
			t.Errorf("repeatedQueryParam(%q, %v) = %v, want %v", c.query, c.repeatable, got, c.want)
		}
	}
}
