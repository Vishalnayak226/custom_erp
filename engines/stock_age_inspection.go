package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Stage 146.3.11 (D21): re-inspection by stock age, the fading-polish check.
// A rule per category (InspectionRule, kind Stock age) with an override per
// item (kind Stock age (item)) says how many days of sitting between checks.
// A daily job files ONE StockAgeInspection per store per age cohort, a week
// ahead of the day the cohort is reached. Nothing is held for sale. The
// person decides: pass, re-polish (on a job-work order), markdown flag, or
// scrap (a stock adjustment, so the write-off is a real journal).
const (
	SettingAgeInspection     = "quality.age_inspection"
	SettingAgeInspectionLead = "quality.age_inspection_days"

	StockAgeInspectionDoctype = "StockAgeInspection"
	ageOpen                   = "Open"
	ageDone                   = "Done"
	ageRuleCode               = "QUALIT-0605"
)

func init() {
	RegisterSetting(SettingDefinition{Key: SettingAgeInspection, Module: "Quality", Label: "Re-inspect stock by age",
		Type: SettingTypeBool, Default: "false",
		Description: "On: a daily check files one inspection per store for stock that has sat as long as an Inspection rule (stock age) says. Nothing is held for sale. Off: no inspections are filed."})
	RegisterSetting(SettingDefinition{Key: SettingAgeInspectionLead, Module: "Quality", Label: "Inspections filed ahead by",
		Type: SettingTypeInt, Default: "7", Unit: "days", Min: settingBound(0), Max: settingBound(60),
		Description: "How many days before the age is reached the inspection is filed and listed as due."})
	settingGatedDoctypes[StockAgeInspectionDoctype] = SettingAgeInspection
	// One screen carries all rules: it shows when either check is on.
	settingGatedDoctypes[InspectionRuleDoctype] = SettingInspectionByRule + "|" + SettingAgeInspection
	RegisterReport(ReportDefinition{ID: "stock-age-inspections-due", Label: "Inspections due by stock age", Category: "Quality",
		Columns: []ReportColumn{{Key: "inspection", Label: "Inspection"}, {Key: "location", Label: "Store"}, {Key: "rule", Label: "Rule"},
			{Key: "age_band", Label: "Age"}, {Key: "age_source", Label: "Age from"}, {Key: "outcome", Label: "Outcome"}, {Key: "markdown", Label: "Markdown"}, {Key: "due", Label: "Due"}, {Key: "days_left", Label: "Days left"}, {Key: "status", Label: "Status"}},
		Run: stockAgeDueReport})
}

// settingGateOpen reads a gate key: "a|b" is open when either setting is on.
func settingGateOpen(tenantID, key string) bool {
	for _, k := range strings.Split(key, "|") {
		if GetSettingBool(tenantID, k) {
			return true
		}
	}
	return false
}

// validateInspectionRule runs when an InspectionRule is saved.
func validateInspectionRule(docData map[string]interface{}) error {
	kind := strField(docData, "rule_kind")
	th := numFromInterface(docData["threshold"])
	bad := func(msg string) error { return &ValidationError{Code: ageRuleCode, Message: msg} }
	switch kind {
	case "Vendor reject rate":
		if th <= 0 || th > 100 {
			return bad("A reject rate rule needs a threshold between 0 and 100.")
		}
	case "Skip lot":
		if th < 1 || numFromInterface(docData["sample_pct"]) <= 0 || numFromInterface(docData["sample_pct"]) > 100 {
			return bad("A skip lot rule needs the number of clean lots and a sample %.")
		}
	case "Stock age", "Stock age (item)":
		if th < 1 || strings.TrimSpace(strField(docData, "ref")) == "" {
			return bad("A stock age rule needs the category (or item) and the days between checks.")
		}
	}
	return nil
}

func ageErr(code, format string, args ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func requireAgeInspection(tenantID string) error {
	if !GetSettingBool(tenantID, SettingAgeInspection) {
		return ageErr(inspectionOffCode, "Stock age inspection is off.")
	}
	return nil
}

// StartStockAgeInspectionWorker files the age inspections of every client that
// has the check on. Filing is idempotent (one per store per cohort), so the
// interval only sets how soon a new cohort is noticed.
func StartStockAgeInspectionWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() || db.DB == nil {
					continue
				}
				schemas, err := sweepTenantSchemas()
				if err != nil {
					continue
				}
				for _, schema := range schemas {
					if !GetSettingBoolForSchema(schema, SettingAgeInspection) {
						continue
					}
					tenantID, err := tenantIDForSchema(schema)
					if err != nil {
						continue
					}
					if on, err := IsModuleEnabled(tenantID, "quality"); err == nil && !on {
						continue
					}
					if _, err := RunStockAgeInspections(tenantID); err != nil {
						log.Printf("[AGEINSP] %s: %v", schema, err)
					}
				}
			}
		}
	}()
}

