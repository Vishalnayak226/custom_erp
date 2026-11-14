package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// Stage 49.11.3 / 49.11.7 / 49.12.3 / 49.12.4: the client administrator's
// Security pane (Settings -> Monitoring -> Security Events). Everything here is scoped to the
// caller's own client by the resolved tenant; no route reads another client.

// handleSecurityPosture: detections, evidence health and risky grants.
func handleSecurityPosture(w http.ResponseWriter, r *http.Request) {
	p, err := engines.GetSecurityPosture(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(p)
}

// handleSecurityTrail: every evidence row for one correlation id.
func handleSecurityTrail(w http.ResponseWriter, r *http.Request) {
	trail, err := engines.InvestigationTrail(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("correlation_id"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if trail == nil {
		trail = []engines.TrailEntry{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"entries": trail})
}

// handleRunSecurityDetections runs every rule now (a drill, or after a fix).
func handleRunSecurityDetections(w http.ResponseWriter, r *http.Request) {
	fired, err := engines.RunSecurityDetections(r.Header.Get("Resolved-Tenant-ID"), time.Now())
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	if fired == nil {
		fired = []engines.SecurityDetection{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"fired": fired})
}

// confirmOwnPassword is the step-up every emergency control asks for: the
// administrator's own password, so a session left open on a desk cannot
// sign everyone out or stop exports.
func confirmOwnPassword(w http.ResponseWriter, r *http.Request, password string) bool {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not check your password")
		return false
	}
	var hash string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT password_hash FROM %s.users WHERE id::text = $1`, schema),
		r.Header.Get("Resolved-User-ID")).Scan(&hash); err != nil ||
		bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Your password is not correct - the control was not used")
		return false
	}
	return true
}

type emergencyRequest struct {
	ID       string `json:"id"`
	On       bool   `json:"on"`
	Reason   string `json:"reason"`
	Password string `json:"password"`
}

func readEmergencyRequest(w http.ResponseWriter, r *http.Request) (emergencyRequest, bool) {
	var req emergencyRequest
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return req, false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Send a reason and your password")
		return req, false
	}
	if !confirmOwnPassword(w, r, req.Password) {
		return req, false
	}
	return req, true
}

func writeEmergencyError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, engines.ErrEmergencyReasonRequired) {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Write a reason (at least 5 characters) - it goes into the audit log")
		return
	}
	writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
}

// handleSignOutUserEverywhere ends every session of one user.
func handleSignOutUserEverywhere(w http.ResponseWriter, r *http.Request) {
	req, ok := readEmergencyRequest(w, r)
	if !ok {
		return
	}
	if forbidSupportUserTarget(w, r, req.ID) {
		return
	}
	name, err := engines.SignOutUserEverywhere(r.Header.Get("Resolved-Tenant-ID"), req.ID, r.Header.Get("Resolved-Username"), req.Reason)
	if err != nil {
		writeEmergencyError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "detail": name + " is signed out everywhere."})
}

// handleSignOutAllUsers ends every session but the caller's own.
func handleSignOutAllUsers(w http.ResponseWriter, r *http.Request) {
	req, ok := readEmergencyRequest(w, r)
	if !ok {
		return
	}
	n, err := engines.SignOutAllUsers(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Username"), req.Reason)
	if err != nil {
		writeEmergencyError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "users": n,
		"detail": fmt.Sprintf("%d user(s) signed out everywhere. You stay signed in.", n)})
}

// handleFreezeExports turns the client's export freeze on or off.
func handleFreezeExports(w http.ResponseWriter, r *http.Request) {
	req, ok := readEmergencyRequest(w, r)
	if !ok {
		return
	}
	if err := engines.SetExportFreeze(r.Header.Get("Resolved-Tenant-ID"), req.On, r.Header.Get("Resolved-Username"), req.Reason); err != nil {
		writeEmergencyError(w, r, err)
		return
	}
	detail := "Exports are frozen for this business."
	if !req.On {
		detail = "Exports are open again."
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "detail": detail})
}

// handleForensicExport downloads the evidence pack for ?from=&to= (dates,
// YYYY-MM-DD, the end day included), encrypted with the passphrase in the
// X-Evidence-Passphrase header (49.12.4; URI-encoded, never in the address).
func handleForensicExport(w http.ResponseWriter, r *http.Request) {
	from, err1 := time.Parse("2006-01-02", r.URL.Query().Get("from"))
	to, err2 := time.Parse("2006-01-02", r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Choose a From and a To date")
		return
	}
	pass, err := url.PathUnescape(r.Header.Get("X-Evidence-Passphrase"))
	if err != nil || utf8.RuneCountInString(pass) < engines.EvidencePassphraseMinLen {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, engines.ErrEvidencePassphrase.Error())
		return
	}
	pack, _, err := engines.BuildForensicPack(r.Header.Get("Resolved-Tenant-ID"), from, to.AddDate(0, 0, 1), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	sealed, err := engines.EncryptEvidencePack(pack, pass)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not encrypt the evidence pack")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", attachmentDisposition("evidence-"+from.Format("20060102")+"-to-"+to.Format("20060102")+".zip.enc"))
	_, _ = w.Write(sealed)
}

// handleListPrivilegedSessions lists the recent sign-ins of full-access users
// (Stage 49.11.7), newest first, each with its state.
func handleListPrivilegedSessions(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	rows, err := engines.ListPrivilegedSessions(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not read the sessions")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"sessions": rows})
}

// handleEndPrivilegedSession ends one session; its token is refused from the
// next request. Needs a reason and the administrator's own password.
func handleEndPrivilegedSession(w http.ResponseWriter, r *http.Request) {
	req, ok := readEmergencyRequest(w, r)
	if !ok {
		return
	}
	if err := engines.EndPrivilegedSession(r.Header.Get("Resolved-Tenant-ID"), req.ID, r.Header.Get("Resolved-Username"), req.Reason); err != nil {
		writeEmergencyError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "detail": "Session ended."})
}

// handleFreezeIntegrations is the emergency integration freeze (49.12.3a):
// one switch for this business's outbound connectors, couriers, webhooks and
// hooks, and a refusal of its inbound webhooks, until turned off.
func handleFreezeIntegrations(w http.ResponseWriter, r *http.Request) {
	req, ok := readEmergencyRequest(w, r)
	if !ok {
		return
	}
	if err := engines.SetIntegrationFreeze(r.Header.Get("Resolved-Tenant-ID"), req.On, r.Header.Get("Resolved-Username"), req.Reason); err != nil {
		writeEmergencyError(w, r, err)
		return
	}
	detail := "Integrations are frozen for this business."
	if !req.On {
		detail = "Integrations are running again."
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "detail": detail})
}

// inboundWebhookGate turns away an inbound webhook while the client's
// integrations are frozen (49.12.3a): 423 with the stable marker header
// X-Refused: INTEGRATIONS_FROZEN, and one audit row. Sits inside apiMiddleware
// so the client is already known.
func inboundWebhookGate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("Resolved-Tenant-ID")
		if tenantID != "" && engines.GetSettingBool(tenantID, engines.SettingFreezeIntegrations) {
			engines.LogAuditEventCtx(r.Context(), tenantID, "webhook", "INBOUND_WEBHOOK", "REFUSED_FROZEN", r.URL.Path)
			w.Header().Set("X-Refused", "INTEGRATIONS_FROZEN")
			writeAPIErrorGeneric(w, r, http.StatusLocked, "Integrations are frozen for this business by an administrator. The sender should retry later.")
			return
		}
		next(w, r)
	}
}

// handleListAccessReviews: every Active user with the state of their last
// access review (49.2.1).
func handleListAccessReviews(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	list, err := engines.ListAccessReviews(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(list)
}

// handleRecordAccessReview confirms or revokes one user's access (49.2.1).
func handleRecordAccessReview(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	var req struct {
		UserID   string `json:"user_id"`
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'user_id' and 'decision' are required")
		return
	}
	if forbidSupportUserTarget(w, r, req.UserID) {
		return
	}
	if err := engines.RecordAccessReview(r.Context(), r.Header.Get("Resolved-Tenant-ID"), req.UserID, req.Decision, req.Note,
		r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Username")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
