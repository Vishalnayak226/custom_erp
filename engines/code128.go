package engines

import (
	"fmt"
	"html"
	"strings"
)

// Stage 42.1.11 - real Code 128 barcode symbology, hand-rolled from the
// public ISO/IEC 15417 Code 128 pattern table (no new dependency, per the
// first principle). Code Set B only: one input byte maps to one symbol via
// (ASCII code - 32), covering space (32) through ~ (126) - the exact range
// this tree's barcode values (arbitrary Item.barcode text, not necessarily
// digits) actually need. Code Set A, mid-stream set-switching and GS1-128
// application identifiers are explicitly out of scope for this pass, per the
// plan's own framing ("Code 128 (GS1-128/SSCC where a partner requires it)")
// - a future addition to this same table, not a guess made here.
//
// This is the browser-print fallback path ONLY. The QZ Tray silent-print path
// (engines/qz_payload.go's BuildStickerPayload) already emits a real,
// scanner-correct Code 128 barcode via the label printer's own native ^BC ZPL
// command and needs nothing from this file. What this closes is the OTHER
// path: the @media print sticker sheet a browser without QZ configured falls
// back to (public/app.js's renderPrintSheet), which rendered the barcode
// value as plain monospace text with no actual bars at all.
//
// code128Widths[v] is the bar/space width pattern for symbol value v (0-105)
// - verbatim from the ISO/IEC 15417 standard table (cross-checked against
// Wikipedia's published table rather than trusted from memory, since a single
// transcription error here would produce a barcode that looks right but does
// not scan, defeating the entire point of this item). Each pattern is 6
// widths (1-4 modules each) summing to 11 modules. code128StopWidths (value
// 106, the STOP symbol) is the one exception: 7 widths summing to 13 modules,
// the extra final bar that lets a scanner read the symbol in either
// direction.
var code128Widths = [106]string{
	"212222", "222122", "222221", "121223", "121322", "131222", "122213", "122312", "132212", "221213",
	"221312", "231212", "112232", "122132", "122231", "113222", "123122", "123221", "223211", "221132",
	"221231", "213212", "223112", "312131", "311222", "321122", "321221", "312212", "322112", "322211",
	"212123", "212321", "232121", "111323", "131123", "131321", "112313", "132113", "132311", "211313",
	"231113", "231311", "112133", "112331", "132131", "113123", "113321", "133121", "313121", "211331",
	"231131", "213113", "213311", "213131", "311123", "311321", "331121", "312113", "312311", "332111",
	"314111", "221411", "431111", "111224", "111422", "121124", "121421", "141122", "141221", "112214",
	"112412", "122114", "122411", "142112", "142211", "241211", "221114", "413111", "241112", "134111",
	"111242", "121142", "121241", "114212", "124112", "124211", "411212", "421112", "421211", "212141",
	"214121", "412121", "111143", "111341", "131141", "114113", "114311", "411113", "411311", "113141",
	"114131", "311141", "411131", "211412", "211214", "211232",
}

const (
	code128StartB    = 104
	code128Stop      = 106
	code128StopWidth = "2331112"
)

// EncodeCode128B converts data into the sequence of bar/space widths (in
// modules, alternating bar-space-bar..., always starting AND ending on a bar)
// for a Code Set B Code 128 symbol: START B, one symbol per input byte, the
// standard mod-103 checksum, then STOP. Only ASCII 32 (space) through 126
// (~) is accepted - not 127/DEL, which no barcode value in this tree needs -
// so an out-of-range character is a clean error rather than a silently wrong
// symbol.
func EncodeCode128B(data string) ([]int, error) {
	if data == "" {
		return nil, fmt.Errorf("cannot encode an empty value as a barcode")
	}
	values := make([]int, len(data))
	for i := 0; i < len(data); i++ {
		c := data[i]
		if c < 32 || c > 126 {
			return nil, fmt.Errorf("character %q at position %d is outside Code Set B's supported range (space through ~)", c, i)
		}
		values[i] = int(c) - 32
	}

	// The standard checksum: start value counts once (as if at position 0),
	// then each data symbol's value is weighted by its 1-based position.
	checksum := code128StartB
	for i, v := range values {
		checksum += v * (i + 1)
	}
	checksum %= 103

	symbols := make([]int, 0, len(values)+3)
	symbols = append(symbols, code128StartB)
	symbols = append(symbols, values...)
	symbols = append(symbols, checksum, code128Stop)

	var widths []int
	for _, sym := range symbols {
		pattern := code128StopWidth
		if sym != code128Stop {
			pattern = code128Widths[sym]
		}
		for _, ch := range pattern {
			widths = append(widths, int(ch-'0'))
		}
	}
	return widths, nil
}

// Code128 rendering constants: module is the pixel width of one barcode
// module - 2px keeps a ~20-character SKU barcode comfortably inside a
// 260px sticker label (public/styles.css's .sticker-label width) while
// staying well above the ~0.19mm/module floor most handheld scanners need at
// typical label-printer DPI. quietZone is the mandatory blank margin either
// side (>=10 modules per the standard - a scanner reads the FIRST bar's edge
// against the quiet zone to calibrate module width, so skipping it produces
// a symbol that measures wrong even though every bar is drawn correctly).
const (
	code128Module    = 2
	code128QuietMods = 10
	code128BarHeight = 60
)

// RenderCode128SVG renders data as a self-contained Code 128 SVG string -
// black bars on a white background, the human-readable value underneath
// (the "HRI" text every printed barcode label carries, for when a scanner
// misreads or isn't handy) - ready to embed directly into an HTML page with
// no image request and no client-side barcode library.
func RenderCode128SVG(data string) (string, error) {
	widths, err := EncodeCode128B(data)
	if err != nil {
		return "", err
	}
	totalModules := code128QuietMods * 2
	for _, w := range widths {
		totalModules += w
	}
	width := totalModules * code128Module
	height := code128BarHeight + 20 // room for the HRI text line below the bars

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" role="img" aria-label="barcode">`,
		width, height, width, height)
	b.WriteString(`<rect width="100%" height="100%" fill="#fff"/>`)
	x := code128QuietMods * code128Module
	for i, w := range widths {
		wpx := w * code128Module
		if i%2 == 0 { // symbols always start on a bar, so even index = bar
			fmt.Fprintf(&b, `<rect x="%d" y="0" width="%d" height="%d" fill="#000"/>`, x, wpx, code128BarHeight)
		}
		x += wpx
	}
	fmt.Fprintf(&b, `<text x="%d" y="%d" font-family="monospace" font-size="14" text-anchor="middle">%s</text>`,
		width/2, code128BarHeight+16, html.EscapeString(data))
	b.WriteString(`</svg>`)
	return b.String(), nil
}
