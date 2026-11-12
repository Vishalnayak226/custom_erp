package engines

import (
	"encoding/json"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 148.22: a non-stock invoice line carries its own income account.

func stage14822Setup(t *testing.T) (tenantID, schema string, insert func(id, doctype, status, createdBy string, data map[string]interface{})) {
	t.Helper()
	tenantID, schema, insert = stage140Tenant(t)
	insert("S14822", "Location", "Active", "system", map[string]interface{}{"code": "S14822", "name": "Store", "type": "Store", "sellable": "Yes"})
	insert("TEE14822", "Item", "Active", "system", map[string]interface{}{"code": "TEE14822", "name": "Tee", "category": "Apparel", "sale_price": 100.0, "standard_cost": 40.0})
	if _, err := PostInventoryLedger(tenantID, "S14822", []interface{}{map[string]interface{}{"sku": "TEE14822", "qty": 20.0}}, false); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][3]string{{"4950", "Installation income", "Revenue"}, {"6950", "Some expense", "Expense"}} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".gl_accounts (account_code, account_name, account_type) VALUES ($1, $2, $3) ON CONFLICT (account_code) DO NOTHING", a[0], a[1], a[2]); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func stage14822Invoice(insert func(id, doctype, status, createdBy string, data map[string]interface{}), id, account string) {
	line := map[string]interface{}{"sku": "INSTALL", "item_name": "Installation", "qty": 1, "rate": 500.0, "amount": 500.0}
	if account != "" {
		line["income_account"] = account
	}
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "TEE14822", "qty": 1, "rate": 100.0, "amount": 100.0}, line})
	insert(id, "SalesInvoice", "Draft", "manager1", map[string]interface{}{"customer": "C14822", "location": "S14822", "items": string(items), "total_amount": 600.0})
}

// A blank account posts exactly as today; a named one takes the line's amount.
func TestStage14822FreeTextLinePostsToItsAccount(t *testing.T) {
	tenantID, schema, insert := stage14822Setup(t)
	stage14822Invoice(insert, "SI-14822-BLANK", "")
	if _, err := PostSalesInvoice(tenantID, "SI-14822-BLANK", "manager1"); err != nil {
		t.Fatal(err)
	}
	if _, c := stage140GL(t, schema, "SalesInvoice", "SI-14822-BLANK", "4100"); c != 60000 {
		t.Fatalf("blank account must keep all revenue on 4100, got %d", c)
	}

	stage14822Invoice(insert, "SI-14822-ACCT", "4950")
	if _, err := PostSalesInvoice(tenantID, "SI-14822-ACCT", "manager1"); err != nil {
		t.Fatal(err)
	}
	if _, c := stage140GL(t, schema, "SalesInvoice", "SI-14822-ACCT", "4950"); c != 50000 {
		t.Fatalf("the free-text line must credit 4950 with 50000 paise, got %d", c)
	}
	if _, c := stage140GL(t, schema, "SalesInvoice", "SI-14822-ACCT", "4100"); c != 10000 {
		t.Fatalf("the stock line must stay on 4100 with 10000 paise, got %d", c)
	}
	if d, _ := stage140GL(t, schema, "SalesInvoice", "SI-14822-ACCT", "1300"); d != 60000 {
		t.Fatalf("receivable must still be the invoice total, got %d", d)
	}
}

// An account that is not income, or not in the chart, refuses the posting.
func TestStage14822RefusesAccountThatIsNotIncome(t *testing.T) {
	tenantID, schema, insert := stage14822Setup(t)
	for _, acct := range []string{"6950", "9999"} {
		id := "SI-14822-BAD-" + acct
		stage14822Invoice(insert, id, acct)
		_, err := PostSalesInvoice(tenantID, id, "manager1")
		if err == nil || !containsCode(err, freeTextAccountCode) {
			t.Fatalf("account %s must be refused with %s, got %v", acct, freeTextAccountCode, err)
		}
		if d, c := stage140GL(t, schema, "SalesInvoice", id, "4100"); d+c != 0 {
			t.Fatalf("a refused invoice must leave no GL row")
		}
	}
}

// A return of a coded free-text line debits the line's own account.
func TestStage14822ReturnFollowsTheLinesAccount(t *testing.T) {
	tenantID, schema, insert := stage14822Setup(t)
	stage14822Invoice(insert, "SI-14822-RET", "4950")
	if _, err := PostSalesInvoice(tenantID, "SI-14822-RET", "manager1"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got := returnRevenueSplit(tx, schema, tenantID, "SI-14822-RET", []GSTLineInput{{Sku: "INSTALL", Qty: 1, UnitRate: 500}}, 50000)
	if got["4950"] != 50000 || len(got) != 1 {
		t.Fatalf("returning the free-text line must go back through 4950, got %v", got)
	}
	got = returnRevenueSplit(tx, schema, tenantID, "SI-14822-RET", []GSTLineInput{{Sku: "TEE14822", Qty: 1, UnitRate: 100}}, 10000)
	if got != nil {
		t.Fatalf("a stock line keeps today's 4100 return, got %v", got)
	}
}

func containsCode(err error, code string) bool {
	if ve, ok := err.(*ValidationError); ok && ve.Code == code {
		return true
	}
	return err != nil && strings.Contains(err.Error(), "not an income account")
}
