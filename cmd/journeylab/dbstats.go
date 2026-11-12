package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// DBStats reads the database counters. It prefers pg_stat_statements (real
// query counts and time) when the extension is installed and loaded; otherwise
// it falls back to pg_stat_database, which counts transactions, not queries.
// The source is named in every reading so the evidence never overstates.
type DBStats struct {
	DB     *sql.DB
	Schema string
	Source string
	Settle time.Duration
}

// NewDBStats chooses the source by probing the view.
func NewDBStats(db *sql.DB, schema string, settle time.Duration) *DBStats {
	d := &DBStats{DB: db, Schema: schema, Settle: settle, Source: "pg_stat_database"}
	var n sql.NullInt64
	if err := db.QueryRow(`SELECT COALESCE(SUM(calls),0) FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&n); err == nil {
		d.Source = "pg_stat_statements"
	}
	return d
}

// Snap takes one reading. Statistics flush lazily, so it waits Settle first.
func (d *DBStats) Snap() DBSnap {
	if d.Settle > 0 {
		time.Sleep(d.Settle)
	}
	s := DBSnap{Source: d.Source}
	_ = d.DB.QueryRow(`SELECT pg_database_size(current_database()), (pg_current_wal_lsn() - '0/0'::pg_lsn)::bigint`).Scan(&s.DBBytes, &s.WALBytes)
	_ = d.DB.QueryRow(`SELECT xact_commit, xact_rollback, tup_inserted, tup_updated, tup_deleted, tup_fetched, blks_read, blks_hit, COALESCE(active_time,0)
		FROM pg_stat_database WHERE datname = current_database()`).
		Scan(&s.XactCommit, &s.XactRollback, &s.TupInserted, &s.TupUpdated, &s.TupDeleted, &s.TupFetched, &s.BlksRead, &s.BlksHit, &s.ActiveTimeMS)
	if d.Source == "pg_stat_statements" {
		_ = d.DB.QueryRow(`SELECT COALESCE(SUM(calls),0), COALESCE(SUM(total_exec_time),0) FROM pg_stat_statements
			WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database()) AND query NOT ILIKE '%pg_stat_%' AND query NOT ILIKE '%pg_database_size%'`).Scan(&s.StmtCalls, &s.StmtTimeMS)
	}
	_ = d.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(pg_table_size(c.oid)) FILTER (WHERE c.relkind = 'r'),0),
		COALESCE(SUM(pg_indexes_size(c.oid)) FILTER (WHERE c.relkind = 'r'),0)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = %s`, quoteLit(d.Schema))).Scan(&s.HeapBytes, &s.IndexBytes)
	_ = d.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.audit_logs`, d.Schema)).Scan(&s.AuditRows)
	_ = d.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'Pending') FROM %s.integration_event_outbox`, d.Schema)).Scan(&s.OutboxRows, &s.OutboxPending)
	return s
}

func quoteLit(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
