package server

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Stage 49.4.2: the cross-origin boundary.
//
// Bearer tokens authorise this API, never a cookie (no handler sets or reads
// one; pinned by TestNoRouteAuthorizesFromCookie), so a forged cross-site
// request has no credential to ride on and CSRF tokens do not apply. What
// remains, and what this file does:
//
//   - the CORS allowlist is exact scheme://host[:port] entries only: "null",
//     "*" and anything with a path, query, userinfo or wildcard is dropped
//     when the list loads, so one bad CORS_ALLOWED_ORIGINS entry cannot open
//     the API to every page or to sandboxed documents;
//   - every response says Vary: Origin so no cache serves one origin's CORS
//     headers to another;
//   - an allowed preflight is cacheable for ten minutes, a refused one gets
//     no CORS headers at all;
//   - a state-changing request whose Origin is neither allowlisted nor this
//     server's own is logged (CSRF_ORIGIN_CHECK=report, the default) or
//     refused (CSRF_ORIGIN_CHECK=enforce). A no-cors fetch can send a body with
//     no Content-Type and no preflight; this is the check that sees it.
//
// "report" is the default so a proxy that rewrites Host cannot lock a client
// out of every write on release; switch to "enforce" once the log shows no
// legitimate origin (see docs/security/adr-session-topology.md).

// normalizeCORSOrigin returns the canonical form of one allowlist entry, or
// false if it is not an exact origin.
func normalizeCORSOrigin(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "null") || strings.ContainsAny(raw, "*") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", false
	}
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	out := scheme + "://" + host
	if strings.Contains(host, ":") { // IPv6 literal
		out = scheme + "://[" + host + "]"
	}
	if port != "" {
		out += ":" + port
	}
	return out, true
}

// originIsSameHost reports whether an Origin names this server itself: its
// host[:port] equals the request's Host (or the proxy's X-Forwarded-Host).
func originIsSameHost(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	h := strings.ToLower(u.Host)
	if h == strings.ToLower(r.Host) {
		return true
	}
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		for _, f := range strings.Split(fwd, ",") {
			if h == strings.ToLower(strings.TrimSpace(f)) {
				return true
			}
		}
	}
	return false
}

func csrfOriginMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CSRF_ORIGIN_CHECK"))) {
	case "enforce":
		return "enforce"
	case "off":
		return "off"
	}
	return "report"
}

var csrfSeen sync.Map // origin -> last logged

// crossOriginWriteRefused applies the state-change origin check. It returns
// true when the request must be refused.
func crossOriginWriteRefused(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" || originAllowed(origin) || originIsSameHost(r, origin) {
		return false
	}
	mode := csrfOriginMode()
	if mode == "off" {
		return false
	}
	if prev, ok := csrfSeen.Load(origin); !ok || time.Since(prev.(time.Time)) > time.Hour {
		csrfSeen.Store(origin, time.Now())
		log.Printf("[csrf-origin] %s: %s %s from Origin %q is neither allowlisted nor this host", mode, r.Method, r.URL.Path, origin)
	}
	return mode == "enforce"
}
