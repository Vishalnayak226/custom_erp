package engines

import (
	"crypto/sha1"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stage 42.6 adds a configured labour-and-billing layer on top of the
// WarehouseTask spine. The deliberately small public surface here is the
// important boundary: masters remain editable through the generic document
// engine, while the calculation, captured evidence and invoice lifecycle stay
// behind these functions. That prevents a browser save from rewriting a
// charge after it has entered a customer's invoice.

type LaborStandardTime struct {
	StandardCode       string  `json:"standard_code"`
	OperationCode      string  `json:"operation_code"`
	Quantity           float64 `json:"quantity"`
	ElementSeconds     float64 `json:"element_seconds"`
	TravelSeconds      float64 `json:"travel_seconds"`
	AllowanceSeconds   float64 `json:"allowance_seconds"`
	TotalSeconds       float64 `json:"total_seconds"`
	LaborRatePerHour   float64 `json:"labor_rate_per_hour"`
	EstimatedLaborCost float64 `json:"estimated_labor_cost"`
}

type LaborPlanRow struct {
	Department              string  `json:"department"`
	OpenTasks               int     `json:"open_tasks"`
	UncoveredTasks          int     `json:"uncovered_tasks"`
	PlannedSeconds          float64 `json:"planned_seconds"`
	PlannedHours            float64 `json:"planned_hours"`
	AvailableHoursPerWorker float64 `json:"available_hours_per_worker"`
	ForecastHeadcount       int     `json:"forecast_headcount"`
}

type CapturedChargeInfo struct {
	ID           string  `json:"id"`
	EventKey     string  `json:"event_key"`
	TriggerEvent string  `json:"trigger_event"`
	ChargeCode   string  `json:"charge_code"`
	OwnerID      string  `json:"owner_id"`
	LocationCode string  `json:"location_code"`
	Quantity     float64 `json:"quantity"`
	NetAmount    float64 `json:"net_amount"`
	TaxRate      float64 `json:"tax_rate"`
	TaxAmount    float64 `json:"tax_amount"`
	TotalAmount  float64 `json:"total_amount"`
	OccurredOn   string  `json:"occurred_on"`
	InvoiceID    string  `json:"invoice_id,omitempty"`
	Status       string  `json:"status"`
}

type StorageBillingV2Row struct {
	RateCode     string  `json:"rate_code"`
	OwnerID      string  `json:"owner_id"`
	LocationCode string  `json:"location_code"`
	Item         string  `json:"item,omitempty"`
	SnapshotDays int     `json:"snapshot_days"`
	AverageUnits float64 `json:"average_units"`
	StorageRate  float64 `json:"storage_rate_per_unit_per_day"`
	NetAmount    float64 `json:"net_amount"`
	TaxRate      float64 `json:"tax_rate"`
	TaxAmount    float64 `json:"tax_amount"`
	TotalAmount  float64 `json:"total_amount"`
}

type InvoiceFromChargesResult struct {
	InvoiceID    string  `json:"invoice_id"`
	ChargeCount  int     `json:"charge_count"`
	TotalAmount  float64 `json:"total_amount"`
	InvoiceState string  `json:"invoice_state"`
}

// stage42AllItemsSnapshot is a zero-valued sentinel only for a rate that
// bills an entire owner/location rather than one SKU. It guarantees that a
// deliberately empty day still exists in the historical average; the billing
// query sums it with ordinary SKU snapshots, so it never inflates units.
const stage42AllItemsSnapshot = "__ALL__"

type chargeTerms struct {
	MarkupPct     float64
	DiscountPct   float64
	MinimumCharge float64
	TaxRate       float64
}

func stage42DataString(data map[string]interface{}, key string) string {
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func stage42Number(data map[string]interface{}, key string) float64 {
	return numFromInterface(data[key])
}

func stage42NumberPresent(data map[string]interface{}, key string) (float64, bool) {
	v, ok := data[key]
	if !ok || v == nil || strings.TrimSpace(fmt.Sprintf("%v", v)) == "" {
		return 0, false
	}
	return numFromInterface(v), true
}

func stage42Codes(value string) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range strings.Split(value, ",") {
		code := strings.TrimSpace(raw)
		if code != "" && !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	return out
}

func stage42ActiveDocByCode(schema, doctype, code string) (map[string]interface{}, error) {
	if strings.TrimSpace(code) == "" {
		return nil, nil
	}
	var raw []byte
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT data FROM %s.documents
		WHERE doctype = $1 AND status = 'Active' AND deleted_at IS NULL
		  AND data->>'code' = $2
		ORDER BY updated_at DESC, id DESC LIMIT 1`, schema), doctype, code).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data := map[string]interface{}{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func stage42ActiveDocs(schema, doctype string) ([]map[string]interface{}, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data FROM %s.documents
		WHERE doctype = $1 AND status = 'Active' AND deleted_at IS NULL
		ORDER BY data->>'code', id`, schema), doctype)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		data := map[string]interface{}{}
		if json.Unmarshal(raw, &data) == nil {
			out = append(out, data)
		}
	}
	return out, rows.Err()
}

