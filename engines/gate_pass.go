package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 35.4.4: gate pass for outbound movement.
//
// The gate pass is the security-desk record: which vehicle left, with whose
// parcels, when, and who authorised it. It is issued per vehicle rather than
// per parcel, which is why it hangs off a Manifest - the manifest is already
// the courier+location grouping a single pickup collects.
//
// Deliberately not a status on Manifest. A manifest can legitimately be
// collected in two runs when a vehicle fills up, and a gate pass is a physical
// event with its own vehicle, driver and timestamp; folding it into the
// manifest's status field would lose the second run entirely.

// GatePassInput is what the gate desk records. Only location is required -
// vehicle and driver details are copied off a licence at the barrier and are
// sometimes genuinely unavailable at the moment the pass is raised.
type GatePassInput struct {
	ManifestID    string `json:"manifest_id"`
	LocationCode  string `json:"location_code"`
	Carrier       string `json:"carrier"`
	VehicleNumber string `json:"vehicle_number"`
	DriverName    string `json:"driver_name"`
	DriverPhone   string `json:"driver_phone"`
	Remarks       string `json:"remarks"`
}

// CreateGatePass raises a Draft gate pass.
//
// When a manifest is given, carrier, location and package count are taken from
// it rather than trusted from the caller: the gate desk is transcribing, and a
// pass whose count disagrees with the manifest it references is worse than no
// pass at all - it is a discrepancy that looks like a reconciled record.
func CreateGatePass(tenantID string, in GatePassInput, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	in.ManifestID = strings.TrimSpace(in.ManifestID)
	in.LocationCode = strings.TrimSpace(in.LocationCode)

	packageCount := 0
	if in.ManifestID != "" {
		var dataStr, status string
		err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT data, status FROM %s.documents WHERE doctype = 'Manifest' AND id = $1 AND deleted_at IS NULL`, schema),
			in.ManifestID).Scan(&dataStr, &status)
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("manifest %s not found", in.ManifestID)
		} else if err != nil {
			return "", err
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &m); err != nil {
			return "", err
		}
		if c, _ := m["courier"].(string); c != "" {
			in.Carrier = c
		}
		if l, _ := m["location_code"].(string); l != "" {
			in.LocationCode = l
		}
		packageCount = int(numFromInterface(m["shipment_count"]))
	}
	if in.LocationCode == "" {
		return "", fmt.Errorf("location_code is required")
	}

	gatePassID := NewDocID("GP")
	data := map[string]interface{}{
		"code":           gatePassID,
		"manifest_id":    in.ManifestID,
		"location_code":  in.LocationCode,
		"carrier":        in.Carrier,
		"vehicle_number": strings.TrimSpace(in.VehicleNumber),
		"driver_name":    strings.TrimSpace(in.DriverName),
		"driver_phone":   strings.TrimSpace(in.DriverPhone),
		"package_count":  packageCount,
		"remarks":        strings.TrimSpace(in.Remarks),
		"status":         "Draft",
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'GatePass', $2, 'Draft', 'system')`, schema),
		gatePassID, encoded); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "CREATE_GATE_PASS", "SUCCESS",
		fmt.Sprintf("Created gate pass %s at %s (manifest %q, %d packages)", gatePassID, in.LocationCode, in.ManifestID, packageCount))
	return gatePassID, nil
}

// UpdateGatePass amends a pass that has not yet been completed or discarded.
// Vehicle and driver details are the fields that actually change - a different
// driver turns up, a registration was mistyped - so those are what this edits.
// The manifest link is not editable: re-pointing a pass at another manifest
// after the fact rewrites what left the building.
func UpdateGatePass(tenantID, gatePassID string, in GatePassInput, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, err := loadGatePass(schema, gatePassID)
	if err != nil {
		return err
	}
	if status == "Completed" || status == "Discarded" {
		return fmt.Errorf("gate pass %s is %s and can no longer be amended", gatePassID, status)
	}
	for key, val := range map[string]string{
		"vehicle_number": in.VehicleNumber,
		"driver_name":    in.DriverName,
		"driver_phone":   in.DriverPhone,
		"remarks":        in.Remarks,
	} {
		if strings.TrimSpace(val) != "" {
			data[key] = strings.TrimSpace(val)
		}
	}
	if err := saveGatePass(schema, gatePassID, data, status); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "UPDATE_GATE_PASS", "SUCCESS", fmt.Sprintf("Updated gate pass %s", gatePassID))
	return nil
}

