package server

import (
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Passkeys (49.2.3): WebAuthn as a phishing-resistant second factor, standard
// library only (engines/webauthn.go). Registration is for a signed-in person;
// the sign-in answer replaces the authenticator code on the MFA challenge.

// passkeyOrigin is the origin the browser must report: this request's own host.
func passkeyOrigin(r *http.Request) engines.PasskeyOrigin {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	return engines.PasskeyOriginFromHost(r.Host, secure)
}

// clientChallenge reads the challenge the browser echoed in clientDataJSON.
func clientChallenge(clientDataJSON []byte) string {
	var cd struct {
		Challenge string `json:"challenge"`
	}
	_ = json.Unmarshal(clientDataJSON, &cd)
	return cd.Challenge
}

// GET /api/v1/me/passkeys
func handleListMyPasskeys(w http.ResponseWriter, r *http.Request) {
	list, err := engines.ListPasskeys(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not read your passkeys")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"passkeys": list})
}

// POST /api/v1/me/passkeys/options
func handlePasskeyRegisterOptions(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID")
	username := r.Header.Get("Resolved-Username")
	ch, err := engines.NewPasskeyChallenge(tenantID, userID, "register")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not start passkey setup")
		return
	}
	existing, _ := engines.PasskeyCredentialIDs(tenantID, userID)
	exclude := []map[string]string{}
	for _, id := range existing {
		exclude = append(exclude, map[string]string{"type": "public-key", "id": id})
	}
	o := passkeyOrigin(r)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"publicKey": map[string]interface{}{
		"challenge": ch,
		"rp":        map[string]string{"id": o.RPID, "name": "ERP"},
		"user":      map[string]string{"id": engines.EncodeB64URL([]byte(userID)), "name": username, "displayName": username},
		"pubKeyCredParams": []map[string]interface{}{
			{"type": "public-key", "alg": -7}, {"type": "public-key", "alg": -257},
		},
		"timeout":            300000,
		"attestation":        "none",
		"excludeCredentials": exclude,
		"authenticatorSelection": map[string]string{
			"residentKey": "preferred", "userVerification": "required",
		},
	}})
}

// POST /api/v1/me/passkeys  {label, client_data_json, attestation_object}  (base64url)
func handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID")
	var req struct {
		Label             string `json:"label"`
		ClientDataJSON    string `json:"client_data_json"`
		AttestationObject string `json:"attestation_object"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	cd, e1 := engines.DecodeB64URL(req.ClientDataJSON)
	att, e2 := engines.DecodeB64URL(req.AttestationObject)
	if e1 != nil || e2 != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid passkey answer")
		return
	}
	ch := clientChallenge(cd)
	if !engines.ConsumePasskeyChallenge(tenantID, userID, "register", ch) {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Passkey setup expired - start again")
		return
	}
	reg, err := engines.VerifyPasskeyRegistration(cd, att, ch, passkeyOrigin(r))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "This passkey could not be verified")
		return
	}
	if err := engines.SavePasskey(tenantID, userID, req.Label, reg); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Could not save this passkey (already added, or the limit is reached)")
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, r.Header.Get("Resolved-Username"), "SECURITY", "PASSKEY_ADDED",
		fmt.Sprintf("Passkey added: %s", strings.TrimSpace(req.Label)))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// DELETE /api/v1/me/passkeys/{id}
func handleDeleteMyPasskey(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID")
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid passkey id")
		return
	}
	ok, err := engines.DeletePasskey(tenantID, userID, id)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not remove the passkey")
		return
	}
	if !ok {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Passkey not found")
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, r.Header.Get("Resolved-Username"), "SECURITY", "PASSKEY_REMOVED", fmt.Sprintf("Passkey %d removed", id))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// POST /api/v1/auth/passkey/options (MFA challenge token)
func handlePasskeyAssertOptions(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Resolved-Purpose") != "mfa_challenge" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "This endpoint requires an MFA challenge token from /api/v1/login")
		return
	}
	tenantID, userID := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID")
	ids, err := engines.PasskeyCredentialIDs(tenantID, userID)
	if err != nil || len(ids) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "No passkey is set up for this account")
		return
	}
	ch, err := engines.NewPasskeyChallenge(tenantID, userID, "assert")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not start passkey sign-in")
		return
	}
	allow := []map[string]string{}
	for _, id := range ids {
		allow = append(allow, map[string]string{"type": "public-key", "id": id})
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"publicKey": map[string]interface{}{
		"challenge": ch, "rpId": passkeyOrigin(r).RPID, "timeout": 300000,
		"userVerification": "required", "allowCredentials": allow,
	}})
}

// POST /api/v1/auth/passkey/verify (MFA challenge token)
func handlePasskeyAssertVerify(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Resolved-Purpose") != "mfa_challenge" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "This endpoint requires an MFA challenge token from /api/v1/login")
		return
	}
	var req struct {
		CredentialID      string `json:"credential_id"`
		ClientDataJSON    string `json:"client_data_json"`
		AuthenticatorData string `json:"authenticator_data"`
		Signature         string `json:"signature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID, userID := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID")
	credID, e1 := engines.DecodeB64URL(req.CredentialID)
	cd, e2 := engines.DecodeB64URL(req.ClientDataJSON)
	ad, e3 := engines.DecodeB64URL(req.AuthenticatorData)
	sig, e4 := engines.DecodeB64URL(req.Signature)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid passkey answer")
		return
	}
	ch := clientChallenge(cd)
	if !engines.ConsumePasskeyChallenge(tenantID, userID, "assert", ch) {
		writeAPIError(w, r, "USERAC-0025", "")
		return
	}
	if err := engines.UsePasskeyAssertion(tenantID, userID, credID, cd, ad, sig, ch, passkeyOrigin(r)); err != nil {
		writeAPIError(w, r, "USERAC-0025", "")
		return
	}
	role, username, locationCode, credentialVersion, err := engines.LookupUserRoleAndUsername(tenantID, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Passkey verified but failed to issue session")
		return
	}
	token := engines.SignToken(userID, username, role, tenantID, locationCode, credentialVersion)
	engines.RecordSignInFrom(r.Context(), tenantID, userID)
	engines.RecordPrivilegedSession(r.Context(), tenantID, userID, username, role, token, credentialVersion)
	engines.LogAuditEventCtx(r.Context(), tenantID, username, "LOGIN", "MFA_VERIFIED", "Passkey verified, session issued")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token, "role": engines.CanonicalRole(role), "user": username,
		"used_recovery_code": false, "recovery_codes_remaining": 0,
	})
}
