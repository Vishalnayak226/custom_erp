package engines

import (
	"archive/zip"
	"bytes"
	"custom_erp/db"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"strings"
	"testing"
)

// TestMain redirects media storage to a scratch %TEMP% directory for this
// package's test run - see mediaStoreDir's own comment (engines/pim_media.go)
// for why: this repo checkout sits under Documents\, and Windows Controlled
// Folder Access blocks a freshly-built test binary from creating a new
// directory there. Production never sets this; the deployed server's
// working directory is not under Documents\.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "erp_pim_dam_test_media_store")
	if err == nil {
		mediaStoreDir = dir
	}
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir) // os.Exit below skips deferred cleanup, so this runs before it instead
	}
	os.Exit(code)
}

// Stage 36.6 - DAM depth tests. Pure filename/resize logic needs no
// database; SaveMediaFile/GetMediaTransform/SearchMedia do (ProductMedia is
// a real doctype with real fields), so those reuse the same
// pimInsertDoc/testConnStr fixtures every other PIM stage's tests do, scoped
// to a uniquely-prefixed id set and cleaned up regardless of outcome.

func tinyTestJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("failed to encode test jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestParseMediaFilename(t *testing.T) {
	cases := []struct {
		filename, wantItem, wantRole string
	}{
		{"SKU-001__gallery.jpg", "SKU-001", "Gallery"},
		{"SKU-001__main-image.png", "SKU-001", "Main Image"},
		{"SKU-001__video-other.jpg", "SKU-001", "Video/Other"},
		{"SKU-001.jpg", "SKU-001", "Gallery"},
		{"SKU-001__not-a-real-role.jpg", "SKU-001", "Gallery"},
		{".jpg", "", "Gallery"},
	}
	for _, c := range cases {
		item, role := parseMediaFilename(c.filename)
		if item != c.wantItem || role != c.wantRole {
			t.Errorf("parseMediaFilename(%q) = (%q, %q), want (%q, %q)", c.filename, item, role, c.wantItem, c.wantRole)
		}
	}
}

func TestResizeToMaxDimAndCrop(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 800, 400))
	resized := resizeToMaxDim(src, 200)
	if resized.Bounds().Dx() != 200 || resized.Bounds().Dy() != 100 {
		t.Fatalf("expected 200x100, got %dx%d", resized.Bounds().Dx(), resized.Bounds().Dy())
	}

	cropped := centerCropSquareRGBA(src)
	if cropped.Bounds().Dx() != cropped.Bounds().Dy() {
		t.Fatalf("expected a square crop, got %dx%d", cropped.Bounds().Dx(), cropped.Bounds().Dy())
	}
	if cropped.Bounds().Dx() != 400 {
		t.Fatalf("expected the crop side to equal the shorter dimension (400), got %d", cropped.Bounds().Dx())
	}
}

func TestGetMediaTransform(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'PIMDAMTEST%'")
	}
	cleanup()
	defer cleanup()

	asset, err := SaveMediaFile("default", tinyTestJPEG(t, 300, 300), "PIMDAMTEST-ITEM.jpg", "PIMDAMTEST-ITEM", "Gallery", "system", "", "")
	if err != nil {
		t.Fatalf("unexpected error saving fixture media: %v", err)
	}
	// The stored file/transform caches live under the TestMain-scoped
	// scratch mediaStoreDir, cleaned up as one directory when the whole
	// test binary exits - no per-asset file cleanup needed here.

	t.Run("refuses an unknown preset", func(t *testing.T) {
		if _, _, err := GetMediaTransform("default", asset.ID, "gigantic"); err == nil {
			t.Fatal("expected an error for an unknown preset")
		}
	})
	t.Run("generates and caches a known preset", func(t *testing.T) {
		path, fileType, err := GetMediaTransform("default", asset.ID, "small")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fileType != "image/jpeg" {
			t.Fatalf("expected image/jpeg, got %q", fileType)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected the transform to be cached on disk: %v", statErr)
		}
		defer os.Remove(path)
		// Second call should hit the cache and return the identical path.
		path2, _, err := GetMediaTransform("default", asset.ID, "small")
		if err != nil {
			t.Fatalf("unexpected error on second call: %v", err)
		}
		if path2 != path {
			t.Fatalf("expected the cached path to be reused, got %q vs %q", path2, path)
		}
	})
}

