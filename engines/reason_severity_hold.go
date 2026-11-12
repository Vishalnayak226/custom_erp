package engines

import (
	"encoding/json"
	"fmt"
	"strings"

	"custom_erp/db"
)

// Stage 74.1.1 (BRD 4.9, FR-RSN-03, owner decision 2026-11-12): a reason code
// with Severity = High sends a purchase return, or a transfer loss, to Pending
// Approval instead of posting at once. Nothing moves until a second person
// approves, through the same maker-checker engine every approval-gated
// document uses. A lower or absent severity posts as before.
//
//   - Purchase return: the return itself goes Pending Approval (it stamps
//     reason_severity when saved); once Approved, Post works as it always did.
//   - Transfer receipt that books damaged, lost or short units under a High code:
//     the whole receipt is parked as a TransferLossHold document; on approval
//     it is replayed through ReceiveTransferOrderLines unchanged.
//
// With no approval rule for the document type the approver is the client's
// Admin (HasFullAccess decides), so the control works without any set-up.

const (
	ReasonSeverityHigh = "High"

	// TransferLossHoldDoctype carries a transfer receipt that waits for approval.
	TransferLossHoldDoctype = "TransferLossHold"
)

func init() {
	// The submitter may decide only where the client's self-approval setting
	// allows it (approvals.self_approval), the same as any other document.
	RegisterApprovalDecisionHook(TransferLossHoldDoctype, false, func(tenantID, docID, decision, actorUserID string) {
		if decision == "Approved" {
			executeTransferLossHold(tenantID, docID, actorUserID)
		}
	})
}

// severityHoldFallbackRole is the approver of a severity-held document when no
// approval rule matches it: the client's Admin.
func severityHoldFallbackRole(doctype string, data map[string]interface{}) string {
	if doctype == TransferLossHoldDoctype || (doctype == "PurchaseReturn" && strField(data, "reason_severity") == ReasonSeverityHigh) {
		return RoleAdmin
	}
	return ""
}

// reasonSeverityOf is the severity of a reason code for a category ("" when
// the code is blank or cannot be resolved).
func reasonSeverityOf(tenantID, code, category string) string {
	if strings.TrimSpace(code) == "" {
		return ""
	}
	snap, err := ResolveReasonCode(tenantID, code, category)
	if err != nil {
		return ""
	}
	return snap.Severity
}

// transferLossHold is what a held receipt is made of.
type transferLossHold struct {
	units map[string]int // sku -> units damaged, lost or short under a High code
	codes map[string]bool
}

func (h *transferLossHold) add(sku, code string, units int) {
	if h.units == nil {
		h.units, h.codes = map[string]int{}, map[string]bool{}
	}
	h.units[sku] += units
	h.codes[code] = true
}

// holdTransferReceipt parks a receipt for approval and returns the result the
// receiver sees. A retry carrying the same receipt key finds the hold it made.
func holdTransferReceipt(tenantID, transferOrderID, userID string, req TransferReceiptRequest, hold *transferLossHold, data map[string]interface{}, status string) (*TransferMovementResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	pending := func(id string) *TransferMovementResult {
		return &TransferMovementResult{TransferOrderID: transferOrderID, Status: status, PendingApproval: true, ApprovalID: id}
	}
	if key := strings.TrimSpace(req.ReceiptKey); key != "" {
		var id string
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
			AND data->>'transfer_order_id' = $2 AND data->>'receipt_key' = $3 AND status IN ('Pending Approval', 'Approved', 'Posted') LIMIT 1`, schema),
			TransferLossHoldDoctype, transferOrderID, key).Scan(&id)
		if err == nil {
			return pending(id), nil
		}
	}
	var value float64
	var codes []string
	for sku, n := range hold.units {
		if paise, ok := ResolveQuoteUnitCostPaise(tenantID, sku); ok {
			value += float64(paise) * float64(n) / 100
		}
	}
	for c := range hold.codes {
		codes = append(codes, c)
	}
	reqJSON, _ := json.Marshal(req)
	id := NewDocID("TLH")
	raw, _ := json.Marshal(map[string]interface{}{
		"code": id, "transfer_order_id": transferOrderID, "receipt_key": strings.TrimSpace(req.ReceiptKey),
		"from_warehouse": strField(data, "from_warehouse"), "to_warehouse": strField(data, "to_warehouse"),
		"reason_codes": strings.Join(codes, ", "), "amount": round2(value), "receipt": string(reqJSON),
		"requested_by": userID, "status": "Draft"})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		id, TransferLossHoldDoctype, raw, userID); err != nil {
		return nil, err
	}
	if err := SubmitForApproval(tenantID, TransferLossHoldDoctype, id, userID, ""); err != nil {
		_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = $1 AND id = $2 AND status = 'Draft'`, schema), TransferLossHoldDoctype, id)
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "HOLD_TRANSFER_RECEIPT", "PENDING",
		fmt.Sprintf("Receipt on transfer order %s waits for approval %s: High-severity reason (%s)", transferOrderID, id, strings.Join(codes, ", ")))
	return pending(id), nil
}

// executeTransferLossHold posts an approved hold. The receipt posts exactly as
// it would have, once: the hold's own key makes a second run a no-op. A
// refusal (the transfer moved on meanwhile) stays on the hold as post_error.
func executeTransferLossHold(tenantID, holdID, approver string) {
	fail := func(err error) {
		if schema, e := db.GetTenantSchema(tenantID); e == nil {
			_ = mergeDocData(schema, TransferLossHoldDoctype, holdID, map[string]interface{}{"post_error": err.Error()})
		}
		LogSystemError(tenantID, approver, "WARN", TransferLossHoldDoctype, fmt.Sprintf("%s: %v", holdID, err), "")
	}
	h, _, err := fetchDocData(tenantID, TransferLossHoldDoctype, holdID)
	if err != nil || h == nil {
		fail(fmt.Errorf("hold %s not found", holdID))
		return
	}
	var req TransferReceiptRequest
	if err := json.Unmarshal([]byte(strField(h, "receipt")), &req); err != nil {
		fail(err)
		return
	}
	if strings.TrimSpace(req.ReceiptKey) == "" {
		req.ReceiptKey = "hold:" + holdID
	}
	req.approvedHold = holdID
	res, err := ReceiveTransferOrderLines(tenantID, strField(h, "transfer_order_id"), strField(h, "requested_by"), req)
	if err != nil {
		fail(err)
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		fail(err)
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Posted', data = data || jsonb_build_object('status', 'Posted', 'event_no', $3::int, 'post_error', ''), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $1 AND id = $2`, schema), TransferLossHoldDoctype, holdID, res.EventNo); err != nil {
		fail(err)
	}
}
