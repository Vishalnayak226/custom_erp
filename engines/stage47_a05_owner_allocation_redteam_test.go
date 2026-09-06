//go:build stage47redteam

package engines

import (
	"custom_erp/db"
	"fmt"
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
	if err := RecordOwnerStock(tenantID, binCode, sku, ownerB, "Good", ownerBQty, "system"); err != nil {
		t.Fatalf("failed to seed owner B's segregated stock: %v", err)
	}

	// No single owner holds more than max(ownerAQty, ownerBQty) = 6. Request
	// 8 - more than either owner's own share, only satisfiable at all by
	// drawing from both owners' segregated stock at once.
	const requested = 8
	const maxSingleOwnerHolding = ownerAQty // the larger of the two shares
	allocated, shortfall, err := AllocateFromStock(tenantID, sku, location, requested)
	if err != nil {
		t.Fatalf("AllocateFromStock returned an unexpected error: %v", err)
	}
	totalAllocated := 0
	for _, c := range allocated {
		totalAllocated += c.Qty
	}

	if totalAllocated > maxSingleOwnerHolding {
		t.Fatalf("A-05: requested %d units of SKU %q from bin %q, which no single owner holds (owner A=%d, owner B=%d) - AllocateFromStock allocated %d (shortfall %d) anyway, silently crossing the owner boundary recorded in bin_stock_owner because allocation reads only the pooled bin_stock table and has no owner parameter at all. Must not allocate past a single owner's own recorded holding once 47.5 makes owner a real inventory dimension through allocation/pick, not just wms_owner_stock.go's billing-only breakdown.",
			requested, sku, binCode, ownerAQty, ownerBQty, totalAllocated, shortfall)
	}
}
