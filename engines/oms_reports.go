package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

// Stage 26.12.7 (Exception/reconciliation dashboard) and Stage 26.12.8 (OMS
// reports) - both built as ordinary ReportDefinition catalog entries
// (engines/report_registry.go, Stage 20 Track B.4), the same "register a
// function, not a new endpoint" pattern every report since GRN Register has
// used; no dedicated routes are needed; every report here is reachable via
// the existing GET /api/v1/reports/catalog + /run/{id} + /drilldown/{id}
// framework, already moduleGate("reports",...)'d in routes.go.
//
// 26.12.7 is deliberately narrow, per the checklist item's own "no new
// retry/DLQ mechanism needed, just OMS-scoped visibility" scope: the
// exception queue reads the existing integration_event_outbox/
// integration_event_log (engines/outbox.go) as-is, and the reconciliation-
// variance report compares real, already-stored SalesOrder vs
// LogisticsBooking status, not a synthetic feed.

func init() {
	RegisterReport(ReportDefinition{
		ID: "oms-exception-queue", Label: "OMS Exception Queue", Category: "OMS",
		Columns: []ReportColumn{
			{Key: "event_id", Label: "Event ID"}, {Key: "event_name", Label: "Event"},
			{Key: "status", Label: "Status"}, {Key: "attempts", Label: "Attempts"},
			{Key: "created_at", Label: "Queued At"}, {Key: "last_error", Label: "Last Error"},
		},
		Run: getOMSExceptionQueueReport,
	})

	RegisterReport(ReportDefinition{
		ID: "oms-reconciliation-variance", Label: "OMS Reconciliation Variance", Category: "OMS",
		Columns: []ReportColumn{
			{Key: "order_id", Label: "Order"}, {Key: "order_status", Label: "Order Status"},
			{Key: "booking_id", Label: "Shipment/Booking"}, {Key: "booking_status", Label: "Booking Status"},
			{Key: "variance", Label: "Variance"},
		},
		Run: getOMSReconciliationVarianceReport,
	})

	RegisterReport(ReportDefinition{
		ID: "order-aging", Label: "Order Aging", Category: "OMS",
		Columns: []ReportColumn{
			{Key: "bucket", Label: "Age Bucket"}, {Key: "count", Label: "Order Count"},
			{Key: "amount", Label: "Total Amount", Sensitive: true},
		},
		Run:       getOrderAgingReport,
		DrillDown: orderAgingDrillDown,
	})

	RegisterReport(ReportDefinition{
		ID: "sla-breach", Label: "SLA Breach", Category: "OMS",
		Columns: []ReportColumn{
			{Key: "task_id", Label: "Task ID"}, {Key: "order_id", Label: "Order"},
			{Key: "location_code", Label: "Location"}, {Key: "status", Label: "Status"},
			{Key: "minutes_elapsed", Label: "Minutes Elapsed"}, {Key: "threshold_minutes", Label: "Threshold (mins)"},
		},
		Params: []ReportParam{{Key: "threshold_minutes", Label: "Threshold (minutes, default 120)", Type: "text"}},
		Run:    getOMSSLABreachReport,
	})

	RegisterReport(ReportDefinition{
		ID: "allocation-pending", Label: "Allocation Pending", Category: "OMS",
		Columns: []ReportColumn{
			{Key: "order_id", Label: "Order"}, {Key: "customer_name", Label: "Customer"},
			{Key: "channel", Label: "Channel"}, {Key: "total_amount", Label: "Total Amount", Sensitive: true},
			{Key: "created_at", Label: "Created At"},
		},
		Run: getAllocationPendingReport,
	})

	RegisterReport(ReportDefinition{
		ID: "stock-mismatch", Label: "Stock Mismatch", Category: "OMS",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "location_code", Label: "Location"},
			{Key: "on_hand", Label: "On Hand"}, {Key: "available", Label: "Available"},
			{Key: "reserved", Label: "Reserved"}, {Key: "safety_stock", Label: "Safety Stock"},
			{Key: "blocked", Label: "Blocked"}, {Key: "qc_hold", Label: "QC Hold"},
			{Key: "damaged", Label: "Damaged"}, {Key: "channel_buffer", Label: "Channel Buffer"},
			{Key: "ats", Label: "ATS (negative = over-committed)"},
		},
		Run: getStockMismatchReport,
	})

	RegisterReport(ReportDefinition{
		ID: "return-aging", Label: "Return Aging", Category: "OMS",
		Columns: []ReportColumn{
			{Key: "bucket", Label: "Age Bucket"}, {Key: "count", Label: "Return Request Count"},
		},
		Run:       getReturnAgingReport,
		DrillDown: returnAgingDrillDown,
	})

	RegisterReport(ReportDefinition{
		ID: "reserved-stock", Label: "Reserved Stock", Category: "OMS",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "location_code", Label: "Location"},
			{Key: "reserved", Label: "Reserved"}, {Key: "committed", Label: "Committed"},
			{Key: "on_hand", Label: "On Hand"}, {Key: "available", Label: "Available"},
		},
		Run: getReservedStockReport,
	})

	RegisterReport(ReportDefinition{
		ID: "courier-performance", Label: "Courier Performance", Category: "OMS",
		Columns: []ReportColumn{
			{Key: "carrier", Label: "Carrier"}, {Key: "total_shipments", Label: "Total Shipments"},
			{Key: "delivered", Label: "Delivered"}, {Key: "rto", Label: "RTO"},
			{Key: "rto_rate_pct", Label: "RTO Rate %"}, {Key: "avg_turnaround_hours", Label: "Avg Handover->Delivered (hrs)"},
		},
		Run: getCourierPerformanceReport,
	})

	// Stage 35.1.3. The plan anticipated a POSCart backfill decision for
	// legacy channel orders. The real finding was that there is nothing to
	// backfill: the retired importers never created the POSCart their own
	// comments promised. What they did leave is channel_order_mapping /
	// unicommerce_order_mapping rows pointing at a synthetic
	// "ORD-<channel>-<id>" / "UC-<store>-<id>" identifier with no document
	// behind it at all.
	//
	// The decision, per the plan's own preference for a read-only view over a
	// data migration: do not invent SalesOrders for them. List them, so a human
	// who can check the channel decides which are still live and re-imports
	// those. POSCart stays the in-store cart; SalesOrder is the only
	// channel-order truth.
	//
	// Reservations are deliberately NOT a column here: inventory_reservation
	// has no order_id (db/migration.sql §301), so the stock those imports
	// reserved cannot be attributed back to a mapping row by any query. See
	// the note in micro_checklist.md 35.1.3 - that is a separate finding, not
	// something this report can honestly show.
	RegisterReport(ReportDefinition{
		ID: "orphaned-channel-orders", Label: "Orphaned Channel Orders (pre-35.1 intake)", Category: "OMS",
		Columns: []ReportColumn{
			{Key: "source", Label: "Mapping Table"}, {Key: "channel", Label: "Channel"},
			{Key: "channel_order_id", Label: "Channel Order ID"}, {Key: "legacy_order_id", Label: "Legacy Order ID"},
			// channel_order_mapping tracks updated_at, unicommerce_order_mapping
			// created_at; both mean "when intake last wrote this row", hence the
			// neutral label rather than pretending they are the same column.
			{Key: "imported_at", Label: "Last Written"},
		},
		Run: getOrphanedChannelOrdersReport,
	})
}

