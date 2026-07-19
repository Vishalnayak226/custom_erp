package engines

import (
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"time"

	"custom_erp/db"
)

// PatchProposal mirrors one row of public.patch_proposals for API responses.
type PatchProposal struct {
	ID              int        `json:"id"`
	TenantID        string     `json:"tenant_id"`
	ModuleSource    string     `json:"module_source"`
	Signature       string     `json:"signature"`
	ErrorSample     string     `json:"error_sample"`
	OccurrenceCount int        `json:"occurrence_count"`
	Classification  string     `json:"classification"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	DecidedBy       *string    `json:"decided_by,omitempty"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
	Notes           *string    `json:"notes,omitempty"`
}

type patchPolicyRule struct {
	pattern        *regexp.Regexp
	classification string
	description    string
}

// StartPatchIntakeWorker (Stage 14.13-14.16) periodically scans every
// tenant's system_error_logs for new ERROR/PANIC rows since the last run,
// groups them into signatures, classifies each against public.patch_policy_rules,
// and records a public.patch_proposals row.
//
// Deliberately, by construction, this worker NEVER mutates any tenant or
// business state - it only ever writes to patch_proposals/patch_intake_state,
// both pure audit/triage tables. "auto_safe" here means "known noise, mark
// dismissed without a human needing to look" (e.g. an expected rate-limit
// rejection), not "automatically change configuration or code." Actually
// applying a real fix - a module-entitlement toggle, a code change promoted
// via promote.ps1 - stays a deliberate, separate human action using the
// tools already built in Phases A/C. This system has no automated
// fix-generation or fix-application capability, and doesn't pretend to -
// see docs/micro_checklist.md's Stage 14.13-14.16 entry for the full
// rationale on why this is a stricter reading of "never auto-deploy code to
// live" than the original plan sketch.
func StartPatchIntakeWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if db.DB == nil {
				continue
			}
			if err := runPatchIntakeCycle(); err != nil {
				log.Printf("[PATCHINTAKE] cycle failed: %v", err)
			}
		}
	}()
}

func runPatchIntakeCycle() error {
	lastRunAt, err := getPatchIntakeLastRun()
	if err != nil {
		return fmt.Errorf("failed to read last run time: %w", err)
	}

	rules, err := loadPatchPolicyRules()
	if err != nil {
		return fmt.Errorf("failed to load policy rules: %w", err)
	}

	schemas, err := listTenantSchemas()
	if err != nil {
		return fmt.Errorf("failed to list tenant schemas: %w", err)
	}

	cycleStart := time.Now()
	for _, schema := range schemas {
		tenantID, tErr := tenantIDForSchema(schema)
		if tErr != nil {
			continue
		}
		if err := intakeSchemaErrors(schema, tenantID, lastRunAt, rules); err != nil {
			log.Printf("[PATCHINTAKE] failed scanning schema %s: %v", schema, err)
		}
	}

	return setPatchIntakeLastRun(cycleStart)
}

