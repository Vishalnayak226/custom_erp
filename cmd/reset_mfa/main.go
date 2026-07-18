package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

func main() {
	connStr := "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Error opening db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Error connecting db: %v", err)
	}

	schemas := []string{"tenant_default", "tenant_a", "tenant_b", "public"}
	for _, schema := range schemas {
		query := fmt.Sprintf("UPDATE %s.users SET mfa_enabled = false, mfa_secret = NULL WHERE role = 'HR/Admin'", schema)
		res, err := db.Exec(query)
		if err == nil {
			rows, _ := res.RowsAffected()
			fmt.Printf("Updated %d admin users in %s.users to reset MFA.\n", rows, schema)
		} else {
			// Ignore if table doesn't exist in that schema
		}
	}
}
