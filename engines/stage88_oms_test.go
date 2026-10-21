package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 88.3: the Pending gate (accept / reject and the answer to the
// channel) and the two bills (OMS bill at Pack, ERP bill at handover).

const (
	s88Order    = "TEST-S88-ORDER"
	s88Order2   = "TEST-S88-ORDER-2"
	s88SKU      = "TEST-S88-SKU"
	s88Location = "TEST-S88-LOC"
	s88Reason   = "TEST-S88-CANCEL"
)

func s88Fixture(t *testing.T) string {
	t.Helper()
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		if _, err := db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype IN ('FulfillmentTask', 'SalesOrderLine') AND data->>'order_id' IN ($1, $2)`,
			s88Order, s88Order2); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
		if _, err := db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id IN ($1, $2, $3, $4)`, s88Order, s88Order2, s88SKU, s88Reason); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	}
	cleanup()
	t.Cleanup(cleanup)
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		data["code"] = id
		body, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'system')",
			id, doctype, body, status); err != nil {
			t.Fatalf("insert %s %s: %v", doctype, id, err)
		}
	}
	insert(s88SKU, "Item", "Active", map[string]interface{}{"name": "Stage 88 item", "gst_rate": 18.0, "sale_price": 100.0})
	insert(s88Reason, "ReasonCode", "Active", map[string]interface{}{"category": "Cancellation", "label": "Test cancel"})
	for _, id := range []string{s88Order, s88Order2} {
		insert(id, "SalesOrder", "Reserved", map[string]interface{}{
			"customer_name": "Stage 88 Customer", "order_status": "Reserved", "payment_status": "Confirmed",
			"channel": "TEST-S88-CHANNEL", "channel_order_id": "CH-" + id, "acceptance_status": AcceptancePending, "total_amount": 100.0,
		})
	}
	insert(s88Order+"-L1", "SalesOrderLine", "Reserved", map[string]interface{}{
		"order_id": s88Order, "sku": s88SKU, "qty": 1, "unit_price": 100.0, "location_code": s88Location, "line_status": "Reserved",
	})
	return schema
}

func s88OrderData(t *testing.T, id string) map[string]interface{} {
	t.Helper()
	_, data, err := fetchSalesOrder("default", id)
	if err != nil {
		t.Fatalf("fetch %s: %v", id, err)
	}
	return data
}

