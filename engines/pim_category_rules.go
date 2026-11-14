package engines

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Per-category attribute rules (168.12) and their inheritance down the
// category tree (168.8).
//
// A CategoryAttributeRule says, for one category and one attribute, Required,
// Optional or Hidden, with an optional default value. A rule on a parent
// category reaches its child categories and their items unless the child has
// its own rule for that attribute: the nearest category wins.
//
//   - Required: an item of the category that has no value for it scores a
//     completeness miss, always. A save is refused only when
//     pim.enforce_category_attribute_rules is on (off by default).
//   - Hidden: the attribute is not offered on that category's item form. A value
//     already stored stays (the same rule 89.4.4 uses for hidden fields).
//   - Optional: asks nothing; it ends an inherited Required for the subtree.
//
// The completeness scoring and ListWorkbench load the whole rule set once
// (categoryRuleIndex), so scoring stays set-based.

const (
	CategoryAttributeRuleDoctype = "CategoryAttributeRule"

	CategoryRuleRequired = "Required"
	CategoryRuleOptional = "Optional"
	CategoryRuleHidden   = "Hidden"

	// SettingEnforceCategoryAttributeRules refuses a save that leaves a Required attribute blank.
	SettingEnforceCategoryAttributeRules = "pim.enforce_category_attribute_rules"
)

//go:embed attribute_library_rules.json
var categoryRuleLibraryJSON []byte

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingEnforceCategoryAttributeRules, Module: "PIM",
		Label: "Refuse saves that miss a category's required attributes", Type: SettingTypeBool, Default: "false",
		Description: "When on, blanking a value the item's category requires, or saving an existing item that still lacks one, is refused. Off: the item only scores lower on completeness.",
	})
}

// CategoryRule is one stored rule.
type CategoryRule struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	Attribute string `json:"attribute"`
	Label     string `json:"label"`
	Rule      string `json:"rule"`
	Default   string `json:"default_value,omitempty"`
}

// EffectiveCategoryRule is the rule that applies to a category after
// inheritance. From is the category that owns it; Inherited is true when that
// is an ancestor, not the category asked about.
type EffectiveCategoryRule struct {
	CategoryRule
	From      string `json:"from"`
	Inherited bool   `json:"inherited"`
}

type categoryRuleIndex struct {
	parent map[string]string
	known  map[string]bool
	names  map[string]string                  // lower(name) -> category id: Item.category may hold either
	rules  map[string]map[string]CategoryRule // category -> attribute -> rule
	labels map[string]string
}

