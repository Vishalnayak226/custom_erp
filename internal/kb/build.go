package kb

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Stage 39.2 - the Knowledge Center builder.
//
// Markdown in docs/kb/ goes in; inert HTML fragments, a navigation index and a
// prebuilt search index come out. Everything is stdlib: no static-site
// generator, no bundler, no docs framework, no search service. The whole build
// is one function so the generator command, the drift check and the tests all
// exercise the same code rather than three approximations of it.
//
// WHERE THE OUTPUT GOES, and why it is not public/help/:
//
// The application serves public/ straight off disk with http.FileServer. Any
// article written there would be readable by anyone who can reach the host,
// which directly contradicts 39.6's "authenticated by default". So the build
// writes into internal/kb/content/, which is embedded into the binary and only
// reachable through an authenticated handler. Articles that opt in with
// `public: true` are served by one narrow unauthenticated endpoint instead.
// This also means the Knowledge Center ships with the binary - no deploy script
// change, no second artifact to forget.

// Article is one Knowledge Center page.
type Article struct {
	Slug          string    `json:"slug"`
	Title         string    `json:"title"`
	Section       string    `json:"section"`
	Order         int       `json:"order"`
	Summary       string    `json:"summary,omitempty"`
	Audience      string    `json:"audience,omitempty"`
	Public        bool      `json:"public,omitempty"`
	Screens       []string  `json:"screens,omitempty"`
	LastVerified  string    `json:"last_verified,omitempty"`
	Owner         string    `json:"owner,omitempty"`
	Status        string    `json:"status,omitempty"`
	TopicType     string    `json:"topic_type,omitempty"`
	Module        string    `json:"module,omitempty"`
	Task          string    `json:"task,omitempty"`
	Prerequisites string    `json:"prerequisites,omitempty"`
	AppliesTo     string    `json:"applies_to,omitempty"`
	SourcePath    string    `json:"source_path"`
	Headings      []Heading `json:"headings,omitempty"`
	// MenuModule is the app's main-menu module this handbook is filed under in
	// the Knowledge Center sidebar (210.2), resolved from Module.
	MenuModule string `json:"menu_module,omitempty"`
	// ScreenSections is the `screen_sections:` front matter: screen=anchor
	// pairs naming the section that documents each screen (210.1).
	ScreenSections []string `json:"-"`

	// HTML is the rendered body. It is written to its own file rather than
	// inlined in the index, so opening the Knowledge Center costs one small
	// index fetch instead of downloading every article.
	HTML string `json:"-"`
	// Text is the de-tagged body, used only to build the search index.
	Text string `json:"-"`
}

// Heading is one entry in an article's on-page table of contents.
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Slug  string `json:"slug"`
}

// Section groups articles for the sidebar.
type Section struct {
	Name     string    `json:"name"`
	Order    int       `json:"order"`
	Articles []Article `json:"articles"`
}

// Index is content/index.json - everything the shell needs to draw its nav and
// resolve a contextual-help request without fetching a single article.
type Index struct {
	GeneratedAt  string              `json:"generated_at"`
	Sections     []Section           `json:"sections"`
	ScreenMap    map[string][]string `json:"screen_map"`
	ArticleCount int                 `json:"article_count"`
	// ScreenSections maps a screen id to "<slug>#<anchor>" entries, so the help
	// dialog shows that screen's own section and its Knowledge Center button
	// lands on it (210.1). MenuModules is the sidebar order of the app's main
	// menu (210.2).
	ScreenSections map[string][]string `json:"screen_sections,omitempty"`
	MenuModules    []string            `json:"menu_modules,omitempty"`
}

