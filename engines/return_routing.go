package engines

import (
	"fmt"
	"strings"

	"custom_erp/db"
)

// routeReturnLocation picks the location a return with its store left blank
// lands at (Stage 140.14.6). Order sourcing needs stock, which the last unit
// sold no longer has, so a return does not: the nearest ACTIVE location with a
// PIN wins whatever it holds (same ranking as order sourcing,
// nearestLocationByPincode). With no PIN, or none a location can be ranked
// against, it falls back to the store that shipped the order. When neither
// works it returns "" and the cause, which names what is missing.
func routeReturnLocation(schema, pincode, orderID string) (loc, cause string) {
	if pin := pincodeShapeRe.FindString(pincode); pin != "" {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents
			WHERE doctype = 'Location' AND deleted_at IS NULL AND COALESCE(data->>'status', status, 'Active') = 'Active'
			  AND COALESCE(data->>'type', '') <> 'HO'`, schema))
		if err == nil {
			var codes []string
			for rows.Next() {
				var c string
				if rows.Scan(&c) == nil {
					codes = append(codes, c)
				}
			}
			rows.Close()
			if best, _ := nearestLocationByPincode(schema, pin, codes); best != "" {
				return best, ""
			}
		}
	}
	shipped := returnShippingStore(schema, orderID)
	if shipped != "" {
		return shipped, ""
	}
	switch {
	case pincodeShapeRe.FindString(pincode) != "":
		return "", "no active location has a PIN to match the order's PIN, and the shipping store is unknown"
	case strings.TrimSpace(orderID) == "":
		return "", "the return names no order, so the shipping store is unknown, and no shipping PIN is on record"
	default:
		return "", "the order has no shipping PIN and its shipping store is unknown"
	}
}

// returnShippingStore is the store an order shipped from: the location of its
// fulfilment task (a sales invoice reads through its sales order), else the
// location on the order or till bill itself. Empty when none is recorded.
func returnShippingStore(schema, orderID string) string {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return ""
	}
	var store string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(t.data->>'location_code', '') FROM %[1]s.documents t
		WHERE t.doctype = 'FulfillmentTask' AND t.deleted_at IS NULL AND COALESCE(t.data->>'location_code', '') <> ''
		  AND t.data->>'order_id' IN ($1, COALESCE((SELECT data->>'sales_order_id' FROM %[1]s.documents WHERE id = $1 AND doctype = 'SalesInvoice'), $1))
		ORDER BY CASE WHEN t.status = 'Dispatched' THEN 0 ELSE 1 END, t.created_at LIMIT 1`, schema), orderID).Scan(&store)
	if store = strings.TrimSpace(store); store != "" {
		return store
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'location_code', ''), data->>'location', '') FROM %s.documents
		WHERE (id = $1 OR data->>'code' = $1) AND doctype IN ('POSCart', 'SalesOrder', 'SalesInvoice') AND deleted_at IS NULL LIMIT 1`, schema), orderID).Scan(&store)
	return strings.TrimSpace(store)
}
