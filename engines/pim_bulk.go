package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const maxPIMBulkEditDocuments = 100

// IsPIMBulkEditableDoctype keeps the PIM bulk-edit endpoint from becoming a
// back door to every generic doctype. Item is included because it is the PIM
// catalog's source of truth even though its legacy module assignment predates
// the PIM module; every other eligible type must belong to module_key=pim.
func IsPIMBulkEditableDoctype(tenantID, doctype string) (bool, error) {
	if doctype == "Item" {
		return true, nil
	}
	moduleKey, err := ModuleForDoctype(tenantID, doctype)
	if err != nil {
		return false, err
	}
	return moduleKey == "pim", nil
}

// BulkUpdateDocuments applies one editable field to a bounded selection of
// PIM documents as one database transaction. It merges the field into each
// existing document (rather than replacing its JSON body), validates every
// resulting record before writing any of them, and preserves the generic
// update path's re-approval-on-edit behavior for approved documents.
func BulkUpdateDocuments(tenantID, doctype string, ids []string, field string, value interface{}, editorUserID, editorRole string) ([]string, error) {
	if strings.TrimSpace(doctype) == "" {
		return nil, fmt.Errorf("doctype is required")
	}
	if field == "" || field == "id" || field == "code" {
		return nil, fmt.Errorf("field %q cannot be bulk edited", field)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("select at least one document")
	}
	if len(ids) > maxPIMBulkEditDocuments {
		return nil, fmt.Errorf("bulk edit supports at most %d documents at a time", maxPIMBulkEditDocuments)
	}

	eligible, err := IsPIMBulkEditableDoctype(tenantID, doctype)
	if err != nil {
		return nil, err
	}
	if !eligible {
		return nil, fmt.Errorf("bulk edit is only available for PIM documents and Item")
	}

	fields, err := GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		return nil, err
	}
	fieldExists := false
	for _, meta := range fields {
		if meta.Fieldname == field {
			fieldExists = true
			break
		}
	}
	if !fieldExists {
		return nil, fmt.Errorf("field %q is not defined for %s", field, doctype)
	}

	seen := make(map[string]struct{}, len(ids))
	orderedIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("document id cannot be empty")
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("duplicate document id %q", id)
		}
		seen[id] = struct{}{}
		orderedIDs = append(orderedIDs, id)
	}
	sort.Strings(orderedIDs) // deterministic lock order prevents batch deadlocks

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	isApprovalGated, err := IsApprovalGated(tenantID, doctype)
	if err != nil {
		return nil, err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}

	type pendingUpdate struct {
		id          string
		data        map[string]interface{}
		priorStatus string
		newStatus   string
	}
	updates := make([]pendingUpdate, 0, len(orderedIDs))

	for _, id := range orderedIDs {
		var rawData, priorStatus string
		err := tx.QueryRow(fmt.Sprintf(`
			SELECT data, status FROM %s.documents
			WHERE doctype = $1 AND id = $2 FOR UPDATE`, schema), doctype, id).Scan(&rawData, &priorStatus)
		if err != nil {
			return nil, fmt.Errorf("document %q not found: %v", id, err)
		}
		data := map[string]interface{}{}
		if err := json.Unmarshal([]byte(rawData), &data); err != nil {
			return nil, fmt.Errorf("document %q has invalid stored data: %v", id, err)
		}
		data[field] = value
		if err := ValidateDocument(tenantID, doctype, data); err != nil {
			return nil, fmt.Errorf("document %q: %v", id, err)
		}

		newStatus := priorStatus
		if field == "status" && value != nil {
			newStatus = fmt.Sprintf("%v", value)
		}
		if priorStatus == "Approved" && isApprovalGated {
			newStatus = "Pending Approval"
			data["status"] = newStatus
		}
		updates = append(updates, pendingUpdate{id: id, data: data, priorStatus: priorStatus, newStatus: newStatus})
	}

	// Variant uniqueness is normally checked on every Item save. A batch also
	// needs an in-memory check so two selected rows cannot be changed to the
	// same combination before either write is visible to the database query.
	if doctype == "Item" && (field == "parent_product_code" || field == "variant_option_values") {
		batchVariants := map[string]string{}
		for _, update := range updates {
			if err := ValidateItemVariantUniqueness(tenantID, update.id, update.data); err != nil {
				return nil, err
			}
			parent, _ := update.data["parent_product_code"].(string)
			options, _ := update.data["variant_option_values"].(string)
			parent = strings.TrimSpace(parent)
			options = normalizeVariantOptions(options)
			if parent == "" || options == "" {
				continue
			}
			key := parent + "\x00" + options
			if existingID, duplicate := batchVariants[key]; duplicate {
				return nil, fmt.Errorf("selected Items %q and %q would have the same variant combination under parent %q", existingID, update.id, parent)
			}
			batchVariants[key] = update.id
		}
	}

	// Extension hooks are part of the generic-save contract. Run every
	// before-save hook after all validation succeeds but before the transaction
	// writes, so a hook failure leaves the whole selected set unchanged.
	for _, update := range updates {
		if err := InvokeBeforeSaveHooks(tenantID, doctype, update.id, update.data); err != nil {
			return nil, err
		}
	}

	for _, update := range updates {
		encoded, err := json.Marshal(update.data)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP
			WHERE doctype = $3 AND id = $4`, schema), encoded, update.newStatus, doctype, update.id); err != nil {
			return nil, err
		}
		if update.priorStatus == "Approved" && isApprovalGated {
			amount := extractAmount(update.data)
			if _, err := tx.Exec(fmt.Sprintf(`
				INSERT INTO %s.approval_log (doctype, document_id, action, actor_user_id, actor_role, amount, comment)
				VALUES ($1, $2, 'Modified', $3, $4, $5, 'Reset to Pending Approval after bulk edit')`, schema), doctype, update.id, editorUserID, editorRole, amount); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for _, update := range updates {
		InvokeAfterSaveHooksAsync(tenantID, doctype, update.id, update.data)
	}
	return orderedIDs, nil
}
