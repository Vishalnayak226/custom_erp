package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"custom_erp/engines"
	"github.com/lib/pq"
)

// RegistrySnapshot contains structural metadata only: never document rows,
// column defaults, connection strings, users, grants or configured secrets.
type RegistrySnapshot struct {
	SchemaVersion int               `json:"schema_version"`
	CapturedOn    string            `json:"captured_on"`
	Environment   string            `json:"environment"`
	TenantSchema  string            `json:"tenant_schema"`
	Scope         string            `json:"scope"`
	Doctypes      []RegistryDoctype `json:"doctypes"`
	Fields        []RegistryField   `json:"fields"`
	Columns       []RegistryColumn  `json:"columns"`
	Keys          []RegistryKey     `json:"keys"`
}
type RegistryDoctype struct {
	Name         string `json:"name"`
	Module       string `json:"module"`
	DocumentType string `json:"document_type"`
}
type RegistryField struct {
	Doctype    string `json:"doctype"`
	Name       string `json:"name"`
	Label      string `json:"label"`
	Type       string `json:"type"`
	Required   bool   `json:"required"`
	LinkTarget string `json:"link_target,omitempty"`
	Order      int    `json:"order"`
}
type RegistryColumn struct {
	Table    string `json:"table"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}
type RegistryKey struct {
	Table        string `json:"table"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Column       string `json:"column"`
	Position     int    `json:"position"`
	TargetTable  string `json:"target_table,omitempty"`
	TargetColumn string `json:"target_column,omitempty"`
}

func captureRegistry(conn, tenant, environment, stamp string) ([]byte, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(tenant) || !regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(environment) {
		return nil, fmt.Errorf("invalid explicit registry scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := sql.Open("postgres", conn)
	if err != nil {
		return nil, fmt.Errorf("registry connection failed")
	}
	defer database.Close()
	tx, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("read-only registry transaction failed")
	}
	defer tx.Rollback()
	snapshot := RegistrySnapshot{SchemaVersion: 1, CapturedOn: stamp, Environment: environment, TenantSchema: tenant,
		Scope: "Metadata-only development snapshot; no business rows, defaults, credentials or production assurance."}
	// Quote identifiers even after validation. All other scope values are parameters.
	queries := []struct {
		query  string
		args   []any
		target any
	}{
		{`SELECT COALESCE(json_agg(x ORDER BY name), '[]') FROM (SELECT name, COALESCE(module,'') AS module, COALESCE(document_type,'') AS document_type FROM ` + pq.QuoteIdentifier(tenant) + `.doctype_meta) x`, nil, &snapshot.Doctypes},
		{`SELECT COALESCE(json_agg(x ORDER BY doctype, "order", name), '[]') FROM (SELECT doctype_name AS doctype, fieldname AS name, COALESCE(label,'') AS label, fieldtype AS type, COALESCE(mandatory,false) AS required, CASE WHEN fieldtype IN ('Link','Table') THEN COALESCE(options,'') ELSE '' END AS link_target, COALESCE(display_order,0) AS "order" FROM ` + pq.QuoteIdentifier(tenant) + `.doctype_fields) x`, nil, &snapshot.Fields},
		{`SELECT COALESCE(json_agg(x ORDER BY "table", name), '[]') FROM (SELECT table_name AS "table", column_name AS name, udt_name AS type, is_nullable='YES' AS nullable FROM information_schema.columns WHERE table_schema=$1) x`, []any{tenant}, &snapshot.Columns},
		{`SELECT COALESCE(json_agg(x ORDER BY "table", name, position), '[]') FROM (SELECT k.table_name AS "table", k.constraint_name AS name, c.constraint_type AS type, k.column_name AS "column", k.ordinal_position AS position, COALESCE(f.table_name,'') AS target_table, COALESCE(f.column_name,'') AS target_column FROM information_schema.key_column_usage k JOIN information_schema.table_constraints c ON (c.constraint_schema,c.constraint_name,c.table_name)=(k.constraint_schema,k.constraint_name,k.table_name) LEFT JOIN information_schema.referential_constraints r ON (r.constraint_schema,r.constraint_name)=(k.constraint_schema,k.constraint_name) LEFT JOIN information_schema.key_column_usage f ON (f.constraint_schema,f.constraint_name,f.ordinal_position)=(r.unique_constraint_schema,r.unique_constraint_name,k.position_in_unique_constraint) WHERE k.table_schema=$1) x`, []any{tenant}, &snapshot.Keys},
	}
	for i, q := range queries {
		var raw []byte
		if err := tx.QueryRowContext(ctx, q.query, q.args...).Scan(&raw); err != nil {
			return nil, fmt.Errorf("registry metadata query %d failed; no outputs written", i+1)
		}
		if err := json.Unmarshal(raw, q.target); err != nil {
			return nil, err
		}
	}
	if err := validateRegistry(snapshot); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("registry read transaction failed")
	}
	body, err := json.MarshalIndent(snapshot, "", "  ")
	return append(body, '\n'), err
}

func validateRegistry(s RegistrySnapshot) error {
	if s.SchemaVersion != 1 || s.Environment == "" || s.TenantSchema == "" || len(s.Doctypes) == 0 || len(s.Columns) == 0 {
		return fmt.Errorf("incomplete registry snapshot")
	}
	if _, err := time.Parse("2006-01-02", s.CapturedOn); err != nil {
		return err
	}
	names := map[string]bool{}
	for _, d := range s.Doctypes {
		if d.Name == "" || names[d.Name] {
			return fmt.Errorf("empty or duplicate doctype %q", d.Name)
		}
		names[d.Name] = true
	}
	fields := map[string]bool{}
	for _, f := range s.Fields {
		key := f.Doctype + "." + f.Name
		if !names[f.Doctype] || f.Name == "" || f.Type == "" || fields[key] {
			return fmt.Errorf("invalid or duplicate registry field %q", key)
		}
		fields[key] = true
	}
	return nil
}

