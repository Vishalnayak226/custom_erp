package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"custom_erp/engines"
)

// outboxDrain: after the server restart, events that were Pending must be
// dispatched by the restarted server. It looks for Pending rows older than a
// minute; none left means the outbox drained.
func outboxDrain(r *Runner, pendingAtKill int64) Disturbance {
	d := Disturbance{Name: "outbox drains after the server restart"}
	var total int64
	_ = r.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.integration_event_outbox`, r.Cfg.Schema)).Scan(&total)
	stale := func() int64 {
		var n int64
		_ = r.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.integration_event_outbox WHERE status = 'Pending' AND created_at < now() - interval '60 seconds'`, r.Cfg.Schema)).Scan(&n)
		return n
	}
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) && stale() > 0 {
		time.Sleep(3 * time.Second)
	}
	left := stale()
	d.Measures = []string{fmt.Sprintf("%d event(s) in the outbox, %d Pending at the moment of the kill, %d Pending for over a minute afterwards", total, pendingAtKill, left)}
	switch {
	case total == 0:
		d.Status, d.Detail = "not_exercised", "the journey wrote no outbox events, so there was nothing to drain"
	case left == 0:
		d.Status, d.Detail = "pass", "no event stayed Pending after the restart"
	default:
		d.Status, d.Detail = "fail", fmt.Sprintf("%d event(s) still Pending after 120 s", left)
	}
	return d
}

// tokenChurn sends many requests with distinct bearer tokens, junk ones and
// validly signed ones, so the rate limiter makes a key for each, then checks
// the server's memory stops growing. The limiter's own table is not visible
// from outside, so memory is the evidence.
func tokenChurn(r *Runner, sp *serverProc, base string, c *commonFlags) Disturbance {
	d := Disturbance{Name: "token and rate-limiter key churn"}
	const batch = 4000
	rss := func() int64 { return ReadProc(int(sp.pid.Load())).RSSKB }
	hc := &http.Client{Timeout: 20 * time.Second}
	var seq atomic.Int64
	run := func(valid bool) (ok, limited int64) {
		var wg sync.WaitGroup
		var o, l atomic.Int64
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < batch/8; i++ {
					n := seq.Add(1)
					tok := fmt.Sprintf("junk-%d-%d", time.Now().UnixNano(), n)
					if valid {
						tok = engines.SignToken("admin", "admin", "Super Admin", c.tenant, "HO", 1)
					}
					req, _ := http.NewRequest("GET", base+"/api/v1/doc/Item?limit=1", nil)
					req.Header.Set("Authorization", "Bearer "+tok)
					resp, err := hc.Do(req)
					if err != nil {
						continue
					}
					resp.Body.Close()
					if resp.StatusCode == 429 {
						l.Add(1)
					} else {
						o.Add(1)
					}
				}
			}()
		}
		wg.Wait()
		return o.Load(), l.Load()
	}
	r0 := rss()
	o1, l1 := run(false)
	r1 := rss()
	o2, l2 := run(true)
	r2 := rss()
	o3, l3 := run(false)
	o4, l4 := run(true)
	r3 := rss()
	first, second := r2-r0, r3-r2
	d.Measures = []string{fmt.Sprintf("%d junk-token and %d signed-token requests per round, 4 rounds; %d answered, %d rate-limited", batch, batch, o1+o2+o3+o4, l1+l2+l3+l4),
		fmt.Sprintf("server RSS KB: before %d, after round 1 %d, after round 2 %d, after rounds 3-4 %d", r0, r1, r2, r3)}
	growthCap := int64(64 * 1024)
	_, up := waitHealthy(base, 10*time.Second)
	switch {
	case !up:
		d.Status, d.Detail = "fail", "the server stopped answering after the churn"
	case r0 == 0:
		d.Status, d.Detail = "not_exercised", "server memory could not be read"
	case second > growthCap:
		d.Status, d.Detail = "fail", fmt.Sprintf("memory kept growing: +%d KB over the first two rounds, +%d KB over the last two", first, second)
	default:
		d.Status, d.Detail = "pass", fmt.Sprintf("memory plateaued: +%d KB over the first two rounds, +%d KB over the last two (cap %d KB)", first, second, growthCap)
	}
	return d
}

