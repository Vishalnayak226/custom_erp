package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Stage 73.2.8 - post-sale edit history.
//
// A sale document may still be edited after it has left Draft - a sales
// invoice's notes or due date, an order's reference, a paid till bill by an
// administrator. Each such edit now needs a reason, and leaves a record of
// who changed what, from what to what, and why: a DocumentEditHistory entry
// per edit, one row per changed field, listed by the Post-Sale Edit History
// report. The reason is checked at the generic save's shared choke point
// (ValidateTransactionalRules), so every current and future caller of the
// generic document API is covered; the history is written straight after the
// save commits.

// postSaleDoctypes is which documents count as a sale once past these
// statuses. A doctype or status absent here is not a post-sale edit.
var postSaleDoctypes = map[string]func(status string) bool{
	"SalesInvoice": func(s string) bool { return s != "" && s != "Draft" },
	"SalesOrder":   func(s string) bool { return s != "" && s != "Draft" },
	"POSCart":      func(s string) bool { return s == "Paid" || s == "Settled" },
}

// postSaleDocLabels names each in the report (and is its filter's options).
var postSaleDocLabels = map[string]string{"SalesInvoice": "Sales Invoice", "SalesOrder": "Sales Order", "POSCart": "Till bill"}

// saleDocNumber is the number people know the document by.
func saleDocNumber(docID string, docs ...map[string]interface{}) string {
	for _, d := range docs {
		for _, k := range []string{"invoice_number", "cart_number", "order_number", "code"} {
			if v := strings.TrimSpace(strField(d, k)); v != "" {
				return v
			}
		}
	}
	return docID
}

// isPostSaleEdit is whether an edit of a document in priorStatus is a
// post-sale edit.
func isPostSaleEdit(doctype, docID, priorStatus string, priorData map[string]interface{}) bool {
	if docID == "" || priorData == nil {
		return false
	}
	match, ok := postSaleDoctypes[doctype]
	return ok && match(priorStatus)
}

// validatePostSaleEdit requires a reason for a post-sale edit (edit_reason).
func validatePostSaleEdit(doctype, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	if !isPostSaleEdit(doctype, docID, priorStatus, priorData) || len(saleEditChanges(priorData, payload)) == 0 {
		return nil
	}
	if reason, _ := payload["edit_reason"].(string); strings.TrimSpace(reason) == "" {
		return &ValidationError{Code: "SALESP-0616", SubFor: "edit_reason", Message: fmt.Sprintf(
			"%s %s has already been issued - say why it is being changed (edit_reason); the change is kept in its edit history", doctype, docID)}
	}
	return nil
}

// saleEditIgnored are keys that change on every save or are not business data.
var saleEditIgnored = map[string]bool{"edit_reason": true, "updated_at": true, "version": true, "expected_version": true}

// SaleEditChange is one changed field.
type SaleEditChange struct {
	Field  string      `json:"field"`
	Label  string      `json:"label,omitempty"`
	Before interface{} `json:"before"`
	After  interface{} `json:"after"`
}

