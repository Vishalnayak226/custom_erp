package engines

import (
	"fmt"

	"custom_erp/db"
)

// Stage 106.6 (user, 2026-10-19, store Jayanagar picked and Purchase Orders
// still listing a PO billed to HO): "Once I switch Store. It will only show
// txn of that store. But if I want to access other store bill with Phone
// number for return that is finee." Decided the same day: a user who sees
// every store's documents gets "All stores" at the top of the store picker
// (everything, as before); picking a store narrows every transaction list to
// it. A user whose role sees only the working store's documents is narrowed
// always. Only lists narrow - opening a document, the return bill lookup by
// phone and reports keep their own rules.

const (
	// StoreViewAll in the switcher (POST /me/working-store) is "All stores".
	StoreViewAll = "*"
	// StoreViewStore is users.store_view when lists follow the working store.
	StoreViewStore = "store"
)

// CanViewAllStores: the user may work at every store and their role reads
// every store's documents, so "All stores" means something for them.
func (s LiveUserState) CanViewAllStores() bool {
	return s.Access.All && SessionScope{Role: s.Role}.HasGlobalScope(ScopeLocation, "")
}

// ListsFollowWorkingStore: transaction lists show the working store's
// documents only - always for a user who cannot view all stores, and for one
// who can once they picked a store in the top bar.
func (s LiveUserState) ListsFollowWorkingStore() bool {
	return !s.CanViewAllStores() || s.StoreView == StoreViewStore
}

// storeViewEitherFields are transactions with two stores: a list following
// the working store shows the document at either one - a transfer at the
// sending and the receiving store, a PO at its billing and delivery store.
var storeViewEitherFields = map[string][]string{
	"TransferOrder": {"from_warehouse", "to_warehouse"},
	"PurchaseOrder": {"location", "target_warehouse"},
	"ReturnRequest": {"return_location"},
}

// StoreViewListFields is what a list following the working store matches:
// any of fields equal to the store, a row with none of them set kept only
// when allowMissing. No fields: the list does not narrow (a master, or a
// record with no store).
func StoreViewListFields(tenantID, doctype string) (fields []string, allowMissing bool) {
	if f, ok := storeViewEitherFields[doctype]; ok {
		return f, false
	}
	if master, err := IsMasterDoctype(tenantID, doctype); err != nil || master {
		return nil, false
	}
	if s, ok := ScopeForDoctype(doctype); ok && len(s.LocationFields) > 0 {
		return s.LocationFields, !s.LocationMandatory
	}
	return nil, false
}

// setStoreView remembers the top bar's list choice for the user.
func setStoreView(tenantID, userID, view string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET store_view = $1 WHERE id = $2`, schema), view, userID); err != nil {
		return err
	}
	InvalidateLiveUserState(tenantID, userID)
	return nil
}
