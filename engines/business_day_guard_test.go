package engines

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Stage 132.19: production runs on UTC, so a "today" taken from the server's
// clock, or a day boundary taken from the database's, is yesterday until 05:30
// IST. This keeps new code from bringing either back.
func TestNoServerClockDays(t *testing.T) {
	bad := []struct {
		re  *regexp.Regexp
		fix string
	}{
		{regexp.MustCompile(`time\.Now\(\)\.Format\("2006-01-02"\)`), `engines.BusinessToday()`},
		{regexp.MustCompile(`\bCURRENT_DATE\b`), `public.erp_today()`},
		{regexp.MustCompile(`(created_at|updated_at|posted_at)\s*(>=|>|<=|<)\s*\(?\$(\d+|%d)::date`), `public.erp_day_start($N::date) (and $N::date + 1 for the end)`},
		{regexp.MustCompile(`(created_at|updated_at|posted_at)\)?::date`), `public.erp_local_date(col)`},
	}
	var found []string
	for _, dir := range []string{".", filepath.Join("..", "internal", "server")} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") || filepath.Base(f) == "business_day.go" {
				continue
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			for i, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				for _, b := range bad {
					if b.re.MatchString(line) {
						found = append(found, f+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line)+"  -> use "+b.fix)
					}
				}
			}
		}
	}
	for _, f := range found {
		t.Error(f)
	}
}
