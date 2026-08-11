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
	// CustomerPhone (Stage 41) is passed through as the channel sent it -
	// cleaning and country detection happen once, inside CreateSalesOrder, so
	// every connector that feeds this struct gets them without its own code.
	CustomerPhone string
	Lines         []SalesOrderLineInput
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
	// The mapping row is only honoured when the order it names still exists
	// (Stage 35.1.3). The retired importers wrote a mapping row pointing at a
	// synthetic "ORD-<channel>-<id>" that was never created as a document, so
	// an unqualified lookup here returned that phantom id and silently skipped
	// the import - which made every legacy channel order permanently
	// un-importable. A dangling row is treated as absent and overwritten below.
	var mappedID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT m.order_id FROM %s.channel_order_mapping m
		WHERE m.channel = $1 AND m.channel_order_id = $2
		  AND EXISTS (SELECT 1 FROM %s.documents d WHERE d.id = m.order_id AND d.deleted_at IS NULL)`, schema, schema),
		input.Channel, input.ChannelOrderID).Scan(&mappedID)
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

	orderID, err := CreateSalesOrder(tenantID, SalesOrderInput{
		Channel:         input.Channel,
		ChannelOrderID:  input.ChannelOrderID,
		CustomerName:    input.CustomerName,
		ShippingAddress: input.ShippingAddress,
		PaymentStatus:   input.PaymentStatus,
		CustomerPhone:   input.CustomerPhone,
		Lines:           input.Lines,
	})
	if err != nil {
		return "", err
	}
	// DO UPDATE, not DO NOTHING (35.1.3): if a dangling pre-35.1 row is what
	// let this import through, DO NOTHING would leave it pointing at the
	// phantom id forever. Repointing it makes the table converge on truth.
	// Replays of a healthy row never reach here - they returned above.
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.channel_order_mapping (order_id, channel, channel_order_id) VALUES ($1, $2, $3) ON CONFLICT (channel, channel_order_id) DO UPDATE SET order_id = EXCLUDED.order_id`, schema), orderID, input.Channel, input.ChannelOrderID)
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
	// Same dangling-mapping guard as ImportChannelSalesOrder above (35.1.3).
	var mappedID string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT m.order_id FROM %s.unicommerce_order_mapping m
		WHERE m.channel_order_id = $1 AND m.store_code = $2
		  AND EXISTS (SELECT 1 FROM %s.documents d WHERE d.id = m.order_id AND d.deleted_at IS NULL)`, schema, schema),
		channelOrderID, storeCode).Scan(&mappedID)
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
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.unicommerce_order_mapping (order_id, channel_order_id, store_code, status) VALUES ($1, $2, $3, 'Imported')`, schema), orderID, channelOrderID, storeCode); err != nil {
		return "", err
	}
	// The unicommerce.order.imported outbox event is part of this path's
	// contract, not an extra: processUnicommerceOutbox consumes it, and the
	// legacy ImportUnicommerceOrder published it. Rewiring intake onto
	// SalesOrder (26.12.1) dropped it, which silently stopped acknowledging
	// imports back to Unicommerce. Restored here (Stage 35.1.2) in the same
	// transaction as the mapping row, so an event can never outlive a
	// rolled-back import.
	items := make([]map[string]interface{}, 0, len(lines))
	for _, l := range lines {
		items = append(items, map[string]interface{}{"sku": l.SKU, "qty": l.Qty})
	}
	if err := PublishEvent(tx, schema, "unicommerce.order.imported", map[string]interface{}{
		"order_id":         orderID,
		"channel_order_id": channelOrderID,
		"store_code":       storeCode,
		"items":            items,
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, "system", "UNICOMMERCE_ORDER_IMPORTED", "SUCCESS", fmt.Sprintf("order=%s store=%s items=%d", orderID, storeCode, len(lines)))
	return orderID, nil
}
