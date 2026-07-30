package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 29.8 closes the two items ERP_LOOPHOLES_ANALYSIS.md had left open:
// the per-doctype status-transition map (was "[needs design decision]") and
// the JWT session-staleness gap (was "deliberately deferred"). Both were
// unblocked by the user's 2026-07-29 decisions.

func TestStatusTransitionMap(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"

	// PurchaseOrder is one of the doctypes Stage 29.8 seeded a matrix for and
	// flagged strict; FulfillmentTask deliberately was not, so the two
	// together prove the opt-in half of the design.
	t.Run("a transition with an explicit rule is allowed", func(t *testing.T) {
		if err := ValidateStatusTransition(tenantID, "PurchaseOrder", "Draft", "Pending Approval", map[string]interface{}{"status": "Pending Approval"}); err != nil {
			t.Fatalf("Draft->Pending Approval should be allowed on PurchaseOrder, got: %v", err)
		}
	})

	t.Run("an unlisted transition on a strict doctype is blocked", func(t *testing.T) {
		// Draft->Closed skips the entire approval path.
		err := ValidateStatusTransition(tenantID, "PurchaseOrder", "Draft", "Closed", map[string]interface{}{"status": "Closed"})
		if err == nil {
			t.Fatal("expected PurchaseOrder Draft->Closed to be blocked under strict mode")
		}
		verr, ok := err.(*ValidationError)
		if !ok || verr.Code != "GLOBAL-0019" {
			t.Fatalf("expected a GLOBAL-0019 ValidationError, got %T: %v", err, err)
		}
		// The message must name the legal alternatives, or the caller is left
		// guessing what they're allowed to do instead.
		if !contains(verr.Message, "allowed from 'Draft'") {
			t.Errorf("rejection should list the allowed destinations, got: %s", verr.Message)
		}
	})

	t.Run("VendorInvoice cannot jump Draft->Paid, skipping the 3-way match", func(t *testing.T) {
		if err := ValidateStatusTransition(tenantID, "VendorInvoice", "Draft", "Paid", map[string]interface{}{"status": "Paid"}); err == nil {
			t.Fatal("expected VendorInvoice Draft->Paid to be blocked - that jump bypasses Matched/Approved entirely")
		}
	})

	t.Run("a disposed Asset cannot be re-capitalised", func(t *testing.T) {
		if err := ValidateStatusTransition(tenantID, "Asset", "Disposed", "Capitalised", map[string]interface{}{"status": "Capitalised"}); err == nil {
			t.Fatal("expected Asset Disposed->Capitalised to be blocked - Disposed is terminal")
		}
	})

	t.Run("a transition flagged requires_reason_code needs one", func(t *testing.T) {
		payload := map[string]interface{}{"status": "Rejected"}
		if err := ValidateStatusTransition(tenantID, "PurchaseOrder", "Pending Approval", "Rejected", payload); err == nil {
			t.Fatal("expected Pending Approval->Rejected to require a reason code")
		}
		payload["reason_code"] = "PO-REJ-01"
		if err := ValidateStatusTransition(tenantID, "PurchaseOrder", "Pending Approval", "Rejected", payload); err != nil {
			t.Fatalf("with a reason_code supplied the same transition should pass, got: %v", err)
		}
		// "reason" is the other spelling this repo's own screens send.
		delete(payload, "reason_code")
		payload["reason"] = "duplicate order"
		if err := ValidateStatusTransition(tenantID, "PurchaseOrder", "Pending Approval", "Rejected", payload); err != nil {
			t.Fatalf("a plain 'reason' should satisfy the requirement too, got: %v", err)
		}
	})

	t.Run("a doctype that was NOT flagged strict is unaffected", func(t *testing.T) {
		// FulfillmentTask has a rich status set and no seeded matrix, so
		// fail-open must still apply - this is what makes the rollout safe.
		if err := ValidateStatusTransition(tenantID, "FulfillmentTask", "Pending", "Dispatched", map[string]interface{}{"status": "Dispatched"}); err != nil {
			t.Fatalf("un-flagged doctypes must keep the previous fail-open behaviour, got: %v", err)
		}
	})

	t.Run("creates and no-op status round-trips are never blocked", func(t *testing.T) {
		if err := ValidateStatusTransition(tenantID, "PurchaseOrder", "", "Closed", map[string]interface{}{"status": "Closed"}); err != nil {
			t.Fatalf("a create (no prior status) must not be gated, got: %v", err)
		}
		// The "GET included status, PUT sent the whole object back" pattern.
		if err := ValidateStatusTransition(tenantID, "PurchaseOrder", "Closed", "Closed", map[string]interface{}{"status": "Closed"}); err != nil {
			t.Fatalf("an unchanged status must not be treated as a transition, got: %v", err)
		}
	})

	t.Run("a document stranded in an undeclared status can still be repaired", func(t *testing.T) {
		// Found on a clone of the dev database: a real SalesInvoice sitting in
		// status "Active", which isn't in its own option set. Strict mode must
		// not trap such a row forever - no rule can name a from_status the
		// schema doesn't declare.
		if err := ValidateStatusTransition(tenantID, "SalesInvoice", "Active", "Draft", map[string]interface{}{"status": "Draft"}); err != nil {
			t.Fatalf("a write leaving an undeclared legacy status must be allowed, got: %v", err)
		}
	})

	t.Run("the rule master rejects an entity that governs nothing", func(t *testing.T) {
		err := validateStatusTransitionRule(tenantID, map[string]interface{}{
			"entity": "NotARealDoctype", "from_status": "A", "to_status": "B",
		})
		if err == nil {
			t.Fatal("expected a rule naming an unknown doctype to be rejected - it would silently govern nothing")
		}
		// The four legacy OMS entity names are not doctypes but must stay valid.
		if err := validateStatusTransitionRule(tenantID, map[string]interface{}{
			"entity": "Order", "from_status": "Shipped", "to_status": "Cancelled",
		}); err != nil {
			t.Fatalf("legacy OMS entity 'Order' must remain valid, got: %v", err)
		}
		if err := validateStatusTransitionRule(tenantID, map[string]interface{}{
			"entity": "GRN", "from_status": "Pending", "to_status": "Pending",
		}); err == nil {
			t.Fatal("expected from_status == to_status to be rejected")
		}
	})
}

