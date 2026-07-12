package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"time"
)

// CreateLogisticsBooking creates a tracking record for order courier dispatches
func CreateLogisticsBooking(tenantID string, orderID string, carrier string, trackingNumber string, shippingCharge int) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	bookingID := fmt.Sprintf("LOG-%d", time.Now().UnixNano())
	docData := map[string]interface{}{
		"code":            bookingID,
		"order_id":        orderID,
		"carrier":         carrier,
		"tracking_number": trackingNumber,
		"shipping_charge": shippingCharge,
		"status":          "Shipped",
	}

	marshaled, err := json.Marshal(docData)
	if err != nil {
		return "", err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by) 
		VALUES ($1, 'LogisticsBooking', $2, 'Shipped', 'system')`, schema)
	_, err = db.DB.Exec(query, bookingID, marshaled)
	return bookingID, err
}

// ProcessMarketplaceSettlement processes settlements, reconciles orders, and posts accounting journals
func ProcessMarketplaceSettlement(tenantID string, channel string, settlementID string, totalSale int, commission int, netPayout int, orderIDs []string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	// 1. Math validation
	if totalSale-commission != netPayout {
		return fmt.Errorf("invalid payout math: total sale (%d) minus commission (%d) must equal net payout (%d)", totalSale, commission, netPayout)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	// 2. Reconcile matched orders
	for _, orderID := range orderIDs {
		// Fetch the document and transition its status to 'Settled'
		var docBytes []byte
		errFetch := tx.QueryRow(fmt.Sprintf(`
			SELECT data FROM %s.documents 
			WHERE id = $1 AND doctype = 'POSCart'`, schema), orderID).Scan(&docBytes)
		if errFetch == nil {
			var orderDoc map[string]interface{}
			if errJson := json.Unmarshal(docBytes, &orderDoc); errJson == nil {
				orderDoc["status"] = "Settled"
				updatedBytes, _ := json.Marshal(orderDoc)
				_, _ = tx.Exec(fmt.Sprintf(`
					UPDATE %s.documents 
					SET data = $1, status = 'Settled', updated_at = CURRENT_TIMESTAMP 
					WHERE id = $2 AND doctype = 'POSCart'`, schema), updatedBytes, orderID)
			}
		}
	}

	// 3. Commit order reconciliation updates
	err = tx.Commit()
	if err != nil {
		return err
	}

	// 4. Post balanced GL accounting entries
	// Debit: Cash/Bank (1100) -> netPayout
	// Debit: Commission Expense (5200) -> commission
	// Credit: Accounts Receivable (1300) -> totalSale
	debits := map[string]int{
		"1100": netPayout,
		"5200": commission,
	}
	credits := map[string]int{
		"1300": totalSale,
	}

	err = PostDoubleEntry(tenantID, "MarketplaceSettlement", settlementID, debits, credits)
	if err != nil {
		return fmt.Errorf("failed to write settlement GL postings: %v", err)
	}

	// 5. Save the MarketplaceSettlement document
	settlementDoc := map[string]interface{}{
		"code":        settlementID,
		"channel":     channel,
		"payout_date": time.Now().Format("2006-01-02"),
		"total_sale":  totalSale,
		"commission":  commission,
		"net_payout":  netPayout,
		"status":      "Reconciled",
		"orders":      orderIDs,
	}
	settlementBytes, err := json.Marshal(settlementDoc)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by) 
		VALUES ($1, 'MarketplaceSettlement', $2, 'Reconciled', 'system') 
		ON CONFLICT (id) DO UPDATE SET 
			data = EXCLUDED.data, 
			status = EXCLUDED.status, 
			updated_at = CURRENT_TIMESTAMP`, schema)
	_, err = db.DB.Exec(query, settlementID, settlementBytes)
	return err
}

// SeedReceivableBalance seeds Accounts Receivable for test transactions
func SeedReceivableBalance(tenantID string, amount int, documentID string) error {
	// To credit Accounts Receivable in payout, we must first debit it (debit Receivable 1300, credit Revenue 4100)
	debits := map[string]int{"1300": amount}
	credits := map[string]int{"4100": amount}
	return PostDoubleEntry(tenantID, "POSCart", documentID, debits, credits)
}
