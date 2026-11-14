package engines

import (
	"fmt"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 170.42: an import for a record type that points at others (a
// purchase order needs its vendor and items) checks that they have rows. By
// default it only warns; with imports.require_load_order on it refuses until
// the others are loaded. The order shown on the import screen is the same
// list: load these first.

// SettingImportRequireLoadOrder is the switch. Off: a warning only.
const SettingImportRequireLoadOrder = "imports.require_load_order"

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingImportRequireLoadOrder, Module: "Imports",
		Label: "Refuse an import until what it points at is loaded", Type: SettingTypeBool, Default: "false",
		Description: "Stage 170.42: an import for a record type that needs others (vendors, items, customers) checks that they have rows. Off: it warns and carries on. On: it refuses until they are loaded.",
	})
}

// LoadOrderStep is one record type an import needs first.
type LoadOrderStep struct {
	Doctype string `json:"doctype"`
	Field   string `json:"field"`
	Rows    int    `json:"rows"`
	Loaded  bool   `json:"loaded"`
}

// loadOrderIgnored are link targets that are not document rows.
var loadOrderIgnored = map[string]bool{"User": true, "Role": true, "Tenant": true}

// ImportLoadOrder lists the record types doctype's mandatory link fields
// point at, each with its row count, in the order the fields are shown.
func ImportLoadOrder(tenantID, doctype string) ([]LoadOrderStep, error) {
	fields, err := GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT name FROM %s.doctype_meta`, schema))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			known[n] = true
		}
	}
	rows.Close()

	sort.SliceStable(fields, func(i, j int) bool { return fields[i].DisplayOrder < fields[j].DisplayOrder })
	seen := map[string]bool{}
	out := []LoadOrderStep{}
	for _, f := range fields {
		target := strings.TrimSpace(f.Options)
		if f.Fieldtype != "Link" || !f.Mandatory || target == "" || target == doctype || seen[target] ||
			loadOrderIgnored[target] || !known[target] {
			continue
		}
		seen[target] = true
		var n int
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL`, schema), target).Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, LoadOrderStep{Doctype: target, Field: f.Fieldname, Rows: n, Loaded: n > 0})
	}
	return out, nil
}

// CheckImportLoadOrder returns one warning per missing record type, or
// refuses (DATAIM-1700) when the setting is on.
func CheckImportLoadOrder(tenantID, doctype string) ([]string, error) {
	steps, err := ImportLoadOrder(tenantID, doctype)
	if err != nil {
		return nil, nil // a lookup problem never blocks an import
	}
	var missing []string
	for _, s := range steps {
		if !s.Loaded {
			missing = append(missing, s.Doctype)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}
	msg := fmt.Sprintf("Load %s first: no rows yet.", strings.Join(missing, ", "))
	if GetSettingBool(tenantID, SettingImportRequireLoadOrder) {
		return nil, &ValidationError{Code: "DATAIM-1700", Message: msg}
	}
	return []string{msg}, nil
}
