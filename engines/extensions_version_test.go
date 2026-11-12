package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 47.11.7: a hook names the contract version it was written for; this
// build refuses one it does not serve or has retired, and every payload names
// its version.
func TestStage47HookAPIVersionContract(t *testing.T) {
	if v, err := CheckHookAPIVersion(0, "2026-10-25"); err != nil || v != HookAPIVersion {
		t.Fatalf("no version means the current one: %d %v", v, err)
	}
	if _, err := CheckHookAPIVersion(99, "2026-10-25"); err == nil || err.(*ValidationError).Code != "EXT-0292" {
		t.Fatalf("an unknown version must be refused with EXT-0292: %v", err)
	}
	// A replaced version is served through its sunset day, then refused.
	hookAPIVersions[7] = "2026-12-31"
	defer delete(hookAPIVersions, 7)
	if _, err := CheckHookAPIVersion(7, "2026-12-31"); err != nil {
		t.Fatalf("a version inside its deprecation window must still register: %v", err)
	}
	if _, err := CheckHookAPIVersion(7, "2027-01-01"); err == nil {
		t.Fatal("a version past its sunset must be refused")
	}
	if p := string(buildHookPayload("default", "document.after_save", "Item", "I1", nil)); !strings.Contains(p, `"api_version":1`) {
		t.Fatalf("the payload must name its version: %s", p)
	}

	db.InitDB(testConnStr())
	if _, _, err := RegisterExtensionHook("default", "document.after_save", "Item", "https://hooks.example.com/x", 1000, "test", 99); err == nil {
		t.Fatal("registering against version 99 must be refused")
	}
	// A hook registered before this change: its row takes the migration's default, version 1.
	var id string
	if err := db.DB.QueryRow(`INSERT INTO tenant_default.extension_hooks (hook_point, doctype, target_url, secret, timeout_ms, created_by)
		VALUES ('document.after_save', 'Item', 'https://hooks.example.com/x', 'test-secret', 1000, 'test') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("seed an older hook: %v", err)
	}
	defer DeleteExtensionHook("default", id)
	hooks, err := ListExtensionHooks("default")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hooks {
		if h.ID == id {
			if h.APIVersion != HookAPIVersion || h.Sunset != "" {
				t.Fatalf("listed hook: version %d sunset %q", h.APIVersion, h.Sunset)
			}
			return
		}
	}
	t.Fatal("the registered hook is not listed")
}
