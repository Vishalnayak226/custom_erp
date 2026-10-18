package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestInboundWorkStage557 walks 55.7's inbound chain on a provisioned
// throwaway client: an expected ASN shows its booking, then its trailer in the
// yard, then at a door; a trailer is checked in once and a door holds one
// trailer; received stock not yet binned is putaway work; a partial receipt
// keeps the ASN open and the receipt that completes it closes the ASN and its
// appointment, after which a new receipt against it is refused.
func TestInboundWorkStage557(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	code := func(err error) string {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Code
		}
		if err == nil {
			return ""
		}
		return err.Error()
	}
	const loc = "IN-557"
	today := time.Now().Format("2006-01-02")
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Inbound DC", "type": "Warehouse"})
	insert("SKU557", "Item", "Active", map[string]interface{}{"code": "SKU557", "name": "Denim jacket", "hsn_code": "6201", "tax_treatment": "Exempt"})
	insert("BIN557", "Bin", "Active", map[string]interface{}{"bin_code": "BIN557", "location": loc, "status": "Active"})
	insert("D1", "DockDoor", "Active", map[string]interface{}{"code": "D1", "door_type": "Inbound", "status": "Active"})
	insert("D2", "DockDoor", "Active", map[string]interface{}{"code": "D2", "door_type": "Inbound", "status": "Active"})
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "SKU557", "qty": 10}})
	insert("ASN557", "ASN", "Expected", map[string]interface{}{"asn_number": "ASN557", "po_number": "PO557", "po_id": "PO557", "location": loc, "expected_items": string(items), "status": "Expected"})
	insert("AP557", "Appointment", "Scheduled", map[string]interface{}{"dock_door": "D1", "appointment_type": "Inbound", "asn_id": "ASN557", "appointment_date": today, "start_time": "09:00", "end_time": "10:00", "status": "Scheduled"})

	work, err := InboundWork(tenantID, loc)
	if err != nil || len(work.ASNs) != 1 || work.ASNs[0].Units != 10 || !strings.HasPrefix(work.ASNs[0].Stage, "Booked") || !work.HasBins {
		t.Fatalf("booked: %v %+v", err, work)
	}

	// Check in once; the appointment follows.
	yardID, err := CheckInTrailer(tenantID, CheckInInput{Trailer: "TR-1", AppointmentID: "AP557"}, "gate")
	if err != nil {
		t.Fatalf("check in: %v", err)
	}
	if _, err := CheckInTrailer(tenantID, CheckInInput{Trailer: "tr-1"}, "gate"); code(err) != "INVENT-0498" {
		t.Fatalf("same trailer twice: %v", err)
	}
	if _, err := CheckInTrailer(tenantID, CheckInInput{Trailer: "TR-9", AppointmentID: "AP557"}, "gate"); code(err) != "INVENT-0498" {
		t.Fatalf("appointment checked in twice: %v", err)
	}
	if _, st, _ := fetchDocData(tenantID, "Appointment", "AP557"); st != "CheckedIn" {
		t.Fatalf("appointment after check-in: %s", st)
	}
	work, _ = InboundWork(tenantID, loc)
	if work.ASNs[0].Yard == nil || work.ASNs[0].Stage != "Trailer TR-1 in the yard" {
		t.Fatalf("in yard: %+v", work.ASNs[0])
	}

	// A door holds one trailer; an unknown door is refused.
	if err := AssignYardDoor(tenantID, yardID, "D1", "gate"); err != nil {
		t.Fatalf("spot: %v", err)
	}
	other, _ := CheckInTrailer(tenantID, CheckInInput{Trailer: "TR-2"}, "gate")
	if err := AssignYardDoor(tenantID, other, "D1", "gate"); code(err) != "INVENT-0497" {
		t.Fatalf("occupied door: %v", err)
	}
	if err := AssignYardDoor(tenantID, other, "D9", "gate"); code(err) != "INVENT-0497" {
		t.Fatalf("unknown door: %v", err)
	}
	if err := AssignYardDoor(tenantID, other, "D2", "gate"); err != nil {
		t.Fatalf("free door: %v", err)
	}
	if _, st, _ := fetchDocData(tenantID, "Appointment", "AP557"); st != "InProgress" {
		t.Fatalf("appointment at the door: %s", st)
	}
	work, _ = InboundWork(tenantID, loc)
	if !strings.Contains(work.ASNs[0].Stage, "at door D1 - ready to receive") {
		t.Fatalf("at door: %+v", work.ASNs[0])
	}

	// Putaway work: received minus binned.
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "SKU557", "qty": 6.0}}, false); err != nil {
		t.Fatalf("stock: %v", err)
	}
	if err := PutawayToBin(tenantID, "BIN557", "SKU557", 2, "op"); err != nil {
		t.Fatalf("putaway: %v", err)
	}
	work, _ = InboundWork(tenantID, loc)
	if len(work.Putaway) != 1 || work.Putaway[0].Waiting != 4 || work.Putaway[0].Name != "Denim jacket" {
		t.Fatalf("waiting: %+v", work.Putaway)
	}

	// Partial receipt keeps the ASN open; the completing one closes it.
	grn := func(id string, qty int) {
		lines, _ := json.Marshal([]map[string]interface{}{{"sku": "SKU557", "qty": qty}})
		insert(id, "GRN", "Approved", map[string]interface{}{"asn_id": "ASN557", "po_id": "PO557", "location": loc, "received_items": string(lines)})
		MarkASNReceivedIfComplete(tenantID, id)
	}
	if err := checkASNOpenForReceipt(tenantID, "G1", "ASN557"); err != nil {
		t.Fatalf("open ASN refused: %v", err)
	}
	grn("G1", 6)
	if _, st, _ := fetchDocData(tenantID, "ASN", "ASN557"); st != "Expected" {
		t.Fatalf("partial receipt closed the ASN: %s", st)
	}
	grn("G2", 4)
	if _, st, _ := fetchDocData(tenantID, "ASN", "ASN557"); st != "Received" {
		t.Fatalf("complete receipt: %s", st)
	}
	if _, st, _ := fetchDocData(tenantID, "Appointment", "AP557"); st != "Completed" {
		t.Fatalf("appointment after receipt: %s", st)
	}
	if err := checkASNOpenForReceipt(tenantID, "G3", "ASN557"); code(err) != "GOODSR-0558" {
		t.Fatalf("receiving a received ASN again: %v", err)
	}
	if err := checkASNOpenForReceipt(tenantID, "G1", "ASN557"); err != nil {
		t.Fatalf("editing an existing receipt: %v", err)
	}
	if work, _ = InboundWork(tenantID, loc); len(work.ASNs) != 0 {
		t.Fatalf("a received ASN is no longer expected: %+v", work.ASNs)
	}
}
