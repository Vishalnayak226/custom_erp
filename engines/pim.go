package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// PIM Foundation MVP (Stage 15, PIM Module Developer Blueprint v1.0) plus
// V2 alignment (Stage 15.2, Blueprint V2 - Repo-Enhanced): completeness
// scoring, variant-uniqueness validation, and (as of 15.2) locale/channel-
// scoped completeness plus the PIMProductProfile derived-status write-
// through. The Family/Attribute framework, Content enrichment (approval-
// gated via the existing engines/approval.go, Stage 13.8) and Item's
// family/variant fields are all plain generic doctypes (see
// db/migration.sql sections 31-32) needing no Go code of their own - this
// file only holds what can't be generic CRUD: scoring a product's
// readiness, deriving its overall enrichment_status, and preventing two
// variant Items from sharing the same option combination under one parent.

// pimProductProfileStatuses that are publish-outcome-owned - CalculateCompleteness
// never downgrades a profile out of one of these; only engines/pim_publish.go
// (or an explicit archive action) does, since completeness recompute has no
// visibility into actual publish attempts.
var publishOwnedStatuses = map[string]bool{
	"Ready to Publish": true,
	"Published":        true,
	"Publish Failed":   true,
	"Archived":         true,
}

// coreItemFields are always checked for completeness regardless of family.
var coreItemFields = []string{"name", "barcode", "category", "hsn_code", "gst_rate"}

type CompletenessResult struct {
	ItemCode           string   `json:"item_code"`
	Family             string   `json:"family"`
	Locale             string   `json:"locale"`
	ChannelID          string   `json:"channel_id,omitempty"`
	Score              float64  `json:"score"`
	TotalChecks        int      `json:"total_checks"`
	PassedChecks       int      `json:"passed_checks"`
	MissingFields      []string `json:"missing_fields"`
	HasApprovedContent bool     `json:"has_approved_content"`
	EnrichmentStatus   string   `json:"enrichment_status"`
}

type WorkbenchEntry struct {
	ItemCode     string  `json:"item_code"`
	Name         string  `json:"name"`
	Family       string  `json:"family"`
	Status       string  `json:"status"`
	Score        float64 `json:"score"`
	MissingCount int     `json:"missing_count"`
}

type familyAttribute struct {
	AttributeCode string
	Label         string
}

func fetchItemDoc(tenantID, itemCode string) (data map[string]interface{}, status string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'Item' AND id = $1`, schema), itemCode).Scan(&dataStr, &status)
	if err != nil {
		return nil, "", err
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", err
	}
	return data, status, nil
}

func isFieldFilled(data map[string]interface{}, field string) bool {
	val, exists := data[field]
	if !exists || val == nil {
		return false
	}
	return strings.TrimSpace(fmt.Sprintf("%v", val)) != ""
}

// fetchMandatoryFamilyAttributes returns the attribute code+label pairs
// marked mandatory=Yes for a family. Link field values store the target
// document's id (== its human-readable code, by this codebase's own "id:
// code" creation convention), so a second lookup against
// ProductAttributeDef resolves the display label - no real SQL JOIN is
// possible across JSONB documents, same sequential-lookup pattern
// fetchBOM (engines/manufacturing.go) already uses for its parent_item.
func fetchMandatoryFamilyAttributes(tenantID, family string) ([]familyAttribute, error) {
	if family == "" {
		return nil, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'attribute' FROM %s.documents
		WHERE doctype = 'ProductFamilyAttribute' AND data->>'family' = $1 AND data->>'mandatory' = 'Yes'`, schema), family)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		if code != "" {
			codes = append(codes, code)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]familyAttribute, 0, len(codes))
	for _, code := range codes {
		label := code
		var l string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'label', '') FROM %s.documents WHERE doctype = 'ProductAttributeDef' AND id = $1`, schema), code).Scan(&l); err == nil && l != "" {
			label = l
		}
		out = append(out, familyAttribute{AttributeCode: code, Label: label})
	}
	return out, nil
}

// attributeValueFilled checks the ProductAttributeValue at the composite id
// "<itemCode>::<attributeCode>" (see migration.sql section 31's id
// convention note) for a non-empty value.
func attributeValueFilled(tenantID, itemCode, attributeCode string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var value string
	id := itemCode + "::" + attributeCode
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'value', '') FROM %s.documents WHERE doctype = 'ProductAttributeValue' AND id = $1`, schema), id).Scan(&value)
	if err != nil {
		return false, nil // no row yet - not filled, not an error
	}
	return strings.TrimSpace(value) != "", nil
}

