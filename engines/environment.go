package engines

import (
	"fmt"
	"log"
	"os"
)

// Stage 47.0.5/47.11.6 (Gate 0 of the deep-persona-audit remediation plan):
// a single, explicit switch for whether this server instance is allowed to
// perform real external side effects (outbound webhook delivery, SMTP send,
// ops-alert webhook, and any future payment/SMS integration). Every such
// call site is expected to check ExternalSideEffectsEnabled() and simulate
// instead of calling out when it is false, the same posture the Stage 38.7
// sandbox-tenant check (IsSandboxSchema) already established per-tenant -
// this is the server-wide twin of that, so an R0 regression/abuse test
// cannot itself trigger a real external call by accident, and an ordinary
// developer running this server locally never surprises a real recipient.
//
// Defaults to OFF everywhere except ENV=production, matching the existing
// ENV-driven convention (auth.go's EnforceNoDefaultAdminCredentialInProduction,
// db.go, routes.go's /api/v1/debug/panic gate). ERP_ENABLE_EXTERNAL_SIDE_EFFECTS=1
// opts a non-production run in deliberately (e.g. a staging rehearsal that
// wants real delivery). ERP_DISABLE_EXTERNAL_SIDE_EFFECTS=1 is the emergency
// kill switch and wins even in production.
func ExternalSideEffectsEnabled() bool {
	if os.Getenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS") == "1" {
		return false
	}
	if os.Getenv("ENV") == "production" {
		return true
	}
	return os.Getenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS") == "1"
}

// EnvironmentBanner returns a short human string describing this instance's
// external-side-effect posture, for the server startup log and the
// GET /api/v1/system/environment endpoint the UI banner (47.6/47.14) reads.
func EnvironmentBanner() string {
	env := os.Getenv("ENV")
	if env == "" {
		env = "development"
	}
	if ExternalSideEffectsEnabled() {
		return fmt.Sprintf("LIVE (ENV=%s) - real email/webhook/ops-alert side effects are enabled", env)
	}
	return fmt.Sprintf("SIMULATED (ENV=%s) - email/webhook/ops-alert side effects are OFF; set ERP_ENABLE_EXTERNAL_SIDE_EFFECTS=1 to opt in", env)
}

// LogEnvironmentBanner prints the banner once at startup, before any
// background worker begins polling - callers (routes.go's Run) place this
// ahead of every engines.Start*Worker call.
func LogEnvironmentBanner() {
	log.Printf("[ENVIRONMENT] %s", EnvironmentBanner())
}

// Stage 47.0.4: a temporary supported-configuration notice, named directly
// by the deep persona audit (audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md,
// findings A-02 through A-07) and by 47.0's own release rule - "no critical
// finding can disappear through wording or menu hiding". Each entry names
// the R0 item that, once it actually closes, retires that entry - flip the
// bool right there in the same commit that closes the item; nothing else
// reads or depends on these flags. Kept as a flat slice of small structs
// (not a doctype/table) because this describes the *product build*, not
// per-tenant data - true identically for every tenant on this binary.
type supportedConfigurationCaveat struct {
	closedByItem string // the R0 item whose closure retires this entry
	closed       bool
	message      string
}

var supportedConfigurationCaveats = []supportedConfigurationCaveat{
	{"47.2-47.4", false, "POS returns and high-value checkout: price/cost/tax are still client-influenced pending 47.2-47.4 - treat as conditional, not fully trustworthy, until closed"},
	{"47.5", false, "3PL/WMS: owner isolation is not yet enforced during allocation/picking - do not run a mixed-owner warehouse until 47.5 closes"},
	{"47.6", false, "Mobile/RF WMS and POS: not yet certified for phone-sized/RF device use - Preview only until 47.6 closes"},
	{"47.7", false, "Audit trail: evidence integrity/retention is not yet closed (legacy blank-checksum rows, unverified concurrent writers) - treat audit-trail completeness/immutability claims as conditional until 47.7 closes"},
}

// SupportedConfigurationNotice returns the still-open caveat messages, for
// the GET /api/v1/system/environment response the UI banner reads.
func SupportedConfigurationNotice() []string {
	var notice []string
	for _, c := range supportedConfigurationCaveats {
		if !c.closed {
			notice = append(notice, c.message)
		}
	}
	return notice
}

// QuarantineStaleExternalDispatchJobs runs once at startup, before the job
// runner/outbox workers begin polling, when external side effects are
// disabled. A Pending webhook_delivery job already sitting in the queue at
// boot (leftover demo/test data, or a row queued by a previous LIVE run
// whose delivery never completed) is moved to a new 'Quarantined' status
// instead of being silently picked up and "delivered" (simulated or not) on
// this boot - the operator sees it explicitly on the Async Jobs tab and
// must call ReplayQuarantinedJob to resume it deliberately. Reservation/
// outbox rows unrelated to an external call are untouched; this only
// touches the one job_type that makes a real outbound HTTP call today.
func QuarantineStaleExternalDispatchJobs() {
	if ExternalSideEffectsEnabled() {
		return
	}
	schemas, err := listTenantSchemas()
	if err != nil {
		log.Printf("[ENVIRONMENT] could not list tenant schemas for startup quarantine sweep: %v", err)
		return
	}
	var total int64
	for _, schema := range schemas {
		n, err := quarantineStaleExternalDispatchJobsInSchema(schema)
		if err != nil {
			log.Printf("[ENVIRONMENT] quarantine sweep failed for %s: %v", schema, err)
			continue
		}
		total += n
	}
	if total > 0 {
		log.Printf("[ENVIRONMENT] quarantined %d pending external-dispatch job(s) at startup (external side effects are OFF) - replay deliberately via the Async Jobs tab if needed", total)
	}
}
