package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"
)

// Ask 170 item 170.33: project revenue recognition.
//
// Behind projects.revenue_recognition (default off), RunProjectRevenue books,
// per project and month, the revenue earned to date against what has been
// billed (approved sales invoices that name the project):
//
//	earned   = by milestone: the value of completed milestones, or
//	           by cost to complete: contract value x min(1, cost to date /
//	           estimated cost)
//	position = earned - billed
//	  > 0  unbilled revenue, a contract asset (1330)
//	  < 0  over-billing, deferred revenue (2600)
//
// Each month posts only the change in that position since the last month, with
// 4100 as the other side, so the revenue books to the earned amount in total.
// A project-month is booked once: its ProjectRevenueEntry and the posting key
// ProjectRevenue:<project>:<YYYY-MM> both stop a second run.

const (
	SettingProjectRevenue          = "projects.revenue_recognition"
	ProjectRevenueEntryDoctype     = "ProjectRevenueEntry"
	PostingEventProjectRevAsset    = "project_revenue_asset"
	PostingEventProjectRevDeferral = "project_revenue_deferral"
	projectRevCode                 = "FIN-1724"
)

func init() {
	postingEvents = append(postingEvents, PostingEventProjectRevAsset, PostingEventProjectRevDeferral)
	RegisterSetting(SettingDefinition{
		Key: SettingProjectRevenue, Module: "Finance",
		Label: "Recognise project revenue monthly", Type: SettingTypeBool, Default: "false",
		Description: "Ask 170.33: each month, a project set to a revenue method books its earned revenue against what was billed (unbilled revenue or deferred revenue). Off: nothing is booked.",
	})
}

// ProjectRevenueResult is one project-month's outcome.
type ProjectRevenueResult struct {
	Project   string  `json:"project"`
	Period    string  `json:"period"`
	Earned    float64 `json:"earned"`
	Billed    float64 `json:"billed"`
	Asset     float64 `json:"asset"`
	Liability float64 `json:"liability"`
	Skipped   bool    `json:"skipped"` // already booked for the month
}

// RunProjectRevenue books the month (YYYY-MM) for every Active project that
// has a revenue method.
func RunProjectRevenue(tenantID, period string) ([]ProjectRevenueResult, error) {
	if !GetSettingBool(tenantID, SettingProjectRevenue) {
		return nil, &ValidationError{Code: projectRevCode, Message: "Project revenue recognition is off. Turn it on in Settings."}
	}
	first, err := time.Parse("2006-01", period)
	if err != nil {
		return nil, &ValidationError{Code: projectRevCode, Message: "Month must look like 2026-11."}
	}
	last := first.AddDate(0, 1, -1)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text FROM %s.documents WHERE doctype = 'Project' AND deleted_at IS NULL AND status = 'Active' ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	type pr struct {
		id   string
		data map[string]interface{}
	}
	var projects []pr
	for rows.Next() {
		var id, raw string
		var d map[string]interface{}
		if rows.Scan(&id, &raw) == nil && json.Unmarshal([]byte(raw), &d) == nil {
			if m := strField(d, "rev_rec_method"); m == "Milestone" || m == "Cost to complete" {
				projects = append(projects, pr{id, d})
			}
		}
	}
	rows.Close()
	out := []ProjectRevenueResult{}
	for _, p := range projects {
		r, err := runProjectRevenueOne(tenantID, schema, p.id, p.data, period, last.Format("2006-01-02"))
		if err != nil {
			return out, fmt.Errorf("project %s: %w", p.id, err)
		}
		out = append(out, r)
	}
	return out, nil
}

