package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Stage 74.1.1, register R-TX-12: goods rejected or damaged at receipt and
// goods sent back to vendors, by reason - the code where one was picked, the
// typed reason (flagged "unmapped", DEC-21) where none was.
func init() {
	RegisterReport(ReportDefinition{
		ID: "rejections-by-reason", Label: "Rejections and Returns by Reason", Category: "Procurement",
		Columns: []ReportColumn{
			{Key: "kind", Label: "What"}, {Key: "reason_code", Label: "Reason code"}, {Key: "reason", Label: "Reason"},
			{Key: "vendor", Label: "Vendor"}, {Key: "documents", Label: "Documents"}, {Key: "qty", Label: "Qty"},
		},
		Params:   []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}, {Key: "vendor", Label: "Vendor (blank = all)", Type: "text"}},
		StoreKey: "-",
		Run:      rejectionsByReasonReport,
	})
	reportQuestions["rejections-by-reason"] = reportQuestion{"Why are goods rejected or sent back?", "Rejected and damaged units at receipt and returns to vendors, by reason and vendor."}
}

func rejectionsByReasonReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var args []interface{}
	where := grnDateFilter("created_at", params, &args) + grnVendorFilter("data->>'vendor'", params, &args)
	type key struct{ kind, code, reason, vendor string }
	type agg struct {
		docs map[string]bool
		qty  float64
	}
	groups := map[key]*agg{}
	add := func(k key, doc string, qty float64) {
		if k.code == "" {
			k.code = "(unmapped)"
		}
		a := groups[k]
		if a == nil {
			a = &agg{docs: map[string]bool{}}
			groups[k] = a
		}
		a.docs[doc] = true
		a.qty += qty
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'GRN' AND deleted_at IS NULL AND status <> 'Cancelled' %s LIMIT 20000`, schema, where), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, dataStr string
		if rows.Scan(&id, &dataStr) != nil {
			continue
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(dataStr), &data) != nil {
			continue
		}
		doc := firstNonEmpty(strField(data, "code"), id)
		for _, l := range grnLineMaps(data) {
			if q := numFromInterface(l["rejected_qty"]); q > 0 {
				add(key{"Rejected at receipt", strField(l, "rejection_reason_code"), strField(l, "rejection_reason"), strField(data, "vendor")}, doc, q)
			}
			if q := numFromInterface(l["damaged_qty"]); q > 0 {
				add(key{"Damaged at receipt", strField(l, "damage_reason_code"), strField(l, "damage_reason"), strField(data, "vendor")}, doc, q)
			}
		}
	}
	rows.Close()
	args = nil
	where = grnDateFilter("created_at", params, &args)
	rows, err = db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'PurchaseReturn' AND deleted_at IS NULL AND status <> 'Cancelled' %s LIMIT 20000`, schema, where), args...)
	if err != nil {
		return nil, err
	}
	vendorWanted := strings.TrimSpace(params["vendor"])
	for rows.Next() {
		var id, dataStr string
		if rows.Scan(&id, &dataStr) != nil {
			continue
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(dataStr), &data) != nil {
			continue
		}
		vendor := firstNonEmpty(strField(data, "vendor_id"), strField(data, "vendor"))
		if vendorWanted != "" && vendor != vendorWanted {
			continue
		}
		var lines []map[string]interface{}
		_ = json.Unmarshal([]byte(strField(data, "return_items")), &lines)
		qty := 0.0
		for _, l := range lines {
			qty += numFromInterface(l["qty"])
		}
		add(key{"Returned to vendor", strField(data, "reason_code"), strField(data, "reason"), vendor}, firstNonEmpty(strField(data, "code"), id), qty)
	}
	rows.Close()
	out := []map[string]interface{}{}
	for k, a := range groups {
		docs := make([]string, 0, len(a.docs))
		for d := range a.docs {
			docs = append(docs, d)
		}
		sort.Strings(docs)
		out = append(out, map[string]interface{}{"kind": k.kind, "reason_code": k.code, "reason": k.reason, "vendor": k.vendor,
			"documents": strings.Join(docs, ", "), "qty": round2(a.qty)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["qty"].(float64) > out[j]["qty"].(float64) })
	return out, nil
}
