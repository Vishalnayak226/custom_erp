package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 73.2.14 - OMS exception queues and one order timeline.
//
// The console already leads with hold, ready-to-release, overdue and
// unmapped-item work (view-oms.js) and four report tiles. What it lacked was
// the order desk's other stuck work: money not confirmed, parcels the carrier
// stopped moving, returns and refunds nobody progressed, and payouts that do
// not match. Each queue below is a read over documents the engines already
// write - no new state - and every row names the order, why it is stuck,
// since when, and the next step the engines will actually accept.

// OMSQueueDef describes one exception queue.
type OMSQueueDef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// OMSExceptionQueueDefs is the fixed queue list, in the order the console
// shows it.
var OMSExceptionQueueDefs = []OMSQueueDef{
	{ID: "payment", Label: "Stuck payments", Hint: "Open orders whose payment is still Pending after 30 minutes."},
	{ID: "carrier", Label: "Carrier exceptions", Hint: "Packed but not booked for 4 hours, bookings with no movement for 48 hours, and RTOs not yet received back."},
	{ID: "return", Label: "Returns and refunds", Hint: "Return requests open for more than 2 days and refunds waiting more than 1 day."},
	{ID: "settlement", Label: "Settlement", Hint: "Payout lines in Variance or Disputed, and shipped channel orders with no settled payout after the aging period."},
	// Stage 99.5.2: a web shop's edit, cancellation or refund that came after
	// picking started - the ERP did not apply it; a person decides.
	{ID: "channel", Label: "Channel changes", Hint: "Edits, cancellations and refunds made in the web shop after picking started - change the order by hand, then mark it done."},
}

const (
	omsStuckPaymentMinutes   = 30
	omsPackedNotBookedHours  = 4
	omsCarrierStalledHours   = 48
	omsReturnStaleHours      = 48
	omsRefundStaleHours      = 24
	omsOpenOrderStatusesSQL  = `('Draft','On Hold','Reserved','Released','Partially Fulfilled','Dispatched')`
	omsLiveBookingStatusSQL  = `('AWB Assigned','Manifested','Handed Over','Shipped','In-Transit')`
	omsOpenReturnStatusesSQL = `('Requested','Approved','Received')`
)

func omsQueueRow(queue, orderID, refDoctype, refID, reason, nextAction string, since time.Time) map[string]interface{} {
	return map[string]interface{}{
		"queue": queue, "order_id": orderID, "ref_doctype": refDoctype, "ref_id": refID,
		"reason": reason, "next_action": nextAction, "since": since,
		"age_hours": int(time.Since(since).Hours()),
	}
}

// omsQueueRowCap bounds each query behind a queue (134.6, 47.10.1): a queue
// of more than this is a backlog to work from the top, not a page to load.
const omsQueueRowCap = 1000

