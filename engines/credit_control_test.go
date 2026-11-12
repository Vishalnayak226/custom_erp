package engines

import (
	"custom_erp/db"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Stage 148.3 (D4) - credit control on the order: an order accepted over the
// customer's limit goes on Credit hold and is released by approval; at invoice
// post only a "stop" customer is refused; a one-time customer is outside
// exposure; the migration's back-fill never guesses a link.
type creditFixture struct {
	t        *testing.T
	tenantID string
	schema   string
}

func newCreditFixture(t *testing.T) *creditFixture {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schemaName) })
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	f := &creditFixture{t: t, tenantID: tenantID, schema: schemaName}
	f.seed("ITEM-CC", "Item", "Active", map[string]interface{}{"code": "ITEM-CC", "name": "Carton", "sale_price": 500.0, "hsn_code": "4819", "tax_treatment": "Exempt"})
	if _, err := PostInventoryLedger(tenantID, "HO", []interface{}{map[string]interface{}{"sku": "ITEM-CC", "qty": 100.0}}, false); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *creditFixture) seed(id, doctype, status string, data map[string]interface{}) {
	f.t.Helper()
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+f.schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
		f.t.Fatalf("seed %s: %v", id, err)
	}
}

func (f *creditFixture) customer(id, name string, extra map[string]interface{}) {
	d := map[string]interface{}{"code": id, "name": name, "status": "Active"}
	for k, v := range extra {
		d[k] = v
	}
	f.seed(id, "Customer", "Active", d)
}

func (f *creditFixture) order(customerName, customerID string, qty int) string {
	f.t.Helper()
	id, err := CreateSalesOrder(f.tenantID, SalesOrderInput{Channel: "Manual", CustomerName: customerName, CustomerID: customerID, ShippingAddress: "12 MG Road, Bengaluru, Karnataka 560001",
		PaymentStatus: "COD", Lines: []SalesOrderLineInput{{SKU: "ITEM-CC", Qty: qty, UnitPrice: 500}}})
	if err != nil {
		f.t.Fatalf("CreateSalesOrder: %v", err)
	}
	return id
}

func (f *creditFixture) field(doctype, id, key string) string {
	var v string
	_ = db.DB.QueryRow(`SELECT COALESCE(data->>'`+key+`', '') FROM `+f.schema+`.documents WHERE doctype = $1 AND id = $2`, doctype, id).Scan(&v)
	return v
}

func codeOf(err error) string {
	if ve, ok := err.(*ValidationError); ok {
		return ve.Code
	}
	return ""
}

