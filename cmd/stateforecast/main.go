// Command stateforecast prints how much disk each client's logs, tokens, jobs
// and evidence will hold after one, three and five years (Stage 49.18.7).
//
// It only reads: the session it opens is read-only, so it is safe to run
// against production. Each row is measured from that client's own last 30
// days; a swept table levels off at its retention setting, evidence kept on
// purpose keeps growing.
//
//	DATABASE_URL=postgres://... go run ./cmd/stateforecast            # every client
//	DATABASE_URL=postgres://... go run ./cmd/stateforecast minn       # one client
package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"custom_erp/db"
	"custom_erp/engines"
)

func main() {
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is not set")
		os.Exit(2)
	}
	db.InitDB(readOnly(raw))

	tenants := os.Args[1:]
	if len(tenants) == 0 {
		lives, err := engines.ListTenantLifecycles()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, l := range lives {
			tenants = append(tenants, l.TenantID)
		}
	}
	for _, t := range tenants {
		rows, err := engines.ForecastStateGrowth(t)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", t, err)
			continue
		}
		fmt.Print(render(t, rows))
	}
}

// readOnly makes every transaction on the pool read-only, so the tool cannot
// write even by mistake.
func readOnly(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("options", strings.TrimSpace(q.Get("options")+" -c default_transaction_read_only=on"))
	u.RawQuery = q.Encode()
	return u.String()
}

func render(tenant string, rows []engines.StateForecast) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## %s\n\n| Table | Rows | Bytes/row | Per day | Bound | 1 year | 3 years | 5 years |\n|---|---:|---:|---:|---|---:|---:|---:|\n", tenant)
	var y1, y3, y5 int64
	for _, r := range rows {
		bound := "kept"
		if r.RetentionDays > 0 {
			bound = fmt.Sprintf("%d days (%s)", r.RetentionDays, r.Retention)
		} else if r.Retention != "" {
			bound = "hold (" + r.Retention + " = 0)"
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %.1f | %s | %s | %s | %s |\n", r.Table, r.Rows, r.AvgRowBytes, r.PerDay, bound, size(r.Year1), size(r.Year3), size(r.Year5))
		y1, y3, y5 = y1+r.Year1, y3+r.Year3, y5+r.Year5
	}
	fmt.Fprintf(&b, "| **total** | | | | | **%s** | **%s** | **%s** |\n", size(y1), size(y3), size(y5))
	return b.String()
}

func size(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
