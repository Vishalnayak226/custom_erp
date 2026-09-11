package server

// Shared fixtures for the Stage 47 red-team suite (see
// stage47_redteam_helpers_test.go for what that suite is and how to run it).
//
// Deliberately NOT behind the stage47redteam build tag. A finding's test is
// promoted out of that tag once the finding is remediated (47.0.1's own
// closure note), and a promoted test has to compile on the ordinary
// `go test ./...` path - which it cannot do if the fixtures it calls are
// tagged. 47.2 promoted the A-02 test and moved these here for that reason.

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

// stage47UniqueID returns a short random hex suffix so IDs created by these
// tests can never collide with another concurrent session's fixtures or
// test-user rows sharing this same tenant_default schema (see CLAUDE.md's
// "this tree sees concurrent sessions" note) - engines.NewDocID already
// solves this for `documents` rows; this covers the non-document identifiers
// (usernames, SKUs, location codes) these tests also need.
func stage47UniqueID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// seedStage47User creates a disposable, throwaway user with the given role,
// active immediately, and returns a cleanup that removes it (and, via the
// documents table's ON DELETE CASCADE from created_by, anything it created).
// Modeled on mfa_recovery_test.go's seedMFATestUser, minus the MFA flow -
// none of these findings are about MFA, and RequiresMFA only gates Super
// Admin (engines/mfa.go), so a directly-minted session token is a faithful
// stand-in for a real login for every role these tests use.
func seedStage47User(t *testing.T, role, location string) (userID string, cleanup func()) {
	t.Helper()
	userID = "__stage47_" + stage47UniqueID() + "__"
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	hash, err := bcrypt.GenerateFromPassword(b, bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash throwaway password: %v", err)
	}
	db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, userID)
	if _, err := db.DB.Exec(
		`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code)
		 VALUES ($1, $1, $2, $3, $4, 'Active', $5)`,
		userID, string(hash), userID+"@stage47.invalid", role, location); err != nil {
		t.Fatalf("failed to seed throwaway %s user: %v", role, err)
	}
	return userID, func() {
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, userID)
	}
}

// stage47Token mints a full session token for userID/role/location directly
// via engines.SignToken (the same primitive engines/auth_claim_injection_test.go
// and internal/server/document_numbering_api_test.go already use to test
// handler behavior without going through a real /login round trip), and
// busts the live-user-state cache so apiMiddleware's Stage 29.8 re-check
// reads this test's freshly-seeded row instead of a stale cached miss.
func stage47Token(userID, role, location string) string {
	// seedStage47User's INSERT never sets credential_version, so a
	// freshly-seeded row is always the column's own DEFAULT (1) - safe to
	// pass literally here, unlike a call site that mints a token for the
	// real persistent "admin"/"system" seed accounts (see
	// currentCredentialVersion below for those).
	token := engines.SignToken(userID, userID, role, "default", location, 1)
	engines.ResetLiveUserStateCache()
	return token
}

// currentCredentialVersion (49.2.2/49.2.4) reads a user's live
// credential_version so a test that mints a token for a shared, persistent
// account - "admin"/"system", not a disposable per-test fixture - stays
// correct regardless of that account's password-change history in this
// shared development database. Defaults to 1 (never fatal) so a lookup
// failure produces a token that 401s with a clear cause rather than
// crashing an unrelated test's setup.
func currentCredentialVersion(userID string) int {
	v := 1
	_ = db.DB.QueryRow(`SELECT credential_version FROM tenant_default.users WHERE id = $1`, userID).Scan(&v)
	return v
}
