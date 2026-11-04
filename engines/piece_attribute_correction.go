package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 148.40: correct a piece's attributes. Purity, karat and weights are
// captured at the GRN (Stage 89.4.3) onto the receipt's Batch (attributes);
// a hallmark re-test or a weight after hallmarking had no way back in. A
// correction rewrites ONLY the attribute values on the Batch or SerialNumber
// and appends a history row (who, when, old, new, why) to its attribute_history.
//
// What it does not touch: the purchase link (grn, vendor, batch_no, received_on),
// the sale links (status, reserved_for, ledger lines) and cost (receipt_cost_paise
// and receipt_cost_qty). Cost reads none of these attributes: the GRN line's
// actual weight feeds the vendor-bill match, and that line is not edited here.
// A piece already sold can be corrected: a hallmark re-test is a fact about the
// piece, not about its stock, and refusing would leave the record wrong. The
// history row carries the status the piece had.

// PieceAttributeField is one attribute that may be corrected.
type PieceAttributeField struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Number  bool     `json:"number,omitempty"`
	Options []string `json:"options,omitempty"`
}

// PieceAttributeChange is one history row.
type PieceAttributeChange struct {
	At     string            `json:"at"`
	By     string            `json:"by"`
	Reason string            `json:"reason"`
	Status string            `json:"status,omitempty"`
	Old    map[string]string `json:"old"`
	New    map[string]string `json:"new"`
}

// PieceAttributes is a piece's current attribute values with its history.
type PieceAttributes struct {
	Doctype string                 `json:"doctype"`
	ID      string                 `json:"id"`
	Item    string                 `json:"item"`
	Ref     string                 `json:"ref"`
	Status  string                 `json:"status"`
	Values  map[string]string      `json:"values"`
	Fields  []PieceAttributeField  `json:"fields"`
	History []PieceAttributeChange `json:"history"`
}

func pieceDoctypeOK(doctype string) bool { return doctype == "Batch" || doctype == "SerialNumber" }

func pieceCorrectableFields(tenantID string) ([]PieceAttributeField, error) {
	rules, err := ItemFieldRules(tenantID)
	if err != nil {
		return nil, err
	}
	var out []PieceAttributeField
	for _, r := range rules {
		if r.Hidden || r.Fixed || (r.Kind == "attribute" && r.LinkedField != "") {
			continue
		}
		out = append(out, PieceAttributeField{Key: r.Key, Label: r.Label, Number: r.Type == "Number", Options: r.Options})
	}
	return out, nil
}

