package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Stage 55.13 - quality and maintenance a person can run end to end.
//
// A QualityInspection names its source (a production order, a GRN received,
// or a sales order going out), the item/lot/location/quantity inspected and
// the readings taken; with an inspection plan, every parameter of the plan
// must have a reading. The overall result is derived: one failed reading
// fails the inspection.
//
// What happens next depends on the source. Production inspections keep the
// 26.9.7 route - Submit for approval, and the order's QC gate reads the
// approved result (a Fail blocks completion, so failed output never reaches
// stock). Incoming and outgoing inspections are decided here, by a
// disposition: a pass is accepted; a fail is never accepted - its stock is
// put on hold (a Hold under a Quality hold code, released only through the
// existing approval), sent to rework (also held), or used as-is by
// concession, which needs a note and a second person. Every failed
// inspection raises one NonConformanceReport linked back to it, and that
// report can only be closed on recorded evidence by an accountable owner.
//
// Maintenance orders carry a task checklist (copied from their schedule),
// the parts and hours used, and who started and completed them; an order
// cannot be completed with an unticked task. Parts are recorded, not issued
// from stock.

const (
	InspectProduction = "Production"
	InspectIncoming   = "Incoming"
	InspectOutgoing   = "Outgoing"

	DispositionAccept     = "Accept"
	DispositionHold       = "Hold stock"
	DispositionRework     = "Rework"
	DispositionConcession = "Use as-is (concession)"
)

var inspectionSourceDoctype = map[string]string{
	InspectProduction: "ProductionOrder",
	InspectIncoming:   "GRN",
	InspectOutgoing:   "SalesOrder",
}

// InspectionReading is one measured parameter.
type InspectionReading struct {
	Parameter string `json:"parameter_name"`
	Spec      string `json:"spec,omitempty"`
	Reading   string `json:"reading"`
	Result    string `json:"pass_fail"`
}

// InspectionInput is what an inspector records.
type InspectionInput struct {
	SourceType     string              `json:"source_type"`
	SourceID       string              `json:"source_id"`
	Item           string              `json:"item"`
	BatchNo        string              `json:"batch_no"`
	Location       string              `json:"location"`
	Qty            float64             `json:"qty_inspected"`
	InspectionPlan string              `json:"inspection_plan"`
	Readings       []InspectionReading `json:"readings"`
	Remarks        string              `json:"remarks"`
}

