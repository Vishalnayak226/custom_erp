package server

import (
	"custom_erp/engines"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// handleControlPanel (Stage 65.5) is the platform operator's one page: every
// client with its status, address, modules, version, users, activity and
// errors, the release ledger and the last backup, plus the running build.
// Platform operators only, like every other cross-client endpoint (Stage 60.8).
func handleControlPanel(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	cp, err := engines.GetControlPanel()
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to build the control panel")
		return
	}
	_ = json.NewEncoder(w).Encode(struct {
		engines.ControlPanel
		Build map[string]string `json:"build"`
		// OperatorTenant is the client the operator is signed in to; the page
		// offers no pause or trial on that row (Stage 65.6).
		OperatorTenant   string `json:"operator_tenant"`
		DefaultTrialDays int    `json:"default_trial_days"`
	}{cp, map[string]string{
		"version":      currentAppVersion(),
		"git_commit":   gitCommit,
		"build_time":   buildTime,
		"release_date": releaseDate(),
	}, operatorTenant(r), engines.DefaultTrialDays})
}

// operatorTenant is the caller's own client id, "" read as "default" the same
// way TenantIsOperational reads it.
func operatorTenant(r *http.Request) string {
	if t := r.Header.Get("Resolved-Tenant-ID"); t != "" {
		return t
	}
	return "default"
}

// controlPanelTarget resolves {tenant} for the two Control Panel changes and
// refuses the operator's own client: pausing it, or letting a trial run out on
// it, would sign the operator out of the one screen that can undo it.
func controlPanelTarget(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !requirePlatformOperator(w, r) {
		return "", false
	}
	target, ok := platformTargetTenant(w, r)
	if !ok {
		return "", false
	}
	if target == operatorTenant(r) {
		writeAPIErrorGeneric(w, r, http.StatusConflict, "That is the client you are signed in to - pausing it or putting it on trial would lock you out")
		return "", false
	}
	return target, true
}

func controlPanelActor(r *http.Request) string {
	return operatorTenant(r) + ":" + r.Header.Get("Resolved-Username") + " (Control Panel)"
}

// POST /api/v1/platform/tenants/{tenant}/status {"action":"pause"|"resume","reason":"..."}
// (Stage 65.6). The same SuspendTenant/ResumeTenant `tenantctl suspend|resume`
// runs, so the evidence trail and the ~immediate cut-off are identical.
func handleControlPanelTenantStatus(w http.ResponseWriter, r *http.Request) {
	target, ok := controlPanelTarget(w, r)
	if !ok {
		return
	}
	var req struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "A reason is required - it is kept in the client's history")
		return
	}
	var err error
	switch req.Action {
	case "pause":
		err = engines.SuspendTenant(target, controlPanelActor(r), reason)
	case "resume":
		err = engines.ResumeTenant(target, controlPanelActor(r), reason)
	default:
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "action must be pause or resume")
		return
	}
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusConflict, err.Error())
		return
	}
	life, err := engines.GetTenantLifecycle(target)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"tenant_id": target, "status": life.Status})
}

// POST /api/v1/platform/tenants/{tenant}/trial {"days":30,"reason":"..."} (Stage 65.6).
// days > 0 starts the trial, or moves its end to that many days from now;
// days == 0 ends it because the client has paid.
func handleControlPanelTenantTrial(w http.ResponseWriter, r *http.Request) {
	target, ok := controlPanelTarget(w, r)
	if !ok {
		return
	}
	var req struct {
		Days   *int   `json:"days"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Days == nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "days is required (0 ends the trial)")
		return
	}
	endsAt, err := engines.SetTenantTrial(target, *req.Days, controlPanelActor(r), strings.TrimSpace(req.Reason))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(struct {
		TenantID    string     `json:"tenant_id"`
		TrialEndsAt *time.Time `json:"trial_ends_at"`
	}{target, endsAt})
}

// GET /api/v1/platform/tenants/{tenant}/attribute-library?industry=JEW
// (Stage 81): the industry attribute library as the operator sees it for one
// client - which attributes it would get, for variants or for PIM, and which it
// already has. The operator's own client is allowed: nothing here locks anyone out.
func handleAttributeLibrary(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	target, ok := platformTargetTenant(w, r)
	if !ok {
		return
	}
	view, err := engines.AttributeLibraryForTenant(target, r.URL.Query().Get("industry"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(view)
}

// POST /api/v1/platform/tenants/{tenant}/attribute-library
// {"attributes":[{"id":"JEW_PRIMARY_COLOUR","used_for":"Variant"}, ...]}
// adds the ticked attributes to that client (Stage 81). One it already has is
// skipped and left as the client keeps it.
func handleApplyAttributeLibrary(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	target, ok := platformTargetTenant(w, r)
	if !ok {
		return
	}
	var req struct {
		Attributes []engines.AttributeLibraryPick `json:"attributes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Attributes) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Tick at least one attribute to add")
		return
	}
	res, err := engines.ApplyAttributeLibrary(target, controlPanelActor(r), req.Attributes)
	if errors.Is(err, engines.ErrUnknownLibraryAttribute) {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
