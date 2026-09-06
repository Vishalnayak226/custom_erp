package docgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiffLeavesBytesAndTimestampsUntouched(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "existing.md")
	if err := os.WriteFile(path, []byte("hand edit\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1234567890, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{"existing.md": []byte("generated\n"), "absent/new.md": []byte("new")}
	if got := Diff(root, want); len(got) != 2 {
		t.Fatal(got)
	}
	body, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(body) != "hand edit\r\n" || !info.ModTime().Equal(stamp) {
		t.Fatal("check mutated existing file")
	}
	if _, err := os.Stat(filepath.Join(root, "absent")); !os.IsNotExist(err) {
		t.Fatal("check created a directory")
	}
	if got := Diff(root, map[string][]byte{"existing.md": []byte("hand edit\n")}); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestOutputPathsCannotEscapeRoot(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "C:/absolute", `..\escape`} {
		if _, err := Path(t.TempDir(), name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	root := t.TempDir()
	if err := Write(root, map[string][]byte{"valid.md": []byte("ok"), "../escape": nil}); err == nil {
		t.Fatal("unsafe write accepted")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("partially wrote invalid output set")
	}
	if got := Diff(root, map[string][]byte{"../escape": nil}); len(got) != 1 || !strings.Contains(got[0], "unsafe") {
		t.Fatal(got)
	}
}
