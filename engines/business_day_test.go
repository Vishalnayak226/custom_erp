package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

// Stage 132.19: a business day is the business's day on any server clock. On
// production the session is UTC: a payment at 01:45 IST on the 26th is stored
// as 2026-10-25 20:15, and must count on the 26th.
func TestBusinessDayOnAUTCAndAnISTServer(t *testing.T) {
	db.InitDB(testConnStr())
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	check := func(zone, wantStart, wantEnd string, stored, wantLocal string) {
		t.Helper()
		if _, err := tx.Exec("SET LOCAL TimeZone = '" + zone + "'"); err != nil {
			t.Fatalf("set zone: %v", err)
		}
		var start, end, local string
		if err := tx.QueryRow(`SELECT public.erp_day_start('2026-10-26')::text, public.erp_day_start(DATE '2026-10-26' + 1)::text,
			public.erp_local_date($1::timestamp)::text`, stored).Scan(&start, &end, &local); err != nil {
			t.Fatalf("%s: %v", zone, err)
		}
		if start != wantStart || end != wantEnd || local != wantLocal {
			t.Errorf("%s session: day 26 runs %s to %s (want %s to %s); %s is business date %s (want %s)", zone, start, end, wantStart, wantEnd, stored, local, wantLocal)
		}
		// The same row through a report's filter shape: inside the day, not the day before.
		var in26, in25 bool
		tx.QueryRow(`SELECT $1::timestamp >= public.erp_day_start('2026-10-26') AND $1::timestamp < public.erp_day_start(DATE '2026-10-26' + 1),
			$1::timestamp >= public.erp_day_start('2026-10-25') AND $1::timestamp < public.erp_day_start(DATE '2026-10-25' + 1)`, stored).Scan(&in26, &in25)
		if !in26 || in25 {
			t.Errorf("%s session: %s counted on the 26th=%v, on the 25th=%v - want the 26th only", zone, stored, in26, in25)
		}
	}
	check("UTC", "2026-10-25 18:30:00", "2026-10-26 18:30:00", "2026-10-25 20:15:55", "2026-10-26")
	check("Asia/Kolkata", "2026-10-26 00:00:00", "2026-10-27 00:00:00", "2026-10-26 01:45:55", "2026-10-26")

	// Go's "today" is the database's business today.
	var dbToday string
	if err := tx.QueryRow(`SELECT public.erp_today()::text`).Scan(&dbToday); err != nil {
		t.Fatalf("erp_today: %v", err)
	}
	if got := BusinessToday(); got != dbToday && got != time.Now().In(BusinessLocation()).Add(-time.Minute).Format("2006-01-02") {
		t.Errorf("BusinessToday %s, database erp_today %s", got, dbToday)
	}
}
