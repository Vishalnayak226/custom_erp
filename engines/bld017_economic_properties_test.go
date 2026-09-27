package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
)

// BLD-017 (economic property and metamorphic tests): bounded, randomly
// generated cases for money/rounding, UOM conversion, stock conservation,
// reversal/refund totals and balanced posting. Every property is checked
// across three rounds, each driven by its own fixed seed in bld017Seeds -
// replayable, not time-based - so a failure names the exact seed and
// iteration that reproduces it (retained as a counterexample in the failure
// message itself, since Go's testing package has no separate corpus store
// for table-style property tests the way testing.F has for fuzz targets).
//
// Every expected value below is derived independently of the function under
// test: from a second, separate implementation of a well-defined rule
// (rounding), from the domain's own ground truth (price x qty, sum of
// signed stock deltas, debit == credit), or from a fixed lookup table read
// directly from source (returnDispositionRule) - never by calling the same
// internal helper the implementation itself uses to compute its answer.
//
// TestBLD017MoneyConservation's GSTSplit subtest found a real bug this same
// session: CalculateGST (gst.go) rounded CGST and SGST independently, which
// drifts CGST+SGST off TotalTax by 1 paisa on any odd-paisa total tax -
// roughly half of all real amounts. Fixed at the source (gst.go) using the
// same remainder-absorption idiom already established by
// ConvertPostingToFunctional/ApplyLandedCostVoucher/monthlyRecognitionPaise;
// this test is the regression proof, not just the discovery.
var bld017Seeds = []int64{20260917, 411, 8675309}

// bld017RoundPaise independently rounds to the nearest paisa (2dp,
// half-away-from-zero) without calling gst.go's own unexported round2 - a
// second, separate implementation of the same well-defined rounding rule.
func bld017RoundPaise(v float64) float64 {
	if v < 0 {
		return -math.Floor(-v*100+0.5) / 100
	}
	return math.Floor(v*100+0.5) / 100
}

func TestBLD017MoneyConservation(t *testing.T) {
	t.Run("GSTSplit", func(t *testing.T) {
		rates := []float64{0, 0.25, 1, 1.5, 3, 5, 12, 18, 28, 40}
		for round, seed := range bld017Seeds {
			rng := rand.New(rand.NewSource(seed))
			const iterations = 500
			for i := 0; i < iterations; i++ {
				taxable := float64(rng.Intn(99999999)) / 100.0 // 0.00 - 999999.99
				rate := rates[rng.Intn(len(rates))]
				interstate := rng.Intn(2) == 0

				got, err := CalculateGST(taxable, rate, interstate)
				if err != nil {
					t.Fatalf("round=%d seed=%d iter=%d taxable=%v rate=%v: unexpected error %v", round, seed, i, taxable, rate, err)
				}

				expectedTotalTax := bld017RoundPaise(taxable * rate / 100)
				if math.Abs(got.TotalTax-expectedTotalTax) > 1e-9 {
					t.Fatalf("counterexample: round=%d seed=%d iter=%d taxable=%v rate=%v: TotalTax=%v want %v", round, seed, i, taxable, rate, got.TotalTax, expectedTotalTax)
				}
				if interstate {
					if math.Abs(got.IGST-expectedTotalTax) > 1e-9 || got.CGST != 0 || got.SGST != 0 {
						t.Fatalf("counterexample: round=%d seed=%d iter=%d taxable=%v rate=%v: interstate IGST=%v CGST=%v SGST=%v want IGST=%v CGST=0 SGST=0",
							round, seed, i, taxable, rate, got.IGST, got.CGST, got.SGST, expectedTotalTax)
					}
				} else {
					// The property the GST-split bug this test found and this
					// session fixed violated: CGST+SGST must equal the total
					// tax exactly, never drift by a paisa.
					if math.Abs((got.CGST+got.SGST)-expectedTotalTax) > 1e-9 {
						t.Fatalf("counterexample: round=%d seed=%d iter=%d taxable=%v rate=%v: CGST(%v)+SGST(%v)=%v != TotalTax %v",
							round, seed, i, taxable, rate, got.CGST, got.SGST, got.CGST+got.SGST, expectedTotalTax)
					}
					if got.IGST != 0 {
						t.Fatalf("counterexample: round=%d seed=%d iter=%d: intrastate posted a nonzero IGST %v", round, seed, i, got.IGST)
					}
				}
				if got.CGST < 0 || got.SGST < 0 || got.IGST < 0 || got.TotalTax < 0 {
					t.Fatalf("counterexample: round=%d seed=%d iter=%d: negative tax component in %+v", round, seed, i, got)
				}
			}
		}
	})

	t.Run("AmortizationSchedule", func(t *testing.T) {
		for round, seed := range bld017Seeds {
			rng := rand.New(rand.NewSource(seed))
			const iterations = 500
			for i := 0; i < iterations; i++ {
				total := float64(1+rng.Intn(999999999)) / 100.0 // 0.01 - 9999999.99
				termMonths := 1 + rng.Intn(60)

				sum := int64(0)
				for m := 0; m < termMonths; m++ {
					sum += monthlyRecognitionPaise(total, termMonths, m)
				}
				expected := RupeesToPaise(total)
				if sum != expected {
					t.Fatalf("counterexample: round=%d seed=%d iter=%d total=%v termMonths=%d: schedule sums to %d paise, want %d",
						round, seed, i, total, termMonths, sum, expected)
				}
			}
		}
	})
}

