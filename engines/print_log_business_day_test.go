package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
	"time"
)

// Item 295.3: a label printed at 00:10 on the business's day counts on that
// day, whatever the server's clock (production's is UTC, where it is stored
// as 18:40 the day before). stickerRows returns nothing on a SQL error, so this
// also proves the query still runs.
func TestPrintLogsCountBusinessDays(t *testing.T) {
	db.InitDB(testConnStr())
	sku := fmt.Sprintf("T2953-%d", time.Now().UnixNano())
	defer db.DB.Exec(`DELETE FROM tenant_default.sticker_print_log WHERE sku = $1`, sku)
	for _, at := range []string{"public.erp_day_start('2026-11-27') + interval '10 minutes'", "public.erp_day_start('2026-11-27') - interval '10 minutes'"} {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.sticker_print_log (sku, printer_code, printed_by, printed_at)
			VALUES ($1, 'T', 'test', `+at+`)`, sku); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	count := func(day string) int {
		rows, err := stickerRows("default", "tenant_default", map[string]string{"from": day, "to": day})
		if err != nil {
			t.Fatalf("stickerRows %s: %v", day, err)
		}
		n := 0
		for _, r := range rows {
			if r.H["sku"] == sku {
				n++
			}
		}
		return n
	}
	if got := count("2026-11-27"); got != 1 {
		t.Errorf("27th: %d of the test labels, want 1 (00:10 on the 27th)", got)
	}
	if got := count("2026-11-26"); got != 1 {
		t.Errorf("26th: %d of the test labels, want 1 (23:50 on the 26th)", got)
	}

	// The other day-bounded readers changed with it still run.
	p := map[string]string{"from": "2026-11-26", "to": "2026-11-27"}
	if _, err := printFailures("default", p); err != nil {
		t.Errorf("printFailures: %v", err)
	}
	if _, err := printReprintAudit("default", p); err != nil {
		t.Errorf("printReprintAudit: %v", err)
	}
	if _, err := GetCRMForecast("default"); err != nil {
		t.Errorf("GetCRMForecast: %v", err)
	}
	if _, _, err := GetCRMLifecycleCounts("default"); err != nil {
		t.Errorf("GetCRMLifecycleCounts: %v", err)
	}
}
