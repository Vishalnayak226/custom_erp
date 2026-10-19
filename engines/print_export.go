package engines

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Stage 98.1 (user, 2026-10-19): every transaction's lines as CSV or Excel -
// design, combination, barcode, qty, every attribute, brand and MRP - under
// the document's own header (number, date, store, vendor or customer, linked
// numbers). Built from the PrintDocument the print uses, so the export, the
// print and the PDF never disagree.
//
// Prices (decision D5): a PO and a GRN carry cost; a sales bill carries its
// own selling price (it is printed on the bill); every other transaction
// carries MRP only.

// exportColumn is one line column; sum marks the ones the Total row adds up.
type exportColumn struct {
	key, label string
	sum        bool
}

// exportPriceColumns is what each kind of transaction shows after MRP.
func exportPriceColumns(txn string) []exportColumn {
	tax := []exportColumn{{"taxable", "Taxable value", true}, {"gst_rate", "GST %", false}, {"cgst", "CGST", true},
		{"sgst", "SGST", true}, {"igst", "IGST", true}, {"tax", "Tax", true}, {"amount", "Amount", true}}
	switch txn {
	case PrintTxnPurchaseOrder, PrintTxnVendorPO:
		return append([]exportColumn{{"rate", "Cost price", false}}, tax...)
	case PrintTxnGRN:
		return append([]exportColumn{{"po_rate", "PO rate", false}, {"rate", "Cost price", false}, {"rate_difference", "Rate difference", false}}, tax...)
	case PrintTxnSalesInvoice, PrintTxnPOSTaxInvoice:
		return append([]exportColumn{{"rate", "Selling price", false}, {"discount", "Discount", true}}, tax...)
	}
	return nil
}

// exportLineColumns are the line columns in order. A column no line fills is
// left out - a PO export has no "Received", a business without brands no
// "Brand" - except Sr., SKU, item name, qty and MRP, which every export has.
func exportLineColumns(d PrintDocument) []exportColumn {
	cols := []exportColumn{{"sr", "Sr.", false}, {"design", "Design ID", false}, {"sku", "Combination ID / SKU", false},
		{"barcode", "Barcode", false}, {"item_name", "Item name", false}, {"variant", "Variant", false}}
	for _, a := range d.AttributeColumns {
		cols = append(cols, exportColumn{"attributes." + a.Code, firstNonEmpty(a.Label, a.Code), false})
	}
	cols = append(cols, []exportColumn{
		{"brand", "Brand", false}, {"hsn_code", "HSN", false}, {"uom", "UOM", false}, {"batch_no", "Batch", false}, {"po_id", "PO", false},
		{"qty", "Qty", true}, {"po_qty", "PO qty", true}, {"accepted_qty", "Accepted", true}, {"rejected_qty", "Rejected", true},
		{"damaged_qty", "Damaged", true}, {"requested_qty", "Requested", true}, {"shipped_qty", "Shipped", true},
		{"received_qty", "Received", true}, {"short_qty", "Short", true}, {"return_qty", "Return qty", true},
		{"original_qty", "Original qty", true}, {"mrp", "MRP", false},
	}...)
	cols = append(cols, exportPriceColumns(d.Transaction)...)
	cols = append(cols, []exportColumn{{"qc_status", "QC status", false}, {"variance_reason", "Variance reason", false},
		{"condition", "Condition", false}, {"reason", "Reason", false}}...)
	out := []exportColumn{}
	for _, c := range cols {
		keep := c.key == "sr" || c.key == "sku" || c.key == "item_name" || c.key == "qty" || c.key == "mrp"
		for _, l := range d.Lines {
			if keep {
				break
			}
			switch v := exportValue(l, c.key).(type) {
			case nil:
			case string:
				keep = strings.TrimSpace(v) != ""
			default:
				keep = numFromInterface(v) != 0
			}
		}
		if keep {
			out = append(out, c)
		}
	}
	return out
}

func exportValue(l map[string]interface{}, key string) interface{} {
	if code, ok := strings.CutPrefix(key, "attributes."); ok {
		if attrs, ok := l["attributes"].(map[string]string); ok {
			return attrs[code]
		}
		return nil
	}
	return l[key]
}

