package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 130.16 (74.5, BRD §11-§12): controls and evidence across the new
// transactions.
//
//   - The registers still missing from R-TX-01..16: R-TX-02 advances
//     outstanding with ageing, tied to 2640; R-TX-13 transfer shortage by
//     reason; R-TX-16 print / reprint audit with repeat-count anomalies.
//   - Control totals for the advance and store-credit liabilities (2640,
//     2650), beside 73.2.19's.
//   - The reconciliation run (§11.1): every report with control totals, plus
//     the custody, stock-pool and print-evidence checks, run once a day by
//     the day-limits job; each result is kept, and a mismatch is an exception
//     in the task inbox (R-TX-16's Global Transaction Exception Worklist,
//     with tax failures, open balances and stale provider jobs).

func init() {
	RegisterReport(ReportDefinition{ID: "advance-outstanding-ageing", Label: "Customer advances outstanding (ageing)", Category: "Sales",
		Columns: []ReportColumn{{Key: "number", Label: "Receipt voucher"}, {Key: "customer_name", Label: "Customer"}, {Key: "location", Label: "Store"},
			{Key: "taken_at", Label: "Taken"}, {Key: "expected_date", Label: "Expected"}, {Key: "balance", Label: "Balance"}, {Key: "age_days", Label: "Age (days)"},
			{Key: "bucket", Label: "Age"}},
		Run: advanceOutstandingAgeing})
	RegisterReport(ReportDefinition{ID: "transfer-shortage-by-reason", Label: "Transfer shortages by reason", Category: "Inventory",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "reason", Label: "Reason"}, {Key: "transfers", Label: "Transfers"}, {Key: "damaged", Label: "Damaged"}, {Key: "lost", Label: "Lost"},
			{Key: "returned", Label: "Returned to source"}, {Key: "excess", Label: "Excess"}},
		Run: transferShortageByReason, StoreKey: "-"})
	RegisterReport(ReportDefinition{ID: "print-reprint-audit", Label: "Print and reprint audit", Category: "Admin",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "document", Label: "Document"}, {Key: "doctype", Label: "Record"}, {Key: "prints", Label: "Prints"}, {Key: "reprints", Label: "Reprints"},
			{Key: "last_reason", Label: "Last reprint reason"}, {Key: "last_by", Label: "Last by"}, {Key: "approved", Label: "Approved reprints"}, {Key: "anomaly", Label: "Anomaly"}},
		Run: printReprintAudit, StoreKey: "-"})
	RegisterReportControlTotals("advance-outstanding-ageing", func(tenantID string, _ map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
		return liabilityControlTotals(tenantID, rows, "balance", AccountCustomerAdvLia, "2640 Customer Advance Liability", "Unused balance of every open advance (incl. its GST)")
	})
	RegisterReportControlTotals("store-credit-liability", func(tenantID string, _ map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
		return liabilityControlTotals(tenantID, rows, "balance", AccountStoreCreditLia, "2650 Store Credit Liability", "Unused store credit of every customer")
	})
}

// liabilityControlTotals ties a liability register to its GL account.
func liabilityControlTotals(tenantID string, rows []map[string]interface{}, key, account, accountLabel, basis string) (*ReportControlTotals, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, cut, err := controlSnapshot(schema)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	detail := sumRowField(rows, key)
	debit, err := glDebitBalanceAt(tx, schema, []string{account}, cut)
	if err != nil {
		return nil, err
	}
	ledger := -debit // a liability's balance is credit less debit
	c := &ReportControlTotals{CutOff: cutOffLabel(cut), Basis: basis, DetailTotal: detail, SummaryTotal: detail, LedgerTotal: &ledger, LedgerAccounts: []string{accountLabel}}
	return c.finish(), nil
}

// --- registers ------------------------------------------------------------------