// loadCategoryRuleIndex reads every rule and the category tree. It returns nil
// when the client has no rules, so callers skip all work.
func loadCategoryRuleIndex(tenantID string) (*categoryRuleIndex, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'category',''), COALESCE(data->>'attribute',''), COALESCE(data->>'rule',''), COALESCE(data->>'default_value','')
		FROM %s.documents WHERE doctype = 'CategoryAttributeRule' AND deleted_at IS NULL ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	idx := &categoryRuleIndex{parent: map[string]string{}, known: map[string]bool{}, names: map[string]string{}, rules: map[string]map[string]CategoryRule{}, labels: map[string]string{}}
	var attrs []string
	for rows.Next() {
		var r CategoryRule
		if err := rows.Scan(&r.ID, &r.Category, &r.Attribute, &r.Rule, &r.Default); err != nil {
			return nil, err
		}
		if r.Category == "" || r.Attribute == "" {
			continue
		}
		if idx.rules[r.Category] == nil {
			idx.rules[r.Category] = map[string]CategoryRule{}
		}
		idx.rules[r.Category][r.Attribute] = r
		attrs = append(attrs, r.Attribute)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(idx.rules) == 0 {
		return nil, nil
	}
	prow, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'parent',''), lower(COALESCE(data->>'name','')) FROM %s.documents WHERE doctype = 'Category' AND deleted_at IS NULL ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	defer prow.Close()
	for prow.Next() {
		var id, parent, name string
		if err := prow.Scan(&id, &parent, &name); err != nil {
			return nil, err
		}
		if parent != "" {
			idx.parent[id] = parent
		}
		if _, dup := idx.names[name]; name != "" && !dup {
			idx.names[name] = id
		}
		idx.known[id] = true
	}
	if err := prow.Err(); err != nil {
		return nil, err
	}
	lrows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'label','') FROM %s.documents WHERE doctype = 'ProductAttributeDef' AND id = ANY($1)`, schema), pq.Array(attrs))
	if err != nil {
		return nil, err
	}
	defer lrows.Close()
	for lrows.Next() {
		var id, label string
		if err := lrows.Scan(&id, &label); err != nil {
			return nil, err
		}
		idx.labels[id] = label
	}
	return idx, lrows.Err()
}

func (x *categoryRuleIndex) label(attr string) string {
	if l := x.labels[attr]; l != "" {
		return l
	}
	return attr
}

// effective is the rule per attribute that applies to category: the category's
// own rule, else the nearest ancestor's. A cycle in the tree stops the walk.
func (x *categoryRuleIndex) effective(category string) map[string]EffectiveCategoryRule {
	out := map[string]EffectiveCategoryRule{}
	if x == nil || category == "" {
		return out
	}
	if !x.known[category] {
		if id, ok := x.names[strings.ToLower(strings.TrimSpace(category))]; ok {
			category = id
		}
	}
	seen := map[string]bool{}
	for c := category; c != "" && !seen[c] && len(seen) < 32; c = x.parent[c] {
		seen[c] = true
		for attr, r := range x.rules[c] {
			if _, taken := out[attr]; taken {
				continue
			}
			r.Label = x.label(attr)
			out[attr] = EffectiveCategoryRule{CategoryRule: r, From: c, Inherited: c != category}
		}
	}
	return out
}

// mergeAttrs is the family's mandatory attributes as this category changes
// them: a Required rule adds one, a Hidden rule drops one. The input slice is
// never modified (the workbench shares one per family).
func (x *categoryRuleIndex) mergeAttrs(category string, attrs []familyAttribute) []familyAttribute {
	if x == nil || category == "" {
		return attrs
	}
	eff := x.effective(category)
	if len(eff) == 0 {
		return attrs
	}
	out := make([]familyAttribute, 0, len(attrs)+len(eff))
	have := map[string]bool{}
	for _, a := range attrs {
		if r, ok := eff[a.AttributeCode]; ok && r.Rule == CategoryRuleHidden {
			continue
		}
		have[a.AttributeCode] = true
		out = append(out, a)
	}
	codes := make([]string, 0, len(eff))
	for code, r := range eff {
		if r.Rule == CategoryRuleRequired && !have[code] {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	for _, code := range codes {
		out = append(out, familyAttribute{AttributeCode: code, Label: eff[code].Label})
	}
	return out
}

// EffectiveCategoryRules lists what applies to a category, own rules and
// inherited ones, sorted by attribute label.
func EffectiveCategoryRules(tenantID, category string) ([]EffectiveCategoryRule, error) {
	idx, err := loadCategoryRuleIndex(tenantID)
	if err != nil {
		return nil, err
	}
	m := idx.effective(category)
	out := make([]EffectiveCategoryRule, 0, len(m))
	for _, r := range m {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label) })
	return out, nil
}

// attributeItemCategory is the category an item sits in ("" when none).
func attributeItemCategory(tenantID, itemCode string) string {
	data, _, err := fetchItemDoc(tenantID, itemCode)
	if err != nil {
		return ""
	}
	return strField(data, "category")
}

// missingRequiredAttributes names the Required attributes (after inheritance)
// the item has no value for.
func missingRequiredAttributes(tenantID, itemCode, category string) ([]string, error) {
	idx, err := loadCategoryRuleIndex(tenantID)
	if err != nil || idx == nil {
		return nil, err
	}
	eff := idx.effective(category)
	var need []string
	for code, r := range eff {
		if r.Rule == CategoryRuleRequired {
			need = append(need, code)
		}
	}
	if len(need) == 0 {
		return nil, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT DISTINCT data->>'attribute' FROM %s.documents
		WHERE doctype = 'ProductAttributeValue' AND deleted_at IS NULL AND data->>'item' = $1
			AND data->>'attribute' = ANY($2) AND btrim(COALESCE(data->>'value','')) <> ''`, schema), itemCode, pq.Array(need))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	filled := map[string]bool{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		filled[a] = true
	}
	var missing []string
	for _, code := range need {
		if !filled[code] {
			missing = append(missing, eff[code].Label)
		}
	}
	sort.Strings(missing)
	return missing, rows.Err()
}

