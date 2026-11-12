package server

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 146.5.9: the signed public image route.

func TestPublicMediaRoute(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef-route-key"
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY", key)
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY_PREVIOUS", "")
	db.InitDB(testConnStr())

	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	tenantID, schema := "pmedia"+suffix, "tenant_pmedia"+suffix
	defer func() {
		db.DB.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
		db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, tenantID)
		engines.InvalidateTenantLifecycleCache(tenantID)
	}()
	if _, err := engines.ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	// A picture stored outside the media store (the legacy path ReadMediaFile
	// still reads), under an id with a slash and a space in it.
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "pic.jpg")
	if err := os.WriteFile(file, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	const mediaID = "PM/ITEM 1::main::abc123"
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'ProductMedia', $2, 'Active', 'system')`,
		mediaID, fmt.Sprintf(`{"item":"PM/ITEM 1","media_role":"Main","file_type":"image/jpeg","file_path":%q}`, file)); err != nil {
		t.Fatal(err)
	}
	if err := engines.SetSetting(tenantID, engines.SettingPublicMediaURLs, "true", "test"); err != nil {
		t.Fatal(err)
	}

	// The route as routes.go registers it.
	src, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `http.HandleFunc("GET /media/{tenant}/{id}/{sig}", handlePublicMedia)`) {
		t.Fatal("routes.go no longer registers the public media route as the test expects")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /media/{tenant}/{id}/{sig}", handlePublicMedia)
	get := func(path string, hdr map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "203.0.113.7:4000"
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}

	path := engines.PublicMediaPath(tenantID, mediaID)
	w := get(path, nil)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), buf.Bytes()) {
		t.Fatalf("valid address: %d, %d bytes (path %s)", w.Code, w.Body.Len(), path)
	}
	h := w.Header()
	if h.Get("Content-Type") != "image/jpeg" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Cache-Control") != "public, max-age=31536000, immutable" || h.Get("Content-Length") == "" {
		t.Errorf("headers: %v", h)
	}
	// A session or extension token riding along changes nothing, either way.
	if w := get(path, map[string]string{"Authorization": "Bearer junk", "X-Tenant-ID": "default"}); w.Code != 200 {
		t.Errorf("extra headers must be ignored, got %d", w.Code)
	}

	// Every refusal is the same plain 404.
	refuse := map[string]string{
		"tampered":     path[:len(path)-2] + "xx",
		"wrong tenant": strings.Replace(path, "/media/"+tenantID+"/", "/media/default/", 1),
		"unknown id":   engines.PublicMediaPath(tenantID, "NOPE"),
		"no sig":       path[:strings.LastIndex(path, "/")] + "/zzz",
	}
	var first *httptest.ResponseRecorder
	check := func(name, p string) {
		t.Helper()
		w := get(p, nil)
		if w.Code != 404 || strings.Contains(w.Header().Get("Content-Type"), "image") || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %v", name, w.Code, w.Header())
		}
		if first == nil {
			first = w
		} else if w.Body.String() != first.Body.String() {
			t.Errorf("%s: body differs from the other refusals: %q vs %q", name, w.Body.String(), first.Body.String())
		}
	}
	for name, p := range refuse {
		check(name, p)
	}
	if err := engines.SetSetting(tenantID, engines.SettingPublicMediaURLs, "false", "test"); err != nil {
		t.Fatal(err)
	}
	check("setting off", path)
	if err := engines.SetSetting(tenantID, engines.SettingPublicMediaURLs, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET deleted_at = now() WHERE id = $1`, mediaID); err != nil {
		t.Fatal(err)
	}
	check("deleted", path)
	if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET deleted_at = NULL WHERE id = $1`, mediaID); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PUBLIC_MEDIA_SIGNING_KEY", "")
	check("key unset", path)
}

func TestPublicMediaRouteUsesImageBucket(t *testing.T) {
	if c, n := rateLimitCategory("/media/t/id/sig", http.MethodGet); c != "image" || n != 600 {
		t.Fatalf("got %s %d", c, n)
	}
}
