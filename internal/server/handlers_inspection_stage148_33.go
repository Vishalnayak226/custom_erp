package server

import (
	"net/http"

	"custom_erp/engines"
)

// Stage 148.33: results of a lot held for inspection, from the store app.
// GET lists this store's pending lots; POST {lot_id, lines, karat, weight}
// records the results (or {lot_id, waive, note}). Off, both refuse.

func handleStoreAppInspections(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, _, role, store := storeAppCaller(r)
	lots, err := engines.StoreAppInspectionLots(tenantID, userID, role, store, r.URL.Query().Get("status"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, map[string]interface{}{"lots": lots})
}

func handleStoreAppInspectionResult(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, _, role, store := storeAppCaller(r)
	var in engines.InspectionResultRequest
	if !storeAppDecode(w, r, &in) {
		return
	}
	res, err := engines.StoreAppInspectionResult(tenantID, userID, role, store, in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, res)
}

func handleStoreAppInspectionDispose(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, _, role, store := storeAppCaller(r)
	var in engines.InspectionDisposeRequest
	if !storeAppDecode(w, r, &in) {
		return
	}
	res, err := engines.StoreAppInspectionDispose(tenantID, userID, role, store, in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, res)
}

// handleStockAgeInspections - GET lists the stock age inspections that are
// due; POST {id, outcome, ...} records what was decided.
func handleStockAgeInspections(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, _, role, _ := storeAppCaller(r)
	// Only the stores this user works at; a full-access user with no scope sees all.
	access, err := engines.UserStoreAccess(tenantID, userID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	if r.Method == http.MethodGet {
		rows, err := engines.ListStockAgeInspections(tenantID, role, r.URL.Query().Get("status"), access)
		// 193: the Quality screen asks on every visit; with the check off that is
		// an answer, not an error. Posting a decision while it is off is still refused.
		if ve, ok := err.(*engines.ValidationError); ok && ve.Code == "QUALIT-0602" {
			storeAppJSON(w, map[string]interface{}{"inspections": []interface{}{}, "off": true})
			return
		}
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		storeAppJSON(w, map[string]interface{}{"inspections": rows})
		return
	}
	var in engines.StockAgeOutcomeRequest
	if !storeAppDecode(w, r, &in) {
		return
	}
	res, err := engines.ResolveStockAgeInspection(tenantID, userID, role, access, in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	storeAppJSON(w, res)
}
