package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 55.7 - inbound work an operator can pick instead of remember.
//
// The inbound chain already exists as separate records: an Appointment books
// a door for an ASN, a YardCheckIn records the trailer arriving (and the door
// it is spotted at), the ASN is received into a GRN, and putaway places the
// received units into bins. Each screen used to ask for the next record's ID
// by hand. InboundWork reads the chain back as one list - every expected ASN
// with how far it has got - plus the stock received at a location that is not
// yet in any bin, which is exactly what putaway still has to do (on hand minus
// everything binned, the same figure PutawayToBin refuses to exceed).
//
// Two yard actions move here from generic record edits so the server can say
// no: checking a trailer in (once - the same trailer cannot be in the yard
// twice - and marking its appointment checked in) and spotting it at a door
// (an active door, not already holding another trailer).

// InboundAppointment is the booking behind an ASN.
type InboundAppointment struct {
	ID     string `json:"id"`
	Date   string `json:"date"`
	Start  string `json:"start_time"`
	Door   string `json:"dock_door"`
	Status string `json:"status"`
}

// InboundYard is the trailer for that booking, while it is on site.
type InboundYard struct {
	ID      string `json:"id"`
	Trailer string `json:"trailer_no"`
	Door    string `json:"dock_door,omitempty"`
	Slot    string `json:"yard_location,omitempty"`
	Status  string `json:"status"`
}

// InboundASN is one expected delivery and how far it has got.
type InboundASN struct {
	ID           string              `json:"id"`
	Number       string              `json:"asn_number"`
	PO           string              `json:"po_id"`
	Vendor       string              `json:"vendor,omitempty"`
	Location     string              `json:"location"`
	ExpectedDate string              `json:"expected_date,omitempty"`
	Lines        int                 `json:"lines"`
	Units        float64             `json:"units"`
	Appointment  *InboundAppointment `json:"appointment,omitempty"`
	Yard         *InboundYard        `json:"yard,omitempty"`
	Stage        string              `json:"stage"`
}

// PutawayWaiting is stock received at a location and not yet in a bin.
type PutawayWaiting struct {
	Sku      string  `json:"sku"`
	Name     string  `json:"name,omitempty"`
	Location string  `json:"location"`
	OnHand   float64 `json:"on_hand"`
	Binned   float64 `json:"binned"`
	Waiting  float64 `json:"waiting"`
}

// InboundWorkList is the whole inbound queue for a location.
type InboundWorkList struct {
	ASNs     []InboundASN     `json:"asns"`
	Putaway  []PutawayWaiting `json:"putaway"`
	HasBins  bool             `json:"has_bins"`
	Location string           `json:"location"`
}