// exportHeader is the document block above the lines.
func exportHeader(d PrintDocument) [][2]string {
	party := d.Party.Name
	if d.Party.GSTIN != "" {
		party += " (GSTIN " + d.Party.GSTIN + ")"
	}
	store := d.Store.Name
	if d.Store.Code != "" && d.Store.Code != d.Store.Name {
		store += " (" + d.Store.Code + ")"
	}
	rows := [][2]string{{"Document", d.Title}, {"Number", d.Number}, {"Date", d.Date}, {"Status", d.Status}, {"Store", store}}
	if party != "" {
		rows = append(rows, [2]string{firstNonEmpty(d.PartyTitle, "Party"), party})
	}
	for _, f := range [][2]string{{"po_numbers", "PO number"}, {"sales_order", "Sales order"}, {"vendor_invoice_no", "Vendor invoice"},
		{"original_invoice_no", "Original invoice"}, {"original_order", "Original order"}, {"irn", "IRN"}} {
		if v, _ := d.Fields[f[0]].(string); strings.TrimSpace(v) != "" {
			rows = append(rows, [2]string{f[1], v})
		}
	}
	return rows
}

var exportFileUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// ExportFileName is "<title>-<number>.<ext>", safe on every OS.
func ExportFileName(d PrintDocument, ext string) string {
	return strings.Trim(exportFileUnsafe.ReplaceAllString(d.Title+"-"+firstNonEmpty(d.Number, d.DocumentID), "-"), "-") + "." + ext
}

// BuildPrintExport renders a print document's lines as "csv" or "xlsx" and
// returns the file, its name and its content type.
func BuildPrintExport(d PrintDocument, format string) ([]byte, string, string, error) {
	cols := exportLineColumns(d)
	header := exportHeader(d)
	rows := make([][]interface{}, 0, len(d.Lines)+1)
	sums := make([]float64, len(cols))
	for _, l := range d.Lines {
		row := make([]interface{}, len(cols))
		for i, c := range cols {
			row[i] = exportValue(l, c.key)
			if c.sum {
				sums[i] += numFromInterface(row[i])
			}
		}
		rows = append(rows, row)
	}
	total := make([]interface{}, len(cols))
	total[0] = "Total"
	for i, c := range cols {
		if i > 0 && c.sum {
			total[i] = round2(sums[i])
		}
	}
	rows = append(rows, total)

	switch format {
	case "csv":
		var buf bytes.Buffer
		buf.WriteString("\xEF\xBB\xBF") // so Excel reads ₹ and Indian names as UTF-8
		w := csv.NewWriter(&buf)
		for _, h := range header {
			_ = w.Write([]string{h[0], h[1]})
		}
		_ = w.Write([]string{})
		labels := make([]string, len(cols))
		for i, c := range cols {
			labels[i] = c.label
		}
		_ = w.Write(labels)
		for _, r := range rows {
			out := make([]string, len(r))
			for i, v := range r {
				out[i] = exportText(v)
			}
			_ = w.Write(out)
		}
		w.Flush()
		return buf.Bytes(), ExportFileName(d, "csv"), "text/csv; charset=utf-8", w.Error()
	case "xlsx":
		sheet := XLSXSheet{Name: "Lines"}
		for _, h := range header {
			sheet.Rows = append(sheet.Rows, []XLSXCell{{Text: h[0], Bold: true}, {Text: h[1]}})
		}
		sheet.Rows = append(sheet.Rows, []XLSXCell{})
		labels := make([]XLSXCell, len(cols))
		for i, c := range cols {
			labels[i] = XLSXCell{Text: c.label, Bold: true}
			width := float64(len(c.label)) + 2
			switch {
			case c.key == "item_name" || c.key == "variant":
				width = 28
			case i == 0:
				width = 16 // the header labels sit in this column too
			case width < 10:
				width = 10
			}
			sheet.ColWidths = append(sheet.ColWidths, width)
		}
		sheet.Rows = append(sheet.Rows, labels)
		for ri, r := range rows {
			out := make([]XLSXCell, len(r))
			for i, v := range r {
				out[i] = exportCell(v)
				out[i].Bold = ri == len(rows)-1
			}
			sheet.Rows = append(sheet.Rows, out)
		}
		body, err := WriteXLSX(sheet)
		return body, ExportFileName(d, "xlsx"), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", err
	}
	return nil, "", "", &ValidationError{Code: "GLOBAL-0002", SubFor: "Format", Message: fmt.Sprintf("%q is not an export format - use csv or xlsx", format)}
}

func exportText(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case string:
		return x
	}
	return fmt.Sprint(v)
}

func exportCell(v interface{}) XLSXCell {
	switch x := v.(type) {
	case nil:
		return XLSXCell{}
	case float64:
		return XLSXCell{Num: x, IsNum: true}
	case int:
		return XLSXCell{Num: float64(x), IsNum: true}
	}
	return XLSXCell{Text: fmt.Sprint(v)}
}
