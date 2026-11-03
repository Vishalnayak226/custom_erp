package engines

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"custom_erp/db"
)

// Stage 49.11.7: privileged sessions. A sign-in by a full-access user leaves
// one small row keyed by the token's jti. The list shows who is signed in
// with that power and from where, and an administrator can end one session
// (the middleware refuses its token at the next request) without signing the
// person out everywhere.

const (
	privilegedSessionRetentionDays = 30
	privilegedSessionListLimit     = 200
	privilegedSessionTouchEvery    = time.Minute
	privilegedSessionCacheMax      = 2000
)

// PrivilegedSession is one row of the list.
type PrivilegedSession struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Username   string     `json:"username"`
	Role       string     `json:"role"`
	StartedAt  time.Time  `json:"started_at"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Address    string     `json:"address"`
	Device     string     `json:"device"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	EndedBy    string     `json:"ended_by,omitempty"`
	State      string     `json:"state"`
}

type privSessionEntry struct {
	ended   bool
	checked time.Time
	touched time.Time
}

var (
	privSessionMu    sync.Mutex
	privSessionCache = map[string]privSessionEntry{}
)

func privSessionKey(tenantID, jti string) string { return tenantID + "|" + jti }

func clipText(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// RecordPrivilegedSession stores the session a sign-in just opened. Only a
// full-access role is recorded; any other call is a no-op. A failure never
// blocks the sign-in.
func RecordPrivilegedSession(ctx context.Context, tenantID, userID, username, role, token string, credentialVersion int) {
	if !HasFullAccess(role) {
		return
	}
	claims, err := ParseToken(token)
	if err != nil || claims["jti"] == "" {
		return
	}
	exp := time.Now().Add(24 * time.Hour)
	if secs, e := strconv.ParseInt(claims["exp"], 10, 64); e == nil && secs > 0 {
		exp = time.Unix(secs, 0)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	src := RequestSourceFromContext(ctx)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.privileged_sessions
		(id, user_id, username, role, expires_at, address, device, credential_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (id) DO NOTHING`, schema),
		claims["jti"], userID, username, role, exp, clipText(src.Address, 64), clipText(src.Device, 200), credentialVersion); err != nil {
		LogBackgroundJobError("PRIVILEGED-SESSION", "record", schema, err)
	}
}

// PrivilegedSessionEnded reports whether an administrator ended this session.
// The answer is cached for the live-state window, so a request costs a map
// lookup; last_seen_at is touched at most once a minute per session.
func PrivilegedSessionEnded(tenantID, jti string) bool {
	if jti == "" {
		return false
	}
	key := privSessionKey(tenantID, jti)
	now := time.Now()
	privSessionMu.Lock()
	e, ok := privSessionCache[key]
	privSessionMu.Unlock()
	if ok && now.Sub(e.checked) < authStateCacheTTL() {
		return e.ended
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var endedAt *time.Time
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT ended_at FROM %s.privileged_sessions WHERE id = $1`, schema), jti).Scan(&endedAt); err != nil {
		endedAt = nil // no row (an older session, or pruned): nothing to enforce
	}
	e.ended, e.checked = endedAt != nil, now
	if !e.ended && now.Sub(e.touched) >= privilegedSessionTouchEvery {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.privileged_sessions SET last_seen_at = now() WHERE id = $1`, schema), jti)
		e.touched = now
	}
	privSessionMu.Lock()
	if len(privSessionCache) >= privilegedSessionCacheMax {
		privSessionCache = map[string]privSessionEntry{}
	}
	privSessionCache[key] = e
	privSessionMu.Unlock()
	return e.ended
}

// EndPrivilegedSession ends one session. Its token is refused from now on.
func EndPrivilegedSession(tenantID, id, actor, reason string) error {
	reason, err := emergencyReason(reason)
	if err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var username, address string
	err = db.DB.QueryRow(fmt.Sprintf(`UPDATE %s.privileged_sessions SET ended_at = now(), ended_by = $2
		WHERE id = $1 AND ended_at IS NULL RETURNING username, address`, schema), id, actor).Scan(&username, &address)
	if err != nil {
		return &ValidationError{Code: "GLOBAL-0002", Message: "That session is not open."}
	}
	privSessionMu.Lock()
	privSessionCache[privSessionKey(tenantID, id)] = privSessionEntry{ended: true, checked: time.Now()}
	privSessionMu.Unlock()
	LogAuditEvent(tenantID, actor, "USER_MANAGEMENT", "SESSION_ENDED",
		fmt.Sprintf("One session of %s (from %s) ended. Reason: %s", username, address, reason))
	return nil
}

// ListPrivilegedSessions returns the newest sessions with their state:
// Active, Ended (by an administrator), Signed out (the user's credentials
// changed, or sign out everywhere ran) or Expired.
func ListPrivilegedSessions(tenantID string) ([]PrivilegedSession, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT s.id, s.user_id, s.username, s.role, s.started_at, s.last_seen_at,
		s.expires_at, s.address, s.device, s.ended_at, s.ended_by, s.credential_version, COALESCE(u.credential_version, -1)
		FROM %[1]s.privileged_sessions s LEFT JOIN %[1]s.users u ON u.id::text = s.user_id
		ORDER BY s.started_at DESC LIMIT %[2]d`, schema, privilegedSessionListLimit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PrivilegedSession{}
	now := time.Now()
	for rows.Next() {
		var s PrivilegedSession
		var rowCV, userCV int
		if err := rows.Scan(&s.ID, &s.UserID, &s.Username, &s.Role, &s.StartedAt, &s.LastSeenAt, &s.ExpiresAt,
			&s.Address, &s.Device, &s.EndedAt, &s.EndedBy, &rowCV, &userCV); err != nil {
			return nil, err
		}
		switch {
		case s.EndedAt != nil:
			s.State = "Ended"
		case rowCV != userCV:
			s.State = "Signed out"
		case now.After(s.ExpiresAt):
			s.State = "Expired"
		default:
			s.State = "Active"
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SweepPrivilegedSessions drops rows older than the retention window.
func SweepPrivilegedSessions(tenantID string) (int64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	return retentionDelete(tenantID, schema, "privileged_sessions",
		fmt.Sprintf("started_at < now() - interval '%d days'", privilegedSessionRetentionDays))
}