func qualityErr(code, format string, args ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// jsonRows reads a JSON array stored either as a string or as an array.
func jsonRows(v interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	switch t := v.(type) {
	case string:
		_ = json.Unmarshal([]byte(t), &out)
	case []interface{}:
		for _, r := range t {
			if m, ok := r.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

// InspectionPlanParameters returns a plan's parameters (name and spec) and
// the item it is for, refusing an unknown or inactive plan.
func InspectionPlanParameters(tenantID, planID string) (item string, params []InspectionReading, err error) {
	plan, status, err := fetchDocData(tenantID, "InspectionPlan", planID)
	if err != nil || plan == nil {
		return "", nil, qualityErr("QUALIT-0562", "inspection plan %s not found", planID)
	}
	if status == "Inactive" || strField(plan, "status") == "Inactive" {
		return "", nil, qualityErr("QUALIT-0562", "inspection plan %s is inactive", planID)
	}
	for _, r := range jsonRows(plan["test_parameters"]) {
		if name := strings.TrimSpace(strField(r, "parameter_name")); name != "" {
			params = append(params, InspectionReading{Parameter: name, Spec: strField(r, "spec")})
		}
	}
	return strField(plan, "item"), params, nil
}

// RecordQualityInspection validates and saves a Draft inspection with its
// derived result.
func RecordQualityInspection(tenantID string, in InspectionInput, userID string) (id, result string, err error) {
	in.SourceType = strings.TrimSpace(in.SourceType)
	in.SourceID = strings.TrimSpace(in.SourceID)
	doctype, ok := inspectionSourceDoctype[in.SourceType]
	if !ok {
		return "", "", qualityErr("QUALIT-0561", "choose what is being inspected: Production, Incoming or Outgoing")
	}
	if in.SourceID == "" {
		return "", "", qualityErr("QUALIT-0561", "name the %s this inspection is for", doctype)
	}
	src, _, err := fetchDocData(tenantID, doctype, in.SourceID)
	if err != nil || src == nil {
		return "", "", qualityErr("QUALIT-0561", "%s %s not found", doctype, in.SourceID)
	}
	if in.Location == "" {
		in.Location = strField(src, "location")
	}
	if in.SourceType == InspectProduction && in.Item == "" {
		if bom, errB := fetchBOMData(tenantID, strField(src, "bom_id")); errB == nil {
			in.Item = strField(bom, "parent_item")
		}
	}
	if strings.TrimSpace(in.Item) == "" {
		return "", "", qualityErr("QUALIT-0561", "name the item inspected")
	}
	item, errI := ResolveItemBySKU(tenantID, strings.TrimSpace(in.Item))
	if errI != nil {
		return "", "", qualityErr("QUALIT-0561", "item %s not found", in.Item)
	}
	if code := strField(item.Data, "code"); code != "" {
		in.Item = code
	}
	if in.SourceType == InspectIncoming {
		onGRN := false
		for _, r := range jsonRows(src["received_items"]) {
			if strField(r, "sku") == in.Item {
				onGRN = true
			}
		}
		if !onGRN {
			return "", "", qualityErr("QUALIT-0561", "%s was not received on %s", in.Item, in.SourceID)
		}
	}
	if in.SourceType != InspectProduction {
		if in.Location == "" {
			return "", "", qualityErr("QUALIT-0561", "name the location the inspected stock is at")
		}
		if in.Qty <= 0 {
			return "", "", qualityErr("QUALIT-0561", "enter the quantity inspected")
		}
	}

	byName := map[string]InspectionReading{}
	for _, r := range in.Readings {
		r.Parameter = strings.TrimSpace(r.Parameter)
		if r.Parameter == "" && strings.TrimSpace(r.Reading) == "" {
			continue
		}
		if r.Parameter == "" {
			return "", "", qualityErr("QUALIT-0562", "every reading needs the parameter it measures")
		}
		if r.Result != "Pass" && r.Result != "Fail" {
			return "", "", qualityErr("QUALIT-0562", "mark %s as Pass or Fail", r.Parameter)
		}
		byName[strings.ToLower(r.Parameter)] = r
	}
	var readings []InspectionReading
	if in.InspectionPlan != "" {
		planItem, params, errP := InspectionPlanParameters(tenantID, in.InspectionPlan)
		if errP != nil {
			return "", "", errP
		}
		if planItem != "" && planItem != in.Item {
			return "", "", qualityErr("QUALIT-0562", "inspection plan %s is for %s, not %s", in.InspectionPlan, planItem, in.Item)
		}
		for _, p := range params {
			r, ok := byName[strings.ToLower(p.Parameter)]
			if !ok {
				return "", "", qualityErr("QUALIT-0562", "the plan asks for %s - record a reading for it", p.Parameter)
			}
			r.Parameter, r.Spec = p.Parameter, p.Spec
			readings = append(readings, r)
			delete(byName, strings.ToLower(p.Parameter))
		}
	}
	for _, r := range in.Readings { // readings beyond the plan, in the order given
		if x, ok := byName[strings.ToLower(strings.TrimSpace(r.Parameter))]; ok {
			readings = append(readings, x)
			delete(byName, strings.ToLower(strings.TrimSpace(r.Parameter)))
		}
	}
	if len(readings) == 0 {
		return "", "", qualityErr("QUALIT-0562", "record at least one reading")
	}
	result = "Pass"
	for _, r := range readings {
		if r.Result == "Fail" {
			result = "Fail"
		}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	id = NewDocID("QI")
	rb, _ := json.Marshal(readings)
	data := map[string]interface{}{
		"id": id, "code": id, "source_type": in.SourceType, "source_id": in.SourceID,
		"item": in.Item, "batch_no": strings.TrimSpace(in.BatchNo), "location": in.Location,
		"qty_inspected": in.Qty, "inspection_plan": in.InspectionPlan, "readings": string(rb),
		"result": result, "remarks": strings.TrimSpace(in.Remarks), "inspected_by": userID, "status": "Draft",
	}
	if in.SourceType == InspectProduction {
		data["production_order_id"] = in.SourceID
	}
	b, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'QualityInspection', $2, 'Draft', $3)`, schema),
		id, b, userID); err != nil {
		return "", "", err
	}
	LogAuditEvent(tenantID, userID, "QUALITY_INSPECTION_RECORDED", "SUCCESS", fmt.Sprintf("%s: %s %s %s from %s %s", id, result, in.Item, in.BatchNo, doctype, in.SourceID))
	return id, result, nil
}

// InspectionDecision is what a disposition did.
type InspectionDecision struct {
	ID          string `json:"id"`
	Result      string `json:"result"`
	Disposition string `json:"disposition"`
	HoldID      string `json:"hold_id,omitempty"`
	NCRID       string `json:"ncr_id,omitempty"`
}

// DisposeQualityInspection records the authorised decision on an incoming
// or outgoing inspection, in one transaction with the hold and the
// non-conformance report it raises.
func DisposeQualityInspection(tenantID, id, disposition, note, holdCode, userID string) (InspectionDecision, error) {
	out := InspectionDecision{ID: id}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	data, status, err := lockDocTx(tx, schema, "QualityInspection", id)
	if err != nil {
		return out, qualityErr("QUALIT-0563", "inspection %s not found", id)
	}
	src := strField(data, "source_type")
	if src == "" || src == InspectProduction {
		return out, qualityErr("QUALIT-0563", "a production inspection is decided by Submit for approval - the order's QC gate reads that decision")
	}
	if status != "Draft" {
		return out, qualityErr("QUALIT-0563", "inspection %s is already decided (%s)", id, strField(data, "disposition"))
	}
	out.Result = strField(data, "result")
	note = strings.TrimSpace(note)
	if disposition == "" && out.Result == "Pass" {
		disposition = DispositionAccept
	}
	switch {
	case out.Result == "Pass" && disposition != DispositionAccept:
		return out, qualityErr("QUALIT-0563", "a passed inspection is accepted - record a new inspection if something is wrong")
	case out.Result == "Fail" && disposition == DispositionAccept:
		return out, qualityErr("QUALIT-0563", "a failed inspection cannot be accepted - choose Hold stock, Rework or Use as-is (concession)")
	case out.Result == "Fail" && disposition != DispositionHold && disposition != DispositionRework && disposition != DispositionConcession:
		return out, qualityErr("QUALIT-0563", "choose Hold stock, Rework or Use as-is (concession)")
	}
	if disposition == DispositionConcession {
		if note == "" {
			return out, qualityErr("QUALIT-0564", "a concession needs a note saying why the stock can be used")
		}
		if strField(data, "inspected_by") == userID {
			return out, qualityErr("QUALIT-0564", "the inspector cannot grant a concession on their own inspection")
		}
	}
	item, batch, loc := strField(data, "item"), strField(data, "batch_no"), strField(data, "location")
	if disposition == DispositionHold || disposition == DispositionRework {
		if holdCode == "" {
			_ = tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'code', id) FROM %s.documents
				WHERE doctype = 'HoldCode' AND status = 'Active' AND deleted_at IS NULL AND data->>'category' = 'Quality'
				ORDER BY id LIMIT 1`, schema)).Scan(&holdCode)
		}
		if holdCode == "" {
			return out, qualityErr("QUALIT-0563", "add a hold code of category Quality (Setup -> Hold Code) before holding stock")
		}
		qty := int(math.Ceil(numFromInterface(data["qty_inspected"]) - 1e-9))
		holdID, errH := placeHoldTx(tx, schema, holdCode, item, loc, batch, qty, fmt.Sprintf("Failed inspection %s (%s)", id, disposition), userID)
		if errH != nil {
			return out, qualityErr("QUALIT-0563", "the stock could not be held: %v", errH)
		}
		data["hold_id"] = holdID
		out.HoldID = holdID
	}
	if out.Result == "Fail" {
		ncrID := NewDocID("NCR")
		desc := fmt.Sprintf("Failed %s inspection %s: %s", strings.ToLower(src), id, item)
		if batch != "" {
			desc += " lot " + batch
		}
		nb, _ := json.Marshal(map[string]interface{}{
			"id": ncrID, "code": ncrID, "description": desc, "source_doctype": "QualityInspection", "source_id": id,
			"item": item, "batch_no": batch, "status": "Draft",
		})
		if _, err := tx.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'NonConformanceReport', $2, 'Draft', $3)`, schema),
			ncrID, nb, userID); err != nil {
			return out, err
		}
		data["ncr_id"] = ncrID
		out.NCRID = ncrID
	}
	data["disposition"] = disposition
	data["disposition_note"] = note
	data["disposed_by"] = userID
	data["disposed_at"] = time.Now().UTC().Format(time.RFC3339)
	if err := writeDocTx(tx, schema, "QualityInspection", id, data, "Approved"); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	out.Disposition = disposition
	LogAuditEvent(tenantID, userID, "QUALITY_INSPECTION_DISPOSED", "SUCCESS", fmt.Sprintf("%s %s: %s hold=%s ncr=%s", id, out.Result, disposition, out.HoldID, out.NCRID))
	return out, nil
}

// RaiseNonConformance is CreateNonConformanceReport with the item and lot.
func RaiseNonConformance(tenantID, description, sourceDoctype, sourceID, item, batchNo, userID string) (string, error) {
	id, err := CreateNonConformanceReport(tenantID, strings.TrimSpace(description), sourceDoctype, sourceID, userID)
	if err != nil || (item == "" && batchNo == "") {
		return id, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return id, err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb WHERE doctype = 'NonConformanceReport' AND id = $2`, schema),
		fmt.Sprintf(`{"item":%q,"batch_no":%q}`, item, batchNo), id)
	return id, err
}

