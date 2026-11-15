package engines

// 50.12 / BLD-030 (ask 193): a portable, versioned export of one client and a
// clean import of it into a NEW client, with an independent reconcile.
//
// Platform authority only: this is called from cmd/tenantctl (export, import,
// reconcile) and has no HTTP route. A route would sit in front of CREATE/DROP
// SCHEMA, the reason tenantctl exists.
//
// The export is a directory:
//
//	manifest.json                  format version, migrations, tenant, per-table rows + sha256
//	tables/<table>.jsonl           one JSON object per row, rows in a fixed order
//	evidence/<name>.jsonl          the client's public-schema evidence rows (lifecycle, package changes)
//	attachments-index.jsonl        every attachment: file name, storage key, size, sha256
//
// Every table of the client's schema is exported (records, configuration, audit
// evidence, attachment metadata) except secrets and queued work, which the
// manifest lists with the reason. Ids are kept verbatim: nothing is remapped, so
// the id mapping is the identity and the manifest says so. Media bytes are NOT
// copied (the index carries the key and sha256); copy the media store beside it.
//
// manifest.json is written last, so a half-written export has no manifest and
// is refused by import. Import validates everything first (format version,
// every file's sha256 and line count, migration set, columns), then creates the
// new client and loads it in ONE transaction, re-counts and re-hashes inside it,
// and removes the new client again if anything fails.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
)

// PortableExportFormat is the manifest format this build writes and reads. An
// import refuses a manifest with a higher number.
const PortableExportFormat = 1

// portableExcluded are the tables an export never carries.
var portableExcluded = map[string]string{
	"api_credentials":          "secret: re-issue in the new client",
	"api_oauth_tokens":         "secret: re-issue in the new client",
	"channel_credentials":      "secret: re-enter in the new client",
	"clevertap_credentials":    "secret: re-enter in the new client",
	"ai_access_clients":        "secret: re-issue in the new client",
	"api_idempotency_keys":     "transient: replay protection of the old client",
	"api_request_log":          "transient: request log of the old client",
	"async_jobs":               "queued work: finish or cancel it before export; never replayed into a copy",
	"integration_event_outbox": "queued work: drain it before export; never replayed into a copy",
	"pim_publish_queue":        "queued work: drain it before export; never replayed into a copy",
}

// portableQueueTables are the excluded tables that hold work not yet done.
var portableQueueTables = []string{"async_jobs", "integration_event_outbox", "pim_publish_queue"}

var portableTerminalStatuses = []string{"Succeeded", "DeadLettered", "Cancelled", "Quarantined", "Dispatched", "Done", "Published", "Sent", "Completed", "Failed"}

var portableConfigTables = map[string]bool{
	"prefix_configs": true, "sequence_counters": true, "dynamic_labels": true, "doctype_meta": true, "doctype_fields": true,
	"role_permissions": true, "gl_accounts": true, "feature_flags": true, "system_settings": true, "module_entitlements": true,
	"extension_hooks": true, "field_permissions": true, "approval_rules": true, "loyalty_tier_rules": true,
	"accounting_periods": true, "tenant_limits": true, "stock_locations": true, "stock_location_defaults": true, "industry_lock": true,
}

var portableAuditTables = map[string]bool{
	"audit_logs": true, "approval_log": true, "system_error_logs": true, "extension_hook_log": true, "integration_event_log": true,
}

func portableClass(table string) string {
	switch {
	case portableConfigTables[table]:
		return "configuration"
	case portableAuditTables[table]:
		return "audit"
	case strings.Contains(table, "attachment") || strings.Contains(table, "media"):
		return "attachments"
	}
	return "records"
}

// PortableTable is one exported table.
type PortableTable struct {
	Name    string   `json:"name"`
	Class   string   `json:"class"` // records | configuration | audit | attachments
	File    string   `json:"file"`
	Rows    int64    `json:"rows"`
	SHA256  string   `json:"sha256"`
	Columns []string `json:"columns"`
	// Generated lists columns the database computes; they are in neither the
	// file nor the hash.
	Generated []string `json:"generated,omitempty"`
}

// PortableFile is an evidence or index file.
type PortableFile struct {
	File   string `json:"file"`
	Rows   int64  `json:"rows"`
	SHA256 string `json:"sha256"`
	Note   string `json:"note,omitempty"`
}