// RunStockAgeInspections files what is due. Age of a store's stock is the
// days since the last receipt into the store of any item the rule covers
// (the oldest such item sets it). It returns how many it filed.
func RunStockAgeInspections(tenantID string) (int, error) {
	if err := requireAgeInspection(tenantID); err != nil {
		return 0, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	rules := loadInspectionRules(schema, "Stock age", "Stock age (item)")
	lead := GetSettingInt(tenantID, SettingAgeInspectionLead)
	today := BusinessToday()
	todayT, _ := time.Parse("2006-01-02", today)
	itemRuled := map[string]bool{}
	for _, r := range rules {
		if r.Kind == "Stock age (item)" {
			itemRuled[r.Ref] = true
		}
	}
	filed := 0
	for _, r := range rules {
		days := int(r.Threshold)
		if days < 1 {
			continue
		}
		var skus []string
		if r.Kind == "Stock age (item)" {
			skus = []string{r.Ref}
		} else {
			rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'code', id) FROM %s.documents WHERE doctype = 'Item' AND deleted_at IS NULL AND data->>'category' = $1`, schema), r.Ref)
			if err != nil {
				return filed, err
			}
			for rows.Next() {
				var s string
				if rows.Scan(&s) == nil && !itemRuled[s] {
					skus = append(skus, s)
				}
			}
			rows.Close()
		}
		if len(skus) == 0 {
			continue
		}
		held := map[string]bool{}
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT sku, location_code FROM %s.inventory_availability WHERE sku = ANY($1) AND COALESCE(available, 0) > 0`, schema), pq.Array(skus))
		if err != nil {
			return filed, err
		}
		for rows.Next() {
			var s, l string
			if rows.Scan(&s, &l) == nil {
				held[s+"\x00"+l] = true
			}
		}
		rows.Close()
		rows, err = db.DB.Query(fmt.Sprintf(`SELECT data->>'item_id', data->>'warehouse_id', max(created_at) FROM %s.documents
			WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = ANY($1) AND COALESCE((data->>'qty')::numeric, 0) > 0
			GROUP BY 1, 2`, schema), pq.Array(skus))
		if err != nil {
			return filed, err
		}
		oldest := map[string]int{} // store -> age in days of its oldest covered item
		source := map[string]string{}
		dated := datedReceiptDays(schema, skus) // receipt-date barcode first
		for rows.Next() {
			var s, l string
			var last time.Time
			if rows.Scan(&s, &l, &last) != nil || !held[s+"\x00"+l] {
				continue
			}
			day, src := localDateString(last), ageSourceLedger
			if d, ok := dated[s+"\x00"+l]; ok {
				day, src = d, ageSourceBarcode
			}
			lt, _ := time.Parse("2006-01-02", day)
			if age := int(todayT.Sub(lt).Hours() / 24); age > oldest[l] {
				oldest[l], source[l] = age, src
			}
		}
		rows.Close()
		for loc, age := range oldest {
			cohort := (age + lead) / days
			if cohort < 1 {
				continue
			}
			var exists bool
			if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'StockAgeInspection' AND deleted_at IS NULL
				AND data->>'rule' = $1 AND data->>'location' = $2 AND data->>'cohort' = $3)`, schema), r.ID, loc, fmt.Sprint(cohort)).Scan(&exists); err != nil {
				return filed, err
			}
			if exists {
				continue
			}
			wait := cohort*days - age
			if wait < 0 {
				wait = 0
			}
			id := NewDocID("SAI")
			data := map[string]interface{}{"location": loc, "rule": r.ID, "rule_ref": r.Ref, "cohort": cohort, "age_band": fmt.Sprintf("%d-%d days", cohort*days, (cohort+1)*days-1),
				"due_date": todayT.AddDate(0, 0, wait).Format("2006-01-02"), "age_source": source[loc], "age_days": age, "inspection_plan": r.Plan, "status": ageOpen}
			b, _ := json.Marshal(data)
			if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'StockAgeInspection', $2, $3, 'system')`, schema), id, b, ageOpen); err != nil {
				return filed, err
			}
			filed++
		}
	}
	return filed, nil
}

