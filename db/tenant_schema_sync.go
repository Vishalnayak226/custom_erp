package db

// Stage 64.1: bring every client schema up to tenant_default's shape.
//
// Found on production 2026-10-16: tenant_minn, provisioned 2026-09-22, was
// missing 16 tables (print_job_log, bin_allocation, mfa_recovery_codes, the
// audit archive tables...), 11 columns (audit_logs.signature among them - so
// every audit write on that client failed), 53 record types with 830 fields,
// their permissions and number series, and every StatusTransitionRule.
//
// Two gaps produced it, and both had been patched one table at a time before
// (26.11.2, 30.2.2, 31.1): most migrations change only tenant_default, and the
// ones that do loop over the client schemas only reach clients that existed
// when they ran; ProvisionTenantSchema then copies a hand-kept table list
// that a new table has to remember to join. Rather than add the 17th entry
// to that list, this compares the catalog and copies whatever is missing, and
// it runs at both choke points: after every `erp-server -migrate`, and inside
// ProvisionTenantSchema's transaction.
//
// What it does, all additive and insert-only (nothing a client already has is
// changed, and no business record is copied):
//   1. creates every tenant_default table the client lacks (LIKE ... INCLUDING ALL);
//   2. adds every missing column, with its type, default and NOT NULL;
//   3. creates every missing index (compared by definition, not by name);
//   4. gives every serial column that draws from another schema's sequence a
//      sequence of its own, started after the column's highest value - so no
//      two clients share an id counter (the hazard 30.2.2 called out, which a
//      LIKE clone still has);
//   5. inserts missing record types and fields; the default permissions for
//      record types it added (never for ones the client already has, so a
//      permission an admin removed is not handed back); missing number series;
//   6. inserts the system seed records named in systemSeedDoctypes, by id;
//   7. puts the row guards in guardTriggers on the tables that lack them, and
//      attaches the documents edit-log trigger when it is missing (Stage
//      47.16.4a; LIKE copies no triggers);
//   8. grants the least-privilege roles on the schema when they exist.
// Counters (sequence_counters), users, settings and every other record are
// left alone.

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/lib/pq"
)

const templateSchema = "tenant_default"

// systemSeedDoctypes are documents rows that migrations seed as system
// configuration, not client data: the status-change rules every record type
// is checked against, the base currency, and the statutory TDS sections.
var systemSeedDoctypes = []string{"StatusTransitionRule", "Currency", "TDSSection"}

// metaTable is a configuration table copied row by row on a natural key.
type metaTable struct {
	name string
	key  []string // natural key the "missing" test compares on
	skip []string // surrogate columns the target assigns itself
	// newDoctypesOnly limits the copy to record types this run added.
	newDoctypesOnly string
}

var syncMetaTables = []metaTable{
	{name: "doctype_meta", key: []string{"name"}},
	{name: "doctype_fields", key: []string{"doctype_name", "fieldname"}, skip: []string{"id"}},
	{name: "role_permissions", key: []string{"role", "doctype_name"}, skip: []string{"id"}, newDoctypesOnly: "doctype_name"},
	{name: "prefix_configs", key: []string{"doc_type"}, skip: []string{"id"}},
}

// guardTriggers are the row guards a client's table carries, as
// tenant_default's does (each added by its migration). Each runs a function in
// public, so one trigger definition serves every schema. Put on a table only
// once its function exists, so a binary ahead of its migration still syncs.
var guardTriggers = []struct{ table, name, fn string }{
	// Stage 47.16.4b: posted ledger rows never change or go (db/migrations_stage47_16_4b_gl_postings_guard.sql).
	{"gl_postings", "gl_postings_guard", "public.gl_postings_guard()"},
	// Stage 47.16.4f: an IRP acknowledgement is never changed (db/migrations_stage75_2_irp_evidence_47_16_4f.sql).
	{"irp_documents", "irp_documents_ack_immutable", "public.irp_documents_ack_immutable()"},
}

