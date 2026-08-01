package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
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
	var prefix, separator, resetFreq string
	var paddingWidth int
	var activeStatus, includeStore bool
	queryConfig := `SELECT prefix, separator, padding_width, reset_frequency, active_status, COALESCE(include_store, TRUE)
	                FROM prefix_configs WHERE doc_type = $1`
	err = tx.QueryRow(queryConfig, docType).Scan(&prefix, &separator, &paddingWidth, &resetFreq, &activeStatus, &includeStore)
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
		return "", &ValidationError{Code: "ADMINC-0030", Message: fmt.Sprintf("numbering configuration for %s is inactive", docType)}
	}

	// 2. Resolve which counter this number draws from. The bucket always
	// matches the segments the number will actually show (below): dropping the
	// store segment from the format while still counting per-store would hand
	// two stores the same visible number.
	periodBucket, periodSegment := sequencePeriod(resetFreq, financialYear, time.Now())
	counterStore := storeCode
	if !includeStore {
		counterStore = counterWildcard
	}

	// 3. Fetch or create counter for that bucket with row lock
	var currentVal int64
	queryCounter := `SELECT current_val FROM sequence_counters
	                 WHERE doc_type = $1 AND store_code = $2 AND financial_year = $3
	                 FOR UPDATE`
	err = tx.QueryRow(queryCounter, docType, counterStore, periodBucket).Scan(&currentVal)
	if err == sql.ErrNoRows {
		currentVal = 0
		insertCounter := `INSERT INTO sequence_counters (doc_type, store_code, financial_year, current_val)
		                  VALUES ($1, $2, $3, $4)`
		_, err = tx.Exec(insertCounter, docType, counterStore, periodBucket, currentVal)
		if err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

	// 4. Increment counter
	nextVal := currentVal + 1
	updateCounter := `UPDATE sequence_counters SET current_val = $1
	                  WHERE doc_type = $2 AND store_code = $3 AND financial_year = $4`
	_, err = tx.Exec(updateCounter, nextVal, docType, counterStore, periodBucket)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	// 5. Format sequence code
	formatStr := fmt.Sprintf("%%0%dd", paddingWidth)
	paddedNum := fmt.Sprintf(formatStr, nextVal)

	// Format: <Prefix><Separator>[<StoreCode><Separator>][<Period><Separator>]<PaddedNum>
	parts := []string{}
	if prefix != "" {
		parts = append(parts, prefix)
	}
	if includeStore && storeCode != "" {
		parts = append(parts, storeCode)
	}
	if periodSegment != "" {
		parts = append(parts, periodSegment)
	}
	parts = append(parts, paddedNum)

	return strings.Join(parts, separator), nil
}

// GenerateVariantCode constructs a variant/child identifier from the parent code and attribute values using a template format pattern
func GenerateVariantCode(tenantID string, parentCode string, pattern string, attributes map[string]string) string {
	if pattern == "" {
		// Fallback default concatenation: ParentCode-AttrVal1-AttrVal2
		var parts []string
		parts = append(parts, parentCode)
		for _, v := range attributes {
			if v != "" {
				parts = append(parts, v)
			}
		}
		return strings.Join(parts, "-")
	}

	result := pattern
	result = strings.ReplaceAll(result, "{Parent}", parentCode)
	for k, v := range attributes {
		result = strings.ReplaceAll(result, "{"+k+"}", v)
	}
	return result
}