func runProjectRevenueOne(tenantID, schema, project string, pd map[string]interface{}, period, lastDay string) (ProjectRevenueResult, error) {
	res := ProjectRevenueResult{Project: project, Period: period}
	tx, err := db.DB.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	// One run per project-month at a time; a second waits, then finds the entry.
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "projrev:"+schema+":"+project+":"+period); err != nil {
		return res, err
	}
	var exists bool
	if err := tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'ProjectRevenueEntry' AND data->>'project' = $1 AND data->>'period' = $2)`, schema), project, period).Scan(&exists); err != nil {
		return res, err
	}
	if exists {
		res.Skipped = true
		return res, nil
	}
	// Earned, in paise.
	var earned int64
	if pd["rev_rec_method"] == "Milestone" {
		mrows, err := tx.Query(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE doctype = 'ProjectMilestone' AND deleted_at IS NULL AND status IN ('Completed','Invoiced') AND data->>'project' = $1 AND COALESCE(data->>'completed_at','') <= $2`, schema), project, lastDay)
		if err != nil {
			return res, err
		}
		for mrows.Next() {
			var raw string
			var m map[string]interface{}
			if mrows.Scan(&raw) == nil && json.Unmarshal([]byte(raw), &m) == nil {
				earned += RupeesToPaise(milestoneAmount(m, pd))
			}
		}
		mrows.Close()
	} else {
		est := RupeesToPaise(numFromInterface(pd["estimated_cost"]))
		if est <= 0 {
			return res, &ValidationError{Code: projectRevCode, Message: fmt.Sprintf("Project %s uses cost to complete but has no estimated cost.", project)}
		}
		var cost int64
		if err := tx.QueryRow(fmt.Sprintf(`
			SELECT COALESCE(SUM(g.debit - g.credit), 0) FROM %s.gl_postings g JOIN %s.gl_accounts a ON a.account_code = g.account_code
			 WHERE g.project = $1 AND a.account_type = 'Expense' AND g.created_at < public.erp_day_start($2::date + 1)`, schema, schema), project, lastDay).Scan(&cost); err != nil {
			return res, err
		}
		pct := math.Max(0, math.Min(1, float64(cost)/float64(est)))
		earned = int64(math.Round(float64(RupeesToPaise(numFromInterface(pd["contract_value"]))) * pct))
	}
	var billed int64
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(ROUND((data->>'total_amount')::numeric * 100)), 0)::bigint FROM %s.documents
		 WHERE doctype = 'SalesInvoice' AND deleted_at IS NULL AND status = 'Approved' AND data->>'project' = $1`, schema), project).Scan(&billed); err != nil {
		return res, err
	}
	pos := earned - billed
	var asset, liab int64
	if pos > 0 {
		asset = pos
	} else {
		liab = -pos
	}
	// The baseline is the latest earlier month's position.
	var asset0, liab0 int64
	var raw string
	err = tx.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE doctype = 'ProjectRevenueEntry' AND data->>'project' = $1 AND data->>'period' < $2 ORDER BY data->>'period' DESC LIMIT 1`, schema), project, period).Scan(&raw)
	if err == nil {
		var prev map[string]interface{}
		if json.Unmarshal([]byte(raw), &prev) == nil {
			asset0 = RupeesToPaise(numFromInterface(prev["asset"]))
			liab0 = RupeesToPaise(numFromInterface(prev["liability"]))
		}
	} else if err != sql.ErrNoRows {
		return res, err
	}
	dAsset, dLiab := asset-asset0, liab-liab0
	debits, credits := map[string]int64{}, map[string]int64{}
	on, _ := time.Parse("2006-01-02", lastDay)
	if dAsset != 0 {
		a := ResolvePostingAccounts(tenantID, PostingEventProjectRevAsset, PostingContext{On: on}, "1330", "4100")
		if dAsset > 0 {
			debits[a.Debit] += dAsset
			credits[a.Credit] += dAsset
		} else {
			credits[a.Debit] += -dAsset
			debits[a.Credit] += -dAsset
		}
	}
	if dLiab != 0 {
		a := ResolvePostingAccounts(tenantID, PostingEventProjectRevDeferral, PostingContext{On: on}, "4100", "2600")
		if dLiab > 0 {
			debits[a.Debit] += dLiab
			credits[a.Credit] += dLiab
		} else {
			credits[a.Debit] += -dLiab
			debits[a.Credit] += -dLiab
		}
	}
	if len(debits) > 0 {
		if err := PostDoubleEntryTx(tx, tenantID, schema, ProjectRevenueEntryDoctype, project+"-"+period, debits, credits, lastDay,
			fmt.Sprintf("ProjectRevenue:%s:%s", project, period), PostingOptions{Project: project}); err != nil {
			return res, fmt.Errorf("failed to post project revenue: %v", err)
		}
	}
	entry := map[string]interface{}{
		"project": project, "period": period, "earned": PaiseToRupees(earned), "billed": PaiseToRupees(billed),
		"asset": PaiseToRupees(asset), "liability": PaiseToRupees(liab), "status": "Posted",
	}
	b, _ := json.Marshal(entry)
	id := NewDocID("PRE")
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'ProjectRevenueEntry', $2, 'Posted', 'system')`, schema), id, b); err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	res.Earned, res.Billed, res.Asset, res.Liability = PaiseToRupees(earned), PaiseToRupees(billed), PaiseToRupees(asset), PaiseToRupees(liab)
	return res, nil
}

// StartProjectRevenueWorker books the month just ended for every tenant that
// has switched the setting on. Cheap and idempotent: a booked month is skipped.
func StartProjectRevenueWorker(ctx context.Context, interval time.Duration) {
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
				schemas, err := listTenantSchemas()
				if err != nil {
					continue
				}
				prev := time.Now().In(businessLocation()).AddDate(0, -1, 0).Format("2006-01")
				for _, schema := range schemas {
					tenantID, err := tenantIDForSchema(schema)
					if err != nil || !GetSettingBoolForSchema(schema, SettingProjectRevenue) {
						continue
					}
					if _, err := RunProjectRevenue(tenantID, prev); err != nil {
						log.Printf("[PROJECT-REVENUE] %s: %v", schema, err)
					}
				}
			}
		}
	}()
}
