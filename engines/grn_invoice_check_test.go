package engines

import (
	"strings"
	"testing"
)

// Stage 110.3: the invoice check names the box to fix in plain words.
func TestGRNInvoiceCheck(t *testing.T) {
	lines := []map[string]interface{}{
		{"line_no": 1, "sku": "I00002", "invoice_qty": 1, "invoice_rate": 213.59, "gst_rate": 3},
		{"line_no": 2, "sku": "I00003", "invoice_qty": 19, "invoice_rate": 213.59, "gst_rate": 3},
	}
	if c := grnInvoiceCheck(lines, 4400); c != nil {
		t.Fatalf("4400 is within Rs 1 of 4399.95, want no check, got %+v", c)
	}
	c := grnInvoiceCheck(lines, 4261)
	if c == nil || c.Field != "invoice_price" || c.LineNo != 2 || c.Gap != -138.95 {
		t.Fatalf("want line 2's price named for a -138.95 gap, got %+v", c)
	}
	if !strings.Contains(c.Message, "138.95 less") || !strings.Contains(c.Message, "total you typed") {
		t.Fatalf("message not plain enough: %q", c.Message)
	}
	// One unit short on the bill: the qty is named.
	c = grnInvoiceCheck(lines, 4399.95-219.9977)
	if c == nil || c.Field != "invoice_qty" || !strings.Contains(c.Message, "1 unit(s)") {
		t.Fatalf("want a whole-unit qty check, got %+v", c)
	}
}