// hasApprovedContent checks whether a ProductContent for the given locale
// (V2 alignment: locale-scoped, not "any language") exists for the item
// with status Approved.
func hasApprovedContent(tenantID, itemCode, locale string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var count int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.documents
		WHERE doctype = 'ProductContent' AND data->>'product_id' = $1 AND data->>'language' = $2 AND status = 'Approved'`, schema), itemCode, locale).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// hasPendingContent checks whether a ProductContent for the given locale is
// currently Pending Approval - used to derive the "Pending Approval"
// enrichment_status.
func hasPendingContent(tenantID, itemCode, locale string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var count int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.documents
		WHERE doctype = 'ProductContent' AND data->>'product_id' = $1 AND data->>'language' = $2 AND status = 'Pending Approval'`, schema), itemCode, locale).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

type channelFieldRule struct {
	TargetField string
	SourceField string
}

// fetchChannelMandatoryFields returns the ChannelFieldMap rows marked
// mandatory=Yes for a channel - a field optional in ERP/PIM core can still
// block publish-readiness for that specific channel (V2 §7/§14).
func fetchChannelMandatoryFields(tenantID, channelID string) ([]channelFieldRule, error) {
	if channelID == "" {
		return nil, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'source_field', ''), COALESCE(data->>'target_field', '') FROM %s.documents
		WHERE doctype = 'ChannelFieldMap' AND data->>'channel' = $1 AND data->>'mandatory' = 'Yes'`, schema), channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []channelFieldRule
	for rows.Next() {
		var r channelFieldRule
		if err := rows.Scan(&r.SourceField, &r.TargetField); err != nil {
			return nil, err
		}
		if r.SourceField != "" {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

// channelFieldFilled checks a channel field mapping's source_field against
// the Item's own data first (covers core fields like name/barcode), then
// falls back to treating it as a ProductAttributeValue attribute code
// (covers PIM-enriched fields like polish/warranty).
func channelFieldFilled(tenantID, itemCode string, itemData map[string]interface{}, sourceField string) (bool, error) {
	if _, exists := itemData[sourceField]; exists {
		return isFieldFilled(itemData, sourceField), nil
	}
	return attributeValueFilled(tenantID, itemCode, sourceField)
}

// CalculateCompleteness scores an Item 0-100 against its core ERP fields,
// its family's mandatory attributes (if a family is set), whether it has
// approved PIM content for the given locale, and (if channelID is set) that
// channel's mandatory field mappings - V2's "Completeness Score =
// valid_required_values / total_required_values for each Family + Channel +
// Locale combination" (blueprint V2 §9). Also derives and write-throughs
// the item's PIMProductProfile.enrichment_status (see the publishOwnedStatuses
// note above the type declarations). locale defaults to "en" if blank.
func CalculateCompleteness(tenantID, itemCode, locale, channelID string) (*CompletenessResult, error) {
	if locale == "" {
		locale = "en"
	}

	data, _, err := fetchItemDoc(tenantID, itemCode)
	if err != nil {
		return nil, fmt.Errorf("item not found: %v", err)
	}
	family, _ := data["family"].(string)

	result := &CompletenessResult{ItemCode: itemCode, Family: family, Locale: locale, ChannelID: channelID}
	missing := []string{}

	for _, f := range coreItemFields {
		result.TotalChecks++
		if isFieldFilled(data, f) {
			result.PassedChecks++
		} else {
			missing = append(missing, f)
		}
	}

	attrs, err := fetchMandatoryFamilyAttributes(tenantID, family)
	if err != nil {
		return nil, err
	}
	for _, a := range attrs {
		result.TotalChecks++
		filled, err := attributeValueFilled(tenantID, itemCode, a.AttributeCode)
		if err != nil {
			return nil, err
		}
		if filled {
			result.PassedChecks++
		} else {
			missing = append(missing, a.Label)
		}
	}

	result.TotalChecks++
	hasContent, err := hasApprovedContent(tenantID, itemCode, locale)
	if err != nil {
		return nil, err
	}
	result.HasApprovedContent = hasContent
	if hasContent {
		result.PassedChecks++
	} else {
		missing = append(missing, fmt.Sprintf("Approved Content (%s)", locale))
	}

	if channelID != "" {
		channelFields, err := fetchChannelMandatoryFields(tenantID, channelID)
		if err != nil {
			return nil, err
		}
		for _, cf := range channelFields {
			result.TotalChecks++
			filled, err := channelFieldFilled(tenantID, itemCode, data, cf.SourceField)
			if err != nil {
				return nil, err
			}
			if filled {
				result.PassedChecks++
			} else {
				missing = append(missing, fmt.Sprintf("Channel field: %s", cf.TargetField))
			}
		}
	}

	result.MissingFields = missing
	if result.TotalChecks > 0 {
		result.Score = math.Round(float64(result.PassedChecks)/float64(result.TotalChecks)*1000) / 10
	}

	pending, err := hasPendingContent(tenantID, itemCode, locale)
	if err != nil {
		return nil, err
	}
	result.EnrichmentStatus, err = deriveAndPersistProfileStatus(tenantID, itemCode, family, result.Score, hasContent, pending, missing)
	if err != nil {
		return nil, err
	}

	return result, nil
}

type itemRow struct {
	ID     string
	Name   string
	Family string
	Status string
}

func listItemsForWorkbench(tenantID, familyFilter string) ([]itemRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`SELECT id, COALESCE(data->>'name', ''), COALESCE(data->>'family', ''), status FROM %s.documents WHERE doctype = 'Item'`, schema)
	args := []interface{}{}
	if familyFilter != "" {
		query += " AND data->>'family' = $1"
		args = append(args, familyFilter)
	}
	query += " ORDER BY id"

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []itemRow
	for rows.Next() {
		var r itemRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Family, &r.Status); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListWorkbench is the Product Workbench data source (blueprint section
// 7): every Item with its completeness score, sorted worst-first so the
// least-ready products surface at the top - the most actionable default
// for a daily triage screen.
func ListWorkbench(tenantID, familyFilter string) ([]WorkbenchEntry, error) {
	items, err := listItemsForWorkbench(tenantID, familyFilter)
	if err != nil {
		return nil, err
	}

	out := make([]WorkbenchEntry, 0, len(items))
	for _, it := range items {
		c, err := CalculateCompleteness(tenantID, it.ID, "en", "")
		if err != nil {
			continue // skip an item that fails to score rather than fail the whole workbench
		}
		out = append(out, WorkbenchEntry{
			ItemCode:     it.ID,
			Name:         it.Name,
			Family:       it.Family,
			Status:       it.Status,
			Score:        c.Score,
			MissingCount: len(c.MissingFields),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score < out[j].Score })
	return out, nil
}

// pimProductProfileID derives the PIMProductProfile document id for an
// item. Deliberately NOT just itemCode: tenant_default.documents.id is a
// single VARCHAR PRIMARY KEY shared across every doctype (not scoped per
// doctype), so a profile using the bare item code would silently collide
// with the Item's own row - the INSERT's ON CONFLICT(id) would match the
// *Item* row, not create/update a profile, and (being DO NOTHING in
// EnsurePIMProductProfile's case) would fail completely silently. Same
// "<item>::suffix" composite-id convention already used for
// ProductAttributeValue/ProductContent (migration.sql section 31).
func pimProductProfileID(itemCode string) string {
	return itemCode + "::profile"
}

// fetchPIMProductProfileStatus returns the current enrichment_status for an
// item's profile, or "" if none exists yet.
func fetchPIMProductProfileStatus(tenantID, itemCode string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'enrichment_status', '') FROM %s.documents WHERE doctype = 'PIMProductProfile' AND id = $1`, schema), pimProductProfileID(itemCode)).Scan(&status)
	if err != nil {
		return "", nil // no profile yet - not an error
	}
	return status, nil
}

