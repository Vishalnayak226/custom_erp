package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func TestVoucherRedemption(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Voucher' AND id LIKE 'TEST-VOUCHER-%'")
	}
	cleanup()
	defer cleanup()

	seedVoucher := func(id string, v map[string]interface{}) {
		v["id"] = id
		bytes, _ := json.Marshal(v)
		status, _ := v["status"].(string)
		if _, err := db.DB.Exec(
			"INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Voucher', $2, $3, 'system')",
			id, bytes, status); err != nil {
			t.Fatalf("seed voucher %s: %v", id, err)
		}
	}

	t.Run("Percentage discount computed and capped at cart amount", func(t *testing.T) {
		cleanup()
		seedVoucher("TEST-VOUCHER-PCT", map[string]interface{}{
			"code": "PCT20", "discount_type": "Percentage", "discount_value": 20, "max_uses": 1, "status": "Active",
		})
		v, discount, err := ValidateVoucher(tenantID, "PCT20", "", 1000)
		if err != nil {
			t.Fatalf("ValidateVoucher: %v", err)
		}
		if discount != 200 {
			t.Fatalf("expected discount=200, got %d", discount)
		}
		if v.Code != "PCT20" {
			t.Fatalf("expected code PCT20, got %s", v.Code)
		}
		// A Flat/Percentage discount can never exceed the cart total.
		_, discount2, err := ValidateVoucher(tenantID, "PCT20", "", 5)
		if err != nil {
			t.Fatalf("ValidateVoucher (small cart): %v", err)
		}
		if discount2 != 1 {
			t.Fatalf("expected discount=1 (20%% of 5), got %d", discount2)
		}
	})

	t.Run("Flat discount capped at cart amount", func(t *testing.T) {
		cleanup()
		seedVoucher("TEST-VOUCHER-FLAT", map[string]interface{}{
			"code": "FLAT500", "discount_type": "Flat", "discount_value": 500, "max_uses": 1, "status": "Active",
		})
		_, discount, err := ValidateVoucher(tenantID, "FLAT500", "", 300)
		if err != nil {
			t.Fatalf("ValidateVoucher: %v", err)
		}
		if discount != 300 {
			t.Fatalf("expected discount capped at cart amount 300, got %d", discount)
		}
	})

	t.Run("expired voucher is rejected", func(t *testing.T) {
		cleanup()
		seedVoucher("TEST-VOUCHER-EXP", map[string]interface{}{
			"code": "EXPIRED1", "discount_type": "Flat", "discount_value": 100, "expiry_date": "2020-01-01", "status": "Active",
		})
		if _, _, err := ValidateVoucher(tenantID, "EXPIRED1", "", 1000); err == nil {
			t.Fatalf("expected an expired voucher to be rejected")
		}
	})

	t.Run("customer-restricted voucher rejects a different customer", func(t *testing.T) {
		cleanup()
		seedVoucher("TEST-VOUCHER-CUST", map[string]interface{}{
			"code": "VIPONLY", "discount_type": "Flat", "discount_value": 100, "customer_id": "CUST-VIP", "status": "Active",
		})
		if _, _, err := ValidateVoucher(tenantID, "VIPONLY", "CUST-OTHER", 1000); err == nil {
			t.Fatalf("expected a customer-restricted voucher to reject a different customer")
		}
		if _, discount, err := ValidateVoucher(tenantID, "VIPONLY", "CUST-VIP", 1000); err != nil || discount != 100 {
			t.Fatalf("expected the restricted customer to succeed with discount=100, got discount=%d err=%v", discount, err)
		}
	})

	t.Run("redemption increments used_count and exhausts at max_uses", func(t *testing.T) {
		cleanup()
		seedVoucher("TEST-VOUCHER-MAX", map[string]interface{}{
			"code": "ONEUSE", "discount_type": "Flat", "discount_value": 50, "max_uses": 1, "used_count": 0, "status": "Active",
		})
		discount, err := RedeemVoucher(tenantID, "ONEUSE", "", "REF-1", 1000)
		if err != nil {
			t.Fatalf("RedeemVoucher: %v", err)
		}
		if discount != 50 {
			t.Fatalf("expected discount=50, got %d", discount)
		}
		var status string
		var usedCount int
		if err := db.DB.QueryRow("SELECT status, (data->>'used_count')::int FROM "+schema+".documents WHERE id='TEST-VOUCHER-MAX'").Scan(&status, &usedCount); err != nil {
			t.Fatalf("query voucher: %v", err)
		}
		if status != "Exhausted" || usedCount != 1 {
			t.Fatalf("expected status=Exhausted used_count=1, got status=%s used_count=%d", status, usedCount)
		}
		if _, err := RedeemVoucher(tenantID, "ONEUSE", "", "REF-2", 1000); err == nil {
			t.Fatalf("expected a second redemption of an Exhausted voucher to be rejected")
		}
	})
}
