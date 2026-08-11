package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 35.2 - the OMS Console's read layer.
//
// Why this file exists at all: the console screen it feeds used to be
// renderOMSWorkbenchView pulling GET /api/v1/doc/SalesOrder, /FulfillmentTask,
// /LogisticsBooking and /SalesInvoice in full and joining them in the browser.
// That is fine at a demo's data volume and untenable at a real one - it has no
// filter, no pagination and no ordering, and it transfers every order a tenant
// has ever taken on every page view. Faceting in particular cannot be done
// client-side on a page that only holds one page of rows.
//
// So the filtering, counting and joining move to SQL, and the browser receives
// one page plus its facet counts. No new query language and no search engine
// (plan §9): these are ordinary indexed lookups over the documents table.

// OrderConsoleFilter is the console's faceted query. Every field is optional;
// the zero value lists the most recent orders unfiltered.
type OrderConsoleFilter struct {
	Channel    string
	Status     string
	HoldReason string
	Location   string
	FromDate   string // YYYY-MM-DD, inclusive
	ToDate     string // YYYY-MM-DD, inclusive
	// SLAMinutes > 0 restricts to orders older than that many minutes which
	// have not yet reached a terminal status - the "SLA-breached" facet
	// Unicommerce's dashboard leads with.
	SLAMinutes int
	Limit      int
	Offset     int
}

// OrderConsoleFacet is one facet value and how many orders carry it.
type OrderConsoleFacet struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// OrderConsoleResult is one page of orders plus the facet counts for the
// filter set around it.
type OrderConsoleResult struct {
	Rows   []map[string]interface{}       `json:"rows"`
	Total  int                            `json:"total"`
	Limit  int                            `json:"limit"`
	Offset int                            `json:"offset"`
	Facets map[string][]OrderConsoleFacet `json:"facets"`
}

// terminalOrderStatuses are the statuses an order can no longer breach an SLA
// from - it is already out the door or dead.
var terminalOrderStatuses = []string{"Shipped", "Delivered", "Closed", "Cancelled"}

// orderConsoleConditions builds the WHERE fragments and args for a filter.
// skipDimension omits one dimension's own condition, which is what makes a
// facet count mean "how many would I get if I picked this value" rather than
// "how many of the rows I am already looking at" - the latter would always
// report the selected facet as the only non-zero one.
func orderConsoleConditions(schema string, f OrderConsoleFilter, skipDimension string) (clauses []string, args []interface{}) {
	argOffset := 0
	add := func(clause string, arg interface{}) {
		argOffset++
		clauses = append(clauses, fmt.Sprintf(clause, argOffset))
		args = append(args, arg)
	}
	if f.Channel != "" && skipDimension != "channel" {
		// Manual orders are stored with an empty channel, so the console's
		// "Manual" facet has to map back to that rather than to a literal.
		if f.Channel == "Manual" {
			clauses = append(clauses, "COALESCE(d.data->>'channel', '') = ''")
		} else {
			add("d.data->>'channel' = $%d", f.Channel)
		}
	}
	if f.Status != "" && skipDimension != "status" {
		add("d.status = $%d", f.Status)
	}
	if f.HoldReason != "" && skipDimension != "hold_reason" {
		add("COALESCE(d.data->>'hold_reason', '') = $%d", f.HoldReason)
	}
	if f.Location != "" && skipDimension != "location" {
		add(`EXISTS (SELECT 1 FROM `+schema+`.documents l WHERE l.doctype = 'SalesOrderLine' AND l.data->>'order_id' = d.id AND l.deleted_at IS NULL AND l.data->>'location_code' = $%d)`, f.Location)
	}
	if f.FromDate != "" {
		add("d.created_at >= $%d::date", f.FromDate)
	}
	if f.ToDate != "" {
		// Inclusive of the whole end day - a date-only filter that excluded
		// today's orders would look like data loss to the person using it.
		add("d.created_at < ($%d::date + INTERVAL '1 day')", f.ToDate)
	}
	if f.SLAMinutes > 0 {
		argOffset++
		clauses = append(clauses, fmt.Sprintf("d.created_at < NOW() - ($%d || ' minutes')::interval", argOffset))
		args = append(args, f.SLAMinutes)
		clauses = append(clauses, "d.status <> ALL($"+fmt.Sprint(argOffset+1)+")")
		args = append(args, pqTextArray(terminalOrderStatuses))
	}
	return clauses, args
}