func advanceOutstandingAgeing(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	list, err := ListCustomerAdvances(tenantID, "", "", true)
	if err != nil {
		return nil, err
	}
	var out []map[string]interface{}
	for _, a := range list {
		if a.Balance <= 0 {
			continue
		}
		age := 0
		if t, err := time.Parse(time.RFC3339, a.TakenAt); err == nil {
			age = int(time.Since(t).Hours() / 24)
		} else if t, err := time.ParseInLocation("2006-01-02", firstNonEmpty(a.TakenAt, ""), time.Local); err == nil {
			age = int(time.Since(t).Hours() / 24)
		}
		bucket := "0-30 days"
		switch {
		case age > 180:
			bucket = "over 180 days"
		case age > 90:
			bucket = "91-180 days"
		case age > 30:
			bucket = "31-90 days"
		}
		out = append(out, map[string]interface{}{"number": a.Number, "customer_name": a.CustomerName, "location": a.Location, "taken_at": a.TakenAt,
			"expected_date": a.ExpectedDate, "balance": a.Balance, "age_days": age, "bucket": bucket})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["age_days"].(int) > out[j]["age_days"].(int) })
	return out, nil
}

func transferShortageByReason(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	docs, err := docsBetween(tenantID, "TransferOrder", p)
	if err != nil {
		return nil, err
	}
	type agg struct {
		transfers                       map[string]bool
		damaged, lost, returned, excess int
	}
	by := map[string]*agg{}
	for _, d := range docs {
		for _, e := range loadTransferEvents(d.data) {
			for _, l := range e.Lines {
				if l.Damaged+l.Lost+l.Returned+l.Excess == 0 {
					continue
				}
				reason := firstNonEmpty(l.ReasonCode, l.Reason, "(no reason)")
				a := by[reason]
				if a == nil {
					a = &agg{transfers: map[string]bool{}}
					by[reason] = a
				}
				a.transfers[d.id] = true
				a.damaged += l.Damaged
				a.lost += l.Lost
				a.returned += l.Returned
				a.excess += l.Excess
			}
		}
	}
	var out []map[string]interface{}
	for reason, a := range by {
		out = append(out, map[string]interface{}{"reason": reason, "transfers": len(a.transfers), "damaged": a.damaged, "lost": a.lost, "returned": a.returned, "excess": a.excess})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["lost"].(int)+out[i]["damaged"].(int) > out[j]["lost"].(int)+out[j]["damaged"].(int)
	})
	return out, nil
}

