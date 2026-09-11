// Command edgecheck is the Stage 49.1.7 outside-in verification tool: it
// probes a deployed ERP instance the way an anonymous internet client would
// - unknown hostnames, the direct application port, alternate HTTP methods,
// encoded paths and the internal-only prefix - and reports whether each one
// fails safely.
//
// It is deliberately read-only. Every check is a GET/HEAD/OPTIONS/TRACE/PUT/
// DELETE against a route that either has no side effect or already refuses
// an unauthenticated write before touching business logic, and it never
// calls POST /api/v1/login (that path is already covered by the deploy
// runbook's own live checks and this tool must never spend the account
// lockout / rate-limit budget of a real login endpoint on an unattended
// scan). It makes no attempt to fix anything it finds - like surfacescan, it
// only reports, so a genuine finding goes through the risk register rather
// than a silent local patch.
//
// Usage:
//
//	go run ./cmd/edgecheck -base https://app.wholeops.in -direct-host 139.59.17.16
//
// Run this from a network that is actually outside the deployment - the
// point is what an anonymous internet client sees. Running it from inside an
// SSH tunnel to the box, or against 127.0.0.1, proves nothing about the
// firewall (docs/ai_handover.md's 2026-08-14 entry hit exactly this trap
// once already).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type status int

const (
	pass status = iota
	fail
	concern
	skip
)

func (s status) String() string {
	switch s {
	case pass:
		return "PASS"
	case fail:
		return "FAIL"
	case concern:
		return "CONCERN"
	default:
		return "SKIP"
	}
}

