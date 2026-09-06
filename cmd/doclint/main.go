// Command doclint inventories and checks documentation offline. Default mode is read-only and advisory.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"custom_erp/internal/kb"
)

type Rule struct {
	Prefix      string `json:"prefix"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Owner       string `json:"owner"`
	Authority   string `json:"authority"`
	Disposition string `json:"disposition"`
}
type Document struct {
	Path            string            `json:"path"`
	ID              string            `json:"doc_id,omitempty"`
	Title           string            `json:"title"`
	Type            string            `json:"type"`
	Status          string            `json:"status"`
	Owner           string            `json:"owner"`
	Ownership       string            `json:"ownership"`
	Audience        string            `json:"audience"`
	Authority       string            `json:"authority"`
	Confidentiality string            `json:"confidentiality"`
	Disposition     string            `json:"disposition"`
	ReviewBy        string            `json:"review_by,omitempty"`
	Replacement     string            `json:"superseded_by,omitempty"`
	LastVerified    string            `json:"last_verified,omitempty"`
	LastCommit      string            `json:"last_commit"`
	Bytes           int               `json:"bytes"`
	SHA256          string            `json:"sha256,omitempty"`
	Inbound         []string          `json:"inbound_links"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}
type Finding struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Register struct {
	SchemaVersion int        `json:"schema_version"`
	SourceCommit  string     `json:"source_commit"`
	CapturedOn    string     `json:"captured_on"`
	Scope         string     `json:"scope"`
	Documents     []Document `json:"documents"`
}
type Report struct {
	Documents int            `json:"documents"`
	Bytes     int            `json:"bytes"`
	Counts    map[string]int `json:"counts"`
	Findings  []Finding      `json:"findings"`
}

var inlineLink = regexp.MustCompile(`!?\[[^\]\n]*\]\((<[^>]+>|[^\s)]+)(?:\s+"[^"]*")?\)`)
var referenceLink = regexp.MustCompile(`(?m)^\s*\[[^\]\n]+\]:\s*(<[^>]+>|\S+)`)
var nonportable = regexp.MustCompile(`(?i)file:///|[a-z]:[\\/]Users[\\/]|/Users/[^/\s]+/|/home/[^/\s]+/`)
var secret = regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----|\b(?:ghp_|github_pat_|sk_live_)[a-z0-9_]{20,}|(?:postgres(?:ql)?://)[^\s:/]+:[^\s@<>]+@`)
var kebab = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

func main() {
	root := flag.String("root", ".", "repository root")
	strict := flag.Bool("strict", false, "exit nonzero on any finding (default: warnings)")
	jsonOutput := flag.Bool("json", false, "print a machine-readable health report")
	registerOut := flag.String("write-register", "", "explicit output path for an inventory snapshot; default never writes")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		die(err)
	}
	rulesBody, err := os.ReadFile(filepath.Join(abs, "docs/governance/register-policy.json"))
	if err != nil {
		die(err)
	}
	var policy struct {
		Rules []Rule `json:"rules"`
	}
	if err := json.Unmarshal(rulesBody, &policy); err != nil {
		die(err)
	}
	reg, report, err := inspect(abs, policy.Rules, time.Now().UTC())
	if err != nil {
		die(err)
	}
	if *registerOut != "" {
		body, err := json.MarshalIndent(reg, "", "  ")
		if err != nil {
			die(err)
		}
		if err := os.WriteFile(*registerOut, append(body, '\n'), 0o644); err != nil {
			die(err)
		}
	}
	if *jsonOutput {
		body, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(body))
	} else {
		for _, finding := range report.Findings {
			fmt.Printf("[warn] %s: %s: %s\n", finding.Path, finding.Code, finding.Message)
		}
		fmt.Printf("doclint: %d files, %d bytes, %d findings (warning mode=%t)\n", report.Documents, report.Bytes, len(report.Findings), !*strict)
	}
	if *strict && len(report.Findings) > 0 {
		os.Exit(1)
	}
}
func die(err error) { fmt.Fprintln(os.Stderr, "doclint:", err); os.Exit(1) }

