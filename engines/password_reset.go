package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/smtp"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Password reset flow (24.28, loophole #22). Previously an admin had to
// manually reset a forgotten password via the Users screen (Stage 22.3) -
// this adds the standard self-service flow. Scoped per this stage's own
// principle of avoiding new dependencies: email delivery uses stdlib
// net/smtp (already available, no new Go module) rather than a mail-sending
// library, and is a safe no-op (logs the reset link locally) when SMTP
// isn't configured - the exact pattern engines/alerting.go's SendOpsAlert
// already established for OPS_ALERT_WEBHOOK_URL.

// Stage 30.7: the "security.password_reset_ttl_minutes" setting (default
// still 30 minutes).
func passwordResetTokenTTLFor(tenantID string) time.Duration {
	return time.Duration(GetSettingInt(tenantID, "security.password_reset_ttl_minutes")) * time.Minute
}

// hashResetToken never stores the raw token - only its SHA-256 hash, so a
// database leak alone can't be replayed into a password reset (mirrors why
// this app never stores a raw session token either, just verifies HMAC
// signatures).
func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// RequestPasswordReset looks up usernameOrEmail, mints a reset token if a
// matching active user exists, and emails (or logs) a reset link. Always
// returns nil regardless of whether a match was found - the caller's HTTP
// handler must respond identically either way, or the response itself
// becomes a username/email enumeration oracle (the same reasoning
// handleLogin's generic USERAC-0021 error already applies).
func RequestPasswordReset(tenantID, usernameOrEmail, resetLinkBase string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	var userID, username, email string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT id, username, COALESCE(email, '') FROM %s.users WHERE (username = $1 OR email = $1) AND status = 'Active'`, schema),
		usernameOrEmail).Scan(&userID, &username, &email)
	if err != nil {
		// No matching active user - silently no-op, see doc comment above.
		return nil
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := hex.EncodeToString(raw)
	expiresAt := time.Now().Add(passwordResetTokenTTLFor(tenantID))

	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.users SET reset_token_hash = $1, reset_token_expires_at = $2 WHERE id = $3`, schema),
		hashResetToken(token), expiresAt, userID); err != nil {
		return err
	}

	resetLink := fmt.Sprintf("%s?token=%s", resetLinkBase, token)
	sendPasswordResetEmail(tenantID, email, username, resetLink)
	LogAuditEvent(tenantID, username, "AUTH", "PASSWORD_RESET_REQUESTED", "Password reset token issued")
	return nil
}

// CompletePasswordReset validates a reset token (by its hash, never the raw
// value) and, if it matches a non-expired row, sets the new password and
// clears the token so it can't be replayed.
//
// 49.2.4: also bumps credential_version, which is what makes this actually
// revoke a session an attacker may already be holding on the compromised
// old password - apiMiddleware's live-state re-check (middleware.go) starts
// rejecting every token minted before the bump within the existing ~30s
// live-state SLO. Without this, a reset closed the "guess the password"
// door but left any session opened through the door standing.
func CompletePasswordReset(tenantID, token, newPassword string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	var userID, username, email string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT id, username, COALESCE(email, '') FROM %s.users WHERE reset_token_hash = $1 AND reset_token_expires_at > NOW()`, schema),
		hashResetToken(token)).Scan(&userID, &username, &email)
	if err != nil {
		return errors.New("reset token is invalid or has expired")
	}

	if err := ValidatePasswordStrength(tenantID, newPassword, username); err != nil {
		return err
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.users SET password_hash = $1, reset_token_hash = NULL, reset_token_expires_at = NULL, failed_login_count = 0, locked_until = NULL, credential_version = credential_version + 1 WHERE id = $2`, schema),
		string(newHash), userID); err != nil {
		return err
	}

	// Without this, the revocation above is real but not immediate - a
	// session cached moments earlier would keep resolving from
	// authStateCache for up to AUTH_STATE_CACHE_SECONDS (default 30s)
	// before the bumped credential_version was ever read back. Same
	// pattern 49.1.5's tenant suspend/deprovision transitions already use.
	InvalidateLiveUserState(tenantID, userID)

	LogAuditEvent(tenantID, username, "AUTH", "PASSWORD_RESET_COMPLETED", "Password reset via emailed token; all other sessions revoked")
	SendPasswordChangedNotice(tenantID, email, username, "a password-reset link")
	return nil
}