func stage42OperationForTask(schema, taskType string) (map[string]interface{}, error) {
	var raw []byte
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT data FROM %s.documents
		WHERE doctype = 'LaborOperation' AND status = 'Active' AND deleted_at IS NULL
		  AND data->>'task_type' = $1
		ORDER BY updated_at DESC, id DESC LIMIT 1`, schema), taskType).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data := map[string]interface{}{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func stage42LaborStandard(schema, operationCode string) (map[string]interface{}, error) {
	var raw []byte
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT data FROM %s.documents
		WHERE doctype = 'LaborStandard' AND status = 'Active' AND deleted_at IS NULL
		  AND data->>'operation_code' = $1
		ORDER BY updated_at DESC, id DESC LIMIT 1`, schema), operationCode).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data := map[string]interface{}{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func stage42ResolveLaborStandard(schema string, standard, operation map[string]interface{}, quantity float64) (*LaborStandardTime, error) {
	if quantity <= 0 {
		quantity = 1
	}
	result := &LaborStandardTime{
		StandardCode:     stage42DataString(standard, "code"),
		OperationCode:    stage42DataString(standard, "operation_code"),
		Quantity:         quantity,
		LaborRatePerHour: stage42Number(operation, "labor_rate_per_hour"),
	}
	for _, code := range stage42Codes(stage42DataString(standard, "element_codes")) {
		element, err := stage42ActiveDocByCode(schema, "LaborElement", code)
		if err != nil {
			return nil, err
		}
		if element == nil {
			return nil, fmt.Errorf("LaborStandard %s references inactive or missing LaborElement %s", result.StandardCode, code)
		}
		result.ElementSeconds += stage42Number(element, "fixed_seconds") + stage42Number(element, "standard_seconds_per_unit")*quantity
	}
	if travelCode := stage42DataString(standard, "travel_section_code"); travelCode != "" {
		travel, err := stage42ActiveDocByCode(schema, "TravelSection", travelCode)
		if err != nil {
			return nil, err
		}
		if travel == nil {
			return nil, fmt.Errorf("LaborStandard %s references inactive or missing TravelSection %s", result.StandardCode, travelCode)
		}
		result.TravelSeconds = stage42Number(travel, "seconds_per_task") + stage42Number(travel, "seconds_per_unit")*quantity
	}
	base := result.ElementSeconds + result.TravelSeconds
	allowancePct := 0.0
	for _, code := range stage42Codes(stage42DataString(standard, "allowance_codes")) {
		allowance, err := stage42ActiveDocByCode(schema, "LaborAllowance", code)
		if err != nil {
			return nil, err
		}
		if allowance == nil {
			return nil, fmt.Errorf("LaborStandard %s references inactive or missing LaborAllowance %s", result.StandardCode, code)
		}
		allowancePct += stage42Number(allowance, "allowance_pct")
		result.AllowanceSeconds += stage42Number(allowance, "fixed_seconds")
	}
	result.AllowanceSeconds += base * allowancePct / 100
	result.TotalSeconds = roundTo2(base + result.AllowanceSeconds)
	result.EstimatedLaborCost = roundTo2(result.TotalSeconds / 3600 * result.LaborRatePerHour)
	return result, nil
}

// ResolveLaborStandardTime calculates a standard from the component masters.
// It intentionally has no "seconds" override parameter: an element, travel
// or allowance change is therefore visible on the next plan/report run.
func ResolveLaborStandardTime(tenantID, standardCode string, quantity float64) (*LaborStandardTime, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	standard, err := stage42ActiveDocByCode(schema, "LaborStandard", standardCode)
	if err != nil {
		return nil, err
	}
	if standard == nil {
		return nil, fmt.Errorf("active LaborStandard %s not found", standardCode)
	}
	operation, err := stage42ActiveDocByCode(schema, "LaborOperation", stage42DataString(standard, "operation_code"))
	if err != nil {
		return nil, err
	}
	if operation == nil {
		return nil, fmt.Errorf("LaborStandard %s references inactive or missing LaborOperation %s", standardCode, stage42DataString(standard, "operation_code"))
	}
	return stage42ResolveLaborStandard(schema, standard, operation, quantity)
}

func stage42ParseISODate(value string) (time.Time, error) {
	return time.Parse("2006-01-02", strings.TrimSpace(value))
}

func stage42ShiftHours(schema, department string, start, end time.Time) float64 {
	var shiftCode, workDays string
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(data->>'shift_code',''), COALESCE(data->>'work_days','')
		FROM %s.documents WHERE doctype = 'WeeklySchedule' AND status = 'Active' AND deleted_at IS NULL
		  AND data->>'department' = $1 ORDER BY updated_at DESC, id DESC LIMIT 1`, schema), department).Scan(&shiftCode, &workDays)
	if err != nil || shiftCode == "" {
		return float64(int(end.Sub(start).Hours()/24)+1) * 8
	}
	shift, err := stage42ActiveDocByCode(schema, "Shift", shiftCode)
	if err != nil || shift == nil {
		return float64(int(end.Sub(start).Hours()/24)+1) * 8
	}
	startClock, err1 := time.Parse("15:04", stage42DataString(shift, "start_time"))
	endClock, err2 := time.Parse("15:04", stage42DataString(shift, "end_time"))
	if err1 != nil || err2 != nil {
		return float64(int(end.Sub(start).Hours()/24)+1) * 8
	}
	minutes := endClock.Sub(startClock).Minutes()
	if minutes <= 0 {
		minutes += 24 * 60
	}
	minutes -= stage42Number(shift, "unpaid_break_minutes")
	if minutes <= 0 {
		return 0
	}
	activeDays := map[string]bool{}
	for _, day := range stage42Codes(workDays) {
		activeDays[strings.ToLower(day[:min(len(day), 3)])] = true
	}
	if len(activeDays) == 0 {
		return 0
	}
	workDayCount := 0
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		if activeDays[strings.ToLower(day.Weekday().String()[:3])] {
			workDayCount++
		}
	}
	return float64(workDayCount) * minutes / 60
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetLaborPlan forecasts work from Pending/Assigned/In Progress task volume.
// A task without an active operation/standard is deliberately counted as
// uncovered instead of guessing a duration; this makes setup omissions visible
// to the planner instead of producing deceptively low staffing numbers.
func GetLaborPlan(tenantID, locationCode, startDate, endDate string) ([]LaborPlanRow, error) {
	start, err := stage42ParseISODate(startDate)
	if err != nil {
		return nil, fmt.Errorf("start date must use YYYY-MM-DD")
	}
	end, err := stage42ParseISODate(endDate)
	if err != nil || end.Before(start) {
		return nil, fmt.Errorf("end date must use YYYY-MM-DD and be on or after start date")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data FROM %s.documents WHERE doctype = 'WarehouseTask' AND deleted_at IS NULL
		AND status IN ('Pending','Assigned','In Progress')
		AND ($1 = '' OR data->>'location_code' = $1)`, schema), locationCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDepartment := map[string]*LaborPlanRow{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		task := map[string]interface{}{}
		if json.Unmarshal(raw, &task) != nil {
			continue
		}
		operation, err := stage42OperationForTask(schema, stage42DataString(task, "task_type"))
		if err != nil {
			return nil, err
		}
		department := "Warehouse"
		if operation != nil && stage42DataString(operation, "department") != "" {
			department = stage42DataString(operation, "department")
		}
		plan := byDepartment[department]
		if plan == nil {
			plan = &LaborPlanRow{Department: department}
			byDepartment[department] = plan
		}
		plan.OpenTasks++
		if operation == nil {
			plan.UncoveredTasks++
			continue
		}
		standard, err := stage42LaborStandard(schema, stage42DataString(operation, "code"))
		if err != nil {
			return nil, err
		}
		if standard == nil {
			plan.UncoveredTasks++
			continue
		}
		timeForTask, err := stage42ResolveLaborStandard(schema, standard, operation, stage42Number(task, "qty"))
		if err != nil {
			return nil, err
		}
		plan.PlannedSeconds += timeForTask.TotalSeconds
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]LaborPlanRow, 0, len(byDepartment))
	for _, plan := range byDepartment {
		plan.PlannedSeconds = roundTo2(plan.PlannedSeconds)
		plannedHours := plan.PlannedSeconds / 3600
		availableHours := stage42ShiftHours(schema, plan.Department, start, end)
		// roundTo2 truncates (appropriately for currency throughout this
		// codebase) and would turn a genuine six-minute workload into zero.
		// Headcount must use the unrounded values; display keeps three decimals.
		plan.PlannedHours = math.Round(plannedHours*1000) / 1000
		plan.AvailableHoursPerWorker = math.Round(availableHours*1000) / 1000
		if plannedHours > 0 && availableHours > 0 {
			plan.ForecastHeadcount = int(math.Ceil(plannedHours / availableHours))
		}
		out = append(out, *plan)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Department < out[j].Department })
	return out, nil
}

