package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
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

// StoreSeriesOverride is one store's own parts for a series. A blank part
// falls back to the series' own (Code: the store's Short Code). Code is the
// store segment (Stage 86.2); Prefix replaces the series prefix and is no
// longer offered on the form - only kept where a store already had one, so
// its numbers do not change.
type StoreSeriesOverride struct {
	Prefix string `json:"prefix,omitempty"`
	Code   string `json:"code,omitempty"`
	Suffix string `json:"suffix,omitempty"`
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
	YearFormat     string                         `json:"year_format"`
	YearPosition   string                         `json:"year_position"` // "" before the number, END at the end (115.1)
	// RecordCode (Stage 126.4): a master's code or a payslip id. It is drawn
	// with no store and no year (GenerateSequence(t, key, "", "")), so it
	// prints neither, and its Restart / Store only pick its counter. Set by
	// the server, never taken from a request.
	RecordCode bool `json:"record_code"`
	confirmed  bool // prefix_configs.confirmed_at is set (76.5)
}

// barcodeCounters mint barcode digits of a fixed shape: EAN needs exactly 9
// counter digits, the receipt-date barcode keeps the last 4, and both count
// in the no-store, no-year bucket (pim_barcode.go, barcode_policy.go).
var barcodeCounters = map[string]bool{"PIMBarcodeSeq": true, "PIMDatedBarcodeSeq": true}

// recordCodeSeries is every series key that numbers a record's code rather
// than a document: the masters (serverNumberedMasters and every doctype_meta
// Master, less the typed-code ones) and the payslip.
func recordCodeSeries(schema string) (map[string]bool, error) {
	out := map[string]bool{"Payslip": true}
	for d := range serverNumberedMasters {
		out[d] = true
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT name FROM %s.doctype_meta WHERE document_type = 'Master'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if !typedCodeMasters[name] {
			out[name] = true
		}
	}
	return out, rows.Err()
}

var validResetFrequency = map[string]bool{"ANNUAL": true, "MONTHLY": true, "NEVER": true}

// validYearFormats are formatPeriodSegment's formats; blank is 26-27.
var validYearFormats = map[string]bool{"": true, "FY_COMPACT": true, "FY_LONG": true, "START_YY": true, "START_YYYY": true}

// gstNumberMaxLength: an invoice, credit or debit note number may have at
// most 16 characters (CGST Rule 46(b)). The user, 2026-10-18: "Never exceed
// 16 char in India" - held for every document series, since any of them can
// end up on a tax document's reference.
const gstNumberMaxLength = 16

// otherCountryNumberMaxLength (Stage 76 D2): no published cap was found for
// the other home countries, so they get a product cap, without GST's
// character rule.
const otherCountryNumberMaxLength = 20

// seriesNumberRules is the length cap, and whether GST's character rule
// applies, for a tenant's home country (blank = India, the default).
func seriesNumberRules(country string) (maxLen int, gst bool) {
	if c := strings.ToUpper(strings.TrimSpace(country)); c == "" || c == "IN" {
		return gstNumberMaxLength, true
	}
	return otherCountryNumberMaxLength, false
}

// gstNumberPattern is what a document number may look like (the e-invoice
// IRP's rule, CGST Rule 46's characters): capital letters, digits, "/" and
// "-", not starting with 0, "/" or "-". Only the whole number's first
// character counts - the running number may start with 0 (TOJAY00001).
// The user, 2026-10-18: "Yes, that you can enforce."
var gstNumberPattern = regexp.MustCompile(`^[A-Z1-9][A-Z0-9/\-]*$`)

// validSeparators are the joiners GST allows; blank is none.
var validSeparators = map[string]bool{"": true, "/": true, "-": true}

// gstTextPattern is what a document series' prefix or suffix may contain:
// gstNumberPattern's characters ("/" too since Stage 121.2 - it decides
// where they may stand).
var gstTextPattern = regexp.MustCompile(`^[A-Z0-9/\-]{0,20}$`)

