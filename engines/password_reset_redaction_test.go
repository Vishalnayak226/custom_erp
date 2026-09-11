package engines

import "testing"

// Stage 49.6.2/49.6.6/49.6.7 - found while auditing this codebase for
// secrets reaching a log line: sendPasswordResetEmail's SMTP-failure branch
// can fire in production on an ordinary transient send failure, and printed
// the raw reset link (a working, unexpired credential) every time. This
// pins the fix: masked in production, still visible in dev where there is
// no other way for a developer with no mailer configured to see it.
func TestMaskedResetLinkRedactsOnlyInProduction(t *testing.T) {
	t.Setenv("ENV", "production")
	link := "https://erp.example.com/reset?token=super-secret-live-token-value"
	if got := maskedResetLink(link); got == link {
		t.Error("expected the reset link to be redacted in production")
	}

	t.Setenv("ENV", "")
	if got := maskedResetLink(link); got != link {
		t.Errorf("expected the reset link to pass through outside production, got %q", got)
	}
}
