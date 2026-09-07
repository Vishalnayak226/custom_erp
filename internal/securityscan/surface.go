// Package securityscan derives this repository's security-relevant facts from
// its own source, with no build tags, no runtime hooks and no third-party
// tooling.
//
// Stage 49.1.1 asks for a machine-readable inventory of "HTTP/static/debug/
// health/metrics routes, methods/content types/auth class ... files/downloads/
// uploads; jobs/schedules; CLI/admin commands; migrations; ... outbound
// destinations; ... ports; environment flags and shipped assets", and 49.1.6
// asks for a drift check that compares the current state to an approved
// profile. Both are served by one generator: scanning the source produces the
// inventory, and comparing that inventory to the committed
// docs/security/attack_surface.json is the drift check.
//
// Why source scanning and not a runtime registry: a runtime inventory needs
// the server booted, a database, and code in the production binary that exists
// only to describe itself. This approach costs the production build exactly
// nothing - the package is imported by a test and a cmd/ tool, never by
// internal/server or engines - which is what 49.18.4 and the project's
// lightweight rule require. It also means the inventory can be regenerated
// from a clean checkout with no infrastructure at all, which is what makes it
// usable as a release gate.
//
// The trade-off is honest and bounded: this reads literal registrations, so a
// route registered through a variable or a loop is reported as the template it
// is, annotated rather than silently missed. The completeness guard is
// TestEveryRouteRegistrationIsRecognised, which fails if routes.go contains a
// registration line this file's patterns do not classify - so the scanner
// cannot quietly go blind as the route table grows.
package securityscan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SchemaVersion is bumped whenever the shape of Surface changes, so a stale
// committed manifest is recognisable as stale rather than merely different.
const SchemaVersion = 1

// Surface is the whole machine-readable attack-surface inventory. Every slice
// is sorted, so regenerating it on an unchanged tree produces byte-identical
// output and any diff is a real change.
type Surface struct {
	SchemaVersion     int                `json:"schema_version"`
	Module            string             `json:"module"`
	Routes            []Route            `json:"routes"`
	StaticRoots       []StaticRoot       `json:"static_roots"`
	BackgroundJobs    []BackgroundJob    `json:"background_jobs"`
	CLICommands       []CLICommand       `json:"cli_commands"`
	Migrations        MigrationSummary   `json:"migrations"`
	EnvironmentFlags  []EnvironmentFlag  `json:"environment_flags"`
	OutboundCallSites []OutboundCallSite `json:"outbound_call_sites"`
	Dependencies      []Dependency       `json:"dependencies"`
	Totals            map[string]int     `json:"totals"`
}

// Auth classes. These describe which gate a request passes through before it
// reaches a handler - the fact 49.1.1 calls "auth class". They deliberately do
// not describe what the handler then allows: role/capability classification is
// 47.1.1's routeCapabilities registry, which has its own completeness test.
const (
	AuthSession       = "session"                // apiMiddleware: a signed bearer token is required
	AuthPublic        = "public"                 // apiMiddleware, but on middleware.go's publicRoutes allowlist
	AuthIntegration   = "integration-credential" // publicAPIMiddleware: an API credential with a declared scope
	AuthNone          = "none"                   // registered with no middleware at all
	AuthStaticContent = "static"                 // static asset or SPA shell, serves no tenant data
)

type Route struct {
	Method string `json:"method"` // "GET", "POST", ... or "ANY" when registered with no method prefix
	Path   string `json:"path"`
	Auth   string `json:"auth"`
	Scope  string `json:"scope,omitempty"` // integration credential scope, where one applies
	Source string `json:"source"`          // file:line of the registration
	Note   string `json:"note,omitempty"`
}

type StaticRoot struct {
	Path      string `json:"path"`
	Directory string `json:"directory"`
	// DirectoryListing is whether this root actually serves a generated index
	// for a directory that has none of its own. False since 49.1.2 wrapped the
	// file server in noDirectoryListing; the subdirectories that would be
	// enumerable without that wrapper are listed below, because they are what
	// would be exposed again if it were ever removed.
	DirectoryListing           bool     `json:"directory_listing"`
	Subdirectories             []string `json:"subdirectories"`
	SubdirectoriesWithoutIndex []string `json:"subdirectories_without_index"`
	FileCount                  int      `json:"file_count"`
	Source                     string   `json:"source"`
}

