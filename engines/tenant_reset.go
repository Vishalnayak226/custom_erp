package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Stage 91.1 / 91.2 (user, 2026-10-18): "Clean up what ever required you do
// for both app and minn. I can start as clean slate ... Do all cleanup except
// login password ... document numbers restart from 1."
//
// ResetTenantData empties one client back to a freshly provisioned client:
// every business record - transactions, stock, ledger, masters, company setup,
// integrations and settings - goes; the logins stay (users with their
// password hashes, roles and permissions), as do what the app needs to run
// (record definitions, the chart of accounts, approval rules, status rules,
// shipped reference records) and every piece of evidence (audit trail, data
// subject requests, e-invoice records). Numbering starts again from 1 and the
// number formats go back to the shipped ones.
//
// It runs from tenantctl only (operator authority, never a route): dry run by
// default, refused unless the client is suspended and a backup from the last
// 24 hours exists, one transaction, and evidenced in tenant_lifecycle_events.
// Every table of the client is classified below; a table that is not - one
// added after this was written - makes it refuse, so nothing new is ever
// wiped or kept by accident (TestTenantResetClassifiesEveryTable).

// TenantEventDataReset is the evidence row a reset writes.
const TenantEventDataReset = "data_reset"

// tenantResetKeep are kept exactly as they are, with why.
var tenantResetKeep = map[string]string{
	"users":                    "the logins, with their password hashes",
	"mfa_recovery_codes":       "the logins' two-factor recovery codes",
	"mfa_trusted_devices":      "the logins' remembered devices (Stage 125.8)",
	"roles":                    "roles",
	"role_permissions":         "permissions",
	"field_permissions":        "permissions",
	"role_template_migrations": "role template history",
	"role_grant_changes":       "evidence: role permission changes (Stage 49.3.6)",
	"doctype_meta":             "record definitions",
	"doctype_fields":           "record definitions",
	"module_entitlements":      "the modules this client has",
	"tenant_limits":            "this client's plan limits",
	"feature_flags":            "platform switches",
	"gl_accounts":              "the chart of accounts the postings need",
	"approval_rules":           "approval rules",
	"loyalty_tier_rules":       "loyalty tiers",
	"stock_locations":          "the stock location types",
	"stock_location_defaults":  "the stock location defaults",
	"audit_logs":               "evidence: the audit trail",
	"audit_archives":           "evidence: the audit trail",
	"audit_checkpoints":        "evidence: the audit trail",
	"audit_legal_hold":         "evidence: legal holds",
	"audit_retention_policy":   "evidence: retention policy",
	"data_subject_requests":    "evidence: privacy requests",
	"irp_events":               "evidence: e-invoice (IRN) history, kept by law",
	"irp_attempts":             "evidence: e-invoice (IRN) history, kept by law",
	"irp_documents":            "evidence: e-invoice (IRN) history, kept by law",
	"irp_profiles":             "evidence: e-invoice (IRN) history, kept by law",
	"ewb_documents":            "evidence: e-way bill history, kept by law", // Stage 130.16
	"system_error_logs":        "the server's error log",
	"crm_suppressions":         "evidence: numbers and addresses that asked for no messages (by contact, so a re-imported one stays stopped)",
	"store_app_pins":           "the logins' store app PINs",
	"web_push_vapid":           "the browser-alert signing key",
}

