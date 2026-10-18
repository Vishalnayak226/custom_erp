package engines

import (
	"fmt"
	"strings"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Stage 85.E8 (77.14): lines uploaded into a transaction from a template
// (Barcode, Qty). The screen parses the file; this says which item each
// code is - by item code, id or barcode - so a row that matches nothing is
// listed back to the user, never silently dropped.

// ResolvedLineCode is one uploaded code and the item it is, if any.
type ResolvedLineCode struct {
	Code  string `json:"code"`
	SKU   string `json:"sku"`
	Name  string `json:"name"`
	Found bool   `json:"found"`
}

// ResolveLineCodes maps codes to items, in the order given (at most 2000).
func ResolveLineCodes(tenantID string, codes []string) ([]ResolvedLineCode, error) {
	if len(codes) > 2000 {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: "Upload at most 2000 lines at a time."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	clean := make([]string, 0, len(codes))
	for _, c := range codes {
		if c = strings.TrimSpace(c); c != "" {
			clean = append(clean, c)
		}
	}
	found := map[string]ResolvedLineCode{}
	if len(clean) > 0 {
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT id, COALESCE(NULLIF(data->>'code', ''), id), COALESCE(data->>'barcode', ''), COALESCE(data->>'name', '')
			  FROM %s.documents
			 WHERE doctype = 'Item' AND deleted_at IS NULL
			   AND (id = ANY($1) OR data->>'code' = ANY($1) OR data->>'barcode' = ANY($1))`, schema), pq.Array(clean))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id, code, barcode, name string
			if err := rows.Scan(&id, &code, &barcode, &name); err != nil {
				return nil, err
			}
			r := ResolvedLineCode{SKU: code, Name: name, Found: true}
			for _, k := range []string{id, code, barcode} {
				if k != "" {
					if _, taken := found[k]; !taken {
						found[k] = r
					}
				}
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	out := make([]ResolvedLineCode, 0, len(codes))
	for _, c := range codes {
		r, ok := found[strings.TrimSpace(c)]
		if !ok {
			r = ResolvedLineCode{}
		}
		r.Code = c
		out = append(out, r)
	}
	return out, nil
}
