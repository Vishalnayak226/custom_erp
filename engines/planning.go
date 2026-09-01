package engines

import (
	"custom_erp/db"
	"fmt"
	"math"
)

// Stage 37.10: Planning depth - forecasting, reorder points, pegging,
// capacity. Pre-build audit found this ~60% already built under different
// names: ForecastDemand/CalculateSalesVelocity (engines/optimization.go) is
// a real, if naive (flat 30-day average), forecast; GetReplenishmentSuggestions/
// GetMRPSuggestions already compute a reorder point from CALL-SITE lead-
// time/safety-stock parameters; GetProductionSchedule (engines/
// manufacturing_scheduling.go) is already a genuine finite-capacity
// scheduler, just never wired to a report. Pegging is the one genuinely
// new piece. Every new function here is a SIBLING of an existing one, not
// a rewrite - the originals and their existing callers/tests are untouched.

// ---------------------------------------------------------------------------
// 37.10.1: Forecasting - a trend-aware forecast alongside the existing flat
// one, not a replacement for it.
// ---------------------------------------------------------------------------

// CalculateWeightedVelocity blends a RECENT window's velocity with a PRIOR
// window's, weighting the recent one more heavily - more responsive to a
// real trend than CalculateSalesVelocity's single flat 30-day average,
// while reusing that exact function for both windows so the two can never
// disagree about what "velocity over N days" means.
func CalculateWeightedVelocity(tenantID, locationCode, sku string, recentDays, priorDays int, recentWeight float64) (float64, error) {
	if recentWeight < 0 || recentWeight > 1 {
		return 0, fmt.Errorf("recentWeight must be between 0 and 1")
	}
	recentVelocity, err := CalculateSalesVelocity(tenantID, locationCode, sku, recentDays)
	if err != nil {
		return 0, err
	}
	priorVelocity, err := CalculateSalesVelocity(tenantID, locationCode, sku, recentDays+priorDays)
	if err != nil {
		return 0, err
	}
	// priorVelocity above is actually the velocity over the WHOLE window
	// (recent+prior days) since CalculateSalesVelocity always looks back
	// from today - there is no "days N to M ago" query in this codebase to
	// reuse instead of building one. Blending the recent window against
	// that whole-window average still weights recent activity more heavily
	// than a flat single-window average would, which is this function's
	// actual claim - it is not a true isolated prior-period velocity.
	return recentVelocity*recentWeight + priorVelocity*(1-recentWeight), nil
}

// ForecastDemandTrend projects forecastDays of demand using
// CalculateWeightedVelocity's blended rate (7 recent days at 60% weight
// against a 30-day whole-window average at 40%) rather than
// ForecastDemand's flat 30-day average - sensible defaults, not caller-
// tunable, so this stays a simple sibling rather than growing its own
// parameter-tuning surface.
func ForecastDemandTrend(tenantID, locationCode, sku string, forecastDays int) (float64, error) {
	velocity, err := CalculateWeightedVelocity(tenantID, locationCode, sku, 7, 23, 0.6)
	if err != nil {
		return 0, err
	}
	return math.Round(velocity*float64(forecastDays)*100) / 100, nil
}

// ---------------------------------------------------------------------------
// 37.10.2: Reorder points - a persisted per-(item, location) override,
// consulted by new sibling functions rather than mutating
// GetReplenishmentSuggestions/GetMRPSuggestions (existing callers/tests
// keep passing their own blanket lead-time/safety-stock and are unaffected).
// ---------------------------------------------------------------------------

func ValidateReorderPointConfigDocument(tenantID string, payload map[string]interface{}) error {
	if days, ok := parityNumber(payload["lead_time_days"]); ok && days <= 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Lead Time (days)", Message: "lead_time_days must be greater than zero"}
	}
	if qty, ok := parityNumber(payload["safety_stock_qty"]); ok && qty < 0 {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Safety Stock Qty", Message: "safety_stock_qty cannot be negative"}
	}
	return nil
}

