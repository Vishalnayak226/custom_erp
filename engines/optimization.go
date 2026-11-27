package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// ReplenishmentSuggestion represents a reorder calculation for a SKU
type ReplenishmentSuggestion struct {
	SKU           string  `json:"sku"`
	LocationCode  string  `json:"location_code"`
	Available     int     `json:"available"`
	DailyVelocity float64 `json:"daily_sales_velocity"`
	ReorderPoint  int     `json:"reorder_point"`
	SuggestedQty  int     `json:"suggested_qty"`
	SafetyStock   int     `json:"safety_stock"`
	LeadTimeDays  int     `json:"lead_time_days"`
	// Stage 146.1.4: the position the reorder compares - available plus what
	// is already coming, less what is waiting. Zero parts are left out.
	Inward     int `json:"inward,omitempty"`
	OnOrder    int `json:"on_order,omitempty"`
	InTransit  int `json:"in_transit,omitempty"`
	BackOrders int `json:"back_orders,omitempty"`
	Position   int `json:"position"`
}

// soldQtySQL sums what was sold in the last $1 days, per location and SKU,
// inside the database (273.1, 273.5). Three parts:
//   - Till sales: paid carts. 'Paid' is set by checkout (handleCheckout);
//     'Settled' is a marketplace-channel order after reconciliation
//     (ProcessMarketplaceSettlement) - it was paid first.
//   - Posted Sales Invoices (Approved or Paid, posted_amounts.go's rule) at
//     their dispatch store, data->>'location', the store issueSalesInvoiceStockTx
//     issues from. Lines come as a list or as a JSON-encoded string.
//   - Less sales returns against those invoices: a SalesReturn, or a
//     ReturnRequest not Rejected (assertReturnEligibleTx's "already claimed"
//     rule). The return names the invoice, or the SalesOrder it billed (an RTO);
//     it is taken off at the invoice's store. A return is never older than its
//     invoice, so the date filter on returns only narrows the read.
// Till returns are not netted (owner's decision, ask 274). %[1]s is the tenant
// schema; %[2]s narrows on s.location / s.sku. A line whose qty is not a
// number is skipped.
const soldQtySQL = `
	WITH inv AS (
		SELECT i.id, COALESCE(i.data->>'sales_order_id', '') AS so, COALESCE(i.data->>'location', '') AS location, i.created_at,
		       CASE WHEN jsonb_typeof(i.data->'items') = 'array' THEN i.data->'items'
		            WHEN jsonb_typeof(i.data->'items') = 'string' AND left(i.data->>'items', 1) = '[' THEN (i.data->>'items')::jsonb
		            ELSE '[]'::jsonb END AS items
		  FROM %[1]s.documents i
		 WHERE i.doctype = 'SalesInvoice' AND i.status IN ('Approved', 'Paid') AND i.deleted_at IS NULL
		   AND i.created_at >= NOW() - $1 * INTERVAL '1 day'
	), ret AS (
		SELECT COALESCE(inv.location, io.location) AS location,
		       CASE WHEN jsonb_typeof(r.data->'items') = 'array' THEN r.data->'items'
		            WHEN jsonb_typeof(r.data->'items') = 'string' AND left(r.data->>'items', 1) = '[' THEN (r.data->>'items')::jsonb
		            ELSE '[]'::jsonb END AS items
		  FROM %[1]s.documents r
		  LEFT JOIN inv ON inv.id = r.data->>'original_order_id'
		  LEFT JOIN (SELECT DISTINCT ON (so) so, location FROM inv WHERE so <> '' ORDER BY so, created_at) io
		         ON io.so = r.data->>'original_order_id'
		 WHERE r.doctype IN ('SalesReturn', 'ReturnRequest') AND r.deleted_at IS NULL
		   AND COALESCE(r.data->>'status', '') <> 'Rejected'
		   AND r.created_at >= NOW() - $1 * INTERVAL '1 day'
		   AND (inv.id IS NOT NULL OR io.so IS NOT NULL)
	)
	SELECT s.location, s.sku, GREATEST(SUM(s.qty), 0) FROM (
		SELECT COALESCE(d.data->>'location', ''), COALESCE(it->>'sku', ''), (it->>'qty')::numeric
		  FROM %[1]s.documents d
		 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(d.data->'items') = 'array' THEN d.data->'items' ELSE '[]'::jsonb END) it
		 WHERE d.doctype = 'POSCart' AND d.status IN ('Paid', 'Settled')
		   AND d.created_at >= NOW() - $1 * INTERVAL '1 day'
		   AND jsonb_typeof(it->'qty') = 'number'
		UNION ALL
		SELECT inv.location, COALESCE(it->>'sku', ''), (it->>'qty')::numeric
		  FROM inv CROSS JOIN LATERAL jsonb_array_elements(inv.items) it
		 WHERE jsonb_typeof(it->'qty') = 'number'
		UNION ALL
		SELECT ret.location, COALESCE(it->>'sku', ''), -(it->>'qty')::numeric
		  FROM ret CROSS JOIN LATERAL jsonb_array_elements(ret.items) it
		 WHERE jsonb_typeof(it->'qty') = 'number'
	) s(location, sku, qty)
	 WHERE true %[2]s
	 GROUP BY 1, 2`

