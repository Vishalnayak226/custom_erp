package main

import (
	"math"
	"sort"
)

// Pure arithmetic and the evidence shapes. Nothing here touches a network or a
// database, so every figure the evidence file reports can be unit-tested.

const evidenceSchemaVersion = 1

// Percentile is the nearest-rank percentile of values (p in 0..100). The input
// need not be sorted. Nearest rank is chosen over interpolation because every
// reported number is then a latency that really happened.
func Percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	s := append([]float64(nil), values...)
	sort.Float64s(s)
	if p <= 0 {
		return s[0]
	}
	if p >= 100 {
		return s[len(s)-1]
	}
	rank := int(math.Ceil(p / 100 * float64(len(s))))
	if rank < 1 {
		rank = 1
	}
	return s[rank-1]
}

// Call is one HTTP exchange as the recording client saw it.
type Call struct {
	Step       string  `json:"step"`
	Role       string  `json:"role"`
	Method     string  `json:"method"`
	Path       string  `json:"path"`
	Status     int     `json:"status"`
	MS         float64 `json:"ms"`
	ReqBytes   int64   `json:"req_bytes"`
	RespBytes  int64   `json:"resp_bytes"`
	Retries    int     `json:"retries"`
	Err        string  `json:"err,omitempty"`
	Expected   bool    `json:"expected_status,omitempty"`
	WriteOK    bool    `json:"-"`
	CountsAsTx bool    `json:"-"`
}

// IsError is true for a transport failure or a status the step did not expect.
func (c Call) IsError() bool {
	if c.Err != "" {
		return true
	}
	if c.Expected {
		return false
	}
	return c.Status >= 400 || c.Status == 0
}

// CallStats summarises a set of calls.
type CallStats struct {
	Calls     int     `json:"calls"`
	Errors    int     `json:"errors"`
	Retries   int     `json:"retries"`
	ReqBytes  int64   `json:"req_bytes"`
	RespBytes int64   `json:"resp_bytes"`
	P50MS     float64 `json:"api_p50_ms"`
	P95MS     float64 `json:"api_p95_ms"`
	MaxMS     float64 `json:"api_max_ms"`
	Writes    int     `json:"business_writes"`
}