type BackgroundJob struct {
	Starter  string `json:"starter"`
	Interval string `json:"interval"`
	Source   string `json:"source"`
}

type CLICommand struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type MigrationSummary struct {
	BaseSchemaFile  string   `json:"base_schema_file"`
	IncrementalFile int      `json:"incremental_files"`
	Newest          []string `json:"newest"`
}

type EnvironmentFlag struct {
	Name             string   `json:"name"`
	SecurityCritical bool     `json:"security_critical"`
	ReadBy           []string `json:"read_by"`
}

type OutboundCallSite struct {
	File string `json:"file"`
	Kind string `json:"kind"`
	Uses int    `json:"uses"`
}

type Dependency struct {
	Module  string `json:"module"`
	Version string `json:"version"`
	Direct  bool   `json:"direct"`
}

// --- registration patterns -------------------------------------------------
//
// Kept together so TestEveryRouteRegistrationIsRecognised can assert that
// every http.Handle/http.HandleFunc line in routes.go matches one of them.

var (
	// http.HandleFunc("GET /api/v1/thing", apiMiddleware(handler))
	middlewareRoute = regexp.MustCompile(`http\.HandleFunc\(\s*"((?:[A-Z]+ )?[^"]+)"\s*,\s*apiMiddleware\(`)
	// http.HandleFunc("GET /internal/tls-ask", handleTLSAsk)
	bareRoute = regexp.MustCompile(`http\.HandleFunc\(\s*"((?:[A-Z]+ )?[^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`)
	// http.Handle("/", fs)
	handleRoute = regexp.MustCompile(`http\.Handle\(\s*"([^"]+)"\s*,`)
	// http.HandleFunc("GET "+prefix, spaShell) - a loop over engines.ProductPackages
	templatedRoute = regexp.MustCompile(`http\.HandleFunc\(\s*"([A-Z]+ )"\s*\+\s*([A-Za-z_][A-Za-z0-9_]*)([^,]*),\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`)
	// any registration at all, for the completeness guard
	anyRegistration = regexp.MustCompile(`http\.Handle(Func)?\(`)

	// {Method: http.MethodGet, Path: "/api/public/v1/items", Scope: "items:read",
	publicAPIRouteDecl = regexp.MustCompile(`Method:\s*http\.Method([A-Za-z]+),\s*Path:\s*"([^"]+)",\s*Scope:\s*"([^"]+)"`)

	// "/api/v1/login": true, inside middleware.go's publicRoutes map
	publicRouteEntry = regexp.MustCompile(`^\s*"(/[^"]+)":\s*true,`)

	workerStart  = regexp.MustCompile(`engines\.(Start[A-Za-z0-9_]+)\(\s*workerCtx\s*,\s*([^)]+)\)`)
	getenvRead   = regexp.MustCompile(`os\.Getenv\("([A-Z][A-Z0-9_]*)"\)`)
	envVarInList = regexp.MustCompile(`^\s*"([A-Z][A-Z0-9_]*)",\s*$`)
	goModRequire = regexp.MustCompile(`^\s*([a-z0-9][^\s]+)\s+(v[^\s]+)(\s+//\s*indirect)?`)
)

var outboundCallKinds = map[string]*regexp.Regexp{
	"http-request":   regexp.MustCompile(`http\.NewRequest(WithContext)?\(`),
	"http-shorthand": regexp.MustCompile(`http\.(Get|Post|PostForm|Head)\(`),
	"smtp":           regexp.MustCompile(`smtp\.(SendMail|Dial|PlainAuth)\(`),
	"raw-socket":     regexp.MustCompile(`net\.Dial(Timeout)?\(`),
}

// ScanSurface walks the repository rooted at root and returns its inventory.
func ScanSurface(root string) (*Surface, error) {
	s := &Surface{SchemaVersion: SchemaVersion, Totals: map[string]int{}}

	module, err := scanGoMod(root, s)
	if err != nil {
		return nil, err
	}
	s.Module = module

	publicPaths, err := scanPublicRouteAllowlist(root)
	if err != nil {
		return nil, err
	}
	if err := scanRoutesFile(root, publicPaths, s); err != nil {
		return nil, err
	}
	if err := scanPublicAPIRoutes(root, s); err != nil {
		return nil, err
	}
	if err := scanStaticRoots(root, s); err != nil {
		return nil, err
	}
	if err := scanCLICommands(root, s); err != nil {
		return nil, err
	}
	if err := scanMigrations(root, s); err != nil {
		return nil, err
	}
	if err := scanEnvironmentAndOutbound(root, s); err != nil {
		return nil, err
	}

	sortSurface(s)
	countTotals(s)
	return s, nil
}