var schemaNameRE = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// TenantSchemaSyncReport says what one sync changed.
type TenantSchemaSyncReport struct {
	Schema         string
	TablesCreated  []string
	ColumnsAdded   []string
	IndexesCreated []string
	SequencesOwned []string
	TriggersAdded  []string
	RowsAdded      map[string]int
	Warnings       []string
}

// Changed reports whether the sync altered anything.
func (r TenantSchemaSyncReport) Changed() bool {
	if len(r.TablesCreated)+len(r.ColumnsAdded)+len(r.IndexesCreated)+len(r.SequencesOwned)+len(r.TriggersAdded) > 0 {
		return true
	}
	for _, n := range r.RowsAdded {
		if n > 0 {
			return true
		}
	}
	return false
}

// Summary is a one-line account of the report.
func (r TenantSchemaSyncReport) Summary() string {
	parts := []string{
		fmt.Sprintf("%d tables", len(r.TablesCreated)),
		fmt.Sprintf("%d columns", len(r.ColumnsAdded)),
		fmt.Sprintf("%d indexes", len(r.IndexesCreated)),
		fmt.Sprintf("%d own sequences", len(r.SequencesOwned)),
	}
	if len(r.TriggersAdded) > 0 {
		parts = append(parts, fmt.Sprintf("%d triggers", len(r.TriggersAdded)))
	}
	keys := make([]string, 0, len(r.RowsAdded))
	for k := range r.RowsAdded {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if r.RowsAdded[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", r.RowsAdded[k], k))
		}
	}
	return strings.Join(parts, ", ")
}

