package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
)

// Print payload construction (Stage 31.1).
//
// QZ Tray takes an array of "data items", each declaring how its payload
// should be interpreted. Two shapes matter here:
//
//	raw    - a byte stream handed straight to the printer, for thermal units
//	         that speak their own command language (ZPL/TSPL/ESC-POS).
//	pixel  - a PDF/HTML/image QZ rasterises through the OS driver.
//
// Which one a given printer needs is recorded on its Printer Master row
// (printer_language, added in db/migrations_stage31_1_qz_print.sql), so the
// operator configures it once and every print path picks the right encoding
// without a per-screen decision.
//
// Marketplace labels (Myntra, and any other channel that hands back a ready
// shipping label) do not come through here at all - they arrive as a finished
// PDF and are passed through untouched by the pass-through branch in
// handleQZPrintPayload. Re-rendering a courier's label would risk changing a
// barcode the carrier has to scan.

// QZDataItem is one entry in a QZ print request's `data` array. Field names
// and values match what qz-tray.js sends, so the tray parses it identically.
type QZDataItem struct {
	Type   string            `json:"type"`             // "raw" | "pixel"
	Format string            `json:"format"`           // "command" | "pdf" | "html" | "image"
	Flavor string            `json:"flavor"`           // "plain" | "base64" | "file"
	Data   string            `json:"data"`             // the payload itself
	Opts   map[string]string `json:"options,omitempty"`
}

// QZPrintPayload is what the browser receives and forwards to the tray.
type QZPrintPayload struct {
	Format string       `json:"format"` // echoed back for the print log
	Items  []QZDataItem `json:"items"`
}

// isRawLanguage reports whether a configured printer language is a command
// stream we generate ourselves rather than something the OS driver renders.
func isRawLanguage(language string) bool {
	switch strings.ToUpper(strings.TrimSpace(language)) {
	case "ZPL", "TSPL", "ESC-POS":
		return true
	}
	return false
}

// isESCPOS is deliberately narrower than isRawLanguage: ZPL and TSPL are
// label languages with no concept of a continuously fed receipt, so a
// receipt or invoice sent to a ZPL printer must be rasterised rather than
// emitted as commands. Asking isRawLanguage here would produce a stream the
// printer answers with a blank label.
func isESCPOS(language string) bool {
	return strings.EqualFold(strings.TrimSpace(language), "ESC-POS")
}

// zplEscape neutralises the three characters that would otherwise be read as
// ZPL control characters mid-field, which is how a stray "^" in a customer
// address silently truncates a label.
var zplEscaper = strings.NewReplacer("^", " ", "~", " ", "\\", " ")

func zplEscape(s string) string { return zplEscaper.Replace(s) }

// BuildShippingLabelPayload renders a LogisticsBooking as a 4x6 shipping
// label in whatever language the target printer speaks.
//
// This upgrades what engines/marketplace.go's GenerateShippingLabel could do
// (it returns a plain text block, and says so in its own comment): on a
// thermal printer the AWB now goes out as a real scannable Code 128 symbol
// rather than as digits a courier would have to key in by hand.
func BuildShippingLabelPayload(tenantID, bookingID, printerLanguage string) (*QZPrintPayload, error) {
	_, data, _, err := fetchLogisticsBooking(tenantID, bookingID)
	if err != nil {
		return nil, err
	}

	carrier, _ := data["carrier"].(string)
	awb, _ := data["awb_number"].(string)
	tracking, _ := data["tracking_number"].(string)
	orderID, _ := data["order_id"].(string)
	pincode, _ := data["destination_pincode"].(string)

	if isRawLanguage(printerLanguage) {
		return &QZPrintPayload{
			Format: strings.ToUpper(printerLanguage),
			Items: []QZDataItem{{
				Type:   "raw",
				Format: "command",
				Flavor: "plain",
				Data:   shippingLabelZPL(pincode, carrier, awb, tracking, orderID, bookingID),
			}},
		}, nil
	}

	return &QZPrintPayload{
		Format: "HTML",
		Items: []QZDataItem{{
			Type:   "pixel",
			Format: "html",
			Flavor: "plain",
			Data:   shippingLabelHTML(pincode, carrier, awb, tracking, orderID, bookingID),
		}},
	}, nil
}

