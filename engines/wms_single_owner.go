package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
)

// Stage 47.5.1 - enforced single-owner-per-warehouse (audit finding A-05).
//
// A-05's finding, in the code's own words (engines/wms_owner_stock.go's
// header): "allocation/picking does not filter by owner ... there is no way
// for any caller to ask allocation to respect an owner boundary."
// `bin_stock_owner` is a billing breakdown; `AllocateFromStock` reads
// `bin_stock` and never consults it. So a demand for one 3PL client's goods
// could be satisfied out of another client's segregated stock, with nothing
// to notice.
//
// The item offered two closures and the user chose the second (2026-09-09):
// make the unsupported configuration impossible rather than build isolation
// nobody uses. The evidence for that choice was concrete - the entire
// development database held two `bin_stock_owner` rows and both were the
// A-05 red-team fixture.
//
// So: a warehouse has at most one owner, enforced here and structurally in the
// database (`warehouse_owner.location_code` is a PRIMARY KEY, so a second
// owner for a location cannot be stored). With one owner in the building there
// is no cross-owner boundary left for allocation to violate.
//
// WHAT THIS DELIBERATELY DOES NOT CLAIM. It is not mixed-owner isolation and
// must never be described as such. `AllocateFromStock` still takes no owner
// parameter; nothing here changes picking. The claim being removed from the
// Production documentation is exactly the one this cannot support. 47.5.2-6
// remain the real thing, if a 3PL pilot ever needs it.

// StockOwnershipModeSetting selects how strictly ownership is enforced.
const StockOwnershipModeSetting = "wms.stock_ownership_mode"

const (
	// OwnershipSingleOwner is the default and the supported configuration: a
	// warehouse holds one owner's stock, so there is no boundary to breach.
	OwnershipSingleOwner = "single_owner"
	// OwnershipMixedUnsupported permits more than one owner per warehouse.
	// It exists so a tenant piloting a real 3PL arrangement can opt in with
	// their eyes open - NOT as a supported mode. Allocation and picking are
	// owner-blind, so choosing this means accepting that one client's demand
	// can be filled from another client's stock. ListMixedOwnerLocations
	// reports every location in that state.
	OwnershipMixedUnsupported = "mixed_unsupported"
)

// SingleOwnerEnforced reports whether the tenant is running the supported,
// guarded configuration.
func SingleOwnerEnforced(tenantID string) bool {
	return GetSettingString(tenantID, StockOwnershipModeSetting) != OwnershipMixedUnsupported
}

