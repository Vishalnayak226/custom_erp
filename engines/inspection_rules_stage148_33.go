package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"custom_erp/db"
)

// Stage 148.33: inspection asked for by rule. With the setting on, a goods
// receipt of an item that needs a check (or from a vendor a rule says to check)
// puts the received lot into qc_hold in the same transaction and files an
// InspectionLot. The lot is sampled, the results are entered (a blank result
// is a Fail, the lines total the lot), and the lot is released to available
// stock, part released, or left held when it fails. The vendor bill waits
// while the lot is pending. With the setting off, or no rule and no flagged
// item, a receipt posts exactly as before.
const (
	SettingInspectionByRule    = "quality.inspection_by_rule"
	SettingInspectionSamplePct = "quality.inspection_sample_pct"

	InspectionLotDoctype  = "InspectionLot"
	InspectionRuleDoctype = "InspectionRule"

	lotPending  = "Pending"
	lotHeld     = "Held" // a list filter, not a status
	lotReleased = "Released"
	lotPart     = "Part released"
	lotRejected = "Rejected"
	lotWaived   = "Waived"

	qcModeMandatory = "QC mandatory"
	qcModeDecide    = "Warehouse may decide"

	inspectionOffCode   = "QUALIT-0602"
	inspectionLotCode   = "QUALIT-0601"
	inspectionInputCode = "QUALIT-0600"
	inspectionTypedCode = "QUALIT-0603"
	inspectionWaiveCode = "QUALIT-0604"
	inspectionBillCode  = "GOODSR-1330"
)

func init() {
	RegisterSetting(SettingDefinition{Key: SettingInspectionByRule, Module: "Quality", Label: "Inspection asked for by rule",
		Type: SettingTypeBool, Default: "false",
		Description: "On: a receipt of an item marked Quality check, or from a vendor an Inspection rule names, holds the lot until it is inspected, and the vendor bill waits for the result. Off: receipts post as they always did."})
	RegisterSetting(SettingDefinition{Key: SettingInspectionSamplePct, Module: "Quality", Label: "Sample size",
		Type: SettingTypeInt, Default: "10", Unit: "%", Min: settingBound(1), Max: settingBound(100),
		Description: "Share of a lot that is inspected, at least one unit. A Skip lot rule lowers it for a vendor with clean lots in a row."})
	for _, f := range []string{"quality_required", "qc_mode"} {
		for _, dt := range []string{"Item", "ItemType"} {
			if settingGatedFields[dt] == nil {
				settingGatedFields[dt] = map[string]string{}
			}
			settingGatedFields[dt][f] = SettingInspectionByRule
		}
	}
	settingGatedDoctypes[InspectionRuleDoctype] = SettingInspectionByRule
	settingGatedDoctypes[InspectionLotDoctype] = SettingInspectionByRule
	RegisterReport(ReportDefinition{ID: "inspection-lots", Label: "Inspection lots", Category: "Quality",
		Columns: []ReportColumn{{Key: "lot", Label: "Lot"}, {Key: "grn", Label: "Receipt"}, {Key: "vendor", Label: "Vendor"}, {Key: "sku", Label: "Item"},
			{Key: "location", Label: "Store"}, {Key: "qty", Label: "Qty"}, {Key: "sample", Label: "Sample"}, {Key: "reason", Label: "Asked by"},
			{Key: "passed", Label: "Passed"}, {Key: "failed", Label: "Failed"}, {Key: "status", Label: "Status"}},
		Run: inspectionLotsReport})
}

