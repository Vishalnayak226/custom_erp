package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Stage 85.C: the customer at the till. Pinned: one mobile number is one
// customer (a merged or deleted record no longer blocks it); a store's
// walk-in customer is made once, carries a placeholder number and earns
// nothing; PAN comes from the GSTIN; and India's high-value rules (address
// at 50,000 to an unregistered buyer, PAN above 2,00,000 or Form 97, cash of
// 2,00,000 refused) are enforced.
func TestWalkInPlaceholder(t *testing.T) {
	if walkInPlaceholder(1) != "1111111111" || walkInPlaceholder(9) != "9999999999" || walkInPlaceholder(12) != "9000000012" {
		t.Errorf("placeholders: %s %s %s", walkInPlaceholder(1), walkInPlaceholder(9), walkInPlaceholder(12))
	}
}

func TestPOSCustomerAtTheTill(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	if TenantPhoneCountry(tenantID) != "IN" {
		t.Skip("the high-value and PAN rules pinned here are India's")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const store = "S85C-STORE"
	phone := fmt.Sprintf("9%09d", time.Now().UnixNano()%1000000000)
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id LIKE 'S85C-%' OR (doctype = 'Customer' AND data->>'walk_in_store' = $1)", store)
		db.DB.Exec("DELETE FROM " + schema + ".loyalty_point_ledger WHERE reference_id = 'S85C-BILL'")
	}
	cleanup()
	defer cleanup()
	raw, _ := json.Marshal(map[string]interface{}{"code": store, "name": "S85C Store", "type": "Store", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", store, raw); err != nil {
		t.Fatalf("location: %v", err)
	}

	// Walk-in: made once, never earns.
	w1, err := EnsureWalkInCustomer(tenantID, store)
	if err != nil {
		t.Fatalf("walk-in: %v", err)
	}
	w2, _ := EnsureWalkInCustomer(tenantID, store)
	if w1.ID == "" || w1.ID != w2.ID || !w1.WalkIn || w1.Phone != "" {
		t.Fatalf("walk-in should be one record, flagged, its placeholder never shown: %+v / %+v", w1, w2)
	}
	if !IsWalkInCustomer(tenantID, w1.ID) {
		t.Error("IsWalkInCustomer")
	}
	if err := EarnLoyaltyPoints(tenantID, w1.ID, 5000, "S85C-BILL"); err != nil {
		t.Fatalf("earn: %v", err)
	}
	var earned int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".loyalty_point_ledger WHERE reference_id = 'S85C-BILL'").Scan(&earned)
	if earned != 0 {
		t.Error("the walk-in customer must not collect points")
	}

	// A customer: phone cleaned, GSTIN upper-cased, PAN taken from it.
	payload := map[string]interface{}{"name": "S85C Buyer", "phone": "+91 " + phone[:5] + " " + phone[5:], "gstin": "29aabcu9603r1zj"}
	if err := validateCustomerMasterRules(tenantID, "S85C-CUST1", payload); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if payload["phone"] != phone || payload["gstin"] != "29AABCU9603R1ZJ" || payload["pan"] != "AABCU9603R" {
		t.Fatalf("cleaned payload: %v", payload)
	}
	data, _ := json.Marshal(payload)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('S85C-CUST1', 'Customer', $1, 'Active', 'system')", data); err != nil {
		t.Fatalf("insert: %v", err)
	}
	found, ok, _, err := LookupCustomerByPhone(tenantID, phone[:5]+"-"+phone[5:])
	if err != nil || !ok || found.ID != "S85C-CUST1" {
		t.Fatalf("lookup by phone: %+v %v %v", found, ok, err)
	}
	var ve *ValidationError
	if err := validateCustomerMasterRules(tenantID, "S85C-CUST2", map[string]interface{}{"name": "Copy", "phone": phone}); !errors.As(err, &ve) || ve.Code != "CUSTOM-0133" {
		t.Errorf("second customer on one number: want CUSTOM-0133, got %v", err)
	}
	if err := validateCustomerMasterRules(tenantID, "S85C-CUST2", map[string]interface{}{"name": "Bad", "pan": "ABC"}); !errors.As(err, &ve) || ve.Code != "CUSTOM-0870" {
		t.Errorf("bad PAN: want CUSTOM-0870, got %v", err)
	}
	if err := validateCustomerMasterRules(tenantID, "S85C-CUST2", map[string]interface{}{"name": "Bad", "gstin": "29AABCU9603R1ZK"}); !errors.As(err, &ve) || ve.Code != "CUSTOM-0869" {
		t.Errorf("bad GSTIN check digit: want CUSTOM-0869, got %v", err)
	}

	// High value. The GSTIN buyer has no address: registered, so 60,000 is fine.
	if err := CheckHighValueSale(tenantID, "S85C-CUST1", 60000, 0, false); err != nil {
		t.Errorf("registered buyer at 60,000 needs no address: %v", err)
	}
	if err := CheckHighValueSale(tenantID, w1.ID, 60000, 0, false); !errors.As(err, &ve) || ve.Code != "POSOFF-0866" {
		t.Errorf("walk-in at 60,000: want POSOFF-0866, got %v", err)
	}
	// An unregistered buyer, no address, then with one.
	plain, _ := json.Marshal(map[string]interface{}{"name": "S85C Plain", "phone": "8000000085"})
	db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('S85C-CUST3', 'Customer', $1, 'Active', 'system')", plain)
	if err := CheckHighValueSale(tenantID, "S85C-CUST3", 50000, 0, false); !errors.As(err, &ve) || ve.Code != "POSOFF-0866" {
		t.Errorf("unregistered at 50,000 with no address: want POSOFF-0866, got %v", err)
	}
	if err := CheckHighValueSale(tenantID, "S85C-CUST3", 49999, 0, false); err != nil {
		t.Errorf("below 50,000: %v", err)
	}
	addr, _ := json.Marshal(map[string]interface{}{"name": "S85C Plain", "phone": "8000000085", "address": "1 MG Road", "city": "Bengaluru", "state": "Karnataka"})
	db.DB.Exec("UPDATE "+schema+".documents SET data = $1 WHERE id = 'S85C-CUST3'", addr)
	if err := CheckHighValueSale(tenantID, "S85C-CUST3", 150000, 0, false); err != nil {
		t.Errorf("with an address at 1,50,000: %v", err)
	}
	if err := CheckHighValueSale(tenantID, "S85C-CUST3", 250000, 0, false); !errors.As(err, &ve) || ve.Code != "POSOFF-0867" {
		t.Errorf("above 2,00,000 without PAN: want POSOFF-0867, got %v", err)
	}
	if err := CheckHighValueSale(tenantID, "S85C-CUST3", 250000, 0, true); err != nil {
		t.Errorf("Form 97 stands in for the PAN: %v", err)
	}
	if err := CheckHighValueSale(tenantID, "S85C-CUST1", 250000, 200000, false); !errors.As(err, &ve) || ve.Code != "POSOFF-0868" {
		t.Errorf("cash of 2,00,000: want POSOFF-0868, got %v", err)
	}
	if err := CheckHighValueSale(tenantID, "S85C-CUST1", 250000, 199999, false); err != nil {
		t.Errorf("cash just under the limit, PAN from the GSTIN: %v", err)
	}
}
