package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func gateEntryTestTenant(t *testing.T) (tenantID, schema string, cleanup func(), insert func(id, doctype, status string, data map[string]interface{})) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema = uniqueLifecycleTenant(t)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert = func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	return tenantID, schema, func() { dropLifecycleTenant(tenantID, schema) }, insert
}

// Stage 146.3.8: the doctype, its fields, its number series and photo attachments exist.
func TestStage14638GateEntryIsRegistered(t *testing.T) {
	tenantID, schema, cleanup, _ := gateEntryTestTenant(t)
	defer cleanup()
	fields, err := GetDocTypeMeta(tenantID, "GateEntry")
	if err != nil || len(fields) < 10 {
		t.Fatalf("GateEntry fields: %v (%d)", err, len(fields))
	}
	if !DoctypeIsTransaction(tenantID, "GateEntry") {
		t.Errorf("GateEntry is a transaction")
	}
	var n int
	if err := db.DB.QueryRow("SELECT count(*) FROM " + schema + ".prefix_configs WHERE doc_type = 'GE'").Scan(&n); err != nil || n != 1 {
		t.Errorf("GE number series: %v %d", err, n)
	}
	if !AttachmentsEnabled("GateEntry") || !AttachmentStatusAllowsChange("GateEntry", "Open") || AttachmentStatusAllowsChange("GateEntry", "GRN Made") {
		t.Errorf("a gate entry takes a photo while Open")
	}
	if sc, ok := ScopeForDoctype("GateEntry"); !ok || len(sc.LocationFields) != 1 || sc.LocationFields[0] != "location_code" {
		t.Errorf("GateEntry scope: %+v", sc)
	}
	var g int
	_ = db.DB.QueryRow("SELECT count(*) FROM " + schema + ".doctype_fields WHERE doctype_name = 'GRN' AND fieldname = 'gate_entry_id'").Scan(&g)
	if g != 1 {
		t.Errorf("GRN.gate_entry_id missing")
	}
}

// A GRN started from a gate entry carries vendor / PO / transport across and links back.
func TestStage14638GRNStartsFromGateEntry(t *testing.T) {
	tenantID, _, cleanup, insert := gateEntryTestTenant(t)
	defer cleanup()
	insert("GE-T1", "GateEntry", "Open", map[string]interface{}{"code": "GE-T1", "location_code": "WH1", "vendor": "V-1", "po_id": "PO-1",
		"parcels": 4.0, "transporter": "VRL", "lr_no": "LR99", "vehicle_number": "KA01AB1234", "status": "Open"})

	payload := map[string]interface{}{"gate_entry_id": "GE-T1"}
	if err := PrepareGRNGateEntry(tenantID, true, payload); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if payload["vendor"] != "V-1" || payload["po_id"] != "PO-1" || payload["lr_awb_no"] != "LR99" || payload["vehicle_no"] != "KA01AB1234" ||
		payload["transporter"] != "VRL" || numFromInterface(payload["package_count"]) != 4 || payload["location"] != "WH1" {
		t.Errorf("carried across: %v", payload)
	}
	// What the person typed on the receipt is kept.
	typed := map[string]interface{}{"gate_entry_id": "GE-T1", "lr_awb_no": "MINE", "po_id": "PO-2"}
	if err := PrepareGRNGateEntry(tenantID, true, typed); err != nil || typed["lr_awb_no"] != "MINE" || typed["po_id"] != "PO-2" {
		t.Errorf("typed values kept: %v %v", err, typed)
	}
	// A different vendor is refused.
	if err := PrepareGRNGateEntry(tenantID, true, map[string]interface{}{"gate_entry_id": "GE-T1", "vendor": "V-9"}); err == nil {
		t.Errorf("a receipt for another vendor must be refused")
	}
	// No gate entry id: nothing happens. An unknown one is refused.
	if err := PrepareGRNGateEntry(tenantID, true, map[string]interface{}{"vendor": "V-9"}); err != nil {
		t.Errorf("no gate entry: %v", err)
	}
	if err := PrepareGRNGateEntry(tenantID, true, map[string]interface{}{"gate_entry_id": "GE-NOPE"}); err == nil {
		t.Errorf("an unknown gate entry must be refused")
	}

	// The saved GRN links back; the entry is then used up.
	if err := LinkGateEntryToGRN(tenantID, "GRN-T1", "GE-T1"); err != nil {
		t.Fatalf("link: %v", err)
	}
	data, status, err := fetchDocData(tenantID, "GateEntry", "GE-T1")
	if err != nil || status != "GRN Made" || data["grn_id"] != "GRN-T1" {
		t.Errorf("after link: %v status %q data %v", err, status, data)
	}
	if err := PrepareGRNGateEntry(tenantID, true, map[string]interface{}{"gate_entry_id": "GE-T1"}); err == nil {
		t.Errorf("a gate entry that has its GRN cannot start another")
	}
	insert("GE-T2", "GateEntry", "Cancelled", map[string]interface{}{"code": "GE-T2", "vendor": "V-1", "status": "Cancelled"})
	if err := PrepareGRNGateEntry(tenantID, true, map[string]interface{}{"gate_entry_id": "GE-T2"}); err == nil {
		t.Errorf("a cancelled gate entry cannot start a GRN")
	}
}

// The report lists open entries with no GRN after N hours.
func TestStage14638GateEntriesWithNoGRNReport(t *testing.T) {
	tenantID, schema, cleanup, insert := gateEntryTestTenant(t)
	defer cleanup()
	insert("GE-R1", "GateEntry", "Open", map[string]interface{}{"code": "GE-R1", "location_code": "WH1", "vendor": "V-1", "parcels": 2.0, "status": "Open"})
	insert("GE-R2", "GateEntry", "Open", map[string]interface{}{"code": "GE-R2", "location_code": "WH1", "vendor": "V-2", "status": "Open"})
	insert("GE-R3", "GateEntry", "GRN Made", map[string]interface{}{"code": "GE-R3", "vendor": "V-3", "status": "GRN Made", "grn_id": "G"})
	insert("GE-R4", "GateEntry", "Cancelled", map[string]interface{}{"code": "GE-R4", "vendor": "V-4", "status": "Cancelled"})
	for id, h := range map[string]int{"GE-R1": 30, "GE-R2": 2, "GE-R3": 50, "GE-R4": 50} {
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET created_at = NOW() - make_interval(hours => $2) WHERE id = $1", id, h); err != nil {
			t.Fatal(err)
		}
	}
	def, ok := reportRegistry["gate-entries-no-grn"]
	if !ok {
		t.Fatalf("report gate-entries-no-grn is not registered")
	}
	rows, err := def.Run(tenantID, map[string]string{"hours": "24"})
	if err != nil || len(rows) != 1 || rows[0]["gate_entry"] != "GE-R1" || rows[0]["vendor"] != "V-1" || numFromInterface(rows[0]["hours"]) < 30 {
		t.Fatalf("24 hours: %v %v", err, rows)
	}
	rows, _ = def.Run(tenantID, map[string]string{"hours": "1"})
	if len(rows) != 2 {
		t.Errorf("1 hour should list both open entries: %v", rows)
	}
	rows, _ = def.Run(tenantID, nil) // no hours given: a day
	if len(rows) != 1 {
		t.Errorf("default 24 hours: %v", rows)
	}
}
