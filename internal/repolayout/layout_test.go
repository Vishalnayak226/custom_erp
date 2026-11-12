package repolayout

import (
	"bufio"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// rootEntries is the root allowlist from CONTRIBUTING.md Article 3. Anything
// else belongs in a folder. Local-only entries (bin/, .local/, logs/,
// backups/, media_store/, graphify-out/, CLAUDE.md, ...) are git-ignored, so
// the file listing below never contains them.
var rootEntries = map[string]bool{
	".dockerignore": true, ".gitattributes": true, ".github": true, ".gitignore": true,
	"CONTRIBUTING.md": true, "Dockerfile": true, "README.md": true, "SECURITY.md": true,
	"cmd": true, "db": true, "deploy": true, "docker-compose.yml": true, "docs": true,
	"engines": true, "environments.json": true, "extension-sdk": true, "go.mod": true,
	"go.sum": true, "internal": true, "manage.ps1": true, "promote.ps1": true,
	"public": true, "railway.json": true, "release": true, "scripts": true,
}

// neverCommitted are file types that are always a build output or a secret,
// wherever they sit (Article 3). These cannot be grandfathered.
var neverCommitted = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".stackdump": true,
	".pem": true, ".key": true, ".pfx": true, ".p12": true,
}

var (
	// stageName matches a build-stage, phase or batch number used as a name:
	// "stage47", "_stage140_14", "phase3", "batch-2". Words that merely
	// contain the letters, such as "multistage", do not match.
	stageName = regexp.MustCompile(`(?i)(^|[^a-z])(stage|phase|batch)[_-]?[0-9]`)
	goName    = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*\.go$`)
	// docName allows dots for version-named notes ("0.1.0-development.md").
	docName = regexp.MustCompile(`^[a-z0-9]+([.-][a-z0-9]+)*\.md$`)
	webName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*(\.[a-z0-9]+)+$`)
	psName  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*\.ps1$`)
	// releasePS is PowerShell's own Verb-Noun form, kept by the release clicks.
	releasePS  = regexp.MustCompile(`^([A-Z][a-z]+)+(-([A-Z][a-z]+)+)?\.ps1$`)
	snakeShell = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*\.(sh|py)$`)
)

// namingProblem returns why p breaks the naming rules of Article 4, or "".
func namingProblem(p string) string {
	base := path.Base(p)
	switch {
	case strings.HasPrefix(p, "docs/archive/"), strings.HasPrefix(p, "docs/sop-video/"):
		return "" // dated records and the video SOPs keep their names
	case strings.HasPrefix(p, "db/migrations/") && strings.HasPrefix(base, "migration") && strings.HasSuffix(base, ".sql"):
		return "" // a migration's number is its version (Article 5)
	case strings.HasPrefix(p, "db/") && strings.HasSuffix(base, ".sql"):
		return "a migration goes in db/migrations/ - the runner embeds only that folder, so a file here is never applied (Article 5)"
	case base == "README.md":
		return "" // the name GitHub renders for a folder
	}
	if stageName.MatchString(base) {
		return "named after a stage, phase or batch - name it by what it does, e.g. <module>_<feature>.go"
	}
	switch {
	case strings.HasSuffix(base, ".go") && !goName.MatchString(base):
		return "Go file names are lower snake_case"
	case strings.HasPrefix(p, "docs/") && strings.HasSuffix(base, ".md") && !docName.MatchString(base):
		return "doc file names are lower kebab-case"
	case strings.HasPrefix(p, "public/") && !webName.MatchString(base):
		return "browser file names are lower kebab-case"
	case strings.HasPrefix(p, "release/scripts/") && strings.HasSuffix(base, ".ps1"):
		if !releasePS.MatchString(base) {
			return "release scripts keep PowerShell's Verb-Noun form"
		}
	case strings.HasSuffix(base, ".ps1") && !psName.MatchString(base):
		return "PowerShell file names are lower kebab-case"
	case (strings.HasSuffix(base, ".sh") || strings.HasSuffix(base, ".py")) && !snakeShell.MatchString(base):
		return "shell and Python file names are lower snake_case"
	}
	return ""
}