// menuModules files Module Handbooks under the app's own main menu
// (public/index.html, the "Module:" groups, in that order), so help for a
// screen sits under the module the screen sits under. Keys are handbook
// `module:` values, or a handbook's own slug where its module is shared with
// handbooks filed elsewhere (Fixed Assets and Expenses are `finance` but sit
// in the HRM menu); a slug is matched before a module. A handbook matching
// neither is filed under "Other" and the build warns.
var menuModules = []struct {
	Label   string
	Modules []string
}{
	{"POS", []string{"pos-operations"}},
	{"Financial Accounting", []string{"finance", "finance-tax"}},
	{"OMS", []string{"oms-order-management", "channel-connectors", "courier-integrations", "returns"}},
	{"CRM", []string{"crm-loyalty"}},
	{"Reports", []string{"reports"}},
	{"Procurement", []string{"procurement"}},
	{"Inventory", []string{"inventory", "inventory-wms-operations", "traceability-batch-serial"}},
	{"WMS", []string{"wms", "warehouse", "place-hold"}},
	{"HRM", []string{"hr-payroll", "assets", "expenses"}},
	{"Manufacturing", []string{"manufacturing-mrp"}},
	{"PIM", []string{"pim-pxm", "pim-product-groups"}},
	{"Settings", []string{"security-approvals", "platform", "settings"}},
}

func menuModuleFor(slug, module string) string {
	for _, key := range []string{slug, module} {
		for _, group := range menuModules {
			for _, known := range group.Modules {
				if strings.EqualFold(known, strings.TrimSpace(key)) {
					return group.Label
				}
			}
		}
	}
	return ""
}

// MaxSnapshotBytes caps one 210.4 snapshot. A cropped WebP of one panel fits
// well inside it; anything bigger is a whole-screen capture or a PNG renamed.
const MaxSnapshotBytes = 40 * 1024

var snapshotRefPattern = regexp.MustCompile(`data-kb-img="([^"]+)"`)

// SearchIndex is content/search.json - a prebuilt inverted index. Prebuilt
// because the alternative is either shipping a search library or scanning every
// article in the browser on each keystroke, and neither is necessary for a
// corpus this size.
type SearchIndex struct {
	Docs  []SearchDoc      `json:"docs"`
	Terms map[string][]int `json:"terms"`
}

type SearchDoc struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Section string `json:"section"`
	Summary string `json:"summary,omitempty"`
}

// BuildResult is everything one build produced, keyed by output path relative
// to the content root.
type BuildResult struct {
	Files    map[string][]byte
	Index    Index
	Articles []Article
	Warnings []string
}

// sectionOrder fixes the sidebar order. A section absent from this list sorts
// after the known ones, alphabetically - so adding a section is possible
// without editing code, but the reading order of the core journey is not left
// to chance.
var sectionOrder = []string{
	"Getting Started",
	"Role Journeys",
	"Module Handbooks",
	"Integrations",
	"Admin & Operations",
	"Reference",
	"Troubleshooting",
}

func sectionRank(name string) int {
	for i, known := range sectionOrder {
		if strings.EqualFold(known, name) {
			return i
		}
	}
	return len(sectionOrder)
}

var frontmatterListPattern = regexp.MustCompile(`^\[(.*)\]$`)

// parseFrontmatter reads the leading --- block. A deliberately tiny subset of
// YAML: scalars, booleans, integers and one-line lists. Anything more would be
// a YAML parser, which is a dependency this repo does not take for a header
// that only ever holds eight keys.
func parseFrontmatter(source string) (map[string]string, string, error) {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	if !strings.HasPrefix(source, "---\n") {
		return nil, source, fmt.Errorf("missing frontmatter: an article must start with a --- block declaring at least title and section")
	}
	rest := source[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, source, fmt.Errorf("frontmatter block is never closed with ---")
	}
	block := rest[:end]
	body := strings.TrimPrefix(rest[end+4:], "\n")

	fields := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			return nil, body, fmt.Errorf("frontmatter line %q is not key: value", line)
		}
		key := strings.TrimSpace(line[:colon])
		value := strings.TrimSpace(line[colon+1:])
		value = strings.Trim(value, `"'`)
		fields[strings.ToLower(key)] = value
	}
	return fields, body, nil
}

func frontmatterList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if match := frontmatterListPattern.FindStringSubmatch(raw); match != nil {
		raw = match[1]
	}
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(strings.Trim(strings.TrimSpace(part), `"'`))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