type result struct {
	Check  string `json:"check"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func main() {
	base := flag.String("base", "", "public base URL of the deployment, e.g. https://app.wholeops.in (required)")
	directHost := flag.String("direct-host", "", "the box's public IP/hostname, to check the app port and other services are not directly reachable (optional but recommended)")
	directPort := flag.Int("direct-port", 8080, "the app's own bind port, expected UNREACHABLE from outside (Caddy should be the only path in)")
	baseDomain := flag.String("tenant-base-domain", "", "TENANT_BASE_DOMAIN if per-tenant hostnames are live, to test an unknown-tenant subdomain (optional)")
	timeout := flag.Duration("timeout", 6*time.Second, "per-request timeout")
	out := flag.String("out", "", "optional file to also write the JSON report to")
	flag.Parse()

	if *base == "" {
		fmt.Fprintln(os.Stderr, "edgecheck: -base is required, e.g. -base https://app.wholeops.in")
		os.Exit(2)
	}
	baseURL, err := url.Parse(*base)
	if err != nil || baseURL.Scheme != "https" {
		fmt.Fprintf(os.Stderr, "edgecheck: -base must be an https URL, got %q\n", *base)
		os.Exit(2)
	}

	client := &http.Client{
		Timeout: *timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	insecureClient := &http.Client{
		Timeout: *timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	var results []result
	record := func(check string, s status, detail string) {
		results = append(results, result{Check: check, Status: s.String(), Detail: detail})
	}

	// 1. Plain HTTP redirects to HTTPS.
	{
		httpURL := "http://" + baseURL.Host + "/"
		req, _ := http.NewRequest(http.MethodGet, httpURL, nil)
		resp, err := insecureClient.Do(req)
		if err != nil {
			record("http-redirects-to-https", concern, fmt.Sprintf("could not reach %s at all: %v", httpURL, err))
		} else {
			resp.Body.Close()
			loc := resp.Header.Get("Location")
			if resp.StatusCode >= 300 && resp.StatusCode < 400 && strings.HasPrefix(loc, "https://") {
				record("http-redirects-to-https", pass, fmt.Sprintf("%d -> %s", resp.StatusCode, loc))
			} else {
				record("http-redirects-to-https", fail, fmt.Sprintf("expected a 3xx to https://, got %d Location=%q", resp.StatusCode, loc))
			}
		}
	}

	// 2. Declared public routes answer as declared.
	for _, c := range []struct {
		path       string
		wantStatus int
	}{
		{"/api/v1/health", http.StatusOK},
		{"/api/v1/version", http.StatusOK},
	} {
		checkSimpleGet(client, baseURL, c.path, c.wantStatus, "declared-public-route:"+c.path, record)
	}

	// 3. /internal/* never answers from the public edge, even though the
	// app itself registers a handler at /internal/tls-ask - that route only
	// exists for Caddy to call over 127.0.0.1 (deploy/Caddyfile's @internal
	// block).
	for _, path := range []string{"/internal/tls-ask", "/internal/", "/internal/anything-at-all"} {
		checkNotFound(client, baseURL, path, "internal-prefix-blocked:"+path, record)
	}

	// 4. Debug/panic surface stays unreachable in production (24.16 gates it
	// on ENV != production).
	checkNotFound(client, baseURL, "/api/v1/debug/panic", "debug-panic-unreachable", record)

	// 5. Alternate HTTP methods against real routes do not succeed as if
	// they were the declared method.
	altMethodChecks(client, baseURL, record)

	// 6. Encoded/traversal paths do not reach anything past the static root
	// or the internal prefix.
	encodedPathChecks(client, baseURL, record)

	// 7. Security headers and no-server-fingerprint hold on the actual
	// public edge (Caddy + the app's own securityHeaders together).
	headerChecks(client, baseURL, record)

	// 8. Unknown-tenant hostname, if TENANT_BASE_DOMAIN is in play.
	if *baseDomain != "" {
		unknownTenantHostCheck(client, baseURL, *baseDomain, record)
	} else {
		record("unknown-tenant-host", skip, "no -tenant-base-domain given; pass it if TENANT_BASE_DOMAIN is set in production (Stage 44)")
	}

	// 9. Direct origin: the app's own bind port must not be reachable from
	// outside - Caddy is declared as the only way in (deploy/Caddyfile,
	// deploy/README.md).
	if *directHost != "" {
		directOriginChecks(*directHost, *directPort, record)
	} else {
		record("direct-origin-unreachable", skip, "no -direct-host given")
		record("no-extraneous-ports", skip, "no -direct-host given")
	}

	// Report.
	failures := 0
	for _, r := range results {
		fmt.Printf("[%-7s] %-38s %s\n", r.Status, r.Check, r.Detail)
		if r.Status == fail.String() || r.Status == concern.String() {
			failures++
		}
	}
	fmt.Printf("\n%d check(s), %d failure/concern\n", len(results), failures)

	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "edgecheck: writing -out: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			fmt.Fprintf(os.Stderr, "edgecheck: encoding -out: %v\n", err)
			os.Exit(1)
		}
	}

	if failures > 0 {
		os.Exit(1)
	}
}

func checkSimpleGet(client *http.Client, base *url.URL, path string, want int, name string, record func(string, status, string)) {
	u := *base
	u.Path = path
	resp, err := client.Get(u.String())
	if err != nil {
		record(name, fail, fmt.Sprintf("request failed: %v", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == want {
		record(name, pass, fmt.Sprintf("%d as declared", resp.StatusCode))
	} else {
		record(name, fail, fmt.Sprintf("expected %d, got %d", want, resp.StatusCode))
	}
}

func checkNotFound(client *http.Client, base *url.URL, path string, name string, record func(string, status, string)) {
	u := *base
	u.Path = path
	resp, err := client.Get(u.String())
	if err != nil {
		// A connection-level failure is not the same guarantee as a clean
		// 404, but it is not a public-reachability problem either.
		record(name, pass, fmt.Sprintf("unreachable rather than exposed: %v", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		record(name, pass, "404")
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	record(name, fail, fmt.Sprintf("expected 404, got %d, body prefix %q", resp.StatusCode, string(body)))
}

func altMethodChecks(client *http.Client, base *url.URL, record func(string, status, string)) {
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodTrace, "/"},
		{http.MethodPut, "/api/v1/login"},
		{http.MethodDelete, "/api/v1/health"},
		{http.MethodGet, "/api/v1/login"}, // login is POST-only
	}
	for _, c := range cases {
		u := *base
		u.Path = c.path
		req, err := http.NewRequest(c.method, u.String(), nil)
		if err != nil {
			record(fmt.Sprintf("alt-method:%s %s", c.method, c.path), concern, fmt.Sprintf("could not build request: %v", err))
			continue
		}
		resp, err := client.Do(req)
		name := fmt.Sprintf("alt-method:%s %s", c.method, c.path)
		if err != nil {
			// Go's own client refuses to send TRACE with a body and some
			// proxies close the connection outright on TRACE/unsupported
			// methods - either is a safe outcome, not a finding.
			record(name, pass, fmt.Sprintf("refused at the transport: %v", err))
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			record(name, fail, fmt.Sprintf("method succeeded with 200 where it should have been refused"))
		} else {
			record(name, pass, fmt.Sprintf("%d", resp.StatusCode))
		}
	}
}

func encodedPathChecks(client *http.Client, base *url.URL, record func(string, status, string)) {
	// Percent-encoded traversal/prefix attempts. Go's url.Parse keeps
	// RawPath as the literal encoded text as long as it is a valid encoding
	// of Path, and http.Transport sends EscapedPath() - i.e. RawPath - on
	// the wire, so these reach the server exactly as written here rather
	// than pre-cleaned by the client.
	rawPaths := []string{
		"/%2e%2e/%2e%2e/%2e%2e/etc/passwd",
		"/api/v1/..%2f..%2finternal/tls-ask",
		"/..%2f..%2finternal/tls-ask",
		"/internal%2ftls-ask",
		"/..;/internal/tls-ask",
	}
	for _, p := range rawPaths {
		full := fmt.Sprintf("%s://%s%s", base.Scheme, base.Host, p)
		req, err := http.NewRequest(http.MethodGet, full, nil)
		if err != nil {
			record("encoded-path:"+p, concern, fmt.Sprintf("could not build request: %v", err))
			continue
		}
		resp, err := client.Do(req)
		name := "encoded-path:" + p
		if err != nil {
			record(name, pass, fmt.Sprintf("refused at the transport: %v", err))
			continue
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		lower := strings.ToLower(string(body))
		if resp.StatusCode == http.StatusOK && (strings.Contains(lower, "root:") || strings.Contains(lower, "tls-ask") || strings.Contains(lower, "on_demand")) {
			record(name, fail, fmt.Sprintf("200 with suspicious body prefix %q", string(body)))
		} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			record(name, concern, fmt.Sprintf("2xx (%d) - review body manually: %q", resp.StatusCode, string(body)))
		} else {
			record(name, pass, fmt.Sprintf("%d", resp.StatusCode))
		}
	}
}

func headerChecks(client *http.Client, base *url.URL, record func(string, status, string)) {
	u := *base
	u.Path = "/"
	resp, err := client.Get(u.String())
	if err != nil {
		record("security-headers", fail, fmt.Sprintf("request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	required := []string{"Strict-Transport-Security", "Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy"}
	var missing []string
	for _, h := range required {
		if resp.Header.Get(h) == "" {
			missing = append(missing, h)
		}
	}
	if len(missing) == 0 {
		record("security-headers", pass, "all present")
	} else {
		record("security-headers", fail, "missing: "+strings.Join(missing, ", "))
	}

	var leaked []string
	for _, h := range []string{"Server", "X-Powered-By"} {
		if v := resp.Header.Get(h); v != "" {
			leaked = append(leaked, h+"="+v)
		}
	}
	if len(leaked) == 0 {
		record("no-server-fingerprint", pass, "Server/X-Powered-By absent")
	} else {
		record("no-server-fingerprint", fail, strings.Join(leaked, ", "))
	}
}

// unknownTenantHostCheck sends a request over a TLS connection to the real
// public host (so the handshake uses the certificate that is actually
// installed) but with an HTTP Host header naming a subdomain that is shaped
// like a tenant address and names no live tenant. Request.Host, not the
// dialed URL, is what net/http sends as the Host header - see net/http's own
// doc comment on Request.Host.
func unknownTenantHostCheck(client *http.Client, base *url.URL, tenantBaseDomain string, record func(string, status, string)) {
	spoofedHost := "edgecheck-nonexistent-tenant." + tenantBaseDomain
	u := *base
	u.Path = "/api/v1/health"
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		record("unknown-tenant-host", concern, fmt.Sprintf("could not build request: %v", err))
		return
	}
	req.Host = spoofedHost
	resp, err := client.Do(req)
	if err != nil {
		record("unknown-tenant-host", pass, fmt.Sprintf("refused at the transport: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode == http.StatusNotFound && strings.Contains(string(body), "GLOBAL-0004") {
		record("unknown-tenant-host", pass, fmt.Sprintf("refused with GLOBAL-0004 as tenantHostGate declares"))
		return
	}
	if resp.StatusCode == http.StatusOK {
		record("unknown-tenant-host", concern, fmt.Sprintf("Host=%s got 200 body=%q - confirm this is the default/apex tenant's own health check and not a data leak", spoofedHost, string(body)))
		return
	}
	record("unknown-tenant-host", concern, fmt.Sprintf("Host=%s -> %d body=%q", spoofedHost, resp.StatusCode, string(body)))
}

func directOriginChecks(host string, appPort int, record func(string, status, string)) {
	// The app's own bind port: declared reachable ONLY via Caddy on
	// 127.0.0.1, so from outside this must refuse or time out, never accept.
	checkPortUnreachable(host, appPort, "direct-origin-unreachable", record)

	// A short list of services that must never be internet-facing on this
	// box, per 49.7.3's network-boundary requirement. 22/80/443 are the
	// three declared inbound rules (docs/ai_handover.md 2026-08-14 entry)
	// and are checked separately as "expected open", not folded into this
	// loop, so a pass here can't be misread as "nothing is open at all".
	for _, p := range []int{5432, 6379, 9200, 3000, 9090} {
		checkPortUnreachable(host, p, fmt.Sprintf("no-extraneous-port:%d", p), record)
	}

	for _, p := range []int{22, 80, 443} {
		checkPortExpectedOpen(host, p, fmt.Sprintf("declared-port-open:%d", p), record)
	}
}

func checkPortUnreachable(host string, port int, name string, record func(string, status, string)) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		record(name, pass, fmt.Sprintf("%s refused/unreachable: %v", addr, err))
		return
	}
	conn.Close()
	record(name, fail, fmt.Sprintf("%s accepted a connection from outside - should not be publicly reachable", addr))
}

func checkPortExpectedOpen(host string, port int, name string, record func(string, status, string)) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		record(name, concern, fmt.Sprintf("%s expected open (declared firewall rule) but did not accept: %v", addr, err))
		return
	}
	conn.Close()
	record(name, pass, fmt.Sprintf("%s open as declared", addr))
}
