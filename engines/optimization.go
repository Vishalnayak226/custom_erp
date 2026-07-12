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

// GetReplenishmentSuggestions checks stock levels and triggers reorder suggestions based on sales velocities
func GetReplenishmentSuggestions(tenantID string, locationCode string, leadTimeDays int, safetyStock int) ([]ReplenishmentSuggestion, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	// Fetch all inventory availability records for the location
	query := fmt.Sprintf(`
		SELECT sku, available FROM %s.inventory_availability 
		WHERE location_code = $1`, schema)
	rows, err := db.DB.Query(query, locationCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var suggestions []ReplenishmentSuggestion
	for rows.Next() {
		var sku string
		var available int
		if errScan := rows.Scan(&sku, &available); errScan == nil {
			// Calculate historical daily velocity over 30 days
			velocity, errVel := CalculateSalesVelocity(tenantID, locationCode, sku, 30)
			if errVel != nil {
				velocity = 0.0 // Default to zero velocity on error
			}

			// Formula: ReorderPoint = (DailySalesVelocity * LeadTimeDays) + SafetyStock
			reorderPoint := int(math.Ceil(velocity*float64(leadTimeDays))) + safetyStock

			// If stock is below reorder point, suggest replenishment
			if available < reorderPoint {
				suggestedQty := reorderPoint - available
				suggestions = append(suggestions, ReplenishmentSuggestion{
					SKU:           sku,
					LocationCode:  locationCode,
					Available:     available,
					DailyVelocity: velocity,
					ReorderPoint:  reorderPoint,
					SuggestedQty:  suggestedQty,
					SafetyStock:   safetyStock,
					LeadTimeDays:  leadTimeDays,
				})
			}
		}
	}

	return suggestions, nil
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

	// Query open tasks (Pending, Picking, Packed)
	query := fmt.Sprintf(`
		SELECT id, data, created_at FROM %s.documents 
		WHERE doctype = 'FulfillmentTask' AND status IN ('Pending', 'Picking', 'Packed')`, schema)
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
		if errScan := rows.Scan(&id, &dataBytes, &createdAt); errScan == nil {
			var task struct {
				OrderID      string `json:"order_id"`
				LocationCode string `json:"location_code"`
				Status       string `json:"status"`
			}
			if errJson := json.Unmarshal(dataBytes, &task); errJson == nil {
				elapsed := time.Since(createdAt).Minutes()
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
