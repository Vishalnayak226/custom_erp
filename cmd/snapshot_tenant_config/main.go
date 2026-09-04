// Command snapshot_tenant_config is Stage 47.0.3: a read-only export tool
// that dumps, for every tenant schema, its full authorization configuration
// (role -> doctype grant set, field-level permissions, users and the role/
// scope they hold) and every warehouse-owner configuration this schema
// version tracks. It makes NO product behavior change - it writes nothing to
// the database, only SELECTs.
//
// Why this exists ahead of 47.1/47.5: those items rebuild authorization and
// enforce 3PL owner isolation for every tenant, not just tenant_default.
// A migration that reshapes role_permissions/field_permissions/owner config
// needs a "before" snapshot to diff against, to prove to a tenant owner
// exactly what changed, and to roll back if something goes wrong. This tool
// produces that snapshot. It does not itself migrate or repair anything.
//
// Convention for any 47.x migration that touches roles, permissions, scopes
// or warehouse-owner config: snapshot before, apply the migration, snapshot
// after, then diff the two output directories (file-by-file, or by comparing
// each tenant's checksum_sha256 in both runs' MANIFEST.json) before calling
// the migration done. A checksum that is unchanged for a tenant is itself
// evidence that migration left that tenant's configuration untouched.
//
// Usage:
//
//	go run ./cmd/snapshot_tenant_config -out docs/audits/tenant_snapshots/pre-47.1-migration
//	go run ./cmd/snapshot_tenant_config -out docs/audits/tenant_snapshots/post-47.1-migration
//
// Connects via DATABASE_URL exactly like every other cmd/ tool in this repo
// (see db.ConnStringFromEnv) - falls back to the standard local dev Postgres
// on :5435 when unset. Output is one <tenant_id>.json file per tenant plus a
// top-level MANIFEST.json indexing all of them. Snapshot output is evidence,
// not a deliverable, and should not normally be committed to git - each 47.x
// migration session generates its own before/after pair and keeps or
// discards it per that session's own audit trail needs.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"custom_erp/db"
)

// --- row shapes -------------------------------------------------------

type rolePermissionRow struct {
	Role        string `json:"role"`
	DoctypeName string `json:"doctype_name"`
	AllowRead   bool   `json:"allow_read"`
	AllowCreate bool   `json:"allow_create"`
	AllowUpdate bool   `json:"allow_update"`
	AllowDelete bool   `json:"allow_delete"`
}

type fieldPermissionRow struct {
	Role        string `json:"role"`
	DoctypeName string `json:"doctype_name"`
	Fieldname   string `json:"fieldname"`
	AllowRead   bool   `json:"allow_read"`
	AllowWrite  bool   `json:"allow_write"`
}

// userRow deliberately excludes password_hash, mfa_secret/mfa_pending_secret,
// reset_token_hash/expires_at and every other credential/secret column -
// this is a permissions/scope snapshot, not a credential dump.
type userRow struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	Email        *string    `json:"email,omitempty"`
	Role         string     `json:"role"`
	Status       string     `json:"status"`
	LocationCode *string    `json:"location_code,omitempty"`
	MFAEnabled   bool       `json:"mfa_enabled"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
}

type locationRow struct {
	ID          string  `json:"id"`
	Code        *string `json:"code,omitempty"`
	Name        *string `json:"name,omitempty"`
	Type        *string `json:"type,omitempty"`
	LegalEntity *string `json:"legal_entity,omitempty"`
	Status      string  `json:"status"`
}

// binOwnerRow is only emitted for Bin documents that have a non-empty
// owner_id set - see warehouseOwnerConfig.SchemaConceptNote for what that
// field does and does not mean today.
type binOwnerRow struct {
	ID      string  `json:"id"`
	Code    *string `json:"code,omitempty"`
	OwnerID string  `json:"owner_id"`
	Status  string  `json:"status"`
}

type binStockOwnerRow struct {
	BinCode      string    `json:"bin_code"`
	SKU          string    `json:"sku"`
	Condition    string    `json:"condition"`
	OwnerID      string    `json:"owner_id"`
	LocationCode string    `json:"location_code"`
	Qty          int       `json:"qty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// warehouseOwnerConfig captures every place ownership can be configured
