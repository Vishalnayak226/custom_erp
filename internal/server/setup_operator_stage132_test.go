package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Stage 132.15: the Setup Operator (Guide's own account per organisation)
// reaches the Setup Workbook, the business settings and the industry - and
// nothing that decides who may do what.
func TestSetupOperatorAdminRoutes(t *testing.T) {
	op := engines.RoleSetupOperator
	for pattern, want := range map[string]bool{
		"POST /api/v1/admin/setup-workbook/apply": true,
		"GET /api/v1/admin/setup-workbook/export": true,
		"PUT /api/v1/admin/settings":              true,
		"POST /api/v1/admin/industry":             true,
		"POST /api/v1/admin/users":                false,
		"POST /api/v1/admin/roles":                false,
		"POST /api/v1/admin/role-permissions":     false,
		"GET /api/v1/admin/users":                 false,
	} {
		ok, _, found := checkRouteCapability(pattern, op)
		if !found {
			t.Fatalf("%s is not classified", pattern)
		}
		if ok != want {
			t.Errorf("Setup Operator on %s: allowed=%v, want %v", pattern, ok, want)
		}
	}
	if ok, _, _ := checkRouteCapability("POST /api/v1/admin/setup-workbook/apply", engines.RoleStoreManager); ok {
		t.Errorf("a Store Manager reached the Setup Workbook")
	}
}

func TestSetupOperatorSettingsAndWorkbookRuns(t *testing.T) {
	db.InitDB(testConnStr())
	op := engines.RoleSetupOperator
	call := func(h http.HandlerFunc, method, role, user string, body interface{}, id string) *httptest.ResponseRecorder {
		t.Helper()
		var rd io.Reader = http.NoBody
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, "/x", rd)
		req.Header.Set("Resolved-Tenant-ID", "default")
		req.Header.Set("Resolved-Role", role)
		req.Header.Set("Resolved-Username", user)
		if id != "" {
			req.SetPathValue("id", id)
		}
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	keys := func(rec *httptest.ResponseRecorder) map[string]bool {
		var list []engines.SettingWithValue
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatalf("settings list: %v %s", err, rec.Body.String())
		}
		out := map[string]bool{}
		for _, s := range list {
			out[s.Key] = true
		}
		return out
	}
	const sec, biz = "security.loyalty_otp_validity_minutes", "procurement.po_edit_window_days"

	// GET: the business settings only; an administrator still sees all.
	rec := call(handleGetSettings, http.MethodGet, op, "manager1", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings as Setup Operator: %d %s", rec.Code, rec.Body.String())
	}
	got := keys(rec)
	for k := range got {
		if engines.SettingIsAdminOnly(k) {
			t.Errorf("Setup Operator sees admin-only setting %s", k)
		}
	}
	if !got[biz] {
		t.Errorf("Setup Operator does not see %s", biz)
	}
	if !keys(call(handleGetSettings, http.MethodGet, engines.RoleAdmin, "admin", nil, ""))[sec] {
		t.Errorf("an administrator no longer sees %s", sec)
	}
	if rec := call(handleGetSettings, http.MethodGet, engines.RoleStoreManager, "manager1", nil, ""); rec.Code != http.StatusForbidden {
		t.Errorf("Store Manager reading settings: %d, want 403", rec.Code)
	}

	// PUT: a batch with a security key is refused whole; a business key saves.
	secBefore, bizBefore := engines.GetSettingString("default", sec), engines.GetSettingString("default", biz)
	defer func() { _ = engines.SetSetting("default", biz, bizBefore, "t132-test") }()
	if rec := call(handleUpdateSettings, http.MethodPut, op, "manager1", map[string]string{sec: "7", biz: "3"}, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("security setting by Setup Operator: %d %s, want 403", rec.Code, rec.Body.String())
	}
	if engines.GetSettingString("default", sec) != secBefore || engines.GetSettingString("default", biz) != bizBefore {
		t.Fatalf("a refused batch changed a setting")
	}
	if rec := call(handleUpdateSettings, http.MethodPut, op, "manager1", map[string]string{biz: "3"}, ""); rec.Code != http.StatusOK {
		t.Fatalf("business setting by Setup Operator: %d %s", rec.Code, rec.Body.String())
	}
	if v := engines.GetSettingString("default", biz); v != "3" {
		t.Fatalf("%s = %q after save, want 3", biz, v)
	}

	// Workbook runs: an administrator's upload (it can carry first passwords)
	// is not the Setup Operator's to list, open or download; its own are.
	adminRun, err := engines.CreateSetupWorkbookRun("default", "admin", "t132-admin.xlsx", []byte("admin"))
	if err != nil {
		t.Fatalf("seed admin run: %v", err)
	}
	opRun, err := engines.CreateSetupWorkbookRun("default", "manager1", "t132-guide.xlsx", []byte("guide"))
	if err != nil {
		t.Fatalf("seed operator run: %v", err)
	}
	defer func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id IN ($1, $2)`, adminRun, opRun)
	}()
	listed := func(role, user string) map[string]bool {
		var runs []map[string]interface{}
		_ = json.Unmarshal(call(handleSetupWorkbookRuns, http.MethodGet, role, user, nil, "").Body.Bytes(), &runs)
		out := map[string]bool{}
		for _, r := range runs {
			id, _ := r["id"].(string)
			out[id] = true
		}
		return out
	}
	if l := listed(op, "manager1"); l[adminRun] || !l[opRun] {
		t.Errorf("Setup Operator's run list: admin run shown=%v, own run shown=%v", l[adminRun], l[opRun])
	}
	if l := listed(engines.RoleAdmin, "admin"); !l[adminRun] || !l[opRun] {
		t.Errorf("an administrator does not see every run")
	}
	for _, h := range []http.HandlerFunc{handleSetupWorkbookRun, handleSetupWorkbookRunFile} {
		if rec := call(h, http.MethodGet, op, "manager1", nil, adminRun); rec.Code != http.StatusNotFound {
			t.Errorf("Setup Operator opened an administrator's run: %d", rec.Code)
		}
		if rec := call(h, http.MethodGet, op, "manager1", nil, opRun); rec.Code != http.StatusOK {
			t.Errorf("Setup Operator cannot open its own run: %d %s", rec.Code, rec.Body.String())
		}
	}

	// Export: the four access sheets are headers only, and no admin-only setting.
	rec = call(handleSetupWorkbookExport, http.MethodGet, op, "manager1", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("export as Setup Operator: %d %s", rec.Code, rec.Body.String())
	}
	tables, _, err := swParse(rec.Body.Bytes(), swHeaderWords("default"))
	if err != nil {
		t.Fatalf("parse export: %v", err)
	}
	for _, sheet := range []string{"Roles", "Role Access", "Users", "Approval Rules"} {
		if tb := tables[swNorm(sheet)]; tb != nil && len(tb.rows) > 0 {
			t.Errorf("Setup Operator's export has %d rows on %s", len(tb.rows), sheet)
		}
	}
	if tb := tables[swNorm("Settings")]; tb != nil {
		for _, row := range tb.rows {
			if k := tb.get(row, "Key"); k != "" && k != "industry" && engines.SettingIsAdminOnly(k) {
				t.Errorf("Setup Operator's export carries admin-only setting %s", k)
			}
		}
	} else {
		t.Errorf("export has no Settings sheet")
	}
}

// A new role named after the template starts with the template's access -
// vendors yes, paying them no, deleting nothing - recorded as a template apply.
func TestCreateRoleNamedAfterTemplateGetsItsAccess(t *testing.T) {
	db.InitDB(testConnStr())
	name := engines.RoleSetupOperator
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.role_permissions WHERE role = $1`, name)
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.roles WHERE name = $1`, name)
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.role_template_migrations WHERE roles = $1`, name)
	}
	cleanup()
	defer cleanup()
	body, _ := json.Marshal(map[string]string{"name": name, "description": "Guide"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/roles", bytes.NewReader(body))
	req.Header.Set("Resolved-Tenant-ID", "default")
	req.Header.Set("Resolved-Role", engines.RoleAdmin)
	req.Header.Set("Resolved-Username", "admin")
	rec := httptest.NewRecorder()
	handleCreateRole(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create %s: %d %s", name, rec.Code, rec.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["template"] != name {
		t.Errorf("response does not name the template: %v", out)
	}
	grant := func(doctype string) (read, create, del bool) {
		_ = db.DB.QueryRow(`SELECT allow_read, allow_create, allow_delete FROM tenant_default.role_permissions WHERE role = $1 AND doctype_name = $2`, name, doctype).Scan(&read, &create, &del)
		return
	}
	if r, c, _ := grant("Vendor"); !r || !c {
		t.Errorf("Vendor: read=%v create=%v, want both", r, c)
	}
	if r, c, _ := grant("VendorInvoice"); !r || c {
		t.Errorf("VendorInvoice: read=%v create=%v, want read only", r, c)
	}
	if r, _, _ := grant("Payslip"); r {
		t.Errorf("Payslip is readable")
	}
	var deletes int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.role_permissions WHERE role = $1 AND allow_delete`, name).Scan(&deletes)
	if deletes > 0 {
		t.Errorf("%d record types deletable", deletes)
	}
	var migrations int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.role_template_migrations WHERE roles = $1 AND approved_by = 'admin'`, name).Scan(&migrations)
	if migrations != 1 {
		t.Errorf("template apply recorded %d times, want 1", migrations)
	}
}
