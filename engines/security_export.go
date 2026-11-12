package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
)

// The destination secret is sealed with the existing CHANNEL_CREDENTIAL_KEY keyring
// (an outside destination credential like a connector token; no new key to deploy).
//
// Stage 49.11.6: an OPTIONAL export of one client's security events to a
// log / SIEM destination the client chose. Off until an administrator sets a
// destination and turns it on.
//
// The shape keeps the business path and the disk out of it:
//   - Nothing is queued. The events already live in audit_logs; the export is a
//     cursor (audit_logs.seq) and a worker that reads one bounded batch beyond
//     it, posts it, and moves the cursor only after a 2xx. A destination that
//     is down costs a retry with backoff and nothing else - no local growth,
//     and no code on the transaction path (the worker reads, it is never
//     called from a write).
//   - One client only: the batch is read from that client's schema.
//   - Personal data: the rows are sent as stored, which is the same text the
//     Activity Log shows - redacted and bounded before they were signed
//     (49.11.2). The client address and device are not sent.
//   - Signed: the existing webhook signing (HMAC-SHA256 over the body, and a
//     timestamped one) over HTTPS only, to an address checked as public both
//     when it is saved and again at connect time. The body is not encrypted
//     beyond TLS.
//   - A retention or archive that removes events before they were sent is
//     reported (gap_note, a SECURITY_EXPORT_GAP audit row), never skipped quietly.
//   - Sandbox clients never export; the outside-calls switches apply.

const (
	SecurityExportEvent    = "security.events"
	securityExportTestName = "security.events.test"
	// A batch is at most this many rows and this many bytes of text; the rest
	// waits for the next pass.
	securityExportBatchRows  = 500
	securityExportBatchBytes = 512 << 10
	// A row is only sent once it is this old, so one that took its sequence
	// number but committed a moment later is not passed over.
	securityExportSettle = 30 * time.Second
	securityExportMaxURL = 500
)

// securityExportCheckURL is the address guard; a variable so a test can reach a
// local receiver (the connect-time guard has its own switch).
var securityExportCheckURL = validateWebhookURL

// SecurityExportStatus is what the screen shows. The secret is never in it.
type SecurityExportStatus struct {
	URL           string     `json:"url"`
	HasSecret     bool       `json:"has_secret"`
	Enabled       bool       `json:"enabled"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastAttemptAt *time.Time `json:"last_attempt_at"`
	LastError     string     `json:"last_error"`
	Failures      int        `json:"failures"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	GapNote       string     `json:"gap_note"`
	ExportedTotal int64      `json:"exported_total"`
	LagSeconds    int64      `json:"lag_seconds"`
	Waiting       int64      `json:"waiting"`
	BackfillFrom  *time.Time `json:"backfill_from"`
	BackfillLeft  int64      `json:"backfill_left"`
	Allowed       bool       `json:"allowed"`
	AllowedWhy    string     `json:"allowed_why"`
}

type securityExportConfig struct {
	url       string
	secretEnc []byte
	enabled   bool
	cursor    int64
	failures  int
	nextAt    sql.NullTime
	// backfillUntil is where a history backfill is complete (0 = none running).
	backfillUntil int64
}

// ValidateSecurityExportURL is the one check for the destination: https, no
// credentials in the address, and a public host (the same guard as webhooks).
func ValidateSecurityExportURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > securityExportMaxURL {
		return errors.New("enter the destination address (https, up to 500 characters)")
	}
	if u, err := url.Parse(raw); err == nil && u.User != nil {
		return errors.New("the address must not carry a user name or password")
	}
	return securityExportCheckURL(raw)
}