// TestBLD017UOMRoundTrip checks ConvertUOMQty's metamorphic round-trip
// property (A -> B -> A recovers the original qty) rather than re-deriving
// the multiply-or-divide formula ConvertUOMQty itself uses - a formula-level
// check would just be testing the implementation against itself.
func TestBLD017UOMRoundTrip(t *testing.T) {
	spInitDB()
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'UOMConversion' AND data->>'item' LIKE 'SKU-BLD017-UOM-%'")
	})

	for round, seed := range bld017Seeds {
		rng := rand.New(rand.NewSource(seed))
		const iterations = 40
		for i := 0; i < iterations; i++ {
			sku := fmt.Sprintf("SKU-BLD017-UOM-%d-%d", seed, i)
			factor := float64(1+rng.Intn(99999)) / 100.0 // 0.01 - 1000.00
			qty := float64(1+rng.Intn(9999999)) / 100.0  // 0.01 - 99999.99

			convData, _ := json.Marshal(map[string]interface{}{
				"item": sku, "from_uom": "A", "to_uom": "B", "factor": factor, "status": "Active",
			})
			if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'UOMConversion', $2, 'Active', 'system')",
				"UOMCONV-BLD017-"+sku, convData); err != nil {
				t.Fatalf("round=%d seed=%d iter=%d: seed UOMConversion: %v", round, seed, i, err)
			}

			forward, err := ConvertUOMQty(tenantID, sku, qty, "A", "B")
			if err != nil {
				t.Fatalf("round=%d seed=%d iter=%d sku=%s qty=%v factor=%v: forward conversion error %v", round, seed, i, sku, qty, factor, err)
			}
			back, err := ConvertUOMQty(tenantID, sku, forward, "B", "A")
			if err != nil {
				t.Fatalf("round=%d seed=%d iter=%d sku=%s qty=%v factor=%v: inverse conversion error %v", round, seed, i, sku, qty, factor, err)
			}

			tolerance := 1e-6 * math.Max(1, math.Abs(qty))
			if math.Abs(back-qty) > tolerance {
				t.Fatalf("counterexample: round=%d seed=%d iter=%d sku=%s qty=%v factor=%v: A->B->A = %v, want %v (forward=%v)",
					round, seed, i, sku, qty, factor, back, qty, forward)
			}
		}
	}
}