func stage42ShortHash(value string) string {
	sum := sha1.Sum([]byte(value))
	return strings.ToUpper(hex.EncodeToString(sum[:])[:16])
}

func stage42ChargeTerms(schema, ownerID, rateGroupCode string, codeData map[string]interface{}, occurredOn string) (chargeTerms, error) {
	terms := chargeTerms{}
	if codeData != nil {
		terms.MarkupPct += stage42Number(codeData, "markup_pct")
		terms.DiscountPct += stage42Number(codeData, "discount_pct")
		terms.MinimumCharge = math.Max(terms.MinimumCharge, stage42Number(codeData, "minimum_charge"))
		terms.TaxRate = stage42Number(codeData, "tax_rate")
		if rateGroupCode == "" {
			rateGroupCode = stage42DataString(codeData, "rate_group_code")
		}
	}
	if rateGroupCode != "" {
		rateGroup, err := stage42ActiveDocByCode(schema, "RateGroup", rateGroupCode)
		if err != nil {
			return terms, err
		}
		if rateGroup != nil {
			terms.MarkupPct += stage42Number(rateGroup, "markup_pct")
			terms.DiscountPct += stage42Number(rateGroup, "discount_pct")
			terms.MinimumCharge = math.Max(terms.MinimumCharge, stage42Number(rateGroup, "minimum_charge"))
			if tax, present := stage42NumberPresent(rateGroup, "tax_rate"); present {
				terms.TaxRate = tax
			}
		}
	}
	var raw []byte
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT data FROM %s.documents WHERE doctype = 'ChargeContract' AND status = 'Active' AND deleted_at IS NULL
		  AND data->>'owner_id' = $1
		  AND (COALESCE(data->>'rate_group_code','') = '' OR COALESCE(data->>'rate_group_code','') = $2)
		  AND COALESCE(data->>'effective_from','') <= $3
		  AND (COALESCE(data->>'effective_to','') = '' OR data->>'effective_to' >= $3)
		ORDER BY COALESCE(data->>'rate_group_code','') DESC, data->>'effective_from' DESC, id DESC LIMIT 1`, schema), ownerID, rateGroupCode, occurredOn).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return terms, err
	}
	if err == nil {
		contract := map[string]interface{}{}
		if err := json.Unmarshal(raw, &contract); err != nil {
			return terms, err
		}
		terms.MarkupPct += stage42Number(contract, "markup_pct")
		terms.DiscountPct += stage42Number(contract, "discount_pct")
		terms.MinimumCharge = math.Max(terms.MinimumCharge, stage42Number(contract, "minimum_charge"))
		if tax, present := stage42NumberPresent(contract, "tax_rate"); present {
			terms.TaxRate = tax
		}
	}
	return terms, nil
}

func stage42ApplyTerms(base float64, terms chargeTerms) (net, taxAmount, total float64) {
	net = base * (1 + terms.MarkupPct/100) * (1 - terms.DiscountPct/100)
	net = math.Max(net, terms.MinimumCharge)
	net = roundTo2(net)
	taxAmount = roundTo2(net * terms.TaxRate / 100)
	total = roundTo2(net + taxAmount)
	return
}

func stage42CaptureCharge(schema, eventKey, triggerEvent, chargeCode, ownerID, locationCode string, quantity, baseRate float64, terms chargeTerms, occurredOn, userID string) (string, error) {
	if strings.TrimSpace(eventKey) == "" || strings.TrimSpace(chargeCode) == "" || strings.TrimSpace(ownerID) == "" {
		return "", fmt.Errorf("event key, charge code and owner are required to capture a charge")
	}
	if quantity <= 0 {
		return "", fmt.Errorf("charge quantity must be positive")
	}
	net, tax, total := stage42ApplyTerms(quantity*baseRate, terms)
	id := "CHG-" + stage42ShortHash(eventKey+"|"+chargeCode)
	data := map[string]interface{}{
		"code": id, "event_key": eventKey, "trigger_event": triggerEvent,
		"charge_code": chargeCode, "owner_id": ownerID, "location_code": locationCode,
		"quantity": quantity, "net_amount": net, "tax_rate": terms.TaxRate,
		"tax_amount": tax, "total_amount": total, "occurred_on": occurredOn, "status": "Captured",
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'CapturedCharge', $2, 'Captured', $3) ON CONFLICT (id) DO NOTHING`, schema), id, payload, userID)
	if err != nil {
		return "", err
	}
	return id, nil
}

