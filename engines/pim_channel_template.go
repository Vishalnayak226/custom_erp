package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Stage 146.5.2 (D14): a marketplace template per channel + channel category,
// loaded from the marketplace's own category file. The file gives the columns,
// which are mandatory, the accepted values, the length limits and the
// "required when" rules; an export through the template then skips an item
// with the column and the reason instead of sending the marketplace a row it
// will reject, and a clean variation gets a parent row. A channel with no
// template exports exactly as before.

// ChannelTemplateColumn is one column of a marketplace category file.
type ChannelTemplateColumn struct {
	Column        string   `json:"column"`
	Mandatory     bool     `json:"mandatory"`
	Active        bool     `json:"active"`
	AllowedValues []string `json:"allowed_values"`
	MaxLength     int      `json:"max_length"`
	RequiredIf    string   `json:"required_if"`
}

// ChannelTemplateParsed is what a category file held.
type ChannelTemplateParsed struct {
	Columns  []ChannelTemplateColumn `json:"columns"`
	Warnings []string                `json:"warnings"`
}

func templateErr(code, sub, msg string) error {
	return &ValidationError{Code: code, SubFor: sub, Message: msg}
}

func normTemplateHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, "?:*")
	return strings.Join(strings.Fields(s), " ")
}

type templateHeaderCols struct{ name, required, allowed, maxLen, reqIf int }

// detectTemplateHeader finds the header row of a column-definitions sheet:
// one cell names the column, another says required / mandatory.
func detectTemplateHeader(row []string) (templateHeaderCols, bool) {
	h := templateHeaderCols{-1, -1, -1, -1, -1}
	for i, cell := range row {
		n := normTemplateHeader(cell)
		if n == "" {
			continue
		}
		switch {
		case strings.Contains(n, "required if") || strings.Contains(n, "conditional") || strings.Contains(n, "depends on"):
			if h.reqIf < 0 {
				h.reqIf = i
			}
		case strings.Contains(n, "required") || strings.Contains(n, "mandatory") || strings.Contains(n, "requirement"):
			if h.required < 0 {
				h.required = i
			}
		case strings.Contains(n, "accepted") || strings.Contains(n, "valid value") || strings.Contains(n, "allowed") ||
			strings.Contains(n, "possible value") || strings.Contains(n, "options"):
			if h.allowed < 0 {
				h.allowed = i
			}
		case strings.Contains(n, "max") && (strings.Contains(n, "length") || strings.Contains(n, "char")):
			if h.maxLen < 0 {
				h.maxLen = i
			}
		case n == "field name" || n == "field" || n == "column" || n == "column name" || n == "attribute" ||
			n == "attribute name" || n == "name" || n == "field label":
			if h.name < 0 {
				h.name = i
			}
		}
	}
	return h, h.name >= 0 && h.required >= 0
}

func cellAt(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func splitTemplateValues(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	sep := func(r rune) bool { return r == '|' || r == '\n' || r == ';' }
	parts := strings.FieldsFunc(s, sep)
	if len(parts) == 1 && strings.Contains(s, ",") {
		parts = strings.Split(s, ",")
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[strings.ToLower(p)] {
			continue
		}
		seen[strings.ToLower(p)] = true
		out = append(out, p)
	}
	return out
}

func templateMandatory(s string) bool {
	n := strings.ToLower(strings.TrimSpace(s))
	switch n {
	case "yes", "y", "true", "1":
		return true
	}
	return strings.HasPrefix(n, "required") || strings.HasPrefix(n, "mandatory")
}

func nonBlankRow(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return true
		}
	}
	return false
}

