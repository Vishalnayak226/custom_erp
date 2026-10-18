package engines

import (
	"bytes"
	"custom_erp/db"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"
)

// Stage 55.15b - document attachments (expense receipts). Storage goes to the
// scratch media store pim_dam_test.go's TestMain sets up for this package.

func attachmentCode(err error) string {
	if verr, ok := err.(*ValidationError); ok {
		return verr.Code
	}
	return ""
}

func tinyTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestAttachmentTypeIsSniffedFromBytes(t *testing.T) {
	webp := append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 32)...)
	pdf := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n")
	for _, tc := range []struct {
		name    string
		content []byte
		want    string
	}{
		{"jpeg", tinyTestJPEG(t, 4, 4), "image/jpeg"},
		{"png", tinyTestPNG(t, 4, 4), "image/png"},
		{"webp", webp, "image/webp"},
		{"pdf", pdf, "application/pdf"},
	} {
		got, _, err := sniffAttachmentType(tc.content)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q err %v, want %q", tc.name, got, err, tc.want)
		}
	}
	for _, tc := range []struct {
		name    string
		content []byte
	}{
		{"text named .jpg", []byte("this is a plain text note, not a receipt photo")},
		{"html", []byte("<!DOCTYPE html><html><script>alert(1)</script></html>")},
		{"gif (a picture, but not an accepted one)", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")},
		{"jpeg magic over garbage", append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("not really a jpeg at all")...)},
		{"windows executable", append([]byte("MZ\x90\x00"), make([]byte, 64)...)},
	} {
		if _, _, err := sniffAttachmentType(tc.content); attachmentCode(err) != "EXP-0541" {
			t.Errorf("%s: want EXP-0541, got %v", tc.name, err)
		}
	}

	for _, tc := range []struct{ in, ext, want string }{
		{"bill.jpg", ".jpg", "bill.jpg"},
		{"Bill.JPEG", ".jpg", "Bill.JPEG"},
		{`C:\Users\me\Desktop\taxi.png`, ".png", "taxi.png"},
		{"../../etc/passwd", ".pdf", "passwd.pdf"},
		{"note.jpg", ".png", "note.jpg.png"},
		{"a\"b\r\n.pdf", ".pdf", "ab.pdf"},
		{"", ".webp", "receipt.webp"},
	} {
		if got := cleanAttachmentName(tc.in, tc.ext); got != tc.want {
			t.Errorf("cleanAttachmentName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := cleanAttachmentName(strings.Repeat("x", 400)+".pdf", ".pdf"); len(got) > 200 || !strings.HasSuffix(got, ".pdf") {
		t.Errorf("long name not trimmed to 200 with its extension: %d %q", len(got), got[len(got)-8:])
	}
}

func TestAttachmentLimitsDuplicatesAndRemoveRules(t *testing.T) {
	db.InitDB(testConnStr())
	const claimID = "__TEST_ATT_EXP_55_15B__"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.document_attachments WHERE document_id = $1`, claimID)
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, claimID)
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'ExpenseClaim', '{"employee_id":"__ATT_EMP__","location":"__ATT_LOC__","amount":100}', 'Draft', 'admin')`, claimID); err != nil {
		t.Fatalf("seed claim: %v", err)
	}
	setStatus := func(s string) {
		if _, err := db.DB.Exec(`UPDATE tenant_default.documents SET status = $2 WHERE id = $1`, claimID, s); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := AddAttachment("default", "Item", claimID, "x.jpg", tinyTestJPEG(t, 2, 2), "att-owner"); attachmentCode(err) != "GLOBAL-0011" {
		t.Fatalf("doctype outside the allow-list: want GLOBAL-0011, got %v", err)
	}
	if _, err := AddAttachment("default", "ExpenseClaim", claimID, "receipt.jpg", []byte("plain text, renamed to .jpg"), "att-owner"); attachmentCode(err) != "EXP-0541" {
		t.Fatalf("text renamed .jpg: want EXP-0541, got %v", err)
	}
	oversize := append([]byte("%PDF-1.4\n"), make([]byte, MaxAttachmentBytes)...)
	if _, err := AddAttachment("default", "ExpenseClaim", claimID, "big.pdf", oversize, "att-owner"); attachmentCode(err) != "EXP-0542" {
		t.Fatalf("file over 5 MB: want EXP-0542, got %v", err)
	}

	first := tinyTestJPEG(t, 3, 3)
	att, err := AddAttachment("default", "ExpenseClaim", claimID, "taxi.jpg", first, "att-owner")
	if err != nil {
		t.Fatalf("add receipt: %v", err)
	}
	if att.ContentType != "image/jpeg" || att.SizeBytes != int64(len(first)) || att.SHA256 != checksumOf(first) || att.FileName != "taxi.jpg" {
		t.Fatalf("stored record wrong: %+v", att)
	}
	stored, _ := GetAttachment("default", att.ID)
	if !attachmentKeyPattern.MatchString(stored.storageKey) || strings.Contains(stored.storageKey, att.SHA256[:16]) || strings.Contains(stored.storageKey, claimID) {
		t.Fatalf("storage name should be random, not derived from content or document: %q", stored.storageKey)
	}
	if _, err := AddAttachment("default", "ExpenseClaim", claimID, "taxi-again.jpg", first, "att-owner"); attachmentCode(err) != "EXP-0544" {
		t.Fatalf("same bytes twice: want EXP-0544, got %v", err)
	}

	// Round trip: what comes back is exactly what went in.
	meta, f, err := OpenAttachment("default", att.ID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if !bytes.Equal(got, first) || meta.DocumentID != claimID {
		t.Fatalf("round trip changed the file (%d bytes vs %d)", len(got), len(first))
	}

	for i := 0; i < MaxAttachmentsPerDocument-1; i++ {
		if _, err := AddAttachment("default", "ExpenseClaim", claimID, "r.png", tinyTestPNG(t, 5+i, 5), "att-owner"); err != nil {
			t.Fatalf("add file %d: %v", i+2, err)
		}
	}
	if _, err := AddAttachment("default", "ExpenseClaim", claimID, "sixth.png", tinyTestPNG(t, 40, 40), "att-owner"); attachmentCode(err) != "EXP-0543" {
		t.Fatalf("sixth file: want EXP-0543, got %v", err)
	}
	list, err := ListAttachments("default", "ExpenseClaim", claimID)
	if err != nil || len(list) != MaxAttachmentsPerDocument {
		t.Fatalf("list: %d files, err %v", len(list), err)
	}
	counts, err := CountAttachments("default", "ExpenseClaim", []string{claimID, "__no_such_claim__"})
	if err != nil || counts[claimID] != MaxAttachmentsPerDocument || counts["__no_such_claim__"] != 0 {
		t.Fatalf("counts %v err %v", counts, err)
	}

	// Remove: only the uploader, only while Draft/Rejected; Super Admin always.
	if _, err := RemoveAttachment("default", att.ID, "someone-else", false); attachmentCode(err) != "GLOBAL-0011" {
		t.Fatalf("remove by another user: want GLOBAL-0011, got %v", err)
	}
	if _, err := RemoveAttachment("default", att.ID, "att-owner", false); err != nil {
		t.Fatalf("uploader removes in Draft: %v", err)
	}
	if _, err := GetAttachment("default", att.ID); attachmentCode(err) != "GLOBAL-0004" {
		t.Fatalf("removed file should no longer be served, got %v", err)
	}
	var kept int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.document_attachments WHERE id = $1 AND deleted_at IS NOT NULL AND deleted_by = 'att-owner'`, att.ID).Scan(&kept)
	if kept != 1 {
		t.Fatal("removal must be a soft delete that keeps the row and who removed it")
	}
	if _, err := AddAttachment("default", "ExpenseClaim", claimID, "taxi.jpg", first, "att-owner"); err != nil {
		t.Fatalf("a removed file may be attached again: %v", err)
	}

	setStatus("Pending Approval")
	list, _ = ListAttachments("default", "ExpenseClaim", claimID)
	if _, err := AddAttachment("default", "ExpenseClaim", claimID, "late.png", tinyTestPNG(t, 41, 41), "att-owner"); attachmentCode(err) != "EXP-0545" {
		t.Fatalf("add after submit: want EXP-0545, got %v", err)
	}
	if _, err := RemoveAttachment("default", list[0].ID, list[0].UploadedBy, false); attachmentCode(err) != "EXP-0545" {
		t.Fatalf("uploader removes after submit: want EXP-0545, got %v", err)
	}
	if _, err := RemoveAttachment("default", list[0].ID, "admin", true); err != nil {
		t.Fatalf("Super Admin removes after submit: %v", err)
	}

	setStatus("Rejected")
	if _, err := AddAttachment("default", "ExpenseClaim", claimID, "fix.png", tinyTestPNG(t, 42, 42), "att-owner"); err != nil {
		t.Fatalf("add while Rejected (being corrected): %v", err)
	}
	if _, err := GetAttachment("default", "not-a-uuid'; DROP TABLE x;--"); attachmentCode(err) != "GLOBAL-0004" {
		t.Fatalf("malformed id: want GLOBAL-0004, got %v", err)
	}
}
