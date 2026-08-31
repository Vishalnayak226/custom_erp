package engines

import (
	"custom_erp/db"
	"fmt"
)

// Stage 36.7.5: bulk catalog translation - the bulk workflow to populate
// 26.4.1's locale/channel overrides across many products at once, not a
// machine-translation integration.
//
// TRANSLATION SOURCE (scope call, the same local-only reasoning 26.4.11's
// governance decision already established for pim_content_assist.go): no
// external translation API, no new dependency, no network call, no
// supplier text repurposed without review. What this bulk-seeds is an
// ordinary Draft ProductContent row in the target language, its text
// copied verbatim from the source language's Approved content - a
// deliberate starting point for a human translator to overwrite, never
// text presented as already translated. The seeded row still needs a
// human to translate it and pass it through the existing approval gate
// (Draft -> Pending Approval -> Approved) before it can publish, exactly
// like every other ProductContent row - no new approval mechanism, no
// bypass of the one that already exists.

// TranslationSeedOutcome is one item's result - the same per-item "report
// exactly which succeeded/failed and why" shape every other PIM bulk
// action (36.2.5's task/workflow actions, 36.6.2's bulk media upload,
// 36.4.5's bulk channel pull) already uses, rather than a batch-aborting
// error.
type TranslationSeedOutcome struct {
	ItemCode  string `json:"item_code"`
	ContentID string `json:"content_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// fetchApprovedProductContent reads the fields a translation seed copies
// from - the Approved row only, since an unreviewed Draft or a Rejected
// one is not text this workflow should be propagating to every other
// product in the selection.
func fetchApprovedProductContent(tenantID, itemCode, language string) (title, shortDesc, longDesc, seoTitle, tags string, found bool, err error) {
	schema, sErr := db.GetTenantSchema(tenantID)
	if sErr != nil {
		return "", "", "", "", "", false, sErr
	}
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'title', ''), COALESCE(data->>'short_desc', ''), COALESCE(data->>'long_desc', ''),
			COALESCE(data->>'seo_title', ''), COALESCE(data->>'tags', '')
		FROM %s.documents
		WHERE doctype = 'ProductContent' AND data->>'product_id' = $1 AND data->>'language' = $2 AND status = 'Approved'
		LIMIT 1`, schema), itemCode, language).Scan(&title, &shortDesc, &longDesc, &seoTitle, &tags)
	if err != nil {
		return "", "", "", "", "", false, nil
	}
	return title, shortDesc, longDesc, seoTitle, tags, true, nil
}

// productContentExists checks for any non-Rejected ProductContent in the
// given language - the same "excluding Rejected" carve-out
// validateProductContentDuplicate already applies, since a rejected
// translation should not block seeding a fresh attempt.
func productContentExists(tenantID, itemCode, language string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var count int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.documents
		WHERE doctype = 'ProductContent' AND data->>'product_id' = $1 AND data->>'language' = $2 AND status != 'Rejected'`, schema),
		itemCode, language).Scan(&count)
	return count > 0, err
}

// BulkSeedCatalogTranslations (36.7.5) walks a product-group-or-explicit
// item selection (the same ResolvePIMBulkTargetIDs seam every other PIM
// bulk action uses) and, for each item missing ProductContent in
// targetLanguage, creates a Draft seeded from its Approved sourceLanguage
// content. An item already carrying targetLanguage content (any status but
// Rejected) or with no Approved sourceLanguage content to seed from is
// skipped with a named reason rather than failing the whole batch.
func BulkSeedCatalogTranslations(tenantID, groupID string, itemCodes []string, sourceLanguage, targetLanguage, userID string) ([]TranslationSeedOutcome, error) {
	if sourceLanguage == "" || targetLanguage == "" {
		return nil, fmt.Errorf("source_language and target_language are both required")
	}
	if sourceLanguage == targetLanguage {
		return nil, fmt.Errorf("source_language and target_language must differ")
	}
	resolved, err := ResolvePIMBulkTargetIDs(tenantID, "Item", groupID, itemCodes)
	if err != nil {
		return nil, err
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("no items to seed - send item codes or a group_id")
	}
	maxItems := maxPIMBulkEditDocumentsFor(tenantID)
	if len(resolved) > maxItems {
		return nil, fmt.Errorf("bulk translation supports at most %d items at a time", maxItems)
	}

	out := make([]TranslationSeedOutcome, 0, len(resolved))
	for _, itemCode := range resolved {
		outcome := TranslationSeedOutcome{ItemCode: itemCode}

		exists, err := productContentExists(tenantID, itemCode, targetLanguage)
		if err != nil {
			outcome.Error = err.Error()
			out = append(out, outcome)
			continue
		}
		if exists {
			outcome.Error = fmt.Sprintf("already has %s content", targetLanguage)
			out = append(out, outcome)
			continue
		}

		title, shortDesc, longDesc, seoTitle, tags, found, err := fetchApprovedProductContent(tenantID, itemCode, sourceLanguage)
		if err != nil {
			outcome.Error = err.Error()
			out = append(out, outcome)
			continue
		}
		if !found {
			outcome.Error = fmt.Sprintf("no approved %s content to seed from", sourceLanguage)
			out = append(out, outcome)
			continue
		}

		contentID := itemCode + "::" + targetLanguage
		data := map[string]interface{}{
			"id": contentID, "code": contentID,
			"product_id": itemCode, "language": targetLanguage,
			"title": title, "short_desc": shortDesc, "long_desc": longDesc,
			"seo_title": seoTitle, "tags": tags,
			"status": "Draft",
		}
		// Recorded in the same business field the Workbench's own content
		// form already writes to (pimContentPayload's "owner"), not
		// documents.created_by - insertPIMDocument always writes that as
		// 'system' since an engine-internal caller may have no users row,
		// per that function's own comment.
		if userID != "" {
			data["owner"] = userID
		}
		if err := ValidateDocument(tenantID, "ProductContent", data); err != nil {
			outcome.Error = err.Error()
			out = append(out, outcome)
			continue
		}
		if err := insertPIMDocument(tenantID, "ProductContent", contentID, data); err != nil {
			outcome.Error = err.Error()
			out = append(out, outcome)
			continue
		}
		outcome.ContentID = contentID
		out = append(out, outcome)
	}
	return out, nil
}