// TestBLD017StockConservation drives PostInventoryLedgerWithVoucher, the
// choke point every issue/receipt/adjustment on a single SKU/location goes
// through, with a bounded random sequence of receipts and issues, and checks
// the persisted on_hand/available after EVERY operation against an
// independently accumulated running total in Go - not against anything the
// function itself returns.
func TestBLD017StockConservation(t *testing.T) {
	spInitDB()
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku LIKE 'SKU-BLD017-STOCK-%'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' LIKE 'SKU-BLD017-STOCK-%'")
	})

	for round, seed := range bld017Seeds {
		rng := rand.New(rand.NewSource(seed))
		sku := fmt.Sprintf("SKU-BLD017-STOCK-%d", seed)
		location := fmt.Sprintf("LOC-BLD017-STOCK-%d", seed)

		// A SKU/location with no row at all yet (never received) must still
		// refuse a negative issue when allowNegative is false - a distinct
		// code path (sql.ErrNoRows) from "0 available but a row exists,"
		// which the sequence below never reaches since it always receives
		// first. A mutation-testing pass this same session found this path
		// untested: a mutant inverting its allowNegative check survived.
		neverStockedSKU := fmt.Sprintf("SKU-BLD017-STOCK-NEW-%d", seed)
		if _, err := PostInventoryLedgerWithVoucher(tenantID, location, []interface{}{
			map[string]interface{}{"sku": neverStockedSKU, "qty": -1},
		}, false, "BLD017Property", fmt.Sprintf("BLD017-STOCK-%d-neverstocked", seed), "system"); err == nil {
			t.Fatalf("counterexample: round=%d seed=%d: issuing -1 of a SKU with no existing inventory_availability row was accepted, expected InsufficientStockError", round, seed)
		}
		// The allowNegative=true counterpart, which is what actually
		// distinguishes the ErrNoRows branch's own allowNegative check from
		// the general floor check a few lines below it (both branches reach
		// the same general check with currentAvailable=0, so an
		// allowNegative=false mutant there is externally equivalent - this
		// is the input that is NOT equivalent: allowNegative=true must be
		// permitted to go negative even when the SKU has no row at all yet).
		neverStockedAllowSKU := fmt.Sprintf("SKU-BLD017-STOCK-NEWALLOW-%d", seed)
		if _, err := PostInventoryLedgerWithVoucher(tenantID, location, []interface{}{
			map[string]interface{}{"sku": neverStockedAllowSKU, "qty": -1},
		}, true, "BLD017Property", fmt.Sprintf("BLD017-STOCK-%d-neverstocked-allow", seed), "system"); err != nil {
			t.Fatalf("counterexample: round=%d seed=%d: issuing -1 of a never-stocked SKU with allowNegative=true was refused: %v", round, seed, err)
		}
		t.Cleanup(func() {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", neverStockedAllowSKU)
		})

		balance := 0
		const ops = 25
		for i := 0; i < ops; i++ {
			var delta int
			switch {
			case balance <= 0:
				delta = 1 + rng.Intn(500) // must receive before there's anything to issue
			case rng.Intn(2) == 0:
				delta = 1 + rng.Intn(500)
			default:
				maxIssue := balance
				if maxIssue > 500 {
					maxIssue = 500
				}
				delta = -(1 + rng.Intn(maxIssue))
			}
			balance += delta

			if _, err := PostInventoryLedgerWithVoucher(tenantID, location, []interface{}{
				map[string]interface{}{"sku": sku, "qty": delta},
			}, false, "BLD017Property", fmt.Sprintf("BLD017-STOCK-%d-%d", seed, i), "system"); err != nil {
				t.Fatalf("round=%d seed=%d op=%d delta=%d running_balance=%d: PostInventoryLedgerWithVoucher error %v", round, seed, i, delta, balance, err)
			}

			var onHand, available int
			if err := db.DB.QueryRow("SELECT on_hand, available FROM "+schema+".inventory_availability WHERE sku=$1 AND location_code=$2", sku, location).Scan(&onHand, &available); err != nil {
				t.Fatalf("round=%d seed=%d op=%d: read back inventory_availability: %v", round, seed, i, err)
			}
			if onHand != balance || available != balance {
				t.Fatalf("counterexample: round=%d seed=%d op=%d delta=%d: expected on_hand=available=%d (independently accumulated sum of deltas), got on_hand=%d available=%d",
					round, seed, i, delta, balance, onHand, available)
			}
		}

		// Deterministically exercise the exact-zero boundary: issuing
		// precisely the current balance must succeed (0 remaining is not
		// insufficient stock), not just be reachable by chance from the
		// random sequence above. A mutation-testing pass this same session
		// found this boundary was previously only reachable by chance and,
		// on the seeds tried, was never actually hit - a mutant shifting the
		// floor check from "< 0" to "<= 0" survived undetected.
		if balance > 0 {
			exhaust := -balance
			if _, err := PostInventoryLedgerWithVoucher(tenantID, location, []interface{}{
				map[string]interface{}{"sku": sku, "qty": exhaust},
			}, false, "BLD017Property", fmt.Sprintf("BLD017-STOCK-%d-exhaust", seed), "system"); err != nil {
				t.Fatalf("counterexample: round=%d seed=%d: issuing exactly the current balance (%d) down to zero was refused: %v", round, seed, balance, err)
			}
			balance = 0
			var onHand, available int
			if err := db.DB.QueryRow("SELECT on_hand, available FROM "+schema+".inventory_availability WHERE sku=$1 AND location_code=$2", sku, location).Scan(&onHand, &available); err != nil {
				t.Fatalf("round=%d seed=%d: read back inventory_availability after exhausting to zero: %v", round, seed, err)
			}
			if onHand != 0 || available != 0 {
				t.Fatalf("counterexample: round=%d seed=%d: expected on_hand=available=0 after issuing exactly the balance, got on_hand=%d available=%d", round, seed, onHand, available)
			}
		}
	}
}

