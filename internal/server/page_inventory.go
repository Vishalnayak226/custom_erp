package server

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"custom_erp/engines"
)

// Stage 135.5: the route half of the page / action-to-role inventory
// (docs/qa/generated/page-inventory.md, rendered by cmd/gendocs). Gates and
// handlers are read from routes.go's registration lines; who may call each
// route comes from checkRouteCapability - the check apiMiddleware runs - so
// the inventory states what is enforced, not what a second list claims.

// RouteAccess is one registered route and who may reach it.
type RouteAccess struct {
	Pattern     string // as registered, e.g. "POST /api/v1/returns/{id}/qc"
	Method      string // "ANY" when the pattern names no method
	Path        string
	Handler     string
	ModuleGate  string
	FeatureGate string
	Level       string // public, signed-in, admin, background, or no session
	Capability  string
	// Roles are the role names checkRouteCapability allows; nil means every
	// signed-in role (or, for public routes, anyone).
	Roles  []string
	Source string // routes.go:line
}

var (
	inventoryRouteLine = regexp.MustCompile(`http\.HandleFunc\(\s*"((?:([A-Z]+) )?([^"]+))"\s*,\s*(.*)$`)
	inventoryGate      = regexp.MustCompile(`(moduleGate|featureGate)\("([^"]+)"`)
	inventoryHandler   = regexp.MustCompile(`\b(handle[A-Za-z0-9_]*)\b`)
)

// InventoryRoles is every role the inventory evaluates: Super Admin, a
// client's Admin, then each role template, in that order.
func InventoryRoles() []string {
	roles := []string{engines.RoleSuperAdmin, engines.RoleAdmin}
	seen := map[string]bool{engines.RoleSuperAdmin: true, engines.RoleAdmin: true}
	for _, template := range engines.RoleTemplates() {
		if !seen[template.Name] {
			seen[template.Name] = true
			roles = append(roles, template.Name)
		}
	}
	return roles
}

// RouteAccessTable reads routes.go (its path given by the caller) and
// classifies every /api route it registers.
func RouteAccessTable(routesGo string) ([]RouteAccess, error) {
	file, err := os.Open(routesGo)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	roles := InventoryRoles()
	var out []RouteAccess
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		m := inventoryRouteLine.FindStringSubmatch(scanner.Text())
		if m == nil || !strings.HasPrefix(m[3], "/api/") {
			continue
		}
		route := RouteAccess{Pattern: m[1], Method: m[2], Path: m[3], Source: fmt.Sprintf("routes.go:%d", line)}
		if route.Method == "" {
			route.Method = "ANY"
		}
		for _, gate := range inventoryGate.FindAllStringSubmatch(m[4], -1) {
			if gate[1] == "moduleGate" {
				route.ModuleGate = gate[2]
			} else {
				route.FeatureGate = gate[2]
			}
		}
		if h := inventoryHandler.FindAllString(m[4], -1); len(h) > 0 {
			route.Handler = h[len(h)-1]
		}
		classification, found := routeCapabilities[route.Pattern]
		if !strings.Contains(m[4], "apiMiddleware(") || !found {
			route.Level = "no session"
			out = append(out, route)
			continue
		}
		route.Capability = classification.Capability
		route.Level = map[RouteAccessLevel]string{LevelPublic: "public", LevelAuthenticated: "signed-in", LevelAdmin: "admin", LevelBackground: "background"}[classification.Level]
		if classification.Level != LevelPublic {
			var allowed []string
			for _, role := range roles {
				if ok, _, _ := checkRouteCapability(route.Pattern, role); ok {
					allowed = append(allowed, role)
				}
			}
			if len(allowed) < len(roles) {
				route.Roles = allowed
			}
		}
		out = append(out, route)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