// ParseChannelTemplateXLSX reads a marketplace category file. It looks for a
// definitions sheet (a column-name header plus a required / mandatory header,
// optionally accepted values, max length and required-if); a separate sheet
// with one column of accepted values per attribute fills any accepted-values
// cell left blank. A file with no definitions sheet falls back to the first
// row's headers, all optional, with a warning.
func ParseChannelTemplateXLSX(data []byte) (*ChannelTemplateParsed, error) {
	sheets, err := ReadXLSX(data)
	if err != nil {
		return nil, templateErr("PIM-0953", "File", "the category file could not be read: "+err.Error())
	}
	out := &ChannelTemplateParsed{Columns: []ChannelTemplateColumn{}}
	defSheet := -1
	seen := map[string]bool{}
	for si, sh := range sheets {
		for ri := 0; ri < len(sh.Rows) && ri < 25; ri++ {
			h, ok := detectTemplateHeader(sh.Rows[ri])
			if !ok {
				continue
			}
			for _, row := range sh.Rows[ri+1:] {
				name := cellAt(row, h.name)
				if name == "" || seen[strings.ToLower(name)] {
					continue
				}
				seen[strings.ToLower(name)] = true
				c := ChannelTemplateColumn{Column: name, Active: true,
					Mandatory:     templateMandatory(cellAt(row, h.required)),
					AllowedValues: splitTemplateValues(cellAt(row, h.allowed)),
					RequiredIf:    cellAt(row, h.reqIf)}
				if v := regexp.MustCompile(`\d+`).FindString(cellAt(row, h.maxLen)); v != "" {
					c.MaxLength, _ = strconv.Atoi(v)
				}
				out.Columns = append(out.Columns, c)
			}
			defSheet = si
			break
		}
		if defSheet >= 0 {
			break
		}
	}
	if defSheet < 0 {
		for _, sh := range sheets {
			for _, row := range sh.Rows {
				if !nonBlankRow(row) {
					continue
				}
				for _, cell := range row {
					name := strings.TrimSpace(cell)
					if name != "" && !seen[strings.ToLower(name)] {
						seen[strings.ToLower(name)] = true
						out.Columns = append(out.Columns, ChannelTemplateColumn{Column: name, Active: true})
					}
				}
				break
			}
			if len(out.Columns) > 0 {
				break
			}
		}
		if len(out.Columns) > 0 {
			out.Warnings = append(out.Warnings, "no column-definitions sheet found; the first row was used and every column is optional")
		}
	} else {
		// accepted values from a wide sheet: header row = attribute names
		byName := map[string]int{}
		for i, c := range out.Columns {
			byName[strings.ToLower(c.Column)] = i
		}
		for si, sh := range sheets {
			if si == defSheet {
				continue
			}
			start := -1
			for ri, row := range sh.Rows {
				if nonBlankRow(row) {
					start = ri
					break
				}
			}
			if start < 0 {
				continue
			}
			for ci, head := range sh.Rows[start] {
				idx, ok := byName[strings.ToLower(strings.TrimSpace(head))]
				if !ok || len(out.Columns[idx].AllowedValues) > 0 {
					continue
				}
				var vals []string
				dup := map[string]bool{}
				for _, row := range sh.Rows[start+1:] {
					v := cellAt(row, ci)
					if v != "" && !dup[strings.ToLower(v)] && len(vals) < 500 {
						dup[strings.ToLower(v)] = true
						vals = append(vals, v)
					}
				}
				out.Columns[idx].AllowedValues = vals
			}
		}
	}
	if len(out.Columns) == 0 {
		return nil, templateErr("PIM-0953", "File", "no columns were found in the category file")
	}
	return out, nil
}

// ChannelTemplate is a loaded ChannelCategoryTemplate.
type ChannelTemplate struct {
	ID, Channel, Category, Status                 string
	Columns                                       []ChannelTemplateColumn
	ParentRows                                    bool
	ParentageColumn, ParentSKUColumn, ThemeColumn string
	SKUColumn                                     string
}

func channelTemplateColumnsJSON(cols []ChannelTemplateColumn) []map[string]string {
	yn := func(b bool) string {
		if b {
			return "Yes"
		}
		return "No"
	}
	out := make([]map[string]string, 0, len(cols))
	for _, c := range cols {
		ml := ""
		if c.MaxLength > 0 {
			ml = strconv.Itoa(c.MaxLength)
		}
		out = append(out, map[string]string{"column": c.Column, "mandatory": yn(c.Mandatory), "active": yn(c.Active),
			"allowed_values": strings.Join(c.AllowedValues, " | "), "max_length": ml, "required_if": c.RequiredIf})
	}
	return out
}