func readLines(root string, rel ...string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(append([]string{root}, rel...)...))
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), nil
}

// scanPublicRouteAllowlist reads middleware.go's publicRoutes map - the only
// list of endpoints reachable with no bearer token. Parsed rather than
// duplicated so this inventory cannot disagree with the running server about
// which routes are open.
func scanPublicRouteAllowlist(root string) (map[string]bool, error) {
	lines, err := readLines(root, "internal", "server", "middleware.go")
	if err != nil {
		return nil, err
	}
	public := map[string]bool{}
	inMap := false
	for _, line := range lines {
		if strings.HasPrefix(line, "var publicRoutes = map[string]bool{") {
			inMap = true
			continue
		}
		if inMap {
			if strings.HasPrefix(line, "}") {
				break
			}
			if m := publicRouteEntry.FindStringSubmatch(line); m != nil {
				public[m[1]] = true
			}
		}
	}
	if len(public) == 0 {
		return nil, fmt.Errorf("found no entries in middleware.go's publicRoutes map - the extraction pattern is broken, not the allowlist")
	}
	return public, nil
}

func splitMethodPath(pattern string) (method, path string) {
	if method, rest, found := strings.Cut(pattern, " "); found {
		return method, rest
	}
	return "ANY", pattern
}

func scanRoutesFile(root string, publicPaths map[string]bool, s *Surface) error {
	const rel = "internal/server/routes.go"
	lines, err := readLines(root, "internal", "server", "routes.go")
	if err != nil {
		return err
	}
	for i, line := range lines {
		src := fmt.Sprintf("%s:%d", rel, i+1)
		if m := middlewareRoute.FindStringSubmatch(line); m != nil {
			method, path := splitMethodPath(m[1])
			auth := AuthSession
			if publicPaths[path] {
				auth = AuthPublic
			}
			note := ""
			if strings.Contains(line, "debug") {
				note = "registered only when ENV is not production (49.1.2)"
			}
			s.Routes = append(s.Routes, Route{Method: method, Path: path, Auth: auth, Source: src, Note: note})
			continue
		}
		if m := templatedRoute.FindStringSubmatch(line); m != nil {
			method := strings.TrimSpace(m[1])
			auth, note := AuthStaticContent, "expanded per engines.ProductPackages at registration; serves the SPA shell only"
			if m[4] != "spaShell" {
				auth, note = AuthNone, "registered from a non-literal pattern; handler "+m[4]
			}
			s.Routes = append(s.Routes, Route{
				Method: method, Path: "{" + m[2] + "}" + strings.TrimSpace(m[3]),
				Auth: auth, Source: src, Note: note,
			})
			continue
		}
		if m := bareRoute.FindStringSubmatch(line); m != nil {
			method, path := splitMethodPath(m[1])
			auth, note := AuthNone, "registered with no middleware - handler is solely responsible for its own access control"
			if m[2] == "spaShell" {
				auth, note = AuthStaticContent, "serves the SPA shell only"
			}
			s.Routes = append(s.Routes, Route{Method: method, Path: path, Auth: auth, Source: src, Note: note})
			continue
		}
		if m := handleRoute.FindStringSubmatch(line); m != nil {
			s.Routes = append(s.Routes, Route{
				Method: "ANY", Path: m[1], Auth: AuthStaticContent, Source: src,
				Note: "static file server; see static_roots",
			})
			continue
		}
		if m := workerStart.FindStringSubmatch(line); m != nil {
			s.BackgroundJobs = append(s.BackgroundJobs, BackgroundJob{
				Starter: m[1], Interval: strings.TrimSpace(m[2]), Source: src,
			})
		}
	}
	if len(s.Routes) == 0 {
		return fmt.Errorf("found no route registrations in %s - the extraction patterns are broken, not the route table", rel)
	}
	return nil
}