// Stage 42.5.5: prefers the explicit per-stock owner (ownerOfBinStock, which
// itself falls back to the bin's own owner_id when no bin_stock_owner row
// exists for this exact SKU) over a bare Bin lookup - a mixed-owner bin now
// attributes the charge to whoever's stock the task actually moved, not to
// "the bin's one owner" when it no longer has just one.
func stage42TaskOwner(schema string, task *WarehouseTaskInfo) string {
	for _, binCode := range []string{task.ToBin, task.FromBin} {
		if binCode == "" {
			continue
		}
		if task.Item != "" {
			if owner, err := ownerOfBinStock(schema, binCode, task.Item, "Good"); err == nil && owner != "" {
				return owner
			}
		}
		var owner string
		err := db.DB.QueryRow(fmt.Sprintf(`
			SELECT COALESCE(data->>'owner_id','') FROM %s.documents
			WHERE doctype = 'Bin' AND deleted_at IS NULL AND data->>'bin_code' = $1
			ORDER BY id LIMIT 1`, schema), binCode).Scan(&owner)
		if err == nil && owner != "" {
			return owner
		}
	}
	return ""
}

// CaptureWarehouseTaskCharges is the event monitor's automatic path. It only
// emits when a completed task can be attributed to an owner bin; a single-owner
// warehouse therefore keeps working without accidentally producing customer
// invoices until it intentionally configures 3PL ownership and charge codes.
func CaptureWarehouseTaskCharges(tenantID, taskID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	task, err := GetWarehouseTask(tenantID, taskID)
	if err != nil || task == nil || task.Status != WTStatusCompleted {
		return err
	}
	ownerID := stage42TaskOwner(schema, task)
	if ownerID == "" {
		return nil
	}
	codes, err := stage42ActiveDocs(schema, "ChargeCode")
	if err != nil {
		return err
	}
	today := time.Now().Format("2006-01-02")
	for _, code := range codes {
		if stage42DataString(code, "trigger_event") != "Warehouse Task Completed" {
			continue
		}
		if wanted := stage42DataString(code, "task_type"); wanted != "" && wanted != task.TaskType {
			continue
		}
		if wanted := stage42DataString(code, "owner_id"); wanted != "" && wanted != ownerID {
			continue
		}
		if wanted := stage42DataString(code, "location_code"); wanted != "" && wanted != task.LocationCode {
			continue
		}
		terms, err := stage42ChargeTerms(schema, ownerID, stage42DataString(code, "rate_group_code"), code, today)
		if err != nil {
			return err
		}
		qty := task.Qty
		if qty <= 0 {
			qty = 1
		}
		if _, err := stage42CaptureCharge(schema, "WarehouseTask:"+task.DocID, "Warehouse Task Completed", stage42DataString(code, "code"), ownerID, task.LocationCode, qty, stage42Number(code, "default_rate"), terms, today, userID); err != nil {
			return err
		}
	}
	return nil
}

// CaptureManualCharge captures a one-off accessorial through the same rate,
// terms and immutable evidence path as a task-triggered charge.
func CaptureManualCharge(tenantID, eventKey, chargeCode, ownerID, locationCode string, quantity float64, occurredOn, userID string) (string, error) {
	if _, err := stage42ParseISODate(occurredOn); err != nil {
		return "", fmt.Errorf("occurred_on must use YYYY-MM-DD")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	code, err := stage42ActiveDocByCode(schema, "ChargeCode", chargeCode)
	if err != nil {
		return "", err
	}
	if code == nil {
		return "", fmt.Errorf("active ChargeCode %s not found", chargeCode)
	}
	if stage42DataString(code, "trigger_event") != "Manual" {
		return "", fmt.Errorf("ChargeCode %s is not configured for Manual capture", chargeCode)
	}
	terms, err := stage42ChargeTerms(schema, ownerID, stage42DataString(code, "rate_group_code"), code, occurredOn)
	if err != nil {
		return "", err
	}
	return stage42CaptureCharge(schema, eventKey, "Manual", chargeCode, ownerID, locationCode, quantity, stage42Number(code, "default_rate"), terms, occurredOn, userID)
}

func ListCapturedCharges(tenantID, ownerID, start, end, status string) ([]CapturedChargeInfo, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data, status FROM %s.documents WHERE doctype = 'CapturedCharge' AND deleted_at IS NULL
		AND ($1 = '' OR data->>'owner_id' = $1)
		AND ($2 = '' OR data->>'occurred_on' >= $2)
		AND ($3 = '' OR data->>'occurred_on' <= $3)
		AND ($4 = '' OR status = $4)
		ORDER BY data->>'occurred_on' DESC, id DESC`, schema), ownerID, start, end, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CapturedChargeInfo{}
	for rows.Next() {
		var info CapturedChargeInfo
		var raw []byte
		if err := rows.Scan(&info.ID, &raw, &info.Status); err != nil {
			return nil, err
		}
		data := map[string]interface{}{}
		if json.Unmarshal(raw, &data) != nil {
			continue
		}
		info.EventKey = stage42DataString(data, "event_key")
		info.TriggerEvent = stage42DataString(data, "trigger_event")
		info.ChargeCode = stage42DataString(data, "charge_code")
		info.OwnerID = stage42DataString(data, "owner_id")
		info.LocationCode = stage42DataString(data, "location_code")
		info.Quantity = stage42Number(data, "quantity")
		info.NetAmount = stage42Number(data, "net_amount")
		info.TaxRate = stage42Number(data, "tax_rate")
		info.TaxAmount = stage42Number(data, "tax_amount")
		info.TotalAmount = stage42Number(data, "total_amount")
		info.OccurredOn = stage42DataString(data, "occurred_on")
		info.InvoiceID = stage42DataString(data, "invoice_id")
		out = append(out, info)
	}
	return out, rows.Err()
}

func stage42InsertStorageSnapshot(schema, snapshotDate, owner, location, item string, qty float64, userID string) (bool, error) {
	id := "SBS-" + stage42ShortHash(snapshotDate+"|"+owner+"|"+location+"|"+item)
	data, _ := json.Marshal(map[string]interface{}{
		"code": id, "snapshot_date": snapshotDate, "owner_id": owner, "location_code": location,
		"item": item, "quantity": qty, "status": "Captured",
	})
	result, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'StorageBalanceSnapshot', $2, 'Captured', $3) ON CONFLICT (id) DO NOTHING`, schema), id, data, userID)
	if err != nil {
		return false, err
	}
	inserted, _ := result.RowsAffected()
	return inserted > 0, nil
}

