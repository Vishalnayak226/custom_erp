package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ensureTestJobHandler registers a no-op handler for each type a test
// queues without meaning to run it (Stage 49.8.6: the handler registry is
// the enqueue allowlist). Idempotent, unlike RegisterJobHandler.
func ensureTestJobHandler(jobTypes ...string) {
	for _, jobType := range jobTypes {
		if _, ok := jobHandlers[jobType]; !ok {
			RegisterJobHandler(jobType, func(context.Context, string, Job) (map[string]interface{}, error) { return nil, nil })
		}
	}
}

func stage4986JobsReady(t *testing.T) (schema string) {
	t.Helper()
	db.InitDB(testConnStr())
	var hasActor bool
	if err := db.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'tenant_default' AND table_name = 'async_jobs' AND column_name = 'actor')`).Scan(&hasActor); err != nil {
		t.Fatal(err)
	}
	if !hasActor {
		t.Skip("async_jobs.actor is absent: apply db/migrations_stage49_8_6_job_actor.sql")
	}
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func stage4986JobRow(t *testing.T, schema, id string) (actor, status, result string) {
	t.Helper()
	if err := db.DB.QueryRow("SELECT actor, status, COALESCE(result::text, '') FROM "+schema+".async_jobs WHERE id = $1", id).Scan(&actor, &status, &result); err != nil {
		t.Fatalf("reload %s: %v", id, err)
	}
	return
}

// TestStage4986UnknownJobTypeRefusedAtEnqueue: a type with no registered
// handler is refused to its caller, not queued for the worker to dead-letter.
func TestStage4986UnknownJobTypeRefusedAtEnqueue(t *testing.T) {
	schema := stage4986JobsReady(t)
	id, err := EnqueueJob("default", "test-4986-nobody-handles-this", map[string]interface{}{"n": 1}, "")
	if err == nil {
		db.DB.Exec("DELETE FROM "+schema+".async_jobs WHERE id = $1", id)
		t.Fatalf("expected the unknown type to be refused, got job %s", id)
	}
	if !strings.Contains(err.Error(), "no registered handler") {
		t.Fatalf("unexpected refusal: %v", err)
	}
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".async_jobs WHERE job_type = 'test-4986-nobody-handles-this'").Scan(&n)
	if n != 0 {
		t.Fatalf("a refused job must not be stored, found %d row(s)", n)
	}
}

// TestStage4986EveryJobNamesItsActor: a request's user, a named background
// worker, an unnamed background caller and a job queued from inside another
// job each leave a non-empty actor on the row.
func TestStage4986EveryJobNamesItsActor(t *testing.T) {
	schema := stage4986JobsReady(t)
	const parent, child = "test-4986-actor-parent", "test-4986-actor-child"
	ensureTestJobHandler(child)
	var childID string
	RegisterJobHandler(parent, func(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
		id, err := EnqueueJob("default", child, nil, "")
		childID = id
		return map[string]interface{}{"child": id}, err
	})
	var ids []string
	defer func() {
		for _, id := range append(ids, childID) {
			db.DB.Exec("DELETE FROM "+schema+".async_jobs WHERE id = $1", id)
		}
	}()

	userID, err := EnqueueJobContext(WithJobActor(context.Background(), "USR-4986"), "default", child, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, userID)
	if actor, _, _ := stage4986JobRow(t, schema, userID); actor != "USR-4986" {
		t.Errorf("request-queued job: actor %q, want the user id", actor)
	}

	workerID, err := EnqueueJobContext(SystemJobContext("Audit-Evidence"), "default", child, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, workerID)
	if actor, _, _ := stage4986JobRow(t, schema, workerID); actor != "system:audit-evidence" {
		t.Errorf("worker-queued job: actor %q, want system:audit-evidence", actor)
	}

	bareID, err := EnqueueJob("default", child, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, bareID)
	if actor, _, _ := stage4986JobRow(t, schema, bareID); actor != jobActorSystemDefault {
		t.Errorf("bare background enqueue: actor %q, want %s", actor, jobActorSystemDefault)
	}

	parentID, err := EnqueueJob("default", parent, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, parentID)
	if _, err := db.DB.Exec("UPDATE "+schema+".async_jobs SET next_attempt_at = CURRENT_TIMESTAMP - INTERVAL '1 second' WHERE id = $1", parentID); err != nil {
		t.Fatal(err)
	}
	// Only this parent is due with a non-default actor expectation; other
	// due rows in the shared table run too, which is harmless.
	runClaimedJobs(schema)
	if childID == "" {
		t.Fatalf("the parent job did not run")
	}
	if actor, _, _ := stage4986JobRow(t, schema, childID); actor != "system:job:"+parent {
		t.Errorf("job queued from a job: actor %q, want system:job:%s", actor, parent)
	}
	if actor, _, _ := stage4986JobRow(t, schema, parentID); actor == "" {
		t.Errorf("a job row must never be stored without an actor")
	}
}

// TestStage4986OversizeResultIsTruncatedWithAMarker: a handler's result
// over jobMaxResultBytes is stored as a marker that names the original
// size and keeps the first bytes - never the whole thing.
func TestStage4986OversizeResultIsTruncatedWithAMarker(t *testing.T) {
	schema := stage4986JobsReady(t)
	const jobType = "test-4986-big-result"
	RegisterJobHandler(jobType, func(context.Context, string, Job) (map[string]interface{}, error) {
		return map[string]interface{}{"blob": strings.Repeat("x", jobMaxResultBytes+1000)}, nil
	})
	id, err := EnqueueJob("default", jobType, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".async_jobs WHERE id = $1", id)
	db.DB.Exec("UPDATE "+schema+".async_jobs SET next_attempt_at = CURRENT_TIMESTAMP - INTERVAL '1 second' WHERE id = $1", id)
	runClaimedJobs(schema)
	_, status, result := stage4986JobRow(t, schema, id)
	if status != "Succeeded" {
		t.Fatalf("status %s, want Succeeded", status)
	}
	if len(result) > jobMaxResultBytes {
		t.Fatalf("stored result is %d bytes, over the %d bound", len(result), jobMaxResultBytes)
	}
	var marker map[string]interface{}
	if err := json.Unmarshal([]byte(result), &marker); err != nil {
		t.Fatalf("stored result is not JSON: %v", err)
	}
	if marker["truncated"] != true || marker["original_size"] == nil || marker["head"] == nil {
		t.Fatalf("expected a truncation marker with original_size and head, got %s", result)
	}
}

// TestStage4986JobOverItsRuntimeBoundFails: a handler that outlives its
// type's runtime bound sees its context end and the attempt is recorded as
// timed out (a failed attempt, retried like any other), while the lease it
// was claimed under covers the bound.
func TestStage4986JobOverItsRuntimeBoundFails(t *testing.T) {
	schema := stage4986JobsReady(t)
	const jobType = "test-4986-slow"
	SetJobMaxRuntime(jobType, 150*time.Millisecond)
	RegisterJobHandler(jobType, func(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
		<-ctx.Done()
		return map[string]interface{}{"finished": true}, nil // claims success after the deadline
	})
	id, err := EnqueueJob("default", jobType, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".async_jobs WHERE id = $1", id)
	db.DB.Exec("UPDATE "+schema+".async_jobs SET next_attempt_at = CURRENT_TIMESTAMP - INTERVAL '1 second' WHERE id = $1", id)
	runClaimedJobs(schema)
	var status, lastError string
	if err := db.DB.QueryRow("SELECT status, last_error FROM "+schema+".async_jobs WHERE id = $1", id).Scan(&status, &lastError); err != nil {
		t.Fatal(err)
	}
	if status == "Succeeded" || !strings.Contains(lastError, "timed out") {
		t.Fatalf("status %s, last_error %q: a job over its runtime bound must be a timed-out attempt", status, lastError)
	}
	if JobMaxRuntime("test-4986-never-registered") != jobDefaultMaxRuntime || JobMaxRuntime(jobType) != 150*time.Millisecond {
		t.Fatalf("runtime registry: default %s, override %s", JobMaxRuntime("test-4986-never-registered"), JobMaxRuntime(jobType))
	}
}
