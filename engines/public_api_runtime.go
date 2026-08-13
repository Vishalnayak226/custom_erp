package engines

import (
	"context"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Stage 38.3 + 38.5 + 38.9 - the runtime spine under every public API route.
//
// One file because these are one mechanism seen from three angles. A public
// request is admitted (is this credential within its burst budget and its daily
// quota?), de-duplicated (has this exact request already been answered under
// this idempotency key?) and recorded (what did we do, for whom, and how long
// did it take?). The traffic log is also what makes the daily quota durable:
// counting rows since midnight means a process restart cannot hand an
// integration a fresh quota, which an in-memory counter alone would.
//
// Nothing here is reachable from the internal /api/v1 surface. Human sessions
// keep their existing per-category IP+session limiter; an integration key gets
// its own budget so the two can never draw each other down.

// publicAPIBurstLimiter is the in-process sliding window behind the per-minute
// budget. A minute-scale burst check does not need durability - the daily quota
// below is the limit that must survive a restart, and it does.
type publicAPIBurstLimiter struct {
	mu      sync.Mutex
	windows map[string][]time.Time
}

var publicAPIBurst = &publicAPIBurstLimiter{windows: make(map[string][]time.Time)}

func (l *publicAPIBurstLimiter) allow(key string, limit int, window time.Duration) (allowed bool, remaining int, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-window)
	kept := l.windows[key][:0]
	for _, stamp := range l.windows[key] {
		if stamp.After(cutoff) {
			kept = append(kept, stamp)
		}
	}
	if len(kept) >= limit {
		l.windows[key] = kept
		return false, 0, time.Until(kept[0].Add(window))
	}
	l.windows[key] = append(kept, now)
	return true, limit - len(kept) - 1, 0
}

// forget drops one key's window. Used when a credential is revoked or rotated
// so a replacement key does not inherit its predecessor's spent burst.
func (l *publicAPIBurstLimiter) forget(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.windows, key)
}

// ResetPublicAPICredentialBudget clears the in-process burst window for one
// credential id. Revocation and rotation call it; nothing else should.
func ResetPublicAPICredentialBudget(credentialID string) {
	publicAPIBurst.forget(credentialID)
}

// PublicAPIBudget is one credential's effective limits and current consumption,
// as the middleware and the admin screen both see it.
type PublicAPIBudget struct {
	RateLimitPerMinute int `json:"rate_limit_per_minute"`
	RateRemaining      int `json:"rate_remaining"`
	DailyQuota         int `json:"daily_quota"`
	DailyUsed          int `json:"daily_used"`
	DailyRemaining     int `json:"daily_remaining"`
}

var (
	// ErrPublicAPIRateLimited and ErrPublicAPIQuotaExceeded are distinguished so
	// the caller can be told which one it hit - "slow down" and "you are done
	// for today" need different client behaviour, and collapsing them into one
	// 429 sends an integration into a pointless retry loop at midnight-minus-one.
	ErrPublicAPIRateLimited   = errors.New("public API rate limit exceeded")
	ErrPublicAPIQuotaExceeded = errors.New("public API daily quota exceeded")
)

func publicAPIDefaultRateLimit(tenantID string) int {
	if value := GetSettingInt(tenantID, "platform.public_api_rate_limit_per_minute"); value > 0 {
		return value
	}
	return 120
}

func publicAPIDefaultDailyQuota(tenantID string) int {
	if value := GetSettingInt(tenantID, "platform.public_api_daily_quota"); value > 0 {
		return value
	}
	return 50000
}

// PublicAPIUnadmittedOutcomes names every traffic-log outcome that was rejected
// BEFORE the request consumed quota. The in-memory counter only advances on
// admission, so the database seed must exclude exactly these or a credential
// that was rate limited this morning would find its quota mysteriously smaller
// after a restart. The two rules live next to each other for that reason.
var PublicAPIUnadmittedOutcomes = []string{
	"auth_failed", "auth_error", "scope_denied", "rate_limited", "quota_exceeded", "admit_error",
}

