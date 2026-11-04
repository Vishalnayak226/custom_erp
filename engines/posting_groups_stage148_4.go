package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 148.4: the fixed-account events - sales revenue, cost of goods, goods
// receipt inventory - read their account from an Approved PostingProfile by
// item group (Item.category), customer group and vendor group. With no profile
// for the event every posting goes to the account it always has: the helpers
// below return the default map untouched, so the journal is byte-for-byte the
// same as before the stage.

// groupLine is one line of a posting: the SKU (for its item group) and the
// weight the posting's total is spread by.
type groupLine struct {
	Sku    string
	Weight int64
}

// postingProfileExists is the cheap gate: is any Approved profile named for
// the event? When none is, nothing below runs a second query.
func postingProfileExists(tenantID, event string) bool {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var found bool
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(SELECT 1 FROM %s.documents
		 WHERE doctype = 'PostingProfile' AND data->>'event' = $1 AND COALESCE(data->>'status', status) = 'Approved' AND deleted_at IS NULL)`, schema), event).Scan(&found); err != nil {
		return false
	}
	return found
}

// itemGroupForSKU is the item's group (its category), "" when it has none.
func itemGroupForSKU(tenantID, sku string) string {
	if strings.TrimSpace(sku) == "" {
		return ""
	}
	item, err := ResolveItemBySKU(tenantID, sku)
	if err != nil || item == nil {
		return ""
	}
	return strings.TrimSpace(strField(item.Data, "category"))
}

// partyGroup reads customer_group / vendor_group off the party's record.
func partyGroup(tenantID, doctype, field, id string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	data, _, err := fetchDocData(tenantID, doctype, id)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strField(data, field))
}

// accountsByItemGroup spreads total over the accounts the event's profiles give
// each line's item group. side is "credit" or "debit" (the profile account that
// applies). The second result is false when every line lands on defaultAcct -
// the caller then keeps its original code path.
func accountsByItemGroup(tenantID, event, side, defaultAcct string, total int64, ctx PostingContext, lines []groupLine) (map[string]int64, bool) {
	out := map[string]int64{defaultAcct: total}
	if total <= 0 || len(lines) == 0 || !postingProfileExists(tenantID, event) {
		return out, false
	}
	weights := map[string]int64{}
	var sum int64
	for _, l := range lines {
		if l.Weight <= 0 {
			continue
		}
		c := ctx
		c.ItemGroup = itemGroupForSKU(tenantID, l.Sku)
		r := ResolvePostingAccounts(tenantID, event, c, defaultAcct, defaultAcct)
		acct := r.Credit
		if side == "debit" {
			acct = r.Debit
		}
		weights[acct] += l.Weight
		sum += l.Weight
	}
	if sum == 0 || (len(weights) == 1 && weights[defaultAcct] > 0) {
		return out, false
	}
	accounts := make([]string, 0, len(weights))
	for a := range weights {
		accounts = append(accounts, a)
	}
	sort.Strings(accounts)
	out = map[string]int64{}
	var given int64
	for i, a := range accounts {
		share := total - given
		if i < len(accounts)-1 {
			share = new(big.Int).Div(new(big.Int).Mul(big.NewInt(total), big.NewInt(weights[a])), big.NewInt(sum)).Int64()
		}
		if share > 0 {
			out[a] += share
		}
		given += share
	}
	return out, true
}

// salesInvoiceLineWeights weights an invoice's lines by their amount.
func salesInvoiceLineWeights(dataStr string) []groupLine {
	var out []groupLine
	for _, l := range salesInvoiceLineItems(dataStr) {
		w := RupeesToPaise(l.Amount)
		if w <= 0 {
			w = RupeesToPaise(l.paidUnitPrice()) * int64(l.Qty)
		}
		out = append(out, groupLine{Sku: l.Sku, Weight: w})
	}
	return out
}

// salesInvoiceRevenueCredits is the sales_invoice_revenue split of an
// invoice's revenue; moved is false when it all stays on defaultAcct.
func salesInvoiceRevenueCredits(tenantID string, data map[string]interface{}, dataStr, defaultAcct string, total int64, eligible bool) (map[string]int64, bool) {
	if !eligible {
		return map[string]int64{defaultAcct: total}, false
	}
	ctx := PostingContext{CustomerGroup: partyGroup(tenantID, "Customer", "customer_group", strField(data, "customer"))}
	return accountsByItemGroup(tenantID, PostingEventSalesInvoiceRevenue, "credit", defaultAcct, total, ctx, salesInvoiceLineWeights(dataStr))
}

// salesInvoiceCOGSAccounts is the sales_invoice_cogs debit (5100) and credit
// (1200) maps for the cost of the goods an invoice issued.
func salesInvoiceCOGSAccounts(tenantID string, data map[string]interface{}, lines []groupLine, total int64) (debits, credits map[string]int64) {
	ctx := PostingContext{CustomerGroup: partyGroup(tenantID, "Customer", "customer_group", strField(data, "customer"))}
	debits, _ = accountsByItemGroup(tenantID, PostingEventSalesInvoiceCOGS, "debit", "5100", total, ctx, lines)
	credits, _ = accountsByItemGroup(tenantID, PostingEventSalesInvoiceCOGS, "credit", "1200", total, ctx, lines)
	return debits, credits
}

// posCartForGroups reads the stored POS cart for its lines and customer.
func posCartForGroups(tx *sql.Tx, schema, checkoutID string) (posCart, bool) {
	var raw string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1 AND doctype = 'POSCart'`, schema), checkoutID).Scan(&raw); err != nil {
		return posCart{}, false
	}
	var cart posCart
	if json.Unmarshal([]byte(raw), &cart) != nil {
		return posCart{}, false
	}
	return cart, true
}

