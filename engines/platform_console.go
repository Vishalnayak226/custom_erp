package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/lib/pq"
)

// Platform data console (Stage 60.6). The user, 2026-10-14: "if any database
// fix is needed, I need all tables and all config access and I will be able
// to fix it - in any client - with customer details hidden." They chose raw
// editing of every table over a narrower tool.
//
// Who may use it. Being a Super Admin is NOT enough: every client has its own
// Super Admin, and a console that reads and writes any client's tables must
// not open to all of them. An operator is a (tenant, username) pair listed in
// the server's ERP_PLATFORM_OPERATORS environment variable, e.g.
// "default:admin,default:vishal". The variable is set on the server, never
// through the app, so no client user can grant it to themselves. Super Admin
// sessions are already MFA-gated at login (RequiresMFA).
//
// What it hides. Passwords, secrets, tokens, credentials and recovery codes
// are never shown and cannot be edited. Customer personal details (phone,
// email, address, PAN/Aadhaar, date of birth, bank details, and a Customer's
// name) are masked in every table and inside every JSON data column, and keep
// their stored value on save. Search covers key columns only, so masked values
// cannot be probed for one character at a time.
//
// What it refuses to change. Primary keys (a row's identity), masked columns,
// and the audit tables, whose value is that nobody - including this console -
// can rewrite them. Every edit writes its before/after (masked) to the client's
// own Activity Log and to the operator's.

// MaskedValue stands in for a hidden value.
const MaskedValue = "••••••"

const platformOperatorsEnv = "ERP_PLATFORM_OPERATORS"

var (
	secretColumnRe = regexp.MustCompile(`(?i)(password|secret|token|otp|hash|api_key|apikey|private|credential|recovery|salt|card_number)`)
	piiKeyRe       = regexp.MustCompile(`(?i)((^|_)(phone|mobile|email|address|pincode|pan|aadhaar|aadhar|dob|birth|account_number|ifsc|upi)($|_)|^customer_name$)`)
	customerNameRe = regexp.MustCompile(`(?i)^(name|first_name|last_name|full_name|customer_name)$`)
	identRe        = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
)

// platformSecretTable is a table whose every non-key column is a secret.
func platformSecretTable(table string) bool {
	return strings.Contains(table, "credential") || table == "mfa_recovery_codes" ||
		strings.Contains(table, "otp_challenge")
}

// platformReadOnlyTable is a table the console shows but never changes.
// Stage 72.8: gift-card balances and their ledger too - a stored-value
// balance only ever changes through an appended ledger event (blueprint 11.4).
func platformReadOnlyTable(table string) bool {
	return strings.HasPrefix(table, "audit_") || table == "system_error_logs" || platformSecretTable(table) ||
		strings.HasPrefix(table, "stored_value_")
}

// IsPlatformOperator reports whether this session may use the console.
func IsPlatformOperator(tenantID, username, role string) bool {
	if !IsSuperAdmin(role) || strings.TrimSpace(username) == "" {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(tenantID) + ":" + strings.TrimSpace(username))
	for _, entry := range strings.Split(os.Getenv(platformOperatorsEnv), ",") {
		if strings.ToLower(strings.TrimSpace(entry)) == want {
			return true
		}
	}
	return false
}

// IsPlatformTenant reports whether a client is the operator's own (Stage
// 125): a tenant named in ERP_PLATFORM_OPERATORS, or "default" when none is
// set. Only there may a Super Admin user be created; every other client's
// top account is Admin.
func IsPlatformTenant(tenantID string) bool {
	tenantID = strings.ToLower(strings.TrimSpace(tenantID))
	if tenantID == "" {
		tenantID = "default"
	}
	named := false
	for _, entry := range strings.Split(os.Getenv(platformOperatorsEnv), ",") {
		t, _, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok {
			continue
		}
		named = true
		if strings.ToLower(strings.TrimSpace(t)) == tenantID {
			return true
		}
	}
	return !named && tenantID == "default"
}

// PlatformTenant is one client in the registry.
type PlatformTenant struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
}

