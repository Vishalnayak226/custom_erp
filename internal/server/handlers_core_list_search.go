package server

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 47.10 (ask 192, user 2026-11-15: "Build all recommended"): search on
// a very large master. The 1m-row acceptance run found the list endpoint's
// search a sequential scan (jsonb_each_text over every row, 23-36 s on a
// million Items). What was approved, and where each part lives:
//
//  1. An exact id / code / barcode match comes first, found through the
//     primary key and the data GIN index (masterExactMatchIDs).
//  2. migrations_stage47_10_master_search_trgm.sql puts a pg_trgm GIN index
//     on one expression over the masters' identity fields, when the host has
//     the extension. A search over fields the index covers adds that
//     expression as a pre-filter (masterSearchPrefilter); the per-field match
//     still decides which rows count, so the result is the same with or
//     without the index.
//  3. On a master over 100k rows, a search with no q_fields covers the
//     master's identity fields instead of every field (masterSearchIsBig).
//  4. A count over 10k matches is an estimate (cappedListCount).
//  5. Deep offsets stay as they are.

// masterSearchFields are the fields a search on a big master covers when the
// caller names none. Item includes its names here (unlike @identity, 147.2)
// because on the list screen a name is how most people look an item up.
var masterSearchFields = map[string][]string{
	"Item":     {"id", "code", "name", "item_name", "barcode", "parent_product_code", "family"},
	"Customer": {"id", "code", "name", "phone", "email", "pan", "gstin"},
	"Vendor":   {"id", "code", "name", "gstin", "contact_phone", "contact_email"},
}

// masterSearchIndexFields is every field the trigram index's expression
// joins, in its order. The migration writes the same expression by hand;
// TestStage47MasterSearchUsesTrigramIndex fails if the two drift apart.
var masterSearchIndexFields = []string{"id", "code", "name", "item_name", "barcode", "parent_product_code", "family",
	"phone", "email", "pan", "gstin", "contact_phone", "contact_email"}

// masterExactFields are the fields an exact match is looked for in.
var masterExactFields = []string{"id", "code", "barcode"}

const (
	masterSearchBigRows = engines.BigDoctypeRows // rows above which (3) applies
	// searchFieldSep joins the fields. A user cannot type it, so a search
	// term never spans two fields.
	searchFieldSepSQL = `E'\x1f'`
)

// listCountCap is the number of matches above which (4) applies. A var only
// so a test can lower it instead of writing 10k rows.
var listCountCap int64 = 10_000

var masterSearchIndexExpr = func() string {
	parts := make([]string, len(masterSearchIndexFields))
	for i, f := range masterSearchIndexFields {
		parts[i] = fmt.Sprintf("coalesce(data->>'%s', '')", f)
	}
	return "(" + strings.Join(parts, " || "+searchFieldSepSQL+" || ") + ")"
}()

// fieldsSearchClause matches the term ($termArg) in any of fields. The field
// names are bound, never spliced: they can come from q_fields.
func fieldsSearchClause(fields []string, termArg int, args *[]interface{}, argIndex *int) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = fmt.Sprintf("coalesce(data->>$%d, '')", *argIndex)
		*args = append(*args, f)
		*argIndex++
	}
	return fmt.Sprintf("(%s) ILIKE $%d", strings.Join(parts, " || "+searchFieldSepSQL+" || "), termArg)
}

// maxDesignNumberMatches caps the designs one item search expands into.
const maxDesignNumberMatches = 10

