package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// counterWildcard buckets a counter that deliberately has no store or no
// period dimension. It must be a value no real store code or financial year
// can take, because sequence_counters' primary key is
// (doc_type, store_code, financial_year) and a collision here would silently
// share one counter between two series that are meant to be independent.
const counterWildcard = "*"

// sequencePeriod resolves the counter bucket a number belongs to, and the
// period segment (if any) that appears in the number itself.
//
// reset_frequency was previously read by GenerateSequence and then ignored -
// the bucket was always whatever financialYear the caller happened to pass, so
// MONTHLY and NEVER were configurable but inert. They now mean what they say.
// The bucket and the visible segment are deliberately derived together: a
// series that resets on a period it does not display would re-issue the same
// number when that period rolls over, and since a generated number becomes the
// document id, that is a lost save rather than a cosmetic problem.
func sequencePeriod(resetFreq, financialYear string, now time.Time) (bucket, segment string) {
	switch strings.ToUpper(strings.TrimSpace(resetFreq)) {
	case "NEVER":
		// One continuous series for the life of the tenant.
		return counterWildcard, ""
	case "MONTHLY":
		// "26-27-04" rather than "26-2704" - the financial year already
		// carries a hyphen, so running the month straight onto it reads as one
		// mangled number instead of a year and a month.
		period := financialYear + "-" + now.Format("01")
		return period, period
	default: // ANNUAL, and any unrecognized value - the historical behavior.
		return financialYear, financialYear
	}
}

// financialYearPattern is the "26-27" every document series is handed.
var financialYearPattern = regexp.MustCompile(`^(\d{2})-(\d{2})$`)

// formatPeriodSegment prints a series' period in its year format (Stage
// 86.2). Only the printed segment changes - sequencePeriod's bucket stays the
// financial year, so a format changed mid-year carries the count on. A
// monthly START_* series prints the calendar year and month (2701 for
// January 2027), since the financial year's start year would read wrong for
// January to March. A period that is not "26-27" (a GRN batch date, an older
// caller's own key) prints as before.
func formatPeriodSegment(yearFormat, resetFreq, financialYear string, now time.Time) string {
	monthly := strings.EqualFold(strings.TrimSpace(resetFreq), "MONTHLY")
	mm := ""
	if monthly {
		mm = now.Format("01")
	}
	m := financialYearPattern.FindStringSubmatch(financialYear)
	if m == nil {
		if monthly {
			return financialYear + "-" + mm
		}
		return financialYear
	}
	start, end := m[1], m[2]
	switch yearFormat {
	case "FY_COMPACT": // 2627, 262704
		return start + end + mm
	case "FY_LONG": // 2026-27, 2026-27-04
		if monthly {
			return "20" + start + "-" + end + "-" + mm
		}
		return "20" + start + "-" + end
	case "START_YY": // 26, monthly 2604
		if monthly {
			return now.Format("0601")
		}
		return start
	case "START_YYYY": // 2026, monthly 202604
		if monthly {
			return now.Format("200601")
		}
		return "20" + start
	default: // 26-27, 26-27-04
		if monthly {
			return financialYear + "-" + mm
		}
		return financialYear
	}
}

