package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
)

// Stage 47.5.1 - the single-owner guard (audit finding A-05).
//
// A-05's own test (stage47_a05_owner_allocation_redteam_test.go) proves the
// refusal from the finding's side. This proves the guard's own behaviour:
// that it adopts an unowned warehouse rather than blocking a tenant who has
// never thought about ownership, that it can be turned off deliberately, and
// that it reports pre-existing violations instead of pretending they are not
// there.
func TestSingleOwnerWarehouseGuard(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("failed to resolve tenant schema: %v", err)
	}

	bin := NewDocID("SOBIN")
	sku := NewDocID("SOSKU")
	loc := NewDocID("SOLOC")
	ownerA := NewDocID("SOOWNERA")
	ownerB := NewDocID("SOOWNERB")

	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.bin_stock (bin_code, sku, condition, location_code, qty) VALUES ($1, $2, 'Good', $3, 20)`, schema),
		bin, sku, loc); err != nil {
		t.Fatalf("failed to seed bin_stock: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.bin_stock WHERE bin_code = $1`, schema), bin)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.bin_stock_owner WHERE bin_code = $1`, schema), bin)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.warehouse_owner WHERE location_code = $1`, schema), loc)
	})

	// An unassigned warehouse adopts its first owner. A tenant that has never
	// configured ownership must keep working exactly as before - the rule
	// blocks the SECOND owner, not the first.
	if err := RecordOwnerStock(tenantID, bin, sku, ownerA, "Good", 5, "system"); err != nil {
		t.Fatalf("the first owner-stock assignment into an unassigned warehouse was refused: %v", err)
	}
	if got, _ := WarehouseOwnerOf(tenantID, loc); got != ownerA {
		t.Errorf("warehouse %s is dedicated to %q after the first assignment, want %q", loc, got, ownerA)
	}

	// More stock for the SAME owner is fine - the rule is about owners, not
	// about how many times one owner puts stock away.
	if err := RecordOwnerStock(tenantID, bin, sku, ownerA, "Good", 3, "system"); err != nil {
		t.Errorf("a second assignment for the SAME owner was refused: %v", err)
	}

	// A different owner is refused.
	if err := RecordOwnerStock(tenantID, bin, sku, ownerB, "Good", 2, "system"); err == nil {
		t.Error("a second OWNER was accepted into a dedicated warehouse")
	}

	// Reassigning the warehouse while the incumbent still holds stock is
	// refused too - otherwise the guard is cosmetic, since an operator could
	// simply rename the owner and carry on mixing.
	if err := AssignWarehouseOwner(tenantID, loc, ownerB, "tester", "attempted takeover"); err == nil {
		t.Error("the warehouse was reassigned while the previous owner still held stock in it")
	}

	// The unsupported mode exists and really does turn the guard off, so a
	// 3PL pilot can opt in knowingly rather than patching the code.
	if err := SetSetting(tenantID, StockOwnershipModeSetting, OwnershipMixedUnsupported, "test"); err != nil {
		t.Fatalf("failed to switch ownership mode: %v", err)
	}
	t.Cleanup(func() {
		_ = SetSetting(tenantID, StockOwnershipModeSetting, OwnershipSingleOwner, "test cleanup")
	})
	if SingleOwnerEnforced(tenantID) {
		t.Error("the guard still reports itself enforced after being switched to the unsupported mode")
	}
	if err := RecordOwnerStock(tenantID, bin, sku, ownerB, "Good", 2, "system"); err != nil {
		t.Errorf("the unsupported mixed mode still refused a second owner: %v", err)
	}

	// ...and having done so, the location is reported as mixed. An operator
	// who opts into the unsupported mode must be able to see exactly which
	// warehouses are in that state.
	mixed, err := ListMixedOwnerLocations(tenantID)
	if err != nil {
		t.Fatalf("failed to list mixed-owner locations: %v", err)
	}
	found := false
	for _, m := range mixed {
		if m.LocationCode == loc {
			found = true
			if m.OwnerCount != 2 {
				t.Errorf("%s reports %d owners, want 2", loc, m.OwnerCount)
			}
		}
	}
	if !found {
		t.Errorf("%s holds two owners' stock but is not reported by ListMixedOwnerLocations; a violation nobody can see is a violation nobody will fix", loc)
	}
}