// TestBLD017RefundNeverExceedsPaid drives the real
// CreateReturnRequest -> ApproveReturnRequest -> ReceiveReturnRequest ->
// ApplyReturnQC workflow with randomly generated lines/prices/dispositions,
// and checks the refund total against an independently summed expectation -
// price x qty for each line whose disposition returnDispositionRule marks
// RefundEligible - not against anything ApplyReturnQC computes internally.
// returnDispositionRule itself is read directly from returns.go as a fixed
// ground-truth lookup table, not derived from the function under test.
func TestBLD017RefundNeverExceedsPaid(t *testing.T) {
	spInitDB()
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	const location = "LOC-BLD017-RET"
	dispositions := []string{"Sellable", "Damaged", "Repairable", "Missing", "Wrong-Item", "Rejected"}

	var cartIDs, returnIDs []string
	t.Cleanup(func() {
		for _, id := range returnIDs {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'RefundRequest' AND data->>'return_request_id' = $1", id)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_type = 'ReturnRequest' AND document_id = $1", id)
		}
		for _, id := range cartIDs {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'POSCart' AND id = $1", id)
		}
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku LIKE 'SKU-BLD017-RET-%'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' LIKE 'SKU-BLD017-RET-%'")
	})

	for round, seed := range bld017Seeds {
		rng := rand.New(rand.NewSource(seed))
		const iterations = 8
		for i := 0; i < iterations; i++ {
			numLines := 1 + rng.Intn(3)
			var items []ReturnItemInput
			var cartLines []map[string]interface{}
			dispBySKU := map[string]string{}
			expectedRefund := 0.0
			totalPaid := 0.0
			for l := 0; l < numLines; l++ {
				sku := fmt.Sprintf("SKU-BLD017-RET-%d-%d-%d", seed, i, l)
				qty := 1 + rng.Intn(5)
				price := float64(100+rng.Intn(900000)) / 100.0 // 1.00 - 9000.99
				cost := price * 0.6
				disp := dispositions[rng.Intn(len(dispositions))]

				cartLines = append(cartLines, map[string]interface{}{"sku": sku, "qty": qty, "sale_price": price, "cost_price": cost})
				items = append(items, ReturnItemInput{SKU: sku, Qty: qty})
				dispBySKU[sku] = disp
				totalPaid += price * float64(qty)
				if returnDispositionRule[disp].RefundEligible {
					expectedRefund += price * float64(qty)
				}
			}

			cartID := fmt.Sprintf("PC-BLD017-RET-%d-%d", seed, i)
			cartData, _ := json.Marshal(map[string]interface{}{"items": cartLines})
			if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')", cartID, cartData); err != nil {
				t.Fatalf("round=%d seed=%d iter=%d: seed POSCart: %v", round, seed, i, err)
			}
			cartIDs = append(cartIDs, cartID)

			returnID, err := CreateReturnRequest(tenantID, "Customer Return", location, cartID, "", "bld017-tester", items)
			if err != nil {
				t.Fatalf("round=%d seed=%d iter=%d: CreateReturnRequest: %v", round, seed, i, err)
			}
			returnIDs = append(returnIDs, returnID)
			if err := ApproveReturnRequest(tenantID, returnID, "bld017-approver"); err != nil {
				t.Fatalf("round=%d seed=%d iter=%d: ApproveReturnRequest: %v", round, seed, i, err)
			}
			if err := ReceiveReturnRequest(tenantID, returnID, "bld017-warehouse"); err != nil {
				t.Fatalf("round=%d seed=%d iter=%d: ReceiveReturnRequest: %v", round, seed, i, err)
			}
			refundTotal, _, err := ApplyReturnQC(tenantID, returnID, dispBySKU, "bld017-qc")
			if err != nil {
				t.Fatalf("round=%d seed=%d iter=%d: ApplyReturnQC: %v", round, seed, i, err)
			}

			if math.Abs(refundTotal-expectedRefund) > 0.005 {
				t.Fatalf("counterexample: round=%d seed=%d iter=%d dispositions=%v: refund=%v, want %v (independently summed refund-eligible lines)",
					round, seed, i, dispBySKU, refundTotal, expectedRefund)
			}
			if refundTotal > totalPaid+0.005 {
				t.Fatalf("counterexample: round=%d seed=%d iter=%d: refund=%v exceeds total paid=%v", round, seed, i, refundTotal, totalPaid)
			}
			if refundTotal < -0.005 {
				t.Fatalf("counterexample: round=%d seed=%d iter=%d: negative refund %v", round, seed, i, refundTotal)
			}
		}
	}
}

