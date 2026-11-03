package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"time"
)

// Stage 47.9.3b / c: buffer figures and a stored baseline for the slow-query
// evidence. The baseline is one row of public.ops_run_log (run_type
// 'query_baseline'); saving again replaces it, so the table stays bounded.
// It holds fingerprints only - the same normalised text the page shows.

const queryBaselineRunType = "query_baseline"

// baselineKeep is how many fingerprints a baseline keeps.
const baselineKeep = 50

// BufferFigures is the database's shared buffer hit and read.
type BufferFigures struct {
	Hit      int64   `json:"hit"`
	Read     int64   `json:"read"`
	HitRatio float64 `json:"hit_ratio"` // 0..100
}

// BaselineInfo says which run the comparison is against.
type BaselineInfo struct {
	SavedAt time.Time `json:"saved_at"`
	SavedBy string    `json:"saved_by"`
	Source  string    `json:"source"`
	Entries int       `json:"entries"`
}

type storedBaseline struct {
	Info    BaselineInfo       `json:"info"`
	Entries []QueryFingerprint `json:"entries"`
}

// databaseBuffers reads pg_stat_database for this database. It is a counter
// view, never EXPLAIN (ANALYZE, BUFFERS): that runs a statement and is not
// done on its own.
func databaseBuffers() *BufferFigures {
	if db.DB == nil {
		return nil
	}
	var hit, read sql.NullInt64
	if err := db.DB.QueryRow(`SELECT blks_hit, blks_read FROM pg_stat_database WHERE datname = current_database()`).Scan(&hit, &read); err != nil {
		return nil
	}
	b := &BufferFigures{Hit: hit.Int64, Read: read.Int64}
	if t := b.Hit + b.Read; t > 0 {
		b.HitRatio = float64(b.Hit) * 100 / float64(t)
	}
	return b
}

// SaveQueryBaseline stores the current heaviest fingerprints as the baseline.
func SaveQueryBaseline(by string) error {
	sum := slowQueriesRaw(baselineKeep)
	sb := storedBaseline{
		Info:    BaselineInfo{SavedAt: time.Now().UTC(), SavedBy: by, Source: sum.Source, Entries: len(sum.ByTotal)},
		Entries: sum.ByTotal,
	}
	raw, err := json.Marshal(sb)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM public.ops_run_log WHERE run_type = $1`, queryBaselineRunType); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO public.ops_run_log (run_type, status, detail) VALUES ($1, 'success', $2)`, queryBaselineRunType, string(raw)); err != nil {
		return err
	}
	return tx.Commit()
}

func loadQueryBaseline() *storedBaseline {
	var raw string
	if err := db.DB.QueryRow(`SELECT detail FROM public.ops_run_log WHERE run_type = $1 ORDER BY finished_at DESC, id DESC LIMIT 1`, queryBaselineRunType).Scan(&raw); err != nil {
		return nil
	}
	var sb storedBaseline
	if json.Unmarshal([]byte(raw), &sb) != nil {
		return nil
	}
	return &sb
}

// applyBaseline fills each listed fingerprint's change against the baseline.
func applyBaseline(sum *SlowQuerySummary) {
	if db.DB == nil {
		return
	}
	sb := loadQueryBaseline()
	if sb == nil {
		return
	}
	sum.Baseline = &sb.Info
	was := map[string]float64{}
	for _, e := range sb.Entries {
		was[e.ID] = e.MeanMs
	}
	mark := func(list []QueryFingerprint) {
		for i := range list {
			if m, ok := was[list[i].ID]; ok {
				mm := m
				list[i].BaselineMeanMs = &mm
				if m > 0 {
					c := (list[i].MeanMs - m) * 100 / m
					list[i].ChangePct = &c
				}
			}
		}
	}
	mark(sum.ByTotal)
	mark(sum.ByBuffers)
}