// upsertPIMProductProfile writes (create or update) the PIMProductProfile
// row for an item - the write-through cache/derived-status doctype (see
// migration.sql section 32). Never called directly by a user; only from
// EnsurePIMProductProfile (on Item create) and deriveAndPersistProfileStatus
// (on every completeness recalculation).
func upsertPIMProductProfile(tenantID, itemCode, status string, score float64, missingFields []string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	missingJSON, err := json.Marshal(missingFields)
	if err != nil {
		return err
	}
	profileID := pimProductProfileID(itemCode)
	data := map[string]interface{}{
		"id":                  profileID,
		"code":                profileID,
		"product_id":          itemCode,
		"enrichment_status":   status,
		"completeness_score":  score,
		"missing_fields_json": string(missingJSON),
		"last_scored_at":      time.Now().Format(time.RFC3339),
	}
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'PIMProductProfile', $2, $3, 'system')
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, status = EXCLUDED.status, updated_at = CURRENT_TIMESTAMP`, schema),
		profileID, marshaled, status)
	return err
}

// EnsurePIMProductProfile auto-creates a PIMProductProfile (status Draft)
// for a newly created Item - V2 §6.1 step 2, "PIM profile is auto-created
// with status PIM Draft." A no-op if a profile already exists (ON CONFLICT
// DO NOTHING), so it's safe to call unconditionally from the Item-create
// hook in handlers_core_doc_engine.go's handleGenericDoc.
func EnsurePIMProductProfile(tenantID, itemCode string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	profileID := pimProductProfileID(itemCode)
	data := map[string]interface{}{
		"id":                  profileID,
		"code":                profileID,
		"product_id":          itemCode,
		"enrichment_status":   "Draft",
		"completeness_score":  0,
		"missing_fields_json": "[]",
		"last_scored_at":      time.Now().Format(time.RFC3339),
	}
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'PIMProductProfile', $2, 'Draft', 'system')
		ON CONFLICT (id) DO NOTHING`, schema), profileID, marshaled)
	return err
}