func loadSecurityExportConfig(schema string) (securityExportConfig, bool, error) {
	var c securityExportConfig
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT url, secret_enc, enabled, cursor_seq, failures, next_attempt_at, backfill_until_seq
		FROM %s.security_export_config WHERE id = 1`, schema)).Scan(&c.url, &c.secretEnc, &c.enabled, &c.cursor, &c.failures, &c.nextAt, &c.backfillUntil)
	if err == sql.ErrNoRows {
		return c, false, nil
	}
	return c, err == nil, err
}

// SaveSecurityExport sets the destination. A blank secret keeps the one on
// file. Turning it on for the first time starts the cursor at the newest
// event, so history is not sent in one go.
func SaveSecurityExport(tenantID, actor, rawURL, secret string, enabled bool) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if sandbox, _ := IsSandboxSchema(schema); sandbox {
		return errors.New("a sandbox client never exports security events")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL != "" || enabled {
		if err := ValidateSecurityExportURL(rawURL); err != nil {
			return err
		}
	}
	cur, found, err := loadSecurityExportConfig(schema)
	if err != nil {
		return err
	}
	enc := cur.secretEnc
	if strings.TrimSpace(secret) != "" {
		if len(secret) < 16 || len(secret) > 200 {
			return errors.New("the signing secret must be 16 to 200 characters")
		}
		if enc, err = encryptVersioned(channelCredSigningKey, []byte(secret)); err != nil {
			return err
		}
	}
	if enabled && len(enc) == 0 {
		return errors.New("set a signing secret before turning the export on")
	}
	cursor := cur.cursor
	if enabled && (!found || (cur.cursor == 0 && !cur.enabled)) {
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(MAX(seq), 0) FROM %s.audit_logs`, schema)).Scan(&cursor); err != nil {
			return err
		}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.security_export_config (id, url, secret_enc, enabled, cursor_seq, updated_by, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, now())
		ON CONFLICT (id) DO UPDATE SET url = EXCLUDED.url, secret_enc = EXCLUDED.secret_enc, enabled = EXCLUDED.enabled,
			cursor_seq = EXCLUDED.cursor_seq, failures = 0, next_attempt_at = NULL, last_error = '', updated_by = EXCLUDED.updated_by, updated_at = now()`, schema),
		rawURL, enc, enabled, cursor, actor); err != nil {
		return err
	}
	state := "off"
	if enabled {
		state = "on"
	}
	LogAuditEvent(tenantID, actor, "SECURITY_EXPORT_CONFIG", "SUCCESS", fmt.Sprintf("Security event export set %s; destination host %s", state, hostOf(rawURL)))
	return nil
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return "-"
}

// GetSecurityExportStatus is the settings screen's read.
func GetSecurityExportStatus(tenantID string) (SecurityExportStatus, error) {
	var s SecurityExportStatus
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return s, err
	}
	var lastS, lastA, nextA, bfFrom sql.NullTime
	var cursor, bfUntil int64
	var enc []byte
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT url, secret_enc, enabled, cursor_seq, last_success_at, last_attempt_at, last_error, failures,
		next_attempt_at, gap_note, exported_total, backfill_from, backfill_until_seq FROM %s.security_export_config WHERE id = 1`, schema)).
		Scan(&s.URL, &enc, &s.Enabled, &cursor, &lastS, &lastA, &s.LastError, &s.Failures, &nextA, &s.GapNote, &s.ExportedTotal, &bfFrom, &bfUntil)
	if err != nil && err != sql.ErrNoRows {
		return s, err
	}
	s.HasSecret = len(enc) > 0
	s.LastSuccessAt, s.LastAttemptAt, s.NextAttemptAt = nullTimePtr(lastS), nullTimePtr(lastA), nullTimePtr(nextA)
	if bfUntil > cursor {
		s.BackfillFrom, s.BackfillLeft = nullTimePtr(bfFrom), bfUntil-cursor
	}
	if sandbox, _ := IsSandboxSchema(schema); sandbox {
		s.AllowedWhy = "A sandbox client never exports."
	} else if ok, why := OutsideCallsAllowed(schema); !ok {
		s.AllowedWhy = why
	} else {
		s.Allowed = true
	}
	if s.Enabled {
		var oldest sql.NullTime
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT MIN(created_at), COUNT(*) FROM (SELECT created_at FROM %s.audit_logs WHERE seq > $1 LIMIT 100000) w`, schema), cursor).
			Scan(&oldest, &s.Waiting); err == nil && oldest.Valid {
			s.LagSeconds = int64(time.Since(oldest.Time).Seconds())
		}
	}
	return s, nil
}

type exportEvent struct {
	Seq           int64  `json:"seq"`
	At            string `json:"at"`
	User          string `json:"user"`
	Action        string `json:"action"`
	Status        string `json:"status"`
	Details       string `json:"details"`
	CorrelationID string `json:"correlation_id,omitempty"`
}

// RunSecurityExport sends one bounded batch for one client and moves the
// cursor only when the destination answered 2xx. It returns how many events
// were sent. Any failure is recorded with a backoff; the events stay put.
func RunSecurityExport(tenantID string, now time.Time) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	cfg, found, err := loadSecurityExportConfig(schema)
	if err != nil || !found || !cfg.enabled {
		return 0, err
	}
	if cfg.nextAt.Valid && cfg.nextAt.Time.After(now.UTC()) {
		return 0, nil
	}
	if sandbox, _ := IsSandboxSchema(schema); sandbox {
		return 0, nil
	}
	if ok, why := OutsideCallsAllowed(schema); !ok {
		recordExportAttempt(schema, cfg, now, "not sent: "+why, false, 0)
		return 0, nil
	}
	cursor, err := reportExportGap(tenantID, schema, cfg.cursor)
	if err != nil {
		return 0, err
	}
	if err := reportLiveWindowGap(tenantID, schema, cursor); err != nil {
		return 0, err
	}
	events, err := readExportBatch(schema, cursor, now)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		recordExportAttempt(schema, cfg, now, "", true, cursor)
		finishBackfill(schema)
		return 0, nil
	}
	secret, err := decryptVersioned(channelCredKeys, cfg.secretEnc)
	if err != nil {
		recordExportAttempt(schema, cfg, now, "the signing secret cannot be read - set it again", false, 0)
		return 0, err
	}
	last := events[len(events)-1].Seq
	if err := postSecurityExport(context.Background(), cfg.url, string(secret), SecurityExportEvent, map[string]interface{}{
		"format": "erp-security-events/1", "tenant": tenantID, "from_seq": events[0].Seq, "to_seq": last,
		"sent_at": now.UTC().Format(time.RFC3339), "events": events, "backfill": cfg.backfillUntil > cursor,
	}, fmt.Sprintf("%s-%d-%d", schema, events[0].Seq, last)); err != nil {
		recordExportAttempt(schema, cfg, now, err.Error(), false, 0)
		return 0, err
	}
	recordExportBatch(schema, now, last, len(events))
	return len(events), nil
}

// readExportBatch walks the log in seq order and stops at the first row not yet
// certain, because the cursor only moves forward and anything it passes is never
// sent. A row is not certain while it is inside the settle delay, while a lower
// seq may still commit, or while it falls in an archived range that the next
// run reports as a gap.
//
// Open writers are read FIRST and in their own statement: read the other way
// round, a writer could commit between the snapshot and the check, its row
// invisible and the hole looking final. The settle delay closes the small window
// between taking a seq and taking a transaction id. backend_xid is visible to
// every role, unlike the query text.
func readExportBatch(schema string, cursor int64, now time.Time) ([]exportEvent, error) {
	var writerOpen bool
	if err := db.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_stat_activity
		WHERE datname = current_database() AND backend_xid IS NOT NULL AND pid <> pg_backend_pid())`).Scan(&writerOpen); err != nil {
		return nil, err
	}
	var archivedFrom int64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(MIN(from_seq), 0) FROM %s.audit_archives WHERE to_seq > $1`, schema), cursor).Scan(&archivedFrom); err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT seq, created_at, user_id, action, status, COALESCE(details, ''), COALESCE(correlation_id::text, ''),
		created_at >= $2::timestamp FROM %s.audit_logs WHERE seq > $1 ORDER BY seq LIMIT $3`, schema),
		cursor, utcStamp(now.Add(-securityExportSettle)), securityExportBatchRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []exportEvent
	prev, size := cursor, 0
	for rows.Next() {
		var e exportEvent
		var at time.Time
		var tooNew bool
		if err := rows.Scan(&e.Seq, &at, &e.User, &e.Action, &e.Status, &e.Details, &e.CorrelationID, &tooNew); err != nil {
			return nil, err
		}
		if tooNew || (writerOpen && e.Seq != prev+1) || (archivedFrom > 0 && e.Seq >= archivedFrom) {
			break
		}
		size += len(e.Details) + 200
		if len(events) > 0 && size > securityExportBatchBytes {
			break
		}
		e.At = at.UTC().Format(time.RFC3339Nano)
		events = append(events, e)
		prev = e.Seq
	}
	return events, rows.Err()
}