// ValidateItemCategoryRules is the save check for an existing Item when
// pim.enforce_category_attribute_rules is on. A new item has no values yet, so
// it is never refused here; its gaps show on the completeness score.
func ValidateItemCategoryRules(tenantID string, payload map[string]interface{}) error {
	if !GetSettingBool(tenantID, SettingEnforceCategoryAttributeRules) {
		return nil
	}
	code, category := strField(payload, "code"), strField(payload, "category")
	if code == "" || category == "" {
		return nil
	}
	if _, _, err := fetchItemDoc(tenantID, code); err != nil {
		return nil // not saved yet
	}
	missing, err := missingRequiredAttributes(tenantID, code, category)
	if err != nil || len(missing) == 0 {
		return err
	}
	return &ValidationError{Code: "PIM-9301", SubFor: "category", Message: "Category " + category + " needs a value for: " + strings.Join(missing, ", ")}
}

// validateBlankedRequiredValue refuses a blank value for an attribute the
// item's category requires, when the setting is on.
func validateBlankedRequiredValue(tenantID string, payload map[string]interface{}) error {
	if !GetSettingBool(tenantID, SettingEnforceCategoryAttributeRules) {
		return nil
	}
	item, attribute := strField(payload, "item"), strField(payload, "attribute")
	if item == "" || attribute == "" {
		return nil
	}
	category := attributeItemCategory(tenantID, item)
	if category == "" {
		return nil
	}
	idx, err := loadCategoryRuleIndex(tenantID)
	if err != nil || idx == nil {
		return err
	}
	if r, ok := idx.effective(category)[attribute]; ok && r.Rule == CategoryRuleRequired {
		return &ValidationError{Code: "PIM-9300", SubFor: "Value", Message: fmt.Sprintf("%s is required for category %s and cannot be blank.", r.Label, category)}
	}
	return nil
}

