package engines

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 148.25: lock a month after GST filing, and the On Hold period status.

type stage14825Fixture struct {
	tenantID, schema string
	insert           func(id, doctype, status, createdBy string, data map[string]interface{})
	month            string // a past month, YYYY-MM
	day              string // a day in it
}

func stage14825Setup(t *testing.T) stage14825Fixture {
	t.Helper()
	tenantID, schema, insert := stage140Tenant(t)
	insert("S14825", "Location", "Active", "system", map[string]interface{}{"code": "S14825", "name": "Store", "type": "Store", "sellable": "Yes"})
	insert("TEE14825", "Item", "Active", "system", map[string]interface{}{"code": "TEE14825", "name": "Tee", "category": "Apparel", "sale_price": 100.0, "standard_cost": 40.0})
	if _, err := PostInventoryLedger(tenantID, "S14825", []interface{}{map[string]interface{}{"sku": "TEE14825", "qty": 20.0}}, false); err != nil {
		t.Fatal(err)
	}
	first := time.Now().UTC().AddDate(0, -2, 0)
	day := time.Date(first.Year(), first.Month(), 10, 0, 0, 0, 0, time.UTC)
	return stage14825Fixture{tenantID, schema, insert, day.Format("2006-01"), day.Format("2006-01-02")}
}

func (f stage14825Fixture) invoice(t *testing.T, id, date string) {
	t.Helper()
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "TEE14825", "qty": 1, "rate": 100.0, "amount": 100.0}})
	f.insert(id, "SalesInvoice", "Draft", "manager1", map[string]interface{}{"customer": "C14825", "location": "S14825", "items": string(items), "total_amount": 100.0, "invoice_date": date})
}

// stage14825Code is the code of a ValidationError, or the code a posting
// wrapper's message carries: PostSalesInvoice flattens the cause into text.
func stage14825Code(err error) string {
	if ve, ok := err.(*ValidationError); ok {
		return ve.Code
	}
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "its GSTR-1 is filed"):
			return gstr1LockedCode
		case strings.Contains(err.Error(), "is On Hold for sales"):
			return periodHoldCode
		}
	}
	return ""
}

// Setting off: marking a month changes nothing. Setting on: the filed month's
// invoice cannot post, another month's can, unmarking frees it.
func TestStage14825FiledMonthLocksSalesPosting(t *testing.T) {
	f := stage14825Setup(t)
	f.invoice(t, "SI-14825-A", f.day)
	if err := MarkGSTR1Filed(f.tenantID, "accountant1", f.month); err != nil {
		t.Fatalf("mark: %v", err)
	}
	// Off by default: the mark alone locks nothing.
	if _, err := PostSalesInvoice(f.tenantID, "SI-14825-A", "manager1"); err != nil {
		t.Fatalf("setting off must post as today: %v", err)
	}

	if err := SetSetting(f.tenantID, SettingLockAfterGSTR1, "true", "admin"); err != nil {
		t.Fatal(err)
	}
	f.invoice(t, "SI-14825-B", f.day)
	_, err := PostSalesInvoice(f.tenantID, "SI-14825-B", "manager1")
	if stage14825Code(err) != gstr1LockedCode {
		t.Fatalf("filed month must refuse with %s, got %v", gstr1LockedCode, err)
	}
	if d, c := stage140GL(t, f.schema, "SalesInvoice", "SI-14825-B", "4100"); d+c != 0 {
		t.Fatalf("a refused invoice must leave no GL row")
	}
	// The month's already-posted invoice cannot be cancelled either.
	if err := CancelSalesInvoice(f.tenantID, "SI-14825-A", "manager1", "wrong"); stage14825Code(err) != gstr1LockedCode {
		t.Fatalf("cancel in a filed month must refuse with %s, got %v", gstr1LockedCode, err)
	}
	// A different month is untouched.
	other := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01") + "-10"
	f.invoice(t, "SI-14825-C", other)
	if _, err := PostSalesInvoice(f.tenantID, "SI-14825-C", "manager1"); err != nil {
		t.Fatalf("an unfiled month posts: %v", err)
	}

	if err := UnmarkGSTR1Filed(f.tenantID, "admin", f.month, ""); err == nil {
		t.Fatal("unmark without a reason must refuse")
	}
	if err := UnmarkGSTR1Filed(f.tenantID, "admin", f.month, "filed in error"); err != nil {
		t.Fatalf("unmark: %v", err)
	}
	if _, err := PostSalesInvoice(f.tenantID, "SI-14825-B", "manager1"); err != nil {
		t.Fatalf("after unmark the month posts: %v", err)
	}
	var n int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + f.schema + ".audit_logs WHERE action IN ('GSTR1_MARKED_FILED','GSTR1_UNMARKED')").Scan(&n)
	if n < 2 {
		t.Fatalf("mark and unmark must both be audited, found %d", n)
	}
}