// shippingLabelZPL lays out a 4x6 label at 203dpi (812 x 1218 dots).
//
// ^PW sets print width and ^LL label length explicitly rather than trusting
// the roll calibration, since an uncalibrated printer otherwise centres the
// content on whatever length it last measured.
func shippingLabelZPL(pincode, carrier, awb, tracking, orderID, bookingID string) string {
	var b strings.Builder
	b.WriteString("^XA\n")
	b.WriteString("^PW812\n")
	b.WriteString("^LL1218\n")
	b.WriteString("^LH0,0\n")
	b.WriteString("^CI28\n") // UTF-8 input, so non-ASCII addresses survive

	// Destination PIN, largest element on the label - it is what a sorter
	// reads first.
	b.WriteString("^FO30,40^A0N,90,90^FD" + zplEscape(pincode) + "^FS\n")
	b.WriteString("^FO30,150^GB752,3,3^FS\n")

	b.WriteString("^FO30,180^A0N,45,45^FD" + zplEscape(carrier) + "^FS\n")

	// Code 128 of the AWB. ^BY3 gives a module width that stays scannable
	// after thermal bleed; ^BCN,160,Y prints the human-readable line too, so
	// the number is still usable if the symbol smudges.
	if awb != "" {
		b.WriteString("^FO30,250^BY3\n")
		b.WriteString("^BCN,160,Y,N,N\n")
		b.WriteString("^FD" + zplEscape(awb) + "^FS\n")
	}

	y := 480
	for _, line := range []struct{ label, value string }{
		{"TRACKING", tracking},
		{"ORDER", orderID},
		{"BOOKING", bookingID},
	} {
		if line.value == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("^FO30,%d^A0N,34,34^FD%s: %s^FS\n", y, line.label, zplEscape(line.value)))
		y += 50
	}

	b.WriteString("^XZ\n")
	return b.String()
}

func shippingLabelHTML(pincode, carrier, awb, tracking, orderID, bookingID string) string {
	row := func(label, value string) string {
		if value == "" {
			return ""
		}
		return "<tr><td class=\"k\">" + html.EscapeString(label) + "</td><td>" + html.EscapeString(value) + "</td></tr>"
	}
	return `<html><head><meta charset="utf-8"><style>
body{font-family:Arial,Helvetica,sans-serif;margin:0;padding:12px;}
.pin{font-size:54px;font-weight:700;line-height:1;}
.carrier{font-size:22px;margin:8px 0 4px;}
hr{border:0;border-top:2px solid #000;margin:8px 0;}
.awb{font-size:30px;font-weight:700;letter-spacing:2px;margin:10px 0;}
table{font-size:15px;border-collapse:collapse;}
td{padding:2px 0;} td.k{font-weight:700;padding-right:10px;}
</style></head><body>
<div class="pin">` + html.EscapeString(pincode) + `</div><hr>
<div class="carrier">` + html.EscapeString(carrier) + `</div>
<div class="awb">` + html.EscapeString(awb) + `</div>
<table>` + row("Tracking", tracking) + row("Order", orderID) + row("Booking", bookingID) + `</table>
</body></html>`
}

// BuildStickerPayload renders already-resolved sticker labels for a thermal
// printer. Callers pass the labels PrintStickers returned, so the SKU
// validation, print log and reprint-reason handling in engines/stickers.go
// stay the single source of truth for what may be printed.
func BuildStickerPayload(labels []StickerLabel, copies int, printerLanguage string) *QZPrintPayload {
	if copies < 1 {
		copies = 1
	}
	if !isRawLanguage(printerLanguage) {
		return nil // caller falls back to the existing @media print sheet
	}

	var b strings.Builder
	for _, label := range labels {
		for i := 0; i < copies; i++ {
			name := label.Name
			if name == "" {
				name = label.SKU
			}
			b.WriteString("^XA\n^CI28\n")
			b.WriteString("^FO20,20^A0N,32,32^FD" + zplEscape(truncateRunes(name, 30)) + "^FS\n")
			if label.Barcode != "" {
				b.WriteString("^FO20,60^BY2\n^BCN,90,Y,N,N\n^FD" + zplEscape(label.Barcode) + "^FS\n")
			}
			b.WriteString("^FO20,190^A0N,26,26^FDSKU: " + zplEscape(label.SKU) + "^FS\n")
			b.WriteString("^XZ\n")
		}
	}
	return &QZPrintPayload{
		Format: strings.ToUpper(printerLanguage),
		Items: []QZDataItem{{
			Type:   "raw",
			Format: "command",
			Flavor: "plain",
			Data:   b.String(),
		}},
	}
}