func scanPublicAPIRoutes(root string, s *Surface) error {
	const rel = "internal/server/routes_public_api_v1.go"
	lines, err := readLines(root, "internal", "server", "routes_public_api_v1.go")
	if err != nil {
		return err
	}
	for i, line := range lines {
		if m := publicAPIRouteDecl.FindStringSubmatch(line); m != nil {
			s.Routes = append(s.Routes, Route{
				Method: strings.ToUpper(m[1]), Path: m[2], Auth: AuthIntegration, Scope: m[3],
				Source: fmt.Sprintf("%s:%d", rel, i+1),
				Note:   "publicAPIMiddleware refuses registration of a route with no scope",
			})
		}
	}
	return nil
}

// scanStaticRoots records what the file server actually exposes, including
// whether a subdirectory would be enumerable. 49.1.2's directory-listing
// finding was invisible until this was written down.
func scanStaticRoots(root string, s *Surface) error {
	publicDir := filepath.Join(root, "public")
	info, err := os.Stat(publicDir)
	if err != nil || !info.IsDir() {
		return nil
	}
	var subdirs, withoutIndex []string
	fileCount := 0
	err = filepath.Walk(publicDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			if path == publicDir {
				return nil
			}
			rel, _ := filepath.Rel(publicDir, path)
			rel = filepath.ToSlash(rel)
			subdirs = append(subdirs, rel)
			if _, err := os.Stat(filepath.Join(path, "index.html")); err != nil {
				withoutIndex = append(withoutIndex, rel)
			}
			return nil
		}
		fileCount++
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(subdirs)
	sort.Strings(withoutIndex)
	// DirectoryListing is false by construction: internal/server/routes.go
	// wraps http.Dir in noDirectoryListing (49.1.2), and
	// TestStaticFileServerNeverListsADirectory holds that true.
	s.StaticRoots = append(s.StaticRoots, StaticRoot{
		Path: "/", Directory: "public", DirectoryListing: false,
		Subdirectories: subdirs, SubdirectoriesWithoutIndex: withoutIndex, FileCount: fileCount,
		Source: "internal/server/routes.go (http.FileServer wrapped by noDirectoryListing)",
	})
	return nil
}

func scanCLICommands(root string, s *Surface) error {
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() {
			s.CLICommands = append(s.CLICommands, CLICommand{Name: e.Name(), Path: "cmd/" + e.Name()})
		}
	}
	return nil
}

func scanMigrations(root string, s *Surface) error {
	entries, err := os.ReadDir(filepath.Join(root, "db"))
	if err != nil {
		return nil
	}
	var incremental []string
	base := ""
	for _, e := range entries {
		name := e.Name()
		switch {
		case name == "migration.sql":
			base = "db/migration.sql"
		case strings.HasPrefix(name, "migrations_") && strings.HasSuffix(name, ".sql"):
			incremental = append(incremental, name)
		}
	}
	sort.Strings(incremental)
	newest := incremental
	if len(newest) > 5 {
		newest = newest[len(newest)-5:]
	}
	s.Migrations = MigrationSummary{BaseSchemaFile: base, IncrementalFile: len(incremental), Newest: append([]string{}, newest...)}
	return nil
}