// dailyUsageCache avoids one COUNT(*) per request. The count is read from the
// traffic log once per credential per day and then advanced in memory; a
// restart simply re-reads it, which is the property an in-memory-only counter
// could not give us.
type dailyUsageCache struct {
	mu     sync.Mutex
	day    map[string]string
	used   map[string]int
	loaded map[string]bool
}

var publicAPIDailyUsage = &dailyUsageCache{
	day: make(map[string]string), used: make(map[string]int), loaded: make(map[string]bool),
}

func (c *dailyUsageCache) currentUsed(schema, credentialID string) (int, error) {
	today := time.Now().Format("2006-01-02")
	c.mu.Lock()
	if c.day[credentialID] != today {
		c.day[credentialID] = today
		c.loaded[credentialID] = false
		c.used[credentialID] = 0
	}
	if c.loaded[credentialID] {
		used := c.used[credentialID]
		c.mu.Unlock()
		return used, nil
	}
	c.mu.Unlock()

	var counted int
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.api_request_log
		WHERE credential_id = $1 AND created_at >= date_trunc('day', CURRENT_TIMESTAMP)
		  AND outcome <> ALL($2::text[])`, schema), credentialID, pqTextArray(PublicAPIUnadmittedOutcomes)).Scan(&counted)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Only seed if another goroutine has not already loaded and advanced the
	// counter, or this read would roll back their increments.
	if !c.loaded[credentialID] && c.day[credentialID] == today {
		c.used[credentialID] = counted
		c.loaded[credentialID] = true
	}
	return c.used[credentialID], nil
}

func (c *dailyUsageCache) increment(credentialID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded[credentialID] {
		c.used[credentialID]++
	}
}

func (c *dailyUsageCache) forget(credentialID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.day, credentialID)
	delete(c.used, credentialID)
	delete(c.loaded, credentialID)
}

// ResolvePublicAPICredentialLimits reads a credential's effective per-minute
// and per-day budgets, falling back to the tenant settings when the credential
// carries no override.
func ResolvePublicAPICredentialLimits(tenantID, credentialID string) (perMinute, perDay int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}
	var rateOverride, quotaOverride sql.NullInt64
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT rate_limit_per_minute, daily_quota
		FROM %s.api_credentials WHERE id = $1`, schema), credentialID).Scan(&rateOverride, &quotaOverride)
	if err != nil {
		return 0, 0, err
	}
	perMinute = publicAPIDefaultRateLimit(tenantID)
	if rateOverride.Valid && rateOverride.Int64 > 0 {
		perMinute = int(rateOverride.Int64)
	}
	perDay = publicAPIDefaultDailyQuota(tenantID)
	if quotaOverride.Valid && quotaOverride.Int64 > 0 {
		perDay = int(quotaOverride.Int64)
	}
	return perMinute, perDay, nil
}