// SendRecoveryEmailChangedNotice (49.2.4, reauthentication-for-destination-
// change's risk-notification half) alerts the OLD address when a user's
// recovery email changes - the address most likely still read by the
// legitimate owner if a hijacked session just pointed the account's "forgot
// password" destination somewhere else. Never sent to the NEW address: this
// is a risk notice, not a confirmation flow - handleUpdateProfile does not
// verify the new address before storing it, matching this stage's own scope
// (reauthentication, not building an email-verification subsystem).
func SendRecoveryEmailChangedNotice(tenantID, oldEmail, username, newEmail string) {
	// 49.7.6: same sandbox guarantee as sendPasswordResetEmail below - this
	// function landed after that fix (49.2.4) and had the identical gap.
	if isSandbox, _ := IsSandboxTenant(tenantID); isSandbox {
		log.Printf("[EMAIL-CHANGED] sandbox tenant %s - simulating notice to %s (no real email sent)", tenantID, username)
		return
	}
	if oldEmail == "" {
		return
	}
	smtpHost := os.Getenv("SMTP_HOST")
	if smtpHost == "" {
		log.Printf("[EMAIL-CHANGED] (no SMTP_HOST configured - notice not sent) for %s", username)
		return
	}
	if !ExternalSideEffectsEnabled() {
		log.Printf("[EMAIL-CHANGED] (external side effects OFF - notice not sent) for %s", username)
		return
	}
	smtpPort := os.Getenv("SMTP_PORT")
	if smtpPort == "" {
		smtpPort = "587"
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "no-reply@custom-erp.local"
	}
	subject := "Your account recovery email was changed"
	body := fmt.Sprintf("Hello %s,\r\n\r\nThe email address on file for your account was just changed to %s.\r\n\r\nIf this was you, no action is needed. If it wasn't, contact your administrator immediately - whoever made this change had your password.\r\n", username, newEmail)
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s", from, oldEmail, subject, body))

	var auth smtp.Auth
	if smtpUser := os.Getenv("SMTP_USER"); smtpUser != "" {
		auth = smtp.PlainAuth("", smtpUser, os.Getenv("SMTP_PASSWORD"), smtpHost)
	}
	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)
	if err := smtp.SendMail(addr, auth, from, []string{oldEmail}, msg); err != nil {
		LogSystemError(tenantID, "", "Medium", "Notifications", fmt.Sprintf("[NOTIFI-0170] failed to send email-changed notice to %s: %v", oldEmail, err), "")
		log.Printf("[EMAIL-CHANGED] failed to send notice to %s: %v", oldEmail, err)
	}
}

// sendPasswordResetEmail sends via SMTP_HOST/SMTP_PORT (+ optional
// SMTP_USER/SMTP_PASSWORD/SMTP_FROM) if configured; otherwise it logs the
// link locally so dev/test environments (no SMTP server available) can
// still exercise and verify the flow end-to-end, same posture as
// OPS_ALERT_WEBHOOK_URL being unset.
// maskedResetLink (Stage 49.6.2/49.6.6/49.6.7 - found while auditing this
// file for exactly this) keeps sendPasswordResetEmail's dev-mode
// convenience of printing the link to the log when there is no mailer,
// while closing the real gap: a reset link IS a working, unexpired
// credential, and the SMTP-failure branch below can fire in PRODUCTION on
// an ordinary transient send failure - at which point every log line that
// printed the raw link had been handing out a live account-takeover token
// to anyone with journal/log access, exactly what 49.6.7 names explicitly
// ("no reset ... secret" in telemetry). Outside production the link still
// prints, because a developer with no mailer configured has no other way to
// see it.
func maskedResetLink(link string) string {
	if IsProductionEnv() {
		return "[redacted in production - see NOTIFI-0170/0171 for the failure reason]"
	}
	return link
}

