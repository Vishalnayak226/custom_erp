package engines

import (
	"archive/zip"
	"bytes"
	"net"
	"strings"
	"testing"
)

// Stage 134.8 (49.10.2): fuzz tests at the shared parsers this stage added or
// leans on. Each runs its seed corpus in every `go test`; `go test -fuzz
// FuzzX -fuzztime 30s ./engines/` searches further. The properties: never
// panic, never accept what the rule exists to refuse.

func FuzzCheckUpload(f *testing.F) {
	var z bytes.Buffer
	zw := zip.NewWriter(&z)
	w, _ := zw.Create("[Content_Types].xml")
	_, _ = w.Write([]byte("<Types/>"))
	_ = zw.Close()
	for _, seed := range [][]byte{nil, []byte("sku,name\nA,B\n"), z.Bytes(), []byte("\x89PNG\r\n\x1a\n"), []byte("GIF89a"), []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), []byte("PK\x03\x04"), []byte("<html>")} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		for _, kind := range []UploadKind{UploadCSV, UploadWorkbook, UploadImage, UploadImageZip} {
			err := CheckUpload(kind, content)
			if err == nil && kind == UploadCSV && bytes.IndexByte(content[:min(len(content), 64<<10)], 0) >= 0 {
				t.Fatalf("a CSV with a NUL byte passed")
			}
			if err == nil && kind == UploadImage && bytes.Contains(bytes.ToLower(content[:min(len(content), 2048)]), []byte("<script")) {
				t.Fatalf("a picture with a script at its head passed")
			}
		}
		_, _ = ReadXLSX(content) // must not panic on anything
	})
}

func FuzzPublicAddress(f *testing.F) {
	for _, seed := range []string{"8.8.8.8", "127.0.0.1", "::1", "::ffff:10.0.0.1", "64:ff9b::a00:1", "2002:a00:1::", "169.254.169.254", "fe80::1", "100.64.0.1"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		ip := net.ParseIP(s)
		if ip == nil {
			if publicAddress(nil) {
				t.Fatal("nil is not public")
			}
			return
		}
		public := publicAddress(ip)
		if public && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) {
			t.Fatalf("%s passed as public", s)
		}
		if v4 := ip.To4(); v4 != nil && public != publicAddress(v4) {
			t.Fatalf("%s and its IPv4 form disagree", s)
		}
	})
}

func FuzzVerifyWebhookHMACAny(f *testing.F) {
	f.Add([]byte(`{"id":1}`), "abc", "sha256=", "hex", "s1")
	f.Add([]byte(""), "", "", "base64", "")
	f.Fuzz(func(t *testing.T, body []byte, sig, prefix, enc, secret string) {
		ok := VerifyWebhookHMACAny(body, sig, prefix, enc, secret)
		if ok && (secret == "" || strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sig), prefix)) == "") {
			t.Fatal("a blank secret or signature verified")
		}
	})
}

func FuzzBrandAccent(f *testing.F) {
	for _, seed := range []string{"", "#0f766e", "#ffffff", "#GGGGGG", "red", "#12345"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, v string) {
		if validateBrandAccent(v) == nil && strings.TrimSpace(v) != "" {
			if !brandAccentPattern.MatchString(strings.TrimSpace(v)) || contrastWithWhite(strings.TrimSpace(v)) < 4.5 {
				t.Fatalf("%q accepted", v)
			}
		}
	})
}

func FuzzInboundEventID(f *testing.F) {
	f.Add("evt-1", []byte("x"))
	f.Add("", []byte(""))
	f.Fuzz(func(t *testing.T, id string, body []byte) {
		got := inboundEventID(id, body)
		if got == "" || len(got) > 200 {
			t.Fatalf("event id %q from %q", got, id)
		}
	})
}