// IssueGatePass authorises the vehicle to leave.
//
// A vehicle number is required at this point and not before: raising the pass
// is paperwork, issuing it is the moment something physically departs, and a
// departure record with no vehicle on it cannot be reconciled against anything
// later.
func IssueGatePass(tenantID, gatePassID, userID string) error {
	return transitionGatePass(tenantID, gatePassID, "Issued", "", userID, func(data map[string]interface{}) error {
		if v, _ := data["vehicle_number"].(string); strings.TrimSpace(v) == "" {
			return fmt.Errorf("a vehicle number is required before a gate pass can be issued")
		}
		data["issued_at"] = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
}

// CompleteGatePass closes the pass once the vehicle is confirmed gone.
func CompleteGatePass(tenantID, gatePassID, userID string) error {
	return transitionGatePass(tenantID, gatePassID, "Completed", "", userID, func(data map[string]interface{}) error {
		data["completed_at"] = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
}

// DiscardGatePass voids a pass that was raised in error or whose pickup did not
// happen. Reason-coded through the same ReasonCode master every other void in
// this repo uses, so a discarded pass always says why.
func DiscardGatePass(tenantID, gatePassID, reasonCode, userID string) error {
	if err := requireActiveReasonCode(tenantID, reasonCode, "Cancellation"); err != nil {
		return err
	}
	return transitionGatePass(tenantID, gatePassID, "Discarded", reasonCode, userID, func(data map[string]interface{}) error {
		data["reason_code"] = reasonCode
		data["discarded_at"] = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
}

// transitionGatePass is the one place a gate pass changes state, so every
// transition goes through Stage 29.8's rule map rather than each action
// re-deciding what is legal - the same consolidation 35.3.6 applied to the
// order mutations.
func transitionGatePass(tenantID, gatePassID, target, reasonCode, userID string, mutate func(map[string]interface{}) error) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, err := loadGatePass(schema, gatePassID)
	if err != nil {
		return err
	}
	if status == target {
		return nil
	}
	payload := map[string]interface{}{}
	if reasonCode != "" {
		payload["reason_code"] = reasonCode
	}
	if err := ValidateStatusTransition(tenantID, "GatePass", status, target, payload); err != nil {
		return err
	}
	if mutate != nil {
		if err := mutate(data); err != nil {
			return err
		}
	}
	data["status"] = target
	if err := saveGatePass(schema, gatePassID, data, target); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "GATE_PASS_"+strings.ToUpper(target), "SUCCESS",
		fmt.Sprintf("Gate pass %s moved %s -> %s", gatePassID, status, target))
	return nil
}

func loadGatePass(schema, gatePassID string) (map[string]interface{}, string, error) {
	var dataStr, status string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'GatePass' AND id = $1 AND deleted_at IS NULL`, schema),
		gatePassID).Scan(&dataStr, &status)
	if err == sql.ErrNoRows {
		return nil, "", fmt.Errorf("gate pass %s not found", gatePassID)
	} else if err != nil {
		return nil, "", err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", err
	}
	return data, status, nil
}

func saveGatePass(schema, gatePassID string, data map[string]interface{}, status string) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'GatePass' AND id = $3`, schema),
		encoded, status, gatePassID)
	return err
}

// SearchGatePasses is the gate desk's own lookup: by vehicle, by manifest, by
// status, at a location. Bounded at 200 rows for the same reason the public API
// clamps its paging - a gate log grows for ever and nobody reads all of it.
func SearchGatePasses(tenantID, locationCode, vehicleNumber, manifestID, status string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		 WHERE doctype = 'GatePass' AND deleted_at IS NULL
		   AND ($1 = '' OR data->>'location_code' = $1)
		   AND ($2 = '' OR UPPER(COALESCE(data->>'vehicle_number', '')) LIKE '%%' || UPPER($2) || '%%')
		   AND ($3 = '' OR data->>'manifest_id' = $3)
		   AND ($4 = '' OR status = $4)
		 ORDER BY created_at DESC, id DESC
		 LIMIT 200`, schema),
		strings.TrimSpace(locationCode), strings.TrimSpace(vehicleNumber), strings.TrimSpace(manifestID), strings.TrimSpace(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			return nil, err
		}
		data["id"] = id
		out = append(out, data)
	}
	return out, rows.Err()
}