// OMSExceptionQueue returns one queue's rows, oldest first.
func OMSExceptionQueue(tenantID, queue string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var out []map[string]interface{}
	scan := func(query string, build func(scan func(...interface{}) error) (map[string]interface{}, error)) error {
		rows, err := db.DB.Query(fmt.Sprintf("%s LIMIT %d", query, omsQueueRowCap))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			row, err := build(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, row)
		}
		return rows.Err()
	}

	switch queue {
	case "payment":
		err = scan(fmt.Sprintf(`
			SELECT id, COALESCE(data->>'payment_status',''), COALESCE(data->>'hold_reason',''), status, updated_at AT TIME ZONE current_setting('TimeZone')
			FROM %s.documents
			WHERE doctype = 'SalesOrder' AND deleted_at IS NULL AND status IN %s
			  AND (COALESCE(data->>'payment_status','') NOT IN ('Confirmed','Paid','Prepaid') OR data->>'hold_reason' = '%s')
			  AND updated_at < CURRENT_TIMESTAMP - INTERVAL '%d minutes'`, schema, omsOpenOrderStatusesSQL, HoldPaymentPending, omsStuckPaymentMinutes),
			func(s func(...interface{}) error) (map[string]interface{}, error) {
				var id, payment, hold, status string
				var since time.Time
				if err := s(&id, &payment, &hold, &status, &since); err != nil {
					return nil, err
				}
				if payment == "" {
					payment = "not recorded"
				}
				reason := fmt.Sprintf("Payment %s; order %s", payment, status)
				return omsQueueRow(queue, id, "SalesOrder", id, reason,
					"Confirm the payment (Edit > Payment status), then release the hold - or cancel the order", since), nil
			})
	case "carrier":
		err = scan(fmt.Sprintf(`
			SELECT t.id, COALESCE(t.data->>'order_id',''), t.updated_at AT TIME ZONE current_setting('TimeZone')
			FROM %[1]s.documents t
			WHERE t.doctype = 'FulfillmentTask' AND t.deleted_at IS NULL AND t.status = 'Packed'
			  AND t.updated_at < CURRENT_TIMESTAMP - INTERVAL '%[2]d hours'
			  AND NOT EXISTS (SELECT 1 FROM %[1]s.documents b WHERE b.doctype = 'LogisticsBooking' AND b.deleted_at IS NULL
			                  AND b.data->>'order_id' = t.data->>'order_id' AND b.status NOT IN ('Cancelled','RTO'))`, schema, omsPackedNotBookedHours),
			func(s func(...interface{}) error) (map[string]interface{}, error) {
				var taskID, orderID string
				var since time.Time
				if err := s(&taskID, &orderID, &since); err != nil {
					return nil, err
				}
				return omsQueueRow(queue, orderID, "FulfillmentTask", taskID, "Packed, no carrier booking",
					"Book the shipment from the order", since), nil
			})
		if err == nil {
			err = scan(fmt.Sprintf(`
				SELECT id, COALESCE(data->>'order_id',''), status, COALESCE(data->>'tracking_number',''), updated_at AT TIME ZONE current_setting('TimeZone')
				FROM %s.documents
				WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL AND status IN %s
				  AND updated_at < CURRENT_TIMESTAMP - INTERVAL '%d hours'`, schema, omsLiveBookingStatusSQL, omsCarrierStalledHours),
				func(s func(...interface{}) error) (map[string]interface{}, error) {
					var id, orderID, status, awb string
					var since time.Time
					if err := s(&id, &orderID, &status, &awb, &since); err != nil {
						return nil, err
					}
					reason := fmt.Sprintf("No carrier movement since %s", status)
					if awb != "" {
						reason += " (AWB " + awb + ")"
					}
					return omsQueueRow(queue, orderID, "LogisticsBooking", id, reason,
						"Chase the carrier, then record the tracking update or mark RTO on Marketplace", since), nil
				})
		}
		if err == nil {
			err = scan(fmt.Sprintf(`
				SELECT b.id, COALESCE(b.data->>'order_id',''), b.updated_at AT TIME ZONE current_setting('TimeZone')
				FROM %[1]s.documents b
				WHERE b.doctype = 'LogisticsBooking' AND b.deleted_at IS NULL AND b.status = 'RTO'
				  AND NOT EXISTS (SELECT 1 FROM %[1]s.documents r WHERE r.doctype = 'ReturnRequest' AND r.deleted_at IS NULL
				                  AND `+omsReturnOrderExpr(schema, "r")+` = b.data->>'order_id' AND r.status IN ('Received','QC Complete','Closed'))`, schema),
				func(s func(...interface{}) error) (map[string]interface{}, error) {
					var id, orderID string
					var since time.Time
					if err := s(&id, &orderID, &since); err != nil {
						return nil, err
					}
					return omsQueueRow(queue, orderID, "LogisticsBooking", id, "Returned to origin, not yet received back",
						"Receive the RTO parcel and run its return QC", since), nil
				})
		}
	case "return":
		err = scan(fmt.Sprintf(`
			SELECT r.id, `+omsReturnOrderExpr(schema, "r")+`, r.status, COALESCE(r.data->>'request_type',''), r.updated_at AT TIME ZONE current_setting('TimeZone')
			FROM %s.documents r
			WHERE doctype = 'ReturnRequest' AND deleted_at IS NULL AND status IN %s
			  AND updated_at < CURRENT_TIMESTAMP - INTERVAL '%d hours'`, schema, omsOpenReturnStatusesSQL, omsReturnStaleHours),
			func(s func(...interface{}) error) (map[string]interface{}, error) {
				var id, orderID, status, kind string
				var since time.Time
				if err := s(&id, &orderID, &status, &kind, &since); err != nil {
					return nil, err
				}
				next := map[string]string{
					"Requested": "Approve or reject the return",
					"Approved":  "Schedule the pickup, then receive the parcel",
					"Received":  "Run return QC so the refund can be raised",
				}[status]
				return omsQueueRow(queue, orderID, "ReturnRequest", id, strings.TrimSpace(kind+" "+status), next, since), nil
			})
		if err == nil {
			err = scan(fmt.Sprintf(`
				SELECT r.id, `+omsReturnOrderExpr(schema, "r")+`, r.status, r.updated_at AT TIME ZONE current_setting('TimeZone')
				FROM %s.documents r
				WHERE doctype = 'RefundRequest' AND deleted_at IS NULL AND status IN ('Pending','Approved')
				  AND updated_at < CURRENT_TIMESTAMP - INTERVAL '%d hours'`, schema, omsRefundStaleHours),
				func(s func(...interface{}) error) (map[string]interface{}, error) {
					var id, orderID, status string
					var since time.Time
					if err := s(&id, &orderID, &status, &since); err != nil {
						return nil, err
					}
					next := "Approve the refund"
					if status == "Approved" {
						next = "Process the refund"
					}
					return omsQueueRow(queue, orderID, "RefundRequest", id, "Refund "+status, next, since), nil
				})
		}
	case "settlement":
		err = scan(fmt.Sprintf(`
			SELECT id, COALESCE(data->>'order_id',''), data->>'match_status', COALESCE((data->>'variance_amount')::numeric, 0), updated_at AT TIME ZONE current_setting('TimeZone')
			FROM %s.documents
			WHERE doctype = 'MarketplaceSettlementLine' AND deleted_at IS NULL AND data->>'match_status' IN ('Variance','Disputed')`, schema),
			func(s func(...interface{}) error) (map[string]interface{}, error) {
				var id, orderID, matchStatus string
				var variance float64
				var since time.Time
				if err := s(&id, &orderID, &matchStatus, &variance, &since); err != nil {
					return nil, err
				}
				next := "Raise a dispute with the marketplace, or write the variance off with a reason"
				if matchStatus == "Disputed" {
					next = "Record the marketplace's corrected amount to resolve the dispute"
				}
				return omsQueueRow(queue, orderID, "MarketplaceSettlementLine", id,
					fmt.Sprintf("Payout %s by %.2f", matchStatus, variance), next, since), nil
			})
		if err == nil {
			var unsettled []map[string]interface{}
			unsettled, err = getUnsettledMarketplaceOrdersReport(tenantID, nil)
			for _, u := range unsettled {
				if overdue, _ := u["overdue"].(bool); !overdue {
					continue
				}
				days, _ := u["days_since_ship"].(int)
				orderID, _ := u["order_id"].(string)
				out = append(out, omsQueueRow(queue, orderID, "SalesOrder", orderID,
					fmt.Sprintf("%v order with no settled payout", u["status"]),
					"Import the channel's payout file and reconcile, or chase the marketplace",
					time.Now().Add(-time.Duration(days)*24*time.Hour)))
			}
		}
	case "channel":
		err = scan(fmt.Sprintf(`
			SELECT id, COALESCE(data->>'channel_change_pending', ''), COALESCE(data->>'channel', ''), updated_at AT TIME ZONE current_setting('TimeZone')
			FROM %s.documents
			WHERE doctype = 'SalesOrder' AND deleted_at IS NULL AND COALESCE(data->>'channel_change_pending', '') <> ''`, schema),
			func(s func(...interface{}) error) (map[string]interface{}, error) {
				var id, change, channel string
				var since time.Time
				if err := s(&id, &change, &channel, &since); err != nil {
					return nil, err
				}
				return omsQueueRow(queue, id, "SalesOrder", id, fmt.Sprintf("%s: %s", channel, change),
					"Make the change on the order (or return it once shipped), then mark it done", since), nil
			})
	default:
		return nil, &ValidationError{Code: "ORDERM-0464", SubFor: "queue", Message: fmt.Sprintf("unknown OMS queue %q", queue)}
	}
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i]["since"].(time.Time).Before(out[j]["since"].(time.Time))
	})
	return out, nil
}