// ClientSchemas lists every client schema other than the template.
func ClientSchemas() ([]string, error) {
	rows, err := DB.Query(`SELECT nspname FROM pg_namespace
		WHERE nspname LIKE 'tenant\_%' ESCAPE '\' AND nspname <> $1 ORDER BY 1`, templateSchema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SyncAllTenantSchemas runs SyncTenantSchemaTx on every client schema, each
// in its own transaction, so one client's failure leaves the others synced.
func SyncAllTenantSchemas() ([]TenantSchemaSyncReport, error) {
	schemas, err := ClientSchemas()
	if err != nil {
		return nil, err
	}
	var reports []TenantSchemaSyncReport
	var failed []string
	for _, s := range schemas {
		tx, err := DB.Begin()
		if err != nil {
			return reports, err
		}
		rep, err := SyncTenantSchemaTx(tx, s)
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", s, err))
			continue
		}
		reports = append(reports, rep)
	}
	if len(failed) > 0 {
		return reports, fmt.Errorf("client schema sync failed for %s", strings.Join(failed, "; "))
	}
	return reports, nil
}

// SyncTenantSchemaTx brings one client schema up to tenant_default inside the
// caller's transaction. A statement that cannot apply (a unique index over
// duplicate data, say) is rolled back to its own savepoint and reported as a
// warning; the rest still applies.
func SyncTenantSchemaTx(tx *sql.Tx, schema string) (TenantSchemaSyncReport, error) {
	rep := TenantSchemaSyncReport{Schema: schema, RowsAdded: map[string]int{}}
	if !schemaNameRE.MatchString(schema) || schema == templateSchema {
		return rep, fmt.Errorf("%q is not a client schema", schema)
	}
	s := pq.QuoteIdentifier(schema)
	tpl := pq.QuoteIdentifier(templateSchema)

	// try runs one statement under a savepoint; false means it was skipped.
	try := func(what, stmt string, args ...interface{}) bool {
		if _, err := tx.Exec(`SAVEPOINT tenant_sync`); err != nil {
			rep.Warnings = append(rep.Warnings, what+": "+err.Error())
			return false
		}
		if _, err := tx.Exec(stmt, args...); err != nil {
			tx.Exec(`ROLLBACK TO SAVEPOINT tenant_sync`)
			rep.Warnings = append(rep.Warnings, what+": "+err.Error())
			return false
		}
		tx.Exec(`RELEASE SAVEPOINT tenant_sync`)
		return true
	}

	// 1. Tables.
	missing, err := queryStrings(tx, `
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r','p') AND NOT c.relispartition
		  AND to_regclass(quote_ident($2) || '.' || quote_ident(c.relname)) IS NULL
		ORDER BY 1`, templateSchema, schema)
	if err != nil {
		return rep, err
	}
	for _, t := range missing {
		qt := pq.QuoteIdentifier(t)
		if try("create table "+t, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.%s (LIKE %s.%s INCLUDING ALL)`, s, qt, tpl, qt)) {
			rep.TablesCreated = append(rep.TablesCreated, t)
		}
	}

	// 2. Columns of tables both schemas have.
	cols, err := tx.Query(`
		SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod), a.attnotnull,
		       COALESCE(pg_get_expr(d.adbin, d.adrelid), ''), a.attidentity::text, a.attgenerated::text
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relkind IN ('r','p') AND NOT c.relispartition
		  AND a.attnum > 0 AND NOT a.attisdropped
		  AND to_regclass(quote_ident($2) || '.' || quote_ident(c.relname)) IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM pg_attribute b
		                  WHERE b.attrelid = to_regclass(quote_ident($2) || '.' || quote_ident(c.relname))
		                    AND b.attname = a.attname AND NOT b.attisdropped)
		ORDER BY c.relname, a.attnum`, templateSchema, schema)
	if err != nil {
		return rep, err
	}
	type colDef struct {
		table, col, typ, def, identity, generated string
		notNull                                   bool
	}
	var addCols []colDef
	for cols.Next() {
		var c colDef
		if err := cols.Scan(&c.table, &c.col, &c.typ, &c.notNull, &c.def, &c.identity, &c.generated); err != nil {
			cols.Close()
			return rep, err
		}
		addCols = append(addCols, c)
	}
	cols.Close()
	for _, c := range addCols {
		qt, qc := pq.QuoteIdentifier(c.table), pq.QuoteIdentifier(c.col)
		clause := c.typ
		hasValue := false
		switch {
		case c.generated == "s":
			clause += fmt.Sprintf(" GENERATED ALWAYS AS (%s) STORED", c.def)
			hasValue = true
		case c.identity == "a":
			clause += " GENERATED ALWAYS AS IDENTITY"
			hasValue = true
		case c.identity == "d":
			clause += " GENERATED BY DEFAULT AS IDENTITY"
			hasValue = true
		case strings.HasPrefix(c.def, "nextval("):
			seq := ownSequenceName(c.table, c.col)
			if !try("sequence for "+c.table+"."+c.col, fmt.Sprintf(`CREATE SEQUENCE IF NOT EXISTS %s.%s`, s, pq.QuoteIdentifier(seq))) {
				continue
			}
			clause += fmt.Sprintf(" DEFAULT nextval(%s::regclass)", pq.QuoteLiteral(schema+"."+seq))
			hasValue = true
		case c.def != "":
			clause += " DEFAULT " + strings.ReplaceAll(c.def, templateSchema+".", schema+".")
			hasValue = true
		}
		if c.notNull && c.generated != "s" {
			if hasValue {
				clause += " NOT NULL"
			} else {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s.%s added without NOT NULL: no default to fill existing rows", c.table, c.col))
			}
		}
		if try("add column "+c.table+"."+c.col, fmt.Sprintf(`ALTER TABLE %s.%s ADD COLUMN IF NOT EXISTS %s %s`, s, qt, qc, clause)) {
			rep.ColumnsAdded = append(rep.ColumnsAdded, c.table+"."+c.col)
			if strings.HasPrefix(c.def, "nextval(") && c.identity == "" {
				tx.Exec(fmt.Sprintf(`ALTER SEQUENCE %s.%s OWNED BY %s.%s.%s`, s, pq.QuoteIdentifier(ownSequenceName(c.table, c.col)), s, qt, qc))
			}
		}
	}

	// 3. Indexes, compared by definition so an index LIKE cloned under a
	// generated name is not created a second time.
	tplIdx, err := indexDefs(tx, templateSchema, schema)
	if err != nil {
		return rep, err
	}
	haveIdx, err := indexDefs(tx, schema, "")
	if err != nil {
		return rep, err
	}
	have := map[string]bool{}
	names := map[string]bool{}
	for _, ix := range haveIdx {
		have[ix.shape] = true
		names[ix.name] = true
	}
	for _, ix := range tplIdx {
		// Present under the same name counts as present: some definitions
		// (an expression index with casts, say) re-parse to different text
		// in the client, so comparing by shape alone re-created an unnamed
		// copy on every run (found on production 2026-10-16).
		if have[ix.shape] || names[ix.name] {
			continue
		}
		stmt := ix.create(schema, ix.name)
		if try("index "+ix.name, stmt) {
			rep.IndexesCreated = append(rep.IndexesCreated, ix.table+":"+ix.name)
			have[ix.shape] = true
		}
	}

	// 4. Serial columns drawing from another schema's sequence.
	foreign, err := tx.Query(`
		SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod)
		FROM pg_attrdef d
		JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
		JOIN pg_class c ON c.oid = d.adrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_depend dep ON dep.classid = 'pg_attrdef'::regclass AND dep.objid = d.oid AND dep.refclassid = 'pg_class'::regclass
		JOIN pg_class sq ON sq.oid = dep.refobjid AND sq.relkind = 'S'
		JOIN pg_namespace sn ON sn.oid = sq.relnamespace
		WHERE n.nspname = $1 AND sn.nspname <> $1
		ORDER BY 1, 2`, schema)
	if err != nil {
		return rep, err
	}
	type fcol struct{ table, col, typ string }
	var fcols []fcol
	for foreign.Next() {
		var f fcol
		if err := foreign.Scan(&f.table, &f.col, &f.typ); err != nil {
			foreign.Close()
			return rep, err
		}
		fcols = append(fcols, f)
	}
	foreign.Close()
	for _, f := range fcols {
		qt, qc := pq.QuoteIdentifier(f.table), pq.QuoteIdentifier(f.col)
		seq := ownSequenceName(f.table, f.col)
		qseq := s + "." + pq.QuoteIdentifier(seq)
		lit := pq.QuoteLiteral(schema + "." + seq)
		stmt := fmt.Sprintf(`
			CREATE SEQUENCE IF NOT EXISTS %[1]s;
			SELECT setval(%[2]s::regclass, COALESCE((SELECT max(%[4]s) FROM %[3]s.%[5]s), 0) + 1, false);
			ALTER TABLE %[3]s.%[5]s ALTER COLUMN %[4]s SET DEFAULT nextval(%[2]s::regclass);
			ALTER SEQUENCE %[1]s OWNED BY %[3]s.%[5]s.%[4]s;`, qseq, lit, s, qc, qt)
		if f.typ != "smallint" && f.typ != "integer" && f.typ != "bigint" {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s.%s draws from another schema's sequence but is %s; left as is", f.table, f.col, f.typ))
			continue
		}
		if try("own sequence "+f.table+"."+f.col, stmt) {
			rep.SequencesOwned = append(rep.SequencesOwned, f.table+"."+f.col)
		}
	}

	// 5. Configuration rows, insert-only.
	var newDoctypes []string
	for _, m := range syncMetaTables {
		if to, _ := regclassExists(tx, schema, m.name); !to {
			continue
		}
		if tf, _ := regclassExists(tx, templateSchema, m.name); !tf {
			continue
		}
		colsList, err := sharedColumns(tx, m.name, schema, m.skip)
		if err != nil {
			return rep, err
		}
		var match []string
		for _, k := range m.key {
			qk := pq.QuoteIdentifier(k)
			match = append(match, fmt.Sprintf("x.%s IS NOT DISTINCT FROM t.%s", qk, qk))
		}
		filter := ""
		if m.newDoctypesOnly != "" {
			if len(newDoctypes) == 0 {
				continue
			}
			filter = fmt.Sprintf(" AND t.%s = ANY($1)", pq.QuoteIdentifier(m.newDoctypesOnly))
		}
		sel := make([]string, len(colsList))
		for i, c := range colsList {
			sel[i] = "t." + c
		}
		stmt := fmt.Sprintf(`INSERT INTO %s.%s (%s) SELECT %s FROM %s.%s t
			WHERE NOT EXISTS (SELECT 1 FROM %s.%s x WHERE %s)%s
			ON CONFLICT DO NOTHING RETURNING %s`,
			s, pq.QuoteIdentifier(m.name), strings.Join(colsList, ", "), strings.Join(sel, ", "),
			tpl, pq.QuoteIdentifier(m.name), s, pq.QuoteIdentifier(m.name), strings.Join(match, " AND "), filter,
			pq.QuoteIdentifier(m.key[0]))
		var args []interface{}
		if filter != "" {
			args = append(args, pq.Array(newDoctypes))
		}
		added, err := insertReturning(tx, stmt, args...)
		if err != nil {
			rep.Warnings = append(rep.Warnings, m.name+": "+err.Error())
			continue
		}
		rep.RowsAdded[m.name] = len(added)
		if m.name == "doctype_meta" {
			newDoctypes = added
		}
	}

	// 6. System seed records.
	if ok, _ := regclassExists(tx, schema, "documents"); ok {
		colsList, err := sharedColumns(tx, "documents", schema, nil)
		if err != nil {
			return rep, err
		}
		sel := make([]string, len(colsList))
		for i, c := range colsList {
			sel[i] = "t." + c
		}
		stmt := fmt.Sprintf(`INSERT INTO %s.documents (%s) SELECT %s FROM %s.documents t
			WHERE t.doctype = ANY($1) AND NOT EXISTS (SELECT 1 FROM %s.documents x WHERE x.id = t.id)
			ON CONFLICT DO NOTHING RETURNING id`,
			s, strings.Join(colsList, ", "), strings.Join(sel, ", "), tpl, s)
		added, err := insertReturning(tx, stmt, pq.Array(systemSeedDoctypes))
		if err != nil {
			rep.Warnings = append(rep.Warnings, "system seed records: "+err.Error())
		} else {
			rep.RowsAdded["system seed records"] = len(added)
		}
	}

	// 7. Row guards (Stage 47.16.4b / 4f): LIKE copies no triggers.
	for _, g := range guardTriggers {
		var tableThere, fnThere, guarded bool
		if err := tx.QueryRow(`SELECT to_regclass(quote_ident($1) || '.' || quote_ident($2)) IS NOT NULL, to_regprocedure($3) IS NOT NULL,
			EXISTS (SELECT 1 FROM pg_trigger t WHERE t.tgrelid = to_regclass(quote_ident($1) || '.' || quote_ident($2)) AND t.tgname = $4)`,
			schema, g.table, g.fn, g.name).Scan(&tableThere, &fnThere, &guarded); err != nil {
			return rep, err
		}
		if !tableThere || !fnThere || guarded {
			continue
		}
		if try("trigger "+g.name, fmt.Sprintf(`CREATE TRIGGER %s BEFORE UPDATE OR DELETE ON %s.%s FOR EACH ROW EXECUTE FUNCTION %s`,
			pq.QuoteIdentifier(g.name), s, pq.QuoteIdentifier(g.table), g.fn)) {
			rep.TriggersAdded = append(rep.TriggersAdded, g.table+":"+g.name)
		}
	}

	// 7b. The edit log (Stage 47.16.4a, Rule 3(1)). LIKE ... INCLUDING ALL
	// copies no triggers, so a new client gets it here - ProvisionTenantSchema
	// runs this sync - and a client that has lost it gets it back on -migrate.
	if missing, err := DocumentEditLogMissingTx(tx, schema); err != nil {
		rep.Warnings = append(rep.Warnings, "edit log: "+err.Error())
	} else if missing && try("edit log", fmt.Sprintf(`CREATE TRIGGER %s AFTER UPDATE ON %s.documents FOR EACH ROW
		WHEN (OLD.data IS DISTINCT FROM NEW.data) EXECUTE FUNCTION %s()`, DocumentEditLogTrigger, s, documentEditLogFunction)) {
		rep.TriggersAdded = append(rep.TriggersAdded, "documents."+DocumentEditLogTrigger)
	}

	// 8. Least-privilege grants (only on a hardened cluster).
	if rep.Changed() {
		if err := GrantLeastPrivilegeSchemaAccessTx(tx, schema); err != nil {
			rep.Warnings = append(rep.Warnings, "grants: "+err.Error())
		}
	}
	return rep, nil
}

// GrantLeastPrivilegeSchemaAccessTx grants erp_app/erp_backup on a schema
// when deploy/postgres_harden.sql has created them; a no-op otherwise.
// (49.7.4: ALTER DEFAULT PRIVILEGES only covers schemas that existed when the
// hardening script ran, and only objects created afterwards.)
func GrantLeastPrivilegeSchemaAccessTx(tx *sql.Tx, schemaName string) error {
	var hardened bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'erp_app')`).Scan(&hardened); err != nil {
		return err
	}
	if !hardened {
		return nil
	}
	_, err := tx.Exec(fmt.Sprintf(`
		GRANT USAGE ON SCHEMA %[1]s TO erp_app;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %[1]s TO erp_app;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %[1]s TO erp_app;
		GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA %[1]s TO erp_app;
		GRANT USAGE ON SCHEMA %[1]s TO erp_backup;
		GRANT SELECT ON ALL TABLES IN SCHEMA %[1]s TO erp_backup;
		GRANT SELECT ON ALL SEQUENCES IN SCHEMA %[1]s TO erp_backup;
		ALTER DEFAULT PRIVILEGES IN SCHEMA %[1]s GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO erp_app;
		ALTER DEFAULT PRIVILEGES IN SCHEMA %[1]s GRANT USAGE, SELECT ON SEQUENCES TO erp_app;
		ALTER DEFAULT PRIVILEGES IN SCHEMA %[1]s GRANT EXECUTE ON FUNCTIONS TO erp_app;
		ALTER DEFAULT PRIVILEGES IN SCHEMA %[1]s GRANT SELECT ON TABLES TO erp_backup;
		ALTER DEFAULT PRIVILEGES IN SCHEMA %[1]s GRANT SELECT ON SEQUENCES TO erp_backup;
	`, pq.QuoteIdentifier(schemaName)))
	return err
}