func inspectionErr(code, format string, args ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// inspectionByRuleOn is the setting, inside a Quality module that is on. With
// the module off the Inspect screen is gone, so nothing may be held for it.
func inspectionByRuleOn(tenantID string) bool {
	if !GetSettingBool(tenantID, SettingInspectionByRule) {
		return false
	}
	on, err := IsModuleEnabled(tenantID, "quality")
	return err != nil || on
}

// requireInspectionByRule refuses a route while the setting is off.
func requireInspectionByRule(tenantID string) error {
	if !inspectionByRuleOn(tenantID) {
		return inspectionErr(inspectionOffCode, "Inspection by rule is off.")
	}
	return nil
}

// inspectionRule is one active InspectionRule.
type inspectionRule struct {
	ID        string
	Kind      string
	Vendor    string
	Ref       string
	Threshold float64
	SamplePct float64
	Plan      string
}

func loadInspectionRules(schema string, kinds ...string) []inspectionRule {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text FROM %s.documents WHERE doctype = 'InspectionRule' AND deleted_at IS NULL
		AND COALESCE(data->>'status', 'Active') = 'Active' ORDER BY id`, schema))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []inspectionRule
	for rows.Next() {
		var id, raw string
		var d map[string]interface{}
		if rows.Scan(&id, &raw) != nil || json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		r := inspectionRule{ID: id, Kind: strField(d, "rule_kind"), Vendor: strField(d, "vendor"), Ref: strField(d, "ref"),
			Threshold: numFromInterface(d["threshold"]), SamplePct: numFromInterface(d["sample_pct"]), Plan: strField(d, "inspection_plan")}
		for _, k := range kinds {
			if r.Kind == k {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// itemQualityPolicy: does this item need a check on receipt, and may the
// warehouse decide to skip it. The item's own setting wins over its type's.
func itemQualityPolicy(tenantID, schema, sku string) (required, mandatory bool) {
	item, err := ResolveItemBySKU(tenantID, sku)
	if err != nil || item == nil {
		return false, true
	}
	req, mode := strField(item.Data, "quality_required"), strField(item.Data, "qc_mode")
	if req == "" || mode == "" {
		var raw string
		typeName := ItemTypeName(item.Data)
		if db.DB.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE doctype = 'ItemType' AND id = $1 AND deleted_at IS NULL`, schema), typeName).Scan(&raw) == nil {
			var td map[string]interface{}
			if json.Unmarshal([]byte(raw), &td) == nil {
				if req == "" {
					req = strField(td, "quality_required")
				}
				if mode == "" {
					mode = strField(td, "qc_mode")
				}
			}
		}
	}
	return req == "Yes", mode != qcModeDecide
}

// vendorLotHistory: completed lots of a vendor, newest first.
type lotHistory struct{ Qty, Failed float64 }