func TestStage1483OverLimitOrderGoesOnCreditHold(t *testing.T) {
	f := newCreditFixture(t)
	f.customer("CUST-ACME", "Acme Ltd", map[string]interface{}{"credit_limit": 1500.0})
	f.customer("CUST-OPEN", "Open Line", nil)

	// 1000 of 1500 is within the limit.
	first := f.order("Acme Ltd", "", 2)
	if got := f.field("SalesOrder", first, "customer_id"); got != "CUST-ACME" {
		t.Fatalf("an exact name links the order to its customer, got %q", got)
	}
	if _, err := AcceptPendingOrder(f.tenantID, first, "clerk"); err != nil {
		t.Fatalf("an order within the limit is accepted: %v", err)
	}

	// Another 1000 takes exposure to 2000: held.
	second := f.order("Acme Ltd", "", 2)
	_, err := AcceptPendingOrder(f.tenantID, second, "clerk")
	if codeOf(err) != "FIN-0860" {
		t.Fatalf("accepting over the limit: want FIN-0860, got %v", err)
	}
	if got := f.field("SalesOrder", second, "order_status"); got != "On Hold" {
		t.Errorf("order_status %q, want On Hold", got)
	}
	if got := f.field("SalesOrder", second, "hold_reason"); got != HoldCredit {
		t.Errorf("hold_reason %q, want %s", got, HoldCredit)
	}
	if f.field("SalesOrder", second, "hold_owner") == "" || f.field("SalesOrder", second, "hold_since") == "" {
		t.Errorf("a hold shows its owner and since when")
	}
	reqID := f.field("SalesOrder", second, "credit_release_id")
	var reqStatus string
	_ = db.DB.QueryRow(`SELECT status FROM `+f.schema+`.documents WHERE doctype = 'CreditHoldRelease' AND id = $1`, reqID).Scan(&reqStatus)
	if reqStatus != "Pending Approval" {
		t.Fatalf("a release request waits in the approval inbox, got %q (%q)", reqStatus, reqID)
	}

	// Not released or re-held by hand.
	if err := ReleaseOrderHold(f.tenantID, second); codeOf(err) != "ORDERM-0980" {
		t.Errorf("hand release of a Credit hold: want ORDERM-0980, got %v", err)
	}
	// A second accept attempt files no second request.
	if _, err := AcceptPendingOrder(f.tenantID, second, "clerk"); err == nil {
		t.Errorf("a held order cannot be accepted again")
	}
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM `+f.schema+`.documents WHERE doctype = 'CreditHoldRelease' AND data->>'order_id' = $1`, second).Scan(&n)
	if n != 1 {
		t.Errorf("one request per order, got %d", n)
	}

	// Approval releases it and sends it on to Pick.
	if err := DecideApproval(f.tenantID, "CreditHoldRelease", reqID, "mgr", "Store Manager", "HO", "Approved", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if got := f.field("SalesOrder", second, "order_status"); got == "On Hold" || got == "" {
		t.Errorf("approved: order_status %q, want released", got)
	}
	if got := f.field("SalesOrder", second, "hold_reason"); got != "" {
		t.Errorf("approved: hold_reason %q, want cleared", got)
	}
	if got := f.field("SalesOrder", second, "acceptance_status"); got != AcceptanceAccepted {
		t.Errorf("approved: acceptance_status %q, want Accepted", got)
	}

	// No limit, or no matching customer: never held.
	for _, name := range []string{"Open Line", "Nobody Known"} {
		for i := 0; i < 3; i++ {
			if _, err := AcceptPendingOrder(f.tenantID, f.order(name, "", 2), "clerk"); err != nil {
				t.Fatalf("%s is never held: %v", name, err)
			}
		}
	}
}

func TestStage1483OneTimeCustomerLeftOutOfExposure(t *testing.T) {
	f := newCreditFixture(t)
	f.customer("CUST-WALK", "Walk Buyer", map[string]interface{}{"credit_limit": 100.0, "one_time": "Yes"})
	f.customer("CUST-REG", "Regular Buyer", map[string]interface{}{"credit_limit": 100.0})
	f.seed("INV-WALK", "SalesInvoice", "Approved", map[string]interface{}{"customer": "Walk Buyer", "customer_id": "CUST-WALK", "total_amount": 5000.0})
	f.seed("INV-REG", "SalesInvoice", "Approved", map[string]interface{}{"customer": "Regular Buyer", "customer_id": "CUST-REG", "total_amount": 5000.0})

	if exp, err := CustomerCreditExposure(f.tenantID, "CUST-WALK", "", 1000); err != nil || exp.Total != 0 || !exp.OneTime {
		t.Errorf("a one-time customer has no exposure: %+v %v", exp, err)
	}
	if exp, err := CustomerCreditExposure(f.tenantID, "CUST-REG", "", 1000); err != nil || exp.Total != 6000 {
		t.Errorf("a regular customer's exposure is receivable + this order: %+v %v", exp, err)
	}
	if _, err := AcceptPendingOrder(f.tenantID, f.order("Walk Buyer", "", 2), "clerk"); err != nil {
		t.Errorf("a one-time customer is never held: %v", err)
	}
	if _, err := AcceptPendingOrder(f.tenantID, f.order("Regular Buyer", "", 2), "clerk"); codeOf(err) != "FIN-0860" {
		t.Errorf("a regular customer over the limit is held, got %v", err)
	}
}

func TestStage1483StopCustomerInvoiceRefused(t *testing.T) {
	f := newCreditFixture(t)
	f.customer("CUST-STOP", "Stop Co", map[string]interface{}{"credit_limit": 500.0, "credit_stop": "Yes"})
	f.customer("CUST-WARN", "Warn Co", map[string]interface{}{"credit_limit": 500.0, "credit_stop": "No"})
	f.customer("CUST-FREE", "Free Co", nil)
	draft := func(id, name, cid string) {
		d := map[string]interface{}{"code": id, "customer": name, "total_amount": 600.0}
		if cid != "" {
			d["customer_id"] = cid
		}
		f.seed(id, "SalesInvoice", "Draft", d)
	}

	draft("INV-STOP", "Stop Co", "CUST-STOP")
	if _, err := PostSalesInvoice(f.tenantID, "INV-STOP", "tester"); codeOf(err) != "FIN-0861" {
		t.Errorf("a stop customer's invoice over the limit is refused with FIN-0861, got %v", err)
	}
	// An invoice with no link still reaches the customer by its typed name.
	draft("INV-STOP2", "Stop Co", "")
	if _, err := PostSalesInvoice(f.tenantID, "INV-STOP2", "tester"); codeOf(err) != "FIN-0861" {
		t.Errorf("the name-based check still works without a link, got %v", err)
	}
	draft("INV-WARN", "Warn Co", "CUST-WARN")
	if _, err := PostSalesInvoice(f.tenantID, "INV-WARN", "tester"); err != nil {
		t.Fatalf("a non-stop customer is only warned: %v", err)
	}
	if !strings.Contains(f.field("SalesInvoice", "INV-WARN", "credit_warning"), "credit limit") {
		t.Errorf("the posted invoice carries the warning, got %q", f.field("SalesInvoice", "INV-WARN", "credit_warning"))
	}
	draft("INV-FREE", "Free Co", "CUST-FREE")
	if _, err := PostSalesInvoice(f.tenantID, "INV-FREE", "tester"); err != nil {
		t.Errorf("no limit, no check: %v", err)
	}
}

func TestStage1483BackfillLeavesAmbiguousNameUnlinked(t *testing.T) {
	f := newCreditFixture(t)
	f.customer("CUST-ONLY", "Only One", map[string]interface{}{"credit_limit": 900.0})
	f.customer("CUST-TWIN1", "Twin Co", nil)
	f.customer("CUST-TWIN2", "Twin Co", nil)
	f.customer("CUST-NOLIM", "No Limit Co", nil)
	f.seed("SO-ONLY", "SalesOrder", "Reserved", map[string]interface{}{"customer_name": "Only One"})
	f.seed("SO-TWIN", "SalesOrder", "Reserved", map[string]interface{}{"customer_name": "Twin Co"})
	f.seed("SO-NONE", "SalesOrder", "Reserved", map[string]interface{}{"customer_name": "Nobody At All"})
	f.seed("SO-SET", "SalesOrder", "Reserved", map[string]interface{}{"customer_name": "Only One", "customer_id": "CUST-TWIN1"})
	f.seed("INV-ONLY", "SalesInvoice", "Draft", map[string]interface{}{"customer": "Only One"})
	f.seed("INV-TWIN", "SalesInvoice", "Draft", map[string]interface{}{"customer": "Twin Co"})

	sqlText, err := os.ReadFile("../db/migrations/migrations_stage148_3_credit_control.sql")
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ { // re-running changes nothing
		if _, err := db.DB.Exec(string(sqlText)); err != nil {
			t.Fatalf("migration run %d: %v", run+1, err)
		}
	}
	want := map[string]string{"SO-ONLY": "CUST-ONLY", "SO-TWIN": "", "SO-NONE": "", "SO-SET": "CUST-TWIN1"}
	for id, w := range want {
		if got := f.field("SalesOrder", id, "customer_id"); got != w {
			t.Errorf("order %s linked to %q, want %q", id, got, w)
		}
	}
	if got := f.field("SalesInvoice", "INV-ONLY", "customer_id"); got != "CUST-ONLY" {
		t.Errorf("invoice with a unique name linked to %q", got)
	}
	if got := f.field("SalesInvoice", "INV-TWIN", "customer_id"); got != "" {
		t.Errorf("invoice with an ambiguous name must stay unlinked, got %q", got)
	}
	if got := f.field("Customer", "CUST-ONLY", "credit_stop"); got != "Yes" {
		t.Errorf("a customer with a limit keeps blocking, credit_stop %q", got)
	}
	if got := f.field("Customer", "CUST-NOLIM", "credit_stop"); got != "" {
		t.Errorf("a customer without a limit is left alone, credit_stop %q", got)
	}
}