// itemDesignNumberClause (ask 272.1, user 2026-11-26: "Search with design or
// combination and show barcode and select and do bill"): the Design No on a
// tag and on Stock on Hand is the design's design_number, which lives on the
// design (ProductFamily), not on its combinations - so an Item search never
// matched it where it differs from the Design ID. This finds the designs whose
// number holds the term and matches their combinations by the design field
// they carry. Only a design field the search covers and the role may read is
// matched, as with any other field. Each arm is a containment test the data
// GIN index answers, so a big master's search stays index-led. "" when no
// design matches.
func itemDesignNumberClause(schema string, fields []string, hidden map[string]bool, term string, args *[]interface{}, argIndex *int) string {
	term = strings.TrimSpace(term)
	if utf8.RuneCountInString(term) < 2 {
		return ""
	}
	searched := map[string]bool{}
	for _, f := range fields {
		searched[f] = true
	}
	var designFields []string
	for _, f := range []string{"family", "parent_product_code"} {
		if !hidden[f] && (fields == nil || searched[f]) {
			designFields = append(designFields, f)
		}
	}
	if len(designFields) == 0 {
		return ""
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'code', '') FROM %s.documents
		WHERE doctype = 'ProductFamily' AND deleted_at IS NULL AND data->>'design_number' ILIKE $1
		ORDER BY lower(data->>'design_number') = lower($2) DESC, id LIMIT %d`, schema, maxDesignNumberMatches), "%"+term+"%", term)
	if err != nil {
		return "" // the search still runs on the item's own fields
	}
	defer rows.Close()
	var refs []string
	seen := map[string]bool{}
	for rows.Next() {
		var id, code string
		if rows.Scan(&id, &code) != nil {
			continue
		}
		for _, ref := range []string{id, code} {
			if ref != "" && !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	if rows.Err() != nil || len(refs) == 0 {
		return ""
	}
	var arms []string
	for _, ref := range refs {
		*args = append(*args, ref)
		for _, f := range designFields {
			arms = append(arms, fmt.Sprintf("data @> jsonb_build_object('%s', $%d::text)", f, *argIndex))
		}
		*argIndex++
	}
	return "(" + strings.Join(arms, " OR ") + ")"
}

// masterSearchPrefilter is the indexed expression for a search over fields,
// or "" when the index cannot serve it: not a big master, a field the
// expression lacks, or a term too short to have a trigram. The pre-filter
// only narrows - a row whose field holds the term always holds it in the
// joined text too.
func masterSearchPrefilter(doctype string, fields []string, term string, termArg int) string {
	if masterSearchFields[doctype] == nil || utf8.RuneCountInString(strings.TrimSpace(term)) < 3 {
		return ""
	}
	covered := map[string]bool{}
	for _, f := range masterSearchIndexFields {
		covered[f] = true
	}
	for _, f := range fields {
		if !covered[f] {
			return ""
		}
	}
	// doctype is a key of masterSearchFields, never the request's own text;
	// spelled out so the partial index's predicate is provable.
	return fmt.Sprintf("doctype = '%s' AND %s ILIKE $%d", doctype, masterSearchIndexExpr, termArg)
}

// masterSearchIsBig says whether (3) applies: the doctype is a big master.
var masterSearchIsBig = func(schema, doctype string) bool {
	return masterSearchFields[doctype] != nil && doctypeIsBig(schema, doctype)
}

// Ask 297.4 (user, 2026-11-26: "If User bulk upload 1Cr row also it should
// not fail ... each and every transaction after that also erp should not
// hang"): on 30 lakh bills a list search took 55 s and ended in a 500. The
// any-field match reads every row, and no index covers a transaction's
// fields. So a doctype over masterSearchBigRows that is not a master is
// searched by its number when the caller names no fields (or @identity): the
// record whose id is the term, as typed or in capitals, through the primary
// key. The response says so (X-Search-Scope: number) and the list's empty
// state says it too. Fields a caller names are still searched as before.
const searchScopeNumber = "number"

var txnSearchByNumber = func(schema, doctype string) bool {
	return masterSearchFields[doctype] == nil && doctypeIsBig(schema, doctype)
}

// doctypeIsBig says whether the doctype's planner estimate is over
// masterSearchBigRows. The estimate (engines.DoctypeRows, shared with POS >
// Sales History) is cached ten minutes; a failed EXPLAIN counts as small.
func doctypeIsBig(schema, doctype string) bool {
	return engines.DoctypeRows(schema, doctype) > masterSearchBigRows
}

// explainRows is the planner's row estimate for query.
func explainRows(query string, args ...interface{}) (int64, error) {
	return engines.PlannerRows(context.Background(), query, args...)
}

// masterExactMatchIDs lists the records (at most 20) whose id, code or
// barcode is the whole term, among those where (the list's filters, without
// the search) allows. They are only pinned in the page query's ORDER BY,
// whose own where still includes the search - so pinning never adds a row,
// and the search is not run twice. fields is the search's field set (nil:
// every field the role can read).
func masterExactMatchIDs(schema, doctype, where string, args []interface{}, fields []string, hidden map[string]bool, term string) ([]string, error) {
	term = strings.TrimSpace(term)
	if masterSearchFields[doctype] == nil || term == "" {
		return nil, nil
	}
	in := map[string]bool{}
	for _, f := range fields {
		in[f] = true
	}
	argIndex := len(args) + 1
	args = append(append([]interface{}(nil), args...), term)
	var match []string
	for _, f := range masterExactFields {
		if hidden[f] || (fields != nil && !in[f]) {
			continue
		}
		if f == "id" {
			match = append(match, fmt.Sprintf("id = $%d", argIndex))
		} else {
			match = append(match, fmt.Sprintf("data @> jsonb_build_object('%s', $%d::text)", f, argIndex))
		}
	}
	if len(match) == 0 {
		return nil, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf("SELECT id FROM %s.documents%s AND (%s) ORDER BY id LIMIT 20", schema, where, strings.Join(match, " OR ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// cappedListCount counts at most listCountCap+1 matches. Over the cap it
// returns an estimate (never below the cap) and estimated=true, so a common
// word on a million rows answers as "about 200,000" instead of counting
// every row. The estimate is the planner's from estimateWhere; when that
// is at or below the cap - which the count just proved wrong, as it is for
// a text search the planner cannot size - it is a page sample instead.
func cappedListCount(schema, where string, args []interface{}, estimateWhere string, estimateArgs []interface{}) (total int64, estimated bool, err error) {
	err = db.DB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM (SELECT 1 FROM %s.documents%s LIMIT %d) capped", schema, where, listCountCap+1), args...).Scan(&total)
	if err != nil || total <= listCountCap {
		return total, false, err
	}
	est, estErr := explainRows(fmt.Sprintf("SELECT 1 FROM %s.documents%s", schema, estimateWhere), estimateArgs...)
	if estErr != nil || est <= listCountCap {
		est = sampledListCount(schema, where, args)
	}
	if est <= listCountCap {
		est = listCountCap + 1
	}
	return est, true, nil
}

// listSampleRows is about how many rows sampledListCount reads.
const listSampleRows = 50_000

// sampledListCount estimates the matches from a random sample of the
// table's pages, sized to read about listSampleRows rows: about 5% of a
// million-row table. A table that small is counted in full instead. 0 when
// neither can be taken.
func sampledListCount(schema, where string, args []interface{}) int64 {
	var rows float64
	if err := db.DB.QueryRow(`SELECT reltuples FROM pg_class WHERE oid = to_regclass($1)`, schema+".documents").Scan(&rows); err != nil {
		return 0
	}
	sample, scale := "", 1.0
	if pct := 100 * listSampleRows / rows; rows > 0 && pct < 100 {
		sample, scale = fmt.Sprintf(" TABLESAMPLE SYSTEM (%g)", pct), 100/pct
	}
	var hits float64
	if err := db.DB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s.documents%s%s", schema, sample, where), args...).Scan(&hits); err != nil {
		return 0
	}
	return int64(hits * scale)
}