// today. SchemaConceptNote records, in plain language, which of these are
// real first-class DB concepts and which are not - so a migration/reader
// never has to reverse-engineer that from the shape of the JSON.
type warehouseOwnerConfig struct {
	SchemaConceptNote string        `json:"schema_concept_note"`
	TotalLocations    int           `json:"total_locations"`
	Locations         []locationRow `json:"locations"`

	TotalBins         int           `json:"total_bins"`
	BinsWithOwnerSet  int           `json:"bins_with_owner_set"`
	BinsWithOwner     []binOwnerRow `json:"bins_with_owner"`

	BinStockOwnerTableExists bool               `json:"bin_stock_owner_table_exists"`
	BinStockOwnerRows        []binStockOwnerRow `json:"bin_stock_owner_rows"`
}

const warehouseOwnerNote = "Warehouse/Location is NOT where ownership lives: the 'Location' doctype " +
	"(tenant_default.documents, doctype='Location') has fields code/name/type/legal_entity/status only " +
	"- no owner concept, first-class or otherwise (see db/migrations_stage17h_location_masters.sql). " +
	"Ownership is tracked one level down: (1) the 'Bin' doctype's optional owner_id field " +
	"(db/migrations_stage26_5_wms_p2.sql, Stage 26.5.15) - a bin is either unowned or wholly one owner's; " +
	"(2) since Stage 42.5.5, the bin_stock_owner breakdown table " +
	"(db/migrations_stage42_5_5_owner_segregation.sql) lets a single bin/SKU/condition be split across " +
	"multiple owners, with Bin.owner_id remaining the fallback attribution for any slice that has no " +
	"explicit bin_stock_owner row (see engines/wms_owner_stock.go's ownerStockQty for the exact combined " +
	"query). This is exactly the gap Stage 47.5 (3PL owner enforcement) is scoped to close - the user " +
	"decided 2026-09-03 to build real mixed-owner support rather than default to single-owner-only."

type rowCounts struct {
	RolePermissions   int `json:"role_permissions"`
	FieldPermissions  int `json:"field_permissions"`
	Users             int `json:"users"`
	Locations         int `json:"locations"`
	Bins              int `json:"bins"`
	BinsWithOwnerSet  int `json:"bins_with_owner_set"`
	BinStockOwnerRows int `json:"bin_stock_owner_rows"`
}

type tenantSnapshot struct {
	TenantID             string               `json:"tenant_id"`
	TenantName           string               `json:"tenant_name"`
	SchemaName           string               `json:"schema_name"`
	SnapshotAt           time.Time            `json:"snapshot_at"`
	DistinctRoles        []string             `json:"distinct_roles"`
	RolePermissions      []rolePermissionRow  `json:"role_permissions"`
	FieldPermissions     []fieldPermissionRow `json:"field_permissions"`
	Users                []userRow            `json:"users"`
	WarehouseOwnerConfig warehouseOwnerConfig `json:"warehouse_owner_config"`
	RowCounts            rowCounts            `json:"row_counts"`
	// ChecksumSHA256 covers TenantID/SchemaName/DistinctRoles/RolePermissions/
	// FieldPermissions/Users/WarehouseOwnerConfig/RowCounts only - NOT
	// SnapshotAt (which always differs run-to-run) and not itself. Two
	// snapshots of an unchanged tenant therefore produce an identical
	// checksum regardless of when each was taken; that equality (or its
	// absence) is the before/after evidence 47.1/47.5 migrations diff
	// against.
	ChecksumSHA256 string `json:"checksum_sha256"`
}

// checksumPayload mirrors tenantSnapshot minus SnapshotAt/ChecksumSHA256 -
// see the field comment above.
type checksumPayload struct {
	TenantID             string
	SchemaName           string
	DistinctRoles        []string
	RolePermissions      []rolePermissionRow
	FieldPermissions     []fieldPermissionRow
	Users                []userRow
	WarehouseOwnerConfig warehouseOwnerConfig
	RowCounts            rowCounts
}

