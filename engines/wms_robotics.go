package engines

import (
	"custom_erp/db"
	"fmt"
)

// Stage 26.5.16 (WMS Enterprise Maturity Sprint P2 follow-up): robotics/
// conveyor/scale inbound API integration. Go-ahead given 2026-07-27 for
// all five P2 bundles previously deferred pending a real warehouse-scale
// pilot - deliberately generic (no vendor-specific SDK/protocol, since no
// specific robotics/conveyor/scale vendor is contracted): a single inbound
// endpoint accepting a small action-tagged payload, mapped onto the
// existing PutawayToBin/ScanPickItem functions exactly like a human
// picker's own actions would be, rather than a parallel stock-mutation
// path. A real vendor integration would still plug in here (translate its
// own wire format to this payload shape) rather than needing new engine
// logic.

func VerifyRoboticsAPIKey(tenantID, provided string) bool {
	if provided == "" {
		return false
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var count int
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'RoboticsIntegrationCredential' AND status = 'Active' AND data->>'api_key' = $1`, schema),
		provided).Scan(&count)
	return count > 0
}

// RoboticsEventResult is what the inbound handler echoes back so the
// calling device/system gets a concrete outcome, not just a bare 200.
type RoboticsEventResult struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// ProcessRoboticsPutaway maps a robotics/conveyor putaway confirmation
// straight onto the existing PutawayToBin - "system" as the actor, same
// convention every other unattended/system-driven write in this codebase
// uses (e.g. LoyaltyExpiry's automatic Burn rows, the scheduled-report
// worker's delivery log).
func ProcessRoboticsPutaway(tenantID, binCode, sku string, qty int, deviceID string) (RoboticsEventResult, error) {
	if binCode == "" || sku == "" || qty <= 0 {
		return RoboticsEventResult{}, &ValidationError{Code: "GLOBAL-0002", Message: "bin_code, sku, and a positive qty are required"}
	}
	if err := PutawayToBin(tenantID, binCode, sku, qty, "system"); err != nil {
		return RoboticsEventResult{}, err
	}
	LogAuditEvent(tenantID, "system", "ROBOTICS_PUTAWAY", "SUCCESS", fmt.Sprintf("device=%s bin=%s sku=%s qty=%d", deviceID, binCode, sku, qty))
	return RoboticsEventResult{Status: "putaway_confirmed"}, nil
}

// ProcessRoboticsPick maps a robotics/conveyor pick confirmation straight
// onto the existing ScanPickItem, same scan-resolution/task-guard rules a
// human picker's own scan already goes through.
func ProcessRoboticsPick(tenantID, taskID, scan, deviceID string) (RoboticsEventResult, error) {
	if taskID == "" || scan == "" {
		return RoboticsEventResult{}, &ValidationError{Code: "GLOBAL-0002", Message: "task_id and scan are both required"}
	}
	sku, pickedQty, err := ScanPickItem(tenantID, taskID, scan)
	if err != nil {
		return RoboticsEventResult{}, err
	}
	LogAuditEvent(tenantID, "system", "ROBOTICS_PICK", "SUCCESS", fmt.Sprintf("device=%s task=%s sku=%s picked_qty=%d", deviceID, taskID, sku, pickedQty))
	return RoboticsEventResult{Status: "pick_confirmed", Detail: sku}, nil
}

// ProcessRoboticsWeightConfirm is a scale-integration audit signal, not a
// stock mutation - a scale validates what a putaway/pick already moved
// (flagging a mismatch for a human to investigate), it doesn't move stock
// itself. Logged as a system error (Warning) only when the measured
// weight-derived qty disagrees with what's on record, same "flag, don't
// block" shape 26.9's capacity-warning (MFG-0277) already uses.
func ProcessRoboticsWeightConfirm(tenantID, binCode, sku string, measuredQty int, deviceID string) (RoboticsEventResult, error) {
	if binCode == "" || sku == "" {
		return RoboticsEventResult{}, &ValidationError{Code: "GLOBAL-0002", Message: "bin_code and sku are required"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return RoboticsEventResult{}, err
	}
	var recordedQty int
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(qty, 0) FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good'`, schema),
		binCode, sku).Scan(&recordedQty)
	if recordedQty != measuredQty {
		msg := fmt.Sprintf("device=%s bin=%s sku=%s scale_measured=%d system_recorded=%d", deviceID, binCode, sku, measuredQty, recordedQty)
		LogSystemError(tenantID, "", "WARN", "ProcessRoboticsWeightConfirm", msg, "")
		return RoboticsEventResult{Status: "weight_mismatch", Detail: msg}, nil
	}
	return RoboticsEventResult{Status: "weight_confirmed"}, nil
}
