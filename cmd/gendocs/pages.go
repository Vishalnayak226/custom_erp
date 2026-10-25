package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"custom_erp/engines"
	"custom_erp/internal/kb"
	"custom_erp/internal/server"
)

// Stage 135.5: docs/qa/generated/page-inventory.md, the page / action-to-role
// inventory 55.0F works from. Every front-end file under public/ is listed
// with the screens the shell registry routes to it, its owner work packages
// (M01-M30, from the outsider plan's page ownership table) and each API call
// its source makes, matched to the route's handler, gates, capability and the
// roles checkRouteCapability allows. Generic Setup and Configuration pages
// come from the data registry and the settings registry. Nothing here is a
// live inspection; tabs and live results are 55.0F's to record.

const (
	pageInventoryPath = "docs/qa/generated/page-inventory.md"
	ownerPlanPath     = "docs/product/wholeops-outsider-plan-2026-10-23.md"
)

var (
	lazyViewEntry = regexp.MustCompile(`^\s*'?([a-z0-9-]+)'?\s*:\s*\[\s*'([^']+)'\s*,\s*'[^']+'\s*,\s*'([^']+)'`)
	shellView     = regexp.MustCompile(`view === '([a-z0-9-]+)'`)
	apiCall       = regexp.MustCompile("[`'\"](/api/[^`'\"\\s]*)")
	templateExpr  = regexp.MustCompile(`\$\{[^}]*(\}|$)`)
	backticked    = regexp.MustCompile("`([^`]+)`")
)

type pageScreen struct{ view, label string }