// TestPendingGateAccept: Accept releases the order to Pick, records the
// answer, and - with no Channel set up - shows the push as Not connected
// rather than failing the accept or retrying forever.
func TestPendingGateAccept(t *testing.T) {
	s88Fixture(t)
	result, err := BulkOrderAction("default", "accept", []string{s88Order}, "", "tester")
	if err != nil {
		t.Fatalf("BulkOrderAction accept: %v", err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("accept refused: %v", result.Failed)
	}
	data := s88OrderData(t, s88Order)
	if data["order_status"] != OrderStatusReleased {
		t.Errorf("accepted order is %v, want %s (sent to Pick)", data["order_status"], OrderStatusReleased)
	}
	if data["acceptance_status"] != AcceptanceAccepted {
		t.Errorf("acceptance_status = %v, want Accepted", data["acceptance_status"])
	}
	push, _ := data["channel_push"].(map[string]interface{})
	if push["decision"] != OrderDecisionAccepted || push["status"] != "Not connected" {
		t.Errorf("channel_push = %v, want Accepted / Not connected", push)
	}
	// Retry is refused while the channel is still not connected - and says why.
	if err := RetryChannelDecisionPush("default", s88Order, "tester"); err == nil {
		t.Error("a retry to a channel that is still not connected reported success")
	}
	// A second accept of a released order is refused, not re-released.
	if again, _ := BulkOrderAction("default", "accept", []string{s88Order}, "", "tester"); len(again.Failed) != 1 {
		t.Errorf("accepting a released order again was not refused: %+v", again)
	}
}

// TestPendingGateReject: Reject cancels under the cancellation rules (a reason
// code is required) and records the answer for the channel.
func TestPendingGateReject(t *testing.T) {
	s88Fixture(t)
	if result, _ := BulkOrderAction("default", "reject", []string{s88Order2}, "", "tester"); len(result.Failed) != 1 {
		t.Fatalf("a reject with no reason was accepted: %+v", result)
	}
	result, err := BulkOrderAction("default", "reject", []string{s88Order2}, s88Reason, "tester")
	if err != nil || len(result.Failed) != 0 {
		t.Fatalf("reject: %v %v", err, result.Failed)
	}
	data := s88OrderData(t, s88Order2)
	if data["order_status"] != "Cancelled" || data["acceptance_status"] != AcceptanceRejected {
		t.Errorf("rejected order = %v / %v, want Cancelled / Rejected", data["order_status"], data["acceptance_status"])
	}
	if push, _ := data["channel_push"].(map[string]interface{}); push["decision"] != OrderDecisionRejected {
		t.Errorf("channel_push = %v, want the Rejected answer recorded", push)
	}
}

// TestOrderDecisionNeverDispatchesOnAmazonOrFlipkart: their status call is a
// shipment confirmation, so an accept / reject must not reach it.
func TestOrderDecisionNeverDispatchesOnAmazonOrFlipkart(t *testing.T) {
	for _, c := range []OmnichannelConnector{amazonSPConnector{}, flipkartConnector{}} {
		for _, d := range []string{OrderDecisionAccepted, OrderDecisionRejected} {
			if err := c.PushOrderStatus(context.Background(), map[string]string{}, ConnectorStatusUpdate{ChannelOrderID: "X", Status: d}); err != ErrOrderDecisionPushUnsupported {
				t.Errorf("%T %s: got %v, want ErrOrderDecisionPushUnsupported", c, d, err)
			}
		}
	}
}

// TestOMSBillAtPackERPBillAtHandover is 88.3.2 / 88.3.3 end to end on the
// 35.4 fixture: the package is billed with an OMS bill (no Sales Invoice
// yet), the label and manifest accept it, and handover makes exactly one
// ERP bill carrying the OMS bill's number.
func TestOMSBillAtPackERPBillAtHandover(t *testing.T) {
	schema := spFixture(t)
	var ready bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.doctype_meta WHERE name = 'OMSBill')`).Scan(&ready)
	if !ready {
		t.Skip("db/migrations_stage88_3_oms_bill.sql has not been applied to this database")
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype IN ('OMSBill', 'Manifest') AND (data->>'sales_order_id' = $1 OR data->>'courier' = 'TestCourier88')`, spOrder)
	})
	pkgID, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatalf("create package: %v", err)
	}
	billID, billNumber, err := GenerateOMSBillForPackage("default", pkgID, "tester", PackInvoiceOptions{})
	if err != nil {
		t.Fatalf("GenerateOMSBillForPackage: %v", err)
	}
	if billNumber == "" {
		t.Error("the OMS bill has no number")
	}
	if again, _, _ := GenerateOMSBillForPackage("default", pkgID, "tester", PackInvoiceOptions{}); again != billID {
		t.Errorf("a second bill call made %s, want the first %s", again, billID)
	}
	p, _ := loadShippingPackage(schema, pkgID)
	if p.Status != "Invoiced" || p.OMSBillID != billID || p.SalesInvoiceID != "" {
		t.Fatalf("package after the OMS bill = %s / bill %q / invoice %q", p.Status, p.OMSBillID, p.SalesInvoiceID)
	}
	countInvoices := func() int {
		var n int
		_ = db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.documents WHERE doctype = 'SalesInvoice' AND data->>'sales_order_id' = $1 AND deleted_at IS NULL`, spOrder).Scan(&n)
		return n
	}
	if n := countInvoices(); n != 0 {
		t.Errorf("Pack made %d Sales Invoice(s); the ERP bill belongs to handover", n)
	}

	bookingID, err := CreateLogisticsBooking("default", spOrder, spTask, "TestCourier88", "", "560001", 40)
	if err != nil {
		t.Fatalf("book: %v", err)
	}
	if _, err := GenerateShippingLabel("default", bookingID); err != nil {
		t.Fatalf("label after the OMS bill was refused: %v", err)
	}
	manifestID, _, err := GenerateManifest("default", "TestCourier88", spLocation)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if err := HandoverManifest("default", manifestID, "tester"); err != nil {
		t.Fatalf("handover: %v", err)
	}
	if n := countInvoices(); n != 1 {
		t.Fatalf("after handover the order has %d Sales Invoice(s), want exactly 1", n)
	}
	var invStr, invStatus string
	if err := db.DB.QueryRow(`SELECT data, status FROM `+schema+`.documents WHERE doctype = 'SalesInvoice' AND data->>'sales_order_id' = $1`, spOrder).Scan(&invStr, &invStatus); err != nil {
		t.Fatalf("read ERP bill: %v", err)
	}
	var inv map[string]interface{}
	_ = json.Unmarshal([]byte(invStr), &inv)
	if inv["invoice_number"] != billNumber || inv["oms_bill_id"] != billID {
		t.Errorf("ERP bill number %v / link %v, want the OMS bill's %s / %s", inv["invoice_number"], inv["oms_bill_id"], billNumber, billID)
	}
	if invStatus == "Draft" {
		t.Error("the ERP bill was left Draft - handover posts it")
	}
	if status, _ := omsBillField(schema, billID, "status"); status != OMSBillBilled {
		t.Errorf("OMS bill after handover = %q, want %s", status, OMSBillBilled)
	}
}