// seriesStoreCodePattern is what a store's own code in a series may be.
var seriesStoreCodePattern = regexp.MustCompile(`^[A-Z0-9/\-]{1,10}$`)

// seriesStore is a store as a number prints it by default.
type seriesStore struct {
	Code    string // the store code (the counter key, and the overrides' key)
	Segment string // its Short Code, else its store code (storeNumberSegment)
	Name    string // what a message calls it
	// Deleted stores keep their code taken but issue no new number, so their
	// sample number is not held against the GST rules (Stage 115.2: a deleted
	// store's code, invisible on the form, blocked every save).
	Deleted bool
}

// counterSeries mint values that are never a document id: barcode digits and
// GRN batch numbers (Stage 61.7).
var counterSeries = map[string]bool{"PIMBarcodeSeq": true, "PIMDatedBarcodeSeq": true, "GRNBatchSeq": true}

// seriesTextPattern is what a prefix, suffix or override may contain: letters,
// digits and the few joiners people use in document numbers. No spaces - a
// number is read aloud, typed into searches and printed on barcodes.
var seriesTextPattern = regexp.MustCompile(`^[A-Za-z0-9_.\-]{0,20}$`)

// documentTextPattern adds "/" for a document series (Stage 121.2, the user:
// "This should allow everything in all txn" - a PO suffix "/" was refused
// though the form passed it). A counter's barcode digits stay as they were.
var documentTextPattern = regexp.MustCompile(`^[A-Za-z0-9_./\-]{0,20}$`)

