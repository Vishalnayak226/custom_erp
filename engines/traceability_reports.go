package engines

import "strconv"

// Stage 42.1 - the traceability reports.
//
// All three are ReportDefinitions rather than bespoke endpoints, which is the
// point the plan makes about 42.1.9: "registered as a report, not a bespoke
// endpoint". Registering them here means they inherit the generic report
// screen, the CSV/PDF export, scheduled delivery, column-level masking and the
// role gate with no new frontend code at all - and the recall pair below is
// the plan's own proof that the 42.1.3 data model is right, since neither
// needed a store of its own.

func init() {
	// 42.1.6 - the near-expiry watchlist. The report a food or pharma
	// warehouse actually opens every morning.
	RegisterReport(ReportDefinition{
		ID: "batch-near-expiry", Label: "Batch Near-Expiry Watchlist", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "sku", Label: "Item"},
			{Key: "batch_no", Label: "Batch / Lot"},
			{Key: "expiry_date", Label: "Expires"},
			{Key: "days_to_expiry", Label: "Days Left"},
			{Key: "status", Label: "Batch Status"},
			{Key: "qty_on_hand", Label: "Qty On Hand"},
			{Key: "locations", Label: "Locations"},
		},
		Params: []ReportParam{
			{Key: "within_days", Label: "Within (days, default 30)", Type: "text"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			within, _ := strconv.Atoi(params["within_days"])
			rows, err := GetNearExpiryBatches(tenantID, within)
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})

	// 42.1.3 backward direction - "where is lot X right now". Answers the
	// first question of a recall: what is still on our shelves that we must
	// stop shipping.
	RegisterReport(ReportDefinition{
		ID: "batch-stock-inquiry", Label: "Batch Stock Inquiry", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "sku", Label: "Item"},
			{Key: "batch_no", Label: "Batch / Lot"},
			{Key: "location_code", Label: "Location"},
			{Key: "bin_code", Label: "Bin"},
			{Key: "condition", Label: "Condition"},
			{Key: "qty", Label: "Qty"},
			{Key: "expiry_date", Label: "Expires"},
			{Key: "days_to_expiry", Label: "Days Left"},
			{Key: "batch_status", Label: "Batch Status"},
		},
		Params: []ReportParam{
			{Key: "sku", Label: "Item Code (optional)", Type: "text"},
			{Key: "location", Label: "Location (optional)", Type: "text"},
			{Key: "batch_no", Label: "Batch / Lot (optional)", Type: "text"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetBatchStock(tenantID, params["sku"], params["location"], params["batch_no"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})

	// 42.1.3 forward direction - "everywhere lot X has been". Filtered to the
	// outbound voucher types this is the customer-notification list; unfiltered
	// it is the full chain of custody from receipt to dispatch.
	RegisterReport(ReportDefinition{
		ID: "batch-movement-history", Label: "Batch Movement History (Recall)", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "moved_at", Label: "When"},
			{Key: "sku", Label: "Item"},
			{Key: "batch_no", Label: "Batch / Lot"},
			{Key: "qty", Label: "Qty"},
			{Key: "voucher_type", Label: "Movement"},
			{Key: "voucher_id", Label: "Document"},
			{Key: "warehouse", Label: "Location"},
			{Key: "from_bin", Label: "From Bin"},
			{Key: "to_bin", Label: "To Bin"},
			{Key: "user_id", Label: "By"},
		},
		Params: []ReportParam{
			{Key: "batch_no", Label: "Batch / Lot", Type: "text", Required: true},
			{Key: "sku", Label: "Item Code (optional)", Type: "text"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetBatchMovementHistory(tenantID, params["sku"], params["batch_no"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