var (
	headingPattern = regexp.MustCompile(`(?s)<h([23]) id="([^"]*)">(.*?)</h[23]>`)
	tagPattern     = regexp.MustCompile(`<[^>]*>`)
	anchorPattern  = regexp.MustCompile(`(?s)\s*<a class="kb-anchor".*?</a>`)
	tokenPattern   = regexp.MustCompile(`[a-z0-9][a-z0-9_-]*`)
)

// searchStopwords keeps the index from being dominated by words that match
// every article and therefore rank nothing.
var searchStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "you": true, "your": true, "with": true,
	"this": true, "that": true, "from": true, "are": true, "not": true, "but": true,
	"has": true, "have": true, "was": true, "were": true, "can": true, "will": true,
	"its": true, "into": true, "when": true, "then": true, "than": true, "each": true,
	"all": true, "any": true, "how": true, "why": true, "use": true, "used": true,
}

func extractHeadings(rendered string) []Heading {
	out := []Heading{}
	for _, match := range headingPattern.FindAllStringSubmatch(rendered, -1) {
		level, _ := strconv.Atoi(match[1])
		text := strings.TrimSpace(html.UnescapeString(tagPattern.ReplaceAllString(anchorPattern.ReplaceAllString(match[3], ""), "")))
		if text == "" {
			continue
		}
		out = append(out, Heading{Level: level, Text: text, Slug: match[2]})
	}
	return out
}

func plainText(rendered string) string {
	stripped := tagPattern.ReplaceAllString(rendered, " ")
	return strings.Join(strings.Fields(html.UnescapeString(stripped)), " ")
}

