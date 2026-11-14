package server

import (
	"crypto/rand"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Ask 170 group E: recruitment, login at onboarding, certificate expiry and
// people analytics (engines/hr_recruitment.go, hr_certificates.go, hr_analytics.go).

func hrActor(r *http.Request) string {
	if a := r.Header.Get("Resolved-Username"); a != "" {
		return a
	}
	return r.Header.Get("Resolved-User-ID")
}

func hrPermitted(w http.ResponseWriter, r *http.Request, doctype, action string) bool {
	ok, err := checkPermission(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), doctype, action)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return false
	}
	if !ok {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return false
	}
	return true
}

// POST /api/v1/hr/job-offers/{id}/accept - accepting makes the draft Employee.
func handleAcceptJobOffer(w http.ResponseWriter, r *http.Request) {
	if !hrPermitted(w, r, "JobOffer", "update") {
		return
	}
	res, err := engines.AcceptJobOffer(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), hrActor(r))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

// POST /api/v1/hr/onboarding/{id}/create-login {role, username?, email?, location_code?}
// Makes the employee's login from a finished onboarding checklist. No password
// is chosen or shown: the account holds a random unknown one and must change
// it; the person sets their own through Forgot password.
func handleCreateLoginFromOnboarding(w http.ResponseWriter, r *http.Request) {
	actorRole := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, actorRole) {
		return
	}
	var req struct {
		Role         string `json:"role"`
		Username     string `json:"username"`
		Email        string `json:"email"`
		LocationCode string `json:"location_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	empID, emp, err := engines.OnboardingLoginSource(tenantID, r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	str := func(k string) string { s, _ := emp[k].(string); return strings.TrimSpace(s) }
	role := strings.TrimSpace(req.Role)
	if role == "" {
		writeAPIError(w, r, "HR-1712", "")
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	known, roleErr := tenantRoleNames(schema)
	if roleErr != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, roleErr.Error())
		return
	}
	matched := ""
	for _, k := range known {
		if strings.EqualFold(k, role) {
			matched = k
		}
	}
	if matched == "" || engines.IsSuperAdmin(matched) {
		writeAPIError(w, r, "HR-1712", "")
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		email = str("email")
	}
	if email == "" {
		writeAPIError(w, r, "HR-1713", "")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = strings.ToLower(strings.ReplaceAll(firstNonEmpty(str("code"), empID), " ", ""))
	}
	loc := strings.TrimSpace(req.LocationCode)
	if loc == "" {
		loc = firstNonEmpty(str("location"), "HO")
	}
	var department interface{}
	if engines.DepartmentNeededForRole(matched) {
		dept, derr := engines.ResolveDepartmentReference(tenantID, str("department"))
		if derr != nil {
			writeEngineError(w, r, derr, http.StatusUnprocessableEntity)
			return
		}
		department = dept
	}
	var active int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.users WHERE status = 'Active' AND %s`, schema, engines.NotSupportUserSQL("users"))).Scan(&active); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := engines.CheckTenantLimit(tenantID, "max_users", active+1); err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	// An unknown random secret: nobody, including HR, can sign in with it.
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to create login")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(fmt.Sprintf("%x", secret)), bcrypt.DefaultCost)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Failed to create login")
		return
	}
	status := "Active"
	if engines.IdentityChangeApprovalOn(tenantID) {
		status = "Inactive"
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.users (id, username, password_hash, email, role, status, location_code, idle_timeout_minutes, must_change_password, department) VALUES ($1, $1, $2, $3, $4, $8, $5, $6, TRUE, $7)`, schema),
		username, string(hash), email, matched, loc, engines.GetSettingInt(tenantID, "security.default_idle_timeout_minutes"), department, status)
	if err != nil {
		msg := "Failed to create the login."
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			msg = fmt.Sprintf("Username %q is already taken.", username)
		}
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, msg)
		return
	}
	if err := engines.LinkEmployeeLogin(tenantID, empID, username); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	engines.LogAuditEventCtx(r.Context(), tenantID, hrActor(r), "USER_MANAGEMENT", "USER_CREATED", fmt.Sprintf("Created login %s (%s) from onboarding %s", username, matched, r.PathValue("id")))
	out := map[string]string{"status": "success", "username": username}
	if status == "Inactive" {
		reqID, reqErr := engines.RequestAccessChange(tenantID, engines.AccessChangeJoiner, username, "new "+matched+" account", "", r.Header.Get("Resolved-User-ID"), actorRole)
		if reqErr != nil {
			writeEngineError(w, r, reqErr, http.StatusUnprocessableEntity)
			return
		}
		out["status"], out["request_id"] = "pending_approval", reqID
	}
	_ = json.NewEncoder(w).Encode(out)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// GET /api/v1/hr/certificates-expiring?days=N
func handleExpiringCertificates(w http.ResponseWriter, r *http.Request) {
	if !hrPermitted(w, r, "TrainingRecord", "read") {
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	list, err := engines.ListExpiringCertificates(r.Header.Get("Resolved-Tenant-ID"), days)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(list)
}

// GET /api/v1/hr/people-analytics?group_by=department|location|position&from=&to=
func handlePeopleAnalytics(w http.ResponseWriter, r *http.Request) {
	if !hrPermitted(w, r, "Employee", "read") {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	base := engines.SessionScope{Role: r.Header.Get("Resolved-Role"), UserID: r.Header.Get("Resolved-User-ID"), LocationCode: r.Header.Get("Resolved-Location")}
	session, err := engines.ResolveSessionScope(tenantID, "Employee", base)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()
	group := q.Get("group_by")
	if group == "" {
		group = "department"
	}
	rows, err := engines.PeopleAnalytics(tenantID, session, group, q.Get("from"), q.Get("to"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(rows)
}
