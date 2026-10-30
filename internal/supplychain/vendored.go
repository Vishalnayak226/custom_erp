package supplychain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Stage 47.18.4 (second slice): third-party JavaScript and CSS vendored
// under public/ is a dependency like any other - it ships to every browser
// with no go.sum to pin it. Each such file has a vendored_asset ledger entry
// naming its version, licence, size and purpose, with a checksum of the
// bytes on disk, so a file that changes, arrives or disappears without the
// ledger moving fails a test.

// VendoredAsset is one third-party file found under public/.
type VendoredAsset struct {
	Path   string // repository-relative, forward slashes: public/qrcode.min.js
	Bytes  int64
	SHA256 string
}

// vendoredHeader catches a third-party licence banner in the head of a file
// that is not minified-named: "@license", "Copyright (c)", "Licensed under".
var vendoredHeader = regexp.MustCompile(`(?i)@license|copyright \(c\)|licensed under`)

// looksVendored says whether a public/ file is third-party: a minified
// build (*.min.js, *.min.css), anything under a vendor/ or lib/ directory,
// or a file whose first 4 KB carries a licence banner.
func looksVendored(rel string, head []byte) bool {
	lower := strings.ToLower(rel)
	if strings.HasSuffix(lower, ".min.js") || strings.HasSuffix(lower, ".min.css") {
		return true
	}
	if strings.Contains(lower, "/vendor/") || strings.Contains(lower, "/lib/") {
		return true
	}
	return vendoredHeader.Match(head)
}

// ParseVendoredAssets walks root/public and returns every file that looks
// third-party, path-sorted, each with its size and SHA-256.
func ParseVendoredAssets(root string) ([]VendoredAsset, error) {
	base := filepath.Join(root, "public")
	var out []VendoredAsset
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		head := data
		if len(head) > 4096 {
			head = head[:4096]
		}
		if !looksVendored(rel, head) {
			return nil
		}
		sum := sha256.Sum256(data)
		out = append(out, VendoredAsset{Path: rel, Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:])})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", base, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// vendoredChecksum is how a vendored_asset entry records its file: the
// SHA-256 of the bytes on disk.
func vendoredChecksum(a VendoredAsset) string { return "sha256:" + a.SHA256 }

// ValidateVendoredCoverage checks that every vendored file under public/ has
// a vendored_asset entry named by its path, with the licence, version, size
// and checksum of the file as it is, and that no vendored_asset entry names
// a file that is gone.
func ValidateVendoredCoverage(ledger *Ledger, assets []VendoredAsset) []string {
	var problems []string
	present := map[string]bool{}
	for _, a := range assets {
		present[a.Path] = true
		e := ledger.findEntry(a.Path)
		switch {
		case e == nil:
			problems = append(problems, fmt.Sprintf("%s has no ledger entry - a vendored file needs a vendored_asset entry (name, version, license, bytes, purpose, checksum sha256:...)", a.Path))
			continue
		case e.Type != ComponentVendoredAsset:
			problems = append(problems, fmt.Sprintf("%s: ledger type %q, want %q", a.Path, e.Type, ComponentVendoredAsset))
		}
		if e.License == "" {
			problems = append(problems, fmt.Sprintf("%s: empty license", a.Path))
		}
		if e.Bytes != a.Bytes {
			problems = append(problems, fmt.Sprintf("%s: ledger says %d bytes, the file is %d", a.Path, e.Bytes, a.Bytes))
		}
		if want := vendoredChecksum(a); e.Checksum != want {
			problems = append(problems, fmt.Sprintf("%s: ledger checksum %q, the file is %q - a changed vendored file needs its entry (version, checksum, bytes) brought up to date", a.Path, e.Checksum, want))
		}
	}
	for _, e := range ledger.EntriesByType(ComponentVendoredAsset) {
		if !present[e.Name] {
			problems = append(problems, fmt.Sprintf("%s: ledger names a vendored file that is not under public/ any more - remove the entry", e.Name))
		}
	}
	return problems
}