type manifestEntry struct {
	TenantID       string    `json:"tenant_id"`
	TenantName     string    `json:"tenant_name"`
	SchemaName     string    `json:"schema_name"`
	OutputFile     string    `json:"output_file"`
	SnapshotAt     time.Time `json:"snapshot_at"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	RowCounts      rowCounts `json:"row_counts"`
}

type manifest struct {
	GeneratedAt time.Time       `json:"generated_at"`
	OutputDir   string          `json:"output_dir"`
	Tool        string          `json:"tool"`
	TenantCount int             `json:"tenant_count"`
	Tenants     []manifestEntry `json:"tenants"`
}

// --- schema introspection helpers --------------------------------------

// tableExists is defensive against exactly the drift Stage 26.11.2 found in
// engines/saas.go's ProvisionTenantSchema: not every tenant schema carries
// every table a newer migration introduced (bin_stock_owner in particular is
// only cloned into tenants provisioned after Stage 42.5.5).
func tableExists(schema, table string) (bool, error) {
	var exists bool
	err := db.DB.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists)
	return exists, err
}

// columnExists guards users.location_code the same way: it was added to
// tenant_default by an ALTER TABLE (Stage 24 security), so any tenant
// schema provisioned before that ALTER ran on tenant_default (or restored
// from an old snapshot) may not have it.
func columnExists(schema, table, column string) (bool, error) {
	var exists bool
	err := db.DB.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
		)`, schema, table, column).Scan(&exists)
	return exists, err
}

// --- per-table fetchers --------------------------------------------------

