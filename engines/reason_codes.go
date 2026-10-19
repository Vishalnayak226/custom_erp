package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 74.1.1 (BRD 4.9, FR-RSN-01..05): one governed reason-code catalogue.
// The ReasonCode master (category = the action a code is for) gained
// effective dates, an evidence flag, a loss class and "when to use"
// (migrations_stage74_1_1_reason_codes.sql). ResolveReasonCode is the one
// check every exception goes through - requireActiveReasonCode (the existing
// 19 callers) included - so a code that is inactive, of another category or
// outside its dates is refused at submit and again at post.
//
// Where an exception was taken as free text (a GRN line rejected or damaged,
// a purchase or sales return, a price override, a reprint), the code is
// offered beside the text: ReasonForException stores the code and a snapshot
// of its label, and the client setting reasons.codes_required makes the code
// mandatory. It is off by default, so existing clients and integrations go on
// as before (NFR-15); the screens always offer the codes.

// Categories added by Stage 74.
const (
	ReasonGRNRejection    = "Goods Receipt Rejection"
	ReasonGRNDamage       = "Goods Receipt Damage"
	ReasonPurchaseReturn  = "Purchase Return"
	ReasonSalesReturn     = "Sales Return"
	ReasonPriceOverride   = "Price Override"
	ReasonReprint         = "Reprint"
	ReasonInventoryAdjust = "Inventory Adjustment"
	ReasonMaterialIssue   = "Material Issue"
	ReasonDeliveryChallan = "Delivery Challan"
)

const SettingReasonCodesRequired = "reasons.codes_required"

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingReasonCodesRequired, Module: "Platform",
		Label: "Reasons must be picked from the reason codes", Type: SettingTypeBool, Default: "false",
		Description: "When on, a rejected or damaged GRN line, a purchase or sales return, a price override and a reprint all need a reason code (Setup > Reason Codes) - a typed reason alone is refused. When off, the code is offered and a typed reason is still accepted.",
	})
}

// ReasonSnapshot is what an event stores: the code and the label it had
// when used, so a renamed or retired code still reads as it was.
type ReasonSnapshot struct {
	Code             string `json:"code"`
	Label            string `json:"label"`
	Category         string `json:"category"`
	LossClass        string `json:"loss_class,omitempty"`
	EvidenceRequired bool   `json:"evidence_required,omitempty"`
}

