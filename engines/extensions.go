package engines

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// ExtensionHook mirrors one row of tenant_default.extension_hooks for API
// responses. Secret is deliberately never included here - it's returned
// once, at creation time, by RegisterExtensionHook's return value only.
type ExtensionHook struct {
	ID        string    `json:"id"`
	HookPoint string    `json:"hook_point"`
	Doctype   string    `json:"doctype"`
	TargetURL string    `json:"target_url"`
	Enabled   bool      `json:"enabled"`
	TimeoutMs int       `json:"timeout_ms"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// ExtensionHookLogEntry mirrors one row of tenant_default.extension_hook_log.
type ExtensionHookLogEntry struct {
	ID                 string    `json:"id"`
	HookID             string    `json:"hook_id"`
	RequestPayloadHash string    `json:"request_payload_hash"`
	ResponseStatus     *int      `json:"response_status,omitempty"`
	LatencyMs          int       `json:"latency_ms"`
	Error              *string   `json:"error,omitempty"`
	CalledAt           time.Time `json:"called_at"`
}

type extensionHookRow struct {
	ID        string
	TargetURL string
	Secret    string
	TimeoutMs int
}

// generateExtensionSecret returns a high-entropy HMAC signing secret for a
// new hook - same "generated + shown once" pattern generateRandomPassword
// already uses for tenant admin passwords, applied here to a different kind
// of one-time secret.
func generateExtensionSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// RegisterExtensionHook creates a new hook and returns its id + secret. The
// secret is never persisted in plaintext-retrievable form beyond this one
// return - callers must capture it now.
func RegisterExtensionHook(tenantID, hookPoint, doctype, targetURL string, timeoutMs int, createdBy string) (id string, secret string, err error) {
	if hookPoint != "document.before_save" && hookPoint != "document.after_save" {
		return "", "", fmt.Errorf("invalid hook_point %q: must be 'document.before_save' or 'document.after_save'", hookPoint)
	}
	if targetURL == "" {
		return "", "", fmt.Errorf("target_url is required")
	}
	if timeoutMs <= 0 || timeoutMs > 10000 {
		timeoutMs = 3000
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	secret, err = generateExtensionSecret()
	if err != nil {
		return "", "", err
	}
	query := fmt.Sprintf(`
		INSERT INTO %s.extension_hooks (hook_point, doctype, target_url, secret, timeout_ms, created_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, schema)
	err = db.DB.QueryRow(query, hookPoint, doctype, targetURL, secret, timeoutMs, createdBy).Scan(&id)
	if err != nil {
		return "", "", err
	}
	return id, secret, nil
}

// ListExtensionHooks returns every hook for a tenant, secrets excluded.
func ListExtensionHooks(tenantID string) ([]ExtensionHook, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, hook_point, doctype, target_url, enabled, timeout_ms, COALESCE(created_by, ''), created_at
		FROM %s.extension_hooks ORDER BY created_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExtensionHook
	for rows.Next() {
		var h ExtensionHook
		if err := rows.Scan(&h.ID, &h.HookPoint, &h.Doctype, &h.TargetURL, &h.Enabled, &h.TimeoutMs, &h.CreatedBy, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// DeleteExtensionHook removes a hook. Returns an error if no matching row existed.
func DeleteExtensionHook(tenantID, hookID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.extension_hooks WHERE id = $1`, schema), hookID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no extension hook found with id %s", hookID)
	}
	return nil
}

// GetExtensionHookLog returns the most recent call log entries for one hook.
func GetExtensionHookLog(tenantID, hookID string) ([]ExtensionHookLogEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, hook_id, request_payload_hash, response_status, latency_ms, error, called_at
		FROM %s.extension_hook_log WHERE hook_id = $1 ORDER BY called_at DESC LIMIT 100`, schema), hookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExtensionHookLogEntry
	for rows.Next() {
		var e ExtensionHookLogEntry
		var status sql.NullInt64
		var errMsg sql.NullString
		if err := rows.Scan(&e.ID, &e.HookID, &e.RequestPayloadHash, &status, &e.LatencyMs, &errMsg, &e.CalledAt); err != nil {
			return nil, err
		}
		if status.Valid {
			s := int(status.Int64)
			e.ResponseStatus = &s
		}
		if errMsg.Valid {
			e.Error = &errMsg.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func matchingHooks(tenantID, hookPoint, doctype string) ([]extensionHookRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, target_url, secret, timeout_ms FROM %s.extension_hooks
		WHERE hook_point = $1 AND enabled = TRUE AND (doctype = $2 OR doctype = '*')`, schema), hookPoint, doctype)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []extensionHookRow
	for rows.Next() {
		var h extensionHookRow
		if err := rows.Scan(&h.ID, &h.TargetURL, &h.Secret, &h.TimeoutMs); err == nil {
			out = append(out, h)
		}
	}
	return out, rows.Err()
}

func logHookCall(tenantID, hookID, payloadHash string, status int, latencyMs int, callErr error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	var errText sql.NullString
	if callErr != nil {
		errText = sql.NullString{String: callErr.Error(), Valid: true}
	}
	var statusVal sql.NullInt64
	if status > 0 {
		statusVal = sql.NullInt64{Int64: int64(status), Valid: true}
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.extension_hook_log (hook_id, request_payload_hash, response_status, latency_ms, error)
		VALUES ($1, $2, $3, $4, $5)`, schema), hookID, payloadHash, statusVal, latencyMs, errText)
}

// callHookWithRecovery POSTs an HMAC-signed payload to a hook's target URL.
// The actual HTTP call runs inside a goroutine with its own recover(), and
// the caller waits on a channel with a hard timeout margin beyond the
// http.Client's own timeout - so a hook that panics, hangs, or never
// responds can neither crash nor indefinitely block the calling request.
// This is what makes it safe to call synchronously from a before_save path
// that needs a real answer (proceed or block).
func callHookWithRecovery(hook extensionHookRow, payload []byte) (status int, latencyMs int, err error) {
	type result struct {
		status int
		err    error
	}
	resultCh := make(chan result, 1)
	start := time.Now()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				select {
				case resultCh <- result{0, fmt.Errorf("hook panicked: %v", r)}:
				default:
				}
			}
		}()
		sig := hmac.New(sha256.New, []byte(hook.Secret))
		sig.Write(payload)
		signature := hex.EncodeToString(sig.Sum(nil))

		client := &http.Client{Timeout: time.Duration(hook.TimeoutMs) * time.Millisecond}
		req, reqErr := http.NewRequest(http.MethodPost, hook.TargetURL, bytes.NewReader(payload))
		if reqErr != nil {
			resultCh <- result{0, reqErr}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Signature", "sha256="+signature)

		resp, doErr := client.Do(req)
		if doErr != nil {
			resultCh <- result{0, doErr}
			return
		}
		defer resp.Body.Close()
		resultCh <- result{resp.StatusCode, nil}
	}()

	select {
	case res := <-resultCh:
		return res.status, int(time.Since(start).Milliseconds()), res.err
	case <-time.After(time.Duration(hook.TimeoutMs+1000) * time.Millisecond):
		// Safety margin over the HTTP client's own timeout - this branch
		// should be unreachable in practice (the client should always time
		// itself out first) but guarantees the caller is never blocked
		// forever regardless of what a misbehaving hook does.
		return 0, int(time.Since(start).Milliseconds()), fmt.Errorf("hook call exceeded safety timeout")
	}
}

func hashPayload(payload []byte) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])
}

