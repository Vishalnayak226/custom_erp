package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

// wsFix is the shared fixture of the 148.31 / 148.32 tests: a fresh tenant with
// a Karnataka store, two taxable items (18%), and helpers for customers, orders
// and vendors. Everything lives in the tenant, which the test drops.
type wsFix struct {
	t        *testing.T
	tenantID string
	schema   string
	insert   func(id, doctype, status, createdBy string, data map[string]interface{})
	seq      int
}

type wsLine struct {
	sku   string
	qty   int
	price float64
	extra map[string]interface{}
}

func newWSFix(t *testing.T, moduleOn bool) *wsFix {
	t.Helper()
	tenantID, schema, insert := stage140Tenant(t)
	f := &wsFix{t: t, tenantID: tenantID, schema: schema, insert: insert}
	insert("WS-HQ", "Location", "Active", "system", map[string]interface{}{"code": "WS-HQ", "name": "Store", "state": "Karnataka"})
	for _, sku := range []string{"WS-A", "WS-B"} {
		insert(sku, "Item", "Active", "system", map[string]interface{}{"code": sku, "name": sku, "gst_rate": 18.0, "hsn_code": "1234",
			"standard_cost": 60.0, "tax_treatment": "Taxable", "status": "Active", "item_type": "Product"})
	}
	if moduleOn {
		f.module(true)
	}
	return f
}

func (f *wsFix) module(on bool) {
	f.t.Helper()
	if err := SetModuleEntitlement(f.tenantID, ModuleWholesale, on, "test"); err != nil {
		f.t.Fatal(err)
	}
}

func (f *wsFix) customer(id string, extra map[string]interface{}) {
	f.t.Helper()
	data := map[string]interface{}{"code": id, "name": "Cust " + id, "status": "Active"}
	for k, v := range extra {
		data[k] = v
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = $1 AND id = $2`, f.schema), "Customer", id) // a test may re-save a customer
	f.insert(id, "Customer", "Active", "system", data)
}

func (f *wsFix) vendor(id string) {
	f.t.Helper()
	f.insert(id, "Vendor", "Active", "system", map[string]interface{}{"code": id, "name": "Vendor " + id, "state": "Maharashtra", "status": "Active"})
}

// order inserts a Shipped sales order with its lines and returns its id.
func (f *wsFix) order(customerID, state, po, status string, lines ...wsLine) string {
	f.t.Helper()
	f.seq++
	id := fmt.Sprintf("SO-WS%d-%s", f.seq, customerID)
	total := 0.0
	for _, l := range lines {
		total += l.price * float64(l.qty)
	}
	f.insert(id, "SalesOrder", status, "system", map[string]interface{}{
		"code": id, "customer_id": customerID, "customer_name": "Cust " + customerID, "shipping_address": "1 Main Rd " + state,
		"shipping_city": "Town", "shipping_pincode": "560001", "shipping_state": state, "customer_reference": po,
		"total_amount": total, "order_status": status, "payment_status": "Confirmed"})
	for i, l := range lines {
		data := map[string]interface{}{"code": fmt.Sprintf("%s-L%d", id, i+1), "order_id": id, "sku": l.sku, "qty": l.qty,
			"unit_price": l.price, "location_code": "WS-HQ", "line_status": "Dispatched", "shipped_qty": l.qty}
		for k, v := range l.extra {
			data[k] = v
		}
		f.insert(fmt.Sprintf("%s-L%d", id, i+1), "SalesOrderLine", "Dispatched", "system", data)
	}
	return id
}

func (f *wsFix) doc(doctype, id string) map[string]interface{} {
	f.t.Helper()
	var raw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype=$1 AND id=$2`, f.schema), doctype, id).Scan(&raw); err != nil {
		f.t.Fatalf("%s %s: %v", doctype, id, err)
	}
	var m map[string]interface{}
	_ = json.Unmarshal(raw, &m)
	return m
}

func (f *wsFix) gl(docType, docID, account string) (debit, credit int64) {
	return stage140GL(f.t, f.schema, docType, docID, account)
}

func wsHasField(fields []FieldMeta, name string) bool {
	for _, fm := range fields {
		if fm.Fieldname == name {
			return true
		}
	}
	return false
}