// BuildPassThroughPayload wraps an already-rendered document - a marketplace
// shipping label or invoice PDF - for printing untouched.
//
// This is the path that carries Myntra/other-channel labels: the courier's
// own PDF is printed exactly as issued, because re-rendering it could alter
// a barcode the carrier scans. base64Data must be raw base64 with no data:
// URI prefix, which is the flavor QZ expects.
func BuildPassThroughPayload(base64Data, docFormat string, printer QZPrinter) *QZPrintPayload {
	format := strings.ToLower(strings.TrimSpace(docFormat))
	if format == "" {
		format = "pdf"
	}

	// A raw command stream for a thermal printer is passed through as-is;
	// the tray decodes the base64 and streams the bytes.
	if format == "command" || isRawLanguage(docFormat) {
		return &QZPrintPayload{
			Format: strings.ToUpper(docFormat),
			Items: []QZDataItem{{
				Type:   "raw",
				Format: "command",
				Flavor: "base64",
				Data:   base64Data,
			}},
		}
	}

	item := QZDataItem{
		Type:   "pixel",
		Format: format,
		Flavor: "base64",
		Data:   base64Data,
	}

	// A 4x6 label sent to a thermal printer driven by its Windows driver
	// must be told its physical size, or the driver scales it to the default
	// paper and clips the barcode.
	if w, h := parseMM(printer.WidthMM), parseMM(printer.HeightMM); w > 0 && h > 0 {
		item.Opts = map[string]string{
			"pageWidth":  strconv.FormatFloat(w/25.4, 'f', 3, 64),
			"pageHeight": strconv.FormatFloat(h/25.4, 'f', 3, 64),
			"units":      "in",
		}
	}

	return &QZPrintPayload{Format: strings.ToUpper(format), Items: []QZDataItem{item}}
}

func parseMM(v string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return 0
	}
	return f
}

// truncateRunes clips by rune, not byte, so a multi-byte product name is not
// cut mid-character into an invalid sequence the printer renders as garbage.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// ---------------------------------------------------------------------------
// Receipts and invoices (Stage 31.1.9)
//
// Both are re-read from their stored document rather than taken from the
// browser. That is what makes a reprint honest: a receipt printed an hour
// later shows exactly what was rung up, and a tampered client cannot print a
// receipt for a bill that never existed. It is also why these take the whole
// QZPrinter - a receipt needs the roll width to lay its columns out, which a
// language string alone does not carry.
// ---------------------------------------------------------------------------

// fetchPrintableDocument reads one document's stored data for printing.
//
// Deliberately its own small reader rather than a call into each module's
// engine: printing must never post, transition or lock anything, and the
// module fetchers all sit inside write paths (SELECT ... FOR UPDATE, status
// guards that reject anything not Draft) which a print has no business
// running.
func fetchPrintableDocument(tenantID, doctype, id string) (map[string]interface{}, string, time.Time, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	var dataBytes []byte
	var status string
	var createdAt time.Time
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status, created_at FROM %s.documents
		 WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema),
		doctype, id).Scan(&dataBytes, &status, &createdAt)
	if err != nil {
		return nil, "", time.Time{}, &ValidationError{
			Code:    "GLOBAL-0004",
			Message: fmt.Sprintf("%s %q was not found", doctype, id),
		}
	}
	data := map[string]interface{}{}
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, "", time.Time{}, err
	}
	return data, status, createdAt, nil
}

// ESC/POS control sequences. Only the handful a receipt actually needs -
// the full command set is large and every extra sequence is one more thing a
// given printer model may not implement, which shows up as literal escape
// characters on the paper rather than as an error.
const (
	escInit        = "\x1b@"         // ESC @   - reset to a known state
	escAlignLeft   = "\x1ba\x00"     // ESC a 0
	escAlignCenter = "\x1ba\x01"     // ESC a 1
	escBoldOn      = "\x1bE\x01"     // ESC E 1
	escBoldOff     = "\x1bE\x00"     // ESC E 0
	escDoubleOn    = "\x1d!\x11"     // GS ! 0x11 - double width + double height
	escDoubleOff   = "\x1d!\x00"     // GS ! 0
	escCut         = "\x1dV\x42\x00" // GS V B 0 - feed to the blade and cut
)

// escposColumns is how many Font-A characters fit across the roll.
//
// Defaults to the 80mm retail roll when label_width_mm is not set, because
// that is what a POS receipt printer almost always is. A narrow 58mm unit
// needs label_width_mm = 58 on its Printer record; without it lines would
// wrap. Every line this file emits is clipped to the column count, so a
// wrong setting costs a truncated product name, never a mangled total.
func escposColumns(widthMM string) int {
	w := parseMM(widthMM)
	if w > 0 && w < 76 {
		return 32
	}
	return 42
}