func decodeChannelTemplateColumns(raw interface{}) ([]ChannelTemplateColumn, error) {
	var rows []map[string]interface{}
	if err := decodeProductGroupJSON(raw, &rows); err != nil {
		return nil, fmt.Errorf("columns must be a JSON array of column rows: %w", err)
	}
	cols := make([]ChannelTemplateColumn, 0, len(rows))
	for _, r := range rows {
		c := ChannelTemplateColumn{Column: strings.TrimSpace(pimString(r["column"])),
			Mandatory:     strings.EqualFold(pimString(r["mandatory"]), "Yes"),
			Active:        !strings.EqualFold(pimString(r["active"]), "No"),
			AllowedValues: splitTemplateValues(pimString(r["allowed_values"])),
			RequiredIf:    strings.TrimSpace(pimString(r["required_if"]))}
		if ml := strings.TrimSpace(pimString(r["max_length"])); ml != "" {
			n, err := strconv.Atoi(ml)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("column %q: max length %q is not a number", c.Column, ml)
			}
			c.MaxLength = n
		}
		cols = append(cols, c)
	}
	return cols, nil
}

type requiredIfCond struct {
	column string
	values []string // empty = any non-blank value
}

// parseRequiredIf reads "Material=Gold", "Material=Gold|Silver", "Material",
// several joined with ";" (any one matching makes the column required).
func parseRequiredIf(s string) ([]requiredIfCond, error) {
	var out []requiredIfCond
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		c := requiredIfCond{column: strings.TrimSpace(kv[0])}
		if c.column == "" {
			return nil, fmt.Errorf("required-if %q names no column", s)
		}
		if len(kv) == 2 {
			for _, v := range strings.Split(kv[1], "|") {
				if v = strings.TrimSpace(v); v != "" {
					c.values = append(c.values, v)
				}
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// ValidateChannelCategoryTemplateDocument runs at ValidateDocument's shared
// exit for the ChannelCategoryTemplate doctype.
func ValidateChannelCategoryTemplateDocument(tenantID string, payload map[string]interface{}) error {
	channel := strings.TrimSpace(pimString(payload["channel"]))
	if channel != "" && db.DB != nil {
		ok, err := verifyDocumentExists(tenantID, "Channel", channel)
		if err != nil {
			return err
		}
		if !ok {
			return &ValidationError{Code: "META-0198", SubFor: "Channel", Message: fmt.Sprintf("linked Channel record %q does not exist", channel)}
		}
	}
	if strings.TrimSpace(pimString(payload["channel_category"])) == "" {
		return templateErr("PIM-0954", "Channel Category", "a template needs the channel's category name")
	}
	cols, err := decodeChannelTemplateColumns(payload["columns"])
	if err != nil {
		return templateErr("PIM-0954", "Columns", err.Error())
	}
	if len(cols) == 0 {
		return templateErr("PIM-0954", "Columns", "a template needs at least one column")
	}
	seen := map[string]bool{}
	for i, c := range cols {
		if c.Column == "" {
			return templateErr("PIM-0954", "Columns", fmt.Sprintf("column %d has no name", i+1))
		}
		if seen[strings.ToLower(c.Column)] {
			return templateErr("PIM-0954", "Columns", fmt.Sprintf("column %q is listed more than once", c.Column))
		}
		seen[strings.ToLower(c.Column)] = true
	}
	for _, c := range cols {
		conds, err := parseRequiredIf(c.RequiredIf)
		if err != nil {
			return templateErr("PIM-0954", "Columns", fmt.Sprintf("column %q: %v", c.Column, err))
		}
		for _, cond := range conds {
			if !seen[strings.ToLower(cond.column)] {
				return templateErr("PIM-0954", "Columns", fmt.Sprintf("column %q is required if %q, which is not a column of this template", c.Column, cond.column))
			}
		}
	}
	if v := strings.TrimSpace(pimString(payload["parent_rows"])); v != "" && v != "Yes" && v != "No" {
		return templateErr("PIM-0954", "Parent Rows", "parent rows must be Yes or No")
	}
	return nil
}

func cctSlug(s string) string {
	s = regexp.MustCompile(`[^A-Za-z0-9]+`).ReplaceAllString(strings.ToUpper(strings.TrimSpace(s)), "-")
	return strings.Trim(s, "-")
}

func decodeChannelTemplate(id, status string, data map[string]interface{}) (*ChannelTemplate, error) {
	cols, err := decodeChannelTemplateColumns(data["columns"])
	if err != nil {
		return nil, err
	}
	return &ChannelTemplate{ID: id, Status: status, Columns: cols,
		Channel: pimString(data["channel"]), Category: strings.TrimSpace(pimString(data["channel_category"])),
		ParentRows:      pimString(data["parent_rows"]) == "Yes",
		ParentageColumn: pimString(data["parentage_column"]), ParentSKUColumn: pimString(data["parent_sku_column"]),
		ThemeColumn: pimString(data["variation_theme_column"]), SKUColumn: pimString(data["sku_column"])}, nil
}

// FetchChannelTemplate loads a template by id or code.
func FetchChannelTemplate(tenantID, idOrCode string) (*ChannelTemplate, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var id, status, raw string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, status, data FROM %s.documents
		WHERE doctype = 'ChannelCategoryTemplate' AND (id = $1 OR UPPER(data->>'code') = UPPER($1)) AND deleted_at IS NULL
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END LIMIT 1`, schema), idOrCode).Scan(&id, &status, &raw)
	if err != nil {
		return nil, templateErr("PIM-0955", "Template", fmt.Sprintf("channel template %q not found", idOrCode))
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}
	return decodeChannelTemplate(id, status, data)
}

// ChannelTemplateOptions are the choices made when a file is loaded.
type ChannelTemplateOptions struct {
	ParentRows                                    bool
	ParentageColumn, ParentSKUColumn, ThemeColumn string
	SKUColumn                                     string
	FileName                                      string
}

// ChannelTemplateLoadResult tells what a load made and what still needs care.
type ChannelTemplateLoadResult struct {
	TemplateID       string   `json:"template_id"`
	ExportTemplateID string   `json:"export_template_id"`
	Columns          int      `json:"columns"`
	Mandatory        int      `json:"mandatory"`
	Replaced         bool     `json:"replaced"`
	Added            []string `json:"added"`
	Removed          []string `json:"removed"`
	Unmapped         []string `json:"unmapped"`
	Warnings         []string `json:"warnings"`
}

func findTemplateColumn(cols []ChannelTemplateColumn, match func(n string) bool) string {
	for _, c := range cols {
		if match(normTemplateHeader(c.Column)) {
			return c.Column
		}
	}
	return ""
}

// LoadChannelTemplate reads a category file and creates (or replaces) the
// channel's template for that category, plus the export template that uses it.
// Re-loading keeps each column's active flag; columns that left the file are
// listed.
func LoadChannelTemplate(tenantID, user, channel, category string, file []byte, opt ChannelTemplateOptions) (*ChannelTemplateLoadResult, error) {
	channel, category = strings.TrimSpace(channel), strings.TrimSpace(category)
	if channel == "" || category == "" {
		return nil, templateErr("PIM-0954", "Channel", "choose the channel and give its category name")
	}
	ok, err := verifyDocumentExists(tenantID, "Channel", channel)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &ValidationError{Code: "META-0198", SubFor: "Channel", Message: fmt.Sprintf("linked Channel record %q does not exist", channel)}
	}
	parsed, err := ParseChannelTemplateXLSX(file)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	res := &ChannelTemplateLoadResult{Added: []string{}, Removed: []string{}, Unmapped: []string{}, Warnings: parsed.Warnings}
	if res.Warnings == nil {
		res.Warnings = []string{}
	}

	id := "CCT-" + cctSlug(channel) + "-" + cctSlug(category)
	var oldID, oldRaw string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'ChannelCategoryTemplate'
		AND UPPER(data->>'channel') = UPPER($1) AND UPPER(TRIM(data->>'channel_category')) = UPPER($2) AND deleted_at IS NULL LIMIT 1`, schema),
		channel, category).Scan(&oldID, &oldRaw)
	if oldID != "" {
		id = oldID
		res.Replaced = true
		var old map[string]interface{}
		if json.Unmarshal([]byte(oldRaw), &old) == nil {
			oldCols, _ := decodeChannelTemplateColumns(old["columns"])
			had := map[string]ChannelTemplateColumn{}
			for _, c := range oldCols {
				had[strings.ToLower(c.Column)] = c
			}
			now := map[string]bool{}
			for i, c := range parsed.Columns {
				now[strings.ToLower(c.Column)] = true
				if o, ok := had[strings.ToLower(c.Column)]; ok {
					parsed.Columns[i].Active = o.Active
				} else {
					res.Added = append(res.Added, c.Column)
				}
			}
			for _, c := range oldCols {
				if !now[strings.ToLower(c.Column)] {
					res.Removed = append(res.Removed, c.Column)
				}
			}
		}
	}

	cols := parsed.Columns
	sku := opt.SKUColumn
	if sku == "" {
		sku = findTemplateColumn(cols, func(n string) bool {
			return n == "sku" || n == "seller sku" || n == "item sku" || n == "item code" || n == "product code" ||
				(strings.Contains(n, "sku") && !strings.Contains(n, "parent"))
		})
	}
	parentage, parentSKU, theme := opt.ParentageColumn, opt.ParentSKUColumn, opt.ThemeColumn
	if opt.ParentRows {
		if parentage == "" {
			parentage = findTemplateColumn(cols, func(n string) bool { return strings.Contains(n, "parentage") })
		}
		if parentSKU == "" {
			parentSKU = findTemplateColumn(cols, func(n string) bool {
				return strings.Contains(n, "parent") && (strings.Contains(n, "sku") || strings.Contains(n, "id") || strings.Contains(n, "code"))
			})
		}
		if theme == "" {
			theme = findTemplateColumn(cols, func(n string) bool { return strings.Contains(n, "theme") })
		}
		if sku == "" || parentage == "" || parentSKU == "" {
			res.Warnings = append(res.Warnings, "parent rows need a SKU, a parentage and a parent SKU column; they will not be added until all three are set")
		}
	}
	yesNo := "No"
	if opt.ParentRows {
		yesNo = "Yes"
	}
	data := map[string]interface{}{"code": id, "channel": channel, "channel_category": category, "status": "Active",
		"columns": channelTemplateColumnsJSON(cols), "parent_rows": yesNo, "parentage_column": parentage,
		"parent_sku_column": parentSKU, "variation_theme_column": theme, "sku_column": sku, "source_file": opt.FileName}
	if err := ValidateChannelCategoryTemplateDocument(tenantID, data); err != nil {
		return nil, err
	}
	if err := upsertTemplateDoc(schema, id, "ChannelCategoryTemplate", data, user, res.Replaced); err != nil {
		return nil, err
	}

	// the export template that uses it: one column per file column, in file order
	targets, err := channelFieldMapTargets(tenantID, channel)
	if err != nil {
		return nil, err
	}
	generated := map[string]bool{strings.ToLower(parentage): opt.ParentRows, strings.ToLower(parentSKU): opt.ParentRows, strings.ToLower(theme): opt.ParentRows}
	mappings := make([]map[string]string, 0, len(cols))
	for _, c := range cols {
		key := c.Column
		if sku != "" && strings.EqualFold(c.Column, sku) {
			key = "item_code"
		}
		lk := strings.ToLower(key)
		if !pimExportChannelFields[lk] && !targets[lk] && !(generated[lk] && lk != "") {
			res.Unmapped = append(res.Unmapped, c.Column)
		}
		mappings = append(mappings, map[string]string{"field_key": key, "column_header": c.Column})
		if c.Mandatory {
			res.Mandatory++
		}
	}
	res.Columns = len(cols)
	exportID := "TPL-" + cctSlug(channel) + "-" + cctSlug(category)
	var exists bool
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT TRUE FROM %s.documents WHERE id = $1 AND doctype = 'PIMExportTemplate'`, schema), exportID).Scan(&exists)
	exportData := map[string]interface{}{"code": exportID, "name": channel + " - " + category, "channel": channel,
		"column_mappings": mappings, "headerless": "No", "variant_mode": "All Rows", "channel_template": id, "status": "Active",
		"description": "Loaded from " + opt.FileName}
	if err := upsertTemplateDoc(schema, exportID, "PIMExportTemplate", exportData, user, exists); err != nil {
		return nil, err
	}
	res.TemplateID, res.ExportTemplateID = id, exportID
	return res, nil
}

func upsertTemplateDoc(schema, id, doctype string, data map[string]interface{}, user string, exists bool) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if exists {
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $2, status = $3, updated_at = NOW() WHERE id = $1`, schema), id, body, pimString(data["status"]))
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents(id, doctype, data, status, created_by) VALUES($1,$2,$3,$4,$5)`, schema), id, doctype, body, pimString(data["status"]), user)
	return err
}

// validateRecord checks one export row against the template. record is in
// export-column order; cols are the export template's columns. It returns the
// first failing column and every reason, or "" when the row is fine.
func (t *ChannelTemplate) validateRecord(cols []PIMExportColumn, record []string) (column, reason string) {
	byHeader := map[string]string{}
	for i, c := range cols {
		byHeader[strings.ToLower(strings.TrimSpace(c.ColumnHeader))] = strings.TrimSpace(record[i])
	}
	skip := map[string]bool{}
	if t.ParentRows {
		for _, c := range []string{t.ParentageColumn, t.ParentSKUColumn, t.ThemeColumn} {
			skip[strings.ToLower(c)] = true
		}
	}
	var reasons []string
	for _, c := range t.Columns {
		lc := strings.ToLower(c.Column)
		if !c.Active || skip[lc] {
			continue
		}
		val, present := byHeader[lc]
		if !present {
			continue
		}
		fail := ""
		switch {
		case val == "":
			if c.Mandatory {
				fail = "is required and blank"
			} else if conds, _ := parseRequiredIf(c.RequiredIf); len(conds) > 0 {
				for _, cond := range conds {
					other := byHeader[strings.ToLower(cond.column)]
					hit := other != "" && len(cond.values) == 0
					for _, v := range cond.values {
						if strings.EqualFold(other, v) {
							hit = true
						}
					}
					if hit {
						fail = "is required when " + cond.column + " is " + other
						break
					}
				}
			}
		default:
			if len(c.AllowedValues) > 0 {
				found := false
				for _, a := range c.AllowedValues {
					if strings.EqualFold(a, val) {
						found = true
						break
					}
				}
				if !found {
					fail = fmt.Sprintf("value %q is not an accepted value", val)
				}
			}
			if fail == "" && c.MaxLength > 0 && utf8.RuneCountInString(val) > c.MaxLength {
				fail = fmt.Sprintf("is %d characters, the limit is %d", utf8.RuneCountInString(val), c.MaxLength)
			}
		}
		if fail != "" {
			if column == "" {
				column = c.Column
			}
			reasons = append(reasons, c.Column+" "+fail)
		}
	}
	return column, strings.Join(reasons, "; ")
}

type variationPair struct{ name, value string }

func orderedVariationPairs(raw string) []variationPair {
	var out []variationPair
	for _, p := range strings.Split(raw, ";") {
		kv := strings.SplitN(p, ":", 2)
		name := strings.TrimSpace(kv[0])
		if name == "" {
			continue
		}
		v := ""
		if len(kv) == 2 {
			v = strings.TrimSpace(kv[1])
		}
		out = append(out, variationPair{name, v})
	}
	return out
}

// addParentRows puts a parent row before the children of every design whose
// variation is clean: at least two combinations, the same non-empty axes on
// each, and no two with the same values. A design that is not clean keeps its
// combinations as standalone rows. Parent rows are not validated.
func (t *ChannelTemplate) addParentRows(tenantID string, cols []PIMExportColumn, codes []string, records [][]string) [][]string {
	idx := map[string]int{}
	for i, c := range cols {
		idx[strings.ToLower(strings.TrimSpace(c.ColumnHeader))] = i
	}
	at := func(name string) int {
		if i, ok := idx[strings.ToLower(strings.TrimSpace(name))]; ok && name != "" {
			return i
		}
		return -1
	}
	skuI, parentageI, parentSKUI, themeI := at(t.SKUColumn), at(t.ParentageColumn), at(t.ParentSKUColumn), at(t.ThemeColumn)
	if skuI < 0 || parentageI < 0 || parentSKUI < 0 {
		return records
	}
	type child struct {
		parent string
		pairs  []variationPair
	}
	kids := make([]child, len(codes))
	byDesign := map[string][]int{}
	rowOf := map[string]int{}
	for i, code := range codes {
		rowOf[code] = i
		item, _, err := fetchItemDoc(tenantID, code)
		if err != nil {
			continue
		}
		p := strings.TrimSpace(pimString(item["parent_product_code"]))
		if p == "" {
			continue
		}
		kids[i] = child{p, orderedVariationPairs(pimString(item["variant_option_values"]))}
		byDesign[p] = append(byDesign[p], i)
	}
	clean := map[string][]string{} // design -> axis names
	for design, members := range byDesign {
		if len(members) < 2 {
			continue
		}
		first := kids[members[0]].pairs
		if len(first) == 0 {
			continue
		}
		ok := true
		combos := map[string]bool{}
		for _, m := range members {
			ps := kids[m].pairs
			if len(ps) != len(first) {
				ok = false
				break
			}
			key := ""
			for k, p := range ps {
				if !strings.EqualFold(p.name, first[k].name) || p.value == "" {
					ok = false
				}
				key += strings.ToLower(p.value) + "\x00"
			}
			if !ok || combos[key] {
				ok = false
				break
			}
			combos[key] = true
		}
		if ok {
			names := make([]string, len(first))
			for k, p := range first {
				names[k] = p.name
			}
			clean[design] = names
		}
	}
	if len(clean) == 0 {
		return records
	}
	themeOf := func(design string) string { return strings.Join(clean[design], "-") }
	out := make([][]string, 0, len(records)+len(clean))
	emitted := map[string]bool{}
	for i, rec := range records {
		if _, isDesignRow := clean[codes[i]]; isDesignRow && kids[i].parent == "" {
			continue // the design's own item row is placed with its children
		}
		design := kids[i].parent
		if _, ok := clean[design]; !ok {
			out = append(out, rec)
			continue
		}
		if !emitted[design] {
			emitted[design] = true
			var parent []string
			if j, ok := rowOf[design]; ok {
				parent = append([]string(nil), records[j]...)
			} else {
				parent = append([]string(nil), rec...)
				parent[skuI] = design
				for _, axis := range clean[design] {
					if ai := at(axis); ai >= 0 {
						parent[ai] = ""
					}
				}
			}
			parent[parentageI], parent[parentSKUI] = "Parent", ""
			if themeI >= 0 {
				parent[themeI] = themeOf(design)
			}
			out = append(out, parent)
		}
		child := append([]string(nil), rec...)
		child[parentageI], child[parentSKUI] = "Child", design
		if themeI >= 0 {
			child[themeI] = themeOf(design)
		}
		out = append(out, child)
	}
	return out
}

// exportChannelTemplate returns the template an export template points at,
// when it has one and it is active; nil otherwise (export as before).
func exportChannelTemplate(tenantID, exportTemplateID string) *ChannelTemplate {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	var ref string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'channel_template','') FROM %s.documents WHERE id = $1`, schema), exportTemplateID).Scan(&ref)
	if ref == "" {
		return nil
	}
	t, err := FetchChannelTemplate(tenantID, ref)
	if err != nil || t.Status != "Active" {
		return nil
	}
	return t
}

