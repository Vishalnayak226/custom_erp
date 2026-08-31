package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Stage 36.3: import depth. PIMImportTemplate is a reusable column mapping
// (a supplier's own header names -> this doctype's own field names, plus an
// optional Stage 36.5 transform rule and default value per column) so a
// recurring source whose file never matches GenerateCSVTemplate's fixed
// output doesn't need every header renamed by hand before every upload.
//
// RunPIMImportTemplate deliberately does NOT reimplement validation,
// batching or dry-run: it remaps/transforms a source file into the same
// fieldname->value shape BulkImportCSV has always produced, then hands it
// to the identical shared core (runDocDataImport/importBatch,
// engines/import.go) - a template import passes every guard a plain one
// already does (mandatory columns, per-row ValidateDocument, batched
// transactions, create/update classification, dry-run preview) because it
// is the same code, not a parallel copy of it.

// PIMImportColumnMapping is one row of a PIMImportTemplate.column_mappings
// JSONTable.
type PIMImportColumnMapping struct {
	SourceColumn  string `json:"source_column"`
	TargetField   string `json:"target_field"`
	TransformRule string `json:"transform_rule,omitempty"`
	DefaultValue  string `json:"default_value,omitempty"`
}

func decodePIMImportColumnMappings(raw interface{}) ([]PIMImportColumnMapping, error) {
	var rows []map[string]interface{}
	if err := decodeProductGroupJSON(raw, &rows); err != nil {
		return nil, fmt.Errorf("column_mappings must be a JSON array of mapping rows: %w", err)
	}
	mappings := make([]PIMImportColumnMapping, 0, len(rows))
	for _, row := range rows {
		mappings = append(mappings, PIMImportColumnMapping{
			SourceColumn: pimString(row["source_column"]), TargetField: pimString(row["target_field"]),
			TransformRule: pimString(row["transform_rule"]), DefaultValue: pimString(row["default_value"]),
		})
	}
	return mappings, nil
}

// ValidatePIMImportTemplateDocument runs at ValidateDocument's shared exit.
// Every check here exists because the failure it prevents is silent: a
// target field that does not exist on the doctype, or a transform rule that
// does not exist or is inactive, saves happily and then either does nothing
// or fails the very first import that uses it, far from where the template
// was authored.
func ValidatePIMImportTemplateDocument(tenantID string, payload map[string]interface{}) error {
	targetDoctype := strings.TrimSpace(pimString(payload["target_doctype"]))
	if targetDoctype == "" {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "Target Doctype", Message: "an import template needs a target doctype"}
	}
	mappings, err := decodePIMImportColumnMappings(payload["column_mappings"])
	if err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Column Mappings", Message: err.Error()}
	}
	if len(mappings) == 0 {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "Column Mappings", Message: "an import template needs at least one column mapping"}
	}

	var targetFieldSet map[string]bool
	if db.DB != nil {
		fields, fErr := GetDocTypeMeta(tenantID, targetDoctype)
		if fErr != nil || len(fields) == 0 {
			return &ValidationError{Code: "META-0198", SubFor: "Target Doctype", Message: fmt.Sprintf("target doctype %q has no known fields", targetDoctype)}
		}
		targetFieldSet = map[string]bool{}
		for _, f := range fields {
			targetFieldSet[strings.ToLower(f.Fieldname)] = true
		}
	}

	seenTarget := map[string]bool{}
	for i, m := range mappings {
		if strings.TrimSpace(m.SourceColumn) == "" {
			return &ValidationError{Code: "GLOBAL-0001", SubFor: "Column Mappings",
				Message: fmt.Sprintf("mapping %d has no source column", i+1)}
		}
		target := strings.ToLower(strings.TrimSpace(m.TargetField))
		if target == "" {
			return &ValidationError{Code: "GLOBAL-0001", SubFor: "Column Mappings",
				Message: fmt.Sprintf("mapping %d has no target field", i+1)}
		}
		if seenTarget[target] {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Column Mappings",
				Message: fmt.Sprintf("target field %q is mapped more than once - an import row cannot write two different values to the same field", m.TargetField)}
		}
		seenTarget[target] = true
		if targetFieldSet != nil && target != "id" && !targetFieldSet[target] {
			return &ValidationError{Code: "META-0198", SubFor: "Column Mappings",
				Message: fmt.Sprintf("mapping %d targets %q, which is not a field on %s", i+1, m.TargetField, targetDoctype)}
		}
		if rule := strings.TrimSpace(m.TransformRule); rule != "" && db.DB != nil {
			if _, _, ruleErr := fetchPIMTransformRule(tenantID, rule); ruleErr != nil {
				return &ValidationError{Code: "META-0198", SubFor: "Column Mappings",
					Message: fmt.Sprintf("mapping %d names transform rule %q: %v", i+1, rule, ruleErr)}
			}
		}
	}
	return nil
}

