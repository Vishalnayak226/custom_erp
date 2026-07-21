package db

import (
	"database/sql"
	"fmt"
	"log"
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