func pieceDecode(raw []byte) (map[string]interface{}, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func pieceHistory(data map[string]interface{}) []PieceAttributeChange {
	var out []PieceAttributeChange
	if raw, _ := data["attribute_history"].(string); strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func pieceView(tenantID, doctype, id string, data map[string]interface{}, status string) (*PieceAttributes, error) {
	fields, err := pieceCorrectableFields(tenantID)
	if err != nil {
		return nil, err
	}
	v := &PieceAttributes{Doctype: doctype, ID: id, Item: strField(data, "item"), Status: status, Fields: fields,
		Values: parseBatchAttributes(strField(data, "attributes")), History: pieceHistory(data)}
	if v.Values == nil {
		v.Values = map[string]string{}
	}
	if doctype == "Batch" {
		v.Ref = strField(data, "batch_no")
	} else {
		v.Ref = strField(data, "serial_no")
		// A serial with no values of its own shows its batch's (what the receipt recorded).
		if len(v.Values) == 0 {
			if schema, err := db.GetTenantSchema(tenantID); err == nil {
				for k, val := range batchItemValues(schema, v.Item, strField(data, "batch_no")) {
					v.Values[k] = val
				}
			}
		}
	}
	if v.History == nil {
		v.History = []PieceAttributeChange{}
	}
	return v, nil
}

var errPieceNotFound = &ValidationError{Code: "PURCHA-5040", Message: "That batch or serial was not found."}

// GetPieceAttributes returns a Batch's or SerialNumber's attribute values,
// the fields that may be corrected, and its correction history.
func GetPieceAttributes(tenantID, doctype, id string) (*PieceAttributes, error) {
	if !pieceDoctypeOK(doctype) {
		return nil, &ValidationError{Code: "PURCHA-5040", Message: "Only a batch or a serial can be corrected."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	var status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data, COALESCE(status, '') FROM %s.documents
		WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema), doctype, id).Scan(&raw, &status)
	if err == sql.ErrNoRows {
		return nil, errPieceNotFound
	}
	if err != nil {
		return nil, err
	}
	data, err := pieceDecode(raw)
	if err != nil {
		return nil, err
	}
	return pieceView(tenantID, doctype, id, data, status)
}

// CorrectPieceAttributes sets the given attribute values on a batch or serial.
// A reason is required; values equal to today's are ignored and nothing
// changed is refused; an empty value clears that attribute. The row and its
// history are written in one statement, then the audit event.
func CorrectPieceAttributes(tenantID, userID, doctype, id string, values map[string]string, reason string) (*PieceAttributes, error) {
	if !pieceDoctypeOK(doctype) {
		return nil, &ValidationError{Code: "PURCHA-5040", Message: "Only a batch or a serial can be corrected."}
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, &ValidationError{Code: "PURCHA-5041", Message: "Say why the attributes are being corrected."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	fields, err := pieceCorrectableFields(tenantID)
	if err != nil {
		return nil, err
	}
	known := map[string]PieceAttributeField{}
	for _, f := range fields {
		known[f.Key] = f
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var raw []byte
	var status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, COALESCE(status, '') FROM %s.documents
		WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL FOR UPDATE`, schema), doctype, id).Scan(&raw, &status); err == sql.ErrNoRows {
		return nil, errPieceNotFound
	} else if err != nil {
		return nil, err
	}
	data, err := pieceDecode(raw)
	if err != nil {
		return nil, err
	}
	cur := parseBatchAttributes(strField(data, "attributes"))
	if cur == nil {
		cur = map[string]string{}
	}
	oldVals, newVals := map[string]string{}, map[string]string{}
	next := map[string]string{}
	for k, v := range cur {
		next[k] = v
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f, ok := known[k]
		v := strings.TrimSpace(values[k])
		if !ok {
			return nil, &ValidationError{Code: "PURCHA-5042", Message: fmt.Sprintf("%s is not a field that can be corrected.", k)}
		}
		if v != "" && f.Number {
			if n, err := strconv.ParseFloat(v, 64); err != nil || n < 0 {
				return nil, &ValidationError{Code: "PURCHA-5042", Message: fmt.Sprintf("%s needs a number, not negative.", f.Label)}
			}
		}
		if v == cur[k] {
			continue
		}
		oldVals[k], newVals[k] = cur[k], v
		if v == "" {
			delete(next, k)
		} else {
			next[k] = v
		}
	}
	if len(newVals) == 0 {
		return nil, &ValidationError{Code: "PURCHA-5043", Message: "Nothing was changed."}
	}
	hist := append(pieceHistory(data), PieceAttributeChange{
		At: time.Now().UTC().Format(time.RFC3339), By: userID, Reason: reason, Status: status, Old: oldVals, New: newVals})
	attrJSON, _ := json.Marshal(next)
	histJSON, _ := json.Marshal(hist)
	patch, _ := json.Marshal(map[string]interface{}{"attributes": string(attrJSON), "attribute_history": string(histJSON)})
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP, updated_by = $4, edit_reason = $5
		WHERE doctype = $2 AND id = $3`, schema), patch, doctype, id, userID, reason); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	data["attributes"], data["attribute_history"] = string(attrJSON), string(histJSON)
	return pieceView(tenantID, doctype, id, data, status)
}
