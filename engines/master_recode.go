package engines

import (
	"bytes"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Old long codes cleaned once (Stage 93.11).
//
// The user, 2026-10-18, on Department/HQ/2026/000001 and
// LegalEntity/HQ/2026/000001: "Clean them and ask to set them once. Then it
// should not change code ... Not good to ask code always. But it should
// suggest and Mandatory to user." Decided the same day: a guided step on each
// master's list - the code format asked once (suggested), the old -> new list
// shown, and on confirm every record still on a long code renumbered.
//
// A record's code is its id, and other records point at the id - a location's
// legal entity, a journal voucher's cost center, the GL's dimensions, stock
// rows keyed on the store, users' home stores, number counters. So a recode
// rewrites the id everywhere it stands as a whole value, in one transaction:
// every text column equal to it, every JSON string equal to it (keys too),
// every text array holding it. Two kinds of table keep the old code, because
// rewriting them would falsify history: the signed audit tables (a signature
// over the row) and append-only ledgers. master_code_changes records each
// old -> new pair, and ResolveLocationReference reads it, so a till or an
// integration still sending an old store code lands on the new one.

// NewMasterCodeFormat is a requested code format (blank prefix / zero length
// = keep the suggestion), upper-cased the way the New form sends it.
func NewMasterCodeFormat(prefix string, length int) *masterCodeFormat {
	return &masterCodeFormat{Prefix: strings.ToUpper(strings.TrimSpace(prefix)), Length: length}
}

// guidedRecodeMasters are the masters 93.11 cleaned first. Only the server
// ever numbered them, so any "/" in one of their codes is the old shape.
var guidedRecodeMasters = map[string]bool{"LegalEntity": true, "Location": true, "CostCenter": true, "Department": true}

// isRecodeMaster: Stage 76.6 (D1, "Re-code everything") widens the clean-up
// to every master the server numbers (76.5) - not the typed-code ones, whose
// code is a currency, a unit or an HSN number and was never long.
func isRecodeMaster(tenantID, doctype string) bool {
	if guidedRecodeMasters[doctype] || IsServerNumberedMaster(doctype) {
		return true
	}
	if IsTypedCodeMaster(doctype) {
		return false
	}
	ok, _ := IsMasterDoctype(tenantID, doctype)
	return ok
}

// recodeWarnings (76.6): codes that are known outside the ERP, which a
// renumber cannot reach. The admin acknowledges it before Apply runs.
var recodeWarnings = map[string]string{
	"Item": "Item codes may be printed on stickers and barcodes, or be the SKU a marketplace or sales channel knows. Those copies outside the ERP keep the old code: reprint the stickers and update the channel listings after renumbering. Searching the old code still finds the item.",
}

// recodeBackupMaxAge (76.6): a renumber rewrites a key across the whole
// client in one go, so it runs only with a backup from the last day to go
// back to.
const recodeBackupMaxAge = 24 * time.Hour

// MasterRecodeBackup is the backup check the preview shows and Apply enforces.
type MasterRecodeBackup struct {
	Required bool       `json:"required"`
	OK       bool       `json:"ok"`
	Newest   *time.Time `json:"newest,omitempty"`
	Note     string     `json:"note"`
}

// recodeBackupStatus reads the newest successful backup that covers this
// client - the nightly whole-database one, or an export of this client's
// schema (public.backup_runs, Stage 56.9). Production always needs one; a
// server that is not production and has never recorded a backup (a
// developer's machine) is not held to it - the backup-freshness alert's rule.
func recodeBackupStatus(schema string) MasterRecodeBackup {
	var b MasterRecodeBackup
	var newest sql.NullTime
	var runs int
	err := db.DB.QueryRow(`SELECT MAX(finished_at) FILTER (WHERE outcome = 'Success' AND (scope = 'database' OR tenant_schema = $1)), COUNT(*)
		FROM public.backup_runs`, schema).Scan(&newest, &runs)
	b.Required = IsProductionEnv() || runs > 0
	const take = " This runs only with a backup from the last 24 hours. The nightly backup runs at 02:00; if it failed, the operator can run deploy/backup.sh now."
	switch {
	case !b.Required:
		b.OK, b.Note = true, "No backups are recorded on this server and it is not production, so no backup is needed first."
	case err != nil:
		b.Note = "The backup record could not be read." + take
	case !newest.Valid:
		b.Note = "No successful backup is recorded." + take
	default:
		b.Newest = &newest.Time
		hours := time.Since(newest.Time).Hours()
		b.OK = time.Since(newest.Time) <= recodeBackupMaxAge
		if b.OK {
			b.Note = fmt.Sprintf("Newest backup: %.0f hour(s) ago.", hours)
		} else {
			b.Note = fmt.Sprintf("The newest backup is %.0f hours old.", hours) + take
		}
	}
	return b
}

// MasterRecodeRow is one record's old and new code.
type MasterRecodeRow struct {
	Old  string `json:"old"`
	New  string `json:"new"`
	Name string `json:"name"`
}

// MasterRecodePlan is what the guided step shows, and what it did.
type MasterRecodePlan struct {
	DocType    string            `json:"doc_type"`
	Prefix     string            `json:"prefix"`
	Length     int               `json:"length"`
	Configured bool              `json:"configured"` // the format is already set - not asked again
	Rows       []MasterRecodeRow `json:"rows"`
	References int               `json:"references"` // other records that point at these codes
	// 76.6: what Apply needs first, what it cannot reach, and (asked for
	// separately - it reads every table) each column it rewrites.
	Backup  MasterRecodeBackup  `json:"backup"`
	Warning string              `json:"warning,omitempty"`
	Touches []MasterRecodeTouch `json:"touches,omitempty"`
}

// MasterRecodeTouch is one column the clean-up rewrites and how many rows.
type MasterRecodeTouch struct {
	Table  string `json:"table"`
	Column string `json:"column"`
	Rows   int    `json:"rows"`
}

// longCodeMasters are a master's live records still on a long code. A short
// code is a prefix and digits (masterPrefixPattern), so a "/" marks the old
// <DocType>/<store>/<year>/<n> shape.
func longCodeMasters(schema, doctype string) ([]MasterRecodeRow, error) {
	match, args := "strpos(id, '/') > 0", []interface{}{doctype}
	if !guidedRecodeMasters[doctype] {
		// 76.6: any other master can hold a code the user typed with a "/"
		// in it - an item SKU like KURTA/RED/M is not an old code. Only the
		// shape GenerateSequence gave a master with no series of its own is:
		// "<DocType>/...", or "<its old series prefix>/...".
		var prefix, sep string
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(prefix, ''), COALESCE(separator, '') FROM %s.prefix_configs WHERE doc_type = $1`, schema), doctype).Scan(&prefix, &sep)
		if sep != "/" || prefix == "" {
			prefix = doctype
		}
		match, args = "(strpos(id, $2) = 1 OR strpos(id, $3) = 1)", append(args, doctype+"/", prefix+"/")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'name', '') FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND %s
		ORDER BY created_at, id`, schema, match), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MasterRecodeRow
	for rows.Next() {
		var r MasterRecodeRow
		if err := rows.Scan(&r.Old, &r.Name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PlanMasterRecode previews a clean-up: the format (confirmed, else the one
// asked for, else the suggestion) and the code each long-coded record would
// get. Nothing is written.
func PlanMasterRecode(tenantID, doctype string, req *masterCodeFormat) (MasterRecodePlan, error) {
	plan := MasterRecodePlan{DocType: doctype, Warning: recodeWarnings[doctype]}
	if !isRecodeMaster(tenantID, doctype) {
		return plan, &ValidationError{Code: "ADMINC-0311", SubFor: doctype, Message: fmt.Sprintf("%s codes are not cleaned here", doctype)}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return plan, err
	}
	plan.Backup = recodeBackupStatus(schema)
	series, err := GetMasterCodeSeries(tenantID, doctype)
	if err != nil {
		return plan, err
	}
	plan.Configured, plan.Prefix, plan.Length = series.Configured, series.Prefix, series.Length
	if !series.Configured && req != nil {
		if req.Prefix != "" {
			plan.Prefix = req.Prefix
		}
		if req.Length != 0 {
			plan.Length = req.Length
		}
		if !masterPrefixPattern.MatchString(plan.Prefix) {
			return plan, &ValidationError{Code: "ADMINC-0311", SubFor: doctype, Message: "the code prefix must be 1 to 6 letters or digits, starting with a letter - for example D"}
		}
		if plan.Length < masterCodeMinLength || plan.Length > masterCodeMaxLength {
			return plan, &ValidationError{Code: "ADMINC-0311", SubFor: doctype, Message: fmt.Sprintf("the number length must be %d to %d digits", masterCodeMinLength, masterCodeMaxLength)}
		}
	}
	if plan.Rows, err = longCodeMasters(schema, doctype); err != nil || len(plan.Rows) == 0 {
		return plan, err
	}

	var current int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT current_val FROM %s.sequence_counters
		WHERE doc_type = $1 AND store_code = $2 AND financial_year = $2`, schema), doctype, counterWildcard).Scan(&current)
	if series.Configured && !strings.HasPrefix(series.Next, plan.Prefix) {
		current = 0
	}
	planned := map[string]bool{}
	olds := make([]string, len(plan.Rows))
	for i := range plan.Rows {
		olds[i] = plan.Rows[i].Old
		for {
			current++
			code := fmt.Sprintf("%s%0*d", plan.Prefix, plan.Length, current)
			var used bool
			if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE id = $1)`, schema), code).Scan(&used); err != nil {
				return plan, err
			}
			if !used && !planned[code] {
				planned[code] = true
				plan.Rows[i].New = code
				break
			}
		}
	}
	quoted := quotedJSONStrings(olds)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents
		WHERE deleted_at IS NULL AND NOT (id = ANY($1)) AND EXISTS (SELECT 1 FROM unnest($2::text[]) q WHERE strpos(data::text, q) > 0)`, schema),
		pq.Array(olds), pq.Array(quoted)).Scan(&plan.References)
	return plan, nil
}

// PlanMasterRecodeWithTouches is the preview plus every column the renumber
// would rewrite, with row counts (76.6) - one read of each table, so it is
// asked for on its own rather than on every list open.
func PlanMasterRecodeWithTouches(tenantID, doctype string, req *masterCodeFormat) (MasterRecodePlan, error) {
	plan, err := PlanMasterRecode(tenantID, doctype, req)
	if err != nil || len(plan.Rows) == 0 {
		return plan, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return plan, err
	}
	olds := make([]string, len(plan.Rows))
	for i, r := range plan.Rows {
		olds[i] = r.Old
	}
	plan.Touches, err = countRecodeTouches(schema, doctype, olds)
	return plan, err
}

// ApplyMasterRecode confirms the format (asked once) and renumbers every
// record still on a long code, rewriting each reference, in one transaction.
// 76.6: only with a backup from the last 24 hours, and - for a master whose
// codes are known outside the ERP - once the admin acknowledged that.
func ApplyMasterRecode(tenantID, doctype string, req *masterCodeFormat, ackWarning bool, userID string) (MasterRecodePlan, error) {
	plan, err := PlanMasterRecode(tenantID, doctype, req)
	if err != nil || len(plan.Rows) == 0 {
		return plan, err
	}
	if !plan.Backup.OK {
		return plan, &ValidationError{Code: "MASTER-0990", SubFor: doctype, Message: plan.Backup.Note}
	}
	if plan.Warning != "" && !ackWarning {
		return plan, &ValidationError{Code: "MASTER-0991", SubFor: doctype, Message: plan.Warning}
	}
	if err := ensureMasterSeries(tenantID, doctype, &masterCodeFormat{Prefix: plan.Prefix, Length: plan.Length}); err != nil {
		return plan, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return plan, err
	}
	// The numbers are drawn for real now (the preview only looked); a number
	// someone used in between is skipped, exactly as a new record's would be.
	for i := range plan.Rows {
		plan.Rows[i].New = ""
		for attempt := 0; attempt < 50 && plan.Rows[i].New == ""; attempt++ {
			code, err := GenerateSequence(tenantID, doctype, "", "")
			if err != nil {
				return plan, err
			}
			var used bool
			if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE id = $1)`, schema), code).Scan(&used); err != nil {
				return plan, err
			}
			if !used {
				plan.Rows[i].New = code
			}
		}
		if plan.Rows[i].New == "" {
			return plan, fmt.Errorf("could not find a free %s number", doctype)
		}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return plan, err
	}
	defer tx.Rollback()
	if err := recodeInSchemaTx(tx, schema, doctype, plan.Rows, userID); err != nil {
		return plan, fmt.Errorf("nothing was changed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		return plan, err
	}
	InvalidateLiveUserStatesForTenant(tenantID) // home stores and departments read from users
	changes := make([]string, len(plan.Rows))
	for i, r := range plan.Rows {
		changes[i] = r.Old + " -> " + r.New
	}
	LogAuditEvent(tenantID, userID, "MASTER_CODES_CLEANED", "Success", fmt.Sprintf("%s codes cleaned: %s", doctype, strings.Join(changes, ", ")))
	return plan, nil
}