// getOrphanedChannelOrdersReport lists channel intake rows written by the
// pre-Stage-35.1 importers whose order_id resolves to no document at all.
// A tenant that only ever ran the post-35.1 intake returns zero rows.
//
// Re-importing any row listed here now works: ImportChannelSalesOrder treats a
// mapping row whose target document is missing as absent rather than as a
// completed import, so replaying the channel order creates the SalesOrder the
// old path never did.
func getOrphanedChannelOrdersReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT 'channel_order_mapping', m.channel, m.channel_order_id, m.order_id, m.updated_at
		FROM %s.channel_order_mapping m
		WHERE NOT EXISTS (SELECT 1 FROM %s.documents d WHERE d.id = m.order_id AND d.deleted_at IS NULL)
		UNION ALL
		SELECT 'unicommerce_order_mapping', 'Unicommerce', u.channel_order_id, u.order_id, u.created_at
		FROM %s.unicommerce_order_mapping u
		WHERE NOT EXISTS (SELECT 1 FROM %s.documents d WHERE d.id = u.order_id AND d.deleted_at IS NULL)
		ORDER BY 5 DESC`, schema, schema, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var source, channel, channelOrderID, legacyOrderID string
		var importedAt time.Time
		if err := rows.Scan(&source, &channel, &channelOrderID, &legacyOrderID, &importedAt); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"source": source, "channel": channel, "channel_order_id": channelOrderID,
			"legacy_order_id": legacyOrderID, "imported_at": importedAt,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, rows.Err()
}

// getOMSExceptionQueueReport surfaces stuck outbox events (Failed outright,
// or Pending past a 30-minute staleness window) - the checklist's own
// "OMS-scoped visibility, no new retry/DLQ mechanism" wording; retry itself
// already exists (RetryIntegrationEvent, engines/outbox.go).
func getOMSExceptionQueueReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT o.id, o.event_name, o.status, o.attempts, o.created_at,
		       COALESCE((SELECT l.error_message FROM %s.integration_event_log l WHERE l.event_id = o.id ORDER BY l.created_at DESC LIMIT 1), '')
		FROM %s.integration_event_outbox o
		WHERE o.status = 'Failed' OR (o.status = 'Pending' AND o.created_at < NOW() - INTERVAL '30 minutes')
		ORDER BY o.created_at DESC`, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var id, eventName, status, lastError string
		var attempts int
		var createdAt time.Time
		if err := rows.Scan(&id, &eventName, &status, &attempts, &createdAt, &lastError); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"event_id": id, "event_name": eventName, "status": status,
			"attempts": attempts, "created_at": createdAt, "last_error": lastError,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, rows.Err()
}