// ResolveReasonCode checks a code for a category and returns its snapshot.
func ResolveReasonCode(tenantID, code, category string) (ReasonSnapshot, error) {
	var s ReasonSnapshot
	code = strings.TrimSpace(code)
	if code == "" {
		return s, &ValidationError{Code: "MASTER-0975", SubFor: category, Message: "choose a reason"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return s, err
	}
	var status, dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(status, ''), data FROM %s.documents WHERE doctype = 'ReasonCode' AND (id = $1 OR data->>'code' = $1) AND deleted_at IS NULL LIMIT 1`, schema), code).Scan(&status, &dataStr)
	if err == sql.ErrNoRows {
		return s, &ValidationError{Code: "MASTER-0975", SubFor: category, Message: fmt.Sprintf("reason code %q not found", code)}
	}
	if err != nil {
		return s, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return s, err
	}
	if status != "Active" {
		return s, &ValidationError{Code: "MASTER-0975", SubFor: category, Message: fmt.Sprintf("reason code %q is not Active", code)}
	}
	if c := strField(data, "category"); c != category {
		return s, &ValidationError{Code: "MASTER-0975", SubFor: category, Message: fmt.Sprintf("reason code %q is for %q, not %q", code, c, category)}
	}
	today := time.Now().Format("2006-01-02")
	if from := strField(data, "effective_from"); from != "" && today < from {
		return s, &ValidationError{Code: "MASTER-0975", SubFor: category, Message: fmt.Sprintf("reason code %q may be used from %s", code, from)}
	}
	if to := strField(data, "effective_to"); to != "" && today > to {
		return s, &ValidationError{Code: "MASTER-0975", SubFor: category, Message: fmt.Sprintf("reason code %q was retired on %s", code, to)}
	}
	return ReasonSnapshot{Code: firstNonEmpty(strField(data, "code"), code), Label: strField(data, "description"), Category: category,
		LossClass: strField(data, "loss_class"), EvidenceRequired: strField(data, "evidence_required") == "Yes"}, nil
}

// ReasonCodesRequired reports whether this client makes the codes mandatory.
func ReasonCodesRequired(tenantID string) bool {
	return GetSettingBool(tenantID, SettingReasonCodesRequired)
}

// ReasonForException is the one rule for an exception that takes a reason:
// a code given is checked and its label fills a blank text; no code is
// refused only when the client requires codes. It returns the code (or "")
// and the text to store.
func ReasonForException(tenantID, category, code, text string) (string, string, error) {
	code, text = strings.TrimSpace(code), strings.TrimSpace(text)
	if code == "" {
		if ReasonCodesRequired(tenantID) {
			return "", text, &ValidationError{Code: "MASTER-0975", SubFor: category, Message: "choose a reason from the list"}
		}
		return "", text, nil
	}
	snap, err := ResolveReasonCode(tenantID, code, category)
	if err != nil {
		return "", text, err
	}
	if text == "" {
		text = snap.Label
	}
	return snap.Code, text, nil
}

// PrepareGRNReasons gives each rejected or damaged GRN line its reason code
// (74.1.1): a code chosen is checked and fills a blank reason; none is refused
// only when the client requires codes. Runs on every new GRN, one-step or in
// steps, before validateGRNRules asks for the reason text.
func PrepareGRNReasons(tenantID string, isCreate bool, payload map[string]interface{}) error {
	if !isCreate {
		return nil
	}
	lines := grnLineMaps(payload)
	if len(lines) == 0 {
		return nil
	}
	for _, l := range lines {
		if err := applyLineReason(tenantID, l, "rejected_qty", "rejection_reason", "rejection_reason_code", ReasonGRNRejection); err != nil {
			return err
		}
		if err := applyLineReason(tenantID, l, "damaged_qty", "damage_reason", "damage_reason_code", ReasonGRNDamage); err != nil {
			return err
		}
	}
	encodeGRNLines(payload, lines)
	return nil
}

// applyLineReason sets one line's reason and code for a quantity set aside.
func applyLineReason(tenantID string, l map[string]interface{}, qtyKey, textKey, codeKey, category string) error {
	if numFromInterface(l[qtyKey]) <= 0 {
		return nil
	}
	code, text, err := ReasonForException(tenantID, category, strField(l, codeKey), strField(l, textKey))
	if err != nil {
		return err
	}
	if code != "" {
		l[codeKey] = code
	} else {
		delete(l, codeKey)
	}
	l[textKey] = text
	return nil
}

// normalizeReprintReason is the reprint reason both sticker print paths
// share (74.1.1): a Reprint reason code becomes "CODE: label"; a typed reason
// is refused only when the client requires codes; no reason is a first print.
func normalizeReprintReason(tenantID, reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", nil
	}
	if snap, err := ResolveReasonCode(tenantID, reason, ReasonReprint); err == nil {
		return snap.Code + ": " + snap.Label, nil
	}
	if ReasonCodesRequired(tenantID) {
		return "", &ValidationError{Code: "MASTER-0975", SubFor: ReasonReprint, Message: "choose why it is printed again from the list"}
	}
	return reason, nil
}

// ReasonCodeOption is one entry of a reason picker.
type ReasonCodeOption struct {
	Code             string `json:"code"`
	Label            string `json:"label"`
	WhenToUse        string `json:"when_to_use,omitempty"`
	EvidenceRequired bool   `json:"evidence_required,omitempty"`
	LossClass        string `json:"loss_class,omitempty"`
}

// ListReasonCodes is a category's usable codes today, for a picker.
func ListReasonCodes(tenantID, category string) ([]ReasonCodeOption, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'code', id), COALESCE(data->>'description', ''), COALESCE(data->>'when_to_use', ''),
		COALESCE(data->>'evidence_required', '') = 'Yes', COALESCE(data->>'loss_class', '')
		FROM %s.documents WHERE doctype = 'ReasonCode' AND deleted_at IS NULL AND status = 'Active' AND data->>'category' = $1
		  AND COALESCE(NULLIF(data->>'effective_from', ''), '0000') <= $2 AND COALESCE(NULLIF(data->>'effective_to', ''), '9999') >= $2
		ORDER BY 2, 1`, schema), category, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReasonCodeOption{}
	for rows.Next() {
		var o ReasonCodeOption
		if err := rows.Scan(&o.Code, &o.Label, &o.WhenToUse, &o.EvidenceRequired, &o.LossClass); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
