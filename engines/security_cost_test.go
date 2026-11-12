package engines

import (
	"context"
	"custom_erp/db"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Stage 49.18.1 - the per-event side of the security cost ledger: what one
// signed audit row costs to write and store, and what one password guess costs
// an attacker (the cost that must not be optimized away, 49.18.6).
func TestStage49181AuditAndPasswordCost(t *testing.T) {
	db.InitDB(testConnStr())
	actor := "cost49181-" + stage4911Suffix()
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE user_id = $1`, actor)

	const n = 500
	times := make([]time.Duration, 0, n)
	detail := "updated Item ITEM-1 price " + strings.Repeat("x", 120) // a typical row, not a payload
	for i := 0; i < n; i++ {
		ctx := WithCorrelationID(context.Background(), stage4911UUID())
		began := time.Now()
		LogAuditEventCtx(ctx, "default", actor, "DOC_UPDATE", "SUCCESS", detail)
		times = append(times, time.Since(began))
	}
	sort.Slice(times, func(a, b int) bool { return times[a] < times[b] })
	var rows int
	var avg float64
	if err := db.DB.QueryRow(`SELECT COUNT(*), COALESCE(AVG(pg_column_size(a.*)), 0) FROM tenant_default.audit_logs a WHERE user_id = $1`, actor).Scan(&rows, &avg); err != nil {
		t.Fatal(err)
	}
	if rows != n {
		t.Fatalf("%d of %d audit rows written", rows, n)
	}
	p95 := times[(n-1)*95/100]
	t.Logf("49.18.1 signed audit row: p50 %v p95 %v to write; %.0f bytes/row, %.0f KiB per 1,000 events before indexes", times[n/2], p95, avg, avg*1000/1024)
	if p95 > 50*time.Millisecond {
		t.Errorf("audit write p95 %v over the 50 ms budget", p95)
	}
	if avg > 1536 {
		t.Errorf("an audit row averages %.0f bytes - over 1.5 KiB; something is copying payloads into the audit log (49.18.3)", avg)
	}

	began := time.Now()
	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	hashCost := time.Since(began)
	began = time.Now()
	_ = bcrypt.CompareHashAndPassword(hash, []byte("wrong guess"))
	guessCost := time.Since(began)
	t.Logf("49.18.1 password: hash %v, one wrong guess %v (cost %d) - an offline attacker pays the same per guess", hashCost, guessCost, bcrypt.DefaultCost)
	if guessCost < 10*time.Millisecond {
		t.Errorf("one password guess costs only %v - the hash cost has been lowered", guessCost)
	}
}
