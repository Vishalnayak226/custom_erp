package engines

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Stage 85.A (user, 2026-10-18): "User wise Admin will give the store
// allocation. First role than user ... In User, There will a section to
// allocate store. Dynamically it should pull to user if I add a new store."
//
// A role carries a store access (All / Selected / Home); a user inherits it,
// with extra stores of their own, or overrides it. "All" is a rule, not a
// copied list, so a store added tomorrow is allowed without an edit. Super
// Admin always has every store. users.location_code stays the home store.
//
// The working store is the token's "loc" claim. apiMiddleware keeps it only
// while StoreAccess still allows it (LiveUserState.WorkingStore), so taking a
// store away from a user ends their work there within the live-state TTL.

const (
	StoreAccessInherit  = "Inherit"
	StoreAccessAll      = "All"
	StoreAccessSelected = "Selected"
	StoreAccessHome     = "Home"
)

// StoreAccess is the set of stores one user may work at.
type StoreAccess struct {
	All    bool
	Home   string
	Stores map[string]bool // always holds Home; not consulted when All
}

// Allows reports whether the user may work at store.
func (a StoreAccess) Allows(store string) bool {
	store = strings.TrimSpace(store)
	if store == "" {
		return false
	}
	return a.All || store == a.Home || a.Stores[store]
}

