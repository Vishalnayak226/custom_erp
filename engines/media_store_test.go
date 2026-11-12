package engines

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestStage75_4_1MediaStore is the server's media store: a write lands
// exactly where the code always wrote it, an exclusive write refuses an
// existing key, and no key can reach outside the store.
func TestStage75_4_1MediaStore(t *testing.T) {
	old := mediaStoreDir
	mediaStoreDir = t.TempDir()
	defer func() { mediaStoreDir = old }()

	if err := serverMedia.Write("abc_thumb.jpg", []byte("x"), MediaWrite{}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mediaStoreDir, "abc_thumb.jpg")); err != nil {
		t.Fatalf("the file must be where it always was: %v", err)
	}
	if b, err := serverMedia.Read("abc_thumb.jpg"); err != nil || string(b) != "x" {
		t.Fatalf("read back %q %v", b, err)
	}
	if err := serverMedia.Write("attachments/tenant_x/f.pdf", []byte("pdf"), MediaWrite{Exclusive: true, Private: true}); err != nil {
		t.Fatalf("private write: %v", err)
	}
	if err := serverMedia.Write("attachments/tenant_x/f.pdf", []byte("again"), MediaWrite{Exclusive: true, Private: true}); err == nil {
		t.Fatalf("an exclusive write must refuse an existing key")
	}
	f, err := serverMedia.Open("attachments/tenant_x/f.pdf")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if string(got) != "pdf" {
		t.Fatalf("open read %q", got)
	}
	for _, bad := range []string{"", "/etc/passwd", "../secret", "a/../../b", `a\b`, "a//b"} {
		if serverMedia.Write(bad, []byte("x"), MediaWrite{}) == nil || serverMedia.Exists(bad) {
			t.Fatalf("key %q must be refused", bad)
		}
	}
	if key := mediaKeyForStoredPath(filepath.Join(mediaStoreDir, "abc.jpg")); key != "abc.jpg" {
		t.Fatalf("a stored path inside the store maps to its key, got %q", key)
	}
	if key := mediaKeyForStoredPath(filepath.Join(t.TempDir(), "elsewhere.jpg")); key != "" {
		t.Fatalf("a path outside the store has no key, got %q", key)
	}
	if err := serverMedia.Remove("abc_thumb.jpg"); err != nil || serverMedia.Exists("abc_thumb.jpg") {
		t.Fatalf("remove: %v", err)
	}
}
