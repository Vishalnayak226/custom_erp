package engines

import (
	"fmt"
	"sort"
)

// Stage 47.16.3 - CERT-In's 180-day floor for security logs.
//
// CERT-In's directions of 28 April 2022 (section 70B(6) of the IT Act) require
// a body corporate to keep the logs of its ICT systems for a rolling 180 days,
// within India. The retention settings that age these logs out
// (platform.public_api_log_retention_days, default 30;
// platform.integration_log_retention_days, default 90;
// platform.error_log_retention_days, default 180) each allow less, so the
// floor is enforced where every retention sweep deletes - retentionDelete -
// rather than in each setting or each sweep: whatever a setting says, and
// whichever sweep (current or future) asks, a security-log row younger than
// SecurityLogMinRetentionDays is never deleted. A setting of 0 is still a hold
// (keep everything); a longer setting still keeps longer.
//
// Only logs that record who or what touched the system are on the list. The
// tables the same settings sweep that hold copies of business payloads rather
// than access records - integration_event_outbox, clevertap_event_log (customer
// event data), pim_publish_log (catalogue snapshots) - stay on their own,
// shorter setting: keeping them longer would be the over-retention 49.6.2's
// data-minimization rule exists to prevent. audit_logs is not here because it
// is never deleted at all - RunAuditArchive moves sealed windows into a signed,
// encrypted archive no earlier than 30 days, and nothing deletes the archive.
//
// docs/security/cert-in-log-inventory.md is the full inventory (host logs,
// clocks, residency, the six-hour reporting workflow);
// TestStage47163SecurityLogInventoryMatchesTheFloor keeps it and this list in
// step.

// SecurityLogMinRetentionDays is the CERT-In rolling retention period.
const SecurityLogMinRetentionDays = 180

// securityLogAgeColumns names each security-log table and the timestamp its
// age is read from.
var securityLogAgeColumns = map[string]string{
	"api_request_log":       "created_at", // public API calls: credential, path, status, client address
	"system_error_logs":     "created_at", // server-side errors, with correlation id
	"integration_event_log": "created_at", // deliveries to and from connected systems
	"extension_hook_log":    "called_at",  // calls to client-configured extension hooks
}

// SecurityLogTables lists the tables the 180-day floor protects, sorted.
func SecurityLogTables() []string {
	out := make([]string, 0, len(securityLogAgeColumns))
	for t := range securityLogAgeColumns {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// withSecurityLogFloor narrows a retention sweep's predicate so a security-log
// row is only deleted once it is older than the CERT-In floor as well.
func withSecurityLogFloor(table, where string) string {
	col, ok := securityLogAgeColumns[table]
	if !ok {
		return where
	}
	return fmt.Sprintf("(%s) AND %s < CURRENT_TIMESTAMP - INTERVAL '%d days'", where, col, SecurityLogMinRetentionDays)
}