// ownSequenceName is the per-schema sequence a serial column is given,
// within Postgres's 63-byte identifier limit.
func ownSequenceName(table, col string) string {
	n := table + "_" + col + "_seq"
	if len(n) > 63 {
		n = n[:63]
	}
	return n
}

type indexDef struct {
	name, table string
	unique      bool
	body        string // "USING btree (...) [WHERE ...]", schema qualifiers stripped
	shape       string // table|unique|body: what two indexes are compared on
}

// create rebuilds the template's index on the target schema's table.
func (ix indexDef) create(schema, name string) string {
	unique := ""
	if ix.unique {
		unique = "UNIQUE "
	}
	return fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s.%s %s", unique, pq.QuoteIdentifier(name),
		pq.QuoteIdentifier(schema), pq.QuoteIdentifier(ix.table), ix.body)
}

var indexOnRE = regexp.MustCompile(`^CREATE (UNIQUE )?INDEX \S+ ON (ONLY )?\S+ (.*)$`)

// indexDefs lists a schema's indexes. With alsoIn set, only tables that
// schema also has are listed.
func indexDefs(tx *sql.Tx, schema, alsoIn string) ([]indexDef, error) {
	q := `SELECT i.relname, t.relname, pg_get_indexdef(i.oid)
		FROM pg_index x
		JOIN pg_class i ON i.oid = x.indexrelid
		JOIN pg_class t ON t.oid = x.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = $1 AND t.relkind IN ('r','p') AND NOT t.relispartition`
	args := []interface{}{schema}
	if alsoIn != "" {
		q += ` AND to_regclass(quote_ident($2) || '.' || quote_ident(t.relname)) IS NOT NULL`
		args = append(args, alsoIn)
	}
	rows, err := tx.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []indexDef
	for rows.Next() {
		var ix indexDef
		var def string
		if err := rows.Scan(&ix.name, &ix.table, &def); err != nil {
			return nil, err
		}
		m := indexOnRE.FindStringSubmatch(def)
		if m == nil {
			continue
		}
		ix.unique = m[1] != ""
		ix.body = strings.ReplaceAll(m[3], schema+".", "")
		ix.shape = fmt.Sprintf("%s|%t|%s", ix.table, ix.unique, ix.body)
		out = append(out, ix)
	}
	return out, rows.Err()
}

