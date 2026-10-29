package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 47.9.3: a fingerprint keeps the statement's shape and nothing it ran
// with - no literal, no client schema name, no comment.
func TestNormalizeQueryStripsLiterals(t *testing.T) {
	cases := map[string]string{
		`SELECT * FROM tenant_minn.documents WHERE id = 'CUST-9876543210' AND qty > 42 AND price = 1.5e3`:    `SELECT * FROM tenant_?.documents WHERE id = ? AND qty > ? AND price = ?`,
		"select x from t where a in (1, 2, 3) and b = $1 -- secret 'pw'\n  and c = $12":                      `select x from t where a in (?) and b = ? and c = ?`,
		`INSERT INTO "tenant_acme"."documents" (id, data) VALUES ('a', '{"pan":"ABCDE1234F"}'), ('b', '{}')`: `INSERT INTO tenant_?."documents" (id, data) VALUES (?)`,
		`SELECT E'it\'s a secret', 'it''s too'`:                                                              `SELECT ?, ?`,
		`DO $$BEGIN PERFORM 1; END$$; SELECT $tag$x$tag$`:                                                    `DO ?; SELECT ?`,
		`/* user=asha@example.com */ UPDATE t1 SET col2 = col3 + 7 WHERE "Col 9" = ANY(ARRAY[1, 2, 3])`:      `UPDATE t1 SET col2 = col3 + ? WHERE "Col 9" = ANY(ARRAY[?])`,
		`SELECT data->>'gstin' FROM public.tenants WHERE tenant_id = 'minn';`:                                `SELECT data->>? FROM public.tenants WHERE tenant_id = ?`,
	}
	for in, want := range cases {
		got := NormalizeQuery(in)
		if got != want {
			t.Errorf("NormalizeQuery(%q)\n got  %q\n want %q", in, got, want)
		}
		for _, secret := range []string{"secret", "ABCDE1234F", "9876543210", "minn", "acme", "asha", "pw", "1.5e3"} {
			if strings.Contains(got, secret) {
				t.Errorf("%q survived in %q", secret, got)
			}
		}
	}
	long := "SELECT " + strings.Repeat("col_a, ", 400) + "1 FROM t"
	if n := len([]rune(NormalizeQuery(long))); n > maxFingerprintChars+1 {
		t.Errorf("a long statement kept %d characters", n)
	}
}

// The store never grows past its size, keeps the heaviest, and folds the same
// statement with different values into one fingerprint.
func TestFingerprintStoreIsBounded(t *testing.T) {
	s := NewFingerprintStore(5)
	now := time.Now()
	for i := 1; i <= 50; i++ {
		s.Record(fmt.Sprintf("SELECT * FROM t%c WHERE id = %d", 'a'+rune(i%26), i), 1, float64(i), float64(i), 0, now)
		if s.Len() > 5 {
			t.Fatalf("store holds %d fingerprints, cap 5", s.Len())
		}
	}
	top := s.Top(0, "total")
	if len(top) != 5 || top[0].TotalMs < top[4].TotalMs {
		t.Fatalf("top = %+v", top)
	}
	floor := top[4].TotalMs
	s.Record("SELECT light FROM nowhere", 1, 0.5, 0.5, 0, now)
	for _, fp := range s.Top(0, "total") {
		if fp.TotalMs < floor {
			t.Errorf("a light statement evicted a heavier one: %+v", fp)
		}
	}
	m := NewFingerprintStore(10)
	m.Record("SELECT * FROM tenant_a.items WHERE code = 'X1'", 1, 100, 100, 7, now)
	m.Record("SELECT * FROM tenant_b.items WHERE code = 'Y2'", 1, 300, 300, 3, now)
	got := m.Top(0, "buffers")
	if len(got) != 1 || got[0].Calls != 2 || got[0].TotalMs != 400 || got[0].MaxMs != 300 || got[0].MeanMs != 200 || got[0].SharedBlocks != 10 {
		t.Errorf("same statement, two clients: %+v", got)
	}
}

// The sampler sees a statement that runs past the threshold, counts it once
// however many samples it spans, and keeps no literal it ran with.
func TestSlowQuerySamplerRecordsOnceWithoutValues(t *testing.T) {
	db.InitDB(testConnStr())
	other, err := sql.Open("postgres", testConnStr())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	s := &SlowQuerySampler{Store: NewFingerprintStore(20), Threshold: 100 * time.Millisecond}
	done := make(chan error, 1)
	go func() {
		_, err := other.Exec(`SELECT pg_sleep(0.9), 'card 4111111111111111' AS stolen`)
		done <- err
	}()
	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx, 60*time.Millisecond)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	cancel()
	var hit *QueryFingerprint
	for _, fp := range s.Store.Top(0, "total") {
		if strings.Contains(fp.Query, "4111") || strings.Contains(fp.Query, "card") {
			t.Errorf("a literal survived: %q", fp.Query)
		}
		if strings.Contains(fp.Query, "pg_sleep(?)") {
			fp := fp
			hit = &fp
		}
	}
	if hit == nil {
		t.Fatalf("the slow statement was not recorded: %+v", s.Store.Top(0, "total"))
	}
	if hit.Calls != 1 || hit.TotalMs < 500 || hit.MaxMs < 500 {
		t.Errorf("one 0.9 s statement recorded as %+v", *hit)
	}
	if hit.Query != "SELECT pg_sleep(?), ? AS stolen" {
		t.Errorf("fingerprint = %q", hit.Query)
	}
}

// Without pg_stat_statements the Control Panel reports the sampler, and says so.
func TestSlowQueriesReportsItsSource(t *testing.T) {
	db.InitDB(testConnStr())
	var installed bool
	_ = db.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).Scan(&installed)
	sum := SlowQueries(5)
	if !installed {
		if _, err := ReadStatStatements(5); err != errNoStatStatements {
			t.Errorf("ReadStatStatements without the extension: %v", err)
		}
		if sum.Source == "pg_stat_statements" {
			t.Errorf("source %q with no extension", sum.Source)
		}
	}
	if sum.ByTotal == nil {
		t.Error("by_total must be a list, even empty")
	}
	cp, err := GetControlPanel()
	if err != nil {
		t.Fatal(err)
	}
	if cp.SlowQueries.Source == "" {
		t.Error("the Control Panel carries no slow-query source")
	}
}
