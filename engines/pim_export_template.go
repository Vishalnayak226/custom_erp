package engines

import (
	"bytes"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Stage 36.4.1/36.4.3: PIMExportTemplate lets an operator choose which
// columns leave the building, in what order, under what header names (or no
// header row at all), and whether a variant collapses under its parent -
// rather than every export being the fixed shape StreamSearchFeedExportCSV/
// ExportPIMProductGroupCSV already produce. A blank `channel` exports the
// same raw ERP fields those two read; a set `channel` exports through
// BuildChannelPayload, so the columns on offer are that channel's own
// ChannelFieldMap target fields - "per-channel format" without a second
// payload-building path.

// pimExportRawFields is the closed set of raw-export column keys, matching
// searchFeedRow (engines/pim_reports.go) plus a variant_count column only a
// raw export can compute (a channel payload has no notion of "how many
// variants this parent has").
var pimExportRawFields = map[string]bool{
	"item_code": true, "name": true, "title": true, "short_desc": true,
	"tags": true, "family": true, "category": true,
	"completeness_score": true, "has_main_image": true, "variant_count": true,
}

// pimExportChannelFields is the closed set of channel-export column keys
// that BuildChannelPayload always produces, independent of any
// ChannelFieldMap row.
var pimExportChannelFields = map[string]bool{
	"item_code": true, "title": true, "description": true, "image_count": true,
	// 140.12.24: the main image's address and the channel's own category.
	"image_url": true, "channel_category": true,
}

// PIMExportColumn is one row of a PIMExportTemplate.column_mappings
// JSONTable. Column order IS array order - no separate sort field.
type PIMExportColumn struct {
	FieldKey     string `json:"field_key"`
	ColumnHeader string `json:"column_header"`
}

func decodePIMExportColumns(raw interface{}) ([]PIMExportColumn, error) {
	var rows []map[string]interface{}
	if err := decodeProductGroupJSON(raw, &rows); err != nil {
		return nil, fmt.Errorf("column_mappings must be a JSON array of column rows: %w", err)
	}
	cols := make([]PIMExportColumn, 0, len(rows))
	for _, row := range rows {
		cols = append(cols, PIMExportColumn{FieldKey: pimString(row["field_key"]), ColumnHeader: pimString(row["column_header"])})
	}
	return cols, nil
}

// channelFieldMapTargets returns the set of target_field values a channel's
// ChannelFieldMap rows publish - the same table BuildChannelPayload itself
// reads, so a template cannot offer a column the payload builder will never
// populate.
func channelFieldMapTargets(tenantID, channel string) (map[string]bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT DISTINCT COALESCE(data->>'target_field','') FROM %s.documents
		 WHERE doctype = 'ChannelFieldMap' AND data->>'channel' = $1`, schema), channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := map[string]bool{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		if t != "" {
			targets[strings.ToLower(t)] = true
		}
	}
	return targets, rows.Err()
}

// ValidatePIMExportTemplateDocument runs at ValidateDocument's shared exit -
// the same "the form cannot offer what the engine cannot run" guarantee
// 36.3.3's import-template validator established, mirrored for export.
func ValidatePIMExportTemplateDocument(tenantID string, payload map[string]interface{}) error {
	channel := strings.TrimSpace(pimString(payload["channel"]))
	if channel != "" && db.DB != nil {
		exists, err := verifyDocumentExists(tenantID, "Channel", channel)
		if err != nil {
			return err
		}
		if !exists {
			return &ValidationError{Code: "META-0198", SubFor: "Channel", Message: fmt.Sprintf("linked Channel record %q does not exist", channel)}
		}
	}

	cols, err := decodePIMExportColumns(payload["column_mappings"])
	if err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Columns", Message: err.Error()}
	}
	if len(cols) == 0 {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "Columns", Message: "an export template needs at least one column"}
	}

	var allowed map[string]bool
	if channel == "" {
		allowed = pimExportRawFields
	} else {
		allowed = map[string]bool{}
		for k := range pimExportChannelFields {
			allowed[k] = true
		}
		if db.DB != nil {
			targets, tErr := channelFieldMapTargets(tenantID, channel)
			if tErr != nil {
				return tErr
			}
			for k := range targets {
				allowed[k] = true
			}
		}
	}

	// 140.12.23: one field may feed two columns (Shopify's Handle and
	// Variant SKU are both the SKU); two columns may not share a header.
	seen := map[string]bool{}
	for i, c := range cols {
		key := strings.ToLower(strings.TrimSpace(c.FieldKey))
		if key == "" {
			return &ValidationError{Code: "GLOBAL-0001", SubFor: "Columns", Message: fmt.Sprintf("column %d has no field", i+1)}
		}
		header := strings.ToLower(strings.TrimSpace(c.ColumnHeader))
		if header == "" {
			return &ValidationError{Code: "GLOBAL-0001", SubFor: "Columns", Message: fmt.Sprintf("column %d has no header", i+1)}
		}
		if seen[header] {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Columns", Message: fmt.Sprintf("header %q is used more than once", c.ColumnHeader)}
		}
		seen[header] = true
		if db.DB != nil && !allowed[key] {
			if channel == "" {
				return &ValidationError{Code: "META-0198", SubFor: "Columns", Message: fmt.Sprintf("column %d field %q is not one of the raw export fields", i+1, c.FieldKey)}
			}
			return &ValidationError{Code: "META-0198", SubFor: "Columns", Message: fmt.Sprintf("column %d field %q is not published by channel %q (add it to that channel's field map first)", i+1, c.FieldKey, channel)}
		}
	}

	headerless := strings.TrimSpace(pimString(payload["headerless"]))
	if headerless != "" && headerless != "Yes" && headerless != "No" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Headerless Output", Message: "headerless must be Yes or No"}
	}
	variantMode := strings.TrimSpace(pimString(payload["variant_mode"]))
	if variantMode != "" && variantMode != "All Rows" && variantMode != "Parent Only - Variants Collapsed" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Variant Mode", Message: "variant_mode must be 'All Rows' or 'Parent Only - Variants Collapsed'"}
	}
	return nil
}

func fetchPIMExportTemplate(tenantID, templateID string) (canonicalID, channel string, cols []PIMExportColumn, headerless, variantMode bool, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", nil, false, false, err
	}
	var raw, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents
		WHERE doctype = 'PIMExportTemplate' AND (id = $1 OR UPPER(data->>'code') = UPPER($1)) AND deleted_at IS NULL
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END, id LIMIT 1`, schema), templateID).Scan(&canonicalID, &raw, &status)
	if err != nil {
		return "", "", nil, false, false, fmt.Errorf("export template %q not found", templateID)
	}
	if status != "Active" {
		return "", "", nil, false, false, fmt.Errorf("export template %q is not active", templateID)
	}
	var data map[string]interface{}
	if uErr := json.Unmarshal([]byte(raw), &data); uErr != nil {
		return "", "", nil, false, false, fmt.Errorf("export template %q has invalid stored data: %w", templateID, uErr)
	}
	cols, err = decodePIMExportColumns(data["column_mappings"])
	if err != nil {
		return "", "", nil, false, false, err
	}
	channel = strings.TrimSpace(pimString(data["channel"]))
	headerless = pimString(data["headerless"]) == "Yes"
	variantMode = pimString(data["variant_mode"]) == "Parent Only - Variants Collapsed"
	return canonicalID, channel, cols, headerless, variantMode, nil
}

