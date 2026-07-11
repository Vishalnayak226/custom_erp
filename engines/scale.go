package engines

import (
	"custom_erp/db"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// SeedScaleTestData seeds initial inventory availability across multiple mock store locations
func SeedScaleTestData(tenantID string, numStores int, sku string, initialQty int) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	// Clean up any existing mock locations for this SKU
	_, _ = tx.Exec(fmt.Sprintf("DELETE FROM %s.inventory_availability WHERE sku = $1", schema), sku)

	// Prepared statement for fast bulk inserts
	stmt, err := tx.Prepare(fmt.Sprintf(`
		INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) 
		VALUES ($1, $2, $3, $3)`, schema))
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numStores; i++ {
		locCode := fmt.Sprintf("LOC-%04d", i)
		_, err := stmt.Exec(sku, locCode, initialQty)
		if err != nil {
			return fmt.Errorf("failed to seed store %s: %v", locCode, err)
		}
	}

	return tx.Commit()
}

// RunScaleSimulation runs a concurrent worker pool executing checkouts and order reservations
func RunScaleSimulation(tenantID string, numWorkers int, numTransactions int, testSKU string, numStores int) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	// Clean up old postings for clean trial asserts
	_, _ = db.DB.Exec(fmt.Sprintf("DELETE FROM %s.gl_postings", schema))

	var wg sync.WaitGroup
	tasksChan := make(chan int, numTransactions)
	durationsChan := make(chan time.Duration, numTransactions)
	errorsChan := make(chan error, numTransactions)

	start := time.Now()

	// Spawn worker pool
	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))

			for range tasksChan {
				txStart := time.Now()
				// Pick a random store location from seeded stores range to simulate load dispersion
				storeIndex := r.Intn(numStores) + 1
				locationCode := fmt.Sprintf("LOC-%04d", storeIndex)

				// Determine operation mix: 60% POS checkouts, 40% Shopify webhooks/reservations
				opType := r.Intn(100)
				var err error

				if opType < 60 {
					// 1. Simulate POS checkout (decredits stock, books GL accounts)
					checkoutItems := []interface{}{
						map[string]interface{}{"sku": testSKU, "qty": 1},
					}
					err = PostInventoryLedger(tenantID, locationCode, checkoutItems)
					if err == nil {
						// Post double-entry accounting records
						cartID := fmt.Sprintf("W-%d-CRT-%d", workerID, time.Now().UnixNano())
						err = PostSalesFinanceBooking(tenantID, cartID, 500, 300)
					}
				} else {
					// 2. Simulate Webhook reservation (reserves stock)
					_, err = CreateReservation(tenantID, testSKU, locationCode, 1, "Online", 300)
				}

				if err != nil {
					errorsChan <- err
				} else {
					durationsChan <- time.Since(txStart)
				}
			}
		}(w)
	}

	// Feed tasks into queue
	for i := 0; i < numTransactions; i++ {
		tasksChan <- i
	}
	close(tasksChan)

	// Wait for workers to complete
	wg.Wait()
	close(durationsChan)
	close(errorsChan)

	elapsed := time.Now().Sub(start)

	// Collect metrics
	var durations []time.Duration
	for dur := range durationsChan {
		durations = append(durations, dur)
	}

	var errors []error
	for err := range errorsChan {
		errors = append(errors, err)
	}

	totalSuccess := len(durations)
	totalFailed := len(errors)

	// Compute percentiles
	p50 := time.Duration(0)
	p95 := time.Duration(0)
	p99 := time.Duration(0)
	avg := time.Duration(0)

	if totalSuccess > 0 {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		p50 = durations[int(float64(totalSuccess)*0.50)]
		p95 = durations[int(float64(totalSuccess)*0.95)]
		p99 = durations[int(float64(totalSuccess)*0.99)]

		var sum time.Duration
		for _, d := range durations {
			sum += d
		}
		avg = sum / time.Duration(totalSuccess)
	}

	tps := float64(totalSuccess) / elapsed.Seconds()

	return map[string]interface{}{
		"elapsed_seconds": elapsed.Seconds(),
		"total_processed": numTransactions,
		"success_count":   totalSuccess,
		"failure_count":   totalFailed,
		"tps":             tps,
		"avg_latency_ms":  float64(avg.Microseconds()) / 1000.0,
		"p50_latency_ms":  float64(p50.Microseconds()) / 1000.0,
		"p95_latency_ms":  float64(p95.Microseconds()) / 1000.0,
		"p99_latency_ms":  float64(p99.Microseconds()) / 1000.0,
	}, nil
}