// monthBoundary checks the database's business-day arithmetic at month, year,
// financial-year and leap-day edges, under three session time zones, because
// production runs the database in UTC while the books run on India time. There
// is no application clock seam, so the application cannot be run at a faked
// date; this tests the day-boundary function every day filter goes through.
func monthBoundary(r *Runner) Disturbance {
	d := Disturbance{Name: "month boundary and time zone (database level)"}
	tx, err := r.DB.Begin()
	if err != nil {
		d.Status, d.Detail = "fail", err.Error()
		return d
	}
	defer tx.Rollback()
	dates := []string{"2026-10-31", "2026-11-30", "2026-12-31", "2027-03-31", "2028-02-28", "2028-02-29"}
	var bad []string
	checked := 0
	const q = `SELECT
		((public.erp_day_start($1::date) AT TIME ZONE current_setting('TimeZone')) AT TIME ZONE public.erp_business_tz())::date = $1::date
		AND (((public.erp_day_start($1::date) AT TIME ZONE current_setting('TimeZone')) - interval '1 second') AT TIME ZONE public.erp_business_tz())::date = ($1::date - 1),
		EXTRACT(EPOCH FROM ((public.erp_day_start(($1::date + 1)) AT TIME ZONE current_setting('TimeZone')) - (public.erp_day_start($1::date) AT TIME ZONE current_setting('TimeZone'))))::bigint`
	for _, zone := range []string{"UTC", "Asia/Calcutta", "America/New_York"} {
		if _, err := tx.Exec(fmt.Sprintf(`SET LOCAL TimeZone = '%s'`, zone)); err != nil {
			d.Status, d.Detail = "fail", err.Error()
			return d
		}
		for _, day := range dates {
			var ok bool
			var gap int64
			err := tx.QueryRow(q, day).Scan(&ok, &gap)
			checked++
			if err != nil || !ok || gap != 86400 {
				bad = append(bad, fmt.Sprintf("%s under %s: ok=%v gap=%d s err=%v", day, zone, ok, gap, err))
			}
		}
	}
	// the business zone is a setting, so a daylight-saving zone must give a 25 hour day on its fall-back date
	if _, err := tx.Exec(`SELECT set_config('erp.business_tz', 'America/New_York', true)`); err == nil {
		var gap int64
		_ = tx.QueryRow(`SELECT EXTRACT(EPOCH FROM ((public.erp_day_start('2026-11-02'::date) AT TIME ZONE current_setting('TimeZone')) - (public.erp_day_start('2026-11-01'::date) AT TIME ZONE current_setting('TimeZone'))))::bigint`).Scan(&gap)
		checked++
		if gap != 90000 {
			bad = append(bad, fmt.Sprintf("a daylight-saving business zone did not give a 25 hour day on 2026-11-01: %d s", gap))
		}
	}
	d.Measures = []string{fmt.Sprintf("%d boundary checks over %d dates and 3 session zones, plus one daylight-saving business-zone check", checked, len(dates))}
	if len(bad) > 0 {
		d.Status, d.Detail = "fail", strings.Join(bad, "; ")
	} else {
		d.Status, d.Detail = "pass", "the business day starts at the same instant whichever zone the session uses, and the day before ends one second earlier; running the application at a faked date still needs a clock seam"
	}
	return d
}

// SoakMarkdown is the human summary of a soak evidence file.
func SoakMarkdown(ev SoakEvidence) string {
	var b strings.Builder
	b.WriteString(recordHeader("Soak evidence", ev.GeneratedAt))
	fmt.Fprintf(&b, "# Soak evidence: %s\n\nGenerated %s. Planned %s, ran %.0f s. Database figures from `%s`.\n\n", mark(ev.Overall), ev.GeneratedAt, ev.DurationPlan, ev.DurationActual, ev.DBStatsSource)
	fmt.Fprintf(&b, "Cycles: %d run, %d passed, %d failed outside any disturbance, %d failed during one.\n\nAPI p95 (median of cycles): first quarter %.0f ms, last quarter %.0f ms. Server RSS KB: start %d, end %d, peak %d.\n\n", ev.Cycles, ev.CyclesPass, ev.CyclesFailOut, ev.CyclesFailIn, ev.P95FirstQuart, ev.P95LastQuart, ev.RSSKBStart, ev.RSSKBEnd, ev.RSSKBPeak)
	b.WriteString("## Disturbances\n\n| Disturbance | Result | Detail |\n|---|---|---|\n")
	for _, d := range ev.Disturbances {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", d.Name, strings.ToUpper(d.Status), strings.ReplaceAll(d.Detail, "|", "/"))
	}
	for _, d := range ev.Disturbances {
		for _, m := range d.Measures {
			fmt.Fprintf(&b, "\n- %s: %s", d.Name, m)
		}
	}
	fmt.Fprintf(&b, "\n\n## Disk\n\nGrew %d bytes in %.0f s (%.0f bytes per hour); projected %d bytes over %.0f h. %s.\n", ev.Disk.MeasuredBytes, ev.Disk.MeasuredSeconds, ev.Disk.BytesPerHour, ev.Disk.ProjectedBytes, ev.Disk.HorizonHours, ev.Disk.Basis)
	b.WriteString("\n## Final reconciliation\n\n| Kind | Check | Result | Detail |\n|---|---|---|---|\n")
	for _, c := range ev.Final {
		res := "PASS"
		if !c.Pass {
			res = "FAIL"
			if c.Warn {
				res = "WARN"
			}
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", c.Kind, c.Name, res, strings.ReplaceAll(trunc(c.Detail, 120), "|", "/"))
	}
	fmt.Fprintf(&b, "\n## The 24-hour run\n\nNot run here. Command: `%s`\n\n## What this run cannot show\n\n", ev.Command24h)
	for _, l := range ev.Limits {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	return b.String()
}

func quartText(first, last float64) string {
	if first == 0 && last == 0 {
		return "not computed (fewer than 8 passing cycles)"
	}
	return fmt.Sprintf("first quarter %.0f ms, last quarter %.0f ms", first, last)
}
