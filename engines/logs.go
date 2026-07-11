package engines

import (
	"custom_erp/db"
	"log"
)

// LogAuditEvent writes an audit log entry for the specified tenant
func LogAuditEvent(tenantID, userID, action, status, details string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		log.Printf("Audit logging failed: cannot get tenant schema: %v", err)
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		log.Printf("Audit logging failed: cannot begin transaction: %v", err)
		return
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		log.Printf("Audit logging failed: cannot set search path: %v", err)
		return
	}

	query := `INSERT INTO audit_logs (user_id, action, status, details) VALUES ($1, $2, $3, $4)`
	_, err = tx.Exec(query, userID, action, status, details)
	if err != nil {
		log.Printf("Audit logging failed: cannot insert entry: %v", err)
		return
	}

	tx.Commit()
}

// LogSystemError writes a system exception/panic trace for the specified tenant
func LogSystemError(tenantID string, correlationID string, severity, moduleSource, message, stackTrace string) {
	log.Printf("[%s] System Error in module %s: %s", severity, moduleSource, message)

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		log.Printf("Error logging failed: cannot get tenant schema: %v", err)
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		log.Printf("Error logging failed: cannot begin transaction: %v", err)
		return
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		log.Printf("Error logging failed: cannot set search path: %v", err)
		return
	}

	query := `INSERT INTO system_error_logs (correlation_id, severity, module_source, error_message, stack_trace) 
	          VALUES (CASE WHEN $1 = '' THEN NULL ELSE $1::uuid END, $2, $3, $4, $5)`
	_, err = tx.Exec(query, correlationID, severity, moduleSource, message, stackTrace)
	if err != nil {
		log.Printf("Error logging failed: cannot insert error entry: %v", err)
		return
	}

	tx.Commit()
}
