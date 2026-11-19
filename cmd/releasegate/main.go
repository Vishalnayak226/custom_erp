// Command releasegate is the release-candidate gate (Stage 47.17.3, 49.17.2).
//
//	releasegate check   [-root .] [-base <sha>] [-tree <sha>] [-out dir] [-impact-file f]
//	                    [-result build=pass ...] [-artifact name=path] [-govulncheck path] [-skip-vuln]
//	                    [-github-kinds browser_budget,journey]   kinds release.yml's evidence job makes
//	releasegate judge   -kinds browser_budget,journey [-commit <sha>]   that job's verdict: exit 1 only when the policy blocks
//	releasegate payload -id <exception id> [-out file]    the text a P1 exception's signature covers
//	releasegate record  -kind <evidence kind> -result pass|fail -by <name> [-note ..] [-date ..]
//	releasegate record  -from <evidence file> -commit <sha>   ingest a record the restore drill wrote
//	releasegate verify  -record f -commit <sha> [-tree <sha>]   is this record for that candidate
//
// check writes release-candidate-<tree>.json and .md plus the SBOM manifest
// into -out and exits 1 only when a check fails. The rules live in
// internal/releasegate and are described in docs/operations/release-candidate-gate.md.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"custom_erp/internal/releasegate"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: releasegate check|judge|payload|record|verify ...")
		os.Exit(2)
	}
	args := os.Args[2:]
	switch os.Args[1] {
	case "check":
		os.Exit(check(args))
	case "payload":
		os.Exit(payload(args))
	case "record":
		os.Exit(record(args))
	case "verify":
		os.Exit(verify(args))
	case "judge":
		os.Exit(judge(args))
	}
	fmt.Fprintln(os.Stderr, "unknown command", os.Args[1])
	os.Exit(2)
}

