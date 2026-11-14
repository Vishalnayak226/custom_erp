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

// ResolvedLineCode is one uploaded code and the item it is, if any. A purchase
// order's upload (Design Number, Combination, Qty) checks the row's design
// against DesignNumber: the item's design's number, else its code, else the
// item's own design reference.
type ResolvedLineCode struct {
	Code         string `json:"code"`
	SKU          string `json:"sku"`
	Name         string `json:"name"`
	Found        bool   `json:"found"`
	Design       string `json:"design,omitempty"`
	DesignNumber string `json:"design_number,omitempty"`
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
			SELECT i.id, COALESCE(NULLIF(i.data->>'code', ''), i.id), COALESCE(i.data->>'barcode', ''), COALESCE(i.data->>'name', ''),
			       COALESCE(f.id, f.ref, ''), COALESCE(NULLIF(f.data->>'design_number', ''), NULLIF(f.data->>'code', ''), f.ref, '')
			  FROM %[1]s.documents i
			  LEFT JOIN LATERAL (
			       SELECT r.ref, d.id, d.data
			         FROM (SELECT COALESCE(NULLIF(i.data->>'family', ''), NULLIF(i.data->>'parent_product_code', '')) AS ref) r
			         LEFT JOIN %[1]s.documents d ON r.ref IS NOT NULL AND d.doctype = 'ProductFamily' AND d.deleted_at IS NULL
			              AND (d.id = r.ref OR d.data->>'code' = r.ref)
			        ORDER BY (d.id = r.ref) DESC NULLS LAST
			        LIMIT 1) f ON true
			 WHERE i.doctype = 'Item' AND i.deleted_at IS NULL
			   AND (i.id = ANY($1) OR i.data->>'code' = ANY($1) OR i.data->>'barcode' = ANY($1))`, schema), pq.Array(clean))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id, code, barcode, name, design, designNo string
			if err := rows.Scan(&id, &code, &barcode, &name, &design, &designNo); err != nil {
				return nil, err
			}
			r := ResolvedLineCode{SKU: code, Name: name, Found: true, Design: design, DesignNumber: designNo}
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
