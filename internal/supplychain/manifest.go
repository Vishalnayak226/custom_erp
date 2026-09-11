package supplychain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ManifestSchemaVersion is bumped whenever ReleaseManifest's shape changes.
const ManifestSchemaVersion = 1

// ReleaseManifest is the 49.9.9 source-to-production trace record for one
// build: which commit, built by what, with which dependencies, producing
// which artifacts - in one file an operator (or an automated deploy step)
// can read back to answer "what exactly produced this running binary".
//
// It is deliberately not a signature itself. Signing (49.9.7) is a detached
// step this manifest is designed to feed: whatever holds the real signing key
// signs the manifest's own bytes (or this file's SHA-256, recorded in
// SelfDigest by the caller after writing it) with cosign, minisign, GPG or an
// HSM-backed key - none of which this repository can provision. See
// docs/security/secure-development-lifecycle.md for exactly where that plugs
// in.
type ReleaseManifest struct {
	SchemaVersion int    `json:"schema_version"`
	GeneratedAt   string `json:"generated_at"` // RFC3339 UTC
	Repository    string `json:"repository"`   // module path, doubles as the source identity
	Commit        string `json:"commit"`
	CommitShort   string `json:"commit_short,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Builder       string `json:"builder"` // "github-actions:<workflow>/<run_id>" or "local:<hostname>"
	GoVersion     string `json:"go_version"`
	GoToolchain   string `json:"go_toolchain"` // go.mod's `go` directive - the version that MUST have built this
	Dependencies  []LedgerEntry `json:"dependencies"`

	Artifacts      []ArtifactHash `json:"artifacts"`
	MigrationSet   HashResult     `json:"migration_set"`
	StaticAssetSet HashResult     `json:"static_asset_set"`

	// Notes records anything a human should know when reading this manifest
	// back - most importantly, any nondeterminism the build could not remove
	// (49.9.6's "or document the exact nondeterminism").
	Notes []string `json:"notes,omitempty"`
}

// ArtifactHash is one named, hashed build output - typically the erp-server
// binary, but the same shape covers an installer or container export.
type ArtifactHash struct {
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

// HashResult is a deterministic content hash of a whole file set - migrations
// or static assets - not just one file.
type HashResult struct {
	SHA256    string `json:"sha256"`
	FileCount int    `json:"file_count"`
}

// HashFile returns the SHA-256 of one file's content.
func HashFile(path string) (ArtifactHash, error) {
	f, err := os.Open(path)
	if err != nil {
		return ArtifactHash{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return ArtifactHash{}, fmt.Errorf("hash %s: %w", path, err)
	}
	return ArtifactHash{Path: path, SHA256: hex.EncodeToString(h.Sum(nil)), SizeBytes: n}, nil
}

// HashFileSet hashes every file at the given paths, in a fixed (sorted) order,
// into one running digest, and returns that digest alongside how many files
// went into it. Sorting first is what makes the result independent of
// directory-walk order across operating systems - the same requirement
// SchemaVersion-sorted JSON serves elsewhere in this codebase's generators.
func HashFileSet(paths []string) (HashResult, error) {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)

	h := sha256.New()
	for _, p := range sorted {
		// The relative path is part of the hash input, not just the bytes -
		// a file renamed with identical content is a different migration
		// set, and should hash differently.
		fmt.Fprintf(h, "\x00path:%s\n", filepath.ToSlash(p))
		f, err := os.Open(p)
		if err != nil {
			return HashResult{}, fmt.Errorf("open %s: %w", p, err)
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return HashResult{}, fmt.Errorf("hash %s: %w", p, err)
		}
	}
	return HashResult{SHA256: hex.EncodeToString(h.Sum(nil)), FileCount: len(sorted)}, nil
}

// HashMigrationSet hashes every db/*.sql file, in the identical
// filename-sorted order .github/workflows/ci.yml's "Apply database schema"
// step applies them in (`ls db/*.sql | sort`) - so this hash changes exactly
// when the applied schema would change, and not when an unrelated file in
// db/ (a .go helper, a test) does.
func HashMigrationSet(root string) (HashResult, error) {
	matches, err := filepath.Glob(filepath.Join(root, "db", "*.sql"))
	if err != nil {
		return HashResult{}, fmt.Errorf("glob db/*.sql: %w", err)
	}
	if len(matches) == 0 {
		return HashResult{}, fmt.Errorf("no db/*.sql files found under %s", root)
	}
	return HashFileSet(matches)
}

// HashStaticAssets hashes every file under public/ (recursively), which is
// what actually ships to a browser - the SPA shell, JS, CSS and any bundled
// media - in filename-sorted order.
func HashStaticAssets(root string) (HashResult, error) {
	base := filepath.Join(root, "public")
	var files []string
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return HashResult{}, fmt.Errorf("walk %s: %w", base, err)
	}
	if len(files) == 0 {
		return HashResult{}, fmt.Errorf("no files found under %s", base)
	}
	return HashFileSet(files)
}

// BuildOptions parameterises BuildManifest. Root, Commit and GoVersion are
// required; everything else is best-effort metadata.
type BuildOptions struct {
	Root      string
	Commit    string
	Branch    string
	Builder   string
	GoVersion string // runtime.Version() of the compiler that ran this tool
	Ledger    *Ledger
	Artifacts []ArtifactHash // pre-hashed; BuildManifest does not read files itself for these
	Notes     []string
}

// BuildManifest assembles a ReleaseManifest from the repository at
// opts.Root: parses go.mod for the module path and toolchain version, hashes
// db/*.sql and public/, and carries opts.Ledger's dependency rows and
// opts.Artifacts through unchanged.
func BuildManifest(opts BuildOptions) (*ReleaseManifest, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("BuildOptions.Root is required")
	}
	if opts.Commit == "" {
		return nil, fmt.Errorf("BuildOptions.Commit is required")
	}
	modulePath, goToolchain, _, err := ParseGoMod(opts.Root)
	if err != nil {
		return nil, err
	}
	migrations, err := HashMigrationSet(opts.Root)
	if err != nil {
		return nil, err
	}
	static, err := HashStaticAssets(opts.Root)
	if err != nil {
		return nil, err
	}
	var deps []LedgerEntry
	if opts.Ledger != nil {
		deps = append(deps, opts.Ledger.Entries...)
		sort.Slice(deps, func(i, j int) bool { return deps[i].Name < deps[j].Name })
	}

	commitShort := opts.Commit
	if len(commitShort) > 12 {
		commitShort = commitShort[:12]
	}

	return &ReleaseManifest{
		SchemaVersion:  ManifestSchemaVersion,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		Repository:     modulePath,
		Commit:         opts.Commit,
		CommitShort:    commitShort,
		Branch:         opts.Branch,
		Builder:        opts.Builder,
		GoVersion:      opts.GoVersion,
		GoToolchain:    goToolchain,
		Dependencies:   deps,
		Artifacts:      opts.Artifacts,
		MigrationSet:   migrations,
		StaticAssetSet: static,
		Notes:          opts.Notes,
	}, nil
}

// Encode serialises a manifest deterministically (stable field order via the
// struct tags above, indented for human review).
func Encode(m *ReleaseManifest) ([]byte, error) {
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// LoadManifest reads and parses a previously generated manifest.
func LoadManifest(path string) (*ReleaseManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var m ReleaseManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	return &m, nil
}

// VerifyArtifact recomputes the SHA-256 of the file at path and compares it
// to the manifest's recorded hash for the artifact named name. This is the
// mechanism 49.9.7 asks a deployment step to run before trusting a binary:
// refuse to proceed when ok is false.
//
//	ok, msg, err := supplychain.VerifyArtifact(manifest, "erp-server", "/opt/erp/erp-server.new")
//	if err != nil || !ok { abort }
func VerifyArtifact(m *ReleaseManifest, name, path string) (ok bool, message string, err error) {
	var want *ArtifactHash
	for i := range m.Artifacts {
		if m.Artifacts[i].Name == name {
			want = &m.Artifacts[i]
			break
		}
	}
	if want == nil {
		return false, fmt.Sprintf("manifest has no recorded artifact named %q", name), nil
	}
	got, err := HashFile(path)
	if err != nil {
		return false, "", err
	}
	if got.SHA256 != want.SHA256 {
		return false, fmt.Sprintf("checksum mismatch for %q: manifest says %s, %s is %s", name, want.SHA256, path, got.SHA256), nil
	}
	if want.SizeBytes != 0 && got.SizeBytes != want.SizeBytes {
		return false, fmt.Sprintf("size mismatch for %q: manifest says %d bytes, %s is %d bytes", name, want.SizeBytes, path, got.SizeBytes), nil
	}
	return true, fmt.Sprintf("%q matches the manifest (sha256 %s)", name, got.SHA256), nil
}