func vendorCompletedLots(schema, vendor string, limit int) []lotHistory {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE((data->>'qty')::numeric, 0), COALESCE((data->>'failed_qty')::numeric, 0)
		FROM %s.documents WHERE doctype = 'InspectionLot' AND deleted_at IS NULL AND data->>'vendor' = $1
		AND status IN ('Released', 'Part released', 'Rejected') ORDER BY updated_at DESC, id DESC LIMIT $2`, schema), vendor, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []lotHistory
	for rows.Next() {
		var h lotHistory
		if rows.Scan(&h.Qty, &h.Failed) == nil {
			out = append(out, h)
		}
	}
	return out
}

// inspectionDecision says whether a received lot is held and why.
type inspectionDecision struct {
	Hold   bool
	Reason string
	Plan   string
	Pct    float64
}

func decideInspection(tenantID, schema, vendor, sku, grnID string, rules []inspectionRule) inspectionDecision {
	d := inspectionDecision{Pct: float64(GetSettingInt(tenantID, SettingInspectionSamplePct))}
	if d.Pct <= 0 {
		d.Pct = 10
	}
	required, _ := itemQualityPolicy(tenantID, schema, sku)
	if required {
		d.Hold, d.Reason = true, "Item"
	}
	for _, r := range rules {
		if r.Vendor != "" && r.Vendor != vendor {
			continue
		}
		switch r.Kind {
		case "New vendor":
			var earlier int
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'GRN' AND deleted_at IS NULL
				AND data->>'vendor' = $1 AND id <> $2 AND status <> 'Cancelled'
				AND (status <> 'Draft' OR EXISTS (SELECT 1 FROM %s.documents l WHERE l.doctype = 'StockLedgerEntry' AND l.data->>'voucher_id' = documents.id))`, schema, schema), vendor, grnID).Scan(&earlier)
			if earlier == 0 && vendor != "" && !d.Hold {
				d.Hold, d.Reason = true, "New vendor"
			}
		case "Vendor reject rate":
			var qty, failed float64
			for _, h := range vendorCompletedLots(schema, vendor, 20) {
				qty += h.Qty
				failed += h.Failed
			}
			if qty > 0 && failed*100/qty > r.Threshold && !d.Hold {
				d.Hold, d.Reason = true, "Vendor reject rate"
			}
		}
		if d.Hold && d.Plan == "" {
			d.Plan = r.Plan
		}
	}
	if !d.Hold {
		return d
	}
	for _, r := range rules {
		if r.Kind != "Skip lot" || (r.Vendor != "" && r.Vendor != vendor) || r.Threshold < 1 || r.SamplePct <= 0 {
			continue
		}
		n := int(r.Threshold)
		hist := vendorCompletedLots(schema, vendor, n)
		clean := len(hist) == n
		for _, h := range hist {
			if h.Failed > 0 {
				clean = false
			}
		}
		if clean {
			d.Pct = r.SamplePct
		}
	}
	if d.Plan == "" {
		var pid string
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'InspectionPlan' AND deleted_at IS NULL
			AND COALESCE(data->>'status', 'Active') = 'Active' AND data->>'item' = $1 AND COALESCE(data->>'applies_to', 'Incoming') = 'Incoming'
			ORDER BY id LIMIT 1`, schema), sku).Scan(&pid)
		d.Plan = pid
	}
	return d
}

// grnLineKept is the accepted qty of one receipt line, as the post computes it.
func grnLineKept(m map[string]interface{}) float64 {
	qty := numFromInterface(m["qty"])
	accepted := qty - numFromInterface(m["rejected_qty"]) - numFromInterface(m["damaged_qty"])
	if _, ok := m["accepted_qty"]; ok {
		accepted = numFromInterface(m["accepted_qty"])
	}
	if accepted < 0 {
		accepted = 0
	}
	return accepted
}

// inspectionHoldTx runs inside the receipt's transaction, after the accepted
// stock has posted. It moves the lots a rule asks for from available to
// qc_hold (value stays in 1200, so there is no journal) and files their
// InspectionLots. It writes nothing when the setting is off or nothing matches.
func inspectionHoldTx(tx *sql.Tx, tenantID, schema, locationCode, userID, grnID, part string, items []interface{}) error {
	if !inspectionByRuleOn(tenantID) {
		return nil
	}
	type key struct{ sku, batch string }
	order := []key{}
	qtys := map[key]float64{}
	for _, raw := range items {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		sku := strField(m, "sku")
		if sku == "" {
			continue
		}
		if asset, _ := isFixedAssetSKU(tenantID, sku); asset {
			continue
		}
		if policy, _, known := ItemPolicyForSKU(tenantID, sku); known && !policy.Tracked() {
			continue
		}
		q := math.Trunc(grnLineKept(m)) // stock takes whole units per line, so the hold does too (148.56)
		if q <= 0 {
			continue
		}
		k := key{sku, strField(m, "batch_no")}
		if _, seen := qtys[k]; !seen {
			order = append(order, k)
		}
		qtys[k] += q
	}
	if len(order) == 0 {
		return nil
	}
	vendor := grnVendor(schema, grnID)
	rules := loadInspectionRules(schema, "New vendor", "Vendor reject rate", "Skip lot")
	for _, k := range order {
		var dup bool
		if err := tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'InspectionLot' AND deleted_at IS NULL
			AND data->>'grn_id' = $1 AND data->>'sku' = $2 AND COALESCE(data->>'batch_no', '') = $3 AND COALESCE(data->>'part', '') = $4)`, schema),
			grnID, k.sku, k.batch, part).Scan(&dup); err != nil {
			return err
		}
		if dup {
			continue
		}
		d := decideInspection(tenantID, schema, vendor, k.sku, grnID, rules)
		if !d.Hold {
			continue
		}
		var avail int
		if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(available, 0) FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema),
			k.sku, locationCode).Scan(&avail); err != nil {
			return err
		}
		hold := int(math.Round(qtys[k]))
		if hold > avail {
			hold = avail
		}
		if hold <= 0 {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET available = available - $3, qc_hold = COALESCE(qc_hold, 0) + $3,
			updated_at = CURRENT_TIMESTAMP WHERE sku = $1 AND location_code = $2`, schema), k.sku, locationCode, hold); err != nil {
			return err
		}
		id := NewDocID("IQL")
		if err := insertStockLedgerEntry(tx, schema, StockLedgerEntry{ItemID: k.sku, WarehouseID: locationCode, Qty: 0,
			VoucherType: InspectionLotDoctype, VoucherID: id, UserID: userID, FromStatus: "Available", ToStatus: "QC-Hold", BatchNo: k.batch,
			IdempotencyKey: "IQL:" + id + ":hold"}); err != nil {
			return err
		}
		sample := int(math.Ceil(float64(hold) * d.Pct / 100))
		if sample < 1 {
			sample = 1
		}
		if sample > hold {
			sample = hold
		}
		_, mandatory := itemQualityPolicy(tenantID, schema, k.sku)
		mode := qcModeMandatory
		if !mandatory {
			mode = qcModeDecide
		}
		data := map[string]interface{}{"grn_id": grnID, "part": part, "vendor": vendor, "sku": k.sku, "batch_no": k.batch, "location": locationCode,
			"qty": hold, "sample_size": sample, "sample_pct": d.Pct, "reason": d.Reason, "inspection_plan": d.Plan, "qc_mode": mode,
			"passed_qty": 0, "failed_qty": 0, "status": lotPending, "created_on": BusinessToday()}
		b, _ := json.Marshal(data)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'InspectionLot', $2, $3, $4)`, schema),
			id, b, lotPending, userID); err != nil {
			return err
		}
	}
	return nil
}