// StockAgeView is one age inspection as a list shows it.
type StockAgeView struct {
	ID        string `json:"id"`
	Location  string `json:"location"`
	Rule      string `json:"rule"`
	Ref       string `json:"rule_ref"`
	AgeBand   string `json:"age_band"`
	Due       string `json:"due_date"`
	DaysLeft  int    `json:"days_left"`
	Plan      string `json:"inspection_plan"`
	Status    string `json:"status"`
	Outcome   string `json:"outcome,omitempty"`
	Markdown  bool   `json:"markdown,omitempty"`
	AgeSource string `json:"age_source,omitempty"`
	AgeDays   int    `json:"age_days,omitempty"`
}

// ListStockAgeInspections lists the Open (default) or Done inspections at the
// stores access allows (a full-access user with no scope sees all).
func ListStockAgeInspections(tenantID, role, status string, access StoreAccess) ([]StockAgeView, error) {
	if err := requireAgeInspection(tenantID); err != nil {
		return nil, err
	}
	if ok, _ := RoleMayOnDoctype(tenantID, role, StockAgeInspectionDoctype, "read"); !ok {
		return nil, ageErr("GLOBAL-0011", "not allowed")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if status != ageDone {
		status = ageOpen
	}
	where, args := "", []interface{}{status}
	if !access.All {
		where, args = " AND data->>'location' = ANY($2)", append(args, pq.Array(access.List()))
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text FROM %s.documents WHERE doctype = 'StockAgeInspection' AND deleted_at IS NULL AND status = $1%s
		ORDER BY data->>'due_date', id LIMIT 500`, schema, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	todayT, _ := time.Parse("2006-01-02", BusinessToday())
	out := []StockAgeView{}
	for rows.Next() {
		var id, raw string
		var d map[string]interface{}
		if rows.Scan(&id, &raw) != nil || json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		v := StockAgeView{ID: id, Location: strField(d, "location"), Rule: strField(d, "rule"), Ref: strField(d, "rule_ref"), AgeBand: strField(d, "age_band"),
			Due: strField(d, "due_date"), Plan: strField(d, "inspection_plan"), Status: status, Outcome: strField(d, "outcome"), Markdown: strField(d, "markdown") == "Yes",
			AgeSource: strField(d, "age_source"), AgeDays: int(numFromInterface(d["age_days"]))}
		if dt, err := time.Parse("2006-01-02", v.Due); err == nil {
			v.DaysLeft = int(math.Round(dt.Sub(todayT).Hours() / 24))
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// StockAgeOutcomeRequest is what a person decides for one age inspection.
type StockAgeOutcomeRequest struct {
	ID         string `json:"id"`
	Outcome    string `json:"outcome"` // Pass, Re-polish, Markdown, Scrap
	Note       string `json:"note"`
	JobWorkRef string `json:"job_work_ref,omitempty"`
	ReasonCode string `json:"reason_code,omitempty"`
	Lines      []struct {
		SKU string `json:"sku"`
		Qty int    `json:"qty"`
	} `json:"lines,omitempty"`
}

// StockAgeOutcomeResult is what the decision did.
type StockAgeOutcomeResult struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Outcome      string `json:"outcome"`
	AdjustmentID string `json:"adjustment_id,omitempty"`
}

// ResolveStockAgeInspection records the decision on an Open inspection at a
// store access allows.
func ResolveStockAgeInspection(tenantID, userID, role string, access StoreAccess, in StockAgeOutcomeRequest) (*StockAgeOutcomeResult, error) {
	if err := requireAgeInspection(tenantID); err != nil {
		return nil, err
	}
	if ok, _ := RoleMayOnDoctype(tenantID, role, StockAgeInspectionDoctype, "update"); !ok {
		return nil, ageErr("GLOBAL-0011", "not allowed")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	bad := func(msg string) error { return ageErr(inspectionWaiveCode, "%s", msg) }
	outcome := strings.TrimSpace(in.Outcome)
	switch outcome {
	case "Pass", "Re-polish", "Markdown", "Scrap":
	default:
		return nil, bad("Choose Pass, Re-polish, Markdown or Scrap.")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	d, status, err := lockDocTx(tx, schema, StockAgeInspectionDoctype, strings.TrimSpace(in.ID))
	if err != nil {
		return nil, ageErr(inspectionLotCode, "That inspection was not found.")
	}
	// Another store's inspection answers the same as one that does not exist.
	if !access.Allows(strField(d, "location")) && !access.All {
		return nil, ageErr(inspectionLotCode, "That inspection was not found.")
	}
	if status != ageOpen {
		return nil, ageErr(inspectionLotCode, "That inspection is already done.")
	}
	res := &StockAgeOutcomeResult{ID: in.ID, Status: ageDone, Outcome: outcome}
	switch outcome {
	case "Re-polish":
		ref := strings.TrimSpace(in.JobWorkRef)
		var found bool
		if ref != "" {
			_ = tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'SubcontractOrder' AND id = $1 AND deleted_at IS NULL)`, schema), ref).Scan(&found)
		}
		if !found {
			return nil, bad("Name the job-work order the pieces go out on.")
		}
		d["job_work_ref"] = ref
	case "Markdown":
		d["markdown"] = "Yes"
	case "Scrap":
		if len(in.Lines) == 0 || strings.TrimSpace(in.ReasonCode) == "" {
			return nil, bad("Scrap needs the items, the quantities and a reason code.")
		}
		var lines []InventoryAdjustmentLine
		asked := map[string]int{}
		for _, l := range in.Lines {
			if l.Qty <= 0 {
				return nil, bad("Enter a quantity above zero for each scrapped item.")
			}
			// Only items this inspection covers, and no more than the store holds.
			sku, held, covered := stockAgeScrapItem(tenantID, schema, d, l.SKU)
			if !covered {
				return nil, bad("That item is not part of this inspection.")
			}
			if asked[sku] += l.Qty; asked[sku] > held {
				return nil, bad("The store does not hold that many of the item.")
			}
			l.SKU = sku
			lines = append(lines, InventoryAdjustmentLine{SKU: l.SKU, Qty: -l.Qty, ReasonCode: strings.TrimSpace(in.ReasonCode), Note: "Stock age inspection " + in.ID})
		}
		// The write-off is an ordinary stock adjustment (stock out, loss booked), made just before this decision is written.
		adj, _, aerr := CreateInventoryAdjustment(tenantID, InventoryAdjustmentInput{Location: strField(d, "location"), Note: "Scrapped at stock age inspection " + in.ID,
			Lines: lines, LossClass: "Damage"}, userID, role)
		if aerr != nil {
			return nil, aerr
		}
		res.AdjustmentID = adj
		d["adjustment_id"] = adj
	}
	d["outcome"], d["outcome_note"] = outcome, strings.TrimSpace(in.Note)
	d["decided_by"], d["decided_on"] = userID, BusinessToday()
	if err := writeDocTx(tx, schema, StockAgeInspectionDoctype, in.ID, d, ageDone); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "STOCK_AGE_INSPECTION_DECIDED", "SUCCESS", fmt.Sprintf("Stock age inspection %s: %s", in.ID, outcome))
	return res, nil
}