// resolveReorderPointConfig looks up an Active per-(item, location) override.
// found=false means "no override, use the caller's own default" - never an
// error, since an item with no config is the normal case for every tenant
// that hasn't opted into per-item overrides yet.
func resolveReorderPointConfig(tenantID, sku, locationCode string) (leadTimeDays, safetyStock int, found bool) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, false
	}
	var leadTime, safety float64
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE((data->>'lead_time_days')::numeric, 0), COALESCE((data->>'safety_stock_qty')::numeric, 0)
		 FROM %s.documents
		 WHERE doctype = 'ReorderPointConfig' AND status = 'Active' AND deleted_at IS NULL
		   AND UPPER(data->>'item') = UPPER($1) AND UPPER(data->>'location_code') = UPPER($2)
		 LIMIT 1`, schema), sku, locationCode).Scan(&leadTime, &safety)
	if err != nil {
		return 0, 0, false
	}
	return int(leadTime), int(safety), true
}

// GetReplenishmentSuggestionsConfigured mirrors GetReplenishmentSuggestions
// (engines/optimization.go) exactly, except each SKU's lead-time/safety-
// stock comes from its own ReorderPointConfig when one exists, falling back
// to the caller's blanket defaultLeadTimeDays/defaultSafetyStock otherwise -
// so a tenant with no configs sees identical output to the original
// function, and one that configures a handful of fast-moving SKUs gets a
// real per-item reorder point without a blanket override for everything else.
func GetReplenishmentSuggestionsConfigured(tenantID, locationCode string, defaultLeadTimeDays, defaultSafetyStock int) ([]ReplenishmentSuggestion, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT sku, available FROM %s.inventory_availability WHERE location_code = $1`, schema), locationCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var suggestions []ReplenishmentSuggestion
	for rows.Next() {
		var sku string
		var available int
		if err := rows.Scan(&sku, &available); err != nil {
			continue
		}
		velocity, verr := CalculateSalesVelocity(tenantID, locationCode, sku, 30)
		if verr != nil {
			velocity = 0.0
		}
		leadTimeDays, safetyStock := defaultLeadTimeDays, defaultSafetyStock
		if configuredLeadTime, configuredSafety, found := resolveReorderPointConfig(tenantID, sku, locationCode); found {
			leadTimeDays, safetyStock = configuredLeadTime, configuredSafety
		}
		reorderPoint := int(math.Ceil(velocity*float64(leadTimeDays))) + safetyStock
		if available < reorderPoint {
			suggestions = append(suggestions, ReplenishmentSuggestion{
				SKU: sku, LocationCode: locationCode, Available: available, DailyVelocity: velocity,
				ReorderPoint: reorderPoint, SuggestedQty: reorderPoint - available,
				SafetyStock: safetyStock, LeadTimeDays: leadTimeDays,
			})
		}
	}
	if suggestions == nil {
		suggestions = []ReplenishmentSuggestion{}
	}
	return suggestions, nil
}

// ---------------------------------------------------------------------------
// 37.10.3: Pegging - genuinely new. Links a SKU's real open demand (open
// SalesOrderLines, not a velocity proxy) against its real open supply
// (on-hand + open PurchaseOrder quantities), so a planner can see exactly
// which orders a given supply figure would cover. Deliberately read-only
// visibility, not an auto-allocation/reservation mechanism - GetMRPSuggestions'
// own velocity-proxy demand model is left untouched, a stated scope
// boundary (a full MRP redesign around real open-order demand is a
// materially larger undertaking than this stage's own scope).
// ---------------------------------------------------------------------------

type PeggedDemandLine struct {
	OrderID    string  `json:"order_id"`
	LineID     string  `json:"line_id"`
	Qty        float64 `json:"qty"`
	LineStatus string  `json:"line_status"`
}

type PeggedSupplyLine struct {
	SourceType string  `json:"source_type"` // "OnHand" or "PurchaseOrder"
	SourceID   string  `json:"source_id"`
	Qty        float64 `json:"qty"`
}