// DocumentEditLogTrigger is the Stage 47.16.4a edit-log trigger on a schema's
// documents table: one audit_logs row per save that changes `data`, with the
// old and new value of each changed field
// (db/migrations_stage47_16_4a_edit_log_every_client.sql).
const DocumentEditLogTrigger = "trg_log_document_changes"

const documentEditLogFunction = "public.erp_log_document_edit"

// DocumentEditLogMissingTx reports whether schema has a documents table that
// lacks the edit-log trigger. It is an error for the trigger's function not to
// exist: the binary is ahead of its migration.
func DocumentEditLogMissingTx(tx *sql.Tx, schema string) (bool, error) {
	if ok, err := regclassExists(tx, schema, "documents"); err != nil || !ok {
		return false, err
	}
	var fn, attached bool
	if err := tx.QueryRow(`SELECT to_regprocedure($1 || '()') IS NOT NULL,
		EXISTS (SELECT 1 FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		         WHERE n.nspname = $2 AND c.relname = 'documents' AND t.tgname = $3 AND NOT t.tgisinternal)`,
		documentEditLogFunction, schema, DocumentEditLogTrigger).Scan(&fn, &attached); err != nil {
		return false, err
	}
	if !attached && !fn {
		return true, fmt.Errorf("%s() does not exist - run erp-server -migrate", documentEditLogFunction)
	}
	return !attached, nil
}

