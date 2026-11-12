package engines

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 146.5.4: one wide product sheet. Item fields, attributes and content
// travel in one CSV: plain columns are Item fields, "attr:<attribute code>"
// columns are attribute values, "content:<language>:<field>" columns are
// content fields. One preview shows every part; the commit writes nothing if
// any row of any part fails, then writes all three.
//
// It reuses the neighbour import's row cap and upload check
// (readCSVRecordsContext), its Item validation and field permissions
// (runDocDataImportContext) and its blank / clear-marker rule. It is not the
// 47.13.1 staging pipeline: there is no staged area and no mapping profile.
//
// Content is only ever written as Draft: it is approval-gated, so a sheet
// cannot publish copy, and content that is Pending or Approved is refused
// rather than silently reopened.

var productSheetContentFields = map[string]bool{"title": true, "short_desc": true, "long_desc": true, "seo_title": true, "tags": true}

type ProductSheetResult struct {
	Committed      bool                 `json:"committed"`
	Rows           int                  `json:"rows"`
	Items          *ImportResult        `json:"items,omitempty"`
	Attributes     int                  `json:"attributes"`
	Contents       int                  `json:"contents"`
	UnknownHeaders []string             `json:"unknown_headers"`
	Errors         []RowValidationError `json:"errors"`
}

type sheetAttrOp struct {
	row        int
	item, attr string
	value      string
	clear      bool
	data       map[string]interface{} // the value as stored: item, attribute, value, source, confidence
}

type sheetContentOp struct {
	row        int
	item, lang string
	fields     map[string]string // "" clears the field
}

type sheetCol struct {
	idx        int
	kind       string // item, attr, content
	name, lang string
}

func sheetErr(code, msg string) error { return &ValidationError{Code: code, Message: msg} }