// GetPeggedDemand returns every open SalesOrderLine (Pending/Reserved -
// not yet Dispatched/Cancelled/Returned) needing sku, alongside the real
// supply that could cover it: on-hand at locationCode plus every Approved
// PurchaseOrder's own line quantity for that SKU (PurchaseOrder items are a
// JSONTable, not a child doctype - read via the existing ParsePOLines seam,
// the same one PreviewPurchaseOrder/resolvePOBaseUnitCostPaise already
// share, so this can never disagree with what the PO screen itself shows).
func GetPeggedDemand(tenantID, sku, locationCode string) (demand []PeggedDemandLine, supply []PeggedSupplyLine, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, nil, err
	}
	if sku == "" {
		return nil, nil, fmt.Errorf("sku is required")
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'order_id', ''), COALESCE((data->>'qty')::numeric, 0), status
		FROM %s.documents
		WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL AND data->>'sku' = $1
		  AND status IN ('Pending', 'Reserved')
		ORDER BY created_at ASC`, schema), sku)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var lineID, orderID, status string
		var qty float64
		if err := rows.Scan(&lineID, &orderID, &qty, &status); err != nil {
			rows.Close()
			return nil, nil, err
		}
		demand = append(demand, PeggedDemandLine{OrderID: orderID, LineID: lineID, Qty: qty, LineStatus: status})
	}
	rows.Close()
	if demand == nil {
		demand = []PeggedDemandLine{}
	}

	if locationCode != "" {
		var onHandAvailable int
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT COALESCE(available, 0) FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema),
			sku, locationCode).Scan(&onHandAvailable); err == nil && onHandAvailable > 0 {
			supply = append(supply, PeggedSupplyLine{SourceType: "OnHand", SourceID: locationCode, Qty: float64(onHandAvailable)})
		}
	}

	poRows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data->>'items' FROM %s.documents WHERE doctype = 'PurchaseOrder' AND status = 'Approved' AND deleted_at IS NULL`, schema))
	if err != nil {
		return demand, supply, err
	}
	defer poRows.Close()
	for poRows.Next() {
		var poID, itemsRaw string
		if err := poRows.Scan(&poID, &itemsRaw); err != nil {
			continue
		}
		lines, err := ParsePOLines(itemsRaw)
		if err != nil {
			continue
		}
		for _, l := range lines {
			if l.SKU == sku && l.Qty > 0 {
				supply = append(supply, PeggedSupplyLine{SourceType: "PurchaseOrder", SourceID: poID, Qty: float64(l.Qty)})
			}
		}
	}
	if supply == nil {
		supply = []PeggedSupplyLine{}
	}
	return demand, supply, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "demand-forecast", Label: "Demand Forecast (Trend)", Category: "Inventory",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "location_code", Label: "Location"},
			{Key: "forecast_qty", Label: "Forecasted Qty"},
		},
		Params: []ReportParam{
			{Key: "sku", Label: "SKU", Type: "text", Required: true},
			{Key: "location_code", Label: "Location", Type: "text", Required: true},
			{Key: "forecast_days", Label: "Forecast Days", Type: "text", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			days := 30
			fmt.Sscanf(params["forecast_days"], "%d", &days)
			forecast, err := ForecastDemandTrend(tenantID, params["location_code"], params["sku"], days)
			if err != nil {
				return nil, err
			}
			return []map[string]interface{}{{"sku": params["sku"], "location_code": params["location_code"], "forecast_qty": forecast}}, nil
		},
	})

	RegisterReport(ReportDefinition{
		ID: "reorder-suggestions", Label: "Reorder Suggestions (Configured)", Category: "Inventory",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "location_code", Label: "Location"}, {Key: "available", Label: "Available"},
			{Key: "daily_sales_velocity", Label: "Daily Velocity"}, {Key: "reorder_point", Label: "Reorder Point"},
			{Key: "suggested_qty", Label: "Suggested Qty"}, {Key: "safety_stock", Label: "Safety Stock"}, {Key: "lead_time_days", Label: "Lead Time (days)"},
		},
		Params: []ReportParam{
			{Key: "location_code", Label: "Location", Type: "text", Required: true},
			{Key: "default_lead_time_days", Label: "Default Lead Time (days)", Type: "text", Required: true},
			{Key: "default_safety_stock", Label: "Default Safety Stock", Type: "text", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			leadTime, safety := 7, 0
			fmt.Sscanf(params["default_lead_time_days"], "%d", &leadTime)
			fmt.Sscanf(params["default_safety_stock"], "%d", &safety)
			suggestions, err := GetReplenishmentSuggestionsConfigured(tenantID, params["location_code"], leadTime, safety)
			if err != nil {
				return nil, err
			}
			return structsToRows(suggestions)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "pegged-demand-supply", Label: "Pegged Demand & Supply", Category: "Inventory",
		Columns: []ReportColumn{
			{Key: "kind", Label: "Kind"}, {Key: "source_type", Label: "Type"}, {Key: "reference", Label: "Reference"}, {Key: "qty", Label: "Qty"}, {Key: "status", Label: "Status"},
		},
		Params: []ReportParam{
			{Key: "sku", Label: "SKU", Type: "text", Required: true},
			{Key: "location_code", Label: "Location (for on-hand supply)", Type: "text", Required: false},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			demand, supply, err := GetPeggedDemand(tenantID, params["sku"], params["location_code"])
			if err != nil {
				return nil, err
			}
			out := make([]map[string]interface{}, 0, len(demand)+len(supply))
			for _, d := range demand {
				out = append(out, map[string]interface{}{"kind": "Demand", "source_type": "SalesOrderLine", "reference": d.OrderID, "qty": d.Qty, "status": d.LineStatus})
			}
			for _, s := range supply {
				out = append(out, map[string]interface{}{"kind": "Supply", "source_type": s.SourceType, "reference": s.SourceID, "qty": s.Qty, "status": ""})
			}
			return out, nil
		},
	})

	// 37.10.4: GetProductionSchedule (engines/manufacturing_scheduling.go) is
	// already a genuine finite-capacity work-center scheduler - it just had
	// no report/API wired to it before this stage.
	RegisterReport(ReportDefinition{
		ID: "production-capacity-schedule", Label: "Production Capacity Schedule", Category: "Manufacturing",
		Columns: []ReportColumn{
			{Key: "order_id", Label: "Production Order"}, {Key: "seq", Label: "Operation Seq"}, {Key: "work_center_id", Label: "Work Center"},
			{Key: "needed_minutes", Label: "Needed Minutes"}, {Key: "finite_date", Label: "Finite Date"}, {Key: "infinite_date", Label: "Infinite Date"},
			{Key: "overflow", Label: "Overflow (past due date)"},
		},
		Params: []ReportParam{},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schedule, err := GetProductionSchedule(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(schedule)
		},
	})
}