// templateColumnNames is what ValidatePIMExportTemplateDocument adds to a
// channel's allowed column keys when the template names a channel template.
func templateColumnNames(tenantID, ref string) []string {
	if ref == "" || db.DB == nil {
		return nil
	}
	t, err := FetchChannelTemplate(tenantID, ref)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(t.Columns))
	for _, c := range t.Columns {
		names = append(names, strings.ToLower(c.Column))
	}
	return names
}

// TemplateCoverageColumn is how well one template column is filled.
type TemplateCoverageColumn struct {
	Column    string `json:"column"`
	Mandatory bool   `json:"mandatory"`
	Active    bool   `json:"active"`
	Mapped    bool   `json:"mapped"`
	Filled    int    `json:"filled"`
}

// TemplateCoverage counts, over the items in the template's category, how many
// fill each column.
type TemplateCoverage struct {
	TemplateID      string                   `json:"template_id"`
	Items           int                      `json:"items"`
	Capped          bool                     `json:"capped"`
	Columns         []TemplateCoverageColumn `json:"columns"`
	MandatoryTotal  int                      `json:"mandatory_total"`
	MandatoryMapped int                      `json:"mandatory_mapped"`
	OptionalTotal   int                      `json:"optional_total"`
	OptionalMapped  int                      `json:"optional_mapped"`
}