// CaptureStorageBalanceSnapshot records one aggregated owner/location/SKU
// balance per day. It also writes an explicit zero for every configured rate
// scope with no stock that day; without that row a zero-balance day would be
// absent from AVG() and historical storage billing would be overstated.
// Deterministic ids make a scheduled retry harmless.
func CaptureStorageBalanceSnapshot(tenantID, snapshotDate, userID string) (int, error) {
	if snapshotDate == "" {
		snapshotDate = time.Now().Format("2006-01-02")
	}
	if _, err := stage42ParseISODate(snapshotDate); err != nil {
		return 0, fmt.Errorf("snapshot_date must use YYYY-MM-DD")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	// Stage 42.5.5: ownerLocationSkuBalances unions the explicit
	// bin_stock_owner breakdown with the same legacy whole-bin fallback this
	// query used alone before - a tenant that has never called
	// RecordOwnerStock sees byte-identical snapshots to before this file
	// existed.
	balances, err := ownerLocationSkuBalances(schema)
	if err != nil {
		return 0, err
	}
	captured := 0
	seen := map[string]bool{}
	for _, bal := range balances {
		owner, location, item, qty := bal.Owner, bal.Location, bal.Sku, bal.Qty
		inserted, err := stage42InsertStorageSnapshot(schema, snapshotDate, owner, location, item, qty, userID)
		if err != nil {
			return captured, err
		}
		seen[owner+"\x00"+location+"\x00"+item] = true
		if inserted {
			captured++
		}
	}

	// A daily snapshot is only meaningful for a configured billing scope. For
	// an item rate, add its zero if that SKU was absent. For an all-item rate,
	// add one zero sentinel unconditionally: real SKU rows plus zero still sum
	// to the correct total, while an empty location now contributes a real 0.
	rateRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'owner_id',''), COALESCE(data->>'location_code',''), COALESCE(data->>'item','')
		FROM %s.documents WHERE doctype = 'StorageBillingRate' AND status = 'Active' AND deleted_at IS NULL`, schema))
	if err != nil {
		return captured, err
	}
	defer rateRows.Close()
	for rateRows.Next() {
		var owner, location, item string
		if err := rateRows.Scan(&owner, &location, &item); err != nil {
			return captured, err
		}
		snapshotItem := item
		if snapshotItem == "" {
			snapshotItem = stage42AllItemsSnapshot
		} else if seen[owner+"\x00"+location+"\x00"+item] {
			continue
		}
		inserted, err := stage42InsertStorageSnapshot(schema, snapshotDate, owner, location, snapshotItem, 0, userID)
		if err != nil {
			return captured, err
		}
		if inserted {
			captured++
		}
	}
	return captured, rateRows.Err()
}

func stage42StorageAverage(schema, owner, location, item, start, end string) (int, float64, error) {
	query := fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(AVG(day_qty), 0) FROM (
			SELECT data->>'snapshot_date', SUM(COALESCE((data->>'quantity')::numeric, 0)) AS day_qty
			FROM %s.documents WHERE doctype = 'StorageBalanceSnapshot' AND status = 'Captured' AND deleted_at IS NULL
			AND data->>'owner_id' = $1 AND data->>'location_code' = $2
			AND ($3 = '' OR data->>'item' = $3)
			AND data->>'snapshot_date' >= $4 AND data->>'snapshot_date' <= $5
			GROUP BY data->>'snapshot_date'
		) daily`, schema)
	var days int
	var average float64
	err := db.DB.QueryRow(query, owner, location, item, start, end).Scan(&days, &average)
	return days, average, err
}

