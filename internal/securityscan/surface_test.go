package securityscan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// repoRoot is where this package's tests find the tree they scan. Tests run
// with the package directory as the working directory.
const repoRoot = "../.."

const manifestPath = "docs/security/attack_surface.json"

func scanOrFail(t *testing.T) *Surface {
	return cachedSurface(t)
}

func uncachedScan(t *testing.T) *Surface {
	t.Helper()
	s, err := ScanSurface(repoRoot)
	if err != nil {
		t.Fatalf("ScanSurface: %v", err)
	}
	return s
}

// Stage 49.1.6, the release gate itself: the committed inventory must describe
// the tree it was generated from. Anything that adds, removes or re-gates a
// route, background job, environment flag, outbound call site or dependency
// fails here until the inventory is regenerated and the diff reviewed - which
// is what makes "a generated surface diff accompanies every release" true by
// construction rather than by remembering.
func TestAttackSurfaceManifestIsCurrent(t *testing.T) {
	current := scanOrFail(t)
	encoded, err := Encode(current)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	committed, err := os.ReadFile(filepath.Join(repoRoot, manifestPath))
	if err != nil {
		t.Fatalf("cannot read %s: %v\nGenerate it with: go run ./cmd/surfacescan", manifestPath, err)
	}
	if string(committed) == string(encoded) {
		return
	}
	var approved Surface
	if err := json.Unmarshal(committed, &approved); err != nil {
		t.Fatalf("%s is not valid JSON: %v", manifestPath, err)
	}
	t.Fatalf(`the attack surface has drifted from the approved profile in %s.

%s
Review each line above. If every change is intended, regenerate the profile:

    go run ./cmd/surfacescan

(On this Windows dev machine, Controlled Folder Access blocks that write; use
 go run ./cmd/surfacescan -out %%TEMP%%\attack_surface.json and copy it in, the
 way docs/brain/update-brain.ps1 does.)`, manifestPath, DescribeDrift(&approved, current))
}

// The scanner reads literal registrations. If routes.go grows a registration
// shape none of the patterns match, the inventory silently loses a route -
// the exact failure mode an attack-surface inventory exists to prevent. This
// asserts every registration line is classified by one of them.
func TestEveryRouteRegistrationIsRecognised(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "internal", "server", "routes.go"))
	if err != nil {
		t.Fatalf("cannot read routes.go: %v", err)
	}
	var unrecognised []string
	for i, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if !anyRegistration.MatchString(line) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue // a comment mentioning the call, not a registration
		}
		if middlewareRoute.MatchString(line) || templatedRoute.MatchString(line) ||
			bareRoute.MatchString(line) || handleRoute.MatchString(line) {
			continue
		}
		unrecognised = append(unrecognised, fmt.Sprintf("  routes.go:%d  %s", i+1, strings.TrimSpace(line)))
	}
	if len(unrecognised) > 0 {
		t.Fatalf(`%d route registration(s) in routes.go match no pattern in surface.go, so they are missing from the
attack-surface inventory. Add a pattern for the new registration shape (or register
the route the way the others are):
%s`, len(unrecognised), strings.Join(unrecognised, "\n"))
	}
}

// A route reachable with no middleware at all answers before any
// authentication, tenant resolution or capability check runs. There is exactly
// one today, and it exists for a reason that cannot be met any other way. Any
// second one is a deliberate decision that has to be made here, in a reviewed
// diff, not noticed later in an audit.
var reviewedUnauthenticatedRoutes = map[string]string{
	"GET /internal/tls-ask": "Caddy's on_demand_tls ask hook - called mid-TLS-handshake, before any request exists, so it cannot carry a bearer token. Answers only whether a hostname is a known tenant.",
}

func TestNoUnauthenticatedRouteOutsideTheReviewedSet(t *testing.T) {
	for _, r := range scanOrFail(t).Routes {
		if r.Auth != AuthNone {
			continue
		}
		key := routeKey(r)
		if _, reviewed := reviewedUnauthenticatedRoutes[key]; !reviewed {
			t.Errorf(`%s (%s) is registered with no middleware, so it answers before authentication,
tenant resolution and every capability check. Either register it through apiMiddleware, or add it to
reviewedUnauthenticatedRoutes with the reason it cannot be.`, key, r.Source)
		}
	}
}

// middleware.go's publicRoutes allowlist names the endpoints reachable with no
// bearer token. An entry there for a path no route serves guards nothing today
// - but it silently pre-authorises whatever is registered at that path next,
// which is how an internal route becomes public without anyone deciding it.
//
// The two exceptions below are real: found by this check on the day it was
// written (49.1.1), recorded in docs/security/risk_register.md as R-05, and
// owned by whoever next touches middleware.go. They are listed rather than
// silently tolerated so the count can only go down.
var knownDeadPublicRouteEntries = map[string]string{
	"/api/v1/integration/courier/delhivery/tracking":  "R-05: allowlisted for a Stage 35.5 route that was never registered - no handler exists anywhere in the tree",
	"/api/v1/integration/courier/shiprocket/tracking": "R-05: allowlisted for a Stage 35.5 route that was never registered - no handler exists anywhere in the tree",
}