func regclassExists(tx *sql.Tx, schema, table string) (bool, error) {
	var ok bool
	err := tx.QueryRow(`SELECT to_regclass(quote_ident($1) || '.' || quote_ident($2)) IS NOT NULL`, schema, table).Scan(&ok)
	return ok, err
}

// sharedColumns lists the template table's columns that the target also has,
// quoted, minus skip.
func sharedColumns(tx *sql.Tx, table, schema string, skip []string) ([]string, error) {
	names, err := queryStrings(tx, `
		SELECT a.attname FROM pg_attribute a
		WHERE a.attrelid = to_regclass(quote_ident($1) || '.' || quote_ident($3)) AND a.attnum > 0 AND NOT a.attisdropped
		  AND a.attgenerated = ''
		  AND EXISTS (SELECT 1 FROM pg_attribute b WHERE b.attrelid = to_regclass(quote_ident($2) || '.' || quote_ident($3))
		              AND b.attname = a.attname AND NOT b.attisdropped AND b.attgenerated = '')
		ORDER BY a.attnum`, templateSchema, schema, table)
	if err != nil {
		return nil, err
	}
	skipSet := map[string]bool{}
	for _, k := range skip {
		skipSet[k] = true
	}
	var out []string
	for _, n := range names {
		if !skipSet[n] {
			out = append(out, pq.QuoteIdentifier(n))
		}
	}
	return out, nil
}

func queryStrings(tx *sql.Tx, q string, args ...interface{}) ([]string, error) {
	rows, err := tx.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// insertReturning runs an INSERT ... RETURNING <text-able key> under a
// savepoint and returns the keys it inserted.
func insertReturning(tx *sql.Tx, stmt string, args ...interface{}) ([]string, error) {
	if _, err := tx.Exec(`SAVEPOINT tenant_sync_rows`); err != nil {
		return nil, err
	}
	rows, err := tx.Query(stmt, args...)
	if err != nil {
		tx.Exec(`ROLLBACK TO SAVEPOINT tenant_sync_rows`)
		return nil, err
	}
	var out []string
	for rows.Next() {
		var v sql.NullString
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			tx.Exec(`ROLLBACK TO SAVEPOINT tenant_sync_rows`)
			return nil, err
		}
		out = append(out, v.String)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		tx.Exec(`ROLLBACK TO SAVEPOINT tenant_sync_rows`)
		return nil, err
	}
	tx.Exec(`RELEASE SAVEPOINT tenant_sync_rows`)
	return out, nil
}