func velocityDivisor(daysRange int) float64 {
	if daysRange <= 0 {
		return 1.0
	}
	return float64(daysRange)
}

// CalculateSalesVelocity is a SKU's average daily sales at a location over the
// last daysRange days: till sales plus posted Sales Invoices, less returns
// against those invoices (soldQtySQL). Until 273.1 it read and decoded every
// paid cart of every store for each SKU; it is now one sum in the database. A
// loop over many SKUs at one store reads SalesVelocityAt once instead.
func CalculateSalesVelocity(tenantID string, locationCode string, sku string, daysRange int) (float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	var sold float64
	q := `SELECT COALESCE(SUM(v.qty), 0) FROM (` + fmt.Sprintf(soldQtySQL, schema, `AND s.location = $2 AND s.sku = $3`) + `) v(location, sku, qty)`
	if err := db.DB.QueryRow(q, daysRange, locationCode, sku).Scan(&sold); err != nil {
		return 0, err
	}
	return sold / velocityDivisor(daysRange), nil
}

// SalesVelocityAt is CalculateSalesVelocity for every SKU sold at one
// location, from one read (273.1). A SKU with no sales is absent (reads 0).
func SalesVelocityAt(tenantID, locationCode string, daysRange int) (map[string]float64, error) {
	all, err := salesVelocityBy(tenantID, daysRange, &locationCode)
	if err != nil {
		return nil, err
	}
	return all[locationCode], nil
}

// SalesVelocityAllLocations is every location's SKU velocities from one read:
// location -> SKU -> average daily sales (rebalancing, 273.2).
func SalesVelocityAllLocations(tenantID string, daysRange int) (map[string]map[string]float64, error) {
	return salesVelocityBy(tenantID, daysRange, nil)
}

func salesVelocityBy(tenantID string, daysRange int, location *string) (map[string]map[string]float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	args := []interface{}{daysRange}
	narrow := ""
	if location != nil {
		narrow = `AND s.location = $2`
		args = append(args, *location)
	}
	rows, err := db.DB.Query(fmt.Sprintf(soldQtySQL, schema, narrow), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]float64{}
	div := velocityDivisor(daysRange)
	for rows.Next() {
		var loc, sku string
		var sold float64
		if err := rows.Scan(&loc, &sku, &sold); err != nil {
			return nil, err
		}
		if out[loc] == nil {
			out[loc] = map[string]float64{}
		}
		out[loc][sku] = sold / div
	}
	return out, rows.Err()
}

