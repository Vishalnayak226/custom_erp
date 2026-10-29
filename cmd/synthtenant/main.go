// Command synthtenant fills a scratch database with a deterministic synthetic
// tenant for the performance laboratory (Stage 47.9.1), and with -measure
// prints the bytes each kind of transaction adds (Stage 47.18.2).
//
// It refuses ENV=production and any database whose name does not look like a
// scratch one (test / scratch / synth / bench / perf); ERP_SYNTH_ALLOW_DB
// names one plainly named copy on purpose. External side effects are turned
// off for the whole run, and the tenant is a sandbox.
//
//	DATABASE_URL=postgres://.../erp_synth go run ./cmd/synthtenant -shape small -seed 1
//	DATABASE_URL=postgres://.../erp_synth go run ./cmd/synthtenant -shape 10k -seed 1 -measure
//	DATABASE_URL=postgres://.../erp_synth go run ./cmd/synthtenant -shape small -seed 2 -slow-ms 25
//	DATABASE_URL=postgres://.../erp_synth go run ./cmd/synthtenant -delete synth-small-1
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

func main() {
	shape := flag.String("shape", "small", "tenant shape: small, 10k, 100k or 1m")
	seed := flag.Uint64("seed", 1, "seed; one seed always builds the same tenant")
	tenant := flag.String("tenant", "", "tenant id (default synth-<shape>-<seed>)")
	measure := flag.Bool("measure", false, "also report bytes per transaction kind")
	asJSON := flag.Bool("json", false, "print the result as JSON")
	del := flag.String("delete", "", "drop this synthetic tenant and exit")
	bills := flag.Int("bills", 0, "override the shape's bill count (masters and stock keep the shape's size)")
	slowMs := flag.Int("slow-ms", 0, "sample statements running longer than this many ms during the build and print the top fingerprints (0 = off)")
	flag.Parse()

	// Before anything connects: nothing this run does may leave the machine.
	os.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "1")
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is not set")
		os.Exit(2)
	}
	db.InitDB(raw)
	if err := engines.CheckSyntheticTarget(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *del != "" {
		if err := engines.DeleteSyntheticTenant(*del); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("deleted", *del)
		return
	}

	// -slow-ms: the statements that ran longest during the build, as
	// fingerprints (47.9.3) - the regression evidence to compare between runs.
	var sampler *engines.SlowQuerySampler
	stop := func() {}
	if *slowMs > 0 {
		sampler = &engines.SlowQuerySampler{Store: engines.NewFingerprintStore(100), Threshold: time.Duration(*slowMs) * time.Millisecond}
		ctx, cancel := context.WithCancel(context.Background())
		go sampler.Run(ctx, time.Duration(max(*slowMs/2, 10))*time.Millisecond)
		stop = cancel
	}
	opts := engines.SynthOptions{Shape: *shape, Seed: *seed, TenantID: *tenant, Measure: *measure, Log: os.Stderr}
	if *bills > 0 {
		// The shape's masters and stock, fewer bills: per-transaction sizes
		// measured against a full-size master set without the full run.
		s, ok := engines.SynthShapeNamed(*shape)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown shape %q\n", *shape)
			os.Exit(2)
		}
		s.Bills = *bills
		s.LargeOrders = min(s.LargeOrders, max(*bills/50, 1))
		s.Name = fmt.Sprintf("%s-b%d", *shape, *bills)
		opts.Custom = &s
	}
	res, err := engines.GenerateSyntheticTenant(opts)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if sampler != nil {
		fmt.Fprintf(os.Stderr, "\nslowest statements over %d ms (sampled, no values):\n", *slowMs)
		for _, fp := range sampler.Store.Top(15, "total") {
			fmt.Fprintf(os.Stderr, "  %8.0f ms total  %4d calls  %6.0f ms max  %s\n", fp.TotalMs, fp.Calls, fp.MaxMs, fp.Query)
		}
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
		return
	}
	fmt.Printf("tenant %s (schema %s), shape %s, seed %d, hot SKU %s, %s\nchecksum %s\n", res.TenantID, res.Schema, res.Shape.Name, res.Seed, res.HotSKU, res.Elapsed.Round(1e6), res.Checksum)
	keys := make([]string, 0, len(res.Counts))
	for k := range res.Counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-18s %d\n", k, res.Counts[k])
	}
	if len(res.Footprint) > 0 {
		fmt.Printf("\n| Kind | Count | Rows | Heap p50 | Heap p95 | Factor | Total p50 | Total p95 |\n|---|---:|---:|---:|---:|---:|---:|---:|\n")
		for _, f := range res.Footprint {
			fmt.Printf("| %s | %d | %.1f | %d | %d | %.2f | %d | %d |\n", f.Kind, f.Count, f.RowsMean, f.HeapP50, f.HeapP95, f.Factor, f.TotalP50, f.TotalP95)
		}
	}
}