// postSecurityExport re-checks the address, then signs and posts. The connect
// itself is checked again by the shared outbound dialer.
func postSecurityExport(ctx context.Context, rawURL, secret, event string, payload map[string]interface{}, deliveryID string) error {
	if err := securityExportCheckURL(rawURL); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := sendWebhookHTTPContext(ctx, rawURL, secret, event, payload, deliveryID)
	// The client error quotes the whole address; a query string may hold a token.
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

// recordExportBatch moves the cursor after a 2xx.
func recordExportBatch(schema string, now time.Time, cursor int64, sent int) {
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.security_export_config SET cursor_seq = GREATEST(cursor_seq, $1), last_attempt_at = $2::timestamp,
		last_success_at = $2::timestamp, last_error = '', failures = 0, next_attempt_at = NULL, exported_total = exported_total + $3 WHERE id = 1`, schema),
		cursor, utcStamp(now), sent); err != nil {
		log.Printf("[SECURITY-EXPORT] %s: could not record the batch: %v", schema, err)
	}
	finishBackfill(schema)
}

// recordExportAttempt writes an attempt that sent nothing. A failure pushes the
// next try later, doubling up to an hour; an empty pass just stamps the time.
func recordExportAttempt(schema string, cfg securityExportConfig, now time.Time, errText string, ok bool, _ int64) {
	now = now.UTC()
	var err error
	if ok {
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.security_export_config SET last_attempt_at = $1::timestamp, last_error = '', failures = 0,
			next_attempt_at = NULL WHERE id = 1`, schema), utcStamp(now))
	} else {
		failures := cfg.failures + 1
		shift := failures
		if shift > 6 {
			shift = 6
		}
		wait := time.Minute << uint(shift)
		if wait > time.Hour {
			wait = time.Hour
		}
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.security_export_config SET last_attempt_at = $1::timestamp, last_error = $2, failures = $3,
			next_attempt_at = $4::timestamp WHERE id = 1`, schema), utcStamp(now), redactAndBoundLogField(errText, 300), failures, utcStamp(now.Add(wait)))
	}
	if err != nil {
		log.Printf("[SECURITY-EXPORT] %s: could not record the attempt: %v", schema, err)
	}
}

// reportExportGap notices events that were archived (and so removed from the
// table) before they were sent. The cursor then jumps past them and the loss
// is written on the status and as an audit row - never skipped quietly.
func reportExportGap(tenantID, schema string, cursor int64) (int64, error) {
	// Only the archive that comes next: no live row may sit between the cursor
	// and it. The archive run can skip a failed range and take a later one first,
	// and jumping to the highest would lose the live rows in between.
	var archivedTo int64
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT a.to_seq FROM %s.audit_archives a WHERE a.to_seq > $1
		AND NOT EXISTS (SELECT 1 FROM %s.audit_logs l WHERE l.seq > $1 AND l.seq < a.from_seq)
		ORDER BY a.from_seq LIMIT 1`, schema, schema), cursor).Scan(&archivedTo)
	if err == sql.ErrNoRows {
		return cursor, nil
	}
	if err != nil {
		return cursor, err
	}
	note := fmt.Sprintf("Events %d to %d were archived before they were sent (%s UTC); they are in the audit archive, not at the destination.", cursor+1, archivedTo, time.Now().UTC().Format("2006-01-02 15:04"))
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.security_export_config SET cursor_seq = $1, gap_note = $2 WHERE id = 1`, schema), archivedTo, note); err != nil {
		return cursor, err
	}
	LogAuditEvent(tenantID, "system", "SECURITY_EXPORT_GAP", "WARNING", note)
	return archivedTo, nil
}

// TestSecurityExport sends one synthetic event, with no audit data, to prove
// the address, the secret and the receiver's check. It does not move the cursor.
func TestSecurityExport(tenantID, actor string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	cfg, found, err := loadSecurityExportConfig(schema)
	if err != nil {
		return "", err
	}
	if !found || cfg.url == "" || len(cfg.secretEnc) == 0 {
		return "", errors.New("save the destination address and a signing secret first")
	}
	if sandbox, _ := IsSandboxSchema(schema); sandbox {
		return "", errors.New("a sandbox client never exports security events")
	}
	if ok, why := OutsideCallsAllowed(schema); !ok {
		return "", errors.New("nothing was sent: " + why)
	}
	secret, err := decryptVersioned(channelCredKeys, cfg.secretEnc)
	if err != nil {
		return "", errors.New("the signing secret cannot be read - set it again")
	}
	err = postSecurityExport(context.Background(), cfg.url, string(secret), securityExportTestName,
		map[string]interface{}{"format": "erp-security-events/1", "tenant": tenantID, "test": true, "sent_at": time.Now().UTC().Format(time.RFC3339)},
		schema+"-test")
	outcome := "SUCCESS"
	if err != nil {
		outcome = "FAILED"
	}
	LogAuditEvent(tenantID, actor, "SECURITY_EXPORT_TEST", outcome, "Test event to "+hostOf(cfg.url))
	if err != nil {
		return "", err
	}
	return "The destination accepted the test event.", nil
}

// StartSecurityExportWorker sends the next batch for every client that turned
// the export on. One bounded read and at most one post per client per pass.
// A suspended client is skipped: nothing is queued, so its events wait in
// audit_logs and go from the saved cursor if it is reinstated.
func StartSecurityExportWorker(ctx context.Context, interval time.Duration) {
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
				schemas, err := listTenantSchemas()
				if err != nil {
					continue
				}
				for _, schema := range schemas {
					tenantID := schemaToTenantID(schema)
					if tenantID == "" {
						continue
					}
					if _, err := RunSecurityExport(tenantID, time.Now()); err != nil {
						log.Printf("[SECURITY-EXPORT] %s: %v", schema, err)
					}
				}
			}
		}
	}()
}
