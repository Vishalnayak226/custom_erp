package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Stage 102 (2026-10-22): the rest of the store app's calls. Each is a thin
// wrapper over an engines/store_app_*.go or web_push.go function; the caller's
// store is always the session's working store (Resolved-Location), never one
// named in the request. What the phone saves - a goods receipt check, a
// transfer receipt, a held bill, a transfer request - goes through the
// existing endpoints the desktop uses, not through here.

func storeAppCaller(r *http.Request) (tenantID, userID, username, role, store string) {
	return r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Username"),
		r.Header.Get("Resolved-Role"), r.Header.Get("Resolved-Location")
}

func storeAppJSON(w http.ResponseWriter, v interface{}) { _ = json.NewEncoder(w).Encode(v) }

func storeAppDecode(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request")
		return false
	}
	return true
}

// ------------------------------------------------------------- push (102.1.3)

// handleStoreAppPushKey - GET /api/v1/store-app/push/key: the key a phone
// subscribes with.
func handleStoreAppPushKey(w http.ResponseWriter, r *http.Request) {
	key, err := engines.WebPushPublicKey(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Push alerts are not available on this server right now")
		return
	}
	storeAppJSON(w, map[string]string{"public_key": key})
}

// handleStoreAppPush - GET ?endpoint= reads this phone's alerts; POST turns
// them on or changes them (the browser's PushSubscription plus approvals /
// low_stock).
func handleStoreAppPush(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, _, _, store := storeAppCaller(r)
	if r.Method == http.MethodGet {
		sub, err := engines.GetWebPushSubscription(tenantID, userID, r.URL.Query().Get("endpoint"))
		if err != nil {
			writeEngineError(w, r, err, http.StatusInternalServerError)
			return
		}
		storeAppJSON(w, sub)
		return
	}
	var in engines.WebPushSubscribeInput
	if !storeAppDecode(w, r, &in) {
		return
	}
	sub, err := engines.SaveWebPushSubscription(tenantID, userID, store, r.UserAgent(), in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, sub)
}

// handleStoreAppPushOff - POST /api/v1/store-app/push/off {endpoint}.
func handleStoreAppPushOff(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, _, _, _ := storeAppCaller(r)
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if !storeAppDecode(w, r, &in) {
		return
	}
	if err := engines.DeleteWebPushSubscription(tenantID, userID, in.Endpoint); err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	storeAppJSON(w, map[string]bool{"subscribed": false})
}

// ------------------------------------------------- shared phone (102.1.4)

// handleStoreAppDevices - GET lists the store phones a manager looks after;
// POST {label} registers this phone to the working store and returns its
// token once.
func handleStoreAppDevices(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, username, role, store := storeAppCaller(r)
	if r.Header.Get("Resolved-Store-Device") != "" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Sign in with your password to manage store phones")
		return
	}
	if r.Method == http.MethodGet {
		devices, err := engines.ListStoreAppDevices(tenantID, userID, role)
		if err != nil {
			writeEngineError(w, r, err, http.StatusForbidden)
			return
		}
		storeAppJSON(w, map[string]interface{}{"devices": devices})
		return
	}
	var in struct {
		Label string `json:"label"`
	}
	if !storeAppDecode(w, r, &in) {
		return
	}
	device, token, err := engines.RegisterStoreAppDevice(tenantID, userID, username, role, store, in.Label)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, map[string]interface{}{"device": device, "device_token": token})
}

// handleStoreAppDeviceRemove - POST /api/v1/store-app/devices/{id}/remove.
func handleStoreAppDeviceRemove(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, username, role, _ := storeAppCaller(r)
	if r.Header.Get("Resolved-Store-Device") != "" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Sign in with your password to manage store phones")
		return
	}
	if err := engines.RemoveStoreAppDevice(tenantID, userID, username, role, r.PathValue("id")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, map[string]string{"id": r.PathValue("id"), "status": "Removed"})
}