func pageInventoryFiles(root, stamp string) (map[string][]byte, error) {
	routes, err := server.RouteAccessTable(filepath.Join(root, "internal", "server", "routes.go"))
	if err != nil {
		return nil, err
	}
	appJS, err := os.ReadFile(filepath.Join(root, "public", "app.js"))
	if err != nil {
		return nil, err
	}
	screens := screensByFile(string(appJS))
	owners, err := planOwners(filepath.Join(root, ownerPlanPath))
	if err != nil {
		return nil, err
	}
	// 47.15.2: each screen's help articles (their `screens:` mapping) and when
	// each was last checked; a checkout without docs/kb lists none.
	help := map[string][]kb.Article{}
	if _, statErr := os.Stat(filepath.Join(root, "docs", "kb")); statErr == nil {
		built, err := kb.Build(filepath.Join(root, "docs", "kb"))
		if err != nil {
			return nil, err
		}
		for _, a := range built.Articles {
			perType := false
			for _, sc := range a.Screens {
				key := strings.ToLower(strings.TrimSpace(sc))
				// doctype:<name> / module:<name> are help for the one Records list.
				if kind, _, ok := strings.Cut(key, ":"); ok && (kind == "doctype" || kind == "module") {
					perType = true
					continue
				}
				help[key] = append(help[key], a)
			}
			if perType {
				help["doctype-table"] = append(help["doctype-table"], a)
			}
		}
	}
	var gaps []string
	var files []string
	for _, pattern := range []string{"*.js", "components/*.js", "store/*.js", "p/*.js", "f/*.js"} {
		matches, err := filepath.Glob(filepath.Join(root, "public", pattern))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			rel, _ := filepath.Rel(filepath.Join(root, "public"), m)
			files = append(files, filepath.ToSlash(rel))
		}
	}
	sort.Strings(files)

	called := map[string]bool{}
	var unmatched [][2]string
	var noCalls []string
	var body strings.Builder
	screenCount, actionCount := 0, 0
	for _, file := range files {
		src, err := os.ReadFile(filepath.Join(root, "public", filepath.FromSlash(file)))
		if err != nil {
			return nil, err
		}
		var rows []server.RouteAccess
		seen := map[string]bool{}
		for _, call := range apiCalls(string(src)) {
			matched := matchRoutes(call, routes)
			if len(matched) == 0 {
				matched = prefixRoutes(call, routes)
			}
			if len(matched) == 0 {
				unmatched = append(unmatched, [2]string{file, call})
			}
			for _, r := range matched {
				if !seen[r.Pattern] {
					seen[r.Pattern] = true
					called[r.Pattern] = true
					rows = append(rows, r)
				}
			}
		}
		if len(rows) == 0 && len(screens[file]) == 0 {
			noCalls = append(noCalls, "`"+file+"`")
			continue
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Path+rows[i].Method < rows[j].Path+rows[j].Method })
		fmt.Fprintf(&body, "### `public/%s`\n\n", file)
		owner := owners[filepath.Base(file)]
		if list := screens[file]; len(list) > 0 {
			var names []string
			for _, s := range list {
				var arts []string
				for _, a := range help[s.view] {
					arts = append(arts, fmt.Sprintf("%s, verified %s", a.Title, a.LastVerified))
				}
				helpText := "no help article"
				if s.view == "doctype-table" && len(arts) > 0 {
					helpText = fmt.Sprintf("help: %d articles, chosen by record type", len(arts))
				} else if len(arts) > 0 {
					helpText = "help: " + strings.Join(arts, "; ")
				}
				names = append(names, fmt.Sprintf("%s (`#/view/%s`; %s)", s.label, s.view, helpText))
				if owner == "" || len(arts) == 0 {
					gaps = append(gaps, fmt.Sprintf("| %s | `%s` | `public/%s` | %s | %s |", s.label, s.view, file, map[bool]string{true: "missing", false: "yes"}[owner == ""], map[bool]string{true: "missing", false: "yes"}[len(arts) == 0]))
				}
			}
			screenCount += len(list)
			fmt.Fprintf(&body, "Screens: %s.  \n", strings.Join(names, ", "))
		}
		if owner == "" {
			owner = "not in the plan's table"
		}
		fmt.Fprintf(&body, "Owners: %s. Live inspection: not recorded.\n\n", owner)
		if len(rows) == 0 {
			body.WriteString("No API calls in this file.\n\n")
			continue
		}
		actionCount += len(rows)
		writeRouteTable(&body, rows)
	}

	var uncalled []server.RouteAccess
	for _, r := range routes {
		if !called[r.Pattern] {
			uncalled = append(uncalled, r)
		}
	}

	var b strings.Builder
	b.WriteString(generatedHeader("Page and action inventory", stamp,
		"`public/**/*.js`, the shell's view registry in `public/app.js`, `internal/server/routes.go` and `route_capabilities.go`, role templates, the data and settings registries, and the owner table in `"+ownerPlanPath+"`",
		"`pwsh docs/update-docs.ps1 -Group Content`"))
	fmt.Fprintf(&b, "Stage 135.5. Each front-end file, the screens the shell routes to it, and each API call its source makes, with the handler, module / feature gate, capability and roles enforced for that route. "+
		"**Roles** are evaluated with `checkRouteCapability`, the check every signed-in request passes, for: %s. "+
		"A module gate also needs the module switched on for the client, and generic record routes also need the role's per-doctype grant. "+
		"**Methods** are every method registered for the called path; the source is not parsed for which one it sends, and a path built from a base is matched to every route under that base. "+
		"**Nothing here was inspected live** and tabs are not derived from source: 55.0F records both. "+
		"**Failure behaviour** is one shared contract for every screen (47.10.6): a refused permission shows *No access*, a missing record *Not found*, any other load failure *Nothing was changed. Trying again is safe.* with a retry.\n\n",
		strings.Join(server.InventoryRoles(), ", "))
	fmt.Fprintf(&b, "| Count | |\n|---|---|\n| Front-end files with screens or API calls | %d |\n| Screens in the shell registry | %d |\n| File-to-route actions | %d |\n| Registered `/api` routes | %d |\n| Routes no front-end file calls | %d |\n| Calls matching no route | %d |\n\n",
		strings.Count(body.String(), "### `public/"), screenCount, actionCount, len(routes), len(uncalled), len(unmatched))
	b.WriteString("## Screens and front-end files\n\n")
	b.WriteString(body.String())
	if len(noCalls) > 0 {
		fmt.Fprintf(&b, "No screen and no API call: %s.\n\n", strings.Join(noCalls, ", "))
	}
	// 47.15.2: every screen needs an owner and a help article.
	fmt.Fprintf(&b, "## Screens missing an owner or a help article\n\n%d of %d screens. An owner comes from the plan's page ownership table, an article from a Knowledge Center article's `screens:` list.\n\n", len(gaps), screenCount)
	if len(gaps) > 0 {
		b.WriteString("| Screen | View | File | Owner | Help article |\n|---|---|---|---|---|\n" + strings.Join(gaps, "\n") + "\n\n")
	}
	if err := writeSetupSections(&b, root, routes); err != nil {
		return nil, err
	}
	b.WriteString("## Routes no front-end file calls\n\nIntegration, device, webhook or customer-link routes, calls built in a way the scan cannot read, or routes nothing uses. Inspect before relying on either.\n\n")
	writeRouteTable(&b, uncalled)
	b.WriteString("## Calls matching no registered route\n\nA path in front-end source that no `/api` registration in routes.go matches: a comment, a path built from pieces, or a call that will fail.\n\n| File | Call |\n|---|---|\n")
	for _, u := range unmatched {
		fmt.Fprintf(&b, "| `%s` | `%s` |\n", u[0], u[1])
	}
	return map[string][]byte{pageInventoryPath: []byte(b.String())}, nil
}

