package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
)

// Stage 170.5: leases (IND AS 116).
//
// Lessee: activating a Lease books the right-of-use asset and the lease
// liability at the present value of the payments (Dr ROU / Cr liability) and
// stores the whole schedule on the lease. Each month that has ended the worker
// posts three keyed journals for that period, together in one transaction:
//
//	:INT  Dr interest expense / Cr lease liability
//	:PAY  Dr lease liability  / Cr payable (2100, or the lease's payment account)
//	:DEP  Dr ROU depreciation / Cr 1421 accumulated ROU depreciation (straight line)
//
// When the last period posts, :CLOSE clears what built up in 1421 against the
// right-of-use asset (Dr 1421 / Cr 1420), so both end at nought for the lease.
// A tenant without 1421 (or a lease set to its ROU account) credits 1420
// directly, as leases did before ask 176, and needs no closing entry.
//
// Lessor (operating lease): the leased-out asset stays on the books and keeps
// depreciating through the asset worker; each ended month posts rent
// (Dr 1300 receivable / Cr 4310 lease rental income).
//
// A period is posted at most once: its row in the schedule carries a posted
// flag read under the lease's row lock, and every journal has its own
// idempotency key. A month in a closed period is left for the next run.

const (
	leaseAccROU          = "1420"
	leaseAccROUAccum     = "1421"
	leaseAccLiability    = "2450"
	leaseAccRentalIncome = "4310"
	leaseAccROUDep       = "5311"
	leaseAccInterest     = "5312"
	leaseAccPayable      = "2100"
	leaseAccReceivable   = "1300"

	leaseInputCode    = "ASSET-1720" // a lease field is missing or unusable
	leaseStateCode    = "ASSET-1721" // the lease is not in a state for this step
	leaseAccountCode  = "ASSET-1722" // an account named on the lease does not exist
	leaseConflictCode = "ASSET-1729" // the asset is already leased out
)

func leaseRefused(code, sub, format string, args ...interface{}) error {
	return &ValidationError{Code: code, SubFor: sub, Message: fmt.Sprintf(format, args...)}
}

// LeasePeriod is one row of the stored schedule. Amounts are paise.
type LeasePeriod struct {
	N            int    `json:"n"`
	Month        string `json:"month"` // YYYY-MM
	Opening      int64  `json:"opening"`
	Interest     int64  `json:"interest"`
	Payment      int64  `json:"payment"`
	Closing      int64  `json:"closing"`
	Depreciation int64  `json:"depreciation"`
	Posted       bool   `json:"posted"`
}

// BuildLeaseSchedule works out the lessee's liability and schedule. rate is a
// percentage a year, taken monthly as rate/12. Payments are equal; the last
// period closes the liability to nought.
func BuildLeaseSchedule(termMonths int, paymentPaise int64, annualRatePct float64, advance bool, startMonth time.Time) (pv int64, rows []LeasePeriod) {
	r := annualRatePct / 12 / 100
	pvf := 0.0
	for k := 0; k < termMonths; k++ {
		exp := k + 1
		if advance {
			exp = k
		}
		pvf += float64(paymentPaise) / math.Pow(1+r, float64(exp))
	}
	pv = int64(math.Round(pvf))
	rouPer := pv / int64(termMonths)
	opening := pv
	for k := 1; k <= termMonths; k++ {
		row := LeasePeriod{N: k, Month: startMonth.AddDate(0, k-1, 0).Format("2006-01"), Opening: opening, Payment: paymentPaise, Depreciation: rouPer}
		if k == termMonths {
			row.Depreciation = pv - rouPer*int64(termMonths-1)
			row.Interest = row.Payment - opening
			if row.Interest < 0 {
				row.Interest = 0
				row.Payment = opening
			}
			row.Closing = 0
		} else {
			base := opening
			if advance {
				base = opening - paymentPaise
			}
			row.Interest = int64(math.Round(float64(base) * r))
			row.Closing = opening + row.Interest - paymentPaise
		}
		rows = append(rows, row)
		opening = row.Closing
	}
	return pv, rows
}

func leaseAccountOrDefault(data map[string]interface{}, key, def string) string {
	if s := strings.TrimSpace(strField(data, key)); s != "" {
		return s
	}
	return def
}

func leaseAccountExists(q interface {
	QueryRow(string, ...interface{}) *sql.Row
}, schema, code string) bool {
	var one int
	return q.QueryRow(fmt.Sprintf(`SELECT 1 FROM %s.gl_accounts WHERE account_code = $1`, schema), code).Scan(&one) == nil
}

