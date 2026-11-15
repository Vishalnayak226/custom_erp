package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// 47.11 (ask 191): two server replicas share one job queue. Each runs its own
// worker ticks against the same table at the same moment; every job runs once
// and ends with one outcome - never twice, never lost.

const replica4711Type = "t4711_two_replicas"

var (
	replica4711Once sync.Once
	replica4711Runs sync.Map // job id -> *int32, how often the handler ran it
)

func replica4711Setup(t *testing.T) string {
	t.Helper()
	db.InitDB(testConnStr())
	replica4711Once.Do(func() {
		RegisterJobHandler(replica4711Type, func(_ context.Context, _ string, job Job) (map[string]interface{}, error) {
			n, _ := replica4711Runs.LoadOrStore(job.ID, new(int32))
			atomic.AddInt32(n.(*int32), 1)
			return map[string]interface{}{"ok": true}, nil
		})
	})
	// A scratch queue of its own, so neither replica touches another test's jobs.
	const schema = "t4711_replicas"
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

func replica4711Enqueue(t *testing.T, schema string, n int, tag string) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id, err := enqueueJobInSchema(SystemJobContext("t4711"), schema, replica4711Type, map[string]interface{}{"n": i}, fmt.Sprintf("%s-%d", tag, i), "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// Two claims of one job released at the same instant: exactly one replica
// gets it, the other sees nothing to do.
func TestJobQueueTwoReplicasClaimOneJobOnce(t *testing.T) {
	schema := replica4711Setup(t)
	ids := replica4711Enqueue(t, schema, 1, "single")
	var start, done sync.WaitGroup
	start.Add(1)
	got := make([][]Job, 2)
	errs := make([]error, 2)
	for r := 0; r < 2; r++ {
		done.Add(1)
		go func(r int) {
			defer done.Done()
			start.Wait()
			got[r], errs[r] = claimJobs(schema, jobClaimBatchSize)
		}(r)
	}
	start.Done()
	done.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := len(got[0]) + len(got[1]); n != 1 {
		t.Fatalf("two replicas claimed the one job %d times (want 1): %d and %d", n, len(got[0]), len(got[1]))
	}
	job := append(got[0], got[1]...)[0]
	if job.ID != ids[0] {
		t.Fatalf("claimed %s, enqueued %s", job.ID, ids[0])
	}
	result, err := jobHandlers[job.JobType](context.Background(), schema, job)
	finishJob(schema, job, result, err)
	// A late second finish (the other replica acting on a stale copy) changes
	// nothing: the outcome is recorded once.
	finishJob(schema, job, nil, fmt.Errorf("stale replica"))
	var status string
	var attempts int
	if err := db.DB.QueryRow(`SELECT status, attempts FROM `+schema+`.async_jobs WHERE id = $1`, job.ID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "Succeeded" || attempts != 0 {
		t.Fatalf("after one run and a stale finish: %s at attempt %d, want Succeeded at 0", status, attempts)
	}
}

// A soak of two replicas ticking over one queue until it is empty: every job
// runs once, every job ends Succeeded, and the claims add up to the jobs.
func TestJobQueueTwoReplicasRunEachJobOnce(t *testing.T) {
	schema := replica4711Setup(t)
	const jobs = 80
	ids := replica4711Enqueue(t, schema, jobs, "soak")
	var start, done sync.WaitGroup
	start.Add(1)
	claimed := make([]int, 2)
	errs := make([]error, 2)
	for r := 0; r < 2; r++ {
		done.Add(1)
		go func(r int) {
			defer done.Done()
			start.Wait()
			for idle := 0; idle < 3; {
				batch, err := claimJobs(schema, 5)
				if err != nil {
					errs[r] = err
					return
				}
				if len(batch) == 0 {
					idle++
					continue
				}
				idle = 0
				claimed[r] += len(batch)
				for _, job := range batch {
					result, err := jobHandlers[job.JobType](context.Background(), schema, job)
					finishJob(schema, job, result, err)
				}
			}
		}(r)
	}
	start.Done()
	done.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("replica A claimed %d, replica B claimed %d", claimed[0], claimed[1])
	if claimed[0]+claimed[1] != jobs {
		t.Fatalf("the replicas claimed %d jobs between them, %d were queued", claimed[0]+claimed[1], jobs)
	}
	for _, id := range ids {
		n, ok := replica4711Runs.Load(id)
		if !ok {
			t.Fatalf("job %s never ran", id)
		}
		if runs := atomic.LoadInt32(n.(*int32)); runs != 1 {
			t.Fatalf("job %s ran %d times", id, runs)
		}
	}
	var succeeded, other int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FILTER (WHERE status = 'Succeeded'), COUNT(*) FILTER (WHERE status <> 'Succeeded') FROM `+schema+`.async_jobs`).Scan(&succeeded, &other); err != nil {
		t.Fatal(err)
	}
	if succeeded != jobs || other != 0 {
		t.Fatalf("after the soak: %d Succeeded (want %d), %d in another state", succeeded, jobs, other)
	}
}
