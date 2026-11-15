package engines

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
)

// 50.12 / BLD-027 (ask 193): metering, quota and tenant cost.
//
// Billable units (SaaSMeters): documents (created in the month), users
// (active users, a month-end gauge) and storage_mb (a gauge). The period is
// the calendar month in Indian time (IST, UTC+05:30), so a document made at
// 23:59 IST on the 30th is that month's and one made a minute later is the
// next month's, whatever the server clock says.
//
// Rules:
//   - A usage fact is keyed (tenant, meter, dedupe_key): recording it twice is
//     recording it once, so a replayed batch or retried job never bills twice.
//   - A gauge (snapshot) is the same key overwritten with the latest value.
//   - A correction is its own keyed row with a signed quantity and a reason;
//     the original is never edited, and a repeated correction id is ignored.
//   - A quota is the plan's included quantity per meter. Over it, a meter with
//     an overage rate keeps working and is billed the overage; a meter with no
//     rate is refused (SAAS-0193). If metering itself cannot be read, the
//     action is allowed and logged: a billing fault never stops a client
//     trading, and it never makes a charge.
//   - Cost = the plan fee + overage. Overage is shown to the client and the
//     owner here; collecting it from Razorpay is a billing decision, see
//     BLD-027 in the build checklist.

// SaaSMeters are the billable units.
var SaaSMeters = map[string]string{
	"documents":  "Documents created",
	"users":      "Active users",
	"storage_mb": "Storage (MB)",
}

// SaaSUsagePeriod is the Indian-time calendar month of a moment, "2026-11".
func SaaSUsagePeriod(t time.Time) string { return t.In(istZone).Format("2006-01") }

func checkMeter(meter string) error {
	if _, ok := SaaSMeters[meter]; !ok {
		return saasFail("%q is not a billable meter", meter)
	}
	return nil
}