// tenantResetClear are emptied.
var tenantResetClear = map[string]string{
	"gl_postings":                       "ledger postings",
	"accounting_periods":                "accounting periods",
	"period_close_log":                  "period close history",
	"period_close_tasks":                "period close tasks",
	"inventory_availability":            "stock",
	"inventory_reservation":             "stock reservations",
	"stock_location_balance":            "stock by location",
	"store_stock_locations":             "stores' stock locations",
	"bin_allocation":                    "bin stock",
	"bin_stock":                         "bin stock",
	"bin_stock_batch":                   "bin stock",
	"bin_stock_lpn":                     "bin stock",
	"bin_stock_owner":                   "bin stock",
	"warehouse_owner":                   "warehouse owners",
	"item_cost":                         "item costs",
	"grn_line":                          "goods receipt lines",
	"grn_line_change":                   "goods receipt changes",
	"customer_advances":                 "customer advances",
	"customer_advance_refunds":          "customer advance refunds",
	"loyalty_point_ledger":              "loyalty points",
	"loyalty_redemption_otp_challenges": "loyalty redemption codes",
	"stored_value_instruments":          "gift cards and credits",
	"stored_value_ledger":               "gift card and credit movements",
	"pos_offline_heartbeats":            "till heartbeats",
	"pos_offline_sales":                 "offline sales waiting for validation", // Stage 49.5.6a
	"payment_utr_log":                   "payment references",
	"print_job_log":                     "print history",
	"print_snapshots":                   "the issued copy of each printed document", // Stage 130.16
	"print_template_versions":           "print template versions",                  // Stage 130.16
	"reconciliation_results":            "reconciliation check results",             // Stage 130.16
	"wave_pick_confirmations":           "wave pick confirmations",                  // Stage 130.13
	"sticker_print_log":                 "sticker print history",
	"pim_publish_log":                   "product publishing history",
	"pim_publish_queue":                 "product publishing queue",
	"product_content_versions":          "product content history",
	"master_code_changes":               "old master codes",
	"approval_log":                      "approval history",
	"async_jobs":                        "background jobs",
	"command_idempotency":               "replay guards",
	"integration_event_log":             "integration events",
	"integration_event_outbox":          "integration events",
	"document_attachments":              "attachments (the files themselves stay in storage)",
	"role_store_access":                 "roles' store access (the stores are removed)",
	"channel_credentials":               "sales channel connections",
	"channel_order_mapping":             "sales channel orders",
	"channel_product_mapping":           "sales channel products",
	"shopify_links":                     "Shopify product links",
	"clevertap_credentials":             "CleverTap connection",
	"clevertap_event_log":               "CleverTap events",
	"pinelabs_credentials":              "Pine Labs connection",
	"pinelabs_transactions":             "Pine Labs transactions",
	"unicommerce_credentials":           "Unicommerce connection",
	"unicommerce_inventory_sync":        "Unicommerce stock sync",
	"unicommerce_order_mapping":         "Unicommerce orders",
	"extension_hooks":                   "extension hooks",
	"extension_hook_log":                "extension hook history",
	"api_credentials":                   "API keys",
	"api_oauth_tokens":                  "API tokens",
	"api_idempotency_keys":              "API replay guards",
	"api_request_log":                   "API request history",
	"inbound_webhook_receipts":          "inbound webhook replay guards", // Stage 134.3
	"system_settings":                   "settings (every one back to its default)",
	"dynamic_labels":                    "renamed labels",
	"industry_lock":                     "the chosen industry (asked again)",
	"sequence_counters":                 "number counters (numbering starts again from 1)",
	// Stage 101 CRM. Consent goes with the customers it was given by: after
	// a reset nobody has agreed to anything, the safe side.
	"crm_appointments":             "CRM appointments",
	"crm_campaign_runs":            "CRM campaign runs",
	"crm_consent_ledger":           "CRM consent (the customers go too)",
	"crm_conversation_messages":    "CRM inbox messages",
	"crm_conversations":            "CRM inbox conversations",
	"crm_customer_notes":           "CRM customer notes",
	"crm_events":                   "CRM events",
	"crm_feedback":                 "CRM feedback",
	"crm_forecast_snapshots":       "CRM forecast snapshots",
	"crm_hold_requests":            "CRM hold requests",
	"crm_journey_runs":             "CRM journey runs",
	"crm_journey_versions":         "CRM journeys",
	"crm_journeys":                 "CRM journeys",
	"crm_lead_rules":               "CRM lead assignment rules",
	"crm_leads":                    "CRM leads",
	"crm_lifecycle_versions":       "CRM lifecycle thresholds (back to the defaults)",
	"crm_opportunities":            "CRM deals",
	"crm_opportunity_history":      "CRM deal history",
	"crm_pipeline_stages":          "CRM pipeline stages (the defaults come back on first use)",
	"crm_pref_tokens":              "CRM preference links",
	"crm_referral_codes":           "CRM referral codes",
	"crm_referrals":                "CRM referrals",
	"crm_segment_snapshot_members": "CRM segment snapshots",
	"crm_segment_snapshots":        "CRM segment snapshots",
	"crm_segment_versions":         "CRM segments",
	"crm_segments":                 "CRM segments",
	"crm_senders":                  "CRM message senders (connections)",
	"crm_sends":                    "CRM messages",
	"crm_static_list_members":      "CRM uploaded lists",
	"crm_static_lists":             "CRM uploaded lists",
	"crm_store_slots":              "CRM appointment slots (the stores are removed)",
	"crm_survey_versions":          "CRM surveys",
	"crm_surveys":                  "CRM surveys",
	"crm_tasks":                    "CRM tasks",
	"crm_template_versions":        "CRM message templates",
	"crm_templates":                "CRM message templates",
	"crm_ticket_notes":             "CRM ticket notes",
	"crm_voucher_reservations":     "CRM voucher holds",
	"crm_wishlist":                 "CRM wish lists",
	// Stage 102 store app.
	"store_app_devices":      "store app devices (the stores are removed)",
	"web_push_subscriptions": "browser alert sign-ups (asked again)",
	"web_push_seen":          "browser alerts already sent",
}