// quotedJSONStrings are the values as JSON writes them ("Location/HQ/...",
// quotes included), so a match is a whole JSON string - never a substring of
// a longer one.
func quotedJSONStrings(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(v)
		out[i] = strings.TrimSpace(b.String())
	}
	return out
}

// recodeInSchemaTx rewrites each old code to its new one across the schema.
func recodeInSchemaTx(tx *sql.Tx, schema, doctype string, rows []MasterRecodeRow, userID string) error {
	olds, news := make([]string, len(rows)), make([]string, len(rows))
	for i, r := range rows {
		olds[i], news[i] = r.Old, r.New
	}
	qOlds, qNews := quotedJSONStrings(olds), quotedJSONStrings(news)
	sch := pq.QuoteIdentifier(schema)

	// The records themselves: id and code.
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents d SET id = m.new, data = jsonb_set(d.data, '{code}', to_jsonb(m.new))
		FROM unnest($1::text[], $2::text[]) AS m(old, new) WHERE d.id = m.old AND d.doctype = $3`, sch),
		pq.Array(olds), pq.Array(news), doctype); err != nil {
		return fmt.Errorf("renumber the records: %v", err)
	}

	targets, err := recodeTargets(tx, schema)
	if err != nil {
		return err
	}
	for _, c := range targets {
		t, cn := sch+"."+pq.QuoteIdentifier(c.table), pq.QuoteIdentifier(c.name)
		if c.kind == "jsonb" {
			// One nested replace() per code: in chunks, so a few thousand
			// items do not build one expression too deep to plan.
			for lo := 0; lo < len(qOlds); lo += recodeJSONChunk {
				hi := min(lo+recodeJSONChunk, len(qOlds))
				expr := cn + "::text"
				args := []interface{}{pq.Array(qOlds[lo:hi])}
				for i := lo; i < hi; i++ {
					args = append(args, qOlds[i], qNews[i])
					expr = fmt.Sprintf("replace(%s, $%d, $%d)", expr, len(args)-1, len(args))
				}
				q := fmt.Sprintf(`UPDATE %s SET %s = (%s)::jsonb WHERE EXISTS (SELECT 1 FROM unnest($1::text[]) o WHERE strpos(%s::text, o) > 0)`, t, cn, expr, cn)
				if _, err := tx.Exec(q, args...); err != nil {
					return fmt.Errorf("%s.%s: %v", c.table, c.name, err)
				}
			}
			continue
		}
		var q string
		if c.kind == "ARRAY" {
			q = fmt.Sprintf(`UPDATE %s x SET %s = (SELECT array_agg(COALESCE(m.new, u.v) ORDER BY u.n)
				FROM unnest(x.%s::text[]) WITH ORDINALITY AS u(v, n) LEFT JOIN unnest($1::text[], $2::text[]) AS m(old, new) ON m.old = u.v)
				WHERE x.%s::text[] && $1::text[]`, t, cn, cn, cn)
		} else {
			q = fmt.Sprintf(`UPDATE %s x SET %s = m.new FROM unnest($1::text[], $2::text[]) AS m(old, new) WHERE x.%s = m.old`, t, cn, cn)
		}
		if _, err := tx.Exec(q, pq.Array(olds), pq.Array(news)); err != nil {
			return fmt.Errorf("%s.%s: %v", c.table, c.name, err)
		}
	}

	for _, r := range rows {
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.master_code_changes (doc_type, old_code, new_code, changed_by) VALUES ($1, $2, $3, $4)
			ON CONFLICT (doc_type, old_code) DO UPDATE SET new_code = EXCLUDED.new_code, changed_by = EXCLUDED.changed_by, changed_at = NOW()`, sch),
			doctype, r.Old, r.New, userID); err != nil {
			return fmt.Errorf("record the change: %v", err)
		}
	}
	return nil
}