func fetchPIMImportTemplate(tenantID, templateID string) (canonicalID, targetDoctype string, mappings []PIMImportColumnMapping, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", nil, err
	}
	var raw, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents
		WHERE doctype = 'PIMImportTemplate' AND (id = $1 OR UPPER(data->>'code') = UPPER($1)) AND deleted_at IS NULL
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END, id LIMIT 1`, schema), templateID).Scan(&canonicalID, &raw, &status)
	if err != nil {
		return "", "", nil, fmt.Errorf("import template %q not found", templateID)
	}
	if status != "Active" {
		return "", "", nil, fmt.Errorf("import template %q is not active", templateID)
	}
	var data map[string]interface{}
	if uErr := json.Unmarshal([]byte(raw), &data); uErr != nil {
		return "", "", nil, fmt.Errorf("import template %q has invalid stored data: %w", templateID, uErr)
	}
	mappings, err = decodePIMImportColumnMappings(data["column_mappings"])
	if err != nil {
		return "", "", nil, fmt.Errorf("import template %q: %w", templateID, err)
	}
	return canonicalID, pimString(data["target_doctype"]), mappings, nil
}

// PIMImportTemplateInfo is the list/picker-facing view of a template.
type PIMImportTemplateInfo struct {
	ID             string                   `json:"id"`
	Code           string                   `json:"code"`
	Name           string                   `json:"name"`
	TargetDoctype  string                   `json:"target_doctype"`
	ColumnMappings []PIMImportColumnMapping `json:"column_mappings"`
}

// ListPIMImportTemplates backs the template picker on a schedule/hook's
// authoring screen - only Active templates, since choosing one is picking
// what applies going forward, not auditing history.
func ListPIMImportTemplates(tenantID string) ([]PIMImportTemplateInfo, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = 'PIMImportTemplate' AND deleted_at IS NULL AND status = 'Active'
		ORDER BY COALESCE(data->>'name', id)`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PIMImportTemplateInfo{}
	for rows.Next() {
		var id, raw string
		if sErr := rows.Scan(&id, &raw); sErr != nil {
			return nil, sErr
		}
		var data map[string]interface{}
		if uErr := json.Unmarshal([]byte(raw), &data); uErr != nil {
			continue
		}
		mappings, _ := decodePIMImportColumnMappings(data["column_mappings"])
		out = append(out, PIMImportTemplateInfo{
			ID: id, Code: pimString(data["code"]), Name: pimString(data["name"]),
			TargetDoctype: pimString(data["target_doctype"]), ColumnMappings: mappings,
		})
	}
	return out, rows.Err()
}

// PIMImportTemplateMappingRow is one resolved mapping, for the 36.3.6
// authoring-time preview: what a column maps to and through which
// transform, before any file is ever uploaded against it.
type PIMImportTemplateMappingRow struct {
	SourceColumn  string `json:"source_column"`
	TargetField   string `json:"target_field"`
	TargetLabel   string `json:"target_label,omitempty"`
	TransformRule string `json:"transform_rule,omitempty"`
	DefaultValue  string `json:"default_value,omitempty"`
}

type PIMImportTemplateMappingPreview struct {
	TemplateID    string                        `json:"template_id"`
	TargetDoctype string                        `json:"target_doctype"`
	Mappings      []PIMImportTemplateMappingRow `json:"mappings"`
}

// PreviewPIMImportTemplateMapping is the 36.3.6 "what maps to what" preview
// - resolved against the target doctype's real field labels, so an author
// can catch a stale/renamed target field before ever uploading a file.
func PreviewPIMImportTemplateMapping(tenantID, templateID string) (*PIMImportTemplateMappingPreview, error) {
	canonicalID, targetDoctype, mappings, err := fetchPIMImportTemplate(tenantID, templateID)
	if err != nil {
		return nil, err
	}
	labelByField := map[string]string{}
	if fields, fErr := GetDocTypeMeta(tenantID, targetDoctype); fErr == nil {
		for _, f := range fields {
			labelByField[strings.ToLower(f.Fieldname)] = f.Label
		}
	}
	out := &PIMImportTemplateMappingPreview{TemplateID: canonicalID, TargetDoctype: targetDoctype}
	for _, m := range mappings {
		out.Mappings = append(out.Mappings, PIMImportTemplateMappingRow{
			SourceColumn: m.SourceColumn, TargetField: m.TargetField,
			TargetLabel:   labelByField[strings.ToLower(m.TargetField)],
			TransformRule: m.TransformRule, DefaultValue: m.DefaultValue,
		})
	}
	return out, nil
}