// ListNumberSeries returns every series of the tenant, ordered by record type.
func ListNumberSeries(tenantID string) ([]NumberSeries, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	records, err := recordCodeSeries(schema)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, doc_type, prefix, COALESCE(suffix, ''), COALESCE(separator, '/'), COALESCE(padding_width, 6),
		       COALESCE(reset_frequency, 'ANNUAL'), COALESCE(active_status, TRUE), COALESCE(include_store, TRUE),
		       COALESCE(store_overrides, '{}'::jsonb)::text, COALESCE(year_format, ''), COALESCE(year_position, ''),
		       confirmed_at IS NOT NULL
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
			&s.ResetFrequency, &s.ActiveStatus, &s.IncludeStore, &overrides, &s.YearFormat, &s.YearPosition, &s.confirmed); err != nil {
			return nil, err
		}
		s.StoreOverrides = parseStoreOverrides(overrides)
		s.RecordCode = records[s.DocType]
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
	s.YearFormat = strings.ToUpper(strings.TrimSpace(s.YearFormat))
	if s.YearFormat == "FY_SHORT" {
		s.YearFormat = "" // 26-27 is stored as blank, the format every older series has
	}

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
	// A document number is capital letters (gstNumberPattern), so what is
	// typed in lower case is saved in capitals. Counters keep what they have.
	norm := strings.TrimSpace
	if !counterSeries[s.DocType] {
		norm = func(v string) string { return strings.ToUpper(strings.TrimSpace(v)) }
		s.Prefix, s.Suffix = norm(s.Prefix), norm(s.Suffix)
	}
	s.YearPosition = strings.ToUpper(strings.TrimSpace(s.YearPosition))
	cleaned := map[string]StoreSeriesOverride{}
	for store, o := range overrides {
		store = strings.TrimSpace(store)
		// A store's code is always capitals (seriesStoreCodePattern) - a
		// counter's lower-case one was refused (126.4).
		o.Prefix, o.Code, o.Suffix = norm(o.Prefix), strings.ToUpper(strings.TrimSpace(o.Code)), norm(o.Suffix)
		if store == "" || (o.Prefix == "" && o.Code == "" && o.Suffix == "") {
			continue
		}
		cleaned[store] = o
	}

	schema, err := db.GetTenantSchema(tenantID)
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
	// Stage 126.4: a record code's Restart and Store only pick its counter,
	// so changing either restarted the count at 1 over codes already issued.
	// They stay as the series counts now; a master not yet confirmed takes
	// what its first record gives it (ensureMasterSeries: never, no store).
	records, err := recordCodeSeries(schema)
	if err != nil {
		return err
	}
	if s.RecordCode = records[s.DocType]; s.RecordCode {
		if current != nil && (current.confirmed || s.DocType == "Payslip") {
			s.ResetFrequency, include = current.ResetFrequency, current.IncludeStore
		} else {
			s.ResetFrequency, include = "NEVER", false
		}
	}
	s.IncludeStore = include
	stores, err := loadSeriesStores(schema)
	if err != nil {
		return err
	}

	if err := validateNumberSeries(s, current, cleaned, existing, stores, TenantPhoneCountry(tenantID)); err != nil {
		return err
	}

	overridesJSON, err := json.Marshal(cleaned)
	if err != nil {
		return err
	}
	// A saved series is confirmed (Stage 126.4, the user: "after typing the
	// values it is not showing"): a master's series set on the screen was
	// unconfirmed, so its first record replaced it with the suggestion.
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.prefix_configs (doc_type, prefix, suffix, separator, padding_width, reset_frequency, active_status, include_store, store_overrides, year_format, year_position, confirmed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, NOW())
		ON CONFLICT (doc_type) DO UPDATE SET
			prefix = EXCLUDED.prefix, suffix = EXCLUDED.suffix, separator = EXCLUDED.separator,
			padding_width = EXCLUDED.padding_width, reset_frequency = EXCLUDED.reset_frequency,
			active_status = EXCLUDED.active_status, include_store = EXCLUDED.include_store,
			store_overrides = EXCLUDED.store_overrides, year_format = EXCLUDED.year_format,
			year_position = EXCLUDED.year_position,
			confirmed_at = COALESCE(prefix_configs.confirmed_at, NOW())`, schema),
		s.DocType, s.Prefix, s.Suffix, s.Separator, s.PaddingWidth, s.ResetFrequency, s.ActiveStatus, include, string(overridesJSON), s.YearFormat, s.YearPosition)
	return err
}

// loadSeriesStores lists every store with the segment it prints by default -
// storeNumberSegment's rule: its Short Code unless another store already
// prints or is coded that, else its store code. Deleted stores are included:
// their numbers exist, so a code they printed is still taken.
func loadSeriesStores(schema string) ([]seriesStore, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'code', ''), id), COALESCE(TRIM(data->>'short_code'), ''),
			COALESCE(NULLIF(TRIM(data->>'name'), ''), NULLIF(data->>'code', ''), id), deleted_at IS NOT NULL
		FROM %s.documents WHERE doctype = 'Location' ORDER BY 1`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type loc struct {
		code, short, name string
		deleted           bool
	}
	var locs []loc
	for rows.Next() {
		var l loc
		if err := rows.Scan(&l.code, &l.short, &l.name, &l.deleted); err != nil {
			return nil, err
		}
		locs = append(locs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]seriesStore, 0, len(locs))
	for _, l := range locs {
		segment := l.code
		if l.short != "" {
			segment = l.short
			for _, other := range locs {
				if other.code != l.code && (strings.EqualFold(other.short, l.short) || strings.EqualFold(other.code, l.short)) {
					segment = l.code
					break
				}
			}
		}
		out = append(out, seriesStore{Code: l.code, Segment: segment, Name: l.name, Deleted: l.deleted})
	}
	return out, nil
}

// seriesSample is the first number a series prints at one store ("" = no
// store in the number, or no store yet).
type seriesSample struct{ Number, Store string }