// GetStorageBillingV2 reports from daily balance snapshots, not a projection
// of today's bins. SnapshotDays is surfaced so an operator can see whether an
// historical billing window is complete before they capture or invoice it.
func GetStorageBillingV2(tenantID, ownerID, start, end string) ([]StorageBillingV2Row, error) {
	if _, err := stage42ParseISODate(start); err != nil {
		return nil, fmt.Errorf("start date must use YYYY-MM-DD")
	}
	if _, err := stage42ParseISODate(end); err != nil {
		return nil, fmt.Errorf("end date must use YYYY-MM-DD")
	}
	if end < start {
		return nil, fmt.Errorf("end date must be on or after start date")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'code', id), COALESCE(data->>'owner_id',''), COALESCE(data->>'location_code',''),
		       COALESCE(data->>'item',''), COALESCE((data->>'storage_rate_per_unit_per_day')::numeric,0),
		       COALESCE(data->>'rate_group_code',''), COALESCE(data->>'charge_code',''), COALESCE((data->>'tax_rate')::numeric,0)
		FROM %s.documents WHERE doctype = 'StorageBillingRate' AND status = 'Active' AND deleted_at IS NULL
		AND ($1 = '' OR data->>'owner_id' = $1) ORDER BY data->>'owner_id', data->>'location_code', id`, schema), ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StorageBillingV2Row{}
	for rows.Next() {
		var row StorageBillingV2Row
		var rateGroupCode, chargeCode string
		if err := rows.Scan(&row.RateCode, &row.OwnerID, &row.LocationCode, &row.Item, &row.StorageRate, &rateGroupCode, &chargeCode, &row.TaxRate); err != nil {
			return nil, err
		}
		row.SnapshotDays, row.AverageUnits, err = stage42StorageAverage(schema, row.OwnerID, row.LocationCode, row.Item, start, end)
		if err != nil {
			return nil, err
		}
		var codeData map[string]interface{}
		if chargeCode != "" {
			codeData, err = stage42ActiveDocByCode(schema, "ChargeCode", chargeCode)
			if err != nil {
				return nil, err
			}
		}
		terms, err := stage42ChargeTerms(schema, row.OwnerID, rateGroupCode, codeData, end)
		if err != nil {
			return nil, err
		}
		if row.TaxRate != 0 && terms.TaxRate == 0 {
			terms.TaxRate = row.TaxRate
		}
		base := row.AverageUnits * row.StorageRate * float64(row.SnapshotDays)
		row.NetAmount, row.TaxAmount, row.TotalAmount = stage42ApplyTerms(base, terms)
		row.TaxRate = terms.TaxRate
		row.AverageUnits = roundTo2(row.AverageUnits)
		out = append(out, row)
	}
	return out, rows.Err()
}

// CaptureStorageCharges turns the v2 historical calculation into ordinary
// CapturedCharge evidence, ready for the same invoice generator as handling
// and accessorials. A rate's optional ChargeCode keeps its configured wording;
// otherwise its own code is a stable invoice-line identifier.
func CaptureStorageCharges(tenantID, ownerID, start, end, userID string) (int, error) {
	rows, err := GetStorageBillingV2(tenantID, ownerID, start, end)
	if err != nil {
		return 0, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	captured := 0
	for _, row := range rows {
		if row.SnapshotDays == 0 || row.TotalAmount == 0 {
			continue
		}
		id := "CHG-" + stage42ShortHash("StoragePeriod:"+row.RateCode+"|"+start+"|"+end)
		data, _ := json.Marshal(map[string]interface{}{
			"code": id, "event_key": "StoragePeriod:" + row.RateCode + ":" + start + ":" + end,
			"trigger_event": "Storage Period", "charge_code": row.RateCode, "owner_id": row.OwnerID,
			"location_code": row.LocationCode, "quantity": row.AverageUnits, "net_amount": row.NetAmount,
			"tax_rate": row.TaxRate, "tax_amount": row.TaxAmount, "total_amount": row.TotalAmount,
			"occurred_on": end, "status": "Captured", "snapshot_days": row.SnapshotDays,
		})
		result, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, 'CapturedCharge', $2, 'Captured', $3) ON CONFLICT (id) DO NOTHING`, schema), id, data, userID)
		if err != nil {
			return captured, err
		}
		if n, _ := result.RowsAffected(); n > 0 {
			captured++
		}
	}
	return captured, nil
}

