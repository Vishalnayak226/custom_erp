package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 148.38 (D10): goods received before the PO. Off, a goods receipt must
// name a PO as it always has. On, a receipt with no PO takes a reason code
// (category "Received Before PO"), is marked, and gets a date its retro PO is
// due by. The retro PO is an ordinary PurchaseOrder carrying the receipt's id
// (retro_grn_id); it goes through the usual approval, and on approval the
// receipt takes its number. Until then no bill for those goods can be paid.
const (
	SettingGoodsBeforePO     = "procurement.goods_before_po"
	SettingGoodsBeforePODays = "procurement.goods_before_po_days"
	ReasonReceivedBeforePO   = "Received Before PO"
)

func init() {
	RegisterSetting(SettingDefinition{Key: SettingGoodsBeforePO, Module: "Procurement & GRN", Label: "Allow goods to be received before the PO",
		Type: SettingTypeBool, Default: "false",
		Description: "On: a goods receipt with no PO is accepted when it has a reason code (Received Before PO). Its retro PO goes for approval marked Received before PO, and no bill for those goods is paid until it is approved. Off: a receipt must name a PO."})
	RegisterSetting(SettingDefinition{Key: SettingGoodsBeforePODays, Module: "Procurement & GRN", Label: "Retro PO due within",
		Type: SettingTypeInt, Default: "7", Unit: "days", Min: settingBound(1), Max: settingBound(365),
		Description: "Days after a receipt with no PO by which its retro PO must be approved. After that the receipt shows as overdue on Goods received before PO."})
	RegisterReport(ReportDefinition{ID: "goods-before-po", Label: "Goods received before PO", Category: "Procurement",
		Columns: []ReportColumn{{Key: "grn", Label: "Receipt"}, {Key: "vendor", Label: "Vendor"}, {Key: "reason", Label: "Reason"},
			{Key: "due", Label: "PO due by"}, {Key: "days_overdue", Label: "Days overdue"}, {Key: "retro_po", Label: "Retro PO"}, {Key: "state", Label: "State"}},
		Run: goodsBeforePOReport})
}

// prepareGRNBeforePO runs for a new GRN that names no PO. Off it changes
// nothing, so the mandatory PO check refuses the receipt as today.
func prepareGRNBeforePO(tenantID string, payload map[string]interface{}) error {
	if !GetSettingBool(tenantID, SettingGoodsBeforePO) {
		delete(payload, "before_po_reason")
		return nil
	}
	code := strings.TrimSpace(strField(payload, "before_po_reason"))
	if code == "" {
		return &ValidationError{Code: "PURCHA-5030", SubFor: "before_po_reason", Message: "Choose why these goods came before a PO."}
	}
	snap, err := ResolveReasonCode(tenantID, code, ReasonReceivedBeforePO)
	if err != nil {
		return err
	}
	if strings.TrimSpace(strField(payload, "vendor")) == "" {
		return &ValidationError{Code: "PURCHA-5031", SubFor: "vendor", Message: "Choose the vendor for this receipt."}
	}
	days := GetSettingInt(tenantID, SettingGoodsBeforePODays)
	if days < 1 {
		days = 7
	}
	today, _ := time.Parse("2006-01-02", BusinessToday())
	payload["received_before_po"] = "Yes"
	payload["before_po_reason"] = snap.Code
	payload["before_po_due"] = today.AddDate(0, 0, days).Format("2006-01-02")
	// No PO to take a GST rate from, and the steps have no box for one: a line
	// that names none takes its item's, or the bill can never add up.
	if lines := grnLineMaps(payload); lines != nil {
		for _, l := range lines {
			if lineNumberSet(l, "gst_rate") {
				continue
			}
			if item, _, err := fetchDocData(tenantID, "Item", strField(l, "sku")); err == nil && lineNumberSet(item, "gst_rate") {
				l["gst_rate"] = numFromInterface(item["gst_rate"])
			}
		}
		if encoded, err := json.Marshal(lines); err == nil {
			payload["received_items"] = string(encoded)
		}
	}
	return nil
}

// KeepGRNBeforePOMarks is an edit's half of "the marks are the engine's": what
// is stored stands, so an edit neither lifts the bill block nor waives a PO.
func KeepGRNBeforePOMarks(tenantID, grnID string, payload map[string]interface{}) {
	prior, _, err := fetchDocData(tenantID, "GRN", grnID)
	for _, key := range []string{"received_before_po", "before_po_reason", "before_po_due", "retro_po_id"} {
		delete(payload, key)
		if err == nil && prior != nil {
			if v, ok := prior[key]; ok {
				payload[key] = v
			}
		}
	}
}

