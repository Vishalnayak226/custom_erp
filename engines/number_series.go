package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Number series administration (Stage 60.1). GenerateSequence (numbering.go)
// issues the numbers; this file is what the Prefix Configurations screen reads
// and saves, and the one place a series' shape is validated.
//
// A generated number becomes the document id, and documents.id is unique
// across every record type, so the validation here is not cosmetic: two
// series that can print the same string mean the second save silently
// replaces the first record. The rules below keep every series' prefixes
// disjoint, and storeNumberSegment keeps two stores from printing the same
// store segment.

// StoreSeriesOverride is one store's own prefix/suffix for a series. A blank
// part falls back to the series' own.
type StoreSeriesOverride struct {
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
}

// NumberSeries is one prefix_configs row.
type NumberSeries struct {
	ID             string                         `json:"id"`
	DocType        string                         `json:"doc_type"`
	Prefix         string                         `json:"prefix"`
	Suffix         string                         `json:"suffix"`
	Separator      string                         `json:"separator"`
	PaddingWidth   int                            `json:"padding_width"`
	ResetFrequency string                         `json:"reset_frequency"`
	ActiveStatus   bool                           `json:"active_status"`
	IncludeStore   bool                           `json:"include_store"`
	StoreOverrides map[string]StoreSeriesOverride `json:"store_overrides"`
}

var validResetFrequency = map[string]bool{"ANNUAL": true, "MONTHLY": true, "NEVER": true}

// counterSeries mint values that are never a document id: barcode digits and
// GRN batch numbers (Stage 61.7).
var counterSeries = map[string]bool{"PIMBarcodeSeq": true, "PIMDatedBarcodeSeq": true, "GRNBatchSeq": true}

// seriesTextPattern is what a prefix, suffix or override may contain: letters,
// digits and the few joiners people use in document numbers. No spaces - a
// number is read aloud, typed into searches and printed on barcodes.
var seriesTextPattern = regexp.MustCompile(`^[A-Za-z0-9_.\-]{0,20}$`)

// ListNumberSeries returns every series of the tenant, ordered by record type.
func ListNumberSeries(tenantID string) ([]NumberSeries, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, doc_type, prefix, COALESCE(suffix, ''), COALESCE(separator, '/'), COALESCE(padding_width, 6),
		       COALESCE(reset_frequency, 'ANNUAL'), COALESCE(active_status, TRUE), COALESCE(include_store, TRUE),
		       COALESCE(store_overrides, '{}'::jsonb)::text
		  FROM %s.prefix_configs ORDER BY doc_type`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NumberSeries{}
	for rows.Next() {
		var s NumberSeries
		var overrides string
		if err := rows.Scan(&s.ID, &s.DocType, &s.Prefix, &s.Suffix, &s.Separator, &s.PaddingWidth,
			&s.ResetFrequency, &s.ActiveStatus, &s.IncludeStore, &overrides); err != nil {
			return nil, err
		}
		s.StoreOverrides = parseStoreOverrides(overrides)
		out = append(out, s)
	}
	return out, rows.Err()
}

func parseStoreOverrides(raw string) map[string]StoreSeriesOverride {
	out := map[string]StoreSeriesOverride{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// SaveNumberSeries validates and upserts one series. includeStore nil keeps
// the stored value (a caller that predates the field must not switch the
// store segment off), and overrides nil keeps the stored store overrides.
func SaveNumberSeries(tenantID string, s NumberSeries, includeStore *bool, overrides map[string]StoreSeriesOverride) error {
	s.DocType = strings.TrimSpace(s.DocType)
	s.Prefix = strings.TrimSpace(s.Prefix)
	s.Suffix = strings.TrimSpace(s.Suffix)
	s.ResetFrequency = strings.ToUpper(strings.TrimSpace(s.ResetFrequency))

	existing, err := ListNumberSeries(tenantID)
	if err != nil {
		return err
	}
	var current *NumberSeries
	for i := range existing {
		if existing[i].DocType == s.DocType {
			current = &existing[i]
		}
	}
	if overrides == nil {
		if current != nil {
			overrides = current.StoreOverrides
		} else {
			overrides = map[string]StoreSeriesOverride{}
		}
	}
	cleaned := map[string]StoreSeriesOverride{}
	for store, o := range overrides {
		store = strings.TrimSpace(store)
		o.Prefix, o.Suffix = strings.TrimSpace(o.Prefix), strings.TrimSpace(o.Suffix)
		if store == "" || (o.Prefix == "" && o.Suffix == "") {
			continue
		}
		cleaned[store] = o
	}

	if err := validateNumberSeries(s, current, cleaned, existing); err != nil {
		return err
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	overridesJSON, err := json.Marshal(cleaned)
	if err != nil {
		return err
	}
	include := true
	if current != nil {
		include = current.IncludeStore
	}
	if includeStore != nil {
		include = *includeStore
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.prefix_configs (doc_type, prefix, suffix, separator, padding_width, reset_frequency, active_status, include_store, store_overrides)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
		ON CONFLICT (doc_type) DO UPDATE SET
			prefix = EXCLUDED.prefix, suffix = EXCLUDED.suffix, separator = EXCLUDED.separator,
			padding_width = EXCLUDED.padding_width, reset_frequency = EXCLUDED.reset_frequency,
			active_status = EXCLUDED.active_status, include_store = EXCLUDED.include_store,
			store_overrides = EXCLUDED.store_overrides`, schema),
		s.DocType, s.Prefix, s.Suffix, s.Separator, s.PaddingWidth, s.ResetFrequency, s.ActiveStatus, include, string(overridesJSON))
	return err
}