// PlanCorrectiveActionFor records the corrective action with the person
// accountable for it and when it is due.
func PlanCorrectiveActionFor(tenantID, ncrID, action, owner, dueDate string) error {
	action, owner = strings.TrimSpace(action), strings.TrimSpace(owner)
	if action == "" || owner == "" {
		return qualityErr("QUALIT-0565", "write the corrective action and name who owns it")
	}
	if dueDate != "" {
		if _, err := time.Parse("2006-01-02", dueDate); err != nil {
			return qualityErr("QUALIT-0565", "the due date must be a date")
		}
	}
	return updateNCRTx(tenantID, ncrID, "Investigating", "CorrectiveActionPlanned", map[string]interface{}{
		"corrective_action": action, "owner": owner, "due_date": dueDate})
}

// CloseNonConformanceWithEvidence closes an NCR on the evidence that the
// corrective action was done.
func CloseNonConformanceWithEvidence(tenantID, ncrID, evidence, userID string) error {
	if strings.TrimSpace(evidence) == "" {
		return qualityErr("QUALIT-0565", "record the evidence the corrective action was done before closing")
	}
	return updateNCRTx(tenantID, ncrID, "CorrectiveActionPlanned", "Closed", map[string]interface{}{
		"closure_evidence": strings.TrimSpace(evidence), "closed_by": userID, "closed_at": time.Now().UTC().Format(time.RFC3339)})
}

