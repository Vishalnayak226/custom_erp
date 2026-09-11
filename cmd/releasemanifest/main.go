// Command releasemanifest builds and verifies Stage 49.9's release manifest:
// the SBOM/checksum/provenance record that ties one build's binary,
// migrations and static assets back to a commit, a toolchain and the
// dependency ledger (docs/security/dependency-ledger.json).
//
// It does not sign anything - this repository has no signing key to hold,
// and provisioning one is an infrastructure decision, not a build-tool one
// (see docs/security/secure-development-lifecycle.md). What it produces is
// exactly what a real signing step would sign: a deterministic JSON manifest
// whose SHA-256 a detached cosign/minisign/GPG signature could cover, and
// whose VerifyArtifact check is what a deployment step runs before trusting
// a binary, signed or not.
//
// Usage:
//
//	# After building the release binary, from the repo root:
//	go run ./cmd/releasemanifest \
//	    -commit "$(git rev-parse HEAD)" \
//	    -builder "github-actions:release/${GITHUB_RUN_ID}" \
//	    -artifact erp-server=deploy/build/erp-server \
//	    -out deploy/build/release_manifest.json
//
//	# Later, before trusting a copy of that binary (e.g. after scp):
//	go run ./cmd/releasemanifest -verify \
//	    -manifest deploy/build/release_manifest.json \
//	    -artifact erp-server=/opt/erp/erp-server.new
//
// On this project's Windows dev machine, Controlled Folder Access blocks a
// freshly built binary from writing under Documents\ (see CLAUDE.md); use
// -out to write into %TEMP% and copy the file in with PowerShell, the same
// way docs/brain/update-brain.ps1 and cmd/surfacescan do.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"custom_erp/internal/supplychain"
)

// artifactFlag collects repeated -artifact name=path flags.
type artifactFlag struct {
	names []string
	paths []string
}

func (a *artifactFlag) String() string { return "" }

func (a *artifactFlag) Set(value string) error {
	name, path, ok := strings.Cut(value, "=")
	if !ok || name == "" || path == "" {
		return fmt.Errorf("expected -artifact name=path, got %q", value)
	}
	a.names = append(a.names, name)
	a.paths = append(a.paths, path)
	return nil
}

func main() {
	root := flag.String("root", ".", "repository root")
	ledgerPath := flag.String("ledger", filepath.Join("docs", "security", "dependency-ledger.json"), "dependency ledger, resolved against -root unless absolute")
	commit := flag.String("commit", "", "commit SHA this build was made from; defaults to `git rev-parse HEAD` in -root")
	branch := flag.String("branch", "", "branch name, if known")
	builder := flag.String("builder", "local", `who built this - e.g. "github-actions:release/12345" or "local:devbox"`)
	out := flag.String("out", "", "file to write the manifest to (default: stdout)")
	verify := flag.Bool("verify", false, "verify -artifact file(s) against -manifest instead of generating a new manifest")
	manifestIn := flag.String("manifest", "", "manifest to verify against (required with -verify)")
	var artifacts artifactFlag
	flag.Var(&artifacts, "artifact", "name=path, repeatable; hashed and recorded (generate mode) or recomputed and compared (verify mode)")
	flag.Parse()

	if *verify {
		runVerify(*manifestIn, artifacts)
		return
	}
	runGenerate(*root, *ledgerPath, *commit, *branch, *builder, *out, artifacts)
}

func runGenerate(root, ledgerPath, commit, branch, builder, out string, artifacts artifactFlag) {
	if commit == "" {
		resolved, err := resolveGitCommit(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "releasemanifest: -commit not given and could not resolve one from git: %v\n", err)
			os.Exit(2)
		}
		commit = resolved
	}

	resolvedLedgerPath := ledgerPath
	if !filepath.IsAbs(resolvedLedgerPath) {
		resolvedLedgerPath = filepath.Join(root, resolvedLedgerPath)
	}
	ledger, err := supplychain.LoadLedger(resolvedLedgerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "releasemanifest: %v\n", err)
		os.Exit(2)
	}

	var hashed []supplychain.ArtifactHash
	for i, name := range artifacts.names {
		a, err := supplychain.HashFile(artifacts.paths[i])
		if err != nil {
			fmt.Fprintf(os.Stderr, "releasemanifest: %v\n", err)
			os.Exit(2)
		}
		a.Name = name
		hashed = append(hashed, a)
	}

	manifest, err := supplychain.BuildManifest(supplychain.BuildOptions{
		Root:      root,
		Commit:    commit,
		Branch:    branch,
		Builder:   builder,
		GoVersion: runtime.Version(),
		Ledger:    ledger,
		Artifacts: hashed,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "releasemanifest: %v\n", err)
		os.Exit(2)
	}

	encoded, err := supplychain.Encode(manifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "releasemanifest: %v\n", err)
		os.Exit(2)
	}

	if out == "" {
		os.Stdout.Write(encoded)
		return
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "releasemanifest: %v\n", err)
		os.Exit(2)
	}
	if err := os.WriteFile(out, encoded, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "releasemanifest: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("releasemanifest: wrote %s (commit %s, %d dependencies, %d artifact(s), %d migration files, %d static asset files)\n",
		out, manifest.CommitShort, len(manifest.Dependencies), len(manifest.Artifacts), manifest.MigrationSet.FileCount, manifest.StaticAssetSet.FileCount)
}

func runVerify(manifestPath string, artifacts artifactFlag) {
	if manifestPath == "" {
		fmt.Fprintln(os.Stderr, "releasemanifest: -verify requires -manifest")
		os.Exit(2)
	}
	if len(artifacts.names) == 0 {
		fmt.Fprintln(os.Stderr, "releasemanifest: -verify requires at least one -artifact name=path")
		os.Exit(2)
	}
	manifest, err := supplychain.LoadManifest(manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "releasemanifest: %v\n", err)
		os.Exit(2)
	}

	allOK := true
	for i, name := range artifacts.names {
		ok, msg, err := supplychain.VerifyArtifact(manifest, name, artifacts.paths[i])
		if err != nil {
			fmt.Fprintf(os.Stderr, "releasemanifest: %v\n", err)
			os.Exit(2)
		}
		status := "OK"
		if !ok {
			status = "REFUSED"
			allOK = false
		}
		fmt.Printf("[%s] %s: %s\n", status, name, msg)
	}
	if !allOK {
		fmt.Fprintln(os.Stderr, "\nreleasemanifest: at least one artifact does not match the manifest - refuse to deploy it")
		os.Exit(1)
	}
	fmt.Printf("releasemanifest: manifest %s verified for commit %s\n", manifestPath, manifest.CommitShort)
}

func resolveGitCommit(root string) (string, error) {
	cmd := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
