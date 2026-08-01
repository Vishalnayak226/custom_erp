// Command brainmap renders the project "brain" - docs/brain/BRAIN.md and
// docs/brain/brain.html - from three inputs and nothing else:
//
//	docs/brain/brain.map.json   hand-maintained: which brain region owns which files
//	the repo itself             which files exist (so nothing can quietly go unowned)
//	graphify-out/graph.json     machine-extracted: the symbols and the call edges
//
// The point of the split is that the only file a human ever edits is the map.
// Region membership, symbol counts, and every connection line in the diagrams
// are derived from the repo and the graph, so the picture cannot quietly drift
// away from the code the way a hand-drawn architecture diagram does.
//
// Usage (from the repo root):
//
//	graphify update .            # refresh the extracted graph (no API cost)
//	go run ./cmd/brainmap        # redraw the brain
//	go run ./cmd/brainmap -check # exit 1 if any file is unclaimed by a region
//
// On Windows with Controlled Folder Access on, use docs/brain/update-brain.ps1
// instead - see writeOut below for why.
//
// Stdlib only, per the repo's lightweight-and-no-new-dependencies principle.
package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed brain.tmpl.html
var htmlTemplate string

// ---------------------------------------------------------------- input types

// brainMap mirrors docs/brain/brain.map.json.
type brainMap struct {
	SchemaVersion int            `json:"schema_version"`
	Title         string         `json:"title"`
	Subtitle      string         `json:"subtitle"`
	Settings      settings       `json:"settings"`
	Lobes         []lobe         `json:"lobes"`
	Regions       []region       `json:"regions"`
	Declared      []declaredLink `json:"declared_links"`
	Pathways      []pathway      `json:"pathways"`
}

type settings struct {
	OverviewMinWeight int      `json:"overview_min_weight"`
	MaxRegionLinks    int      `json:"max_region_links"`
	TopSymbols        int      `json:"top_symbols"`
	InferredDashRatio float64  `json:"inferred_dash_ratio"`
	HubRegions        []string `json:"hub_regions"`
	Ignore            []string `json:"ignore"`
	GraphPath         string   `json:"graph_path"`
	OutDir            string   `json:"out_dir"`
}

type lobe struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Metaphor string `json:"metaphor"`
	Light    string `json:"light"`
	Dark     string `json:"dark"`
}

type region struct {
	ID       string   `json:"id"`
	Lobe     string   `json:"lobe"`
	Name     string   `json:"name"`
	Role     string   `json:"role"`
	Match    []string `json:"match"`
	Symbols  []string `json:"symbols"`
	Priority int      `json:"priority"`
	Diagram  *bool    `json:"diagram"`
}

func (r region) inDiagram() bool { return r.Diagram == nil || *r.Diagram }

// declaredLink is a connection a call-graph extractor structurally cannot see:
// the browser talking to the server over HTTP, a PowerShell script driving the
// binary, a connector reaching a third-party API. Stated by hand, drawn
// differently from an extracted edge, and never silently mixed in with one.
type declaredLink struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
	Note  string `json:"note"`
}

type pathway struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Note  string `json:"note"`
	Steps []struct {
		Label string `json:"label"`
		Ref   string `json:"ref"`
	} `json:"steps"`
}

// graph mirrors the subset of graphify-out/graph.json this tool reads.
type graph struct {
	Nodes         []gnode `json:"nodes"`
	Links         []glink `json:"links"`
	BuiltAtCommit string  `json:"built_at_commit"`
}

type gnode struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	FileType   string `json:"file_type"`
	SourceFile string `json:"source_file"`
	SourceLoc  string `json:"source_location"`
}

type glink struct {
	Source     string  `json:"source"`
	Target     string  `json:"target"`
	Relation   string  `json:"relation"`
	Confidence string  `json:"confidence"`
	Weight     float64 `json:"weight"`
}

// --------------------------------------------------------------- output types

type symbolOut struct {
	Label  string `json:"label"`
	File   string `json:"file"`
	Loc    string `json:"loc"`
	Degree int    `json:"degree"`
}

type edgeOut struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Weight   int    `json:"weight"`
	Inferred int    `json:"inferred"`
	Declared bool   `json:"declared"`
	Label    string `json:"label"`
	Note     string `json:"note"`
}

type regionOut struct {
	ID      string      `json:"id"`
	Lobe    string      `json:"lobe"`
	Name    string      `json:"name"`
	Role    string      `json:"role"`
	Diagram bool        `json:"diagram"`
	Hub     bool        `json:"hub"`
	Files   []string    `json:"files"`
	Nodes   int         `json:"nodes"`
	Symbols []symbolOut `json:"symbols"`
}

type brainData struct {
	Title     string      `json:"title"`
	Subtitle  string      `json:"subtitle"`
	Commit    string      `json:"commit"`
	Generated string      `json:"generated"`
	Stats     statsOut    `json:"stats"`
	Lobes     []lobe      `json:"lobes"`
	Regions   []regionOut `json:"regions"`
	Edges     []edgeOut   `json:"edges"`
	Pathways  []pathway   `json:"pathways"`
	Unmapped  []string    `json:"unmapped"`
	MinWeight int         `json:"min_weight"`
}

