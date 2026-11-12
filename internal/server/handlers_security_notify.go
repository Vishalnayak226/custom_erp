package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Stage 49.11.6 (security event export) and 49.12.5 (notification decision
// workflow): the client administrator's Security pane. One client only, by the
// resolved tenant. The export destination and the decisions are written by a
// person; nothing here decides that anyone must be told.

func securityAdmin(w http.ResponseWriter, r *http.Request) bool {
	return requireHRAdmin(w, r, r.Header.Get("Resolved-Role"))
}

func readJSONBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(v); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The request could not be read")
		return false
	}
	return true
}

func handleSecurityExportStatus(w http.ResponseWriter, r *http.Request) {
	if !securityAdmin(w, r) {
		return
	}
	s, err := engines.GetSecurityExportStatus(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(s)
}

func handleSecurityExportSave(w http.ResponseWriter, r *http.Request) {
	if !securityAdmin(w, r) {
		return
	}
	var req struct {
		URL      string `json:"url"`
		Secret   string `json:"secret"`
		Enabled  bool   `json:"enabled"`
		Password string `json:"password"`
	}
	if !readJSONBody(w, r, &req) || !confirmOwnPassword(w, r, req.Password) {
		return
	}
	if err := engines.SaveSecurityExport(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), req.URL, req.Secret, req.Enabled); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	handleSecurityExportStatus(w, r)
}

func handleSecurityExportTest(w http.ResponseWriter, r *http.Request) {
	if !securityAdmin(w, r) {
		return
	}
	detail, err := engines.TestSecurityExport(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "detail": detail})
}

func handleNotifyMatrix(w http.ResponseWriter, r *http.Request) {
	if !securityAdmin(w, r) {
		return
	}
	tenant := r.Header.Get("Resolved-Tenant-ID")
	m, err := engines.CurrentNotifyMatrix(tenant)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	versions, err := engines.NotifyMatrixVersions(tenant)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"current": m, "versions": versions})
}

func handleNotifyMatrixSave(w http.ResponseWriter, r *http.Request) {
	if !securityAdmin(w, r) {
		return
	}
	var req struct {
		Entries []engines.NotifyMatrixRow `json:"entries"`
		Note    string                    `json:"note"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	v, err := engines.SaveNotifyMatrix(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), req.Entries, req.Note)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "version": v})
}

// handleNotifyDecisions: an Admin sees every decision; a Privacy Approver
// (49.12.5) sees only the ones that name them approver.
func handleNotifyDecisions(w http.ResponseWriter, r *http.Request) {
	if !requireAdminCapability(w, r, engines.CapPrivacyApproval) {
		return
	}
	list, err := engines.ListNotifyDecisions(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	if !engines.HasFullAccess(r.Header.Get("Resolved-Role")) {
		me := r.Header.Get("Resolved-Username")
		mine := list[:0:0]
		for _, d := range list {
			if strings.EqualFold(d.Approver, me) {
				mine = append(mine, d)
			}
		}
		list = mine
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"decisions": list, "now": time.Now().UTC()})
}

func handleNotifyIncidentOpen(w http.ResponseWriter, r *http.Request) {
	if !securityAdmin(w, r) {
		return
	}
	var req struct {
		Title        string    `json:"title"`
		DiscoveredAt time.Time `json:"discovered_at"`
		Keys         []string  `json:"keys"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	ref, err := engines.OpenNotifyIncident(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), req.Title, req.DiscoveredAt, req.Keys)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "incident_ref": ref})
}

func handleNotifyDecide(w http.ResponseWriter, r *http.Request) {
	if !securityAdmin(w, r) {
		return
	}
	var req struct {
		ID          string `json:"id"`
		Decision    string `json:"decision"`
		Reason      string `json:"reason"`
		EvidenceRef string `json:"evidence_ref"`
		Approver    string `json:"approver"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	if err := engines.RecordNotifyDecision(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), req.ID, req.Decision, req.Reason, req.EvidenceRef, req.Approver); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleNotifyApprovers lists who may be named approver, for the Decide form.
func handleNotifyApprovers(w http.ResponseWriter, r *http.Request) {
	if !securityAdmin(w, r) {
		return
	}
	names, err := engines.NotifyApproverCandidates(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"approvers": names})
}

// handleNotifyReview: the named approver answers - an Admin or a Privacy
// Approver; the engine checks they are the named one, still active, and not
// the decider.
func handleNotifyReview(w http.ResponseWriter, r *http.Request) {
	if !requireAdminCapability(w, r, engines.CapPrivacyApproval) {
		return
	}
	var req struct {
		ID        string `json:"id"`
		Approve   bool   `json:"approve"`
		Note      string `json:"note"`
		DecidedAt string `json:"decided_at"` // the decision the approver read
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	if err := engines.ReviewNotifyDecision(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), req.ID, req.Approve, req.Note, req.DecidedAt); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleSecurityExportBackfill sends the history from a start date again, in order.
func handleSecurityExportBackfill(w http.ResponseWriter, r *http.Request) {
	if !securityAdmin(w, r) {
		return
	}
	var req struct {
		From     string `json:"from"`
		Password string `json:"password"`
	}
	if !readJSONBody(w, r, &req) || !confirmOwnPassword(w, r, req.Password) {
		return
	}
	from, err := time.Parse("2006-01-02", req.From)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Pick a start date.")
		return
	}
	rows, err := engines.StartSecurityExportBackfill(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), from, time.Now())
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "events": rows})
}