// printReprintAudit is R-TX-16's print half: per document, its prints and
// reprints; three or more reprints, or a reprint with no reason, is flagged.
func printReprintAudit(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(doc_number, doc_id, ''), doctype, COUNT(*) FILTER (WHERE NOT is_reprint AND print_action IN ('Print', 'Reprint')),
		       COUNT(*) FILTER (WHERE is_reprint OR print_action = 'Reprint'),
		       COALESCE((ARRAY_AGG(reprint_reason ORDER BY printed_at DESC) FILTER (WHERE COALESCE(reprint_reason, '') <> ''))[1], ''),
		       COALESCE((ARRAY_AGG(printed_by ORDER BY printed_at DESC) FILTER (WHERE is_reprint OR print_action = 'Reprint'))[1], ''),
		       COUNT(*) FILTER (WHERE COALESCE(approved_by, '') <> ''),
		       COUNT(*) FILTER (WHERE (is_reprint OR print_action = 'Reprint') AND COALESCE(reprint_reason, '') = '')
		FROM %s.print_job_log WHERE doctype IS NOT NULL AND ($1 = '' OR printed_at >= $1::date) AND ($2 = '' OR printed_at < $2::date + 1)
		GROUP BY 1, 2 HAVING COUNT(*) FILTER (WHERE is_reprint OR print_action = 'Reprint') > 0
		ORDER BY 4 DESC LIMIT 2000`, schema), strings.TrimSpace(p["from"]), strings.TrimSpace(p["to"]))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var doc, doctype, reason, by string
		var prints, reprints, approved, noReason int
		if err := rows.Scan(&doc, &doctype, &prints, &reprints, &reason, &by, &approved, &noReason); err != nil {
			return nil, err
		}
		var flags []string
		if reprints >= 3 {
			flags = append(flags, fmt.Sprintf("printed again %d times", reprints))
		}
		if noReason > 0 {
			flags = append(flags, fmt.Sprintf("%d reprint(s) with no reason", noReason))
		}
		out = append(out, map[string]interface{}{"document": doc, "doctype": doctype, "prints": prints, "reprints": reprints, "last_reason": reason,
			"last_by": by, "approved": approved, "anomaly": strings.Join(flags, "; ")})
	}
	return out, rows.Err()
}

// --- the reconciliation run (§11.1) ------------------------------------------------

// ReconciliationResult is one check's latest outcome.
type ReconciliationResult struct {
	CheckID    string    `json:"check_id"`
	Label      string    `json:"label"`
	Tied       bool      `json:"tied"`
	Difference float64   `json:"difference"`
	Detail     string    `json:"detail"`
	RanAt      time.Time `json:"ran_at"`
}

// reconciliationChecks are the checks beyond the report control totals.
var reconciliationChecks = []struct {
	id, label string
	run       func(schema string) (bool, float64, string, error)
}{
	{"custody-with-party", "Goods with parties = open challan balances", checkCustody},
	{"stock-pools", "Every store's on-hand covers its stock pools", checkStockPools},
	{"print-evidence", "Every reprint has its issued copy", checkPrintEvidence},
}

// RunReconciliationChecks runs every check once and keeps the results; it
// runs at most once a day unless forced.
func RunReconciliationChecks(tenantID string, force bool) ([]ReconciliationResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if !force {
		var last time.Time
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(MAX(ran_at), '2000-01-01') FROM %s.reconciliation_results`, schema)).Scan(&last)
		if time.Since(last) < 20*time.Hour {
			return LatestReconciliationResults(tenantID)
		}
	}
	now := time.Now().UTC()
	record := func(id, label string, tied bool, diff float64, detail string) {
		_, _ = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.reconciliation_results (check_id, label, tied, difference, detail, ran_at) VALUES ($1, $2, $3, $4, $5, $6)`, schema),
			id, label, tied, roundRupees(diff), detail, now)
	}
	ids := make([]string, 0, len(reportControlTotals))
	for id := range reportControlTotals {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		def, _, _, c, err := RunReportWithControlTotals(ctx, tenantID, id, RoleSuperAdmin, "system", map[string]string{})
		cancel()
		label := id
		if def != nil {
			label = def.Label
		}
		switch {
		case err != nil:
			record(id, label, false, 0, "could not run: "+err.Error())
		case c == nil:
		default:
			diff := c.DetailVsSummary
			if c.SummaryVsLedger != nil {
				diff += *c.SummaryVsLedger
			}
			record(id, label, c.Tied, diff, strings.Join(c.Notes, " "))
		}
	}
	for _, chk := range reconciliationChecks {
		tied, diff, detail, err := chk.run(schema)
		if err != nil {
			record(chk.id, chk.label, false, 0, "could not run: "+err.Error())
			continue
		}
		record(chk.id, chk.label, tied, diff, detail)
	}
	return LatestReconciliationResults(tenantID)
}

// LatestReconciliationResults is each check's most recent result.
func LatestReconciliationResults(tenantID string) ([]ReconciliationResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT ON (check_id) check_id, label, tied, difference, COALESCE(detail, ''), ran_at
		FROM %s.reconciliation_results ORDER BY check_id, ran_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReconciliationResult{}
	for rows.Next() {
		var r ReconciliationResult
		if err := rows.Scan(&r.CheckID, &r.Label, &r.Tied, &r.Difference, &r.Detail, &r.RanAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// checkCustody: what the stock rows say is with parties = what open
// returnable challans say is out.
func checkCustody(schema string) (bool, float64, string, error) {
	pool := map[string]int{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT sku, location_code, with_party FROM %s.inventory_availability WHERE COALESCE(with_party, 0) <> 0`, schema))
	if err != nil {
		return false, 0, "", err
	}
	for rows.Next() {
		var sku, loc string
		var n int
		if rows.Scan(&sku, &loc, &n) == nil {
			pool[sku+"@"+loc] = n
		}
	}
	rows.Close()
	open := map[string]int{}
	r2, err := db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'DeliveryChallan' AND deleted_at IS NULL AND status IN ('Dispatched', 'Partly Returned')`, schema))
	if err != nil {
		return false, 0, "", err
	}
	for r2.Next() {
		var raw string
		if r2.Scan(&raw) != nil {
			continue
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &data)
		if !truthy(data["returnable"]) {
			continue
		}
		bal, _ := challanState(data)
		for _, b := range bal {
			open[b.SKU+"@"+strField(data, "location")] += b.Open()
		}
	}
	r2.Close()
	var off []string
	diff := 0
	for k := range mergeKeys(pool, open) {
		if pool[k] != open[k] {
			diff += absInt(pool[k] - open[k])
			off = append(off, fmt.Sprintf("%s: stock %d, challans %d", k, pool[k], open[k]))
		}
	}
	sort.Strings(off)
	if len(off) > 5 {
		off = append(off[:5], fmt.Sprintf("and %d more", len(off)-5))
	}
	return diff == 0, float64(diff), strings.Join(off, "; "), nil
}

// checkStockPools: on_hand never less than what sits in its pools.
func checkStockPools(schema string) (bool, float64, string, error) {
	var n int
	var sample string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*), COALESCE(MIN(sku || '@' || location_code), '') FROM %s.inventory_availability
		WHERE on_hand < GREATEST(available, 0) + COALESCE(qc_hold, 0) + COALESCE(damaged, 0) + COALESCE(with_party, 0) + COALESCE(rtv, 0)
		   OR COALESCE(with_party, 0) < 0 OR COALESCE(qc_hold, 0) < 0 OR COALESCE(damaged, 0) < 0`, schema)).Scan(&n, &sample)
	if err != nil {
		return false, 0, "", err
	}
	if n == 0 {
		return true, 0, "", nil
	}
	return false, float64(n), fmt.Sprintf("%d stock row(s) where on-hand is less than its pools, e.g. %s", n, sample), nil
}

