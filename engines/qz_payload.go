package engines

import (
	"fmt"
	"html"
	"strconv"
	"strings"
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
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