const templateCoverageCap = 500

func ChannelTemplateCoverage(tenantID, templateID string) (*TemplateCoverage, error) {
	t, err := FetchChannelTemplate(tenantID, templateID)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	targets, err := channelFieldMapTargets(tenantID, t.Channel)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT i.id FROM %[1]s.documents i
		JOIN %[1]s.documents m ON m.doctype = 'ChannelCategoryMap' AND m.deleted_at IS NULL AND m.data->>'channel' = $1
		 AND m.data->>'erp_category' = i.data->>'category' AND UPPER(TRIM(m.data->>'channel_category')) = UPPER($2)
		WHERE i.doctype = 'Item' AND i.deleted_at IS NULL AND i.status <> 'Cancelled' ORDER BY i.id LIMIT %[2]d`, schema, templateCoverageCap+1), t.Channel, t.Category)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	cov := &TemplateCoverage{TemplateID: t.ID, Columns: []TemplateCoverageColumn{}}
	if len(ids) > templateCoverageCap {
		ids, cov.Capped = ids[:templateCoverageCap], true
	}
	cov.Items = len(ids)

	keyOf := func(c ChannelTemplateColumn) string {
		if t.SKUColumn != "" && strings.EqualFold(c.Column, t.SKUColumn) {
			return "item_code"
		}
		return strings.ToLower(c.Column)
	}
	generated := map[string]bool{}
	if t.ParentRows {
		for _, g := range []string{t.ParentageColumn, t.ParentSKUColumn, t.ThemeColumn} {
			if g != "" {
				generated[strings.ToLower(g)] = true
			}
		}
	}
	extra := channelExportExtras(tenantID, t.Channel)
	filled := make([]int, len(t.Columns))
	for _, id := range ids {
		payload, err := buildChannelPayload(tenantID, id, t.Channel, false)
		if err != nil {
			continue
		}
		for i, c := range t.Columns {
			key := keyOf(c)
			v, ok := extra(id, key)
			if !ok {
				v = channelExportFieldValue(payload, key)
			}
			if strings.TrimSpace(v) != "" {
				filled[i]++
			}
		}
	}
	for i, c := range t.Columns {
		key := keyOf(c)
		col := TemplateCoverageColumn{Column: c.Column, Mandatory: c.Mandatory, Active: c.Active, Filled: filled[i],
			Mapped: pimExportChannelFields[key] || targets[key] || generated[key]}
		cov.Columns = append(cov.Columns, col)
		if !c.Active {
			continue
		}
		if c.Mandatory {
			cov.MandatoryTotal++
			if col.Mapped {
				cov.MandatoryMapped++
			}
		} else {
			cov.OptionalTotal++
			if col.Mapped {
				cov.OptionalMapped++
			}
		}
	}
	return cov, nil
}