// tenantResetSeedDoctypes are the reference records the app ships into every
// client's documents; the shipped rows (created by 'system') stay, anything a
// person added to them goes with the rest of the documents.
var tenantResetSeedDoctypes = []string{"StatusTransitionRule", "ItemType", "TDSSection", "StoredValueProfile",
	"Currency", "POSTender", "HSNCode", "ItemAttribute", "ReasonCode", "PaymentTerm"}

// TenantResetTable is one table's part of the plan.
type TenantResetTable struct {
	Table  string `json:"table"`
	Rows   int64  `json:"rows"`
	Action string `json:"action"` // clear | keep | reset
	What   string `json:"what"`
}

// TenantResetPlan is what a reset will do (dry run) or did.
type TenantResetPlan struct {
	TenantID         string             `json:"tenant_id"`
	Schema           string             `json:"schema"`
	Status           string             `json:"status"`
	Tables           []TenantResetTable `json:"tables"`
	DocumentsRemoved map[string]int64   `json:"documents_removed"`
	DocumentsKept    map[string]int64   `json:"documents_kept"`
	Unclassified     []string           `json:"unclassified,omitempty"`
	Backup           MasterRecodeBackup `json:"backup"`
	Users            int64              `json:"users"`
	Applied          bool               `json:"applied"`
	Blockers         []string           `json:"blockers,omitempty"`
}

