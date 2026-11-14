package engines

import (
	"strings"
	"testing"

	"custom_erp/db"
)

// A record type with a mandatory link to an empty type warns by default and
// refuses once the setting is on.
func TestImportLoadOrderWarnsThenRefuses(t *testing.T) {
	tenant, schema := retentionTestSetup(t)
	var doctype, target string
	rows, err := db.DB.Query(`SELECT f.doctype_name, f.options FROM ` + schema + `.doctype_fields f JOIN ` + schema + `.doctype_meta m ON m.name = f.options
		WHERE f.fieldtype = 'Link' AND f.mandatory AND f.options NOT IN ('User','Role','Tenant') AND f.options <> f.doctype_name
		AND NOT EXISTS (SELECT 1 FROM ` + schema + `.documents d WHERE d.doctype = f.options AND d.deleted_at IS NULL) LIMIT 1`)
	if err == nil {
		if rows.Next() {
			rows.Scan(&doctype, &target)
		}
		rows.Close()
	}
	if doctype == "" {
		t.Skip("no record type with a mandatory link to an empty type in this database")
	}
	defer SetSetting(tenant, SettingImportRequireLoadOrder, "false", "test")
	SetSetting(tenant, SettingImportRequireLoadOrder, "false", "test")
	warns, err := CheckImportLoadOrder(tenant, doctype)
	if err != nil || len(warns) == 0 || !strings.Contains(warns[0], target) {
		t.Fatalf("default: warns=%v err=%v, want a warning naming %s", warns, err, target)
	}
	SetSetting(tenant, SettingImportRequireLoadOrder, "true", "test")
	_, err = CheckImportLoadOrder(tenant, doctype)
	wantValidationCode(t, err, "DATAIM-1700")
}
