package server

import (
	"regexp"
	"strings"
	"testing"
)

// TestStage47142DatesDisplayUnambiguously is the static half of 47.14.2: the
// one shared formatDay helper builds "03 Feb 2026" from the ISO parts (never
// toLocaleDateString, which reads month first on a US-locale browser), and
// the record list's Date cells and a batch's expiry go through it.
func TestStage47142DatesDisplayUnambiguously(t *testing.T) {
	app := moduleSource(t, "../../public/app.js")
	body := regexp.MustCompile(`(?s)function formatDay\(value\) \{(.*?)\n\}`).FindStringSubmatch(app)
	if body == nil {
		t.Fatal("public/app.js has no formatDay(value) helper")
	}
	if strings.Contains(body[1], "toLocale") {
		t.Error("formatDay uses a locale formatter - build it from the ISO parts")
	}
	if !strings.Contains(app, `const FORMAT_DAY_MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];`) {
		t.Error("formatDay must name the month as a word")
	}
	batch := regexp.MustCompile(`(?s)function batchCellHTML\(line\) \{(.*?)\n\}`).FindStringSubmatch(app)
	if batch == nil || !strings.Contains(batch[1], "formatDay(expiry)") {
		t.Error("batchCellHTML does not show the expiry through formatDay")
	}
	docs := moduleSource(t, "../../public/view-documents.js")
	if !strings.Contains(docs, "f.fieldtype === 'Date' && val) {") || !strings.Contains(docs, "copyableCell(formatDay(val), val)") {
		t.Error("the record list does not show Date fields through formatDay")
	}
}