// GenerateInvoiceFromCapturedCharges creates and posts a SalesInvoice in the
// same way finance posts any other credit sale. Its deterministic batch key
// makes an interrupted retry safe: a draft is resumed and posted, while an
// already-posted batch only links its still-Captured evidence.
func GenerateInvoiceFromCapturedCharges(tenantID, ownerID, start, end, userID string) (*InvoiceFromChargesResult, error) {
	if strings.TrimSpace(ownerID) == "" {
		return nil, fmt.Errorf("owner_id is required")
	}
	if _, err := stage42ParseISODate(start); err != nil {
		return nil, fmt.Errorf("start date must use YYYY-MM-DD")
	}
	if _, err := stage42ParseISODate(end); err != nil || end < start {
		return nil, fmt.Errorf("end date must use YYYY-MM-DD and be on or after start date")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	batchKey := "CapturedCharges:" + ownerID + ":" + start + ":" + end
	invoiceID := "INV-CHG-" + stage42ShortHash(batchKey)
	result := &InvoiceFromChargesResult{InvoiceID: invoiceID}
	var existingStatus string
	var invoicePayload []byte
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT status, data FROM %s.documents WHERE id = $1 AND doctype = 'SalesInvoice' AND deleted_at IS NULL`, schema), invoiceID).Scan(&existingStatus, &invoicePayload)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	var chargeIDs []string
	if err == sql.ErrNoRows {
		charges, err := ListCapturedCharges(tenantID, ownerID, start, end, "Captured")
		if err != nil {
			return nil, err
		}
		if len(charges) == 0 {
			return nil, fmt.Errorf("no captured charges are available for this owner and period")
		}
		location := ""
		for _, charge := range charges {
			result.TotalAmount += charge.TotalAmount
			chargeIDs = append(chargeIDs, charge.ID)
			if location == "" {
				location = charge.LocationCode
			}
		}
		result.TotalAmount = roundTo2(result.TotalAmount)
		result.ChargeCount = len(charges)
		invoice := map[string]interface{}{
			"code": invoiceID, "invoice_number": invoiceID, "customer": ownerID, "location": location,
			"total_amount": result.TotalAmount, "status": "Draft", "charge_batch_key": batchKey,
			"charge_event_count": len(charges), "billing_period_start": start, "billing_period_end": end,
			"charge_event_ids": chargeIDs, "source": "WMS Captured Charges",
		}
		if err := ApplyDocumentCurrency(tenantID, "SalesInvoice", invoice); err != nil {
			return nil, err
		}
		payload, err := json.Marshal(invoice)
		if err != nil {
			return nil, err
		}
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, 'SalesInvoice', $2, 'Draft', 'system')`, schema), invoiceID, payload); err != nil {
			return nil, err
		}
		existingStatus = "Draft"
	} else {
		// The invoice permanently owns the exact charge ids it priced. Never
		// re-query a date range on retry: a charge captured after the first
		// attempt belongs to the next invoice, not to a document whose total
		// has already been posted to the GL.
		invoice := map[string]interface{}{}
		if err := json.Unmarshal(invoicePayload, &invoice); err != nil {
			return nil, err
		}
		rawIDs, ok := invoice["charge_event_ids"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("charge invoice %s has no frozen charge_event_ids; do not retry it automatically", invoiceID)
		}
		for _, raw := range rawIDs {
			if id, ok := raw.(string); ok && id != "" {
				chargeIDs = append(chargeIDs, id)
			}
		}
		if len(chargeIDs) == 0 {
			return nil, fmt.Errorf("charge invoice %s has no frozen charge_event_ids; do not retry it automatically", invoiceID)
		}
		result.ChargeCount = len(chargeIDs)
		result.TotalAmount = stage42Number(invoice, "total_amount")
	}
	if existingStatus == "Cancelled" {
		return nil, fmt.Errorf("charge invoice %s is cancelled; create a new billing period batch rather than resurrecting it", invoiceID)
	}
	if existingStatus == "Draft" {
		if _, err := PostSalesInvoice(tenantID, invoiceID, userID); err != nil {
			return nil, fmt.Errorf("invoice %s is retained as Draft but could not be posted: %w", invoiceID, err)
		}
		existingStatus = "Approved"
	}
	chargeIDsJSON, err := json.Marshal(chargeIDs)
	if err != nil {
		return nil, err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = data || jsonb_build_object('invoice_id', $1::text, 'billed_at', CURRENT_TIMESTAMP::text),
		status = 'Billed', updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'CapturedCharge' AND status = 'Captured' AND deleted_at IS NULL
		  AND id IN (SELECT jsonb_array_elements_text($2::jsonb))`, schema), invoiceID, string(chargeIDsJSON)); err != nil {
		return nil, err
	}
	result.InvoiceState = existingStatus
	LogAuditEvent(tenantID, userID, "GENERATE_CAPTURED_CHARGE_INVOICE", "SUCCESS", fmt.Sprintf("Posted %s from %d captured WMS charges", invoiceID, result.ChargeCount))
	return result, nil
}

type stage42TaskMetric struct {
	TaskType        string
	LocationCode    string
	UserID          string
	TaskCount       int
	Quantity        float64
	StandardSeconds float64
	LaborCost       float64
}

func stage42TaskMetrics(tenantID, start, end string) ([]stage42TaskMetric, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'task_type',''), COALESCE(data->>'location_code',''), COALESCE(data->>'user_id',''),
		COALESCE((data->>'qty')::numeric, 0)
		FROM %s.documents WHERE doctype = 'TaskCompletionLog' AND deleted_at IS NULL
		AND ($1 = '' OR created_at >= $1::date) AND ($2 = '' OR created_at < ($2::date + interval '1 day'))`, schema), start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []stage42TaskMetric
	for rows.Next() {
		var metric stage42TaskMetric
		if err := rows.Scan(&metric.TaskType, &metric.LocationCode, &metric.UserID, &metric.Quantity); err != nil {
			return nil, err
		}
		metric.TaskCount = 1
		operation, err := stage42OperationForTask(schema, metric.TaskType)
		if err != nil || operation == nil {
			out = append(out, metric)
			continue
		}
		standard, err := stage42LaborStandard(schema, stage42DataString(operation, "code"))
		if err != nil || standard == nil {
			out = append(out, metric)
			continue
		}
		resolved, err := stage42ResolveLaborStandard(schema, standard, operation, metric.Quantity)
		if err != nil {
			return nil, err
		}
		metric.StandardSeconds = resolved.TotalSeconds
		metric.LaborCost = resolved.EstimatedLaborCost
		out = append(out, metric)
	}
	return out, rows.Err()
}

func stage42MetricRows(metrics []stage42TaskMetric, group func(stage42TaskMetric) string, label string) []map[string]interface{} {
	type aggregate struct {
		Tasks              int
		Qty, Seconds, Cost float64
	}
	byKey := map[string]*aggregate{}
	for _, m := range metrics {
		key := group(m)
		if key == "" {
			key = "Unspecified"
		}
		a := byKey[key]
		if a == nil {
			a = &aggregate{}
			byKey[key] = a
		}
		a.Tasks += m.TaskCount
		a.Qty += m.Quantity
		a.Seconds += m.StandardSeconds
		a.Cost += m.LaborCost
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]map[string]interface{}, 0, len(keys))
	for _, key := range keys {
		a := byKey[key]
		out = append(out, map[string]interface{}{label: key, "task_count": a.Tasks, "quantity": roundTo2(a.Qty), "standard_hours": roundTo2(a.Seconds / 3600), "labor_cost": roundTo2(a.Cost)})
	}
	return out
}

