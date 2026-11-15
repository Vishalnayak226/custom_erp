package server

// 195.1: Guide's hidden key. Guide, the owner's own test robot, sends a secret
// key in a request header the page never sees. The server keeps only the key's
// SHA-256 (ERP_AUTOMATION_KEY_SHA256), never the key itself, compares it in
// constant time, and removes the header from the request at the door so no
// handler, log or error report can ever see or echo it.
//
// Unset, the whole feature is off: nothing is lifted and no browser is refused,
// so local tools and tests keep working. Set:
//   - a request carrying the key skips the per-minute budgets, except sign-in's
//     (a leaked key must never become a password-guessing tool);
//   - a browser that says it is driven by automation, and has no key, is refused
//     at sign-in with the same USERAC-0021 a wrong password gets, so a bot
//     learns nothing; the refusal is written to the audit log for the admin.
//
// API clients and PrintBridge are not browsers and are not affected. A bot
// built to hide that it is automated is the edge's job (Cloudflare bot
// controls, 26.1.3b), not this check's.

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

const (
	automationKeyHeader    = "X-ERP-Automation-Key"
	automatedBrowserHeader = "X-ERP-Automated" // the app sends it when navigator.webdriver is true
	automationKeyEnv       = "ERP_AUTOMATION_KEY_SHA256"
	// A ceiling, not "unlimited": a runaway loop in Guide still stops somewhere.
	automationKeyLimit = 1200
)

type automationKeyCtxKey struct{}

var automationKeyBadEnv sync.Once

// automationKeyHash is the configured SHA-256, or nil when the feature is off.
// Read on each call so a changed env file takes effect on restart without a
// cache to forget, and tests can set it per case.
func automationKeyHash() []byte {
	v := strings.TrimSpace(os.Getenv(automationKeyEnv))
	if v == "" {
		return nil
	}
	h, err := hex.DecodeString(strings.ToLower(v))
	if err != nil || len(h) != sha256.Size {
		automationKeyBadEnv.Do(func() {
			log.Printf("%s is not a 64-character hex SHA-256, so Guide's key is off", automationKeyEnv)
		})
		return nil
	}
	return h
}

// takeAutomationKey checks the key header, deletes it from the request and
// records the answer on the context. apiMiddleware calls it first thing.
func takeAutomationKey(r *http.Request) *http.Request {
	got := r.Header.Get(automationKeyHeader)
	r.Header.Del(automationKeyHeader)
	ok := false
	if want := automationKeyHash(); want != nil && got != "" {
		sum := sha256.Sum256([]byte(got))
		ok = subtle.ConstantTimeCompare(sum[:], want) == 1
	}
	return r.WithContext(context.WithValue(r.Context(), automationKeyCtxKey{}, ok))
}

func hasAutomationKey(r *http.Request) bool {
	ok, _ := r.Context().Value(automationKeyCtxKey{}).(bool)
	return ok
}

// automatedBrowser: Playwright, Selenium and Puppeteer all set
// navigator.webdriver, which the app reports in a header; a headless browser
// also names itself in its user agent.
func automatedBrowser(r *http.Request) bool {
	return r.Header.Get(automatedBrowserHeader) != "" || strings.Contains(r.UserAgent(), "HeadlessChrome")
}

// automatedSignInRefused: with the key set, an automated browser signs in only
// with the key.
func automatedSignInRefused(r *http.Request) bool {
	return automationKeyHash() != nil && automatedBrowser(r) && !hasAutomationKey(r)
}
