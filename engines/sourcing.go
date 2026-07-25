package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// FindBestFulfillmentNode resolves the location node with the highest ATS stock for all requested items
func FindBestFulfillmentNode(tenantID string, items []map[string]interface{}) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	// 1. Fetch all availability records for these SKUs
	if len(items) == 0 {
		return "", errors.New("no items specified for sourcing")
	}

	// Query unique locations
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT DISTINCT location_code 
		FROM %s.inventory_availability`, schema))
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var locations []string
	for rows.Next() {
		var loc string
		if err := rows.Scan(&loc); err == nil {
			locations = append(locations, loc)
		}
	}
	rows.Close()

	bestLocation := ""
	maxTotalATS := -1

	// 2. Evaluate each location node
	for _, loc := range locations {
		hasAllItems := true
		totalATS := 0

		for _, item := range items {
			sku, _ := item["sku"].(string)
			reqQty, _ := item["qty"].(int)

			var available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer int
			err = db.DB.QueryRow(fmt.Sprintf(`
				SELECT available, reserved, safety_stock, blocked, qc_hold, damaged, channel_buffer
				FROM %s.inventory_availability
				WHERE sku = $1 AND location_code = $2`, schema), sku, loc).Scan(&available, &reserved, &safetyStock, &blocked, &qcHold, &damaged, &channelBuffer)
			if err == sql.ErrNoRows {
				hasAllItems = false
				break
			} else if err != nil {
				return "", err
			}

			ats := computeATS(available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer)
			if ats < reqQty {
				hasAllItems = false
				break
			}
			totalATS += ats
		}

		if hasAllItems && totalATS > maxTotalATS {
			maxTotalATS = totalATS
			bestLocation = loc
		}
	}

	if bestLocation == "" {
		// OMNI-0247 (Stage 25.6): "No fulfillment node available" - the
		// catalog marks this Blocking:true, but this function's own
		// existing design deliberately never rejects an order for lack of
		// stock (same "don't reverse an already-deliberate workflow
		// decision" reasoning as SALESP-0123/MOBILE-0176) - it always
		// returns a usable node (HO) for the caller to route to and let a
		// human decide backorder/split/cancel. Log-only, not a rejection.
		LogSystemError(tenantID, "", "Medium", "Omnichannel / OMS", "[OMNI-0247] no store/warehouse had sufficient ATS for the requested items - falling back to HO", "")
		// Fallback to HO default node if no specific store has enough stock
		return "HO", nil
	}

	return bestLocation, nil
}

// AllocationPlan is the outcome of running Stage 26.12.9's configured
// AllocationRule chain against a set of order lines (Stage 26.12.2). LineLocations
// is index-aligned with the input lines - every entry is the same location
// code unless Split is true (only the "Split Shipment" strategy can produce
// more than one distinct location; every other strategy requires a single
// location to cover every line or it doesn't produce a plan at all).
type AllocationPlan struct {
	LineLocations []string
	Strategy      string // which AllocationRule.strategy produced this plan, or "Default" for the no-rules-configured fallback
	Split         bool
}

// candidateLocation is one location node with enough ATS to cover every
// requested line by itself, plus the per-strategy signals (total ATS,
// oldest stock-touch time) needed to rank candidates without re-scanning
// inventory_availability once per strategy.
type candidateLocation struct {
	code          string
	totalATS      int
	oldestUpdated time.Time
}

// qualifyingLocations is FindBestFulfillmentNode's own per-location ATS scan
// (engines/sourcing.go:11), factored out so every single-location allocation
// strategy below shares one implementation instead of repeating it. Unlike
// FindBestFulfillmentNode, it does not fall back to "HO" - it simply returns
// every location that can cover all of items by itself (possibly none).
func qualifyingLocations(schema string, items []map[string]interface{}) ([]candidateLocation, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT location_code FROM %s.inventory_availability`, schema))
	if err != nil {
		return nil, err
	}
	var locs []string
	for rows.Next() {
		var loc string
		if err := rows.Scan(&loc); err == nil {
			locs = append(locs, loc)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var candidates []candidateLocation
	for _, loc := range locs {
		hasAll := true
		totalATS := 0
		var oldest time.Time
		for _, item := range items {
			sku, _ := item["sku"].(string)
			reqQty, _ := item["qty"].(int)

			var available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer int
			var updatedAt time.Time
			err := db.DB.QueryRow(fmt.Sprintf(`
				SELECT available, reserved, safety_stock, blocked, qc_hold, damaged, channel_buffer, updated_at
				FROM %s.inventory_availability
				WHERE sku = $1 AND location_code = $2`, schema), sku, loc,
			).Scan(&available, &reserved, &safetyStock, &blocked, &qcHold, &damaged, &channelBuffer, &updatedAt)
			if err == sql.ErrNoRows {
				hasAll = false
				break
			} else if err != nil {
				return nil, err
			}

			ats := computeATS(available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer)
			if ats < reqQty {
				hasAll = false
				break
			}
			totalATS += ats
			if oldest.IsZero() || updatedAt.Before(oldest) {
				oldest = updatedAt
			}
		}
		if hasAll {
			candidates = append(candidates, candidateLocation{code: loc, totalATS: totalATS, oldestUpdated: oldest})
		}
	}
	return candidates, nil
}

// singleLocationHighestATS is the pre-26.12.2 default strategy, re-expressed
// over qualifyingLocations so it composes with the other strategies below.
func singleLocationHighestATS(schema string, items []map[string]interface{}) (string, bool, error) {
	cands, err := qualifyingLocations(schema, items)
	if err != nil {
		return "", false, err
	}
	best := ""
	bestATS := -1
	for _, c := range cands {
		if c.totalATS > bestATS {
			bestATS = c.totalATS
			best = c.code
		}
	}
	return best, best != "", nil
}

// singleLocationOldestStock picks the qualifying location whose relevant
// stock rows were least recently touched, i.e. have been sitting longest -
// inventory_availability has no per-batch received-date, so updated_at is
// the closest proxy available today (same "proxy, not a real port" caveat
// the design note gives Nearest Pincode's own distance calculation, see
// docs/specs/oms_master_blueprint_reference.md §12).
func singleLocationOldestStock(schema string, items []map[string]interface{}) (string, bool, error) {
	cands, err := qualifyingLocations(schema, items)
	if err != nil {
		return "", false, err
	}
	best := ""
	var bestTime time.Time
	for _, c := range cands {
		if best == "" || c.oldestUpdated.Before(bestTime) {
			best = c.code
			bestTime = c.oldestUpdated
		}
	}
	return best, best != "", nil
}

// singleLocationLowestWorkload picks the qualifying location with the fewest
// open FulfillmentTask rows (status not Dispatched/Rejected - the two
// terminal states TransitionTaskStatus already uses, engines/fulfillment.go)
// as a workload proxy.
func singleLocationLowestWorkload(schema string, items []map[string]interface{}) (string, bool, error) {
	cands, err := qualifyingLocations(schema, items)
	if err != nil {
		return "", false, err
	}
	best := ""
	bestWorkload := -1
	for _, c := range cands {
		var workload int
		err := db.DB.QueryRow(fmt.Sprintf(`
			SELECT COUNT(*) FROM %s.documents
			WHERE doctype = 'FulfillmentTask' AND deleted_at IS NULL
			  AND data->>'location_code' = $1 AND status NOT IN ('Dispatched', 'Rejected')`, schema), c.code).Scan(&workload)
		if err != nil {
			return "", false, err
		}
		if best == "" || workload < bestWorkload {
			best = c.code
			bestWorkload = workload
		}
	}
	return best, best != "", nil
}

// singleLocationNearestPincode uses abs(orderPincode-locationPincode) as a
// distance proxy, per the design note's own validated shape (§12: "a real
// geo/zone lookup would be a genuine improvement, not just a port"). Returns
// ok=false (not an error) whenever the order has no pincode-shaped token in
// its shipping address or no qualifying location has a Location.pincode set
// - either way this strategy simply can't run, so the caller falls through
// to the next configured rule rather than failing the whole allocation.
func singleLocationNearestPincode(schema, shippingAddress string, items []map[string]interface{}) (string, bool, error) {
	match := pincodeShapeRe.FindString(shippingAddress)
	if match == "" {
		return "", false, nil
	}
	orderPincode, err := strconv.Atoi(match)
	if err != nil {
		return "", false, nil
	}

	cands, err := qualifyingLocations(schema, items)
	if err != nil {
		return "", false, err
	}
	best := ""
	bestDiff := -1
	for _, c := range cands {
		var locPincode sql.NullString
		err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT data->>'pincode' FROM %s.documents WHERE doctype = 'Location' AND id = $1 AND deleted_at IS NULL`, schema),
			c.code).Scan(&locPincode)
		if err != nil && err != sql.ErrNoRows {
			return "", false, err
		}
		if !locPincode.Valid || locPincode.String == "" {
			continue
		}
		lp, err := strconv.Atoi(locPincode.String)
		if err != nil {
			continue
		}
		diff := orderPincode - lp
		if diff < 0 {
			diff = -diff
		}
		if best == "" || diff < bestDiff {
			best = c.code
			bestDiff = diff
		}
	}
	return best, best != "", nil
}

// splitShipmentPlan is the only strategy allowed to spread lines across more
// than one location: each line is sourced independently (highest-ATS among
// that line's own qualifying locations), so it can succeed even when no
// single location covers every line. ok=false only when at least one line
// has no qualifying location anywhere.
func splitShipmentPlan(schema string, lines []SalesOrderLineInput) (*AllocationPlan, bool, error) {
	lineLocations := make([]string, len(lines))
	distinct := map[string]bool{}
	for i, l := range lines {
		item := []map[string]interface{}{{"sku": l.SKU, "qty": l.Qty}}
		cands, err := qualifyingLocations(schema, item)
		if err != nil {
			return nil, false, err
		}
		if len(cands) == 0 {
			return nil, false, nil
		}
		best := cands[0]
		for _, c := range cands[1:] {
			if c.totalATS > best.totalATS {
				best = c
			}
		}
		lineLocations[i] = best.code
		distinct[best.code] = true
	}
	return &AllocationPlan{LineLocations: lineLocations, Strategy: "Split Shipment", Split: len(distinct) > 1}, true, nil
}

// uniformPlan wraps a single location as the plan for every line - the
// result shape every single-location strategy above produces.
func uniformPlan(location, strategy string, n int) *AllocationPlan {
	locs := make([]string, n)
	for i := range locs {
		locs[i] = location
	}
	return &AllocationPlan{LineLocations: locs, Strategy: strategy, Split: false}
}

// ResolveAllocationPlan is the Allocation/Sourcing Engine (Stage 26.12.2):
// runs every Active AllocationRule (Stage 26.12.9) scoped to channel (or the
// global blank-channel rules) in ascending priority order, trying each rule's
// configured strategy until one produces a usable plan - the first rule to
// succeed wins, matching the checklist's own "priority-ordered" wording. A
// "Manual" rule always fails to produce a plan and stops the search
// immediately without trying lower-priority rules (it means "route this
// channel's orders to a human," not "skip me if I don't fit" - configure it
// as the lowest-priority catch-all so richer rules get a chance first).
//
// Falls back to the pre-26.12.2 Highest ATS single-node behavior
// (FindBestFulfillmentNode, which always returns a usable node) when no
// AllocationRule row is configured at all, so a tenant that hasn't touched
// the new master keeps today's exact out-of-the-box behavior - same
// "usable before any admin config" precedent as isCancellationBlocked's own
// fallback (engines/orders.go). Once at least one rule IS configured,
// exhausting every rule without a plan returns (nil, nil) instead of ever
// falling back to HO - the allocation-exception case; CreateSalesOrder/
// ReleaseOrderHold (engines/orders.go) hold the order with
// HoldAllocationFailed rather than this function itself erroring, mirroring
// validateOrderChain's "return a routable code, don't reject" contract. The
// resulting On Hold queue (SalesOrder rows with hold_reason=ALLOCATION_FAILED)
// *is* the "allocation exception queue" the checklist item calls for - no
// separate table needed, same "reuse the doctype list view" precedent Stage
// 26.12.1's hold routing and 26.12.7's planned exception dashboard both use.
func ResolveAllocationPlan(tenantID, channel, shippingAddress string, lines []SalesOrderLineInput) (*AllocationPlan, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, errors.New("no lines to allocate")
	}

	items := make([]map[string]interface{}, len(lines))
	for i, l := range lines {
		items[i] = map[string]interface{}{"sku": l.SKU, "qty": l.Qty}
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'strategy' FROM %s.documents
		WHERE doctype = 'AllocationRule' AND status = 'Active' AND deleted_at IS NULL
		  AND (data->>'channel' = $1 OR data->>'channel' IS NULL OR data->>'channel' = '')
		ORDER BY (data->>'priority')::numeric ASC`, schema), channel)
	if err != nil {
		return nil, err
	}
	var strategies []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			strategies = append(strategies, s)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, strategy := range strategies {
		switch strategy {
		case "Manual":
			return nil, nil
		case "Highest ATS":
			if loc, ok, err := singleLocationHighestATS(schema, items); err != nil {
				return nil, err
			} else if ok {
				return uniformPlan(loc, strategy, len(lines)), nil
			}
		case "Nearest Pincode":
			if loc, ok, err := singleLocationNearestPincode(schema, shippingAddress, items); err != nil {
				return nil, err
			} else if ok {
				return uniformPlan(loc, strategy, len(lines)), nil
			}
		case "Lowest Workload":
			if loc, ok, err := singleLocationLowestWorkload(schema, items); err != nil {
				return nil, err
			} else if ok {
				return uniformPlan(loc, strategy, len(lines)), nil
			}
		case "Oldest Stock":
			if loc, ok, err := singleLocationOldestStock(schema, items); err != nil {
				return nil, err
			} else if ok {
				return uniformPlan(loc, strategy, len(lines)), nil
			}
		case "Split Shipment":
			if plan, ok, err := splitShipmentPlan(schema, lines); err != nil {
				return nil, err
			} else if ok {
				return plan, nil
			}
		}
		// This rule's strategy produced no usable plan - fall through to the
		// next rule in priority order.
	}

	if len(strategies) == 0 {
		loc, err := FindBestFulfillmentNode(tenantID, items)
		if err != nil {
			return nil, err
		}
		return uniformPlan(loc, "Default", len(lines)), nil
	}

	return nil, nil
}