// A receipt against a posted invoice, and a credit note, still work in a filed
// month: only the sale's own postings are locked.
func TestStage14825FiledMonthStillTakesReceiptsAndCreditNotes(t *testing.T) {
	f := stage14825Setup(t)
	f.invoice(t, "SI-14825-D", f.day)
	if _, err := PostSalesInvoice(f.tenantID, "SI-14825-D", "manager1"); err != nil {
		t.Fatal(err)
	}
	if err := MarkGSTR1Filed(f.tenantID, "accountant1", f.month); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(f.tenantID, SettingLockAfterGSTR1, "true", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := SettleSalesInvoice(f.tenantID, "SI-14825-D", "manager1"); err != nil {
		t.Fatalf("a receipt in a filed month must work: %v", err)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, kind := range []string{"CreditNote", "SalesReturn"} {
		if checkSalesMonthLockedTx(tx, f.schema, kind, "X-14825", "X-14825:POST", f.day) != nil {
			t.Fatalf("%s must not be locked by a filed month", kind)
		}
	}
}

// Mark rules: only a finished month; twice is refused; a reason is kept.
func TestStage14825MarkRules(t *testing.T) {
	f := stage14825Setup(t)
	if err := MarkGSTR1Filed(f.tenantID, "a", BusinessToday()[:7]); stage14825Code(err) != gstr1MarkCode {
		t.Fatalf("this month cannot be marked: %v", err)
	}
	if err := MarkGSTR1Filed(f.tenantID, "a", f.month); err != nil {
		t.Fatal(err)
	}
	if err := MarkGSTR1Filed(f.tenantID, "a", f.month); stage14825Code(err) != gstr1MarkCode {
		t.Fatalf("marking twice must refuse: %v", err)
	}
	if err := UnmarkGSTR1Filed(f.tenantID, "admin", f.month, "wrong month"); err != nil {
		t.Fatal(err)
	}
	rows, err := ListGSTR1FiledMonths(f.tenantID)
	if err != nil || len(rows) != 1 || rows[0].Locked || rows[0].UnmarkReason != "wrong month" || rows[0].UnmarkedBy != "admin" {
		t.Fatalf("list after unmark: %+v %v", rows, err)
	}
	if err := MarkGSTR1Filed(f.tenantID, "a", f.month); err != nil {
		t.Fatalf("re-mark after unmark: %v", err)
	}
}

// On Hold: no setting needed; sales postings in the period refuse, release
// puts it back, a hold can be closed.
func TestStage14825PeriodOnHold(t *testing.T) {
	f := stage14825Setup(t)
	var id string
	if err := db.DB.QueryRow("INSERT INTO "+f.schema+".accounting_periods (period_name, start_date, end_date, status, created_by) VALUES ($1, $2::date, ($2::date + INTERVAL '1 month - 1 day')::date, 'Open', 'admin') RETURNING id", "P-14825", f.month+"-01").Scan(&id); err != nil {
		t.Fatalf("period: %v", err)
	}
	f.invoice(t, "SI-14825-E", f.day)
	if err := SetAccountingPeriodHold(f.tenantID, "accountant1", id, true); err != nil {
		t.Fatalf("hold: %v", err)
	}
	if _, err := PostSalesInvoice(f.tenantID, "SI-14825-E", "manager1"); stage14825Code(err) != periodHoldCode || !strings.Contains(err.Error(), "P-14825") {
		t.Fatalf("On Hold must refuse with %s naming the period, got %v", periodHoldCode, err)
	}
	if err := SetAccountingPeriodHold(f.tenantID, "accountant1", id, true); err == nil {
		t.Fatal("holding a held period must refuse")
	}
	if err := SetAccountingPeriodHold(f.tenantID, "accountant1", id, false); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := PostSalesInvoice(f.tenantID, "SI-14825-E", "manager1"); err != nil {
		t.Fatalf("after release it posts: %v", err)
	}
}
