// Package supplychain derives Stage 49.9's dependency-inventory and
// release-artifact facts, with no third-party tooling and no code imported by
// the production binary - the same "scan the source, cost the build nothing"
// shape as internal/securityscan (49.1.1).
//
// Two concerns live here, because the second is the first read back at
// release time:
//
//   - The dependency ledger (49.9.4): every Go module, CI action, CI-only
//     tool and OS/runner image this build depends on, with an owner, purpose,
//     license, provenance note, version and checksum. docs/security/
//     dependency-ledger.json is the hand-maintained record; LoadLedger reads
//     it and the Validate* functions cross-check it against go.mod, go.sum
//     and .github/workflows/*.yml so a new or changed dependency that is not
//     recorded fails a test rather than shipping silently reviewed.
//   - The release manifest (49.9.6/49.9.7/49.9.9): manifest.go builds a
//     signed-ready JSON record of exactly what produced one build - commit,
//     toolchain, the ledger above, and content hashes of the binary,
//     migrations and static assets - and can verify a candidate artifact
//     against a previously generated manifest.
//
// cmd/releasemanifest is the only caller outside this package's own tests.
package supplychain

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// LedgerSchemaVersion is bumped whenever Ledger's shape changes.
const LedgerSchemaVersion = 1

// ComponentType classifies one dependency ledger entry into the four
// categories 49.9.4 asks for: direct/transitive/tool/OS.
type ComponentType string

const (
	ComponentGoModuleDirect   ComponentType = "go_module_direct"
	ComponentGoModuleIndirect ComponentType = "go_module_indirect"
	ComponentGoToolchain      ComponentType = "go_toolchain"
	ComponentCIAction         ComponentType = "ci_action"
	ComponentCITool           ComponentType = "ci_tool"
	ComponentOSRunner         ComponentType = "os_runner"
	ComponentOSImage          ComponentType = "os_container_image"
)

// LedgerEntry is one inventoried component: owner/purpose/license/
// provenance/version/checksum, exactly the fields 49.9.4 names.
type LedgerEntry struct {
	Name       string        `json:"name"`
	Type       ComponentType `json:"type"`
	Version    string        `json:"version"`
	Checksum   string        `json:"checksum"`
	Owner      string        `json:"owner"`
	Purpose    string        `json:"purpose"`
	License    string        `json:"license"`
	Provenance string        `json:"provenance"`
	Note       string        `json:"note,omitempty"`
}

// Ledger is the whole committed inventory: docs/security/dependency-ledger.json.
type Ledger struct {
	SchemaVersion int           `json:"schema_version"`
	Updated       string        `json:"updated"`
	Entries       []LedgerEntry `json:"entries"`
}

// LoadLedger reads and parses the ledger file at path.
func LoadLedger(path string) (*Ledger, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ledger %s: %w", path, err)
	}
	var l Ledger
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("parse ledger %s: %w", path, err)
	}
	return &l, nil
}

// findEntry returns the ledger entry named name, or nil.
func (l *Ledger) findEntry(name string) *LedgerEntry {
	for i := range l.Entries {
		if l.Entries[i].Name == name {
			return &l.Entries[i]
		}
	}
	return nil
}

