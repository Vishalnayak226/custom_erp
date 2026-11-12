package engines

// Stage 148.31 / 148.32 reports (module wholesale). Both read what the
// screens read, so a report and a screen cannot disagree.

func init() {
	RegisterReport(ReportDefinition{ID: "summary-invoice-groups", Label: "Summary invoice groups", Category: "Sales", ModuleKey: ModuleWholesale,
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "payer_name", Label: "Bill-to"}, {Key: "currency", Label: "Currency"}, {Key: "place_of_supply", Label: "Place of supply"},
			{Key: "customer_po", Label: "Customer PO"}, {Key: "orders", Label: "Orders"}, {Key: "total", Label: "Total"}, {Key: "gst_basis", Label: "GST basis"},
			{Key: "status", Label: "Status"}},
		Run: summaryInvoiceGroupsReport, StoreKey: "-"})
	RegisterReport(ReportDefinition{ID: "dropship-open-lines", Label: "Drop-ship lines open", Category: "Procurement", ModuleKey: ModuleWholesale,
		Columns: []ReportColumn{{Key: "order_id", Label: "Order"}, {Key: "customer", Label: "Customer"}, {Key: "address", Label: "Ship to"}, {Key: "sku", Label: "Item"},
			{Key: "qty", Label: "Qty"}, {Key: "vendor", Label: "Vendor"}, {Key: "po_id", Label: "Purchase order"}, {Key: "po_status", Label: "PO status"},
			{Key: "expected_date", Label: "Expected"}},
		Run: dropShipOpenReport, StoreKey: "-"})
}

func summaryInvoiceGroupsReport(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	prev, err := PreviewSummaryInvoices(tenantID, p["from"], p["to"], "")
	if err != nil {
		return nil, err
	}
	rows := []map[string]interface{}{}
	for _, g := range prev.Groups {
		status := "Ready"
		if g.Blocked {
			status = g.Reason
		}
		rows = append(rows, map[string]interface{}{"payer_name": g.PayerName, "currency": g.Currency, "place_of_supply": g.PlaceOfSupply,
			"customer_po": g.CustomerPO, "orders": len(g.Orders), "total": g.Total, "gst_basis": g.GSTBasis, "status": status})
	}
	return rows, nil
}

func dropShipOpenReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	list, err := OpenDropShipLines(tenantID, "")
	if err != nil {
		return nil, err
	}
	rows := []map[string]interface{}{}
	for _, l := range list {
		rows = append(rows, map[string]interface{}{"order_id": l.OrderID, "customer": l.Customer, "address": l.Address, "sku": l.Sku, "qty": l.Qty,
			"vendor": l.Vendor, "po_id": l.POID, "po_status": l.POStatus, "expected_date": l.Expected})
	}
	return rows, nil
}
