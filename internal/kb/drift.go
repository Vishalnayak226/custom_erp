package kb

// Stage 39.8 - drift guards beyond 39.7's build-staleness check. 39.7 already
// catches "the committed output no longer matches its source"; this catches a
// narrower, sneakier failure - the output matches its source, but the source
// itself has quietly stopped being true, because the screen/endpoint/error
// code it names moved on without the article.
//
// Every check reads the real source of truth as a plain text file rather than
// importing it: internal/server already imports internal/kb to serve the
// Knowledge Center, so importing internal/server back would be a cycle, and
// regex-over-source mirrors this package's own "no YAML dependency for eight
// keys" approach in build.go - stdlib only, cheap to read.

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// StaleAfter is how long an article's last_verified date may age before it is
// flagged. Not derived from anything else in the codebase - six months is a
// working default, long enough that a normal content pass doesn't nag, short
// enough that a screen rename is unlikely to still be undetected two
// staleness cycles later.
const StaleAfter = 180 * 24 * time.Hour

var (
	viewIDPattern         = regexp.MustCompile(`view\s*===\s*'([a-z0-9-]+)'`)
	// The method group is optional: a handful of routes (the generic doc API
	// chief among them) are registered with no method prefix at all - Go's
	// mux then matches every method, so an empty capture here is stored as a
	// wildcard rather than silently dropping the route from validRoutes.
	literalRoutePattern   = regexp.MustCompile(`http\.HandleFunc\(\s*"(?:([A-Z]+) )?([^"]+)"`)
	structRoutePattern    = regexp.MustCompile(`Method:\s*(http\.Method\w+|"[A-Z]+")\s*,\s*Path:\s*"([^"]+)"`)
	errorCodeDefPattern   = regexp.MustCompile(`(?m)^\s*"([A-Z][A-Z0-9]{1,9}-\d{4})":\s*\{`)
	citedEndpointPattern  = regexp.MustCompile(`\b(GET|POST|PUT|PATCH|DELETE)\s+(/api/[a-zA-Z0-9/_{}.-]+)`)
	citedErrorCodePattern = regexp.MustCompile(`\b([A-Z][A-Z0-9]{1,9}-\d{4})\b`)
)

var httpMethodConstants = map[string]string{
	"http.MethodGet": "GET", "http.MethodPost": "POST", "http.MethodPut": "PUT",
	"http.MethodPatch": "PATCH", "http.MethodDelete": "DELETE", "http.MethodHead": "HEAD",
}

// DriftSources names the real files each guard reads. Missing files degrade
// that one guard to a single "skipped" warning rather than a hard failure -
// this tool has no business refusing to build the Knowledge Center because,
// say, the server package moved.
type DriftSources struct {
	AppJSPath        string   // e.g. public/app.js
	ErrorCatalogPath string   // e.g. internal/server/error_catalog_generated.go
	RouteFiles       []string // e.g. internal/server/routes.go, internal/server/routes_public_api_v1.go
}

type route struct {
	Method  string
	Pattern string
}