// ValidateCategoryAttributeRuleDocument is the ValidateDocument hook for a rule.
func ValidateCategoryAttributeRuleDocument(tenantID string, payload map[string]interface{}) error {
	rule := strField(payload, "rule")
	if rule != CategoryRuleRequired && rule != CategoryRuleOptional && rule != CategoryRuleHidden {
		return &ValidationError{Code: "PIM-9302", SubFor: "rule", Message: "Rule must be Required, Optional or Hidden."}
	}
	category, attribute := strField(payload, "category"), strField(payload, "attribute")
	if def := strField(payload, "default_value"); def != "" {
		if rule == CategoryRuleHidden {
			return &ValidationError{Code: "PIM-9303", SubFor: "default_value", Message: "A hidden attribute has no default value."}
		}
		if _, err := CheckAttributeValue(tenantID, attribute, def); err != nil {
			return &ValidationError{Code: "PIM-9303", SubFor: "default_value", Message: "Default value: " + err.Error()}
		}
	}
	if category == "" {
		return nil
	}
	if schema, err := db.GetTenantSchema(tenantID); err == nil {
		var n int
		if db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'CategoryAttributeRule' AND deleted_at IS NULL AND data->>'category' = $1 AND data->>'attribute' = $2 AND id <> $3`, schema), category, attribute, strField(payload, "id")).Scan(&n) == nil && n > 0 {
			return &ValidationError{Code: "PIM-9304", SubFor: "attribute", Message: "This category already has a rule for that attribute. Edit it instead."}
		}
	}
	return nil
}

// --- Workbook import (sheet 05_Category_Rules) ---

type categoryRuleLibrary struct {
	Skipped int `json:"skipped"`
	Rules   []struct {
		Industry     string `json:"industry"`
		CategoryID   string `json:"category_id"`
		CategoryName string `json:"category_name"`
		Attribute    string `json:"attribute"`
		Rule         string `json:"rule"`
	} `json:"rules"`
}

// CategoryRuleImportResult is the preview or the outcome of an import.
type CategoryRuleImportResult struct {
	Applied         bool     `json:"applied"`
	ToAdd           int      `json:"to_add"`
	Added           int      `json:"added"`
	AlreadyThere    int      `json:"already_there"`
	NoCategory      int      `json:"no_category"`
	NoAttribute     int      `json:"no_attribute"`
	LeftOut         int      `json:"left_out"`
	MissingCategory []string `json:"missing_categories,omitempty"`
}

// ImportCategoryRules reads the workbook's category rules into
// CategoryAttributeRule documents for one client. A workbook category matches a
// client category by id or by name; its attribute must already be in the
// client's list. Mandatory becomes Required, Optional stays Optional; a
// Conditional or Regulatory row is counted and left out (the workbook says it is
// not the same as always required). A rule the client already has is never
// changed, so re-applying adds nothing twice. apply false only counts.
func ImportCategoryRules(tenantID, actor, industry string, apply bool) (*CategoryRuleImportResult, error) {
	var lib categoryRuleLibrary
	if err := json.Unmarshal(categoryRuleLibraryJSON, &lib); err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	res := &CategoryRuleImportResult{Applied: apply, LeftOut: lib.Skipped}
	collect := func(q string, each func(a, b string)) error {
		rows, err := db.DB.Query(fmt.Sprintf(q, schema))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a, b string
			if err := rows.Scan(&a, &b); err != nil {
				return err
			}
			each(a, b)
		}
		return rows.Err()
	}
	byKey := map[string]string{} // id or lower(name) -> category id
	if err := collect(`SELECT id, lower(COALESCE(data->>'name','')) FROM %s.documents WHERE doctype = 'Category' AND deleted_at IS NULL ORDER BY id`, func(id, name string) {
		byKey[id] = id
		if _, dup := byKey[name]; name != "" && !dup {
			byKey[name] = id
		}
	}); err != nil {
		return nil, err
	}
	haveAttr := map[string]bool{}
	if err := collect(`SELECT id, '' FROM %s.documents WHERE doctype = 'ProductAttributeDef' AND deleted_at IS NULL`, func(id, _ string) { haveAttr[id] = true }); err != nil {
		return nil, err
	}
	existing := map[string]bool{}
	if err := collect(`SELECT COALESCE(data->>'category',''), COALESCE(data->>'attribute','') FROM %s.documents WHERE doctype = 'CategoryAttributeRule' AND deleted_at IS NULL`, func(c, a string) { existing[c+"|"+a] = true }); err != nil {
		return nil, err
	}

	missing := map[string]bool{}
	for _, r := range lib.Rules {
		if industry != "" && !strings.EqualFold(r.Industry, industry) {
			continue
		}
		cat := byKey[r.CategoryID]
		if cat == "" {
			cat = byKey[strings.ToLower(r.CategoryName)]
		}
		if cat == "" {
			res.NoCategory++
			missing[r.CategoryName] = true
			continue
		}
		if !haveAttr[r.Attribute] {
			res.NoAttribute++
			continue
		}
		key := cat + "|" + r.Attribute
		if existing[key] {
			res.AlreadyThere++
			continue
		}
		existing[key] = true
		res.ToAdd++
		if !apply {
			continue
		}
		id := "CAR-" + cat + "-" + r.Attribute
		body, _ := json.Marshal(map[string]interface{}{
			"code": id, "category": cat, "attribute": r.Attribute, "rule": r.Rule, "status": "Active", "added_by": actor, "library_id": r.Attribute,
		})
		ins, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'CategoryAttributeRule', $2, 'Active', $3) ON CONFLICT DO NOTHING`, schema), id, body, actor)
		if err != nil {
			return res, err
		}
		if n, _ := ins.RowsAffected(); n > 0 {
			res.Added++
		}
	}
	for n := range missing {
		res.MissingCategory = append(res.MissingCategory, n)
	}
	sort.Strings(res.MissingCategory)
	if len(res.MissingCategory) > 20 {
		res.MissingCategory = res.MissingCategory[:20]
	}
	if apply && res.Added > 0 {
		LogAuditEvent(tenantID, actor, "CATEGORY_RULES_IMPORTED", "SUCCESS", fmt.Sprintf("%d category attribute rules added from the workbook", res.Added))
	}
	return res, nil
}