// InspectionResultLine is one result line: a result and how many units it covers.
type InspectionResultLine struct {
	Result   string             `json:"result"`
	Qty      float64            `json:"qty"`
	Readings map[string]float64 `json:"readings,omitempty"`
	Note     string             `json:"note,omitempty"`
}

// InspectionResultRequest is what a person enters for one lot.
type InspectionResultRequest struct {
	LotID  string                 `json:"lot_id"`
	Lines  []InspectionResultLine `json:"lines"`
	Karat  string                 `json:"karat,omitempty"`
	Weight string                 `json:"weight,omitempty"`
	Waive  bool                   `json:"waive,omitempty"`
	Note   string                 `json:"note,omitempty"`
}

// InspectionLotResult is what recording the results did.
type InspectionLotResult struct {
	ID      string  `json:"id"`
	Status  string  `json:"status"`
	Passed  float64 `json:"passed_qty"`
	Failed  float64 `json:"failed_qty"`
	PassPct float64 `json:"pass_pct"`
	Release float64 `json:"released_qty"`
}

type planLimit struct{ Min, Max *float64 }

func inspectionPlanLimits(tenantID, planID string) (limits map[string]planLimit, acceptPct float64) {
	acceptPct = 100
	if planID == "" {
		return nil, acceptPct
	}
	plan, _, err := fetchDocData(tenantID, "InspectionPlan", planID)
	if err != nil || plan == nil {
		return nil, acceptPct
	}
	if p := numFromInterface(plan["acceptance_pct"]); p > 0 && p <= 100 {
		acceptPct = p
	}
	limits = map[string]planLimit{}
	for _, r := range jsonRows(plan["test_parameters"]) {
		name := strings.TrimSpace(strField(r, "parameter_name"))
		if name == "" {
			continue
		}
		var l planLimit
		if s := strings.TrimSpace(fmt.Sprint(r["min"])); s != "" && s != "<nil>" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				l.Min = &v
			}
		}
		if s := strings.TrimSpace(fmt.Sprint(r["max"])); s != "" && s != "<nil>" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				l.Max = &v
			}
		}
		limits[name] = l
	}
	return limits, acceptPct
}