// PlanTenantDataReset works out what ResetTenantData would remove and keep,
// changing nothing.
func PlanTenantDataReset(tenantID string) (*TenantResetPlan, error) {
	life, err := GetTenantLifecycle(tenantID)
	if err != nil {
		return nil, err
	}
	schema := life.SchemaName
	if !validSQLIdentifier(schema) {
		return nil, fmt.Errorf("%q is not a usable schema name", schema)
	}
	plan := &TenantResetPlan{TenantID: tenantID, Schema: schema, Status: life.Status,
		DocumentsRemoved: map[string]int64{}, DocumentsKept: map[string]int64{}}
	tables, err := tenantResetTables(db.DB, schema)
	if err != nil {
		return nil, err
	}
	for _, t := range tables {
		count := func() int64 {
			var n int64
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s`, schema, t)).Scan(&n)
			return n
		}
		switch {
		case t == "documents":
			plan.Tables = append(plan.Tables, TenantResetTable{Table: t, Rows: count(), Action: "clear", What: "every record - transactions, masters, company setup - except the shipped reference records"})
		case t == "prefix_configs":
			what := "number formats back to the shipped ones"
			if schema == "tenant_default" {
				what = "number formats kept (this is the template client)"
			}
			plan.Tables = append(plan.Tables, TenantResetTable{Table: t, Rows: count(), Action: "reset", What: what})
		case tenantResetClear[t] != "":
			plan.Tables = append(plan.Tables, TenantResetTable{Table: t, Rows: count(), Action: "clear", What: tenantResetClear[t]})
		case tenantResetKeep[t] != "":
			plan.Tables = append(plan.Tables, TenantResetTable{Table: t, Rows: count(), Action: "keep", What: tenantResetKeep[t]})
		default:
			plan.Unclassified = append(plan.Unclassified, t)
		}
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT doctype, (doctype = ANY($1) AND COALESCE(created_by, '') = 'system') AS kept, COUNT(*)
		FROM %s.documents GROUP BY 1, 2`, schema), pqStringArray(tenantResetSeedDoctypes))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var doctype string
		var kept bool
		var n int64
		if err := rows.Scan(&doctype, &kept, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if kept {
			plan.DocumentsKept[doctype] = n
		} else {
			plan.DocumentsRemoved[doctype] = n
		}
	}
	rows.Close()
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.users`, schema)).Scan(&plan.Users)
	plan.Backup = recodeBackupStatus(schema)
	if len(plan.Unclassified) > 0 {
		plan.Blockers = append(plan.Blockers, "these tables are not classified as kept or cleared, so nothing is reset until tenantctl is updated for them: "+strings.Join(plan.Unclassified, ", "))
	}
	if life.Status != TenantStatusSuspended {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("the client is %s - suspend it first (tenantctl suspend), so nobody writes during the reset; resume it afterwards", life.Status))
	}
	if !plan.Backup.OK {
		plan.Blockers = append(plan.Blockers, plan.Backup.Note)
	}
	if life.LegalHold {
		plan.Blockers = append(plan.Blockers, "the client is under a legal hold")
	}
	sort.Slice(plan.Tables, func(i, j int) bool {
		if plan.Tables[i].Action != plan.Tables[j].Action {
			return plan.Tables[i].Action < plan.Tables[j].Action
		}
		return plan.Tables[i].Table < plan.Tables[j].Table
	})
	return plan, nil
}

// tenantResetTables lists the client's own tables.
func tenantResetTables(q queryRower, schema string) ([]string, error) {
	rows, err := q.Query(`SELECT table_name FROM information_schema.tables WHERE table_schema = $1 AND table_type = 'BASE TABLE' ORDER BY 1`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ResetTenantData applies the plan in one transaction. It refuses while any
// blocker stands; reason and actor go into the evidence row.
func ResetTenantData(tenantID, actor, reason string) (*TenantResetPlan, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("a reason is required and is recorded in the client's evidence trail")
	}
	plan, err := PlanTenantDataReset(tenantID)
	if err != nil {
		return nil, err
	}
	if len(plan.Blockers) > 0 {
		return plan, fmt.Errorf("not reset: %s", strings.Join(plan.Blockers, "; "))
	}
	schema := plan.Schema
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, t := range plan.Tables {
		switch {
		case t.Table == "documents":
			if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE NOT (doctype = ANY($1) AND COALESCE(created_by, '') = 'system')`, schema),
				pqStringArray(tenantResetSeedDoctypes)); err != nil {
				return nil, fmt.Errorf("documents: %w", err)
			}
		case t.Table == "prefix_configs":
			if schema == "tenant_default" {
				continue
			}
			if err := restoreShippedRows(tx, schema, "prefix_configs"); err != nil {
				return nil, err
			}
		case t.Action == "clear":
			if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.%s`, schema, t.Table)); err != nil {
				return nil, fmt.Errorf("%s: %w", t.Table, err)
			}
		}
	}
	removed := int64(0)
	for _, n := range plan.DocumentsRemoved {
		removed += n
	}
	if err := recordTenantLifecycleEventTx(tx, tenantID, TenantEventDataReset, actor,
		fmt.Sprintf("business data reset to a clean slate: %d record(s) removed, %d login(s) kept; reason: %s", removed, plan.Users, reason)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	InvalidateTenantLifecycleCache(tenantID)
	plan.Applied = true
	return plan, nil
}

// restoreShippedRows replaces a client's copy of a seeded table with the
// template client's (tenant_default), column by column so a column order
// that differs between the two never misplaces a value.
func restoreShippedRows(tx *sql.Tx, schema, table string) error {
	cols, err := sharedColumns(tx, schema, table)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.%s`, schema, table)); err != nil {
		return fmt.Errorf("%s: %w", table, err)
	}
	list := strings.Join(cols, ", ")
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.%s (%s) SELECT %s FROM tenant_default.%s`, schema, table, list, list, table)); err != nil {
		return fmt.Errorf("%s: %w", table, err)
	}
	return nil
}

func sharedColumns(tx *sql.Tx, schema, table string) ([]string, error) {
	rows, err := tx.Query(`SELECT a.column_name FROM information_schema.columns a
		JOIN information_schema.columns b ON b.table_schema = 'tenant_default' AND b.table_name = a.table_name AND b.column_name = a.column_name
		WHERE a.table_schema = $1 AND a.table_name = $2 ORDER BY a.ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, `"`+c+`"`)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("%s has no columns in common with the template", table)
	}
	return cols, rows.Err()
}