func validateNumberSeries(s NumberSeries, current *NumberSeries, overrides map[string]StoreSeriesOverride, all []NumberSeries) error {
	bad := func(msg string) error { return &ValidationError{Code: "ADMINC-0311", SubFor: s.DocType, Message: msg} }
	if s.DocType == "" {
		return bad("choose the transaction this series numbers")
	}
	if !validResetFrequency[s.ResetFrequency] {
		return bad("reset must be ANNUAL, MONTHLY or NEVER")
	}
	if s.PaddingWidth < 1 || s.PaddingWidth > 12 {
		return bad("number length must be between 1 and 12 digits")
	}
	if len(s.Separator) > 3 || strings.ContainsAny(s.Separator, " \t") {
		return bad("separator must be at most 3 characters with no spaces")
	}
	if !seriesTextPattern.MatchString(s.Prefix) || !seriesTextPattern.MatchString(s.Suffix) {
		return bad("prefix and suffix may use only letters, digits, '-', '_' and '.', up to 20 characters")
	}
	// An empty prefix is kept only where it already is (the barcode counters,
	// which mint digits rather than document numbers). A document series with
	// no prefix would print the same string as any other prefix-less one.
	if s.Prefix == "" && (current == nil || current.Prefix != "") {
		return bad("a prefix is required")
	}
	for store, o := range overrides {
		if !seriesTextPattern.MatchString(o.Prefix) || !seriesTextPattern.MatchString(o.Suffix) {
			return bad(fmt.Sprintf("store %s: prefix and suffix may use only letters, digits, '-', '_' and '.', up to 20 characters", store))
		}
	}

	mine := seriesPrefixes(s.Prefix, overrides)
	for _, other := range all {
		// A counter's number is never a record id, so it cannot replace a
		// record - its prefix may repeat one a document series uses.
		if counterSeries[s.DocType] || counterSeries[other.DocType] {
			continue
		}
		if other.DocType == s.DocType {
			continue
		}
		for p := range seriesPrefixes(other.Prefix, other.StoreOverrides) {
			if mine[p] {
				return &ValidationError{Code: "ADMINC-0310", SubFor: s.DocType, Message: fmt.Sprintf(
					"prefix %q is already used by the %s series - two series with one prefix can print the same number, and the second save would replace the first record", p, other.DocType)}
			}
		}
	}
	return nil
}

// seriesPrefixes is every non-empty prefix a series can print, upper-cased so
// "po" and "PO" count as the same.
func seriesPrefixes(prefix string, overrides map[string]StoreSeriesOverride) map[string]bool {
	out := map[string]bool{}
	if p := strings.ToUpper(strings.TrimSpace(prefix)); p != "" {
		out[p] = true
	}
	for _, o := range overrides {
		if p := strings.ToUpper(strings.TrimSpace(o.Prefix)); p != "" {
			out[p] = true
		}
	}
	return out
}

// storeNumberSegment is what a number prints for a store: its Short Code when
// that is set and belongs to no other store (as a short code or a store code),
// otherwise the store code itself. The fallback is what keeps two stores from
// printing one segment - their counters are separate, so a shared segment
// would mean both issue .../00001.
func storeNumberSegment(q interface {
	QueryRow(string, ...interface{}) *sql.Row
}, storeCode string) string {
	var short string
	if err := q.QueryRow(`SELECT COALESCE(TRIM(data->>'short_code'), '') FROM documents
		WHERE doctype = 'Location' AND (id = $1 OR data->>'code' = $1) LIMIT 1`, storeCode).Scan(&short); err != nil || short == "" {
		return storeCode
	}
	var clash int
	if err := q.QueryRow(`SELECT COUNT(*) FROM documents
		WHERE doctype = 'Location' AND id <> $1 AND COALESCE(data->>'code', id) <> $1
		  AND (UPPER(TRIM(data->>'short_code')) = UPPER($2) OR UPPER(COALESCE(data->>'code', id)) = UPPER($2) OR UPPER(id) = UPPER($2))`,
		storeCode, short).Scan(&clash); err != nil || clash > 0 {
		return storeCode
	}
	return short
}