func TestJWTSigningKeyRotation(t *testing.T) {
	t.Run("no numbered keys leaves the legacy single-key behaviour untouched", func(t *testing.T) {
		keys, signing := loadJWTKeyring()
		if len(keys) != 1 || signing.kid != "" {
			t.Fatalf("expected exactly the legacy key with an empty kid, got %d key(s), signing kid=%q", len(keys), signing.kid)
		}
	})

	t.Run("the highest-numbered key signs, older keys still verify", func(t *testing.T) {
		t.Setenv("JWT_SECRET_1", "old-key-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		t.Setenv("JWT_SECRET_2", "new-key-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

		keys, signing := loadJWTKeyring()
		if signing.kid != "2" {
			t.Fatalf("expected the highest-numbered key (2) to sign, got kid=%q", signing.kid)
		}
		// 2 and 1, plus the legacy JWT_SECRET as a trailing verify key.
		if len(keys) != 3 {
			t.Fatalf("expected 3 verification keys (2, 1, legacy), got %d", len(keys))
		}

		// Swap the package ring in for the duration of this subtest so
		// sign/verify exercise the real code paths.
		origKeys, origSigning := jwtKeys, jwtSigningKey
		defer func() { jwtKeys, jwtSigningKey = origKeys, origSigning }()
		jwtKeys, jwtSigningKey = keys, signing

		token := SignToken("rot-user", "rot-user", "HR/Admin", "default", "HO")
		claims, err := ParseToken(token)
		if err != nil {
			t.Fatalf("a token signed with the active key must verify: %v", err)
		}
		if claims["kid"] != "2" {
			t.Errorf("expected the emitted token to carry kid=2 for ops visibility, got %q", claims["kid"])
		}

		// A token issued BEFORE the rotation (signed by key 1) must still be
		// accepted - that is the entire point of keeping old keys in the ring.
		jwtSigningKey = jwtKey{kid: "1", secret: []byte("old-key-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
		oldToken := SignToken("rot-user", "rot-user", "HR/Admin", "default", "HO")
		jwtSigningKey = signing
		if _, err := ParseToken(oldToken); err != nil {
			t.Fatalf("a token signed by a still-configured older key must verify: %v", err)
		}

		// Retiring key 1 (removing it from the ring) is what finally
		// invalidates the tokens it signed.
		jwtKeys = []jwtKey{signing}
		if _, err := ParseToken(oldToken); err == nil {
			t.Fatal("once the old key is dropped from the ring its tokens must stop verifying")
		}
	})

	t.Run("non-numeric JWT_SECRET_ suffixes are ignored, not treated as keys", func(t *testing.T) {
		t.Setenv("JWT_SECRET_TYPO", "not-a-key")
		keys, signing := loadJWTKeyring()
		if len(keys) != 1 || signing.kid != "" {
			t.Fatalf("a non-numeric suffix must not become a signing key, got %d key(s) signing kid=%q", len(keys), signing.kid)
		}
	})

	t.Run("a forged signature is still rejected under rotation", func(t *testing.T) {
		token := SignToken("rot-user", "rot-user", "HR/Admin", "default", "HO")
		if _, err := ParseToken(token + "ff"); err == nil {
			t.Fatal("a tampered signature must not verify against any key in the ring")
		}
	})
}

func TestLiveUserStateRecheck(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	const userID = "stage298-live-user"

	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, userID)
		ResetLiveUserStateCache()
	}
	cleanup()
	defer cleanup()

	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, role, status, location_code)
		VALUES ($1, $1, 'x', 'Store Manager', 'Active', 'HO')`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	t.Run("an active user resolves to their current role and location", func(t *testing.T) {
		state, err := ResolveLiveUserState(tenantID, userID)
		if err != nil {
			t.Fatalf("expected an active user to resolve, got: %v", err)
		}
		if state.Role != "Store Manager" || state.LocationCode != "HO" {
			t.Fatalf("got role=%q location=%q, want Store Manager/HO", state.Role, state.LocationCode)
		}
	})

	t.Run("deactivating the user ends the session, not just future logins", func(t *testing.T) {
		if _, err := db.DB.Exec(`UPDATE tenant_default.users SET status = 'Inactive' WHERE id = $1`, userID); err != nil {
			t.Fatalf("deactivate user: %v", err)
		}
		// Without the invalidation hook the cached Active entry would still be
		// served, which is exactly what the mutation sites call.
		InvalidateLiveUserState(tenantID, userID)

		_, err := ResolveLiveUserState(tenantID, userID)
		if err == nil {
			t.Fatal("a deactivated user must not resolve - this is the gap where a dismissed employee kept access for the rest of their token's life")
		}
		if !IsUserNotActiveError(err) {
			t.Fatalf("expected the deactivation error (so callers 401 rather than 503), got: %v", err)
		}
	})

	t.Run("a role change takes effect on the live session", func(t *testing.T) {
		if _, err := db.DB.Exec(`UPDATE tenant_default.users SET status = 'Active', role = 'Cashier' WHERE id = $1`, userID); err != nil {
			t.Fatalf("reactivate + demote user: %v", err)
		}
		InvalidateLiveUserState(tenantID, userID)

		state, err := ResolveLiveUserState(tenantID, userID)
		if err != nil {
			t.Fatalf("expected the reactivated user to resolve, got: %v", err)
		}
		// The token would still be claiming Store Manager here.
		if state.Role != "Cashier" {
			t.Fatalf("the database must win over the token's stale role claim, got %q", state.Role)
		}
	})

	t.Run("a user that never existed is rejected, and the rejection is cached", func(t *testing.T) {
		if _, err := ResolveLiveUserState(tenantID, "no-such-user-at-all"); !IsUserNotActiveError(err) {
			t.Fatalf("expected a phantom user to be rejected, got: %v", err)
		}
		// Second call is served from the negative cache rather than re-querying.
		if _, err := ResolveLiveUserState(tenantID, "no-such-user-at-all"); !IsUserNotActiveError(err) {
			t.Fatalf("the negative result must stay a rejection when served from cache, got: %v", err)
		}
	})

	t.Run("an empty user id is rejected outright", func(t *testing.T) {
		if _, err := ResolveLiveUserState(tenantID, ""); !IsUserNotActiveError(err) {
			t.Fatalf("expected an empty user id to be rejected, got: %v", err)
		}
	})

	t.Run("the cache TTL is configurable and 0 disables caching", func(t *testing.T) {
		t.Setenv("AUTH_STATE_CACHE_SECONDS", "0")
		if ttl := authStateCacheTTL(); ttl != 0 {
			t.Fatalf("expected caching to be disabled at 0, got %v", ttl)
		}
		// With caching off the DB is authoritative on every call.
		if _, err := db.DB.Exec(`UPDATE tenant_default.users SET status = 'Inactive' WHERE id = $1`, userID); err != nil {
			t.Fatalf("deactivate user: %v", err)
		}
		if _, err := ResolveLiveUserState(tenantID, userID); !IsUserNotActiveError(err) {
			t.Fatalf("with caching disabled a deactivation must be seen immediately, got: %v", err)
		}
	})
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
