package engines

import (
	"bytes"
	"testing"
)

// Stage 49.8.2: a CSV a person opens in a spreadsheet never carries a live
// formula, and numbers - negative ones included - stay numbers.
func TestStage49ExportCSVNeverCarriesAFormula(t *testing.T) {
	var buf bytes.Buffer
	w := newSafeCSVWriter(&buf)
	_ = w.Write([]string{`=HYPERLINK("http://x","click")`, "+91 98765", "@SUM(A1)", "-5", "-12.50", "+3", "Kurta", " =1+1", ""})
	w.Flush()
	got := buf.String()
	want := `"'=HYPERLINK(""http://x"",""click"")",'+91 98765,'@SUM(A1),-5,-12.50,+3,Kurta,' =1+1,` + "\n"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}
