package engines

// Stage 148.49: a posted receipt of goods that came before their PO has a bill
// that waits as a Draft until the retro PO is approved (148.38). It is not
// Ready to pay, so the inbox puts it on its own tab and the Ready to pay count
// is the list that tab opens.

const grnInboxTabWaitingPO = "po"

// grnBillWaitsForPO is true when the receipt came before its PO and the PO is
// not on file or not yet approved - the same test retroPOBlocksBill applies at
// payment.
func grnBillWaitsForPO(tenantID string, grn map[string]interface{}) bool {
	if strField(grn, "received_before_po") != "Yes" {
		return false
	}
	poID := strField(grn, "retro_po_id")
	if poID == "" {
		return true
	}
	po, status, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err != nil || po == nil {
		return true
	}
	return status != "Approved" && status != "Closed"
}