// GenerateSequence generates the next document code using row-level locking
func GenerateSequence(tenantID, docType, storeCode, financialYear string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}

	// 1. Fetch prefix configuration for this document type
	var prefix, suffix, separator, resetFreq, overridesRaw, yearFormat, yearPosition string
	var paddingWidth int
	var activeStatus, includeStore bool
	queryConfig := `SELECT prefix, COALESCE(suffix, ''), separator, padding_width, reset_frequency, active_status,
	                       COALESCE(include_store, TRUE), COALESCE(store_overrides, '{}'::jsonb)::text, COALESCE(year_format, ''),
	                       COALESCE(year_position, '')
	                FROM prefix_configs WHERE doc_type = $1`
	err = tx.QueryRow(queryConfig, docType).Scan(&prefix, &suffix, &separator, &paddingWidth, &resetFreq, &activeStatus, &includeStore, &overridesRaw, &yearFormat, &yearPosition)
	if err == sql.ErrNoRows {
		// Use default fallbacks
		prefix = docType
		separator = "/"
		paddingWidth = 6
		resetFreq = "ANNUAL"
		activeStatus = true
		includeStore = true
	} else if err != nil {
		return "", err
	}

	if !activeStatus {
		// ADMINC-0030 (Stage 25.5): "Missing number series" - a
		// prefix_configs row explicitly deactivated is functionally "no
		// number series available for this doctype" (the no-row-at-all
		// case deliberately falls back to defaults just above rather than
		// erroring, so it isn't this scenario).
		// Stage 76.8: the message names the screen that fixes it.
		return "", &ValidationError{Code: "ADMINC-0030", Message: fmt.Sprintf("the %s number series is switched off - an administrator turns it on in Settings → Customize → Prefix Configs", docType)}
	}

	// 2. Resolve which counter this number draws from. The bucket always
	// matches the segments the number will actually show (below): dropping the
	// store segment from the format while still counting per-store would hand
	// two stores the same visible number.
	now := time.Now()
	periodBucket, periodSegment := sequencePeriod(resetFreq, financialYear, now)
	if periodSegment != "" {
		periodSegment = formatPeriodSegment(yearFormat, resetFreq, financialYear, now)
	}
	counterStore := storeCode
	if !includeStore {
		counterStore = counterWildcard
	}

	// Stage 60.1: a store prints its Short Code rather than its store code,
	// and may carry its own suffix for this series. Stage 86.2: its own code
	// for this series replaces only the store segment (user, 2026-10-18: "It
	// should only change store's prefix"). A store prefix saved before 86.2
	// still replaces the series prefix, so those numbers do not change. The
	// counter stays keyed by the store code, so renaming a code never
	// restarts a series.
	var storeOwnCode string
	if o, ok := parseStoreOverrides(overridesRaw)[storeCode]; ok && storeCode != "" {
		if o.Prefix != "" {
			prefix = o.Prefix
		}
		if o.Suffix != "" {
			suffix = o.Suffix
		}
		storeOwnCode = o.Code
	}
	storeSegment := storeCode
	if includeStore && storeCode != "" {
		storeSegment = storeOwnCode
		if storeSegment == "" {
			storeSegment = storeNumberSegment(tx, storeCode)
		}
	}

	// 3-4. Take the next number of that bucket in one statement (74.1.2): a
	// new bucket starts at 1, an existing one is incremented under its row
	// lock. Two first saves of a new store / year at the same moment used to
	// both try to insert the bucket, and one failed on the primary key.
	var nextVal int64
	err = tx.QueryRow(`INSERT INTO sequence_counters (doc_type, store_code, financial_year, current_val)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (doc_type, store_code, financial_year) DO UPDATE SET current_val = sequence_counters.current_val + 1
		RETURNING current_val`, docType, counterStore, periodBucket).Scan(&nextVal)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	// 5. Format sequence code
	formatStr := fmt.Sprintf("%%0%dd", paddingWidth)
	paddedNum := fmt.Sprintf(formatStr, nextVal)

	if !includeStore || storeCode == "" {
		storeSegment = ""
	}
	return joinNumberParts(prefix, storeSegment, periodSegment, paddedNum, suffix, separator, yearPosition), nil
}

// YearPositionEnd puts a series' year after the running number and suffix
// (Stage 115.1, user: "year in the end"); blank keeps it before the number.
const YearPositionEnd = "END"

// joinNumberParts is the one layout of a document number, shared by issuing
// (GenerateSequence) and the save-time checks (seriesSampleNumbers):
// <Prefix>/[<Store>/][<Period>/]<Number>[/<Suffix>], or with the year at the
// end <Prefix>/[<Store>/]<Number>[/<Suffix>][/<Period>]. Empty parts are
// left out.
func joinNumberParts(prefix, store, period, number, suffix, sep, yearPosition string) string {
	parts := []string{}
	for _, p := range []string{prefix, store} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	atEnd := strings.EqualFold(yearPosition, YearPositionEnd)
	if period != "" && !atEnd {
		parts = append(parts, period)
	}
	parts = append(parts, number)
	if suffix != "" {
		parts = append(parts, suffix)
	}
	if period != "" && atEnd {
		parts = append(parts, period)
	}
	return strings.Join(parts, sep)
}
