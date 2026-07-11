package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

var DB *sql.DB

// InitDB initializes the global connection pool
func InitDB(connStr string) {
	var err error
	DB, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}

	err = DB.Ping()
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}

	log.Println("Database connection established successfully")
}

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
	return schemaName, nil
}

// SetSearchPath scopes database queries to the tenant's schema within a transaction
func SetSearchPath(tx *sql.Tx, schemaName string) error {
	_, err := tx.Exec(fmt.Sprintf("SET LOCAL search_path TO %s, public", schemaName))
	return err
}
