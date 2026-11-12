package engines

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/png"
	"strings"
	"testing"
)

// TestStage134UploadPolicy (49.8.1): each kind of upload accepts its own
// content only, judged from the bytes; zips are held to the archive limits;
// pictures to a pixel cap and no second file riding along.
func TestStage134UploadPolicy(t *testing.T) {
	pngOf := func(w, h int) []byte {
		var b bytes.Buffer
		_ = png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h)))
		return b.Bytes()
	}
	zipOf := func(files map[string][]byte, store bool) []byte {
		var b bytes.Buffer
		zw := zip.NewWriter(&b)
		for name, data := range files {
			method := zip.Deflate
			if store {
				method = zip.Store
			}
			w, _ := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
			_, _ = w.Write(data)
		}
		_ = zw.Close()
		return b.Bytes()
	}
	refused := func(kind UploadKind, content []byte, why string) {
		t.Helper()
		if err := CheckUpload(kind, content); err == nil {
			t.Errorf("%s: %s is refused", kind, why)
		}
	}
	accepted := func(kind UploadKind, content []byte, what string) {
		t.Helper()
		if err := CheckUpload(kind, content); err != nil {
			t.Errorf("%s: %s is accepted: %v", kind, what, err)
		}
	}

	accepted(UploadCSV, []byte("\xef\xbb\xbfsku,name\nA1,Ring\n"), "a CSV with Excel's BOM")
	refused(UploadCSV, []byte("<html><body>x</body></html>"), "an HTML page")
	refused(UploadCSV, []byte("s\x00k\x00u\x00"), "text with NUL bytes")
	refused(UploadCSV, pngOf(4, 4), "a picture")
	refused(UploadCSV, nil, "an empty file")

	workbook := zipOf(map[string][]byte{"[Content_Types].xml": []byte("<Types/>"), "xl/workbook.xml": []byte("<workbook/>")}, false)
	accepted(UploadWorkbook, workbook, "an Office package")
	refused(UploadWorkbook, zipOf(map[string][]byte{"a.txt": []byte("x")}, false), "a zip that is not a workbook")
	refused(UploadWorkbook, zipOf(map[string][]byte{"[Content_Types].xml": nil, "../../etc/x": []byte("x")}, false), "a member that climbs out")
	refused(UploadWorkbook, zipOf(map[string][]byte{"[Content_Types].xml": nil, "C:/x": []byte("x")}, false), "a member naming a drive")
	refused(UploadWorkbook, zipOf(map[string][]byte{"[Content_Types].xml": nil, "xl/sharedStrings.xml": make([]byte, 4<<20)}, false), "a zip bomb (4 MB of zeros packs to a few KB)")
	accepted(UploadWorkbook, zipOf(map[string][]byte{"[Content_Types].xml": nil, "xl/media/big.bin": make([]byte, 2<<20)}, true), "a large member stored uncompressed")

	accepted(UploadImage, pngOf(64, 64), "a PNG")
	refused(UploadImage, pngOf(8000, 7000), "a 56-megapixel picture")
	refused(UploadImage, append(pngOf(8, 8), []byte("<script>alert(1)</script>")...), "a picture with a script appended")
	refused(UploadImage, append(pngOf(8, 8), zipOf(map[string][]byte{"x.html": []byte("<b>")}, false)...), "a picture with a zip appended")
	refused(UploadImage, []byte("GIF89a not really"), "a GIF header on nothing")
	refused(UploadImage, []byte("%PDF-1.7\n"), "a PDF")

	accepted(UploadImageZip, zipOf(map[string][]byte{"A1__main.png": pngOf(4, 4)}, false), "a zip of pictures")
	refused(UploadImageZip, zipOf(map[string][]byte{"inner.zip": zipOf(map[string][]byte{"a.png": pngOf(4, 4)}, false)}, false), "a zip inside the zip")

	// Every CSV import reads through readCSVRecordsContext, so it carries the rule.
	if _, err := readCSVRecordsContext(context.Background(), "default", bytes.NewReader(pngOf(4, 4))); err == nil || !strings.Contains(err.Error(), "CSV") {
		t.Errorf("a CSV import refuses a picture: %v", err)
	}
	if _, err := ReadXLSX(zipOf(map[string][]byte{"[Content_Types].xml": nil, "xl/workbook.xml": make([]byte, 4<<20)}, false)); err == nil {
		t.Error("ReadXLSX refuses a zip bomb before reading a part")
	}
}
