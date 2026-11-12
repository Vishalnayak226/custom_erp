package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Stage 49.3.8: authorization tests generated from the route registry. Every
// /api route registered in routes.go is called through apiMiddleware with no
// token (refused unless it is one of the explicit public routes), and every
// route restricted to some roles is called by a role it does not allow
// (refused) and by one it does (let through). A new route is covered the day
// it is registered; RouteAccessTable is the same reading the page inventory
// (135.5) prints.
func TestStage49GeneratedAuthorizationCases(t *testing.T) {
	db.InitDB(testConnStr())
	routes, err := RouteAccessTable("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	reached := map[string]bool{}
	mux := http.NewServeMux()
	for _, rt := range routes {
		pattern := rt.Pattern
		mux.HandleFunc(pattern, apiMiddleware(func(w http.ResponseWriter, r *http.Request) {
			reached[pattern] = true
			w.WriteHeader(http.StatusOK)
		}))
	}

	// A disposable user per role the cases need, so the live-state check (the
	// database role wins over the token's) answers for a real row.
	users := map[string]string{}
	for role, id := range map[string]string{engines.RoleCashier: "authz-gen-cashier", engines.RoleAdmin: "authz-gen-admin", engines.RoleSuperAdmin: "authz-gen-super"} {
		hash, _ := bcrypt.GenerateFromPassword([]byte(randomTestPassword()), bcrypt.MinCost)
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id)
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status) VALUES ($1, $1, $2, $3, $4, 'Active')`, id, string(hash), id+"@erp.com", role); err != nil {
			t.Fatalf("seed %s: %v", role, err)
		}
		defer db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id)
		users[role] = engines.SignToken(id, id, role, "default", "HO", currentCredentialVersion(id))
	}

	param := regexp.MustCompile(`\{[^}]*\}`)
	path := func(p string) string {
		return param.ReplaceAllStringFunc(p, func(m string) string {
			if strings.HasSuffix(m, "...}") {
				return "a/b"
			}
			return "x1"
		})
	}
	method := func(rt RouteAccess) string {
		if rt.Method == "ANY" {
			return http.MethodGet
		}
		return rt.Method
	}
	call := func(rt RouteAccess, token string) (int, string) {
		rec := doRequestFromIP(t, mux.ServeHTTP, method(rt), path(rt.Path), token, nil, mfaTestIP())
		return rec.Code, rec.Body.String()
	}

	unauth, wrongRole, rightRole := 0, 0, 0
	for _, rt := range routes {
		if rt.Level == "no session" {
			continue // registered without apiMiddleware: its own handler authenticates
		}
		if !publicRoutes[path(rt.Path)] {
			unauth++
			if code, body := call(rt, ""); code != http.StatusUnauthorized {
				t.Errorf("%s with no token: %d %s, want 401", rt.Pattern, code, strings.TrimSpace(body))
			}
		}
		if rt.Roles == nil {
			continue
		}
		allowed := map[string]bool{}
		for _, r := range rt.Roles {
			allowed[r] = true
		}
		for _, role := range []string{engines.RoleCashier, engines.RoleAdmin, engines.RoleSuperAdmin} {
			code, body := call(rt, users[role])
			if allowed[role] {
				rightRole++
				if code == http.StatusUnauthorized || code == http.StatusForbidden {
					t.Errorf("%s as %s (allowed): %d %s", rt.Pattern, role, code, strings.TrimSpace(body))
				}
			} else {
				wrongRole++
				if code != http.StatusForbidden || !strings.Contains(body, "GLOBAL-0011") {
					t.Errorf("%s as %s (not allowed): %d %s, want 403 GLOBAL-0011", rt.Pattern, role, code, strings.TrimSpace(body))
				}
			}
		}
	}
	if unauth < 500 || wrongRole < 50 {
		t.Fatalf("too few cases ran (no token %d, wrong role %d): the route reading has broken", unauth, wrongRole)
	}
	t.Logf("no token: %d routes refused; wrong role: %d refused; allowed role: %d let through", unauth, wrongRole, rightRole)
}
