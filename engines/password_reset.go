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

const passwordResetTokenTTL = 30 * time.Minute

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
	expiresAt := time.Now().Add(passwordResetTokenTTL)

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
func CompletePasswordReset(tenantID, token, newPassword string) error {
	if len(newPassword) < 8 {
		return errors.New("new password must be at least 8 characters")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	var userID, username string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT id, username FROM %s.users WHERE reset_token_hash = $1 AND reset_token_expires_at > NOW()`, schema),
		hashResetToken(token)).Scan(&userID, &username)
	if err != nil {
		return errors.New("reset token is invalid or has expired")
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.users SET password_hash = $1, reset_token_hash = NULL, reset_token_expires_at = NULL, failed_login_count = 0, locked_until = NULL WHERE id = $2`, schema),
		string(newHash), userID); err != nil {
		return err
	}

	LogAuditEvent(tenantID, username, "AUTH", "PASSWORD_RESET_COMPLETED", "Password reset via emailed token")
	return nil
}

// sendPasswordResetEmail sends via SMTP_HOST/SMTP_PORT (+ optional
// SMTP_USER/SMTP_PASSWORD/SMTP_FROM) if configured; otherwise it logs the
// link locally so dev/test environments (no SMTP server available) can
// still exercise and verify the flow end-to-end, same posture as
// OPS_ALERT_WEBHOOK_URL being unset.
func sendPasswordResetEmail(tenantID, toEmail, username, resetLink string) {
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
		log.Printf("[PASSWORD-RESET] (user has no email on file - not sent) reset link for %s: %s", username, resetLink)
		return
	}
	smtpHost := os.Getenv("SMTP_HOST")
	if smtpHost == "" {
		log.Printf("[PASSWORD-RESET] (no SMTP_HOST configured - not sent) reset link for %s: %s", username, resetLink)
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
		log.Printf("[PASSWORD-RESET] failed to send reset email to %s: %v (link: %s)", toEmail, err, resetLink)
	}
}
