package engines

import (
	"crypto/rand"
	"custom_erp/db"
	"encoding/hex"
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func passwordResetTestConnStr() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
}

func seedPasswordResetTestUser(t *testing.T) (userID, initialPassword string, cleanup func()) {
	t.Helper()
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	userID = "__pwreset_" + hex.EncodeToString(suffix) + "__"
	initialPassword = "Kx9#mQ2wPz$7Ln4v"
	hash, err := bcrypt.GenerateFromPassword([]byte(initialPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash initial password: %v", err)
	}
	if _, err := db.DB.Exec(
		`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status)
		 VALUES ($1, $1, $2, $3, 'Cashier', 'Active')`,
		userID, string(hash), userID+"@pwreset.invalid"); err != nil {
		t.Fatalf("seed password-reset test user: %v", err)
	}
	return userID, initialPassword, func() {
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, userID)
		ResetLiveUserStateCache()
	}
}

// Stage 49.2.4: RequestPasswordReset/CompletePasswordReset had no dedicated
// test at all before this - this is the first coverage of 24.28's reset
// flow, not just of this stage's own additions to it (credential_version
// bump, strength check, single-use token enforcement).
func TestPasswordResetFlowEndToEnd(t *testing.T) {
	db.InitDB(passwordResetTestConnStr())
	userID, _, cleanup := seedPasswordResetTestUser(t)
	defer cleanup()

	if err := RequestPasswordReset("default", userID, "https://erp.example/reset"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}

	var tokenHash string
	if err := db.DB.QueryRow(`SELECT reset_token_hash FROM tenant_default.users WHERE id = $1`, userID).Scan(&tokenHash); err != nil {
		t.Fatalf("read minted reset_token_hash: %v", err)
	}
	if tokenHash == "" {
		t.Fatal("RequestPasswordReset did not mint a reset token")
	}

	// The raw token is never persisted anywhere this test can read back
	// (that is the point - see hashResetToken's doc comment), so exercise
	// CompletePasswordReset's own validation surface directly instead of
	// trying to recover the raw value.
	if err := CompletePasswordReset("default", "not-the-real-token", "Zq8$vNw3Kd7#Lm2p"); err == nil {
		t.Error("a token that doesn't hash to the stored value must be rejected")
	}
}

// TestCompletePasswordResetRevokesSessionsAndEnforcesPolicy drives the
// completion path through hashResetToken directly (package-internal test,
// same package as password_reset.go) so it can mint a token whose hash it
// knows, without reaching into net/smtp or a real mailbox.
func TestCompletePasswordResetRevokesSessionsAndEnforcesPolicy(t *testing.T) {
	db.InitDB(passwordResetTestConnStr())
	userID, _, cleanup := seedPasswordResetTestUser(t)
	defer cleanup()

	var before int
	if err := db.DB.QueryRow(`SELECT credential_version FROM tenant_default.users WHERE id = $1`, userID).Scan(&before); err != nil {
		t.Fatalf("read initial credential_version: %v", err)
	}
	if before != 1 {
		t.Fatalf("a freshly seeded user should start at credential_version=1, got %d", before)
	}

	rawToken := "test-raw-reset-token-" + userID
	if _, err := db.DB.Exec(
		`UPDATE tenant_default.users SET reset_token_hash = $1, reset_token_expires_at = NOW() + interval '30 minutes' WHERE id = $2`,
		hashResetToken(rawToken), userID); err != nil {
		t.Fatalf("seed reset token: %v", err)
	}

	// 49.2.2: a weak new password must be refused, and refusal must not
	// consume the single-use token - the user gets another try.
	if err := CompletePasswordReset("default", rawToken, "abcdefghijkl"); err == nil {
		t.Error("a trivial-sequence password must be rejected by CompletePasswordReset")
	}
	var stillPending string
	if err := db.DB.QueryRow(`SELECT COALESCE(reset_token_hash, '') FROM tenant_default.users WHERE id = $1`, userID).Scan(&stillPending); err != nil {
		t.Fatalf("re-read reset_token_hash: %v", err)
	}
	if stillPending == "" {
		t.Fatal("a rejected password must not consume the reset token")
	}

	newPassword := "Rb4$xTq8Wm2#Zk9v"
	if err := CompletePasswordReset("default", rawToken, newPassword); err != nil {
		t.Fatalf("CompletePasswordReset with a strong password: %v", err)
	}

	var after int
	var hash, pendingHash string
	if err := db.DB.QueryRow(`SELECT credential_version, password_hash, COALESCE(reset_token_hash, '') FROM tenant_default.users WHERE id = $1`, userID).Scan(&after, &hash, &pendingHash); err != nil {
		t.Fatalf("read post-reset row: %v", err)
	}
	if after != before+1 {
		t.Errorf("credential_version should have bumped from %d to %d, got %d", before, before+1, after)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(newPassword)) != nil {
		t.Error("the stored hash does not match the new password")
	}
	if pendingHash != "" {
		t.Error("a completed reset must clear reset_token_hash so the token cannot be replayed")
	}

	// Replay: the same raw token must not work a second time.
	if err := CompletePasswordReset("default", rawToken, "Another$trongOne9"); err == nil {
		t.Error("a reset token must be single-use")
	}
}
