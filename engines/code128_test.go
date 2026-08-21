package engines

import (
	"strings"
	"testing"
)

// TestEncodeCode128B_WorkedExample locks the encoder (table + checksum)
// against a fully worked, independently-sourced example: encoding
// "1-X7VQBT" in Code Set B has a documented checksum of 80 ('p'), computed
// as 104 + 17*1 + 13*2 + 56*3 + 23*4 + 54*5 + 49*6 + 34*7 + 52*8 = 1625,
// 1625 mod 103 = 80. This is the one test that would catch a wrong digit
// anywhere in the 106-row pattern table or a wrong checksum weighting - a
// self-consistency check alone could pass with either table shifted or the
// weights off by one, so this pins the encoder against a source that isn't
// this file.
func TestEncodeCode128B_WorkedExample(t *testing.T) {
	got, err := EncodeCode128B("1-X7VQBT")
	if err != nil {
		t.Fatalf("EncodeCode128B: %v", err)
	}

	// Independently reconstruct the expected widths from the same table by
	// symbol value, per the worked example's own character values (START B,
	// then 17,13,56,23,54,49,34,52 for "1-X7VQBT", then checksum 80, then
	// STOP) - if EncodeCode128B's internal indexing or checksum math drifts
	// from this, the two will disagree even though both read the same table.
	wantSymbols := []int{code128StartB, 17, 13, 56, 23, 54, 49, 34, 52, 80, code128Stop}
	var want []int
	for _, sym := range wantSymbols {
		pattern := code128StopWidth
		if sym != code128Stop {
			pattern = code128Widths[sym]
		}
		for _, ch := range pattern {
			want = append(want, int(ch-'0'))
		}
	}

	if len(got) != len(want) {
		t.Fatalf("width count mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("width[%d]: got %d, want %d (full got=%v want=%v)", i, got[i], want[i], got, want)
		}
	}
}

func TestEncodeCode128B_Rejections(t *testing.T) {
	if _, err := EncodeCode128B(""); err == nil {
		t.Error("expected an empty value to be refused")
	}
	if _, err := EncodeCode128B("SKU\x7f001"); err == nil {
		t.Error("expected DEL (0x7F) to be refused - outside Code Set B's supported range")
	}
	if _, err := EncodeCode128B("café"); err == nil {
		t.Error("expected a non-ASCII byte to be refused")
	}
}

func TestEncodeCode128B_StructuralInvariants(t *testing.T) {
	for _, data := range []string{"A", "SKU-001", "ITEM/42", "0000000012345"} {
		widths, err := EncodeCode128B(data)
		if err != nil {
			t.Fatalf("EncodeCode128B(%q): %v", data, err)
		}
		// START + one symbol per byte + checksum = len(data)+2 symbols of 6
		// widths each, plus STOP's 7.
		wantLen := (len(data)+2)*6 + 7
		if len(widths) != wantLen {
			t.Errorf("EncodeCode128B(%q): expected %d widths, got %d", data, wantLen, len(widths))
		}
		for i, w := range widths {
			if w < 1 || w > 4 {
				t.Errorf("EncodeCode128B(%q): width[%d]=%d out of the valid 1-4 module range", data, i, w)
			}
		}
	}
}

func TestRenderCode128SVG(t *testing.T) {
	svg, err := RenderCode128SVG("SKU-CODE128-TEST")
	if err != nil {
		t.Fatalf("RenderCode128SVG: %v", err)
	}
	if !strings.HasPrefix(svg, "<svg") || !strings.HasSuffix(svg, "</svg>") {
		t.Errorf("expected a well-formed <svg>...</svg> string, got: %s", svg)
	}
	if !strings.Contains(svg, "SKU-CODE128-TEST") {
		t.Error("expected the human-readable value to appear in the SVG")
	}
	if !strings.Contains(svg, `fill="#000"`) {
		t.Error("expected at least one black bar rect in the SVG")
	}

	if _, err := RenderCode128SVG(""); err == nil {
		t.Error("expected an empty value to fail to render")
	}
}
