package engines

import (
	"custom_erp/db"
	"sync"
	"testing"
	"time"
)

// Stage 47.12.7: two people signing the same pack at once. One sign-off lands;
// the other is refused with GLOBAL-9118 and no second record is written.
func TestStage47127ConcurrentSignOffsOneWins(t *testing.T) {
	tenantID, schema, done := goLiveTestTenant(t)
	defer done()
	p, err := GetGoLivePack(tenantID, PackGoLive)
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	for _, l := range p.Lines {
		if goLiveBlocks(l) {
			if err := WaiveGoLiveLine(tenantID, l.ID, "accepted for the pilot", expiry, "asha"); err != nil {
				t.Fatalf("waive %s: %v", l.ID, err)
			}
		}
	}
	signers := []string{"ravi", "meera", "kiran", "dev"}
	errs := make([]error, len(signers))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, who := range signers {
		wg.Add(1)
		go func(i int, who string) {
			defer wg.Done()
			<-start
			_, errs[i] = SignOffGoLivePack(tenantID, PackGoLive, "go", who)
		}(i, who)
	}
	close(start)
	wg.Wait()
	won := 0
	for i, e := range errs {
		switch {
		case e == nil:
			won++
		case goLiveCode(e) != "GLOBAL-9118":
			t.Errorf("%s: want GLOBAL-9118, got %v", signers[i], e)
		}
	}
	if won != 1 {
		t.Fatalf("%d sign-offs succeeded, want exactly 1", won)
	}
	var n int
	if err := goLiveCountSignOffs(schema, PackGoLive, &n); err != nil || n != 1 {
		t.Fatalf("sign-off records: %d (%v), want 1", n, err)
	}
	// Signing again while it stands is refused; the winner still shows.
	if _, err := SignOffGoLivePack(tenantID, PackGoLive, "again", "zoya"); goLiveCode(err) != "GLOBAL-9118" {
		t.Fatalf("second sign-off: %v", err)
	}
}

func goLiveCountSignOffs(schema, pack string, n *int) error {
	return db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.golive_records WHERE kind = 'SignOff' AND pack = $1`, pack).Scan(n)
}
