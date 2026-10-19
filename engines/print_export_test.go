package engines

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
)

// Stage 98.1: the line export's columns follow the transaction (D5 prices),
// carry each attribute under its label - a renamed label is the heading -
// and leave out what no line fills.
func TestBuildPrintExportColumnsAndPrices(t *testing.T) {
	lines := func() []map[string]interface{} {
		return []map[string]interface{}{
			{"sr": 1, "sku": "NK-RED-7", "design": "NK-01", "barcode": "890100", "item_name": "Antique Necklace", "brand": "Kalyan",
				"qty": 2.0, "mrp": 1500.0, "rate": 800.0, "taxable": 1600.0, "gst_rate": 3.0, "cgst": 24.0, "sgst": 24.0, "tax": 48.0, "amount": 1648.0,
				"attributes": map[string]string{"COLOR": "Red", "SIZE": "7"}},
			{"sr": 2, "sku": "NK-GRN-8", "design": "NK-01", "barcode": "890101", "item_name": "Antique Necklace",
				"qty": 1.0, "mrp": 1500.0, "rate": 800.0, "taxable": 800.0, "gst_rate": 3.0, "cgst": 12.0, "sgst": 12.0, "tax": 24.0, "amount": 824.0,
				"attributes": map[string]string{"COLOR": "Green"}},
		}
	}
	doc := func(txn string) PrintDocument {
		return PrintDocument{Transaction: txn, Title: "Purchase Order", Number: "PO/HQ/2026/000042", Date: "2026-10-19", Status: "Approved",
			Store: PrintStore{Code: "HQ", Name: "Head Office"}, Party: PrintStore{Name: "Shree Jewels", GSTIN: "27AAAAA0000A1Z5"}, PartyTitle: "Vendor",
			Fields:           map[string]interface{}{"po_numbers": ""},
			AttributeColumns: []AttributeColumn{{Code: "COLOR", Label: "Colour"}, {Code: "SIZE", Label: "Ring Size"}},
			Lines:            lines()}
	}
	read := func(t *testing.T, d PrintDocument) [][]string {
		t.Helper()
		body, name, ctype, err := BuildPrintExport(d, "csv")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(body, []byte("\xEF\xBB\xBF")) || !strings.HasPrefix(ctype, "text/csv") || !strings.HasSuffix(name, ".csv") {
			t.Fatalf("csv framing: name %q, type %q, bom %v", name, ctype, bytes.HasPrefix(body, []byte("\xEF\xBB\xBF")))
		}
		r := csv.NewReader(bytes.NewReader(body[3:]))
		r.FieldsPerRecord = -1
		rows, err := r.ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	headerRow := func(rows [][]string) []string {
		for _, r := range rows {
			if len(r) > 0 && r[0] == "Sr." {
				return r
			}
		}
		return nil
	}

	po := read(t, doc(PrintTxnPurchaseOrder))
	head := strings.Join(headerRow(po), "|")
	for _, want := range []string{"Design ID", "Combination ID / SKU", "Barcode", "Colour", "Ring Size", "Brand", "Qty", "MRP", "Cost price", "Taxable value", "Amount"} {
		if !strings.Contains(head, want) {
			t.Errorf("PO export lacks column %q: %s", want, head)
		}
	}
	for _, unwanted := range []string{"IGST", "Received", "Batch", "Selling price"} {
		if strings.Contains(head, unwanted) {
			t.Errorf("PO export has empty or foreign column %q: %s", unwanted, head)
		}
	}
	if po[1][0] != "Number" || po[1][1] != "PO/HQ/2026/000042" || po[5][0] != "Vendor" || !strings.Contains(po[5][1], "Shree Jewels") {
		t.Errorf("header block wrong: %v", po[:6])
	}
	last := po[len(po)-1]
	if last[0] != "Total" || !strings.Contains(strings.Join(last, "|"), "|3|") || !strings.Contains(strings.Join(last, "|"), "2472") {
		t.Errorf("total row should add qty 3 and amount 2472: %v", last)
	}

	transfer := doc(PrintTxnTransferOut)
	transfer.Title = "Stock Transfer - Dispatch"
	head = strings.Join(headerRow(read(t, transfer)), "|")
	if !strings.Contains(head, "MRP") {
		t.Errorf("transfer export must carry MRP: %s", head)
	}
	for _, cost := range []string{"Cost price", "Taxable value", "Amount", "Selling price"} {
		if strings.Contains(head, cost) {
			t.Errorf("transfer export must carry MRP only, has %q: %s", cost, head)
		}
	}

	sale := doc(PrintTxnSalesInvoice)
	head = strings.Join(headerRow(read(t, sale)), "|")
	if !strings.Contains(head, "Selling price") || strings.Contains(head, "Cost price") {
		t.Errorf("sales bill export carries its selling price, not cost: %s", head)
	}

	body, name, _, err := BuildPrintExport(doc(PrintTxnGRN), "xlsx")
	if err != nil || !bytes.HasPrefix(body, []byte("PK")) || name != "Purchase-Order-PO-HQ-2026-000042.xlsx" {
		t.Errorf("xlsx: err %v, zip %v, name %q", err, bytes.HasPrefix(body, []byte("PK")), name)
	}
	if _, _, _, err := BuildPrintExport(doc(PrintTxnGRN), "pdf"); err == nil {
		t.Error("an unknown format must be refused")
	}
}

// Stage 98.1: a package bill stores its lines as a JSON string; the print
// and the e-invoice read them, not the whole order's lines.
func TestSalesInvoiceLinesReadsStoredString(t *testing.T) {
	data := map[string]interface{}{"sales_order_id": "SO-NOT-READ",
		"items": `[{"sku":"NK-RED-7","qty":2,"unit_price":1030,"line_total":2060}]`}
	lines := salesInvoiceLines("default", "tenant_default", data)
	if len(lines) != 1 || lines[0].SKU != "NK-RED-7" || lines[0].Qty != 2 || lines[0].Gross != RupeesToPaise(2060) {
		t.Fatalf("package bill lines = %+v, want one line NK-RED-7 x2 worth 2060", lines)
	}
}
