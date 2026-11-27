package db

import (
	"context"
	"testing"
	"time"
)

// 283.3: every connection in the pool is the wrapper, so a bound context
// reaches lib/pq. Fails first if a lib/pq upgrade drops an interface the
// wrapper forwards (wrapCancelConn then hands back the bare connection).
func TestBindContextWrapsEveryConnection(t *testing.T) {
	InitDB(ConnStringFromEnv())
	conn, err := DB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Raw(func(dc any) error {
		if _, ok := dc.(cancelConn); !ok {
			t.Fatalf("a pool connection is %T, not cancelConn", dc)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A context-free query or transaction on a bound goroutine stops in Postgres
// when the context ends; another goroutine and a call with its own context are
// not touched.
func TestBindContextCancelsTheQueryInPostgres(t *testing.T) {
	InitDB(ConnStringFromEnv())
	running := func(marker string) int {
		t.Helper()
		var n int
		if err := DB.QueryRow(`SELECT count(*) FROM pg_stat_activity
			WHERE state = 'active' AND pid <> pg_backend_pid() AND query LIKE '%' || $1 || '%'`, marker).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatalf("timed out waiting for %s", what)
	}

	for name, run := range map[string]func(marker string) error{
		"query": func(marker string) error {
			_, err := DB.Exec(`SELECT pg_sleep(30) /* ` + marker + ` */`)
			return err
		},
		"transaction": func(marker string) error {
			tx, err := DB.Begin()
			if err != nil {
				return err
			}
			defer tx.Rollback()
			_, err = tx.Exec(`SELECT pg_sleep(30) /* ` + marker + ` */`)
			return err
		},
	} {
		marker := "bind283_" + name
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			defer BindContext(ctx)()
			done <- run(marker)
		}()
		waitFor(name+" to start in Postgres", func() bool { return running(marker) > 0 })

		// Meanwhile another goroutine, and a call here with its own context,
		// run normally under the ended context.
		cancel()
		other := make(chan error, 1)
		go func() {
			_, err := DB.Exec(`SELECT pg_sleep(0.3)`)
			other <- err
		}()
		if err := <-other; err != nil {
			t.Fatalf("%s: an unbound goroutine's query failed: %v", name, err)
		}

		select {
		case err := <-done:
			if err == nil {
				t.Fatalf("%s: the bound statement finished without an error after its context ended", name)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: the bound statement kept running after its context ended", name)
		}
		waitFor(name+" to stop in Postgres", func() bool { return running(marker) == 0 })
	}

	ended, cancel := context.WithCancel(context.Background())
	cancel()
	release := BindContext(ended)
	own, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	_, err := DB.ExecContext(own, `SELECT pg_sleep(0.3)`)
	release()
	if err != nil {
		t.Fatalf("a call with its own context took the bound one: %v", err)
	}
}

// Bindings nest, and the last release leaves nothing bound.
func TestBindContextNestsAndReleases(t *testing.T) {
	bg := context.Background()
	outer, stopOuter := context.WithCancel(bg)
	defer stopOuter()
	inner, stopInner := context.WithCancel(bg)
	defer stopInner()
	before := boundCount.Load()

	releaseOuter := BindContext(outer)
	if boundContext(bg) != outer {
		t.Fatal("the bound context is not used for a call without one")
	}
	releaseInner := BindContext(inner)
	if boundContext(bg) != inner {
		t.Fatal("the inner binding is not in force")
	}
	releaseInner()
	if boundContext(bg) != outer {
		t.Fatal("releasing the inner binding did not put back the outer one")
	}
	releaseOuter()
	if boundContext(bg) != bg || boundCount.Load() != before {
		t.Fatal("after the last release a binding is still in force")
	}
	// a context that cannot end binds nothing
	BindContext(bg)()
	if boundCount.Load() != before {
		t.Fatal("binding context.Background changed the count")
	}
}