// deriveAndPersistProfileStatus computes the item's overall enrichment_status
// from its current completeness/content state and writes it through to
// PIMProductProfile, returning the (possibly unchanged) status. Deliberately
// never assigns a publishOwnedStatuses value (Ready to Publish/Published/
// Publish Failed/Archived) - those are only ever set by engines/pim_publish.go
// based on real publish attempts, and are left untouched here if already set,
// so a routine completeness recompute (e.g. viewing the Workbench) can never
// silently downgrade an already-published product.
func deriveAndPersistProfileStatus(tenantID, itemCode, family string, score float64, hasApprovedContent, hasPendingContent bool, missingFields []string) (string, error) {
	existing, err := fetchPIMProductProfileStatus(tenantID, itemCode)
	if err != nil {
		return "", err
	}
	if publishOwnedStatuses[existing] {
		return existing, nil
	}

	status := "Draft"
	switch {
	case family == "":
		status = "Draft"
	case hasPendingContent:
		status = "Pending Approval"
	case score >= 100 && hasApprovedContent:
		status = "Approved"
	default:
		status = "Enrichment In Progress"
	}

	if err := upsertPIMProductProfile(tenantID, itemCode, status, score, missingFields); err != nil {
		return "", err
	}
	return status, nil
}

// normalizeVariantOptions parses the "Key:Value;Key:Value" shorthand
// (case-insensitively) and re-serializes it with sorted keys, so
// "Color:Red;Size:M" and "Size:M;Color:Red" are recognized as the same
// combination.
func normalizeVariantOptions(raw string) string {
	pairs := strings.Split(raw, ";")
	kv := make(map[string]string, len(pairs))
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts := strings.SplitN(p, ":", 2)
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		if key == "" {
			continue
		}
		val := ""
		if len(parts) == 2 {
			val = strings.ToLower(strings.TrimSpace(parts[1]))
		}
		kv[key] = val
	}

	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+":"+kv[k])
	}
	return strings.Join(parts, ";")
}

// ValidateItemVariantUniqueness blocks two Items under the same
// parent_product_code from sharing the same variant_option_values
// combination (blueprint sections 9/17: "prevent duplicate variant
// combinations"). A no-op for a standalone Item (no parent/no options set).
// docID is the id of the Item being saved (blank on a create where the
// server will generate a fresh UUID) so it can exclude itself on update.
func ValidateItemVariantUniqueness(tenantID, docID string, payload map[string]interface{}) error {
	parentCode, _ := payload["parent_product_code"].(string)
	variantOptions, _ := payload["variant_option_values"].(string)
	parentCode = strings.TrimSpace(parentCode)
	variantOptions = strings.TrimSpace(variantOptions)
	if parentCode == "" || variantOptions == "" {
		return nil
	}

	normalized := normalizeVariantOptions(variantOptions)
	if normalized == "" {
		return nil
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'variant_option_values', '') FROM %s.documents
		WHERE doctype = 'Item' AND data->>'parent_product_code' = $1 AND id != $2`, schema), parentCode, docID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var siblingID, siblingOptions string
		if err := rows.Scan(&siblingID, &siblingOptions); err != nil {
			return err
		}
		if siblingOptions == "" {
			continue
		}
		if normalizeVariantOptions(siblingOptions) == normalized {
			return fmt.Errorf("a variant with this exact option combination already exists under parent %q: %s", parentCode, siblingID)
		}
	}
	return rows.Err()
}