// PortableSequence is a sequence's position, restored on import so numbering
// carries on from where the source stopped.
type PortableSequence struct {
	Name      string `json:"name"`
	LastValue int64  `json:"last_value"`
}

// PortableManifest is manifest.json.
type PortableManifest struct {
	FormatVersion int                `json:"format_version"`
	TenantID      string             `json:"tenant_id"`
	Schema        string             `json:"schema"`
	AppVersion    string             `json:"app_version,omitempty"`
	ExportedAt    string             `json:"exported_at"`
	ExportedBy    string             `json:"exported_by"`
	Migrations    []string           `json:"migrations"`
	IDs           string             `json:"ids"`
	Tables        []PortableTable    `json:"tables"`
	Sequences     []PortableSequence `json:"sequences,omitempty"`
	Evidence      []PortableFile     `json:"evidence"`
	Attachments   PortableFile       `json:"attachments_index"`
	Excluded      map[string]string  `json:"excluded"`
	PendingQueue  map[string]int64   `json:"pending_queued_work,omitempty"`
	// Digest is the sha256 of every table's name, row count and hash, in
	// name order: one value that names the whole export.
	Digest string `json:"digest"`
}

func (m *PortableManifest) computeDigest() string {
	h := sha256.New()
	for _, t := range m.Tables {
		fmt.Fprintf(h, "%s|%d|%s\n", t.Name, t.Rows, t.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func portableQuote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func portableTableColumns(q interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}, schema, table string) (cols, generated []string, err error) {
	rows, err := q.Query(`SELECT a.attname, a.attgenerated <> ''
		FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum`, schema, table)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var gen bool
		if err := rows.Scan(&name, &gen); err != nil {
			return nil, nil, err
		}
		if gen {
			generated = append(generated, name)
		} else {
			cols = append(cols, name)
		}
	}
	return cols, generated, rows.Err()
}

// portableDump writes the table's rows, one canonical JSON object per line in
// byte order, to w (nil to only count and hash) and returns the count and the
// sha256 of what it wrote. dropCols are left out of every row: generated
// columns, and (on a reconcile) columns a newer schema added.
func portableDump(tx *sql.Tx, schema, table string, dropCols []string, w io.Writer) (int64, string, error) {
	q := fmt.Sprintf(`SELECT j FROM (SELECT (to_jsonb(t) - $1::text[])::text AS j FROM %s.%s t) x ORDER BY j COLLATE "C"`,
		portableQuote(schema), portableQuote(table))
	rows, err := tx.Query(q, saasTextArray(dropCols))
	if err != nil {
		return 0, "", fmt.Errorf("%s: %w", table, err)
	}
	defer rows.Close()
	h := sha256.New()
	var n int64
	for rows.Next() {
		var j string
		if err := rows.Scan(&j); err != nil {
			return 0, "", err
		}
		h.Write([]byte(j))
		h.Write([]byte{'\n'})
		if w != nil {
			if _, err := io.WriteString(w, j+"\n"); err != nil {
				return 0, "", err
			}
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func portableWriteFile(path string, fill func(w io.Writer) (int64, string, error)) (int64, string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, "", err
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	n, sum, err := fill(bw)
	if err == nil {
		err = bw.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return n, sum, err
}

func portableAppliedMigrations(q interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}) ([]string, error) {
	rows, err := q.Query(`SELECT migration_file FROM public.schema_migrations ORDER BY migration_file`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ExportTenantPortable writes the client's portable export into dir, which
// must not exist or must be empty. It refuses a client with queued work
// outstanding unless allowPendingQueue, because queued work is never carried.
// Reads run in one repeatable-read, read-only transaction: the export is a
// single consistent snapshot and nothing in the source changes.
func ExportTenantPortable(tenantID, dir, actor string, allowPendingQueue bool) (*PortableManifest, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if !validSQLIdentifier(schema) {
		return nil, fmt.Errorf("%q is not a usable schema name", schema)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("export folder %s is not empty; use a new folder", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok { // nothing half-written: no manifest, no files
			_ = os.RemoveAll(dir)
		}
	}()

	tx, err := db.DB.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return nil, err
	}

	m := &PortableManifest{FormatVersion: PortableExportFormat, TenantID: tenantID, Schema: schema,
		ExportedAt: time.Now().UTC().Format(time.RFC3339), ExportedBy: actor,
		IDs: "kept verbatim: no id is remapped, so the id mapping is the identity", Excluded: map[string]string{}, PendingQueue: map[string]int64{}}
	_ = tx.QueryRow(`SELECT COALESCE(app_version, '') FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&m.AppVersion)
	if m.Migrations, err = portableAppliedMigrations(tx); err != nil {
		return nil, fmt.Errorf("migration list: %w", err)
	}
	tables, err := tenantResetTables(tx, schema)
	if err != nil {
		return nil, err
	}
	sort.Strings(tables)

	for _, qt := range portableQueueTables {
		var exists bool
		if err := tx.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+"."+qt).Scan(&exists); err != nil || !exists {
			continue
		}
		var hasStatus bool
		_ = tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = 'status')`, schema, qt).Scan(&hasStatus)
		query := fmt.Sprintf(`SELECT count(*) FROM %s.%s`, portableQuote(schema), portableQuote(qt))
		if hasStatus {
			query += fmt.Sprintf(` WHERE status <> ALL ('%s'::text[])`, saasTextArray(portableTerminalStatuses))
		}
		var n int64
		if err := tx.QueryRow(query).Scan(&n); err != nil {
			return nil, fmt.Errorf("%s: %w", qt, err)
		}
		if n > 0 {
			m.PendingQueue[qt] = n
		}
	}
	if len(m.PendingQueue) > 0 && !allowPendingQueue {
		return nil, fmt.Errorf("queued work is still outstanding (%v): let it finish or cancel it, or pass -allow-pending-queue to export without it (it is never carried)", m.PendingQueue)
	}

	for _, t := range tables {
		if why, skip := portableExcluded[t]; skip {
			m.Excluded[t] = why
			continue
		}
		cols, gen, err := portableTableColumns(tx, schema, t)
		if err != nil {
			return nil, err
		}
		pt := PortableTable{Name: t, Class: portableClass(t), File: "tables/" + t + ".jsonl", Columns: cols, Generated: gen}
		pt.Rows, pt.SHA256, err = portableWriteFile(filepath.Join(dir, filepath.FromSlash(pt.File)), func(w io.Writer) (int64, string, error) {
			return portableDump(tx, schema, t, gen, w)
		})
		if err != nil {
			return nil, err
		}
		m.Tables = append(m.Tables, pt)
	}
	if rows, err := tx.Query(`SELECT sequencename, COALESCE(last_value, 0) FROM pg_sequences WHERE schemaname = $1 AND last_value IS NOT NULL ORDER BY 1`, schema); err == nil {
		for rows.Next() {
			var s PortableSequence
			if err := rows.Scan(&s.Name, &s.LastValue); err == nil {
				m.Sequences = append(m.Sequences, s)
			}
		}
		rows.Close()
	}

	// Evidence kept in the shared (public) schema for this client.
	for _, ev := range []struct{ name, table string }{
		{"tenant_lifecycle_events", "public.tenant_lifecycle_events"},
		{"saas_package_transitions", "public.saas_package_transitions"},
	} {
		var exists bool
		if err := tx.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, ev.table).Scan(&exists); err != nil || !exists {
			continue
		}
		pf := PortableFile{File: "evidence/" + ev.name + ".jsonl", Note: "the old client's own trail; read-only evidence, not loaded into the new client"}
		pf.Rows, pf.SHA256, err = portableWriteFile(filepath.Join(dir, filepath.FromSlash(pf.File)), func(w io.Writer) (int64, string, error) {
			return portableEvidenceDump(tx, ev.table, tenantID, w)
		})
		if err != nil {
			return nil, err
		}
		m.Evidence = append(m.Evidence, pf)
	}

	// Attachment index: where each file is and the hash it must have. Bytes stay in the media store.
	m.Attachments = PortableFile{File: "attachments-index.jsonl", Note: "media bytes are not copied; copy the media store beside this export and check each sha256"}
	var hasAtt bool
	_ = tx.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+".document_attachments").Scan(&hasAtt)
	m.Attachments.Rows, m.Attachments.SHA256, err = portableWriteFile(filepath.Join(dir, m.Attachments.File), func(w io.Writer) (int64, string, error) {
		if !hasAtt {
			return 0, hex.EncodeToString(sha256.New().Sum(nil)), nil
		}
		return portableAttachmentIndex(tx, schema, w)
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	m.Digest = m.computeDigest()
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), append(raw, '\n'), 0o600); err != nil {
		return nil, err
	}
	ok = true
	return m, nil
}

func portableEvidenceDump(tx *sql.Tx, table, tenantID string, w io.Writer) (int64, string, error) {
	rows, err := tx.Query(fmt.Sprintf(`SELECT j FROM (SELECT to_jsonb(t)::text AS j FROM %s t WHERE tenant_id = $1) x ORDER BY j COLLATE "C"`, table), tenantID)
	if err != nil {
		return 0, "", err
	}
	defer rows.Close()
	h := sha256.New()
	var n int64
	for rows.Next() {
		var j string
		if err := rows.Scan(&j); err != nil {
			return 0, "", err
		}
		h.Write([]byte(j))
		h.Write([]byte{'\n'})
		if _, err := io.WriteString(w, j+"\n"); err != nil {
			return 0, "", err
		}
		n++
	}
	return n, hex.EncodeToString(h.Sum(nil)), rows.Err()
}

func portableAttachmentIndex(tx *sql.Tx, schema string, w io.Writer) (int64, string, error) {
	rows, err := tx.Query(fmt.Sprintf(`SELECT j FROM (SELECT jsonb_build_object('id', id, 'doctype', doctype, 'document_id', document_id,
		'file_name', file_name, 'size_bytes', size_bytes, 'sha256', sha256, 'storage_key', storage_key, 'deleted', deleted_at IS NOT NULL)::text AS j
		FROM %s.document_attachments) x ORDER BY j COLLATE "C"`, portableQuote(schema)))
	if err != nil {
		return 0, "", err
	}
	defer rows.Close()
	h := sha256.New()
	var n int64
	for rows.Next() {
		var j string
		if err := rows.Scan(&j); err != nil {
			return 0, "", err
		}
		h.Write([]byte(j))
		h.Write([]byte{'\n'})
		if _, err := io.WriteString(w, j+"\n"); err != nil {
			return 0, "", err
		}
		n++
	}
	return n, hex.EncodeToString(h.Sum(nil)), rows.Err()
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

func portableFileSum(path string) (lines int64, sum string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	br := bufio.NewReaderSize(f, 1<<20)
	for {
		line, rerr := br.ReadBytes('\n')
		if len(line) > 0 {
			h.Write(line)
			if line[len(line)-1] == '\n' {
				lines++
			} else {
				return 0, "", fmt.Errorf("%s ends in the middle of a row", filepath.Base(path))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return 0, "", rerr
		}
	}
	return lines, hex.EncodeToString(h.Sum(nil)), nil
}

// ReadPortableManifest reads and checks manifest.json and every file it names
// (present, whole, right row count and sha256). It touches no database. A
// problem is reported in words that say which file and what is wrong.
func ReadPortableManifest(dir string) (*PortableManifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("manifest.json is missing: this export is incomplete or is not an export folder")
		}
		return nil, err
	}
	var m PortableManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest.json is damaged: %v", err)
	}
	if m.FormatVersion < 1 {
		return nil, fmt.Errorf("manifest.json has no format version: not an export of this system")
	}
	if m.FormatVersion > PortableExportFormat {
		return nil, fmt.Errorf("this export is format %d; this version reads up to format %d: upgrade this system first", m.FormatVersion, PortableExportFormat)
	}
	if m.TenantID == "" || m.Schema == "" {
		return nil, fmt.Errorf("manifest.json does not name its client")
	}
	if want := m.computeDigest(); want != m.Digest {
		return nil, fmt.Errorf("manifest.json does not match its own digest: it was edited or damaged")
	}
	check := func(f PortableFile) error {
		path := filepath.Join(dir, filepath.FromSlash(f.File))
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("%s points outside the export folder", f.File)
		}
		lines, sum, err := portableFileSum(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s is missing: the export is partial", f.File)
			}
			return err
		}
		if lines != f.Rows {
			return fmt.Errorf("%s has %d rows; the manifest says %d: the file is cut short or changed", f.File, lines, f.Rows)
		}
		if sum != f.SHA256 {
			return fmt.Errorf("%s does not match its sha256: the file is damaged or changed", f.File)
		}
		return nil
	}
	for _, t := range m.Tables {
		if !validSQLIdentifier(t.Name) {
			return nil, fmt.Errorf("manifest names %q, which is not a table name", t.Name)
		}
		if err := check(PortableFile{File: t.File, Rows: t.Rows, SHA256: t.SHA256}); err != nil {
			return nil, err
		}
	}
	for _, f := range append(append([]PortableFile{}, m.Evidence...), m.Attachments) {
		if err := check(f); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

// ---------------------------------------------------------------------------
// Import
// ---------------------------------------------------------------------------

// PortableImportResult says what an import did.
type PortableImportResult struct {
	TenantID    string   `json:"tenant_id"`
	Schema      string   `json:"schema"`
	SourceID    string   `json:"source_tenant"`
	Tables      int      `json:"tables"`
	Rows        int64    `json:"rows"`
	NewerBy     []string `json:"upgraded_by_migrations,omitempty"` // migrations the target has and the export predates
	AddedTables []string `json:"tables_new_since_export,omitempty"`
	Digest      string   `json:"digest"`
}

// ImportTenantPortable creates a NEW client (newTenantID in newSchema, both
// must be unused) and loads the export into it. Nothing is written until the
// export has been read and checked whole; if loading fails the new client is
// removed again and the error says why.
func ImportTenantPortable(dir, newTenantID, newSchema, actor, reason string) (*PortableImportResult, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("a reason is required and is recorded in the client's evidence trail")
	}
	if newTenantID == "" || newSchema == "" || !validSQLIdentifier(newSchema) {
		return nil, fmt.Errorf("a new client id and a usable schema name are required")
	}
	m, err := ReadPortableManifest(dir)
	if err != nil {
		return nil, fmt.Errorf("import refused, nothing was written: %w", err)
	}

	// The target must be new, and must know every migration the export was made under.
	var taken int
	if err := db.DB.QueryRow(`SELECT (SELECT count(*) FROM public.tenants WHERE tenant_id = $1 OR schema_name = $2)
		+ (SELECT count(*) FROM information_schema.schemata WHERE schema_name = $2)`, newTenantID, newSchema).Scan(&taken); err != nil {
		return nil, err
	}
	if taken > 0 {
		return nil, fmt.Errorf("import refused, nothing was written: client %q or schema %q already exists; an import only goes into a new, empty client", newTenantID, newSchema)
	}
	have, err := portableAppliedMigrations(db.DB)
	if err != nil {
		return nil, err
	}
	haveSet := map[string]bool{}
	for _, f := range have {
		haveSet[f] = true
	}
	var missing []string
	wantSet := map[string]bool{}
	for _, f := range m.Migrations {
		wantSet[f] = true
		if !haveSet[f] {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("import refused, nothing was written: this database is older than the export (missing %d migration(s), first %s); upgrade it first", len(missing), missing[0])
	}
	res := &PortableImportResult{TenantID: newTenantID, Schema: newSchema, SourceID: m.TenantID, Digest: m.Digest}
	for _, f := range have {
		if !wantSet[f] {
			res.NewerBy = append(res.NewerBy, f)
		}
	}

	// Columns and tables must fit the template every new client is cloned from.
	tmpl, err := portableSchemaShape(db.DB, "tenant_default")
	if err != nil {
		return nil, err
	}
	if err := portableCheckShape(m, tmpl); err != nil {
		return nil, fmt.Errorf("import refused, nothing was written: %w", err)
	}
	exported := map[string]bool{}
	for _, t := range m.Tables {
		exported[t.Name] = true
	}
	for t := range tmpl {
		if _, skip := portableExcluded[t]; !exported[t] && !skip {
			res.AddedTables = append(res.AddedTables, t)
		}
	}
	sort.Strings(res.AddedTables)

	if _, err := ProvisionTenantSchema(newTenantID, newSchema, m.AppVersion); err != nil {
		return nil, fmt.Errorf("import failed, nothing was kept: could not create the new client: %w", err)
	}
	fail := func(err error) (*PortableImportResult, error) {
		portableRemoveClient(newTenantID, newSchema)
		return nil, fmt.Errorf("import failed, the new client was removed and nothing was kept: %w", err)
	}
	if err := portableLoad(dir, m, newSchema); err != nil {
		return fail(err)
	}
	for _, t := range m.Tables {
		res.Tables++
		res.Rows += t.Rows
	}
	if err := RecordTenantLifecycleEvent(newTenantID, "imported", actor,
		fmt.Sprintf("loaded from the export of client %s made %s (format %d, digest %s): %d table(s), %d row(s); reason: %s", m.TenantID, m.ExportedAt, m.FormatVersion, m.Digest, res.Tables, res.Rows, reason)); err != nil {
		return fail(err)
	}
	// Independent check on a fresh connection, after commit.
	rep, err := ReconcileTenantPortable(dir, newTenantID)
	if err != nil {
		return fail(err)
	}
	if !rep.OK {
		_ = SuspendTenant(newTenantID, actor, "import did not reconcile: "+strings.Join(rep.Differences, "; "))
		return res, fmt.Errorf("import loaded but does NOT reconcile; the new client is suspended: %s", strings.Join(rep.Differences, "; "))
	}
	return res, nil
}

func portableRemoveClient(tenantID, schema string) {
	_, _ = db.DB.Exec(`DROP SCHEMA IF EXISTS ` + portableQuote(schema) + ` CASCADE`)
	_, _ = db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
	_, _ = db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, tenantID)
	InvalidateTenantLifecycleCache(tenantID)
}

// portableSchemaShape maps table -> columns (including generated) of a schema.
func portableSchemaShape(q interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}, schema string) (map[string]map[string]bool, error) {
	rows, err := q.Query(`SELECT c.relname, a.attname FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r','p') AND a.attnum > 0 AND NOT a.attisdropped`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]bool{}
	for rows.Next() {
		var t, c string
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		if out[t] == nil {
			out[t] = map[string]bool{}
		}
		out[t][c] = true
	}
	return out, rows.Err()
}

