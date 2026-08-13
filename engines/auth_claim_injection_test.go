package engines

import (
	"testing"
	"time"
)

// Stage 43.1 - claim delimiter smuggling.
//
// The token payload is a flat "k=v&k=v" string (see SignToken). Every value in
// it comes from a database column - username, role, tenant, location_code -
// and none of those columns restrict their characters. A value carrying a raw
// "&" or "=" therefore used to change the *shape* of the payload rather than
// just its content, and because the server signs the result, the forged claims
// arrived with a perfectly valid HMAC.
//
// docs/operations/pentest_scope.md names this as a P1 for an external tester;
// these tests close it in-house instead. They assert the property that makes
// the whole class impossible - a claim value round-trips to exactly itself,
// and can never introduce a key that the issuer did not emit.
func TestTokenClaimsCannotBeSmuggledThroughValues(t *testing.T) {
	t.Run("a username cannot introduce a claim the issuer never emitted", func(t *testing.T) {
		// "purpose" is the dangerous one: apiMiddleware skips the Stage 29.8
		// live user-state re-check when purpose == "extension", so smuggling it
		// into a full session token freezes role/location at issue time and
		// makes deactivating the account stop working.
		token := SignToken("u1", "evil&purpose=extension", "Cashier", "default", "HO")
		claims, err := ParseToken(token)
		if err != nil {
			t.Fatalf("token must still parse: %v", err)
		}
		if _, injected := claims["purpose"]; injected {
			t.Errorf("a username must not be able to introduce a purpose claim, got %q", claims["purpose"])
		}
		if claims["user"] != "evil&purpose=extension" {
			t.Errorf("username must round-trip verbatim, got %q", claims["user"])
		}
	})

	t.Run("a location code cannot override a claim emitted before it", func(t *testing.T) {
		// loc is emitted after role and tenant, so under last-write-wins map
		// assignment an injected pair here beat the real one.
		token := SignToken("u2", "cashier1", "Cashier", "default", "HO&role=Super Admin&tenant=victim")
		claims, err := ParseToken(token)
		if err != nil {
			t.Fatalf("token must still parse: %v", err)
		}
		if claims["role"] != "Cashier" {
			t.Errorf("role must stay the issued value, got %q", claims["role"])
		}
		if claims["tenant"] != "default" {
			t.Errorf("tenant must stay the issued value, got %q", claims["tenant"])
		}
	})

	t.Run("an equals sign in a value does not drop the claim", func(t *testing.T) {
		// The old parser skipped any pair that did not split into exactly two
		// halves, so a username containing "=" silently produced an empty
		// Resolved-Username - actions attributed to nobody in the audit trail.
		token := SignToken("u3", "a=b", "Cashier", "default", "HO")
		claims, err := ParseToken(token)
		if err != nil {
			t.Fatalf("token must still parse: %v", err)
		}
		if claims["user"] != "a=b" {
			t.Errorf("a username containing '=' must round-trip, got %q", claims["user"])
		}
	})

	t.Run("a purpose token cannot be upgraded into a session token", func(t *testing.T) {
		// SignPurposeToken deliberately omits role/loc so an MFA-challenge
		// token (issued after the password, before the second factor) can never
		// act as a full session. An injected role claim would defeat that.
		token := SignPurposeToken("u4", "evil&role=Super Admin&loc=HO", "default", "mfa_challenge", 5*time.Minute)
		claims, err := ParseToken(token)
		if err != nil {
			t.Fatalf("token must still parse: %v", err)
		}
		if _, injected := claims["role"]; injected {
			t.Errorf("an MFA purpose token must carry no role claim, got %q", claims["role"])
		}
		if claims["purpose"] != "mfa_challenge" {
			t.Errorf("purpose must stay the issued value, got %q", claims["purpose"])
		}
	})

	t.Run("ordinary values are unaffected", func(t *testing.T) {
		token := SignToken("admin", "admin", "Super Admin", "default", "HO")
		claims, err := ParseToken(token)
		if err != nil {
			t.Fatalf("token must parse: %v", err)
		}
		for key, want := range map[string]string{
			"id": "admin", "user": "admin", "role": "Super Admin", "tenant": "default", "loc": "HO",
		} {
			if claims[key] != want {
				t.Errorf("claim %q: got %q, want %q", key, claims[key], want)
			}
		}
	})
}
