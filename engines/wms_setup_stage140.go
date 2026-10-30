package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
)

// EnsureBinZone (140.12.21) makes a bin's zone on the Zone master when it does
// not exist yet, so a bin is not refused (META-0198) only because its zone was
// typed before the zone was set up. Called by the record save for a person
// who may create zones; an import still needs the zone first, so a preview
// never writes. The zone takes the bin's location and is Active.
func EnsureBinZone(tenantID, user string, payload map[string]interface{}) {
	zone := strings.TrimSpace(strField(payload, "zone"))
	if zone == "" {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	var exists bool
	if db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Zone' AND data->>'code' = $1 AND deleted_at IS NULL)`, schema), zone).Scan(&exists) != nil || exists {
		return
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": zone, "name": zone, "location": strings.TrimSpace(strField(payload, "location")), "status": "Active"})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Zone', $2, 'Active', $3) ON CONFLICT DO NOTHING`, schema),
		NewDocID("ZONE"), raw, user); err != nil {
		return
	}
	LogAuditEvent(tenantID, user, "WMS", "ZONE_CREATED_WITH_BIN", fmt.Sprintf("zone %s made for a new bin at %s", zone, strField(payload, "location")))
}