func init() {
	// 42.6.5: six complementary report definitions. They deliberately share the
	// TaskCompletionLog instrumentation rather than introducing six nearly
	// identical WMS query endpoints.
	dateParams := []ReportParam{{Key: "start", Label: "From (optional)", Type: "date"}, {Key: "end", Label: "To (optional)", Type: "date"}}
	RegisterReport(ReportDefinition{ID: "labor-enterprise-productivity", Label: "Labour Enterprise Productivity", Category: "WMS", Columns: []ReportColumn{{Key: "enterprise", Label: "Scope"}, {Key: "task_count", Label: "Tasks"}, {Key: "quantity", Label: "Quantity"}, {Key: "standard_hours", Label: "Standard Hours"}, {Key: "labor_cost", Label: "Labour Cost", Sensitive: true}}, Params: dateParams,
		Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
			m, e := stage42TaskMetrics(tenantID, p["start"], p["end"])
			return stage42MetricRows(m, func(stage42TaskMetric) string { return "Enterprise" }, "enterprise"), e
		}})
	RegisterReport(ReportDefinition{ID: "labor-facility-performance", Label: "Labour Facility Performance", Category: "WMS", Columns: []ReportColumn{{Key: "location", Label: "Location"}, {Key: "task_count", Label: "Tasks"}, {Key: "quantity", Label: "Quantity"}, {Key: "standard_hours", Label: "Standard Hours"}, {Key: "labor_cost", Label: "Labour Cost", Sensitive: true}}, Params: dateParams,
		Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
			m, e := stage42TaskMetrics(tenantID, p["start"], p["end"])
			return stage42MetricRows(m, func(v stage42TaskMetric) string { return v.LocationCode }, "location"), e
		}})
	RegisterReport(ReportDefinition{ID: "labor-task-productivity", Label: "Labour Task Productivity", Category: "WMS", Columns: []ReportColumn{{Key: "task_type", Label: "Task Type"}, {Key: "task_count", Label: "Tasks"}, {Key: "quantity", Label: "Quantity"}, {Key: "standard_hours", Label: "Standard Hours"}, {Key: "labor_cost", Label: "Labour Cost", Sensitive: true}}, Params: dateParams,
		Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
			m, e := stage42TaskMetrics(tenantID, p["start"], p["end"])
			return stage42MetricRows(m, func(v stage42TaskMetric) string { return v.TaskType }, "task_type"), e
		}})
	RegisterReport(ReportDefinition{ID: "labor-user-performance", Label: "Labour User Performance", Category: "WMS", Columns: []ReportColumn{{Key: "user", Label: "User"}, {Key: "task_count", Label: "Tasks"}, {Key: "quantity", Label: "Quantity"}, {Key: "standard_hours", Label: "Standard Hours"}, {Key: "labor_cost", Label: "Labour Cost", Sensitive: true}}, Params: dateParams,
		Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
			m, e := stage42TaskMetrics(tenantID, p["start"], p["end"])
			return stage42MetricRows(m, func(v stage42TaskMetric) string { return v.UserID }, "user"), e
		}})
	RegisterReport(ReportDefinition{ID: "labor-standards-audit", Label: "Labour Standards Audit", Category: "WMS", Columns: []ReportColumn{{Key: "standard_code", Label: "Standard"}, {Key: "operation_code", Label: "Operation"}, {Key: "element_seconds", Label: "Element Seconds"}, {Key: "travel_seconds", Label: "Travel Seconds"}, {Key: "allowance_seconds", Label: "Allowance Seconds"}, {Key: "total_seconds", Label: "Total Seconds"}},
		Run: func(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
			schema, e := db.GetTenantSchema(tenantID)
			if e != nil {
				return nil, e
			}
			standards, e := stage42ActiveDocs(schema, "LaborStandard")
			if e != nil {
				return nil, e
			}
			out := []map[string]interface{}{}
			for _, standard := range standards {
				op, x := stage42ActiveDocByCode(schema, "LaborOperation", stage42DataString(standard, "operation_code"))
				if x != nil || op == nil {
					continue
				}
				r, x := stage42ResolveLaborStandard(schema, standard, op, 1)
				if x != nil {
					return nil, x
				}
				out = append(out, map[string]interface{}{"standard_code": r.StandardCode, "operation_code": r.OperationCode, "element_seconds": r.ElementSeconds, "travel_seconds": r.TravelSeconds, "allowance_seconds": r.AllowanceSeconds, "total_seconds": r.TotalSeconds})
			}
			return out, nil
		}})
	RegisterReport(ReportDefinition{ID: "labor-cost", Label: "Labour Cost", Category: "WMS", Columns: []ReportColumn{{Key: "location", Label: "Location"}, {Key: "task_count", Label: "Tasks"}, {Key: "quantity", Label: "Quantity"}, {Key: "standard_hours", Label: "Standard Hours"}, {Key: "labor_cost", Label: "Labour Cost", Sensitive: true}}, Params: dateParams,
		Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
			m, e := stage42TaskMetrics(tenantID, p["start"], p["end"])
			return stage42MetricRows(m, func(v stage42TaskMetric) string { return v.LocationCode }, "location"), e
		}})
	RegisterReport(ReportDefinition{ID: "storage-billing-v2", Label: "3PL Storage Billing v2", Category: "WMS", Columns: []ReportColumn{{Key: "rate_code", Label: "Rate"}, {Key: "owner_id", Label: "Owner"}, {Key: "location_code", Label: "Location"}, {Key: "item", Label: "Item"}, {Key: "snapshot_days", Label: "Snapshot Days"}, {Key: "average_units", Label: "Average Units"}, {Key: "net_amount", Label: "Net", Sensitive: true}, {Key: "tax_amount", Label: "Tax", Sensitive: true}, {Key: "total_amount", Label: "Total", Sensitive: true}}, Params: []ReportParam{{Key: "owner_id", Label: "Owner (optional)", Type: "text"}, {Key: "start", Label: "Period Start", Type: "date", Required: true}, {Key: "end", Label: "Period End", Type: "date", Required: true}},
		Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
			rows, e := GetStorageBillingV2(tenantID, p["owner_id"], p["start"], p["end"])
			if e != nil {
				return nil, e
			}
			return structsToRows(rows)
		}})
}