func updateNCRTx(tenantID, ncrID, from, to string, patch map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockDocTx(tx, schema, "NonConformanceReport", ncrID)
	if err != nil {
		return qualityErr("QUALIT-0565", "non-conformance report %s not found", ncrID)
	}
	if status != from {
		return qualityErr("QUALIT-0565", "%s is %s - this step needs it to be %s", ncrID, status, from)
	}
	for k, v := range patch {
		data[k] = v
	}
	if err := writeDocTx(tx, schema, "NonConformanceReport", ncrID, data, to); err != nil {
		return err
	}
	return tx.Commit()
}

// MaintenanceTask is one checklist line.
type MaintenanceTask struct {
	Task string `json:"task"`
	Done bool   `json:"done"`
}

// MaintenancePart is a part used.
type MaintenancePart struct {
	Sku string  `json:"sku"`
	Qty float64 `json:"qty"`
}

// MaintenanceCompletion is what the technician records at completion.
type MaintenanceCompletion struct {
	Notes     string            `json:"completion_notes"`
	Checklist []MaintenanceTask `json:"checklist"`
	Parts     []MaintenancePart `json:"parts_used"`
	Hours     float64           `json:"labour_hours"`
}

// checklistFromText turns "one task per line" into checklist JSON.
func checklistFromText(v interface{}) string {
	text, _ := v.(string)
	var tasks []MaintenanceTask
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			tasks = append(tasks, MaintenanceTask{Task: t})
		}
	}
	if len(tasks) == 0 {
		return ""
	}
	b, _ := json.Marshal(tasks)
	return string(b)
}

func maintenanceStep(tenantID, orderID, from, to string, apply func(data map[string]interface{}) error) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockDocTx(tx, schema, "MaintenanceOrder", orderID)
	if err != nil {
		return qualityErr("QUALIT-0566", "maintenance order %s not found", orderID)
	}
	if status != from {
		return qualityErr("QUALIT-0566", "%s is %s - this step needs it to be %s", orderID, status, from)
	}
	if err := apply(data); err != nil {
		return err
	}
	if err := writeDocTx(tx, schema, "MaintenanceOrder", orderID, data, to); err != nil {
		return err
	}
	return tx.Commit()
}

