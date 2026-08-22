package engines

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// GenerateShippingLabelPDF returns a self-contained 4x6-inch PDF. It calls
// GenerateShippingLabel first so invoice-before-label enforcement and the
// label_generated_at stamp remain in their existing shared choke point.
func GenerateShippingLabelPDF(tenantID, bookingID string) ([]byte, error) {
	if _, err := GenerateShippingLabel(tenantID, bookingID); err != nil {
		return nil, err
	}
	_, data, _, err := fetchLogisticsBooking(tenantID, bookingID)
	if err != nil {
		return nil, err
	}
	awb := courierDataString(data, "awb_number")
	if awb == "" {
		return nil, fmt.Errorf("booking %s has no AWB", bookingID)
	}
	lines := []string{
		"SHIP TO PIN: " + courierDataString(data, "destination_pincode"),
		"CARRIER: " + courierDataString(data, "carrier"),
		"AWB: " + awb,
		"ORDER: " + courierDataString(data, "order_id"),
		"BOOKING: " + bookingID,
	}
	var stream strings.Builder
	stream.WriteString("0 0 0 rg\nBT /F1 18 Tf 24 405 Td (SHIPPING LABEL) Tj ET\n")
	y := 378
	for _, line := range lines {
		stream.WriteString(fmt.Sprintf("BT /F1 11 Tf 24 %d Td (%s) Tj ET\n", y, pdfEscape(line)))
		y -= 20
	}
	stream.WriteString(code128PDFCommands(awb, 24, 160, 240, 90))
	stream.WriteString(fmt.Sprintf("BT /F1 12 Tf 24 138 Td (%s) Tj ET\n", pdfEscape(awb)))
	return buildSinglePagePDF(288, 432, stream.String()), nil
}

func pdfEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "(", "\\(")
	return strings.ReplaceAll(s, ")", "\\)")
}

func buildSinglePagePDF(width, height int, content string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>", width, height),
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&out, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}

// Code 128-B covers ordinary courier AWBs without restricting them to digits.
// The checksum and stop symbol follow ISO/IEC 15417; the PDF draws each module
// as a vector rectangle, so the result stays sharp on a 203/300-DPI printer.
func code128PDFCommands(value string, x, y, width, height float64) string {
	value = strings.ToUpper(value)
	var codes []int
	checksum := 104 // Start B
	for i, r := range value {
		code := int(r) - 32
		if code < 0 || code > 95 {
			code = int('?') - 32
		}
		codes = append(codes, code)
		checksum += code * (i + 1)
	}
	codes = append([]int{104}, codes...)
	codes = append(codes, checksum%103, 106)
	modules := 0
	for _, code := range codes {
		for _, c := range code128Patterns[code] {
			n, _ := strconv.Atoi(string(c))
			modules += n
		}
	}
	moduleWidth := width / float64(modules)
	var b strings.Builder
	pos := x
	black := true
	for _, code := range codes {
		for _, c := range code128Patterns[code] {
			n, _ := strconv.Atoi(string(c))
			w := float64(n) * moduleWidth
			if black {
				b.WriteString(fmt.Sprintf("%.3f %.3f %.3f %.3f re f\n", pos, y, w, height))
			}
			pos += w
			black = !black
		}
	}
	return b.String()
}

var code128Patterns = []string{
	"212222", "222122", "222221", "121223", "121322", "131222", "122213", "122312", "132212", "221213", "221312", "231212",
	"112232", "122132", "122231", "113222", "123122", "123221", "223211", "221132", "221231", "213212", "223112", "312131",
	"311222", "321122", "321221", "312212", "322112", "322211", "212123", "212321", "232121", "111323", "131123", "131321",
	"112313", "132113", "132311", "211313", "231113", "231311", "112133", "112331", "132131", "113123", "113321", "133121",
	"313121", "211331", "231131", "213113", "213311", "213131", "311123", "311321", "331121", "312113", "312311", "332111",
	"314111", "221411", "431111", "111224", "111422", "121124", "121421", "141122", "141221", "112214", "112412", "122114",
	"122411", "142112", "142211", "241211", "221114", "413111", "241112", "134111", "111242", "121142", "121241", "114212",
	"124112", "124211", "411212", "421112", "421211", "212141", "214121", "412121", "111143", "111341", "131141", "114113",
	"114311", "411113", "411311", "113141", "114131", "311141", "411131", "211412", "211214", "211232", "2331112",
}
