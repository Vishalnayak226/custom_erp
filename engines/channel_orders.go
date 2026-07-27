package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
)

// ChannelOrderInput is the normalized, transport-independent order shape
// accepted from channel webhooks and pollers.  It deliberately feeds the
// SalesOrder engine instead of creating a parallel channel-order document.
type ChannelOrderInput struct {
	Channel         string
	ChannelOrderID  string
	CustomerName    string
	ShippingAddress string
	PaymentStatus   string
	Lines           []SalesOrderLineInput
}

// ImportChannelSalesOrder maps a channel payload then delegates all order
// validation, allocation, reservations, holds, and idempotency to
// CreateSalesOrder. channel_order_mapping remains populated for backwards
// compatible connector lookups.
func ImportChannelSalesOrder(tenantID string, input ChannelOrderInput) (string, error) {
	if input.Channel == "" || input.ChannelOrderID == "" {
		return "", fmt.Errorf("channel and channel_order_id are required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var mappedID string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT order_id FROM %s.channel_order_mapping WHERE channel = $1 AND channel_order_id = $2`, schema), input.Channel, input.ChannelOrderID).Scan(&mappedID)
	if err == nil {
		return mappedID, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	for i := range input.Lines {
		var sku string
		err = db.DB.QueryRow(fmt.Sprintf(`SELECT sku FROM %s.channel_product_mapping WHERE channel = $1 AND channel_sku = $2`, schema), input.Channel, input.Lines[i].SKU).Scan(&sku)
		if err == nil {
			input.Lines[i].SKU = sku
		} else if err != sql.ErrNoRows {
			return "", err
		}
	}

	orderID, err := CreateSalesOrder(tenantID, input.Channel, input.ChannelOrderID, input.CustomerName, input.ShippingAddress, input.PaymentStatus, input.Lines)
	if err != nil {
		return "", err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.channel_order_mapping (order_id, channel, channel_order_id) VALUES ($1, $2, $3) ON CONFLICT (channel, channel_order_id) DO NOTHING`, schema), orderID, input.Channel, input.ChannelOrderID)
	return orderID, err
}

// ImportUnicommerceSalesOrder keeps the existing Unicommerce mapping/event
// contract while routing its order into the shared SalesOrder lifecycle.
func ImportUnicommerceSalesOrder(tenantID, channelOrderID, storeCode string, lines []SalesOrderLineInput) (string, error) {
	if channelOrderID == "" || storeCode == "" {
		return "", fmt.Errorf("channel_order_id and store_code are required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var mappedID string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT order_id FROM %s.unicommerce_order_mapping WHERE channel_order_id = $1 AND store_code = $2`, schema), channelOrderID, storeCode).Scan(&mappedID)
	if err == nil {
		return mappedID, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	orderID, err := ImportChannelSalesOrder(tenantID, ChannelOrderInput{
		Channel:         "Unicommerce",
		ChannelOrderID:  channelOrderID,
		CustomerName:    "Unicommerce order " + channelOrderID,
		ShippingAddress: "Store " + storeCode + " 000000",
		PaymentStatus:   "Confirmed",
		Lines:           lines,
	})
	if err != nil {
		return "", err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.unicommerce_order_mapping (order_id, channel_order_id, store_code, status) VALUES ($1, $2, $3, 'Imported')`, schema), orderID, channelOrderID, storeCode)
	if err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, "system", "UNICOMMERCE_ORDER_IMPORTED", "SUCCESS", fmt.Sprintf("order=%s store=%s items=%d", orderID, storeCode, len(lines)))
	return orderID, nil
}