// slugFromPath derives an article's URL slug from its filename. Basenames must
// be unique across the tree: a slug is a permanent link that people bookmark
// and that other articles reference, so it must not change because someone
// moved a file between folders.
func slugFromPath(relPath string) string {
	base := filepath.Base(relPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return strings.ToLower(base)
}

// Build renders every Markdown file under root. It returns the complete output
// set in memory rather than writing it, so the drift check can compare a fresh
// build against what is committed without touching the tree.
func Build(root string) (*BuildResult, error) {
	var sources []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Underscore-prefixed directories are scratch space, never content.
			if strings.HasPrefix(info.Name(), "_") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") && !strings.HasPrefix(info.Name(), "_") {
			sources = append(sources, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	sort.Strings(sources)
	if len(sources) == 0 {
		return nil, fmt.Errorf("no Markdown articles found under %s", root)
	}

	result := &BuildResult{Files: map[string][]byte{}, Warnings: []string{}}
	bySlug := map[string]string{}
	for _, path := range sources {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		relPath, _ := filepath.Rel(root, path)
		relPath = filepath.ToSlash(relPath)

		fields, body, fmErr := parseFrontmatter(string(raw))
		if fmErr != nil {
			return nil, fmt.Errorf("%s: %w", relPath, fmErr)
		}
		article := Article{
			Slug:         slugFromPath(relPath),
			Title:        fields["title"],
			Section:      fields["section"],
			Summary:      fields["summary"],
			Audience:     fields["audience"],
			Public:       strings.EqualFold(fields["public"], "true"),
			Screens:      frontmatterList(fields["screens"]),
			LastVerified: fields["last_verified"],
			Owner:        fields["owner"], Status: fields["status"], TopicType: fields["topic_type"],
			Module: fields["module"], Task: fields["task"], Prerequisites: fields["prerequisites"], AppliesTo: fields["applies_to"],
			SourcePath: relPath,
		}
		if article.Title == "" {
			return nil, fmt.Errorf("%s: frontmatter needs a title", relPath)
		}
		if article.Section == "" {
			// An article with no section cannot appear in the sidebar, which is
			// 39.7's "unreachable article". Failing here is better than shipping
			// a page nobody can navigate to.
			return nil, fmt.Errorf("%s: frontmatter needs a section, or the article is unreachable from the sidebar", relPath)
		}
		if order := fields["order"]; order != "" {
			parsed, convErr := strconv.Atoi(order)
			if convErr != nil {
				return nil, fmt.Errorf("%s: order must be a whole number, got %q", relPath, order)
			}
			article.Order = parsed
		}
		if existing, duplicate := bySlug[article.Slug]; duplicate {
			return nil, fmt.Errorf("%s and %s both produce the slug %q; article filenames must be unique across the tree because the slug is a permanent link", existing, relPath, article.Slug)
		}
		bySlug[article.Slug] = relPath

		article.HTML = RenderMarkdown(body)
		article.Headings = extractHeadings(article.HTML)
		article.Text = plainText(article.HTML)
		if strings.EqualFold(article.Section, "Module Handbooks") {
			article.MenuModule = menuModuleFor(article.Slug, article.Module)
			if article.MenuModule == "" {
				article.MenuModule = "Other"
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: module %q is not filed under a main-menu module in internal/kb/build.go menuModules - it shows under Other", relPath, article.Module))
			}
		}
		// 210.1: every screen=anchor pair must name a screen this article
		// already lists (so the drift check validates the screen id) and a
		// heading the article really has (so the link can never land nowhere).
		anchors := map[string]bool{}
		for _, h := range article.Headings {
			anchors[h.Slug] = true
		}
		listed := map[string]bool{}
		for _, screen := range article.Screens {
			listed[strings.ToLower(strings.TrimSpace(screen))] = true
		}
		for _, pair := range frontmatterList(fields["screen_sections"]) {
			screen, anchor, found := strings.Cut(pair, "=")
			screen, anchor = strings.ToLower(strings.TrimSpace(screen)), strings.TrimSpace(anchor)
			if !found || screen == "" || anchor == "" {
				return nil, fmt.Errorf("%s: screen_sections entry %q must be screen=anchor", relPath, pair)
			}
			if !listed[screen] {
				return nil, fmt.Errorf("%s: screen_sections names %q, which is not in this article's screens:", relPath, screen)
			}
			if !anchors[anchor] {
				return nil, fmt.Errorf("%s: screen_sections points %q at #%s, which is not a heading in this article", relPath, screen, anchor)
			}
			article.ScreenSections = append(article.ScreenSections, screen+"="+anchor)
		}
		if article.Summary == "" {
			result.Warnings = append(result.Warnings, relPath+": no summary - search results and the sidebar will show the title alone")
		}
		if article.LastVerified == "" {
			result.Warnings = append(result.Warnings, relPath+": no last_verified date - 39.8's staleness check cannot judge this article")
		}
		result.Files["articles/"+article.Slug+".html"] = []byte(article.HTML)
		result.Articles = append(result.Articles, article)
	}

	// Group into sections, ordered by the fixed reading order above and then by
	// each article's own order/title.
	grouped := map[string][]Article{}
	for _, article := range result.Articles {
		grouped[article.Section] = append(grouped[article.Section], article)
	}
	sections := make([]Section, 0, len(grouped))
	for name, articles := range grouped {
		sort.Slice(articles, func(i, j int) bool {
			if articles[i].Order != articles[j].Order {
				return articles[i].Order < articles[j].Order
			}
			return articles[i].Title < articles[j].Title
		})
		sections = append(sections, Section{Name: name, Order: sectionRank(name), Articles: articles})
	}
	sort.Slice(sections, func(i, j int) bool {
		if sections[i].Order != sections[j].Order {
			return sections[i].Order < sections[j].Order
		}
		return sections[i].Name < sections[j].Name
	})

	// screen_map powers 39.5: a screen id from the frontend resolves to the
	// articles that document it, with no second mapping file to keep in sync.
	screenMap := map[string][]string{}
	for _, article := range result.Articles {
		for _, screen := range article.Screens {
			screen = strings.ToLower(strings.TrimSpace(screen))
			screenMap[screen] = append(screenMap[screen], article.Slug)
		}
	}
	// The help drawer opens the first slug, so order by how directly an
	// article documents the screen: a module handbook before a journey or an
	// overview, then the article that lists this screen earlier in its own
	// screens: (authors list what an article is about first), then slug.
	// Alphabetical alone opened "A day as a warehouse operator" on Bin
	// Conditions and Channel Connectors on every master list.
	handbook := map[string]bool{}
	position := map[string]int{}
	for _, article := range result.Articles {
		handbook[article.Slug] = strings.EqualFold(article.Section, "Module Handbooks")
		for i, screen := range article.Screens {
			position[article.Slug+"\x00"+strings.ToLower(strings.TrimSpace(screen))] = i
		}
	}
	for screen, slugs := range screenMap {
		sort.SliceStable(slugs, func(i, j int) bool {
			a, b := slugs[i], slugs[j]
			if handbook[a] != handbook[b] {
				return handbook[a]
			}
			if pa, pb := position[a+"\x00"+screen], position[b+"\x00"+screen]; pa != pb {
				return pa < pb
			}
			return a < b
		})
	}

	// 210.1: screen -> the sections that document it, in the order the drawer
	// shows them: the screen_map's article order, then each article's own.
	screenSections := map[string][]string{}
	sectionsOf := map[string][]string{}
	for _, article := range result.Articles {
		for _, pair := range article.ScreenSections {
			screen, anchor, _ := strings.Cut(pair, "=")
			sectionsOf[article.Slug+"\x00"+screen] = append(sectionsOf[article.Slug+"\x00"+screen], anchor)
		}
	}
	for screen, slugs := range screenMap {
		for _, slug := range slugs {
			for _, anchor := range sectionsOf[slug+"\x00"+screen] {
				screenSections[screen] = append(screenSections[screen], slug+"#"+anchor)
			}
		}
	}
	menuOrder := make([]string, 0, len(menuModules)+1)
	for _, group := range menuModules {
		menuOrder = append(menuOrder, group.Label)
	}
	menuOrder = append(menuOrder, "Other")

	// 210.4: snapshots. Every file under img/ is a small WebP that some article
	// shows, and every snapshot an article shows exists - a missing or stray
	// image fails the build rather than shipping a broken frame or dead weight.
	shown := map[string]string{}
	for _, article := range result.Articles {
		for _, match := range snapshotRefPattern.FindAllStringSubmatch(article.HTML, -1) {
			shown[match[1]] = article.SourcePath
		}
	}
	images, _ := os.ReadDir(filepath.Join(root, "img"))
	present := map[string]bool{}
	for _, entry := range images {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if SnapshotPattern.FindStringSubmatch("img/"+name) == nil {
			return nil, fmt.Errorf("img/%s: a snapshot is a lower-case kebab-case .webp file", name)
		}
		data, readErr := os.ReadFile(filepath.Join(root, "img", name))
		if readErr != nil {
			return nil, readErr
		}
		if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
			return nil, fmt.Errorf("img/%s: not a WebP file", name)
		}
		if len(data) > MaxSnapshotBytes {
			return nil, fmt.Errorf("img/%s: %d bytes is over the %d-byte snapshot cap; crop it to the working panel or lower the quality", name, len(data), MaxSnapshotBytes)
		}
		if shown[name] == "" {
			return nil, fmt.Errorf("img/%s: no article shows this snapshot; delete it or reference it", name)
		}
		present[name] = true
		result.Files["img/"+name] = data
	}
	for name, source := range shown {
		if !present[name] {
			return nil, fmt.Errorf("%s: shows snapshot %s, which is not in docs/kb/img/", source, name)
		}
	}

	result.Index = Index{
		// Deliberately NOT a timestamp: the output is committed, and a build
		// stamp would make every regeneration a diff even when no content
		// changed, which would make -Check useless as a drift signal.
		GeneratedAt:    "content-hash build; see docs/kb/update-kb.ps1",
		Sections:       sections,
		ScreenMap:      screenMap,
		ArticleCount:   len(result.Articles),
		ScreenSections: screenSections,
		MenuModules:    menuOrder,
	}
	// 217 (210.10): compact like search.json below - the indentation was a
	// third of the file, which Help downloads on every open, and it pushed the
	// embedded Knowledge Center over its 2 MiB budget.
	indexJSON, err := json.Marshal(result.Index)
	if err != nil {
		return nil, err
	}
	result.Files["index.json"] = append(indexJSON, '\n')

	// The search index is a machine artifact. Compact encoding preserves every
	// token/posting while avoiding indentation that exceeded the 250 KiB budget.
	searchJSON, err := json.Marshal(buildSearchIndex(result.Articles))
	if err != nil {
		return nil, err
	}
	result.Files["search.json"] = append(searchJSON, '\n')
	return result, nil
}

// isReleaseNotes reports the pages cmd/gendocs makes from the ledger:
// release-notes.md, release-notes-2.md and so on.
func isReleaseNotes(slug string) bool {
	return slug == "release-notes" || strings.HasPrefix(slug, "release-notes-")
}

// buildSearchIndex tokenizes title, summary and body. The title's terms are
// added twice so a query that matches a title outranks one that only appears in
// a paragraph - which is the whole of the ranking model, and enough for a
// corpus measured in dozens of articles.
func buildSearchIndex(articles []Article) SearchIndex {
	index := SearchIndex{Docs: make([]SearchDoc, 0, len(articles)), Terms: map[string][]int{}}
	seenPerDoc := map[string]bool{}
	add := func(term string, docIndex int) {
		if len(term) < 3 || searchStopwords[term] {
			return
		}
		key := term + "\x00" + strconv.Itoa(docIndex)
		if seenPerDoc[key] {
			return
		}
		seenPerDoc[key] = true
		index.Terms[term] = append(index.Terms[term], docIndex)
	}
	for i, article := range articles {
		index.Docs = append(index.Docs, SearchDoc{
			Slug: article.Slug, Title: article.Title, Section: article.Section, Summary: article.Summary,
		})
		body := article.Text
		if isReleaseNotes(article.Slug) {
			// 265.9: the release notes are the build ledger, which grows with every
			// ask; their body words were a sixth of this index and kept pushing it
			// over its 250 KiB budget. Found by title and summary; still readable.
			body = ""
		}
		for _, source := range []string{article.Title, article.Title, article.Summary, article.Section, body} {
			for _, token := range tokenPattern.FindAllString(strings.ToLower(source), -1) {
				add(token, i)
			}
		}
	}
	for term := range index.Terms {
		sort.Ints(index.Terms[term])
	}
	return index
}

// WriteTo materialises a build under dir, replacing whatever was there. It
// removes stale article files: an article deleted from docs/kb/ must stop being
// served, and leaving its old HTML behind is exactly the kind of drift 39.7
// exists to catch.
func WriteTo(dir string, result *BuildResult) error {
	for _, sub := range []string{"articles", "img"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return err
		}
	}
	existing, err := filepath.Glob(filepath.Join(dir, "articles", "*.html"))
	if err != nil {
		return err
	}
	snapshots, _ := filepath.Glob(filepath.Join(dir, "img", "*"))
	existing = append(existing, snapshots...)
	wanted := map[string]bool{}
	for name := range result.Files {
		wanted[filepath.Join(dir, filepath.FromSlash(name))] = true
	}
	for _, path := range existing {
		if !wanted[path] {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	for name, body := range result.Files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Diff compares a fresh build against what is on disk, and names every
// difference. This is what `update-kb.ps1 -Check` reports and exits non-zero
// on: committed output that no longer matches its source is output nobody can
// trust.
func Diff(dir string, result *BuildResult) []string {
	differences := []string{}
	for name, body := range result.Files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		onDisk, err := os.ReadFile(path)
		if err != nil {
			differences = append(differences, "missing generated file: "+name)
			continue
		}
		if string(onDisk) != string(body) {
			differences = append(differences, "stale generated file: "+name)
		}
	}
	existing, _ := filepath.Glob(filepath.Join(dir, "articles", "*.html"))
	for _, path := range existing {
		name := "articles/" + filepath.Base(path)
		if _, wanted := result.Files[name]; !wanted {
			differences = append(differences, "orphaned generated file (its source article is gone): "+name)
		}
	}
	snapshots, _ := filepath.Glob(filepath.Join(dir, "img", "*"))
	for _, path := range snapshots {
		name := "img/" + filepath.Base(path)
		if _, wanted := result.Files[name]; !wanted {
			differences = append(differences, "orphaned generated file (its source snapshot is gone): "+name)
		}
	}
	sort.Strings(differences)
	return differences
}
