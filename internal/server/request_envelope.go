package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Stage 49.4.4 - the shared request envelope.
//
// Every API request passes through here once, from apiMiddleware (and the
// public API's publicAPIMiddleware, the other caller boundary), before any
// handler runs. Before this, the only shared bound was a 2MB body cap, and
// each handler decided for itself what else it would tolerate. So the bounds
// below live in one place instead of in every handler:
//
//   - methods and content types are allowlisted;
//   - body, headers, JSON depth, array length, string length and number
//     size are bounded;
//   - a JSON object that repeats a key is rejected;
//   - the request carries a deadline that handlers inherit via r.Context().
//
// Every rejection goes through writeAPIErrorDetail, so it carries the request's
// correlation id and a fixed, safe detail line - never a stack, SQL, a path or
// any echo of the submitted content.
//
// The limits were set from measured payloads (2026-10-09), not guessed:
//
//   - Stored documents in every local database: depth <= 3, arrays <= 7
//     items, strings <= 654 chars. JSON depth 32 and arrays of 50,000 items
//     leave wide room. 50,000 is above platform.max_import_rows (20,000), so
//     nothing a user can legitimately import is cut off.
//   - Sticker Studio: a logo is capped at 400 KB in the browser, which is
//     about 546,000 chars as a data URL, and templateToDoc stringifies every
//     logo into the one `design` field. 1 MiB per string covers one logo
//     with room to spare. StickerTemplate saves get the full 2 MiB body,
//     because two or three logos in one design are a real label.
//   - Bulk CSV import: 20,000 rows of a wide Item file run to several MB as
//     multipart, so the import routes allow 16 MiB. Before this, the 2 MiB
//     cap here silently capped every upload route below what its own
//     handler declared: a nested MaxBytesReader cannot raise an outer one.
//   - Headers: a session sends a JWT of a few hundred bytes. 32 KiB total
//     and 100 fields are far above any real browser or integration.
//   - Deadline: 55s, inside the server's 60s WriteTimeout, so a handler
//     that watches its context stops before the connection is cut.
//   - Numbers (49.4.4a, measured 2026-10-11): the 29,571 JSON numbers stored
//     in both local databases peak at 5,000,000, with literals of at most 18
//     characters. Request bodies legitimately carry more: paise amounts,
//     epoch milliseconds (~1.7e12), a 13-digit barcode (~9e12). Handlers
//     decode numbers into float64, and the browser into Number, so above
//     2^53-1 an integer is silently changed before anything stores it. That
//     is the magnitude bound, about 700x the largest of those. The one inbound
//     int64 decoder, the BigCommerce webhook's order id, carries a store's
//     small sequential order number. Shopify's id is read as a string. Literals are
//     capped at 64 characters, so a megabyte of "0.000...1" (tiny, but huge)
//     is refused too.

const (
	envelopeMaxBody          int64 = 2 << 20
	envelopeMaxHeaderBytes         = 32 << 10
	envelopeMaxHeaderCount         = 100
	envelopeMaxJSONDepth           = 32
	envelopeMaxJSONArray           = 50000
	envelopeMaxJSONString          = 1 << 20
	envelopeMaxJSONNumberLen       = 64
	envelopeMaxJSONNumber          = 1<<53 - 1
	envelopeDeadline               = 55 * time.Second
)

// envelopeMethods is every method a route in this server is registered for.
// OPTIONS is answered by the middleware before the envelope runs.
var envelopeMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true,
	http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
}

// envelopeDefaultTypes are the request bodies the browser client and every
// integration send: apiFetch always sends JSON, apiUpload sends multipart.
var envelopeDefaultTypes = []string{"application/json", "multipart/form-data"}

// envelopeRoute widens the defaults for one path pattern. "*" matches exactly
// one path segment.
type envelopeRoute struct {
	pattern    string
	maxBody    int64
	maxString  int
	extraTypes []string
	// repeatableParams are query parameters this route reads as a list
	// (?status=a&status=b). Every other name may appear once (49.4.4a).
	repeatableParams []string
}

