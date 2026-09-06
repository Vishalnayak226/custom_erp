package securityscan

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Stage 49.1.4 - the no-bypass inventory.
//
// "Search code, migrations, scripts, fixtures and docs for magic user/role/
// tenant, universal password/token, support impersonation, skipped MFA,
// permissive fallback, hidden route, direct mutation, emergency feature flag
// or 'temporary' exception. Remove it or convert it to governed break-glass."
//
// A one-off grep answers that for the day it is run. What 49.1 actually needs
// is the property to hold at every release, so this is a scanner plus a
// reviewed allowlist (bypass_test.go): every hit is either removed from the
// code, or written down with the reason it is safe and the control that keeps
// it safe. A new hit in a category, or an extra hit in a file that already has
// reviewed ones, fails the build.
//
// The patterns are deliberately narrow. A scanner that reports a hundred
// maybes gets suppressed wholesale within a week; one that reports eight real
// things, each of which someone has written a sentence about, stays switched
// on. Where a pattern cannot be made precise (a "permissive fallback" is a
// shape, not a string) the honest answer is that it belongs to code review and
// the 49.10.6 insider red-team scenarios, not to a regex - and this file says
// so rather than pretending otherwise.

// Bypass severities.
const (
	BypassBlock  = "block"  // must be removed or explicitly reviewed
	BypassReview = "review" // worth a human look at each release
)

// BypassFinding is one scanner hit. Excerpt is redacted: a finding is printed
// in test output and CI logs, so it may never carry the credential it found.
type BypassFinding struct {
	Category string
	Severity string
	File     string
	Line     int
	Excerpt  string
}

type bypassPattern struct {
	category string
	severity string
	pattern  *regexp.Regexp
	// exts limits the pattern to file types where it is meaningful; empty
	// means every scanned type.
	exts []string
}

var bypassPatterns = []bypassPattern{
	{
		// A password hash or private key committed to the repository. Every one
		// is a credential that ships to whoever can read the source.
		category: "seeded-credential", severity: BypassBlock,
		pattern: regexp.MustCompile(`\$2[aby]\$[0-9]{2}\$[./A-Za-z0-9]{53}|-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	},
	{
		// A credential-shaped literal assigned to a credential-named symbol.
		// Requires a digit in the value, which is what separates a real secret
		// from a constant like "integration.secret".
		category: "credential-assignment", severity: BypassBlock,
		pattern: regexp.MustCompile(`(?i)(password|passwd|secret|api_?key|private_?key|access_?token|bearer)\s*(:|:=|=)\s*"[^"]*[0-9][^"]*"`),
		exts:    []string{".go", ".js", ".ps1", ".sh", ".yml", ".yaml"},
	},
	{
		// An environment flag whose name says it turns a control off. Each one
		// is an emergency feature flag until it is shown to be governed.
		category: "security-disabling-flag", severity: BypassBlock,
		pattern: regexp.MustCompile(`os\.Getenv\("[A-Z_]*(SKIP|DISABLE|NO_AUTH|INSECURE|ALLOW_ALL|UNSAFE|BYPASS)[A-Z_]*"\)`),
		exts:    []string{".go"},
	},
	{
		// A privileged decision made by comparing against a magic name instead
		// of going through the role/capability engine.
		category: "privileged-shortcut", severity: BypassBlock,
		pattern: regexp.MustCompile(`(==|!=)\s*"(admin|Admin|ADMIN|root|system|System|superadmin|SuperAdmin|Administrator|HR/Admin)"`),
		exts:    []string{".go"},
	},
	{
		// The vocabulary of a deliberate back door. Present in a comment is
		// enough to want a human to read the surrounding code.
		category: "bypass-vocabulary", severity: BypassBlock,
		pattern: regexp.MustCompile(`(?i)backdoor|back door|bypass auth|skip (mfa|auth|authentication|permission)|god ?mode|master password|universal token|impersonat`),
	},
	{
		// An unfinished or explicitly temporary decision inside a file that
		// makes security decisions. Reported, not blocked: the point is that
		// someone reads it each release, not that it can never exist.
		// Anchored to a comment so a word list that happens to contain "todo"
		// (engines/security_baseline.go's placeholder detector does) is not a
		// finding - only a human note is.
		category: "unreviewed-marker", severity: BypassReview,
		pattern: regexp.MustCompile(`(?i)//.*\b(TODO|FIXME|HACK|XXX|for now|temporar\w+)\b`),
		exts:    []string{".go"},
	},
}

// securityDecisionFiles are the files the unreviewed-marker pattern applies
// to - the ones where a "for now" is a security decision rather than a note
// about a report layout. Matched as path suffixes.
var securityDecisionFiles = []string{
	"engines/auth.go",
	"engines/roles.go",
	"engines/mfa.go",
	"engines/security_baseline.go",
	"internal/server/middleware.go",
	"internal/server/middleware_public_api.go",
	"internal/server/route_capabilities.go",
	"internal/server/static_fileserver.go",
}

// redactor blanks anything that looks like the secret itself before a finding
// is printed. The scanner's own output is a place a credential must not leak.
var (
	redactHash   = regexp.MustCompile(`\$2[aby]\$[0-9]{2}\$[./A-Za-z0-9]{53}`)
	redactQuoted = regexp.MustCompile(`"[^"]{8,}"`)
	redactPEM    = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
)

func redact(category, line string) string {
	line = strings.TrimSpace(line)
	if len(line) > 160 {
		line = line[:160] + "..."
	}
	switch category {
	case "seeded-credential":
		line = redactHash.ReplaceAllLiteralString(line, "<redacted bcrypt hash>")
		line = redactPEM.ReplaceAllLiteralString(line, "-----BEGIN <redacted> PRIVATE KEY-----")
	case "credential-assignment":
		line = redactQuoted.ReplaceAllLiteralString(line, `"<redacted>"`)
	}
	return line
}

// scannedExtensions is what ScanBypass reads. Deliberately includes .sql and
// .ps1 (49.1.4 says "migrations, scripts") and deliberately excludes
// generated output, dependencies and the knowledge graph.
var scannedExtensions = map[string]bool{
	".go": true, ".sql": true, ".js": true, ".ps1": true, ".sh": true,
	".yml": true, ".yaml": true, ".json": false, // .json is data, not logic
}

var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, "graphify-out": true,
	"docs":               true, // documentation is reviewed as prose, not scanned as code
	"skill-observations": true,
}

