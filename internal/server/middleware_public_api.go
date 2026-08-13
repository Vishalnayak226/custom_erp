package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"custom_erp/engines"
)

// Stage 38 - the public API boundary.
//
// This is deliberately a SECOND middleware rather than a branch inside
// apiMiddleware. The two admit different callers on different evidence: a human
// session presents a JWT and inherits a role; an integration presents an opaque
// key and holds only its explicit scopes. Folding them together is how a public
// route accidentally inherits a human's permissions, so the split is the
// safety property, not a stylistic choice.
//
// Every /api/public/v1 route runs through here, and the order below is load
// bearing:
//
//	authenticate -> authorize scope -> admit (38.3) -> de-duplicate (38.5)
//	-> handle -> record (38.9)
//
// Authentication precedes admission so an unauthenticated flood cannot consume
// a real credential's budget. Admission precedes idempotency so a caller
// hammering the same key still gets rate limited. Recording is last and
// unconditional, so a rejected call is as visible as a successful one.

const publicAPIPathPrefix = "/api/public/v1/"

// publicAPIAuthFailureMessage is the single response for every authentication
// failure - absent header, malformed key, unknown prefix, wrong tenant, revoked
// key, expired key, hash mismatch. A caller learns that it is not authenticated
// and nothing else, so the endpoint cannot be used to enumerate which tenants
// or key prefixes exist.
const publicAPIAuthFailureMessage = "Invalid API credential."

// publicAPIResponseRecorder captures what was sent so the traffic log can
// record the status and the idempotency store can replay the body. The body
// buffer is capped because a large export must not be held twice in memory.
type publicAPIResponseRecorder struct {
	http.ResponseWriter
	status    int
	body      bytes.Buffer
	capture   bool
	truncated bool
}

const publicAPIMaxCapturedBody = 256 << 10

func (rec *publicAPIResponseRecorder) WriteHeader(status int) {
	if rec.status == 0 {
		rec.status = status
	}
	rec.ResponseWriter.WriteHeader(status)
}

func (rec *publicAPIResponseRecorder) Write(b []byte) (int, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	if rec.capture {
		if rec.body.Len()+len(b) > publicAPIMaxCapturedBody {
			rec.truncated = true
			rec.capture = false
			rec.body.Reset()
		} else {
			rec.body.Write(b)
		}
	}
	return rec.ResponseWriter.Write(b)
}

func (rec *publicAPIResponseRecorder) statusOrOK() int {
	if rec.status == 0 {
		return http.StatusOK
	}
	return rec.status
}

