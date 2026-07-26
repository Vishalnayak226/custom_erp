package main

// 24.38: host-shell access to run this tool at all is an inherent,
// unpreventable privilege (whoever can reach the Postgres box could always
// run raw SQL directly) - this can't "fix" that. What it can fix is that the
// old version was silent, blanket, and untraceable: one invocation reset
// *every* HR/Admin across four hardcoded schemas with zero confirmation and
// zero audit trail. Now it's scoped to one schema/user by default, defaults
// to a dry run, requires a recorded reason, and writes a real audit_logs
// entry (same tamper-evident checksum chain as every other MFA action - see
// engines.LogAuditEvent/auditChecksum) so a reset via this path leaves
// exactly the same trail one via the app would.

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
)

// auditChecksum mirrors engines.LogAuditEvent's hash formula exactly (same
// field order/separator) so entries written by this standalone tool chain
// correctly with entries the main app itself writes to the same table.
func auditChecksum(prevChecksum, userID, action, status, details string) string {
	h := sha256.Sum256([]byte(prevChecksum + "|" + userID + "|" + action + "|" + status + "|" + details))
	return hex.EncodeToString(h[:])
}

func logAudit(db *sql.DB, schema, targetUser, status, details string) {
	tx, err := db.Begin()
	if err != nil {
		log.Printf("[WARN] audit log failed (MFA reset still applied): %v", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf("SET LOCAL search_path TO %s, public", schema)); err != nil {
		log.Printf("[WARN] audit log failed (MFA reset still applied): %v", err)
		return
	}
	var prevChecksum string
	_ = tx.QueryRow(`SELECT checksum FROM audit_logs ORDER BY created_at DESC, id DESC LIMIT 1`).Scan(&prevChecksum)
	checksum := auditChecksum(prevChecksum, targetUser, "MFA_RESET", status, details)
	if _, err := tx.Exec(
		`INSERT INTO audit_logs (user_id, action, status, details, checksum) VALUES ($1, $2, $3, $4, $5)`,
		targetUser, "MFA_RESET", status, details, checksum,
	); err != nil {
		log.Printf("[WARN] audit log insert failed (MFA reset still applied): %v", err)
		return
	}
	tx.Commit()
}

func main() {
	schema := flag.String("schema", "", "Tenant schema to operate on, e.g. tenant_default (required - no more silent all-schema sweep).")
	username := flag.String("user", "", "Exact username whose MFA should be reset (required unless -all-admins is set).")
	allAdmins := flag.Bool("all-admins", false, "Reset MFA for every HR/Admin user in -schema instead of one -user. Wider blast radius - only for when you genuinely don't know which admin is locked out.")
	reason := flag.String("reason", "", "Why this reset is being performed, e.g. an incident/ticket reference (required - recorded in the audit trail).")
	confirm := flag.Bool("yes", false, "Actually perform the reset. Without this flag the tool only prints what it would do.")
	flag.Parse()

	if *schema == "" {
		log.Fatal("-schema is required, e.g. -schema=tenant_default")
	}
	if *username == "" && !*allAdmins {
		log.Fatal("-user=<username> is required (or pass -all-admins to reset every HR/Admin in this schema)")
	}
	if *reason == "" {
		log.Fatal("-reason is required and is recorded in the audit trail")
	}

	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Error opening db: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("Error connecting db: %v", err)
	}

	operator := os.Getenv("USERNAME")
	if operator == "" {
		operator = os.Getenv("USER")
	}
	if operator == "" {
		operator = "unknown-host-shell-operator"
	}
	host, _ := os.Hostname()
	details := fmt.Sprintf("host-shell MFA reset via cmd/reset_mfa; operator_os_user=%s host=%s reason=%q", operator, host, *reason)

	var rows *sql.Rows
	if *allAdmins {
		rows, err = db.Query(fmt.Sprintf(`SELECT username FROM %s.users WHERE role = 'HR/Admin' AND mfa_enabled = true`, *schema))
	} else {
		rows, err = db.Query(fmt.Sprintf(`SELECT username FROM %s.users WHERE username = $1 AND mfa_enabled = true`, *schema), *username)
	}
	if err != nil {
		log.Fatalf("Error querying users: %v", err)
	}
	var targets []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err == nil {
			targets = append(targets, u)
		}
	}
	rows.Close()

	if len(targets) == 0 {
		fmt.Printf("No matching user(s) with mfa_enabled=true found in %s.users - nothing to do.\n", *schema)
		return
	}

	fmt.Printf("This will disable MFA for %d user(s) in schema %q: %v\n", len(targets), *schema, targets)
	fmt.Printf("Reason: %s\n", *reason)
	if !*confirm {
		fmt.Println("Dry run only - re-run with -yes to actually perform the reset.")
		return
	}

	for _, u := range targets {
		_, err := db.Exec(fmt.Sprintf(`UPDATE %s.users SET mfa_enabled = false, mfa_secret = NULL WHERE username = $1`, *schema), u)
		if err != nil {
			log.Printf("Failed to reset MFA for %s: %v", u, err)
			logAudit(db, *schema, u, "FAILED", details+fmt.Sprintf(" error=%v", err))
			continue
		}
		fmt.Printf("Reset MFA for %s.\n", u)
		logAudit(db, *schema, u, "SUCCESS", details)
	}
}
