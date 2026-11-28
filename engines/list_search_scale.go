package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"custom_erp/db"
)

// Ask 297.4: how a search tells a big book from a small one. The list
// endpoint (47.10's big masters, 297.4's search by number) and POS > Sales
// History both ask the planner how many rows a doctype has, and search
// differently above BigDoctypeRows. One estimate, cached here, serves both.

// BigDoctypeRows is the row count above which a search covers only what an
// index can find.
const BigDoctypeRows = 100_000

type rowEstimate struct {
	rows int64
	at   time.Time
}

var doctypeRowEstimates sync.Map // schema/doctype -> rowEstimate

// DoctypeRows is the planner's estimate of a doctype's live rows, cached ten
// minutes. A failed EXPLAIN gives 0 (small: the ordinary search) and is not
// cached.
func DoctypeRows(schema, doctype string) int64 {
	key := schema + "/" + doctype
	if v, ok := doctypeRowEstimates.Load(key); ok && time.Since(v.(rowEstimate).at) < 10*time.Minute {
		return v.(rowEstimate).rows
	}
	n, err := PlannerRows(context.Background(), fmt.Sprintf("SELECT 1 FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL", schema), doctype)
	if err != nil {
		return 0
	}
	doctypeRowEstimates.Store(key, rowEstimate{rows: n, at: time.Now()})
	return n
}

// PlannerRows is the planner's row estimate for query. It reads no rows.
func PlannerRows(ctx context.Context, query string, args ...interface{}) (int64, error) {
	var raw string
	if err := db.DB.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		return 0, err
	}
	var plan []struct {
		Plan struct {
			Rows float64 `json:"Plan Rows"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &plan); err != nil || len(plan) == 0 {
		return 0, fmt.Errorf("unreadable plan: %v", err)
	}
	return int64(plan[0].Plan.Rows), nil
}