func publicAPIMutating(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// publicAPIMiddleware wraps one curated public route. requiredScope names the
// single scope a credential must hold; a route with no scope cannot be
// registered, which is what stops a public endpoint from silently defaulting to
// "any valid key will do".
func publicAPIMiddleware(requiredScope string, next http.HandlerFunc) http.HandlerFunc {
	if strings.TrimSpace(requiredScope) == "" {
		panic("publicAPIMiddleware: every public route must declare a scope")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		correlationID := generateUUID()
		w.Header().Set("X-Correlation-ID", correlationID)
		w.Header().Set("Content-Type", "application/json")
		r.Header.Set("Resolved-Correlation-ID", correlationID)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		rec := &publicAPIResponseRecorder{ResponseWriter: w}
		entry := engines.PublicAPILogEntry{
			Method: r.Method, Path: r.URL.Path, RequiredScope: requiredScope,
			CorrelationID: correlationID, ClientIP: clientIP(r),
		}
		tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		// The log is written from a defer so that a panic, an early rejection
		// and a normal response are all recorded by the same code path.
		recorded := false
		record := func(outcome string) {
			if recorded {
				return
			}
			recorded = true
			entry.StatusCode = rec.statusOrOK()
			entry.DurationMS = int(time.Since(started).Milliseconds())
			entry.Outcome = outcome
			logTenant := tenantID
			if logTenant == "" {
				// An unauthenticated call with no usable tenant still deserves a
				// trace; "default" is where this deployment's own operational
				// logs already land.
				logTenant = "default"
			}
			engines.RecordPublicAPIRequest(logTenant, entry)
		}

		if tenantID == "" {
			writeAPIErrorGeneric(rec, r, http.StatusUnauthorized, publicAPIAuthFailureMessage)
			record("auth_failed")
			return
		}
		r.Header.Set("Resolved-Tenant-ID", tenantID)

		rawKey := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		credential, err := engines.AuthenticateAPICredential(tenantID, strings.TrimSpace(rawKey))
		if err != nil {
			if !errors.Is(err, engines.ErrInvalidAPICredential) {
				writeAPIErrorGeneric(rec, r, http.StatusServiceUnavailable, "Unable to verify the API credential - please retry shortly.")
				record("auth_error")
				return
			}
			writeAPIErrorGeneric(rec, r, http.StatusUnauthorized, publicAPIAuthFailureMessage)
			record("auth_failed")
			return
		}
		entry.CredentialID = credential.ID
		entry.KeyPrefix = credential.KeyPrefix

		if !engines.APICredentialHasScope(credential, requiredScope) {
			// Naming the missing scope is safe and saves an integrator a support
			// ticket - it reveals nothing beyond what their own key already is.
			writeAPIErrorGeneric(rec, r, http.StatusForbidden,
				fmt.Sprintf("This API credential does not hold the %q scope required by this endpoint.", requiredScope))
			record("scope_denied")
			return
		}

		budget, retryAfter, admitErr := engines.AdmitPublicAPIRequest(tenantID, credential.ID)
		if budget != nil {
			rec.Header().Set("X-RateLimit-Limit", strconv.Itoa(budget.RateLimitPerMinute))
			rec.Header().Set("X-RateLimit-Remaining", strconv.Itoa(budget.RateRemaining))
			rec.Header().Set("X-Quota-Limit", strconv.Itoa(budget.DailyQuota))
			rec.Header().Set("X-Quota-Remaining", strconv.Itoa(budget.DailyRemaining))
		}
		if admitErr != nil {
			if errors.Is(admitErr, engines.ErrPublicAPIRateLimited) || errors.Is(admitErr, engines.ErrPublicAPIQuotaExceeded) {
				seconds := int(retryAfter.Seconds())
				if seconds < 1 {
					seconds = 1
				}
				rec.Header().Set("Retry-After", strconv.Itoa(seconds))
				message := fmt.Sprintf("Rate limit of %d requests per minute exceeded for this API credential. Retry in %d second(s).", budget.RateLimitPerMinute, seconds)
				outcome := "rate_limited"
				if errors.Is(admitErr, engines.ErrPublicAPIQuotaExceeded) {
					message = fmt.Sprintf("Daily quota of %d requests exhausted for this API credential. It resets at midnight, in %d second(s).", budget.DailyQuota, seconds)
					outcome = "quota_exceeded"
				}
				writeAPIErrorDetail(rec, r, "SEC-0280", "", message)
				record(outcome)
				return
			}
			writeAPIErrorGeneric(rec, r, http.StatusServiceUnavailable, "Unable to evaluate this credential's request budget - please retry shortly.")
			record("admit_error")
			return
		}

		// 38.5. Every mutating public call carries an idempotency key, so a
		// client that retries after a timeout cannot create a second order.
		idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		claimed := false
		if publicAPIMutating(r.Method) {
			r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				writeAPIErrorGeneric(rec, r, http.StatusRequestEntityTooLarge, "Request body is too large.")
				record("body_too_large")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			entry.IdempotencyKey = idempotencyKey

			replay, idemErr := engines.BeginIdempotentRequest(tenantID, credential.ID, idempotencyKey, r.Method, r.URL.Path, body)
			switch {
			case errors.Is(idemErr, engines.ErrIdempotencyKeyRequired):
				writeAPIErrorGeneric(rec, r, http.StatusBadRequest,
					"An Idempotency-Key header is required on every mutating public API request, so a retry cannot be mistaken for a second request.")
				record("idempotency_missing")
				return
			case errors.Is(idemErr, engines.ErrIdempotencyKeyReused):
				writeAPIErrorGeneric(rec, r, http.StatusConflict,
					"This Idempotency-Key has already been used for a different request. Use a new key for a new request.")
				record("idempotency_conflict")
				return
			case errors.Is(idemErr, engines.ErrIdempotencyInProgress):
				rec.Header().Set("Retry-After", "1")
				writeAPIErrorGeneric(rec, r, http.StatusConflict,
					"An identical request with this Idempotency-Key is still being processed. Retry shortly.")
				record("idempotency_in_progress")
				return
			case idemErr != nil:
				writeEngineError(rec, r, idemErr, http.StatusUnprocessableEntity)
				record("idempotency_error")
				return
			}
			if replay != nil {
				rec.Header().Set("Idempotency-Replayed", "true")
				rec.WriteHeader(replay.StatusCode)
				_, _ = rec.ResponseWriter.Write([]byte(replay.Body))
				record("idempotent_replay")
				return
			}
			claimed = true
			rec.capture = true
		}

		// A panic must not leave an idempotency key wedged in "In Progress" -
		// that would lock the caller out of retrying for the whole retention
		// window over a bug on our side.
		defer func() {
			if err := recover(); err != nil {
				if claimed {
					engines.ReleaseIdempotentRequest(tenantID, credential.ID, idempotencyKey)
				}
				engines.LogSystemError(tenantID, correlationID, "PANIC", r.URL.Path, fmt.Sprintf("%v", err), string(debug.Stack()))
				entry2 := errorCatalog["GLOBAL-0302"]
				writeResponse(rec, entry2.HTTPStatus, apiErrorBody{
					Error: entry2.UserMessage, Code: entry2.Code,
					CorrelationID: correlationID, Retryable: entry2.Retryable,
				})
				record("panic")
			}
		}()

		if !tenantConcurrency.acquire(tenantID) {
			if claimed {
				engines.ReleaseIdempotentRequest(tenantID, credential.ID, idempotencyKey)
			}
			writeAPIErrorGeneric(rec, r, http.StatusServiceUnavailable, "Too many concurrent requests in flight for this tenant - please retry shortly.")
			record("shed")
			return
		}
		next.ServeHTTP(rec, r)
		tenantConcurrency.release(tenantID)

		if claimed {
			if rec.truncated {
				// The response was too large to store, so it cannot be replayed
				// honestly. Releasing the claim is the safe direction: a retry
				// re-executes rather than receiving a truncated body that would
				// parse as a valid, wrong answer.
				engines.ReleaseIdempotentRequest(tenantID, credential.ID, idempotencyKey)
			} else if err := engines.CompleteIdempotentRequest(tenantID, credential.ID, idempotencyKey, rec.statusOrOK(), rec.body.String()); err != nil {
				engines.LogSystemError(tenantID, correlationID, "Error", r.URL.Path, "store idempotent response: "+err.Error(), "")
			}
		}
		record("handled")
	}
}