func TestPublicRouteAllowlistHasNoNewDeadEntries(t *testing.T) {
	public, err := scanPublicRouteAllowlist(repoRoot)
	if err != nil {
		t.Fatalf("scanPublicRouteAllowlist: %v", err)
	}
	registered := map[string]bool{}
	for _, r := range scanOrFail(t).Routes {
		registered[r.Path] = true
	}
	var dead []string
	for path := range public {
		if registered[path] {
			continue
		}
		if _, known := knownDeadPublicRouteEntries[path]; known {
			continue
		}
		dead = append(dead, "  "+path)
	}
	sort.Strings(dead)
	if len(dead) > 0 {
		t.Fatalf(`%d entr(y/ies) in middleware.go's publicRoutes allowlist name a path no route serves:
%s
An allowlist entry that guards nothing today pre-authorises whatever is registered at that path
tomorrow. Remove the entry, or register the route it was written for.`, len(dead), strings.Join(dead, "\n"))
	}
	// And the reverse: an exception that has since been cleaned up must not
	// linger here pretending there is still a problem.
	for path := range knownDeadPublicRouteEntries {
		if registered[path] || !public[path] {
			t.Errorf("knownDeadPublicRouteEntries still lists %s, but it is no longer a dead allowlist entry - remove it from that map", path)
		}
	}
}

// The inventory is only useful as a drift check if an unchanged tree produces
// identical bytes. Map iteration order is the usual way that stops being true.
func TestSurfaceScanIsDeterministic(t *testing.T) {
	first, err := Encode(uncachedScan(t))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		next, err := Encode(uncachedScan(t))
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(next) {
			t.Fatal("two scans of the same tree produced different output - something in the scan depends on map iteration order, so the drift check would fire at random")
		}
	}
}

// Sanity floor. If a refactor breaks an extraction pattern, the counts collapse
// and every other test here still passes against a nearly empty inventory.
func TestSurfaceScanFindsTheExpectedShapeOfTree(t *testing.T) {
	s := scanOrFail(t)
	for _, c := range []struct {
		what string
		got  int
		min  int
	}{
		{"routes", s.Totals["routes"], 400},
		{"session-authenticated routes", s.Totals["routes_session"], 400},
		{"integration-credential routes", s.Totals["routes_integration_credential"], 1},
		{"public routes", s.Totals["routes_public"], 5},
		{"background jobs", s.Totals["background_jobs"], 20},
		{"CLI commands", s.Totals["cli_commands"], 5},
		{"environment flags", s.Totals["environment_flags"], 15},
		{"security-critical environment flags", s.Totals["environment_flags_security_critical"], 10},
		{"incremental migrations", s.Totals["incremental_migrations"], 100},
	} {
		if c.got < c.min {
			t.Errorf("found only %d %s (expected at least %d) - an extraction pattern has probably stopped matching", c.got, c.what, c.min)
		}
	}
	if s.Module != "custom_erp" {
		t.Errorf("module = %q, want custom_erp", s.Module)
	}
	if len(s.StaticRoots) != 1 {
		t.Fatalf("expected exactly one static root (public/), got %d", len(s.StaticRoots))
	}
	if s.StaticRoots[0].DirectoryListing {
		t.Error("the static root reports directory listing enabled - 49.1.2 requires it suppressed")
	}
}

// Both scanners walk the whole tree, and six tests in this package want the
// result. Scanning once per test binary keeps `go test ./...` from spending
// half a minute re-reading the same ~700 files. TestSurfaceScanIsDeterministic
// deliberately calls ScanSurface directly instead, because repeating the scan
// is the whole point of that one.
var (
	memoSurfaceOnce sync.Once
	memoSurface     *Surface
	memoSurfaceErr  error

	memoBypassOnce sync.Once
	memoBypass     []BypassFinding
	memoBypassErr  error
)

func cachedSurface(t *testing.T) *Surface {
	t.Helper()
	memoSurfaceOnce.Do(func() { memoSurface, memoSurfaceErr = ScanSurface(repoRoot) })
	if memoSurfaceErr != nil {
		t.Fatalf("ScanSurface: %v", memoSurfaceErr)
	}
	return memoSurface
}

func cachedBypass(t *testing.T) []BypassFinding {
	t.Helper()
	memoBypassOnce.Do(func() { memoBypass, memoBypassErr = ScanBypass(repoRoot) })
	if memoBypassErr != nil {
		t.Fatalf("ScanBypass: %v", memoBypassErr)
	}
	return memoBypass
}