func getPatchIntakeLastRun() (time.Time, error) {
	var lastRun sql.NullTime
	err := db.DB.QueryRow("SELECT last_run_at FROM public.patch_intake_state WHERE id = 1").Scan(&lastRun)
	if err == sql.ErrNoRows || !lastRun.Valid {
		// First run ever (or state row missing) - look back 24h so the very
		// first cycle isn't a no-op against a fresh install.
		return time.Now().Add(-24 * time.Hour), nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return lastRun.Time, nil
}

func setPatchIntakeLastRun(t time.Time) error {
	_, err := db.DB.Exec(`
		INSERT INTO public.patch_intake_state (id, last_run_at) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET last_run_at = EXCLUDED.last_run_at`, t)
	return err
}

func loadPatchPolicyRules() ([]patchPolicyRule, error) {
	rows, err := db.DB.Query("SELECT error_pattern, classification, COALESCE(description, '') FROM public.patch_policy_rules")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []patchPolicyRule
	for rows.Next() {
		var pattern, classification, description string
		if err := rows.Scan(&pattern, &classification, &description); err != nil {
			continue
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			log.Printf("[PATCHINTAKE] skipping invalid rule pattern %q: %v", pattern, err)
			continue
		}
		rules = append(rules, patchPolicyRule{pattern: compiled, classification: classification, description: description})
	}
	return rules, rows.Err()
}

// classify returns "requires_approval" for anything not explicitly matched -
// the same fail-closed default IsFeatureEnabled/IsModuleEnabled/checkPermission
// already use elsewhere in this codebase, applied here to blast-radius
// instead of access.
func classify(errorMessage string, rules []patchPolicyRule) string {
	for _, r := range rules {
		if r.pattern.MatchString(errorMessage) {
			return r.classification
		}
	}
	return "requires_approval"
}

func tenantIDForSchema(schema string) (string, error) {
	if schema == "tenant_default" {
		return "default", nil
	}
	var tenantID string
	err := db.DB.QueryRow("SELECT tenant_id FROM public.tenants WHERE schema_name = $1", schema).Scan(&tenantID)
	return tenantID, err
}

type errorSignature struct {
	moduleSource string
	errorMessage string
}

func intakeSchemaErrors(schema, tenantID string, since time.Time, rules []patchPolicyRule) error {
	query := fmt.Sprintf(`
		SELECT module_source, error_message
		FROM %s.system_error_logs
		WHERE created_at > $1 AND severity IN ('ERROR', 'PANIC')`, schema)
	rows, err := db.DB.Query(query, since)
	if err != nil {
		return err
	}
	defer rows.Close()

	counts := make(map[errorSignature]int)
	for rows.Next() {
		var sig errorSignature
		if err := rows.Scan(&sig.moduleSource, &sig.errorMessage); err == nil {
			counts[sig]++
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for sig, count := range counts {
		classification := classify(sig.errorMessage, rules)
		status := "pending"
		var decidedBy sql.NullString
		var decidedAt sql.NullTime
		if classification == "auto_safe" {
			status = "dismissed"
			decidedBy = sql.NullString{String: "system", Valid: true}
			decidedAt = sql.NullTime{Time: time.Now(), Valid: true}
		}
		_, err := db.DB.Exec(`
			INSERT INTO public.patch_proposals
				(tenant_id, module_source, signature, error_sample, occurrence_count, classification, status, decided_by, decided_at, notes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			tenantID, sig.moduleSource, sig.errorMessage, sig.errorMessage, count, classification, status,
			decidedBy, decidedAt, nullableNote(classification))
		if err != nil {
			log.Printf("[PATCHINTAKE] failed to record proposal for %s/%s: %v", tenantID, sig.moduleSource, err)
		}
	}
	return nil
}

func nullableNote(classification string) sql.NullString {
	if classification == "auto_safe" {
		return sql.NullString{String: "Auto-dismissed: matched a known-noise policy rule.", Valid: true}
	}
	return sql.NullString{}
}

// ListPatchProposals returns proposals, optionally filtered by status
// ("" means no filter). HR/Admin-only at the API layer (internal/server).
func ListPatchProposals(status string) ([]PatchProposal, error) {
	query := `SELECT id, tenant_id, module_source, signature, error_sample, occurrence_count, classification, status, created_at, decided_by, decided_at, notes FROM public.patch_proposals`
	args := []interface{}{}
	if status != "" {
		query += " WHERE status = $1"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC LIMIT 200"

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PatchProposal
	for rows.Next() {
		var p PatchProposal
		var decidedBy sql.NullString
		var decidedAt sql.NullTime
		var notes sql.NullString
		if err := rows.Scan(&p.ID, &p.TenantID, &p.ModuleSource, &p.Signature, &p.ErrorSample, &p.OccurrenceCount,
			&p.Classification, &p.Status, &p.CreatedAt, &decidedBy, &decidedAt, &notes); err != nil {
			return nil, err
		}
		if decidedBy.Valid {
			p.DecidedBy = &decidedBy.String
		}
		if decidedAt.Valid {
			p.DecidedAt = &decidedAt.Time
		}
		if notes.Valid {
			p.Notes = &notes.String
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DecidePatchProposal records a human approve/reject decision. It never
// takes any action beyond recording the decision - see the package doc
// comment on StartPatchIntakeWorker for why that's a deliberate boundary,
// not a missing feature.
func DecidePatchProposal(proposalID int, decision, decidedBy, notes string) error {
	if decision != "approved" && decision != "rejected" {
		return fmt.Errorf("invalid decision %q: must be 'approved' or 'rejected'", decision)
	}
	res, err := db.DB.Exec(`
		UPDATE public.patch_proposals
		SET status = $1, decided_by = $2, decided_at = CURRENT_TIMESTAMP, notes = $3
		WHERE id = $4 AND status = 'pending'`,
		decision, decidedBy, notes, proposalID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("no pending proposal found with id %d", proposalID)
	}
	return nil
}
