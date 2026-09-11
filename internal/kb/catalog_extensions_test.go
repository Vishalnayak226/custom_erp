package kb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestErrorCodeGuardIncludesSourceOwnedExtensions(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"error_catalog_generated.go":  "package server\nvar catalog=map[string]Entry{\n\"GLOBAL-0001\": {Code: \"GLOBAL-0001\"},\n}\n",
		"error_catalog_extensions.go": "package server\nvar additions=[]Entry{{Code: \"GOODSR-0096\"}, {Code: \"GOODSR-0097\"}}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	codes, err := extractErrorCodes(filepath.Join(dir, "error_catalog_generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"GLOBAL-0001", "GOODSR-0096", "GOODSR-0097"} {
		if !codes[code] {
			t.Fatalf("live catalog code missing: %s", code)
		}
	}
	if codes["GOODSR-0999"] {
		t.Fatal("unknown code accepted")
	}
}