func inspect(root string, rules []Rule, now time.Time) (Register, Report, error) {
	reg := Register{SchemaVersion: 1, SourceCommit: git(root, "rev-parse", "HEAD"), CapturedOn: now.Format("2006-01-02"), Scope: "working tree; provisional owners; not a release approval or pre-migration baseline"}
	report := Report{Counts: map[string]int{}, Findings: []Finding{}}
	add := func(path, code, message string) {
		report.Findings = append(report.Findings, Finding{path, code, message})
		report.Counts[code]++
	}
	paths := map[string]bool{}
	for _, dir := range []string{"docs", "internal/kb/content", "cmd/gendocs", "cmd/genkb", "cmd/brainmap", "cmd/doclint", "internal/docgen"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("linked inventory input: %s", path)
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			paths[filepath.ToSlash(rel)] = true
			return nil
		})
		if err != nil {
			return reg, report, err
		}
	}
	for _, path := range []string{"README.md", ".github/workflows/ci.yml", ".github/pull_request_template.md"} {
		if _, err := os.Stat(filepath.Join(root, path)); err == nil {
			paths[path] = true
		}
	}
	names := make([]string, 0, len(paths))
	for p := range paths {
		names = append(names, p)
	}
	sort.Strings(names)
	registered := map[string]bool{}
	var old Register
	if body, err := os.ReadFile(filepath.Join(root, "docs/governance/document-register.json")); err == nil {
		if err := json.Unmarshal(body, &old); err != nil {
			return reg, report, fmt.Errorf("invalid document register: %w", err)
		}
		for _, d := range old.Documents {
			registered[d.Path] = true
		}
	}
	commits := lastCommits(root)
	texts := map[string]string{}
	anchors := map[string]map[string]bool{}
	slugs := map[string]string{}
	ids := map[string]string{}
	for _, path := range names {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return reg, report, err
		}
		text := strings.ReplaceAll(string(body), "\r\n", "\n")
		meta, content := frontmatter(text)
		rule := classify(path, rules)
		d := Document{Path: path, Title: filepath.Base(path), Type: rule.Type, Status: rule.Status, Owner: rule.Owner, Ownership: "provisional-role", Audience: "maintainers", Authority: rule.Authority, Confidentiality: "internal", Disposition: rule.Disposition, Bytes: len(body), LastCommit: commits[path], Inbound: []string{}, Metadata: meta}
		if d.LastCommit == "" {
			d.LastCommit = "uncommitted"
		}
		for key, dest := range map[string]*string{"doc_id": &d.ID, "title": &d.Title, "type": &d.Type, "status": &d.Status, "owner": &d.Owner, "audience": &d.Audience, "authority": &d.Authority, "confidentiality": &d.Confidentiality, "review_by": &d.ReviewBy, "last_verified": &d.LastVerified, "superseded_by": &d.Replacement} {
			if meta[key] != "" {
				*dest = meta[key]
			}
		}
		if meta["owner"] != "" {
			d.Ownership = "authored-role; acceptance not implied"
		}
		hash := sha256.Sum256(body)
		d.SHA256 = hex.EncodeToString(hash[:])
		if path == "docs/governance/document-register.json" {
			d.Bytes = 0
			d.SHA256 = ""
			d.LastCommit = "self-inventory"
		}
		if !registered[path] {
			add(path, "unregistered", "refresh the reviewed inventory snapshot")
		}
		if d.Owner == "" {
			add(path, "owner", "owner is required")
		}
		if strings.HasSuffix(path, ".md") {
			texts[path] = stripCode(content)
			anchors[path] = headingIDs(texts[path], strings.HasPrefix(path, "docs/kb/"))
			h1 := 0
			for _, line := range strings.Split(texts[path], "\n") {
				if strings.HasPrefix(line, "# ") {
					h1++
					if meta["title"] == "" && h1 == 1 {
						d.Title = strings.TrimSpace(line[2:])
					}
				}
			}
			if h1 != 1 {
				add(path, "heading", "expected one H1")
			}
			if nonportable.MatchString(content) {
				add(path, "nonportable-link", "contains a workstation-specific path or file URI")
			}
			if secret.MatchString(text) {
				add(path, "secret-pattern", "possible credential/private key; value withheld")
			}
			if d.Type != "record" && d.Type != "generated" {
				missing := []string{}
				for _, key := range []string{"doc_id", "title", "type", "status", "owner", "approvers", "audience", "applies_to", "authority", "confidentiality", "last_verified", "review_by", "supersedes", "superseded_by"} {
					if meta[key] == "" {
						missing = append(missing, key)
					}
				}
				if len(missing) > 0 {
					add(path, "metadata", "missing "+strings.Join(missing, ", ")+"; disposition="+d.Disposition)
				}
				if len(body) > 120*1024 {
					add(path, "article-budget", "exceeds 120 KiB raw")
				}
			}
			if strings.HasPrefix(path, "docs/kb/") {
				slug := strings.TrimSuffix(filepath.Base(path), ".md")
				if previous := slugs[slug]; previous != "" {
					add(path, "kb-slug", "duplicate article slug")
				}
				slugs[slug] = path
			}
		}
		if d.ID != "" {
			if previous := ids[d.ID]; previous != "" {
				add(path, "duplicate-id", "also used by "+previous)
			}
			ids[d.ID] = path
		}
		if !contains([]string{"draft", "in-review", "approved", "active", "deprecated", "superseded", "archived"}, d.Status) {
			add(path, "lifecycle", "unknown lifecycle status")
		}
		if !contains([]string{"normative", "procedure", "reference", "record", "generated", "template"}, d.Type) {
			add(path, "type", "unknown document type")
		}
		if !contains([]string{"canonical", "projection", "historical", "proposed-policy", "proposed-design", "navigation", "source", "transition-copy"}, d.Authority) {
			add(path, "authority", "unknown authority category")
		}
		if !contains([]string{"public", "customer", "internal", "restricted"}, d.Confidentiality) {
			add(path, "confidentiality", "invalid or prohibited documentation class")
		}
		if d.Status == "superseded" && (d.Replacement == "" || d.Replacement == "none") {
			add(path, "replacement", "superseded document needs a replacement")
		}
		if d.ReviewBy != "" && d.Type != "record" {
			date, err := time.Parse("2006-01-02", d.ReviewBy)
			if err != nil {
				add(path, "review", "invalid review date")
			} else if date.Format("2006-01-02") < now.Format("2006-01-02") {
				add(path, "review", "review expired")
			}
		}
		if !registered[path] || strings.HasPrefix(path, "docs/governance/") {
			for _, part := range strings.Split(path, "/") {
				if !kebab.MatchString(part) {
					add(path, "naming", "new paths must use lowercase kebab-case")
					break
				}
			}
		}
		if path == "docs/ai_handover.md" && strings.Count(text, "\n") > 150 {
			add(path, "handover-budget", "exceeds 150 lines; split pending Stage 48.8")
		}
		reg.Documents = append(reg.Documents, d)
		report.Bytes += d.Bytes
	}
	inbound := map[string]map[string]bool{}
	for source, text := range texts {
		links := append(inlineLink.FindAllStringSubmatch(text, -1), referenceLink.FindAllStringSubmatch(text, -1)...)
		for _, link := range links {
			target, fragment, external := resolveLink(source, strings.Trim(link[1], "<>"), slugs)
			if external {
				continue
			}
			full := filepath.Join(root, filepath.FromSlash(target))
			rel, err := filepath.Rel(root, full)
			if err != nil || !filepath.IsLocal(rel) {
				add(source, "broken-link", "link escapes repository")
				continue
			}
			if _, err := os.Stat(full); err != nil {
				add(source, "broken-link", "missing "+target)
				continue
			}
			if inbound[target] == nil {
				inbound[target] = map[string]bool{}
			}
			inbound[target][source] = true
			if fragment != "" && anchors[target] != nil && !anchors[target][fragment] {
				add(source, "broken-anchor", "missing heading in "+target+" (#"+fragment+")")
			}
		}
	}
	for i := range reg.Documents {
		for path := range inbound[reg.Documents[i].Path] {
			reg.Documents[i].Inbound = append(reg.Documents[i].Inbound, path)
		}
		sort.Strings(reg.Documents[i].Inbound)
	}
	for path := range registered {
		if !paths[path] {
			add(path, "missing-registered-file", "registered file is absent; review its replacement/retention")
		}
	}
	checkManifests(root, add)
	kbBytes := 0
	for _, d := range reg.Documents {
		if strings.HasPrefix(d.Path, "internal/kb/content/") {
			kbBytes += d.Bytes
		}
		if d.Path == "internal/kb/content/search.json" && d.Bytes > 250*1024 {
			add(d.Path, "kb-index-budget", "search index exceeds 250 KiB")
		}
	}
	if kbBytes > 2*1024*1024 {
		add("internal/kb/content", "kb-budget", "embedded KB exceeds 2 MiB")
	}
	if _, err := os.Stat(filepath.Join(root, "docs/kb")); err == nil {
		result, err := kb.Build(filepath.Join(root, "docs/kb"))
		if err != nil {
			add("docs/kb", "kb-build", err.Error())
		} else {
			for _, warning := range kb.DriftGuards(result.Articles, kb.DriftSources{AppJSPath: filepath.Join(root, "public/app.js"), ErrorCatalogPath: filepath.Join(root, "internal/server/error_catalog_generated.go"), RouteFiles: []string{filepath.Join(root, "internal/server/routes.go"), filepath.Join(root, "internal/server/routes_public_api_v1.go")}}, now) {
				add("docs/kb", "help-coverage", warning)
			}
		}
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		a, b := report.Findings[i], report.Findings[j]
		return a.Path+"\000"+a.Code+"\000"+a.Message < b.Path+"\000"+b.Code+"\000"+b.Message
	})
	report.Documents = len(reg.Documents)
	return reg, report, nil
}