func buildHookPayload(tenantID, hookPoint, doctype, documentID string, data map[string]interface{}) []byte {
	payload, _ := json.Marshal(map[string]interface{}{
		"hook_point":  hookPoint,
		"doctype":     doctype,
		"document_id": documentID,
		"tenant_id":   tenantID,
		"data":        data,
	})
	return payload
}

// InvokeBeforeSaveHooks calls every enabled document.before_save hook
// matching this doctype, synchronously, in registration order. The first
// hook that errors or returns a non-2xx status blocks the save (returns a
// non-nil error) - a pricing or validation hook that doesn't run must not
// let the save silently proceed with an unreviewed value. No hooks matching
// is the overwhelmingly common case and returns immediately with no
// network call.
func InvokeBeforeSaveHooks(tenantID, doctype, documentID string, data map[string]interface{}) error {
	hooks, err := matchingHooks(tenantID, "document.before_save", doctype)
	if err != nil || len(hooks) == 0 {
		return nil
	}
	payload := buildHookPayload(tenantID, "document.before_save", doctype, documentID, data)
	payloadHash := hashPayload(payload)

	for _, hook := range hooks {
		status, latencyMs, callErr := callHookWithRecovery(hook, payload)
		logHookCall(tenantID, hook.ID, payloadHash, status, latencyMs, callErr)
		if callErr != nil {
			return fmt.Errorf("before_save hook failed: %v", callErr)
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("before_save hook rejected the save (status %d)", status)
		}
	}
	return nil
}

// InvokeAfterSaveHooksAsync fires every enabled document.after_save hook
// matching this doctype in the background - the caller (handleGenericDoc)
// does not wait for these, matching the design: a notification/sync hook
// can't roll back an already-valid save, so there's nothing useful for the
// HTTP response to wait on. Each call still goes through the same
// recover()-wrapped, timeout-bounded path as before_save for process safety.
func InvokeAfterSaveHooksAsync(tenantID, doctype, documentID string, data map[string]interface{}) {
	hooks, err := matchingHooks(tenantID, "document.after_save", doctype)
	if err != nil || len(hooks) == 0 {
		return
	}
	payload := buildHookPayload(tenantID, "document.after_save", doctype, documentID, data)
	payloadHash := hashPayload(payload)

	for _, hook := range hooks {
		hook := hook
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[EXTENSIONS] after_save hook %s panicked outside callHookWithRecovery: %v", hook.ID, r)
				}
			}()
			status, latencyMs, callErr := callHookWithRecovery(hook, payload)
			logHookCall(tenantID, hook.ID, payloadHash, status, latencyMs, callErr)
			if callErr != nil {
				log.Printf("[EXTENSIONS] after_save hook %s failed (logged, save already committed): %v", hook.ID, callErr)
			}
		}()
	}
}
