package server

// Stage 49.5.6d (user, 2026-10-29: "all recommended"): a validated offline
// sale posts on the till's sale day - or today when that day is in a closed
// accounting period - and the cash a held sale took is its own line in the
// drawer's expected cash until it is validated.

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// notAtTillTime counts the sale's GL rows, stock ledger lines and bill that
// are not at the till's sale time - and how many there are in all.
func (o *offlineSaleFixture) notAtTillTime(cart string) (off, all int) {
	for _, q := range []string{
		`SELECT COUNT(*) FILTER (WHERE created_at <> $2::timestamptz), COUNT(*) FROM %s.gl_postings WHERE document_type = 'POSCart' AND document_id = $1`,
		`SELECT COUNT(*) FILTER (WHERE created_at <> $2::timestamptz), COUNT(*) FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1`,
		`SELECT COUNT(*) FILTER (WHERE created_at <> $2::timestamptz), COUNT(*) FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`,
	} {
		var a, b int
		if err := db.DB.QueryRow(fmt.Sprintf(q, o.schema), cart, o.soldAt).Scan(&a, &b); err != nil {
			o.t.Fatalf("dates of %s: %v", cart, err)
		}
		off, all = off+a, all+b
	}
	return
}

func (o *offlineSaleFixture) validatedAudit(id string) string {
	var details string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT details FROM %s.audit_logs WHERE action = 'POS_OFFLINE_SALE_VALIDATED' AND details LIKE $1`, o.schema), "%"+id+"%").Scan(&details)
	return details
}

func TestStage4956dValidatedSalePostsOnTheTillsDay(t *testing.T) {
	o := newOfflineSaleFixture(t)
	sold := time.Now().AddDate(0, 0, -3).UTC().Truncate(time.Second)
	o.soldAt = sold.Format(time.RFC3339)
	cart := "OFS-DAY-" + o.suffix
	id := o.hold(cart, o.barcode, 1)
	if status, resp := o.decide("validate", id, o.superviserTok, ""); status != http.StatusOK || resp["status"] != "Validated" {
		t.Fatalf("validate = %d %v, want 200 Validated", status, resp)
	}
	off, all := o.notAtTillTime(cart)
	if all < 3 || off != 0 {
		t.Fatalf("%d of %d GL / stock ledger / bill rows are not at the till's sale time %s - want all of them there", off, all, o.soldAt)
	}
	tillDay := sold.In(engines.BusinessLocation()).Format("2006-01-02")
	if d := o.validatedAudit(id); !strings.Contains(d, " on "+tillDay) {
		t.Fatalf("validation audit %q, want it to say the sale posted on %s", d, tillDay)
	}
}

func TestStage4956dClosedTillDayPostsToday(t *testing.T) {
	o := newOfflineSaleFixture(t)
	sold := time.Now().AddDate(-1, 0, -40).UTC().Truncate(time.Second)
	o.soldAt = sold.Format(time.RFC3339)
	tillDay := sold.In(engines.BusinessLocation()).Format("2006-01-02")
	var periodID string
	if err := db.DB.QueryRow(fmt.Sprintf(`INSERT INTO %s.accounting_periods (period_name, start_date, end_date, status, created_by)
		VALUES ($1, $2::date, $2::date, 'Closed', 'test') RETURNING id`, o.schema), "T4956D-"+o.suffix, tillDay).Scan(&periodID); err != nil {
		t.Fatalf("close the till's day: %v", err)
	}
	t.Cleanup(func() { db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.accounting_periods WHERE id = $1`, o.schema), periodID) })

	cart := "OFS-CLOSED-" + o.suffix
	id := o.hold(cart, o.barcode, 1)
	if status, resp := o.decide("validate", id, o.superviserTok, ""); status != http.StatusOK || resp["status"] != "Validated" {
		t.Fatalf("validate = %d %v, want 200 Validated (posted today)", status, resp)
	}
	off, all := o.notAtTillTime(cart)
	if all < 3 || off != all {
		t.Fatalf("%d of %d rows moved to the till's day %s, which is in a closed period - want none", all-off, all, tillDay)
	}
	if d := o.validatedAudit(id); !strings.Contains(d, " on "+engines.BusinessToday()) || !strings.Contains(d, tillDay+", in a closed period") {
		t.Fatalf("validation audit %q, want today's date and the closed till day", d)
	}
}

func TestStage4956dHeldOfflineCashIsItsOwnLine(t *testing.T) {
	o := newOfflineSaleFixture(t)
	cart := "OFS-CASH-" + o.suffix
	id := o.hold(cart, o.barcode, 2)
	var session string
	var total float64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT pos_session, total FROM %s.pos_offline_sales WHERE id = $1`, o.schema), id).Scan(&session, &total)
	if total <= 0 {
		t.Fatalf("held sale total %v, want the server's price for it", total)
	}

	held, err := engines.SessionCashPosition("default", session)
	if err != nil {
		t.Fatalf("X-report: %v", err)
	}
	if held.HeldOffline != total {
		t.Fatalf("X-report held offline cash %v, want the held cash sale's %v", held.HeldOffline, total)
	}

	if status, resp := o.decide("validate", id, o.superviserTok, ""); status != http.StatusOK {
		t.Fatalf("validate = %d %v", status, resp)
	}
	after, err := engines.SessionCashPosition("default", session)
	if err != nil {
		t.Fatalf("X-report after validation: %v", err)
	}
	// Validated, the cash moves from the held line to cash sales; the drawer
	// should hold the same.
	if after.HeldOffline != 0 || math.Abs(after.Expected-held.Expected) > 0.01 || after.CashSales-held.CashSales < total-0.01 {
		t.Fatalf("after validation held=%v expected=%v cash sales %v->%v, want held 0, expected still %v, cash sales up by %v",
			after.HeldOffline, after.Expected, held.CashSales, after.CashSales, held.Expected, total)
	}
}

func TestStage4956dTillCloseCountsHeldOfflineCash(t *testing.T) {
	o := newOfflineSaleFixture(t)
	id := o.hold("OFS-CLOSE-"+o.suffix, o.barcode, 1)
	var session, cashier string
	var total float64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT pos_session, cashier, total FROM %s.pos_offline_sales WHERE id = $1`, o.schema), id).Scan(&session, &cashier, &total)
	x, err := engines.SessionCashPosition("default", session)
	if err != nil {
		t.Fatalf("X-report: %v", err)
	}
	res, err := engines.ClosePOSSession("default", session, cashier, x.Expected, "")
	if err != nil {
		t.Fatalf("close counting the X-report's expected %v: %v", x.Expected, err)
	}
	if res.HeldOffline != total || res.Expected != x.Expected || res.Variance != 0 {
		t.Fatalf("close held=%v expected=%v variance=%v, want held %v and the X-report's %v with no variance", res.HeldOffline, res.Expected, res.Variance, total, x.Expected)
	}
}
