package engines

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Stage 47.14.2: a date typed into an import or sent to a Date field is read
// day first, the way an Indian client writes it, and stored as ISO
// (YYYY-MM-DD). 03/02/2026 is 3 February, never 2 March; a value that only
// makes sense month first (02/13/2026) is refused rather than swapped. ISO
// dates and ISO date-times - what the date picker and the API send - are kept
// exactly as they are.
//
// Stage 47.14.2a: the order is a per-client choice, Settings → Configuration
// → Localization → Date order: day first (the default), month first or year
// first. A typed numeric date is read in that order only - never guessed in
// another; ISO and month-name dates are read in every order.

// SettingKeyDateOrder is the client's date order; DateOrder* are its values.
const (
	SettingKeyDateOrder = "localization.date_order"
	DateOrderDMY        = "DMY"
	DateOrderMDY        = "MDY"
	DateOrderYMD        = "YMD"
)

// ClientDateOrder is the client's date order - day first unless set.
func ClientDateOrder(tenantID string) string {
	if o := GetSettingString(tenantID, SettingKeyDateOrder); o == DateOrderMDY || o == DateOrderYMD {
		return o
	}
	return DateOrderDMY
}

// ClientDateLayout is the Go layout a print shows a numeric date in, in the
// client's order: 02-01-2006, 01-02-2006 or 2006-01-02.
func ClientDateLayout(tenantID string) string {
	switch ClientDateOrder(tenantID) {
	case DateOrderMDY:
		return "01-02-2006"
	case DateOrderYMD:
		return "2006-01-02"
	}
	return "02-01-2006"
}

// dateOrderWords is how a refusal names each order: the order a numeric date
// is read in, and the forms to use instead.
var dateOrderWords = map[string][2]string{
	DateOrderDMY: {"day first (DD-MM-YYYY)", "DD-MM-YYYY or YYYY-MM-DD"},
	DateOrderMDY: {"month first (MM-DD-YYYY)", "MM-DD-YYYY or YYYY-MM-DD"},
	DateOrderYMD: {"year first (YYYY-MM-DD)", "YYYY-MM-DD"},
}

var (
	isoDatePrefix  = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(?:[T ].*)?$`)
	yearFirstSlash = regexp.MustCompile(`^(\d{4})[/.](\d{1,2})[/.](\d{1,2})(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?$`)
	// yearLastDate is NN/NN/YYYY: day-month or month-day by the order.
	yearLastDate = regexp.MustCompile(`^(\d{1,2})[/.\-](\d{1,2})[/.\-](\d{4}|\d{2})(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?$`)
)

// namedMonthLayouts are the spellings with a month name - unambiguous in
// either order.
var namedMonthLayouts = []string{
	"2-Jan-2006", "2 Jan 2006", "2-Jan-06", "2 Jan 06",
	"2-January-2006", "2 January 2006",
	"Jan 2, 2006", "January 2, 2006", "Jan 2 2006",
}

// errNotADate is the refusal: the value is none of the accepted forms.
type errNotADate struct{ reason string }

func (e errNotADate) Error() string { return e.reason }

// ParseDayFirstDate is ParseDateInOrder for a day-first client.
func ParseDayFirstDate(value string) (string, error) { return ParseDateInOrder(value, DateOrderDMY) }

// ParseDateInOrder returns value as an ISO date, reading a numeric NN/NN/YYYY
// date in order (a DateOrder* value; anything else reads day first). An ISO
// date or date-time comes back unchanged; every other accepted form comes
// back as YYYY-MM-DD. An empty value comes back empty. A year-first client
// has no NN/NN/YYYY form: it is refused, not guessed.
func ParseDateInOrder(value, order string) (string, error) {
	words, known := dateOrderWords[order]
	if !known {
		order, words = DateOrderDMY, dateOrderWords[DateOrderDMY]
	}
	v := strings.TrimSpace(value)
	if v == "" {
		return "", nil
	}
	if m := isoDatePrefix.FindStringSubmatch(v); m != nil {
		if _, ok := calendarDate(m[1], m[2], m[3]); !ok {
			return "", errNotADate{fmt.Sprintf("%s is not a calendar date", v)}
		}
		return v, nil
	}
	if m := yearFirstSlash.FindStringSubmatch(v); m != nil {
		if iso, ok := calendarDate(m[1], m[2], m[3]); ok {
			return iso, nil
		}
		return "", errNotADate{fmt.Sprintf("%s is not a calendar date", v)}
	}
	if m := yearLastDate.FindStringSubmatch(v); m != nil {
		if order != DateOrderYMD {
			year, month, day := m[3], m[2], m[1]
			if len(year) == 2 {
				year = "20" + year
			}
			if order == DateOrderMDY {
				month, day = m[1], m[2]
			}
			if iso, ok := calendarDate(year, month, day); ok {
				return iso, nil
			}
		}
		return "", errNotADate{fmt.Sprintf("%s is not a date - dates are read %s", v, words[0])}
	}
	for _, layout := range namedMonthLayouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t.Format("2006-01-02"), nil
		}
	}
	return "", errNotADate{fmt.Sprintf("%s is not a date - use %s", v, words[1])}
}

// calendarDate validates year/month/day text and returns it as ISO.
func calendarDate(y, m, d string) (string, bool) {
	year, err1 := strconv.Atoi(y)
	month, err2 := strconv.Atoi(m)
	day, err3 := strconv.Atoi(d)
	if err1 != nil || err2 != nil || err3 != nil || month < 1 || month > 12 || day < 1 || day > 31 {
		return "", false
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Day() != day || int(t.Month()) != month {
		return "", false // 31 February rolls over; refuse it
	}
	return t.Format("2006-01-02"), true
}