// PlatformTenants lists every client.
func PlatformTenants() ([]PlatformTenant, error) {
	rows, err := db.DB.Query(`SELECT tenant_id, COALESCE(name, tenant_id), COALESCE(lifecycle_status, '') FROM public.tenants ORDER BY tenant_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlatformTenant{}
	for rows.Next() {
		var t PlatformTenant
		if err := rows.Scan(&t.TenantID, &t.Name, &t.Status); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PlatformColumn describes one column of a table.
type PlatformColumn struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Options  string `json:"options,omitempty"`
	Type     string `json:"type"`
	Key      bool   `json:"key"`
	Masked   bool   `json:"masked"`
	Editable bool   `json:"editable"`
}

// PlatformTable is one table of a client's schema.
type PlatformTable struct {
	Name     string `json:"name"`
	Rows     int64  `json:"rows"`
	ReadOnly bool   `json:"read_only"`
	HasKey   bool   `json:"has_key"`
}

// PlatformTables lists a client's tables with an approximate row count.
func PlatformTables(tenantID string) ([]PlatformTable, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(`
		SELECT c.relname, GREATEST(c.reltuples, 0)::bigint,
		       EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = c.oid AND i.indisprimary)
		  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = $1 AND c.relkind IN ('r', 'p')
		 ORDER BY c.relname`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlatformTable{}
	for rows.Next() {
		var t PlatformTable
		if err := rows.Scan(&t.Name, &t.Rows, &t.HasKey); err != nil {
			return nil, err
		}
		t.ReadOnly = platformReadOnlyTable(t.Name) || !t.HasKey
		out = append(out, t)
	}
	return out, rows.Err()
}

// platformColumns reads a table's columns in order, with masking/edit flags.
// An unknown table returns an error, which is also what keeps every later
// identifier in this file to names the catalog itself produced.
func platformColumns(schema, table string) ([]PlatformColumn, error) {
	if !identRe.MatchString(table) {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: "unknown table"}
	}
	rows, err := db.DB.Query(`
		SELECT a.attname, format_type(a.atttypid, a.atttypmod),
		       COALESCE(a.attnum = ANY(i.indkey::int2[]), FALSE)
		  FROM pg_attribute a
		  JOIN pg_class c ON c.oid = a.attrelid
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		  LEFT JOIN pg_index i ON i.indrelid = c.oid AND i.indisprimary
		 WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
		 ORDER BY a.attnum`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	readOnly := platformReadOnlyTable(table)
	secretTable := platformSecretTable(table)
	cols := []PlatformColumn{}
	for rows.Next() {
		var c PlatformColumn
		if err := rows.Scan(&c.Name, &c.Type, &c.Key); err != nil {
			return nil, err
		}
		c.Masked = !c.Key && (secretTable || secretColumnRe.MatchString(c.Name) || piiKeyRe.MatchString(c.Name))
		c.Editable = !readOnly && !c.Key && !c.Masked
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("table %q does not exist in this client", table)}
	}
	hasKey := false
	for _, c := range cols {
		hasKey = hasKey || c.Key
	}
	if !hasKey {
		for i := range cols {
			cols[i].Editable = false
		}
	}
	return cols, nil
}

// maskJSONValue masks personal keys inside a JSON object (recursively). A
// Customer document also hides its name.
func maskJSONValue(v interface{}, customer bool) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			if piiKeyRe.MatchString(k) || secretColumnRe.MatchString(k) || (customer && customerNameRe.MatchString(k)) {
				if val == nil || val == "" {
					out[k] = val
				} else {
					out[k] = MaskedValue
				}
				continue
			}
			out[k] = maskJSONValue(val, customer)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, val := range t {
			out[i] = maskJSONValue(val, customer)
		}
		return out
	}
	return v
}

// maskRow applies column masking and JSON-key masking to one row.
func maskRow(cols []PlatformColumn, row map[string]interface{}) map[string]interface{} {
	customer := false
	if d, ok := row["doctype"].(string); ok && d == "Customer" {
		customer = true
	}
	out := make(map[string]interface{}, len(row))
	for _, c := range cols {
		val := row[c.Name]
		switch {
		case c.Masked && val != nil && val != "":
			out[c.Name] = MaskedValue
		case strings.HasPrefix(c.Type, "json"):
			out[c.Name] = maskJSONValue(val, customer)
		default:
			out[c.Name] = val
		}
	}
	return out
}

// PlatformRowPage is one page of a table.
type PlatformRowPage struct {
	Table    string                   `json:"table"`
	Columns  []PlatformColumn         `json:"columns"`
	Rows     []map[string]interface{} `json:"rows"`
	Total    int64                    `json:"total"`
	ReadOnly bool                     `json:"read_only"`
}

// PlatformRowFilter narrows a page. Doctype applies to tables with a doctype
// column (documents); Q matches key columns only (see the file comment).
type PlatformRowFilter struct {
	Doctype string
	Q       string
	Limit   int
	Offset  int
}

// PlatformRows reads one page of a client's table, masked.
func PlatformRows(tenantID, table string, f PlatformRowFilter) (*PlatformRowPage, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	cols, err := platformColumns(schema, table)
	if err != nil {
		return nil, err
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	where, args := []string{}, []interface{}{}
	keyCols, hasDoctype := []string{}, false
	for _, c := range cols {
		if c.Key {
			keyCols = append(keyCols, pq.QuoteIdentifier(c.Name))
		}
		if c.Name == "doctype" {
			hasDoctype = true
		}
	}
	if hasDoctype && strings.TrimSpace(f.Doctype) != "" {
		args = append(args, strings.TrimSpace(f.Doctype))
		where = append(where, fmt.Sprintf("t.doctype = $%d", len(args)))
	}
	if q := strings.TrimSpace(f.Q); q != "" && len(keyCols) > 0 {
		args = append(args, "%"+q+"%")
		parts := make([]string, len(keyCols))
		for i, k := range keyCols {
			parts[i] = fmt.Sprintf("t.%s::text ILIKE $%d", k, len(args))
		}
		where = append(where, "("+strings.Join(parts, " OR ")+")")
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}
	from := fmt.Sprintf("%s.%s t", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(table))

	page := &PlatformRowPage{Table: table, Columns: cols, Rows: []map[string]interface{}{}, ReadOnly: platformReadOnlyTable(table)}
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+from+whereSQL, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	order := ""
	if len(keyCols) > 0 {
		order = " ORDER BY " + strings.Join(prefixAll("t.", keyCols), ", ")
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := db.DB.Query(fmt.Sprintf("SELECT to_jsonb(t.*)::text FROM %s%s%s LIMIT $%d OFFSET $%d", from, whereSQL, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var row map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			return nil, err
		}
		page.Rows = append(page.Rows, maskRow(cols, row))
	}
	return page, rows.Err()
}

func prefixAll(prefix string, in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = prefix + s
	}
	return out
}

// PlatformEditResult is what an edit changed.
type PlatformEditResult struct {
	Before map[string]interface{} `json:"before"`
	After  map[string]interface{} `json:"after"`
}

// PlatformUpdateRow changes one row, found by its full primary key. set maps a
// column to its new value as text (nil = NULL); the database casts it to the
// column's type, so a bad value is refused by Postgres itself. A JSON column
// keeps every masked key's stored value whatever the caller sent.
func PlatformUpdateRow(tenantID, table string, key map[string]string, set map[string]*string, actorTenant, actor string) (*PlatformEditResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	cols, err := platformColumns(schema, table)
	if err != nil {
		return nil, err
	}
	if platformReadOnlyTable(table) {
		return nil, &ValidationError{Code: "PLATFORM-0316", Message: fmt.Sprintf("table %s is read-only in the console (audit trail or secrets)", table)}
	}
	byName := map[string]PlatformColumn{}
	keyNames := []string{}
	for _, c := range cols {
		byName[c.Name] = c
		if c.Key {
			keyNames = append(keyNames, c.Name)
		}
	}
	if len(keyNames) == 0 {
		return nil, &ValidationError{Code: "PLATFORM-0316", Message: fmt.Sprintf("table %s has no primary key, so a single row cannot be addressed safely", table)}
	}
	if len(key) != len(keyNames) {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("give every key column (%s) to choose the row", strings.Join(keyNames, ", "))}
	}
	if len(set) == 0 {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: "nothing to change"}
	}
	setNames := make([]string, 0, len(set))
	for name := range set {
		c, ok := byName[name]
		if !ok {
			return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("column %s does not exist on %s", name, table)}
		}
		if !c.Editable {
			return nil, &ValidationError{Code: "PLATFORM-0316", Message: fmt.Sprintf("column %s is a key, hidden or read-only and cannot be changed here", name)}
		}
		setNames = append(setNames, name)
	}
	sort.Strings(setNames)

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	from := fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(table))
	keyArgs := []interface{}{}
	keyConds := []string{}
	for _, k := range keyNames {
		v, ok := key[k]
		if !ok {
			return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("key column %s is missing", k)}
		}
		keyArgs = append(keyArgs, v)
		keyConds = append(keyConds, fmt.Sprintf("t.%s::text = $%d", pq.QuoteIdentifier(k), len(keyArgs)))
	}
	keySQL := strings.Join(keyConds, " AND ")

	var beforeRaw string
	if err := tx.QueryRow(fmt.Sprintf("SELECT to_jsonb(t.*)::text FROM %s t WHERE %s FOR UPDATE", from, keySQL), keyArgs...).Scan(&beforeRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &ValidationError{Code: "GLOBAL-0002", Message: "that row no longer exists"}
		}
		return nil, err
	}
	var before map[string]interface{}
	if err := json.Unmarshal([]byte(beforeRaw), &before); err != nil {
		return nil, err
	}
	customer := before["doctype"] == "Customer"

	args := append([]interface{}{}, keyArgs...)
	assigns := []string{}
	for _, name := range setNames {
		c := byName[name]
		val := set[name]
		if val != nil && strings.HasPrefix(c.Type, "json") {
			merged, errM := keepMaskedJSON(*val, before[name], customer)
			if errM != nil {
				return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("%s must be valid JSON: %v", name, errM)}
			}
			val = &merged
		}
		if val == nil {
			assigns = append(assigns, fmt.Sprintf("%s = NULL", pq.QuoteIdentifier(name)))
			continue
		}
		args = append(args, *val)
		assigns = append(assigns, fmt.Sprintf("%s = CAST($%d AS %s)", pq.QuoteIdentifier(name), len(args), c.Type))
	}
	res, err := tx.Exec(fmt.Sprintf("UPDATE %s t SET %s WHERE %s", from, strings.Join(assigns, ", "), keySQL), args...)
	if err != nil {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("the database refused the change: %v", err)}
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, fmt.Errorf("expected to change 1 row, changed %d", n)
	}
	var afterRaw string
	if err := tx.QueryRow(fmt.Sprintf("SELECT to_jsonb(t.*)::text FROM %s t WHERE %s", from, keySQL), keyArgs...).Scan(&afterRaw); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	var after map[string]interface{}
	_ = json.Unmarshal([]byte(afterRaw), &after)

	result := &PlatformEditResult{Before: maskRow(cols, before), After: maskRow(cols, after)}
	changedBefore, changedAfter := map[string]interface{}{}, map[string]interface{}{}
	for _, name := range setNames {
		changedBefore[name], changedAfter[name] = result.Before[name], result.After[name]
	}
	detail, _ := json.Marshal(map[string]interface{}{
		"table": table, "key": key, "columns": setNames,
		"before": changedBefore, "after": changedAfter,
		"operator": actorTenant + ":" + actor,
	})
	LogAuditEvent(tenantID, actor, "PLATFORM_DATA_EDIT", "SUCCESS", string(detail))
	if actorTenant != tenantID {
		LogAuditEvent(actorTenant, actor, "PLATFORM_DATA_EDIT", "SUCCESS", fmt.Sprintf("client %s: %s", tenantID, detail))
	}
	return result, nil
}

// keepMaskedJSON parses the caller's JSON and puts back every masked key's
// stored value, so a hidden field can neither be read nor overwritten (the
// caller only ever saw MaskedValue there).
func keepMaskedJSON(newText string, stored interface{}, customer bool) (string, error) {
	var incoming interface{}
	if err := json.Unmarshal([]byte(newText), &incoming); err != nil {
		return "", err
	}
	merged := restoreMasked(incoming, stored, customer)
	// Anything still reading as the mask was hidden somewhere this merge could
	// not line up (a list reordered, a key renamed). Saving it would write the
	// mask over real data, so the edit is refused instead.
	if containsMasked(merged) {
		return "", fmt.Errorf("a hidden value was moved or changed - edit the visible fields only")
	}
	out, err := json.Marshal(merged)
	return string(out), err
}

func containsMasked(v interface{}) bool {
	switch t := v.(type) {
	case string:
		return t == MaskedValue
	case map[string]interface{}:
		for _, val := range t {
			if containsMasked(val) {
				return true
			}
		}
	case []interface{}:
		for _, val := range t {
			if containsMasked(val) {
				return true
			}
		}
	}
	return false
}

func restoreMasked(incoming, stored interface{}, customer bool) interface{} {
	if inList, ok := incoming.([]interface{}); ok {
		stList, _ := stored.([]interface{})
		if len(stList) != len(inList) {
			return incoming
		}
		out := make([]interface{}, len(inList))
		for i := range inList {
			out[i] = restoreMasked(inList[i], stList[i], customer)
		}
		return out
	}
	in, okIn := incoming.(map[string]interface{})
	st, okSt := stored.(map[string]interface{})
	if !okIn {
		return incoming
	}
	if !okSt {
		st = map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	for k, sv := range st {
		if piiKeyRe.MatchString(k) || secretColumnRe.MatchString(k) || (customer && customerNameRe.MatchString(k)) {
			out[k] = sv
			continue
		}
		if iv, ok := out[k]; ok {
			out[k] = restoreMasked(iv, sv, customer)
		}
	}
	return out
}

// --- Record tables (Stage 60.6, the user: "I need an individual table for
// every transaction, and to fix any record that goes wrong") ----------------
//
// Every PO, GRN, Vendor, Item, Store... is a row of the one documents table,
// told apart by doctype, with its fields inside a JSON column. Splitting that
// into one physical table per record type would move every client's live data
// and every module's queries, so instead the console presents each record type
// AS its own table: one column per field (from doctype_fields, so a field
// added on Database Schema Design appears here too), plus the row's id, status
// and timestamps. Edits write back into that record's JSON field by field,
// typed by the field's definition, with the same masking and audit as the raw
// tables. Keys a record carries that its record type no longer defines are
// shown under "other_data" rather than hidden.

// PlatformRecordType is one record type with its row count.
type PlatformRecordType struct {
	Doctype string `json:"doctype"`
	Module  string `json:"module"`
	Rows    int64  `json:"rows"`
}

// PlatformRecordTypes lists a client's record types, each as a table.
func PlatformRecordTypes(tenantID string) ([]PlatformRecordType, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT m.name, COALESCE(m.module, ''), COALESCE(c.n, 0)
		  FROM %[1]s.doctype_meta m
		  LEFT JOIN (SELECT doctype, COUNT(*) AS n FROM %[1]s.documents GROUP BY doctype) c ON c.doctype = m.name
		 ORDER BY m.module, m.name`, pq.QuoteIdentifier(schema)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlatformRecordType{}
	for rows.Next() {
		var t PlatformRecordType
		if err := rows.Scan(&t.Doctype, &t.Module, &t.Rows); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// recordSystemColumns are the documents columns shown beside a record's fields.
var recordSystemColumns = []PlatformColumn{
	{Name: "id", Type: "key", Key: true},
	{Name: "status", Type: "status", Editable: true},
	{Name: "created_by", Type: "text"},
	{Name: "created_at", Type: "timestamp"},
	{Name: "updated_at", Type: "timestamp"},
	{Name: "deleted_at", Type: "timestamp"},
}

func recordFieldMasked(doctype, field string) bool {
	return piiKeyRe.MatchString(field) || secretColumnRe.MatchString(field) ||
		(doctype == "Customer" && customerNameRe.MatchString(field))
}

// recordColumns is a record type's table shape: system columns, one column per
// defined field (Type = the field type, Label = its label), then other_data.
func recordColumns(tenantID, doctype string) ([]PlatformColumn, map[string]FieldMeta, error) {
	fields, err := GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		return nil, nil, err
	}
	if len(fields) == 0 {
		return nil, nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("record type %q has no fields in this client", doctype)}
	}
	cols := append([]PlatformColumn{}, recordSystemColumns...)
	byName := map[string]FieldMeta{}
	for _, f := range fields {
		byName[f.Fieldname] = f
		if f.Fieldname == "id" || f.Fieldname == "status" {
			continue
		}
		masked := recordFieldMasked(doctype, f.Fieldname)
		cols = append(cols, PlatformColumn{Name: f.Fieldname, Label: f.Label, Type: f.Fieldtype, Options: f.Options, Masked: masked, Editable: !masked})
	}
	cols = append(cols, PlatformColumn{Name: "other_data", Label: "Other data (not a defined field)", Type: "json"})
	return cols, byName, nil
}

