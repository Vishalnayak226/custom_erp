package engines

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	_ "image/gif" // DecodeConfig for GIF uploads
	"net/http"
	"path"
	"strings"
)

// Stage 134.4 (49.8.1): the one rule every upload passes before anything
// reads it, whatever screen it came from. What a file is comes from its
// bytes, never its name; each kind of upload accepts only its own types.
//
//   - CSV: plain text, no NUL bytes, not HTML or XML.
//   - Workbook (.xlsx): a zip that is an Office package.
//   - Image: JPEG, PNG, GIF or WebP whose header reads as one, at most 50
//     megapixels (a small file can decode to gigabytes), and nothing that
//     makes it a second kind of file too - markup or a zip directory inside
//     a picture is a polyglot and refused.
//   - Image zip (bulk media): a zip of pictures, nothing nested.
//
// Every zip: at most 10,000 members, no member path that climbs out (".."),
// starts at a root or names a drive, at most 256 MB unpacked in all, and no
// member that unpacks to more than 200 times its packed size once it is
// past 1 MB - the shape of a zip bomb.
//
// The request's own size is capped before this by the request envelope
// (request_envelope.go) and each handler's MaxBytesReader; this caps the
// file itself per kind. Stored files keep server-chosen names (attachments,
// media) and are served with nosniff.

// UploadKind is what an upload is for.
type UploadKind string

const (
	UploadCSV      UploadKind = "CSV file"
	UploadWorkbook UploadKind = "Excel workbook (.xlsx)"
	UploadImage    UploadKind = "picture"
	UploadImageZip UploadKind = "zip of pictures"
)

const (
	uploadZipMaxMembers  = 10000
	uploadZipMaxUnpacked = 256 << 20
	uploadZipMaxRatio    = 200
	uploadImageMaxPixels = 50_000_000
)

func uploadRefused(kind UploadKind, why string) error {
	return &ValidationError{Code: "GLOBAL-0002", SubFor: "file", Message: fmt.Sprintf("This is not an acceptable %s: %s", kind, why)}
}

// CheckUpload refuses content that is not what kind expects.
func CheckUpload(kind UploadKind, content []byte) error {
	if len(content) == 0 {
		return uploadRefused(kind, "the file is empty")
	}
	sniffed := http.DetectContentType(content)
	if i := strings.IndexByte(sniffed, ';'); i >= 0 {
		sniffed = strings.TrimSpace(sniffed[:i])
	}
	switch kind {
	case UploadCSV:
		head := content
		if len(head) > 64<<10 {
			head = head[:64<<10]
		}
		if sniffed != "text/plain" || bytes.IndexByte(head, 0) >= 0 {
			return uploadRefused(kind, "it is not plain text - save it from Excel as CSV")
		}
		return nil
	case UploadWorkbook:
		if sniffed != "application/zip" {
			return uploadRefused(kind, "save it from Excel as .xlsx")
		}
		zr, err := checkUploadZip(kind, content)
		if err != nil {
			return err
		}
		for _, f := range zr.File {
			if f.Name == "[Content_Types].xml" {
				return nil
			}
		}
		return uploadRefused(kind, "it is a zip, not an Excel workbook")
	case UploadImage:
		return checkUploadImage(kind, sniffed, content)
	case UploadImageZip:
		if sniffed != "application/zip" {
			return uploadRefused(kind, "it is not a .zip file")
		}
		zr, err := checkUploadZip(kind, content)
		if err != nil {
			return err
		}
		for _, f := range zr.File {
			if strings.HasSuffix(f.Name, "/") {
				continue
			}
			if strings.EqualFold(path.Ext(f.Name), ".zip") {
				return uploadRefused(kind, fmt.Sprintf("%s is a zip inside the zip", f.Name))
			}
		}
		return nil
	}
	return uploadRefused(kind, "unknown upload")
}

// checkUploadZip opens a zip and applies the archive limits.
func checkUploadZip(kind UploadKind, content []byte) (*zip.Reader, error) {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, uploadRefused(kind, "the zip cannot be read")
	}
	if len(zr.File) > uploadZipMaxMembers {
		return nil, uploadRefused(kind, fmt.Sprintf("it holds %d files; at most %d", len(zr.File), uploadZipMaxMembers))
	}
	var total uint64
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.HasPrefix(name, "/") || strings.Contains(name, ":") || strings.Contains(name, "\x00") {
			return nil, uploadRefused(kind, fmt.Sprintf("%q is not a plain path", f.Name))
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				return nil, uploadRefused(kind, fmt.Sprintf("%q climbs out of the zip", f.Name))
			}
		}
		total += f.UncompressedSize64
		if total > uploadZipMaxUnpacked {
			return nil, uploadRefused(kind, "it unpacks to more than 256 MB")
		}
		if f.UncompressedSize64 > 1<<20 && f.UncompressedSize64 > uploadZipMaxRatio*(f.CompressedSize64+1) {
			return nil, uploadRefused(kind, fmt.Sprintf("%s unpacks to %d times its size", f.Name, f.UncompressedSize64/(f.CompressedSize64+1)))
		}
	}
	return zr, nil
}

// checkUploadImage accepts one real picture and nothing riding along.
func checkUploadImage(kind UploadKind, sniffed string, content []byte) error {
	switch sniffed {
	case "image/jpeg", "image/png", "image/gif":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil {
			return uploadRefused(kind, "it looks like a picture but cannot be read as one")
		}
		if int64(cfg.Width)*int64(cfg.Height) > uploadImageMaxPixels {
			return uploadRefused(kind, fmt.Sprintf("it is %d × %d pixels; at most 50 megapixels", cfg.Width, cfg.Height))
		}
	case "image/webp":
		if len(content) < 30 || string(content[8:12]) != "WEBP" {
			return uploadRefused(kind, "it looks like a WebP picture but cannot be read as one")
		}
	default:
		return uploadRefused(kind, "it is not a JPEG, PNG, GIF or WebP picture")
	}
	// A polyglot puts its second file where a viewer does not look: just
	// after the header, or appended at the end (a zip's directory is always
	// in its last 64 KB). Only those ends are searched - the compressed
	// picture data between them is random bytes, where a short marker turns
	// up by chance.
	head, tail := content, content
	if len(head) > 2048 {
		head = head[:2048]
	}
	if len(tail) > 66<<10 {
		tail = tail[len(tail)-66<<10:]
	}
	ends := bytes.ToLower(append(append([]byte{}, head...), tail[max(0, len(tail)-4096):]...))
	for _, marker := range []string{"<script", "<html", "<svg", "<?php", "<!doctype"} {
		if bytes.Contains(ends, []byte(marker)) {
			return uploadRefused(kind, "it carries web page code inside the picture")
		}
	}
	if bytes.Contains(tail, []byte("PK\x05\x06")) {
		return uploadRefused(kind, "it carries a zip inside the picture")
	}
	return nil
}
