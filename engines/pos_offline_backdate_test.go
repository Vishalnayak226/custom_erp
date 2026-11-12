package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

// Stage 49.5.7 - a till's clock cannot date an offline sale into any open period.

func TestStage4957OfflineSaleDayFollowsTheTillOnlyWithinTheLimit(t *testing.T) {
	db.InitDB(testConnStr())
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	today := BusinessToday()
	old := time.Now().AddDate(-1, 0, 0)
	week := time.Now().AddDate(0, 0, -2)
	future := time.Now().AddDate(0, 0, 40)
	day := func(at time.Time) string {
		d, _, err := offlineSalePostingDay(tx, "tenant_default", &at)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	// Off: the till's day stands (today's behaviour) when its period is open.
	if got := day(old); got == today {
		t.Skipf("last year's period is closed here; the off case cannot be shown (%s)", got)
	}
	tuningSetting(t, SettingPOSOfflineMaxBackdateDays, "7")
	if got := day(old); got != today {
		t.Errorf("a year-old till day posted on %s, want today (%s)", got, today)
	}
	if got := day(week); got != week.In(BusinessLocation()).Format("2006-01-02") {
		t.Errorf("a two-day-old sale posted on %s, want its own day", got)
	}
	if got := day(future); got != today {
		t.Errorf("a future till day posted on %s, want today", got)
	}
}
