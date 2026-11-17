package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"custom_erp/db"
	"custom_erp/engines"
)

// 205.7 / 205.9: the records API. A key reads and writes any document type its
// module grant covers, as its owner: the call re-enters the same handlers the
// browser uses, with the owner's own identity, role and store, so every rule that
// holds for the owner holds for the key. The middleware (publicAPIMiddleware)
// already proved the module, level, doctype list, owner and IP; this file only
// builds the owner's session and hands over.

const (
	recordsDocPattern   = "/api/v1/doc/{doctype}"
	recordsDocIDPattern = "/api/v1/doc/{doctype}/{id}"
)

// asKeyOwner returns a copy of r that carries the key owner's session, the way
// apiMiddleware leaves a signed-in user's request. ok is false when an answer was
// already written.
func asKeyOwner(w http.ResponseWriter, r *http.Request, pattern string) (*http.Request, bool) {
	call := publicAPICallFrom(r)
	if call == nil || call.Access == nil {
		writeAPIError(w, r, engines.APIKeyOwnerInactive, "")
		return nil, false
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	state, err := engines.OwnerStateForKey(tenantID, call.Access)
	if err != nil {
		writeAPIError(w, r, engines.APIKeyOwnerInactive, "")
		return nil, false
	}
	location := state.LocationCode
	if stores := call.Access.Stores; len(stores) > 0 {
		inKey := false
		for _, s := range stores {
			if s == location {
				inKey = true
			}
		}
		if !inKey {
			location = stores[0]
		}
	}
	allowed, classification, _ := checkRouteCapability(pattern, state.Role)
	r2 := r.Clone(engines.WithRequestPolicy(r.Context(), classification.Capability, policyVersion()))
	r2 = r2.WithContext(engines.WithOutsideCallsTenant(r2.Context(), tenantID))
	r2 = r2.WithContext(engines.WithJobActor(r2.Context(), call.Access.OwnerUserID))
	r2 = r2.WithContext(engines.WithSessionStore(r2.Context(), location))
	if !allowed {
		writeAPIError(w, r2, "GLOBAL-0011", "")
		return nil, false
	}
	r2.Header.Set("Resolved-User-ID", call.Access.OwnerUserID)
	r2.Header.Set("Resolved-Username", engines.APIKeyOwnerName(tenantID, call.Access.OwnerUserID))
	r2.Header.Set("Resolved-Role", engines.CanonicalRole(state.Role))
	r2.Header.Set("Resolved-Location", location)
	r2.Header.Set("Resolved-Store-View", state.StoreView)
	r2.Header.Set("Resolved-Purpose", "")
	r2.Header.Set("Resolved-Scope-Doctype", "")
	r2.Header.Set("Resolved-Store-Device", "")
	return r2, true
}

func withJSONBody(r *http.Request, method string, v interface{}) *http.Request {
	raw, _ := json.Marshal(v)
	r2 := r.Clone(r.Context())
	r2.Method = method
	r2.Body = io.NopCloser(bytes.NewReader(raw))
	r2.ContentLength = int64(len(raw))
	return r2
}

func recordStatus(tenantID, doctype, id string) (status string, data map[string]interface{}, found bool) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, false
	}
	var raw string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, COALESCE(status,'') FROM %s.documents WHERE doctype=$1 AND id=$2 AND deleted_at IS NULL`, schema), doctype, id).Scan(&raw, &status); err != nil {
		return "", nil, false
	}
	data = map[string]interface{}{}
	_ = json.Unmarshal([]byte(raw), &data)
	return status, data, true
}

// handlePublicRecords serves the collection and one-record routes.
func handlePublicRecords(w http.ResponseWriter, r *http.Request) {
	doctype, id := r.PathValue("doctype"), r.PathValue("id")
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	w.Header().Set("Content-Type", "application/json")

	pattern := recordsDocPattern
	if id != "" {
		pattern = recordsDocIDPattern
	}
	owner, ok := asKeyOwner(w, r, pattern)
	if !ok {
		return
	}
	owner.SetPathValue("doctype", doctype)
	owner.SetPathValue("id", id)

	switch {
	case r.Method == http.MethodPut || r.Method == http.MethodPatch:
		owner.Method = http.MethodPost
	case r.Method == http.MethodDelete:
		// A key removes drafts only; anything that has moved on is cancelled instead.
		status, _, found := recordStatus(tenantID, doctype, id)
		if found && status != "Draft" && status != "" {
			writeAPIErrorDetail(w, r, engines.APIKeyActionNotAllowed, "", "A key can delete a draft only. Cancel this "+doctype+" instead.")
			return
		}
	}
	handleGenericDoc(w, owner)
}

// handlePublicRecordAction serves submit, cancel, approve and reject.
func handlePublicRecordAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		doctype, id := r.PathValue("doctype"), r.PathValue("id")
		tenantID := r.Header.Get("Resolved-Tenant-ID")
		w.Header().Set("Content-Type", "application/json")

		switch action {
		case "submit":
			owner, ok := asKeyOwner(w, r, "POST /api/v1/approval/submit")
			if !ok {
				return
			}
			handleSubmitApproval(w, withJSONBody(owner, http.MethodPost, map[string]string{"doctype": doctype, "document_id": id}))
		case "approve", "reject":
			owner, ok := asKeyOwner(w, r, "POST /api/v1/approval/decide")
			if !ok {
				return
			}
			var in struct {
				Comment string `json:"comment"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			decision := "Approved"
			if action == "reject" {
				decision = "Rejected"
			}
			handleDecideApproval(w, withJSONBody(owner, http.MethodPost, map[string]string{"doctype": doctype, "document_id": id, "decision": decision, "comment": in.Comment}))
		case "cancel":
			owner, ok := asKeyOwner(w, r, recordsDocIDPattern)
			if !ok {
				return
			}
			_, data, found := recordStatus(tenantID, doctype, id)
			if !found {
				writeAPIErrorGeneric(w, r, http.StatusNotFound, doctype+" "+id+" was not found.")
				return
			}
			data["status"] = "Cancelled"
			owner = withJSONBody(owner, http.MethodPost, data)
			owner.SetPathValue("doctype", doctype)
			owner.SetPathValue("id", id)
			handleGenericDoc(w, owner)
		}
	}
}