// recodeJSONChunk is how many codes one JSON rewrite statement carries.
const recodeJSONChunk = 200

// recodeTarget is one column a code can stand in as a whole value.
type recodeTarget struct{ table, name, kind string }

// recodeTargets are the columns a renumber rewrites: every text, JSON and
// text-array column of the schema, except in the tables that keep the old
// code - signed ones (a signature over the row), append-only ones (a trigger
// refuses updates) and the change record itself - and the documents table's
// own key columns (its id is renumbered on its own, by doctype).
func recodeTargets(q queryer, schema string) ([]recodeTarget, error) {
	skip := map[string]bool{"master_code_changes": true}
	keep, err := q.Query(`
		SELECT DISTINCT c.table_name FROM information_schema.columns c
		 WHERE c.table_schema = $1 AND c.column_name = 'signature'
		UNION
		SELECT DISTINCT cl.relname FROM pg_trigger tg
		  JOIN pg_class cl ON cl.oid = tg.tgrelid JOIN pg_namespace ns ON ns.oid = cl.relnamespace
		 WHERE ns.nspname = $1 AND NOT tg.tgisinternal
		   AND (tg.tgname ILIKE '%append_only%' OR tg.tgname ILIKE '%immutable%')`, schema)
	if err != nil {
		return nil, err
	}
	for keep.Next() {
		var t string
		if err := keep.Scan(&t); err != nil {
			keep.Close()
			return nil, err
		}
		skip[t] = true
	}
	keep.Close()

	cols, err := q.Query(`
		SELECT c.table_name, c.column_name, c.data_type
		  FROM information_schema.columns c
		  JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name AND t.table_type = 'BASE TABLE'
		 WHERE c.table_schema = $1 AND c.is_generated = 'NEVER'
		   AND (c.data_type IN ('text', 'character varying', 'jsonb') OR (c.data_type = 'ARRAY' AND c.udt_name IN ('_text', '_varchar')))
		 ORDER BY c.table_name, c.column_name`, schema)
	if err != nil {
		return nil, err
	}
	defer cols.Close()
	var targets []recodeTarget
	for cols.Next() {
		var c recodeTarget
		if err := cols.Scan(&c.table, &c.name, &c.kind); err != nil {
			return nil, err
		}
		if skip[c.table] || (c.table == "documents" && c.name != "data") {
			continue
		}
		targets = append(targets, c)
	}
	return targets, cols.Err()
}