// skippedFiles is this file itself. Every pattern above is written out in
// full here, so the scanner matches its own source in three categories; the
// reviewed allowlist would then be mostly entries about the scanner rather
// than about the product. Nothing else is exempt.
var skippedFiles = map[string]bool{
	"internal/securityscan/bypass.go": true,
}

// ScanBypass walks the repository rooted at root and returns every hit, sorted
// by category then file then line. Test files are skipped: a synthetic
// credential in a test is expected (49.9.3 requires exactly that), and
// including them would bury the findings that matter in fixtures.
func ScanBypass(root string) ([]BypassFinding, error) {
	var findings []BypassFinding

	err := filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			if skippedDirs[fi.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !scannedExtensions[ext] || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if skippedFiles[rel] {
			return nil
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		for _, p := range bypassPatterns {
			if len(p.exts) > 0 && !contains(p.exts, ext) {
				continue
			}
			if p.category == "unreviewed-marker" && !isSecurityDecisionFile(rel) {
				continue
			}
			for i, line := range lines {
				if !p.pattern.MatchString(line) {
					continue
				}
				findings = append(findings, BypassFinding{
					Category: p.category, Severity: p.severity,
					File: rel, Line: i + 1, Excerpt: redact(p.category, line),
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Category != findings[j].Category {
			return findings[i].Category < findings[j].Category
		}
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, nil
}

func isSecurityDecisionFile(rel string) bool {
	for _, f := range securityDecisionFiles {
		if rel == f || strings.HasSuffix(rel, "/"+f) {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// CountByCategoryAndFile collapses findings to the shape the reviewed
// allowlist is written in: how many hits of each category each file has.
// Counts rather than line numbers, because a line number changes every time
// anything above it is edited and would make the allowlist unmaintainable in
// a tree with concurrent work in it.
func CountByCategoryAndFile(findings []BypassFinding) map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, f := range findings {
		if out[f.Category] == nil {
			out[f.Category] = map[string]int{}
		}
		out[f.Category][f.File]++
	}
	return out
}
