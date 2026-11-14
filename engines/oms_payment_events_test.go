package engines

import (
	"custom_erp/db"
	"sync"
	"testing"
)

// Stage 73.4.2.3: capture releases a payment hold; refunds stop at what was
// paid, also when two arrive at once; a reference sent twice is recorded once;
// a chargeback opens, and a lost one marks the payment Charged back.
func TestStage73423OrderPayments(t *testing.T) {
	f := newCreditFixture(t)
	order, err := CreateSalesOrder(f.tenantID, SalesOrderInput{Channel: "Manual", CustomerName: "Asha",
		ShippingAddress: "12 MG Road, Bengaluru, Karnataka 560001", PaymentStatus: "Pending",
		Lines: []SalesOrderLineInput{{SKU: "ITEM-CC", Qty: 2, UnitPrice: 500}}})
	if err != nil {
		t.Fatal(err)
	}
	if h := f.field("SalesOrder", order, "hold_reason"); h != HoldPaymentPending {
		t.Fatalf("want %s hold, got %q", HoldPaymentPending, h)
	}
	pay := func(action string, amount float64, ref, reason string) error {
		_, err := RecordOrderPayment(f.tenantID, order, OrderPaymentInput{Action: action, Amount: amount, Reference: ref, Reason: reason}, "mgr1")
		return err
	}

	// Refund before payment is refused.
	if err := pay(PaymentRefund, 100, "", ""); codeOf(err) != "ORDERM-0470" {
		t.Fatalf("refund unpaid: want ORDERM-0470, got %v", err)
	}
	// Capture: Confirmed, the hold is retried and the order reserves.
	if err := pay(PaymentCapture, 0, "pay_1", ""); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if s, p, c := f.field("SalesOrder", order, "order_status"), f.field("SalesOrder", order, "payment_status"), f.field("SalesOrder", order, "captured_amount"); s != "Reserved" || p != "Confirmed" || c != "1000" {
		t.Fatalf("after capture: %s / %s / %s", s, p, c)
	}
	if err := pay(PaymentCapture, 0, "pay_2", ""); codeOf(err) != "ORDERM-0470" {
		t.Fatalf("second capture: want ORDERM-0470, got %v", err)
	}

	// Two refunds of 600 at once: only one fits in 1000.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, ref := range []string{"rf_a", "rf_b"} {
		wg.Add(1)
		go func(i int, ref string) {
			defer wg.Done()
			errs[i] = pay(PaymentRefund, 600, ref, "")
		}(i, ref)
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("concurrent refunds: want exactly one to pass, got %v / %v", errs[0], errs[1])
	}
	if r := f.field("SalesOrder", order, "refunded_amount"); r != "600" {
		t.Fatalf("refunded: want 600, got %s", r)
	}
	// The passing refund sent again is recorded once.
	passed := "rf_a"
	if errs[0] != nil {
		passed = "rf_b"
	}
	if err := pay(PaymentRefund, 600, passed, ""); err != nil {
		t.Fatalf("replayed refund: %v", err)
	}
	if r := f.field("SalesOrder", order, "refunded_amount"); r != "600" {
		t.Fatalf("after replay: want 600, got %s", r)
	}

	// Chargeback needs a reason; then it opens, and a lost one marks the payment.
	if err := pay(PaymentChargeback, 400, "cb_1", ""); codeOf(err) != "ORDERM-0470" {
		t.Fatalf("chargeback with no reason: want ORDERM-0470, got %v", err)
	}
	if err := pay(PaymentChargeback, 400, "cb_1", "Not received"); err != nil {
		t.Fatalf("chargeback: %v", err)
	}
	if err := pay(PaymentChargeback, 400, "cb_2", "Again"); codeOf(err) != "ORDERM-0470" {
		t.Fatalf("second open chargeback: want ORDERM-0470, got %v", err)
	}
	if err := pay(PaymentChargebackLost, 0, "", ""); err != nil {
		t.Fatalf("chargeback lost: %v", err)
	}
	if c, p := f.field("SalesOrder", order, "chargeback_status"), f.field("SalesOrder", order, "payment_status"); c != "Lost" || p != PaymentStatusChargedBack {
		t.Fatalf("after lost: %s / %s", c, p)
	}

	// The last 400 refunded in full on a fresh paid order -> Refunded.
	paid := f.order("Bala", "", 1)
	if _, err := RecordOrderPayment(f.tenantID, paid, OrderPaymentInput{Action: PaymentCapture}, "mgr1"); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordOrderPayment(f.tenantID, paid, OrderPaymentInput{Action: PaymentRefund, Amount: 500}, "mgr1"); err != nil {
		t.Fatal(err)
	}
	if p := f.field("SalesOrder", paid, "payment_status"); p != PaymentStatusRefunded {
		t.Fatalf("full refund: want Refunded, got %s", p)
	}

	var events int
	_ = db.DB.QueryRow(`SELECT jsonb_array_length(data->'payment_events') FROM `+f.schema+`.documents WHERE id = $1`, order).Scan(&events)
	if events != 4 { // capture, refund, chargeback, chargeback_lost
		t.Fatalf("payment_events: want 4, got %d", events)
	}
}
