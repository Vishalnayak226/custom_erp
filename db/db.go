package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"regexp"
	"time"

	_ "github.com/lib/pq"
)

var DB *sql.DB

// Connection pool bounds (24.13) - db.DB ran on Go's unbounded defaults
// before this, same as the http.Server it serves (see routes.go's Run()).
// Sized for this app's current single-process/single-Postgres-instance scale
// (ai_handover.md §1), not a distributed deployment - revisit if that changes.
const (
	dbMaxOpenConns    = 50
	dbMaxIdleConns    = 10
	dbConnMaxLifetime = 30 * time.Minute
	dbConnMaxIdleTime = 5 * time.Minute
)

// InitDB initializes the global connection pool
func InitDB(connStr string) {
	var err error
	DB, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	DB.SetMaxOpenConns(dbMaxOpenConns)
	DB.SetMaxIdleConns(dbMaxIdleConns)
	DB.SetConnMaxLifetime(dbConnMaxLifetime)
	DB.SetConnMaxIdleTime(dbConnMaxIdleTime)

	err = DB.Ping()
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}

	log.Println("Database connection established successfully")
}

// EnforceUTF8Encoding (20.6) checks the connected Postgres server's own
// encoding - found for real via messy-data stress testing, 2026-07-25, not
// a theoretical concern: this Windows dev instance's whole cluster
// (including template0/template1, so every future CREATE DATABASE inherits
// it too) was provisioned as WIN1252, not UTF8. Any Chinese/Japanese/
// Korean/Arabic/Cyrillic-beyond-Latin1/emoji character in any text field -
// a customer name, a vendor name, a product description - hard-fails the
// INSERT with a raw Postgres encoding error the moment someone types one
// in, anywhere in this application. Fixing the cluster's encoding itself
// means dropping and recreating every database from template0 with
// ENCODING 'UTF8', which is destructive to whatever's already stored and
// out of proportion to do automatically against a shared dev database this
// tree already has standing warnings about (concurrent sessions/edits) -
// so this only surfaces the problem loudly instead of fixing it silently
// wrong, same posture as EnforceNoDefaultAdminCredentialInProduction. In
// production this should never trigger at all: essentially every managed
// Postgres provider (RDS, Cloud SQL, etc.) defaults new databases to UTF8,
// so this is a hard-stop precisely because a production instance failing
// this check means something unusual happened during provisioning.
func EnforceUTF8Encoding() error {
	var encoding string
	if err := DB.QueryRow(`SHOW server_encoding`).Scan(&encoding); err != nil {
		// Can't determine encoding - don't block startup over a query that
		// itself failed; a real connectivity problem surfaces elsewhere.
		return nil
	}
	if encoding == "UTF8" {
		return nil
	}
	if os.Getenv("ENV") == "production" {
		return fmt.Errorf("database server_encoding is %q, not UTF8 - non-Latin1 text (CJK, Arabic, emoji, ...) in any field will hard-fail on save; recreate the database with ENCODING 'UTF8' TEMPLATE template0 before running with ENV=production", encoding)
	}
	log.Printf("[WARN] database server_encoding is %q, not UTF8 - non-Latin1 text (CJK, Arabic, emoji, ...) in any field will hard-fail on save with a raw Postgres error. Fine to keep developing, but this must be fixed (recreate the database with ENCODING 'UTF8' TEMPLATE template0) before any real deployment - ENV=production refuses to start with this condition.", encoding)
	return nil
}

// validSchemaNameRe (24.17) allowlists what a schema name is allowed to look
// like before it's spliced into a fmt.Sprintf'd SQL string anywhere in this
// codebase's ~100+ call sites - checked once here, at the single place every
// one of those call sites ultimately gets its schema name from, rather than
// needing to touch each one individually. Exploitability today is low (the
// public.tenants table isn't reachable through any tenant-facing write
// path), but this is cheap, stdlib-only defense-in-depth.
var validSchemaNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// GetTenantSchema resolves the tenant schema name based on tenant_id
func GetTenantSchema(tenantID string) (string, error) {
	if tenantID == "" || tenantID == "default" {
		return "tenant_default", nil
	}
	var schemaName string
	err := DB.QueryRow("SELECT schema_name FROM public.tenants WHERE tenant_id = $1", tenantID).Scan(&schemaName)
	if err == sql.ErrNoRows {
		return "tenant_default", nil // Fallback to default
	} else if err != nil {
		return "", err
	}
	if !validSchemaNameRe.MatchString(schemaName) {
		return "", fmt.Errorf("resolved schema name %q is not a valid identifier", schemaName)
	}
	return schemaName, nil
}

// SetSearchPath scopes database queries to the tenant's schema within a transaction
func SetSearchPath(tx *sql.Tx, schemaName string) error {
	_, err := tx.Exec(fmt.Sprintf("SET LOCAL search_path TO %s, public", schemaName))
	return err
}
