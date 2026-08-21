package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Stage 42.3.5: HoldCode master + hold/release workflow. Placing a hold is
// immediate (PlaceHold) - a defensive action that must never wait on a
// checker's queue. Releasing one is gated: ReleaseHold is only ever called
// from DecideApproval (engines/approval.go) once a HoldReleaseRequest has
// actually been Approved, never directly, so "who released this and when"
// is always an approval_log entry, not a bare field edit.

// PlaceHold records a Hold document (status Active) against a SKU/location
// (optionally a specific batch) and raises inventory_availability.hold_qty
// by qty - the same "aggregate column touched directly at the point of the
// event" convention qc_hold/blocked already use (wms_receiving.go,
// returns.go). computeATS (engines/inventory.go) subtracts it from every
// ATS reader in the system without any of them needing to know Hold exists.
func PlaceHold(tenantID, holdCode, sku, locationCode, batchNo string, qty int, reason, userID string) (string, error) {
	if qty <= 0 {
		return "", errors.New("hold quantity must be positive")
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
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}

	var codeActive bool
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'HoldCode' AND data->>'code' = $1 AND status = 'Active')`, schema),
		holdCode).Scan(&codeActive); err != nil {
		return "", err
	}
	if !codeActive {
		return "", fmt.Errorf("no Active hold code %q exists - create it on the HoldCode master first", holdCode)
	}

	var onHand, existingHold int
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT on_hand, hold_qty FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema),
		sku, locationCode).Scan(&onHand, &existingHold)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no inventory record for SKU %s at location %s", sku, locationCode)
	} else if err != nil {
		return "", err
	}
	if existingHold+qty > onHand {
		return "", fmt.Errorf("hold qty exceeds on-hand stock at %s (on_hand=%d, already held=%d, requested=%d)",
			locationCode, onHand, existingHold, qty)
	}

	holdID := NewDocID("HOLD")
	data := map[string]interface{}{
		"hold_code":     holdCode,
		"sku":           sku,
		"location_code": locationCode,
		"batch_no":      batchNo,
		"qty":           qty,
		"reason":        reason,
		"status":        "Active",
	}
	payload, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Hold', $2, 'Active', $3)`, schema),
		holdID, payload, userID); err != nil {
		return "", err
	}

	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.inventory_availability SET hold_qty = hold_qty + $1 WHERE sku = $2 AND location_code = $3`, schema),
		qty, sku, locationCode); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return holdID, nil
}

// ReleaseHold moves an Active Hold to Released and gives its qty back to
// hold_qty. Called only from DecideApproval on an Approved HoldReleaseRequest
// - see that function's own doc comment. Writes status with a direct SQL
// UPDATE rather than going through the generic doc save path, the same way
// DecideApproval's own status write does, which is what keeps this
// unreachable from a plain Hold edit regardless of role_permissions.
func ReleaseHold(tenantID, holdID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'Hold' AND id = $1 FOR UPDATE`, schema),
		holdID).Scan(&dataStr, &status); err != nil {
		return fmt.Errorf("hold %s not found: %v", holdID, err)
	}
	if status != "Active" {
		return fmt.Errorf("hold %s is not Active (status: %s)", holdID, status)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	sku := strField(data, "sku")
	locationCode := strField(data, "location_code")
	qty := int(numFromInterface(data["qty"]))

	data["status"] = "Released"
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Released', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Hold' AND id = $2`, schema),
		marshaled, holdID); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.inventory_availability SET hold_qty = GREATEST(hold_qty - $1, 0) WHERE sku = $2 AND location_code = $3`, schema),
		qty, sku, locationCode); err != nil {
		return err
	}

	return tx.Commit()
}
