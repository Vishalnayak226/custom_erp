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
	where = withAuditLogFloor(table, where)    // 47.16.4c: Rule 3(5)'s eight years
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

// retentionClearField (Stage 49.6.2a) is retentionDelete for one personal
// field of a record that is itself kept: it removes field from the doctype's
// documents made more than days ago, in the same batches and never under a
// legal hold. The key is removed rather than blanked, so the change log row
// the documents trigger writes does not copy the old value back out.
func retentionClearField(tenantID, schema, doctype, field string, days int) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	if held, err := tenantLegalHold(tenantID); err != nil || held {
		return 0, err
	}
	stmt := fmt.Sprintf(`UPDATE %[1]s.documents SET data = data - $1::text, updated_at = CURRENT_TIMESTAMP, version = version + 1,
			updated_by = 'system', edit_reason = 'personal data retention'
		WHERE ctid IN (SELECT ctid FROM %[1]s.documents WHERE doctype = $2 AND COALESCE(data->>$1::text, '') <> ''
			AND created_at < CURRENT_TIMESTAMP - ($3 || ' days')::interval LIMIT %[2]d)`, schema, retentionBatchRows)
	var total int64
	for i := 0; i < retentionMaxBatches; i++ {
		res, err := db.DB.Exec(stmt, field, doctype, days)
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
	// Stage 49.18.2 (the remaining part), from stateStoreLimits: the most one
	// row or entry holds and what caps it, the index its lookups or its sweep
	// read ("" for memory and files) and the time they are held to.
	MaxPayload string
	Index      string
	QuerySLO   string
}

// SecurityStateStores lists every bounded store. Tables are tenant-schema
// tables unless prefixed "public.".
func SecurityStateStores() []StateStore {
	const (
		holdKeeps   = "kept while the tenant is under legal hold (retentionDelete)"
		notEvidence = "not evidence; removed when it expires"
		evidence    = "evidence; never swept"
	)
	base := []struct{ name, kind, key, bound, cleanup, hold string }{
		{"audit_logs", "table", "event", "archived and removed by the audit archive policy", "writeArchiveWindow", "audit_legal_hold blocks the archive"},
		{"audit_archives", "table", "archive batch", "one row per archived range; never deleted inside 8 years (withAuditLogFloor)", "", evidence},
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
		{"documents (ImportJob raw rows: open_failed_rows, source_header, error_csv)", "table", "import job", "stripped after platform.import_file_retention_days; the job's counts, totals and lineage stay", "SweepImportFileRetention", holdKeeps},
		{"PIM import drop folders (imported/, failed/)", "files", "file", "platform.import_file_retention_days by file age", "SweepImportFileRetention", holdKeeps},
		{"public.tenants (sandbox)", "table", "sandbox client", "suspended at sandbox_expires_at; purged by the tenant purge path", "SweepExpiredSandboxTenants", "an operator's purge is refused while the hold is set (PurgeTenant)"},
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
		{"webhookEventCounts", "memory", "client + outcome", "emptied every webhookEventWindow; at most webhookEventRowCap audit rows per key per window", "webhookEventAllowed", notEvidence},
		{"certificateWatch", "memory", "public host", "replaced on every check; at most certificateHostCap hosts", "CheckCertificates", notEvidence},
	}
	stores := make([]StateStore, len(base))
	for i, b := range base {
		l := stateStoreLimits[b.name]
		stores[i] = StateStore{b.name, b.kind, b.key, b.bound, b.cleanup, b.hold, l.payload, l.index, l.slo}
	}
	return stores
}

// The query SLOs a state store is held to, on its index.
const (
	stateSLOLookup = "a key lookup within 10 ms"
	stateSLOSweep  = "one sweep batch (5,000 rows) within 1 s"
	stateSLOBoth   = "a key lookup within 10 ms; one sweep batch within 1 s"
	stateSLOMemory = "in memory: a map lookup"
)