var envelopeRoutes = []envelopeRoute{
	// Multipart uploads; each handler's own ParseMultipartForm/MaxBytesReader
	// is now reachable instead of being capped at 2 MiB underneath.
	{pattern: "/api/v1/pim/media/upload", maxBody: 10 << 20},
	{pattern: "/api/v1/pim/media/bulk-upload", maxBody: 100 << 20},
	// Stage 55.15b: one 5 MB document attachment plus its multipart framing.
	{pattern: "/api/v1/attachments/*/*", maxBody: 6 << 20},
	{pattern: "/api/v1/import/*", maxBody: 16 << 20},
	{pattern: "/api/v1/pim/import/*/preview", maxBody: 16 << 20},
	{pattern: "/api/v1/pim/import-templates/*/preview", maxBody: 16 << 20},
	{pattern: "/api/v1/pim/import-templates/*/import", maxBody: 16 << 20},
	// Stage 134.10: a brand logo of at most 300 KB plus its framing.
	{pattern: "/api/v1/admin/branding/logo", maxBody: 1 << 20},
	// Stage 128: a Setup Workbook of at most 8 MB plus its multipart framing.
	{pattern: "/api/v1/admin/setup-workbook/preview", maxBody: 9 << 20},
	{pattern: "/api/v1/admin/setup-workbook/apply", maxBody: 9 << 20},
	// Stage 129: a Bulk Purchase Upload workbook, the same size.
	{pattern: "/api/v1/procurement/bulk-upload/preview", maxBody: 9 << 20},
	{pattern: "/api/v1/procurement/bulk-upload/apply", maxBody: 9 << 20},
	// The import hook takes a raw CSV body from a scheduled external sender,
	// limited to 5 MiB by the handler itself.
	{pattern: "/api/v1/pim/import/hook", maxBody: 5 << 20, extraTypes: []string{"text/csv", "text/plain", "application/octet-stream"}},
	// Browsers send CSP violation reports with their own media types.
	{pattern: "/api/v1/security/csp-report", extraTypes: []string{"application/csp-report", "application/reports+json"}},
	// Sticker designs carry their logos inside one string field.
	{pattern: "/api/v1/doc/StickerTemplate", maxString: int(envelopeMaxBody)},
	{pattern: "/api/v1/doc/StickerTemplate/*", maxString: int(envelopeMaxBody)},
}

type envelopePolicy struct {
	maxBody          int64
	maxString        int
	types            []string
	repeatableParams []string
}

func envelopePolicyFor(path string) envelopePolicy {
	p := envelopePolicy{maxBody: envelopeMaxBody, maxString: envelopeMaxJSONString, types: envelopeDefaultTypes}
	for _, rt := range envelopeRoutes {
		if !envelopePathMatch(rt.pattern, path) {
			continue
		}
		if rt.maxBody > 0 {
			p.maxBody = rt.maxBody
		}
		if rt.maxString > 0 {
			p.maxString = rt.maxString
		}
		if len(rt.extraTypes) > 0 {
			p.types = append(append([]string{}, envelopeDefaultTypes...), rt.extraTypes...)
		}
		p.repeatableParams = rt.repeatableParams
		break
	}
	return p
}

func envelopePathMatch(pattern, path string) bool {
	ps, xs := strings.Split(pattern, "/"), strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(ps) != len(xs) {
		return false
	}
	for i := range ps {
		if ps[i] != "*" && ps[i] != xs[i] {
			return false
		}
		if ps[i] == "*" && xs[i] == "" {
			return false
		}
	}
	return true
}

