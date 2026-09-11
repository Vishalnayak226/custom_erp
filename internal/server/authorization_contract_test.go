package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

// Stage 47.1.8 - generated authorization contract tests.
//
// "Generated" is the operative word: the matrix below is not a hand-written
// list of expected answers that would rot the moment a route is added. It
// walks routeCapabilities itself, crosses it with every role template
// (engines/role_templates.go) plus the two cases a hand-written list always
// forgets - no role at all, and a role nobody declared - and asserts the
// contract the registry implies. Add a route, add a role template, or change
// a capability, and this test covers it without being edited.
//
// The "fail the build on an unclassified route" half of the item is already
// enforced by route_capabilities_test.go's completeness test; this file adds
// the behavioral half, plus the HTTP-level checks (unauthenticated,
// deactivated/demoted session, cross-location, cross-employee, sensitive
// fields) the matrix cannot express on its own.

// contractRoles is every role the matrix is evaluated for: each template's
// canonical name, each legacy name it governs, and the two negative cases.
func contractRoles() []string {
	roles := []string{"", "Some Role Nobody Declared"}
	for _, template := range engines.RoleTemplates() {
		roles = append(roles, template.Name)
		roles = append(roles, template.LegacyNames...)
	}
	return roles
}

// declaredCapability reports whether a role's template declares a capability.
func declaredCapability(role, capability string) bool {
	template, ok := engines.RoleTemplateFor(role)
	if !ok {
		return false
	}
	for _, held := range template.Capabilities {
		if held == capability {
			return true
		}
	}
	return false
}

func TestAuthorizationContractMatrix(t *testing.T) {
	restricted := map[string]bool{}
	for _, capability := range restrictedCapabilities {
		restricted[capability] = true
	}

	checked := 0
	for pattern, classification := range routeCapabilities {
		for _, role := range contractRoles() {
			allowed, _, found := checkRouteCapability(pattern, role)
			if !found {
				t.Fatalf("%s: the registry has an entry but checkRouteCapability reported none", pattern)
			}
			checked++

			var want bool
			switch classification.Level {
			case LevelPublic:
				want = true
			case LevelAdmin:
				want = engines.IsSuperAdmin(role)
			default:
				if restricted[classification.Capability] {
					want = engines.IsSuperAdmin(role) || declaredCapability(role, classification.Capability)
				} else {
					want = true
				}
			}
			if allowed != want {
				t.Errorf("%s (%v/%s) for role %q: allowed=%v, contract says %v",
					pattern, classification.Level, classification.Capability, role, allowed, want)
			}
		}
	}
	if checked < 400*len(contractRoles()) {
		t.Fatalf("only %d route/role pairs checked - the matrix is not covering the registry", checked)
	}
}

// The single most important line of the whole item, stated on its own so a
// regression names itself: no floor role reaches an administrative route.
func TestOnlyTheAdministratorTemplateReachesAdminRoutes(t *testing.T) {
	for pattern, classification := range routeCapabilities {
		if classification.Level != LevelAdmin {
			continue
		}
		for _, role := range contractRoles() {
			if engines.IsSuperAdmin(role) {
				continue
			}
			if allowed, _, _ := checkRouteCapability(pattern, role); allowed {
				t.Errorf("%s is admin-level but role %q was allowed", pattern, role)
			}
		}
	}
}

// A-01 restated as a contract, not a scenario: the Cashier template holds no
// restricted capability at all, so every restricted route denies it.
func TestCashierHoldsNoRestrictedCapability(t *testing.T) {
	for _, capability := range restrictedCapabilities {
		if declaredCapability(engines.RoleCashier, capability) {
			t.Errorf("the Cashier template declares %q - the A-01 finding by another name", capability)
		}
	}
	for pattern, classification := range routeCapabilities {
		if !contains(restrictedCapabilities, classification.Capability) {
			continue
		}
		if allowed, _, _ := checkRouteCapability(pattern, engines.RoleCashier); allowed {
			t.Errorf("Cashier reached %s (%s)", pattern, classification.Capability)
		}
	}
}

func TestUnclassifiedRouteFailsClosed(t *testing.T) {
	for _, role := range contractRoles() {
		allowed, _, found := checkRouteCapability("GET /api/v1/route-that-was-never-registered", role)
		if found || allowed {
			t.Errorf("an unclassified route must fail closed, got allowed=%v found=%v for role %q", allowed, found, role)
		}
	}
}

// Every restricted capability must be reachable by at least one template.
// A capability no role can exercise is a screen nobody can open, which is
// how a security control gets "fixed" by being widened again later.
func TestEveryRestrictedCapabilityHasAHolder(t *testing.T) {
	for _, capability := range restrictedCapabilities {
		holders := engines.RolesWithCapability(capability)
		if len(holders) == 0 {
			t.Errorf("no role template holds %q - nobody can use the routes it gates", capability)
		}
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// --- HTTP-level contract ------------------------------------------------

func contractUniqueID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// seedContractUser creates a throwaway Active user, uniquely named so it can
// never collide with a concurrent session's fixtures in this shared
// tenant_default schema.
func seedContractUser(t *testing.T, role, location string) (userID string, cleanup func()) {
	t.Helper()
	userID = "__authzcontract_" + contractUniqueID() + "__"
	secret := make([]byte, 8)
	_, _ = rand.Read(secret)
	hash, err := bcrypt.GenerateFromPassword(secret, bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash throwaway password: %v", err)
	}
	if _, err := db.DB.Exec(
		`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code)
		 VALUES ($1, $1, $2, $3, $4, 'Active', $5)`,
		userID, string(hash), userID+"@authz.invalid", role, location); err != nil {
		t.Fatalf("seed contract user: %v", err)
	}
	return userID, func() { db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, userID) }
}

func contractMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	return mux
}