// TestBLD017BalancedPostingInvariant checks PostDoubleEntry's core invariant
// directly against randomly generated debit/credit maps over real chart-of-
// accounts codes: a balanced map always posts and persists with matching
// summed debit/credit; a map perturbed to be unbalanced is always refused,
// with nothing written under its key.
func TestBLD017BalancedPostingInvariant(t *testing.T) {
	spInitDB()
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'BLD017Property'")
	})

	// Real seeded gl_accounts codes (db/migration.sql) - account_code carries
	// a foreign key to gl_accounts, so this can't be arbitrary random text.
	accounts := []string{"1100", "1200", "2100", "4100", "5100"}

	for round, seed := range bld017Seeds {
		rng := rand.New(rand.NewSource(seed))
		const iterations = 15
		for i := 0; i < iterations; i++ {
			total := int64(100 + rng.Intn(9999999)) // 100 - ~10,000,099 paise

			splitInto := func(n int) map[string]int64 {
				cuts := make([]int64, n-1)
				for c := range cuts {
					cuts[c] = 1 + rng.Int63n(total-1) // in [1, total-1]
				}
				sort.Slice(cuts, func(a, b int) bool { return cuts[a] < cuts[b] })
				accs := append([]string(nil), accounts...)
				rng.Shuffle(len(accs), func(a, b int) { accs[a], accs[b] = accs[b], accs[a] })
				accs = accs[:n]
				m := map[string]int64{}
				prev := int64(0)
				for idx := 0; idx < n; idx++ {
					upper := total
					if idx < len(cuts) {
						upper = cuts[idx]
					}
					m[accs[idx]] += upper - prev
					prev = upper
				}
				return m
			}

			debits := splitInto(1 + rng.Intn(3))
			credits := splitInto(1 + rng.Intn(3))

			docID := fmt.Sprintf("BLD017-DOC-%d-%d", seed, i)
			postingKey := fmt.Sprintf("BLD017Property:%d:%d", seed, i)

			if err := PostDoubleEntry(tenantID, "BLD017Property", docID, debits, credits, "", postingKey); err != nil {
				t.Fatalf("round=%d seed=%d iter=%d debits=%v credits=%v: expected balanced post (both sum to %d) to succeed, got %v",
					round, seed, i, debits, credits, total, err)
			}

			var sumDebit, sumCredit int64
			if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE idempotency_key=$1", postingKey).Scan(&sumDebit, &sumCredit); err != nil {
				t.Fatalf("round=%d seed=%d iter=%d: read back gl_postings: %v", round, seed, i, err)
			}
			if sumDebit != total || sumCredit != total || sumDebit != sumCredit {
				t.Fatalf("counterexample: round=%d seed=%d iter=%d: persisted debit=%d credit=%d, want both %d", round, seed, i, sumDebit, sumCredit, total)
			}

			// Perturb one side so it can no longer balance, and confirm the
			// post is refused with nothing written under a fresh key. Which
			// side gets perturbed is itself randomized - a mutation-testing
			// pass this same session found that always perturbing debits
			// upward left a real gap: a mutant that only checks
			// sumDebits > sumCredits (missing the understated-debits /
			// overstated-credits direction) survived, because every
			// generated case had debits > credits by construction. Both
			// directions must appear across the bounded random iterations.
			unbalanced := map[string]int64{}
			perturbSide := debits
			perturbOther := credits
			perturbLabel := "debits"
			if rng.Intn(2) == 0 {
				perturbSide = credits
				perturbOther = debits
				perturbLabel = "credits"
			}
			for k, v := range perturbSide {
				unbalanced[k] = v
			}
			perturbAccount := accounts[rng.Intn(len(accounts))]
			unbalanced[perturbAccount] += 1 + rng.Int63n(1000)
			badKey := postingKey + ":unbalanced"
			badDocID := docID + "-unbalanced"
			var postErr error
			if perturbLabel == "debits" {
				postErr = PostDoubleEntry(tenantID, "BLD017Property", badDocID, unbalanced, perturbOther, "", badKey)
			} else {
				postErr = PostDoubleEntry(tenantID, "BLD017Property", badDocID, perturbOther, unbalanced, "", badKey)
			}
			if postErr == nil {
				t.Fatalf("counterexample: round=%d seed=%d iter=%d: unbalanced %s=%v (other side=%v) was accepted, expected an error",
					round, seed, i, perturbLabel, unbalanced, perturbOther)
			}
			var stray int
			if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".gl_postings WHERE idempotency_key=$1", badKey).Scan(&stray); err != nil {
				t.Fatalf("round=%d seed=%d iter=%d: read back rejected posting: %v", round, seed, i, err)
			}
			if stray != 0 {
				t.Fatalf("counterexample: round=%d seed=%d iter=%d: a rejected unbalanced posting still wrote %d gl_postings rows", round, seed, i, stray)
			}
		}
	}
}