// OMSExceptionQueueCounts returns every queue with its row count. One queue
// failing reports its own error instead of failing the console, the
// OMSConsoleTiles precedent.
func OMSExceptionQueueCounts(tenantID string) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(OMSExceptionQueueDefs))
	for _, def := range OMSExceptionQueueDefs {
		entry := map[string]interface{}{"id": def.ID, "label": def.Label, "hint": def.Hint}
		rows, err := OMSExceptionQueue(tenantID, def.ID)
		if err != nil {
			entry["error"] = err.Error()
			entry["count"] = 0
		} else {
			entry["count"] = len(rows)
			entry["more"] = len(rows) >= omsQueueRowCap // the count stops at the cap
		}
		out = append(out, entry)
	}
	return out
}

// buildOrderTimeline merges an order detail's sections into one list of
// dated events, oldest first. Lines and reservations are current state, not
// events, so they stay out; everything that happened to the order does not.
func buildOrderTimeline(orderID string, createdAt time.Time, detail map[string]interface{}) []map[string]interface{} {
	events := []map[string]interface{}{{
		"at": createdAt, "kind": "Order", "ref_doctype": "SalesOrder", "ref_id": orderID,
		"title": "Order received", "status": "",
	}}
	sections := []struct{ key, kind, doctype, title string }{
		{"fulfillment_tasks", "Fulfillment", "FulfillmentTask", "Pick task created"},
		{"shipments", "Shipment", "LogisticsBooking", "Carrier booking"},
		{"invoices", "Invoice", "SalesInvoice", "Invoice"},
		{"returns", "Return", "ReturnRequest", "Return request"},
		{"refunds", "Refund", "RefundRequest", "Refund"},
		{"notifications", "Notification", "NotificationLog", "Notification"},
		{"replacements", "Replacement", "SalesOrder", "Replacement order"},
	}
	for _, s := range sections {
		rows, _ := detail[s.key].([]map[string]interface{})
		for _, r := range rows {
			title := s.title
			if d, _ := r["detail"].(string); d != "" {
				title += " - " + d
			}
			events = append(events, map[string]interface{}{
				"at": r["created_at"], "kind": s.kind, "ref_doctype": s.doctype, "ref_id": r["id"],
				"title": title, "status": r["status"],
			})
		}
	}
	if audit, ok := detail["audit_trail"].([]map[string]interface{}); ok {
		for _, a := range audit {
			// The documents trigger's raw "Created/Deleted Document ... with
			// data" rows repeat the events above as JSON; they stay on the
			// audit trail, not the story.
			d, _ := a["details"].(string)
			if strings.HasPrefix(d, "Created Document ID") || strings.HasPrefix(d, "Deleted Document ID") {
				continue
			}
			// Of the trigger's per-field UPDATE rows only the status changes
			// tell the story; released_at and friends stay on the audit trail.
			if strings.HasPrefix(d, "Field ") && !strings.HasPrefix(d, `Field "order_status"`) {
				continue
			}
			events = append(events, map[string]interface{}{
				"at": a["created_at"], "kind": "Change", "title": fmt.Sprint(a["action"]),
				"status": a["status"], "by": a["user_id"], "detail": a["details"],
			})
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		ti, _ := events[i]["at"].(time.Time)
		tj, _ := events[j]["at"].(time.Time)
		return ti.Before(tj)
	})
	return events
}

