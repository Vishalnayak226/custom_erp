package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Stage 42.5.6/42.5.7/42.5.8: facility hierarchy (Location.parent/level),
// facility copy (cloning a location's Zone/Bin WMS layout onto a new,
// empty location), and cross-facility inventory inquiry. 42.5.8's other
// half - in-transit stock as a real bucket - already exists
// (inventory_availability.in_transit, Stage 17.6's DispatchTransferOrder/
// ReceiveTransferOrder) and needed no new work here; what was actually
// missing was a way to see stock across locations at once, which is what
// GetCrossFacilityStock/GetFacilityRollup below add.

// ------------------------------------------------------------------
// 42.5.6: Facility hierarchy
// ------------------------------------------------------------------

// FacilityNode is one Location, flattened to just the fields the hierarchy
// views need.
type FacilityNode struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
	Level  string `json:"level,omitempty"`
}

// GetChildLocations (42.5.6) returns every Active Location whose parent
// field is exactly parentCode - direct children only, not recursive.
func GetChildLocations(tenantID, parentCode string) ([]FacilityNode, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'name', ''), COALESCE(data->>'level', '')
		FROM %s.documents WHERE doctype = 'Location' AND status = 'Active' AND data->>'parent' = $1
		ORDER BY id`, schema), parentCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FacilityNode
	for rows.Next() {
		var n FacilityNode
		if err := rows.Scan(&n.Code, &n.Name, &n.Level); err != nil {
			return nil, err
		}
		n.Parent = parentCode
		out = append(out, n)
	}
	return out, rows.Err()
}

// GetFacilityDescendants (42.5.6) walks the parent chain breadth-first from
// rootCode (included first) down through every descendant. Cycle-safe: a
// Location whose parent chain loops back on itself (a data-entry mistake -
// nothing in this Stage prevents setting it, since doing so would need a
// full DAG check on every single Location save for a case that is, in
// practice, rare) is visited once per node, never infinitely.
func GetFacilityDescendants(tenantID, rootCode string) ([]string, error) {
	rootCode = strings.TrimSpace(rootCode)
	if rootCode == "" {
		return nil, errors.New("facility code is required")
	}
	visited := map[string]bool{rootCode: true}
	order := []string{rootCode}
	queue := []string{rootCode}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		children, err := GetChildLocations(tenantID, cur)
		if err != nil {
			return nil, err
		}
		for _, c := range children {
			if visited[c.Code] {
				continue
			}
			visited[c.Code] = true
			order = append(order, c.Code)
			queue = append(queue, c.Code)
		}
	}
	return order, nil
}

// FacilityRollupRow is one SKU's stock, summed across a facility and every
// one of its descendants.
type FacilityRollupRow struct {
	Sku                string `json:"sku"`
	OnHand             int    `json:"on_hand"`
	Available          int    `json:"available"`
	InTransit          int    `json:"in_transit"`
	LocationsWithStock int    `json:"locations_with_stock"`
}

// GetFacilityRollup (42.5.6) aggregates on_hand/available/in_transit by SKU
// across facilityCode and every descendant GetFacilityDescendants finds -
// the "roll-up inquiry" the plan calls for: a Division or DC's total stock
// position without having to sum its Warehouses by hand. A leaf Location
// (no children) still works, rolling up to just itself.
func GetFacilityRollup(tenantID, facilityCode string) ([]FacilityRollupRow, error) {
	locations, err := GetFacilityDescendants(tenantID, facilityCode)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	// Parameterised placeholder list rather than pq.Array - lib/pq is only
	// an indirect (driver) dependency in this repo, and engines/pos_offers.go
	// already established not promoting it to a direct one for an IN clause
	// a few lines of plain SQL building covers just as well.
	placeholders := make([]string, len(locations))
	args := make([]interface{}, len(locations))
	for i, loc := range locations {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = loc
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT sku, SUM(on_hand), SUM(available), SUM(COALESCE(in_transit, 0)), COUNT(*)
		FROM %s.inventory_availability
		WHERE location_code IN (%s) AND (on_hand > 0 OR available > 0 OR COALESCE(in_transit, 0) > 0)
		GROUP BY sku ORDER BY sku`, schema, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FacilityRollupRow
	for rows.Next() {
		var r FacilityRollupRow
		if err := rows.Scan(&r.Sku, &r.OnHand, &r.Available, &r.InTransit, &r.LocationsWithStock); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------
// 42.5.7: Facility copy
// ------------------------------------------------------------------

// CopyFacilityConfig (42.5.7) clones every Zone and Bin registered against
// sourceLocation onto targetLocation, which must already exist as an empty
// (no Zone/Bin of its own yet) Active Location - onboarding a new warehouse
// with the same layout as an existing one without re-typing every zone and
// bin by hand. Config only: no stock, bin_stock or bin_status is copied, so
// every cloned bin starts genuinely empty and Active, exactly like a
// freshly set-up warehouse. A cloned code is derived by substituting
// targetLocation for sourceLocation inside the source's own code wherever
// it appears there (the common "WH1-A-01" -> "WH2-A-01" naming case);
// codes that don't contain the source location's code at all get
// targetLocation appended instead, to guarantee the new id can never
// collide with the source's.
func CopyFacilityConfig(tenantID, sourceLocation, targetLocation, userID string) (zonesCopied, binsCopied int, err error) {
	sourceLocation = strings.TrimSpace(sourceLocation)
	targetLocation = strings.TrimSpace(targetLocation)
	if sourceLocation == "" || targetLocation == "" {
		return 0, 0, errors.New("source and target location are both required")
	}
	if sourceLocation == targetLocation {
		return 0, 0, errors.New("source and target location must differ")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}

	var targetStatus string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'Location' AND id = $1`, schema), targetLocation).
		Scan(&targetStatus); err != nil {
		if err == sql.ErrNoRows {
			return 0, 0, fmt.Errorf("target location %s not found - create it first, then copy the layout onto it", targetLocation)
		}
		return 0, 0, err
	}
	if targetStatus != "Active" {
		return 0, 0, fmt.Errorf("target location %s is not Active", targetLocation)
	}
	var existingBins int
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'Bin' AND data->>'location' = $1`, schema), targetLocation).
		Scan(&existingBins); err != nil {
		return 0, 0, err
	}
	if existingBins > 0 {
		return 0, 0, fmt.Errorf("target location %s already has %d bin(s) - facility copy is only for onboarding an empty location", targetLocation, existingBins)
	}

	deriveCode := func(sourceCode string) string {
		if strings.Contains(sourceCode, sourceLocation) {
			return strings.Replace(sourceCode, sourceLocation, targetLocation, 1)
		}
		return sourceCode + "-" + targetLocation
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return 0, 0, err
	}

	zoneRows, err := tx.Query(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'Zone' AND status = 'Active' AND data->>'location' = $1`, schema), sourceLocation)
	if err != nil {
		return 0, 0, err
	}
	zoneCodeMap := map[string]string{}
	var zoneDatas []map[string]interface{}
	for zoneRows.Next() {
		var dataStr string
		if err := zoneRows.Scan(&dataStr); err != nil {
			zoneRows.Close()
			return 0, 0, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}
		zoneDatas = append(zoneDatas, data)
	}
	zoneRows.Close()
	if err := zoneRows.Err(); err != nil {
		return 0, 0, err
	}

	for _, zd := range zoneDatas {
		oldCode, _ := zd["code"].(string)
		if oldCode == "" {
			continue
		}
		newCode := deriveCode(oldCode)
		zoneCodeMap[oldCode] = newCode
		newData := map[string]interface{}{}
		for k, v := range zd {
			newData[k] = v
		}
		newData["code"] = newCode
		newData["location"] = targetLocation
		newData["status"] = "Active"
		marshaled, _ := json.Marshal(newData)
		if _, err := tx.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Zone', $2, 'Active', $3)
			 ON CONFLICT (id) DO NOTHING`, schema), newCode, marshaled, userID); err != nil {
			return 0, 0, err
		}
		zonesCopied++
	}

	binRows, err := tx.Query(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'Bin' AND status = 'Active' AND data->>'location' = $1`, schema), sourceLocation)
	if err != nil {
		return zonesCopied, 0, err
	}
	var binDatas []map[string]interface{}
	for binRows.Next() {
		var dataStr string
		if err := binRows.Scan(&dataStr); err != nil {
			binRows.Close()
			return zonesCopied, 0, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}
		binDatas = append(binDatas, data)
	}
	binRows.Close()
	if err := binRows.Err(); err != nil {
		return zonesCopied, 0, err
	}

	for _, bd := range binDatas {
		oldCode, _ := bd["bin_code"].(string)
		if oldCode == "" {
			continue
		}
		newCode := deriveCode(oldCode)
		newData := map[string]interface{}{}
		for k, v := range bd {
			newData[k] = v
		}
		newData["bin_code"] = newCode
		newData["location"] = targetLocation
		newData["status"] = "Active"
		// A fresh bin starts genuinely empty and available - never inherit
		// the source's operational state (e.g. Counting/Blocked/Full).
		delete(newData, "bin_status")
		if oldZone, ok := bd["zone"].(string); ok && oldZone != "" {
			if mapped, found := zoneCodeMap[oldZone]; found {
				newData["zone"] = mapped
			}
		}
		marshaled, _ := json.Marshal(newData)
		if _, err := tx.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', $3)
			 ON CONFLICT (id) DO NOTHING`, schema), newCode, marshaled, userID); err != nil {
			return zonesCopied, binsCopied, err
		}
		binsCopied++
	}

	if err := tx.Commit(); err != nil {
		return zonesCopied, binsCopied, err
	}
	LogAuditEvent(tenantID, userID, "WMS_FACILITY_COPY", "SUCCESS",
		fmt.Sprintf("Copied facility config from %s to %s: %d zone(s), %d bin(s)", sourceLocation, targetLocation, zonesCopied, binsCopied))
	return zonesCopied, binsCopied, nil
}

