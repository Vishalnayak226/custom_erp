package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
)

// handleProductionMaterialPlan (Stage 55.12) answers "can this order be
// issued?" before anyone presses Issue Material: GET with bom, qty and
// location for a new order, or order=<id> for a saved one.
func handleProductionMaterialPlan(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	q := r.URL.Query()
	bomID, location := q.Get("bom"), q.Get("location")
	qty, _ := strconv.ParseFloat(q.Get("qty"), 64)
	var lines []engines.MaterialNeed
	var err error
	if orderID := q.Get("order"); orderID != "" {
		bomID, location, qty, lines, err = engines.ProductionOrderMaterialPlan(tenantID, orderID)
	} else if bomID == "" || location == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Choose a BOM and a location")
		return
	} else {
		lines, err = engines.ProductionMaterialPlan(tenantID, bomID, qty, location)
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	short := 0
	for _, l := range lines {
		if l.Short > 0 && !l.Backflush {
			short++
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"bom_id": bomID, "location": location, "qty": qty, "lines": lines, "short_lines": short})
}