func docsByType(schema, doctype string) ([]map[string]interface{}, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, COALESCE(status, '') FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL`, schema), doctype)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, raw, status string
		if err := rows.Scan(&id, &raw, &status); err != nil {
			return nil, err
		}
		d := map[string]interface{}{}
		_ = json.Unmarshal([]byte(raw), &d)
		d["id"], d["status"] = id, status
		out = append(out, d)
	}
	return out, rows.Err()
}

// InboundWork lists expected ASNs (all locations when location is blank) and
// the stock waiting for putaway.
func InboundWork(tenantID, location string) (InboundWorkList, error) {
	out := InboundWorkList{ASNs: []InboundASN{}, Putaway: []PutawayWaiting{}, Location: location}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	asns, err := docsByType(schema, "ASN")
	if err != nil {
		return out, err
	}
	appts, _ := docsByType(schema, "Appointment")
	yards, _ := docsByType(schema, "YardCheckIn")
	apptByASN := map[string]map[string]interface{}{}
	for _, a := range appts {
		if s := strField(a, "status"); s == "Cancelled" || s == "NoShow" {
			continue
		}
		if asn := strField(a, "asn_id"); asn != "" {
			apptByASN[asn] = a
		}
	}
	yardByAppt := map[string]map[string]interface{}{}
	for _, y := range yards {
		if strField(y, "status") != "Departed" && strField(y, "appointment_id") != "" {
			yardByAppt[strField(y, "appointment_id")] = y
		}
	}
	for _, a := range asns {
		if strField(a, "status") != "Expected" || (location != "" && strField(a, "location") != location) {
			continue
		}
		row := InboundASN{ID: strField(a, "id"), Number: strField(a, "asn_number"), PO: strField(a, "po_id"), Vendor: strField(a, "vendor"),
			Location: strField(a, "location"), ExpectedDate: strField(a, "expected_date"), Stage: "Expected"}
		if row.PO == "" {
			row.PO = strField(a, "po_number")
		}
		for _, l := range jsonRows(a["expected_items"]) {
			row.Lines++
			row.Units += numFromInterface(l["qty"])
		}
		if ap, ok := apptByASN[row.ID]; ok {
			row.Appointment = &InboundAppointment{ID: strField(ap, "id"), Date: strField(ap, "appointment_date"), Start: strField(ap, "start_time"),
				Door: strField(ap, "dock_door"), Status: strField(ap, "status")}
			row.Stage = fmt.Sprintf("Booked %s %s, door %s", row.Appointment.Date, row.Appointment.Start, row.Appointment.Door)
			if y, ok := yardByAppt[row.Appointment.ID]; ok {
				row.Yard = &InboundYard{ID: strField(y, "id"), Trailer: strField(y, "trailer_no"), Door: strField(y, "dock_door"),
					Slot: strField(y, "yard_location"), Status: strField(y, "status")}
				if row.Yard.Status == "AtDoor" {
					row.Stage = fmt.Sprintf("Trailer %s at door %s - ready to receive", row.Yard.Trailer, row.Yard.Door)
				} else {
					row.Stage = fmt.Sprintf("Trailer %s in the yard", row.Yard.Trailer)
				}
			}
		}
		out.ASNs = append(out.ASNs, row)
	}
	sort.Slice(out.ASNs, func(i, j int) bool {
		rank := func(a InboundASN) int {
			switch {
			case a.Yard != nil && a.Yard.Status == "AtDoor":
				return 0
			case a.Yard != nil:
				return 1
			case a.Appointment != nil:
				return 2
			}
			return 3
		}
		if ri, rj := rank(out.ASNs[i]), rank(out.ASNs[j]); ri != rj {
			return ri < rj
		}
		return out.ASNs[i].ExpectedDate < out.ASNs[j].ExpectedDate
	})

	// Putaway: only where the location keeps bins at all.
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH binned AS (
			SELECT sku, location_code, SUM(qty) AS q FROM %s.bin_stock GROUP BY 1, 2
		), binlocs AS (
			SELECT DISTINCT data->>'location' AS loc FROM %s.documents
			 WHERE doctype = 'Bin' AND status = 'Active' AND deleted_at IS NULL
		)
		SELECT ia.sku, ia.location_code, ia.on_hand, COALESCE(b.q, 0)
		  FROM %s.inventory_availability ia
		  JOIN binlocs bl ON bl.loc = ia.location_code
		  LEFT JOIN binned b ON b.sku = ia.sku AND b.location_code = ia.location_code
		 WHERE ia.on_hand > COALESCE(b.q, 0) AND ($1 = '' OR ia.location_code = $1)
		 ORDER BY ia.location_code, ia.sku`, schema, schema, schema), location)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p PutawayWaiting
		if err := rows.Scan(&p.Sku, &p.Location, &p.OnHand, &p.Binned); err != nil {
			return out, err
		}
		p.Waiting = p.OnHand - p.Binned
		out.Putaway = append(out.Putaway, p)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for i := range out.Putaway {
		if it, err := ResolveItemBySKU(tenantID, out.Putaway[i].Sku); err == nil {
			out.Putaway[i].Name = strField(it.Data, "name")
		}
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Bin' AND status = 'Active' AND deleted_at IS NULL
		AND ($1 = '' OR data->>'location' = $1))`, schema), location).Scan(&out.HasBins)
	return out, nil
}

// CheckInInput is a trailer arriving at the gate.
type CheckInInput struct {
	Trailer       string `json:"trailer_no"`
	Carrier       string `json:"carrier"`
	Driver        string `json:"driver_name"`
	Slot          string `json:"yard_location"`
	AppointmentID string `json:"appointment_id"`
}

// CheckInTrailer records a trailer in the yard once, and marks its
// appointment checked in.
func CheckInTrailer(tenantID string, in CheckInInput, userID string) (string, error) {
	in.Trailer = strings.TrimSpace(in.Trailer)
	if in.Trailer == "" {
		return "", &ValidationError{Code: "INVENT-0498", Message: "enter the trailer number"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Serialise check-ins of the same trailer.
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, schema+":yard:"+strings.ToUpper(in.Trailer)); err != nil {
		return "", err
	}
	var already string
	_ = tx.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'YardCheckIn' AND deleted_at IS NULL
		AND status <> 'Departed' AND upper(data->>'trailer_no') = upper($1) LIMIT 1`, schema), in.Trailer).Scan(&already)
	if already != "" {
		return "", &ValidationError{Code: "INVENT-0498", Message: fmt.Sprintf("trailer %s is already in the yard (%s) - check it out before checking it in again", in.Trailer, already)}
	}
	if in.AppointmentID != "" {
		appt, status, err := lockDocTx(tx, schema, "Appointment", in.AppointmentID)
		if err != nil {
			return "", &ValidationError{Code: "INVENT-0498", Message: fmt.Sprintf("appointment %s not found", in.AppointmentID)}
		}
		if status != "Scheduled" {
			return "", &ValidationError{Code: "INVENT-0498", Message: fmt.Sprintf("appointment %s is %s, not Scheduled", in.AppointmentID, status)}
		}
		if in.Carrier == "" {
			in.Carrier = strField(appt, "carrier")
		}
		appt["trailer_no"] = in.Trailer
		appt["checked_in_at"] = time.Now().UTC().Format(time.RFC3339)
		if err := writeDocTx(tx, schema, "Appointment", in.AppointmentID, appt, "CheckedIn"); err != nil {
			return "", err
		}
	}
	id := NewDocID("YARD")
	b, _ := json.Marshal(map[string]interface{}{"id": id, "trailer_no": in.Trailer, "carrier": strings.TrimSpace(in.Carrier), "driver_name": strings.TrimSpace(in.Driver),
		"yard_location": strings.TrimSpace(in.Slot), "appointment_id": in.AppointmentID, "status": "InYard", "checked_in_at": time.Now().UTC().Format(time.RFC3339)})
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'YardCheckIn', $2, 'InYard', $3)`, schema), id, b, userID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

// AssignYardDoor spots a trailer at an active door that no other trailer is
// at, and starts its appointment.
func AssignYardDoor(tenantID, yardID, door, userID string) error {
	door = strings.TrimSpace(door)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, schema+":door:"+strings.ToUpper(door)); err != nil {
		return err
	}
	y, status, err := lockDocTx(tx, schema, "YardCheckIn", yardID)
	if err != nil {
		return &ValidationError{Code: "INVENT-0497", Message: fmt.Sprintf("yard record %s not found", yardID)}
	}
	if status == "Departed" {
		return &ValidationError{Code: "INVENT-0497", Message: fmt.Sprintf("trailer %s has already left", strField(y, "trailer_no"))}
	}
	var doorStatus string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = 'DockDoor' AND deleted_at IS NULL AND data->>'code' = $1`, schema), door).Scan(&doorStatus); err != nil {
		return &ValidationError{Code: "INVENT-0497", Message: fmt.Sprintf("door %s is not set up", door)}
	}
	if doorStatus != "Active" {
		return &ValidationError{Code: "INVENT-0497", Message: fmt.Sprintf("door %s is not active", door)}
	}
	var occupant string
	_ = tx.QueryRow(fmt.Sprintf(`SELECT data->>'trailer_no' FROM %s.documents WHERE doctype = 'YardCheckIn' AND deleted_at IS NULL
		AND status = 'AtDoor' AND data->>'dock_door' = $1 AND id <> $2 LIMIT 1`, schema), door, yardID).Scan(&occupant)
	if occupant != "" {
		return &ValidationError{Code: "INVENT-0497", Message: fmt.Sprintf("door %s is occupied by trailer %s - check that trailer out or choose another door", door, occupant)}
	}
	y["dock_door"] = door
	y["at_door_since"] = time.Now().UTC().Format(time.RFC3339)
	if err := writeDocTx(tx, schema, "YardCheckIn", yardID, y, "AtDoor"); err != nil {
		return err
	}
	if apptID := strField(y, "appointment_id"); apptID != "" {
		if appt, st, err := lockDocTx(tx, schema, "Appointment", apptID); err == nil && (st == "Scheduled" || st == "CheckedIn") {
			appt["actual_dock_door"] = door
			if err := writeDocTx(tx, schema, "Appointment", apptID, appt, "InProgress"); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// checkASNOpenForReceipt refuses a new GRN against an ASN that is no longer
// Expected. An edit of a GRN that already exists is left alone.
func checkASNOpenForReceipt(tenantID, grnID, asnID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var exists bool
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'GRN' AND id = $1)`, schema), grnID).Scan(&exists)
	if exists {
		return nil
	}
	asn, status, err := fetchDocData(tenantID, "ASN", asnID)
	if err != nil || asn == nil {
		return &ValidationError{Code: "GOODSR-0558", Message: fmt.Sprintf("ASN %s not found", asnID)}
	}
	if status != "Expected" {
		return &ValidationError{Code: "GOODSR-0558", Message: fmt.Sprintf("ASN %s is already %s - this delivery cannot be received again", asnID, status)}
	}
	return nil
}

// MarkASNReceivedIfComplete runs after a GRN posts: when the GRNs against its
// ASN now cover every expected line, the ASN becomes Received and its
// appointment Completed. A partial receipt leaves the ASN Expected for the
// rest.
func MarkASNReceivedIfComplete(tenantID, grnID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	grn, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil || grn == nil {
		return
	}
	asnID := strField(grn, "asn_id")
	if asnID == "" {
		return
	}
	asn, status, err := fetchDocData(tenantID, "ASN", asnID)
	if err != nil || asn == nil || status != "Expected" {
		return
	}
	received := map[string]float64{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data->>'received_items' FROM %s.documents
		WHERE doctype = 'GRN' AND deleted_at IS NULL AND status <> 'Cancelled' AND data->>'asn_id' = $1`, schema), asnID)
	if err != nil {
		return
	}
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) == nil {
			for _, l := range jsonRows(raw) {
				received[strField(l, "sku")] += numFromInterface(l["qty"])
			}
		}
	}
	rows.Close()
	for _, l := range jsonRows(asn["expected_items"]) {
		if received[strField(l, "sku")]+1e-9 < numFromInterface(l["qty"]) {
			return
		}
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Received', data = data || jsonb_build_object('status', 'Received', 'received_by_grn', $2::text),
		updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ASN' AND id = $1 AND status = 'Expected'`, schema), asnID, grnID)
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Completed', data = data || jsonb_build_object('status', 'Completed'),
		updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Appointment' AND data->>'asn_id' = $1 AND status IN ('Scheduled', 'CheckedIn', 'InProgress')`, schema), asnID)
}