// checkPrintEvidence: a reprint of an issued document has the copy it
// reprinted (74.1.4).
func checkPrintEvidence(schema string) (bool, float64, string, error) {
	var n int
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.print_job_log l WHERE l.is_reprint AND l.doc_revision IS NOT NULL
		AND l.printed_at > NOW() - INTERVAL '90 days'
		AND NOT EXISTS (SELECT 1 FROM %s.print_snapshots s WHERE s.txn = l.txn AND s.doc_id = l.doc_id AND s.revision = l.doc_revision)`, schema, schema)).Scan(&n)
	if err != nil {
		return false, 0, "", err
	}
	if n == 0 {
		return true, 0, "", nil
	}
	return false, float64(n), fmt.Sprintf("%d reprint(s) in 90 days with no issued copy kept", n), nil
}

func mergeKeys(a, b map[string]int) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// --- the exception worklist (R-TX-16) ------------------------------------------------

// TransactionExceptionItems are the inbox's transaction exceptions: tax
// failures, open balances past their date, stale provider jobs and
// reconciliation mismatches.
func TransactionExceptionItems(tenantID string) ([]TaskInboxItem, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var items []TaskInboxItem
	count := func(q string, args ...interface{}) (int, time.Time) {
		var n int
		var oldest time.Time
		_ = db.DB.QueryRow(fmt.Sprintf(q, schema), args...).Scan(&n, &oldest)
		return n, oldest
	}
	add := func(n int, oldest time.Time, title, reason, view string, overdue bool) {
		if n > 0 {
			items = append(items, TaskInboxItem{Source: "Exceptions", Title: fmt.Sprintf("%s: %d", title, n), Reason: reason, View: view, Count: n, WaitingSince: oldest, Overdue: overdue})
		}
	}
	now := time.Now().UTC()
	n, t := count(`SELECT COUNT(*), COALESCE(MIN(updated_at), NOW()) FROM %s.irp_documents WHERE state IN ('REJECTED', 'UNKNOWN', 'RECONCILE', 'CANCEL_UNKNOWN', 'MANUAL_REVIEW')`)
	add(n, t, "E-invoices needing attention", "Rejected, unknown or waiting for review at the IRP", "einvoice", true)
	n, t = count(`SELECT COUNT(*), COALESCE(MIN(updated_at), NOW()) FROM %s.irp_documents WHERE state IN ('QUEUED', 'SUBMITTING') AND updated_at < $1`, now.Add(-time.Hour))
	add(n, t, "E-invoices stuck in the queue", "Queued or submitting for over an hour - check the provider", "einvoice", true)
	n, t = count(`SELECT COUNT(*), COALESCE(MIN(updated_at), NOW()) FROM %s.ewb_documents WHERE status = 'Failed' OR (status = 'Submitted' AND updated_at < $1)`, now.Add(-30*time.Minute))
	add(n, t, "E-way bills failed or unconfirmed", "Refused, or sent with no answer - see the reason on each", "einvoice:ewb", true)
	n, t = count(`SELECT COUNT(*), COALESCE(MIN(valid_upto), NOW()) FROM %s.ewb_documents WHERE status IN ('Generated', 'Reconciled') AND valid_upto BETWEEN $1 AND $2`, now, now.Add(ewbExtendWindowHours*time.Hour))
	add(n, t, "E-way bills expiring", "Within 8 hours - extend them if the goods are still moving", "einvoice:ewb", false)
	n, t = count(`SELECT COUNT(*), COALESCE(MIN(updated_at), NOW()) FROM %s.ewb_documents WHERE status = 'Ready' AND created_at < $1`, now.Add(-24*time.Hour))
	add(n, t, "E-way bills not made", "Needed for over a day and still not generated or recorded", "einvoice:ewb", true)
	n, t = count(`SELECT COUNT(*), COALESCE(MIN(updated_at), NOW()) FROM %s.documents d WHERE d.doctype = 'DeliveryChallan' AND d.deleted_at IS NULL AND d.status IN ('Dispatched', 'Partly Returned')
		AND EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(d.data->'items') = 'string' THEN (d.data->>'items')::jsonb ELSE COALESCE(d.data->'items', '[]'::jsonb) END) x
		            WHERE COALESCE(x->>'expected_return', '') <> '' AND x->>'expected_return' < $1)`, now.Format("2006-01-02"))
	add(n, t, "Challan goods overdue", "Returnable goods past their expected date - follow up or write off", "inventory:challans", true)
	n, t = count(`SELECT COUNT(*), COALESCE(MIN(updated_at), NOW()) FROM %s.documents WHERE doctype = 'SubcontractOrder' AND deleted_at IS NULL AND status IN ('Sent', 'Partly Received')
		AND COALESCE(data->>'due_date', '') <> '' AND data->>'due_date' < $1`, now.Format("2006-01-02"))
	add(n, t, "Job work overdue", "Past the due date with goods still out", "manufacturing:subcontracting", true)
	if results, err := LatestReconciliationResults(tenantID); err == nil {
		var off []string
		var at time.Time
		for _, r := range results {
			if !r.Tied {
				off = append(off, r.Label)
				at = r.RanAt
			}
		}
		if len(off) > 0 {
			items = append(items, TaskInboxItem{Source: "Exceptions", Title: fmt.Sprintf("Reconciliation checks not tied: %d", len(off)), Reason: strings.Join(off, "; "),
				View: "reports", Count: len(off), WaitingSince: at, Overdue: true})
		}
	}
	return items, nil
}
