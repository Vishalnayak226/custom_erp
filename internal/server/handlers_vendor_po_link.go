package server

import (
	"custom_erp/engines"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Stage 148.18: the vendor's signed PO link. The three /public routes carry no
// session - the signed link is the credential - and are rate limited and
// audited. The rest are staff routes behind the PurchaseOrder permission and
// the store scope.

const vendorPublicBodyMax = 3 << 20

func vlPublicFail(w http.ResponseWriter, r *http.Request, err error) {
	writeEngineError(w, r, err, http.StatusUnprocessableEntity)
}

func handleVendorLinkPublic(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	v, err := engines.VendorLinkPublicView(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("t"))
	if err != nil {
		vlPublicFail(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func handleVendorLinkPublicProposal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Lines []engines.VendorProposalLine `json:"lines"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The request is not valid")
		return
	}
	res, err := engines.SubmitVendorProposal(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("t"), req.Lines)
	if err != nil {
		vlPublicFail(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleVendorLinkPublicInvoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, vendorPublicBodyMax)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The file is larger than 2 MB or the request is not valid")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Choose a file to send")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 2<<20+1))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The file could not be read")
		return
	}
	var amount *float64
	if a := strings.TrimSpace(r.FormValue("amount")); a != "" {
		v, perr := strconv.ParseFloat(a, 64)
		if perr != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The amount must be a number")
			return
		}
		amount = &v
	}
	if err := engines.SubmitVendorInvoice(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("t"), hdr.Filename, data,
		r.FormValue("invoice_no"), r.FormValue("invoice_date"), amount); err != nil {
		vlPublicFail(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// vlStaff checks the PurchaseOrder permission and the store scope of the order
// behind an id, and writes the refusal itself.
func vlStaff(w http.ResponseWriter, r *http.Request, action, kind, id string) (tenantID string, ok bool) {
	tenantID = r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	allowed, err := checkPermission(tenantID, role, "PurchaseOrder", action)
	if err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return "", false
	}
	po, err := engines.VendorLinkPOData(tenantID, kind, id)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return "", false
	}
	scope := engines.SessionScope{Role: role, UserID: r.Header.Get("Resolved-User-ID"), LocationCode: r.Header.Get("Resolved-Location")}
	scope, err = engines.ResolveSessionScope(tenantID, "PurchaseOrder", scope)
	if err != nil {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return "", false
	}
	if named := engines.LocationOutOfScope("PurchaseOrder", scope, po); named != "" {
		writeAPIErrorDetail(w, r, "GLOBAL-0011", "", engines.LocationOutOfScopeMessage(named, scope.LocationCode))
		return "", false
	}
	return tenantID, true
}

func handleVendorLinkState(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := vlStaff(w, r, "read", "po", r.PathValue("id"))
	if !ok {
		return
	}
	v, err := engines.VendorLinkAdminView(tenantID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func handleVendorLinkCreate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := vlStaff(w, r, "update", "po", r.PathValue("id"))
	if !ok {
		return
	}
	info, err := engines.CreateVendorPOLink(tenantID, r.Header.Get("Resolved-User-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(info)
}

func handleVendorLinkRevoke(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := vlStaff(w, r, "update", "po", r.PathValue("id"))
	if !ok {
		return
	}
	if err := engines.RevokeVendorPOLink(tenantID, r.Header.Get("Resolved-User-ID"), r.PathValue("id")); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func vlRowID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || n < 1 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid id")
		return 0, false
	}
	return n, true
}

func handleVendorLinkApply(w http.ResponseWriter, r *http.Request) {
	n, ok := vlRowID(w, r)
	if !ok {
		return
	}
	tenantID, ok := vlStaff(w, r, "update", "proposal", r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		RememberUnit bool `json:"remember_unit"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
	if err := engines.ApplyVendorProposal(tenantID, r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"), n, req.RememberUnit); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleVendorLinkReject(w http.ResponseWriter, r *http.Request) {
	n, ok := vlRowID(w, r)
	if !ok {
		return
	}
	tenantID, ok := vlStaff(w, r, "update", "proposal", r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
	if err := engines.RejectVendorProposal(tenantID, r.Header.Get("Resolved-User-ID"), n, req.Note); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleVendorLinkFile(w http.ResponseWriter, r *http.Request) {
	n, ok := vlRowID(w, r)
	if !ok {
		return
	}
	tenantID, ok := vlStaff(w, r, "read", "file", r.PathValue("id"))
	if !ok {
		return
	}
	name, ctype, data, err := engines.VendorLinkFile(tenantID, n)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", attachmentDisposition(name))
	_, _ = w.Write(data)
}

func handleVendorLinkFileReview(w http.ResponseWriter, r *http.Request) {
	n, ok := vlRowID(w, r)
	if !ok {
		return
	}
	tenantID, ok := vlStaff(w, r, "update", "file", r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
	if err := engines.ReviewVendorFile(tenantID, r.Header.Get("Resolved-User-ID"), n, req.Status, req.Note); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
