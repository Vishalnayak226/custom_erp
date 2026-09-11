package supplychain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashFileSetIsOrderIndependent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	forward, err := HashFileSet([]string{a, b})
	if err != nil {
		t.Fatalf("HashFileSet forward: %v", err)
	}
	backward, err := HashFileSet([]string{b, a})
	if err != nil {
		t.Fatalf("HashFileSet backward: %v", err)
	}
	if forward.SHA256 != backward.SHA256 {
		t.Fatalf("hash depends on input order: %s vs %s", forward.SHA256, backward.SHA256)
	}
	if forward.FileCount != 2 {
		t.Fatalf("FileCount = %d, want 2", forward.FileCount)
	}
}

func TestHashFileSetChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := HashFileSet([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := HashFileSet([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if before.SHA256 == after.SHA256 {
		t.Fatalf("hash did not change when file content changed")
	}
}

func TestHashFileSetChangesWithPath(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.txt")
	p2 := filepath.Join(dir, "renamed.txt")
	if err := os.WriteFile(p1, []byte("same content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("same content"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1, err := HashFileSet([]string{p1})
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashFileSet([]string{p2})
	if err != nil {
		t.Fatal(err)
	}
	if h1.SHA256 == h2.SHA256 {
		t.Fatalf("identical content under a different path name hashed the same - a rename would be invisible to this manifest")
	}
}

func TestHashMigrationSetAgainstRealTree(t *testing.T) {
	result, err := HashMigrationSet(repoRoot)
	if err != nil {
		t.Fatalf("HashMigrationSet: %v", err)
	}
	if result.FileCount < 100 {
		t.Fatalf("FileCount = %d, expected the repo's real migration count (100+)", result.FileCount)
	}
	if result.SHA256 == "" {
		t.Fatalf("empty hash")
	}
}

func TestHashStaticAssetsAgainstRealTree(t *testing.T) {
	result, err := HashStaticAssets(repoRoot)
	if err != nil {
		t.Fatalf("HashStaticAssets: %v", err)
	}
	if result.FileCount == 0 {
		t.Fatalf("found zero files under public/")
	}
}

func TestBuildManifestAgainstRealTree(t *testing.T) {
	ledger := loadRepoLedger(t)
	binPath := filepath.Join(t.TempDir(), "fake-binary")
	if err := os.WriteFile(binPath, []byte("not a real binary, just for the test"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact, err := HashFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Name = "erp-server"

	m, err := BuildManifest(BuildOptions{
		Root:      repoRoot,
		Commit:    "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		Branch:    "test",
		Builder:   "go-test",
		GoVersion: "go1.22.12",
		Ledger:    ledger,
		Artifacts: []ArtifactHash{artifact},
	})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	if m.Repository != "custom_erp" {
		t.Errorf("Repository = %q, want custom_erp", m.Repository)
	}
	if m.GoToolchain == "" {
		t.Errorf("GoToolchain is empty - go.mod's `go` directive was not parsed")
	}
	if len(m.Dependencies) == 0 {
		t.Errorf("Dependencies is empty - ledger entries were not carried through")
	}
	if m.MigrationSet.FileCount == 0 || m.StaticAssetSet.FileCount == 0 {
		t.Errorf("migration or static asset hash set is empty: %+v / %+v", m.MigrationSet, m.StaticAssetSet)
	}

	encoded, err := Encode(m)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(manifestPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if reloaded.Commit != m.Commit {
		t.Errorf("round-trip lost the commit: got %q, want %q", reloaded.Commit, m.Commit)
	}

	// VerifyArtifact must accept the artifact it was built from...
	ok, msg, err := VerifyArtifact(reloaded, "erp-server", binPath)
	if err != nil {
		t.Fatalf("VerifyArtifact: %v", err)
	}
	if !ok {
		t.Fatalf("VerifyArtifact rejected the exact file it was built from: %s", msg)
	}

	// ...and must refuse a tampered one. This is the mechanism 49.9.7 asks a
	// deployment step to run before trusting a binary.
	if err := os.WriteFile(binPath, []byte("tampered content, different from what was hashed"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, msg, err = VerifyArtifact(reloaded, "erp-server", binPath)
	if err != nil {
		t.Fatalf("VerifyArtifact: %v", err)
	}
	if ok {
		t.Fatalf("VerifyArtifact accepted a tampered artifact")
	}
	if msg == "" {
		t.Errorf("VerifyArtifact gave no explanation for the mismatch")
	}

	// And must refuse an artifact name it never recorded.
	ok, _, err = VerifyArtifact(reloaded, "no-such-artifact", binPath)
	if err != nil {
		t.Fatalf("VerifyArtifact: %v", err)
	}
	if ok {
		t.Fatalf("VerifyArtifact accepted an artifact name that was never in the manifest")
	}
}
