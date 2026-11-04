package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 49.5.5 (command <-> outbox): a completed POS checkout command whose
// sale event is missing, and a sale event that no command and no paid cart
// stands behind, are exceptions with the same dismiss / lapse flow. A healthy
// tenant has none.
func TestStage49155CommandOutboxReconciliation(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	command := func(key, cart, status, age string, total float64) {
		exec(`INSERT INTO `+schema+`.command_idempotency (idempotency_key, command, request_digest, status, response, document_id, claimed_at, lease_expires_at, completed_at)
			VALUES ($1, 'pos.checkout', 'd', $2, jsonb_build_object('sale_total', $5::numeric), $3, now() - $4::interval, now(), now() - $4::interval)`, key, status, cart, age, total)
	}
	event := func(cart, age string, total float64) {
		exec(`INSERT INTO `+schema+`.integration_event_outbox (event_name, payload, status, created_at)
			VALUES ('pos.sale.completed', jsonb_build_object('cart_number', $1::text, 'sale_total', $3::numeric), 'Processed', now() - $2::interval)`, cart, age, total)
	}
	cart := func(id, status string) {
		exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', '{}', $2, 'system')`, id, status)
	}
	find := func(rows []OrderToCashRow, kind, ref string) *OrderToCashRow {
		for i := range rows {
			if rows[i].Kind == kind && rows[i].Ref == ref {
				return &rows[i]
			}
		}
		return nil
	}

	// healthy: a matched pair, a keyless sale (event + paid cart, no command),
	// an in-flight claim, and old rows outside the window
	command("k1", "C-OK", "Completed", "1 hour", 500)
	event("C-OK", "1 hour", 500)
	cart("C-OK", "Paid")
	event("C-KEYLESS", "2 hours", 80)
	cart("C-KEYLESS", "Paid")
	command("k2", "C-FLIGHT", "InProgress", "1 minute", 0)
	command("k3", "C-OLD", "Completed", "20 days", 10)
	event("C-OLD-EVENT", "20 days", 10)
	rows, err := OrderToCashMismatches(tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("healthy tenant has %d findings, want 0: %+v", len(rows), rows)
	}

	// drift
	command("k4", "C-NOEVENT", "Completed", "3 hours", 1200)
	event("C-GHOST", "4 hours", 90)
	event("C-DRAFT", "4 hours", 70)
	cart("C-DRAFT", "Draft")
	rows, _ = OrderToCashMismatches(tenantID, false)
	if len(rows) != 3 {
		t.Fatalf("got %d findings, want 3: %+v", len(rows), rows)
	}
	if r := find(rows, "Sale without event", "C-NOEVENT"); r == nil || r.InvoicedAmount != 1200 || r.Owner == "" || r.Hint == "" {
		t.Errorf("C-NOEVENT: %+v", r)
	}
	if r := find(rows, "Event without sale", "C-GHOST"); r == nil || r.InvoicedAmount != 90 {
		t.Errorf("C-GHOST: %+v", r)
	}
	if find(rows, "Event without sale", "C-DRAFT") == nil {
		t.Errorf("an event for an unpaid cart should be listed")
	}

	// a dismissal needs a note, takes it off the list, and lapses when the figures change
	if err := SetReconciliationException(tenantID, "Sale without event", "C-NOEVENT", "Dismissed", "", "", "asha"); err == nil {
		t.Errorf("dismissal without a note must be refused")
	}
	if err := SetReconciliationException(tenantID, "Sale without event", "C-NOEVENT", "Dismissed", "event purged on purpose", "", "asha"); err != nil {
		t.Fatal(err)
	}
	rows, _ = OrderToCashMismatches(tenantID, false)
	if find(rows, "Sale without event", "C-NOEVENT") != nil {
		t.Errorf("dismissed finding still listed")
	}
	exec(`UPDATE ` + schema + `.command_idempotency SET response = jsonb_build_object('sale_total', 1300) WHERE idempotency_key = 'k4'`)
	rows, _ = OrderToCashMismatches(tenantID, false)
	if find(rows, "Sale without event", "C-NOEVENT") == nil {
		t.Errorf("dismissal should lapse when the sale total changes")
	}

	// repaired: the event arrives, the finding goes
	event("C-NOEVENT", "1 hour", 1300)
	rows, _ = OrderToCashMismatches(tenantID, false)
	if find(rows, "Sale without event", "C-NOEVENT") != nil {
		t.Errorf("a repaired finding should clear")
	}
}
