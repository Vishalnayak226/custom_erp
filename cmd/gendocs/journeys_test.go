package main

import (
	"strings"
	"testing"
)

// BLD-002: open items land on their stage's journey, decision and blocked
// items are counted, and a stage nobody has placed is listed, not guessed.
func TestBLD002BacklogMapsOntoJourneys(t *testing.T) {
	checklist := strings.Join([]string{
		"- [ ] **53.4 Till closes the session.** text *Status: Not started*",
		"  - [ ] **53.5 Refund on a card.** [needs design decision: which provider] *Status: Needs decision*",
		"- [ ] **75.2.3 E-invoice credit note QR.** *Status: Blocked - sandbox*",
		"- [ ] **75.1.7 Restore drill from the bucket.** *Status: Not started*",
		"- [x] **53.1 Done item.** *Status: Done*",
		"- [ ] **999.1 A stage nobody placed.** *Status: Not started*",
	}, "\n")
	build := strings.Join([]string{
		"| [ ] | **JRN-01 POS/store** | Open session | Store/finance; 47.2 |",
		"| [ ] | **JRN-12 India tax** | GST | Tax owner; 75.2 |",
		"| [ ] | **JRN-21 platform/integrations** | API | Platform owner; 38 |",
	}, "\n")
	body := string(journeyMapFiles(checklist, build, "2026-10-26")[journeyMapPath])
	for _, want := range []string{
		"| JRN-01 POS/store | REF-RETAIL-IN | Store/finance; 47.2 | 2 | 1 | 0 |",
		"| JRN-12 India tax | REF-RETAIL-IN, REF-WAREHOUSE-IN | Tax owner; 75.2 | 1 | 0 | 1 |",
		"| JRN-21 platform/integrations | REF-RETAIL-IN, REF-WAREHOUSE-IN | Platform owner; 38 | 1 | 0 | 0 |",
		"**Unmapped:** Stage 999 (1)",
		"- **53.5** Refund on a card *(decision)*",
		"5 open checklist items",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("map is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "53.1") {
		t.Error("a closed item must not appear")
	}
}