// grnBeforePOAllowed is the check ValidateDocument asks before it waives the
// mandatory PO of a GRN: the setting is on and the receipt carries a reason
// code of the right kind. It trusts nothing the caller sent.
func grnBeforePOAllowed(tenantID string, docData map[string]interface{}) bool {
	if strField(docData, "received_before_po") != "Yes" || !GetSettingBool(tenantID, SettingGoodsBeforePO) {
		return false
	}
	_, err := ResolveReasonCode(tenantID, strField(docData, "before_po_reason"), ReasonReceivedBeforePO)
	return err == nil
}

// ValidatePORetroLink checks a PO that names the receipt it is the retro PO
// for, and stamps it "Received before PO". A PO that names none loses the mark
// if a caller sent one.
func ValidatePORetroLink(tenantID string, payload map[string]interface{}) error {
	// Stage 148.39: only the reminder job escalates, never a caller's save.
	delete(payload, "escalated_to_role")
	delete(payload, "escalated_at")
	grnID := strings.TrimSpace(strField(payload, "retro_grn_id"))
	if grnID == "" {
		delete(payload, "received_before_po")
		return nil
	}
	grn, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil || grn == nil || strField(grn, "received_before_po") != "Yes" {
		return &ValidationError{Code: "PURCHA-5033", SubFor: "retro_grn_id", Message: fmt.Sprintf("receipt %s was not received before a PO", grnID)}
	}
	vendor := firstNonEmpty(strField(payload, "vendor"), strField(payload, "vendor_id"))
	if v := strField(grn, "vendor"); v != "" && vendor != "" && v != vendor {
		return &ValidationError{Code: "PURCHA-5034", SubFor: "vendor", Message: fmt.Sprintf("receipt %s is from vendor %s, not %s", grnID, v, vendor)}
	}
	if linked := strField(grn, "retro_po_id"); linked != "" && linked != strField(payload, "id") && linked != strField(payload, "code") {
		return &ValidationError{Code: "PURCHA-5035", SubFor: "retro_grn_id", Message: fmt.Sprintf("receipt %s already has its PO %s", grnID, linked)}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var other string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL
		AND data->>'retro_grn_id' = $1 AND status NOT IN ('Rejected', 'Cancelled') AND id <> $2 AND id <> $3 LIMIT 1`, schema),
		grnID, strField(payload, "id"), strField(payload, "code")).Scan(&other)
	if err == nil {
		return &ValidationError{Code: "PURCHA-5035", SubFor: "retro_grn_id", Message: fmt.Sprintf("receipt %s already has its PO %s", grnID, other)}
	}
	if err != sql.ErrNoRows {
		return err
	}
	payload["received_before_po"] = "Yes"
	return nil
}

func init() {
	// On approval the receipt takes its PO, so the bill matches against it.
	RegisterApprovalDecisionHook("PurchaseOrder", false, func(tenantID, docID, decision, actorUserID string) {
		if decision != "Approved" {
			return
		}
		po, _, err := fetchDocData(tenantID, "PurchaseOrder", docID)
		if err != nil || po == nil {
			return
		}
		grnID := strField(po, "retro_grn_id")
		if grnID == "" {
			return
		}
		grn, _, err := fetchDocData(tenantID, "GRN", grnID)
		if err != nil || grn == nil || strField(grn, "received_before_po") != "Yes" {
			return
		}
		schema, err := db.GetTenantSchema(tenantID)
		if err != nil {
			return
		}
		lines := grnLineMaps(grn)
		for _, l := range lines {
			l["po_id"] = docID
		}
		patch := map[string]interface{}{"po_id": docID, "po_ids": docID, "retro_po_id": docID}
		if len(lines) > 0 {
			tmp := map[string]interface{}{}
			encodeGRNLines(tmp, lines)
			for k, v := range tmp {
				patch[k] = v
			}
		}
		if err := mergeDocData(schema, "GRN", grnID, patch); err != nil {
			LogSystemError(tenantID, actorUserID, "WARN", "RetroPOLink", fmt.Sprintf("receipt %s not linked to PO %s: %v", grnID, docID, err), "")
			return
		}
		SyncGRNLineRows(tenantID, grnID)
		LogAuditEvent(tenantID, actorUserID, "RETRO_PO_LINKED", "SUCCESS", fmt.Sprintf("PO %s approved as the retro PO of receipt %s", docID, grnID))
		matchRetroPOBill(tenantID, actorUserID, schema, grnID, docID)
	})
}

// matchRetroPOBill gives the bill made while the receipt had no PO its PO. The
// bill a receipt in steps made for itself then matches, as it would have at
// posting; a bill keyed by hand is matched by hand, as ever.
func matchRetroPOBill(tenantID, actorUserID, schema, grnID, poID string) {
	var billID string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL
		AND data->>'grn_id' = $1 AND COALESCE(data->>'po_id', '') = '' ORDER BY created_at LIMIT 1`, schema), grnID).Scan(&billID)
	if err != nil {
		return // no bill yet: posting makes it with the PO on it
	}
	warn := func(err error) {
		LogSystemError(tenantID, actorUserID, "WARN", "RetroPOLink", fmt.Sprintf("receipt %s: %v", grnID, err), "")
		_ = mergeDocData(schema, "GRN", grnID, map[string]interface{}{"posting_warning": err.Error()})
	}
	if err := mergeDocData(schema, "VendorInvoice", billID, map[string]interface{}{"po_id": poID}); err != nil {
		warn(err)
		return
	}
	grn, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil || grn == nil || !IsGRNStepFlow(grn) {
		return
	}
	if _, _, err := ensureStepGRNVendorBill(tenantID, actorUserID, grnID, grn); err != nil {
		warn(err)
	}
}

