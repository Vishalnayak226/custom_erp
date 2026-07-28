package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 26.7.9/26.7.10/26.7.11 (CRM/Loyalty Sprint P2 follow-up) engine
// tests. Kept in its own file, same convention wms_enterprise_test.go/
// reports_stage26_10_test.go already established for staying out of
// engines_test.go while other concurrent work is mid-editing it.
func TestCRMAnalytics(t *testing.T) {
	connStr := "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
	db.InitDB(connStr)

	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}

	t.Run("MergeCustomers", func(t *testing.T) {
		primaryID := "CUST-MERGE-PRIMARY"
		dupID := "CUST-MERGE-DUP"
		cartID := "CART-MERGE-TEST"
		invoiceID := "INV-MERGE-TEST"
		voucherID := "VOUCH-MERGE-TEST"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1,$2,$3,$4,$5)", primaryID, dupID, cartID, invoiceID, voucherID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".loyalty_point_ledger WHERE customer_id IN ($1,$2)", primaryID, dupID)

		for _, id := range []string{primaryID, dupID} {
			custData, _ := json.Marshal(map[string]interface{}{"code": id, "name": id})
			if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2, 'Active', 'system')", id, custData); err != nil {
				t.Fatalf("failed to seed customer %s: %v", id, err)
			}
		}
		cartData, _ := json.Marshal(map[string]interface{}{"code": cartID, "customer_id": dupID, "total_sale_price": 500})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')", cartID, cartData); err != nil {
			t.Fatalf("failed to seed POSCart: %v", err)
		}
		invData, _ := json.Marshal(map[string]interface{}{"code": invoiceID, "customer": dupID, "total_amount": 1200})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Active', 'system')", invoiceID, invData); err != nil {
			t.Fatalf("failed to seed SalesInvoice: %v", err)
		}
		voucherData, _ := json.Marshal(map[string]interface{}{"code": voucherID, "customer_id": dupID, "discount_type": "Flat", "discount_value": 50})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Voucher', $2, 'Active', 'system')", voucherID, voucherData); err != nil {
			t.Fatalf("failed to seed Voucher: %v", err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".loyalty_point_ledger (customer_id, transaction_type, points) VALUES ($1, 'Earn', 100)", dupID); err != nil {
			t.Fatalf("failed to seed loyalty ledger: %v", err)
		}

		if err := MergeCustomers(tenantID, primaryID, dupID, "tester"); err != nil {
			t.Fatalf("MergeCustomers failed: %v", err)
		}

		var cartCustomer, invCustomer, voucherCustomer string
		if err := db.DB.QueryRow("SELECT data->>'customer_id' FROM "+schema+".documents WHERE id = $1", cartID).Scan(&cartCustomer); err != nil || cartCustomer != primaryID {
			t.Errorf("expected POSCart reassigned to %s, got %q (err=%v)", primaryID, cartCustomer, err)
		}
		if err := db.DB.QueryRow("SELECT data->>'customer' FROM "+schema+".documents WHERE id = $1", invoiceID).Scan(&invCustomer); err != nil || invCustomer != primaryID {
			t.Errorf("expected SalesInvoice reassigned to %s, got %q (err=%v)", primaryID, invCustomer, err)
		}
		if err := db.DB.QueryRow("SELECT data->>'customer_id' FROM "+schema+".documents WHERE id = $1", voucherID).Scan(&voucherCustomer); err != nil || voucherCustomer != primaryID {
			t.Errorf("expected Voucher reassigned to %s, got %q (err=%v)", primaryID, voucherCustomer, err)
		}
		var loyaltyCount int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".loyalty_point_ledger WHERE customer_id = $1", primaryID).Scan(&loyaltyCount); err != nil || loyaltyCount != 1 {
			t.Errorf("expected loyalty ledger row reassigned to %s, count=%d (err=%v)", primaryID, loyaltyCount, err)
		}
		var dupStatus, mergedInto string
		if err := db.DB.QueryRow("SELECT status, data->>'merged_into' FROM "+schema+".documents WHERE id = $1", dupID).Scan(&dupStatus, &mergedInto); err != nil {
			t.Fatalf("failed to read duplicate customer: %v", err)
		}
		if dupStatus != "Merged" || mergedInto != primaryID {
			t.Errorf("expected duplicate status=Merged merged_into=%s, got status=%s merged_into=%s", primaryID, dupStatus, mergedInto)
		}

		// Merging an already-merged record must be rejected.
		if err := MergeCustomers(tenantID, primaryID, dupID, "tester"); err == nil {
			t.Error("expected merging an already-Merged customer to fail")
		}
	})

	t.Run("CustomerLifetimeValueAndChurn", func(t *testing.T) {
		custID := "CUST-CLV-TEST"
		cartID := "CART-CLV-TEST"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1,$2)", custID, cartID)
		custData, _ := json.Marshal(map[string]interface{}{"code": custID, "name": custID})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2, 'Active', 'system')", custID, custData); err != nil {
			t.Fatalf("failed to seed customer: %v", err)
		}
		cartData, _ := json.Marshal(map[string]interface{}{"code": cartID, "customer_id": custID, "total_sale_price": 777})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')", cartID, cartData); err != nil {
			t.Fatalf("failed to seed POSCart: %v", err)
		}

		clv, err := GetCustomerLifetimeValue(tenantID, 1) // churn threshold 1 day - this fresh cart shouldn't count as churned
		if err != nil {
			t.Fatalf("GetCustomerLifetimeValue failed: %v", err)
		}
		var found *CustomerLifetimeValue
		for i := range clv {
			if clv[i].CustomerID == custID {
				found = &clv[i]
			}
		}
		if found == nil {
			t.Fatalf("expected a CLV row for %s, got %+v", custID, clv)
		}
		if found.LifetimeValue != 777 {
			t.Errorf("expected lifetime_value=777, got %v", found.LifetimeValue)
		}
		if found.ChurnFlag {
			t.Errorf("expected a just-created order to not be flagged churned yet")
		}
	})
}
