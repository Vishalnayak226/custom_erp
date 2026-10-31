package engines

import (
	"context"
	"custom_erp/db"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// 49.10.5 (rest): retry storms stay bounded. A job that keeps failing (an
// outbound webhook whose endpoint is down runs on this runner -
// webhook_delivery) is retried with doubling, capped waits and then
// dead-lettered; a tight loop of worker ticks claims at most a batch each and
// never re-runs a job before its wait; a provider re-sending the same inbound
// webhook in a burst is processed once.

const storm49105Type = "t49105_always_fails"

var storm49105Once sync.Once

func storm49105Setup(t *testing.T) string {
	t.Helper()
	db.InitDB(testConnStr())
	storm49105Once.Do(func() {
		RegisterJobHandler(storm49105Type, func(context.Context, string, Job) (map[string]interface{}, error) {
			return nil, errors.New("endpoint returned HTTP 503")
		})
	})
	// A scratch queue of its own, so the storm touches no other test's jobs.
	const schema = "t49105_storm"
	for _, q := range []string{
		`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`,
		`CREATE SCHEMA ` + schema,
		`CREATE TABLE ` + schema + `.async_jobs (LIKE tenant_default.async_jobs INCLUDING ALL)`,
	} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = db.DB.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`) })
	return schema
}

// tick49105 is one worker pass over the scratch queue: what runClaimedJobs
// does for a schema (claim a batch, run each, record the outcome).
func tick49105(t *testing.T, schema string) int {
	t.Helper()
	jobs, err := claimJobs(schema, jobClaimBatchSize)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		result, err := jobHandlers[job.JobType](context.Background(), schema, job)
		finishJob(schema, job, result, err)
	}
	return len(jobs)
}

func TestStage49105JobRetryStormIsBounded(t *testing.T) {
	schema := storm49105Setup(t)
	const jobs = 35
	for i := 0; i < jobs; i++ {
		if _, err := enqueueJobInSchema(SystemJobContext("t49105"), schema, storm49105Type, map[string]interface{}{"n": i}, fmt.Sprintf("storm-%d", i), ""); err != nil {
			t.Fatal(err)
		}
	}
	// A storm of ticks with no time passing: each claims at most a batch,
	// every job is tried once, and then nothing more until its wait is up.
	total := 0
	for i := 0; i < 40; i++ {
		n := tick49105(t, schema)
		if n > jobClaimBatchSize {
			t.Fatalf("one tick claimed %d jobs, the batch is %d", n, jobClaimBatchSize)
		}
		total += n
	}
	if total != jobs {
		t.Fatalf("40 immediate ticks ran %d attempts for %d failing jobs - a failed job was retried before its wait", total, jobs)
	}
	var tried, early int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FILTER (WHERE attempts = 1), COUNT(*) FILTER (WHERE next_attempt_at < updated_at + interval '29 seconds') FROM `+schema+`.async_jobs WHERE status = 'Pending'`).Scan(&tried, &early)
	if tried != jobs || early != 0 {
		t.Fatalf("after one failure each: %d at attempt 1 (want %d), %d scheduled sooner than 30 s", tried, jobs, early)
	}

	// Let each wait pass, tick by tick: the waits double (30, 60, 120, 240 s)
	// and the fifth failure dead-letters - five attempts, never more.
	for attempt := 2; attempt <= 6; attempt++ {
		var minWait, maxWait float64
		_ = db.DB.QueryRow(`SELECT COALESCE(MIN(EXTRACT(EPOCH FROM next_attempt_at - updated_at)), 0), COALESCE(MAX(EXTRACT(EPOCH FROM next_attempt_at - updated_at)), 0) FROM `+schema+`.async_jobs WHERE status = 'Pending'`).Scan(&minWait, &maxWait)
		if attempt <= 5 {
			want := jobRetryBackoff(attempt - 1).Seconds()
			if minWait < want-2 || maxWait > want+2 {
				t.Fatalf("before attempt %d the wait is %.0f-%.0f s, want %.0f s", attempt, minWait, maxWait, want)
			}
		}
		if _, err := db.DB.Exec(`UPDATE ` + schema + `.async_jobs SET next_attempt_at = CURRENT_TIMESTAMP - interval '1 second' WHERE status = 'Pending'`); err != nil {
			t.Fatal(err)
		}
		for tick49105(t, schema) > 0 {
		}
	}
	var dead, maxAttempts, pending int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FILTER (WHERE status = 'DeadLettered'), COALESCE(MAX(attempts), 0), COUNT(*) FILTER (WHERE status = 'Pending') FROM `+schema+`.async_jobs`).Scan(&dead, &maxAttempts, &pending)
	if dead != jobs || maxAttempts != 5 || pending != 0 {
		t.Fatalf("after the storm: %d dead-lettered (want %d), at most %d attempts (want 5), %d still pending", dead, jobs, maxAttempts, pending)
	}
	// A dead-lettered job is never claimed again by a tick.
	if n := tick49105(t, schema); n != 0 {
		t.Fatalf("a tick claimed %d dead-lettered jobs", n)
	}
}

// The wait doubles to a cap and never wraps: from the 30th attempt the old
// shift overflowed to a negative wait, which would have retried a job with a
// raised max_attempts on every tick.
func TestStage49105JobRetryBackoffIsCappedAndNeverNegative(t *testing.T) {
	prev := time.Duration(0)
	for n := 1; n <= 200; n++ {
		b := jobRetryBackoff(n)
		if b <= 0 || b > jobMaxBackoff || b < prev {
			t.Fatalf("attempt %d waits %v (previous %v, cap %v)", n, b, prev, jobMaxBackoff)
		}
		prev = b
	}
	if jobRetryBackoff(1) != 30*time.Second || jobRetryBackoff(4) != 240*time.Second || jobRetryBackoff(200) != jobMaxBackoff {
		t.Fatalf("schedule changed: %v %v %v", jobRetryBackoff(1), jobRetryBackoff(4), jobRetryBackoff(200))
	}
}

// A provider re-sending one webhook in a burst - Shopify retries for up to 48
// hours - is processed once; the rest are answered as duplicates.
func TestStage49105InboundWebhookRetryStormIsProcessedOnce(t *testing.T) {
	db.InitDB(testConnStr())
	body := []byte(fmt.Sprintf(`{"id": "t49105-%d", "event": "orders/create"}`, time.Now().UnixNano()))
	const burst = 60
	var mu sync.Mutex
	fresh, dupes := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dup, err := ClaimInboundWebhook("default", "t49105", "", body)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if dup {
				dupes++
			} else {
				fresh++
			}
		}()
	}
	wg.Wait()
	if fresh != 1 || dupes != burst-1 {
		t.Fatalf("a burst of %d identical deliveries: %d processed, %d duplicates - want 1 and %d", burst, fresh, dupes, burst-1)
	}
	// A different delivery is still new.
	if dup, err := ClaimInboundWebhook("default", "t49105", "", append(body, ' ', '1')); err != nil || dup {
		t.Fatalf("a new delivery was taken for a duplicate: %v %v", dup, err)
	}
	ReleaseInboundWebhook("default", "t49105", "", body)
	ReleaseInboundWebhook("default", "t49105", "", append(body, ' ', '1'))
}
