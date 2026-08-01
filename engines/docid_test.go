package engines

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestNewDocIDNoCollisions is the regression guard for the documents_pkey
// violations described in docid.go. It deliberately runs the generator the way
// concurrent HTTP handlers do - many goroutines, no work in between - which is
// precisely the shape that made the old time.Now().UnixNano() scheme collide on
// >99% of IDs on a coarse (Windows, ~520us) clock.
//
// This test does NOT need a database and does NOT need a coarse clock to be
// meaningful: uniqueNanos guarantees strict monotonicity regardless of clock
// resolution, so a regression here fails on any platform.
func TestNewDocIDNoCollisions(t *testing.T) {
	const (
		workers = 64
		perGo   = 2000
	)

	var mu sync.Mutex
	seen := make(map[string]struct{}, workers*perGo)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]string, 0, perGo)
			for i := 0; i < perGo; i++ {
				local = append(local, NewDocID("TSK"))
			}
			mu.Lock()
			defer mu.Unlock()
			for _, id := range local {
				if _, dup := seen[id]; dup {
					t.Errorf("duplicate document ID generated: %s", id)
					return
				}
				seen[id] = struct{}{}
			}
		}()
	}
	wg.Wait()

	if got, want := len(seen), workers*perGo; got != want {
		t.Fatalf("expected %d unique IDs, got %d (%d collisions)", want, got, want-got)
	}
}

// TestNewDocIDBeatsTheOldScheme documents WHY docid.go exists: it asserts that
// the exact expression the codebase used before genuinely does collide under
// the same conditions where NewDocID does not. If a future platform's clock
// were fine-grained enough that the old scheme stopped colliding here, this
// test skips rather than fails - the point is to record the hazard, not to
// depend on it.
func TestNewDocIDBeatsTheOldScheme(t *testing.T) {
	const n = 20000
	old := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		old[fmt.Sprintf("TSK-%d", time.Now().UnixNano())] = struct{}{}
	}
	oldCollisions := n - len(old)

	fresh := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		fresh[NewDocID("TSK")] = struct{}{}
	}
	if newCollisions := n - len(fresh); newCollisions != 0 {
		t.Fatalf("NewDocID collided %d times in %d sequential calls", newCollisions, n)
	}

	if oldCollisions == 0 {
		t.Skipf("this platform's clock is fine-grained enough that the legacy "+
			"UnixNano scheme did not collide across %d sequential calls; "+
			"NewDocID is still collision-free, which is what matters", n)
	}
	t.Logf("legacy UnixNano scheme collided on %d/%d sequential IDs (%.1f%%); "+
		"NewDocID collided on 0", oldCollisions, n, float64(oldCollisions)/float64(n)*100)
}

// TestNewDocIDMonotonic pins the ordering property callers rely on when they
// sort or paginate by id.
func TestNewDocIDMonotonic(t *testing.T) {
	prev := ""
	for i := 0; i < 5000; i++ {
		id := NewDocID("SO")
		if prev != "" && id <= prev {
			t.Fatalf("IDs not strictly increasing: %q followed by %q", prev, id)
		}
		prev = id
	}
}

// TestNewDocIDFitsColumn guards the VARCHAR(100) primary-key column: an ID that
// overflows it becomes a runtime INSERT error, which is exactly the class of
// failure this whole change exists to remove.
func TestNewDocIDFitsColumn(t *testing.T) {
	for _, prefix := range []string{"TSK", "SO", "POSSYNCVAR", "IMPJOB", "RPTEXP"} {
		if got := len(NewDocID(prefix)); got > 100 {
			t.Errorf("NewDocID(%q) is %d chars, exceeds documents.id VARCHAR(100)", prefix, got)
		}
		if got := len(NewDocIDCompact(prefix)); got > 100 {
			t.Errorf("NewDocIDCompact(%q) is %d chars, exceeds VARCHAR(100)", prefix, got)
		}
	}
}
