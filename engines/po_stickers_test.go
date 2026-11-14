package engines

import (
	"fmt"
	"strings"
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

// Stage 52.7: a sales order is a sticker source too. Its lines are
// SalesOrderLine records, not a table on the order: one sticker per ordered
// unit, a cancelled line left out, two lines of one SKU printed as one, and
// the order found by number in the Document list.
func TestStage527SalesOrderIsAStickerSource(t *testing.T) {
	tenantID, _, cleanup, insert := gateEntryTestTenant(t)
	defer cleanup()
	insert("SO-ST1", "SalesOrder", "Reserved", map[string]interface{}{"code": "SO-ST1", "order_status": "Reserved", "customer_name": "C"})
	line := func(n int, sku string, qty int, status string) {
		id := fmt.Sprintf("SO-ST1-L%d", n)
		insert(id, "SalesOrderLine", status, map[string]interface{}{"code": id, "order_id": "SO-ST1", "sku": sku, "qty": qty, "line_status": status})
	}
	line(1, "SKU-A", 2, "Reserved")
	line(2, "SKU-B", 4, "Cancelled")
	line(3, "SKU-C", 1, "Pending")
	for n := 4; n <= 9; n++ {
		line(n, "SKU-C", 0, "Pending") // zero qty prints nothing
	}
	line(10, "SKU-A", 3, "Reserved")
	lines, err := ResolveDocumentStickerLines(tenantID, "SalesOrder", "SO-ST1")
	if err != nil || len(lines) != 2 || lines[0].SKU != "SKU-A" || lines[0].Qty != 5 || lines[1].SKU != "SKU-C" || lines[1].Qty != 1 {
		t.Fatalf("SO sticker lines: %v %+v", err, lines)
	}
	docs, err := SearchStickerSourceDocs(tenantID, "SalesOrder", "ST1", 10, SessionScope{Role: "Super Admin"})
	if err != nil || len(docs) != 1 || docs[0].ID != "SO-ST1" {
		t.Fatalf("SO in the Document list: %v %+v", err, docs)
	}
	insert("SO-ST2", "SalesOrder", "Cancelled", map[string]interface{}{"code": "SO-ST2", "order_status": "Cancelled"})
	if _, err := ResolveDocumentStickerLines(tenantID, "SalesOrder", "SO-ST2"); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("a cancelled order prints nothing: %v", err)
	}
	if _, err := ResolveDocumentStickerLines(tenantID, "SalesOrder", "SO-NONE"); err == nil {
		t.Fatal("an unknown order number is an error")
	}
}
