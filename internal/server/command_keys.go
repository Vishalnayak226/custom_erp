package server

import (
	"bytes"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Stage 49.4.7a - the command key on the commands that create a new money or
// stock record each time they run: a journal voucher, a payment proposal's
// execution, a payroll run, a loan disbursement, a stock adjustment, a
// customer advance, a gift card issue, a loyalty or voucher redemption. A
// command that moves an existing record on (post, approve, cancel) is
// guarded by that record's status instead and is not wrapped here.
//
// It reuses the checkout's command key (engines.ClaimCommand, Stage 47.3.1)
// rather than a mechanism of its own: the caller sends an Idempotency-Key
// header (or an idempotency_key body field); the first request with a key
// runs and its answer is kept for 7 days; a retry with the same key and the
// same request answers that first result and creates nothing; the same key
// with a different request is refused (GLOBAL-4972); one still running
// answers GLOBAL-4971; a different key is a new command. A request with no
// key runs as before, so API clients that never sent one are unaffected.
//
// Registered in routes.go as commandKeyed("<command>", handler), which is also
// what the idempotency audit (docs/security/idempotency-audit.md) reads.
//
// The handler keeps its own transaction; the answer is recorded right after
// it (engines.CompleteCommand). A refused request (4xx) frees the key at once
// so the corrected request can reuse it; a failure (5xx) is remembered as
// failed and the retry runs again - neither committed anything.

// commandKeyMaxLen bounds a caller's key, like the checkout's.
const commandKeyMaxLen = 200

// commandKeyRecorder holds a handler's answer until the claim is settled, so
// what the caller receives and what a retry is replayed are the same bytes.
type commandKeyRecorder struct {
	header      http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (c *commandKeyRecorder) Header() http.Header { return c.header }

func (c *commandKeyRecorder) WriteHeader(status int) {
	if !c.wroteHeader {
		c.status, c.wroteHeader = status, true
	}
}

func (c *commandKeyRecorder) Write(b []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	return c.body.Write(b)
}

// commandKeyed wraps one command's handler with the command key.
func commandKeyed(command string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
			next(w, r)
			return
		}
		var raw []byte
		if r.Body != nil {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				writeAPIErrorDetail(w, r, "GLOBAL-0307", "", "The request body could not be read completely.")
				return
			}
			raw = b
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
		var body map[string]interface{}
		if len(bytes.TrimSpace(raw)) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			if s, ok := body["idempotency_key"].(string); ok {
				key = strings.TrimSpace(s)
			}
		}
		if key == "" {
			next(w, r)
			return
		}
		if len(key) > commandKeyMaxLen {
			writeAPIErrorDetail(w, r, "GLOBAL-0307", "", fmt.Sprintf("An idempotency key is at most %d characters.", commandKeyMaxLen))
			return
		}
		tenantID, userID, correlationID := resolvedContext(r)

		// The request the key stands for: the address (a proposal's id is in
		// it) and the body, without the body's own key - the screen may mint
		// that per press, while the header key is the command's identity.
		var payload interface{} = string(raw)
		if body != nil {
			trimmed := make(map[string]interface{}, len(body))
			for k, v := range body {
				if k != "idempotency_key" {
					trimmed[k] = v
				}
			}
			payload = trimmed
		}
		digest := engines.CommandDigest(map[string]interface{}{"method": r.Method, "path": r.URL.Path, "body": payload})

		// Keys are per user, as the checkout's are: two people cannot collide.
		claim, err := engines.ClaimCommand(tenantID, command, userID+":"+key, digest, correlationID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to claim this command")
			return
		}
		switch claim.Outcome {
		case engines.ClaimReplay:
			replayCommandResponse(w, claim.Response)
			return
		case engines.ClaimInProgress:
			writeAPIError(w, r, "GLOBAL-4971", "")
			return
		case engines.ClaimPayloadMismatch:
			writeAPIError(w, r, "GLOBAL-4972", "")
			return
		}

		rec := &commandKeyRecorder{header: w.Header(), status: http.StatusOK}
		settled := false
		defer func() {
			// A handler that panicked committed nothing it could answer for;
			// the claim is failed so the retry runs, and the panic goes on.
			if !settled {
				engines.FailCommand(tenantID, claim.Key, "the command did not finish")
			}
		}()
		next(rec, r)
		switch {
		case rec.status >= 200 && rec.status < 300:
			stored := map[string]interface{}{"status": rec.status}
			var parsed interface{}
			if err := json.Unmarshal(rec.body.Bytes(), &parsed); err == nil {
				stored["body"] = parsed
			} else {
				stored["text"] = rec.body.String()
			}
			if err := engines.CompleteCommand(tenantID, claim.Key, commandDocumentID(parsed), stored); err != nil {
				// The command committed; only its record did not. The key
				// stays "still being processed" until its lease ends rather
				// than letting an immediate retry run it a second time.
				engines.LogSystemError(tenantID, correlationID, "ERROR", "commandKeyed",
					fmt.Sprintf("command %s completed but its key %s was not recorded: %v", command, claim.Key, err), "")
			}
		case rec.status >= 500:
			engines.FailCommand(tenantID, claim.Key, fmt.Sprintf("HTTP %d", rec.status))
		default:
			engines.ReleaseCommand(tenantID, claim.Key)
		}
		settled = true
		w.WriteHeader(rec.status)
		_, _ = w.Write(rec.body.Bytes())
	}
}

// replayCommandResponse writes a kept answer again, marked as a replay.
func replayCommandResponse(w http.ResponseWriter, stored map[string]interface{}) {
	status := http.StatusOK
	if s, ok := stored["status"].(float64); ok && s >= 200 && s < 300 {
		status = int(s)
	}
	w.Header().Set("Idempotent-Replayed", "true")
	if text, ok := stored["text"].(string); ok {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(text))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(stored["body"])
}

// commandDocumentID names the record a command's answer reports, for the
// claim's document_id column: the first of id, number or payslip_id.
func commandDocumentID(parsed interface{}) string {
	m, ok := parsed.(map[string]interface{})
	if !ok {
		return ""
	}
	for _, k := range []string{"id", "number", "payslip_id", "voucher_id"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
