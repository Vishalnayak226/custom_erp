package engines

import (
	"fmt"
	"strings"

	"custom_erp/db"
)

// Stage 146.2.3: items that cannot be paid with points.
//
// An item's own "Pay with points" field decides first ("Not allowed" /
// "Allowed"); left blank, the item follows its product groups - a group with
// "Not payable with points" ticked takes every product it resolves to out.
// The redeemable cap on the till and the spread of the points tender over the
// bill's lines both work from the eligible lines only.

const (
	itemPointsNotAllowed = "Not allowed"
	itemPointsAllowed    = "Allowed"
)

// loyaltyNoPointsGroupItems is the set of item codes inside any product group
// flagged "not payable with points". No flagged group means one cheap query.
func loyaltyNoPointsGroupItems(tenantID string) (map[string]bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents
		WHERE doctype = 'PIMProductGroup' AND deleted_at IS NULL AND status = 'Active'
		  AND lower(COALESCE(data->>'no_points_redemption', '')) IN ('true', '1', 'yes')`, schema))
	if err != nil {
		return nil, err
	}
	var groups []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		groups = append(groups, id)
	}
	rows.Close()
	set := map[string]bool{}
	for _, g := range groups {
		codes, err := ResolvePIMProductGroupItemCodes(tenantID, g)
		if err != nil {
			return nil, err
		}
		for _, c := range codes {
			set[strings.TrimSpace(c)] = true
		}
	}
	return set, nil
}

// LoyaltyNoPointsSKUs returns which of skus cannot be paid with points.
func LoyaltyNoPointsSKUs(tenantID string, skus []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(skus) == 0 {
		return out, nil
	}
	var groupSet map[string]bool
	for _, sku := range skus {
		if _, done := out[sku]; done {
			continue
		}
		own := ""
		if data, _, err := fetchDocData(tenantID, "Item", sku); err == nil {
			own = strings.TrimSpace(fmt.Sprintf("%v", data["loyalty_redemption"]))
			if own == "<nil>" {
				own = ""
			}
		}
		switch {
		case strings.EqualFold(own, itemPointsNotAllowed):
			out[sku] = true
		case strings.EqualFold(own, itemPointsAllowed):
			out[sku] = false
		default:
			if groupSet == nil {
				g, err := loyaltyNoPointsGroupItems(tenantID)
				if err != nil {
					return nil, err
				}
				groupSet = g
			}
			out[sku] = groupSet[sku]
		}
	}
	return out, nil
}