func gitOut(root string, a ...string) string {
	out, err := exec.Command("git", append([]string{"-C", root}, a...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func readIf(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	return b, err == nil
}

func check(args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	root := fs.String("root", ".", "release worktree")
	base := fs.String("base", "", "previous release commit (default: HEAD of -root)")
	tree := fs.String("tree", "", "tree being released (default: git write-tree of -root)")
	version := fs.String("version", "", "release number")
	out := fs.String("out", "", "directory for the record (default: a folder in the temp dir)")
	impact := fs.String("impact-file", "", "securityimpact output of G6b")
	vuln := fs.String("govulncheck", "", "path to govulncheck (default: PATH, then ~/go/bin)")
	skipVuln := fs.Bool("skip-vuln", false, "do not run govulncheck (recorded as not-run)")
	now := fs.String("now", "", "override today, YYYY-MM-DD (tests)")
	github := fs.String("github-kinds", "", "evidence kinds release.yml's evidence job makes on the candidate (comma-separated): recorded as deferred, not read from release/evidence")
	var results, artifacts multi
	fs.Var(&results, "result", "name=value from the release's own run (build, vet, tests), repeatable")
	fs.Var(&artifacts, "artifact", "name=path, repeatable")
	fs.Parse(args)

	if *base == "" {
		*base = gitOut(*root, "rev-parse", "HEAD")
	}
	if *tree == "" {
		*tree = gitOut(*root, "write-tree")
	}
	in := releasegate.Input{Now: time.Now(), Version: *version, BaseCommit: *base, Tree: *tree, Results: map[string]string{}}
	if *now != "" {
		t, err := time.Parse("2006-01-02", *now)
		if err != nil {
			fmt.Fprintln(os.Stderr, "-now:", err)
			return 2
		}
		in.Now = t
	}
	if in.Version == "" {
		if b, ok := readIf(filepath.Join(*root, "internal", "server", "VERSION")); ok {
			in.Version = strings.TrimSpace(string(b))
		}
	}
	for _, r := range results {
		k, v, _ := strings.Cut(r, "=")
		in.Results[k] = v
	}
	for _, a := range artifacts {
		name, path, _ := strings.Cut(a, "=")
		h, err := releasegate.HashArtifact(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "-artifact:", err)
			return 2
		}
		h.Name = name
		in.Artifacts = append(in.Artifacts, h)
	}

	ghKinds, err := parseKinds(*github)
	if err != nil {
		fmt.Fprintln(os.Stderr, "-github-kinds:", err)
		return 2
	}
	if len(ghKinds) > 0 {
		in.GitHubKinds = map[string]bool{}
		for _, k := range ghKinds {
			in.GitHubKinds[k] = true
		}
	}
	if err := loadRegisters(*root, &in); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	in.IsAncestor = releasegate.IsAncestor(*root, *base)
	lb, _ := readIf(filepath.Join(*root, "docs", "operations", "service-operations.md"))
	in.LimitsExist = strings.Contains(string(lb), "## Availability model")

	var mErr error
	in.Migrations, in.MigrationsSHA, in.MigrationNote, mErr = releasegate.CollectMigrations(*root, *base)
	if mErr != nil {
		fmt.Fprintln(os.Stderr, mErr)
		return 2
	}
	in.Config = releasegate.CollectConfig(*root, *base)
	outDir := *out
	if outDir == "" {
		outDir = filepath.Join(os.TempDir(), "releasegate")
	}
	in.SBOM, in.Deps = releasegate.CollectSBOM(*root, *base, *tree, outDir, in.Artifacts)
	if *impact != "" {
		b, ok := readIf(*impact)
		in.Surface = releasegate.ParseImpact(string(b), ok)
	}
	if *skipVuln {
		in.Vuln = releasegate.Vuln{Detail: "skipped with -skip-vuln"}
	} else {
		in.Vuln = releasegate.RunGovulncheck(*vuln, *root)
	}

	rec := releasegate.Evaluate(in)
	js, _ := rec.JSON()
	name := "release-candidate-" + releasegate.Short(*tree)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := os.WriteFile(filepath.Join(outDir, name+".json"), js, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	_ = os.WriteFile(filepath.Join(outDir, name+".md"), []byte(rec.Markdown()), 0o644)
	for _, c := range rec.Checks {
		fmt.Printf("%-8s %-22s %s\n", strings.ToUpper(string(c.Status)), c.ID, c.Detail)
	}
	fmt.Printf("\nrelease-candidate gate: %s (%d warning/not-run). Record: %s\n", strings.ToUpper(rec.Verdict), rec.Warnings, filepath.Join(outDir, name+".json"))
	if rec.Verdict != "pass" {
		return 1
	}
	return 0
}

// loadRegisters reads what check and judge both judge against: the policy,
// the exception register and its public key, and release/evidence.
func loadRegisters(root string, in *releasegate.Input) error {
	var err error
	if pb, ok := readIf(filepath.Join(root, "release", "gate-policy.json")); ok {
		in.Policy, err = releasegate.ParsePolicy(pb)
	} else {
		in.Policy, err = releasegate.ParsePolicy(nil)
	}
	if err != nil {
		return err
	}
	if eb, ok := readIf(filepath.Join(root, "release", "exceptions.json")); ok {
		f, err := releasegate.ParseExceptions(eb)
		if err != nil {
			return err
		}
		in.Exceptions = &f
	}
	if kb, ok := readIf(filepath.Join(root, "release", "signing.pub")); ok {
		in.PublicKey, _ = releasegate.ParsePublicKey(kb)
	}
	in.Evidence, err = releasegate.LoadEvidence(filepath.Join(root, "release", "evidence"))
	return err
}

func knownKind(kind string) bool {
	for _, k := range releasegate.EvidenceKinds {
		if k.Kind == kind {
			return true
		}
	}
	return false
}

// parseKinds splits a comma-separated list of evidence kinds and refuses an
// unknown one, so a typo never silently skips a kind.
func parseKinds(s string) ([]string, error) {
	var kinds []string
	for _, k := range strings.Split(s, ",") {
		if k = strings.TrimSpace(k); k == "" {
			continue
		}
		if !knownKind(k) {
			return nil, fmt.Errorf("unknown evidence kind %q", k)
		}
		kinds = append(kinds, k)
	}
	return kinds, nil
}

// judge is the last step of release.yml's evidence job (47.9.4, 47.17.2): it
// judges the evidence the job just wrote with the click's own rules and exits
// 1 only when the policy blocks, so a kind on block_after_pass warns until its
// first pass and blocks after it.
func judge(args []string) int {
	fs := flag.NewFlagSet("judge", flag.ExitOnError)
	root := fs.String("root", ".", "worktree holding release/evidence")
	kindList := fs.String("kinds", "", "evidence kinds to judge, comma-separated")
	commit := fs.String("commit", "", "the candidate commit the evidence must be for (default: HEAD)")
	now := fs.String("now", "", "override today, YYYY-MM-DD (tests)")
	fs.Parse(args)
	kinds, err := parseKinds(*kindList)
	if err != nil {
		fmt.Fprintln(os.Stderr, "-kinds:", err)
		return 2
	}
	if len(kinds) == 0 {
		fmt.Fprintln(os.Stderr, "need -kinds, one or more evidence kinds")
		return 2
	}
	in := releasegate.Input{Now: time.Now()}
	if *now != "" {
		if in.Now, err = time.Parse("2006-01-02", *now); err != nil {
			fmt.Fprintln(os.Stderr, "-now:", err)
			return 2
		}
	}
	if err := loadRegisters(*root, &in); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *commit == "" {
		*commit = gitOut(*root, "rev-parse", "HEAD")
	}
	in.IsAncestor = releasegate.IsAncestor(*root, *commit)
	code := 0
	for _, c := range releasegate.JudgeEvidence(in, kinds) {
		fmt.Printf("%-8s %-22s %s\n", strings.ToUpper(string(c.Status)), c.ID, c.Detail)
		if c.Status == releasegate.Fail {
			code = 1
		}
	}
	return code
}

func payload(args []string) int {
	fs := flag.NewFlagSet("payload", flag.ExitOnError)
	root := fs.String("root", ".", "worktree")
	id := fs.String("id", "", "exception id")
	out := fs.String("out", "", "file to write (default: stdout)")
	fs.Parse(args)
	b, err := os.ReadFile(filepath.Join(*root, "release", "exceptions.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	f, err := releasegate.ParseExceptions(b)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	for _, e := range f.Exceptions {
		if e.ID == *id {
			if *out == "" {
				os.Stdout.Write(e.Payload())
				return 0
			}
			if err := os.WriteFile(*out, e.Payload(), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			fmt.Println("wrote", *out, "- now run: releasesign sign -key %USERPROFILE%\\.erp-release\\signing.key -in", *out, "and paste the line into \"signature\"")
			return 0
		}
	}
	fmt.Fprintln(os.Stderr, "no exception with id", *id)
	return 2
}

func record(args []string) int {
	fs := flag.NewFlagSet("record", flag.ExitOnError)
	root := fs.String("root", ".", "worktree")
	kind := fs.String("kind", "", "evidence kind")
	result := fs.String("result", "", "pass or fail")
	by := fs.String("by", "", "who ran it")
	note := fs.String("note", "", "what was run, where")
	date := fs.String("date", time.Now().UTC().Format("2006-01-02"), "day the run finished")
	from := fs.String("from", "", "an evidence file another program wrote (restore_drill.sh); -commit names the build it tested")
	commit := fs.String("commit", "", "commit tested (default: HEAD)")
	fs.Parse(args)
	if *from != "" {
		b, err := os.ReadFile(*from)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		ev, err := releasegate.ParseEvidenceFile(b, *commit)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		p, err := releasegate.WriteEvidence(*root, ev)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Println("wrote", p, "-", ev.Kind, ev.Result, ev.Date)
		return 0
	}
	if !knownKind(*kind) || (*result != "pass" && *result != "fail") || *by == "" {
		fmt.Fprintln(os.Stderr, "need -kind (one of the evidence kinds), -result pass|fail and -by")
		return 2
	}
	if *commit == "" {
		*commit = gitOut(*root, "rev-parse", "HEAD")
	}
	ev := releasegate.Evidence{Kind: *kind, Date: *date, Commit: *commit, Result: *result, By: *by, Note: *note}
	p, err := releasegate.WriteEvidence(*root, ev)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Println("wrote", p)
	return 0
}

func verify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	rec := fs.String("record", "", "release-candidate-*.json")
	commit := fs.String("commit", "", "base commit the record must be for")
	tree := fs.String("tree", "", "tree the record must be for (optional)")
	fs.Parse(args)
	b, err := os.ReadFile(*rec)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var r releasegate.Record
	if err := json.Unmarshal(b, &r); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := releasegate.VerifyBinding(r, *commit, *tree); err != nil {
		fmt.Fprintln(os.Stderr, "REFUSED:", err)
		for _, f := range r.Failures() {
			fmt.Fprintln(os.Stderr, "  blocking check failed -", f)
		}
		return 1
	}
	fmt.Println("record matches", *commit)
	return 0
}
