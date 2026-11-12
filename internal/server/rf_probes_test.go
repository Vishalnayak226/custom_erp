package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"testing"
)

// Stage 146.3.2 / 146.3.3: the RF menu asks whether to offer Piece codes and
// My next task. Switched off, the answer is 200 {"on": false}, not a 422 that
// the browser logs as an error on every RF screen.
func TestStage1463RFProbesAnswerOnFlag(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/wms/work-queue", apiMiddleware(handleWorkQueue))
	mux.HandleFunc("/api/v1/wms/piece-codes", apiMiddleware(handlePieceCodeMint))
	admin := seedGeneratedCaseUsers(t)[engines.RoleAdmin]
	ip := mfaTestIP()

	const tenant = "default"
	prevQueue := engines.GetSettingString(tenant, "wms.work_queue")
	prevCodes := engines.GetSettingString(tenant, "inventory.piece_codes_stores")
	t.Cleanup(func() {
		engines.SetSetting(tenant, "wms.work_queue", prevQueue, "test")
		engines.SetSetting(tenant, "inventory.piece_codes_stores", prevCodes, "test")
	})
	set := func(queue, codes string) {
		t.Helper()
		if err := engines.SetSetting(tenant, "wms.work_queue", queue, "test"); err != nil {
			t.Fatalf("set wms.work_queue: %v", err)
		}
		if err := engines.SetSetting(tenant, "inventory.piece_codes_stores", codes, "test"); err != nil {
			t.Fatalf("set inventory.piece_codes_stores: %v", err)
		}
	}
	probe := func(path string, body map[string]interface{}) (int, interface{}) {
		rec := doRequestFromIP(t, mux.ServeHTTP, http.MethodPost, path, admin, body, ip)
		out := map[string]interface{}{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out["on"]
	}

	set("false", "")
	if code, on := probe("/api/v1/wms/work-queue", map[string]interface{}{"action": "probe"}); code != http.StatusOK || on != false {
		t.Fatalf("work-queue probe while off: %d on=%v, want 200 on=false", code, on)
	}
	if code, on := probe("/api/v1/wms/piece-codes", map[string]interface{}{"probe": true}); code != http.StatusOK || on != false {
		t.Fatalf("piece-codes probe while off: %d on=%v, want 200 on=false", code, on)
	}
	// Off still refuses real work.
	if code, _ := probe("/api/v1/wms/work-queue", map[string]interface{}{"action": "next", "location_code": "HO"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("work-queue next while off: %d, want 422", code)
	}
	if code, _ := probe("/api/v1/wms/piece-codes", map[string]interface{}{"sku": "X", "location_code": "HO", "count": 1}); code != http.StatusUnprocessableEntity {
		t.Fatalf("piece-codes mint while off: %d, want 422", code)
	}

	set("true", "HO")
	if code, on := probe("/api/v1/wms/work-queue", map[string]interface{}{"action": "probe"}); code != http.StatusOK || on != true {
		t.Fatalf("work-queue probe while on: %d on=%v, want 200 on=true", code, on)
	}
	if code, on := probe("/api/v1/wms/piece-codes", map[string]interface{}{"probe": true}); code != http.StatusOK || on != true {
		t.Fatalf("piece-codes probe while on: %d on=%v, want 200 on=true", code, on)
	}
}
