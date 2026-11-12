package engines

import (
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 47.14.5: where a role lands, and which sidebar entries are not yet
// finished. The landing is stored per role (role_landing) and falls back to
// its role template's Landing, then to today's Home. The client only goes
// there when the role can open that screen (public/shell-nav.js checks the
// sidebar entry), so a stale choice never lands on a refusal.

// SettingKeyHideUnfinished hides Experimental / Preview sidebar entries from
// everyone except administrators. Off by default: they are labelled instead.
const SettingKeyHideUnfinished = "navigation.hide_unfinished"

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingKeyHideUnfinished, Module: "Navigation",
		Label: "Hide Experimental and Preview screens", Type: SettingTypeBool, Default: "false",
		Description: "Off: those entries show a Preview or Experimental label. On: only administrators see them.",
	})
}

// LandingChoice is one screen a role can land on.
type LandingChoice struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// landingChoices are view keys the shell opens as a top-level menu entry
// (menu-<key> in index.html).
var landingChoices = []LandingChoice{
	{"home", "Home"}, {"pos", "POS"}, {"oms", "Orders"}, {"returns", "Returns"},
	{"finance", "Finance"}, {"approvals", "Approvals"}, {"reports", "Reports"},
	{"inventory", "Inventory"}, {"purchase-orders", "Purchase orders"},
	{"grn", "Goods receipts"}, {"hr", "HR"}, {"crm", "CRM"},
}

// LandingChoices lists the screens a landing can be.
func LandingChoices() []LandingChoice {
	return append([]LandingChoice(nil), landingChoices...)
}

func validLanding(view string) bool {
	for _, c := range landingChoices {
		if c.Key == view {
			return true
		}
	}
	return false
}

// RoleLandings is every stored landing, by role name.
func RoleLandings(tenantID string) (map[string]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT role, view FROM %s.role_landing`, schema))
	if err != nil {
		return out, nil // a schema the migration has not reached: nobody has a landing
	}
	defer rows.Close()
	for rows.Next() {
		var role, view string
		if err := rows.Scan(&role, &view); err != nil {
			return nil, err
		}
		out[role] = view
	}
	return out, rows.Err()
}

// ResolveRoleLanding is the screen a role lands on: its stored choice, else
// its role template's, else "" (today's Home). fromTemplate says which.
func ResolveRoleLanding(tenantID, role string) (view string, fromTemplate bool) {
	if all, err := RoleLandings(tenantID); err == nil {
		for name, v := range all {
			if strings.EqualFold(name, role) && validLanding(v) {
				return v, false
			}
		}
	}
	if t, ok := RoleTemplateFor(role); ok && validLanding(t.Landing) {
		return t.Landing, true
	}
	return "", false
}

// SetRoleLanding saves a role's landing; an empty view clears it.
func SetRoleLanding(tenantID, role, view, actor string) error {
	role = strings.TrimSpace(role)
	if role == "" {
		return &ValidationError{Code: "GLOBAL-9140", Message: "Choose the role."}
	}
	view = strings.TrimSpace(view)
	if view != "" && !validLanding(view) {
		return &ValidationError{Code: "GLOBAL-9141", Message: "Choose one of the offered screens."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	// The read matches a role in any case, so the save replaces every spelling of it.
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(fmt.Sprintf(`DELETE FROM %s.role_landing WHERE lower(role) = lower($1)`, schema), role); err != nil {
		return err
	}
	if view != "" {
		if _, err = tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.role_landing (role, view, updated_by, updated_at) VALUES ($1, $2, $3, $4)
			ON CONFLICT (role) DO UPDATE SET view = EXCLUDED.view, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`, schema),
			role, view, actor, time.Now()); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "USER_MANAGEMENT", "ROLE_LANDING_CHANGED", fmt.Sprintf("Role %s lands on %q", role, view))
	return nil
}

// NavInfo is what the shell needs to decorate the sidebar and pick a landing.
type NavInfo struct {
	Landing string            `json:"landing,omitempty"`
	Hide    bool              `json:"hide,omitempty"`
	Modules map[string]string `json:"modules,omitempty"` // module key -> level, below Production only
	Menus   map[string]string `json:"menus,omitempty"`   // menu id -> level, below Production only
}

// NavForRole builds the NavInfo for one signed-in role.
func NavForRole(tenantID, role string) NavInfo {
	n := NavInfo{}
	n.Landing, _ = ResolveRoleLanding(tenantID, role)
	n.Modules, n.Menus = NavMaturity()
	n.Hide = GetSettingBool(tenantID, SettingKeyHideUnfinished) && !HasFullAccess(role)
	return n
}