// seriesSampleNumbers is the first number the series prints at every store -
// what the GST rules (characters, 16 at most) are held against. The period is
// today's; its length does not change from year to year. With no store yet,
// "HQ" stands in, as the form's preview shows.
func seriesSampleNumbers(s NumberSeries, overrides map[string]StoreSeriesOverride, stores []seriesStore) []seriesSample {
	now := time.Now()
	fy := documentFinancialYear(now)
	if s.RecordCode {
		// Drawn with no store and no year (126.4) - GenerateSequence's own
		// period for an empty financial year.
		_, p := sequencePeriod(s.ResetFrequency, "", now)
		if p != "" {
			p = formatPeriodSegment(s.YearFormat, s.ResetFrequency, "", now)
		}
		return []seriesSample{{Number: joinNumberParts(s.Prefix, "", p, fmt.Sprintf("%0*d", s.PaddingWidth, 1), s.Suffix, s.Separator, s.YearPosition)}}
	}
	_, period := sequencePeriod(s.ResetFrequency, fy, now)
	if period != "" {
		period = formatPeriodSegment(s.YearFormat, s.ResetFrequency, fy, now)
	}
	build := func(o StoreSeriesOverride, segment string) string {
		prefix, suffix := s.Prefix, s.Suffix
		if o.Prefix != "" {
			prefix = o.Prefix
		}
		if o.Suffix != "" {
			suffix = o.Suffix
		}
		return joinNumberParts(prefix, segment, period, fmt.Sprintf("%0*d", s.PaddingWidth, 1), suffix, s.Separator, s.YearPosition)
	}
	if !s.IncludeStore {
		return []seriesSample{{Number: build(StoreSeriesOverride{}, "")}}
	}
	out := make([]seriesSample, 0, len(stores))
	for _, st := range stores {
		if st.Deleted {
			continue
		}
		o := overrides[st.Code]
		segment := st.Segment
		if o.Code != "" {
			segment = o.Code
		}
		name := st.Name
		if name == "" {
			name = st.Code
		}
		out = append(out, seriesSample{Number: build(o, segment), Store: name})
	}
	if len(out) == 0 {
		return []seriesSample{{Number: build(StoreSeriesOverride{}, "HQ")}}
	}
	return out
}

