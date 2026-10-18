package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"custom_erp/engines"
)

// Stage 38.2d - POST /api/public/v1/oauth/token, the OAuth2 client_credentials
// grant (RFC 6749 section 4.4).
//
// Deliberately outside publicAPIMiddleware: that middleware authenticates a
// bearer, and this endpoint is where a client without one gets one. It keeps
// the boundary's other habits - a correlation id, a per-address throttle, the
// public traffic log and no-store caching - and speaks RFC 6749's error
// vocabulary ({"error": "invalid_client"}) because that is what OAuth client
// libraries parse. The request envelope is not applied: token requests are
// form-encoded by the standard, which that envelope refuses.

const oauthTokenMaxBody = 16 << 10

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description})
}

func handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	correlationID := generateUUID()
	w.Header().Set("X-Correlation-ID", correlationID)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	entry := engines.PublicAPILogEntry{Method: r.Method, Path: r.URL.Path, RequiredScope: "oauth:token", CorrelationID: correlationID, ClientIP: clientIP(r)}
	record := func(status int, outcome string) {
		entry.StatusCode, entry.Outcome = status, outcome
		entry.DurationMS = int(time.Since(started).Milliseconds())
		logTenant := tenantID
		if logTenant == "" {
			logTenant = "default"
		}
		engines.RecordPublicAPIRequest(logTenant, entry)
	}

	// Thirty tries a minute per address: plenty for a client that caches its
	// token, useless for guessing a secret.
	if !globalLimiter.Allow("oauth-token:"+clientIP(r), 30, time.Minute) {
		w.Header().Set("Retry-After", "60")
		writeOAuthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "Too many token requests from this address. Retry in a minute.")
		record(http.StatusTooManyRequests, "rate_limited")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, oauthTokenMaxBody)
	form := url.Values{}
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "The body must be form-encoded (or a flat JSON object).")
			record(http.StatusBadRequest, "invalid_request")
			return
		}
		for k, v := range body {
			form.Set(k, v)
		}
	} else {
		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "The body could not be read as application/x-www-form-urlencoded.")
			record(http.StatusBadRequest, "invalid_request")
			return
		}
		form = r.PostForm
	}
	if tenantID == "" {
		tenantID = strings.TrimSpace(form.Get("tenant_id"))
	}

	if grant := form.Get("grant_type"); grant != "client_credentials" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "Only grant_type=client_credentials is supported.")
		record(http.StatusBadRequest, "unsupported_grant_type")
		return
	}

	clientID, clientSecret, basic := r.BasicAuth()
	if basic {
		// RFC 6749 section 2.3.1: both halves are form-urlencoded inside Basic.
		if id, err := url.QueryUnescape(clientID); err == nil {
			clientID = id
		}
		if secret, err := url.QueryUnescape(clientSecret); err == nil {
			clientSecret = secret
		}
		if form.Get("client_secret") != "" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "Send the client secret in the Authorization header or the body, not both.")
			record(http.StatusBadRequest, "invalid_request")
			return
		}
	} else {
		clientID, clientSecret = form.Get("client_id"), form.Get("client_secret")
	}
	entry.KeyPrefix = strings.TrimSpace(clientID)

	if tenantID == "" || clientID == "" || clientSecret == "" {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "Client authentication failed.")
		record(http.StatusUnauthorized, "auth_failed")
		return
	}
	token, credential, err := engines.ExchangeClientCredentials(tenantID, clientID, clientSecret, strings.Fields(form.Get("scope")))
	if credential != nil {
		entry.CredentialID = credential.ID
	}
	switch {
	case errors.Is(err, engines.ErrOAuthInvalidClient):
		if basic {
			w.Header().Set("WWW-Authenticate", `Basic realm="erp-public-api"`)
		}
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "Client authentication failed.")
		record(http.StatusUnauthorized, "auth_failed")
		return
	case errors.Is(err, engines.ErrOAuthInvalidScope):
		writeOAuthError(w, http.StatusBadRequest, "invalid_scope", "A requested scope is not granted to this client.")
		record(http.StatusBadRequest, "invalid_scope")
		return
	case err != nil:
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "The token could not be issued - please retry shortly.")
		record(http.StatusServiceUnavailable, "token_error")
		return
	}
	_ = json.NewEncoder(w).Encode(token)
	record(http.StatusOK, "token_issued")
}
