package engines

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"custom_erp/db"
)

// 34.4 tests. No test reaches myntra.com: every call goes to an httptest
// server on loopback, with a fake clock so spacing is checked without waiting.

func myntraTestPage(products ...string) []byte {
	return []byte(`<html><head><title>search</title></head><body><script>window.__myx = {"searchData":{"results":{"products":[` +
		strings.Join(products, ",") + `]}}}; window.other = 1;</script><p>padding padding padding padding</p></body></html>`)
}

func myntraTestProduct(id int, name string, price, mrp int) string {
	return fmt.Sprintf(`{"productId":%d,"productName":%q,"brand":"Brandy","price":%d,"mrp":%d,"rating":4.2,"ratingCount":31,"landingPageUrl":"kurtas/brandy/p%d/buy"}`, id, name, price, mrp, id)
}

type myntraFake struct {
	mu     sync.Mutex
	paths  []string
	agents []string
	robots string
	status int // 0 = normal
	pages  map[string][]byte
}

func (f *myntraFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.paths = append(f.paths, r.URL.RequestURI())
	f.agents = append(f.agents, r.UserAgent())
	f.mu.Unlock()
	if r.URL.Path == "/robots.txt" {
		_, _ = w.Write([]byte(f.robots))
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte("blocked"))
		return
	}
	if p, ok := f.pages[r.URL.RequestURI()]; ok {
		_, _ = w.Write(p)
		return
	}
	_, _ = w.Write(myntraTestPage()) // an empty result page
}

func (f *myntraFake) hits() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

type harvestRig struct {
	h      *myntraHarvester
	fake   *myntraFake
	sleeps []time.Duration
	tenant string
	schema string
}

func newHarvestRig(t *testing.T, fake *myntraFake) *harvestRig {
	t.Helper()
	tenantID, schema := sodTenant(t)
	ts := httptest.NewServer(fake)
	t.Cleanup(ts.Close)
	base, _ := url.Parse(ts.URL)
	rig := &harvestRig{fake: fake, tenant: tenantID, schema: schema}
	clock := time.Date(2026, 11, 13, 10, 0, 0, 0, istZone)
	prev := mediaStoreDir
	mediaStoreDir = t.TempDir()
	t.Cleanup(func() { mediaStoreDir = prev })
	rig.h = &myntraHarvester{
		schema: schema, base: base,
		client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		now:    func() time.Time { return clock },
		sleep: func(_ context.Context, d time.Duration) error {
			rig.sleeps = append(rig.sleeps, d)
			clock = clock.Add(d)
			return nil
		},
		interval: time.Second, // below the floor on purpose: the floor must win
		root:     marketHarvestRoot(schema),
	}
	return rig
}

