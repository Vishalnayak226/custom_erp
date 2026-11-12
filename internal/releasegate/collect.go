package releasegate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"custom_erp/internal/supplychain"
)

func git(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// IsAncestor is the evidence rule's "commit is in this release's history".
func IsAncestor(root, base string) func(string) bool {
	return func(c string) bool {
		if !isHexCommit(c) {
			return false
		}
		cmd := exec.Command("git", "-C", root, "merge-base", "--is-ancestor", c, base)
		return cmd.Run() == nil
	}
}

// checksumOf mirrors db.checksumOf (CRLF to LF, sha256), the value stored in
// schema_migrations.checksum, so this record and the database ledger agree.
func checksumOf(body []byte) string {
	sum := sha256.Sum256(bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n")))
	return hex.EncodeToString(sum[:])
}

// CollectMigrations lists db/migrations/*.sql with checksums and says what changed since base.
func CollectMigrations(root, base string) ([]Migration, string, string, error) {
	files, err := filepath.Glob(filepath.Join(root, "db", "migrations", "*.sql"))
	if err != nil {
		return nil, "", "", err
	}
	sort.Strings(files)
	var ms []Migration
	set := sha256.New()
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return nil, "", "", err
		}
		m := Migration{Name: filepath.Base(f), SHA256: checksumOf(body)}
		ms = append(ms, m)
		set.Write([]byte(m.Name + " " + m.SHA256 + "\n"))
	}
	note := ""
	if isHexCommit(base) {
		if out, err := git(root, "diff", "--name-status", base, "--", "db/migrations/*.sql"); err == nil {
			added, edited := 0, 0
			for _, l := range strings.Split(out, "\n") {
				switch {
				case strings.HasPrefix(l, "A"):
					added++
				case strings.HasPrefix(l, "M"):
					edited++
				}
			}
			note = fmt.Sprintf("Since the previous release: %d added, %d edited (an edited file is re-checksummed by the migration runner).", added, edited)
		}
	}
	return ms, hex.EncodeToString(set.Sum(nil)), note, nil
}

var envNameRe = regexp.MustCompile(`[eE]nv\w*\(\s*"([A-Z][A-Z0-9_]{2,})"`)

// scanEnv returns the environment names a Go source text reads.
func scanEnv(src string, into map[string]bool) {
	for _, m := range envNameRe.FindAllStringSubmatch(src, -1) {
		into[m[1]] = true
	}
}

