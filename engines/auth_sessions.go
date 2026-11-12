package engines

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"sync"
	"time"

	"custom_erp/db"
)

// 162.15 (session-topology ADR, Option B step 1): server-side sessions. Every
// full session token already carries a jti; apiMiddleware keeps one row per
// jti in user_sessions, so:
//   - signing out ends that session on the server, not only in the browser -
//     a copied token stops working;
//   - when the client sets security.session_idle_minutes, a session nobody
//     used for longer is ended, whoever holds the token;
//   - the absolute limit stays the token's own expiry (session_token_ttl_hours).
// The row is added the first time the middleware sees a session, so every
// place that issues a token is covered without each one recording it.
// Off by default: with the idle limit at 0 the only change is a real sign-out.

const (
	SettingSessionIdleMinutes = "security.session_idle_minutes"
	userSessionTouchEvery     = time.Minute
	userSessionCacheMax       = 10000
	userSessionGraceAfterExp  = 24 * time.Hour
)

// TokenExpiry is a parsed token's exp claim, or a day from now when it has none.
func TokenExpiry(claims map[string]string) time.Time {
	if secs, err := strconv.ParseInt(claims["exp"], 10, 64); err == nil && secs > 0 {
		return time.Unix(secs, 0)
	}
	return time.Now().Add(userSessionGraceAfterExp)
}

type userSessionEntry struct {
	ended    bool
	lastSeen time.Time // the last request this server saw; the row lags it by up to a minute
	checked  time.Time
	touched  time.Time
}

var (
	userSessionMu    sync.Mutex
	userSessionCache = map[string]userSessionEntry{}
)

// UserSessionRefused reports whether this session may not be used: it was
// signed out or ended, or - only when the client set an idle limit - it went
// unused for longer than that. The request itself counts as use. A request
// costs a map lookup; the row is read once per live-state window and
// last_seen_at written at most once a minute. A database error never refuses
// a session (the token's own checks still apply).
func UserSessionRefused(ctx context.Context, tenantID, userID, jti string, expires time.Time) bool {
	if tenantID == "" || jti == "" {
		return false
	}
	key := privSessionKey(tenantID, jti)
	now := time.Now()
	userSessionMu.Lock()
	e, ok := userSessionCache[key]
	userSessionMu.Unlock()
	schema := ""
	if !ok || now.Sub(e.checked) >= authStateCacheTTL() {
		var err error
		if schema, err = db.GetTenantSchema(tenantID); err != nil {
			return false
		}
		var endedAt sql.NullTime
		var lastSeen time.Time
		err = db.DB.QueryRow(fmt.Sprintf(`SELECT ended_at, last_seen_at FROM %s.user_sessions WHERE id = $1`, schema), jti).Scan(&endedAt, &lastSeen)
		switch {
		case err == sql.ErrNoRows:
			src := RequestSourceFromContext(ctx)
			if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.user_sessions (id, user_id, expires_at, address, device)
				VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO NOTHING`, schema),
				jti, userID, expires, clipText(src.Address, 64), clipText(src.Device, 200)); err != nil {
				LogBackgroundJobError("USER-SESSION", "record", schema, err)
				return false
			}
			e = userSessionEntry{lastSeen: now, touched: now}
		case err != nil:
			return false
		default:
			e.ended = endedAt.Valid
			if lastSeen.After(e.lastSeen) {
				e.lastSeen = lastSeen
			}
		}
		e.checked = now
	}
	if !e.ended {
		if idle := GetSettingInt(tenantID, SettingSessionIdleMinutes); idle > 0 && now.Sub(e.lastSeen) > time.Duration(idle)*time.Minute {
			if schema == "" {
				schema, _ = db.GetTenantSchema(tenantID)
			}
			if schema != "" {
				_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.user_sessions SET ended_at = now(), ended_by = 'idle' WHERE id = $1 AND ended_at IS NULL`, schema), jti)
			}
			e.ended = true
		}
	}
	if !e.ended {
		e.lastSeen = now
		if now.Sub(e.touched) >= userSessionTouchEvery {
			if schema == "" {
				schema, _ = db.GetTenantSchema(tenantID)
			}
			if schema != "" {
				_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.user_sessions SET last_seen_at = now() WHERE id = $1`, schema), jti)
			}
			e.touched = now
		}
	}
	userSessionMu.Lock()
	if len(userSessionCache) >= userSessionCacheMax {
		userSessionCache = map[string]userSessionEntry{}
	}
	userSessionCache[key] = e
	userSessionMu.Unlock()
	return e.ended
}

// EndUserSession signs one session out: its token is refused from the next
// request on. It works for a session the middleware has not recorded yet, and
// a full-access session also shows as ended in the privileged-session list.
func EndUserSession(tenantID, userID, username, jti string, expires time.Time) error {
	if jti == "" {
		return &ValidationError{Code: "GLOBAL-0002", Message: "That session is not open."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.user_sessions (id, user_id, expires_at, ended_at, ended_by)
		VALUES ($1, $2, $3, now(), $4)
		ON CONFLICT (id) DO UPDATE SET ended_at = COALESCE(%[1]s.user_sessions.ended_at, now()),
			ended_by = CASE WHEN %[1]s.user_sessions.ended_at IS NULL THEN EXCLUDED.ended_by ELSE %[1]s.user_sessions.ended_by END`, schema),
		jti, userID, expires, clipText(username, 100)); err != nil {
		return err
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.privileged_sessions SET ended_at = now(), ended_by = $2 WHERE id = $1 AND ended_at IS NULL`, schema), jti, clipText(username, 100))
	now := time.Now()
	userSessionMu.Lock()
	userSessionCache[privSessionKey(tenantID, jti)] = userSessionEntry{ended: true, checked: now}
	userSessionMu.Unlock()
	privSessionMu.Lock()
	privSessionCache[privSessionKey(tenantID, jti)] = privSessionEntry{ended: true, checked: now}
	privSessionMu.Unlock()
	return nil
}

// SweepUserSessions drops rows a day after their token expired. Never sooner:
// a missing row reads as a live session, so a signed-out token must have
// expired before its row goes.
func SweepUserSessions(tenantID string) (int64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	return retentionDelete(tenantID, schema, "user_sessions",
		fmt.Sprintf("expires_at < now() - interval '%d hours'", int(userSessionGraceAfterExp/time.Hour)))
}
