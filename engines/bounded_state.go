package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
)

// Stage 49.18.2 / 49.18.7: every store of security and operational state is
// bounded, and the bound is written down where a test can hold it to the code.
// SecurityStateStores is that list; TestStage49182EveryStateTableIsBounded
// fails when a table matching the state-table pattern appears in the schema
// without an entry, or when an entry names a cleanup that no longer exists.
//
// retentionDelete is the one way the retention sweeps delete: in batches, so a
// first sweep over a year of debris never becomes one long transaction holding
// every row lock and one WAL burst; and never while the tenant is under legal
// hold, so a hold set for an investigation keeps the logs it is about.

// retentionBatchRows bounds one DELETE; retentionMaxBatches bounds one sweep,
// so a huge backlog drains over several hourly ticks instead of one long one.
const (
	retentionBatchRows  = 5000
	retentionMaxBatches = 200
)

// retentionDelete deletes the rows of schema.table matching where (its
// parameters in args) in batches, and returns how many went. A tenant under
// legal hold keeps everything and the sweep reports zero.
func retentionDelete(tenantID, schema, table, where string, args ...interface{}) (int64, error) {
	if held, err := tenantLegalHold(tenantID); err != nil || held {
		return 0, err
	}
	where = withSecurityLogFloor(table, where) // 47.16.3: CERT-In's 180 days
	stmt := fmt.Sprintf(`DELETE FROM %[1]s.%[2]s WHERE ctid IN (SELECT ctid FROM %[1]s.%[2]s WHERE %[3]s LIMIT %[4]d)`,
		schema, table, where, retentionBatchRows)
	var total int64
	for i := 0; i < retentionMaxBatches; i++ {
		res, err := db.DB.Exec(stmt, args...)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < retentionBatchRows {
			break
		}
	}
	return total, nil
}

// tenantLegalHold reports whether the tenant's registry row has a legal hold.
// A tenant with no row (tenant_default on a fresh database) has none.
func tenantLegalHold(tenantID string) (bool, error) {
	if tenantID == "" {
		tenantID = "default"
	}
	var held bool
	err := db.DB.QueryRow(`SELECT COALESCE(legal_hold, FALSE) FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&held)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return held, err
}

// StateStore is one place security or operational state accumulates.
type StateStore struct {
	Name      string // table (schema-qualified for public.*) or in-memory map
	Kind      string // "table" or "memory"
	Key       string // what one row or entry is keyed by - its cardinality
	Bound     string // what keeps it from growing without limit
	Cleanup   string // the function that enforces Bound ("" when Bound needs none)
	LegalHold string // what a legal hold does to it
}

// SecurityStateStores lists every bounded store. Tables are tenant-schema
// tables unless prefixed "public.".
func SecurityStateStores() []StateStore {
	const (
		holdKeeps   = "kept while the tenant is under legal hold (retentionDelete)"
		notEvidence = "not evidence; removed when it expires"
		evidence    = "evidence; never swept"
	)
	return []StateStore{
		{"audit_logs", "table", "event", "archived and removed by the audit archive policy", "writeArchiveWindow", "audit_legal_hold blocks the archive"},
		{"audit_archives", "table", "archive batch", "one row per archived range", "", evidence},
		{"audit_checkpoints", "table", "checkpoint", "one row per hour per tenant", "", evidence},
		{"audit_legal_hold", "table", "hold", "set and lifted by an administrator", "", evidence},
		{"audit_retention_policy", "table", "policy", "one row per tenant", "", evidence},
		{"async_jobs", "table", "job", "terminal jobs older than platform.job_runner_retention_days", "SweepJobRunnerRetention", holdKeeps},
		{"command_idempotency", "table", "command key", "finished keys expire", "purgeSettledCommandClaims", notEvidence},
		{"api_idempotency_keys", "table", "credential + key", "platform.public_api_idempotency_retention_hours", "SweepPublicAPIRuntime", holdKeeps},
		{"api_request_log", "table", "request", "platform.public_api_log_retention_days", "SweepPublicAPIRuntime", holdKeeps},
		{"api_oauth_tokens", "table", "token digest", "expired a day ago", "SweepExpiredOAuthTokens", notEvidence},
		{"inbound_webhook_receipts", "table", "source + event id", "replay window, pruned on receipt", "ClaimInboundWebhook", notEvidence},
		{"integration_event_outbox", "table", "event", "sent events older than platform.integration_log_retention_days", "SweepIntegrationLogRetention", holdKeeps},
		{"integration_event_log", "table", "delivery", "platform.integration_log_retention_days", "SweepIntegrationLogRetention", holdKeeps},
		{"clevertap_event_log", "table", "event", "sent events older than platform.integration_log_retention_days", "SweepIntegrationLogRetention", holdKeeps},
		{"pim_publish_log", "table", "item + channel + publish", "platform.integration_log_retention_days", "SweepIntegrationLogRetention", holdKeeps},
		{"extension_hook_log", "table", "hook call", "platform.integration_log_retention_days", "SweepIntegrationLogRetention", holdKeeps},
		{"system_error_logs", "table", "error", "platform.error_log_retention_days", "SweepIntegrationLogRetention", holdKeeps},
		{"mfa_recovery_codes", "table", "user", "a fixed set per user, replaced on regeneration", "", notEvidence},
		{"mfa_trusted_devices", "table", "user + device", "expired and over-limit devices dropped on each trust", "RememberMFADevice", notEvidence},
		{"crm_pref_tokens", "table", "customer", "crm.message_retention_months", "crmApplyRetention", holdKeeps},
		{"crm_events", "table", "event", "processed events older than crm.message_retention_months", "crmApplyRetention", holdKeeps},
		{"irp_attempts", "table", "e-invoice attempt", "tax evidence for the statutory period", "", evidence},
		{"irp_events", "table", "e-invoice event", "tax evidence for the statutory period", "", evidence},
		{"print_job_log", "table", "print", "the print history reprints are made from", "", evidence},
		{"sticker_print_log", "table", "label print", "the sticker print history reprints are made from", "", evidence},
		{"approval_log", "table", "approval decision", "maker-checker evidence", "", evidence},
		{"payment_utr_log", "table", "payment reference", "payment evidence", "", evidence},
		{"period_close_log", "table", "period close", "finance evidence", "", evidence},
		{"irp_documents", "table", "e-invoice", "tax evidence for the statutory period", "", evidence},
		{"irp_profiles", "table", "GSTIN profile", "one row per registration", "", evidence},
		{"public.ops_run_log", "table", "backup or drill run", "a few rows a day", "", evidence},
		{"web_push_seen", "table", "scope + reference", "replaced with the current set on every pass", "", notEvidence},
		{"public.tenant_lifecycle_events", "table", "lifecycle change", "a few rows per client lifetime", "", evidence},
		{"public.backup_runs", "table", "host + run", "one row per backup run per host", "", evidence},
		{"authStateCache", "memory", "tenant + user", "authStateCacheMaxEntries, cleared when full; TTL authStateCacheTTL", "", notEvidence},
		{"tenantGateCache", "memory", "tenant", "authStateCacheMaxEntries, cleared when full", "", notEvidence},
		{"rateLimiter.requests", "memory", "address or session", "rateLimitMaxKeys; stale keys evicted first", "", notEvidence},
		{"loginAbuse", "memory", "address, name, backoff", "pruneLoginAbuse drops expired entries", "", notEvidence},
	}
}
