package engines

import (
	"custom_erp/db"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Stage 49.18.7: a multi-year forecast of the bounded stores, read from a
// tenant's own data - read-only, so an operator can run it against production
// (cmd/stateforecast) and size the disk before it fills. A swept store levels
// off at its retention window; evidence kept on purpose grows for ever, and
// the forecast says by how much.

// StateForecast is one table's growth: today, its daily rate over the last 30
// days, and the bytes it will hold after one, three and five years.
type StateForecast struct {
	Table         string
	Rows          int64
	AvgRowBytes   int64
	PerDay        float64
	Retention     string // the setting that bounds it, "" when kept
	RetentionDays int    // 0 when kept
	Year1         int64
	Year3         int64
	Year5         int64
}

// stateRowOverhead covers index entries and page slack beyond the row's own
// pg_column_size: a deliberate round factor, not a measurement.
const stateRowOverhead = 1.5

var (
	retentionSettingRe = regexp.MustCompile(`(platform|crm)\.[a-z_]+_(days|hours|months)`)
	stateTimeColumns   = []string{"created_at", "received_at", "occurred_at", "completed_at", "called_at", "printed_at", "started_at", "at"}
)

// ForecastStateGrowth reports every tenant-schema state table the tenant has.
func ForecastStateGrowth(tenantID string) ([]StateForecast, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var out []StateForecast
	for _, s := range SecurityStateStores() {
		if s.Kind != "table" || strings.HasPrefix(s.Name, "public.") {
			continue
		}
		f, ok, err := forecastTable(tenantID, schema, s)
		if err != nil {
			return out, fmt.Errorf("%s: %w", s.Name, err)
		}
		if ok {
			out = append(out, f)
		}
	}
	return out, nil
}

func forecastTable(tenantID, schema string, s StateStore) (StateForecast, bool, error) {
	f := StateForecast{Table: s.Name}
	var cols []string
	rows, err := db.DB.Query(`SELECT column_name FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2`, schema, s.Name)
	if err != nil {
		return f, false, err
	}
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			cols = append(cols, c)
		}
	}
	rows.Close()
	if len(cols) == 0 {
		return f, false, nil // the tenant does not have this table
	}
	timeCol := ""
	for _, want := range stateTimeColumns {
		if containsString(cols, want) {
			timeCol = want
			break
		}
	}

	var avg float64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(AVG(pg_column_size(t.*)), 0) FROM (SELECT * FROM %s.%s LIMIT 2000) t`, schema, s.Name)).Scan(&avg); err != nil {
		return f, false, err
	}
	// The planner's estimate on a big table (a production audit log is
	// millions of rows); an exact count only where that is cheap.
	if err := db.DB.QueryRow(`SELECT GREATEST(reltuples, 0)::bigint FROM pg_class WHERE oid = to_regclass($1)`, schema+"."+s.Name).Scan(&f.Rows); err != nil {
		return f, false, err
	}
	if f.Rows < 100000 {
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s`, schema, s.Name)).Scan(&f.Rows); err != nil {
			return f, false, err
		}
	}
	f.AvgRowBytes = int64(math.Round(avg * stateRowOverhead))
	if timeCol != "" {
		var recent int64
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE %s >= NOW() - INTERVAL '30 days'`, schema, s.Name, timeCol)).Scan(&recent); err != nil {
			return f, false, err
		}
		f.PerDay = float64(recent) / 30
	}

	if key := retentionSettingRe.FindString(s.Bound); key != "" && s.Cleanup != "" {
		f.Retention = key
		n := GetSettingInt(tenantID, key)
		switch {
		case strings.HasSuffix(key, "_hours"):
			f.RetentionDays = int(math.Ceil(float64(n) / 24))
		case strings.HasSuffix(key, "_months"):
			f.RetentionDays = n * 30
		default:
			f.RetentionDays = n
		}
	}
	project := func(years int) int64 {
		days := float64(365 * years)
		if f.RetentionDays > 0 && float64(f.RetentionDays) < days {
			// A swept table levels off at its window.
			return int64(f.PerDay * float64(f.RetentionDays) * float64(f.AvgRowBytes))
		}
		return int64((float64(f.Rows) + f.PerDay*days) * float64(f.AvgRowBytes))
	}
	f.Year1, f.Year3, f.Year5 = project(1), project(3), project(5)
	return f, true, nil
}