// handleStoreAppDeviceCheck - POST /api/v1/store-app/device/check
// {device_token}: public (the sign-in screen has no session). Says whether
// this phone is still registered, and to which store - nothing else.
func handleStoreAppDeviceCheck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceToken string `json:"device_token"`
	}
	if !storeAppDecode(w, r, &in) {
		return
	}
	device, err := engines.StoreAppDeviceForToken(r.Header.Get("Resolved-Tenant-ID"), in.DeviceToken)
	if errors.Is(err, engines.ErrStoreAppDeviceUnknown) {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "This phone is no longer registered to a store. A store manager can register it again.")
		return
	}
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not check this phone")
		return
	}
	storeAppJSON(w, map[string]string{"store": device.Store, "store_name": device.StoreName, "label": device.Label})
}

// handleStoreAppPINLogin - POST /api/v1/store-app/pin/login {device_token,
// username, pin}: public, in the login rate-limit bucket (the path ends in
// /login), with the client checks handleLogin makes first.
func handleStoreAppPINLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceToken string `json:"device_token"`
		Username    string `json:"username"`
		PIN         string `json:"pin"`
	}
	if !storeAppDecode(w, r, &in) {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if expired, _ := engines.IsSandboxExpired(tenantID); expired {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "Wrong name or PIN.")
		return
	}
	if operational, err := engines.TenantIsOperational(tenantID); err != nil && !errors.Is(err, engines.ErrTenantNotRegistered) {
		writeAPIErrorGeneric(w, r, http.StatusServiceUnavailable, "Sign-in is temporarily unavailable")
		return
	} else if err != nil || !operational {
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "Wrong name or PIN.")
		return
	}
	session, err := engines.StoreAppPINSignIn(tenantID, in.DeviceToken, in.Username, in.PIN)
	switch {
	case errors.Is(err, engines.ErrStoreAppDeviceUnknown):
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "This phone is no longer registered to a store. Sign in with your password, or ask a store manager to register it again.")
		return
	case errors.Is(err, engines.ErrStoreAppPINSignIn):
		engines.LogAuditEventCtx(r.Context(), tenantID, strings.TrimSpace(in.Username), "LOGIN", "PIN_FAILED", "Store app PIN sign-in refused")
		writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "Wrong name or PIN.")
		return
	case err != nil:
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not sign in")
		return
	}
	storeAppJSON(w, session)
}

// handleStoreAppPIN - GET says whether the caller has a PIN; POST {password,
// pin} sets it (the password is asked again).
func handleStoreAppPIN(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, _, role, _ := storeAppCaller(r)
	if r.Method == http.MethodGet {
		status, err := engines.StoreAppPINStatus(tenantID, userID, role)
		if err != nil {
			writeEngineError(w, r, err, http.StatusInternalServerError)
			return
		}
		storeAppJSON(w, status)
		return
	}
	var in struct {
		Password string `json:"password"`
		PIN      string `json:"pin"`
	}
	if !storeAppDecode(w, r, &in) {
		return
	}
	if err := engines.SetStoreAppPIN(tenantID, userID, role, in.Password, in.PIN); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, map[string]bool{"set": true})
}

// handleStoreAppPINRemove - POST /api/v1/store-app/pin/remove.
func handleStoreAppPINRemove(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, _, _, _ := storeAppCaller(r)
	if err := engines.ClearStoreAppPIN(tenantID, userID); err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	storeAppJSON(w, map[string]bool{"set": false})
}

// ------------------------------------------------------- manager (102.2.1)

