//go:build stage47redteam

package server

// Stage 47.0.1 - red-team regression/abuse scenarios for audit findings A-01
// through A-07 (docs/audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md, lines
// 63-132; tracked at docs/micro_checklist.md Stage 47, item 47.0.1).
//
// HOW TO RUN:
//
//	go test -tags stage47redteam ./engines/... ./internal/server/...
//
// or narrower, e.g.:
//
//	go test -tags stage47redteam ./internal/server/... -run TestA01
//
// These tests assert the SECURE/CORRECT outcome for each finding - the
// outcome the audit says the product does NOT currently deliver. They are
// EXPECTED TO FAIL (red) against the codebase as it stands on 2026-09-03.
// This item (47.0.1) is explicitly scoped to freezing evidence, not fixing
// it - see the item's own text: "no product behavior change in this item."
//
// A PASSING result for a given finding's test means that finding has been
// remediated (by 47.1-47.7, whichever owns that area) - at that point
// whichever session closes the corresponding 47.x item should promote that
// test out of this build tag into the ordinary suite, per 47.0.1's own
// closure note.
//
// The stage47redteam tag keeps these off the default `go test ./...` path
// so a deliberately-failing security assertion never breaks the build for a
// concurrent session or CI sharing this tree - see docs/micro_checklist.md's
// Stage 47.0.1 entry and the top-level CLAUDE.md's shared-tree note.
//
// This file holds helpers shared by every stage47_a0N_*_redteam_test.go file
// in this package.

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
	token := engines.SignToken(userID, userID, role, "default", location)
	engines.ResetLiveUserStateCache()
	return token
}
