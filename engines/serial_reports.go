package engines

// Stage 42.1.9 - serial inquiry + full movement history, registered as
// ReportDefinitions rather than bespoke endpoints, exactly the same "one
// screen, no new frontend code" shape engines/traceability_reports.go
// established for batch - see that file's header for the full reasoning.
// Both reports here answer the plan's own wording verbatim
// (docs/specs/wms_parity_plan.md:196-198): "one screen answering 'where is
// this unit now and everywhere it has been'."

func init() {
	// "Where is this unit now" - and, with sku/status filters instead of a
	// specific serial, "what serial-tracked stock of this item do we have".
	RegisterReport(ReportDefinition{
		ID: "serial-inquiry", Label: "Serial Number Inquiry", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "sku", Label: "Item"},
			{Key: "serial_no", Label: "Serial Number"},
			{Key: "batch_no", Label: "Batch / Lot"},
			{Key: "status", Label: "Status"},
			{Key: "current_bin", Label: "Current Bin"},
			{Key: "location_code", Label: "Location"},
			{Key: "reserved_for", Label: "Reserved For"},
			{Key: "vendor", Label: "Supplier"},
		},
		Params: []ReportParam{
			{Key: "sku", Label: "Item Code (optional)", Type: "text"},
			{Key: "serial_no", Label: "Serial Number (optional)", Type: "text"},
			{Key: "status", Label: "Status (optional)", Type: "select",
				Options: "InStock,Allocated,Shipped,Returned,Scrapped"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetSerialInquiry(tenantID, params["sku"], params["serial_no"], params["status"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})

	// "Everywhere this unit has been" - the forward recall direction for a
	// serialised item, the same chain-of-custody answer batch-movement-history
	// gives for a lot.
	RegisterReport(ReportDefinition{
		ID: "serial-movement-history", Label: "Serial Movement History", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "moved_at", Label: "When"},
			{Key: "sku", Label: "Item"},
			{Key: "serial_no", Label: "Serial Number"},
			{Key: "voucher_type", Label: "Movement"},
			{Key: "voucher_id", Label: "Document"},
			{Key: "warehouse", Label: "Location"},
			{Key: "from_status", Label: "From"},
			{Key: "to_status", Label: "To"},
			{Key: "user_id", Label: "By"},
		},
		Params: []ReportParam{
			{Key: "serial_no", Label: "Serial Number", Type: "text", Required: true},
			{Key: "sku", Label: "Item Code (optional)", Type: "text"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetSerialMovementHistory(tenantID, params["sku"], params["serial_no"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
