package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// ItemException is the blueprint's section 18.1 exception queue (Stage 72.5):
// one record per thing a store manager must look at - a component that went
// short, a non-sellable item someone tried to bill, a stored-value posting
// that needs review. Inventory -> Item Exceptions lists them; a manager sets
// Status to Resolved with a comment. Written by the engine only.
type ItemExceptionInput struct {
	ProcessType   string // ItemException* below
	SourceDoctype string
	SourceID      string
	SKU           string
	Location      string
	Code          string
	Message       string
	Qty           int
	Status        string // "" = Open
	// DedupKey makes the record idempotent: the same key writes once, so a
	// cart re-quoted ten times logs one attempt, not ten.
	DedupKey string
}

const (
	ItemExceptionShortStock    = "Backflush short stock"
	ItemExceptionNotSellable   = "Non-sellable POS attempt"
	ItemExceptionPromoOnly     = "Promo-only POS attempt"
	ItemExceptionStoredValue   = "Stored value"
	ItemExceptionNoMapping     = "Backflush mapping missing"
	ItemExceptionStatusOpen    = "Open"
	ItemExceptionStatusLogged  = "Logged"
	ItemExceptionStatusDone    = "Resolved"
	itemExceptionDoctype       = "ItemException"
	itemExceptionIDPrefix      = "IEX-"
	itemExceptionMessageMaxLen = 500
)

// RecordItemException writes one exception. Best-effort by design: it runs
// after the transaction it describes has committed (or been refused), and a
// failure to record must never undo or block that - it is logged instead.
func RecordItemException(tenantID string, in ItemExceptionInput) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	status := in.Status
	if status == "" {
		status = ItemExceptionStatusOpen
	}
	id := NewDocIDCompact("IEX")
	if in.DedupKey != "" {
		sum := sha256.Sum256([]byte(tenantID + "|" + in.DedupKey))
		id = itemExceptionIDPrefix + strings.ToUpper(hex.EncodeToString(sum[:8]))
	}
	msg := in.Message
	if len(msg) > itemExceptionMessageMaxLen {
		msg = msg[:itemExceptionMessageMaxLen]
	}
	data, _ := json.Marshal(map[string]interface{}{
		"process_type": in.ProcessType, "source_doctype": in.SourceDoctype, "source_id": in.SourceID,
		"sku": in.SKU, "location": in.Location, "error_code": in.Code, "message": msg,
		"qty": in.Qty, "attempt_count": 1, "status": status,
	})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, $2, $3, $4, 'system') ON CONFLICT DO NOTHING`, schema), id, itemExceptionDoctype, data, status); err != nil {
		LogSystemError(tenantID, "", "ERROR", "RecordItemException", fmt.Sprintf("could not record %s for %s %s: %v", in.ProcessType, in.SourceDoctype, in.SourceID, err), "")
	}
}

// validateBackflushRule checks a BackflushRule on save (MASTER-0408).
func validateBackflushRule(tenantID, docID string, payload map[string]interface{}) error {
	fail := func(sub, msg string) error {
		return &ValidationError{Code: "MASTER-0408", SubFor: sub, Message: msg}
	}
	basisRaw := strings.TrimSpace(pimString(payload["basis"]))
	basis := normalizeBackflushBasis(basisRaw)
	if basisRaw != "" && policyKey(basisRaw) != policyKey(basis) {
		return fail("basis", "Applies to must be Item, Attribute, Category, Item Type or All items")
	}
	value := strings.TrimSpace(pimString(payload["basis_value"]))
	switch basis {
	case BackflushBasisAll:
		if value != "" {
			payload["basis_value"] = ""
		}
	case BackflushBasisItem:
		if value == "" {
			return fail("basis_value", "name the item this rule applies to")
		}
		if item, err := ResolveItemBySKU(tenantID, value); err != nil || item == nil {
			return fail("basis_value", fmt.Sprintf("no item %q - use its code or barcode", value))
		}
	case BackflushBasisCategory:
		if value == "" {
			return fail("basis_value", "name the category this rule applies to")
		}
		if !categoryExists(tenantID, value) {
			return fail("basis_value", fmt.Sprintf("no category %q - use a Category from Master Data", value))
		}
	case BackflushBasisItemType:
		if value == "" {
			return fail("basis_value", "name the item type this rule applies to")
		}
		if data, _, err := fetchDocData(tenantID, "ItemType", value); err != nil || data == nil {
			return fail("basis_value", fmt.Sprintf("no item type %q", value))
		}
	case BackflushBasisAttribute:
		if k, v, ok := strings.Cut(value, ":"); !ok || strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			return fail("basis_value", "write the attribute as Name:Value, for example Size:Large")
		}
	}
	component := strings.TrimSpace(pimString(payload["component_item"]))
	if component == "" {
		return fail("component_item", "choose the item that is consumed (the box, bag or paper)")
	}
	compItem, err := ResolveItemBySKU(tenantID, component)
	if err != nil || compItem == nil {
		return fail("component_item", fmt.Sprintf("no item %q", component))
	}
	if !ItemPolicyFor(tenantID, compItem.Data).Tracked() {
		return &ValidationError{Code: "MASTER-0405", SubFor: "component_item", Message: fmt.Sprintf(
			"%s is not stock tracked, so there is no store stock to consume - give it a stock-tracked item type (for example Packaging)", itemLabel(compItem.Data, component))}
	}
	if raw := strings.TrimSpace(pimString(payload["qty_per_unit"])); raw != "" {
		q := numFromInterface(payload["qty_per_unit"])
		if q < 1 || q != float64(int(q)) {
			return fail("qty_per_unit", "quantity per unit sold is a whole number of 1 or more (stock is counted in whole units)")
		}
	}
	from, to := ruleDay(pimString(payload["effective_from"])), ruleDay(pimString(payload["effective_to"]))
	if from != "" && to != "" && to < from {
		return fail("effective_to", "Effective to is before Effective from")
	}
	return nil
}

func categoryExists(tenantID, value string) bool {
	if data, _, err := fetchDocData(tenantID, "Category", value); err == nil && data != nil {
		return true
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var exists bool
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(
		SELECT 1 FROM %[1]s.documents WHERE doctype = 'Category' AND deleted_at IS NULL AND lower(data->>'name') = lower($1)
		UNION ALL SELECT 1 FROM %[1]s.documents WHERE doctype = 'Item' AND deleted_at IS NULL AND lower(data->>'category') = lower($1))`, schema), value).Scan(&exists)
	return exists
}
