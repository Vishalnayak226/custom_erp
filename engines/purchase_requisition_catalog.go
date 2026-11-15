package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PreparePurchaseRequisition owns the values that must not depend on a
// browser: a new requisition number comes from the tenant's PR Prefix Config,
// and entered description text is normalized before the successful save adds
// it to the reusable suggestion master. Callers may not choose a PR number on
// creation; this avoids duplicate or skipped browser-side counters while
// still leaving Prefix Configs as the one place an administrator controls the
// series.
func PreparePurchaseRequisition(tenantID, location string, isCreate bool, payload map[string]interface{}) error {
	_, err := DrawPurchaseRequisition(tenantID, location, isCreate, payload)
	return err
}

// DrawPurchaseRequisition is PreparePurchaseRequisition that also returns
// where the PR number came from, so a refused save gives it back (57.26 d).
func DrawPurchaseRequisition(tenantID, location string, isCreate bool, payload map[string]interface{}) (*NumberDraw, error) {
	if rawDescription, ok := payload["description"]; ok && rawDescription != nil {
		description := strings.TrimSpace(fmt.Sprintf("%v", rawDescription))
		payload["description"] = description
	}

	if err := PrepareRequisitionLines(tenantID, isCreate, payload); err != nil { // 148.14
		return nil, err
	}
	if !isCreate {
		return nil, nil
	}
	if strings.TrimSpace(location) == "" {
		location = "HQ"
	}
	code, draw, err := drawSequence(tenantID, "PR", location, purchaseRequisitionFinancialYear(time.Now()))
	if err != nil {
		return nil, err
	}
	payload["code"] = code
	return draw, nil
}

// EnsurePurchaseRequisitionDescription saves a canonical requirement phrase
// once. The full SHA-256-based ID is deterministic and tenant-local, so two
// concurrent saves of the same wording converge on one Master record without
// inventing another user-visible number series.
func EnsurePurchaseRequisitionDescription(tenantID, description string) error {
	description = strings.TrimSpace(description)
	if description == "" {
		return nil
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	canonical := strings.ToLower(description)
	digest := sha256.Sum256([]byte(canonical))
	id := "PRDESC-" + hex.EncodeToString(digest[:])
	payload, err := json.Marshal(map[string]interface{}{
		"code":        id,
		"description": description,
		"status":      "Active",
	})
	if err != nil {
		return err
	}

	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'PurchaseRequisitionDescription', $2, 'Active', 'system')
		ON CONFLICT (id) DO NOTHING`, schema), id, payload)
	return err
}

// purchaseRequisitionFinancialYear is the same April-March year every other
// generated number uses; kept as a named wrapper because PR's own tests pin
// this behavior by this name.
func purchaseRequisitionFinancialYear(now time.Time) string {
	return documentFinancialYear(now)
}
