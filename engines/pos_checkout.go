package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// FinalizePOSCheckout runs the side effects of a completed sale - inventory
// decrement, GL/GST posting, loyalty earn - against a POSCart document that
// already exists in the documents table, and marks it Paid. Reads the cart's
// own stored data (captured by handleCheckout at request time) as the single
// source of truth, rather than taking items/totals as parameters, so it can
// be called identically from two places: handleCheckout's normal synchronous
// path, and handleDecideApproval once a manager approves a discount-gated
// cart (Stage 20.10) - the two can never compute different totals this way.
func FinalizePOSCheckout(tenantID, cartNumber, correlationID string) (saleTotal float64, costTotal float64, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}

	var dataStr string
	if err = db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber).Scan(&dataStr); err != nil {
		return 0, 0, fmt.Errorf("cart not found: %v", err)
	}

	var cart struct {
		Location    string `json:"location"`
		PaymentMode string `json:"payment_mode"`
		CustomerID  string `json:"customer_id"`
		Items       []struct {
			Sku       string  `json:"sku"`
			Qty       int     `json:"qty"`
			SalePrice float64 `json:"sale_price"`
			CostPrice float64 `json:"cost_price"`
		} `json:"items"`
		GSTBreakdown GSTBreakdown `json:"gst_breakdown"`
	}
	if err = json.Unmarshal([]byte(dataStr), &cart); err != nil {
		return 0, 0, err
	}

	markFailed := func() {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Failed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber)
	}

	itemsInterface := make([]interface{}, len(cart.Items))
	totalSale, totalCost := 0, 0
	for i, item := range cart.Items {
		itemsInterface[i] = map[string]interface{}{"sku": item.Sku, "qty": -item.Qty}
		totalSale += int(item.SalePrice) * item.Qty
		totalCost += int(item.CostPrice) * item.Qty
	}

	if err = PostInventoryLedger(tenantID, cart.Location, itemsInterface); err != nil {
		markFailed()
		return 0, 0, fmt.Errorf("inventory decrement failed: %v", err)
	}
	if err = PostSalesFinanceBooking(tenantID, cartNumber, totalSale, totalCost, cart.PaymentMode); err != nil {
		markFailed()
		return 0, 0, fmt.Errorf("GL booking failed: %v", err)
	}
	if err = PostSalesGSTBooking(tenantID, cartNumber, cart.GSTBreakdown); err != nil {
		markFailed()
		return 0, 0, fmt.Errorf("GST booking failed: %v", err)
	}

	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Paid', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'POSCart' AND id = $1`, schema), cartNumber)

	// Loyalty earn stays outside the failure path above, same as before this
	// refactor: it's purely additive and must never undo an already-completed sale.
	if cart.CustomerID != "" {
		if errEarn := EarnLoyaltyPoints(tenantID, cart.CustomerID, totalSale, cartNumber); errEarn != nil {
			LogSystemError(tenantID, correlationID, "LOYALTY_EARN_FAILED", "/api/v1/checkout", errEarn.Error(), "")
		}
	}

	return float64(totalSale), float64(totalCost), nil
}
