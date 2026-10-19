package server

import (
	"strings"
	"testing"
)

// Stage 102.1.2: the camera is allowed on the store app only; every other
// path, the ERP screens and the API included, keeps it denied.
func TestPermissionsPolicyCameraOnlyOnStoreApp(t *testing.T) {
	for path, allowed := range map[string]bool{
		"/store":                 true,
		"/store/":                true,
		"/store/store.js":        true,
		"/":                      false,
		"/index.html":            false,
		"/storefront":            false,
		"/api/v1/store-app/item": false,
		"/wms/store/":            false,
	} {
		got := permissionsPolicyFor(path)
		if strings.Contains(got, "camera=(self)") != allowed {
			t.Errorf("%s: %q, camera allowed want %v", path, got, allowed)
		}
		if !strings.Contains(got, "microphone=()") || !strings.Contains(got, "geolocation=()") || !strings.Contains(got, "payment=()") {
			t.Errorf("%s: the other capabilities must stay denied: %q", path, got)
		}
	}
}