// RecordInspectionResult enters the results of one pending lot (or waives it)
// and moves the stock: passed units go to available, failed units stay in
// qc_hold - so a failed lot cannot be sold - and a lot below the plan's
// acceptance % is held whole. The lot's store is checked by the caller.
func RecordInspectionResult(tenantID, userID string, in InspectionResultRequest) (*InspectionLotResult, error) {
	if err := requireInspectionByRule(tenantID); err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	lot, status, err := lockDocTx(tx, schema, InspectionLotDoctype, strings.TrimSpace(in.LotID))
	if err != nil {
		return nil, inspectionErr(inspectionLotCode, "That inspection lot was not found.")
	}
	if status != lotPending {
		return nil, inspectionErr(inspectionLotCode, "That lot is already %s.", strings.ToLower(status))
	}
	qty := numFromInterface(lot["qty"])
	sku, loc, batch := strField(lot, "sku"), strField(lot, "location"), strField(lot, "batch_no")
	res := &InspectionLotResult{ID: strings.TrimSpace(in.LotID)}
	var passed, failed float64
	var kept []map[string]interface{}
	newStatus := lotWaived
	if in.Waive {
		if strField(lot, "qc_mode") != qcModeDecide {
			return nil, inspectionErr(inspectionWaiveCode, "This item's check is mandatory.")
		}
		if strings.TrimSpace(in.Note) == "" {
			return nil, inspectionErr(inspectionWaiveCode, "Give a reason to waive the check.")
		}
		passed = qty
	} else {
		limits, acceptPct := inspectionPlanLimits(tenantID, strField(lot, "inspection_plan"))
		var total float64
		for i, l := range in.Lines {
			if l.Qty < 0 || l.Qty != math.Trunc(l.Qty) {
				return nil, inspectionErr(inspectionInputCode, "Line %d: enter a whole quantity.", i+1)
			}
			result := "Fail" // a blank result is a Fail
			if strings.EqualFold(strings.TrimSpace(l.Result), "Pass") {
				result = "Pass"
			}
			var notes []string
			for name, v := range l.Readings {
				if lim, ok := limits[name]; ok && ((lim.Min != nil && v < *lim.Min) || (lim.Max != nil && v > *lim.Max)) {
					result = "Fail"
					notes = append(notes, name+" out of limits")
				}
			}
			total += l.Qty
			if result == "Pass" {
				passed += l.Qty
			} else {
				failed += l.Qty
			}
			kept = append(kept, map[string]interface{}{"result": result, "qty": l.Qty, "readings": l.Readings, "note": strings.TrimSpace(strings.Join(append(notes, l.Note), "; "))})
		}
		if total != qty {
			return nil, inspectionErr(inspectionInputCode, "The result lines total %s; the lot is %s.", trimNum(total), trimNum(qty))
		}
		res.PassPct = round2(passed * 100 / qty)
		switch {
		case passed*100 < acceptPct*qty:
			newStatus = lotRejected
		case failed > 0:
			newStatus = lotPart
		default:
			newStatus = lotReleased
		}
	}
	release := passed
	if newStatus == lotRejected {
		release = 0
	}
	if release > 0 {
		r := int(release)
		moved, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET available = available + $3, qc_hold = COALESCE(qc_hold, 0) - $3,
			updated_at = CURRENT_TIMESTAMP WHERE sku = $1 AND location_code = $2 AND COALESCE(qc_hold, 0) >= $3`, schema), sku, loc, r)
		if err != nil {
			return nil, err
		}
		if n, _ := moved.RowsAffected(); n == 0 {
			return nil, inspectionErr(inspectionLotCode, "QC hold for %s no longer has %d units.", sku, r)
		}
		if err := insertStockLedgerEntry(tx, schema, StockLedgerEntry{ItemID: sku, WarehouseID: loc, Qty: 0, VoucherType: InspectionLotDoctype,
			VoucherID: res.ID, UserID: userID, FromStatus: "QC-Hold", ToStatus: "Available", BatchNo: batch,
			IdempotencyKey: "IQL:" + res.ID + ":release"}); err != nil {
			return nil, err
		}
	}
	lot["results"] = kept
	lot["passed_qty"], lot["failed_qty"] = passed, failed
	lot["released_qty"] = release
	lot["held_qty"] = qty - release // units still in QC hold; the bill waits until this is 0
	lot["decided_by"], lot["decided_on"] = userID, BusinessToday()
	if n := strings.TrimSpace(in.Note); n != "" {
		lot["decision_note"] = n
	}
	if err := writeDocTx(tx, schema, InspectionLotDoctype, res.ID, lot, newStatus); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if k, w := strings.TrimSpace(in.Karat), strings.TrimSpace(in.Weight); (k != "" || w != "") && batch != "" {
		writeLotBatchAttributes(tenantID, schema, sku, batch, k, w)
	}
	LogAuditEvent(tenantID, userID, "INSPECTION_LOT_DECIDED", "SUCCESS", fmt.Sprintf("Inspection lot %s: %s", res.ID, newStatus))
	res.Status, res.Passed, res.Failed, res.Release = newStatus, passed, failed, release
	return res, nil
}

// writeLotBatchAttributes puts the karat / weight found in the check on the lot's Batch.
func writeLotBatchAttributes(tenantID, schema, sku, batch, karat, weight string) {
	b, err := GetBatch(tenantID, sku, batch)
	if err != nil || b == nil {
		return
	}
	attrs := b.Attributes
	if attrs == nil {
		attrs = map[string]string{}
	}
	if karat != "" {
		attrs["karat"] = karat
	}
	if weight != "" {
		attrs["weight"] = weight
	}
	enc, _ := json.Marshal(attrs)
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('attributes', $1::text), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'Batch' AND id = $2`, schema), string(enc), b.DocID)
}

// InspectionLotView is one lot as a list shows it.
type InspectionLotView struct {
	ID         string   `json:"id"`
	GRN        string   `json:"grn_id"`
	Vendor     string   `json:"vendor"`
	SKU        string   `json:"sku"`
	Batch      string   `json:"batch_no"`
	Location   string   `json:"location"`
	Qty        float64  `json:"qty"`
	Sample     float64  `json:"sample_size"`
	Reason     string   `json:"reason"`
	Plan       string   `json:"inspection_plan"`
	CanWaive   bool     `json:"can_waive"`
	Status     string   `json:"status"`
	Parameters []string `json:"parameters,omitempty"`
	Held       float64  `json:"held_qty,omitempty"`
}

