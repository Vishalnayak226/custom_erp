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

// Stage 60.6 record tables: each record type reads as its own table (one
// column per defined field), and an edit writes typed values back into that
// record's JSON - a Number stays a number - with hidden fields refused.
func TestPlatformRecordTablesReadAndEdit(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		('Item', 'SKU-1', '{"code":"SKU-1","name":"Blue Shirt","sale_price":499,"legacy_note":"x"}', 'Active', 'admin'),
		('Customer', 'CUST-9', '{"code":"CUST-9","name":"Ravi","phone":"9000000001"}', 'Active', 'admin')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	types, err := PlatformRecordTypes(tenantID)
	if err != nil {
		t.Fatalf("PlatformRecordTypes: %v", err)
	}
	found := false
	for _, rt := range types {
		if rt.Doctype == "Item" && rt.Rows == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("Item should be listed as a table with 1 row")
	}

	page, err := PlatformRecords(tenantID, "Item", PlatformRowFilter{Q: "blue"})
	if err != nil {
		t.Fatalf("PlatformRecords: %v", err)
	}
	if page.Total != 1 || page.Rows[0]["name"] != "Blue Shirt" {
		t.Fatalf("Item table by name search: total %d rows %v", page.Total, page.Rows)
	}
	var priceCol *PlatformColumn
	for i := range page.Columns {
		if page.Columns[i].Name == "sale_price" {
			priceCol = &page.Columns[i]
		}
	}
	if priceCol == nil || priceCol.Label == "" || !priceCol.Editable {
		t.Errorf("sale_price should be an editable, labelled column: %+v", priceCol)
	}
	if other, _ := page.Rows[0]["other_data"].(map[string]interface{}); other["legacy_note"] != "x" {
		t.Errorf("an undefined key should appear under other_data: %v", page.Rows[0]["other_data"])
	}

	price, name, status := "550.5", "Blue Shirt XL", "Inactive"
	if _, err := PlatformUpdateRecord(tenantID, "Item", "SKU-1", map[string]*string{"sale_price": &price, "name": &name, "status": &status}, "default", "ops"); err != nil {
		t.Fatalf("PlatformUpdateRecord: %v", err)
	}
	var priceType, storedName, storedStatus string
	if err := db.DB.QueryRow(`SELECT jsonb_typeof(data->'sale_price'), data->>'name', status FROM `+schemaName+`.documents WHERE id = 'SKU-1'`).Scan(&priceType, &storedName, &storedStatus); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if priceType != "number" || storedName != "Blue Shirt XL" || storedStatus != "Inactive" {
		t.Errorf("after edit: price type %s, name %q, status %q", priceType, storedName, storedStatus)
	}

	bad := "abc"
	var verr *ValidationError
	if _, err := PlatformUpdateRecord(tenantID, "Item", "SKU-1", map[string]*string{"sale_price": &bad}, "default", "ops"); !errors.As(err, &verr) {
		t.Errorf("a non-number price must be refused, got %v", err)
	}
	phone := "1"
	if _, err := PlatformUpdateRecord(tenantID, "Customer", "CUST-9", map[string]*string{"phone": &phone}, "default", "ops"); !errors.As(err, &verr) || verr.Code != "PLATFORM-0316" {
		t.Errorf("editing a hidden customer phone: err = %v, want PLATFORM-0316", err)
	}
	cust, err := PlatformRecords(tenantID, "Customer", PlatformRowFilter{})
	if err != nil || cust.Rows[0]["phone"] != MaskedValue || cust.Rows[0]["name"] != MaskedValue {
		t.Errorf("customer fields must be masked: %v %v", err, cust.Rows)
	}
	if p, _ := PlatformRecords(tenantID, "Customer", PlatformRowFilter{Q: "Ravi"}); p.Total != 0 {
		t.Errorf("a customer must not be findable by its hidden name")
	}
}