func leaseStartMonth(start string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", start)
	if err != nil {
		return time.Time{}, leaseRefused(leaseInputCode, "Start date", "Use a start date like 2026-11-04.")
	}
	return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC), nil
}

// ActivateLease validates a Draft lease, books the opening entry (lessee) and
// stores the schedule. A second call on an Active lease is refused.
func ActivateLease(tenantID, leaseID, userID string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	data, status, err := lockDocTx(tx, schema, "Lease", leaseID)
	if err != nil {
		return nil, leaseRefused(leaseStateCode, "Lease", "That lease is not on file.")
	}
	if status != "Draft" && status != "" {
		return nil, leaseRefused(leaseStateCode, "Lease", "Only a Draft lease can be activated (it is %s).", status)
	}
	leaseType := strField(data, "lease_type")
	if leaseType != "Lessee" && leaseType != "Lessor" {
		return nil, leaseRefused(leaseInputCode, "Type", "Choose Lessee or Lessor.")
	}
	term := int(numFromInterface(data["term_months"]))
	if term < 1 || term > 600 {
		return nil, leaseRefused(leaseInputCode, "Term (months)", "Enter a term of 1 to 600 months.")
	}
	payment := RupeesToPaise(numFromInterface(data["payment_amount"]))
	if payment <= 0 {
		return nil, leaseRefused(leaseInputCode, "Payment per month", "Enter a payment above nought.")
	}
	rate := numFromInterface(data["discount_rate"])
	if rate < 0 || rate > 100 {
		return nil, leaseRefused(leaseInputCode, "Discount rate", "Enter a rate from 0 to 100.")
	}
	timing := strField(data, "payment_timing")
	if timing == "" {
		timing = "Arrears"
	}
	if timing != "Arrears" && timing != "Advance" {
		return nil, leaseRefused(leaseInputCode, "Paid", "Choose Arrears or Advance.")
	}
	startMonth, err := leaseStartMonth(strField(data, "start_date"))
	if err != nil {
		return nil, err
	}

	if leaseType == "Lessor" {
		if strings.TrimSpace(strField(data, "customer")) == "" {
			return nil, leaseRefused(leaseInputCode, "Customer", "Choose the customer.")
		}
		assetID := strings.TrimSpace(strField(data, "asset"))
		if assetID == "" {
			return nil, leaseRefused(leaseInputCode, "Asset leased out", "Choose the asset.")
		}
		_, aStatus, aerr := fetchAssetData(tenantID, assetID)
		if aerr != nil || aStatus != "Capitalised" {
			return nil, leaseRefused(leaseStateCode, "Asset leased out", "Only a Capitalised asset can be leased out.")
		}
		var other string
		if err := tx.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Lease' AND deleted_at IS NULL
			AND id <> $1 AND status = 'Active' AND data->>'lease_type' = 'Lessor' AND data->>'asset' = $2 LIMIT 1`, schema),
			leaseID, assetID).Scan(&other); err == nil {
			return nil, leaseRefused(leaseConflictCode, "Asset leased out", "That asset is already leased out on %s.", other)
		}
		rows := make([]LeasePeriod, term)
		for k := 1; k <= term; k++ {
			rows[k-1] = LeasePeriod{N: k, Month: startMonth.AddDate(0, k-1, 0).Format("2006-01"), Payment: payment}
		}
		data["schedule"] = leaseRowsToJSON(rows)
		data["periods_posted"] = float64(0)
		data["payment_timing"] = timing
		if err := writeDocTx(tx, schema, "Lease", leaseID, data, "Active"); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		LogAuditEvent(tenantID, userID, "LEASE_ACTIVATED", "SUCCESS", fmt.Sprintf("Lease %s (lessor) activated for asset %s", leaseID, assetID))
		return data, nil
	}

	if strings.TrimSpace(strField(data, "vendor")) == "" {
		return nil, leaseRefused(leaseInputCode, "Vendor", "Choose the vendor.")
	}
	rouAcc := leaseAccountOrDefault(data, "rou_account", leaseAccROU)
	liabAcc := leaseAccountOrDefault(data, "liability_account", leaseAccLiability)
	intAcc := leaseAccountOrDefault(data, "interest_account", leaseAccInterest)
	depAcc := leaseAccountOrDefault(data, "depreciation_account", leaseAccROUDep)
	payAcc := leaseAccountOrDefault(data, "payment_account", leaseAccPayable)
	for _, a := range []struct{ label, code string }{{"Right-of-use account", rouAcc}, {"Lease liability account", liabAcc},
		{"Interest account", intAcc}, {"Depreciation account", depAcc}, {"Payment credit account", payAcc}} {
		if !leaseAccountExists(tx, schema, a.code) {
			return nil, leaseRefused(leaseAccountCode, a.label, "Account %s is not in the chart of accounts.", a.code)
		}
	}
	pv, rows := BuildLeaseSchedule(term, payment, rate, timing == "Advance", startMonth)
	if pv <= 0 {
		return nil, leaseRefused(leaseInputCode, "Payment per month", "The present value works out to nought.")
	}
	if err := PostDoubleEntryTx(tx, tenantID, schema, "Lease", leaseID,
		map[string]int64{rouAcc: pv}, map[string]int64{liabAcc: pv}, strField(data, "start_date"),
		"Lease:"+leaseID+":ACTIVATE"); err != nil {
		return nil, err
	}
	data["schedule"] = leaseRowsToJSON(rows)
	data["liability_opening"] = PaiseToRupees(pv)
	data["periods_posted"] = float64(0)
	data["payment_timing"] = timing
	if err := writeDocTx(tx, schema, "Lease", leaseID, data, "Active"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "LEASE_ACTIVATED", "SUCCESS", fmt.Sprintf("Lease %s (lessee) activated, liability %.2f", leaseID, PaiseToRupees(pv)))
	return data, nil
}

func leaseRowsToJSON(rows []LeasePeriod) []interface{} {
	out := make([]interface{}, len(rows))
	for i, r := range rows {
		out[i] = map[string]interface{}{"n": float64(r.N), "month": r.Month, "opening": float64(r.Opening), "interest": float64(r.Interest),
			"payment": float64(r.Payment), "closing": float64(r.Closing), "depreciation": float64(r.Depreciation), "posted": r.Posted}
	}
	return out
}

func leaseRowFromJSON(v interface{}) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	return m, ok
}

// postLeasePeriodTx posts the first unposted, ended period of one lease inside
// its own transaction. It returns whether a period was posted.
func postLeasePeriodTx(tenantID, schema, leaseID string, now time.Time) (bool, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	data, status, err := lockDocTx(tx, schema, "Lease", leaseID)
	if err != nil {
		return false, err
	}
	if status != "Active" {
		return false, nil
	}
	sched, _ := data["schedule"].([]interface{})
	idx := -1
	for i, v := range sched {
		if m, ok := leaseRowFromJSON(v); ok {
			if posted, _ := m["posted"].(bool); !posted {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		// Every period posted: close it.
		if err := closeLeaseROUTx(tx, tenantID, schema, leaseID, data, now.Format("2006-01-02")); err != nil {
			return false, err
		}
		if err := writeDocTx(tx, schema, "Lease", leaseID, data, "Closed"); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	row, _ := leaseRowFromJSON(sched[idx])
	month := strField(row, "month")
	first, err := time.Parse("2006-01", month)
	if err != nil {
		return false, fmt.Errorf("lease %s: bad month %q", leaseID, month)
	}
	monthEnd := first.AddDate(0, 1, -1)
	if !monthEnd.Before(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)) {
		return false, nil // the month has not ended
	}
	date := monthEnd.Format("2006-01-02")
	opt := PostingOptions{PostedAt: date + " 23:59:59"}
	keyBase := fmt.Sprintf("Lease:%s:PERIOD:%s", leaseID, month)
	post := func(suffix string, debit, credit string, amount int64) error {
		if amount <= 0 {
			return nil
		}
		return PostDoubleEntryTx(tx, tenantID, schema, "Lease", leaseID,
			map[string]int64{debit: amount}, map[string]int64{credit: amount}, date, keyBase+":"+suffix, opt)
	}
	if strField(data, "lease_type") == "Lessor" {
		if err := post("RENT", leaseAccReceivable, leaseAccRentalIncome, int64(numFromInterface(row["payment"]))); err != nil {
			return false, err
		}
	} else {
		liab := leaseAccountOrDefault(data, "liability_account", leaseAccLiability)
		if err := post("INT", leaseAccountOrDefault(data, "interest_account", leaseAccInterest), liab, int64(numFromInterface(row["interest"]))); err != nil {
			return false, err
		}
		if err := post("PAY", liab, leaseAccountOrDefault(data, "payment_account", leaseAccPayable), int64(numFromInterface(row["payment"]))); err != nil {
			return false, err
		}
		if err := post("DEP", leaseAccountOrDefault(data, "depreciation_account", leaseAccROUDep),
			leaseROUAccumAccount(tx, schema, data), int64(numFromInterface(row["depreciation"]))); err != nil {
			return false, err
		}
	}
	row["posted"] = true
	posted := 0
	for _, v := range sched {
		if m, ok := leaseRowFromJSON(v); ok {
			if p, _ := m["posted"].(bool); p {
				posted++
			}
		}
	}
	data["periods_posted"] = float64(posted)
	newStatus := "Active"
	if posted == len(sched) {
		newStatus = "Closed"
		if err := closeLeaseROUTx(tx, tenantID, schema, leaseID, data, date); err != nil {
			return false, err
		}
	}
	if err := writeDocTx(tx, schema, "Lease", leaseID, data, newStatus); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// leaseROUAccumAccount is the account a lessee lease's depreciation credits:
// the lease's own choice, else 1421, else (no 1421 in this chart) the
// right-of-use account itself.
func leaseROUAccumAccount(tx *sql.Tx, schema string, data map[string]interface{}) string {
	rou := leaseAccountOrDefault(data, "rou_account", leaseAccROU)
	acc := leaseAccountOrDefault(data, "rou_accumulated_account", leaseAccROUAccum)
	if acc == rou || !leaseAccountExists(tx, schema, acc) {
		return rou
	}
	return acc
}

// closeLeaseROUTx clears the accumulated ROU depreciation of a lessee lease
// against its right-of-use asset once every period has posted. It reads what
// this lease's own postings left in the accumulated account, so a lease that
// credited 1420 directly for some months closes only what went to 1421.
// Keyed per lease: a second call posts nothing.
func closeLeaseROUTx(tx *sql.Tx, tenantID, schema, leaseID string, data map[string]interface{}, date string) error {
	if strField(data, "lease_type") == "Lessor" {
		return nil
	}
	rou := leaseAccountOrDefault(data, "rou_account", leaseAccROU)
	acc := leaseROUAccumAccount(tx, schema, data)
	if acc == rou {
		return nil
	}
	var built int64
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(credit - debit), 0) FROM %s.gl_postings
		WHERE document_type = 'Lease' AND document_id = $1 AND account_code = $2`, schema), leaseID, acc).Scan(&built); err != nil {
		return err
	}
	if built <= 0 {
		return nil
	}
	return PostDoubleEntryTx(tx, tenantID, schema, "Lease", leaseID,
		map[string]int64{acc: built}, map[string]int64{rou: built}, date, "Lease:"+leaseID+":CLOSE",
		PostingOptions{PostedAt: date + " 23:59:59"})
}