// SetPublicAPICredentialLimits stores per-credential overrides. A nil value
// clears the override and returns that credential to the tenant default.
func SetPublicAPICredentialLimits(tenantID, credentialID string, perMinute, perDay *int) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if perMinute != nil && *perMinute <= 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Rate Limit", Message: "requests per minute must be greater than zero"}
	}
	if perDay != nil && *perDay <= 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Daily Quota", Message: "the daily quota must be greater than zero"}
	}
	var rate, quota interface{}
	if perMinute != nil {
		rate = *perMinute
	}
	if perDay != nil {
		quota = *perDay
	}
	result, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials
		SET rate_limit_per_minute = $2, daily_quota = $3 WHERE id = $1 AND status = 'Active'`, schema),
		credentialID, rate, quota)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return &ValidationError{Code: "GLOBAL-0004", Message: "active API credential not found"}
	}
	ResetPublicAPICredentialBudget(credentialID)
	return nil
}

// AdmitPublicAPIRequest is the single admission gate. It returns the budget it
// applied so the middleware can publish X-RateLimit-*/X-Quota-* headers on
// every response, allowed or not - an integration that can only discover its
// limits by being rejected will keep being rejected.
func AdmitPublicAPIRequest(tenantID, credentialID string) (*PublicAPIBudget, time.Duration, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, 0, err
	}
	perMinute, perDay, err := ResolvePublicAPICredentialLimits(tenantID, credentialID)
	if err != nil {
		return nil, 0, err
	}
	used, err := publicAPIDailyUsage.currentUsed(schema, credentialID)
	if err != nil {
		return nil, 0, err
	}
	budget := &PublicAPIBudget{
		RateLimitPerMinute: perMinute, DailyQuota: perDay,
		DailyUsed: used, DailyRemaining: maxInt(perDay-used, 0),
	}
	if used >= perDay {
		budget.RateRemaining = 0
		// Retry when the day rolls over, not in a minute - see the two-error
		// note above.
		return budget, time.Until(startOfNextDay()), ErrPublicAPIQuotaExceeded
	}
	allowed, remaining, retryAfter := publicAPIBurst.allow(credentialID, perMinute, time.Minute)
	budget.RateRemaining = remaining
	if !allowed {
		return budget, retryAfter, ErrPublicAPIRateLimited
	}
	// The request is admitted, so it will be logged and must count against the
	// quota whatever its outcome. Counting here rather than at log time keeps
	// concurrent requests from all passing the same check.
	publicAPIDailyUsage.increment(credentialID)
	budget.DailyUsed = used + 1
	budget.DailyRemaining = maxInt(perDay-budget.DailyUsed, 0)
	return budget, 0, nil
}

func startOfNextDay() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(24 * time.Hour)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// PublicAPILogEntry is one recorded call. Deliberately metadata only - no
// request or response body is stored, so support and capacity questions can be
// answered without the log becoming a copy of customer payloads.
type PublicAPILogEntry struct {
	ID             int64     `json:"id"`
	CredentialID   string    `json:"credential_id,omitempty"`
	KeyPrefix      string    `json:"key_prefix,omitempty"`
	Method         string    `json:"method"`
	Path           string    `json:"path"`
	RequiredScope  string    `json:"required_scope,omitempty"`
	StatusCode     int       `json:"status_code"`
	DurationMS     int       `json:"duration_ms"`
	CorrelationID  string    `json:"correlation_id,omitempty"`
	ClientIP       string    `json:"client_ip,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	Outcome        string    `json:"outcome"`
	CreatedAt      time.Time `json:"created_at"`
}

// RecordPublicAPIRequest appends one traffic row. It never returns an error to
// the request path: an observability write must not be able to fail a call that
// already succeeded, so a failure is logged through the existing system-error
// channel and the response stands.
func RecordPublicAPIRequest(tenantID string, entry PublicAPILogEntry) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		LogSystemError(tenantID, entry.CorrelationID, "Error", entry.Path, "public API traffic log: "+err.Error(), "")
		return
	}
	var credentialID interface{}
	if strings.TrimSpace(entry.CredentialID) != "" {
		credentialID = entry.CredentialID
	}
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.api_request_log
		(credential_id, key_prefix, method, path, required_scope, status_code, duration_ms,
		 correlation_id, client_ip, idempotency_key, outcome)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, schema),
		credentialID, nullIfEmpty(entry.KeyPrefix), entry.Method, truncateForLog(entry.Path, 512),
		nullIfEmpty(entry.RequiredScope), entry.StatusCode, entry.DurationMS,
		nullIfEmpty(entry.CorrelationID), nullIfEmpty(entry.ClientIP),
		nullIfEmpty(truncateForLog(entry.IdempotencyKey, 255)), entry.Outcome)
	if err != nil {
		LogSystemError(tenantID, entry.CorrelationID, "Error", entry.Path, "public API traffic log: "+err.Error(), "")
	}
}

