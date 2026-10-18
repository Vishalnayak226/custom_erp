package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Stage 75.3.4: a register report from a declaration, so the catalog's many
// "list every X with these columns" reports (75.3.5-75.3.7) are a column map
// each, not a hand-written query each. A register reads one doctype, maps
// columns to document fields, resolves master ids to names in one query per
// master, offers the standard filters (date range, store, status), is cut to
// the user's stores (75.3.3) and can show item images and column totals.

// RegisterColumn is one column of a register.
type RegisterColumn struct {
	Key, Label string
	// Field is the document field ("customer", "items.0.sku" is not
	// supported - registers list documents, not lines); "id", "status" and
	// "created_at" read the record itself.
	Field string
	// Lookup "<Doctype>.<field>" shows that master's field for the id the
	// document holds (Customer.name for a customer id).
	Lookup string
	// Type "image": Field holds an item SKU, the value is its main image.
	Type      string
	Sensitive bool
	Total     bool
}

// RegisterSpec declares a register.
type RegisterSpec struct {
	ID, Label, Category, Description string
	Doctype                          string
	// DateField filters the date range ("" = when the record was made).
	DateField string
	// StoreField is the document's store ("" = "location", "-" = none).
	StoreField string
	// Statuses are offered in the status filter (empty = no filter).
	Statuses []string
	Columns  []RegisterColumn
}

const registerRowCap = 20000

// RegisterDocumentRegister turns a spec into a catalog report.
func RegisterDocumentRegister(s RegisterSpec) {
	storeField := s.StoreField
	if storeField == "" {
		storeField = "location"
	}
	params := []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}}
	storeKey := "-"
	if storeField != "-" {
		params = append(params, ReportParam{Key: "location", Label: "Store", Type: "text"})
		storeKey = "_store"
	}
	if len(s.Statuses) > 0 {
		params = append(params, ReportParam{Key: "status", Label: "Status", Type: "select", Options: "," + strings.Join(s.Statuses, ",")})
	}
	cols := make([]ReportColumn, 0, len(s.Columns))
	for _, c := range s.Columns {
		cols = append(cols, ReportColumn{Key: c.Key, Label: c.Label, Sensitive: c.Sensitive, Type: c.Type, Total: c.Total})
	}
	spec := s
	RegisterReport(ReportDefinition{
		ID: s.ID, Label: s.Label, Category: s.Category, Description: s.Description,
		Columns: cols, Params: params, StoreKey: storeKey,
		Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
			return runDocumentRegister(tenantID, spec, storeField, p)
		},
	})
}

func registerFieldValue(id, status string, created time.Time, data map[string]interface{}, field string) interface{} {
	switch field {
	case "id":
		return id
	case "status":
		return status
	case "created_at":
		return created.Format("2006-01-02 15:04")
	}
	return data[field]
}

func runDocumentRegister(tenantID string, s RegisterSpec, storeField string, p map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	where := []string{"doctype = $1", "deleted_at IS NULL"}
	args := []interface{}{s.Doctype}
	add := func(cond string, v interface{}) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	dateExpr := "to_char(created_at, 'YYYY-MM-DD')"
	if s.DateField != "" {
		dateExpr = fmt.Sprintf("LEFT(COALESCE(data->>'%s', to_char(created_at, 'YYYY-MM-DD')), 10)", strings.ReplaceAll(s.DateField, "'", ""))
	}
	if v := strings.TrimSpace(p["from"]); v != "" {
		add(dateExpr+" >= $%d", v)
	}
	if v := strings.TrimSpace(p["to"]); v != "" {
		add(dateExpr+" <= $%d", v)
	}
	if storeField != "-" {
		if v := strings.TrimSpace(p["location"]); v != "" {
			add(fmt.Sprintf("data->>'%s' = ANY($%%d)", strings.ReplaceAll(storeField, "'", "")), pq.Array(strings.Split(v, ",")))
		}
	}
	if v := strings.TrimSpace(p["status"]); v != "" {
		add("status = $%d", v)
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(status, ''), created_at, data FROM %s.documents WHERE %s ORDER BY created_at DESC LIMIT %d`,
		schema, strings.Join(where, " AND "), registerRowCap), args...)
	if err != nil {
		return nil, err
	}
	type rec struct {
		id, status string
		created    time.Time
		data       map[string]interface{}
	}
	var recs []rec
	for rows.Next() {
		var r rec
		var raw []byte
		if rows.Scan(&r.id, &r.status, &r.created, &raw) != nil {
			continue
		}
		_ = json.Unmarshal(raw, &r.data)
		if r.data == nil {
			r.data = map[string]interface{}{}
		}
		recs = append(recs, r)
	}
	rows.Close()

	// One query per master looked up, and one for the images.
	lookups := map[string]map[string]string{} // "Doctype.field" -> id -> value
	want := map[string]map[string]bool{}
	var skus []string
	seenSKU := map[string]bool{}
	for _, r := range recs {
		for _, c := range s.Columns {
			v := strings.TrimSpace(fmt.Sprint(registerFieldValue(r.id, r.status, r.created, r.data, c.Field)))
			if v == "" || v == "<nil>" {
				continue
			}
			if c.Lookup != "" {
				if want[c.Lookup] == nil {
					want[c.Lookup] = map[string]bool{}
				}
				want[c.Lookup][v] = true
			}
			if c.Type == "image" && !seenSKU[v] {
				seenSKU[v] = true
				skus = append(skus, v)
			}
		}
	}
	for lk, ids := range want {
		parts := strings.SplitN(lk, ".", 2)
		if len(parts) != 2 {
			continue
		}
		list := make([]string, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		lookups[lk] = map[string]string{}
		lrows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>$3, '') FROM %s.documents WHERE doctype = $1 AND id = ANY($2)`, schema), parts[0], pq.Array(list), parts[1])
		if err != nil {
			continue
		}
		for lrows.Next() {
			var id, val string
			if lrows.Scan(&id, &val) == nil {
				lookups[lk][id] = val
			}
		}
		lrows.Close()
	}
	images := map[string]string{}
	if len(skus) > 0 {
		irows, err := db.DB.Query(fmt.Sprintf(`
			SELECT DISTINCT ON (data->>'item') data->>'item', id FROM %s.documents
			WHERE doctype = 'ProductMedia' AND status = 'Active' AND data->>'item' = ANY($1) AND COALESCE(data->>'file_type', '') LIKE 'image/%%'
			ORDER BY data->>'item', (data->>'media_role' = 'Main Image') DESC, created_at`, schema), pq.Array(skus))
		if err == nil {
			for irows.Next() {
				var sku, id string
				if irows.Scan(&sku, &id) == nil {
					images[sku] = id
				}
			}
			irows.Close()
		}
	}

	out := make([]map[string]interface{}, 0, len(recs))
	for _, r := range recs {
		row := map[string]interface{}{}
		for _, c := range s.Columns {
			v := registerFieldValue(r.id, r.status, r.created, r.data, c.Field)
			key := strings.TrimSpace(fmt.Sprint(v))
			switch {
			case c.Type == "image":
				row[c.Key] = images[key]
			case c.Lookup != "":
				if name := lookups[c.Lookup][key]; name != "" {
					row[c.Key] = name
				} else {
					row[c.Key] = v // an id the master no longer has still shows
				}
			default:
				row[c.Key] = v
			}
		}
		if storeField != "-" {
			row["_store"] = strField(r.data, storeField) // for store scope (75.3.3); not a column
		}
		out = append(out, row)
	}
	return out, nil
}
