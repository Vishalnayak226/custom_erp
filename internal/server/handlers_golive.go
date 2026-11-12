package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 47.12.1 / 47.12.2 / 47.12.3 / 47.12.7 / 49.17.3: the go-live scope, the
// structure graph and the finance, cutover and go-live packs. Admin only; the
// packs are a record and a sign-off and block nothing else.

// goLiveGate is the door of every go-live route: the route-capability check
// skips extension tokens, so this refuses them itself.
func goLiveGate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdminCapability(w, r, "admin.golive") {
			return
		}
		next(w, r)
	}
}

func goLiveTenant(r *http.Request) string { return r.Header.Get("Resolved-Tenant-ID") }
func goLiveActor(r *http.Request) string  { return r.Header.Get("Resolved-Username") }

func goLiveBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return false
	}
	return true
}

func goLiveDone(w http.ResponseWriter, r *http.Request, v interface{}, err error) {
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func handleGoLiveScopeGet(w http.ResponseWriter, r *http.Request) {
	s, err := engines.GetGoLiveScope(goLiveTenant(r))
	goLiveDone(w, r, s, err)
}

func handleGoLiveScopeSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Answers engines.ScopeAnswers `json:"answers"`
	}
	if !goLiveBody(w, r, &req) {
		return
	}
	s, err := engines.SaveGoLiveScope(goLiveTenant(r), req.Answers, goLiveActor(r))
	goLiveDone(w, r, s, err)
}

func handleGoLiveGraph(w http.ResponseWriter, r *http.Request) {
	g, err := engines.GetGoLiveGraph(goLiveTenant(r))
	goLiveDone(w, r, g, err)
}

func handleGoLiveGraphScope(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Doc    string `json:"doc"`
		Out    bool   `json:"out"`
		Reason string `json:"reason"`
	}
	if !goLiveBody(w, r, &req) {
		return
	}
	err := engines.SetGoLiveNodeScope(goLiveTenant(r), req.Doc, req.Out, req.Reason, goLiveActor(r))
	goLiveDone(w, r, map[string]interface{}{"ok": true}, err)
}

func handleGoLivePack(w http.ResponseWriter, r *http.Request) {
	p, err := engines.GetGoLivePack(goLiveTenant(r), r.PathValue("pack"))
	goLiveDone(w, r, p, err)
}

func handleGoLiveEvidence(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Line      string `json:"line"`
		Note      string `json:"note"`
		Reference string `json:"reference"`
	}
	if !goLiveBody(w, r, &req) {
		return
	}
	err := engines.AttachGoLiveEvidence(goLiveTenant(r), req.Line, req.Note, req.Reference, goLiveActor(r))
	goLiveDone(w, r, map[string]interface{}{"ok": true}, err)
}

func handleGoLiveWaive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Line      string `json:"line"`
		Reason    string `json:"reason"`
		ExpiresOn string `json:"expires_on"`
	}
	if !goLiveBody(w, r, &req) {
		return
	}
	err := engines.WaiveGoLiveLine(goLiveTenant(r), req.Line, req.Reason, req.ExpiresOn, goLiveActor(r))
	goLiveDone(w, r, map[string]interface{}{"ok": true}, err)
}

func handleGoLiveWaiveEnd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Line string `json:"line"`
	}
	if !goLiveBody(w, r, &req) {
		return
	}
	err := engines.EndGoLiveWaiver(goLiveTenant(r), req.Line, goLiveActor(r))
	goLiveDone(w, r, map[string]interface{}{"ok": true}, err)
}

func handleGoLiveSignOff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Note string `json:"note"`
	}
	if !goLiveBody(w, r, &req) {
		return
	}
	p, err := engines.SignOffGoLivePack(goLiveTenant(r), r.PathValue("pack"), req.Note, goLiveActor(r))
	goLiveDone(w, r, p, err)
}
