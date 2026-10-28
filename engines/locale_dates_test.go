package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// TestStage47142DayFirstDatesAreNeverSwapped is the India day/month
// ambiguity case by case: 03/02 is 3 February and 02/03 is 2 March, 12/11 is
// 12 November, a date that only reads month first is refused, and ISO dates
// and date-times pass untouched.
func TestStage47142DayFirstDatesAreNeverSwapped(t *testing.T) {
	cases := []struct{ in, want string }{
		{"03/02/2026", "2026-02-03"},
		{"02/03/2026", "2026-03-02"},
		{"12/11/2026", "2026-11-12"},
		{"11/12/2026", "2026-12-11"},
		{"3/2/2026", "2026-02-03"},
		{"03-02-2026", "2026-02-03"},
		{"03.02.2026", "2026-02-03"},
		{"03/02/26", "2026-02-03"},
		{"03/02/2026 14:30", "2026-02-03"},
		{"3-Feb-2026", "2026-02-03"},
		{"03 Feb 2026", "2026-02-03"},
		{"3 February 2026", "2026-02-03"},
		{"Feb 3, 2026", "2026-02-03"},
		{"2026/02/03", "2026-02-03"},
		{"29/02/2028", "2028-02-29"},
		// What the date picker and the API send stays exactly as it is.
		{"2026-02-03", "2026-02-03"},
		{"2026-02-03T10:15:00Z", "2026-02-03T10:15:00Z"},
		{"2026-02-03T10:15:00+05:30", "2026-02-03T10:15:00+05:30"},
		{"2026-02-03 10:15:00", "2026-02-03 10:15:00"},
		{"", ""},
	}
	for _, c := range cases {
		got, err := ParseDayFirstDate(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseDayFirstDate(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"02/13/2026", "13/13/2026", "31/02/2026", "29/02/2026", "2026-02-30", "2026-13-01", "tomorrow", "46000", "2026-10", "3rd Feb"} {
		if got, err := ParseDayFirstDate(bad); err == nil {
			t.Errorf("ParseDayFirstDate(%q) = %q, want it refused", bad, got)
		}
	}
	if _, err := ParseDayFirstDate("02/13/2026"); err == nil || !strings.Contains(err.Error(), "day first") {
		t.Errorf("a month-first date's refusal should say dates are read day first, got %v", err)
	}
}

// TestStage47142ImportReadsDatesDayFirst: through the real import (Budget
// periods), a typed
// 03/02/2026 is stored as 2026-02-03, an ISO date is stored unchanged and a
// month-first 02/13/2026 is refused with its row named.
func TestStage47142ImportReadsDatesDayFirst(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Budget' AND id LIKE 'N20D-DATE-%'")
	}
	cleanup()
	defer cleanup()

	fields, err := GetDocTypeMeta("default", "Budget")
	if err != nil {
		t.Fatal(err)
	}
	// Every other mandatory Budget field gets a value the doctype accepts.
	header := []string{"id", "code", "period_start", "period_end"}
	extra := []string{}
	for _, f := range fields {
		if !f.Mandatory || f.Fieldname == "id" || f.Fieldname == "code" || f.Fieldname == "period_start" || f.Fieldname == "period_end" {
			continue
		}
		v := "1100" // a seeded GL account; account_code is checked
		switch f.Fieldtype {
		case "Select":
			v = strings.TrimSpace(strings.Split(f.Options, ",")[0])
		case "Number":
			v = "1"
		case "Link":
			t.Skipf("Budget has a mandatory link %s; pick another doctype", f.Fieldname)
		}
		header = append(header, f.Fieldname)
		extra = append(extra, v)
	}
	row := func(id, from, to string) string {
		return strings.Join(append([]string{id, id, from, to}, extra...), ",") + "\n"
	}
	file := strings.Join(header, ",") + "\n" +
		row("N20D-DATE-1", "03/02/2026", "12/11/2026") +
		row("N20D-DATE-2", "2026-02-03", "2026-03-02") +
		row("N20D-DATE-3", "02/13/2026", "02/14/2026")
	res, err := BulkImportCSV("default", "Budget", strings.NewReader(file), "system", "", false)
	if err != nil {
		t.Fatalf("BulkImportCSV: %v", err)
	}
	if res.SuccessRows != 2 || res.FailedRows != 1 || len(res.Errors) == 0 || res.Errors[0].RowNumber != 4 {
		t.Fatalf("got %d imported, %d refused, errors %+v; want 2, 1 and row 4 named", res.SuccessRows, res.FailedRows, res.Errors)
	}
	if !strings.Contains(res.Errors[0].Message, "day first") {
		t.Errorf("refusal %q should say dates are read day first", res.Errors[0].Message)
	}
	for id, want := range map[string][2]string{"N20D-DATE-1": {"2026-02-03", "2026-11-12"}, "N20D-DATE-2": {"2026-02-03", "2026-03-02"}} {
		var from, to string
		if err := db.DB.QueryRow("SELECT data->>'period_start', data->>'period_end' FROM "+schema+".documents WHERE id = $1", id).Scan(&from, &to); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if from != want[0] || to != want[1] {
			t.Errorf("%s stored %s .. %s, want %s .. %s", id, from, to, want[0], want[1])
		}
	}
}

// TestStage47142aEachDateOrderReadsItsOwnForm: a typed numeric date is read in
// the client's order only - 03/02/2026 is 3 February day first, 2 March month
// first and refused year first; each order refuses an impossible date in its
// own words; ISO and month-name dates read the same in every order; the
// setting ships day first.
func TestStage47142aEachDateOrderReadsItsOwnForm(t *testing.T) {
	read := map[string][]struct{ in, want string }{
		DateOrderDMY: {{"03/02/2026", "2026-02-03"}, {"13/02/2026", "2026-02-13"}, {"3-2-26", "2026-02-03"}, {"03.02.2026 14:30", "2026-02-03"}},
		DateOrderMDY: {{"03/02/2026", "2026-03-02"}, {"02/13/2026", "2026-02-13"}, {"3-2-26", "2026-03-02"}, {"12/31/2026 09:00", "2026-12-31"}},
		DateOrderYMD: {{"2026/02/03", "2026-02-03"}, {"2026.2.3", "2026-02-03"}, {"2026/12/31 09:00", "2026-12-31"}},
	}
	refused := map[string][]string{
		DateOrderDMY: {"02/13/2026", "31/02/2026"},
		DateOrderMDY: {"13/02/2026", "02/31/2026", "02/29/2026"},
		DateOrderYMD: {"03/02/2026", "2026/02/30", "2026/13/01"},
	}
	words := map[string]string{DateOrderDMY: "day first", DateOrderMDY: "month first", DateOrderYMD: "year first"}
	for order, cases := range read {
		for _, c := range cases {
			if got, err := ParseDateInOrder(c.in, order); err != nil || got != c.want {
				t.Errorf("%s: ParseDateInOrder(%q) = %q, %v; want %q", order, c.in, got, err, c.want)
			}
		}
		// Every order: ISO, ISO date-time, month names, blank.
		for in, want := range map[string]string{"2026-02-03": "2026-02-03", "2026-02-03T10:15:00Z": "2026-02-03T10:15:00Z",
			"3 Feb 2026": "2026-02-03", "3-Feb-2026": "2026-02-03", "Feb 3, 2026": "2026-02-03", "2026/02/03": "2026-02-03", "": ""} {
			if got, err := ParseDateInOrder(in, order); err != nil || got != want {
				t.Errorf("%s: ParseDateInOrder(%q) = %q, %v; want %q", order, in, got, err, want)
			}
		}
		for _, bad := range append(refused[order], "2026-02-30", "tomorrow", "46000") {
			if got, err := ParseDateInOrder(bad, order); err == nil {
				t.Errorf("%s: ParseDateInOrder(%q) = %q, want it refused", order, bad, got)
			}
		}
		if _, err := ParseDateInOrder(refused[order][0], order); err == nil || !strings.Contains(err.Error(), words[order]) {
			t.Errorf("%s: refusing %q should say dates are read %s, got %v", order, refused[order][0], words[order], err)
		}
	}
	// An unknown order reads day first, the same as ParseDayFirstDate.
	if got, err := ParseDateInOrder("03/02/2026", "XYZ"); err != nil || got != "2026-02-03" {
		t.Errorf("unknown order read 03/02/2026 as %q, %v; want day first", got, err)
	}
	for _, d := range ListSettingDefinitions() {
		if d.Key == SettingKeyDateOrder {
			if d.Default != DateOrderDMY || d.Module != "Localization" || len(d.Options) != 3 {
				t.Errorf("date order setting = %+v; want Localization, 3 options, day first by default", d)
			}
			return
		}
	}
	t.Errorf("%s is not registered", SettingKeyDateOrder)
}
