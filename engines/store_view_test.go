package engines

import (
	"custom_erp/db"
	"reflect"
	"testing"
)

// Stage 106.6: who gets "All stores", when lists follow the working store,
// and what a following list matches per doctype.
func TestStage1066StoreView(t *testing.T) {
	admin := LiveUserState{Role: "Admin", Access: StoreAccess{All: true}}
	if !admin.CanViewAllStores() || admin.ListsFollowWorkingStore() {
		t.Errorf("an admin starts on All stores")
	}
	admin.StoreView = StoreViewStore
	if admin.ListsFollowWorkingStore() != true {
		t.Errorf("an admin who picked a store is narrowed to it")
	}
	manager := LiveUserState{Role: RoleStoreManager, Access: StoreAccess{All: true}}
	if manager.CanViewAllStores() || !manager.ListsFollowWorkingStore() {
		t.Errorf("a role that reads only its store's documents is always narrowed, even with every store given")
	}

	db.InitDB(testConnStr())
	for doctype, want := range map[string][]string{
		"TransferOrder": {"from_warehouse", "to_warehouse"},
		"PurchaseOrder": {"location", "target_warehouse"},
		"GRN":           {"location"},
		"Vendor":        nil, // a master never narrows
		"Location":      nil,
	} {
		got, _ := StoreViewListFields("default", doctype)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: want %v, got %v", doctype, want, got)
		}
	}
	if _, allow := StoreViewListFields("default", "PurchaseReturn"); !allow {
		t.Errorf("a purchase return with no store stays listed (its store is optional)")
	}
}
