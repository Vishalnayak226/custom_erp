package engines

import (
	"os"
	"sync"
	"time"
)

// Stage 132.19: the business's day, whatever the server's clock. The
// production server runs on UTC, so time.Now().Format("2006-01-02") gave
// yesterday's date until 05:30 IST - a bill, a GRN or a settlement after
// midnight was dated the day (and, on the 1st, the month) before. Every
// "today" of the engines comes from BusinessToday instead; the database side
// is public.erp_today() / erp_day_start() (db/migrations_stage132_19_business_day.sql).
//
// ERP_BUSINESS_TZ names the zone (IANA, e.g. Asia/Dubai); unset, Asia/Kolkata -
// the same default as public.erp_business_tz(). A system without zone data
// (a Windows machine) falls back to a fixed +05:30, which is India's all year.
var businessLocation = sync.OnceValue(func() *time.Location {
	name := os.Getenv("ERP_BUSINESS_TZ")
	if name == "" {
		name = "Asia/Kolkata"
	}
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.FixedZone("IST", 5*3600+30*60)
})

// BusinessLocation is the business's time zone.
func BusinessLocation() *time.Location { return businessLocation() }

// BusinessToday is today's date in the business's time zone, "2006-01-02".
func BusinessToday() string { return time.Now().In(businessLocation()).Format("2006-01-02") }