// getOMSReconciliationVarianceReport flags a Shipped/Delivered SalesOrder
// whose linked LogisticsBooking(s) (Stage 26.12.4) status doesn't actually
// support that claim yet, or has no booking at all - the "OMS vs courier"
// variance the checklist calls for, over real stored data rather than a
// synthetic channel feed this repo doesn't have.
func getOMSReconciliationVarianceReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT so.id, so.status, lb.id, lb.status
		FROM %s.documents so
		LEFT JOIN %s.documents lb ON lb.doctype = 'LogisticsBooking' AND lb.data->>'order_id' = so.id AND lb.deleted_at IS NULL
		WHERE so.doctype = 'SalesOrder' AND so.deleted_at IS NULL AND so.status IN ('Shipped', 'Delivered')`, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var orderID, orderStatus string
		var bookingID, bookingStatus sql.NullString
		if err := rows.Scan(&orderID, &orderStatus, &bookingID, &bookingStatus); err != nil {
			return nil, err
		}
		variance := ""
		switch {
		case !bookingID.Valid:
			variance = fmt.Sprintf("order marked %s but no LogisticsBooking exists", orderStatus)
		case orderStatus == "Shipped" && bookingStatus.String != "Handed Over" && bookingStatus.String != "Delivered":
			variance = fmt.Sprintf("order marked Shipped but its booking is still %s", bookingStatus.String)
		case orderStatus == "Delivered" && bookingStatus.String != "Delivered":
			variance = fmt.Sprintf("order marked Delivered but its booking is still %s", bookingStatus.String)
		}
		if variance == "" {
			continue
		}
		results = append(results, map[string]interface{}{
			"order_id": orderID, "order_status": orderStatus,
			"booking_id": bookingID.String, "booking_status": bookingStatus.String, "variance": variance,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, rows.Err()
}

// ageBucketKey mirrors GetPayablesAgeingReport/GetReceivablesAgeingReport's
// own inline switch (engines/reports.go) - kept as the same literal
// four-bucket shape so Order/Return Aging read identically to the existing
// Payables/Receivables Ageing reports users already know.
func ageBucketKey(ageDays int) string {
	switch {
	case ageDays <= 30:
		return "0-30"
	case ageDays <= 60:
		return "31-60"
	case ageDays <= 90:
		return "61-90"
	default:
		return "90plus"
	}
}

func newAgeingBuckets() (map[string]*PayablesAgeingBucket, []string) {
	buckets := map[string]*PayablesAgeingBucket{
		"0-30":   {Bucket: "0-30 days"},
		"31-60":  {Bucket: "31-60 days"},
		"61-90":  {Bucket: "61-90 days"},
		"90plus": {Bucket: "90+ days"},
	}
	return buckets, []string{"0-30", "31-60", "61-90", "90plus"}
}

// getOrderAgingReport buckets every open (not Delivered/Cancelled/Closed)
// SalesOrder by age since creation, the same PayablesAgeingBucket shape and
// ageBucketLabel-matched drilldown convention as Payables/Receivables
// Ageing (Stage 20.33/20.38).
func getOrderAgingReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE((data->>'total_amount')::numeric, 0), created_at FROM %s.documents
		WHERE doctype = 'SalesOrder' AND deleted_at IS NULL AND status NOT IN ('Delivered', 'Cancelled', 'Closed')`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	buckets, order := newAgeingBuckets()
	now := time.Now()
	for rows.Next() {
		var amount float64
		var createdAt time.Time
		if err := rows.Scan(&amount, &createdAt); err != nil {
			return nil, err
		}
		key := ageBucketKey(int(now.Sub(createdAt).Hours() / 24))
		buckets[key].Count++
		buckets[key].Amount += amount
	}
	results := make([]PayablesAgeingBucket, 0, len(order))
	for _, k := range order {
		results = append(results, *buckets[k])
	}
	return structsToRows(results)
}

