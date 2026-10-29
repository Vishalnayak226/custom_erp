package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
)

// Stage 47.18.2: bytes per business transaction, measured on a synthetic
// tenant (GenerateSyntheticTenant with Measure on).
//
// How: every row version PostgreSQL stores carries the id of the transaction
// that wrote it (xmin). The generator notes the transaction-id window each
// business transaction ran in - one sale, one return, one receipt - and
// afterwards every row of the tenant's schema is summed by xmin into those
// windows. A window holds every database transaction the business step ran
// (a sale's checkout plus its loyalty earn, a return's six steps), and only
// rows in this tenant's schema are read, so another session on the same
// cluster taking ids inside a window adds nothing.
//
// What it counts: the new row versions a step wrote (pg_column_size of each
// row, so a TOASTed value counts compressed). An UPDATE's new version counts;
// the dead version it leaves until vacuum does not. Index entries, page slack
// and TOAST chunk overhead are not per-row; they come in as one measured
// factor - the schema's pg_total_relation_size over its live row bytes - and
// TotalP50/TotalP95 are the row figures times that factor, a projection.

// SynthFootprint is one kind of business transaction's measured size.
type SynthFootprint struct {
	Kind     string  `json:"kind"`
	Count    int     `json:"count"`     // business transactions (rows, for masters)
	RowsMean float64 `json:"rows_mean"` // row versions written per transaction
	HeapP50  int64   `json:"heap_p50"`  // measured row bytes per transaction
	HeapP95  int64   `json:"heap_p95"`
	HeapMean int64   `json:"heap_mean"`
	Factor   float64 `json:"factor"`    // schema total size / live row bytes, measured
	TotalP50 int64   `json:"total_p50"` // HeapP50 x Factor - projected
	TotalP95 int64   `json:"total_p95"`
}

type synthMeter struct {
	windows []synthWindow
}

type synthWindow struct {
	kind   string
	n      int // business items in the window (a master import chunk holds many)
	lo, hi uint32
}

// measure runs fn and, when measuring, records the transaction-id window it
// ran in. A failed step is not recorded.
func (g *synthGen) measure(kind string, n int, fn func() error) error {
	if g.meter == nil {
		return fn()
	}
	lo, err := snapshotXmax()
	if err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	hi, err := snapshotXmax()
	if err != nil {
		return err
	}
	g.meter.windows = append(g.meter.windows, synthWindow{kind: kind, n: n, lo: lo, hi: hi})
	return nil
}

// snapshotXmax is the next transaction id the cluster will hand out, in the
// 32-bit form xmin is stored in. Reading it assigns no id.
func snapshotXmax() (uint32, error) {
	var x uint64
	if err := db.DB.QueryRow(`SELECT pg_snapshot_xmax(pg_current_snapshot())::text::bigint`).Scan(&x); err != nil {
		return 0, err
	}
	return uint32(x), nil
}

type xidBytes struct {
	xid   uint32
	rows  int64
	bytes int64
}

func (m *synthMeter) footprint(schema string) ([]SynthFootprint, error) {
	tables, err := schemaBaseTables(schema)
	if err != nil {
		return nil, err
	}
	byXid := map[uint32]*xidBytes{}
	var liveBytes, totalSize int64
	for _, t := range tables {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT xmin::text::bigint, COUNT(*), COALESCE(SUM(pg_column_size(t.*)), 0) FROM %s.%s t GROUP BY 1`, schema, t))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t, err)
		}
		for rows.Next() {
			var x uint64
			var n, b int64
			if err := rows.Scan(&x, &n, &b); err != nil {
				rows.Close()
				return nil, err
			}
			e := byXid[uint32(x)]
			if e == nil {
				e = &xidBytes{xid: uint32(x)}
				byXid[uint32(x)] = e
			}
			e.rows += n
			e.bytes += b
			liveBytes += b
		}
		rows.Close()
		var size int64
		if err := db.DB.QueryRow(`SELECT pg_total_relation_size($1::regclass)`, schema+"."+t).Scan(&size); err != nil {
			return nil, err
		}
		totalSize += size
	}
	factor := 1.0
	if liveBytes > 0 {
		factor = float64(totalSize) / float64(liveBytes)
	}
	xids := make([]*xidBytes, 0, len(byXid))
	for _, e := range byXid {
		xids = append(xids, e)
	}
	sort.Slice(xids, func(i, j int) bool { return xids[i].xid < xids[j].xid })

	type acc struct {
		per   []int64
		rows  float64
		count int
	}
	kinds := map[string]*acc{}
	var order []string
	for _, w := range m.windows {
		// Windows are sequential and non-overlapping; a binary search finds
		// the first id inside each.
		i := sort.Search(len(xids), func(i int) bool { return xids[i].xid >= w.lo })
		var rows, bytes int64
		for ; i < len(xids) && xids[i].xid < w.hi; i++ {
			rows += xids[i].rows
			bytes += xids[i].bytes
		}
		a := kinds[w.kind]
		if a == nil {
			a = &acc{}
			kinds[w.kind] = a
			order = append(order, w.kind)
		}
		n := max(w.n, 1)
		for k := 0; k < n; k++ {
			a.per = append(a.per, bytes/int64(n))
		}
		a.rows += float64(rows)
		a.count += n
	}
	out := make([]SynthFootprint, 0, len(order))
	for _, k := range order {
		a := kinds[k]
		sort.Slice(a.per, func(i, j int) bool { return a.per[i] < a.per[j] })
		var sum int64
		for _, v := range a.per {
			sum += v
		}
		f := SynthFootprint{Kind: k, Count: a.count, RowsMean: a.rows / float64(a.count),
			HeapP50: percentileInt(a.per, 50), HeapP95: percentileInt(a.per, 95), HeapMean: sum / int64(len(a.per)), Factor: factor}
		f.TotalP50 = int64(float64(f.HeapP50) * factor)
		f.TotalP95 = int64(float64(f.HeapP95) * factor)
		out = append(out, f)
	}
	return out, nil
}

// percentileInt is the nearest-rank percentile of an ascending slice.
func percentileInt(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := (p*len(sorted) + 99) / 100
	return sorted[min(max(rank, 1), len(sorted))-1]
}

func schemaBaseTables(schema string) ([]string, error) {
	rows, err := db.DB.Query(`SELECT table_name FROM information_schema.tables WHERE table_schema = $1 AND table_type = 'BASE TABLE' ORDER BY 1`, schema)
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