// PlatformRecords reads one page of a record type as a table.
func PlatformRecords(tenantID, doctype string, f PlatformRowFilter) (*PlatformRowPage, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	cols, byName, err := recordColumns(tenantID, doctype)
	if err != nil {
		return nil, err
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	customer := doctype == "Customer"
	where, args := "doctype = $1", []interface{}{doctype}
	if q := strings.TrimSpace(f.Q); q != "" {
		// The record's number/code and, except for Customers, its name - never
		// a hidden field, so masked values cannot be found by searching.
		args = append(args, "%"+q+"%")
		cond := "id ILIKE $2 OR data->>'code' ILIKE $2"
		if !customer {
			cond += " OR data->>'name' ILIKE $2"
		}
		where += " AND (" + cond + ")"
	}
	page := &PlatformRowPage{Table: doctype, Columns: cols, Rows: []map[string]interface{}{}}
	from := pq.QuoteIdentifier(schema) + ".documents"
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+from+" WHERE "+where, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(status, ''), COALESCE(created_by, ''), COALESCE(created_at::text, ''),
		COALESCE(updated_at::text, ''), COALESCE(deleted_at::text, ''), data::text
		FROM %s WHERE %s ORDER BY created_at DESC, id LIMIT $%d OFFSET $%d`, from, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, status, by, created, updated, deleted, raw string
		if err := rows.Scan(&id, &status, &by, &created, &updated, &deleted, &raw); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &data)
		row := map[string]interface{}{"id": id, "status": status, "created_by": by, "created_at": created, "updated_at": updated, "deleted_at": deleted}
		other := map[string]interface{}{}
		for k, v := range data {
			if k == "id" || k == "status" {
				continue
			}
			if _, defined := byName[k]; !defined {
				other[k] = v
				continue
			}
			if recordFieldMasked(doctype, k) && v != nil && v != "" {
				row[k] = MaskedValue
			} else {
				row[k] = maskJSONValue(v, customer)
			}
		}
		row["other_data"] = maskJSONValue(other, customer)
		page.Rows = append(page.Rows, row)
	}
	return page, rows.Err()
}

// recordValue converts the console's text for one field into what that field
// stores: a number, a true/false, parsed JSON for table/map fields, else text.
// keep=false (the "empty" tick, or a blank number) removes the key.
func recordValue(f FieldMeta, text *string) (val interface{}, keep bool, err error) {
	if text == nil {
		return nil, false, nil
	}
	v := strings.TrimSpace(*text)
	switch f.Fieldtype {
	case "Number", "Currency", "Float", "Int":
		if v == "" {
			return nil, false, nil
		}
		var n float64
		if _, err := fmt.Sscanf(v, "%g", &n); err != nil {
			return nil, false, fmt.Errorf("%s must be a number", f.Label)
		}
		return n, true, nil
	case "Check":
		switch strings.ToLower(v) {
		case "true", "yes", "1":
			return true, true, nil
		case "false", "no", "0", "":
			return false, true, nil
		}
		return nil, false, fmt.Errorf("%s must be yes or no", f.Label)
	case "JSONTable", "JSONMap":
		if v == "" {
			return nil, false, nil
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return nil, false, fmt.Errorf("%s must be valid JSON: %v", f.Label, err)
		}
		return parsed, true, nil
	}
	return *text, true, nil
}

// PlatformUpdateRecord changes one record's fields (and optionally its
// status), writing each into the record's JSON with its field's type. Hidden
// fields are refused; a JSON-valued field keeps its hidden keys' stored values.
func PlatformUpdateRecord(tenantID, doctype, id string, set map[string]*string, actorTenant, actor string) (*PlatformEditResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	_, byName, err := recordColumns(tenantID, doctype)
	if err != nil {
		return nil, err
	}
	if len(set) == 0 {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: "nothing to change"}
	}
	customer := doctype == "Customer"
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	from := pq.QuoteIdentifier(schema) + ".documents"
	var raw, status string
	if err := tx.QueryRow("SELECT data::text, COALESCE(status, '') FROM "+from+" WHERE doctype = $1 AND id = $2 FOR UPDATE", doctype, id).Scan(&raw, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("%s %s no longer exists", doctype, id)}
		}
		return nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil || data == nil {
		data = map[string]interface{}{}
	}
	before, after := map[string]interface{}{}, map[string]interface{}{}
	newStatus := status
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		text := set[name]
		if name == "status" {
			if text == nil || strings.TrimSpace(*text) == "" {
				return nil, &ValidationError{Code: "GLOBAL-0002", Message: "status cannot be empty"}
			}
			newStatus = strings.TrimSpace(*text)
			before["status"], after["status"] = status, newStatus
			if _, ok := data["status"]; ok {
				data["status"] = newStatus
			}
			continue
		}
		f, defined := byName[name]
		if !defined || name == "id" {
			return nil, &ValidationError{Code: "PLATFORM-0316", Message: fmt.Sprintf("%s is not an editable field of %s", name, doctype)}
		}
		if recordFieldMasked(doctype, name) {
			return nil, &ValidationError{Code: "PLATFORM-0316", Message: fmt.Sprintf("%s is hidden and cannot be changed here", f.Label)}
		}
		val, keep, errV := recordValue(f, text)
		if errV != nil {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: name, Message: errV.Error()}
		}
		if keep {
			switch val.(type) {
			case map[string]interface{}, []interface{}:
				val = restoreMasked(val, data[name], customer)
				if containsMasked(val) {
					return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: name, Message: "a hidden value inside " + f.Label + " was moved or changed - edit the visible parts only"}
				}
			}
		}
		before[name] = maskJSONValue(data[name], customer)
		if keep {
			data[name] = val
		} else {
			delete(data, name)
		}
		after[name] = maskJSONValue(data[name], customer)
	}
	newRaw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec("UPDATE "+from+" SET data = $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = $3 AND id = $4",
		string(newRaw), newStatus, doctype, id); err != nil {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("the database refused the change: %v", err)}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	detail, _ := json.Marshal(map[string]interface{}{
		"record_type": doctype, "id": id, "fields": names,
		"before": before, "after": after, "operator": actorTenant + ":" + actor,
	})
	LogAuditEvent(tenantID, actor, "PLATFORM_DATA_EDIT", "SUCCESS", string(detail))
	if actorTenant != tenantID {
		LogAuditEvent(actorTenant, actor, "PLATFORM_DATA_EDIT", "SUCCESS", fmt.Sprintf("client %s: %s", tenantID, detail))
	}
	return &PlatformEditResult{Before: before, After: after}, nil
}
