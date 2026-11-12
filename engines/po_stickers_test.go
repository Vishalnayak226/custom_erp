package engines

import (
	"testing"
)

// Stage 146.4.6(c): "Stickers for this PO" - a purchase order is a source in
// Sticker Printing > Print from Transaction: one sticker per ordered unit, and
// it is found by number in the Document list.
func TestStage14646cPOIsAStickerSource(t *testing.T) {
	tenantID, _, cleanup, insert := gateEntryTestTenant(t)
	defer cleanup()
	insert("PO-ST1", "PurchaseOrder", "Approved", map[string]interface{}{"code": "PO-ST1", "location": "WH1", "vendor": "V-1",
		"items": []map[string]interface{}{{"sku": "SKU-A", "qty": 3, "rate": 10}, {"sku": "SKU-B", "qty": 2, "rate": 5}}})
	lines, err := ResolveDocumentStickerLines(tenantID, "PurchaseOrder", "PO-ST1")
	if err != nil || len(lines) != 2 || lines[0].SKU != "SKU-A" || lines[0].Qty != 3 || lines[1].Qty != 2 {
		t.Fatalf("PO sticker lines: %v %+v", err, lines)
	}
	docs, err := SearchStickerSourceDocs(tenantID, "PurchaseOrder", "ST1", 10, SessionScope{Role: "Super Admin"})
	if err != nil || len(docs) != 1 || docs[0].ID != "PO-ST1" {
		t.Fatalf("PO in the Document list: %v %+v", err, docs)
	}
}