// handleStoreAppToday - GET /api/v1/store-app/today?store=all|<code>.
func handleStoreAppToday(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, _, role, store := storeAppCaller(r)
	if !engines.StoreAppManagerRole(role) {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Today's sales are for the store manager")
		return
	}
	today, err := engines.StoreAppTodayForUser(tenantID, userID, store, r.URL.Query().Get("store"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, today)
}

// handleStoreAppLowStock - GET /api/v1/store-app/low-stock.
func handleStoreAppLowStock(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, _, _, store := storeAppCaller(r)
	items, err := engines.StoreAppLowStock(tenantID, userID, store)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, map[string]interface{}{"items": items})
}

// --------------------------------------------------------- stock (102.3)

// handleStoreAppReceiveList - GET /api/v1/store-app/receive. Transfers are
// Inventory (a core module); goods receipts are listed only with Procurement.
func handleStoreAppReceiveList(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, _, _, store := storeAppCaller(r)
	docs, err := engines.StoreAppReceiveList(tenantID, userID, store)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	if enabled, _ := engines.IsModuleEnabled(tenantID, "procurement"); !enabled {
		kept := docs[:0]
		for _, d := range docs {
			if d.Kind != "grn" {
				kept = append(kept, d)
			}
		}
		docs = kept
	}
	storeAppJSON(w, map[string]interface{}{"documents": docs})
}

// handleStoreAppReceiveDocument - GET /api/v1/store-app/receive/{kind}/{id}.
func handleStoreAppReceiveDocument(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, _, role, store := storeAppCaller(r)
	kind := r.PathValue("kind")
	module := map[string]string{"grn": "procurement", "transfer": "inventory"}[kind]
	if module != "" {
		if enabled, _ := engines.IsModuleEnabled(tenantID, module); !enabled {
			writeAPIError(w, r, "SAAS-0191", "")
			return
		}
	}
	doc, err := engines.StoreAppReceiveDocument(tenantID, userID, role, store, kind, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, doc)
}

// handleStoreAppCounts - GET lists the store's open full counts; POST sends a
// phone's count (spot or full).
func handleStoreAppCounts(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, username, role, store := storeAppCaller(r)
	if r.Method == http.MethodGet {
		sessions, err := engines.StoreAppCountSessions(tenantID, userID, store)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		storeAppJSON(w, map[string]interface{}{"full_counts": sessions})
		return
	}
	var in engines.StoreAppCountInput
	if !storeAppDecode(w, r, &in) {
		return
	}
	res, err := engines.StoreAppSubmitCount(tenantID, userID, username, role, store, in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, res)
}

// handleStoreAppSources - GET /api/v1/store-app/sources: where a transfer to
// this store could come from.
func handleStoreAppSources(w http.ResponseWriter, r *http.Request) {
	tenantID, _, _, _, store := storeAppCaller(r)
	sources, err := engines.StoreAppStockSources(tenantID, store)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	storeAppJSON(w, map[string]interface{}{"sources": sources})
}

// handleStoreAppLabels - GET lists the store's recent label requests; POST
// {lines:[{code, copies, batch_no}], note} asks the store's computer to print.
func handleStoreAppLabels(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, username, _, store := storeAppCaller(r)
	if r.Method == http.MethodGet {
		reqs, err := engines.StoreAppLabelRequests(tenantID, userID, store)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		storeAppJSON(w, map[string]interface{}{"requests": reqs})
		return
	}
	var in struct {
		Lines []engines.StoreAppLabelLine `json:"lines"`
		Note  string                      `json:"note"`
	}
	if !storeAppDecode(w, r, &in) {
		return
	}
	req, err := engines.StoreAppRequestLabels(tenantID, userID, username, store, in.Lines, in.Note)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, req)
}

// ------------------------------------------------- selling floor (102.4.3)

// handleStoreAppOrder - POST /api/v1/store-app/orders: a home-delivery order
// from another store or the warehouse.
func handleStoreAppOrder(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, username, _, store := storeAppCaller(r)
	var in engines.StoreAppOrderInput
	if !storeAppDecode(w, r, &in) {
		return
	}
	res, err := engines.StoreAppCreateOrder(tenantID, userID, username, store, in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, res)
}