func saleEditChanges(before, after map[string]interface{}) []SaleEditChange {
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	var out []SaleEditChange
	for k := range keys {
		if saleEditIgnored[k] {
			continue
		}
		b, a := before[k], after[k]
		if !reflect.DeepEqual(normaliseEditValue(b), normaliseEditValue(a)) {
			out = append(out, SaleEditChange{Field: k, Before: b, After: a})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

// normaliseEditValue makes a stored value and a submitted one comparable
// (numbers decode as float64 either way; "" and a missing key are the same).
func normaliseEditValue(v interface{}) interface{} {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return n
		}
		return s
	}
	raw, _ := json.Marshal(v)
	var out interface{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// RecordPostSaleEdit writes the history of a post-sale edit that has just been
// saved, with the reason the save was given. A no-op for anything that is not
// one, or that changed nothing.
func RecordPostSaleEdit(tenantID, doctype, docID, priorStatus string, priorData, payload map[string]interface{}, actor, reason string) error {
	if !isPostSaleEdit(doctype, docID, priorStatus, priorData) {
		return nil
	}
	changes := saleEditChanges(priorData, payload)
	if len(changes) == 0 {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	labels := map[string]string{}
	if rows, err := db.DB.Query(fmt.Sprintf(`SELECT fieldname, label FROM %s.doctype_fields WHERE doctype_name = $1`, schema), doctype); err == nil {
		for rows.Next() {
			var f, l string
			if rows.Scan(&f, &l) == nil {
				labels[f] = l
			}
		}
		rows.Close()
	}
	for i := range changes {
		changes[i].Label = labels[changes[i].Field]
	}
	id := NewDocID("EDIT")
	data, _ := json.Marshal(map[string]interface{}{
		"code": id, "document_type": doctype, "document_id": docID, "document_number": saleDocNumber(docID, payload, priorData), "document_status": priorStatus,
		"edited_by": actor, "reason": strings.TrimSpace(reason), "changes": changes,
		"edited_at": time.Now().Format(time.RFC3339),
	})
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'DocumentEditHistory', $2, 'Active', 'system')`, schema), id, data)
	return err
}

func postSaleEditReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	from, to := strings.TrimSpace(params["from_date"]), strings.TrimSpace(params["to_date"])
	if from == "" {
		from = "1900-01-01"
	}
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}
	docType := strings.TrimSpace(params["document_type"])
	for dt, label := range postSaleDocLabels {
		if strings.EqualFold(docType, label) {
			docType = dt
		}
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data, to_char(created_at, 'YYYY-MM-DD HH24:MI') FROM %s.documents
		WHERE doctype = 'DocumentEditHistory' AND created_at >= $1::date AND created_at < ($2::date + 1)
		  AND ($3 = '' OR data->>'document_type' = $3) AND ($4 = '' OR data->>'document_id' = $4 OR data->>'document_number' ILIKE $4)
		ORDER BY created_at DESC LIMIT 2000`, schema), from, to, docType, strings.TrimSpace(params["document_id"]))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var dataStr, at string
		if err := rows.Scan(&dataStr, &at); err != nil {
			return nil, err
		}
		var h struct {
			DocType string           `json:"document_type"`
			DocID   string           `json:"document_id"`
			Number  string           `json:"document_number"`
			Status  string           `json:"document_status"`
			By      string           `json:"edited_by"`
			Reason  string           `json:"reason"`
			Changes []SaleEditChange `json:"changes"`
		}
		_ = json.Unmarshal([]byte(dataStr), &h)
		docLabel, number := postSaleDocLabels[h.DocType], h.Number
		if docLabel == "" {
			docLabel = h.DocType
		}
		if number == "" {
			number = h.DocID
		}
		for _, c := range h.Changes {
			field := c.Label
			if field == "" {
				field = c.Field
			}
			out = append(out, map[string]interface{}{"edited_at": at, "document_type": docLabel, "document_id": number, "status": h.Status,
				"field": field, "before": editValueText(c.Before), "after": editValueText(c.After), "edited_by": h.By, "reason": h.Reason})
		}
	}
	return out, rows.Err()
}

func editValueText(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	raw, _ := json.Marshal(v)
	if len(raw) > 200 {
		return string(raw[:200]) + "..."
	}
	return string(raw)
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "post-sale-edit-history", Label: "Post-Sale Edit History", Category: "Sales",
		Columns: []ReportColumn{{Key: "edited_at", Label: "When"}, {Key: "document_type", Label: "Document"}, {Key: "document_id", Label: "Number"},
			{Key: "status", Label: "Status then"}, {Key: "field", Label: "Field"}, {Key: "before", Label: "Before"}, {Key: "after", Label: "After"},
			{Key: "edited_by", Label: "Changed by"}, {Key: "reason", Label: "Reason"}},
		Params: []ReportParam{{Key: "from_date", Label: "From", Type: "date"}, {Key: "to_date", Label: "To", Type: "date"},
			{Key: "document_type", Label: "Document type", Type: "select", Options: ",Sales Invoice,Sales Order,Till bill"}, {Key: "document_id", Label: "Number", Type: "text"}},
		Run: postSaleEditReport,
	})
}