type DictionaryDefinitions struct {
	Status            string            `json:"status"`
	Owner             string            `json:"owner"`
	ReviewBy          string            `json:"review_by"`
	DefaultDefinition string            `json:"default_definition"`
	Domains           map[string]string `json:"domains"`
	Definitions       map[string]string `json:"definitions"`
	KeyPolicy         string            `json:"key_policy"`
	TenantScope       string            `json:"tenant_scope"`
	Retention         string            `json:"retention"`
	Classification    string            `json:"classification"`
}

func dictionaryFiles(root, stamp string) (map[string][]byte, error) {
	raw, err := os.ReadFile(filepath.Join(root, "docs/data/registry-snapshot.json"))
	if err != nil {
		return nil, err
	}
	var snapshot RegistrySnapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	if err = validateRegistry(snapshot); err != nil {
		return nil, err
	}
	definitionsRaw, err := os.ReadFile(filepath.Join(root, "docs/data/business-definitions.json"))
	if err != nil {
		return nil, err
	}
	var definitions DictionaryDefinitions
	if err = json.Unmarshal(definitionsRaw, &definitions); err != nil {
		return nil, err
	}
	if definitions.Owner == "" || definitions.Status != "draft" || definitions.DefaultDefinition == "" || definitions.Retention == "" {
		return nil, fmt.Errorf("business definitions require explicit draft stewardship and retention policy")
	}
	if _, err = time.Parse("2006-01-02", definitions.ReviewBy); err != nil {
		return nil, err
	}
	known := map[string]bool{}
	counts := map[string]int{}
	for _, d := range snapshot.Doctypes {
		known[d.Name] = true
	}
	for name := range definitions.Definitions {
		if !known[name] {
			return nil, fmt.Errorf("definition references unknown doctype %s", name)
		}
	}
	for _, f := range snapshot.Fields {
		counts[f.Doctype]++
	}
	sensitive := map[string][]engines.SensitiveFieldAccess{}
	for _, doctype := range engines.SensitiveFieldDoctypes() {
		sensitive[doctype] = engines.ExplainSensitiveFields("", doctype)
	}
	sum := sha256.Sum256(raw)
	dictionary := struct {
		GeneratedOn     string                                    `json:"generated_on"`
		SnapshotSHA256  string                                    `json:"snapshot_sha256"`
		Registry        RegistrySnapshot                          `json:"registry"`
		Business        DictionaryDefinitions                     `json:"business"`
		SensitiveFields map[string][]engines.SensitiveFieldAccess `json:"sensitive_fields"`
		Reports         []engines.ReportDefinition                `json:"reports"`
	}{stamp, hex.EncodeToString(sum[:]), snapshot, definitions, sensitive, engines.ListReportDefinitions()}
	body, err := json.MarshalIndent(dictionary, "", "  ")
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(generatedHeader("Data dictionary", stamp, "`docs/data/registry-snapshot.json`, `business-definitions.json`, sensitive-field and report registries", "`pwsh docs/update-docs.ps1 -Group Content`"))
	fmt.Fprintf(&b, "## Scope and provenance\n\nMetadata captured **%s**, environment **%s**, schema **%s**. Snapshot SHA-256: `%s`. This is a development structure snapshot, not a claim that any deployed tenant has this schema.\n\n[Machine-readable dictionary](dictionary.json) contains every captured field, type, required flag, relationship, physical column/key, sensitive-field policy and report definition. [Capture procedure](../dictionary-workflow.md) explains refresh and review. Field labels describe the configured form; source validation and database constraints remain authoritative. No customer records or default values are exported.\n\n", snapshot.CapturedOn, cell(snapshot.Environment), cell(snapshot.TenantSchema), hex.EncodeToString(sum[:]))
	fmt.Fprintf(&b, "## Stewardship and interpretation\n\nOwner: **%s**. Business definitions are **%s**, review due **%s**.\n\n- Key policy: %s\n- Tenant scope: %s\n- Retention: %s\n- Classification: %s\n\n## Document types\n\n| Document type | Module | Kind | Fields | Business meaning |\n|---|---|---|---:|---|\n", cell(definitions.Owner), cell(definitions.Status), cell(definitions.ReviewBy), definitions.KeyPolicy, definitions.TenantScope, definitions.Retention, definitions.Classification)
	for _, d := range snapshot.Doctypes {
		meaning := definitions.Definitions[d.Name]
		if meaning == "" {
			meaning = definitions.Domains[d.Module]
		}
		if meaning == "" {
			meaning = definitions.DefaultDefinition
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %s |\n", cell(d.Name), cell(d.Module), cell(d.DocumentType), counts[d.Name], cell(meaning))
	}
	fmt.Fprintf(&b, "\n## Completeness\n\n%d document types, %d configured fields, %d physical columns and %d key-column memberships were captured. Link targets describe configured relations; not every JSON document relation is a physical foreign key. Unlisted sensitive fields are **unclassified**, not automatically public. Review business definitions and retention before customer use. Report labels, parameters and columns come from the source report registry in the JSON projection.\n", len(snapshot.Doctypes), len(snapshot.Fields), len(snapshot.Columns), len(snapshot.Keys))
	return map[string][]byte{"docs/data/generated/dictionary.json": append(body, '\n'), "docs/data/generated/dictionary.md": []byte(b.String())}, nil
}