// ------------------------------------------------------------------
// 42.5.8: Cross-facility inventory inquiry
// ------------------------------------------------------------------

// CrossFacilityStockRow is one Location's stock position for one SKU.
type CrossFacilityStockRow struct {
	Sku          string `json:"sku"`
	LocationCode string `json:"location_code"`
	OnHand       int    `json:"on_hand"`
	Available    int    `json:"available"`
	InTransit    int    `json:"in_transit"`
}

// GetCrossFacilityStock (42.5.8) is the "where is this SKU across every
// facility" inquiry: one row per Location currently carrying any on_hand,
// available, or in-transit quantity of sku, across the whole tenant. The
// in-transit bucket itself is not new here - inventory_availability.
// in_transit has tracked it since Stage 17.6's DispatchTransferOrder/
// ReceiveTransferOrder - what this adds is the cross-location view of it
// alongside on_hand/available in one query, instead of looking a location
// up one at a time.
func GetCrossFacilityStock(tenantID, sku string) ([]CrossFacilityStockRow, error) {
	sku = strings.TrimSpace(sku)
	if sku == "" {
		return nil, errors.New("sku is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT location_code, on_hand, available, COALESCE(in_transit, 0) FROM %s.inventory_availability
		WHERE sku = $1 AND (on_hand > 0 OR available > 0 OR COALESCE(in_transit, 0) > 0)
		ORDER BY location_code`, schema), sku)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CrossFacilityStockRow
	for rows.Next() {
		r := CrossFacilityStockRow{Sku: sku}
		if err := rows.Scan(&r.LocationCode, &r.OnHand, &r.Available, &r.InTransit); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "cross-facility-stock", Label: "Cross-Facility Inventory Inquiry", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "location_code", Label: "Location"},
			{Key: "on_hand", Label: "On Hand"}, {Key: "available", Label: "Available"}, {Key: "in_transit", Label: "In Transit"},
		},
		Params: []ReportParam{{Key: "sku", Label: "SKU", Type: "text", Required: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetCrossFacilityStock(tenantID, params["sku"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
	RegisterReport(ReportDefinition{
		ID: "facility-rollup", Label: "Facility Roll-Up Inventory Inquiry", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "on_hand", Label: "On Hand"}, {Key: "available", Label: "Available"},
			{Key: "in_transit", Label: "In Transit"}, {Key: "locations_with_stock", Label: "Locations"},
		},
		Params: []ReportParam{{Key: "facility_code", Label: "Facility (Location code)", Type: "text", Required: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetFacilityRollup(tenantID, params["facility_code"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