// RecordSaaSUsage records one usage fact once. It reports whether this call
// was the one that recorded it (false = already known, nothing added).
func RecordSaaSUsage(tenantID, meter, dedupeKey string, qty int64, at time.Time) (bool, error) {
	if err := checkMeter(meter); err != nil {
		return false, err
	}
	if strings.TrimSpace(dedupeKey) == "" || qty < 0 {
		return false, saasFail("a usage fact needs a key and a quantity of zero or more")
	}
	res, err := db.DB.Exec(`INSERT INTO public.saas_usage (tenant_id, meter, period_key, dedupe_key, qty, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (tenant_id, meter, dedupe_key) DO NOTHING`,
		tenantID, meter, SaaSUsagePeriod(at), dedupeKey, qty, at)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// SetSaaSUsageSnapshot records a gauge: the same key overwritten with the
// latest reading.
func SetSaaSUsageSnapshot(tenantID, meter, key string, qty int64, at time.Time) error {
	if err := checkMeter(meter); err != nil {
		return err
	}
	_, err := db.DB.Exec(`INSERT INTO public.saas_usage (tenant_id, meter, period_key, dedupe_key, qty, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, meter, dedupe_key) DO UPDATE SET qty = EXCLUDED.qty, occurred_at = EXCLUDED.occurred_at`,
		tenantID, meter, SaaSUsagePeriod(at), "snap:"+key, qty, at)
	return err
}

// CorrectSaaSUsage adds a signed correction to a period. Repeating the same
// correction id changes nothing.
func CorrectSaaSUsage(tenantID, meter, correctionID string, delta int64, at time.Time, reason string) (bool, error) {
	if err := checkMeter(meter); err != nil {
		return false, err
	}
	if strings.TrimSpace(correctionID) == "" || strings.TrimSpace(reason) == "" {
		return false, saasFail("a correction needs an id and a reason")
	}
	res, err := db.DB.Exec(`INSERT INTO public.saas_usage (tenant_id, meter, period_key, dedupe_key, qty, occurred_at, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (tenant_id, meter, dedupe_key) DO NOTHING`,
		tenantID, meter, SaaSUsagePeriod(at), "corr:"+correctionID, delta, at, reason)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// SnapshotSaaSDocumentUsage counts the documents a client created on one
// business day and stores it as that day's reading, so the month's total is
// the sum of its days and re-running a day just refreshes it.
func SnapshotSaaSDocumentUsage(tenantID, day string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	d, err := time.ParseInLocation("2006-01-02", day, istZone)
	if err != nil {
		return saasFail("day must be YYYY-MM-DD")
	}
	var n int64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents
		WHERE created_at >= public.erp_day_start($1::date) AND created_at < public.erp_day_start($1::date + 1)`, schema), day).Scan(&n); err != nil {
		return err
	}
	return SetSaaSUsageSnapshot(tenantID, "documents", "docs:"+day, n, d.Add(12*time.Hour))
}

// SaaSMeterLine is one meter in a statement.
type SaaSMeterLine struct {
	Meter        string `json:"meter"`
	Label        string `json:"label"`
	Used         int64  `json:"used"`
	Quota        int64  `json:"quota"` // 0 = no quota
	Remaining    int64  `json:"remaining"`
	OveragePaise int64  `json:"overage_paise"`
}

// SaaSStatement is a client's usage and cost for one Indian-time month.
type SaaSStatement struct {
	TenantID     string          `json:"tenant_id"`
	Period       string          `json:"period"`
	Plan         string          `json:"plan"`
	Meters       []SaaSMeterLine `json:"meters"`
	BasePaise    int64           `json:"base_paise"`
	OveragePaise int64           `json:"overage_paise"`
	TotalPaise   int64           `json:"total_paise"`
}

// SaaSUsageStatement works out a month's usage and cost for one client.
func SaaSUsageStatement(tenantID, period string) (*SaaSStatement, error) {
	if len(period) != 7 {
		return nil, saasFail("period must be YYYY-MM")
	}
	st := &SaaSStatement{TenantID: tenantID, Period: period, Meters: []SaaSMeterLine{}}
	var plan *SaaSPlan
	if sub, err := GetSaaSSubscription(tenantID); err != nil {
		return nil, err
	} else if sub != nil {
		if p, err := GetSaaSPlan(sub.PlanKey); err == nil {
			plan = p
			st.Plan, st.BasePaise = p.PlanKey, p.AmountPaise
		}
	}
	rows, err := db.DB.Query(`SELECT meter, COALESCE(SUM(qty), 0) FROM public.saas_usage WHERE tenant_id = $1 AND period_key = $2 GROUP BY meter`, tenantID, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := map[string]int64{}
	for rows.Next() {
		var m string
		var q int64
		if err := rows.Scan(&m, &q); err != nil {
			return nil, err
		}
		if q < 0 {
			q = 0
		}
		used[m] = q
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(SaaSMeters))
	for m := range SaaSMeters {
		names = append(names, m)
	}
	sort.Strings(names)
	for _, m := range names {
		line := SaaSMeterLine{Meter: m, Label: SaaSMeters[m], Used: used[m]}
		if plan != nil {
			line.Quota = plan.Quotas[m]
			if line.Quota > 0 {
				if over := line.Used - line.Quota; over > 0 {
					line.OveragePaise = over * plan.OveragePaise[m]
				} else {
					line.Remaining = line.Quota - line.Used
				}
			}
		}
		st.OveragePaise += line.OveragePaise
		st.Meters = append(st.Meters, line)
	}
	st.TotalPaise = st.BasePaise + st.OveragePaise
	return st, nil
}

// CheckSaaSQuota decides whether a client may add `add` more of a meter this
// month. See the rules at the top of this file.
func CheckSaaSQuota(tenantID, meter string, add int64) error {
	st, err := SaaSUsageStatement(tenantID, SaaSUsagePeriod(saasNow()))
	if err != nil {
		log.Printf("[SAAS] quota for %s/%s could not be read, allowing: %v", tenantID, meter, err)
		return nil
	}
	for _, l := range st.Meters {
		if l.Meter != meter || l.Quota <= 0 || l.Used+add <= l.Quota {
			continue
		}
		plan, perr := GetSaaSPlan(st.Plan)
		if perr == nil && plan.OveragePaise[meter] > 0 {
			return nil // over quota but billed as overage
		}
		return &ValidationError{Code: "SAAS-0193", Message: fmt.Sprintf("%s limit of %d reached for this plan (this month: %d)", l.Label, l.Quota, l.Used)}
	}
	return nil
}

// SaaSTenantCosts is the owner's view: every subscribed client's cost for a month.
func SaaSTenantCosts(period string) ([]SaaSStatement, error) {
	rows, err := db.DB.Query(`SELECT tenant_id FROM public.saas_subscriptions ORDER BY tenant_id`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []SaaSStatement{}
	for _, id := range ids {
		st, err := SaaSUsageStatement(id, period)
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return out, nil
}

// SnapshotSaaSGaugeUsage records a client's active users and database storage
// (in MB, rounded up) as this month's gauge. A gauge is a reading of the
// present, so one keyed row per client, meter and month is overwritten with
// the latest reading and the month's figure is the last one taken. A past day
// is skipped: replaying it would stamp today's reading on an old month. Media
// files kept outside the database are not counted.
func SnapshotSaaSGaugeUsage(tenantID, day string) error {
	now := saasNow()
	if day != now.In(istZone).Format("2006-01-02") {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var users, bytes int64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.users WHERE COALESCE(status, 'Active') = 'Active'`, schema)).Scan(&users); err != nil {
		return err
	}
	if err := db.DB.QueryRow(`SELECT COALESCE(SUM(pg_total_relation_size(c.oid)), 0)::bigint FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relkind = 'r'`, schema).Scan(&bytes); err != nil {
		return err
	}
	key := "gauge:" + SaaSUsagePeriod(now)
	if err := SetSaaSUsageSnapshot(tenantID, "users", key, users, now); err != nil {
		return err
	}
	return SetSaaSUsageSnapshot(tenantID, "storage_mb", key, (bytes+(1<<20)-1)>>20, now)
}

// SnapshotSaaSUsageAll runs the daily pass for every client with a
// subscription: yesterday's and today's documents, and today's gauges. It is
// keyed throughout, so running it every hour or twice a day changes nothing
// but the latest reading. It returns how many clients it covered.
func SnapshotSaaSUsageAll() (int, error) {
	rows, err := db.DB.Query(`SELECT tenant_id FROM public.saas_subscriptions`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	today := saasNow().In(istZone)
	days := []string{today.AddDate(0, 0, -1).Format("2006-01-02"), today.Format("2006-01-02")}
	n := 0
	for _, id := range ids {
		var firstErr error
		for _, d := range days {
			if err := SnapshotSaaSDocumentUsage(id, d); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if err := SnapshotSaaSGaugeUsage(id, days[1]); err != nil && firstErr == nil {
			firstErr = err
		}
		if firstErr != nil {
			log.Printf("[SAAS] usage snapshot for %s: %v", id, firstErr)
			continue
		}
		n++
	}
	return n, nil
}
