package server

import (
	"net/http"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 146.5.6: a feed run takes the same file out as a template export, so it
// gets the same emergency freeze and DATA_EXPORT event.
func TestStage14656FeedRunIsABulkExport(t *testing.T) {
	if !IsBulkExportPattern("POST /api/v1/pim/feeds/{id}/run") {
		t.Error("a feed run must count as a bulk export")
	}
	if IsBulkExportPattern("POST /api/v1/pim/feeds") {
		t.Error("the feed list is not a bulk export")
	}

	db.InitDB(testConnStr())
	adminID, _, cleanup := seedSecurityOpsUser(t, "Super Admin")
	defer cleanup()
	engines.ResetLiveUserStateCache()
	token := engines.SignToken(adminID, adminID, "Super Admin", "default", "HO", 1)
	ran := false
	run := apiMiddleware(func(w http.ResponseWriter, r *http.Request) { ran = true })
	const path = "/api/v1/pim/feeds/F1/run"

	if err := engines.SetSetting("default", engines.SettingFreezeExports, "true", "test"); err != nil {
		t.Fatal(err)
	}
	defer engines.SetSetting("default", engines.SettingFreezeExports, "false", "test")
	if rec := doRequest(t, run, http.MethodPost, path, token, nil); rec.Code != http.StatusLocked {
		t.Fatalf("feed run under the freeze: %d, want 423", rec.Code)
	}
	if ran {
		t.Fatal("the handler ran under the freeze")
	}
}
