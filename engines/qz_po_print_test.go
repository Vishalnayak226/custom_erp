package engines

import (
	"strings"
	"testing"
)

// samplePOPrint is a hand-built POPrint rather than one fetched through
// BuildPurchaseOrderPrint, so these tests exercise only the new renderer
// (renderPurchaseOrderPayload) and do not need a database - the pricing and
// party-resolution logic it reuses is already covered by
// purchase_order_test.go's own tests.
func samplePOPrint(status string) POPrint {
	return POPrint{
		PONumber:   "PO-1001",
		DocumentID: "TEST-PO-1001",
		Status:     status,
		OrderDate:  "01 Sep 2026",
		ShipTo:     "Main Warehouse",
		Buyer:      POParty{Name: "Wholeops Retail Pvt Ltd", GSTIN: "27AAAAA0000A1Z5", State: "Maharashtra"},
		Vendor:     POParty{Name: "Acme Supplies", GSTIN: "29BBBBB0000B1Z3", State: "Karnataka"},
		Lines: []POLinePreview{
			{
				POLineInput: POLineInput{SKU: "SKU-1", Qty: 10, Rate: 100},
				ItemName:    "Widget",
				HSNCode:     "8471",
				GSTRate:     18,
				Taxable:     1000,
				LineTotal:   1180,
			},
		},
		GSTMode: "Exclusive",
		Breakdown: GSTBreakdown{
			TaxableAmount: 1000,
			Interstate:    true,
			IGST:          180,
			TotalTax:      180,
			TotalAmount:   1180,
		},
		GrandTotal:    1180,
		AmountInWords: "One Thousand One Hundred Eighty Rupees Only",
	}
}

func TestRenderPurchaseOrderPayloadHTMLMarksDraftAndCarriesTotals(t *testing.T) {
	draft := renderPurchaseOrderPayload(samplePOPrint("Draft"), QZPrinter{Language: "PDF"})
	if draft.Format != "HTML" || len(draft.Items) != 1 {
		t.Fatalf("expected a single HTML item, got %+v", draft)
	}
	if !strings.Contains(draft.Items[0].Data, "DRAFT") {
		t.Fatalf("a Draft PO printed with no DRAFT marking:\n%s", draft.Items[0].Data)
	}
	for _, want := range []string{"PO-1001", "Acme Supplies", "Widget", "8471", "180.00", "1180.00"} {
		if !strings.Contains(draft.Items[0].Data, want) {
			t.Fatalf("PO print is missing %q:\n%s", want, draft.Items[0].Data)
		}
	}

	approved := renderPurchaseOrderPayload(samplePOPrint("Approved"), QZPrinter{Language: "PDF"})
	if strings.Contains(approved.Items[0].Data, "DRAFT") {
		t.Fatal("an Approved PO was marked DRAFT")
	}
}

func TestRenderPurchaseOrderPayloadHTMLSplitsCGSTSGSTWhenNotInterstate(t *testing.T) {
	po := samplePOPrint("Approved")
	po.Breakdown = GSTBreakdown{TaxableAmount: 1000, Interstate: false, CGST: 90, SGST: 90, TotalTax: 180, TotalAmount: 1180}
	payload := renderPurchaseOrderPayload(po, QZPrinter{Language: "PDF"})
	data := payload.Items[0].Data
	if !strings.Contains(data, "CGST") || !strings.Contains(data, "SGST") || strings.Contains(data, ">IGST<") {
		t.Fatalf("intra-state PO should show CGST/SGST, not IGST:\n%s", data)
	}
}

func TestRenderPurchaseOrderPayloadESCPOSIsARawCutCommandStream(t *testing.T) {
	payload := renderPurchaseOrderPayload(samplePOPrint("Approved"), QZPrinter{Language: "ESC-POS", WidthMM: "58"})
	if payload.Format != "ESC-POS" || len(payload.Items) != 1 {
		t.Fatalf("expected a single ESC-POS item, got %+v", payload)
	}
	item := payload.Items[0]
	if item.Type != "raw" || !strings.HasSuffix(item.Data, escCut) {
		t.Fatalf("ESC-POS PO must be a raw command stream ending in a cut, got %+v", item)
	}
	if !strings.Contains(item.Data, "PO-1001") || !strings.Contains(item.Data, "1180.00") {
		t.Fatalf("ESC-POS PO is missing PO number or grand total:\n%q", item.Data)
	}
}

func TestRenderPurchaseOrderPayloadOmitsWordsWhenAbsent(t *testing.T) {
	po := samplePOPrint("Approved")
	po.AmountInWords = ""
	payload := renderPurchaseOrderPayload(po, QZPrinter{Language: "PDF"})
	if strings.Contains(payload.Items[0].Data, `class="words"`) {
		t.Fatal("empty AmountInWords should not render a words block")
	}
}
