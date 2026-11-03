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

// CalculateSalesVelocity calculates average daily sales for a SKU at a location over the last N days
func CalculateSalesVelocity(tenantID string, locationCode string, sku string, daysRange int) (float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}

	// 'Paid' is set by checkout (handleCheckout); 'Settled' is the downstream state a
	// marketplace-channel order transitions to after reconciliation (ProcessMarketplaceSettlement).
	// Both represent a completed sale for velocity purposes - a settled cart was paid first.
	query := fmt.Sprintf(`
		SELECT data FROM %s.documents
		WHERE doctype = 'POSCart' AND status IN ('Paid', 'Settled') AND created_at >= NOW() - $1 * INTERVAL '1 day'`, schema)
	rows, err := db.DB.Query(query, daysRange)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	totalSold := 0
	for rows.Next() {
		var docBytes []byte
		if err := rows.Scan(&docBytes); err == nil {
			var doc struct {
				Location string `json:"location"`
				Items    []struct {
					Sku string `json:"sku"`
					Qty int    `json:"qty"`
				} `json:"items"`
			}
			if errJson := json.Unmarshal(docBytes, &doc); errJson == nil {
				if doc.Location == locationCode {
					for _, item := range doc.Items {
						if item.Sku == sku {
							totalSold += item.Qty
						}
					}
				}
			}
		}
	}

	divisor := float64(daysRange)
	if divisor <= 0 {
		divisor = 1.0
	}
	return float64(totalSold) / divisor, nil
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
