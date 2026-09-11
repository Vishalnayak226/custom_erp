package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Stage 47.0.1 / audit finding A-05 ("3PL stock ownership is not enforced
// during allocation/picking" -
// docs/audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md lines 101-107).
//
// This gap is not a hidden accident - it is a documented, deliberate scope
// decision in the code itself: engines/wms_owner_stock.go's own header
// comment (lines 31-37, Stage 42.5.5) states "Deliberately out of scope:
// allocation/picking does not filter by owner. SalesOrder has no owner_id
// anywhere in this tree... order-level owner attribution is a future item."
// bin_stock_owner (RecordOwnerStock/ConsumeOwnerStock, same file) is a
// billing-only breakdown of bin_stock's pooled qty - confirmed by grep,
// ConsumeOwnerStock is called from nowhere in the real pick/ship path, only
// from its own file and its own test. AllocateFromStock (traceability.go:753)
// and the query it calls, allocateByOrder (traceability.go:779), read
// bin_stock directly and take no owner parameter at all - there is no way
// for any caller to ask allocation to respect an owner boundary.
//
// This test proves the mechanical consequence: seed one bin/SKU with stock
// split across two owners via the one real owner-segregation API
// (RecordOwnerStock), so that no single owner holds more than a known
// maximum - then show AllocateFromStock will still allocate MORE than any
// one owner's own recorded holding, because it never looks at
// bin_stock_owner at all. That is a demand for one owner's goods silently
// being satisfied out of a different owner's segregated stock.
//
// This test asserts the SECURE/CORRECT outcome - allocation cannot hand out
// more of a SKU/bin than any single owner's own recorded holding allows -
// which the audit (and the code's own comment) says the product does NOT
// currently deliver. See stage47_a06_phone_field_redteam_test.go for why
// this suite is build-tagged.
//
// Required closure (47.5, referenced by 47.1's audit finding but owned by
// 47.5's own item text): make owner a mandatory inventory dimension from
// receipt through allocation/pick/pack/ship, not just the billing
// approximation this file currently provides.
func TestA05AllocationIgnoresOwnerSegregationAndCrossesOwnerBoundary(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("failed to resolve tenant schema: %v", err)
	}

	binCode := NewDocID("A05BIN")
	sku := NewDocID("A05SKU")
	location := NewDocID("A05LOC")
	ownerA := NewDocID("A05OWNERA")
	ownerB := NewDocID("A05OWNERB")
	const ownerAQty = 6
	const ownerBQty = 4
	const binQty = ownerAQty + ownerBQty // 10, fully split - no unassigned remainder

	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1, $2, $3, 'Good', $4)`, schema),
		binCode, sku, location, binQty); err != nil {
		t.Fatalf("failed to seed bin_stock: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2`, schema), binCode, sku)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.bin_stock_owner WHERE bin_code = $1 AND sku = $2`, schema), binCode, sku)
	})

	if err := RecordOwnerStock(tenantID, binCode, sku, ownerA, "Good", ownerAQty, "system"); err != nil {
		t.Fatalf("failed to seed owner A's segregated stock: %v", err)
	}
	// --- the closure, asserted ------------------------------------------
	//
	// CLOSED by Stage 47.5.1 (2026-09-09) and PROMOTED out of the
	// stage47redteam build tag per 47.0.1's own closure note.
	//
	// A-05 offered two closures and the user chose the second (2026-09-09):
	// "either mixed-owner isolation is proven at every transition, or the
	// system makes the unsupported configuration impossible." The evidence for
	// choosing it was concrete - the entire development database held two
	// bin_stock_owner rows and both were this test's own fixture family, so
	// nobody was using mixed-owner 3PL at all.
	//
	// So the assertion inverts. The finding's mechanism (allocation reads
	// pooled bin_stock and has no owner parameter) is UNCHANGED and still
	// true; what changed is that a second owner can no longer get into the
	// warehouse for allocation to cross a boundary between. This asserts the
	// refusal directly, which is a stronger statement than the original
	// "allocation stayed within one owner's holding" - that could have passed
	// by luck on a quiet bin.
	err = RecordOwnerStock(tenantID, binCode, sku, ownerB, "Good", ownerBQty, "system")
	if err == nil {
		t.Fatalf("A-05: %s already holds stock for owner %s, but assigning stock to a SECOND owner (%s) in the same warehouse was accepted. "+
			"Allocation and picking take no owner parameter anywhere in this codebase (AllocateFromStock -> allocateByOrder read bin_stock directly), "+
			"so a second owner under one roof means one client's demand can be filled from another client's segregated stock. "+
			"Stage 47.5.1 makes that configuration impossible; this refusal is the closure.",
			location, ownerA, ownerB)
	}
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "INVENT-0104" {
		t.Errorf("the refusal is not a coded ValidationError an operator or an API caller can act on: %v", err)
	}
	if !strings.Contains(err.Error(), ownerA) {
		t.Errorf("the refusal does not name the owner the warehouse is already dedicated to, so the operator cannot tell what to do about it: %v", err)
	}

	// The warehouse really is dedicated - the guard is backed by stored state,
	// not by a check that happened to run.
	dedicated, derr := WarehouseOwnerOf(tenantID, location)
	if derr != nil {
		t.Fatalf("failed to read the warehouse owner back: %v", derr)
	}
	if dedicated != ownerA {
		t.Errorf("warehouse %s is dedicated to %q, want %q - the first owner-stock write must claim the warehouse so the second has something to be refused against", location, dedicated, ownerA)
	}

	// ...and nothing partial was written by the refusal.
	var ownerBRows int
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.bin_stock_owner WHERE bin_code = $1 AND owner_id = $2`, schema),
		binCode, ownerB).Scan(&ownerBRows); err != nil {
		t.Fatalf("failed to count owner B rows: %v", err)
	}
	if ownerBRows != 0 {
		t.Errorf("the refused assignment still wrote %d bin_stock_owner row(s) for %s", ownerBRows, ownerB)
	}

	// The original finding, still measurable: with only one owner present,
	// allocation cannot cross an owner boundary because there is no second
	// owner to cross to. This is deliberately NOT a claim that allocation
	// became owner-aware - it did not, and 47.5.2-6 remain the real thing if a
	// 3PL pilot ever needs it.
	const requested = 8
	allocated, _, allocErr := AllocateFromStock(tenantID, sku, location, requested)
	if allocErr != nil {
		t.Fatalf("AllocateFromStock returned an unexpected error: %v", allocErr)
	}
	totalAllocated := 0
	for _, c := range allocated {
		totalAllocated += c.Qty
	}
	owners := map[string]bool{}
	rows, qerr := db.DB.Query(fmt.Sprintf(
		`SELECT DISTINCT owner_id FROM %s.bin_stock_owner WHERE location_code = $1 AND qty > 0`, schema), location)
	if qerr != nil {
		t.Fatalf("failed to list owners at %s: %v", location, qerr)
	}
	defer rows.Close()
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err == nil {
			owners[o] = true
		}
	}
	if len(owners) > 1 {
		t.Fatalf("A-05: %s ended up holding stock for %d owners (%v) despite the single-owner guard; allocation of %d units then crossed between them unchecked",
			location, len(owners), owners, totalAllocated)
	}
}