// ImportChannelOrder validates and imports an external order, reserving stock atomically
func ImportChannelOrder(tenantID string, channel string, channelOrderID string, items []map[string]interface{}) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	// 1. Check idempotency: has this order already been processed?
	var exists bool
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1 FROM %s.channel_order_mapping 
			WHERE channel_order_id = $1 AND channel = $2
		)`, schema), channelOrderID, channel).Scan(&exists)
	if err != nil {
		return "", err
	}
	if exists {
		return "", errors.New("ORDER_ALREADY_IMPORTED")
	}

	// 2. Map channel SKUs to ERP SKUs
	var mappedItems []map[string]interface{}
	for _, item := range items {
		channelSku, _ := item["sku"].(string)
		qty, _ := item["qty"].(int)

		var erpSku string
		err = db.DB.QueryRow(fmt.Sprintf(`
			SELECT sku FROM %s.channel_product_mapping 
			WHERE channel_sku = $1 AND channel = $2`, schema), channelSku, channel).Scan(&erpSku)
		if err == sql.ErrNoRows {
			// Fallback to channel SKU string itself
			erpSku = channelSku
		} else if err != nil {
			return "", err
		}

		mappedItems = append(mappedItems, map[string]interface{}{
			"sku": erpSku,
			"qty": qty,
		})
	}

	// 3. Find the best fulfillment location node
	location, err := FindBestFulfillmentNode(tenantID, mappedItems)
	if err != nil {
		return "", err
	}

	// 4. Create stock reservations
	for _, item := range mappedItems {
		sku := item["sku"].(string)
		qty := item["qty"].(int)
		_, err = CreateReservation(tenantID, sku, location, qty, "Online", 86400) // 24hr expiration
		if err != nil {
			return "", fmt.Errorf("failed to reserve stock for SKU %s at node %s: %v", sku, location, err)
		}
	}

	// 5. Create POSCart document inside ERP in 'Reserved' status
	orderID := fmt.Sprintf("ORD-%s-%s", channel, channelOrderID)
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}

	// Save mapping
	_, err = tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.channel_order_mapping (order_id, channel, channel_order_id) 
		VALUES ($1, $2, $3)`, schema), orderID, channel, channelOrderID)
	if err != nil {
		return "", err
	}

	err = tx.Commit()
	return orderID, err
}

// MapChannelProduct registers mapping records between external channels and internal SKUs
func MapChannelProduct(tenantID string, channel string, sku string, channelSku string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.channel_product_mapping (sku, channel, channel_sku) 
		VALUES ($1, $2, $3) 
		ON CONFLICT (sku, channel) DO UPDATE SET 
			channel_sku = EXCLUDED.channel_sku, 
			updated_at = CURRENT_TIMESTAMP`, schema)
	_, err = db.DB.Exec(query, sku, channel, channelSku)
	return err
}