// pqTextArray renders a Go string slice as a Postgres text[] literal. The repo
// has no pq.Array helper available in engines/ (lib/pq is only wired up in
// db/), and a literal keeps this a single parameter rather than N.
func pqTextArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		quoted = append(quoted, `"`+strings.ReplaceAll(v, `"`, `\"`)+`"`)
	}
	return "{" + strings.Join(quoted, ",") + "}"
}

func whereFrom(clauses []string) string {
	base := "d.doctype = 'SalesOrder' AND d.deleted_at IS NULL"
	if len(clauses) == 0 {
		return base
	}
	return base + " AND " + strings.Join(clauses, " AND ")
}

// ListOrdersForConsole returns one page of cross-channel orders plus the facet
// counts the console's filter rail renders.
func ListOrdersForConsole(tenantID string, f OrderConsoleFilter) (*OrderConsoleResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	clauses, args := orderConsoleConditions(schema, f, "")
	where := whereFrom(clauses)

	result := &OrderConsoleResult{Limit: f.Limit, Offset: f.Offset, Facets: map[string][]OrderConsoleFacet{}}

	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents d WHERE %s`, schema, where), args...).Scan(&result.Total); err != nil {
		return nil, err
	}

	rowArgs := append(append([]interface{}{}, args...), f.Limit, f.Offset)
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT d.id,
		       COALESCE(d.data->>'channel', ''),
		       COALESCE(d.data->>'channel_order_id', ''),
		       COALESCE(d.data->>'customer_name', ''),
		       COALESCE(d.data->>'customer_phone', ''),
		       d.status,
		       COALESCE(d.data->>'hold_reason', ''),
		       COALESCE(d.data->>'payment_status', ''),
		       COALESCE((d.data->>'total_amount')::numeric, 0),
		       COALESCE(d.data->>'priority', ''),
		       d.created_at,
		       -- Age is computed in SQL, not as time.Since(created_at) in Go.
		       -- documents.created_at is a bare TIMESTAMP (no time zone)
		       -- holding the database server's local wall clock, but lib/pq
		       -- hands it back tagged UTC - so on this deployment
		       -- (timezone = Asia/Calcutta) a Go-side subtraction reports every
		       -- order as 330 minutes in the FUTURE. NOW() and created_at are
		       -- both in the database's own frame, so the difference is right
		       -- whatever zone the server runs in.
		       GREATEST(EXTRACT(EPOCH FROM (NOW() - d.created_at)) / 60, 0)::int,
		       COALESCE((SELECT string_agg(DISTINCT NULLIF(l.data->>'location_code', ''), ', ')
		                 FROM %s.documents l WHERE l.doctype = 'SalesOrderLine' AND l.data->>'order_id' = d.id AND l.deleted_at IS NULL), ''),
		       (SELECT COUNT(*) FROM %s.documents l WHERE l.doctype = 'SalesOrderLine' AND l.data->>'order_id' = d.id AND l.deleted_at IS NULL)
		FROM %s.documents d
		WHERE %s
		ORDER BY (COALESCE(d.data->>'priority', '') = 'Expedite') DESC, d.created_at DESC
		LIMIT $%d OFFSET $%d`,
		schema, schema, schema, where, len(args)+1, len(args)+2), rowArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, channel, channelOrderID, customerName, customerPhone, status, holdReason, paymentStatus, priority, locations string
		var totalAmount float64
		var lineCount, ageMinutes int
		var createdAt time.Time
		if err := rows.Scan(&id, &channel, &channelOrderID, &customerName, &customerPhone, &status, &holdReason,
			&paymentStatus, &totalAmount, &priority, &createdAt, &ageMinutes, &locations, &lineCount); err != nil {
			return nil, err
		}
		terminal := false
		for _, t := range terminalOrderStatuses {
			if status == t {
				terminal = true
				break
			}
		}
		result.Rows = append(result.Rows, map[string]interface{}{
			"order_id": id, "channel": channel, "channel_order_id": channelOrderID,
			"customer_name": customerName, "customer_phone": customerPhone,
			"status": status, "hold_reason": holdReason, "payment_status": paymentStatus,
			"total_amount": totalAmount, "priority": priority, "created_at": createdAt,
			"locations": locations, "line_count": lineCount,
			"age_minutes": ageMinutes,
			"terminal":    terminal,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result.Rows == nil {
		result.Rows = []map[string]interface{}{}
	}

	for dimension, expr := range map[string]string{
		"channel":     "COALESCE(NULLIF(d.data->>'channel', ''), 'Manual')",
		"status":      "d.status",
		"hold_reason": "NULLIF(d.data->>'hold_reason', '')",
	} {
		facets, err := orderConsoleFacet(schema, f, dimension, expr)
		if err != nil {
			return nil, err
		}
		result.Facets[dimension] = facets
	}
	locationFacets, err := orderConsoleLocationFacet(schema, f)
	if err != nil {
		return nil, err
	}
	result.Facets["location"] = locationFacets

	return result, nil
}

func orderConsoleFacet(schema string, f OrderConsoleFilter, dimension, expr string) ([]OrderConsoleFacet, error) {
	clauses, args := orderConsoleConditions(schema, f, dimension)
	where := whereFrom(clauses)
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT %s AS facet_value, COUNT(*) FROM %s.documents d
		WHERE %s AND %s IS NOT NULL
		GROUP BY 1 ORDER BY 2 DESC, 1`, expr, schema, where, expr), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrderConsoleFacet{}
	for rows.Next() {
		var fc OrderConsoleFacet
		if err := rows.Scan(&fc.Value, &fc.Count); err != nil {
			return nil, err
		}
		out = append(out, fc)
	}
	return out, rows.Err()
}

// orderConsoleLocationFacet counts orders per fulfilment location. Separate
// from orderConsoleFacet because location lives on the lines, so one order can
// legitimately count towards two locations (a split allocation) - DISTINCT on
// the order id is what stops it being counted twice for the same location.
func orderConsoleLocationFacet(schema string, f OrderConsoleFilter) ([]OrderConsoleFacet, error) {
	clauses, args := orderConsoleConditions(schema, f, "location")
	where := whereFrom(clauses)
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT l.data->>'location_code' AS facet_value, COUNT(DISTINCT d.id)
		FROM %s.documents d
		JOIN %s.documents l ON l.doctype = 'SalesOrderLine' AND l.data->>'order_id' = d.id AND l.deleted_at IS NULL
		WHERE %s AND NULLIF(l.data->>'location_code', '') IS NOT NULL
		GROUP BY 1 ORDER BY 2 DESC, 1`, schema, schema, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrderConsoleFacet{}
	for rows.Next() {
		var fc OrderConsoleFacet
		if err := rows.Scan(&fc.Value, &fc.Count); err != nil {
			return nil, err
		}
		out = append(out, fc)
	}
	return out, rows.Err()
}

// GetOrderConsoleDetail assembles everything the order detail screen shows in
// one call: the order, its lines with per-line status and allocation, the
// reservations behind them, fulfillment tasks, shipments, invoices, returns,
// refunds, the notification log and the audit trail.
//
// One endpoint rather than the nine the browser would otherwise fire, because
// the detail screen is useless in halves - a page that renders lines but is
// still waiting on shipments invites the exact "is it missing or still
// loading?" ambiguity the second principle is about.
func GetOrderConsoleDetail(tenantID, orderID string) (map[string]interface{}, error) {
	schema, orderData, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return nil, err
	}

	detail := map[string]interface{}{"order_id": orderID, "order": orderData}

	// Lines, with the reservation currently backing each one.
	lines, err := queryDocs(schema, `
		SELECT d.id, d.status, COALESCE(d.data->>'sku', ''), COALESCE((d.data->>'qty')::int, 0),
		       COALESCE((d.data->>'unit_price')::numeric, 0), COALESCE(d.data->>'location_code', ''),
		       COALESCE(d.data->>'line_status', ''), COALESCE(d.data->>'hold_reason', '')
		FROM `+schema+`.documents d
		WHERE d.doctype = 'SalesOrderLine' AND d.data->>'order_id' = $1 AND d.deleted_at IS NULL
		ORDER BY d.id`, orderID, func(scan func(...interface{}) error) (map[string]interface{}, error) {
		var id, status, sku, location, lineStatus, holdReason string
		var qty int
		var unitPrice float64
		if err := scan(&id, &status, &sku, &qty, &unitPrice, &location, &lineStatus, &holdReason); err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"line_id": id, "status": status, "sku": sku, "qty": qty, "unit_price": unitPrice,
			"location_code": location, "line_status": lineStatus, "hold_reason": holdReason,
			"line_total": unitPrice * float64(qty),
		}, nil
	})
	if err != nil {
		return nil, err
	}
	detail["lines"] = lines

	// Reservations are not order-linked in the schema (inventory_reservation
	// has no order_id - see the note in oms_reports.go), so they are matched
	// by the SKU/location pairs this order's lines actually allocated to.
	// Stated rather than hidden: this is the closest honest answer available,
	// and it can include another order's reservation on the same pair.
	reservations, err := queryDocs(schema, `
		SELECT r.sku, r.location_code, r.quantity, r.reservation_type, r.expires_at
		FROM `+schema+`.inventory_reservation r
		WHERE (r.sku, r.location_code) IN (
			SELECT l.data->>'sku', l.data->>'location_code' FROM `+schema+`.documents l
			WHERE l.doctype = 'SalesOrderLine' AND l.data->>'order_id' = $1 AND l.deleted_at IS NULL
			  AND NULLIF(l.data->>'location_code', '') IS NOT NULL)
		ORDER BY r.expires_at`, orderID, func(scan func(...interface{}) error) (map[string]interface{}, error) {
		var sku, location, resType string
		var qty int
		var expiresAt time.Time
		if err := scan(&sku, &location, &qty, &resType, &expiresAt); err != nil {
			return nil, err
		}
		return map[string]interface{}{"sku": sku, "location_code": location, "quantity": qty,
			"reservation_type": resType, "expires_at": expiresAt}, nil
	})
	if err != nil {
		return nil, err
	}
	detail["reservations"] = reservations

	// The related-document sections all share one shape: doctype rows whose
	// data->>'order_id' is this order.
	for key, spec := range map[string]struct{ doctype, extra string }{
		"fulfillment_tasks": {"FulfillmentTask", "COALESCE(d.data->>'location_code', '')"},
		"shipments":         {"LogisticsBooking", "COALESCE(d.data->>'tracking_number', '')"},
		"returns":           {"ReturnRequest", "COALESCE(d.data->>'request_type', '')"},
		"refunds":           {"RefundRequest", "COALESCE(d.data->>'refund_mode', '')"},
		"notifications":     {"NotificationLog", "COALESCE(d.data->>'event', '')"},
	} {
		rowsOut, err := queryDocs(schema, `
			SELECT d.id, d.status, `+spec.extra+`, d.created_at
			FROM `+schema+`.documents d
			WHERE d.doctype = '`+spec.doctype+`' AND d.data->>'order_id' = $1 AND d.deleted_at IS NULL
			ORDER BY d.created_at DESC`, orderID, func(scan func(...interface{}) error) (map[string]interface{}, error) {
			var id, status, extra string
			var createdAt time.Time
			if err := scan(&id, &status, &extra, &createdAt); err != nil {
				return nil, err
			}
			return map[string]interface{}{"id": id, "status": status, "detail": extra, "created_at": createdAt}, nil
		})
		if err != nil {
			return nil, err
		}
		detail[key] = rowsOut
	}

	// SalesInvoice references the order through sales_order_id, not order_id.
	invoices, err := queryDocs(schema, `
		SELECT d.id, d.status, COALESCE((d.data->>'total_amount')::numeric, 0)::text, d.created_at
		FROM `+schema+`.documents d
		WHERE d.doctype = 'SalesInvoice' AND d.data->>'sales_order_id' = $1 AND d.deleted_at IS NULL
		ORDER BY d.created_at DESC`, orderID, func(scan func(...interface{}) error) (map[string]interface{}, error) {
		var id, status, amount string
		var createdAt time.Time
		if err := scan(&id, &status, &amount, &createdAt); err != nil {
			return nil, err
		}
		return map[string]interface{}{"id": id, "status": status, "detail": amount, "created_at": createdAt}, nil
	})
	if err != nil {
		return nil, err
	}
	detail["invoices"] = invoices

	// Audit trail. audit_logs is not document-linked either - it stores a free
	// text detail - so the order id is matched inside it. Bounded, because an
	// order that has been round-tripped many times should not return an
	// unbounded log to a browser.
	audit, err := queryDocs(schema, `
		SELECT a.user_id, a.action, a.status, COALESCE(a.details, ''), a.created_at
		FROM `+schema+`.audit_logs a
		WHERE a.details LIKE '%' || $1 || '%'
		ORDER BY a.created_at DESC LIMIT 100`, orderID, func(scan func(...interface{}) error) (map[string]interface{}, error) {
		var userID, action, status, details string
		var createdAt time.Time
		if err := scan(&userID, &action, &status, &details, &createdAt); err != nil {
			return nil, err
		}
		return map[string]interface{}{"user_id": userID, "action": action, "status": status,
			"details": details, "created_at": createdAt}, nil
	})
	if err != nil {
		return nil, err
	}
	detail["audit_trail"] = audit

	return detail, nil
}