func nullIfEmpty(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func truncateForLog(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

// ListPublicAPITraffic returns the most recent calls, optionally for one
// credential. Bounded by design: this is an operator's "what just happened"
// view, not an export path.
func ListPublicAPITraffic(tenantID, credentialID string, limit int) ([]PublicAPILogEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	where, args := "", []interface{}{}
	if strings.TrimSpace(credentialID) != "" {
		where = " WHERE credential_id = $1"
		args = append(args, credentialID)
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(credential_id::text,''), COALESCE(key_prefix,''),
		method, path, COALESCE(required_scope,''), status_code, duration_ms, COALESCE(correlation_id,''),
		COALESCE(client_ip,''), COALESCE(idempotency_key,''), outcome, created_at
		FROM %s.api_request_log%s ORDER BY created_at DESC, id DESC LIMIT %d`, schema, where, limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PublicAPILogEntry{}
	for rows.Next() {
		var entry PublicAPILogEntry
		if err := rows.Scan(&entry.ID, &entry.CredentialID, &entry.KeyPrefix, &entry.Method, &entry.Path,
			&entry.RequiredScope, &entry.StatusCode, &entry.DurationMS, &entry.CorrelationID,
			&entry.ClientIP, &entry.IdempotencyKey, &entry.Outcome, &entry.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

// --- 38.5: idempotency ---------------------------------------------------

// PublicAPIIdempotentReplay is a previously stored response being served again.
type PublicAPIIdempotentReplay struct {
	StatusCode int
	Body       string
}

var (
	// ErrIdempotencyKeyRequired is returned for a mutating public call with no
	// Idempotency-Key header. Required, not optional: a retry after a timeout is
	// the normal case for any network integration, and without a key the server
	// cannot tell a retry from a second genuine request.
	ErrIdempotencyKeyRequired = errors.New("idempotency key required")
	// ErrIdempotencyKeyReused means the same key arrived with a different
	// request. Replaying the stored response would answer a question the caller
	// did not ask, so this is an error rather than a replay.
	ErrIdempotencyKeyReused = errors.New("idempotency key reused with a different request")
	// ErrIdempotencyInProgress means an identical request is still running.
	ErrIdempotencyInProgress = errors.New("an identical request is still in progress")
)

func publicAPIRequestFingerprint(method, path string, body []byte) string {
	sum := sha256.New()
	sum.Write([]byte(strings.ToUpper(method)))
	sum.Write([]byte{0})
	sum.Write([]byte(path))
	sum.Write([]byte{0})
	sum.Write(body)
	return hex.EncodeToString(sum.Sum(nil))
}

// BeginIdempotentRequest claims an idempotency key for this request. The unique
// index is the lock: exactly one caller can insert a given (credential, key)
// pair, so two concurrent retries cannot both proceed no matter how they are
// scheduled. A returned replay means the work was already done and its response
// should be re-sent verbatim.
func BeginIdempotentRequest(tenantID, credentialID, key, method, path string, body []byte) (*PublicAPIIdempotentReplay, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	if len(key) > 255 {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Idempotency-Key", Message: "idempotency key cannot exceed 255 characters"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	fingerprint := publicAPIRequestFingerprint(method, path, body)
	retentionHours := GetSettingInt(tenantID, "platform.public_api_idempotency_retention_hours")
	if retentionHours <= 0 {
		retentionHours = 24
	}

	var claimed bool
	err = db.DB.QueryRow(fmt.Sprintf(`INSERT INTO %s.api_idempotency_keys
		(credential_id, idempotency_key, request_fingerprint, method, path)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (credential_id, idempotency_key) DO NOTHING
		RETURNING true`, schema), credentialID, key, fingerprint, strings.ToUpper(method), truncateForLog(path, 512)).Scan(&claimed)
	if err == nil && claimed {
		return nil, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// The key already exists. Decide between replay, conflict and in-progress.
	var storedFingerprint, state string
	var status sql.NullInt64
	var storedBody sql.NullString
	var createdAt time.Time
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT request_fingerprint, state, response_status, response_body, created_at
		FROM %s.api_idempotency_keys WHERE credential_id = $1 AND idempotency_key = $2`, schema), credentialID, key).
		Scan(&storedFingerprint, &state, &status, &storedBody, &createdAt)
	if err != nil {
		return nil, err
	}
	if time.Since(createdAt) > time.Duration(retentionHours)*time.Hour {
		// Past its retention window the key is no longer a promise about
		// anything. Reclaim it for this request rather than replaying a stale
		// response or refusing a key the caller may legitimately have recycled.
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_idempotency_keys
			SET request_fingerprint = $3, method = $4, path = $5, state = 'In Progress',
			    response_status = NULL, response_body = NULL, created_at = CURRENT_TIMESTAMP, completed_at = NULL
			WHERE credential_id = $1 AND idempotency_key = $2`, schema),
			credentialID, key, fingerprint, strings.ToUpper(method), truncateForLog(path, 512)); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if storedFingerprint != fingerprint {
		return nil, ErrIdempotencyKeyReused
	}
	if state != "Completed" {
		return nil, ErrIdempotencyInProgress
	}
	replay := &PublicAPIIdempotentReplay{StatusCode: int(status.Int64), Body: storedBody.String}
	if replay.StatusCode == 0 {
		replay.StatusCode = 200
	}
	return replay, nil
}

// CompleteIdempotentRequest stores the response so a retry can replay it. A
// server-error response is deliberately NOT stored - it is released instead, so
// a caller retrying after a 500 gets a real second attempt rather than a
// permanently cached failure.
func CompleteIdempotentRequest(tenantID, credentialID, key string, statusCode int, body string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if statusCode >= 500 {
		_, err = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.api_idempotency_keys
			WHERE credential_id = $1 AND idempotency_key = $2 AND state = 'In Progress'`, schema), credentialID, key)
		return err
	}
	const maxStoredBody = 256 << 10
	if len(body) > maxStoredBody {
		body = body[:maxStoredBody]
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_idempotency_keys
		SET state = 'Completed', response_status = $3, response_body = $4, completed_at = CURRENT_TIMESTAMP
		WHERE credential_id = $1 AND idempotency_key = $2`, schema), credentialID, key, statusCode, body)
	return err
}

// ReleaseIdempotentRequest drops an unfinished claim, so a request that never
// produced a response does not leave a key wedged in "In Progress" until its
// retention window expires.
func ReleaseIdempotentRequest(tenantID, credentialID, key string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.api_idempotency_keys
		WHERE credential_id = $1 AND idempotency_key = $2 AND state = 'In Progress'`, schema), credentialID, key)
}