type publicRecordField struct {
	Fieldname string `json:"fieldname"`
	Label     string `json:"label"`
	Type      string `json:"type"`
	Required  bool   `json:"required"`
	Options   string `json:"options,omitempty"`
	ReadOnly  bool   `json:"read_only"`
	MaxLength *int   `json:"max_length,omitempty"`
}

type publicRecordSchema struct {
	Doctype string              `json:"doctype"`
	Class   string              `json:"class"`
	Module  string              `json:"module"`
	Fields  []publicRecordField `json:"fields"`
}

// handlePublicRecordSchema lists the fields of a document type the owner may see.
func handlePublicRecordSchema(w http.ResponseWriter, r *http.Request) {
	doctype := r.PathValue("doctype")
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	w.Header().Set("Content-Type", "application/json")
	call := publicAPICallFrom(r)
	if call == nil {
		writeAPIError(w, r, engines.APIKeyOwnerInactive, "")
		return
	}
	state, err := engines.OwnerStateForKey(tenantID, call.Access)
	if err != nil {
		writeAPIError(w, r, engines.APIKeyOwnerInactive, "")
		return
	}
	meta, err := engines.GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	hidden := map[string]bool{}
	if list, herr := engines.HiddenFieldsForRole(tenantID, state.Role, doctype); herr == nil {
		for _, f := range list {
			hidden[f] = true
		}
	}
	module, _ := engines.ModuleForDoctype(tenantID, doctype)
	out := publicRecordSchema{Doctype: doctype, Class: engines.APIDoctypeClass(doctype), Module: module, Fields: []publicRecordField{}}
	readOnlyDoctype := out.Class != engines.APIDoctypeRW
	for _, f := range meta {
		if hidden[f.Fieldname] {
			continue
		}
		out.Fields = append(out.Fields, publicRecordField{
			Fieldname: f.Fieldname, Label: f.Label, Type: f.Fieldtype, Required: f.Mandatory,
			Options: f.Options, ReadOnly: f.AutoGenerated || readOnlyDoctype, MaxLength: f.MaxLength,
		})
	}
	_ = json.NewEncoder(w).Encode(out)
}

// handlePublicMe tells a key what it is.
func handlePublicMe(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	w.Header().Set("Content-Type", "application/json")
	call := publicAPICallFrom(r)
	if call == nil || call.Credential == nil {
		writeAPIError(w, r, engines.APIKeyOwnerInactive, "")
		return
	}
	row, err := engines.GetAPIKey(tenantID, call.Access.OwnerUserID, true, call.Credential.ID)
	if err != nil || row == nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not read this key.")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"name": row.Name, "key_prefix": row.KeyPrefix, "auth_mode": row.AuthMode, "status": row.Status,
		"owner":   map[string]interface{}{"id": row.OwnerID, "name": row.OwnerName, "active": row.OwnerActive},
		"modules": row.Modules, "doctypes": row.Doctypes, "can_approve": row.CanApprove,
		"stores": row.Stores, "expires_at": row.ExpiresAt, "limits": row.Limits,
	})
}

// handlePublicClaim hands a developer link's key over once. No sign-in: the link is the proof.
func handlePublicClaim(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))
	if tenantID == "" {
		tenantID = strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	}
	out, err := engines.ClaimAPIKey(tenantID, r.PathValue("token"))
	if err != nil || out == nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "This link has been used or has expired. Ask for a new one.")
		return
	}
	_ = json.NewEncoder(w).Encode(out.Present(publicOrigin(r)))
}
