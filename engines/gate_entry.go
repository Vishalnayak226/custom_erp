package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
)

// Stage 146.3.8: the inward gate-entry register. A GateEntry is what the
// security desk writes when a vehicle arrives with parcels (GatePass is the
// outbound pass tied to a Manifest, so this is a new doctype). A GRN can start
// from it: the vendor, PO and transport details carry across and the entry
// links back to the receipt.
const (
	GateEntryDoctype = "GateEntry"
	// PURCHA-4966: the gate entry is unknown, already used or cancelled.
	GateEntryNotOpenCode = "PURCHA-4966"
	// PURCHA-4967: the receipt names a different vendor than the gate entry.
	GateEntryVendorCode = "PURCHA-4967"
)

func init() {
	RegisterReport(ReportDefinition{ID: "gate-entries-no-grn", Label: "Gate entries with no goods receipt", Category: "Procurement",
		Columns: []ReportColumn{{Key: "gate_entry", Label: "Gate entry"}, {Key: "vendor", Label: "Vendor"}, {Key: "po", Label: "PO"}, {Key: "location", Label: "Location"},
			{Key: "parcels", Label: "Parcels"}, {Key: "arrived", Label: "Arrived"}, {Key: "hours", Label: "Hours waiting"}},
		Params: []ReportParam{{Key: "hours", Label: "After hours", Type: "text"}},
		Run:    gateEntriesNoGRNReport})
}

func gateEntriesNoGRNReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	hours := 24
	if v := strings.TrimSpace(params["hours"]); v != "" {
		if n := int(numFromInterface(v)); n > 0 {
			hours = n
		}
	}
	return dayLimitRows(tenantID, `SELECT COALESCE(data->>'code', id), COALESCE(data->>'vendor', ''), COALESCE(data->>'po_id', ''), COALESCE(data->>'location_code', ''),
		COALESCE(NULLIF(data->>'parcels', '')::numeric, 0), to_char((created_at AT TIME ZONE current_setting('TimeZone')) AT TIME ZONE public.erp_business_tz(), 'YYYY-MM-DD HH24:MI'),
		(EXTRACT(EPOCH FROM (NOW() - created_at)) / 3600)::int
		FROM {s}.documents WHERE doctype = 'GateEntry' AND deleted_at IS NULL AND status = 'Open'
		  AND COALESCE(data->>'grn_id', '') = '' AND created_at < NOW() - make_interval(hours => $1)
		ORDER BY created_at LIMIT 20001`, []interface{}{hours},
		func(scan func(...interface{}) error) (map[string]interface{}, error) {
			var ge, vendor, po, loc, arrived string
			var parcels float64
			var h int
			if err := scan(&ge, &vendor, &po, &loc, &parcels, &arrived, &h); err != nil {
				return nil, err
			}
			return map[string]interface{}{"gate_entry": ge, "vendor": vendor, "po": po, "location": loc, "parcels": parcels, "arrived": arrived, "hours": h}, nil
		})
}

// PrepareGRNGateEntry validates the gate entry a new GRN starts from and
// fills the receipt's blank header fields from it. What was typed is kept.
func PrepareGRNGateEntry(tenantID string, isCreate bool, payload map[string]interface{}) error {
	id := strings.TrimSpace(str(payload, "gate_entry_id"))
	if id == "" || !isCreate {
		return nil
	}
	entry, status, err := fetchDocData(tenantID, GateEntryDoctype, id)
	if err != nil {
		return &ValidationError{Code: GateEntryNotOpenCode, SubFor: id, Message: fmt.Sprintf("gate entry %s was not found", id)}
	}
	if status != "Open" {
		return &ValidationError{Code: GateEntryNotOpenCode, SubFor: id, Message: fmt.Sprintf("gate entry %s is %s - only an open one can start a goods receipt", id, status)}
	}
	if v := strings.TrimSpace(str(payload, "vendor")); v != "" && strings.TrimSpace(str(entry, "vendor")) != "" && v != str(entry, "vendor") {
		return &ValidationError{Code: GateEntryVendorCode, SubFor: id, Message: fmt.Sprintf("gate entry %s is for vendor %s, not %s", id, str(entry, "vendor"), v)}
	}
	fill := func(dst, src string) {
		if strings.TrimSpace(str(payload, dst)) == "" && strings.TrimSpace(str(entry, src)) != "" {
			payload[dst] = entry[src]
		}
	}
	fill("vendor", "vendor")
	fill("po_id", "po_id")
	fill("lr_awb_no", "lr_no")
	fill("vehicle_no", "vehicle_number")
	fill("transporter", "transporter")
	fill("location", "location_code")
	if numFromInterface(payload["package_count"]) == 0 && numFromInterface(entry["parcels"]) > 0 {
		payload["package_count"] = entry["parcels"]
	}
	return nil
}

// LinkGateEntryToGRN writes the receipt back onto its gate entry and closes it.
func LinkGateEntryToGRN(tenantID, grnID, gateEntryID string) error {
	gateEntryID = strings.TrimSpace(gateEntryID)
	if gateEntryID == "" || grnID == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'GRN Made', updated_at = NOW(),
		data = jsonb_set(jsonb_set(data, '{grn_id}', to_jsonb($1::text)), '{status}', '"GRN Made"')
		WHERE doctype = 'GateEntry' AND deleted_at IS NULL AND (id = $2 OR data->>'code' = $2)`, schema), grnID, gateEntryID)
	return err
}