// stateStoreLimits (Stage 49.18.2, the remaining part) is each store's
// maximum payload - every large column of the table named with what caps it -
// and the index and SLO of its lookups or its sweep.
// TestStage49182StatePayloadAndQuerySLO holds both to the schema: a store
// without an entry, a large column its payload does not name, or an index the
// table does not have fails it. Found writing it: eleven swept tables had no
// index on the column their sweep ages them by (each sweep read the whole
// table) - added by db/migrations_stage140_13_state_sweep_indexes_49_18_2.sql - and
// the extension hook log stored a hook's error text uncapped.
var stateStoreLimits = func() map[string]struct{ payload, index, slo string } {
	const (
		fixed   = "fixed columns: ids, codes, hashes and times"
		bodyCap = "the request body cap, 2 MiB (envelopeMaxBody)"
	)
	return map[string]struct{ payload, index, slo string }{
		"audit_logs":             {"details ≤ 16 KB (maxAuditDetailBytes)", "idx_audit_logs_created_desc", stateSLOBoth},
		"audit_archives":         {"file_path and counts; the archive itself is a file", "idx_audit_archives_created", stateSLOLookup},
		"audit_checkpoints":      {fixed, "idx_audit_checkpoints_range", stateSLOLookup},
		"audit_legal_hold":       {"reason: typed, within " + bodyCap, "idx_audit_legal_hold_active", stateSLOLookup},
		"audit_retention_policy": {"note: typed, within " + bodyCap, "audit_retention_policy_pkey", stateSLOLookup},
		"async_jobs": {"payload ≤ 64 KB (jobMaxPayloadBytes), result ≤ 64 KB (jobMaxResultBytes), last_error ≤ 2,000 bytes",
			"idx_async_jobs_completed", stateSLOBoth},
		"command_idempotency":      {"response: the command's answer (a document id and status) or its error", "idx_command_idempotency_completed", stateSLOBoth},
		"api_idempotency_keys":     {"response_body ≤ 256 KB (maxStoredBody), path ≤ 512 (truncateForLog)", "idx_api_idempotency_created", stateSLOBoth},
		"api_request_log":          {"path ≤ 512 (truncateForLog); " + fixed, "idx_api_request_log_time", stateSLOBoth},
		"api_oauth_tokens":         {"scopes: the client's granted scopes; " + fixed, "idx_api_oauth_tokens_expires", stateSLOBoth},
		"inbound_webhook_receipts": {fixed, "idx_inbound_webhook_receipts_at", stateSLOBoth},
		"integration_event_outbox": {"payload: one event, built from a record saved within " + bodyCap, "idx_outbox_created", stateSLOBoth},
		"integration_event_log":    {"error_message: written empty (deliveries only); " + fixed, "idx_integration_event_log_created", stateSLOSweep},
		"clevertap_event_log":      {"event_data: one profile or bill event", "idx_clevertap_event_log_created", stateSLOBoth},
		"pim_publish_log":          {"payload_snapshot: one item's channel payload; error_message: the channel's answer", "idx_pim_publish_log_created", stateSLOBoth},
		"extension_hook_log":       {"error ≤ 4 KB (maxSystemErrorMessageBytes); the payload is kept as its hash", "idx_extension_hook_log_called", stateSLOBoth},
		"system_error_logs": {"error_message ≤ 4 KB (maxSystemErrorMessageBytes), stack_trace ≤ 32 KB (maxSystemErrorStackBytes)",
			"idx_system_error_logs_created", stateSLOSweep},
		"documents (ImportJob raw rows: open_failed_rows, source_header, error_csv)": {"one upload ≤ 16 MiB and platform.max_import_rows rows",
			"idx_documents_doctype_status", stateSLOBoth},
		"PIM import drop folders (imported/, failed/)": {"one dropped file", "", "one directory listing per sweep"},
		"public.tenants (sandbox)":                     {"one client's registry row", "tenants_pkey", stateSLOLookup},
		"mfa_recovery_codes":                           {fixed, "idx_mfa_recovery_codes_user", stateSLOLookup},
		"mfa_trusted_devices":                          {fixed, "mfa_trusted_devices_user_idx", stateSLOLookup},
		"crm_pref_tokens":                              {fixed, "crm_pref_tokens_created_idx", stateSLOBoth},
		"crm_events":                                   {"payload: one store event (a bill, a visit, a message)", "crm_events_occurred_idx", stateSLOBoth},
		"irp_attempts":                                 {"response_body ≤ 1 MiB (irpResponseBodyMax), error_message ≤ 500 bytes", "irp_attempts_pkey", stateSLOLookup},
		"irp_events":                                   {"detail ≤ 2,000 bytes", "irp_events_doc_idx", stateSLOLookup},
		"print_job_log":                                {"reprint_reason ≤ 500 (clip); error_detail: one printer error", "idx_print_job_log_document", stateSLOLookup},
		"sticker_print_log":                            {"reprint_reason: typed, within " + bodyCap, "uq_sticker_print_log_request", stateSLOLookup},
		"approval_log":                                 {"comment: typed, within " + bodyCap, "idx_approval_log_doc", stateSLOLookup},
		"payment_utr_log":                              {fixed, "payment_utr_log_utr_key", stateSLOLookup},
		"period_close_log":                             {"detail: one close step's result", "idx_period_close_log_period", stateSLOLookup},
		"irp_documents": {"payload, signed_invoice, signed_qr: one e-invoice as the IRP returns it, ≤ 1 MiB (irpResponseBodyMax); last_error: its validation issues or the IRP's error; reason, cancel_reason: one line each",
			"irp_documents_due_idx", stateSLOLookup},
		"irp_profiles":                   {"one registration's settings", "irp_profiles_environment_gstin_key", stateSLOLookup},
		"public.ops_run_log":             {"detail: one run's summary", "idx_ops_run_log_type_finished", stateSLOLookup},
		"web_push_seen":                  {"ref_key ≤ 300 (its column)", "web_push_seen_pkey", stateSLOLookup},
		"public.tenant_lifecycle_events": {"detail: one change's summary", "idx_tenant_lifecycle_events_tenant", stateSLOLookup},
		"public.backup_runs":             {"detail, offsite_key: one run's summary", "idx_backup_runs_latest", stateSLOLookup},
		"authStateCache":                 {"one user's role, store and status", "", stateSLOMemory},
		"tenantGateCache":                {"one client's state", "", stateSLOMemory},
		"rateLimiter.requests":           {"the request times inside one window", "", stateSLOMemory},
		"loginAbuse":                     {"a counter and a backoff time", "", stateSLOMemory},
		"webhookEventCounts":             {"a count per client and outcome", "", stateSLOMemory},
		"certificateWatch":               {"one host's expiry, issuer and the dial / TLS error line", "", stateSLOMemory},
	}
}()
