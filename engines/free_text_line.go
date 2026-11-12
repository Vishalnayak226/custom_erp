package engines

import (
	"database/sql"
	"fmt"
	"strings"
)

// Stage 148.22: a sales-invoice line with no stock item can name the income
// account it credits (and carries its own GST rate on the line, as every line
// does). Blank keeps today's posting: all revenue on the invoice's own account
// (4100). The posting moves each such line's amount from that account to the
// line's; a return of a coded line goes back through the same account.

const freeTextAccountCode = "FIN-0979"

// freeTextMove is one line's revenue to move: its taxable value (used when the
// invoice books output GST separately) or gross amount (when it does not).
type freeTextMove struct {
	Account        string
	Taxable, Gross int64
}

type freeTextMoves []freeTextMove

// invoiceHoldCustomer is who an invoice sells to, for a piece hold (148.41): the form
// may carry only the customer's name, or the customer's id in the same field.
func invoiceHoldCustomer(tenantID string, data map[string]interface{}) string {
	if id := strings.TrimSpace(strField(data, "customer_id")); id != "" {
		return id
	}
	name := strings.TrimSpace(strField(data, "customer"))
	return firstNonEmpty(ResolveCustomerLink(tenantID, name), name)
}

// freeTextLineIsStock is true when the line's code is a stock-tracked item.
func freeTextLineIsStock(tenantID, sku string) bool {
	if strings.TrimSpace(sku) == "" {
		return false
	}
	policy, _, known := ItemPolicyForSKU(tenantID, sku)
	return known && policy.Tracked()
}

// freeTextRevenueMoves reads the invoice's items and checks each named account
// is a Revenue account in the chart. eligible false (a deferred or foreign
// invoice) moves nothing, as before.
func freeTextRevenueMoves(tx *sql.Tx, tenantID, schema, dataStr string, eligible bool) (freeTextMoves, error) {
	if !eligible {
		return nil, nil
	}
	var out freeTextMoves
	for _, l := range salesInvoiceLineItems(dataStr) {
		acct := strings.TrimSpace(l.IncomeAccount)
		if acct == "" || l.Qty <= 0 || freeTextLineIsStock(tenantID, l.Sku) {
			continue
		}
		var kind string
		err := tx.QueryRow(fmt.Sprintf(`SELECT account_type FROM %s.gl_accounts WHERE account_code = $1`, schema), acct).Scan(&kind)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err == sql.ErrNoRows || kind != "Revenue" {
			return nil, &ValidationError{Code: freeTextAccountCode, Message: fmt.Sprintf("account %s is not an income account", acct)}
		}
		taxable := l.Taxable
		if taxable <= 0 {
			taxable = round2(float64(l.Qty) * l.Rate)
		}
		gross := l.Amount
		if gross <= 0 {
			gross = round2(taxable * (1 + l.GSTRate/100))
		}
		out = append(out, freeTextMove{Account: acct, Taxable: RupeesToPaise(taxable), Gross: RupeesToPaise(gross)})
	}
	return out, nil
}

// apply moves the lines' amounts from the account they were credited to. It
// never moves more than that account holds.
func (m freeTextMoves) apply(credits map[string]int64, from string, split bool) map[string]int64 {
	if len(m) == 0 {
		return credits
	}
	out := make(map[string]int64, len(credits)+len(m))
	for a, v := range credits {
		out[a] = v
	}
	for _, mv := range m {
		amt := mv.Gross
		if split {
			amt = mv.Taxable
		}
		if amt > out[from] {
			amt = out[from]
		}
		if amt <= 0 || mv.Account == from {
			continue
		}
		out[from] -= amt
		out[mv.Account] += amt
	}
	if out[from] <= 0 {
		delete(out, from)
	}
	return out
}

// freeTextReturnAccounts maps the codes of an invoice's coded non-stock lines to
// the income account each names.
func freeTextReturnAccounts(tenantID, dataStr string) map[string]string {
	out := map[string]string{}
	for _, l := range salesInvoiceLineItems(dataStr) {
		if a := strings.TrimSpace(l.IncomeAccount); a != "" && strings.TrimSpace(l.Sku) != "" && !freeTextLineIsStock(tenantID, l.Sku) {
			out[strings.TrimSpace(l.Sku)] = a
		}
	}
	return out
}