// retroPOBlocksBill refuses to pay a bill whose goods came before a PO that is
// not yet approved. Every payment path calls it with the bill's own data.
func retroPOBlocksBill(tenantID string, bill map[string]interface{}) error {
	block := func() error {
		return &ValidationError{Code: "PURCHA-5032", Message: "This bill is for goods received before a PO."}
	}
	if grnID := strField(bill, "grn_id"); grnID != "" {
		grn, _, err := fetchDocData(tenantID, "GRN", grnID)
		if err == nil && grn != nil && strField(grn, "received_before_po") == "Yes" && strField(grn, "retro_po_id") == "" {
			return block()
		}
	}
	if poID := strField(bill, "po_id"); poID != "" {
		po, status, err := fetchDocData(tenantID, "PurchaseOrder", poID)
		if err == nil && po != nil && strField(po, "received_before_po") == "Yes" && status != "Approved" && status != "Closed" {
			return block()
		}
	}
	return nil
}

func goodsBeforePOReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	return dayLimitRows(tenantID, `SELECT g.id, COALESCE(g.data->>'vendor', ''), COALESCE(g.data->>'before_po_reason', ''),
		COALESCE(g.data->>'before_po_due', ''), COALESCE(g.data->>'retro_po_id', ''),
		COALESCE((SELECT p.status FROM {s}.documents p WHERE p.doctype = 'PurchaseOrder' AND p.deleted_at IS NULL
			AND p.data->>'retro_grn_id' = g.id AND p.status NOT IN ('Rejected', 'Cancelled') ORDER BY p.created_at DESC LIMIT 1), '')
		FROM {s}.documents g WHERE g.doctype = 'GRN' AND g.deleted_at IS NULL AND g.data->>'received_before_po' = 'Yes'
		ORDER BY g.data->>'before_po_due', g.id LIMIT 20001`, nil,
		func(scan func(...interface{}) error) (map[string]interface{}, error) {
			var id, vendor, reason, due, retro, poStatus string
			if err := scan(&id, &vendor, &reason, &due, &retro, &poStatus); err != nil {
				return nil, err
			}
			state, overdue := "Linked", int64(0)
			if retro == "" {
				state = "Waiting for PO"
				if poStatus != "" {
					state = "PO " + poStatus
				}
				if today := BusinessToday(); due != "" && due < today {
					state = "Overdue"
					d1, _ := time.Parse("2006-01-02", due)
					d2, _ := time.Parse("2006-01-02", today)
					overdue = int64(d2.Sub(d1).Hours() / 24)
				}
			}
			return map[string]interface{}{"grn": id, "vendor": vendor, "reason": reason, "due": due, "days_overdue": overdue, "retro_po": retro, "state": state}, nil
		})
}