// exportableItemCodes returns every non-cancelled Item, optionally collapsed
// to parents only (36.4.3): a row whose parent_product_code is set is a
// variant and is skipped, its parent (already in the list) standing in for
// the whole family. variantCounts maps a parent's code to how many variant
// rows it has, for the variant_count raw column.
func exportableItemCodes(tenantID string, parentOnly bool, filter ...PIMExportFilter) (codes []string, variantCounts map[string]int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, nil, err
	}
	var f PIMExportFilter
	if len(filter) > 0 {
		f = filter[0]
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, COALESCE(data->>'parent_product_code','') FROM %s.documents
		 WHERE doctype = 'Item' AND deleted_at IS NULL AND status <> 'Cancelled'
		   AND ($1 = '' OR data->>'family' = $1) AND ($2 = '' OR data->>'category' = $2) ORDER BY id`, schema),
		strings.TrimSpace(f.Family), strings.TrimSpace(f.Category))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	variantCounts = map[string]int{}
	var all []struct{ id, parent string }
	for rows.Next() {
		var id, parent string
		if err := rows.Scan(&id, &parent); err != nil {
			return nil, nil, err
		}
		all = append(all, struct{ id, parent string }{id, parent})
		if parent != "" {
			variantCounts[parent]++
		}
	}
	for _, r := range all {
		if parentOnly && r.parent != "" {
			continue
		}
		codes = append(codes, r.id)
	}
	return codes, variantCounts, rows.Err()
}

// RunPIMExportTemplate produces the CSV a template describes: chosen columns
// in chosen order under chosen headers (or no header row), scoped to every
// row (or parents only, variants collapsed), reading either raw ERP fields
// or - for a channel-bound template - the actual outbound payload each item
// would publish through BuildChannelPayload, so a channel export can never
// disagree with what a real publish sends.
func RunPIMExportTemplate(tenantID, templateID string) ([]byte, error) {
	out, err := RunPIMExport(tenantID, templateID, PIMExportFilter{})
	if err != nil {
		return nil, err
	}
	return out.Data, nil
}

// PIMExportFilter narrows a feed (140.12.24): a family, a category, and the
// file format - "csv" (default) or "xlsx" (Amazon and Myntra take sheets).
type PIMExportFilter struct {
	Family   string
	Category string
	Format   string
}

// PIMExportSkip is an item a channel feed left out, and why.
type PIMExportSkip struct {
	Item   string `json:"item"`
	Reason string `json:"reason"`
}

// PIMExportResult is a feed file and the items it skipped.
type PIMExportResult struct {
	Data        []byte
	ContentType string
	Extension   string
	Rows        int
	Skipped     []PIMExportSkip
}

// RunPIMExport is RunPIMExportTemplate with a filter and a format. An item
// the channel cannot take (a transform that does not apply, say) is left out
// and listed, instead of stopping the whole feed (140.12.24).
func RunPIMExport(tenantID, templateID string, f PIMExportFilter) (*PIMExportResult, error) {
	_, channel, cols, headerless, variantMode, err := fetchPIMExportTemplate(tenantID, templateID)
	if err != nil {
		return nil, err
	}

	codes, variantCounts, err := exportableItemCodes(tenantID, variantMode, f)
	if err != nil {
		return nil, err
	}

	res := &PIMExportResult{Skipped: []PIMExportSkip{}}
	var records [][]string
	if channel == "" {
		feed, fErr := fetchSearchFeedRows(tenantID, codes)
		if fErr != nil {
			return nil, fErr
		}
		for _, row := range feed {
			record := make([]string, len(cols))
			for i, c := range cols {
				record[i] = rawExportFieldValue(row, variantCounts[row.ItemCode], strings.ToLower(strings.TrimSpace(c.FieldKey)))
			}
			records = append(records, record)
		}
	} else {
		extra := channelExportExtras(tenantID, channel)
		for _, code := range codes {
			payload, pErr := BuildChannelPayload(tenantID, code, channel)
			if pErr != nil {
				res.Skipped = append(res.Skipped, PIMExportSkip{Item: code, Reason: pErr.Error()})
				continue
			}
			record := make([]string, len(cols))
			for i, c := range cols {
				key := strings.ToLower(strings.TrimSpace(c.FieldKey))
				if v, ok := extra(code, key); ok {
					record[i] = v
				} else {
					record[i] = channelExportFieldValue(payload, key)
				}
			}
			records = append(records, record)
		}
	}
	res.Rows = len(records)

	if strings.EqualFold(strings.TrimSpace(f.Format), "xlsx") {
		sheet := XLSXSheet{Name: "Feed"}
		if !headerless {
			head := make([]XLSXCell, len(cols))
			for i, c := range cols {
				head[i] = XLSXCell{Text: c.ColumnHeader, Header: true}
			}
			sheet.Rows = append(sheet.Rows, head)
			sheet.FreezeRows = 1
		}
		for _, r := range records {
			row := make([]XLSXCell, len(r))
			for i, v := range r {
				row[i] = XLSXCell{Text: v}
			}
			sheet.Rows = append(sheet.Rows, row)
		}
		data, err := WriteXLSX(sheet)
		if err != nil {
			return nil, err
		}
		res.Data, res.ContentType, res.Extension = data, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx"
		return res, nil
	}

	var buf bytes.Buffer
	writer := newSafeCSVWriter(&buf)
	if !headerless {
		headerRow := make([]string, len(cols))
		for i, c := range cols {
			headerRow[i] = c.ColumnHeader
		}
		_ = writer.Write(headerRow)
	}
	for _, r := range records {
		_ = writer.Write(r)
	}
	writer.Flush()
	res.Data, res.ContentType, res.Extension = buf.Bytes(), "text/csv", "csv"
	return res, writer.Error()
}

// channelExportExtras answers the two columns a channel payload does not
// carry (140.12.24): image_url - the item's main picture when its media
// path is a public http(s) address - and channel_category - the item's
// category as the channel's category map names it.
func channelExportExtras(tenantID, channel string) func(item, key string) (string, bool) {
	schema, _ := db.GetTenantSchema(tenantID)
	return func(item, key string) (string, bool) {
		var v string
		switch key {
		case "image_url":
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'file_path', '') FROM %s.documents
				WHERE doctype = 'ProductMedia' AND data->>'item' = $1 AND deleted_at IS NULL AND status = 'Active'
				  AND data->>'file_path' ~* '^https?://'
				ORDER BY (data->>'media_role' = 'Main') DESC, COALESCE((data->>'sort_order')::numeric, 0), id LIMIT 1`, schema), item).Scan(&v)
		case "channel_category":
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(m.data->>'channel_category', '') FROM %s.documents m, %s.documents i
				WHERE m.doctype = 'ChannelCategoryMap' AND m.deleted_at IS NULL AND m.data->>'channel' = $1
				  AND i.doctype = 'Item' AND i.id = $2 AND m.data->>'erp_category' = i.data->>'category' LIMIT 1`, schema, schema), channel, item).Scan(&v)
		default:
			return "", false
		}
		return v, true
	}
}

func rawExportFieldValue(row searchFeedRow, variantCount int, key string) string {
	switch key {
	case "item_code":
		return row.ItemCode
	case "name":
		return row.Name
	case "title":
		return row.Title
	case "short_desc":
		return row.ShortDesc
	case "tags":
		return row.Tags
	case "family":
		return row.Family
	case "category":
		return row.Category
	case "completeness_score":
		return strconv.FormatFloat(row.CompletenessScore, 'f', 1, 64)
	case "has_main_image":
		return strconv.FormatBool(row.HasMainImage)
	case "variant_count":
		return strconv.Itoa(variantCount)
	default:
		return ""
	}
}

// channelExportFieldValue looks up an export column's value against a built
// payload. The attribute lookup is case-insensitive against
// payload.Attributes: those keys carry ChannelFieldMap.target_field's own
// original casing, while every field_key comparison in this file is
// lowercased for a forgiving, typo-tolerant match - so the lookup here must
// fold case too, or a target field like "ProductType" would silently never
// match a column keyed "producttype".
func channelExportFieldValue(payload *ChannelProductPayload, key string) string {
	switch key {
	case "item_code":
		return payload.ItemCode
	case "title":
		return payload.Title
	case "description":
		return payload.Description
	case "image_count":
		return strconv.Itoa(len(payload.Images))
	default:
		for attrKey, attrVal := range payload.Attributes {
			if strings.EqualFold(attrKey, key) {
				return attrVal
			}
		}
		return ""
	}
}
