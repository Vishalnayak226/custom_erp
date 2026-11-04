package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 146.2.10 - points on online orders (decision D11: on by default).
//
// When the last parcel of a Shopify / marketplace / OMS order is delivered the
// order's customer earns points on it, by the same rules as a till bill
// (PlanLoyaltyEarn): the scheme of the store the order started in, tier
// multiplier and bonus offers. The rows wait, pending, until the return window
// after delivery has passed; a return inside the window takes them back in
// proportion (reverseOnlineOrderPointsTx, called from ProcessRefundRequest).
// A delivered order cannot be cancelled, so there is nothing to take back on
// cancel. The unique index uq_loyalty_ledger_online_order makes a repeated
// delivery event pay once.
//
// Nothing earns when the CRM & loyalty module is off for the client, when
// loyalty.online_order_points is off, for walk-ins, or for blocked members.

const loyaltyOnlineDoctype = "SalesOrder"

// loyaltyOnlineEnabled reports whether online orders earn for this client.
func loyaltyOnlineEnabled(tenantID string) bool {
	if on, _ := IsModuleEnabled(tenantID, "crm_loyalty"); !on {
		return false
	}
	return GetSettingBool(tenantID, "loyalty.online_order_points")
}

// loyaltyOnlineOrderDelivered earns the order's points once every forward
// parcel that is still going out has been delivered. It is called after each
// delivery event and is safe to call again.
func loyaltyOnlineOrderDelivered(tenantID, orderID string) error {
	if orderID == "" || !loyaltyOnlineEnabled(tenantID) {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var open, delivered int
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FILTER (WHERE COALESCE(status, '') NOT IN ('Delivered', 'Cancelled', 'RTO')),
		       COUNT(*) FILTER (WHERE status = 'Delivered')
		FROM %s.documents WHERE doctype = 'LogisticsBooking' AND data->>'order_id' = $1
		  AND COALESCE(data->>'shipment_direction', '') <> 'Reverse' AND deleted_at IS NULL`, schema), orderID).Scan(&open, &delivered); err != nil {
		return err
	}
	if open > 0 || delivered == 0 {
		return nil
	}
	_, order, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return err
	}
	customerID := loyaltyOnlineCustomer(tenantID, order)
	if customerID == "" || IsWalkInCustomer(tenantID, customerID) {
		return nil
	}
	in := LoyaltyEarnInput{CustomerID: customerID, Net: int(numFromInterface(order["total_amount"]) + 0.5),
		Location: loyaltyOnlineLocation(schema, order)}
	if day := strField(order, "order_date"); len(day) >= 10 {
		in.BillDate = day[:10]
	}
	lines, err := db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL
		AND COALESCE(data->>'line_status', '') <> 'Cancelled'`, schema), orderID)
	if err != nil {
		return err
	}
	for lines.Next() {
		var raw []byte
		if lines.Scan(&raw) != nil {
			continue
		}
		var d map[string]interface{}
		_ = json.Unmarshal(raw, &d)
		qty := int(numFromInterface(d["qty"]))
		in.Lines = append(in.Lines, LoyaltyEarnLine{Sku: strField(d, "sku"), Qty: qty, Net: numFromInterface(d["unit_price"]) * float64(qty)})
	}
	lines.Close()
	plan, err := PlanLoyaltyEarn(tenantID, in)
	if err != nil || plan.Total <= 0 {
		return err
	}
	expires := time.Now().AddDate(0, 0, GetSettingInt(tenantID, "loyalty.point_expiry_days"))
	var until *time.Time
	if days := GetSettingInt(tenantID, "sales.return_window_days"); days > 0 {
		t := time.Now().AddDate(0, 0, days)
		until = &t
	}
	given := 0
	for _, r := range plan.Rows {
		res, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id, expires_at, pending_until, location, reason)
			VALUES ($1, 'Earn', $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''))
			ON CONFLICT (customer_id, reference_id, (COALESCE(reason, ''))) WHERE reference_doctype = 'SalesOrder' AND transaction_type = 'Earn' DO NOTHING`, schema),
			customerID, r.Points, loyaltyOnlineDoctype, orderID, expires, until, in.Location, r.Reason)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			given += r.Points
		}
	}
	if given > 0 {
		if err := postLoyaltyValue(tenantID, loyaltyOnlineDoctype, orderID, "SalesOrder:"+orderID+":LOYALTY_ACCRUE",
			int64(given)*loyaltyPointValuePaise(tenantID), true, ""); err != nil {
			LogSystemError(tenantID, "", "ERROR", "loyaltyOnlineOrderDelivered", fmt.Sprintf("points earned on %s but their liability was not booked: %v", orderID, err), "")
		}
	}
	return nil
}

// loyaltyOnlineCustomer is the order's customer: the one linked on it (the
// Shopify import does this), else the one with its phone number.
func loyaltyOnlineCustomer(tenantID string, order map[string]interface{}) string {
	if id := strings.TrimSpace(strField(order, "customer_id")); id != "" {
		return id
	}
	if phone := strings.TrimSpace(strField(order, "customer_phone")); phone != "" {
		if ref, found, _, err := LookupCustomerByPhone(tenantID, phone); err == nil && found {
			return ref.ID
		}
	}
	return ""
}

// loyaltyOnlineLocation is the store the order started in: its own store
// field, else the Location named like its channel, else blank (the
// tenant-wide earn rate applies).
func loyaltyOnlineLocation(schema string, order map[string]interface{}) string {
	if loc := firstNonEmpty(strField(order, "location"), strField(order, "store"), strField(order, "preferred_location")); loc != "" {
		return loc
	}
	channel := strings.TrimSpace(strField(order, "channel"))
	if channel == "" {
		return ""
	}
	var id string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Location' AND deleted_at IS NULL
		AND (LOWER(id) = LOWER($1) OR LOWER(COALESCE(data->>'code', '')) = LOWER($1)) LIMIT 1`, schema), channel).Scan(&id) == nil {
		return id
	}
	return ""
}

// reverseOnlineOrderPointsTx takes back the points a returned online order
// had earned, in proportion to the money refunded so far (the same cumulative
// rule as a till bill). It returns the points reversed and writes the
// liability release; an order that earned nothing returns 0.
func reverseOnlineOrderPointsTx(tx *sql.Tx, schema, refundRequestID, orderID string) (int, error) {
	var customerID string
	var total float64
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE((SELECT customer_id FROM %[1]s.loyalty_point_ledger WHERE transaction_type = 'Earn' AND reference_doctype = 'SalesOrder' AND reference_id = $1 LIMIT 1), ''),
		COALESCE((SELECT (data->>'total_amount')::numeric FROM %[1]s.documents WHERE doctype = 'SalesOrder' AND id = $1), 0)`, schema), orderID).Scan(&customerID, &total); err != nil {
		return 0, err
	}
	if customerID == "" {
		return 0, nil
	}
	reversed, err := reverseEarnedPointsDocTx(tx, schema, loyaltyOnlineDoctype, customerID, orderID, refundRequestID, int64(total*100+0.5))
	if err != nil || reversed <= 0 {
		return 0, err
	}
	tenantID, err := tenantIDForSchema(schema)
	if err != nil {
		return 0, err
	}
	return reversed, postLoyaltyValueTx(tx, tenantID, schema, "RefundRequest", refundRequestID, "RefundRequest:"+refundRequestID+":LOYALTY_REVERSE",
		int64(reversed)*loyaltyPointValuePaise(tenantID), false, "")
}
