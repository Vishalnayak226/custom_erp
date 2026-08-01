package server

import (
	"encoding/json"
	"net/http"
	"regexp"

	"custom_erp/engines"
)

// Stage 23: standardized error-response plumbing. Every API error response
// should go through writeAPIError (a precise, curated errorCatalog code) or
// writeAPIErrorGeneric (a status-code fallback for call sites not yet mapped
// to a precise code) instead of http.Error/hand-rolled json.Encode calls, so
// every response gets the same {"error", "code", "correlation_id",
// "retryable"} JSON shape - apiMiddleware already sets Content-Type:
// application/json on every response (middleware.go:310), so a plain-text
// http.Error body silently breaks every frontend caller that does
// `await res.json()` (all of them - see public/app.js). See
// docs/specs/message_catalog.md for the full design.

// genericCodeForStatus maps an HTTP status to the nearest Global/Common (or
// Security) catalog code, for call sites with no precise scenario match.
// The original call site's own message text is still what's shown to the
// user (writeAPIErrorGeneric never overwrites it) - this only attaches a
// code so severity/log/audit metadata and the response envelope are
// consistent everywhere, even before per-scenario curation catches up.
var genericCodeForStatus = map[int]string{
	http.StatusBadRequest:          "GLOBAL-0002", // Invalid format
	http.StatusUnauthorized:        "GLOBAL-0009", // Session expired
	http.StatusForbidden:           "GLOBAL-0011", // Permission denied
	http.StatusNotFound:            "GLOBAL-0004", // Record not found
	http.StatusConflict:            "GLOBAL-0006", // Concurrent update
	http.StatusUnprocessableEntity: "GLOBAL-0002", // Invalid format
	http.StatusTooManyRequests:     "SEC-0280",    // Rate limit exceeded
	http.StatusServiceUnavailable:  "GLOBAL-0010", // System unavailable
	http.StatusInternalServerError: "GLOBAL-0302", // Unexpected server error (gap-fill row, see message_catalog.md)
}

// placeholderRe matches the {field name}/{unique key}-style placeholder a
// catalog UserMessage may contain (at most one per message in the source
// matrix).
var placeholderRe = regexp.MustCompile(`\{[^}]+\}`)

