package engines

import (
	"context"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Stage 47.9.3: slow-query fingerprints, bounded, with no parameter values.
//
// Where pg_stat_statements is loaded (a benchmark or staging host that set
// shared_preload_libraries and ran CREATE EXTENSION), its cumulative totals
// are read - calls, total and max time, shared buffers touched. Everywhere
// else a sampler looks at pg_stat_activity every few seconds and records any
// statement that has been running longer than the threshold.
//
// What is kept is a fingerprint, never a statement as it ran: comments go,
// every literal ('text', 42, 1.5e3, $$body$$, E'..') and every bind
// placeholder ($1) becomes ?, IN-lists and VALUES rows collapse to one, and a
// client's schema name becomes tenant_? - so the same query across clients is
// one row, and no client name, customer value or secret survives. Bind
// parameter values are never read at all: pg_stat_activity does not show
// them and nothing here asks for them.
//
// The store is fixed-size (slowQueryCapacity fingerprints, each capped at
// maxFingerprintChars): when it is full a new fingerprint replaces the one
// with the least total time, so memory never grows with traffic.

// QueryFingerprint is one normalised statement's accumulated cost.
type QueryFingerprint struct {
	ID      string  `json:"id"`
	Query   string  `json:"query"`
	Calls   int64   `json:"calls"`
	TotalMs float64 `json:"total_ms"`
	MaxMs   float64 `json:"max_ms"`
	MeanMs  float64 `json:"mean_ms"`
	// SharedBlocks is shared buffers hit + read - pg_stat_statements only.
	SharedBlocks int64     `json:"shared_blocks,omitempty"`
	LastSeen     time.Time `json:"last_seen"`
}

const (
	slowQueryCapacity   = 100
	maxFingerprintChars = 1000
)

var (
	fpWhitespace = regexp.MustCompile(`\s+`)
	fpTenant     = regexp.MustCompile(`(?i)"?\btenant_[a-z0-9_]+"?\.`)
	fpList       = regexp.MustCompile(`\(\s*\?(?:\s*,\s*\?)+\s*\)`)
	fpRows       = regexp.MustCompile(`\(\?\)(?:\s*,\s*\(\?\))+`)
	fpArray      = regexp.MustCompile(`(?i)ARRAY\[\s*\?(?:\s*,\s*\?)+\s*\]`)
	fpDollarTag  = regexp.MustCompile(`^\$[A-Za-z_][A-Za-z0-9_]*\$|^\$\$`)
)

func fpIdentByte(c byte) bool {
	return c == '_' || c == '.' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

// NormalizeQuery turns a statement into its fingerprint text.
func NormalizeQuery(q string) string {
	var b strings.Builder
	b.Grow(len(q))
	last := func() byte {
		s := b.String()
		if s == "" {
			return ' '
		}
		return s[len(s)-1]
	}
	n := len(q)
	for i := 0; i < n; {
		c := q[i]
		switch {
		case c == '-' && i+1 < n && q[i+1] == '-':
			for i < n && q[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case c == '/' && i+1 < n && q[i+1] == '*':
			end := strings.Index(q[i+2:], "*/")
			if end < 0 {
				i = n
			} else {
				i += end + 4
			}
			b.WriteByte(' ')
		case c == '\'' || ((c == 'E' || c == 'e') && i+1 < n && q[i+1] == '\'' && !fpIdentByte(last())):
			escapes := c != '\''
			if escapes {
				i++
			}
			i++ // opening quote
			for i < n {
				if escapes && q[i] == '\\' {
					i += 2
					continue
				}
				if q[i] == '\'' {
					if i+1 < n && q[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			b.WriteByte('?')
		case c == '$' && i+1 < n && q[i+1] >= '0' && q[i+1] <= '9' && !fpIdentByte(last()):
			i++
			for i < n && q[i] >= '0' && q[i] <= '9' {
				i++
			}
			b.WriteByte('?')
		case c == '$' && fpDollarTag.MatchString(q[i:]):
			tag := fpDollarTag.FindString(q[i:])
			end := strings.Index(q[i+len(tag):], tag)
			if end < 0 {
				i = n
			} else {
				i += len(tag) + end + len(tag)
			}
			b.WriteByte('?')
		case c == '"':
			end := strings.IndexByte(q[i+1:], '"')
			if end < 0 {
				b.WriteString(q[i:])
				i = n
			} else {
				b.WriteString(q[i : i+end+2])
				i += end + 2
			}
		case c >= '0' && c <= '9' && !fpIdentByte(last()):
			for i < n && ((q[i] >= '0' && q[i] <= '9') || q[i] == '.' || q[i] == '_') {
				i++
			}
			if i < n && (q[i] == 'e' || q[i] == 'E') {
				j := i + 1
				if j < n && (q[j] == '+' || q[j] == '-') {
					j++
				}
				if j < n && q[j] >= '0' && q[j] <= '9' {
					i = j
					for i < n && q[i] >= '0' && q[i] <= '9' {
						i++
					}
				}
			}
			b.WriteByte('?')
		default:
			b.WriteByte(c)
			i++
		}
	}
	out := fpWhitespace.ReplaceAllString(b.String(), " ")
	out = fpTenant.ReplaceAllString(out, "tenant_?.")
	out = fpList.ReplaceAllString(out, "(?)")
	out = fpRows.ReplaceAllString(out, "(?)")
	out = fpArray.ReplaceAllString(out, "ARRAY[?]")
	out = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(out), ";"))
	if r := []rune(out); len(r) > maxFingerprintChars {
		out = string(r[:maxFingerprintChars]) + "…"
	}
	return out
}

func fingerprintID(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}

// FingerprintStore keeps at most max fingerprints, the heaviest by total time.
type FingerprintStore struct {
	mu  sync.Mutex
	max int
	m   map[string]*QueryFingerprint
}

// NewFingerprintStore makes a store for at most max fingerprints.
func NewFingerprintStore(max int) *FingerprintStore {
	return &FingerprintStore{max: max, m: map[string]*QueryFingerprint{}}
}

// Record adds cost to a statement's fingerprint. raw is normalised here, so a
// caller can never store it as it ran. calls is how many executions this
// observation is (0 when it extends one already counted), addMs the time to
// add, peakMs the longest single execution seen in it.
func (s *FingerprintStore) Record(raw string, calls int64, addMs, peakMs float64, blocks int64, at time.Time) {
	norm := NormalizeQuery(raw)
	if norm == "" {
		return
	}
	id := fingerprintID(norm)
	s.mu.Lock()
	defer s.mu.Unlock()
	fp := s.m[id]
	if fp == nil {
		if len(s.m) >= s.max {
			var lightest *QueryFingerprint
			for _, v := range s.m {
				if lightest == nil || v.TotalMs < lightest.TotalMs {
					lightest = v
				}
			}
			if lightest.TotalMs > addMs {
				return // lighter than everything kept
			}
			delete(s.m, lightest.ID)
		}
		fp = &QueryFingerprint{ID: id, Query: norm}
		s.m[id] = fp
	}
	fp.Calls += calls
	fp.TotalMs += addMs
	fp.MaxMs = max(fp.MaxMs, peakMs)
	fp.SharedBlocks += blocks
	if fp.Calls > 0 {
		fp.MeanMs = fp.TotalMs / float64(fp.Calls)
	}
	if at.After(fp.LastSeen) {
		fp.LastSeen = at
	}
}

// Len is how many fingerprints are held.
func (s *FingerprintStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}

// Top returns up to n fingerprints, heaviest first: by "total" time or by
// shared "buffers".
func (s *FingerprintStore) Top(n int, by string) []QueryFingerprint {
	s.mu.Lock()
	out := make([]QueryFingerprint, 0, len(s.m))
	for _, v := range s.m {
		out = append(out, *v)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if by == "buffers" && out[i].SharedBlocks != out[j].SharedBlocks {
			return out[i].SharedBlocks > out[j].SharedBlocks
		}
		if out[i].TotalMs != out[j].TotalMs {
			return out[i].TotalMs > out[j].TotalMs
		}
		return out[i].ID < out[j].ID
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// SlowQuerySampler feeds a store from pg_stat_activity. A statement still
// running at the next sample is the same execution: its extra time is added
// and it is not counted twice.
type SlowQuerySampler struct {
	Store     *FingerprintStore
	Threshold time.Duration
	mu        sync.Mutex
	inflight  map[string]float64 // pid@start -> ms already recorded
}

// SampleOnce takes one look at what is running now.
func (s *SlowQuerySampler) SampleOnce() error {
	rows, err := db.DB.Query(`SELECT pid, query_start, query, EXTRACT(EPOCH FROM (clock_timestamp() - query_start)) * 1000
		FROM pg_stat_activity
		WHERE datname = current_database() AND state = 'active' AND backend_type = 'client backend'
		  AND pid <> pg_backend_pid() AND query_start IS NOT NULL
		  AND query_start < clock_timestamp() - make_interval(secs => $1)`, s.Threshold.Seconds())
	if err != nil {
		return err
	}
	defer rows.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight == nil {
		s.inflight = map[string]float64{}
	}
	now := time.Now()
	seen := map[string]bool{}
	for rows.Next() {
		var pid int
		var started time.Time
		var query string
		var ms float64
		if err := rows.Scan(&pid, &started, &query, &ms); err != nil {
			return err
		}
		if strings.HasPrefix(query, "<insufficient privilege>") {
			continue
		}
		key := strconv.Itoa(pid) + "@" + strconv.FormatInt(started.UnixMicro(), 10)
		seen[key] = true
		if prev, ok := s.inflight[key]; ok {
			s.Store.Record(query, 0, max(ms-prev, 0), ms, 0, now)
		} else {
			s.Store.Record(query, 1, ms, ms, 0, now)
		}
		s.inflight[key] = ms
	}
	for k := range s.inflight {
		if !seen[k] {
			delete(s.inflight, k)
		}
	}
	return rows.Err()
}

// Run samples every interval until ctx ends.
func (s *SlowQuerySampler) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if db.DB == nil {
				continue
			}
			if err := s.SampleOnce(); err != nil && ctx.Err() == nil {
				log.Printf("[SLOW-QUERY] sample: %v", err)
			}
		}
	}
}

var serverSlowQueries = &SlowQuerySampler{Store: NewFingerprintStore(slowQueryCapacity)}

// slowQueryThreshold reads ERP_SLOW_QUERY_MS: the running time from which a
// statement is recorded (default 1000 ms; 0 turns the sampler off).
func slowQueryThreshold() time.Duration {
	ms := 1000
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ERP_SLOW_QUERY_MS"))); err == nil && v >= 0 {
		ms = v
	}
	return time.Duration(ms) * time.Millisecond
}

// StartSlowQuerySampler starts the server's sampler. Each server keeps its own
// in-memory store; nothing is written to the database.
func StartSlowQuerySampler(ctx context.Context, interval time.Duration) {
	threshold := slowQueryThreshold()
	if threshold <= 0 {
		return
	}
	serverSlowQueries.Threshold = threshold
	go serverSlowQueries.Run(ctx, interval)
}

// SlowQuerySummary is what the platform Control Panel shows.
type SlowQuerySummary struct {
	Source      string             `json:"source"` // pg_stat_statements | pg_stat_activity sampling | off
	ThresholdMs int64              `json:"threshold_ms,omitempty"`
	ByTotal     []QueryFingerprint `json:"by_total"`
	ByBuffers   []QueryFingerprint `json:"by_buffers,omitempty"`
}

var errNoStatStatements = errors.New("pg_stat_statements is not installed in this database")

// ReadStatStatements folds pg_stat_statements' rows for this database into a
// fresh store - its own text already has placeholders, NormalizeQuery still
// strips client schema names and collapses lists.
func ReadStatStatements(limit int) (*FingerprintStore, error) {
	var installed bool
	if err := db.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).Scan(&installed); err != nil {
		return nil, err
	}
	if !installed {
		return nil, errNoStatStatements
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT query, calls, total_exec_time, max_exec_time, shared_blks_hit + shared_blks_read
		FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
		ORDER BY total_exec_time DESC LIMIT %d`, limit*5))
	if err != nil {
		return nil, err // not preloaded, or an old version's column names
	}
	defer rows.Close()
	store := NewFingerprintStore(limit)
	now := time.Now()
	for rows.Next() {
		var q string
		var calls, blocks int64
		var total, peak float64
		if err := rows.Scan(&q, &calls, &total, &peak, &blocks); err != nil {
			return nil, err
		}
		store.Record(q, calls, total, peak, blocks, now)
	}
	return store, rows.Err()
}

// SlowQueries reports the heaviest fingerprints: pg_stat_statements where it
// can be read, this server's sampler otherwise.
func SlowQueries(limit int) SlowQuerySummary {
	if store, err := ReadStatStatements(limit); err == nil {
		return SlowQuerySummary{Source: "pg_stat_statements", ByTotal: store.Top(limit, "total"), ByBuffers: store.Top(limit, "buffers")}
	}
	if serverSlowQueries.Threshold <= 0 {
		return SlowQuerySummary{Source: "off", ByTotal: []QueryFingerprint{}}
	}
	return SlowQuerySummary{Source: "pg_stat_activity sampling", ThresholdMs: serverSlowQueries.Threshold.Milliseconds(),
		ByTotal: serverSlowQueries.Store.Top(limit, "total")}
}