// DriftGuards checks every article's last_verified age, and - wherever the
// matching source file is readable - its screens/cited-endpoint/cited-error-
// code claims against the real thing. It also reports, once, how many real
// screens have no article mapped to them at all, so authors can see where
// 39.13's remaining coverage gaps are without grepping for it by hand.
func DriftGuards(articles []Article, sources DriftSources, now time.Time) []string {
	var warnings []string

	validScreens, screenErr := extractViewIDs(sources.AppJSPath)
	if screenErr != nil {
		warnings = append(warnings, fmt.Sprintf("drift guard skipped (screen ids): %v", screenErr))
	}
	validRoutes, routeErr := extractRoutes(sources.RouteFiles)
	if routeErr != nil {
		warnings = append(warnings, fmt.Sprintf("drift guard skipped (endpoints): %v", routeErr))
	}
	validCodes, codeErr := extractErrorCodes(sources.ErrorCatalogPath)
	if codeErr != nil {
		warnings = append(warnings, fmt.Sprintf("drift guard skipped (error codes): %v", codeErr))
	}
	knownCodePrefixes := map[string]bool{}
	for code := range validCodes {
		if idx := strings.Index(code, "-"); idx > 0 {
			knownCodePrefixes[code[:idx]] = true
		}
	}

	mappedScreens := map[string]bool{}
	for _, article := range articles {
		if article.LastVerified != "" {
			verified, parseErr := time.Parse("2006-01-02", article.LastVerified)
			if parseErr != nil {
				warnings = append(warnings, fmt.Sprintf("%s: last_verified %q is not a YYYY-MM-DD date", article.SourcePath, article.LastVerified))
			} else if age := now.Sub(verified); age > StaleAfter {
				warnings = append(warnings, fmt.Sprintf("%s: last_verified %s is %d days old (over the %d-day guard) - re-check it still matches the app", article.SourcePath, article.LastVerified, int(age.Hours()/24), int(StaleAfter.Hours()/24)))
			}
		}

		if validScreens != nil {
			for _, screen := range article.Screens {
				screen = strings.ToLower(strings.TrimSpace(screen))
				mappedScreens[screen] = true
				if !validScreens[screen] {
					warnings = append(warnings, fmt.Sprintf("%s: screens: lists %q, which is not a view id dispatched in %s", article.SourcePath, screen, sources.AppJSPath))
				}
			}
		}

		if validRoutes != nil {
			for _, match := range citedEndpointPattern.FindAllStringSubmatch(article.Text, -1) {
				method, path := match[1], match[2]
				if !routeExists(validRoutes, method, path) {
					warnings = append(warnings, fmt.Sprintf("%s: cites %q, which matches no registered route", article.SourcePath, method+" "+path))
				}
			}
		}

		if validCodes != nil {
			for _, match := range citedErrorCodePattern.FindAllStringSubmatch(article.Text, -1) {
				code := match[1]
				prefix := code[:strings.Index(code, "-")]
				if knownCodePrefixes[prefix] && !validCodes[code] {
					warnings = append(warnings, fmt.Sprintf("%s: cites error code %q, which is not in the error catalog", article.SourcePath, code))
				}
			}
		}
	}

	if validScreens != nil {
		var unmapped []string
		for screen := range validScreens {
			if !mappedScreens[screen] {
				unmapped = append(unmapped, screen)
			}
		}
		if len(unmapped) > 0 {
			sort.Strings(unmapped)
			warnings = append(warnings, fmt.Sprintf("%d of %d screens have no Knowledge Center article mapped to them: %s", len(unmapped), len(validScreens), strings.Join(unmapped, ", ")))
		}
	}

	sort.Strings(warnings)
	return warnings
}

func extractViewIDs(path string) (map[string]bool, error) {
	if path == "" {
		return nil, fmt.Errorf("no app.js path configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, match := range viewIDPattern.FindAllStringSubmatch(string(data), -1) {
		ids[match[1]] = true
	}
	return ids, nil
}

func extractErrorCodes(path string) (map[string]bool, error) {
	if path == "" {
		return nil, fmt.Errorf("no error catalog path configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	codes := map[string]bool{}
	for _, match := range errorCodeDefPattern.FindAllStringSubmatch(string(data), -1) {
		codes[match[1]] = true
	}
	return codes, nil
}

func extractRoutes(paths []string) ([]route, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no route files configured")
	}
	var routes []route
	var readErr error
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			readErr = err
			continue
		}
		text := string(data)
		for _, match := range literalRoutePattern.FindAllStringSubmatch(text, -1) {
			routes = append(routes, route{Method: match[1], Pattern: match[2]})
		}
		for _, match := range structRoutePattern.FindAllStringSubmatch(text, -1) {
			method := match[1]
			if strings.HasPrefix(method, "http.Method") {
				method = httpMethodConstants[method]
			} else {
				method = strings.Trim(method, `"`)
			}
			if method != "" {
				routes = append(routes, route{Method: method, Pattern: match[2]})
			}
		}
	}
	if len(routes) == 0 {
		if readErr != nil {
			return nil, readErr
		}
		return nil, fmt.Errorf("no routes found in %v", paths)
	}
	return routes, nil
}

// routeExists matches a cited literal path against every registered pattern,
// treating a Go 1.22 mux {param} segment as a wildcard and a {param...}
// segment as matching the rest of the path.
func routeExists(routes []route, method, path string) bool {
	for _, r := range routes {
		if r.Method != "" && r.Method != method {
			continue
		}
		if routePatternRegex(r.Pattern).MatchString(path) {
			return true
		}
	}
	return false
}

func routePatternRegex(pattern string) *regexp.Regexp {
	segments := strings.Split(pattern, "/")
	for i, seg := range segments {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			if strings.HasSuffix(seg, "...}") {
				segments[i] = ".*"
			} else {
				segments[i] = "[^/]+"
			}
		} else {
			segments[i] = regexp.QuoteMeta(seg)
		}
	}
	return regexp.MustCompile("^" + strings.Join(segments, "/") + "$")
}
