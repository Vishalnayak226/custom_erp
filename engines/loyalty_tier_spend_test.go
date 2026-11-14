package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 166.1: a customer's tier spend is paid bills less processed refunds,
// and both lookups are served by migration 166_1's partial indexes instead of
// reading every paid cart and every refund the client has.
func TestStage1661TierSpendIsIndexServed(t *testing.T) {
	db.InitDB(testConnStr())

	// What it sums: two paid bills, one Processed refund on the first, a
	// Pending refund and another customer's refund that must not count.
	sfx := fmt.Sprintf("%d", time.Now().UnixNano())
	cust, cartA, cartB, other := "C1661-"+sfx, "CART1661A-"+sfx, "CART1661B-"+sfx, "CART1661X-"+sfx
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id LIKE '%1661%-' || $1`, sfx)
	for _, row := range []struct{ id, doctype, status, data string }{
		{cartA, "POSCart", "Paid", `{"customer_id":"` + cust + `","amount_paid":1000}`},
		{cartB, "POSCart", "Paid", `{"customer_id":"` + cust + `","amount_paid":500}`},
		{other, "POSCart", "Paid", `{"customer_id":"someone-else-` + sfx + `","amount_paid":700}`},
		{"RR1661P-" + sfx, "RefundRequest", "Active", `{"original_order_id":"` + cartA + `","status":"Processed","net_amount":200}`},
		{"RR1661Q-" + sfx, "RefundRequest", "Active", `{"original_order_id":"` + cartB + `","status":"Pending","net_amount":500}`},
		{"RR1661X-" + sfx, "RefundRequest", "Active", `{"original_order_id":"` + other + `","status":"Processed","net_amount":700}`},
	} {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3::jsonb, $4, 'system')`,
			row.id, row.doctype, row.data, row.status); err != nil {
			t.Fatal(err)
		}
	}
	spend, err := customerTierSpend("default", "tenant_default", cust)
	if err != nil {
		t.Fatal(err)
	}
	if spend != 1300 {
		t.Errorf("tier spend = %v, want 1300 (1000 + 500 paid, less the one processed refund of 200)", spend)
	}

	// Which index answers: a temporary copy holding only the two indexes and
	// 20,000 carts and refunds, priced so the plan names whichever index (if
	// any) can serve each lookup, whatever one table's statistics prefer.
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`CREATE TEMP TABLE documents (LIKE tenant_default.documents INCLUDING DEFAULTS) ON COMMIT DROP`,
		`CREATE INDEX idx_documents_poscart_paid_customer ON pg_temp.documents ((data->>'customer_id'), created_at) WHERE doctype = 'POSCart' AND status = 'Paid'`,
		`CREATE INDEX idx_documents_refund_original_order ON pg_temp.documents ((data->>'original_order_id')) WHERE doctype = 'RefundRequest'`,
		`INSERT INTO pg_temp.documents (id, doctype, data, status, created_by)
		 SELECT 'CART-' || g, 'POSCart', jsonb_build_object('customer_id', 'CUST-' || (g % 2000), 'amount_paid', 100), 'Paid', 'system'
		 FROM generate_series(1, 20000) g`,
		`INSERT INTO pg_temp.documents (id, doctype, data, status, created_by)
		 SELECT 'RR-' || g, 'RefundRequest', jsonb_build_object('original_order_id', 'CART-' || g, 'status', 'Processed', 'net_amount', 10), 'Active', 'system'
		 FROM generate_series(1, 20000) g`,
		`ANALYZE pg_temp.documents`,
		`SET LOCAL enable_seqscan = off`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	explain := func(query string) string {
		t.Helper()
		rows, err := tx.Query("EXPLAIN "+query, "CUST-7", 30)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var plan strings.Builder
		for rows.Next() {
			var line string
			_ = rows.Scan(&line)
			plan.WriteString(line + "\n")
		}
		return plan.String()
	}
	paid := explain(fmt.Sprintf(tierSpendPaidSQL, "pg_temp"))
	if !strings.Contains(paid, "idx_documents_poscart_paid_customer") || strings.Contains(paid, "Seq Scan") {
		t.Errorf("the paid-bill sum is not served by the customer index:\n%s", paid)
	}
	refunded := explain(fmt.Sprintf(tierSpendRefundedSQL, "pg_temp", "pg_temp"))
	if !strings.Contains(refunded, "idx_documents_refund_original_order") || strings.Contains(refunded, "Seq Scan") {
		t.Errorf("the refund sum is not served by the original-order index:\n%s", refunded)
	}

	// Migration 166_1 put both indexes on the real tables.
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = 'tenant_default' AND tablename = 'documents'
		AND indexname IN ('idx_documents_poscart_paid_customer', 'idx_documents_refund_original_order')`).Scan(&n)
	if n != 2 {
		t.Errorf("tenant_default.documents has %d of the two 166_1 indexes", n)
	}
}