// SweepPublicAPIRuntime deletes expired idempotency keys and traffic rows past
// their retention. Both tables are append-heavy and neither is a system of
// record, so unbounded growth is the only real risk they carry.
func SweepPublicAPIRuntime(tenantID string) (idempotencyDeleted, logDeleted int64, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}
	retentionHours := GetSettingInt(tenantID, "platform.public_api_idempotency_retention_hours")
	if retentionHours <= 0 {
		retentionHours = 24
	}
	retentionDays := GetSettingInt(tenantID, "platform.public_api_log_retention_days")
	if retentionDays <= 0 {
		retentionDays = 30
	}
	result, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.api_idempotency_keys
		WHERE created_at < CURRENT_TIMESTAMP - ($1 || ' hours')::interval`, schema), retentionHours)
	if err != nil {
		return 0, 0, err
	}
	idempotencyDeleted, _ = result.RowsAffected()
	result, err = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.api_request_log
		WHERE created_at < CURRENT_TIMESTAMP - ($1 || ' days')::interval`, schema), retentionDays)
	if err != nil {
		return idempotencyDeleted, 0, err
	}
	logDeleted, _ = result.RowsAffected()
	return idempotencyDeleted, logDeleted, nil
}

// StartPublicAPIRuntimeSweeper keeps the two append-only tables bounded. Hourly
// rather than daily because the idempotency retention window is measured in
// hours, and a key that has expired should stop replaying promptly rather than
// at the next midnight.
func StartPublicAPIRuntimeSweeper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[PUBLIC-API-SWEEP] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					tenantID, idErr := tenantIDForSchema(schema)
					if idErr != nil {
						continue
					}
					// A tenant whose database predates the Stage 38.3 migration
					// simply has no tables to sweep. Skipping quietly keeps the
					// worker from logging an error every hour on every schema
					// during the window between deploy and migrate.
					var tablesExist bool
					if err := db.DB.QueryRow(`SELECT to_regclass($1) IS NOT NULL AND to_regclass($2) IS NOT NULL`,
						schema+".api_idempotency_keys", schema+".api_request_log").Scan(&tablesExist); err != nil || !tablesExist {
						continue
					}
					if _, _, err := SweepPublicAPIRuntime(tenantID); err != nil {
						log.Printf("[PUBLIC-API-SWEEP] %s: %v", schema, err)
					}
				}
			}
		}
	}()
}