// TestBLD017OwnershipInvariant checks the single-owner-per-warehouse guard
// (AssertSingleOwnerForLocation, Stage 47.5.1/audit A-05) as a property over
// randomized owner-assignment sequences: once a location is claimed by an
// owner, WarehouseOwnerOf must agree with the FIRST owner ever assigned in
// the sequence for every step after - tracked independently in Go, not
// re-derived from anything AssertSingleOwnerForLocation itself returns. This
// generalizes the fixed example TestSingleOwnerWarehouseGuard already covers
// (one owner adopts, same owner repeats, a different owner is refused) into
// bounded random sequences across three rounds.
func TestBLD017OwnershipInvariant(t *testing.T) {
	spInitDB()
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	previousMode := GetSettingString(tenantID, StockOwnershipModeSetting)
	if err := SetSetting(tenantID, StockOwnershipModeSetting, OwnershipSingleOwner, "bld017-test"); err != nil {
		t.Fatalf("failed to set ownership mode: %v", err)
	}
	t.Cleanup(func() {
		_ = SetSetting(tenantID, StockOwnershipModeSetting, previousMode, "bld017-test")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".warehouse_owner WHERE location_code LIKE 'LOC-BLD017-OWN-%'")
	})

	owners := []string{"OWNER-BLD017-A", "OWNER-BLD017-B", "OWNER-BLD017-C"}

	for round, seed := range bld017Seeds {
		rng := rand.New(rand.NewSource(seed))
		location := fmt.Sprintf("LOC-BLD017-OWN-%d", seed)
		var boundOwner string
		const iterations = 15
		for i := 0; i < iterations; i++ {
			owner := owners[rng.Intn(len(owners))]
			assignErr := AssertSingleOwnerForLocation(tenantID, location, owner)

			switch {
			case boundOwner == "":
				// First call for this location: must succeed and bind it.
				if assignErr != nil {
					t.Fatalf("counterexample: round=%d seed=%d iter=%d: first assignment of %s to %s refused: %v", round, seed, i, location, owner, assignErr)
				}
				boundOwner = owner
			case owner == boundOwner:
				if assignErr != nil {
					t.Fatalf("counterexample: round=%d seed=%d iter=%d: repeat assignment of the SAME owner %s to %s refused: %v", round, seed, i, owner, location, assignErr)
				}
			default:
				if assignErr == nil {
					t.Fatalf("counterexample: round=%d seed=%d iter=%d: a DIFFERENT owner %s was accepted into %s, already bound to %s", round, seed, i, owner, location, boundOwner)
				}
			}

			got, err := WarehouseOwnerOf(tenantID, location)
			if err != nil {
				t.Fatalf("round=%d seed=%d iter=%d: WarehouseOwnerOf: %v", round, seed, i, err)
			}
			if got != boundOwner {
				t.Fatalf("counterexample: round=%d seed=%d iter=%d: WarehouseOwnerOf=%q, want the first-ever-bound owner %q (independently tracked in Go, not re-derived from the guard's own return value)",
					round, seed, i, got, boundOwner)
			}
		}
	}
}