// portableCheckShape refuses an export the target schema cannot hold: a table
// or column the export has that the target lacks. Columns the target added
// since (an additive upgrade) are fine; they take their defaults.
func portableCheckShape(m *PortableManifest, shape map[string]map[string]bool) error {
	for _, t := range m.Tables {
		have, ok := shape[t.Name]
		if !ok {
			return fmt.Errorf("the export has table %s, which this version does not have", t.Name)
		}
		for _, c := range t.Columns {
			if !have[c] {
				return fmt.Errorf("the export has column %s.%s, which this version does not have: importing would lose it", t.Name, c)
			}
		}
	}
	return nil
}

func portableLoad(dir string, m *PortableManifest, schema string) error {
	tx, err := db.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return err
	}
	shape, err := portableSchemaShape(tx, schema)
	if err != nil {
		return err
	}
	if err := portableCheckShape(m, shape); err != nil {
		return err
	}
	if err := sanctionWriteTx(tx, sanctionPurge); err != nil {
		return err
	}
	// The new client's own seed rows are replaced by the export's. The triggers that write
	// edit logs and guard ledger rows are switched off for the load and back on after: the
	// exported rows already include what they wrote.
	for _, t := range m.Tables {
		qt := portableQuote(schema) + "." + portableQuote(t.Name)
		if _, err := tx.Exec(`ALTER TABLE ` + qt + ` DISABLE TRIGGER USER`); err != nil {
			return fmt.Errorf("%s: %w", t.Name, err)
		}
		if _, err := tx.Exec(`DELETE FROM ` + qt); err != nil {
			return fmt.Errorf("%s: could not clear the new client's own rows: %w", t.Name, err)
		}
	}
	for _, t := range m.Tables {
		if err := portableLoadTable(tx, filepath.Join(dir, filepath.FromSlash(t.File)), schema, t); err != nil {
			return fmt.Errorf("%s: %w", t.Name, err)
		}
	}
	for _, s := range m.Sequences {
		var exists bool
		if err := tx.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, portableQuote(schema)+"."+portableQuote(s.Name)).Scan(&exists); err != nil || !exists {
			continue
		}
		if _, err := tx.Exec(`SELECT setval($1::regclass, $2, true)`, portableQuote(schema)+"."+portableQuote(s.Name), s.LastValue); err != nil {
			return fmt.Errorf("sequence %s: %w", s.Name, err)
		}
	}
	// Re-count and re-hash what is now in the new client, before committing.
	for _, t := range m.Tables {
		extra := portableExtraColumns(shape[t.Name], t)
		n, sum, err := portableDump(tx, schema, t.Name, append(append([]string{}, t.Generated...), extra...), nil)
		if err != nil {
			return err
		}
		if n != t.Rows || sum != t.SHA256 {
			return fmt.Errorf("%s did not load faithfully (rows %d of %d, hash match %v)", t.Name, n, t.Rows, sum == t.SHA256)
		}
	}
	for _, t := range m.Tables {
		if _, err := tx.Exec(`ALTER TABLE ` + portableQuote(schema) + "." + portableQuote(t.Name) + ` ENABLE TRIGGER USER`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// portableExtraColumns lists columns the live table has that the export did not,
// so a reconcile hashes only what the export held.
func portableExtraColumns(live map[string]bool, t PortableTable) []string {
	known := map[string]bool{}
	for _, c := range t.Columns {
		known[c] = true
	}
	for _, c := range t.Generated {
		known[c] = true
	}
	var extra []string
	for c := range live {
		if !known[c] {
			extra = append(extra, c)
		}
	}
	sort.Strings(extra)
	return extra
}

func portableLoadTable(tx *sql.Tx, path, schema string, t PortableTable) error {
	if t.Rows == 0 || len(t.Columns) == 0 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	quoted := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		quoted[i] = portableQuote(c)
	}
	list := strings.Join(quoted, ", ")
	qt := portableQuote(schema) + "." + portableQuote(t.Name)
	stmt := fmt.Sprintf(`INSERT INTO %s (%s) OVERRIDING SYSTEM VALUE SELECT %s FROM jsonb_populate_recordset(NULL::%s, $1::jsonb)`, qt, list, list, qt)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	var batch bytes.Buffer
	count := 0
	flush := func() error {
		if count == 0 {
			return nil
		}
		batch.WriteByte(']')
		if _, err := tx.Exec(stmt, batch.String()); err != nil {
			return err
		}
		batch.Reset()
		count = 0
		return nil
	}
	for sc.Scan() {
		if count == 0 {
			batch.WriteByte('[')
		} else {
			batch.WriteByte(',')
		}
		batch.Write(sc.Bytes())
		count++
		if count >= 500 || batch.Len() > 8<<20 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}

// ---------------------------------------------------------------------------
// Reconcile
// ---------------------------------------------------------------------------

// PortableReconcile is the result of comparing a live client with an export.
type PortableReconcile struct {
	TenantID    string   `json:"tenant_id"`
	Tables      int      `json:"tables_checked"`
	Rows        int64    `json:"rows_checked"`
	OK          bool     `json:"ok"`
	Differences []string `json:"differences,omitempty"`
}

// ReconcileTenantPortable recounts and rehashes every exported table in the
// client and compares both with the manifest, and re-reads each file from disk
// against its manifest hash. It reports every difference and sets OK only when
// there are none. It writes nothing.
func ReconcileTenantPortable(dir, tenantID string) (*PortableReconcile, error) {
	m, err := ReadPortableManifest(dir)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return nil, err
	}
	shape, err := portableSchemaShape(tx, schema)
	if err != nil {
		return nil, err
	}
	rep := &PortableReconcile{TenantID: tenantID}
	for _, t := range m.Tables {
		live, ok := shape[t.Name]
		if !ok {
			rep.Differences = append(rep.Differences, fmt.Sprintf("table %s is missing from the client", t.Name))
			continue
		}
		rep.Tables++
		n, sum, err := portableDump(tx, schema, t.Name, append(append([]string{}, t.Generated...), portableExtraColumns(live, t)...), nil)
		if err != nil {
			return nil, err
		}
		rep.Rows += n
		if n != t.Rows {
			rep.Differences = append(rep.Differences, fmt.Sprintf("%s: %d rows in the client, %d in the export", t.Name, n, t.Rows))
		} else if sum != t.SHA256 {
			rep.Differences = append(rep.Differences, fmt.Sprintf("%s: same row count (%d) but different content", t.Name, n))
		}
	}
	for _, s := range m.Sequences {
		var last sql.NullInt64
		err := tx.QueryRow(`SELECT last_value FROM pg_sequences WHERE schemaname = $1 AND sequencename = $2`, schema, s.Name).Scan(&last)
		if err == sql.ErrNoRows {
			continue // a sequence the new version no longer has
		}
		if err != nil {
			return nil, err
		}
		if !last.Valid || last.Int64 < s.LastValue {
			rep.Differences = append(rep.Differences, fmt.Sprintf("sequence %s is behind the export (%d < %d): new numbers could repeat", s.Name, last.Int64, s.LastValue))
		}
	}
	rep.OK = len(rep.Differences) == 0
	return rep, nil
}