func fetchRolePermissions(schema string) ([]rolePermissionRow, error) {
	ok, err := tableExists(schema, "role_permissions")
	if err != nil || !ok {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
		 FROM %s.role_permissions ORDER BY role, doctype_name`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rolePermissionRow
	for rows.Next() {
		var r rolePermissionRow
		if err := rows.Scan(&r.Role, &r.DoctypeName, &r.AllowRead, &r.AllowCreate, &r.AllowUpdate, &r.AllowDelete); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func fetchFieldPermissions(schema string) ([]fieldPermissionRow, error) {
	ok, err := tableExists(schema, "field_permissions")
	if err != nil || !ok {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT role, doctype_name, fieldname, allow_read, allow_write
		 FROM %s.field_permissions ORDER BY role, doctype_name, fieldname`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fieldPermissionRow
	for rows.Next() {
		var r fieldPermissionRow
		if err := rows.Scan(&r.Role, &r.DoctypeName, &r.Fieldname, &r.AllowRead, &r.AllowWrite); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func fetchUsers(schema string) ([]userRow, error) {
	ok, err := tableExists(schema, "users")
	if err != nil || !ok {
		return nil, err
	}
	hasLocationCode, err := columnExists(schema, "users", "location_code")
	if err != nil {
		return nil, err
	}
	col := "NULL::varchar"
	if hasLocationCode {
		col = "location_code"
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, username, email, role, status, %s, mfa_enabled, created_at
		 FROM %s.users ORDER BY username`, col, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []userRow
	for rows.Next() {
		var u userRow
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.Status, &u.LocationCode, &u.MFAEnabled, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func fetchWarehouseOwnerConfig(schema string) (warehouseOwnerConfig, error) {
	cfg := warehouseOwnerConfig{SchemaConceptNote: warehouseOwnerNote}

	docsExist, err := tableExists(schema, "documents")
	if err != nil {
		return cfg, err
	}
	if docsExist {
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT count(*) FROM %s.documents WHERE doctype = 'Location'`, schema)).Scan(&cfg.TotalLocations); err != nil {
			return cfg, err
		}
		locRows, err := db.DB.Query(fmt.Sprintf(
			`SELECT id, data->>'code', data->>'name', data->>'type', data->>'legal_entity', status
			 FROM %s.documents WHERE doctype = 'Location' ORDER BY id`, schema))
		if err != nil {
			return cfg, err
		}
		for locRows.Next() {
			var l locationRow
			if err := locRows.Scan(&l.ID, &l.Code, &l.Name, &l.Type, &l.LegalEntity, &l.Status); err != nil {
				locRows.Close()
				return cfg, err
			}
			cfg.Locations = append(cfg.Locations, l)
		}
		if err := locRows.Err(); err != nil {
			locRows.Close()
			return cfg, err
		}
		locRows.Close()

		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT count(*) FROM %s.documents WHERE doctype = 'Bin'`, schema)).Scan(&cfg.TotalBins); err != nil {
			return cfg, err
		}
		binRows, err := db.DB.Query(fmt.Sprintf(
			`SELECT id, data->>'code', data->>'owner_id', status
			 FROM %s.documents
			 WHERE doctype = 'Bin' AND data->>'owner_id' IS NOT NULL AND data->>'owner_id' <> ''
			 ORDER BY id`, schema))
		if err != nil {
			return cfg, err
		}
		for binRows.Next() {
			var b binOwnerRow
			if err := binRows.Scan(&b.ID, &b.Code, &b.OwnerID, &b.Status); err != nil {
				binRows.Close()
				return cfg, err
			}
			cfg.BinsWithOwner = append(cfg.BinsWithOwner, b)
		}
		if err := binRows.Err(); err != nil {
			binRows.Close()
			return cfg, err
		}
		binRows.Close()
		cfg.BinsWithOwnerSet = len(cfg.BinsWithOwner)
	}

	bsoExists, err := tableExists(schema, "bin_stock_owner")
	if err != nil {
		return cfg, err
	}
	cfg.BinStockOwnerTableExists = bsoExists
	if bsoExists {
		rows, err := db.DB.Query(fmt.Sprintf(
			`SELECT bin_code, sku, condition, owner_id, location_code, qty, updated_at
			 FROM %s.bin_stock_owner ORDER BY bin_code, sku, condition, owner_id`, schema))
		if err != nil {
			return cfg, err
		}
		defer rows.Close()
		for rows.Next() {
			var r binStockOwnerRow
			if err := rows.Scan(&r.BinCode, &r.SKU, &r.Condition, &r.OwnerID, &r.LocationCode, &r.Qty, &r.UpdatedAt); err != nil {
				return cfg, err
			}
			cfg.BinStockOwnerRows = append(cfg.BinStockOwnerRows, r)
		}
		if err := rows.Err(); err != nil {
			return cfg, err
		}
	}

	return cfg, nil
}

func distinctRoles(rp []rolePermissionRow, fp []fieldPermissionRow, users []userRow) []string {
	seen := map[string]bool{}
	for _, r := range rp {
		seen[r.Role] = true
	}
	for _, r := range fp {
		seen[r.Role] = true
	}
	for _, u := range users {
		seen[u.Role] = true
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

func snapshotChecksum(payload checksumPayload) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func buildSnapshot(tenantID, tenantName, schema string) (tenantSnapshot, error) {
	rp, err := fetchRolePermissions(schema)
	if err != nil {
		return tenantSnapshot{}, fmt.Errorf("role_permissions: %w", err)
	}
	fp, err := fetchFieldPermissions(schema)
	if err != nil {
		return tenantSnapshot{}, fmt.Errorf("field_permissions: %w", err)
	}
	users, err := fetchUsers(schema)
	if err != nil {
		return tenantSnapshot{}, fmt.Errorf("users: %w", err)
	}
	owner, err := fetchWarehouseOwnerConfig(schema)
	if err != nil {
		return tenantSnapshot{}, fmt.Errorf("warehouse_owner_config: %w", err)
	}

	counts := rowCounts{
		RolePermissions:   len(rp),
		FieldPermissions:  len(fp),
		Users:             len(users),
		Locations:         len(owner.Locations),
		Bins:              owner.TotalBins,
		BinsWithOwnerSet:  owner.BinsWithOwnerSet,
		BinStockOwnerRows: len(owner.BinStockOwnerRows),
	}

	snap := tenantSnapshot{
		TenantID:             tenantID,
		TenantName:           tenantName,
		SchemaName:           schema,
		SnapshotAt:           time.Now().UTC(),
		DistinctRoles:        distinctRoles(rp, fp, users),
		RolePermissions:      rp,
		FieldPermissions:     fp,
		Users:                users,
		WarehouseOwnerConfig: owner,
		RowCounts:            counts,
	}

	checksum, err := snapshotChecksum(checksumPayload{
		TenantID:             snap.TenantID,
		SchemaName:           snap.SchemaName,
		DistinctRoles:        snap.DistinctRoles,
		RolePermissions:      snap.RolePermissions,
		FieldPermissions:     snap.FieldPermissions,
		Users:                snap.Users,
		WarehouseOwnerConfig: snap.WarehouseOwnerConfig,
		RowCounts:            snap.RowCounts,
	})
	if err != nil {
		return tenantSnapshot{}, fmt.Errorf("checksum: %w", err)
	}
	snap.ChecksumSHA256 = checksum
	return snap, nil
}

func safeFilename(tenantID string) string {
	b := []byte(tenantID)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			b[i] = '_'
		}
	}
	return string(b)
}

func main() {
	out := flag.String("out", "docs/audits/tenant_snapshots/manual", "Output directory for one <tenant_id>.json per tenant plus MANIFEST.json.")
	flag.Parse()

	db.InitDB(db.ConnStringFromEnv())

	rows, err := db.DB.Query(`SELECT tenant_id, name, schema_name FROM public.tenants ORDER BY tenant_id`)
	if err != nil {
		log.Fatalf("querying public.tenants: %v", err)
	}
	type tenantRef struct{ id, name, schema string }
	var tenants []tenantRef
	for rows.Next() {
		var t tenantRef
		if err := rows.Scan(&t.id, &t.name, &t.schema); err != nil {
			rows.Close()
			log.Fatalf("scanning public.tenants: %v", err)
		}
		tenants = append(tenants, t)
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("reading public.tenants: %v", err)
	}
	rows.Close()

	if len(tenants) == 0 {
		log.Fatal("public.tenants has no rows - nothing to snapshot")
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatalf("creating output dir %s: %v", *out, err)
	}

	man := manifest{
		GeneratedAt: time.Now().UTC(),
		OutputDir:   *out,
		Tool:        "cmd/snapshot_tenant_config (Stage 47.0.3)",
		TenantCount: len(tenants),
	}

	for _, t := range tenants {
		snap, err := buildSnapshot(t.id, t.name, t.schema)
		if err != nil {
			log.Fatalf("snapshotting tenant %q (schema %s): %v", t.id, t.schema, err)
		}

		body, err := json.MarshalIndent(snap, "", "  ")
		if err != nil {
			log.Fatalf("marshaling snapshot for tenant %q: %v", t.id, err)
		}
		filename := safeFilename(t.id) + ".json"
		fullPath := filepath.Join(*out, filename)
		if err := os.WriteFile(fullPath, body, 0o644); err != nil {
			log.Fatalf("writing %s: %v", fullPath, err)
		}

		man.Tenants = append(man.Tenants, manifestEntry{
			TenantID:       snap.TenantID,
			TenantName:     snap.TenantName,
			SchemaName:     snap.SchemaName,
			OutputFile:     filename,
			SnapshotAt:     snap.SnapshotAt,
			ChecksumSHA256: snap.ChecksumSHA256,
			RowCounts:      snap.RowCounts,
		})

		fmt.Printf("snapshotted tenant %-20s schema=%-20s roles=%d field_perms=%d users=%d locations=%d bins=%d(owner=%d) bin_stock_owner_rows=%d -> %s\n",
			t.id, t.schema, snap.RowCounts.RolePermissions, snap.RowCounts.FieldPermissions, snap.RowCounts.Users,
			snap.RowCounts.Locations, snap.RowCounts.Bins, snap.RowCounts.BinsWithOwnerSet, snap.RowCounts.BinStockOwnerRows, fullPath)
	}

	manBody, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		log.Fatalf("marshaling MANIFEST.json: %v", err)
	}
	manPath := filepath.Join(*out, "MANIFEST.json")
	if err := os.WriteFile(manPath, manBody, 0o644); err != nil {
		log.Fatalf("writing %s: %v", manPath, err)
	}
	fmt.Printf("wrote manifest for %d tenant(s) -> %s\n", len(tenants), manPath)
}
