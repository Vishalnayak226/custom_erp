package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 49.3.6 over HTTP: the Roles screen's save endpoint previews on
// request, and a real save returns the change id and the dangerous grants it
// gave, with the evidence row written.
func TestStage4936RolePermissionsPreviewAndEvidence(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/admin/role-permissions", apiMiddleware(handleRolePermissions))
	adminID, cleanupAdmin := seedStage47User(t, engines.RoleSuperAdmin, "")
	t.Cleanup(cleanupAdmin)
	token := stage47Token(adminID, engines.RoleSuperAdmin, "")
	role := fmt.Sprintf("QA HTTP %d", time.Now().UnixNano()%1_000_000_000)
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.role_permissions WHERE role = $1`, role)
		db.DB.Exec(`DELETE FROM tenant_default.role_grant_changes WHERE role = $1`, role)
	})
	body := map[string]interface{}{
		"role":    role,
		"grants":  []map[string]interface{}{{"doctype_name": "PaymentProposal", "allow_read": true, "allow_create": true, "allow_update": true}},
		"reason":  "covering the payables clerk",
		"preview": true,
	}
	ip := mfaTestIP()

	rec := doRequestFromIP(t, mux.ServeHTTP, "POST", "/api/v1/admin/role-permissions", token, body, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	var preview struct {
		Status  string `json:"status"`
		Preview struct {
			HighRisk        bool                     `json:"high_risk"`
			DangerousGained []engines.DangerousGrant `json:"dangerous_gained"`
		} `json:"preview"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil || preview.Status != "preview" || !preview.Preview.HighRisk {
		t.Fatalf("preview body %s (%v)", rec.Body.String(), err)
	}
	var n int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.role_permissions WHERE role = $1`, role).Scan(&n)
	if n != 0 {
		t.Fatalf("a preview saved %d grant(s)", n)
	}

	body["preview"] = false
	rec = doRequestFromIP(t, mux.ServeHTTP, "POST", "/api/v1/admin/role-permissions", token, body, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	var saved struct {
		Status          string                   `json:"status"`
		ChangeID        string                   `json:"change_id"`
		HighRisk        bool                     `json:"high_risk"`
		DangerousGained []engines.DangerousGrant `json:"dangerous_gained"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil || saved.Status != "success" || saved.ChangeID == "" ||
		!saved.HighRisk || len(saved.DangerousGained) != 1 || saved.DangerousGained[0].Doctype != "PaymentProposal" {
		t.Fatalf("save body %s (%v)", rec.Body.String(), err)
	}
	var reason, changedBy string
	if err := db.DB.QueryRow(`SELECT reason, changed_by FROM tenant_default.role_grant_changes WHERE id = $1`, saved.ChangeID).Scan(&reason, &changedBy); err != nil {
		t.Fatalf("evidence row: %v", err)
	}
	if reason != "covering the payables clerk" || changedBy != adminID {
		t.Fatalf("evidence reason=%q changed_by=%q", reason, changedBy)
	}
	if err := engines.VerifyRoleGrantChangeEvidence("default", saved.ChangeID); err != nil {
		t.Fatalf("evidence: %v", err)
	}
}
