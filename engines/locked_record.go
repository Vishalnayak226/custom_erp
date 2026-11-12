package engines

import (
	"encoding/json"
	"fmt"

	"custom_erp/db"
)

// Stage 162.3: one locked-record message for every document whose body is
// fixed once it has gone out or posted. Two doors lead to it - a record-form
// save (ValidateTransactionalRules) and a PUT / PATCH on the generic route
// (LockedRecordMessage, asked by the handler) - so a dispatched challan no
// longer answers a bare 405 while a posted invoice explains itself.

type lockedRecordRule struct {
	statuses []string
	// next names what to do instead, as the message ends it.
	next string
	// bodyLocked: every field is fixed, not only the money fields.
	bodyLocked bool
}

var lockedRecordRules = map[string]lockedRecordRule{
	DeliveryChallanDoctype: {[]string{"Dispatched", "Partly Returned"}, "receive a return or cancel the challan", true},
	"PurchaseReturn":       {[]string{"Posted"}, "its stock and debit note are already booked - raise a new return or a credit note", false},
}

func lockedRecordApplies(doctype, status string) (lockedRecordRule, bool) {
	rule, ok := lockedRecordRules[doctype]
	if !ok {
		return rule, false
	}
	for _, s := range rule.statuses {
		if s == status {
			return rule, true
		}
	}
	return rule, false
}

func lockedRecordText(doctype, docID, status, next string) string {
	return fmt.Sprintf("%s %s is %s and can no longer be edited - %s", doctype, docID, status, next)
}

// validateLockedRecordEdit refuses a save that changes a locked document.
// Saving the same values again is not a change.
func validateLockedRecordEdit(doctype, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	rule, ok := lockedRecordApplies(doctype, priorStatus)
	if !ok || !rule.bodyLocked || priorData == nil {
		return nil
	}
	// A key the payload does not carry is not a change (a partial save).
	for k, v := range payload {
		if saleEditIgnored[k] {
			continue
		}
		if fmt.Sprint(normaliseEditValue(v)) != fmt.Sprint(normaliseEditValue(priorData[k])) {
			return &ValidationError{Code: "GLOBAL-0019", Message: lockedRecordText(doctype, docID, priorStatus, rule.next)}
		}
	}
	return nil
}

// LockedRecordMessage is what a generic PUT / PATCH on a stored document is
// told when the document is locked, or "" when it is not (the caller then
// answers 405 as before). It covers the documents above and every document
// the posted-amounts rules know.
func LockedRecordMessage(tenantID, doctype, docID string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || docID == "" {
		return ""
	}
	var status, dataStr string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(status, ''), data FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema),
		doctype, docID).Scan(&status, &dataStr) != nil {
		return ""
	}
	if rule, ok := lockedRecordApplies(doctype, status); ok {
		return lockedRecordText(doctype, docID, status, rule.next)
	}
	if rule, ok := postedAmountRules[doctype]; ok {
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		if rule.posted(status, data) {
			return fmt.Sprintf("%s %s has posted to the accounts and can no longer be edited this way - correct it with a credit or debit note, or reverse it", doctype, docID)
		}
	}
	return ""
}
