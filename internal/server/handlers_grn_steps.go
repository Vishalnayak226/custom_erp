package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 94: goods receipt in steps (engines/grn_steps.go). Every route needs
// the matching Goods Receipt right; the steps after Receive also need the
// step's own role setting (Configuration > Procurement > "Goods receipt: who
// may ..."), blank meaning every role that can edit goods receipts.

// grnStepGate checks the GRN right and, for a step, the step's role list.
func grnStepGate(w http.ResponseWriter, r *http.Request, action, step string) bool {
	if !requireDocPermission(w, r, "GRN", action, "You do not have permission to work on goods receipts") {
		return false
	}
	if step != "" && !engines.GRNStepAllowed(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), step) {
		writeEngineError(w, r, &engines.ValidationError{Code: "GOODSR-0947", Message: "your role does not do this step of a goods receipt - ask someone who does, or an admin to add your role under Configuration > Procurement"}, http.StatusForbidden)
		return false
	}
	return true
}

// grnStepAllowedMap tells the screen which steps this person may do, so it
// shows only the buttons that will work.
func grnStepAllowedMap(r *http.Request) map[string]bool {
	tenantID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role")
	update, _ := checkPermission(tenantID, role, "GRN", "update")
	out := map[string]bool{"receive": update}
	for _, s := range []string{"verify", "price", "post", "pay", "correct"} {
		out[s] = update && engines.GRNStepAllowed(tenantID, role, s)
	}
	return out
}

func writeGRNStepView(w http.ResponseWriter, r *http.Request, v *engines.GRNStepView, err error) {
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	v.Allowed = grnStepAllowedMap(r)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeGRNStepBody(w http.ResponseWriter, r *http.Request, into interface{}) bool {
	if r.ContentLength == 0 {
		return true
	}
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return false
	}
	return true
}

// GET /api/v1/grn-steps - the inbox: every receipt in steps, with its tab.
func handleGRNStepsInbox(w http.ResponseWriter, r *http.Request) {
	if !grnStepGate(w, r, "read", "") {
		return
	}
	rows, err := engines.ListGRNInbox(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to load goods receipts")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"rows": rows, "allowed": grnStepAllowedMap(r),
		"blind_count": engines.GetSettingBool(r.Header.Get("Resolved-Tenant-ID"), engines.SettingGRNBlindCount),
	})
}

// GET /api/v1/grn-steps/open-pos?vendor= - Receive's tick-list.
func handleGRNStepsOpenPOs(w http.ResponseWriter, r *http.Request) {
	if !grnStepGate(w, r, "create", "") {
		return
	}
	pos, err := engines.OpenPOsForVendor(r.Header.Get("Resolved-Tenant-ID"), strings.TrimSpace(r.URL.Query().Get("vendor")))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to load the vendor's open purchase orders")
		return
	}
	_ = json.NewEncoder(w).Encode(pos)
}

// GET /api/v1/grn-steps/{id}
func handleGRNStepsGet(w http.ResponseWriter, r *http.Request) {
	if !grnStepGate(w, r, "read", "") {
		return
	}
	v, err := engines.GetGRNStepView(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusNotFound)
		return
	}
	writeGRNStepView(w, r, v, nil)
}

// POST /api/v1/grn-steps/{id}/receive - Back to step 1 and change it.
func handleGRNStepsReceive(w http.ResponseWriter, r *http.Request) {
	if !grnStepGate(w, r, "update", "") {
		return
	}
	var in engines.GRNReceiveEdit
	if !decodeGRNStepBody(w, r, &in) {
		return
	}
	v, err := engines.SaveGRNReceive(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.PathValue("id"), in)
	writeGRNStepView(w, r, v, err)
}

// POST /api/v1/grn-steps/{id}/verify
func handleGRNStepsVerify(w http.ResponseWriter, r *http.Request) {
	if !grnStepGate(w, r, "update", "verify") {
		return
	}
	var in engines.GRNVerifyInput
	if !decodeGRNStepBody(w, r, &in) {
		return
	}
	v, err := engines.VerifyGRNStep(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"), r.PathValue("id"), in)
	writeGRNStepView(w, r, v, err)
}

// POST /api/v1/grn-steps/{id}/price
func handleGRNStepsPrice(w http.ResponseWriter, r *http.Request) {
	if !grnStepGate(w, r, "update", "price") {
		return
	}
	var in engines.GRNPriceCheckInput
	if !decodeGRNStepBody(w, r, &in) {
		return
	}
	v, err := engines.PriceCheckGRNStep(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.PathValue("id"), in)
	writeGRNStepView(w, r, v, err)
}

// POST /api/v1/grn-steps/{id}/post/{child} - MRP and post one child.
func handleGRNStepsPostChild(w http.ResponseWriter, r *http.Request) {
	if !grnStepGate(w, r, "update", "post") {
		return
	}
	var in engines.GRNPostInput
	if !decodeGRNStepBody(w, r, &in) {
		return
	}
	v, err := engines.PostGRNChildStep(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Location"),
		r.PathValue("id"), r.PathValue("child"), in)
	writeGRNStepView(w, r, v, err)
}

// POST /api/v1/grn-steps/{id}/post-all - the quick path.
func handleGRNStepsPostAll(w http.ResponseWriter, r *http.Request) {
	if !grnStepGate(w, r, "update", "verify") || !grnStepGate(w, r, "update", "price") || !grnStepGate(w, r, "update", "post") {
		return
	}
	v, err := engines.PostAllGRNStep(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"),
		r.Header.Get("Resolved-Location"), r.PathValue("id"))
	writeGRNStepView(w, r, v, err)
}

// POST /api/v1/grn-steps/{id}/pay - Pay now (optional).
func handleGRNStepsPay(w http.ResponseWriter, r *http.Request) {
	if !grnStepGate(w, r, "update", "pay") {
		return
	}
	if !requireDocPermission(w, r, "VendorInvoice", "update", "You do not have permission to pay vendor bills") {
		return
	}
	var in engines.GRNPayInput
	if !decodeGRNStepBody(w, r, &in) {
		return
	}
	v, err := engines.PayGRNStep(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.PathValue("id"), in)
	writeGRNStepView(w, r, v, err)
}

// POST /api/v1/grn-steps/{id}/correct - preview (apply false) or apply.
func handleGRNStepsCorrect(w http.ResponseWriter, r *http.Request) {
	if !grnStepGate(w, r, "update", "correct") {
		return
	}
	var in engines.GRNCorrectionInput
	if !decodeGRNStepBody(w, r, &in) {
		return
	}
	eff, err := engines.CorrectGRNStep(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Location"), r.PathValue("id"), in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(eff)
}