// List is the explicit store set, sorted (empty meaning "every store" when All).
func (a StoreAccess) List() []string {
	out := make([]string, 0, len(a.Stores))
	for s := range a.Stores {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// combineStoreAccess is the one place the role-then-user rule lives.
func combineStoreAccess(role, home, userMode string, userStores []string, roleMode string, roleStores []string) StoreAccess {
	a := StoreAccess{Home: strings.TrimSpace(home), Stores: map[string]bool{}}
	if a.Home != "" {
		a.Stores[a.Home] = true
	}
	if IsSuperAdmin(role) {
		a.All = true
		return a
	}
	add := func(list []string) {
		for _, s := range list {
			if s = strings.TrimSpace(s); s != "" {
				a.Stores[s] = true
			}
		}
	}
	switch userMode {
	case StoreAccessAll:
		a.All = true
	case StoreAccessHome:
	case StoreAccessSelected:
		add(userStores)
	default: // Inherit: the role decides, the user's own stores add to it.
		switch roleMode {
		case StoreAccessAll:
			a.All = true
		case StoreAccessSelected:
			add(roleStores)
		}
		add(userStores)
	}
	return a
}

// loadStoreAccess reads the user's and the role's settings. It is tolerant of
// a schema the Stage 85 migration has not reached (no columns, no table):
// the answer is then "home store only", which narrows rather than widens.
func loadStoreAccess(schema, role, home string, userMode string, userStoresJSON string) StoreAccess {
	var userStores []string
	_ = json.Unmarshal([]byte(userStoresJSON), &userStores)
	roleMode, roleStores := StoreAccessHome, []string{}
	var stores pq.StringArray
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT access, stores FROM %s.role_store_access WHERE lower(role) = lower($1)`, schema), role).Scan(&roleMode, &stores)
	if err == nil {
		roleStores = stores
	} else {
		roleMode = StoreAccessHome
	}
	return combineStoreAccess(role, home, userMode, userStores, roleMode, roleStores)
}

// WorkingStore is the store this request works at: the token's claimed store
// while the user may still work there, otherwise the home store.
func (s LiveUserState) WorkingStore(claimed string) string {
	claimed = strings.TrimSpace(claimed)
	if claimed != "" && claimed != s.LocationCode && s.Access.Allows(claimed) {
		return claimed
	}
	return s.LocationCode
}

// StoreOption is one store a user may pick at the top of the screen.
type StoreOption struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Sellable bool   `json:"sellable"`
}

// ListStores is every active Location, by name. It is what "All stores"
// means, read live.
func ListStores(tenantID string) ([]StoreOption, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(NULLIF(data->>'name', ''), id), COALESCE(data->>'type', ''), COALESCE(data->>'sellable', '')
		  FROM %s.documents
		 WHERE doctype = 'Location' AND deleted_at IS NULL
		   AND COALESCE(data->>'status', 'Active') <> 'Inactive'
		 ORDER BY lower(COALESCE(NULLIF(data->>'name', ''), id))`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoreOption{}
	for rows.Next() {
		var o StoreOption
		var sellable string
		if err := rows.Scan(&o.Code, &o.Name, &o.Type, &sellable); err != nil {
			return nil, err
		}
		o.Sellable = sellable == "Yes" || (sellable == "" && o.Type == "Store")
		out = append(out, o)
	}
	return out, rows.Err()
}

// StoresFor is the stores this user may pick, by name. A home store that is
// not (or no longer) a Location is still listed, under its code, so the user
// is never shown an empty list.
func StoresFor(tenantID string, access StoreAccess) ([]StoreOption, error) {
	all, err := ListStores(tenantID)
	if err != nil {
		return nil, err
	}
	out := []StoreOption{}
	seenHome := false
	for _, o := range all {
		if access.Allows(o.Code) {
			out = append(out, o)
			if o.Code == access.Home {
				seenHome = true
			}
		}
	}
	if !seenHome && access.Home != "" {
		out = append([]StoreOption{{Code: access.Home, Name: access.Home}}, out...)
	}
	return out, nil
}

// ErrStoreNotAllowed is the coded refusal for working at a store the user has
// not been given.
func storeNotAllowedError(store string) error {
	return &ValidationError{Code: "STORE-0850", Message: fmt.Sprintf("%s is not one of your stores.", store)}
}

// ValidateUserMayWorkAt refuses a store the user may not work at. Callers
// pass the live state apiMiddleware resolved, so this costs no query.
func ValidateUserMayWorkAt(access StoreAccess, store string) error {
	if strings.TrimSpace(store) == "" || access.Allows(store) {
		return nil
	}
	return storeNotAllowedError(store)
}

// UserStoreAccess resolves a user's store access from the live state.
func UserStoreAccess(tenantID, userID string) (StoreAccess, error) {
	live, err := ResolveLiveUserState(tenantID, userID)
	if err != nil {
		return StoreAccess{}, err
	}
	return live.Access, nil
}

func normaliseStoreMode(mode string, allowInherit bool) (string, error) {
	for _, m := range []string{StoreAccessAll, StoreAccessSelected, StoreAccessHome, StoreAccessInherit} {
		if strings.EqualFold(strings.TrimSpace(mode), m) {
			if m == StoreAccessInherit && !allowInherit {
				break
			}
			return m, nil
		}
	}
	return "", &ValidationError{Code: "STORE-0851", Message: "Store access must be All stores, Selected stores or Home store only" + map[bool]string{true: " (or Same as role)", false: ""}[allowInherit] + "."}
}

// cleanStoreList trims, de-duplicates and checks that every store exists.
func cleanStoreList(schema string, stores []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range stores {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return out, nil
	}
	var known pq.StringArray
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(array_agg(id), '{}') FROM %s.documents WHERE doctype = 'Location' AND deleted_at IS NULL AND id = ANY($1)`, schema),
		pq.Array(out)).Scan(&known); err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, k := range known {
		have[k] = true
	}
	for _, s := range out {
		if !have[s] {
			return nil, &ValidationError{Code: "STORE-0852", Message: fmt.Sprintf("%s is not a store in Setup -> Location.", s)}
		}
	}
	return out, nil
}

// RoleStoreSetting is what the Roles screen shows and saves.
type RoleStoreSetting struct {
	Role   string   `json:"role"`
	Access string   `json:"access"`
	Stores []string `json:"stores"`
}

// GetRoleStoreAccess returns the role's setting; a role never set is Home.
func GetRoleStoreAccess(tenantID, role string) (RoleStoreSetting, error) {
	out := RoleStoreSetting{Role: role, Access: StoreAccessHome, Stores: []string{}}
	if IsSuperAdmin(role) {
		out.Access = StoreAccessAll
		return out, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	var stores pq.StringArray
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT access, stores FROM %s.role_store_access WHERE lower(role) = lower($1)`, schema), role).Scan(&out.Access, &stores)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Stores = append([]string{}, stores...)
	return out, nil
}