// RunLeasePostings posts every ended, unposted period of every Active lease in
// one tenant schema. A closed period leaves that lease for the next run.
func RunLeasePostings(tenantID, schema string, now time.Time) (posted int, err error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Lease' AND status = 'Active' AND deleted_at IS NULL ORDER BY id`, schema))
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		for {
			ok, perr := postLeasePeriodTx(tenantID, schema, id, now)
			if perr != nil {
				if reason, closed := closedPeriodReason(perr); closed {
					log.Printf("[LEASE] %s %s: %s", schema, id, reason)
				} else {
					err = perr
					log.Printf("[LEASE] %s %s: %v", schema, id, perr)
				}
				break
			}
			if !ok {
				break
			}
			posted++
		}
	}
	return posted, err
}

// StartLeaseWorker runs RunLeasePostings for every tenant with the assets module.
func StartLeaseWorker(ctx context.Context, interval time.Duration) {
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
				schemas, err := listTenantSchemas("assets")
				if err != nil {
					log.Printf("[LEASE] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					tenantID, err := tenantIDForSchema(schema)
					if err != nil {
						continue
					}
					if n, err := RunLeasePostings(tenantID, schema, time.Now()); n > 0 || err != nil {
						log.Printf("[LEASE] %s: posted %d period(s), err=%v", schema, n, err)
					}
				}
			}
		}
	}()
}

// RunLeasePostingsForTenant is the manual "post what is due" action.
func RunLeasePostingsForTenant(tenantID string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	return RunLeasePostings(tenantID, schema, time.Now())
}
