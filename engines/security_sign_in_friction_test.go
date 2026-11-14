package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
	"time"
)

// 49.17.4: the Security screen's sign-in friction counts the last 14 days'
// locks (account and PIN), password resets (asked for or by an
// administrator), lost authenticators (a recovery code used, or the
// two-factor reset) and "No" on the sign-in help articles - and nothing
// older, no other status and no other article's answer.
func TestStage49174SignInFrictionCountsTheLastFortnight(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.audit_logs WHERE action = 'T49174'`, schema))
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id LIKE 'T49174-%%'`, schema))
	}
	cleanup()
	t.Cleanup(cleanup)
	before := signInFriction(schema)

	now := time.Now().UTC()
	for _, r := range []struct {
		status string
		at     time.Time
	}{
		{"ACCOUNT_LOCKED", now}, {"PIN_LOCKED", now},
		{"PASSWORD_RESET_REQUESTED", now}, {"PASSWORD_RESET_BY_ADMIN", now},
		{"MFA_RECOVERY_CODE_USED", now},
		{"ACCOUNT_LOCKED", now.AddDate(0, 0, -20)}, // too old
		{"SUCCESS", now},                           // not friction
	} {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.audit_logs (user_id, action, status, details, created_at)
			VALUES ('t49174', 'T49174', $1, 'test', $2)`, schema), r.status, r.at); err != nil {
			t.Fatalf("seed %s: %v", r.status, err)
		}
	}
	for _, d := range []struct{ id, article, helpful string }{
		{"T49174-1", "first-login", "No"},
		{"T49174-2", "security-approvals", "No"},
		{"T49174-3", "first-login", "Yes"},
		{"T49174-4", "first-order", "No"},
	} {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, 'HelpArticleFeedback', jsonb_build_object('article', $2::text, 'helpful', $3::text), 'Active', 'admin')`, schema),
			d.id, d.article, d.helpful); err != nil {
			t.Fatalf("seed %s: %v", d.id, err)
		}
	}

	after := signInFriction(schema)
	got := SignInFriction{
		Locked:            after.Locked - before.Locked,
		PasswordResets:    after.PasswordResets - before.PasswordResets,
		LostAuthenticator: after.LostAuthenticator - before.LostAuthenticator,
		HelpNotHelpful:    after.HelpNotHelpful - before.HelpNotHelpful,
	}
	if want := (SignInFriction{Locked: 2, PasswordResets: 2, LostAuthenticator: 1, HelpNotHelpful: 2}); got != want {
		t.Fatalf("friction added %+v, want %+v", got, want)
	}
	p, err := GetSecurityPosture(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if p.SignInFriction.Locked < 2 || p.SignInFriction.HelpNotHelpful < 2 {
		t.Fatalf("the posture does not carry the friction: %+v", p.SignInFriction)
	}
}