// pimVariantParentPreflight (36.3.5) checks every row whose parent_product_code
// is set against the set of Item codes that will exist once this file is
// fully imported: already in the database, or the id of an earlier row in
// the same file. A variant row whose parent is neither is refused with a
// named reason here, at preview/import time, rather than saving happily and
// silently orphaning a variant no parent screen will ever show.
//
// Parent existence, not sibling duplication: ValidateItemVariantUniqueness
// (engines/pim.go, called by ValidateDocument on every row regardless) already
// covers two rows sharing the same parent+option combination. This is the
// complementary check the per-row path cannot make on its own, because
// "does an earlier row in this same uncommitted file define this id" is not
// something a query against already-committed data can answer.
func pimVariantParentPreflight(tenantID string, docRows []map[string]interface{}) []string {
	errs := make([]string, len(docRows))
	var neededParents []string
	seenParent := map[string]bool{}
	for _, row := range docRows {
		if parent := strings.TrimSpace(pimString(row["parent_product_code"])); parent != "" && !seenParent[parent] {
			seenParent[parent] = true
			neededParents = append(neededParents, parent)
		}
	}
	if len(neededParents) == 0 {
		return errs
	}
	existing := map[string]bool{}
	if schema, sErr := db.GetTenantSchema(tenantID); sErr == nil {
		rows, qErr := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents
			WHERE doctype = 'Item' AND id = ANY($1::text[]) AND deleted_at IS NULL`, schema), pqTextArray(neededParents))
		if qErr == nil {
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					existing[id] = true
				}
			}
			rows.Close()
		}
	}

	knownInFile := map[string]bool{}
	for i, row := range docRows {
		if parent := strings.TrimSpace(pimString(row["parent_product_code"])); parent != "" {
			if !existing[parent] && !knownInFile[parent] {
				errs[i] = fmt.Sprintf("parent product code %q was not found in the database or earlier in this file", parent)
			}
		}
		if id := strings.TrimSpace(pimString(row["id"])); id != "" {
			knownInFile[id] = true
		}
	}
	return errs
}

// RunPIMImportTemplate remaps a source file's own headers through a
// template's column mappings (applying each column's transform rule and
// default value), then delegates to the exact same batching/validation core
// every other import already uses. templateID and doctype must agree, so an
// operator cannot point a Vendor-targeted template at the Item import
// screen (or a hook minted for one doctype) and have it silently import
// into a different one.
func RunPIMImportTemplate(tenantID, templateID string, r io.Reader, userID, role string, dryRun bool) (*ImportResult, error) {
	_, targetDoctype, mappings, err := fetchPIMImportTemplate(tenantID, templateID)
	if err != nil {
		return nil, err
	}

	records, err := readCSVRecords(r)
	if err != nil {
		return nil, err
	}
	sourceHeaders := records[0]
	sourceIndex := map[string]int{}
	for i, h := range sourceHeaders {
		sourceIndex[strings.ToLower(strings.TrimSpace(h))] = i
	}

	mappedTargetSet := map[string]bool{}
	var mappedTargetHeaders []string
	for _, m := range mappings {
		key := strings.ToLower(strings.TrimSpace(m.TargetField))
		if !mappedTargetSet[key] {
			mappedTargetSet[key] = true
			mappedTargetHeaders = append(mappedTargetHeaders, key)
		}
	}
	// DATAIM-0164, checked against the template's OWN target fields rather
	// than the source file's raw headers - the whole point of a template is
	// that the source header names don't have to match the doctype's own
	// field names, so this cannot reuse BulkImportCSV's header-level check.
	if missing := missingMandatoryColumns(tenantID, targetDoctype, mappedTargetHeaders); len(missing) > 0 {
		return nil, &ValidationError{Code: "DATAIM-0164", Message: fmt.Sprintf("template %q does not map mandatory field(s): %s", templateID, strings.Join(missing, ", "))}
	}

	dataRows := records[1:]
	docRows := make([]map[string]interface{}, len(dataRows))
	preErrors := make([]string, len(dataRows))
	for i, row := range dataRows {
		docData := make(map[string]interface{})
		for _, m := range mappings {
			var val string
			if idx, ok := sourceIndex[strings.ToLower(strings.TrimSpace(m.SourceColumn))]; ok && idx < len(row) {
				val = strings.TrimSpace(row[idx])
			}
			if val == "" && m.DefaultValue != "" {
				val = m.DefaultValue
			}
			if val != "" && m.TransformRule != "" {
				transformed, tErr := ApplyPIMTransformRule(tenantID, m.TransformRule, val)
				if tErr != nil {
					preErrors[i] = fmt.Sprintf("column %q -> %q: %v", m.SourceColumn, m.TargetField, tErr)
					break
				}
				val = transformed
			}
			docData[strings.ToLower(strings.TrimSpace(m.TargetField))] = sanitizeCSVCell(val)
		}
		docRows[i] = docData
	}

	if strings.EqualFold(targetDoctype, "Item") {
		for i, vErr := range pimVariantParentPreflight(tenantID, docRows) {
			if vErr != "" && preErrors[i] == "" {
				preErrors[i] = vErr
			}
		}
	}

	return runDocDataImport(tenantID, targetDoctype, userID, role, dryRun, docRows, preErrors)
}