// enforceRequestEnvelope applies the envelope. It returns the request to hand
// on (body replaced after JSON validation, deadline attached) plus the cancel
// for that deadline, or ok=false once it has written the rejection.
func enforceRequestEnvelope(w http.ResponseWriter, r *http.Request) (_ *http.Request, cancel context.CancelFunc, ok bool) {
	if !envelopeMethods[r.Method] {
		w.Header().Set("Allow", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
		writeAPIErrorDetail(w, r, "GLOBAL-0303", "", "This address does not accept "+safeMethodName(r.Method)+" requests.")
		return r, nil, false
	}

	headerBytes, headerCount := 0, 0
	for name, values := range r.Header {
		for _, v := range values {
			headerCount++
			headerBytes += len(name) + len(v) + 4 // ": " and CRLF
		}
	}
	if headerCount > envelopeMaxHeaderCount || headerBytes > envelopeMaxHeaderBytes {
		writeAPIErrorDetail(w, r, "GLOBAL-0306", "", fmt.Sprintf("Requests may carry at most %d header fields and %d KB of headers.", envelopeMaxHeaderCount, envelopeMaxHeaderBytes>>10))
		return r, nil, false
	}

	// Matched on the escaped path, as the router matches: a record id holding
	// "/" (EXP/HO/26-27/00005) arrives as %2F and stays one path segment.
	policy := envelopePolicyFor(r.URL.EscapedPath())
	if repeatedQueryParam(r.URL.RawQuery, policy.repeatableParams) {
		writeAPIErrorDetail(w, r, "GLOBAL-0307", "", "A query parameter is repeated, so it is ambiguous which value applies.")
		return r, nil, false
	}
	if r.ContentLength > policy.maxBody {
		writeAPIErrorDetail(w, r, "GLOBAL-0305", "", envelopeBodyLimitDetail(policy.maxBody))
		return r, nil, false
	}
	hasBody := r.ContentLength > 0 || (r.ContentLength < 0 && r.Body != nil && r.Body != http.NoBody)
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, policy.maxBody)
	}
	if hasBody {
		if enc := strings.TrimSpace(r.Header.Get("Content-Encoding")); enc != "" && !strings.EqualFold(enc, "identity") {
			// Nothing here decompresses a request body, so a compressed one
			// is refused outright rather than handed on as opaque bytes.
			writeAPIErrorDetail(w, r, "GLOBAL-0304", "", "Compressed request bodies are not accepted; send the body uncompressed.")
			return r, nil, false
		}
		// No Content-Type at all is read as JSON and held to every JSON bound
		// below. The type allowlist exists because a cross-site form or a
		// no-cors fetch can send text/plain or form encodings without a CORS
		// preflight; neither can send a body with no type, while scripts and
		// integrations often omit it.
		contentType := r.Header.Get("Content-Type")
		if strings.TrimSpace(contentType) == "" {
			contentType = "application/json"
		}
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || !envelopeTypeAllowed(mediaType, policy.types) {
			writeAPIErrorDetail(w, r, "GLOBAL-0304", "", "This address accepts "+strings.Join(policy.types, ", ")+" request bodies.")
			return r, nil, false
		}
		if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") || mediaType == "application/csp-report" {
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					writeAPIErrorDetail(w, r, "GLOBAL-0305", "", envelopeBodyLimitDetail(policy.maxBody))
				} else {
					writeAPIErrorDetail(w, r, "GLOBAL-0307", "", "The request body could not be read completely.")
				}
				return r, nil, false
			}
			if len(bytes.TrimSpace(raw)) > 0 {
				if problem := validateJSONEnvelope(raw, policy.maxString); problem != "" {
					writeAPIErrorDetail(w, r, "GLOBAL-0307", "", problem)
					return r, nil, false
				}
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), envelopeDeadline)
	return r.WithContext(ctx), cancel, true
}

// repeatedQueryParam reports whether a query string names the same parameter
// twice (49.4.4a, model approved 2026-10-11). Every handler reads with
// .Get(), which takes the first value, while some proxies and log tools take
// the last, so ?tenant_id=a&tenant_id=b could mean different things to them.
// Names are compared after URL-decoding and case-sensitively, like JSON keys,
// and an empty repeat (?a=&a=) counts. The raw string is split here rather
// than through url.ParseQuery, which silently drops a pair whose value fails
// to decode - and with it the evidence of the repeat.
func repeatedQueryParam(rawQuery string, repeatable []string) bool {
	if rawQuery == "" {
		return false
	}
	seen := map[string]bool{}
	for _, pair := range strings.Split(rawQuery, "&") {
		if pair == "" {
			continue
		}
		name, _, _ := strings.Cut(pair, "=")
		if decoded, err := url.QueryUnescape(name); err == nil {
			name = decoded
		}
		if seen[name] && !slices.Contains(repeatable, name) {
			return true
		}
		seen[name] = true
	}
	return false
}

