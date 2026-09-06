package server

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Stage 47.1.1's own acceptance text: "Registration/test must fail for an
// authenticated route with no classification." This test parses
// routes.go's actual apiMiddleware-wrapped http.HandleFunc registrations
// (the same shape internal/kb/drift.go's literalRoutePattern already
// extracts for the 39.8 drift guard) and fails if any pattern has no entry
// in routeCapabilities (route_capabilities.go) - so a new route ships
// classified, or `go test ./...` breaks, not a silent gap discovered later
// by an audit.
var routeLinePattern = regexp.MustCompile(`http\.HandleFunc\(\s*"((?:[A-Z]+ )?[^"]+)"\s*,\s*apiMiddleware\(`)

func TestEveryAPIMiddlewareRouteIsClassified(t *testing.T) {
	data, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("failed to read routes.go: %v", err)
	}
	var missing []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		m := routeLinePattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pattern := m[1]
		if seen[pattern] {
			continue
		}
		seen[pattern] = true
		if _, ok := routeCapabilities[pattern]; !ok {
			missing = append(missing, pattern)
		}
	}
	if len(seen) == 0 {
		t.Fatalf("found zero apiMiddleware-wrapped routes in routes.go - the extraction regex itself is broken, not the registry")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("%d route(s) registered through apiMiddleware in routes.go have no entry in routeCapabilities (route_capabilities.go) - every authenticated route must be classified (47.1.1). Add each to the map:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// The reverse direction: a routeCapabilities entry for a pattern routes.go
// no longer registers is stale, not dangerous - but it means the map has
// drifted from the route table it exists to describe, which is exactly the
// kind of silent gap 47.1.1 exists to prevent on the other side. Flagged as
// a failure rather than a warning so it gets cleaned up rather than
// accumulating.
func TestNoStaleRouteCapabilityEntries(t *testing.T) {
	data, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("failed to read routes.go: %v", err)
	}
	registered := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := routeLinePattern.FindStringSubmatch(line); m != nil {
			registered[m[1]] = true
		}
	}
	var stale []string
	for pattern := range routeCapabilities {
		if !registered[pattern] {
			stale = append(stale, pattern)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Fatalf("%d entr(y/ies) in routeCapabilities no longer match any apiMiddleware route in routes.go - remove them (route no longer exists, or the pattern string changed):\n  %s",
			len(stale), strings.Join(stale, "\n  "))
	}
}
