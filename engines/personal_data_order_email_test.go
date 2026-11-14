package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
)

// 49.6.2 (owner's answers): the hourly sweep removes the buyer's e-mail from a
// Delivered or Closed order and leaves it on one still on its way; a billing
// address equal to the shipping address is not stored.
func TestStage4962DeliveredOrderLosesTheBuyerEmail(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() { db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id LIKE 'SO-4962E-%%'`, schema)) }
	cleanup()
	t.Cleanup(cleanup)
	seed := map[string]string{"SO-4962E-DEL": "Delivered", "SO-4962E-CLO": "Closed", "SO-4962E-SHP": "Shipped"}
	for id, st := range seed {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, 'SalesOrder', jsonb_build_object('order_status', $2::text, 'customer_email', 'buyer@example.test', 'customer_name', 'Buyer'), $2, 'admin')`, schema), id, st); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	if _, err := clearDeliveredOrderEmails(tenantID, schema); err != nil {
		t.Fatal(err)
	}
	for id, st := range seed {
		var email, name string
		var by string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'customer_email', ''), COALESCE(data->>'customer_name', ''), COALESCE(updated_by, '') FROM %s.documents WHERE id = $1`, schema), id).Scan(&email, &name, &by); err != nil {
			t.Fatal(err)
		}
		if name != "Buyer" {
			t.Errorf("%s lost the customer name", id)
		}
		if gone := email == ""; gone != (st != "Shipped") {
			t.Errorf("%s (%s): e-mail %q after the sweep", id, st, email)
		}
		if st != "Shipped" && by != "system" {
			t.Errorf("%s: cleared by %q, want system", id, by)
		}
	}

	for _, c := range []struct{ bill, ship, want string }{
		{"12 MG Road, Pune", "12  mg road, PUNE", ""},
		{"", "12 MG Road", ""},
		{"Office 4, Mumbai", "12 MG Road, Pune", "Office 4, Mumbai"},
	} {
		if got := billingUnlessShipping(c.bill, c.ship); got != c.want {
			t.Errorf("billingUnlessShipping(%q, %q) = %q, want %q", c.bill, c.ship, got, c.want)
		}
	}
}