// WarehouseOwnerOf returns the owner a location is dedicated to, or "" when it
// has not been assigned one.
func WarehouseOwnerOf(tenantID, locationCode string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var owner string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT owner_id FROM %s.warehouse_owner WHERE location_code = $1`, schema),
		locationCode).Scan(&owner)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return owner, err
}

// AssignWarehouseOwner dedicates a location to one owner.
//
// Reassignment is refused while the location still holds another owner's
// stock: a warehouse whose owner changes on paper while the previous client's
// goods are still on the racks is precisely the mixed state this stage exists
// to prevent, and letting it through would make the guard cosmetic.
func AssignWarehouseOwner(tenantID, locationCode, ownerID, assignedBy, note string) error {
	locationCode = strings.TrimSpace(locationCode)
	ownerID = strings.TrimSpace(ownerID)
	if locationCode == "" || ownerID == "" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "owner_id",
			Message: "a location code and an owner are both required to dedicate a warehouse"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	var otherOwners int
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.bin_stock_owner
		 WHERE location_code = $1 AND owner_id <> $2 AND qty > 0`, schema),
		locationCode, ownerID).Scan(&otherOwners); err != nil {
		return err
	}
	if otherOwners > 0 {
		return &ValidationError{Code: "INVENT-0104", SubFor: "",
			Message: fmt.Sprintf("%s still holds stock for %d other owner slice(s); move or re-attribute that stock before dedicating the warehouse to %s", locationCode, otherOwners, ownerID)}
	}

	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.warehouse_owner (location_code, owner_id, assigned_by, note)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (location_code) DO UPDATE
		   SET owner_id = EXCLUDED.owner_id, assigned_by = EXCLUDED.assigned_by,
		       note = EXCLUDED.note, assigned_at = CURRENT_TIMESTAMP`, schema),
		locationCode, ownerID, assignedBy, note)
	if err == nil {
		LogAuditEvent(tenantID, assignedBy, "WAREHOUSE_OWNER_ASSIGNED", "Success",
			fmt.Sprintf("%s dedicated to owner %s. %s", locationCode, ownerID, note))
	}
	return err
}

// AssertSingleOwnerForLocation is the guard every owner-stock write passes
// through. It is called from RecordOwnerStock - the one API in this codebase
// that can create a second owner in a warehouse - rather than being repeated
// at each call site, for the same "attach it at the one shared choke point"
// reason the rest of this codebase follows.
//
// An unassigned location adopts the first owner written to it. That keeps a
// tenant that has never thought about ownership working exactly as before
// (their first write simply defines the warehouse), while still making the
// SECOND owner impossible - which is the whole rule.
func AssertSingleOwnerForLocation(tenantID, locationCode, ownerID string) error {
	if !SingleOwnerEnforced(tenantID) {
		return nil
	}
	locationCode = strings.TrimSpace(locationCode)
	ownerID = strings.TrimSpace(ownerID)
	if locationCode == "" || ownerID == "" {
		return nil // nothing to compare; the caller's own validation applies
	}

	existing, err := WarehouseOwnerOf(tenantID, locationCode)
	if err != nil {
		return err
	}
	if existing == "" {
		// First owner into an unassigned warehouse claims it. Recorded rather
		// than merely permitted, so the next write has something to check
		// against and an operator can see why the warehouse is dedicated.
		return AssignWarehouseOwner(tenantID, locationCode, ownerID, "system",
			"Auto-dedicated on first owner-stock assignment (Stage 47.5.1 single-owner rule).")
	}
	if existing != ownerID {
		return &ValidationError{Code: "INVENT-0104", SubFor: "",
			Message: fmt.Sprintf("%s is dedicated to owner %s, so stock cannot be assigned to %s there. This build supports one owner per warehouse: allocation and picking are owner-blind, so mixing owners in one building would let one client's order be filled from another client's stock. Use a separate location for %s.",
				locationCode, existing, ownerID, ownerID)}
	}
	return nil
}

// MixedOwnerLocation is one warehouse that violates the single-owner rule -
// data that predates it, or a tenant running the unsupported mixed mode.
type MixedOwnerLocation struct {
	LocationCode string   `json:"location_code"`
	OwnerCount   int      `json:"owner_count"`
	Owners       []string `json:"owners"`
	TotalQty     int      `json:"total_qty"`
}

// ListMixedOwnerLocations reports every location holding more than one owner's
// stock.
//
// This exists because the migration deliberately does not "fix" pre-existing
// mixed data: aborting would block every later migration on such a database,
// and silently picking a winner would rewrite somebody's stock ownership
// without being asked. Naming them is the only honest option, and this is how
// an operator finds the list to resolve.
func ListMixedOwnerLocations(tenantID string) ([]MixedOwnerLocation, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT location_code, COUNT(DISTINCT owner_id), SUM(qty),
		       STRING_AGG(DISTINCT owner_id, ',' ORDER BY owner_id)
		  FROM %s.bin_stock_owner
		 WHERE COALESCE(location_code, '') <> '' AND qty > 0
		 GROUP BY location_code
		HAVING COUNT(DISTINCT owner_id) > 1
		 ORDER BY location_code`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MixedOwnerLocation{}
	for rows.Next() {
		var m MixedOwnerLocation
		var owners string
		if err := rows.Scan(&m.LocationCode, &m.OwnerCount, &m.TotalQty, &owners); err != nil {
			return nil, err
		}
		m.Owners = strings.Split(owners, ",")
		out = append(out, m)
	}
	return out, rows.Err()
}