type statsOut struct {
	Nodes        int     `json:"nodes"`
	Links        int     `json:"links"`
	RepoFiles    int     `json:"repo_files"`
	GraphFiles   int     `json:"graph_files"`
	Regions      int     `json:"regions"`
	Lobes        int     `json:"lobes"`
	CrossEdges   int     `json:"cross_edges"`
	InferredPct  float64 `json:"inferred_pct"`
	CoveragePct  float64 `json:"coverage_pct"`
	ExternalRefs int     `json:"external_refs"`
	Declared     int     `json:"declared"`
}

// ------------------------------------------------------------------- matching

// globMatch supports '*' as "any run of characters" and nothing else - enough
// for the path patterns the map file uses, without pulling in a glob library.
func globMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(s, parts[i])
		if idx < 0 {
			return false
		}
		s = s[idx+len(parts[i]):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}

func matchAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if globMatch(p, s) {
			return true
		}
	}
	return false
}

// specificity ranks competing patterns so map order does not matter: the
// pattern that spells out more literal characters wins. An explicit "priority"
// on the region beats specificity outright (that is how the test suite claims
// engines/pim_bulk_test.go instead of the PIM region's engines/pim*.go).
func specificity(pattern string) int {
	return len(strings.ReplaceAll(pattern, "*", ""))
}

type matcher struct{ regions []region }

func (m matcher) forFile(file string) string {
	file = normPath(file)
	if file == "" {
		return ""
	}
	bestID, bestPrio, bestSpec := "", -1, -1
	for _, r := range m.regions {
		for _, pat := range r.Match {
			spec := specificity(pat)
			if !globMatch(pat, file) {
				continue
			}
			if r.Priority > bestPrio || (r.Priority == bestPrio && spec > bestSpec) {
				bestID, bestPrio, bestSpec = r.ID, r.Priority, spec
			}
		}
	}
	return bestID
}

// forSymbol lets one function be pulled out of a file that otherwise belongs
// somewhere else, without splitting the file itself across regions.
func (m matcher) forSymbol(label string) string {
	sym := strings.TrimSuffix(label, "()")
	bestID, bestPrio, bestSpec := "", -1, -1
	for _, r := range m.regions {
		for _, pat := range r.Symbols {
			if !globMatch(pat, sym) {
				continue
			}
			spec := specificity(pat)
			if r.Priority > bestPrio || (r.Priority == bestPrio && spec > bestSpec) {
				bestID, bestPrio, bestSpec = r.ID, r.Priority, spec
			}
		}
	}
	return bestID
}

func normPath(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// ----------------------------------------------------------------------- main

func main() {
	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}
	mapPath := flag.String("map", filepath.Join(root, "docs", "brain", "brain.map.json"), "path to brain.map.json")
	graphPath := flag.String("graph", "", "path to graphify graph.json (default: from brain.map.json settings)")
	outDir := flag.String("out", "", "output directory (default: from brain.map.json settings)")
	check := flag.Bool("check", false, "exit non-zero if any repo file is not claimed by a region")
	flag.Parse()

	bm, err := loadMap(*mapPath)
	if err != nil {
		fatal(err)
	}
	if *graphPath == "" {
		*graphPath = filepath.Join(root, filepath.FromSlash(bm.Settings.GraphPath))
	}
	if *outDir == "" {
		*outDir = filepath.Join(root, filepath.FromSlash(bm.Settings.OutDir))
	}

	g, err := loadGraph(*graphPath)
	if err != nil {
		fatal(err)
	}
	repoFiles, err := walkRepo(root, bm.Settings.Ignore)
	if err != nil {
		fatal(err)
	}

	data := build(bm, g, repoFiles)

	mdPath := filepath.Join(*outDir, "BRAIN.md")
	if err := writeOut(mdPath, []byte(renderMarkdown(bm, data))); err != nil {
		fatal(err)
	}
	htmlPath := filepath.Join(*outDir, "brain.html")
	html, err := renderHTML(data)
	if err != nil {
		fatal(err)
	}
	if err := writeOut(htmlPath, html); err != nil {
		fatal(err)
	}

	fmt.Printf("brainmap: %d regions in %d lobes · %d repo files · %d symbols · %d cross-region relationships (+%d declared)\n",
		data.Stats.Regions, data.Stats.Lobes, data.Stats.RepoFiles, data.Stats.Nodes,
		data.Stats.CrossEdges, data.Stats.Declared)
	fmt.Printf("brainmap: region coverage %.1f%% (%s unclaimed)\n", data.Stats.CoveragePct, plural(len(data.Unmapped), "file"))
	fmt.Printf("brainmap: wrote %s\n", mdPath)
	fmt.Printf("brainmap: wrote %s\n", htmlPath)
	if len(data.Unmapped) > 0 {
		fmt.Fprintf(os.Stderr, "brainmap: %s not claimed by any region:\n", plural(len(data.Unmapped), "file"))
		for _, f := range data.Unmapped {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		if *check {
			os.Exit(1)
		}
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "brainmap: %v\n", err)
	os.Exit(1)
}

// writeOut exists to turn one specific, very confusing Windows failure into an
// actionable message. With Controlled Folder Access on (Defender's ransomware
// protection, on by default), a freshly compiled binary is an "unknown app" and
// is refused any write under %USERPROFILE%\Documents - and Windows reports that
// refusal as ERROR_FILE_NOT_FOUND, so Go surfaces it as "the system cannot find
// the file specified" for a directory that plainly exists.
// docs/brain/update-brain.ps1 sidesteps it by generating into TEMP and copying
// the result in with PowerShell, which Windows already trusts.
func writeOut(path string, content []byte) error {
	err := os.WriteFile(path, content, 0o644)
	if err == nil {
		return nil
	}
	if info, statErr := os.Stat(filepath.Dir(path)); statErr == nil && info.IsDir() {
		return fmt.Errorf("could not write %s (%v).\n"+
			"  The directory exists, so this is almost certainly Windows Controlled Folder Access\n"+
			"  refusing an unknown binary a write under Documents. Run docs/brain/update-brain.ps1\n"+
			"  instead, or pass -out to write somewhere unprotected", path, err)
	}
	return err
}

// repoRoot walks up from the working directory looking for go.mod, so the tool
// works the same whether it is run from the repo root or from cmd/brainmap.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod above %s - run this from inside the repo", dir)
		}
		dir = parent
	}
}

