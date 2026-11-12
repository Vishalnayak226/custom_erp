package engines

import (
	"context"
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 49.11.7: the retention sweep drops old session rows and keeps recent
// ones; a session nobody ended is not reported ended.
func TestStage49117PrivilegedSessionSweepKeepsRecent(t *testing.T) {
	db.InitDB(testConnStr())
	user := "qa49117sweep"
	cleanup := func() { db.DB.Exec(`DELETE FROM tenant_default.privileged_sessions WHERE user_id = $1`, user) }
	cleanup()
	t.Cleanup(cleanup)
	ctx := context.Background()
	tok := SignToken(user, user, "Super Admin", "default", "HO", 1)
	RecordPrivilegedSession(ctx, "default", user, user, "Super Admin", tok, 1)
	old := SignToken(user, user, "Super Admin", "default", "HO", 1)
	RecordPrivilegedSession(ctx, "default", user, user, "Super Admin", old, 1)
	oldID := mustJTI(t, old)
	db.DB.Exec(`UPDATE tenant_default.privileged_sessions SET started_at = $2 WHERE id = $1`, oldID, time.Now().AddDate(0, 0, -(privilegedSessionRetentionDays+2)))
	if PrivilegedSessionEnded("default", mustJTI(t, tok)) {
		t.Fatal("a session nobody ended reads as ended")
	}
	if _, err := SweepPrivilegedSessions("default"); err != nil {
		t.Fatal(err)
	}
	var n int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.privileged_sessions WHERE user_id = $1`, user).Scan(&n)
	if n != 1 {
		t.Fatalf("expected the recent row to stay and the old one to go, %d left", n)
	}
}

func mustJTI(t *testing.T, token string) string {
	t.Helper()
	claims, err := ParseToken(token)
	if err != nil || claims["jti"] == "" {
		t.Fatalf("token has no jti: %v", err)
	}
	return claims["jti"]
}