// scanEnvironmentAndOutbound walks every non-test .go file once, collecting
// both the environment flags it reads and the outbound-call sites it contains.
func scanEnvironmentAndOutbound(root string, s *Surface) error {
	critical, err := securityCriticalEnvVarNames(root)
	if err != nil {
		return err
	}
	readBy := map[string]map[string]bool{}
	outbound := map[string]map[string]int{}

	err = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			switch fi.Name() {
			// ".claude" holds agent worktrees - full, gitignored copies of this
			// repository - and counting their files reported the same outbound
			// call site once per worktree. Same omission, and same reason, as
			// skippedDirs in bypass.go.
			case ".git", "node_modules", "graphify-out", "docs", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		text := string(data)
		for _, m := range getenvRead.FindAllStringSubmatch(text, -1) {
			if readBy[m[1]] == nil {
				readBy[m[1]] = map[string]bool{}
			}
			readBy[m[1]][rel] = true
		}
		for kind, pattern := range outboundCallKinds {
			if n := len(pattern.FindAllString(text, -1)); n > 0 {
				if outbound[rel] == nil {
					outbound[rel] = map[string]int{}
				}
				outbound[rel][kind] += n
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for name, files := range readBy {
		var list []string
		for f := range files {
			list = append(list, f)
		}
		sort.Strings(list)
		s.EnvironmentFlags = append(s.EnvironmentFlags, EnvironmentFlag{
			Name: name, SecurityCritical: critical[name], ReadBy: list,
		})
	}
	for file, kinds := range outbound {
		for kind, n := range kinds {
			s.OutboundCallSites = append(s.OutboundCallSites, OutboundCallSite{File: file, Kind: kind, Uses: n})
		}
	}
	return nil
}

// securityCriticalEnvVarNames reads engines/security_baseline.go's own
// securityCriticalEnvVars list, so the inventory's "security_critical" flag
// and the startup validator's coverage can never disagree.
func securityCriticalEnvVarNames(root string) (map[string]bool, error) {
	lines, err := readLines(root, "engines", "security_baseline.go")
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	inList := false
	for _, line := range lines {
		if strings.HasPrefix(line, "var securityCriticalEnvVars = []string{") {
			inList = true
			continue
		}
		if inList {
			if strings.HasPrefix(line, "}") {
				break
			}
			if m := envVarInList.FindStringSubmatch(line); m != nil {
				out[m[1]] = true
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("found no entries in engines/security_baseline.go's securityCriticalEnvVars - the extraction pattern is broken")
	}
	return out, nil
}

func scanGoMod(root string, s *Surface) (string, error) {
	lines, err := readLines(root, "go.mod")
	if err != nil {
		return "", err
	}
	module := ""
	inRequire := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") {
			module = strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
			continue
		}
		if strings.HasPrefix(trimmed, "require (") {
			inRequire = true
			continue
		}
		if inRequire {
			if trimmed == ")" {
				inRequire = false
				continue
			}
			if m := goModRequire.FindStringSubmatch(line); m != nil {
				s.Dependencies = append(s.Dependencies, Dependency{
					Module: m[1], Version: m[2], Direct: m[3] == "",
				})
			}
		}
	}
	return module, nil
}

func sortSurface(s *Surface) {
	sort.Slice(s.Routes, func(i, j int) bool {
		if s.Routes[i].Path != s.Routes[j].Path {
			return s.Routes[i].Path < s.Routes[j].Path
		}
		return s.Routes[i].Method < s.Routes[j].Method
	})
	sort.Slice(s.BackgroundJobs, func(i, j int) bool { return s.BackgroundJobs[i].Starter < s.BackgroundJobs[j].Starter })
	sort.Slice(s.CLICommands, func(i, j int) bool { return s.CLICommands[i].Name < s.CLICommands[j].Name })
	sort.Slice(s.EnvironmentFlags, func(i, j int) bool { return s.EnvironmentFlags[i].Name < s.EnvironmentFlags[j].Name })
	sort.Slice(s.OutboundCallSites, func(i, j int) bool {
		if s.OutboundCallSites[i].File != s.OutboundCallSites[j].File {
			return s.OutboundCallSites[i].File < s.OutboundCallSites[j].File
		}
		return s.OutboundCallSites[i].Kind < s.OutboundCallSites[j].Kind
	})
	sort.Slice(s.Dependencies, func(i, j int) bool { return s.Dependencies[i].Module < s.Dependencies[j].Module })
	sort.Slice(s.StaticRoots, func(i, j int) bool { return s.StaticRoots[i].Path < s.StaticRoots[j].Path })
}

func countTotals(s *Surface) {
	byAuth := map[string]int{}
	for _, r := range s.Routes {
		byAuth[r.Auth]++
	}
	s.Totals["routes"] = len(s.Routes)
	for auth, n := range byAuth {
		s.Totals["routes_"+strings.ReplaceAll(auth, "-", "_")] = n
	}
	s.Totals["background_jobs"] = len(s.BackgroundJobs)
	s.Totals["cli_commands"] = len(s.CLICommands)
	s.Totals["incremental_migrations"] = s.Migrations.IncrementalFile
	s.Totals["environment_flags"] = len(s.EnvironmentFlags)
	critical := 0
	for _, f := range s.EnvironmentFlags {
		if f.SecurityCritical {
			critical++
		}
	}
	s.Totals["environment_flags_security_critical"] = critical
	s.Totals["outbound_call_sites"] = len(s.OutboundCallSites)
	s.Totals["direct_dependencies"] = 0
	for _, d := range s.Dependencies {
		if d.Direct {
			s.Totals["direct_dependencies"]++
		}
	}
	s.Totals["dependencies"] = len(s.Dependencies)
}
