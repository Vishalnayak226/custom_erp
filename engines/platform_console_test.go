package engines

import (
	"custom_erp/db"
	"errors"
	"strings"
	"testing"
)

// Stage 60.6 - platform data console: operator allowlist, masking of secrets
// and customer details, raw edits with masked JSON keys preserved, and the
// protected columns/tables. Own throwaway tenant schema.
func TestPlatformConsoleMaskingAndEdits(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	t.Setenv(platformOperatorsEnv, "default:ops, other:root")
	if !IsPlatformOperator("default", "ops", RoleSuperAdmin) {
		t.Errorf("listed Super Admin should be an operator")
	}
	if IsPlatformOperator("default", "ops", "Store Manager") {
		t.Errorf("a listed username without Super Admin must not be an operator")
	}
	if IsPlatformOperator(tenantID, "ops", RoleSuperAdmin) {
		t.Errorf("the same username in another client must not be an operator")
	}

	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		('Customer', 'CUST-1', '{"code":"CUST-1","name":"Asha Rao","phone":"9876543210","tier":"Gold","addresses":[{"address":"12 MG Road","city":"Pune"}]}', 'Active', 'admin')`); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	page, err := PlatformRows(tenantID, "documents", PlatformRowFilter{Doctype: "Customer"})
	if err != nil {
		t.Fatalf("PlatformRows: %v", err)
	}
	if page.Total != 1 || len(page.Rows) != 1 {
		t.Fatalf("customer page = %d rows (total %d), want 1", len(page.Rows), page.Total)
	}
	data, _ := page.Rows[0]["data"].(map[string]interface{})
	if data["phone"] != MaskedValue || data["name"] != MaskedValue || data["tier"] != "Gold" {
		t.Errorf("customer data not masked as expected: %v", data)
	}
	addr := data["addresses"].([]interface{})[0].(map[string]interface{})
	if addr["address"] != MaskedValue || addr["city"] != "Pune" {
		t.Errorf("nested address not masked: %v", addr)
	}

	// Search must not match inside masked values.
	if p, _ := PlatformRows(tenantID, "documents", PlatformRowFilter{Q: "98765"}); p.Total != 0 {
		t.Errorf("search reached a masked phone number (%d rows)", p.Total)
	}

	users, err := PlatformRows(tenantID, "users", PlatformRowFilter{})
	if err != nil {
		t.Fatalf("users: %v", err)
	}
	for _, u := range users.Rows {
		if v, ok := u["password_hash"]; ok && v != MaskedValue && v != nil && v != "" {
			t.Errorf("password hash exposed: %v", v)
		}
	}

	// Edit the visible part of the JSON; the masked keys come back as sent
	// (masked) and must keep their stored values.
	newData := `{"code":"CUST-1","name":"` + MaskedValue + `","phone":"` + MaskedValue + `","tier":"Platinum","addresses":[{"address":"` + MaskedValue + `","city":"Mumbai"}]}`
	if _, err := PlatformUpdateRow(tenantID, "documents", map[string]string{"id": "CUST-1"}, map[string]*string{"data": &newData}, "default", "ops"); err != nil {
		t.Fatalf("PlatformUpdateRow: %v", err)
	}
	var stored string
	if err := db.DB.QueryRow(`SELECT data::text FROM ` + schemaName + `.documents WHERE id = 'CUST-1'`).Scan(&stored); err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, want := range []string{`"tier": "Platinum"`, `"city": "Mumbai"`, `"phone": "9876543210"`, `"name": "Asha Rao"`, `"address": "12 MG Road"`} {
		if !strings.Contains(stored, want) {
			t.Errorf("after edit, stored data lacks %s: %s", want, stored)
		}
	}
	var audit int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schemaName + `.audit_logs WHERE action = 'PLATFORM_DATA_EDIT'`).Scan(&audit)
	if audit == 0 {
		t.Errorf("edit was not written to the client's audit log")
	}

	// A plain column edit, cast by the database.
	status := "Inactive"
	if _, err := PlatformUpdateRow(tenantID, "documents", map[string]string{"id": "CUST-1"}, map[string]*string{"status": &status}, "default", "ops"); err != nil {
		t.Fatalf("status edit: %v", err)
	}

	var verr *ValidationError
	id := "CUST-2"
	if _, err := PlatformUpdateRow(tenantID, "documents", map[string]string{"id": "CUST-1"}, map[string]*string{"id": &id}, "default", "ops"); !errors.As(err, &verr) || verr.Code != "PLATFORM-0316" {
		t.Errorf("editing a key column: err = %v, want PLATFORM-0316", err)
	}
	hash := "x"
	if _, err := PlatformUpdateRow(tenantID, "users", map[string]string{"id": "admin"}, map[string]*string{"password_hash": &hash}, "default", "ops"); !errors.As(err, &verr) || verr.Code != "PLATFORM-0316" {
		t.Errorf("editing a password hash: err = %v, want PLATFORM-0316", err)
	}
	if _, err := PlatformUpdateRow(tenantID, "audit_logs", map[string]string{"id": "1"}, map[string]*string{"action": &hash}, "default", "ops"); !errors.As(err, &verr) || verr.Code != "PLATFORM-0316" {
		t.Errorf("editing the audit log: err = %v, want PLATFORM-0316", err)
	}
	if _, err := PlatformRows(tenantID, "no_such_table; drop", PlatformRowFilter{}); err == nil {
		t.Errorf("an invalid table name must be refused")
	}
}