func TestRepositoryLayout(t *testing.T) {
	root := repoRoot(t)
	files := listFiles(t, root)
	legacy := readLegacy(t)

	if _, err := os.Stat(filepath.Join(root, "CONTRIBUTING.md")); err != nil {
		t.Fatalf("CONTRIBUTING.md (the project constitution) is missing from the repository root")
	}

	var problems []string
	present := make(map[string]bool, len(files))
	for _, p := range files {
		present[p] = true
		if top, _, _ := strings.Cut(p, "/"); !rootEntries[top] {
			problems = append(problems, p+": not allowed at the repository root - put it in a folder (Article 3)")
		}
		base := path.Base(p)
		// public/downloads/ is the one place a binary ships on purpose: the
		// PrintBridge installer the app offers for download (release gate G4d).
		shippedDownload := strings.HasPrefix(p, "public/downloads/")
		if (neverCommitted[strings.ToLower(path.Ext(base))] && !shippedDownload) || strings.Contains(base, ".local.") || base == ".env" {
			problems = append(problems, p+": a build output or secret - builds go in bin/, private notes in .local/, both git-ignored (Article 3)")
		}
		if msg := namingProblem(p); msg != "" && !legacy[p] {
			problems = append(problems, p+": "+msg+" (Article 4)")
		}
	}
	// A listed file that is missing is only logged, not failed: a release
	// snapshot leaves held work out (release/hold.txt), so a listed file can
	// be absent there while it still exists in the shared tree.
	for p := range legacy {
		switch {
		case !present[p]:
			t.Logf("testdata/legacy-names.txt lists %s, which is not here - if it was renamed or deleted, delete that line", p)
		case namingProblem(p) == "":
			problems = append(problems, "testdata/legacy-names.txt lists "+p+", which now follows the rules - delete that line")
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Errorf("%d repository layout problem(s) - see CONTRIBUTING.md:\n  %s",
			len(problems), strings.Join(problems, "\n  "))
	}
}

// TestNamingRules pins what the name checks accept and reject, so a later
// edit to the patterns cannot quietly loosen them.
func TestNamingRules(t *testing.T) {
	for p, wantProblem := range map[string]bool{
		"engines/inventory_reorder.go":                          false,
		"engines/inventory_reorder_test.go":                     false,
		"engines/stage140_screen_fixes.go":                      true,
		"internal/server/handlers_stage47_18.go":                true,
		"engines/InventoryReorder.go":                           true,
		"engines/multistage_routing.go":                         false,
		"db/migrations/migrations_stage158_1_reorder_rules.sql": false,
		"db/migrations_stage158_1_reorder_rules.sql":            true,
		"docs/operations/backup-restore.md":                     false,
		"docs/operations/Backup_Restore.md":                     true,
		"docs/qa/phase2-findings.md":                            true,
		"docs/archive/audits/stage48-baseline.json":             false,
		"docs/brain/README.md":                                  false,
		"docs/product/releases/0.1.0-development.md":            false,
		"public/view-attribute-library.js":                      false,
		"public/viewAttributeLibrary.js":                        true,
		"scripts/batch2_fix.ps1":                                true,
		"docs/update-docs.ps1":                                  false,
		"scripts/verify_connector_live.ps1":                     true,
		"release/scripts/Invoke-Release.ps1":                    false,
		"release/scripts/ReleaseCommon.ps1":                     false,
		"release/scripts/invoke-release.ps1":                    true,
		"deploy/restore_drill.sh":                               false,
		"deploy/restore-drill.sh":                               true,
		"scripts/gen_error_catalog.py":                          false,
		"docs/qa/audit-http.py":                                 true,
		"internal/repolayout/testdata/legacy-names.txt":         false,
	} {
		if got := namingProblem(p) != ""; got != wantProblem {
			t.Errorf("namingProblem(%q) flagged=%v, want %v", p, got, wantProblem)
		}
	}
}

// repoRoot walks up from the package directory to the folder holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// listFiles returns every file git would commit: tracked files that still
// exist, plus new files that are not git-ignored. Ignored local folders
// (bin/, .local/, logs/, ...) are therefore never checked.
func listFiles(t *testing.T, root string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		t.Skipf("git ls-files is unavailable here (%v); the layout check needs a git checkout", err)
	}
	seen := map[string]bool{}
	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(p))); err != nil || info.IsDir() {
			continue // deleted in the working tree but not yet staged
		}
		files = append(files, p)
	}
	return files
}

// readLegacy loads the grandfather list: one repository path per line,
// '#' starts a comment.
func readLegacy(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "legacy-names.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	legacy := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		legacy[filepath.ToSlash(line)] = true
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return legacy
}