// walkRepo is what makes "did we forget to file something?" answerable. The
// graph only knows about files its extractors parse - not .sql migrations, not
// JSON profiles, not CI config - so region coverage is measured against the
// working tree, not against the graph.
func walkRepo(root string, ignore []string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = normPath(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if matchAny(ignore, rel) || matchAny(ignore, rel+"/") {
				return fs.SkipDir
			}
			return nil
		}
		if matchAny(ignore, rel) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out, err
}

func loadMap(path string) (brainMap, error) {
	var bm brainMap
	raw, err := os.ReadFile(path)
	if err != nil {
		return bm, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &bm); err != nil {
		return bm, fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(bm.Regions) == 0 {
		return bm, fmt.Errorf("%s defines no regions", path)
	}
	if bm.Settings.TopSymbols == 0 {
		bm.Settings.TopSymbols = 6
	}
	if bm.Settings.MaxRegionLinks == 0 {
		bm.Settings.MaxRegionLinks = 8
	}
	if bm.Settings.OverviewMinWeight == 0 {
		bm.Settings.OverviewMinWeight = 10
	}
	if bm.Settings.InferredDashRatio == 0 {
		bm.Settings.InferredDashRatio = 0.9
	}
	if bm.Settings.GraphPath == "" {
		bm.Settings.GraphPath = "graphify-out/graph.json"
	}
	if bm.Settings.OutDir == "" {
		bm.Settings.OutDir = "docs/brain"
	}
	// Every region must name a lobe that exists, and every declared link must
	// name regions that exist - otherwise they would silently vanish from the
	// diagrams instead of failing loudly here.
	lobes := map[string]bool{}
	for _, l := range bm.Lobes {
		lobes[l.ID] = true
	}
	regions := map[string]bool{}
	for _, r := range bm.Regions {
		if !lobes[r.Lobe] {
			return bm, fmt.Errorf("region %q names lobe %q, which is not defined in `lobes`", r.ID, r.Lobe)
		}
		if regions[r.ID] {
			return bm, fmt.Errorf("region id %q is defined twice", r.ID)
		}
		regions[r.ID] = true
	}
	for _, d := range bm.Declared {
		if !regions[d.From] || !regions[d.To] {
			return bm, fmt.Errorf("declared_link %s -> %s names a region that does not exist", d.From, d.To)
		}
	}
	for _, h := range bm.Settings.HubRegions {
		if !regions[h] {
			return bm, fmt.Errorf("settings.hub_regions names %q, which is not a region", h)
		}
	}
	return bm, nil
}

func loadGraph(path string) (graph, error) {
	var g graph
	raw, err := os.ReadFile(path)
	if err != nil {
		return g, fmt.Errorf("reading %s: %w (run `graphify update .` first)", path, err)
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return g, fmt.Errorf("parsing %s: %w", path, err)
	}
	return g, nil
}

// ------------------------------------------------------------------ the build

func build(bm brainMap, g graph, repoFiles []string) brainData {
	m := matcher{regions: bm.Regions}
	inDiagram := map[string]bool{}
	isHub := map[string]bool{}
	for _, r := range bm.Regions {
		inDiagram[r.ID] = r.inDiagram()
	}
	for _, h := range bm.Settings.HubRegions {
		isHub[h] = true
	}

	// Files come from the working tree, so a file nobody filed shows up as a
	// gap rather than simply not existing as far as the brain is concerned.
	regionFiles := map[string][]string{}
	var unmapped []string
	for _, f := range repoFiles {
		rid := m.forFile(f)
		if rid == "" {
			unmapped = append(unmapped, f)
			continue
		}
		regionFiles[rid] = append(regionFiles[rid], f)
	}

	// Symbols and edges come from the graph.
	nodeRegion := make(map[string]string, len(g.Nodes))
	regionNodes := map[string]int{}
	graphFiles := map[string]bool{}
	external := 0
	for _, n := range g.Nodes {
		file := normPath(n.SourceFile)
		if file == "" {
			external++
			continue
		}
		graphFiles[file] = true
		rid := m.forSymbol(n.Label)
		if rid == "" {
			rid = m.forFile(file)
		}
		if rid == "" {
			continue
		}
		nodeRegion[n.ID] = rid
		regionNodes[rid]++
	}

	// Coupling degree deliberately ignores "contains": a file contains every
	// function in its file, which would rank files above the functions that
	// actually do the connecting.
	degree := map[string]int{}
	type edgeKey struct{ from, to string }
	agg := map[edgeKey]*edgeOut{}
	crossCount, inferredCount := 0, 0

	for _, l := range g.Links {
		if l.Relation == "contains" {
			continue
		}
		degree[l.Source]++
		degree[l.Target]++
		from, to := nodeRegion[l.Source], nodeRegion[l.Target]
		if from == "" || to == "" || from == to {
			continue
		}
		// Regions marked diagram:false (the test suite) reach into everything;
		// counting their edges would drown out the structure being described.
		if !inDiagram[from] || !inDiagram[to] {
			continue
		}
		k := edgeKey{from, to}
		e := agg[k]
		if e == nil {
			e = &edgeOut{From: from, To: to}
			agg[k] = e
		}
		e.Weight++
		crossCount++
		if l.Confidence == "INFERRED" {
			e.Inferred++
			inferredCount++
		}
	}

	var edges []edgeOut
	for _, e := range agg {
		edges = append(edges, *e)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Weight != edges[j].Weight {
			return edges[i].Weight > edges[j].Weight
		}
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	for _, d := range bm.Declared {
		edges = append(edges, edgeOut{
			From: d.From, To: d.To, Weight: 0, Declared: true, Label: d.Label, Note: d.Note,
		})
	}

	// Top symbols per region, by coupling degree.
	topSyms := map[string][]symbolOut{}
	for _, n := range g.Nodes {
		rid := nodeRegion[n.ID]
		if rid == "" || n.FileType != "code" {
			continue
		}
		file := normPath(n.SourceFile)
		if n.Label == filepath.Base(file) { // the node standing for the file itself
			continue
		}
		topSyms[rid] = append(topSyms[rid], symbolOut{
			Label: n.Label, File: file, Loc: n.SourceLoc, Degree: degree[n.ID],
		})
	}
	for rid, syms := range topSyms {
		sort.Slice(syms, func(i, j int) bool {
			if syms[i].Degree != syms[j].Degree {
				return syms[i].Degree > syms[j].Degree
			}
			return syms[i].Label < syms[j].Label
		})
		if len(syms) > bm.Settings.TopSymbols {
			syms = syms[:bm.Settings.TopSymbols]
		}
		topSyms[rid] = syms
	}

	// Empty slices must serialise as [] and not null: brain.html reads .length
	// off every one of these, and a region with no files is perfectly normal.
	regions := make([]regionOut, 0, len(bm.Regions))
	for _, r := range bm.Regions {
		files, syms := regionFiles[r.ID], topSyms[r.ID]
		if files == nil {
			files = []string{}
		}
		if syms == nil {
			syms = []symbolOut{}
		}
		regions = append(regions, regionOut{
			ID: r.ID, Lobe: r.Lobe, Name: r.Name, Role: r.Role,
			Diagram: r.inDiagram(), Hub: isHub[r.ID],
			Files: files, Nodes: regionNodes[r.ID], Symbols: syms,
		})
	}
	if unmapped == nil {
		unmapped = []string{}
	}
	if edges == nil {
		edges = []edgeOut{}
	}
	if bm.Pathways == nil {
		bm.Pathways = []pathway{}
	}

	coverage := 100.0
	if len(repoFiles) > 0 {
		coverage = float64(len(repoFiles)-len(unmapped)) / float64(len(repoFiles)) * 100
	}
	inferredPct := 0.0
	if crossCount > 0 {
		inferredPct = float64(inferredCount) / float64(crossCount) * 100
	}
	commit := g.BuiltAtCommit
	if len(commit) > 8 {
		commit = commit[:8]
	}

	return brainData{
		Title:     bm.Title,
		Subtitle:  bm.Subtitle,
		Commit:    commit,
		Generated: time.Now().Format("2006-01-02"),
		Stats: statsOut{
			Nodes: len(g.Nodes), Links: len(g.Links),
			RepoFiles: len(repoFiles), GraphFiles: len(graphFiles),
			Regions: len(bm.Regions), Lobes: len(bm.Lobes),
			CrossEdges: crossCount, InferredPct: inferredPct,
			CoveragePct: coverage, ExternalRefs: external, Declared: len(bm.Declared),
		},
		Lobes:     bm.Lobes,
		Regions:   regions,
		Edges:     edges,
		Pathways:  bm.Pathways,
		Unmapped:  unmapped,
		MinWeight: bm.Settings.OverviewMinWeight,
	}
}

// -------------------------------------------------------------------- markdown

func mermaidID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return "n_" + b.String()
}

func safeLabel(s string) string {
	s = strings.ReplaceAll(s, `"`, "'")
	return strings.ReplaceAll(s, "\n", " ")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

type mdCtx struct {
	d          brainData
	byID       map[string]regionOut
	lobeName   map[string]string
	dashRatio  float64
	maxLinks   int
	extracted  []edgeOut
	declared   []edgeOut
	hubRegions map[string]bool
}

func (c mdCtx) arrow(e edgeOut) string {
	if e.Declared {
		return "==>"
	}
	if e.Weight > 0 && float64(e.Inferred)/float64(e.Weight) >= c.dashRatio {
		return "-.->"
	}
	return "-->"
}

func renderMarkdown(bm brainMap, d brainData) string {
	c := mdCtx{
		d:          d,
		byID:       map[string]regionOut{},
		lobeName:   map[string]string{},
		dashRatio:  bm.Settings.InferredDashRatio,
		maxLinks:   bm.Settings.MaxRegionLinks,
		hubRegions: map[string]bool{},
	}
	for _, r := range d.Regions {
		c.byID[r.ID] = r
		if r.Hub {
			c.hubRegions[r.ID] = true
		}
	}
	for _, l := range d.Lobes {
		c.lobeName[l.ID] = l.Name
	}
	for _, e := range d.Edges {
		if e.Declared {
			c.declared = append(c.declared, e)
		} else {
			c.extracted = append(c.extracted, e)
		}
	}

	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	p("# %s", d.Title)
	p("")
	p("<!-- GENERATED FILE - do not edit by hand. Edit docs/brain/brain.map.json instead. -->")
	p("> **Generated.** Which regions exist comes from [`brain.map.json`](brain.map.json); which files exist comes")
	p("> from the working tree; what calls what comes from `graphify-out/graph.json`. Edit the map, never this")
	p("> file, then run `pwsh docs/brain/update-brain.ps1`. How to do that is in [README.md](README.md).")
	p("")
	p("%s", d.Subtitle)
	p("")
	p("| | |")
	p("|---|---|")
	p("| Graph built from commit | `%s` |", d.Commit)
	p("| Brain redrawn | %s |", d.Generated)
	p("| Regions / lobes | %d / %d |", d.Stats.Regions, d.Stats.Lobes)
	p("| Files in the working tree | %d (%d of them parsed into the graph) |", d.Stats.RepoFiles, d.Stats.GraphFiles)
	p("| Files claimed by a region | **%.1f%%** |", d.Stats.CoveragePct)
	p("| Symbols in the graph | %d |", d.Stats.Nodes)
	p("| Cross-region relationships | %d extracted (%.0f%% inferred) + %d declared by hand |",
		d.Stats.CrossEdges, d.Stats.InferredPct, d.Stats.Declared)
	p("")
	p("**Interactive version: [brain.html](brain.html)** — open it in a browser and click any region.")
	p("")

	// Legend ------------------------------------------------------------
	p("## How to read this")
	p("")
	p("- A **lobe** is a layer of the system; a **region** is one area of responsibility inside it. Which files belong to which region is decided entirely by the `match` patterns in `brain.map.json`.")
	p("- A **thin arrow** is a real relationship graphify extracted from the source — a call, a type reference, a method, an embed — aggregated up to the region level. The number on it is how many such relationships cross that boundary, which is a measure of coupling, not of importance.")
	p("- A **thick `==>` arrow** is *declared by hand* in `brain.map.json`. These are the connections a call-graph extractor structurally cannot see: the browser talking to the server over HTTP, a script driving the binary, a connector reaching a third-party API. They are drawn differently on purpose — they are asserted, not measured.")
	p("- A **solid arrow** contains at least one relationship graphify parsed straight out of the source (`EXTRACTED`). A **dotted arrow** is one where *every* underlying relationship is `INFERRED` — graphify's heuristic guess. Dotted is the common case here, and that is a property of the extractor, not a defect in the code: it resolves calls within a file exactly and calls across files by name, and %.0f%% of cross-region relationships are cross-file by definition. So: the shape of this map is reliable, any *single* dotted edge is a lead to confirm with grep before you rely on it, and a solid arrow is one graphify actually saw.", d.Stats.InferredPct)
	p("- `contains` edges are excluded everywhere: a file containing its own functions says nothing about how areas of the system relate.")
	p("- The test suite is a region but is deliberately left out of every wiring diagram and every count. Tests reach into everything, so drawing them would flatten the real structure into noise.")
	p("")

	renderLobeSection(&b, c)
	renderRegionMap(&b, c)
	renderPathways(&b, c)
	renderIndex(&b, c)
	renderDetail(&b, c)
	renderCoverage(&b, c)
	return b.String()
}

func renderLobeSection(b *strings.Builder, c mdCtx) {
	p := func(format string, args ...any) { fmt.Fprintf(b, format+"\n", args...) }
	d := c.d

	type agg struct{ w, inf int }
	lobeAgg := map[[2]string]*agg{}
	cohesion := map[string]int{}
	for _, e := range c.extracted {
		fr, to := c.byID[e.From], c.byID[e.To]
		if fr.Lobe == to.Lobe {
			cohesion[fr.Lobe] += e.Weight
			continue
		}
		k := [2]string{fr.Lobe, to.Lobe}
		if lobeAgg[k] == nil {
			lobeAgg[k] = &agg{}
		}
		lobeAgg[k].w += e.Weight
		lobeAgg[k].inf += e.Inferred
	}
	// First declaration wins, so the label on a lobe-to-lobe arrow is the one
	// the map file lists first rather than whichever happened to be last.
	declaredLobe := map[[2]string]string{}
	for _, e := range c.declared {
		fr, to := c.byID[e.From], c.byID[e.To]
		if fr.Lobe == to.Lobe {
			continue
		}
		k := [2]string{fr.Lobe, to.Lobe}
		if _, seen := declaredLobe[k]; !seen {
			declaredLobe[k] = e.Label
		}
	}

	p("## 1. The whole brain, at lobe level")
	p("")
	p("%d lobes. If you read one diagram, read this one.", len(d.Lobes))
	p("")
	p("```mermaid")
	p("flowchart LR")
	for _, l := range d.Lobes {
		n, files := 0, 0
		for _, r := range d.Regions {
			if r.Lobe == l.ID {
				n += r.Nodes
				files += len(r.Files)
			}
		}
		p("  %s[\"%s<br/><small>%s · %s</small>\"]", mermaidID(l.ID), safeLabel(l.Name), plural(files, "file"), plural(n, "symbol"))
	}
	var lk [][2]string
	for k := range lobeAgg {
		lk = append(lk, k)
	}
	sort.Slice(lk, func(i, j int) bool { return lobeAgg[lk[i]].w > lobeAgg[lk[j]].w })
	for _, k := range lk {
		e := lobeAgg[k]
		arrow := "-->"
		if float64(e.inf)/float64(e.w) >= c.dashRatio {
			arrow = "-.->"
		}
		p("  %s %s|%d| %s", mermaidID(k[0]), arrow, e.w, mermaidID(k[1]))
	}
	var dk [][2]string
	for k := range declaredLobe {
		dk = append(dk, k)
	}
	sort.Slice(dk, func(i, j int) bool { return dk[i][0]+dk[i][1] < dk[j][0]+dk[j][1] })
	for _, k := range dk {
		p("  %s ==>|%s| %s", mermaidID(k[0]), safeLabel(declaredLobe[k]), mermaidID(k[1]))
	}
	for _, l := range d.Lobes {
		p("  classDef %s stroke:%s,stroke-width:3px;", mermaidID(l.ID), l.Light)
		p("  class %s %s;", mermaidID(l.ID), mermaidID(l.ID))
	}
	p("```")
	p("")
	p("| Lobe | What it is | Regions | Files | Symbols | Wiring inside the lobe |")
	p("|---|---|---:|---:|---:|---:|")
	for _, l := range d.Lobes {
		n, files, sym := 0, 0, 0
		for _, r := range d.Regions {
			if r.Lobe == l.ID {
				n++
				files += len(r.Files)
				sym += r.Nodes
			}
		}
		p("| **%s** | %s | %d | %d | %d | %d |", l.Name, l.Metaphor, n, files, sym, cohesion[l.ID])
	}
	p("")
}

func renderRegionMap(b *strings.Builder, c mdCtx) {
	p := func(format string, args ...any) { fmt.Fprintf(b, format+"\n", args...) }
	d := c.d

	drawRegions := func(skipHubs bool) {
		for _, l := range d.Lobes {
			var members []regionOut
			for _, r := range d.Regions {
				if r.Lobe != l.ID || !r.Diagram {
					continue
				}
				if skipHubs && r.Hub {
					continue
				}
				members = append(members, r)
			}
			if len(members) == 0 {
				continue
			}
			p("  subgraph %s [\"%s\"]", mermaidID("g_"+l.ID), safeLabel(l.Name))
			p("    direction TB")
			for _, r := range members {
				p("    %s[\"%s<br/><small>%s · %s</small>\"]", mermaidID(r.ID), safeLabel(r.Name), plural(len(r.Files), "file"), plural(r.Nodes, "symbol"))
			}
			p("  end")
		}
	}
	styleRegions := func(skipHubs bool) {
		for _, l := range d.Lobes {
			var ids []string
			for _, r := range d.Regions {
				if r.Lobe != l.ID || !r.Diagram {
					continue
				}
				if skipHubs && r.Hub {
					continue
				}
				ids = append(ids, mermaidID(r.ID))
			}
			if len(ids) == 0 {
				continue
			}
			p("  classDef %s stroke:%s,stroke-width:2px;", mermaidID(l.ID), l.Light)
			p("  class %s %s;", strings.Join(ids, ","), mermaidID(l.ID))
		}
	}

	p("## 2. Region map")
	p("")
	p("Every region, grouped by lobe, with the connections of weight **%d or more**. The full set is in [brain.html](brain.html) and in §5 below.", d.MinWeight)
	p("")
	p("```mermaid")
	p("flowchart LR")
	drawRegions(false)
	for _, e := range c.extracted {
		if e.Weight < d.MinWeight || !c.byID[e.From].Diagram || !c.byID[e.To].Diagram {
			continue
		}
		p("  %s %s|%d| %s", mermaidID(e.From), c.arrow(e), e.Weight, mermaidID(e.To))
	}
	for _, e := range c.declared {
		p("  %s ==>|%s| %s", mermaidID(e.From), safeLabel(e.Label), mermaidID(e.To))
	}
	styleRegions(false)
	p("```")
	p("")

	if len(c.hubRegions) > 0 {
		var hubNames []string
		for _, r := range d.Regions {
			if r.Hub {
				hubNames = append(hubNames, "**"+r.Name+"**")
			}
		}
		p("### 2b. The same map with the universal hubs removed")
		p("")
		p("%s are reached from nearly every region — which is the point of them, but it means they dominate the diagram above and hide everything else. Take them out and what is left is how the business areas actually relate to each other.", strings.Join(hubNames, ", "))
		p("")
		p("```mermaid")
		p("flowchart LR")
		drawRegions(true)
		shown := 0
		for _, e := range c.extracted {
			if c.hubRegions[e.From] || c.hubRegions[e.To] {
				continue
			}
			if e.Weight < 3 || !c.byID[e.From].Diagram || !c.byID[e.To].Diagram {
				continue
			}
			p("  %s %s|%d| %s", mermaidID(e.From), c.arrow(e), e.Weight, mermaidID(e.To))
			shown++
		}
		for _, e := range c.declared {
			if c.hubRegions[e.From] || c.hubRegions[e.To] {
				continue
			}
			p("  %s ==>|%s| %s", mermaidID(e.From), safeLabel(e.Label), mermaidID(e.To))
		}
		styleRegions(true)
		p("```")
		p("")
		p("*Showing every non-hub connection of weight 3 or more (%d of them).*", shown)
		p("")
	}

	if len(c.declared) > 0 {
		p("### 2c. Declared connections")
		p("")
		p("These are asserted in `brain.map.json`, not measured. Each one is a boundary a call-graph extractor cannot cross.")
		p("")
		p("| From | To | Boundary | Why it has to be declared |")
		p("|---|---|---|---|")
		for _, e := range c.declared {
			p("| %s | %s | %s | %s |", c.byID[e.From].Name, c.byID[e.To].Name, e.Label, e.Note)
		}
		p("")
	}
}

func renderPathways(b *strings.Builder, c mdCtx) {
	p := func(format string, args ...any) { fmt.Fprintf(b, format+"\n", args...) }
	p("## 3. Signal pathways")
	p("")
	p("The routes a signal actually takes through the brain. These are described by hand in `brain.map.json` (`pathways`) because ordering is intent — a call graph can tell you that A reaches B, not that it must happen third.")
	p("")
	for _, pw := range c.d.Pathways {
		p("### %s", pw.Name)
		p("")
		if pw.Note != "" {
			p("%s", pw.Note)
			p("")
		}
		p("```mermaid")
		p("flowchart LR")
		for i, s := range pw.Steps {
			label := safeLabel(s.Label)
			if s.Ref != "" {
				label += "<br/><small>" + safeLabel(s.Ref) + "</small>"
			}
			p("  %s%d[\"%s\"]", mermaidID(pw.ID), i, label)
			if i > 0 {
				p("  %s%d --> %s%d", mermaidID(pw.ID), i-1, mermaidID(pw.ID), i)
			}
		}
		p("```")
		p("")
	}
}

func renderIndex(b *strings.Builder, c mdCtx) {
	p := func(format string, args ...any) { fmt.Fprintf(b, format+"\n", args...) }
	p("## 4. Region index")
	p("")
	p("| Region | Lobe | Files | Symbols | Busiest connection |")
	p("|---|---|---:|---:|---|")
	for _, r := range c.d.Regions {
		best, bestDir, bestW := "", "", -1
		for _, e := range c.extracted {
			if e.From == r.ID && e.Weight > bestW {
				best, bestDir, bestW = c.byID[e.To].Name, "→", e.Weight
			}
			if e.To == r.ID && e.Weight > bestW {
				best, bestDir, bestW = c.byID[e.From].Name, "←", e.Weight
			}
		}
		busiest := "—"
		if bestW > 0 {
			busiest = fmt.Sprintf("%s %s (%d)", bestDir, best, bestW)
		}
		p("| [%s](#%s) | %s | %d | %d | %s |", r.Name, anchor(r.Name), c.lobeName[r.Lobe], len(r.Files), r.Nodes, busiest)
	}
	p("")
}

func renderDetail(b *strings.Builder, c mdCtx) {
	p := func(format string, args ...any) { fmt.Fprintf(b, format+"\n", args...) }
	p("## 5. Region detail")
	p("")
	for _, l := range c.d.Lobes {
		var members []regionOut
		for _, r := range c.d.Regions {
			if r.Lobe == l.ID {
				members = append(members, r)
			}
		}
		if len(members) == 0 {
			continue
		}
		p("### %s", l.Name)
		p("")
		p("*%s*", l.Metaphor)
		p("")
		for _, r := range members {
			p("#### %s", r.Name)
			p("")
			p("%s", r.Role)
			p("")
			if len(r.Symbols) > 0 {
				p("**Most connected symbols**")
				p("")
				for _, s := range r.Symbols {
					loc := s.File
					if s.Loc != "" {
						loc += "#" + s.Loc
					}
					p("- `%s` — [%s](%s) · degree %d", s.Label, s.File, relLink(loc), s.Degree)
				}
				p("")
			}
			var out, in []edgeOut
			for _, e := range c.d.Edges {
				if e.From == r.ID {
					out = append(out, e)
				} else if e.To == r.ID {
					in = append(in, e)
				}
			}
			if len(out) > c.maxLinks {
				out = out[:c.maxLinks]
			}
			if len(in) > c.maxLinks {
				in = in[:c.maxLinks]
			}
			if len(out) > 0 || len(in) > 0 {
				p("**Wired to**")
				p("")
				for _, e := range out {
					p("- → **%s** — %s", c.byID[e.To].Name, edgeNote(e))
				}
				for _, e := range in {
					p("- ← **%s** — %s", c.byID[e.From].Name, edgeNote(e))
				}
				p("")
			}
			p("<details><summary>%s</summary>", plural(len(r.Files), "file"))
			p("")
			for _, f := range r.Files {
				p("- [%s](%s)", f, relLink(f))
			}
			if len(r.Files) == 0 {
				p("- *(no file in the working tree matches this region's patterns)*")
			}
			p("")
			p("</details>")
			p("")
		}
	}
}

func renderCoverage(b *strings.Builder, c mdCtx) {
	p := func(format string, args ...any) { fmt.Fprintf(b, format+"\n", args...) }
	d := c.d
	p("## 6. What the brain does not know yet")
	p("")
	if len(d.Unmapped) == 0 {
		p("Nothing — every one of the %d files in the working tree is claimed by a region (%.1f%% coverage). "+
			"When that stops being true, the unclaimed files get listed here and `update-brain.ps1 -Check` fails, "+
			"which is the signal to add a `match` pattern (or a whole new region) to `brain.map.json`.",
			d.Stats.RepoFiles, d.Stats.CoveragePct)
	} else {
		p("These %d files exist in the working tree but no region in `brain.map.json` claims them. Each one is a prompt to add a `match` pattern to an existing region — or, if it is genuinely a new area of the system, to add a new region.", len(d.Unmapped))
		p("")
		for _, f := range d.Unmapped {
			p("- `%s`", f)
		}
	}
	p("")
	p("Two other things the brain is honest about not seeing:")
	p("")
	p("- **%d of %d files are parsed into the call graph.** The rest — `.sql` migrations, JSON industry profiles, PowerShell, CI config, Markdown — are filed into regions by path, but contribute no symbols or edges, because graphify has no extractor for them. A region can therefore be substantial and still show few symbols.", d.Stats.GraphFiles, d.Stats.RepoFiles)
	p("- **%d graph nodes are external type references** (`sql.Tx`, `context.Context` and friends) with no source file of their own. They belong to no region by design.", d.Stats.ExternalRefs)
	p("")
}

func edgeNote(e edgeOut) string {
	if e.Declared {
		return "declared: " + e.Label
	}
	if e.Inferred == 0 {
		return plural(e.Weight, "relationship") + ", all extracted"
	}
	return fmt.Sprintf("%s, %d inferred", plural(e.Weight, "relationship"), e.Inferred)
}

// relLink turns a repo-relative path into one relative to docs/brain/.
func relLink(p string) string { return "../../" + p }

func anchor(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ------------------------------------------------------------------------ html

func renderHTML(d brainData) ([]byte, error) {
	var payload bytes.Buffer
	enc := json.NewEncoder(&payload)
	enc.SetEscapeHTML(true) // so a "</script>" inside any label cannot close the block
	if err := enc.Encode(d); err != nil {
		return nil, err
	}
	tmpl, err := template.New("brain").Parse(htmlTemplate)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	err = tmpl.Execute(&out, map[string]any{
		"Title":     d.Title,
		"Data":      template.JS(payload.String()),
		"Generated": d.Generated,
	})
	return out.Bytes(), err
}
