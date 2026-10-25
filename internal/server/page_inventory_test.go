package server

import (
	"reflect"
	"testing"

	"custom_erp/engines"
)

// Stage 135.5: the inventory's role column is checkRouteCapability's answer,
// so it must match what apiMiddleware enforces for each kind of route.
func TestRouteAccessTableStatesEnforcedRoles(t *testing.T) {
	routes, err := RouteAccessTable("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	byPattern := map[string]RouteAccess{}
	for _, r := range routes {
		byPattern[r.Pattern] = r
	}
	pickup := byPattern["POST /api/v1/returns/{id}/reverse-pickup"]
	if pickup.Handler != "handleScheduleReturnReversePickup" || pickup.ModuleGate != "oms" || pickup.Level != "signed-in" || pickup.Roles != nil {
		t.Errorf("an ordinary signed-in route: %+v", pickup)
	}
	for pattern, r := range byPattern {
		switch {
		case r.Capability == "finance.statements":
			for _, role := range r.Roles {
				if role == engines.RoleCashier {
					t.Errorf("%s: a restricted capability lists Cashier", pattern)
				}
			}
			if len(r.Roles) == 0 {
				t.Errorf("%s: a restricted capability lists no role", pattern)
			}
		case devOnlyCapabilities[r.Capability]:
			if !reflect.DeepEqual(r.Roles, []string{engines.RoleSuperAdmin}) {
				t.Errorf("%s: a developer area is Super Admin's alone, got %v", pattern, r.Roles)
			}
		}
	}
	if len(routes) < 500 {
		t.Fatalf("read %d routes from routes.go; the registration pattern no longer matches", len(routes))
	}
}
