package server

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Stage 49.5.1: read-only views of the route registry for cmd/gendocs'
// command catalogue (docs/security/command-catalog.md). Kept here, not in
// route_capabilities.go, so the registry file itself does not change.

// RouteClassificationFor answers the access level and capability of one
// registered pattern ("POST /api/v1/checkout"), as routeCapabilities has it.
func RouteClassificationFor(pattern string) (level, capability string, ok bool) {
	c, found := routeCapabilities[pattern]
	if !found {
		return "", "", false
	}
	switch c.Level {
	case LevelPublic:
		level = "public"
	case LevelAuthenticated:
		level = "authenticated"
	case LevelAdmin:
		level = "admin"
	case LevelBackground:
		level = "background"
	}
	return level, c.Capability, true
}

// CapabilityRoleAllowlistFor lists the roles a capability is restricted to
// beyond "authenticated" (capabilityRoleAllowlist), or false when any
// signed-in role of the module may call it.
func CapabilityRoleAllowlistFor(capability string) ([]string, bool) {
	roles, restricted := capabilityRoleAllowlist[capability]
	if !restricted {
		return nil, false
	}
	out := append([]string(nil), roles...)
	sort.Strings(out)
	return out, true
}

// RegisteredRoutePatterns is every pattern in the registry, sorted.
func RegisteredRoutePatterns() []string {
	out := make([]string, 0, len(routeCapabilities))
	for p := range routeCapabilities {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// IsBulkExportPattern reports whether apiMiddleware logs a route pattern as
// a DATA_EXPORT security event (isBulkExport), judged on the pattern's path.
func IsBulkExportPattern(pattern string) bool {
	path := pattern
	if i := strings.Index(pattern, " "); i >= 0 {
		path = pattern[i+1:]
	}
	// An in-memory request for isBulkExport only (it reads the path); nothing is sent.
	return isBulkExport(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: path}})
}
