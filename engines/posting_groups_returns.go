package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

// Stage 148.4 (returns): a return or refund reverses revenue through the
// account the original sale's posting rule used, not a fixed 4100. With no
// rule on the sale - or a sale that posted wholly to 4100 - every function here
// returns nil and the caller keeps its original 4100 posting.

// saleRevenueRuleUsed is true when the original sale credited revenue to an
// account other than 4100 (so a rule moved it). A rule added after the sale
// leaves the return on 4100, as the sale was.
func saleRevenueRuleUsed(tx *sql.Tx, schema, docType, docID, keyLike string) bool {
	var found bool
	err := tx.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(SELECT 1 FROM %s.gl_postings
		 WHERE document_type = $1 AND document_id = $2 AND credit > 0 AND idempotency_key LIKE $3
		   AND account_code NOT IN ('4100', '2200', '2201', '2202', '2600', '%s'))`, schema, accountRoundOff),
		docType, docID, keyLike).Scan(&found)
	return err == nil && found
}

// returnRevenueSplit spreads total over the accounts the original sale's
// revenue rule gives the returned lines. nil means "post to 4100 as before".
func returnRevenueSplit(tx *sql.Tx, schema, tenantID, originalOrderID string, lines []GSTLineInput, total int64) map[string]int64 {
	if total <= 0 || len(lines) == 0 || strings.TrimSpace(originalOrderID) == "" {
		return nil
	}
	var event, customer string
	var raw string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema), originalOrderID).Scan(&raw); err == nil {
		var cart posCart
		if json.Unmarshal([]byte(raw), &cart) != nil || !postingProfileExists(tenantID, PostingEventPOSSaleRevenue) ||
			!saleRevenueRuleUsed(tx, schema, "POSCart", originalOrderID, "%:SALE_REVENUE") {
			return nil
		}
		event, customer = PostingEventPOSSaleRevenue, cart.CustomerID
	} else {
		invID := salesInvoiceForOrder(tenantID, originalOrderID)
		if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1`, schema), invID).Scan(&raw); err != nil {
			return nil
		}
		var inv map[string]interface{}
		if json.Unmarshal([]byte(raw), &inv) != nil {
			return nil
		}
		// Stage 148.22: coded lines with their own income account go back through it.
		if ft := freeTextReturnAccounts(tenantID, raw); len(ft) > 0 {
			ruleOK := postingProfileExists(tenantID, PostingEventSalesInvoiceRevenue) && saleRevenueRuleUsed(tx, schema, "SalesInvoice", invID, "%:POST")
			if got := returnWithFreeText(tenantID, lines, total, ft, strField(inv, "customer"), ruleOK); got != nil {
				return got
			}
		}
		if !postingProfileExists(tenantID, PostingEventSalesInvoiceRevenue) ||
			!saleRevenueRuleUsed(tx, schema, "SalesInvoice", invID, "%:POST") {
			return nil
		}
		event, customer = PostingEventSalesInvoiceRevenue, strField(inv, "customer")
	}
	weights := make([]groupLine, 0, len(lines))
	for _, l := range lines {
		weights = append(weights, groupLine{Sku: l.Sku, Weight: RupeesToPaise(l.UnitRate) * int64(l.Qty)})
	}
	ctx := PostingContext{CustomerGroup: partyGroup(tenantID, "Customer", "customer_group", customer)}
	out, moved := accountsByItemGroup(tenantID, event, "credit", "4100", total, ctx, weights)
	if !moved {
		return nil
	}
	return out
}

// returnRevenueSplitForRequest is returnRevenueSplit for a ReturnRequest's
// refundable lines.
func returnRevenueSplitForRequest(tx *sql.Tx, schema, tenantID, returnRequestID string, total int64) map[string]int64 {
	if strings.TrimSpace(returnRequestID) == "" {
		return nil
	}
	_, rr, err := fetchReturnRequest(tenantID, returnRequestID)
	if err != nil {
		return nil
	}
	return returnRevenueSplit(tx, schema, tenantID, strField(rr, "original_order_id"), refundedGSTLines(rr), total)
}

// spreadByAccounts splits total over the accounts in like, in proportion to
// their amounts; the last account takes the rounding remainder.
func spreadByAccounts(total int64, like map[string]int64) map[string]int64 {
	var sum int64
	accounts := make([]string, 0, len(like))
	for a, v := range like {
		if v > 0 {
			accounts = append(accounts, a)
			sum += v
		}
	}
	if total <= 0 || sum == 0 {
		return nil
	}
	sort.Strings(accounts)
	out := map[string]int64{}
	var given int64
	for i, a := range accounts {
		share := total - given
		if i < len(accounts)-1 {
			share = new(big.Int).Div(new(big.Int).Mul(big.NewInt(total), big.NewInt(like[a])), big.NewInt(sum)).Int64()
		}
		if share > 0 {
			out[a] += share
		}
		given += share
	}
	return out
}

// cartRevenueAccounts is what a POS sale credited to revenue, by account (its
// round-off excluded). Used to take the sale's tax back off the same accounts.
func cartRevenueAccounts(tx *sql.Tx, schema, checkoutID string) map[string]int64 {
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT account_code, COALESCE(SUM(credit), 0) FROM %s.gl_postings
		 WHERE document_type = 'POSCart' AND document_id = $1 AND idempotency_key LIKE '%%:SALE_REVENUE' AND credit > 0 AND account_code <> $2
		 GROUP BY account_code`, schema), checkoutID, accountRoundOff)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var a string
		var v int64
		if rows.Scan(&a, &v) == nil {
			out[a] = v
		}
	}
	return out
}

// saleDebitAgainstRevenue is the debit map for taking amount (tax, exempt
// turnover) back out of a POS sale's revenue: 4100 as ever when the sale
// posted only there, else spread over the accounts it actually credited.
func saleDebitAgainstRevenue(tx *sql.Tx, schema, checkoutID string, amount int64) map[string]int64 {
	rev := cartRevenueAccounts(tx, schema, checkoutID)
	if len(rev) == 0 || (len(rev) == 1 && rev["4100"] > 0) {
		return map[string]int64{"4100": amount}
	}
	if out := spreadByAccounts(amount, rev); out != nil {
		return out
	}
	return map[string]int64{"4100": amount}
}

// returnWithFreeText splits a return between the lines that name their own
// income account and the rest (which keep the sale's rule, or 4100). nil when no
// returned line is a free-text one.
func returnWithFreeText(tenantID string, lines []GSTLineInput, total int64, ft map[string]string, customer string, ruleOK bool) map[string]int64 {
	ftW := map[string]int64{}
	var rest []groupLine
	var all, ftAll int64
	for _, l := range lines {
		w := RupeesToPaise(l.UnitRate) * int64(l.Qty)
		all += w
		if a, ok := ft[strings.TrimSpace(l.Sku)]; ok {
			ftW[a] += w
			ftAll += w
		} else {
			rest = append(rest, groupLine{Sku: l.Sku, Weight: w})
		}
	}
	if ftAll == 0 || all == 0 {
		return nil
	}
	out := spreadByAccounts(total*ftAll/all, ftW)
	var given int64
	for _, v := range out {
		given += v
	}
	left := total - given
	if left > 0 {
		base := map[string]int64{"4100": left}
		if ruleOK {
			ctx := PostingContext{CustomerGroup: partyGroup(tenantID, "Customer", "customer_group", customer)}
			if split, moved := accountsByItemGroup(tenantID, PostingEventSalesInvoiceRevenue, "credit", "4100", left, ctx, rest); moved {
				base = split
			}
		}
		for a, v := range base {
			out[a] += v
		}
	}
	return out
}
