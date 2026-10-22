package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Stage 102.1.4 through the real middleware: a PIN sign-in on a registered
// phone needs no session, works at the phone's store only, cannot manage
// phones or switch store, and ends the moment the phone is removed.
func TestStoreAppPINSessionThroughMiddleware(t *testing.T) {
	db.InitDB(testConnStr())
	mgr, cleanupMgr := seedStage47User(t, "Store Manager", "HO")
	defer cleanupMgr()
	cash, cleanupCash := seedStage47User(t, "Cashier", "HO")
	defer cleanupCash()
	engines.InvalidateLiveUserState("default", mgr)
	engines.InvalidateLiveUserState("default", cash)

	phone, token, err := engines.RegisterStoreAppDevice("default", mgr, mgr, "Store Manager", "HO", "T102 phone")
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.store_app_devices WHERE id = $1`, phone.ID)
	h, _ := bcrypt.GenerateFromPassword([]byte("3816"), bcrypt.MinCost)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.store_app_pins (user_id, pin_hash) VALUES ($1, $2)`, cash, string(h)); err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.store_app_pins WHERE user_id = $1`, cash)

	login := apiMiddleware(handleStoreAppPINLogin)
	if rec := doRequestFromIP(t, login, http.MethodPost, "/api/v1/store-app/pin/login", "", map[string]string{"device_token": token, "username": cash, "pin": "3817"}, "10.102.0.1"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong PIN = %d %s", rec.Code, rec.Body.String())
	}
	rec := doRequestFromIP(t, login, http.MethodPost, "/api/v1/store-app/pin/login", "", map[string]string{"device_token": token, "username": cash, "pin": "3816"}, "10.102.0.2")
	if rec.Code != http.StatusOK {
		t.Fatalf("PIN sign-in = %d %s", rec.Code, rec.Body.String())
	}
	var s engines.StoreAppPINSession
	_ = json.Unmarshal(rec.Body.Bytes(), &s)
	if s.Token == "" || s.Store != "HO" {
		t.Fatalf("session = %+v", s)
	}

	probe := apiMiddleware(handleStoreAppPIN)
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/store-app/pin", s.Token, nil); rec.Code != http.StatusOK {
		t.Fatalf("PIN session call = %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, apiMiddleware(handleSwitchWorkingStore), http.MethodPost, "/api/v1/me/working-store", s.Token, map[string]string{"store": "HO"}); rec.Code != http.StatusForbidden {
		t.Fatalf("store switch on a shared phone = %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, apiMiddleware(handleStoreAppDevices), http.MethodGet, "/api/v1/store-app/devices", s.Token, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("managing phones from a PIN session = %d %s", rec.Code, rec.Body.String())
	}

	if err := engines.RemoveStoreAppDevice("default", mgr, mgr, "Store Manager", phone.ID); err != nil {
		t.Fatal(err)
	}
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/store-app/pin", s.Token, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("session after the phone was removed = %d %s", rec.Code, rec.Body.String())
	}
	check := apiMiddleware(handleStoreAppDeviceCheck)
	if rec := doRequestFromIP(t, check, http.MethodPost, "/api/v1/store-app/device/check", "", map[string]string{"device_token": token}, "10.102.0.3"); rec.Code != http.StatusNotFound {
		t.Fatalf("removed phone check = %d %s", rec.Code, rec.Body.String())
	}
}