// orderNextActions names the step(s) the engines will accept next, most
// useful first. It mirrors the engines' own guards rather than guessing: a
// hold must be released before fulfilment, only a Reserved order can be
// released to the warehouse, and booking needs a packed task with no live
// booking.
func orderNextActions(detail map[string]interface{}) []map[string]interface{} {
	order, _ := detail["order"].(map[string]interface{})
	status, _ := order["order_status"].(string)
	hold, _ := order["hold_reason"].(string)
	payment, _ := order["payment_status"].(string)
	rowsOf := func(key string) []map[string]interface{} {
		rows, _ := detail[key].([]map[string]interface{})
		return rows
	}
	hasStatus := func(key string, statuses ...string) bool {
		for _, r := range rowsOf(key) {
			for _, s := range statuses {
				if r["status"] == s {
					return true
				}
			}
		}
		return false
	}
	action := func(key, label, why string) map[string]interface{} {
		return map[string]interface{}{"action": key, "label": label, "reason": why}
	}
	var out []map[string]interface{}
	switch status {
	case "On Hold":
		why := "Held: " + hold + ". Fix the cause, then release - the same checks run again."
		if hold == HoldPaymentPending || payment == "Pending" {
			out = append(out, action("edit", "Confirm payment", "Payment is "+payment+". Record it as Confirmed, then release the hold."))
		}
		out = append(out, action("release", "Release hold", why))
	case "Draft", "Reserved", "Partially Fulfilled":
		if payment != "" && payment != "Confirmed" && payment != "Paid" && payment != "Prepaid" {
			out = append(out, action("edit", "Confirm payment", "Payment is "+payment+"."))
		}
		if status == "Reserved" {
			out = append(out, action("fulfil", "Release to fulfillment", "Stock is reserved; this creates the warehouse pick tasks."))
		}
	case OrderStatusReleased, "Dispatched":
		liveBooking := hasStatus("shipments", "AWB Assigned", "Manifested", "Handed Over", "Shipped", "In-Transit")
		switch {
		case hasStatus("fulfillment_tasks", "Packed", "Dispatched") && !liveBooking:
			out = append(out, action("book", "Book shipment", "Packed and waiting for a carrier."))
		case liveBooking:
			out = append(out, action("marketplace", "Hand over and track", "Booked; hand the manifest to the carrier and record tracking."))
		default:
			out = append(out, action("fulfillment", "Pick and pack", "Pick tasks are open on the Fulfillment screen."))
		}
	case "Shipped":
		out = append(out, action("marketplace", "Record delivery or RTO", "In transit; the carrier's tracking closes it."))
	case "Delivered":
		if hasStatus("invoices", "Draft") {
			out = append(out, action("invoice", "Post the invoice", "Delivered with a Draft invoice."))
		}
	}
	if refund := numFromInterface(order["refund_due_amount"]); refund > 0 {
		out = append(out, action("returns", "Refund the difference", fmt.Sprintf("A line change lowered the confirmed total by %.2f.", refund)))
	}
	if replaceableOrderStatuses[status] {
		out = append(out, action("replace", "Reship or replace", "Lost, damaged or wrong goods go again on a linked, no-charge order."))
	}
	if hasStatus("returns", "Requested", "Approved", "Received") {
		out = append(out, action("returns", "Work the return", "A return on this order is still open."))
	}
	if hasStatus("refunds", "Pending", "Approved") {
		out = append(out, action("returns", "Process the refund", "A refund on this order is waiting."))
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out
}

// omsReturnOrderExpr (Stage 55.9) is the SalesOrder a return or refund row
// belongs to. Both store it in original_order_id; a customer return on an OMS
// order is raised against that order's sales invoice, so an invoice id is
// followed to its sales_order_id.
func omsReturnOrderExpr(schema, alias string) string {
	ref := fmt.Sprintf("COALESCE(%[1]s.data->>'original_order_id', %[1]s.data->>'order_id', '')", alias)
	return fmt.Sprintf(`COALESCE((SELECT inv.data->>'sales_order_id' FROM %s.documents inv WHERE inv.doctype = 'SalesInvoice' AND inv.id = %s LIMIT 1), %s)`, schema, ref, ref)
}