// escposRow lays out a label on the left and an amount flush right, which is
// the only layout primitive a receipt needs. The amount always survives
// intact - the description is what gets clipped - because a customer
// checking a receipt is checking the numbers.
func escposRow(left, right string, cols int) string {
	if cols < 8 {
		cols = 32
	}
	if right == "" {
		// A full-width row of trailing spaces is wasted ribbon travel on a
		// thermal head and shows up as a ragged right edge on the paper.
		return truncateRunes(left, cols) + "\n"
	}
	r := truncateRunes(right, cols-1)
	l := truncateRunes(left, cols-len([]rune(r))-1)
	pad := cols - len([]rune(l)) - len([]rune(r))
	if pad < 1 {
		pad = 1
	}
	return l + strings.Repeat(" ", pad) + r + "\n"
}

func escposCentered(s string, cols int) string {
	s = truncateRunes(s, cols)
	pad := (cols - len([]rune(s))) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + s + "\n"
}

func money(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// receiptTotals is what a completed POSCart adds up to, recomputed from the
// stored cart exactly the way engines/pos_checkout.go does it.
//
// The int() truncation on each line is not a rounding choice made here - it
// is what FinalizePOSCheckout books to the GL and what handleCheckout tells
// the cashier to collect. Computing it any more precisely would print a
// total that disagrees with the money actually taken.
type receiptTotals struct {
	saleTotal       float64
	loyaltyDiscount float64
	offerDiscount   float64
	amountDue       float64
}

// BuildReceiptPayload renders a completed POS sale as a receipt.
//
// Only a Paid cart prints. A Draft or Pending Approval cart is money not yet
// collected and a Failed one is a sale that did not happen, so a receipt for
// either would be a receipt for nothing - it is refused with GLOBAL-0019
// rather than printed with a caveat, because a printed receipt is what a
// customer walks out with.
func BuildReceiptPayload(tenantID, cartNumber string, printer QZPrinter) (*QZPrintPayload, error) {
	data, status, createdAt, err := fetchPrintableDocument(tenantID, "POSCart", cartNumber)
	if err != nil {
		return nil, err
	}
	if status != "Paid" {
		return nil, &ValidationError{
			Code: "GLOBAL-0019",
			Message: fmt.Sprintf("sale %s is %s, not Paid - a receipt can only be printed for a completed sale",
				cartNumber, strings.ToLower(status)),
		}
	}

	location, _ := data["location"].(string)
	paymentMode, _ := data["payment_mode"].(string)
	customer, _ := data["customer_id"].(string)

	var totals receiptTotals
	type receiptLine struct {
		sku    string
		qty    int
		amount float64
	}
	lines := []receiptLine{}
	if items, ok := data["items"].([]interface{}); ok {
		for _, raw := range items {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			sku, _ := item["sku"].(string)
			qty := int(numFromInterface(item["qty"]))
			amount := float64(int(numFromInterface(item["sale_price"])) * qty)
			lines = append(lines, receiptLine{sku: sku, qty: qty, amount: amount})
			totals.saleTotal += amount
		}
	}

	// Stage 30.7 stores applied_offers/offer_discount on the cart precisely so
	// the receipt can reconstruct how the bill's price was reached, and Stage
	// 30.2.5 stores redeem_points for the same reason. The redemption value is
	// recomputed through LoyaltyRedemptionValue rather than re-read from the
	// ledger, so this stays a pure read - printing must not touch a balance.
	if pts := int(numFromInterface(data["redeem_points"])); pts > 0 {
		totals.loyaltyDiscount = float64(LoyaltyRedemptionValue(tenantID, pts))
		if totals.loyaltyDiscount > totals.saleTotal {
			// FinalizePOSCheckout caps an over-redemption at the bill value and
			// returns the remainder; the receipt has to show the same cap or it
			// prints a negative amount due.
			totals.loyaltyDiscount = totals.saleTotal
		}
	}
	totals.offerDiscount = numFromInterface(data["offer_discount"])
	totals.amountDue = totals.saleTotal - totals.loyaltyDiscount - totals.offerDiscount

	appliedOffers := []string{}
	if offers, ok := data["applied_offers"].([]interface{}); ok {
		for _, raw := range offers {
			offer, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := offer["name"].(string)
			if name == "" {
				name, _ = offer["code"].(string)
			}
			if name != "" {
				appliedOffers = append(appliedOffers, name)
			}
		}
	}

	if isESCPOS(printer.Language) {
		cols := escposColumns(printer.WidthMM)
		var b strings.Builder
		b.WriteString(escInit)
		b.WriteString(escAlignCenter)
		b.WriteString(escBoldOn + escDoubleOn)
		b.WriteString("Sales Receipt\n")
		b.WriteString(escDoubleOff + escBoldOff)
		b.WriteString(cartNumber + "\n")
		b.WriteString(location + "\n")
		b.WriteString(createdAt.Format("02 Jan 2006 15:04") + "\n")
		if customer != "" {
			b.WriteString("Customer: " + customer + "\n")
		}
		b.WriteString(escAlignLeft)
		b.WriteString(strings.Repeat("-", cols) + "\n")

		for _, line := range lines {
			b.WriteString(escposRow(fmt.Sprintf("%s x%d", line.sku, line.qty), money(line.amount), cols))
		}
		b.WriteString(strings.Repeat("-", cols) + "\n")

		if totals.loyaltyDiscount > 0 || totals.offerDiscount > 0 {
			b.WriteString(escposRow("Subtotal", money(totals.saleTotal), cols))
		}
		for _, name := range appliedOffers {
			b.WriteString(escposRow("Offer: "+name, "", cols))
		}
		if totals.offerDiscount > 0 {
			b.WriteString(escposRow("Offer discount", "-"+money(totals.offerDiscount), cols))
		}
		if totals.loyaltyDiscount > 0 {
			b.WriteString(escposRow("Loyalty points applied", "-"+money(totals.loyaltyDiscount), cols))
		}
		b.WriteString(escBoldOn)
		b.WriteString(escposRow("TOTAL ("+paymentMode+")", money(totals.amountDue), cols))
		b.WriteString(escBoldOff)
		b.WriteString(strings.Repeat("-", cols) + "\n")
		b.WriteString(escAlignCenter)
		b.WriteString(escposCentered("Thank you", cols))
		b.WriteString("\n\n\n")
		b.WriteString(escCut)

		return &QZPrintPayload{
			Format: "ESC-POS",
			Items: []QZDataItem{{
				Type:   "raw",
				Format: "command",
				Flavor: "plain",
				Data:   b.String(),
			}},
		}, nil
	}

	// Anything else - an A4 laser, a PDF driver, a ZPL label unit someone set
	// as the receipt printer - goes through the OS driver as HTML.
	var rows strings.Builder
	for _, line := range lines {
		rows.WriteString("<tr><td>" + html.EscapeString(fmt.Sprintf("%s x%d", line.sku, line.qty)) +
			"</td><td class=\"r\">" + money(line.amount) + "</td></tr>")
	}
	if totals.loyaltyDiscount > 0 || totals.offerDiscount > 0 {
		rows.WriteString("<tr><td>Subtotal</td><td class=\"r\">" + money(totals.saleTotal) + "</td></tr>")
	}
	for _, name := range appliedOffers {
		rows.WriteString("<tr><td colspan=\"2\">Offer: " + html.EscapeString(name) + "</td></tr>")
	}
	if totals.offerDiscount > 0 {
		rows.WriteString("<tr><td>Offer discount</td><td class=\"r\">-" + money(totals.offerDiscount) + "</td></tr>")
	}
	if totals.loyaltyDiscount > 0 {
		rows.WriteString("<tr><td>Loyalty points applied</td><td class=\"r\">-" + money(totals.loyaltyDiscount) + "</td></tr>")
	}

	customerLine := ""
	if customer != "" {
		customerLine = "<div>Customer: " + html.EscapeString(customer) + "</div>"
	}
	return &QZPrintPayload{
		Format: "HTML",
		Items: []QZDataItem{{
			Type:   "pixel",
			Format: "html",
			Flavor: "plain",
			Data: `<html><head><meta charset="utf-8"><style>
body{font-family:"Courier New",monospace;margin:0;padding:10px;font-size:13px;}
.c{text-align:center;} .t{font-size:20px;font-weight:700;}
hr{border:0;border-top:1px dashed #000;margin:6px 0;}
table{width:100%;border-collapse:collapse;} td{padding:1px 0;} td.r{text-align:right;}
.total td{font-weight:700;font-size:15px;padding-top:4px;}
</style></head><body>
<div class="c"><div class="t">Sales Receipt</div>
<div>` + html.EscapeString(cartNumber) + `</div>
<div>` + html.EscapeString(location) + `</div>
<div>` + createdAt.Format("02 Jan 2006 15:04") + `</div>` + customerLine + `</div><hr>
<table>` + rows.String() + `<tr class="total"><td>TOTAL (` + html.EscapeString(paymentMode) + `)</td>
<td class="r">` + money(totals.amountDue) + `</td></tr></table><hr>
<div class="c">Thank you</div>
</body></html>`,
		}},
	}, nil
}

// BuildInvoicePayload renders a SalesInvoice for the invoice printer.
//
// Any status prints, unlike a receipt: a Draft invoice is a legitimate thing
// to hand a customer as a proforma. What it must not do is look like a
// posted one, so the status is stamped on the document itself rather than
// being checked away - a Draft prints saying "DRAFT".
func BuildInvoicePayload(tenantID, invoiceID string, printer QZPrinter) (*QZPrintPayload, error) {
	data, status, createdAt, err := fetchPrintableDocument(tenantID, "SalesInvoice", invoiceID)
	if err != nil {
		return nil, err
	}

	number, _ := data["invoice_number"].(string)
	if number == "" {
		number = invoiceID
	}
	customer, _ := data["customer"].(string)
	location, _ := data["location"].(string)
	amount := numFromInterface(data["total_amount"])

	if isESCPOS(printer.Language) {
		cols := escposColumns(printer.WidthMM)
		var b strings.Builder
		b.WriteString(escInit)
		b.WriteString(escAlignCenter)
		b.WriteString(escBoldOn + escDoubleOn)
		b.WriteString("Tax Invoice\n")
		b.WriteString(escDoubleOff + escBoldOff)
		if status != "Approved" && status != "Paid" {
			b.WriteString(strings.ToUpper(status) + "\n")
		}
		b.WriteString(escAlignLeft)
		b.WriteString(strings.Repeat("-", cols) + "\n")
		b.WriteString(escposRow("Invoice", number, cols))
		b.WriteString(escposRow("Date", createdAt.Format("02 Jan 2006"), cols))
		if customer != "" {
			b.WriteString(escposRow("Customer", customer, cols))
		}
		if location != "" {
			b.WriteString(escposRow("Location", location, cols))
		}
		b.WriteString(escposRow("Status", status, cols))
		b.WriteString(strings.Repeat("-", cols) + "\n")
		b.WriteString(escBoldOn)
		b.WriteString(escposRow("TOTAL", money(amount), cols))
		b.WriteString(escBoldOff)
		b.WriteString("\n\n\n")
		b.WriteString(escCut)

		return &QZPrintPayload{
			Format: "ESC-POS",
			Items: []QZDataItem{{
				Type:   "raw",
				Format: "command",
				Flavor: "plain",
				Data:   b.String(),
			}},
		}, nil
	}

	watermark := ""
	if status != "Approved" && status != "Paid" {
		watermark = `<div class="draft">` + html.EscapeString(strings.ToUpper(status)) + `</div>`
	}
	row := func(label, value string) string {
		if value == "" {
			return ""
		}
		return "<tr><td class=\"k\">" + html.EscapeString(label) + "</td><td>" + html.EscapeString(value) + "</td></tr>"
	}
	return &QZPrintPayload{
		Format: "HTML",
		Items: []QZDataItem{{
			Type:   "pixel",
			Format: "html",
			Flavor: "plain",
			Data: `<html><head><meta charset="utf-8"><style>
body{font-family:Arial,Helvetica,sans-serif;margin:0;padding:28px;font-size:14px;}
h1{font-size:26px;margin:0 0 2px;}
.draft{color:#b91c1c;font-size:16px;font-weight:700;letter-spacing:3px;margin-bottom:10px;}
hr{border:0;border-top:2px solid #000;margin:14px 0;}
table{border-collapse:collapse;} td{padding:4px 0;} td.k{font-weight:700;padding-right:20px;}
.total{margin-top:24px;font-size:22px;font-weight:700;}
</style></head><body>
<h1>Tax Invoice</h1>` + watermark + `<hr>
<table>` + row("Invoice", number) + row("Date", createdAt.Format("02 Jan 2006")) +
				row("Customer", customer) + row("Location", location) + row("Status", status) + `</table>
<div class="total">Total: ` + money(amount) + `</div>
</body></html>`,
		}},
	}, nil
}