// stockAgeScrapItem says whether sku is covered by the inspection's rule (the
// one item the rule names, or an item of the category it names that has no rule
// of its own) and how many the inspection's store holds. The returned sku is the
// item's own code.
func stockAgeScrapItem(tenantID, schema string, insp map[string]interface{}, sku string) (string, int, bool) {
	item, err := ResolveItemBySKU(tenantID, strings.TrimSpace(sku))
	if err != nil || item == nil {
		return "", 0, false
	}
	code := strField(item.Data, "code")
	if code == "" {
		code = item.ID
	}
	var raw string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE doctype = 'InspectionRule' AND id = $1 AND deleted_at IS NULL`, schema), strField(insp, "rule")).Scan(&raw) != nil {
		return "", 0, false
	}
	rule := map[string]interface{}{}
	if json.Unmarshal([]byte(raw), &rule) != nil {
		return "", 0, false
	}
	ref := strField(rule, "ref")
	switch strField(rule, "rule_kind") {
	case "Stock age (item)":
		if code != ref {
			return "", 0, false
		}
	case "Stock age":
		if strField(item.Data, "category") != ref {
			return "", 0, false
		}
		for _, r := range loadInspectionRules(schema, "Stock age (item)") {
			if r.Ref == code {
				return "", 0, false
			}
		}
	default:
		return "", 0, false
	}
	var held int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(sum(on_hand), 0)::int FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), code, strField(insp, "location")).Scan(&held)
	return code, held, true
}

func stockAgeDueReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	lead := GetSettingInt(tenantID, SettingAgeInspectionLead)
	until := time.Now().AddDate(0, 0, lead).Format("2006-01-02")
	if t, err := time.Parse("2006-01-02", BusinessToday()); err == nil {
		until = t.AddDate(0, 0, lead).Format("2006-01-02")
	}
	todayT, _ := time.Parse("2006-01-02", BusinessToday())
	return dayLimitRows(tenantID, `SELECT id, COALESCE(data->>'location', ''), COALESCE(data->>'rule_ref', ''), COALESCE(data->>'age_band', ''),
		COALESCE(data->>'due_date', ''), status, COALESCE(data->>'age_source', ''), COALESCE(data->>'outcome', ''), COALESCE(data->>'markdown', '')
		FROM {s}.documents WHERE doctype = 'StockAgeInspection' AND deleted_at IS NULL
		AND ((status = 'Open' AND COALESCE(data->>'due_date', '') <= $1) OR data->>'markdown' = 'Yes') ORDER BY data->>'due_date', id LIMIT 20001`, []interface{}{until},
		func(scan func(...interface{}) error) (map[string]interface{}, error) {
			var id, loc, ref, band, due, status, src, outcome, md string
			if err := scan(&id, &loc, &ref, &band, &due, &status, &src, &outcome, &md); err != nil {
				return nil, err
			}
			left := 0
			if dt, err := time.Parse("2006-01-02", due); err == nil {
				left = int(math.Round(dt.Sub(todayT).Hours() / 24))
			}
			return map[string]interface{}{"inspection": id, "location": loc, "rule": ref, "age_band": band, "due": due, "days_left": left, "status": status, "age_source": src, "outcome": outcome, "markdown": md}, nil
		})
}

// StoreAppInspectionLots lists the pending lots of the caller's store.
func StoreAppInspectionLots(tenantID, userID, role, store, status string) ([]InspectionLotView, error) {
	if err := requireInspectionByRule(tenantID); err != nil {
		return nil, err
	}
	store, err := storeAppStoreFor(tenantID, userID, store)
	if err != nil {
		return nil, err
	}
	if ok, _ := RoleMayOnDoctype(tenantID, role, InspectionLotDoctype, "read"); !ok {
		return nil, ageErr("GLOBAL-0011", "not allowed")
	}
	if status != lotHeld {
		status = lotPending
	}
	return ListInspectionLots(tenantID, store, status)
}

// StoreAppInspectionResult records results for a lot at the caller's store.
func StoreAppInspectionResult(tenantID, userID, role, store string, in InspectionResultRequest) (*InspectionLotResult, error) {
	if err := requireInspectionByRule(tenantID); err != nil {
		return nil, err
	}
	store, err := storeAppStoreFor(tenantID, userID, store)
	if err != nil {
		return nil, err
	}
	if ok, _ := RoleMayOnDoctype(tenantID, role, InspectionLotDoctype, "update"); !ok {
		return nil, ageErr("GLOBAL-0011", "not allowed")
	}
	if loc := InspectionLotLocation(tenantID, in.LotID); loc != store {
		return nil, ageErr(inspectionLotCode, "That lot is not at your store.")
	}
	return RecordInspectionResult(tenantID, userID, in)
}

const (
	ageSourceBarcode = "Receipt-date barcode"
	ageSourceLedger  = "Last receipt"
)

// datedReceiptDays: for each item and store the newest receipt date a
// receipt-date barcode (inventory.barcode_per_receipt_date) records, keyed
// sku NUL store. An item with no such barcode is absent and uses its ledger.
func datedReceiptDays(schema string, skus []string) map[string]string {
	out := map[string]string{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT b.data->>'item_code', COALESCE(g.data->>'location', ''), max(b.data->>'received_on')
		FROM %[1]s.documents b
		CROSS JOIN LATERAL jsonb_array_elements_text(COALESCE(b.data->'grns', '[]'::jsonb)) AS gid(v)
		JOIN %[1]s.documents g ON g.doctype = 'GRN' AND g.id = gid.v
		WHERE b.doctype = 'ItemBarcode' AND b.deleted_at IS NULL AND b.data->>'kind' = 'Dated' AND b.data->>'item_code' = ANY($1)
		GROUP BY 1, 2`, schema), pq.Array(skus))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s, l, d string
		if rows.Scan(&s, &l, &d) == nil && d != "" {
			out[s+"\x00"+l] = d
		}
	}
	return out
}