func contractRequest(mux *http.ServeMux, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestUnauthenticatedAndDeactivatedSessionsAreRejected(t *testing.T) {
	db.InitDB(testConnStr())
	// ResolveLiveUserState caches the live role/status for
	// AUTH_STATE_CACHE_SECONDS (engines/auth_livestate.go) - a deliberate,
	// bounded revocation window, not a bug. This test is about what the
	// check itself does, so the window is set to zero for its duration
	// rather than sleeping through the default.
	t.Setenv("AUTH_STATE_CACHE_SECONDS", "0")
	mux := contractMux()

	if rec := contractRequest(mux, http.MethodGet, "/api/v1/doc/Brand", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET status=%d, want 401", rec.Code)
	}
	if rec := contractRequest(mux, http.MethodGet, "/api/v1/doc/Brand", "not.a.real.token"); rec.Code != http.StatusUnauthorized {
		t.Errorf("garbage token status=%d, want 401", rec.Code)
	}

	userID, cleanup := seedContractUser(t, engines.RoleCashier, "HO")
	defer cleanup()
	token := engines.SignToken(userID, userID, engines.RoleCashier, "default", "HO", 1)
	if rec := contractRequest(mux, http.MethodGet, "/api/v1/doc/Brand", token); rec.Code != http.StatusOK {
		t.Fatalf("active Cashier GET Brand status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Deactivated mid-session: the token is still cryptographically valid.
	if _, err := db.DB.Exec(`UPDATE tenant_default.users SET status = 'Inactive' WHERE id = $1`, userID); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}
	if rec := contractRequest(mux, http.MethodGet, "/api/v1/doc/Brand", token); rec.Code != http.StatusUnauthorized {
		t.Errorf("deactivated session status=%d, want 401 - a revoked account must not survive on a signed token", rec.Code)
	}

	// Demoted mid-session: the DB role is authoritative, so a token minted
	// while the user was Super Admin must not keep admin access.
	if _, err := db.DB.Exec(`UPDATE tenant_default.users SET status = 'Active', role = $2 WHERE id = $1`, userID, engines.RoleCashier); err != nil {
		t.Fatalf("reactivate user: %v", err)
	}
	staleAdminToken := engines.SignToken(userID, userID, engines.RoleSuperAdmin, "default", "HO", 1)
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /api/v1/admin/role-templates", apiMiddleware(handleListRoleTemplates))
	if rec := contractRequest(adminMux, http.MethodGet, "/api/v1/admin/role-templates", staleAdminToken); rec.Code == http.StatusOK {
		t.Error("a token claiming Super Admin for a demoted user reached an admin route")
	}
}

// Cross-location and sensitive-field denial, end to end over HTTP, on the
// two doctypes the audit named. Seeded and torn down per run so it is safe
// in the shared development schema.
func TestCrossLocationAndSensitiveFieldsAreDeniedOverHTTP(t *testing.T) {
	db.InitDB(testConnStr())
	mux := contractMux()

	suffix := contractUniqueID()
	ownID := "AUTHZ-POS-OWN-" + suffix
	otherID := "AUTHZ-POS-OTHER-" + suffix
	payslipID := "AUTHZ-PAYSLIP-" + suffix
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id IN ($1,$2,$3)`, ownID, otherID, payslipID)

	if _, err := db.DB.Exec(
		`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
		 ($1, 'POSSession', $4, 'Open', 'system'),
		 ($2, 'POSSession', $5, 'Open', 'system'),
		 ($3, 'Payslip', $6, 'Draft', 'system')`,
		ownID, otherID, payslipID,
		`{"code":"`+ownID+`","location":"HO"}`,
		`{"code":"`+otherID+`","location":"AUTHZ-ELSEWHERE"}`,
		`{"code":"`+payslipID+`","employee_id":"AUTHZ-EMP-`+suffix+`","gross_pay":50000,"net_pay":42000}`,
	); err != nil {
		t.Fatalf("seed contract documents: %v", err)
	}

	userID, cleanup := seedContractUser(t, engines.RoleCashier, "HO")
	defer cleanup()
	token := engines.SignToken(userID, userID, engines.RoleCashier, "default", "HO", 1)

	if rec := contractRequest(mux, http.MethodGet, "/api/v1/doc/POSSession/"+ownID, token); rec.Code != http.StatusOK {
		t.Errorf("own-location POSSession status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := contractRequest(mux, http.MethodGet, "/api/v1/doc/POSSession/"+otherID, token); rec.Code == http.StatusOK {
		t.Error("a Cashier read another location's POSSession")
	}

	// The list must not contain the other location's row either - object
	// checks and list filters have to agree, or the leak just moves.
	rec := contractRequest(mux, http.MethodGet, "/api/v1/doc/POSSession", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POSSession list status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), otherID) {
		t.Error("another location's POSSession appeared in the Cashier's list")
	}

	// Payslip: this user has no linked Employee record, so self scope must
	// return none of them - and even if it did, the money fields are gone.
	rec = contractRequest(mux, http.MethodGet, "/api/v1/doc/Payslip", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("Payslip list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payslips []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payslips); err != nil {
		t.Fatalf("decode payslip list: %v (body=%s)", err, rec.Body.String())
	}
	for _, row := range payslips {
		if row["id"] == payslipID {
			t.Error("a Cashier with no Employee link listed a colleague's payslip")
		}
		for _, field := range []string{"gross_pay", "net_pay"} {
			if _, present := row[field]; present {
				t.Errorf("payslip %v still carried %s for a Cashier", row["id"], field)
			}
		}
	}
}
