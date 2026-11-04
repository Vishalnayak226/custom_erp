package engines

import "testing"

// Stage 146.4.6(d): the buyer is told when a GRN is posted with rejected qty.
func TestStage14646dBuyerToldOfRejectedQty(t *testing.T) {
	tenantID, schema, cleanup, insert := gateEntryTestTenant(t)
	defer cleanup()
	insert("PO-RJ1", "PurchaseOrder", "Approved", map[string]interface{}{"code": "PO-RJ1", "vendor": "V-1"})
	insert("GRN-RJ1", "GRN", "Posted", map[string]interface{}{"code": "GRN-RJ1", "po_id": "PO-RJ1", "vendor": "V-1",
		"received_items": `[{"sku":"A","qty":10,"rejected_qty":2},{"sku":"B","qty":5}]`})
	insert("GRN-RJ2", "GRN", "Posted", map[string]interface{}{"code": "GRN-RJ2", "po_id": "PO-RJ1", "vendor": "V-1",
		"received_items": `[{"sku":"A","qty":10}]`})
	if got := NotifyBuyerOfGRNRejections(tenantID, "GRN-RJ1"); got != 0 {
		t.Errorf("off by default, got %v", got)
	}
	_ = SetSetting(tenantID, SettingNotifyBuyerOnRejection, "true", "admin")
	if got := NotifyBuyerOfGRNRejections(tenantID, "GRN-RJ1"); got != 2 {
		t.Errorf("rejected qty 2, got %v", got)
	}
	if n := notificationLogCount(t, schema, "GRN Rejected Qty", "GRN-RJ1"); n != 1 {
		t.Errorf("one notification for GRN-RJ1, got %d", n)
	}
	if got := NotifyBuyerOfGRNRejections(tenantID, "GRN-RJ2"); got != 0 || notificationLogCount(t, schema, "GRN Rejected Qty", "GRN-RJ2") != 0 {
		t.Errorf("nothing rejected - no notification")
	}
}
