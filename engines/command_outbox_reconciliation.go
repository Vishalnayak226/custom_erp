package engines

import (
	"custom_erp/db"
	"fmt"
)

// Stage 49.5.5 (command <-> outbox). The one command whose event is fixed and
// whose two sides are both stored is the POS checkout: a Completed
// "pos.checkout" command (command_idempotency, document_id = the cart) and the
// "pos.sale.completed" outbox row (payload cart_number), written in the same
// transaction. Two exceptions, in the order-to-cash list's dismiss / lapse flow:
//
//   - Sale without event   a Completed checkout command with no sale event
//   - Event without sale   a sale event with no Completed command and no Paid
//     cart behind it (a keyless checkout has no command, but its cart is Paid)
//
// Only rows from the last commandOutboxWindowDays are compared: command claims
// are purged after 7 days and sent events later still, so an older row's
// partner may be gone for a good reason.
const commandOutboxWindowDays = 5

func commandOutboxRows(tenantID string) ([]OrderToCashRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var have bool
	if err := db.DB.QueryRow(`SELECT to_regclass($1) IS NOT NULL AND to_regclass($2) IS NOT NULL`,
		schema+".command_idempotency", schema+".integration_event_outbox").Scan(&have); err != nil || !have {
		return nil, err
	}
	out := []OrderToCashRow{}
	window := fmt.Sprintf("%d days", commandOutboxWindowDays)

	rs, err := db.DB.Query(fmt.Sprintf(`
		SELECT c.document_id, COALESCE((c.response->>'sale_total')::numeric, 0)
		  FROM %[1]s.command_idempotency c
		 WHERE c.command = 'pos.checkout' AND c.status = 'Completed' AND COALESCE(c.document_id, '') <> ''
		   AND c.completed_at >= CURRENT_TIMESTAMP - $1::interval
		   AND NOT EXISTS (SELECT 1 FROM %[1]s.integration_event_outbox e
		                    WHERE e.event_name = 'pos.sale.completed' AND e.payload->>'cart_number' = c.document_id)
		 ORDER BY c.completed_at LIMIT %[2]d`, schema, reconcileLimit), window)
	if err != nil {
		return nil, err
	}
	for rs.Next() {
		var cart string
		var total float64
		if err := rs.Scan(&cart, &total); err != nil {
			rs.Close()
			return nil, err
		}
		out = append(out, OrderToCashRow{Kind: "Sale without event", Ref: cart, Order: cart, InvoicedAmount: total, Owner: "IT",
			Hint: "The sale posted but its event was not queued. Send it again.", fingerprint: fmt.Sprintf("cmd|%.2f", total)})
	}
	rs.Close()

	rs, err = db.DB.Query(fmt.Sprintf(`
		SELECT DISTINCT ON (e.payload->>'cart_number') e.payload->>'cart_number', COALESCE((e.payload->>'sale_total')::numeric, 0)
		  FROM %[1]s.integration_event_outbox e
		 WHERE e.event_name = 'pos.sale.completed' AND COALESCE(e.payload->>'cart_number', '') <> ''
		   AND e.created_at >= CURRENT_TIMESTAMP - $1::interval
		   AND NOT EXISTS (SELECT 1 FROM %[1]s.command_idempotency c
		                    WHERE c.command = 'pos.checkout' AND c.status = 'Completed' AND c.document_id = e.payload->>'cart_number')
		   AND NOT EXISTS (SELECT 1 FROM %[1]s.documents d
		                    WHERE d.doctype = 'POSCart' AND d.id = e.payload->>'cart_number' AND d.status = 'Paid' AND d.deleted_at IS NULL)
		 ORDER BY e.payload->>'cart_number', e.id LIMIT %[2]d`, schema, reconcileLimit), window)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	for rs.Next() {
		var cart string
		var total float64
		if err := rs.Scan(&cart, &total); err != nil {
			return nil, err
		}
		out = append(out, OrderToCashRow{Kind: "Event without sale", Ref: cart, Order: cart, InvoicedAmount: total, Owner: "IT",
			Hint: "A sale event was queued but no paid sale stands behind it. Check the cart.", fingerprint: fmt.Sprintf("evt|%.2f", total)})
	}
	return out, rs.Err()
}