// SummarizeCalls folds calls into counts, bytes and latency percentiles.
func SummarizeCalls(calls []Call) CallStats {
	var st CallStats
	var ms []float64
	for _, c := range calls {
		st.Calls++
		if c.IsError() {
			st.Errors++
		}
		st.Retries += c.Retries
		st.ReqBytes += c.ReqBytes
		st.RespBytes += c.RespBytes
		ms = append(ms, c.MS)
		if c.MS > st.MaxMS {
			st.MaxMS = c.MS
		}
		if c.CountsAsTx {
			st.Writes++
		}
	}
	st.P50MS = round2(Percentile(ms, 50))
	st.P95MS = round2(Percentile(ms, 95))
	st.MaxMS = round2(st.MaxMS)
	return st
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// DBSnap is one reading of the database counters.
type DBSnap struct {
	Source        string // "pg_stat_statements" or "pg_stat_database"
	DBBytes       int64
	HeapBytes     int64 // sum of table heap (+toast) bytes in the tenant schema
	IndexBytes    int64
	WALBytes      int64 // pg_current_wal_lsn() as a byte offset from 0/0
	XactCommit    int64
	XactRollback  int64
	TupInserted   int64
	TupUpdated    int64
	TupDeleted    int64
	TupFetched    int64
	BlksRead      int64
	BlksHit       int64
	ActiveTimeMS  float64 // pg_stat_database.active_time (PG14+)
	StmtCalls     int64   // pg_stat_statements only
	StmtTimeMS    float64 // pg_stat_statements only
	AuditRows     int64
	OutboxRows    int64
	OutboxPending int64
}

// DBDelta is what changed between two snapshots.
type DBDelta struct {
	Source        string  `json:"source"`
	QueryCount    int64   `json:"query_count"` // pg_stat_statements calls; 0 when the source cannot count queries
	QueryCountOK  bool    `json:"query_count_available"`
	DBTimeMS      float64 `json:"db_time_ms"` // statements total time, else pg_stat_database active_time
	Transactions  int64   `json:"transactions"`
	Rollbacks     int64   `json:"rollbacks"`
	TupInserted   int64   `json:"tuples_inserted"`
	TupUpdated    int64   `json:"tuples_updated"`
	TupDeleted    int64   `json:"tuples_deleted"`
	BlksRead      int64   `json:"blocks_read"`
	BlksHit       int64   `json:"blocks_hit"`
	DBGrowthBytes int64   `json:"db_growth_bytes"`
	HeapGrowth    int64   `json:"heap_growth_bytes"`
	IndexGrowth   int64   `json:"index_growth_bytes"`
	WALBytes      int64   `json:"wal_bytes"`
	AuditRows     int64   `json:"audit_rows_added"`
	OutboxRows    int64   `json:"outbox_rows_added"`
}

// Delta subtracts two snapshots. A counter that went backwards (a stats reset
// or a database restart) is reported as the new reading, never as a negative.
func Delta(a, b DBSnap) DBDelta {
	d := DBDelta{Source: b.Source}
	nz := func(x, y int64) int64 {
		if y < x {
			return y
		}
		return y - x
	}
	d.Transactions = nz(a.XactCommit, b.XactCommit)
	d.Rollbacks = nz(a.XactRollback, b.XactRollback)
	d.TupInserted = nz(a.TupInserted, b.TupInserted)
	d.TupUpdated = nz(a.TupUpdated, b.TupUpdated)
	d.TupDeleted = nz(a.TupDeleted, b.TupDeleted)
	d.BlksRead = nz(a.BlksRead, b.BlksRead)
	d.BlksHit = nz(a.BlksHit, b.BlksHit)
	d.WALBytes = nz(a.WALBytes, b.WALBytes)
	d.DBGrowthBytes = b.DBBytes - a.DBBytes
	d.HeapGrowth = b.HeapBytes - a.HeapBytes
	d.IndexGrowth = b.IndexBytes - a.IndexBytes
	d.AuditRows = b.AuditRows - a.AuditRows
	d.OutboxRows = b.OutboxRows - a.OutboxRows
	if b.Source == "pg_stat_statements" && a.Source == b.Source {
		d.QueryCount = nz(a.StmtCalls, b.StmtCalls)
		d.QueryCountOK = true
		if b.StmtTimeMS >= a.StmtTimeMS {
			d.DBTimeMS = round2(b.StmtTimeMS - a.StmtTimeMS)
		} else {
			d.DBTimeMS = round2(b.StmtTimeMS)
		}
	} else if b.ActiveTimeMS >= a.ActiveTimeMS {
		d.DBTimeMS = round2(b.ActiveTimeMS - a.ActiveTimeMS)
	} else {
		d.DBTimeMS = round2(b.ActiveTimeMS)
	}
	return d
}

// AddDelta sums two deltas (a journey is the sum of its steps).
func AddDelta(a, b DBDelta) DBDelta {
	a.QueryCount += b.QueryCount
	a.QueryCountOK = a.QueryCountOK || b.QueryCountOK
	a.DBTimeMS = round2(a.DBTimeMS + b.DBTimeMS)
	a.Transactions += b.Transactions
	a.Rollbacks += b.Rollbacks
	a.TupInserted += b.TupInserted
	a.TupUpdated += b.TupUpdated
	a.TupDeleted += b.TupDeleted
	a.BlksRead += b.BlksRead
	a.BlksHit += b.BlksHit
	a.DBGrowthBytes += b.DBGrowthBytes
	a.HeapGrowth += b.HeapGrowth
	a.IndexGrowth += b.IndexGrowth
	a.WALBytes += b.WALBytes
	a.AuditRows += b.AuditRows
	a.OutboxRows += b.OutboxRows
	if a.Source == "" {
		a.Source = b.Source
	}
	return a
}

// Per1k scales a total to a per-1000-transactions figure. With no transactions
// there is no honest figure, so it returns 0 and the caller says "n/a".
func Per1k(total int64, transactions int) float64 {
	if transactions <= 0 {
		return 0
	}
	return round2(float64(total) / float64(transactions) * 1000)
}

// ProcSnap is the server process's CPU time and resident memory.
type ProcSnap struct {
	OK     bool
	CPUMS  float64
	RSSKB  int64
	PeakKB int64
}

// ProcDelta is the change over a step; RSS is the reading at the end.
type ProcDelta struct {
	Available bool    `json:"available"`
	CPUMS     float64 `json:"cpu_ms"`
	RSSKB     int64   `json:"rss_kb"`
	PeakRSSKB int64   `json:"peak_rss_kb"`
}

// ProcDiff reports CPU consumed between two readings and the later memory.
func ProcDiff(a, b ProcSnap) ProcDelta {
	if !a.OK || !b.OK {
		return ProcDelta{}
	}
	cpu := b.CPUMS - a.CPUMS
	if cpu < 0 {
		cpu = b.CPUMS // the process was replaced
	}
	return ProcDelta{Available: true, CPUMS: round2(cpu), RSSKB: b.RSSKB, PeakRSSKB: b.PeakKB}
}

// ReconCheck is one line of reconciliation evidence.
type ReconCheck struct {
	Kind   string  `json:"kind"` // money, stock, subledger, gl, audit
	Name   string  `json:"name"`
	Pass   bool    `json:"pass"`
	Warn   bool    `json:"warn,omitempty"` // true: reported, does not turn the evidence red
	Diff   float64 `json:"difference,omitempty"`
	Detail string  `json:"detail,omitempty"`
}

// ReconPassed is true when no non-warning check failed.
func ReconPassed(checks []ReconCheck) bool {
	for _, c := range checks {
		if !c.Pass && !c.Warn {
			return false
		}
	}
	return true
}

// Verdict is the journey status from its steps and its reconciliation. A
// journey is green only if no step errored and every reconciliation tied.
func Verdict(stepErrors int, stepFailed bool, checks []ReconCheck) string {
	if stepFailed || stepErrors > 0 || !ReconPassed(checks) {
		return "fail"
	}
	return "pass"
}

// StepRecord is one step of a journey in the evidence file.
type StepRecord struct {
	Name   string    `json:"name"`
	Role   string    `json:"role"`
	WallMS float64   `json:"wall_ms"`
	HTTP   CallStats `json:"http"`
	Status []int     `json:"http_statuses"`
	DB     DBDelta   `json:"db"`
	Proc   ProcDelta `json:"server_process"`
	Errors []string  `json:"errors,omitempty"`
}

// JourneyRecord is one journey in the evidence file.
type JourneyRecord struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Status   string       `json:"status"` // pass, fail, blocked, not_driven
	Reason   string       `json:"reason,omitempty"`
	Roles    []string     `json:"roles"`
	WallMS   float64      `json:"wall_ms"`
	Steps    []StepRecord `json:"steps"`
	Summary  JourneySum   `json:"summary"`
	Recon    []ReconCheck `json:"reconciliation"`
	Evidence []string     `json:"notes,omitempty"`
}