func countCompetitor(t *testing.T, schema, extra string) int {
	t.Helper()
	var n int
	q := fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'CompetitorPrice' AND data->>'platform' = 'Myntra' %s`, schema, extra)
	if err := db.DB.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMyntraRobotsLongestMatchAndAgents(t *testing.T) {
	rr := parseRobots(`
User-agent: *
Disallow: /checkout
Disallow: /search
Allow: /search/public
Disallow: /*.json$
User-agent: someotherbot
Disallow: /
User-agent: WholeOps-MarketPrice
Disallow: /private
`)
	cases := map[string]bool{
		"/kurta?rawQuery=kurta":        true,
		"/checkout/cart":               false,
		"/search/deep":                 false,
		"/search/public/page":          true, // the longer Allow beats the shorter Disallow
		"/data/feed.json":              false,
		"/data/feed.json?x=1":          true,  // $ anchors the end
		"/private/area":                false, // our own agent group is applied too
		"/someotherbot-only":           true,  // another bot's group is not ours
		"/robots.txt":                  true,
		"/search/public":               true,
		"/checkouts-are-also-blocked":  false, // prefix match, as robots.txt means it
		"/not-checkout/foo":            true,
		"/searchable":                  false,
		"/other/search":                true,
		"/privately":                   false,
		"/priv":                        true,
		"/search/publicity/other-page": true,
	}
	for path, want := range cases {
		if got := rr.allowed(path); got != want {
			t.Errorf("allowed(%q) = %v, want %v", path, got, want)
		}
	}
	// Allow wins an exact tie.
	if !parseRobots("User-agent: *\nDisallow: /a\nAllow: /a\n").allowed("/a") {
		t.Error("an Allow and a Disallow of the same length: Allow should win")
	}
	if !parseRobots("User-agent: *\nDisallow:\n").allowed("/anything") {
		t.Error("an empty Disallow allows everything")
	}
}

func TestMyntraHarvestFetchesSpacedCachesAndParses(t *testing.T) {
	fake := &myntraFake{robots: "User-agent: *\nDisallow: /checkout\n", pages: map[string][]byte{}}
	rig := newHarvestRig(t, fake)
	fake.pages["/kurta?rawQuery=kurta"] = myntraTestPage(myntraTestProduct(1, "Brandy Blue Kurta", 799, 1999), myntraTestProduct(2, "Brandy Red Kurta", 599, 1299))
	fake.pages["/kurta?p=2&rawQuery=kurta"] = myntraTestPage(myntraTestProduct(3, "Brandy Green Kurta", 899, 899))
	// page 3 is the empty page, which ends the keyword

	res := rig.h.run(context.Background(), rig.tenant, MyntraHarvestRequest{Keywords: []string{"kurta"}}, 10)
	if res.Fetched != 3 || res.Cached != 0 || res.StopReason != "finished" {
		t.Fatalf("first run: %+v", res)
	}
	if res.Parse.Written != 3 || res.Parse.Products != 3 {
		t.Fatalf("parse: %+v", res.Parse)
	}
	hits := fake.hits()
	if hits[0] != "/robots.txt" || len(hits) != 4 {
		t.Fatalf("robots must come first, once; hits = %v", hits)
	}
	for _, a := range fake.agents {
		if a != myntraUserAgent {
			t.Fatalf("user agent %q", a)
		}
	}
	// 4 requests = 3 gaps, each at least 5 seconds although the interval was set to 1.
	if len(rig.sleeps) != 3 {
		t.Fatalf("sleeps = %v", rig.sleeps)
	}
	for _, d := range rig.sleeps {
		if d < 5*time.Second {
			t.Fatalf("requests closer than 5s: %v", rig.sleeps)
		}
	}
	var price, mrp, disc, brand, kw, url string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'competitor_price', data->>'mrp', data->>'discount_pct', data->>'brand', data->>'keyword', data->>'source_url'
		FROM %s.documents WHERE id = 'MYN-20261113-1'`, rig.schema)).Scan(&price, &mrp, &disc, &brand, &kw, &url); err != nil {
		t.Fatal(err)
	}
	if price != "799" || mrp != "1999" || disc != "60" || brand != "Brandy" || kw != "kurta" || url != "https://www.myntra.com/kurtas/brandy/p1/buy" {
		t.Fatalf("row = %s %s %s %s %s %s", price, mrp, disc, brand, kw, url)
	}

	// A second run the same day: every page is on disk, nothing is requested.
	before := len(fake.hits())
	res2 := rig.h.run(context.Background(), rig.tenant, MyntraHarvestRequest{Keywords: []string{"kurta"}}, 10)
	if res2.Cached != 3 || res2.Fetched != 0 {
		t.Fatalf("second run: %+v", res2)
	}
	if len(fake.hits()) != before {
		t.Fatalf("the cached run made requests: %v", fake.hits()[before:])
	}
	if n := countCompetitor(t, rig.schema, ""); n != 3 {
		t.Fatalf("a repeat must not duplicate rows, have %d", n)
	}
}

func TestMyntraHarvestNeverFetchesADisallowedPath(t *testing.T) {
	fake := &myntraFake{robots: "User-agent: *\nDisallow: /kurta\n"}
	rig := newHarvestRig(t, fake)
	res := rig.h.run(context.Background(), rig.tenant, MyntraHarvestRequest{Keywords: []string{"kurta", "saree"}}, 10)
	if res.Refused != 1 || len(res.RefusedPaths) != 1 || res.RefusedPaths[0] != "/kurta" {
		t.Fatalf("refused: %+v", res)
	}
	for _, p := range fake.hits() {
		if strings.HasPrefix(p, "/kurta") {
			t.Fatalf("a disallowed path was requested: %s", p)
		}
	}
	if res.Fetched == 0 { // saree is allowed and still runs
		t.Fatalf("the allowed keyword should still be fetched: %+v", res)
	}
	if res.Pages[0].Outcome != "refused by robots.txt" {
		t.Fatalf("log: %+v", res.Pages)
	}
}

func TestMyntraHarvestStopsOnABlockAndAtThePageCap(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{{429, "429"}, {403, "403"}, {302, "redirect"}} {
		fake := &myntraFake{status: tc.status}
		rig := newHarvestRig(t, fake)
		// robots.txt itself answers normally in the fake; the page is blocked.
		res := rig.h.run(context.Background(), rig.tenant, MyntraHarvestRequest{Keywords: []string{"kurta", "saree"}}, 10)
		if !strings.Contains(res.StopReason, tc.want) {
			t.Fatalf("status %d: stop reason %q", tc.status, res.StopReason)
		}
		pageHits := 0
		for _, p := range fake.hits() {
			if p != "/robots.txt" {
				pageHits++
			}
		}
		if pageHits != 1 {
			t.Fatalf("status %d: the run must stop at the first block, made %d page requests", tc.status, pageHits)
		}
	}

	// A captcha-looking 200 stops too and is not kept.
	fake := &myntraFake{pages: map[string][]byte{"/kurta?rawQuery=kurta": []byte(`<html>Please solve this CAPTCHA to continue, we saw unusual traffic from your network</html>`)}}
	rig := newHarvestRig(t, fake)
	res := rig.h.run(context.Background(), rig.tenant, MyntraHarvestRequest{Keywords: []string{"kurta"}}, 10)
	if !strings.Contains(res.StopReason, "captcha") || res.Fetched != 0 {
		t.Fatalf("captcha: %+v", res)
	}

	// The page cap is hard: more pages exist, only two are fetched.
	fake = &myntraFake{pages: map[string][]byte{}}
	for i, key := range []string{"/kurta?rawQuery=kurta", "/kurta?p=2&rawQuery=kurta", "/kurta?p=3&rawQuery=kurta"} {
		fake.pages[key] = myntraTestPage(myntraTestProduct(100+i, "Kurta "+key, 500, 900))
	}
	rig = newHarvestRig(t, fake)
	res = rig.h.run(context.Background(), rig.tenant, MyntraHarvestRequest{Keywords: []string{"kurta"}}, 2)
	if res.Fetched != 2 {
		t.Fatalf("cap: %+v", res)
	}
	if n := len(fake.hits()); n != 3 { // robots + 2 pages
		t.Fatalf("requests = %v", fake.hits())
	}
}

func TestMyntraHarvestStopsWhenRobotsCannotBeRead(t *testing.T) {
	fake := &myntraFake{}
	rig := newHarvestRig(t, fake)
	rig.h.base, _ = url.Parse("http://127.0.0.1:1") // nothing listens
	res := rig.h.run(context.Background(), rig.tenant, MyntraHarvestRequest{Keywords: []string{"kurta"}}, 5)
	if !strings.Contains(res.StopReason, "robots.txt") || res.Fetched != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestMyntraHarvestUsesOneFixedHost(t *testing.T) {
	prod := newMyntraHarvester("tenant_x", 0)
	if prod.base.Scheme != "https" || prod.base.Host != "www.myntra.com" || prod.spacing() != 5*time.Second {
		t.Fatalf("production harvester: %v %v", prod.base, prod.spacing())
	}
	rig := newHarvestRig(t, &myntraFake{})
	for _, bad := range []string{"http://evil.example/x", "//evil.example/x", "https://www.myntra.com/x"} {
		if _, _, err := rig.h.get(context.Background(), bad); err == nil {
			t.Errorf("get(%q) must be refused", bad)
		}
	}
	if len(rig.fake.hits()) != 0 {
		t.Fatalf("a refused address made a request: %v", rig.fake.hits())
	}
	// Product links that leave www.myntra.com are dropped.
	if got := myntraProductURL("https://evil.example/p"); got != "" {
		t.Errorf("off-host link kept: %q", got)
	}
	if got := myntraProductURL("//evil.example/p"); got != "" {
		t.Errorf("off-host link kept: %q", got)
	}
	// A page that is not found is not an error that stops the whole run.
	_ = errors.New
}

func TestMyntraStateExtraction(t *testing.T) {
	one := myntraTestProduct(7, "Kurta", 100, 200)
	good := myntraTestPage(one)
	if ps, err := parseMyntraPage(good); err != nil || len(ps) != 1 || ps[0].Discount != 50 {
		t.Fatalf("__myx: %v %+v", err, ps)
	}
	fallback := []byte(`<script>window.__m_initial_state = {"searchData":{"results":{"products":[` + one + `]}}};</script> and some padding text here`)
	if ps, err := parseMyntraPage(fallback); err != nil || len(ps) != 1 || ps[0].Name != "Kurta" {
		t.Fatalf("fallback: %v %+v", err, ps)
	}
	// A "};" inside a string must not cut the object short.
	tricky := []byte(`<script>window.__myx = {"searchData":{"results":{"products":[{"productId":9,"productName":"Odd };  name","price":10,"mrp":10}]}}};</script> padding padding padding`)
	if ps, err := parseMyntraPage(tricky); err != nil || len(ps) != 1 || ps[0].Name != "Odd };  name" {
		t.Fatalf("tricky: %v %+v", err, ps)
	}
	if _, err := parseMyntraPage([]byte(`<script>window.__myx = {"searchData": {"results": </script> padding padding padding padding padding`)); err == nil {
		t.Fatal("malformed JSON must be an error, not a crash")
	}
	if _, err := parseMyntraPage([]byte("<html>no state here, padding padding padding padding padding padding padding</html>")); err == nil {
		t.Fatal("a page with no state must be an error")
	}
	// A product with no price is not an observation.
	if ps, _ := parseMyntraPage(myntraTestPage(`{"productId":1,"productName":"No price"}`, one)); len(ps) != 1 {
		t.Fatalf("priceless product kept: %+v", ps)
	}
}

func TestMyntraReparseNeedsNoNetworkAndKeepsPeopleEdits(t *testing.T) {
	fake := &myntraFake{pages: map[string][]byte{}}
	rig := newHarvestRig(t, fake)
	day := "2026-11-13"
	dir := filepath.Join(rig.h.root, day, "kurta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, b []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("page_1.html", myntraTestPage(myntraTestProduct(11, "Kurta A", 300, 600)))
	write("page_2.html", []byte(`<script>window.__myx = {"searchData": BROKEN</script> padding padding padding padding padding padding padding padding`))

	sum, err := ReparseMyntraHarvest(rig.tenant, day)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Files != 2 || sum.Written != 1 || len(sum.BadFiles) != 1 || !strings.Contains(sum.BadFiles[0], "page_2.html") {
		t.Fatalf("a bad page is recorded and the rest still parses: %+v", sum)
	}
	// A person links the row to their item; a re-parse with a fixed price must keep that.
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || '{"our_item":"SKU-9"}'::jsonb WHERE id = 'MYN-20261113-11'`, rig.schema)); err != nil {
		t.Fatal(err)
	}
	write("page_1.html", myntraTestPage(myntraTestProduct(11, "Kurta A", 250, 600)))
	if _, err := ReparseMyntraHarvest(rig.tenant, day); err != nil {
		t.Fatal(err)
	}
	var price, item string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'competitor_price', data->>'our_item' FROM %s.documents WHERE id = 'MYN-20261113-11'`, rig.schema)).Scan(&price, &item)
	if price != "250" || item != "SKU-9" {
		t.Fatalf("price %q our_item %q", price, item)
	}
	if n := countCompetitor(t, rig.schema, ""); n != 1 {
		t.Fatalf("rows = %d", n)
	}
	if len(fake.hits()) != 0 {
		t.Fatal("a re-parse made a request")
	}
	_, err = ReparseMyntraHarvest(rig.tenant, "2020-01-01")
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != harvestNoPagesCode {
		t.Fatalf("an empty day: %v", err)
	}
	if _, err = ReparseMyntraHarvest(rig.tenant, "13/11/2026"); !errors.As(err, &ve) || ve.Code != harvestInvalidCode {
		t.Fatalf("a bad day: %v", err)
	}
}

func TestMyntraHarvestIsOffByDefaultAndPerClient(t *testing.T) {
	fake := &myntraFake{pages: map[string][]byte{"/kurta?rawQuery=kurta": myntraTestPage(myntraTestProduct(21, "Kurta B", 400, 800))}}
	rigA := newHarvestRig(t, fake)
	tenantB, schemaB := sodTenant(t)

	// Off by default: enqueue is refused with PIM-9340 and the job handler does nothing either.
	_, err := EnqueueMyntraHarvest(rigA.tenant, "admin", MyntraHarvestRequest{Keywords: []string{"kurta"}})
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != harvestOffCode {
		t.Fatalf("setting off: %v", err)
	}
	out, herr := jobHandlers[myntraHarvestJobType](context.Background(), rigA.schema, Job{Payload: map[string]interface{}{"tenant_id": rigA.tenant, "keywords": []interface{}{"kurta"}}})
	if herr != nil || !strings.Contains(fmt.Sprint(out["stop_reason"]), "switched off") || len(fake.hits()) != 0 {
		t.Fatalf("handler with the setting off: %v %v hits=%v", out, herr, fake.hits())
	}

	// On for A only: a run is queued; a second one is refused while it waits; invalid input is refused.
	if err := SetSetting(rigA.tenant, SettingMyntraHarvest, "true", "system"); err != nil {
		t.Fatal(err)
	}
	if _, err := EnqueueMyntraHarvest(rigA.tenant, "admin", MyntraHarvestRequest{Keywords: []string{"  "}}); !errors.As(err, &ve) || ve.Code != harvestInvalidCode {
		t.Fatalf("no keyword: %v", err)
	}
	if _, err := EnqueueMyntraHarvest(tenantB, "admin", MyntraHarvestRequest{Keywords: []string{"kurta"}}); !errors.As(err, &ve) || ve.Code != harvestOffCode {
		t.Fatalf("client B never turned it on: %v", err)
	}
	id, err := EnqueueMyntraHarvest(rigA.tenant, "admin", MyntraHarvestRequest{Keywords: []string{"kurta"}, MaxPages: 3})
	if err != nil || id == "" {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := EnqueueMyntraHarvest(rigA.tenant, "admin", MyntraHarvestRequest{Keywords: []string{"kurta"}}); !errors.As(err, &ve) || ve.Code != harvestBusyCode {
		t.Fatalf("second run while one waits: %v", err)
	}
	runs, err := MyntraHarvestRuns(rigA.tenant, 5)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs: %v %v", runs, err)
	}
	if other, _ := MyntraHarvestRuns(tenantB, 5); len(other) != 0 {
		t.Fatalf("client B sees client A's run: %v", other)
	}

	// Results land in A's schema only, and A's raw pages sit in A's own folder.
	res := rigA.h.run(context.Background(), rigA.tenant, MyntraHarvestRequest{Keywords: []string{"kurta"}}, 3)
	if res.Parse.Written != 1 {
		t.Fatalf("%+v", res)
	}
	if n := countCompetitor(t, rigA.schema, ""); n != 1 {
		t.Fatalf("A rows = %d", n)
	}
	if n := countCompetitor(t, schemaB, ""); n != 0 {
		t.Fatalf("client B sees client A's prices: %d", n)
	}
	if marketHarvestRoot(rigA.schema) == marketHarvestRoot(schemaB) || !strings.Contains(marketHarvestRoot(rigA.schema), rigA.schema) {
		t.Fatal("raw pages must live in a per-client folder")
	}
	if _, err := os.Stat(marketHarvestRoot(schemaB)); err == nil {
		t.Fatal("client B has a raw folder")
	}
	// Min interval is enforced even if a setting were forced lower.
	if st := MyntraHarvestStatus(rigA.tenant); st["interval_seconds"].(int) < 5 || st["enabled"] != true {
		t.Fatalf("status: %v", st)
	}
}