func sendPasswordResetEmail(tenantID, toEmail, username, resetLink string) {
	// 49.7.6: a sandbox tenant (Stage 38.7) must never reach a real side
	// effect, the same guarantee engines/webhook.go's deliverWebhook already
	// gives outbound webhooks. This call site took tenantID from the start
	// but never actually checked it - a sandbox tenant on a production
	// binary (where ExternalSideEffectsEnabled() is true server-wide) would
	// have a real password-reset email land in a real inbox, which is
	// exactly the "sandbox reaches a real side effect" failure 49.7.6 exists
	// to close. Checked before SMTP_HOST/ExternalSideEffectsEnabled below,
	// same ordering deliverWebhook uses (sandbox is the more specific gate).
	if isSandbox, _ := IsSandboxTenant(tenantID); isSandbox {
		log.Printf("[PASSWORD-RESET] sandbox tenant %s - simulating delivery to %s (no real email sent)", tenantID, username)
		return
	}
	if toEmail == "" {
		// NOTIFI-0171 (Stage 25.5): "Email recipient missing" - logged, not
		// surfaced to the HTTP caller, since RequestPasswordReset's own
		// contract (24.28) is to respond identically whether or not a
		// matching user/email exists; a distinct error response here would
		// reopen the exact enumeration vector that contract exists to close.
		// The catalog entry itself lives in package server (errorCatalog),
		// not reachable from here - same reasoning every other engines-
		// package LogSystemError call already logs a plain code-prefixed
		// string rather than importing the catalog.
		LogSystemError(tenantID, "", "Medium", "Notifications", "[NOTIFI-0171] "+username+" has no email on file - password reset link not sent", "")
		log.Printf("[PASSWORD-RESET] (user has no email on file - not sent) reset link for %s: %s", username, maskedResetLink(resetLink))
		return
	}
	smtpHost := os.Getenv("SMTP_HOST")
	if smtpHost == "" {
		log.Printf("[PASSWORD-RESET] (no SMTP_HOST configured - not sent) reset link for %s: %s", username, maskedResetLink(resetLink))
		return
	}
	if !ExternalSideEffectsEnabled() {
		// Stage 47.0.5/47.11.6 Gate 0: an SMTP_HOST configured against a
		// shared dev/staging relay must not actually deliver mail just
		// because a regression/abuse test or a developer triggered this path.
		log.Printf("[PASSWORD-RESET] (external side effects OFF - not sent) reset link for %s: %s", username, maskedResetLink(resetLink))
		return
	}
	smtpPort := os.Getenv("SMTP_PORT")
	if smtpPort == "" {
		smtpPort = "587"
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "no-reply@custom-erp.local"
	}
	subject := "Password reset request"
	body := fmt.Sprintf("Hello %s,\r\n\r\nA password reset was requested for your account. This link expires in 30 minutes:\r\n\r\n%s\r\n\r\nIf you didn't request this, you can ignore this email.\r\n", username, resetLink)
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s", from, toEmail, subject, body))

	var auth smtp.Auth
	if smtpUser := os.Getenv("SMTP_USER"); smtpUser != "" {
		auth = smtp.PlainAuth("", smtpUser, os.Getenv("SMTP_PASSWORD"), smtpHost)
	}
	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)
	if err := smtp.SendMail(addr, auth, from, []string{toEmail}, msg); err != nil {
		// NOTIFI-0170 (Stage 25.5): "Notification not sent" - the SMTP send
		// itself failed (bad host/auth/network), distinct from NOTIFI-0171
		// above (nothing to send to in the first place).
		LogSystemError(tenantID, "", "Medium", "Notifications", fmt.Sprintf("[NOTIFI-0170] failed to send password reset email to %s: %v", toEmail, err), "")
		log.Printf("[PASSWORD-RESET] failed to send reset email to %s: %v (link: %s)", toEmail, err, maskedResetLink(resetLink))
	}
}

// SendPasswordChangedNotice (49.2.4 risk notification) tells the account
// owner their password just changed, via whichever of CompletePasswordReset
// or handleChangePassword's callers just changed it - "a password-reset
// link" or "your account settings" names which. Best-effort and silent on
// every failure mode the same way sendPasswordResetEmail is: a missing
// email/SMTP_HOST/ExternalSideEffectsEnabled gate must not fail (or even
// slow down) the password change that already succeeded, and a distinct
// error response here has no caller who could safely see it - the
// self-service change flow has already returned success by the time this
// runs, and RequestPasswordReset's own generic-response contract forbids
// a reset from ever branching client-visible behavior on email delivery.
func SendPasswordChangedNotice(tenantID, toEmail, username, viaWhat string) {
	// 49.7.6: same sandbox guarantee as sendPasswordResetEmail above - this
	// function landed after that fix (49.2.4) and had the identical gap.
	if isSandbox, _ := IsSandboxTenant(tenantID); isSandbox {
		log.Printf("[PASSWORD-CHANGED] sandbox tenant %s - simulating notice to %s (no real email sent)", tenantID, username)
		return
	}
	if toEmail == "" {
		log.Printf("[PASSWORD-CHANGED] (user has no email on file - notice not sent) for %s", username)
		return
	}
	smtpHost := os.Getenv("SMTP_HOST")
	if smtpHost == "" {
		log.Printf("[PASSWORD-CHANGED] (no SMTP_HOST configured - notice not sent) for %s", username)
		return
	}
	if !ExternalSideEffectsEnabled() {
		log.Printf("[PASSWORD-CHANGED] (external side effects OFF - notice not sent) for %s", username)
		return
	}
	smtpPort := os.Getenv("SMTP_PORT")
	if smtpPort == "" {
		smtpPort = "587"
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "no-reply@custom-erp.local"
	}
	subject := "Your password was changed"
	body := fmt.Sprintf("Hello %s,\r\n\r\nYour account password was just changed via %s.\r\n\r\nIf this was you, no action is needed. If it wasn't, contact your administrator immediately - your other active sessions have already been signed out.\r\n", username, viaWhat)
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s", from, toEmail, subject, body))

	var auth smtp.Auth
	if smtpUser := os.Getenv("SMTP_USER"); smtpUser != "" {
		auth = smtp.PlainAuth("", smtpUser, os.Getenv("SMTP_PASSWORD"), smtpHost)
	}
	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)
	if err := smtp.SendMail(addr, auth, from, []string{toEmail}, msg); err != nil {
		LogSystemError(tenantID, "", "Medium", "Notifications", fmt.Sprintf("[NOTIFI-0170] failed to send password-changed notice to %s: %v", toEmail, err), "")
		log.Printf("[PASSWORD-CHANGED] failed to send notice to %s: %v", toEmail, err)
	}
}
