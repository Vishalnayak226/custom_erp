package engines

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"custom_erp/db"
)

// SystemFault (206.2) is one open fault on the System Errors screen: every
// unresolved system_error_logs row with the same severity, module and message,
// carried by its latest occurrence.
type SystemFault struct {
	LogID         string    `json:"log_id"`
	CorrelationID string    `json:"correlation_id"` // "" when the row has none
	Severity      string    `json:"severity"`
	ModuleSource  string    `json:"module_source"`
	ErrorMessage  string    `json:"error_message"`
	StackTrace    string    `json:"stack_trace"`
	Count         int       `json:"count"`
	FirstSeen     time.Time `json:"first_seen"`
	CreatedAt     time.Time `json:"created_at"` // last seen
}

const systemFaultListCap = 100

// ListSystemFaults returns the client's open faults, newest first. Only
// faults - the filter the Control Panel's Errors (24h) and the error-rate
// alert use (systemFaultSQL): a refusal the user can correct stays in the
// log for the correlation trail but is not an error to fix.
func ListSystemFaults(tenantID string) ([]SystemFault, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// Group on the small columns and keep only the latest row's id, then read
	// that one row's trace - never aggregate every trace of a busy fault.
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH g AS (
			SELECT (array_agg(log_id ORDER BY created_at DESC))[1] AS last_id,
			       COUNT(*) AS n, MIN(created_at) AS first_at, MAX(created_at) AS last_at
			FROM %[1]s.system_error_logs
			WHERE resolved_at IS NULL AND %[2]s
			GROUP BY severity, module_source, error_message
			ORDER BY last_at DESC
			LIMIT %[3]d)
		SELECT l.log_id::text, COALESCE(l.correlation_id::text, ''), l.severity, l.module_source, l.error_message,
		       COALESCE(l.stack_trace, ''), g.n, g.first_at, g.last_at
		FROM g JOIN %[1]s.system_error_logs l ON l.log_id = g.last_id
		ORDER BY g.last_at DESC`, schema, systemFaultSQL("$1"), systemFaultListCap), clientErrorCodesArg())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SystemFault{}
	for rows.Next() {
		var f SystemFault
		if err := rows.Scan(&f.LogID, &f.CorrelationID, &f.Severity, &f.ModuleSource, &f.ErrorMessage,
			&f.StackTrace, &f.Count, &f.FirstSeen, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ErrSystemFaultNotFound is a resolve for a log row this client does not have.
var ErrSystemFaultNotFound = errors.New("system error not found")

// logIDPattern is a log_id's uuid shape; anything else is not a row, and
// casting it would be a database error instead of a not-found.
var logIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ResolveSystemFault (206.3) marks every open occurrence of logID's fault
// resolved. A later occurrence is a new row with no resolved_at, so the fault
// comes back on the screen by itself if it happens again. It returns how many
// rows it closed.
func ResolveSystemFault(tenantID, logID, by string) (int64, error) {
	if !logIDPattern.MatchString(logID) {
		return 0, ErrSystemFaultNotFound
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %[1]s.system_error_logs l SET resolved_at = NOW(), resolved_by = $2
		FROM %[1]s.system_error_logs f
		WHERE f.log_id = $1::uuid AND l.resolved_at IS NULL
		  AND l.severity = f.severity AND l.module_source = f.module_source AND l.error_message = f.error_message`, schema),
		logID, by)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists bool
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.system_error_logs WHERE log_id = $1::uuid)`, schema), logID).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			return 0, ErrSystemFaultNotFound
		}
	}
	return n, nil
}