// 134.6 (47.10.1): most rows the replenishment (lowest stock first) and SLA-breach (oldest task first) scans read before their Go filter.
const optimizationScanRowCap = 5000

// GetReplenishmentSuggestions checks stock levels and triggers reorder suggestions based on sales velocities
func GetReplenishmentSuggestions(tenantID string, locationCode string, leadTimeDays int, safetyStock int) ([]ReplenishmentSuggestion, error) {
	// Stage 146.1.4: one calculation for both entry points (planning.go), so
	// stock already on order, in transit or inward is counted here too.
	suggestions, err := replenishmentSuggestions(tenantID, locationCode, leadTimeDays, safetyStock, false)
	if len(suggestions) == 0 {
		suggestions = nil
	}
	return suggestions, err
}

// SLABreachReport represents a fulfillment task exceeding standard completion times
type SLABreachReport struct {
	TaskID         string  `json:"task_id"`
	OrderID        string  `json:"order_id"`
	LocationCode   string  `json:"location_code"`
	Status         string  `json:"status"`
	MinutesElapsed float64 `json:"minutes_elapsed"`
	ThresholdMins  float64 `json:"threshold_minutes"`
	Breached       bool    `json:"breached"`
}

// GetSLABreaches scans open fulfillment tasks and checks them against threshold durations
func GetSLABreaches(tenantID string, thresholdMinutes float64) ([]SLABreachReport, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	// Query open tasks (Pending, Picking, Packed).
	//
	// Elapsed minutes are computed in SQL rather than with time.Since in Go
	// (Stage 35.2). documents.created_at is a bare TIMESTAMP holding the
	// database server's local wall clock, but lib/pq returns it tagged UTC, so
	// time.Since() was off by the server's UTC offset - on an Asia/Calcutta
	// deployment it reported every open task as 330 minutes in the future,
	// which made `elapsed > thresholdMinutes` false for anything younger than
	// 5.5 hours plus the threshold. SLA breaches simply never fired for the
	// window that matters most. NOW() and created_at share the database's own
	// frame, so their difference is correct in any time zone.
	query := fmt.Sprintf(`
		SELECT id, data, created_at, EXTRACT(EPOCH FROM (NOW() - created_at)) / 60 FROM %s.documents
		WHERE doctype = 'FulfillmentTask' AND status IN ('Pending', 'Picking', 'Packed')
		ORDER BY created_at, id
		LIMIT %d`, schema, optimizationScanRowCap)
	rows, err := db.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []SLABreachReport
	for rows.Next() {
		var id string
		var dataBytes []byte
		var createdAt time.Time
		var elapsedMinutes float64
		if errScan := rows.Scan(&id, &dataBytes, &createdAt, &elapsedMinutes); errScan == nil {
			var task struct {
				OrderID      string `json:"order_id"`
				LocationCode string `json:"location_code"`
				Status       string `json:"status"`
			}
			if errJson := json.Unmarshal(dataBytes, &task); errJson == nil {
				elapsed := elapsedMinutes
				if elapsed > thresholdMinutes {
					reports = append(reports, SLABreachReport{
						TaskID:         id,
						OrderID:        task.OrderID,
						LocationCode:   task.LocationCode,
						Status:         task.Status,
						MinutesElapsed: math.Round(elapsed*100) / 100,
						ThresholdMins:  thresholdMinutes,
						Breached:       true,
					})
				}
			}
		}
	}

	return reports, nil
}

// ForecastDemand projects future SKU demand based on historical checkout velocity
func ForecastDemand(tenantID string, locationCode string, sku string, forecastDays int) (float64, error) {
	// Compute velocity over the past 30 days
	velocity, err := CalculateSalesVelocity(tenantID, locationCode, sku, 30)
	if err != nil {
		return 0, err
	}
	// Projection: velocity * days
	forecasted := velocity * float64(forecastDays)
	return math.Round(forecasted*100) / 100, nil
}