// ScheduleMaintenanceOrderOn moves a Draft order to Scheduled for a date.
func ScheduleMaintenanceOrderOn(tenantID, orderID, date string) error {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return qualityErr("QUALIT-0566", "choose the date the work is scheduled for")
	}
	return maintenanceStep(tenantID, orderID, "Draft", "Scheduled", func(d map[string]interface{}) error {
		d["scheduled_date"] = date
		return nil
	})
}

// StartMaintenanceOrderBy is StartMaintenanceOrder recording who started.
func StartMaintenanceOrderBy(tenantID, orderID, userID string) error {
	return maintenanceStep(tenantID, orderID, "Scheduled", "InProgress", func(d map[string]interface{}) error {
		d["started_by"] = userID
		d["started_at"] = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
}

// CompleteMaintenanceOrderWith completes an order on its evidence: notes,
// every checklist task ticked, the parts and hours used.
func CompleteMaintenanceOrderWith(tenantID, orderID string, c MaintenanceCompletion, userID string) error {
	if strings.TrimSpace(c.Notes) == "" {
		return qualityErr("QUALIT-0566", "write what was done before completing")
	}
	if c.Hours < 0 {
		return qualityErr("QUALIT-0566", "hours cannot be negative")
	}
	var parts []MaintenancePart
	for _, p := range c.Parts {
		p.Sku = strings.TrimSpace(p.Sku)
		if p.Sku == "" && p.Qty == 0 {
			continue
		}
		item, err := ResolveItemBySKU(tenantID, p.Sku)
		if err != nil || p.Qty <= 0 {
			return qualityErr("QUALIT-0566", "each part needs a known item and a quantity above zero (%s)", p.Sku)
		}
		if code := strField(item.Data, "code"); code != "" {
			p.Sku = code
		}
		parts = append(parts, p)
	}
	return maintenanceStep(tenantID, orderID, "InProgress", "Completed", func(d map[string]interface{}) error {
		var tasks []MaintenanceTask
		if s, _ := d["checklist"].(string); s != "" {
			_ = json.Unmarshal([]byte(s), &tasks)
		}
		done := map[string]bool{}
		for _, t := range c.Checklist {
			if t.Done {
				done[strings.TrimSpace(t.Task)] = true
			}
		}
		for i := range tasks {
			if !done[tasks[i].Task] {
				return qualityErr("QUALIT-0566", "task not done: %s", tasks[i].Task)
			}
			tasks[i].Done = true
		}
		if len(tasks) > 0 {
			b, _ := json.Marshal(tasks)
			d["checklist"] = string(b)
		}
		pb, _ := json.Marshal(parts)
		d["parts_used"] = string(pb)
		d["labour_hours"] = c.Hours
		d["completion_notes"] = strings.TrimSpace(c.Notes)
		d["completed_by"] = userID
		d["completed_at"] = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
}

// CreateMaintenanceOrder raises a one-off order for an asset, numbered the
// way the schedule worker numbers its orders; with a date it is Scheduled
// straight away, otherwise it waits in Draft.
func CreateMaintenanceOrder(tenantID, asset, description, scheduledDate, checklistText, userID string) (string, error) {
	asset, description = strings.TrimSpace(asset), strings.TrimSpace(description)
	if asset == "" || description == "" {
		return "", qualityErr("QUALIT-0566", "name the asset and the work to be done")
	}
	if a, _, err := fetchDocData(tenantID, "Asset", asset); err != nil || a == nil {
		return "", qualityErr("QUALIT-0566", "asset %s not found", asset)
	}
	status := "Draft"
	if scheduledDate != "" {
		if _, err := time.Parse("2006-01-02", scheduledDate); err != nil {
			return "", qualityErr("QUALIT-0566", "choose the date the work is scheduled for")
		}
		status = "Scheduled"
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	id := NewDocID("MO")
	data := map[string]interface{}{"id": id, "code": id, "asset": asset, "description": description, "scheduled_date": scheduledDate, "status": status}
	if c := checklistFromText(checklistText); c != "" {
		data["checklist"] = c
	}
	b, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'MaintenanceOrder', $2, $3, $4)`, schema),
		id, b, status, userID); err != nil {
		return "", err
	}
	return id, nil
}