func orderAgingDrillDown(tenantID, rowKey string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE((data->>'total_amount')::numeric, 0), status, created_at FROM %s.documents
		WHERE doctype = 'SalesOrder' AND deleted_at IS NULL AND status NOT IN ('Delivered', 'Cancelled', 'Closed')`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	var results []map[string]interface{}
	for rows.Next() {
		var id, status string
		var amount float64
		var createdAt time.Time
		if err := rows.Scan(&id, &amount, &status, &createdAt); err != nil {
			return nil, err
		}
		if ageBucketLabel(int(now.Sub(createdAt).Hours()/24)) != rowKey {
			continue
		}
		results = append(results, map[string]interface{}{
			"id": id, "total_amount": amount, "status": status, "created_at": createdAt,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, rows.Err()
}

// getOMSSLABreachReport wraps the pre-existing GetSLABreaches
// (engines/optimization.go, Stage 17.10's FulfillmentTask monitor) as an
// OMS report catalog entry, per the checklist's own "reuses Stage 17.10's
// SLA-breach monitor pattern" instruction - no separate SLA engine needed.
func getOMSSLABreachReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	threshold := 120.0
	if t := params["threshold_minutes"]; t != "" {
		if parsed, err := strconv.ParseFloat(t, 64); err == nil {
			threshold = parsed
		}
	}
	breaches, err := GetSLABreaches(tenantID, threshold)
	if err != nil {
		return nil, err
	}
	return structsToRows(breaches)
}

// getAllocationPendingReport lists every SalesOrder On Hold with
// HoldAllocationFailed (Stage 26.12.2's allocation-exception code) - the
// exact "usable list-view queue" precedent 26.12.2's own build note already
// established (filtering by hold_reason IS the exception queue, no
// separate table).
func getAllocationPendingReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'customer_name', ''), COALESCE(data->>'channel', ''),
		       COALESCE((data->>'total_amount')::numeric, 0), created_at
		FROM %s.documents WHERE doctype = 'SalesOrder' AND deleted_at IS NULL
		  AND status = 'On Hold' AND data->>'hold_reason' = $1`, schema), HoldAllocationFailed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var id, customerName, channel string
		var amount float64
		var createdAt time.Time
		if err := rows.Scan(&id, &customerName, &channel, &amount, &createdAt); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"order_id": id, "customer_name": customerName, "channel": channel,
			"total_amount": amount, "created_at": createdAt,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, rows.Err()
}

