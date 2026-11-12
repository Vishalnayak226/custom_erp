package engines

import (
	"context"
	"testing"
	"time"

	"custom_erp/db"
)

// TestUserSessionsSignOutAndIdle (162.15): the middleware's session check
// records a session the first time it sees it, refuses it once signed out,
// ends it after the client's idle limit (and only when one is set), and the
// sweep never drops the row of a signed-out token that has not expired.
func TestUserSessionsSignOutAndIdle(t *testing.T) {
	db.InitDB(testConnStr())
	user := "qa162_15_sessions"
	cleanup := func() {
		db.DB.Exec(`DELETE FROM tenant_default.user_sessions WHERE user_id = $1`, user)
		db.DB.Exec(`DELETE FROM tenant_default.system_settings WHERE key = $1`, SettingSessionIdleMinutes)
		settingsCacheMu.Lock()
		delete(settingsCache, settingsCacheKey("tenant_default", SettingSessionIdleMinutes))
		settingsCacheMu.Unlock()
	}
	cleanup()
	t.Cleanup(cleanup)
	if SettingIsOverridden("default", SettingSessionIdleMinutes) || GetSettingInt("default", SettingSessionIdleMinutes) != 0 {
		t.Fatal("the idle limit must be off unless a client sets it")
	}
	ctx := WithRequestSource(context.Background(), "203.0.113.7", "Mozilla/5.0 (Windows NT 10.0) Chrome/120.0 Safari/537.36")
	session := func() (string, time.Time) {
		claims, err := ParseToken(SignToken(user, user, "Cashier", "default", "HO", 1))
		if err != nil {
			t.Fatal(err)
		}
		return claims["jti"], TokenExpiry(claims)
	}
	forget := func(jti string) { // as after a restart: the next check reads the row
		userSessionMu.Lock()
		delete(userSessionCache, privSessionKey("default", jti))
		userSessionMu.Unlock()
	}
	age := func(jti string, d time.Duration) {
		db.DB.Exec(`UPDATE tenant_default.user_sessions SET last_seen_at = now() - make_interval(secs => $2) WHERE id = $1`, jti, d.Seconds())
		forget(jti)
	}

	// Recorded on first sight, with where it came from.
	a, expA := session()
	if UserSessionRefused(ctx, "default", user, a, expA) {
		t.Fatal("a new session was refused")
	}
	var address string
	if err := db.DB.QueryRow(`SELECT address FROM tenant_default.user_sessions WHERE id = $1`, a).Scan(&address); err != nil || address != "203.0.113.7" {
		t.Fatalf("the session row: %q %v", address, err)
	}

	// Idle limit off: a session unused for a day still works.
	age(a, 24*time.Hour)
	if UserSessionRefused(ctx, "default", user, a, expA) {
		t.Fatal("with the idle limit off an idle session was refused")
	}

	// Signing out ends this session only, and survives a restart.
	b, expB := session()
	UserSessionRefused(ctx, "default", user, b, expB)
	if err := EndUserSession("default", user, user, a, expA); err != nil {
		t.Fatal(err)
	}
	if !UserSessionRefused(ctx, "default", user, a, expA) {
		t.Fatal("a signed-out session still works")
	}
	forget(a)
	if !UserSessionRefused(ctx, "default", user, a, expA) {
		t.Fatal("a signed-out session works again after a restart")
	}
	if UserSessionRefused(ctx, "default", user, b, expB) {
		t.Fatal("signing out one session ended the user's other session")
	}
	// A session the middleware never saw can be signed out too.
	c, expC := session()
	if err := EndUserSession("default", user, user, c, expC); err != nil || !UserSessionRefused(ctx, "default", user, c, expC) {
		t.Fatalf("signing out an unrecorded session: %v", err)
	}

	// Idle limit on: past it the session ends; within it, it carries on.
	if err := SetSetting("default", SettingSessionIdleMinutes, "15", "qa"); err != nil {
		t.Fatal(err)
	}
	age(b, 10*time.Minute)
	if UserSessionRefused(ctx, "default", user, b, expB) {
		t.Fatal("a session used 10 minutes ago was refused under a 15-minute limit")
	}
	age(b, 20*time.Minute)
	if !UserSessionRefused(ctx, "default", user, b, expB) {
		t.Fatal("a session idle for 20 minutes still works under a 15-minute limit")
	}
	var by string
	db.DB.QueryRow(`SELECT ended_by FROM tenant_default.user_sessions WHERE id = $1`, b).Scan(&by)
	if by != "idle" {
		t.Errorf("the idle session ended by %q", by)
	}

	// The sweep drops a row only a day after its token expired.
	gone, _ := session()
	db.DB.Exec(`INSERT INTO tenant_default.user_sessions (id, user_id, expires_at) VALUES ($1, $2, now() - interval '2 days')`, gone, user)
	if _, err := SweepUserSessions("default"); err != nil {
		t.Fatal(err)
	}
	var left int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.user_sessions WHERE id = $1`, gone).Scan(&left)
	if left != 0 {
		t.Error("the row of a long-expired token was kept")
	}
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.user_sessions WHERE id = $1 AND ended_at IS NOT NULL`, a).Scan(&left)
	if left != 1 {
		t.Error("the sweep dropped a signed-out session whose token is still valid")
	}
}