// queryDocs runs one query and maps each row through scanFn. A small local
// helper rather than a package-wide one: it exists only to keep the eleven
// near-identical blocks in GetOrderConsoleDetail from being eleven copies of
// the same rows/defer/scan/append boilerplate.
func queryDocs(schema, query string, arg interface{}, scanFn func(scan func(...interface{}) error) (map[string]interface{}, error)) ([]map[string]interface{}, error) {
	rows, err := db.DB.Query(query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		item, err := scanFn(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// SearchOrdersGlobal is 35.2.6: one lookup that resolves whatever identifier
// the person on the phone actually has - the order id, the channel's own order
// id, an AWB/tracking number, a customer phone number, or a SKU on the order.
//
// Deliberately a UNION of indexed lookups rather than a search engine (plan
// §9). Each arm reports how it matched, so the result list can say *why* an
// order came back - which matters when a SKU search returns forty orders.
func SearchOrdersGlobal(tenantID, query string, limit int) ([]map[string]interface{}, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []map[string]interface{}{}, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	// Phone numbers are stored normalized (Stage 41), so a search for
	// "+91 98765 43210" has to be normalized the same way before it can match.
	phoneQuery := NormalizeTenantPhone(tenantID, query).National
	if phoneQuery == "" {
		phoneQuery = query
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH matches AS (
			SELECT d.id AS order_id, 'Order ID' AS matched_on FROM %s.documents d
			  WHERE d.doctype = 'SalesOrder' AND d.deleted_at IS NULL AND d.id ILIKE '%%' || $1 || '%%'
			UNION
			SELECT d.id, 'Channel order ID' FROM %s.documents d
			  WHERE d.doctype = 'SalesOrder' AND d.deleted_at IS NULL AND d.data->>'channel_order_id' ILIKE '%%' || $1 || '%%'
			UNION
			SELECT d.id, 'Customer phone' FROM %s.documents d
			  WHERE d.doctype = 'SalesOrder' AND d.deleted_at IS NULL AND d.data->>'customer_phone' ILIKE '%%' || $2 || '%%'
			UNION
			SELECT d.id, 'Customer name' FROM %s.documents d
			  WHERE d.doctype = 'SalesOrder' AND d.deleted_at IS NULL AND d.data->>'customer_name' ILIKE '%%' || $1 || '%%'
			UNION
			SELECT l.data->>'order_id', 'SKU' FROM %s.documents l
			  WHERE l.doctype = 'SalesOrderLine' AND l.deleted_at IS NULL AND l.data->>'sku' ILIKE '%%' || $1 || '%%'
			UNION
			SELECT b.data->>'order_id', 'AWB / tracking' FROM %s.documents b
			  WHERE b.doctype = 'LogisticsBooking' AND b.deleted_at IS NULL AND b.data->>'tracking_number' ILIKE '%%' || $1 || '%%'
		)
		SELECT d.id, string_agg(DISTINCT m.matched_on, ', '), COALESCE(NULLIF(d.data->>'channel', ''), 'Manual'),
		       COALESCE(d.data->>'channel_order_id', ''), COALESCE(d.data->>'customer_name', ''),
		       d.status, COALESCE((d.data->>'total_amount')::numeric, 0), d.created_at
		FROM matches m
		JOIN %s.documents d ON d.id = m.order_id AND d.doctype = 'SalesOrder' AND d.deleted_at IS NULL
		GROUP BY d.id, d.data, d.status, d.created_at
		ORDER BY d.created_at DESC
		LIMIT $3`, schema, schema, schema, schema, schema, schema, schema), query, phoneQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []map[string]interface{}{}
	for rows.Next() {
		var id, matchedOn, channel, channelOrderID, customerName, status string
		var totalAmount float64
		var createdAt time.Time
		if err := rows.Scan(&id, &matchedOn, &channel, &channelOrderID, &customerName, &status, &totalAmount, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"order_id": id, "matched_on": matchedOn, "channel": channel,
			"channel_order_id": channelOrderID, "customer_name": customerName,
			"status": status, "total_amount": totalAmount, "created_at": createdAt,
		})
	}
	return out, rows.Err()
}

// OMSBulkResult reports per-order outcomes of a bulk action.
type OMSBulkResult struct {
	Succeeded []string          `json:"succeeded"`
	Failed    map[string]string `json:"failed"`
}

// BulkOrderAction applies one console action across a selection (35.2.5).
//
// Loops the single-order engine functions rather than writing a set-based
// variant of each - the same shape BulkDecideApproval established. Every
// per-order guard (reason-code validity, the cancellation stage gate, "not On
// Hold") therefore still applies to each order individually, and one order's
// refusal never rolls back the others.
func BulkOrderAction(tenantID, action string, orderIDs []string, reasonCode, userID string) (*OMSBulkResult, error) {
	result := &OMSBulkResult{Succeeded: []string{}, Failed: map[string]string{}}
	if len(orderIDs) == 0 {
		return result, nil
	}
	if len(orderIDs) > 500 {
		return nil, fmt.Errorf("a bulk action is limited to 500 orders at a time, got %d", len(orderIDs))
	}
	for _, orderID := range orderIDs {
		var err error
		switch action {
		case "hold":
			err = PlaceOrderHold(tenantID, orderID, reasonCode, userID)
		case "release":
			err = ReleaseOrderHold(tenantID, orderID)
		case "cancel":
			err = CancelOrder(tenantID, orderID, reasonCode)
		default:
			return nil, fmt.Errorf("unknown bulk action %q (expected hold, release or cancel)", action)
		}
		if err != nil {
			result.Failed[orderID] = err.Error()
			continue
		}
		result.Succeeded = append(result.Succeeded, orderID)
	}
	LogAuditEvent(tenantID, userID, "OMS_BULK_"+strings.ToUpper(action), "SUCCESS",
		fmt.Sprintf("%d succeeded, %d failed", len(result.Succeeded), len(result.Failed)))
	return result, nil
}

// OMSConsoleTiles returns the four headline numbers the console's tiles show,
// each computed by running the already-registered report behind it rather than
// by a second, separately-maintained query (35.2.4). If a report fails, its
// tile reports the error instead of the whole console failing.
func OMSConsoleTiles(tenantID, role string) []map[string]interface{} {
	tiles := []struct{ id, label, reportID string }{
		{"exceptions", "Integration exceptions", "oms-exception-queue"},
		{"sla", "SLA breached", "sla-breach"},
		{"allocation", "Allocation pending", "allocation-pending"},
		{"variance", "Reconciliation variance", "oms-reconciliation-variance"},
	}
	out := make([]map[string]interface{}, 0, len(tiles))
	for _, t := range tiles {
		tile := map[string]interface{}{"id": t.id, "label": t.label, "report_id": t.reportID}
		_, rows, _, err := RunReport(tenantID, t.reportID, role, "", nil)
		if err != nil {
			tile["error"] = err.Error()
			tile["count"] = 0
		} else {
			tile["count"] = len(rows)
		}
		out = append(out, tile)
	}
	return out
}

// SaveOMSView stores a named filter set for the console's saved views
// (35.2.1). Implemented on the doctype engine as an OMSSavedView document
// rather than a new preferences table - the doctype engine already gives it
// RBAC, audit and a list view for free.
func SaveOMSView(tenantID, userID, name string, filter OrderConsoleFilter) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("a saved view needs a name")
	}
	// The owner is what scopes every read and the delete, so an unattributed
	// view would be visible to every other unattributed session. Refuse rather
	// than store one.
	if strings.TrimSpace(userID) == "" {
		return "", fmt.Errorf("a saved view needs an owner")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	filterJSON, err := json.Marshal(filter)
	if err != nil {
		return "", err
	}
	viewID := NewDocID("OSV")
	doc, err := json.Marshal(map[string]interface{}{
		"code": viewID, "name": name, "owner": userID, "filter": string(filterJSON),
	})
	if err != nil {
		return "", err
	}
	// created_by is 'system', not userID: documents.created_by carries a
	// foreign key to users, and the owner this view is actually scoped by is
	// already stored in data->>'owner'. Writing the owner into both would make
	// saving a view fail for any caller whose id is not a users row - which is
	// every engine-internal and test caller - for no gain, since nothing reads
	// created_by here. Same choice CreateSalesOrder and writeNotificationLog
	// make for their engine-written documents.
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'OMSSavedView', $2, 'Active', 'system')`, schema),
		viewID, doc); err != nil {
		return "", err
	}
	return viewID, nil
}

// ListOMSViews returns the saved views visible to a user: their own, plus
// anything saved by an account that no longer exists is simply theirs to see
// too - views are a convenience, not a permission boundary, and the filters
// they carry grant no access the console does not already give.
func ListOMSViews(tenantID, userID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return queryDocs(schema, `
		SELECT d.id, COALESCE(d.data->>'name', ''), COALESCE(d.data->>'filter', '{}'), COALESCE(d.data->>'owner', '')
		FROM `+schema+`.documents d
		WHERE d.doctype = 'OMSSavedView' AND d.deleted_at IS NULL AND d.status = 'Active'
		  AND COALESCE(d.data->>'owner', '') = $1
		ORDER BY d.created_at DESC`, userID, func(scan func(...interface{}) error) (map[string]interface{}, error) {
		var id, name, filterJSON, owner string
		if err := scan(&id, &name, &filterJSON, &owner); err != nil {
			return nil, err
		}
		var filter map[string]interface{}
		_ = json.Unmarshal([]byte(filterJSON), &filter)
		return map[string]interface{}{"id": id, "name": name, "owner": owner, "filter": filter}, nil
	})
}

// DeleteOMSView soft-deletes a saved view, and only the owner's own.
func DeleteOMSView(tenantID, userID, viewID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = 'OMSSavedView' AND COALESCE(data->>'owner', '') = $2 AND deleted_at IS NULL`, schema),
		viewID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("saved view %s not found", viewID)
	}
	return nil
}