func classify(path string, rules []Rule) Rule {
	for _, rule := range rules {
		if strings.HasPrefix(path, rule.Prefix) {
			return rule
		}
	}
	return Rule{}
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func frontmatter(s string) (map[string]string, string) {
	meta := map[string]string{}
	if !strings.HasPrefix(s, "---\n") {
		return meta, s
	}
	end := strings.Index(s[4:], "\n---\n")
	if end < 0 {
		return meta, s
	}
	for _, line := range strings.Split(s[4:4+end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok {
			meta[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), "\"'")
		}
	}
	return meta, s[4+end+5:]
}
func stripCode(s string) string {
	var out strings.Builder
	fence := ""
	for _, line := range strings.Split(s, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			if fence == "" {
				fence = trim[:3]
			} else if strings.HasPrefix(trim, fence) {
				fence = ""
			}
			continue
		}
		if fence == "" {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}
func headingIDs(s string, isKB bool) map[string]bool {
	out := map[string]bool{}
	seen := map[string]int{}
	for _, line := range strings.Split(s, "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(line, "#"))
		var slug string
		if isKB {
			slug = kb.HeadingSlug(title)
		} else {
			var b strings.Builder
			for _, r := range strings.ToLower(title) {
				if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' || r == '_' {
					b.WriteRune(r)
				} else if r == ' ' {
					b.WriteByte('-')
				}
			}
			slug = b.String()
		}
		n := seen[slug]
		seen[slug]++
		if n > 0 {
			slug = fmt.Sprintf("%s-%d", slug, n)
		}
		out[slug] = true
	}
	return out
}
func resolveLink(source, raw string, slugs map[string]string) (string, string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return raw, "", false
	}
	if u.Scheme != "" || u.Host != "" {
		return "", "", true
	}
	target := u.Path
	if target == "" {
		return source, u.Fragment, false
	}
	if strings.HasPrefix(source, "docs/kb/") && !strings.Contains(target, "/") {
		if p := slugs[strings.TrimSuffix(target, ".md")]; p != "" {
			return p, u.Fragment, false
		}
	}
	return filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(source), filepath.FromSlash(target)))), u.Fragment, false
}
func git(root string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	body, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(body))
}
func lastCommits(root string) map[string]string {
	out := map[string]string{}
	current := ""
	for _, line := range strings.Split(git(root, "log", "--format=COMMIT:%h %cs", "--name-only", "--", "docs", "README.md", "cmd", "internal/kb/content", "internal/docgen", ".github"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "COMMIT:") {
			current = strings.TrimPrefix(line, "COMMIT:")
		} else if line != "" && out[line] == "" {
			out[line] = current
		}
	}
	return out
}
func checkManifests(root string, add func(string, string, string)) {
	manifests, _ := filepath.Glob(filepath.Join(root, "docs/generated/*-manifest.json"))
	if len(manifests) == 0 {
		add("docs/generated", "generated-manifest", "no generated manifests; run the documentation wrapper")
	}
	for _, path := range manifests {
		body, err := os.ReadFile(path)
		if err != nil {
			add("docs/generated", "generated-manifest", "unreadable manifest")
			continue
		}
		var m struct {
			Outputs []struct {
				Path   string `json:"path"`
				SHA256 string `json:"sha256"`
			} `json:"outputs"`
		}
		if json.Unmarshal(body, &m) != nil {
			add("docs/generated", "generated-manifest", "invalid manifest")
			continue
		}
		for _, o := range m.Outputs {
			if !filepath.IsLocal(o.Path) || strings.ContainsAny(o.Path, "\\:") {
				add("docs/generated", "generated-manifest", "unsafe output path")
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(o.Path)))
			if err != nil {
				add(o.Path, "generated-drift", "missing output")
				continue
			}
			sum := sha256.Sum256([]byte(strings.ReplaceAll(string(data), "\r\n", "\n")))
			if hex.EncodeToString(sum[:]) != o.SHA256 {
				add(o.Path, "generated-drift", "checksum differs; regenerate from source")
			}
		}
	}
}
