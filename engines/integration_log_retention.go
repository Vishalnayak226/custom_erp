package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
	"log"
	"time"
)

// Stage 47.18.3: bounded cleanup for the append-only integration and hook
// logs - the outbox's finished events, the integration event log and the
// extension hook call log - the SweepJobRunnerRetention precedent: hourly,
// leader only, one tenant at a time. An event still Pending in the outbox is
// never swept. A retention of 0 days is a hold: nothing is swept, and the
// setting change that put the hold on is in the audit log like every other.

// integrationLogTables are the tables swept and the timestamp each is aged by;
// the outbox keeps its Pending (unsent) events whatever their age.
var integrationLogTables = []struct{ table, column, keep, setting string }{
	{"integration_event_outbox", "created_at", "status = 'Pending'", "platform.integration_log_retention_days"},
	{"integration_event_log", "created_at", "", "platform.integration_log_retention_days"},
	{"extension_hook_log", "called_at", "", "platform.integration_log_retention_days"},
	// Stage 49.18.2: the CleverTap send log (unsent events kept), the PIM
	// channel publish log (a payload snapshot per item per publish) and the
	// system error log were the append-only logs nothing swept.
	{"clevertap_event_log", "created_at", "status = 'Pending'", "platform.integration_log_retention_days"},
	{"pim_publish_log", "created_at", "", "platform.integration_log_retention_days"},
	{"system_error_logs", "created_at", "", "platform.error_log_retention_days"},
}

// SweepIntegrationLogRetention deletes rows older than each table's retention
// setting and reports how many went from each table that lost rows. Deletes
// go through retentionDelete: batched, and nothing while the tenant is under
// legal hold.
func SweepIntegrationLogRetention(tenantID string) (map[string]int64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, t := range integrationLogTables {
		days := GetSettingInt(tenantID, t.setting)
		if days <= 0 {
			continue // a hold
		}
		var exists bool
		if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)`, schema, t.table).Scan(&exists); err != nil || !exists {
			continue
		}
		where := fmt.Sprintf("%s < CURRENT_TIMESTAMP - ($1 || ' days')::interval", t.column)
		if t.keep != "" {
			where += " AND NOT (" + t.keep + ")"
		}
		n, err := retentionDelete(tenantID, schema, t.table, where, days)
		if err != nil {
			return out, err
		}
		if n > 0 {
			out[t.table] = n
		}
	}
	return out, nil
}

// StartIntegrationLogRetentionSweeper runs the sweep hourly on the leader.
func StartIntegrationLogRetentionSweeper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() || db.DB == nil {
					continue
				}
				schemas, err := listRegisteredTenantSchemas()
				if err != nil {
					log.Printf("[INTEGRATION-LOG-SWEEP] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					tenantID, idErr := tenantIDForSchema(schema)
					if idErr != nil {
						continue
					}
					if _, err := SweepIntegrationLogRetention(tenantID); err != nil {
						LogBackgroundJobError("INTEGRATION-LOG-SWEEP", "sweep", schema, err)
					}
				}
			}
		}
	}()
}