// screensByFile maps each module file to the screens the registry routes to
// it; the views app.js renders itself belong to app.js.
func screensByFile(appJS string) map[string][]pageScreen {
	out := map[string][]pageScreen{}
	registered := map[string]bool{}
	if start := strings.Index(appJS, "const LAZY_VIEW_MODULES = {"); start >= 0 {
		block := appJS[start:]
		if end := strings.Index(block, "\n};"); end >= 0 {
			block = block[:end]
		}
		for _, line := range strings.Split(block, "\n") {
			if m := lazyViewEntry.FindStringSubmatch(line); m != nil {
				out[m[2]] = append(out[m[2]], pageScreen{m[1], m[3]})
				registered[m[1]] = true
			}
		}
	}
	for _, m := range shellView.FindAllStringSubmatch(appJS, -1) {
		if !registered[m[1]] {
			registered[m[1]] = true
			out["app.js"] = append(out["app.js"], pageScreen{m[1], m[1]})
		}
	}
	return out
}

// planOwners reads the outsider plan's page ownership table: file -> owners.
func planOwners(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	out := map[string]string{}
	start := strings.Index(text, "### Page ownership inventory")
	if start < 0 {
		return out, nil
	}
	for _, line := range strings.Split(text[start:], "\n")[1:] {
		if strings.HasPrefix(line, "### ") {
			break
		}
		cells := strings.Split(strings.Trim(line, "| "), "|")
		if len(cells) != 2 || !strings.Contains(cells[0], ".js`") {
			continue
		}
		for _, name := range backticked.FindAllStringSubmatch(cells[0], -1) {
			out[name[1]] = strings.TrimSpace(cells[1])
		}
	}
	return out, nil
}

// apiCalls returns the distinct /api paths a source names, with each
// template expression reduced to {} and any query string dropped.
func apiCalls(src string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range apiCall.FindAllStringSubmatch(src, -1) {
		call := templateExpr.ReplaceAllString(m[1], "{}")
		if i := strings.IndexAny(call, "?#"); i >= 0 {
			call = call[:i]
		}
		if strings.HasSuffix(call, "/") {
			call += "{}" // '/api/v1/doc/' + doctype
		}
		if !seen[call] {
			seen[call] = true
			out = append(out, call)
		}
	}
	return out
}

// matchRoutes returns the routes a call reaches, preferring the patterns
// whose literal segments match most (the mux's own most-specific rule).
func matchRoutes(call string, routes []server.RouteAccess) []server.RouteAccess {
	cs := strings.Split(strings.Trim(call, "/"), "/")
	var best []server.RouteAccess
	bestScore := -1
	for _, r := range routes {
		score, ok := segmentsMatch(cs, strings.Split(strings.Trim(r.Path, "/"), "/"))
		if !ok {
			continue
		}
		if score > bestScore {
			best, bestScore = nil, score
		}
		if score == bestScore {
			best = append(best, r)
		}
	}
	return best
}

// prefixRoutes is the fallback for a path built from a base, such as
// `${base}/preview` after `const base = '/api/v1/...'`: every route under it.
func prefixRoutes(call string, routes []server.RouteAccess) []server.RouteAccess {
	cs := strings.Split(strings.Trim(strings.TrimSuffix(call, "/{}"), "/"), "/")
	if len(cs) < 3 || strings.Contains(strings.Join(cs, "/"), "{}") {
		return nil // "/api/" + anything names no base
	}
	var out []server.RouteAccess
	for _, r := range routes {
		ps := strings.Split(strings.Trim(r.Path, "/"), "/")
		if len(ps) <= len(cs) {
			continue
		}
		if _, ok := segmentsMatch(cs, ps[:len(cs)]); ok {
			out = append(out, r)
		}
	}
	return out
}

