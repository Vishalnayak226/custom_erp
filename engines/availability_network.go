package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
)

// Stage 147.1 (user, 2026-10-31, on OMS ▸ New order: "It should show avil
// qty once i select a sku"): a manual order is sourced from whichever store
// or warehouse can fill it (FindBestFulfillmentNode looks at every location),
// so the quantity worth showing is the sellable quantity everywhere, with the
// stores it sits in - the same ATS math, summed.

// LocationATS is one location's sellable quantity of a SKU.
type LocationATS struct {
	LocationCode string `json:"location_code"`
	ATS          int    `json:"ats"`
}

// GetNetworkAvailableToSell is a SKU's ATS over every location holding it:
// the total, and each location with some to sell (most first).
func GetNetworkAvailableToSell(tenantID, sku string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	locations := []LocationATS{}
	if bundle, found, bundleErr := loadActiveProductBundle(tenantID, sku); bundleErr != nil {
		return nil, bundleErr
	} else if found && bundle.FulfillmentMode == "Virtual" {
		rows, err := GetBundleAvailability(tenantID, sku)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			locations = append(locations, LocationATS{LocationCode: r.Location, ATS: r.ATS})
		}
	} else {
		policy := atsPolicyFor(schema)
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT location_code, available, reserved, safety_stock, blocked, qc_hold, damaged, channel_buffer, hold_qty
			FROM %s.inventory_availability WHERE sku = $1`, schema), sku)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var loc string
			var available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer, held int
			if err := rows.Scan(&loc, &available, &reserved, &safetyStock, &blocked, &qcHold, &damaged, &channelBuffer, &held); err != nil {
				return nil, err
			}
			locations = append(locations, LocationATS{LocationCode: loc, ATS: computeATS(policy, available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer, held)})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	total := 0
	selling := []LocationATS{}
	for _, l := range locations {
		if l.ATS > 0 {
			total += l.ATS
			selling = append(selling, l)
		}
	}
	sort.Slice(selling, func(i, j int) bool {
		if selling[i].ATS != selling[j].ATS {
			return selling[i].ATS > selling[j].ATS
		}
		return selling[i].LocationCode < selling[j].LocationCode
	})
	return map[string]interface{}{"sku": sku, "ats": total, "locations": selling}, nil
}