// countRecodeTouches counts, per column, the rows a renumber of these codes
// would rewrite - the same matches recodeInSchemaTx makes - with one read of
// each table. The records themselves come first.
func countRecodeTouches(schema, doctype string, olds []string) ([]MasterRecodeTouch, error) {
	targets, err := recodeTargets(db.DB, schema)
	if err != nil {
		return nil, err
	}
	qOlds := quotedJSONStrings(olds)
	out := []MasterRecodeTouch{{Table: "documents", Column: "id (" + doctype + " records)", Rows: len(olds)}}
	for start := 0; start < len(targets); {
		end := start
		for end < len(targets) && targets[end].table == targets[start].table {
			end++
		}
		table := targets[start].table
		var args []interface{}
		param := func(v interface{}) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		exprs := make([]string, 0, end-start)
		for _, c := range targets[start:end] {
			cn := pq.QuoteIdentifier(c.name)
			switch c.kind {
			case "jsonb":
				exprs = append(exprs, fmt.Sprintf("COUNT(*) FILTER (WHERE EXISTS (SELECT 1 FROM unnest(%s::text[]) o WHERE strpos(%s::text, o) > 0))", param(pq.Array(qOlds)), cn))
			case "ARRAY":
				exprs = append(exprs, fmt.Sprintf("COUNT(*) FILTER (WHERE %s::text[] && %s::text[])", cn, param(pq.Array(olds))))
			default:
				exprs = append(exprs, fmt.Sprintf("COUNT(*) FILTER (WHERE %s = ANY(%s::text[]))", cn, param(pq.Array(olds))))
			}
		}
		where := ""
		if table == "documents" { // the records' own data is rewritten with them
			where = fmt.Sprintf(" WHERE NOT (doctype = %s AND id = ANY(%s::text[]))", param(doctype), param(pq.Array(olds)))
		}
		counts := make([]int, end-start)
		dest := make([]interface{}, len(counts))
		for i := range counts {
			dest[i] = &counts[i]
		}
		if err := db.DB.QueryRow(fmt.Sprintf("SELECT %s FROM %s.%s%s", strings.Join(exprs, ", "), pq.QuoteIdentifier(schema), pq.QuoteIdentifier(table), where), args...).Scan(dest...); err != nil {
			return nil, fmt.Errorf("%s: %v", table, err)
		}
		for i, n := range counts {
			if n > 0 {
				out = append(out, MasterRecodeTouch{Table: table, Column: targets[start+i].name, Rows: n})
			}
		}
		start = end
	}
	return out, nil
}

// renamedMasterCode is the code an old long code was cleaned to, if it was.
func renamedMasterCode(schema, doctype, old string) string {
	var code string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT new_code FROM %s.master_code_changes WHERE doc_type = $1 AND old_code = $2`, schema), doctype, old).Scan(&code)
	return code
}

// RenamedMasterCode is the code an old long master code was cleaned to, or
// "" - so a search or a link carrying the old code (an old printout, a
// till's cache) still finds its record (76.6). Every cleaned code had a "/",
// so anything else costs no query; no change table yet reads as no rename.
func RenamedMasterCode(tenantID, doctype, old string) string {
	if !strings.Contains(old, "/") {
		return ""
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	return renamedMasterCode(schema, doctype, old)
}
