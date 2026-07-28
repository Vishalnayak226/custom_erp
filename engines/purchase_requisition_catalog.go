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
	if rawDescription, ok := payload["description"]; ok && rawDescription != nil {
		description := strings.TrimSpace(fmt.Sprintf("%v", rawDescription))
		payload["description"] = description
	}

	if !isCreate {
		return nil
	}
	if strings.TrimSpace(location) == "" {
		location = "HQ"
	}
	code, err := GenerateSequence(tenantID, "PR", location, purchaseRequisitionFinancialYear(time.Now()))
	if err != nil {
		return err
	}
	payload["code"] = code
	return nil
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

func purchaseRequisitionFinancialYear(now time.Time) string {
	startYear := now.Year()
	if now.Month() < time.April {
		startYear--
	}
	return fmt.Sprintf("%02d-%02d", startYear%100, (startYear+1)%100)
}
