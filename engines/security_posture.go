package engines

import (
	"archive/zip"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// --- Stage 49.11.3: one investigation trail by correlation id --------------
//
// Security events reference business evidence instead of copying it. Every
// store that carries the request's correlation id - signed audit rows, system
// error rows, async jobs and the integration outbox - is read for one id, so
// "what did this request do" is one lookup, not four greps.

// TrailEntry is one row of an investigation trail.
type TrailEntry struct {
	Source string    `json:"source"`
	At     time.Time `json:"at"`
	Who    string    `json:"who,omitempty"`
	What   string    `json:"what"`
	Status string    `json:"status,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

const trailRowCap = 200

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// InvestigationTrail returns every evidence row tied to one correlation id.
func InvestigationTrail(tenantID, correlationID string) ([]TrailEntry, error) {
	correlationID = SafeCorrelationID(correlationID)
	if correlationID == "" {
		return nil, errors.New("a correlation id is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var out []TrailEntry
	add := func(source, q string, args ...interface{}) error {
		rows, err := db.DB.Query(q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e := TrailEntry{Source: source}
			if err := rows.Scan(&e.At, &e.Who, &e.What, &e.Status, &e.Detail); err != nil {
				return err
			}
			e.Detail = redactAndBoundLogField(e.Detail, 2000)
			out = append(out, e)
		}
		return rows.Err()
	}
	// Signed audit rows are naive UTC (see utcStamp); the others are session-local.
	if err := add("audit", fmt.Sprintf(`SELECT created_at, COALESCE(user_id, ''), action, COALESCE(status, ''), COALESCE(details, '')
		FROM %s.audit_logs WHERE correlation_id = $1 ORDER BY seq LIMIT %d`, schema, trailRowCap), correlationID); err != nil {
		return nil, err
	}
	if uuidPattern.MatchString(correlationID) {
		if err := add("system error", fmt.Sprintf(`SELECT created_at AT TIME ZONE current_setting('TimeZone') AT TIME ZONE 'UTC', '', COALESCE(module_source, ''), COALESCE(severity, ''), COALESCE(error_message, '')
			FROM %s.system_error_logs WHERE correlation_id = $1::uuid ORDER BY created_at LIMIT %d`, schema, trailRowCap), correlationID); err != nil {
			return nil, err
		}
	}
	if err := add("job", fmt.Sprintf(`SELECT created_at AT TIME ZONE current_setting('TimeZone') AT TIME ZONE 'UTC', '', job_type, status, COALESCE(last_error, '')
		FROM %s.async_jobs WHERE correlation_id = $1 ORDER BY created_at LIMIT %d`, schema, trailRowCap), correlationID); err != nil {
		return nil, err
	}
	if err := add("integration event", fmt.Sprintf(`SELECT created_at AT TIME ZONE current_setting('TimeZone') AT TIME ZONE 'UTC', '', event_name, status, ''
		FROM %s.integration_event_outbox WHERE correlation_id = $1 ORDER BY created_at LIMIT %d`, schema, trailRowCap), correlationID); err != nil {
		return nil, err
	}
	return out, nil
}

// --- Stage 49.11.7: the client's security posture ---------------------------
//
// For the client's own administrator: this client's detections, evidence
// health and risky grants. Never another client's data, never a secret.

// PostureDetection is one fired SECURITY_ALERT row.
type PostureDetection struct {
	At       time.Time `json:"at"`
	Severity string    `json:"severity"`
	Details  string    `json:"details"`
}

// PostureCredential is one API credential worth a look.
type PostureCredential struct {
	Name      string     `json:"name"`
	Why       string     `json:"why"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// SecurityPosture is the Security pane's whole answer.
type SecurityPosture struct {
	Detections          []PostureDetection      `json:"detections"`
	Rules               []SecurityDetectionRule `json:"rules"`
	LastSweep           *time.Time              `json:"last_sweep,omitempty"`
	AuditUnsealedHours  int                     `json:"audit_unsealed_hours"`
	LastCheckpoint      *time.Time              `json:"last_checkpoint,omitempty"`
	AuditVerifyFailures int                     `json:"audit_verify_failures_7d"`
	// LastVerify (49.11.1): the verifier's last run and what it found.
	LastVerify        *PostureVerify      `json:"last_verify,omitempty"`
	AdminsWithout2FA  []string            `json:"admins_without_2fa"`
	Credentials       []PostureCredential `json:"credentials"`
	DeadLetteredJobs  int                 `json:"dead_lettered_jobs_7d"`
	ExportsFrozen     bool                `json:"exports_frozen"`
	AlertsDelivered   bool                `json:"alerts_delivered"`
	AuditRowsEstimate int64               `json:"audit_rows_estimate"`
	AuditWritesLost   int64               `json:"audit_writes_lost"`
	AuditLossReason   string              `json:"audit_loss_reason,omitempty"`
	// Stage 49.2.1: Active accounts with no sign-in for DormantDays - the
	// policy's period while it is on (DormantAutoDisable), else 90.
	DormantDays        int              `json:"dormant_days"`
	DormantAutoDisable bool             `json:"dormant_auto_disable"`
	DormantAccounts    []DormantAccount `json:"dormant_accounts"`
}

// GetSecurityPosture reads the posture for one client.
func GetSecurityPosture(tenantID string) (*SecurityPosture, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	p := &SecurityPosture{Rules: SecurityDetectionRules(), AdminsWithout2FA: []string{},
		Credentials: []PostureCredential{}, Detections: []PostureDetection{}, DormantAccounts: []DormantAccount{}}
	if last := LastSecuritySweep(); !last.IsZero() {
		p.LastSweep = &last
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT created_at, status, COALESCE(details, '') FROM %s.audit_logs
		WHERE action = $1 AND created_at > $2::timestamp ORDER BY created_at DESC LIMIT 50`, schema),
		SecurityAlertAction, utcStamp(time.Now().AddDate(0, 0, -14)))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d PostureDetection
		if err := rows.Scan(&d.At, &d.Severity, &d.Details); err != nil {
			rows.Close()
			return nil, err
		}
		p.Detections = append(p.Detections, d)
	}
	rows.Close()

	if oldest, err := oldestUnsealedAuditRow(schema); err == nil && oldest != nil {
		p.AuditUnsealedHours = int(time.Since(*oldest).Hours())
	}
	var lastCP sql.NullTime
	if db.DB.QueryRow(fmt.Sprintf(`SELECT MAX(created_at) AT TIME ZONE current_setting('TimeZone') FROM %s.audit_checkpoints`, schema)).Scan(&lastCP) == nil && lastCP.Valid {
		p.LastCheckpoint = &lastCP.Time
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.system_error_logs
		WHERE module_source = 'audit.verify' AND created_at > CURRENT_TIMESTAMP - INTERVAL '7 days'`, schema)).Scan(&p.AuditVerifyFailures)
	p.LastVerify = lastAuditVerification(schema)

	admins, err := db.DB.Query(fmt.Sprintf(`SELECT username, role FROM %s.users
		WHERE status = 'Active' AND NOT COALESCE(mfa_enabled, FALSE) AND NOT COALESCE(is_platform_support, FALSE)
		ORDER BY username`, schema))
	if err == nil {
		for admins.Next() {
			var name, role string
			if admins.Scan(&name, &role) == nil && HasFullAccess(role) {
				p.AdminsWithout2FA = append(p.AdminsWithout2FA, name)
			}
		}
		admins.Close()
	}

	creds, err := db.DB.Query(fmt.Sprintf(`SELECT name, expires_at,
		CASE WHEN expires_at IS NOT NULL AND expires_at < CURRENT_TIMESTAMP + INTERVAL '30 days' THEN 'expires within 30 days'
		     WHEN last_used_at IS NULL AND created_at < CURRENT_TIMESTAMP - INTERVAL '30 days' THEN 'never used'
		     WHEN last_used_at < CURRENT_TIMESTAMP - INTERVAL '90 days' THEN 'unused for 90 days'
		     ELSE '' END
		FROM %s.api_credentials WHERE revoked_at IS NULL AND status = 'Active' ORDER BY name`, schema))
	if err == nil {
		for creds.Next() {
			var c PostureCredential
			var exp sql.NullTime
			if creds.Scan(&c.Name, &exp, &c.Why) == nil && c.Why != "" {
				if exp.Valid {
					c.ExpiresAt = &exp.Time
				}
				p.Credentials = append(p.Credentials, c)
			}
		}
		creds.Close()
	}

	p.DormantDays = GetSettingInt(tenantID, SettingDormantAccountDays)
	p.DormantAutoDisable = p.DormantDays > 0
	if !p.DormantAutoDisable {
		p.DormantDays = postureDormantDays
	}
	if dormant, err := dormantAccounts(schema, p.DormantDays, false, nil); err == nil {
		p.DormantAccounts = dormant
	}

	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.async_jobs
		WHERE status = 'DeadLettered' AND updated_at > CURRENT_TIMESTAMP - INTERVAL '7 days'`, schema)).Scan(&p.DeadLetteredJobs)
	p.ExportsFrozen = GetSettingBool(tenantID, SettingFreezeExports)
	p.AlertsDelivered = OpsAlertDeliveryStatus().DeliveryWouldBeSent
	p.AuditWritesLost, p.AuditLossReason, _ = AuditWriteLoss()
	_ = db.DB.QueryRow(`SELECT COALESCE(c.reltuples, 0)::bigint FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = 'audit_logs'`, schema).Scan(&p.AuditRowsEstimate)
	return p, nil
}

// --- Stage 49.12.4: forensic evidence pack ----------------------------------
//
// One client, one time window, read-only. A zip of JSON-lines files - audit
// rows with their seq / signature so the pack can be re-verified, system
// errors, checkpoints, the releases live in the window and every applied
// migration's checksum - plus manifest.json naming each file's SHA-256, and a
// signature over the manifest with the audit signing key. Collecting it
// writes one audit row (who, which window) and changes nothing else.

const (
	forensicMaxWindow = 31 * 24 * time.Hour
	forensicRowCap    = 50000
)

// ForensicManifest is manifest.json.
type ForensicManifest struct {
	Tenant      string            `json:"tenant"`
	From        string            `json:"from"`
	To          string            `json:"to"`
	GeneratedAt string            `json:"generated_at"`
	GeneratedBy string            `json:"generated_by"`
	Files       map[string]string `json:"files_sha256"`
	Rows        map[string]int    `json:"rows"`
	ClockNote   string            `json:"clock_note"`
}

// ErrForensicWindow is returned for a window that is empty, reversed or too long.
var ErrForensicWindow = errors.New("choose a window of at most 31 days, start before end")

// BuildForensicPack builds the pack for [from, to).
func BuildForensicPack(tenantID string, from, to time.Time, actor string) ([]byte, *ForensicManifest, error) {
	if !to.After(from) || to.Sub(from) > forensicMaxWindow {
		return nil, nil, ErrForensicWindow
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, nil, err
	}
	fromUTC, toUTC := utcStamp(from), utcStamp(to)
	files := []struct {
		name, query string
		args        []interface{}
	}{
		// Signed rows are naive UTC; trigger rows are session-local - the
		// window is applied to each in its own clock and the row says which.
		{"audit_logs.jsonl", fmt.Sprintf(`SELECT row_to_json(t)::text FROM (
			SELECT seq, id, created_at, user_id, action, status, details, entity_type, entity_id, correlation_id,
			       signature, sig_version, (signature IS NOT NULL) AS signed_utc
			FROM %s.audit_logs
			WHERE (signature IS NOT NULL AND created_at >= $1::timestamp AND created_at < $2::timestamp)
			   OR (signature IS NULL AND created_at >= ($3::timestamptz AT TIME ZONE current_setting('TimeZone'))
			                         AND created_at < ($4::timestamptz AT TIME ZONE current_setting('TimeZone')))
			ORDER BY seq LIMIT %d) t`, schema, forensicRowCap+1), []interface{}{fromUTC, toUTC, from, to}},
		{"system_error_logs.jsonl", fmt.Sprintf(`SELECT row_to_json(t)::text FROM (
			SELECT log_id, correlation_id, severity, module_source, error_message, created_at
			FROM %s.system_error_logs
			WHERE created_at >= ($1::timestamptz AT TIME ZONE current_setting('TimeZone'))
			  AND created_at < ($2::timestamptz AT TIME ZONE current_setting('TimeZone'))
			ORDER BY created_at LIMIT %d) t`, schema, forensicRowCap+1), []interface{}{from, to}},
		{"audit_checkpoints.jsonl", fmt.Sprintf(`SELECT row_to_json(t)::text FROM (
			SELECT id, from_seq, to_seq, row_count, unsigned_count, row_digest, prev_checkpoint_signature,
			       signature, sig_version, kind, created_at
			FROM %s.audit_checkpoints ORDER BY to_seq LIMIT %d) t`, schema, forensicRowCap+1), nil},
		// The release live at the window's start and every one after it, so
		// each event can be tied to the software that wrote it.
		{"releases.jsonl", `SELECT row_to_json(t)::text FROM (
			SELECT environment, app_version, git_commit, promoted_at, build_status FROM public.deployments
			WHERE promoted_at < $2 AND promoted_at >= COALESCE((SELECT MAX(promoted_at) FROM public.deployments WHERE promoted_at <= $1), '-infinity')
			ORDER BY promoted_at) t`, []interface{}{from, to}},
		{"migrations.jsonl", `SELECT row_to_json(t)::text FROM (
			SELECT migration_file, applied_at, checksum FROM public.schema_migrations ORDER BY migration_file) t`, nil},
	}

	manifest := &ForensicManifest{Tenant: tenantID, From: from.UTC().Format(time.RFC3339), To: to.UTC().Format(time.RFC3339),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339), GeneratedBy: actor,
		Files: map[string]string{}, Rows: map[string]int{},
		ClockNote: "audit rows with signed_utc=true carry UTC times; every other time is the database session's local time"}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		var body bytes.Buffer
		n, err := writeJSONLines(&body, f.query, f.args...)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", f.name, err)
		}
		if n > forensicRowCap {
			return nil, nil, fmt.Errorf("%s has more than %d rows in this window - choose a shorter window", f.name, forensicRowCap)
		}
		sum := sha256.Sum256(body.Bytes())
		manifest.Files[f.name] = hex.EncodeToString(sum[:])
		manifest.Rows[f.name] = n
		w, err := zw.Create(f.name)
		if err != nil {
			return nil, nil, err
		}
		if _, err := w.Write(body.Bytes()); err != nil {
			return nil, nil, err
		}
	}
	mb, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	for _, f := range []struct {
		name string
		body []byte
	}{{"manifest.json", mb}, {"manifest.sig", []byte(SignForensicManifest(mb))}} {
		w, err := zw.Create(f.name)
		if err != nil {
			return nil, nil, err
		}
		if _, err := w.Write(f.body); err != nil {
			return nil, nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, nil, err
	}
	LogAuditEvent(tenantID, actor, "EVIDENCE_EXPORTED", "SUCCESS",
		fmt.Sprintf("Forensic evidence pack %s to %s (%d audit rows)", manifest.From, manifest.To, manifest.Rows["audit_logs.jsonl"]))
	return buf.Bytes(), manifest, nil
}

// SignForensicManifest signs manifest bytes with the audit signing key, so a
// pack can be shown to have come from this server unaltered.
func SignForensicManifest(manifest []byte) string {
	mac := hmac.New(sha256.New, auditSigningKey.key)
	mac.Write([]byte("forensic-manifest|" + AuditSigVersion + "|"))
	mac.Write(manifest)
	return AuditSigVersion + ":" + hex.EncodeToString(mac.Sum(nil))
}

func writeJSONLines(w *bytes.Buffer, query string, args ...interface{}) (int, error) {
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return n, err
		}
		w.WriteString(line)
		w.WriteByte('\n')
		n++
	}
	return n, rows.Err()
}