// classifySheetHeaders sorts the header row into Item, attribute and content
// columns; anything else is returned as unknown (listed, never guessed at).
func classifySheetHeaders(schema string, header []string, itemFields map[string]bool) (cols []sheetCol, unknown []string, idCol int) {
	idCol = -1
	unknown = []string{}
	for i, raw := range header {
		h := strings.TrimSpace(raw)
		lower := strings.ToLower(h)
		switch {
		case h == "":
		case strings.HasPrefix(lower, "attr:"):
			code := strings.TrimSpace(h[5:])
			if _, _, found := attributeDefType(schema, code); !found || code == "" {
				unknown = append(unknown, h)
				continue
			}
			cols = append(cols, sheetCol{idx: i, kind: "attr", name: code})
		case strings.HasPrefix(lower, "content:"):
			parts := strings.SplitN(lower[8:], ":", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || !productSheetContentFields[strings.TrimSpace(parts[1])] {
				unknown = append(unknown, h)
				continue
			}
			cols = append(cols, sheetCol{idx: i, kind: "content", lang: strings.TrimSpace(parts[0]), name: strings.TrimSpace(parts[1])})
		case itemFields[lower]:
			if lower == "id" {
				idCol = i
			}
			cols = append(cols, sheetCol{idx: i, kind: "item", name: lower})
		default:
			unknown = append(unknown, h)
		}
	}
	return cols, unknown, idCol
}

// ImportProductSheet previews (commit false) or commits (commit true) a wide
// product sheet. A commit writes nothing unless every part is clean.
func ImportProductSheet(ctx context.Context, tenantID string, r io.Reader, userID, role string, commit bool) (*ProductSheetResult, error) {
	records, err := readCSVRecordsContext(ctx, tenantID, newImportSourceReader(r))
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	meta, err := GetDocTypeMeta(tenantID, "Item")
	if err != nil {
		return nil, err
	}
	itemFields := map[string]bool{"id": true}
	for _, f := range meta {
		itemFields[f.Fieldname] = true
	}

	res := &ProductSheetResult{Rows: len(records) - 1, Errors: []RowValidationError{}}
	cols, unknown, idCol := classifySheetHeaders(schema, records[0], itemFields)
	res.UnknownHeaders = unknown
	if idCol < 0 {
		return nil, sheetErr("PIM-1000", "The sheet needs an id column holding the item code.")
	}
	var itemHeaders []string
	for _, c := range cols {
		if c.kind == "item" {
			itemHeaders = append(itemHeaders, c.name)
		}
	}
	// An Item's id is its code (master invariant), so the code column is filled from id when the sheet has none.
	fillCode := itemFields["code"] && !containsString(itemHeaders, "code")
	if fillCode {
		itemHeaders = append(itemHeaders, "code")
	}
	if missing := missingMandatoryColumns(tenantID, "Item", itemHeaders); len(missing) > 0 {
		return nil, &ValidationError{Code: "DATAIM-0164", Message: fmt.Sprintf("uploaded file is missing mandatory column(s): %s", strings.Join(missing, ", "))}
	}

	body := records[1:]
	docRows := make([]map[string]interface{}, len(body))
	var attrOps []sheetAttrOp
	contentByKey := map[string]*sheetContentOp{}
	var contentOrder []string
	addErr := func(row int, msg string) {
		res.Errors = append(res.Errors, RowValidationError{RowNumber: row, Message: msg})
	}
	for i, rec := range body {
		rowNo := i + 2
		cell := func(idx int) string {
			if idx >= len(rec) {
				return ""
			}
			return sanitizeCSVCell(strings.TrimSpace(rec[idx]))
		}
		doc := map[string]interface{}{}
		itemID := cell(idCol)
		for _, c := range cols {
			v := cell(c.idx)
			switch c.kind {
			case "item":
				doc[c.name] = v
			case "attr":
				if v == "" {
					continue
				}
				if itemID == "" {
					addErr(rowNo, "This row sets an attribute but has no item id.")
					continue
				}
				op := sheetAttrOp{row: rowNo, item: itemID, attr: c.name, value: v}
				if isImportClearMarker(v) {
					op.clear, op.value = true, ""
				} else {
					norm, cErr := CheckAttributeValue(tenantID, c.name, v)
					if cErr != nil {
						addErr(rowNo, cErr.Error())
						continue
					}
					op.value = norm
				}
				// 47.13.4: the sheet answers to the stewardship register like every
				// other door. Refused here, before the transaction opens, so preview
				// and commit name the same rows.
				op.data = map[string]interface{}{"item": itemID, "attribute": c.name, "value": op.value}
				StampStewardshipSource("ProductAttributeValue", op.data, "Import", userID)
				if gErr := StewardshipGuard(tenantID, op.data); gErr != nil {
					var ve *ValidationError
					if !errors.As(gErr, &ve) {
						return nil, gErr
					}
					addErr(rowNo, gErr.Error())
					continue
				}
				attrOps = append(attrOps, op)
			case "content":
				if v == "" {
					continue
				}
				if itemID == "" {
					addErr(rowNo, "This row sets content but has no item id.")
					continue
				}
				key := itemID + "::" + c.lang
				op := contentByKey[key]
				if op == nil {
					op = &sheetContentOp{row: rowNo, item: itemID, lang: c.lang, fields: map[string]string{}}
					contentByKey[key] = op
					contentOrder = append(contentOrder, key)
				}
				if isImportClearMarker(v) {
					v = ""
				}
				op.fields[c.name] = v
			}
		}
		if fillCode && itemID != "" {
			doc["code"] = itemID
		}
		docRows[i] = doc
	}

	// Content that is Pending or Approved is not reopened by a sheet.
	contentRows := map[string]map[string]interface{}{}
	for _, key := range contentOrder {
		op := contentByKey[key]
		var status, raw string
		var stored map[string]interface{}
		qerr := db.DB.QueryRow(fmt.Sprintf(`SELECT status, data FROM %s.documents WHERE doctype='ProductContent' AND id=$1 AND deleted_at IS NULL`, schema), key).Scan(&status, &raw)
		if qerr == nil {
			if status != "Draft" && status != "Rejected" {
				addErr(op.row, fmt.Sprintf("Content for %s (%s) is %s. Change it in the product workbench.", op.item, op.lang, status))
				continue
			}
			stored = map[string]interface{}{}
			_ = json.Unmarshal([]byte(raw), &stored)
		}
		contentRows[key] = mergeSheetContent(key, op, stored, userID)
	}

	pre := pimVariantParentPreflight(tenantID, docRows)
	items, err := runDocDataImportContext(ctx, tenantID, "Item", userID, role, true, docRows, pre)
	if err != nil {
		return nil, err
	}
	res.Items = items
	for _, e := range items.Errors {
		addErr(e.RowNumber, e.Message)
	}
	// Content is checked once the Item pass is known: an item this sheet creates does not exist yet.
	creating := map[string]bool{}
	for _, id := range items.CreatedIDs {
		creating[id] = true
	}
	for _, key := range contentOrder {
		merged, ok := contentRows[key]
		if !ok {
			continue
		}
		if vErr := ValidateDocument(tenantID, "ProductContent", merged); vErr != nil {
			if ve, isVE := vErr.(*ValidationError); isVE && ve.Code == "META-0198" && creating[contentByKey[key].item] {
				continue
			}
			addErr(contentByKey[key].row, vErr.Error())
			delete(contentRows, key)
		}
	}
	sort.SliceStable(res.Errors, func(a, b int) bool { return res.Errors[a].RowNumber < res.Errors[b].RowNumber })
	res.Attributes, res.Contents = len(attrOps), len(contentOrder)
	if !commit || len(res.Errors) > 0 || items.FailedRows > 0 {
		return res, nil
	}

	// One transaction for all three parts (148.53). Until it commits, res.Items
	// stays the preview's figures, so a refusal never reports rows as saved.
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	real, err := runDocDataImportInTx(ctx, tx, tenantID, "Item", userID, role, docRows, pre)
	if err != nil {
		return nil, err
	}
	if real.FailedRows > 0 {
		for _, e := range real.Errors {
			addErr(e.RowNumber, e.Message)
		}
		return res, nil
	}
	if userID == "" {
		userID = "system"
	}
	for _, op := range attrOps {
		if err := writeSheetAttr(tx, schema, op, userID); err != nil {
			return nil, err
		}
	}
	for _, key := range contentOrder {
		if err := writeSheetContent(tx, schema, key, contentRows[key], userID); err != nil {
			if errors.Is(err, errSheetContentMoved) {
				op := contentByKey[key]
				addErr(op.row, fmt.Sprintf("Content for %s (%s) changed while importing. Run the sheet again.", op.item, op.lang))
				return res, nil
			}
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	res.Items = real
	res.Committed = true
	for _, e := range real.pendingEdits {
		LogDocumentEdit(tenantID, userID, "Item", e.id, e.before, e.after, "CSV import")
	}
	LogAuditEvent(tenantID, userID, "PIM", "PRODUCT_SHEET_IMPORTED", fmt.Sprintf("rows=%d attributes=%d content=%d", res.Rows, res.Attributes, res.Contents))
	return res, nil
}

// mergeSheetContent lays the sheet's fields over what is stored (blank cells
// never reach here) and keeps the status Draft.
func mergeSheetContent(key string, op *sheetContentOp, stored map[string]interface{}, userID string) map[string]interface{} {
	m := map[string]interface{}{}
	for k, v := range stored {
		m[k] = v
	}
	m["id"], m["code"], m["product_id"], m["language"] = key, key, op.item, op.lang
	for f, v := range op.fields {
		m[f] = v
	}
	m["status"] = "Draft"
	if _, has := m["owner"]; !has && userID != "" {
		m["owner"] = userID
	}
	return m
}

func writeSheetAttr(tx *sql.Tx, schema string, op sheetAttrOp, user string) error {
	id := attributeValueID(op.item, op.attr, "", "")
	if op.clear {
		_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET deleted_at = NOW(), updated_at = NOW() WHERE doctype='ProductAttributeValue' AND id=$1 AND deleted_at IS NULL`, schema), id)
		return err
	}
	body, _ := json.Marshal(op.data)
	res, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data=$2, updated_at=NOW() WHERE doctype='ProductAttributeValue' AND id=$1 AND deleted_at IS NULL`, schema), id, body)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents(id, doctype, data, status, created_by) VALUES($1,'ProductAttributeValue',$2,'Active',$3)`, schema), id, body, user)
	return err
}

// errSheetContentMoved: the content was approved or submitted after the preview read it.
var errSheetContentMoved = errors.New("content changed while the sheet was importing; nothing was saved")

func writeSheetContent(tx *sql.Tx, schema, id string, data map[string]interface{}, user string) error {
	body, _ := json.Marshal(data)
	res, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data=$2, status='Draft', updated_at=NOW() WHERE doctype='ProductContent' AND id=$1 AND deleted_at IS NULL AND status IN ('Draft','Rejected')`, schema), id, body)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	var exists bool
	if err := tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype='ProductContent' AND id=$1 AND deleted_at IS NULL)`, schema), id).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return errSheetContentMoved
	}
	_, err = tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents(id, doctype, data, status, created_by) VALUES($1,'ProductContent',$2,'Draft',$3)`, schema), id, body, user)
	return err
}