func envelopeTypeAllowed(mediaType string, allowed []string) bool {
	for _, t := range allowed {
		if strings.EqualFold(mediaType, t) {
			return true
		}
	}
	return false
}

func envelopeBodyLimitDetail(limit int64) string {
	return fmt.Sprintf("This address accepts request bodies of at most %d MB.", limit>>20)
}

// safeMethodName keeps a client-chosen method token out of the response when
// it is not a plain short word.
func safeMethodName(m string) string {
	if len(m) > 16 {
		return "this method of"
	}
	for _, c := range m {
		if c < 'A' || c > 'Z' {
			return "this method of"
		}
	}
	return m
}

// validateJSONEnvelope streams the body token by token, so no limit is
// checked only after an oversized structure was already built in memory. It
// returns "" when the body is acceptable, otherwise a fixed description of the
// first problem - deliberately never quoting the submitted keys or values.
func validateJSONEnvelope(raw []byte, maxString int) string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	type frame struct {
		object    bool
		expectKey bool
		keys      map[string]struct{}
		items     int
	}
	var stack []*frame
	valueSeen := func() string {
		if len(stack) == 0 {
			return ""
		}
		top := stack[len(stack)-1]
		if top.object {
			top.expectKey = true
			return ""
		}
		top.items++
		if top.items > envelopeMaxJSONArray {
			return fmt.Sprintf("A JSON list in this request has more than %s items.", strconv.Itoa(envelopeMaxJSONArray))
		}
		return ""
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			if len(stack) == 0 {
				return ""
			}
			return "The request body is not valid JSON."
		}
		if err != nil {
			return "The request body is not valid JSON."
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{', '[':
				if len(stack) > 0 && stack[len(stack)-1].object && stack[len(stack)-1].expectKey {
					return "The request body is not valid JSON."
				}
				if len(stack) >= envelopeMaxJSONDepth {
					return fmt.Sprintf("The request body nests JSON more than %d levels deep.", envelopeMaxJSONDepth)
				}
				f := &frame{object: v == '{', expectKey: v == '{'}
				if f.object {
					f.keys = map[string]struct{}{} // 49.10.5: a list has no keys; a map per "[" made 2 MiB of nested lists cost 70 MB
				}
				stack = append(stack, f)
			case '}', ']':
				stack = stack[:len(stack)-1]
				if problem := valueSeen(); problem != "" {
					return problem
				}
				if len(stack) == 0 && dec.More() {
					return "The request body holds more than one JSON value."
				}
			}
		case string:
			if len(v) > maxString {
				return fmt.Sprintf("A text value in this request is longer than %d characters.", maxString)
			}
			if len(stack) > 0 && stack[len(stack)-1].object && stack[len(stack)-1].expectKey {
				top := stack[len(stack)-1]
				if _, dup := top.keys[v]; dup {
					// Two spellings of one key ("qty" and "qty" decode
					// alike) are caught here too, since keys are compared
					// after unescaping. Case variants are deliberately left
					// alone: copies_override, dispositions and exchange_for
					// are maps keyed by SKU or line, where "ab1" and "AB1" can
					// both be real.
					return "A JSON object in this request repeats the same field name, so it is ambiguous which value applies."
				}
				top.keys[v] = struct{}{}
				top.expectKey = false
				continue
			}
			if problem := valueSeen(); problem != "" {
				return problem
			}
		case json.Number:
			if len(v) > envelopeMaxJSONNumberLen {
				return fmt.Sprintf("A number in this request is longer than %d characters.", envelopeMaxJSONNumberLen)
			}
			// ParseFloat saturates to +-Inf with a range error, which the
			// magnitude check rejects as well.
			if f, _ := strconv.ParseFloat(string(v), 64); math.Abs(f) > envelopeMaxJSONNumber {
				return "A number in this request is outside the range that can be stored exactly (plus or minus 9,007,199,254,740,991)."
			}
			if problem := valueSeen(); problem != "" {
				return problem
			}
		default:
			if problem := valueSeen(); problem != "" {
				return problem
			}
		}
		if len(stack) == 0 && dec.More() {
			return "The request body holds more than one JSON value."
		}
	}
}