// ListRoleStoreAccess is every role that has a store setting, by role name.
// Roles not listed are Home store only.
func ListRoleStoreAccess(tenantID string) (map[string]RoleStoreSetting, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := map[string]RoleStoreSetting{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT role, access, stores FROM %s.role_store_access`, schema))
	if err != nil {
		return out, nil // a schema the migration has not reached: every role is Home
	}
	defer rows.Close()
	for rows.Next() {
		var s RoleStoreSetting
		var stores pq.StringArray
		if err := rows.Scan(&s.Role, &s.Access, &stores); err != nil {
			return nil, err
		}
		s.Stores = append([]string{}, stores...)
		out[s.Role] = s
	}
	return out, rows.Err()
}

// SetRoleStoreAccess saves a role's store access. Super Admin is fixed at All.
func SetRoleStoreAccess(tenantID, role, mode string, stores []string, actor string) error {
	role = strings.TrimSpace(role)
	if role == "" {
		return &ValidationError{Code: "STORE-0851", Message: "Choose the role."}
	}
	if IsSuperAdmin(role) {
		return &ValidationError{Code: "STORE-0853", Message: "Super Admin always works at every store; it cannot be narrowed."}
	}
	mode, err := normaliseStoreMode(mode, false)
	if err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if mode != StoreAccessSelected {
		stores = nil
	}
	clean, err := cleanStoreList(schema, stores)
	if err != nil {
		return err
	}
	if mode == StoreAccessSelected && len(clean) == 0 {
		return &ValidationError{Code: "STORE-0851", Message: "Tick at least one store, or choose Home store only."}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.role_store_access (role, access, stores, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (role) DO UPDATE SET access = EXCLUDED.access, stores = EXCLUDED.stores,
		                                 updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`, schema),
		role, mode, pq.Array(clean), actor, time.Now()); err != nil {
		return err
	}
	// Every holder of the role is affected, so the whole tenant's cached
	// live state goes (the next request re-reads it).
	InvalidateLiveUserStatesForTenant(tenantID)
	LogAuditEvent(tenantID, actor, "USER_MANAGEMENT", "ROLE_STORES_CHANGED", fmt.Sprintf("Role %s store access set to %s %v", role, mode, clean))
	return nil
}

// UserStoreSetting is what the Users screen shows and saves.
type UserStoreSetting struct {
	UserID string   `json:"id"`
	Home   string   `json:"home"`
	Access string   `json:"access"`
	Stores []string `json:"stores"`
	// Role's setting, so the screen can say what "Same as role" means.
	Role       string   `json:"role"`
	RoleAccess string   `json:"role_access"`
	RoleStores []string `json:"role_stores"`
}

// GetUserStoreAccess returns the user's own setting plus their role's.
func GetUserStoreAccess(tenantID, userID string) (UserStoreSetting, error) {
	out := UserStoreSetting{UserID: userID, Access: StoreAccessInherit, Stores: []string{}}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	var stores pq.StringArray
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT role, location_code, store_access, stores FROM %s.users WHERE id = $1`, schema), userID).
		Scan(&out.Role, &out.Home, &out.Access, &stores); err != nil {
		return out, err
	}
	out.Stores = append([]string{}, stores...)
	roleSetting, err := GetRoleStoreAccess(tenantID, out.Role)
	if err != nil {
		return out, err
	}
	out.RoleAccess, out.RoleStores = roleSetting.Access, roleSetting.Stores
	return out, nil
}

// SetUserStoreAccess saves a user's own store access (the home store is set
// through the existing Set Location path).
func SetUserStoreAccess(tenantID, userID, mode string, stores []string, actor string) error {
	mode, err := normaliseStoreMode(mode, true)
	if err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if mode == StoreAccessAll || mode == StoreAccessHome {
		stores = nil
	}
	clean, err := cleanStoreList(schema, stores)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET store_access = $1, stores = $2 WHERE id = $3`, schema),
		mode, pq.Array(clean), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &ValidationError{Code: "STORE-0851", Message: "No such user."}
	}
	InvalidateLiveUserState(tenantID, userID)
	LogAuditEvent(tenantID, actor, "USER_MANAGEMENT", "USER_STORES_CHANGED", fmt.Sprintf("User %s store access set to %s %v", userID, mode, clean))
	return nil
}

// openTillAt names the open POS session this cashier holds at store, if any.
func openTillAt(schema, cashier, store string) string {
	var id string
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		 WHERE doctype = 'POSSession' AND status = 'Open' AND deleted_at IS NULL
		   AND data->>'cashier' = $1 AND data->>'location' = $2
		 LIMIT 1`, schema), cashier, store).Scan(&id)
	return id
}

// SwitchWorkingStore checks a switch from the current store to target and
// returns the target's code. The caller mints the new session token.
func SwitchWorkingStore(tenantID, userID, cashier, current, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", &ValidationError{Code: "STORE-0851", Message: "Choose the store to work at."}
	}
	live, err := ResolveLiveUserState(tenantID, userID)
	if err != nil {
		return "", err
	}
	if err := ValidateUserMayWorkAt(live.Access, target); err != nil {
		return "", err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if target != live.LocationCode {
		if _, err := cleanStoreList(schema, []string{target}); err != nil {
			return "", err
		}
	}
	if target != current {
		if till := openTillAt(schema, cashier, current); till != "" {
			return "", &ValidationError{Code: "STORE-0854", Message: fmt.Sprintf("Session %s is open at %s.", till, current)}
		}
	}
	if target != current {
		LogAuditEvent(tenantID, cashier, "USER_MANAGEMENT", "WORKING_STORE_SWITCHED", fmt.Sprintf("Switched working store from %s to %s", current, target))
	}
	return target, nil
}