// EntriesByType returns every entry of the given type, name-sorted.
func (l *Ledger) EntriesByType(t ComponentType) []LedgerEntry {
	var out []LedgerEntry
	for _, e := range l.Entries {
		if e.Type == t {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// GoModule is one require-block entry from go.mod, before cross-referencing
// go.sum for its checksum.
type GoModule struct {
	Path    string
	Version string
	Direct  bool
}

var goModRequireLine = regexp.MustCompile(`^\s*([^\s]+)\s+(v[^\s]+)(\s*//\s*indirect)?\s*$`)

// ParseGoMod reads root/go.mod and returns the module's own path, its
// declared `go` toolchain version, and every require-block entry.
func ParseGoMod(root string) (modulePath, goVersion string, modules []GoModule, err error) {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", "", nil, fmt.Errorf("open go.mod: %w", err)
	}
	defer f.Close()

	inRequire := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "module "):
			modulePath = strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
		case strings.HasPrefix(trimmed, "go ") && goVersion == "":
			goVersion = strings.TrimSpace(strings.TrimPrefix(trimmed, "go "))
		case strings.HasPrefix(trimmed, "require ("):
			inRequire = true
		case inRequire && trimmed == ")":
			inRequire = false
		case inRequire:
			if m := goModRequireLine.FindStringSubmatch(line); m != nil {
				modules = append(modules, GoModule{Path: m[1], Version: m[2], Direct: m[3] == ""})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", nil, fmt.Errorf("scan go.mod: %w", err)
	}
	if modulePath == "" {
		return "", "", nil, fmt.Errorf("go.mod has no module line")
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	return modulePath, goVersion, modules, nil
}

// ParseGoSum reads root/go.sum and returns, for each "module version" pair,
// the h1 content hash of the module zip (not its /go.mod hash line) - the
// same value `go mod verify` checks downloaded module content against.
func ParseGoSum(root string) (map[string]string, error) {
	f, err := os.Open(filepath.Join(root, "go.sum"))
	if err != nil {
		return nil, fmt.Errorf("open go.sum: %w", err)
	}
	defer f.Close()

	out := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 {
			continue
		}
		module, version, hash := fields[0], fields[1], fields[2]
		if strings.HasSuffix(version, "/go.mod") {
			continue // the go.mod-only hash line; we want the module content hash
		}
		out[module+"@"+version] = hash
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan go.sum: %w", err)
	}
	return out, nil
}

// PinnedAction is one `uses:` reference found in a GitHub Actions workflow
// file.
type PinnedAction struct {
	Action string // e.g. "actions/checkout"
	Ref    string // whatever follows '@' - a tag, branch or commit SHA
	File   string
	Line   int
}

// PinnedSHA reports whether the action's ref is a full 40-character commit
// SHA - the only ref an attacker who compromises the tag cannot silently
// repoint (49.9.8's "pinned immutable third-party actions").
var fullCommitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (p PinnedAction) PinnedSHA() bool { return fullCommitSHA.MatchString(p.Ref) }

var usesLine = regexp.MustCompile(`^\s*(?:-\s*)?uses:\s*([A-Za-z0-9_.\-]+/[A-Za-z0-9_.\-]+)@([A-Za-z0-9_.\-]+)\s*(?:#.*)?$`)

// ParsePinnedActions scans every *.yml/*.yaml file under root/.github/workflows
// for `uses: owner/repo@ref` lines. Local actions (a bare relative path,
// no '@') and Docker-reference actions ("docker://...") are not third-party
// registry actions in the sense 49.9.8 means, and are skipped.
func ParsePinnedActions(root string) ([]PinnedAction, error) {
	dir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var out []PinnedAction
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if m := usesLine.FindStringSubmatch(line); m != nil {
				out = append(out, PinnedAction{Action: m[1], Ref: m[2], File: name, Line: i + 1})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// ValidateGoModuleCoverage checks that every module go.mod requires has a
// ledger entry of the matching direct/indirect type, with every field filled
// and a checksum matching go.sum's content hash for that exact version. It
// returns one human-readable problem string per finding, empty when clean.
//
// This is the automatic half of "review new dependency source/maintainer/
// update surface" (49.9.4): a module bump or addition that is not reflected
// in the ledger fails here, not silently at review time.
func ValidateGoModuleCoverage(ledger *Ledger, modules []GoModule, sums map[string]string) []string {
	var problems []string
	for _, m := range modules {
		wantType := ComponentGoModuleIndirect
		if m.Direct {
			wantType = ComponentGoModuleDirect
		}
		entry := ledger.findEntry(m.Path)
		if entry == nil {
			problems = append(problems, fmt.Sprintf("go.mod requires %s %s with no ledger entry - add one to docs/security/dependency-ledger.json", m.Path, m.Version))
			continue
		}
		if entry.Type != wantType {
			problems = append(problems, fmt.Sprintf("%s: ledger type %q does not match go.mod (want %q - direct-ness changed)", m.Path, entry.Type, wantType))
		}
		if entry.Version != m.Version {
			problems = append(problems, fmt.Sprintf("%s: ledger version %s does not match go.mod %s - dependency was bumped, update the ledger", m.Path, entry.Version, m.Version))
		}
		for field, val := range map[string]string{"owner": entry.Owner, "purpose": entry.Purpose, "license": entry.License, "provenance": entry.Provenance, "checksum": entry.Checksum} {
			if strings.TrimSpace(val) == "" {
				problems = append(problems, fmt.Sprintf("%s: ledger entry has an empty %s field", m.Path, field))
			}
		}
		if want, ok := sums[m.Path+"@"+m.Version]; ok && entry.Checksum != "" && entry.Checksum != want {
			problems = append(problems, fmt.Sprintf("%s@%s: ledger checksum %s does not match go.sum %s", m.Path, m.Version, entry.Checksum, want))
		}
	}
	return problems
}

// ValidateActionsArePinned reports every third-party action reference that
// is not pinned to a full commit SHA - 49.9.8's containment control, applied
// automatically to every action in every workflow, present or future.
func ValidateActionsArePinned(actions []PinnedAction) []string {
	var problems []string
	for _, a := range actions {
		if !a.PinnedSHA() {
			problems = append(problems, fmt.Sprintf("%s:%d: %s@%s is not pinned to a full commit SHA", a.File, a.Line, a.Action, a.Ref))
		}
	}
	return problems
}

// ValidateActionLedgerCoverage checks that every distinct action referenced
// in the workflows has a ci_action ledger entry, independent of whether it is
// pinned - so a new action still needs an owner/purpose/license/provenance
// row even before anyone gets around to pinning it.
func ValidateActionLedgerCoverage(ledger *Ledger, actions []PinnedAction) []string {
	seen := map[string]bool{}
	var problems []string
	for _, a := range actions {
		if seen[a.Action] {
			continue
		}
		seen[a.Action] = true
		if ledger.findEntry(a.Action) == nil {
			problems = append(problems, fmt.Sprintf("%s: no ledger entry for this GitHub Action (first seen %s:%d)", a.Action, a.File, a.Line))
		}
	}
	return problems
}