// DisplayStyle (Stage 23.8) passes the catalog's own "Toast"/"Page banner"
// display style straight through to the client so showApiError (public/app.js)
// can dispatch to the matching non-blocking primitive instead of always
// defaulting to the blocking modal - the two styles it actually dispatches on
// are generic enough to render with no per-call-site field context; every
// other style (Inline field message, Modal popup, ...) keeps the modal
// fallback since only the call site knows which field it belongs to.
//
// Detail and UserAction (Stage 30.2.4) are the two halves of the answer that
// the envelope used to throw away. The catalog's UserMessage is intentionally
// generic ("Tax configuration is missing for this transaction. Please contact
// administrator.") because one row covers many call sites; the engine that
// actually rejected the request knows the specific reason ("item
// 'QA-TSHIRT-01' is missing hsn_code"), and the catalog itself carries a
// populated UserAction on all 302 rows telling the user what to do next.
// Neither ever reached the client, so an admin was told to contact an
// administrator about an unnamed item. Both are optional and additive: any
// existing client that reads only `error` is unaffected.
type apiErrorBody struct {
	Error         string `json:"error"`
	Detail        string `json:"detail,omitempty"`
	UserAction    string `json:"user_action,omitempty"`
	Code          string `json:"code,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	Retryable     bool   `json:"retryable,omitempty"`
	DisplayStyle  string `json:"display_style,omitempty"`
}

// resolvedContext pulls the tenant/user/correlation context apiMiddleware
// already resolved onto the request (middleware.go:404-417), with the same
// "default" tenant fallback used elsewhere (e.g. the panic handler) for the
// few call sites that can fire before that context is fully populated.
func resolvedContext(r *http.Request) (tenantID, userID, correlationID string) {
	tenantID = r.Header.Get("Resolved-Tenant-ID")
	if tenantID == "" {
		tenantID = "default"
	}
	userID = r.Header.Get("Resolved-User-ID")
	correlationID = r.Header.Get("Resolved-Correlation-ID")
	return
}

// logForEntry fires the existing audit/system-error logging hooks
// (engines/logs.go) per the catalog row's Log Required/Audit Required
// flags, prefixing the message with the code so it stays grep-able in the
// existing log tables without a schema change.
func logForEntry(r *http.Request, entry CatalogEntry, message string) {
	if !entry.LogRequired && !entry.AuditRequired {
		return
	}
	tenantID, userID, correlationID := resolvedContext(r)
	tagged := "[" + entry.Code + "] " + message
	if entry.LogRequired {
		engines.LogSystemError(tenantID, correlationID, entry.Severity, entry.Module, tagged, "")
	}
	if entry.AuditRequired {
		engines.LogAuditEvent(tenantID, userID, entry.Code, "Error", tagged)
	}
}

// writeResponse is the single place every API error response body gets
// written from, so the JSON shape can never drift between call sites.
func writeResponse(w http.ResponseWriter, status int, body apiErrorBody) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeAPIError writes a standardized error response for a precise
// errorCatalog code. subFor, if non-empty, replaces the single {placeholder}
// the catalog message contains (e.g. "{field name}" -> "Vendor GSTIN");
// pass "" when the message has no placeholder or the generic wording is
// fine as-is.
func writeAPIError(w http.ResponseWriter, r *http.Request, code string, subFor string) {
	writeAPIErrorDetail(w, r, code, subFor, "")
}

// writeAPIErrorDetail is writeAPIError plus the specific reason the request
// was rejected (Stage 30.2.4). The catalog's UserMessage stays the headline -
// it is the curated, translatable, one-row-covers-many-call-sites text - and
// detail is the engine's own precise explanation shown underneath it, e.g.
// "item 'QA-TSHIRT-01' is missing hsn_code - required before it can be sold or
// purchased" under "Tax configuration is missing for this transaction."
//
// Pass a detail only for text that is safe to show a user: a business-rule
// explanation, never a raw driver/SQL error (see writeAPIErrorGeneric's own
// note on why 5xx internals stay server-side).
func writeAPIErrorDetail(w http.ResponseWriter, r *http.Request, code string, subFor string, detail string) {
	entry, ok := errorCatalog[code]
	if !ok {
		// Programmer error (typo'd code) - fail loudly in a way that's still
		// a valid, consistent envelope rather than panicking mid-response.
		writeResponse(w, http.StatusInternalServerError, apiErrorBody{
			Error: "Unexpected error occurred.",
			Code:  "GLOBAL-0302",
		})
		return
	}

	message := entry.UserMessage
	if subFor != "" {
		message = placeholderRe.ReplaceAllString(message, subFor)
	}

	// Log the detail when there is one - it is strictly more informative than
	// the generic headline the log used to record.
	logged := message
	if detail != "" {
		logged = message + " | " + detail
	}
	logForEntry(r, entry, logged)

	// A detail identical to the headline adds a duplicated line to every
	// dialog and no information.
	if detail == message {
		detail = ""
	}

	_, _, correlationID := resolvedContext(r)
	writeResponse(w, entry.HTTPStatus, apiErrorBody{
		Error:         message,
		Detail:        detail,
		UserAction:    entry.UserAction,
		Code:          entry.Code,
		CorrelationID: correlationID,
		Retryable:     entry.Retryable,
		DisplayStyle:  entry.DisplayStyle,
	})
}

// writeEngineError (Stage 25 Batch 3) is the same *engines.ValidationError
// type-assert dance handlers_core_doc_engine.go's generic doc POST path
// already does inline in three places - this gives the action-style
// handlers in handlers_operations.go/handlers_pim_pos_finance.go (which
// call a single-purpose engine function rather than going through the
// generic doc engine) the same one-liner instead of duplicating the
// assertion at every call site. fallbackStatus is used when err isn't a
// precisely-coded *engines.ValidationError.
func writeEngineError(w http.ResponseWriter, r *http.Request, err error, fallbackStatus int) {
	if verr, ok := err.(*engines.ValidationError); ok && verr.Code != "" {
		// Stage 30.2.4: verr.Message - the engine's own specific reason - used
		// to be discarded here in favour of the catalog's generic wording. It
		// now rides along as `detail`. These messages are written by this
		// codebase's own validators for a user to read, so they are safe to
		// show (unlike a raw 5xx internal error).
		writeAPIErrorDetail(w, r, verr.Code, verr.SubFor, verr.Message)
		return
	}
	writeAPIErrorGeneric(w, r, fallbackStatus, err.Error())
}

// writeAPIErrorGeneric writes a standardized error envelope for a call site
// that has no precise catalog scenario match yet. It keeps the caller's own
// message text verbatim and only attaches the nearest generic code (by HTTP
// status) so every response is at least consistently shaped and coded -
// EXCEPT for 5xx, where the caller's message is almost always a raw
// err.Error() from an internal failure (DB driver error, JSON marshal
// failure, ...), not curated user-facing text (unlike a 4xx call site's own
// message, e.g. "Field 'location' is required", which is deliberate and
// safe to show as-is).
//
// 20.6 (found via messy-data stress testing, 2026-07-25): this surfaced for
// real, not just in theory - a Unicode name (CJK/emoji) hits a Postgres
// encoding-mismatch error whose raw message
// ("character with byte sequence 0xe4 0xbd 0xa0 ... has no equivalent in
// encoding WIN1252") was going straight to the client. Past the specific
// info-leak/confusion risk, showing internal driver/schema detail to an end
// user on every unexpected 500 was already a real gap this generic-message
// fallback should have closed from the start. GLOBAL-0302's catalog message
// is shown instead; the raw message is still captured (LogRequired=true on
// that entry, via logForEntry below) so it's fully retrievable server-side
// by the correlation_id shown to the user - nothing is lost, only what's
// shown externally changes.
func writeAPIErrorGeneric(w http.ResponseWriter, r *http.Request, status int, message string) {
	code, ok := genericCodeForStatus[status]
	if !ok {
		// GLOBAL-0302 ("Unexpected server error") only fits an unmapped 5xx -
		// an unmapped 4xx (e.g. 405 Method Not Allowed) is a client-side
		// rejection, not a server failure, so it falls back to the generic
		// "Invalid format" bucket instead.
		if status >= 500 {
			code = "GLOBAL-0302"
		} else {
			code = "GLOBAL-0002"
		}
	}
	entry := errorCatalog[code]

	logForEntry(r, entry, message)

	responseMessage := message
	if status >= 500 && entry.UserMessage != "" {
		responseMessage = entry.UserMessage
	}

	_, _, correlationID := resolvedContext(r)
	writeResponse(w, status, apiErrorBody{
		Error: responseMessage,
		// Stage 30.2.4: the catalog's UserAction is populated on all 302 rows
		// and had never been sent to a client. No `detail` here on purpose -
		// on a 4xx the caller's own specific message is already the headline,
		// and on a 5xx the raw internal message is deliberately withheld.
		UserAction:    entry.UserAction,
		Code:          code,
		CorrelationID: correlationID,
		Retryable:     entry.Retryable,
		DisplayStyle:  entry.DisplayStyle,
	})
}