// posRevenueCredits is the pos_sale_revenue credit map for a POS sale.
func posRevenueCredits(tx *sql.Tx, schema, tenantID, checkoutID string, salePrice int64) map[string]int64 {
	def := map[string]int64{"4100": salePrice}
	if !postingProfileExists(tenantID, PostingEventPOSSaleRevenue) {
		return def
	}
	cart, ok := posCartForGroups(tx, schema, checkoutID)
	if !ok {
		return def
	}
	lines := make([]groupLine, 0, len(cart.Items))
	for _, it := range cart.Items {
		lines = append(lines, groupLine{Sku: it.Sku, Weight: RupeesToPaise(it.SalePrice) * int64(it.Qty)})
	}
	ctx := PostingContext{CustomerGroup: partyGroup(tenantID, "Customer", "customer_group", cart.CustomerID)}
	out, _ := accountsByItemGroup(tenantID, PostingEventPOSSaleRevenue, "credit", "4100", salePrice, ctx, lines)
	return out
}

// posCOGSAccounts is the pos_sale_cogs debit and credit maps of a POS sale.
func posCOGSAccounts(tx *sql.Tx, schema, tenantID, checkoutID string, costPrice int64) (debits, credits map[string]int64) {
	debits, credits = map[string]int64{"5100": costPrice}, map[string]int64{"1200": costPrice}
	if !postingProfileExists(tenantID, PostingEventPOSSaleCOGS) {
		return debits, credits
	}
	cart, ok := posCartForGroups(tx, schema, checkoutID)
	if !ok {
		return debits, credits
	}
	lines := make([]groupLine, 0, len(cart.Items))
	for _, it := range cart.Items {
		w := ResolveCOGSUnitCostPaise(tenantID, it.Sku, itemStandardCostRupees(tenantID, it.Sku)) * int64(it.Qty)
		if w <= 0 {
			w = RupeesToPaise(it.SalePrice) * int64(it.Qty)
		}
		lines = append(lines, groupLine{Sku: it.Sku, Weight: w})
	}
	ctx := PostingContext{CustomerGroup: partyGroup(tenantID, "Customer", "customer_group", cart.CustomerID)}
	debits, _ = accountsByItemGroup(tenantID, PostingEventPOSSaleCOGS, "debit", "5100", costPrice, ctx, lines)
	credits, _ = accountsByItemGroup(tenantID, PostingEventPOSSaleCOGS, "credit", "1200", costPrice, ctx, lines)
	return debits, credits
}

// grnInventoryDebits is the grn_receipt debit map: the received value on 1200,
// or on the account the item's group / the vendor's group sends it to.
func grnInventoryDebits(tenantID, grnID, poID string, lines []groupLine, total int64) map[string]int64 {
	if !postingProfileExists(tenantID, PostingEventGRNReceipt) {
		return map[string]int64{"1200": total}
	}
	vendor := ""
	if grn, _, err := fetchDocData(tenantID, "GRN", grnID); err == nil {
		vendor = strings.TrimSpace(firstNonEmpty(strField(grn, "vendor"), strField(grn, "vendor_id")))
	}
	if vendor == "" && poID != "" {
		if po, _, err := fetchDocData(tenantID, "PurchaseOrder", poID); err == nil {
			vendor = strings.TrimSpace(firstNonEmpty(strField(po, "vendor_id"), strField(po, "vendor")))
		}
	}
	ctx := PostingContext{VendorGroup: partyGroup(tenantID, "Vendor", "vendor_group", vendor)}
	out, _ := accountsByItemGroup(tenantID, PostingEventGRNReceipt, "debit", "1200", total, ctx, lines)
	return out
}