// CollectConfig scans non-test Go files for the environment names the build
// reads (Getenv, LookupEnv and the env helpers), and diffs against base.
func CollectConfig(root, base string) ConfigRef {
	now := map[string]bool{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "graphify-out", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			if b, err := os.ReadFile(p); err == nil {
				scanEnv(string(b), now)
			}
		}
		return nil
	})
	ref := ConfigRef{EnvNames: keys(now)}
	h := sha256.Sum256([]byte(strings.Join(ref.EnvNames, "\n")))
	ref.SHA256 = hex.EncodeToString(h[:])
	if isHexCommit(base) {
		if out, err := git(root, "grep", "-h", "-E", `[eE]nv[A-Za-z0-9_]*\(\s*"[A-Z][A-Z0-9_]{2,}"`, base, "--", "*.go", ":!*_test.go"); err == nil {
			old := map[string]bool{}
			scanEnv(out, old)
			for k := range now {
				if !old[k] {
					ref.Added = append(ref.Added, k)
				}
			}
			for k := range old {
				if !now[k] {
					ref.Removed = append(ref.Removed, k)
				}
			}
			sort.Strings(ref.Added)
			sort.Strings(ref.Removed)
		}
	}
	return ref
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CollectSBOM writes the release manifest (cmd/releasemanifest's library:
// commit, toolchain, dependency ledger, migration and static-asset hashes,
// artifacts) into outDir and returns a reference to it, plus the dependency
// and licence record read from the same ledger.
func CollectSBOM(root, base, tree, outDir string, arts []Artifact) (SBOMRef, DepsRef) {
	ledgerPath := filepath.Join("docs", "security", "dependency-ledger.json")
	deps := DepsRef{ByType: map[string]int{}, Licenses: map[string]int{}, LedgerFile: filepath.ToSlash(ledgerPath)}
	ledger, err := supplychain.LoadLedger(filepath.Join(root, ledgerPath))
	if err != nil {
		return SBOMRef{Error: err.Error()}, deps
	}
	for _, e := range ledger.Entries {
		deps.ByType[string(e.Type)]++
		lic := strings.TrimSpace(e.License)
		if lic == "" {
			deps.NoLicense = append(deps.NoLicense, e.Name)
			continue
		}
		deps.Licenses[lic]++
	}
	var hashed []supplychain.ArtifactHash
	for _, a := range arts {
		hashed = append(hashed, supplychain.ArtifactHash{Name: a.Name, SHA256: a.SHA256, SizeBytes: a.Size})
	}
	m, err := supplychain.BuildManifest(supplychain.BuildOptions{
		Root: root, Commit: base, Builder: "release:Invoke-Release.ps1", GoVersion: runtime.Version(), Ledger: ledger, Artifacts: hashed,
	})
	if err != nil {
		return SBOMRef{Error: err.Error()}, deps
	}
	m.Notes = append(m.Notes, fmt.Sprintf("candidate tree %s on top of release %s; the new commit sha exists after G11", tree, base))
	enc, err := supplychain.Encode(m)
	if err != nil {
		return SBOMRef{Error: err.Error()}, deps
	}
	name := "release-manifest-" + short(tree) + ".json"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return SBOMRef{Error: err.Error()}, deps
	}
	if err := os.WriteFile(filepath.Join(outDir, name), enc, 0o644); err != nil {
		return SBOMRef{Error: err.Error()}, deps
	}
	sum := sha256.Sum256(enc)
	return SBOMRef{File: name, SHA256: hex.EncodeToString(sum[:]), Dependencies: len(m.Dependencies)}, deps
}

// ParseImpact reads cmd/securityimpact's text output.
func ParseImpact(text string, ok bool) Surface {
	s := Surface{Available: ok}
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		switch {
		case strings.HasPrefix(l, "  ! "):
			s.High = append(s.High, strings.TrimPrefix(l, "  ! "))
		case strings.HasPrefix(l, "  - "):
			s.Other++
		}
	}
	return s
}

// RunGovulncheck runs the scanner if it is installed. Exit 0 is clean, 3 is
// "vulnerabilities found"; anything else (no network, no tool) is not-run.
func RunGovulncheck(tool, dir string) Vuln {
	if tool == "" {
		if p, err := exec.LookPath("govulncheck"); err == nil {
			tool = p
		} else if home, err := os.UserHomeDir(); err == nil {
			for _, n := range []string{"govulncheck.exe", "govulncheck"} {
				p := filepath.Join(home, "go", "bin", n)
				if _, err := os.Stat(p); err == nil {
					tool = p
					break
				}
			}
		}
	}
	if tool == "" {
		return Vuln{Detail: "govulncheck is not installed on this machine"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, "./...")
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := out.String()
	if err == nil {
		return Vuln{Ran: true}
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 3 {
		var ids []string
		for _, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(l, "Vulnerability #") {
				if i := strings.Index(l, ": "); i > 0 {
					ids = append(ids, strings.TrimSpace(l[i+2:]))
				}
			}
		}
		return Vuln{Ran: true, Findings: max1(len(ids)), Detail: strings.Join(firstN(ids, 8), ", ")}
	}
	tail := strings.Split(strings.TrimSpace(text), "\n")
	return Vuln{Detail: "govulncheck could not complete: " + strings.Join(tail[max0(len(tail)-2):], " ")}
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// LoadEvidence reads release/evidence/<kind>.json for every kind; missing files stay nil.
func LoadEvidence(dir string) (map[string]*Evidence, error) {
	out := map[string]*Evidence{}
	for _, k := range EvidenceKinds {
		b, err := os.ReadFile(filepath.Join(dir, k.Kind+".json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var e Evidence
		if err := json.Unmarshal(b, &e); err != nil {
			return nil, fmt.Errorf("%s.json: %w", k.Kind, err)
		}
		out[k.Kind] = &e
	}
	return out, nil
}