// JourneySum is the per-journey rollup, including growth per 1k transactions.
type JourneySum struct {
	HTTP           CallStats `json:"http"`
	DB             DBDelta   `json:"db"`
	CPUMS          float64   `json:"server_cpu_ms"`
	RSSKBEnd       int64     `json:"server_rss_kb_end"`
	PeakRSSKB      int64     `json:"server_peak_rss_kb"`
	Writes         int       `json:"business_writes"`
	HeapPer1k      float64   `json:"heap_bytes_per_1k_tx"`
	IndexPer1k     float64   `json:"index_bytes_per_1k_tx"`
	WALPer1k       float64   `json:"wal_bytes_per_1k_tx"`
	AuditRowsPer1k float64   `json:"audit_rows_per_1k_tx"`
	OutboxPer1k    float64   `json:"outbox_rows_per_1k_tx"`
}

// Summarize rolls a journey's steps up.
func Summarize(steps []StepRecord, calls []Call) JourneySum {
	var s JourneySum
	s.HTTP = SummarizeCalls(calls)
	for _, st := range steps {
		s.DB = AddDelta(s.DB, st.DB)
		s.CPUMS += st.Proc.CPUMS
		if st.Proc.RSSKB > 0 {
			s.RSSKBEnd = st.Proc.RSSKB
		}
		if st.Proc.PeakRSSKB > s.PeakRSSKB {
			s.PeakRSSKB = st.Proc.PeakRSSKB
		}
	}
	s.CPUMS = round2(s.CPUMS)
	s.Writes = s.HTTP.Writes
	s.HeapPer1k = Per1k(s.DB.HeapGrowth, s.Writes)
	s.IndexPer1k = Per1k(s.DB.IndexGrowth, s.Writes)
	s.WALPer1k = Per1k(s.DB.WALBytes, s.Writes)
	s.AuditRowsPer1k = Per1k(s.DB.AuditRows, s.Writes)
	s.OutboxPer1k = Per1k(s.DB.OutboxRows, s.Writes)
	return s
}

// Overall is red if any journey that was driven failed.
func Overall(js []JourneyRecord) string {
	driven := 0
	for _, j := range js {
		switch j.Status {
		case "fail":
			return "fail"
		case "pass":
			driven++
		}
	}
	if driven == 0 {
		return "not_driven"
	}
	return "pass"
}

// DiskProjection extends a measured growth rate. Rate is bytes per second over
// the measured window; the result is the projected total after horizonHours.
type DiskProjection struct {
	MeasuredSeconds float64 `json:"measured_seconds"`
	MeasuredBytes   int64   `json:"measured_growth_bytes"`
	BytesPerHour    float64 `json:"bytes_per_hour"`
	HorizonHours    float64 `json:"horizon_hours"`
	ProjectedBytes  int64   `json:"projected_growth_bytes"`
	Basis           string  `json:"basis"`
}

// ProjectDisk is linear: it says so, because load, retention and vacuum all
// bend the real curve, and a linear figure is the conservative planning one.
func ProjectDisk(seconds float64, grown int64, horizonHours float64) DiskProjection {
	p := DiskProjection{MeasuredSeconds: round2(seconds), MeasuredBytes: grown, HorizonHours: horizonHours, Basis: "linear extrapolation of the measured window at the same load; real growth bends with retention, vacuum and load"}
	if seconds <= 0 || grown <= 0 {
		return p
	}
	p.BytesPerHour = round2(float64(grown) / seconds * 3600)
	p.ProjectedBytes = int64(p.BytesPerHour * horizonHours)
	return p
}
