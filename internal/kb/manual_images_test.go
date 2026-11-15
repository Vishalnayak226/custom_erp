package kb

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Item 48.7.6: docs/guides/img holds only the current manual set. Every image there is shown
// by a living guide, SOP or KB article, every image a manual shows is there, and every one has a
// shot in the capture harness (docs/guides/capture-screenshots.js), so it is re-taken by the tool
// under its role, release and fixture tags instead of being edited by hand. An image no manual
// uses any more moves to docs/archive/legacy/screenshots-<date>/, as the 2026-08-01 set did.

var (
	manualImageRef = regexp.MustCompile(`\]\(([^)\s]*img/[A-Za-z0-9_-]+\.png)\)`)
	captureShotID  = regexp.MustCompile(`\{\s*id:\s*'([a-z0-9-]+)'`)
)

func TestManualImagesAreTheLivingSet(t *testing.T) {
	root := filepath.Join("..", "..")
	imgDir := filepath.Join(root, "docs", "guides", "img")

	// Images a living manual shows, resolved against the manual's own folder.
	shown := map[string][]string{} // image file name -> manuals that show it
	for _, dir := range []string{filepath.Join(root, "docs", "guides"), filepath.Join(root, "docs", "kb")} {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range manualImageRef.FindAllStringSubmatch(string(data), -1) {
				target := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(m[1])))
				if filepath.Dir(target) != filepath.Clean(imgDir) {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				shown[filepath.Base(target)] = append(shown[filepath.Base(target)], filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("read manuals under %s: %v", dir, err)
		}
	}
	if len(shown) == 0 {
		t.Fatal("no manual shows an image from docs/guides/img; the reference pattern no longer matches the guides")
	}

	src, err := os.ReadFile(filepath.Join(root, "docs", "guides", "capture-screenshots.js"))
	if err != nil {
		t.Fatalf("read capture harness: %v", err)
	}
	shots := map[string]bool{}
	for _, m := range captureShotID.FindAllStringSubmatch(string(src), -1) {
		shots[m[1]] = true
	}

	entries, err := os.ReadDir(imgDir)
	if err != nil {
		t.Fatalf("read %s: %v", imgDir, err)
	}
	present := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("docs/guides/img/%s: a folder in the living image set; old sets go to docs/archive/legacy/", e.Name())
			continue
		}
		present[e.Name()] = true
		if !strings.HasSuffix(e.Name(), ".png") {
			t.Errorf("docs/guides/img/%s: only the manual images belong here; records and manifests go to docs/archive/", e.Name())
			continue
		}
		if len(shown[e.Name()]) == 0 {
			t.Errorf("docs/guides/img/%s: no guide, SOP or KB article shows it; move it to docs/archive/legacy/screenshots-<date>/ and add a row to the archive's \"Where did it go?\" table", e.Name())
		}
	}

	names := make([]string, 0, len(shown))
	for name := range shown {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !present[name] {
			t.Errorf("%s: shows img/%s, which is not in docs/guides/img", strings.Join(shown[name], ", "), name)
		}
		if id := strings.TrimSuffix(name, ".png"); !shots[id] {
			t.Errorf("img/%s: no shot %q in docs/guides/capture-screenshots.js, so the harness cannot re-take it", name, id)
		}
	}
}
