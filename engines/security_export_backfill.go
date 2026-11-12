package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"log"
	"time"
)

// Stage 49.11.6 (history backfill and the live-window gap check) on the
// optional security-event export (security_export.go).
//
// Backfill: an administrator picks a start date and the cursor moves back to
// just before the first event on or after it. The same worker then sends
// strictly in seq order through the same batch limits, and the old cursor
// position is where the backfill is complete. It reads the log like any other
// pass and is never called from a write, so a backfill cannot slow a business
// transaction. Each batch says "backfill": true so the receiver can tell a
// replay from live events; the "seq" of each event is its de-duplication key.
//
// Live-window gap check: the periodic evidence checkpoints say how many rows a
// window held when it was sealed. A sealed window that is not archived and now
// holds fewer rows has lost events from the live table; if they were not sent
// yet they never will be. It is reported once (the status note and a
// SECURITY_EXPORT_GAP audit row) - the cursor does not move.
//
// The body is still TLS plus the signature; backfill adds no new encryption.

const (
	// Backfill reaches back at most this far, and replays at most this many events.
	securityExportBackfillMaxDays = 400
	securityExportBackfillMaxRows = 500000
	// Sealed windows examined per pass.
	securityExportLiveGapWindows = 50
)

// StartSecurityExportBackfill rewinds the export to the first event on or after
// from (a date, UTC) and returns how many events will be replayed.
func StartSecurityExportBackfill(tenantID, actor string, from time.Time, now time.Time) (int64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	cfg, found, err := loadSecurityExportConfig(schema)
	if err != nil {
		return 0, err
	}
	if !found || !cfg.enabled {
		return 0, errors.New("turn the export on before sending history")
	}
	if cfg.backfillUntil > cfg.cursor {
		return 0, errors.New("a history send is already running; wait until it finishes")
	}
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	if from.After(now.UTC().AddDate(0, 0, 1)) {
		return 0, errors.New("the start date cannot be in the future")
	}
	if from.Before(now.UTC().AddDate(0, 0, -securityExportBackfillMaxDays)) {
		return 0, fmt.Errorf("history can start at most %d days back", securityExportBackfillMaxDays)
	}
	var startSeq int64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(MIN(seq), 0) FROM %s.audit_logs WHERE created_at >= $1::timestamp AND seq <= $2`, schema),
		utcStamp(from), cfg.cursor).Scan(&startSeq); err != nil {
		return 0, err
	}
	if startSeq == 0 {
		return 0, errors.New("there are no events between that date and where the export already is")
	}
	var rows int64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT 1 FROM %s.audit_logs WHERE seq >= $1 AND seq <= $2 LIMIT %d) w`, schema, securityExportBackfillMaxRows+1),
		startSeq, cfg.cursor).Scan(&rows); err != nil {
		return 0, err
	}
	if rows > securityExportBackfillMaxRows {
		return 0, fmt.Errorf("that is more than %d events; choose a later start date", securityExportBackfillMaxRows)
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.security_export_config SET cursor_seq = $1, backfill_until_seq = $2, backfill_from = $3::timestamp,
		failures = 0, next_attempt_at = NULL WHERE id = 1 AND cursor_seq = $2 AND backfill_until_seq <= cursor_seq`, schema),
		startSeq-1, cfg.cursor, utcStamp(from))
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, errors.New("the export moved while this was set up; try again")
	}
	LogAuditEvent(tenantID, actor, "SECURITY_EXPORT_CONFIG", "SUCCESS",
		fmt.Sprintf("Security event export history from %s requested: %d events will be sent again", from.Format("2006-01-02"), rows))
	return rows, nil
}

// finishBackfill ends a backfill the cursor has caught up with.
func finishBackfill(schema string) {
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.security_export_config SET backfill_until_seq = 0, backfill_from = NULL
		WHERE id = 1 AND backfill_until_seq > 0 AND cursor_seq >= backfill_until_seq`, schema)); err != nil {
		log.Printf("[SECURITY-EXPORT] %s: could not close the backfill: %v", schema, err)
	}
}

// reportLiveWindowGap checks the sealed, not-archived windows still ahead of
// the cursor against the live table. Each window is checked once.
func reportLiveWindowGap(tenantID, schema string, cursor int64) error {
	var checked int64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT live_gap_checked_seq FROM %s.security_export_config WHERE id = 1`, schema)).Scan(&checked); err != nil {
		return err
	}
	low := checked
	if cursor > low {
		low = cursor
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT c.from_seq, c.to_seq, c.row_count,
		(SELECT COUNT(*) FROM %[1]s.audit_logs l WHERE l.seq BETWEEN c.from_seq AND c.to_seq)
		FROM %[1]s.audit_checkpoints c
		WHERE c.to_seq > $1 AND c.archive_id IS NULL
		  AND NOT EXISTS (SELECT 1 FROM %[1]s.audit_archives a WHERE a.from_seq <= c.to_seq AND a.to_seq >= c.from_seq)
		ORDER BY c.to_seq LIMIT %[2]d`, schema, securityExportLiveGapWindows), low)
	if err != nil {
		return err
	}
	type win struct{ from, to, expected, live int64 }
	var wins []win
	for rows.Next() {
		var w win
		if err := rows.Scan(&w.from, &w.to, &w.expected, &w.live); err != nil {
			rows.Close()
			return err
		}
		wins = append(wins, w)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(wins) == 0 {
		return nil
	}
	var notes []string
	for _, w := range wins {
		if w.live < w.expected {
			note := fmt.Sprintf("%d events of the sealed window %d to %d are no longer in the live log (%s UTC); any not yet sent cannot be.",
				w.expected-w.live, w.from, w.to, time.Now().UTC().Format("2006-01-02 15:04"))
			notes = append(notes, note)
			LogAuditEvent(tenantID, "system", "SECURITY_EXPORT_GAP", "WARNING", note)
		}
	}
	// The checked mark moves whether or not a window was short: each is reported once.
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.security_export_config SET live_gap_checked_seq = GREATEST(live_gap_checked_seq, $1) WHERE id = 1`, schema), wins[len(wins)-1].to); err != nil {
		return err
	}
	if len(notes) > 0 {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.security_export_config SET gap_note = LEFT(CASE WHEN gap_note = '' THEN $1 ELSE gap_note || ' ' || $1 END, 1500) WHERE id = 1`, schema), notes[len(notes)-1]); err != nil {
			return err
		}
	}
	return nil
}