// sameSeriesSamples reports whether two sets of sample numbers are the same
// numbers at the same stores.
func sameSeriesSamples(a, b []seriesSample) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func validateNumberSeries(s NumberSeries, current *NumberSeries, overrides map[string]StoreSeriesOverride, all []NumberSeries, stores []seriesStore, country ...string) error {
	home := ""
	if len(country) > 0 {
		home = country[0]
	}
	maxLen, gstRules := seriesNumberRules(home)
	// A record's code (a vendor's, an item's) is not a GST document number,
	// so GST's characters and 16 do not apply to it (126.4).
	if s.RecordCode {
		gstRules = false
	}
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
	// Stage 126.4: a barcode counter's shape is fixed - another length breaks
	// every EAN, another Restart or Store moves its counter and repeats
	// barcodes already printed.
	if barcodeCounters[s.DocType] && current != nil && (s.Prefix != current.Prefix || s.Suffix != current.Suffix ||
		s.Separator != current.Separator || s.PaddingWidth != current.PaddingWidth ||
		!strings.EqualFold(s.ResetFrequency, current.ResetFrequency) || s.IncludeStore != current.IncludeStore ||
		!reflect.DeepEqual(overrides, current.StoreOverrides)) {
		return bad("a barcode counter's digits are fixed - only its status can change")
	}
	document := !counterSeries[s.DocType]
	// A save that prints exactly the numbers the series prints now - a status
	// change on a series made before the 16-character rule - is not held to
	// that length (126.4: such a series could not be saved at all, even
	// unchanged). A character GST refuses is still named: a store's own code
	// fixes it.
	unchanged := current != nil && sameSeriesSamples(seriesSampleNumbers(s, overrides, stores), seriesSampleNumbers(*current, current.StoreOverrides, stores))
	if len(s.Separator) > 3 || strings.ContainsAny(s.Separator, " \t") {
		return bad("separator must be at most 3 characters with no spaces")
	}
	textPattern, textChars := seriesTextPattern, "letters, digits, '-', '_' and '.'"
	if document {
		textPattern, textChars = documentTextPattern, "letters, digits, '/', '-', '_' and '.'"
	}
	if !textPattern.MatchString(s.Prefix) || !textPattern.MatchString(s.Suffix) {
		return bad("prefix and suffix may use only " + textChars + ", up to 20 characters")
	}
	if document && gstRules && !validSeparators[s.Separator] {
		return bad("the separator may be /, - or none - GST allows no other character between the parts")
	}
	if document && gstRules && (!gstTextPattern.MatchString(s.Prefix) || !gstTextPattern.MatchString(s.Suffix)) {
		return bad("prefix and suffix may use only capital letters, digits, '/' and '-' - the characters GST allows in a document number")
	}
	// An empty prefix is kept only where it already is (the barcode counters,
	// which mint digits rather than document numbers). A document series with
	// no prefix would print the same string as any other prefix-less one.
	if s.Prefix == "" && (current == nil || current.Prefix != "") {
		return bad("a prefix is required")
	}
	for store, o := range overrides {
		if !textPattern.MatchString(o.Prefix) || !textPattern.MatchString(o.Suffix) {
			return bad(fmt.Sprintf("store %s: prefix and suffix may use only %s, up to 20 characters", store, textChars))
		}
		if document && gstRules && (!gstTextPattern.MatchString(o.Prefix) || !gstTextPattern.MatchString(o.Suffix)) {
			return bad(fmt.Sprintf("store %s: prefix and suffix may use only capital letters, digits, '/' and '-'", store))
		}
		if o.Code != "" && !seriesStoreCodePattern.MatchString(o.Code) {
			return bad(fmt.Sprintf("store %s: its code in the number may use only capital letters, digits, '/' and '-', up to 10 characters", store))
		}
	}
	if !validYearFormats[s.YearFormat] {
		return bad("choose how the year is shown: 26-27, 2627, 2026-27, 26 or 2026")
	}
	if s.YearPosition != "" && s.YearPosition != YearPositionEnd {
		return bad("choose where the year goes: before the number, or at the end")
	}

	// Two stores printing one code would both issue .../00001 - their counters
	// are separate - and the second save would replace the first record.
	if s.IncludeStore {
		printedBy := map[string]string{}
		for _, st := range stores {
			segment := st.Segment
			if o := overrides[st.Code]; o.Code != "" {
				segment = o.Code
			}
			key := strings.ToUpper(segment)
			if other, taken := printedBy[key]; taken {
				return bad(fmt.Sprintf("stores %s and %s would both print %q in the number - give one of them its own code", other, st.Code, segment))
			}
			printedBy[key] = st.Code
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

	if document && !s.RecordCode {
		at := func(store string) string {
			if store == "" {
				return ""
			}
			return " at store " + store
		}
		var longest seriesSample
		for _, n := range seriesSampleNumbers(s, overrides, stores) {
			if gstRules && !gstNumberPattern.MatchString(n.Number) {
				fix := "change the prefix"
				if n.Store != "" {
					fix = "give that store a Code in number"
				}
				return bad(fmt.Sprintf("the number%s would be %s - a GST document number may use only capital letters, digits, / and -, and may not start with 0, / or -. Change it: %s",
					at(n.Store), n.Number, fix))
			}
			if len(n.Number) > len(longest.Number) {
				longest = n
			}
		}
		if len(longest.Number) > maxLen && !unchanged {
			rule := "GST allows"
			if !gstRules {
				rule = "a document number here allows"
			}
			return bad(fmt.Sprintf("the number%s would be %s - %d characters, and %s at most %d. Shorten the prefix, show the year as 2627 or 26, use fewer digits or leave the separator blank",
				at(longest.Store), longest.Number, len(longest.Number), rule, maxLen))
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