func TestBulkUploadAndDownloadMediaZip(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'PIMDAMTEST%'")
	}
	cleanup()
	defer cleanup()

	pimInsertDoc(t, schema, "PIMDAMTEST-A", "Item", "Active", map[string]interface{}{
		"id": "PIMDAMTEST-A", "code": "PIMDAMTEST-A", "name": "DAM Test Item A",
	})

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	writeEntry := func(name string, data []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	writeEntry("PIMDAMTEST-A__main-image.jpg", tinyTestJPEG(t, 100, 100))
	writeEntry("no-item-code-prefix-missing/.jpg", []byte{}) // becomes "" after Base/parse -> refused
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	outcomes, err := BulkUploadMediaZip("default", zipBuf.Bytes(), "system")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected 2 outcomes, got %d: %+v", len(outcomes), outcomes)
	}
	var mediaID string
	for _, o := range outcomes {
		if o.Filename == "PIMDAMTEST-A__main-image.jpg" {
			if o.Error != "" {
				t.Fatalf("expected the well-named entry to succeed, got error: %v", o.Error)
			}
			if o.ItemCode != "PIMDAMTEST-A" || o.MediaRole != "Main Image" {
				t.Fatalf("unexpected association: %+v", o)
			}
			mediaID = o.MediaID
		}
	}
	if mediaID == "" {
		t.Fatal("expected the well-named entry to produce a media id")
	}

	zipOut, err := BulkDownloadMediaZip("default", []string{"PIMDAMTEST-A"})
	if err != nil {
		t.Fatalf("unexpected error downloading: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(zipOut), int64(len(zipOut)))
	if err != nil {
		t.Fatalf("download did not produce a valid zip: %v", err)
	}
	if len(reader.File) != 1 || !strings.HasPrefix(reader.File[0].Name, "PIMDAMTEST-A__main-image") {
		t.Fatalf("unexpected zip contents: %+v", reader.File)
	}
}

func TestSearchMediaAndTagUpdate(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'PIMDAMTEST%'")
	}
	cleanup()
	defer cleanup()

	asset, err := SaveMediaFile("default", tinyTestJPEG(t, 50, 50), "PIMDAMTEST-TAG.jpg", "PIMDAMTEST-TAG", "Gallery", "system", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tags := "winter-2026, hero-shot"
	if err := UpdateMediaMetadata("default", asset.ID, "", "", &tags); err != nil {
		t.Fatalf("unexpected error setting tags: %v", err)
	}

	t.Run("finds by tag", func(t *testing.T) {
		results, err := SearchMedia("default", PIMMediaSearchFilters{Tag: "winter-2026"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 || results[0].ID != asset.ID {
			t.Fatalf("expected exactly this asset, got: %+v", results)
		}
	})
	t.Run("a nil tags pointer leaves tags untouched", func(t *testing.T) {
		if err := UpdateMediaMetadata("default", asset.ID, "new alt", "", nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		results, err := SearchMedia("default", PIMMediaSearchFilters{Tag: "hero-shot"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected tags to survive an alt-text-only update, got: %+v", results)
		}
	})
	t.Run("finds by role and item", func(t *testing.T) {
		results, err := SearchMedia("default", PIMMediaSearchFilters{Item: "PIMDAMTEST-TAG", MediaRole: "Gallery"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected exactly one match, got: %+v", results)
		}
	})
	t.Run("no match for an unrelated tag", func(t *testing.T) {
		results, err := SearchMedia("default", PIMMediaSearchFilters{Tag: "no-such-tag-anywhere"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("expected no matches, got: %+v", results)
		}
	})
}