// ListInspectionLots lists the lots of a status for a store ("" = every store).
func ListInspectionLots(tenantID, store, status string) ([]InspectionLotView, error) {
	if err := requireInspectionByRule(tenantID); err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if status == "" {
		status = lotPending
	}
	filter := "AND status = $1"
	if status == lotHeld { // failed units still in QC hold, waiting to be scrapped or released
		filter = "AND status IN ('Rejected', 'Part released') AND COALESCE((data->>'held_qty')::numeric, 0) > 0 AND $1::text <> ''"
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text, status FROM %s.documents WHERE doctype = 'InspectionLot' AND deleted_at IS NULL
		%s AND ($2 = '' OR data->>'location' = $2) ORDER BY created_at, id LIMIT 200`, schema, filter), status, store)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InspectionLotView{}
	for rows.Next() {
		var id, raw, st string
		var d map[string]interface{}
		if rows.Scan(&id, &raw, &st) != nil || json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		v := InspectionLotView{ID: id, GRN: strField(d, "grn_id"), Vendor: strField(d, "vendor"), SKU: strField(d, "sku"), Batch: strField(d, "batch_no"),
			Location: strField(d, "location"), Qty: numFromInterface(d["qty"]), Sample: numFromInterface(d["sample_size"]), Reason: strField(d, "reason"),
			Plan: strField(d, "inspection_plan"), CanWaive: strField(d, "qc_mode") == qcModeDecide, Status: st, Held: numFromInterface(d["held_qty"])}
		if v.Plan != "" {
			if plan, _, e := fetchDocData(tenantID, "InspectionPlan", v.Plan); e == nil && plan != nil {
				for _, r := range jsonRows(plan["test_parameters"]) {
					if n := strings.TrimSpace(strField(r, "parameter_name")); n != "" {
						v.Parameters = append(v.Parameters, n)
					}
				}
			}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// InspectionLotLocation is the store a lot sits in, for a scope check.
func InspectionLotLocation(tenantID, lotID string) string {
	d, _, err := fetchDocData(tenantID, InspectionLotDoctype, lotID)
	if err != nil || d == nil {
		return ""
	}
	return strField(d, "location")
}

// inspectionBlocksBill makes a vendor bill wait while a lot of its receipt is
// still being inspected. Off, or with no pending lot, it changes nothing.
func inspectionBlocksBill(tenantID string, bill map[string]interface{}) error {
	grnID := strField(bill, "grn_id")
	if grnID == "" || !inspectionByRuleOn(tenantID) {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	var waiting bool
	if db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'InspectionLot' AND deleted_at IS NULL
		AND data->>'grn_id' = $1 AND (status = 'Pending' OR COALESCE((data->>'held_qty')::numeric, 0) > 0))`, schema), grnID).Scan(&waiting) == nil && waiting {
		return &ValidationError{Code: inspectionBillCode, Message: "This bill is for goods still being inspected or still held after a failed check."}
	}
	return nil
}

func inspectionLotsReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	return dayLimitRows(tenantID, `SELECT id, COALESCE(data->>'grn_id', ''), COALESCE(data->>'vendor', ''), COALESCE(data->>'sku', ''),
		COALESCE(data->>'location', ''), COALESCE((data->>'qty')::numeric, 0), COALESCE((data->>'sample_size')::numeric, 0),
		COALESCE(data->>'reason', ''), COALESCE((data->>'passed_qty')::numeric, 0), COALESCE((data->>'failed_qty')::numeric, 0), status
		FROM {s}.documents WHERE doctype = 'InspectionLot' AND deleted_at IS NULL ORDER BY created_at DESC, id LIMIT 20001`, nil,
		func(scan func(...interface{}) error) (map[string]interface{}, error) {
			var id, grn, vendor, sku, loc, reason, status string
			var qty, sample, passed, failed float64
			if err := scan(&id, &grn, &vendor, &sku, &loc, &qty, &sample, &reason, &passed, &failed, &status); err != nil {
				return nil, err
			}
			return map[string]interface{}{"lot": id, "grn": grn, "vendor": vendor, "sku": sku, "location": loc, "qty": qty, "sample": sample,
				"reason": reason, "passed": passed, "failed": failed, "status": status}, nil
		})
}