func segmentsMatch(call, pattern []string) (int, bool) {
	score := 0
	for i, p := range pattern {
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "...}") {
			return score, i < len(call)
		}
		if i >= len(call) {
			return 0, false
		}
		switch c := call[i]; {
		case c == p:
			score++
		case strings.HasPrefix(p, "{"), strings.Contains(c, "{}"):
		default:
			return 0, false
		}
	}
	return score, len(call) == len(pattern)
}

func writeRouteTable(b *strings.Builder, rows []server.RouteAccess) {
	b.WriteString("| Method | Route | Handler | Gate | Capability | Roles |\n|---|---|---|---|---|---|\n")
	for _, r := range rows {
		gate := strings.Trim(r.ModuleGate+" / "+r.FeatureGate, " /")
		capability := r.Capability
		if capability == "" {
			capability = r.Level
		} else {
			capability = r.Level + ": " + capability
		}
		fmt.Fprintf(b, "| %s | `%s` | `%s` | %s | %s | %s |\n", r.Method, r.Path, r.Handler, gate, capability, inventoryRoles(r))
	}
	b.WriteString("\n")
}

func inventoryRoles(r server.RouteAccess) string {
	switch {
	case r.Level == "public" || r.Level == "no session":
		return "anyone"
	case r.Roles == nil:
		return "every signed-in role"
	case len(r.Roles) == 0:
		return "no role"
	}
	return strings.Join(r.Roles, ", ")
}

// writeSetupSections covers the pages generated from metadata rather than a
// file: one generic list and form per doctype, and Settings → Configuration.
func writeSetupSections(b *strings.Builder, root string, routes []server.RouteAccess) error {
	raw, err := os.ReadFile(filepath.Join(root, "docs/data/registry-snapshot.json"))
	if err != nil {
		return err
	}
	var snapshot RegistrySnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	byModule := map[string][]string{}
	for _, d := range snapshot.Doctypes {
		byModule[d.Module] = append(byModule[d.Module], d.Name)
	}
	modules := make([]string, 0, len(byModule))
	for m := range byModule {
		modules = append(modules, m)
	}
	sort.Strings(modules)
	fmt.Fprintf(b, "## Generic Setup pages\n\nEach record type is one generic list and form (`#/view/doctype-table`, `view-documents.js` and the shell's document form), owned per the plan by the shared contract across M01-M30. "+
		"Who may read or change a record type is the role's per-doctype grant (Settings → Roles), on top of the routes below. Record types from the data registry snapshot captured %s; types added since appear at the next capture.\n\n", snapshot.CapturedOn)
	var docRoutes, settingRoutes []server.RouteAccess
	for _, r := range routes {
		if strings.HasPrefix(r.Path, "/api/v1/doc/") || r.Path == "/api/v1/doc" {
			docRoutes = append(docRoutes, r)
		}
		if strings.Contains(r.Path, "/settings") {
			settingRoutes = append(settingRoutes, r)
		}
	}
	writeRouteTable(b, docRoutes)
	b.WriteString("| Module | Record types |\n|---|---|\n")
	for _, m := range modules {
		names := byModule[m]
		sort.Strings(names)
		fmt.Fprintf(b, "| %s | %s |\n", m, strings.Join(names, ", "))
	}
	b.WriteString("\n## Configuration settings\n\nSettings → Configuration renders the settings registry by module, through the routes below.\n\n")
	writeRouteTable(b, settingRoutes)
	count := map[string]int{}
	for _, d := range engines.ListSettingDefinitions() {
		count[d.Module]++
	}
	modules = modules[:0]
	for m := range count {
		modules = append(modules, m)
	}
	sort.Strings(modules)
	b.WriteString("| Module | Settings |\n|---|---|\n")
	for _, m := range modules {
		fmt.Fprintf(b, "| %s | %d |\n", m, count[m])
	}
	b.WriteString("\n")
	return nil
}
