package engines

import (
	"fmt"
	"strings"
)

// Stage 130.9 (72.D a, user decision 2026-10-22): item release approval. With
// "New items need approval before they are sold" on, an item made on the Item
// screen starts as Draft and goes straight to the Item approval rule's
// approver; approved, it is Active, rejected, Blocked. Until it is Active the
// till refuses it (posSellabilityGate) and an order refuses it (orders.go
// already takes Active items only). Items an existing flow makes with a
// document - a PO's or a goods receipt's design, the bulk purchase upload -
// are released with that document. Off by default: turning it on without an
// approver would only stop the counter.

const (
	SettingItemReleaseApproval = "inventory.item_release_approval"
	ItemStatusBlocked          = "Blocked"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingItemReleaseApproval, Module: "Inventory",
		Label: "New items need approval before they are sold", Type: SettingTypeBool, Default: "false",
		Description: "On: an item made on the Item screen starts as Draft and goes to the Item approval rule's approver (Settings > Approval Rules - set one first); it can be sold once approved, and is Blocked if rejected. Items made by a PO, a goods receipt or the bulk upload are released with it. Items that exist today are not touched.",
	})
	RegisterApprovalDecisionHook("Item", false, releaseItemOnDecision)
}

// itemAwaitingRelease is a status an item cannot be sold in.
func itemAwaitingRelease(status string) bool {
	switch strings.TrimSpace(status) {
	case "Draft", "Pending Approval", "Rejected", ItemStatusBlocked:
		return true
	}
	return false
}

// ItemReleaseApprovalOn reports the setting.
func ItemReleaseApprovalOn(tenantID string) bool {
	return GetSettingBool(tenantID, SettingItemReleaseApproval)
}

// PrepareItemRelease runs on an Item save from the Item screen: a new item
// starts as Draft (refused when no one could approve it), and an item still
// waiting for release cannot be made Active, or anything else, by an edit.
func PrepareItemRelease(tenantID string, isCreate bool, priorStatus string, payload map[string]interface{}) error {
	if !ItemReleaseApprovalOn(tenantID) {
		return nil
	}
	if isCreate {
		if role, _, err := LowestApproverRoleForDoctype(tenantID, "Item"); err != nil {
			return err
		} else if role == "" {
			return &ValidationError{Code: "MASTER-1302", SubFor: "status", Message: "new items need approval, but no one approves items yet - add an Item rule under Settings > Approval Rules first"}
		}
		payload["status"] = "Draft"
		return nil
	}
	if !itemAwaitingRelease(priorStatus) {
		return nil
	}
	if s := strings.TrimSpace(fmt.Sprintf("%v", payload["status"])); payload["status"] != nil && s != "" && s != priorStatus {
		return &ValidationError{Code: "MASTER-1302", SubFor: "status", Message: fmt.Sprintf("this item is %s - only its approval releases it", priorStatus)}
	}
	return nil
}

// releaseItemOnDecision turns the approval engine's Approved / Rejected into
// the item's own Active / Blocked.
func releaseItemOnDecision(tenantID, docID, decision, actorUserID string) {
	data, _, _, err := fetchDocument(tenantID, "Item", docID)
	if err != nil {
		return
	}
	status := "Active"
	if decision == "Rejected" {
		status = ItemStatusBlocked
	}
	if err := setDocumentStatus(tenantID, "Item", docID, status, data); err != nil {
		LogSystemError(tenantID, "", "ERROR", "releaseItemOnDecision", fmt.Sprintf("item %s: %v", docID, err), "")
		return
	}
	LogAuditEvent(tenantID, actorUserID, "ITEM_RELEASE_"+strings.ToUpper(decision), "SUCCESS", fmt.Sprintf("Item %s is %s", docID, status))
}

// itemReleaseGate is the till's refusal of an item not yet released.
func itemReleaseGate(item map[string]interface{}, sku string) error {
	status := pimString(item["status"])
	if !itemAwaitingRelease(status) {
		return nil
	}
	return &ValidationError{Code: "POSOFF-1303", SubFor: sku, Message: fmt.Sprintf("%s is %s - it can be sold once its approval releases it", posLineLabel(item, sku), status)}
}