// getStockMismatchReport flags every SKU/location where computeATS
// (engines/inventory.go, Stage 26.12.6's shared 7-term formula) is
// negative - a real correctness signal (something over-reserved/over-
// committed past what's physically available) rather than a Stock Ledger
// reconciliation, since Stage 26.10.1's ledger wiring isn't built yet
// (docs/micro_checklist.md).
func getStockMismatchReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT sku, location_code, on_hand, available, reserved, safety_stock, blocked, qc_hold, damaged, channel_buffer, hold_qty
		FROM %s.inventory_availability`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var sku, location string
		var onHand, available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer, held int
		if err := rows.Scan(&sku, &location, &onHand, &available, &reserved, &safetyStock, &blocked, &qcHold, &damaged, &channelBuffer, &held); err != nil {
			return nil, err
		}
		ats := computeATS(available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer, held)
		if ats >= 0 {
			continue
		}
		results = append(results, map[string]interface{}{
			"sku": sku, "location_code": location, "on_hand": onHand, "available": available,
			"reserved": reserved, "safety_stock": safetyStock, "blocked": blocked,
			"qc_hold": qcHold, "damaged": damaged, "channel_buffer": channelBuffer, "ats": ats,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, rows.Err()
}

// getReturnAgingReport buckets every open (not Closed/Rejected)
// ReturnRequest (Stage 26.12.5) by age since creation - amount is
// deliberately omitted (unlike Order Aging): a return's refund total isn't
// known until QC completes, so an "amount" column would read as real money
// for requests that don't have one resolved yet.
func getReturnAgingReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT created_at FROM %s.documents WHERE doctype = 'ReturnRequest' AND deleted_at IS NULL
		  AND status NOT IN ('Closed', 'Rejected')`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	buckets, order := newAgeingBuckets()
	now := time.Now()
	for rows.Next() {
		var createdAt time.Time
		if err := rows.Scan(&createdAt); err != nil {
			return nil, err
		}
		buckets[ageBucketKey(int(now.Sub(createdAt).Hours()/24))].Count++
	}
	results := make([]PayablesAgeingBucket, 0, len(order))
	for _, k := range order {
		results = append(results, *buckets[k])
	}
	return structsToRows(results)
}

func returnAgingDrillDown(tenantID, rowKey string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'request_type', ''), status, created_at FROM %s.documents
		WHERE doctype = 'ReturnRequest' AND deleted_at IS NULL AND status NOT IN ('Closed', 'Rejected')`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	var results []map[string]interface{}
	for rows.Next() {
		var id, requestType, status string
		var createdAt time.Time
		if err := rows.Scan(&id, &requestType, &status, &createdAt); err != nil {
			return nil, err
		}
		if ageBucketLabel(int(now.Sub(createdAt).Hours()/24)) != rowKey {
			continue
		}
		results = append(results, map[string]interface{}{
			"id": id, "request_type": requestType, "status": status, "created_at": createdAt,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, rows.Err()
}

// getReservedStockReport lists every SKU/location currently holding a
// reservation - a direct read of inventory_availability, no aggregation.
func getReservedStockReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT sku, location_code, reserved, committed, on_hand, available FROM %s.inventory_availability
		WHERE reserved > 0 ORDER BY reserved DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var sku, location string
		var reserved, committed, onHand, available int
		if err := rows.Scan(&sku, &location, &reserved, &committed, &onHand, &available); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"sku": sku, "location_code": location, "reserved": reserved,
			"committed": committed, "on_hand": onHand, "available": available,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, rows.Err()
}

// getCourierPerformanceReport aggregates LogisticsBooking (Stage 26.12.4)
// by carrier: shipment volume, delivered/RTO counts and rate, and an
// average Handed-Over-to-Delivered turnaround using each booking's own
// created_at/updated_at as a turnaround proxy (no separate per-status-change
// timestamp history exists yet).
func getCourierPerformanceReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'carrier', ''), status, created_at, updated_at FROM %s.documents
		WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type carrierStat struct {
		total, delivered, rto int
		turnaroundHoursSum    float64
		turnaroundCount       int
	}
	byCarrier := map[string]*carrierStat{}
	for rows.Next() {
		var carrier, status string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&carrier, &status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if carrier == "" {
			carrier = "(unknown)"
		}
		s, ok := byCarrier[carrier]
		if !ok {
			s = &carrierStat{}
			byCarrier[carrier] = s
		}
		s.total++
		switch status {
		case "Delivered":
			s.delivered++
			s.turnaroundHoursSum += updatedAt.Sub(createdAt).Hours()
			s.turnaroundCount++
		case "RTO":
			s.rto++
		}
	}

	var results []map[string]interface{}
	for carrier, s := range byCarrier {
		rtoRate := 0.0
		if s.total > 0 {
			rtoRate = math.Round(float64(s.rto)/float64(s.total)*10000) / 100
		}
		avgTurnaround := 0.0
		if s.turnaroundCount > 0 {
			avgTurnaround = math.Round(s.turnaroundHoursSum/float64(s.turnaroundCount)*100) / 100
		}
		results = append(results, map[string]interface{}{
			"carrier": carrier, "total_shipments": s.total, "delivered": s.delivered,
			"rto": s.rto, "rto_rate_pct": rtoRate, "avg_turnaround_hours": avgTurnaround,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i]["carrier"].(string) < results[j]["carrier"].(string)
	})
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, nil
}
