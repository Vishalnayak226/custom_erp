package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Stage 170.1: a GL line names the document line it came from.
//
// gl_postings.source_line_id is the position (1, 2, 3 ...) of the document
// line a ledger row belongs to, or empty for a row that belongs to the whole
// document. The posters post one row per account for the whole document, so
// naming the line means writing one row per account per line - more rows, same
// totals. That changes what an existing posting writes, so it is behind the
// setting below (default off): with it off every poster writes exactly the rows
// it always did and the column stays NULL.

const SettingGLSourceLine = "finance.gl_source_line"

func init() {
	RegisterSetting(SettingDefinition{Key: SettingGLSourceLine, Module: "Finance", Label: "Name the source line on GL rows",
		Type: SettingTypeBool, Default: "false",
		Description: "On: a sales invoice posts one ledger row per account per invoice line, each naming its line, so GL Drill-down shows the line. Off: one row per account, as before."})
}

// sourceLineShare is one document line and the weight its share of every
// account's amount is spread by.
type sourceLineShare struct {
	ID     string
	Weight int64
}

func lineNoID(n int) string { return strconv.Itoa(n) }

// sourceLinesOn is the setting, read for a schema.
func sourceLinesOn(schema string) bool { return GetSettingBoolForSchema(schema, SettingGLSourceLine) }

// spreadByShares splits total over shares in proportion to their weights; the
// last share takes the remainder, so the parts always add up to total.
func spreadByShares(total int64, shares []sourceLineShare) []int64 {
	out := make([]int64, len(shares))
	var sum int64
	for _, s := range shares {
		if s.Weight > 0 {
			sum += s.Weight
		}
	}
	if total <= 0 || sum == 0 {
		return out
	}
	last := -1
	for i, s := range shares {
		if s.Weight > 0 {
			last = i
		}
	}
	var given int64
	for i, s := range shares {
		if s.Weight <= 0 {
			continue
		}
		if i == last {
			out[i] = total - given
			continue
		}
		out[i] = total * s.Weight / sum
		given += out[i]
	}
	return out
}

// sourceLinePostingLines is the account maps spread over the document's lines:
// one PostingLine per account per line, each naming its line. Nil when there is
// nothing to name (fewer than one weighted line).
func sourceLinePostingLines(debits, credits map[string]int64, shares []sourceLineShare) []PostingLine {
	weighted := 0
	for _, s := range shares {
		if s.Weight > 0 {
			weighted++
		}
	}
	if weighted == 0 {
		return nil
	}
	var out []PostingLine
	add := func(side map[string]int64, debit bool) {
		accounts := make([]string, 0, len(side))
		for a := range side {
			accounts = append(accounts, a)
		}
		sort.Strings(accounts)
		for _, a := range accounts {
			for i, v := range spreadByShares(side[a], shares) {
				if v <= 0 {
					continue
				}
				l := PostingLine{Account: a, SourceLineID: shares[i].ID}
				if debit {
					l.Debit = v
				} else {
					l.Credit = v
				}
				out = append(out, l)
			}
		}
	}
	add(debits, true)
	add(credits, false)
	return out
}

// withSourceLines returns opts naming the source lines of a posting when the
// setting is on: the rows are written one per account per line. A posting that
// already carries its own Lines, or that is in a foreign currency, is left as
// it is. With the setting off opts comes back untouched.
func withSourceLines(schema string, opts []PostingOptions, debits, credits map[string]int64, shares []sourceLineShare) []PostingOptions {
	return withSourceLineParts(schema, opts, sourceLinePart{Debits: debits, Credits: credits, Shares: shares})
}

// sourceLinePart is one slice of a posting and the lines it is spread over: a
// GRN's expensed lines carry the expense account, its stock lines the
// inventory account, and every line its share of the payable.
type sourceLinePart struct {
	Debits, Credits map[string]int64
	Shares          []sourceLineShare
}

// withSourceLineParts is withSourceLines for a posting made of parts. Any part
// that cannot be spread (no weighted line) leaves opts untouched, so the rows
// always add up to the posting.
func withSourceLineParts(schema string, opts []PostingOptions, parts ...sourceLinePart) []PostingOptions {
	if len(parts) == 0 || !sourceLinesOn(schema) {
		return opts
	}
	var o PostingOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	if len(o.Lines) > 0 || o.Currency != "" {
		return opts
	}
	var lines []PostingLine
	for _, p := range parts {
		var amount int64
		for _, v := range p.Debits {
			amount += max(v, 0)
		}
		for _, v := range p.Credits {
			amount += max(v, 0)
		}
		if amount == 0 {
			continue
		}
		pl := sourceLinePostingLines(p.Debits, p.Credits, p.Shares)
		if pl == nil {
			return opts
		}
		lines = append(lines, pl...)
	}
	if len(lines) == 0 {
		return opts
	}
	o.Lines = lines
	return []PostingOptions{o}
}

// posCartLineShares weights a POS cart's lines, numbered by position, by what
// each sold for (net of its offer share) or, with byCost, by its cost. Nil
// when the setting is off, so the cart is only read when it is wanted.
func posCartLineShares(tx *sql.Tx, schema, tenantID, checkoutID string, byCost bool) []sourceLineShare {
	if !sourceLinesOn(schema) {
		return nil
	}
	cart, ok := posCartForGroups(tx, schema, checkoutID)
	if !ok {
		return nil
	}
	out := make([]sourceLineShare, 0, len(cart.Items))
	for i, it := range cart.Items {
		w := RupeesToPaise(it.SalePrice)*int64(it.Qty) - RupeesToPaise(it.OfferShare)
		if byCost {
			if c := ResolveCOGSUnitCostPaise(tenantID, it.Sku, itemStandardCostRupees(tenantID, it.Sku)) * int64(it.Qty); c > 0 {
				w = c
			}
		}
		out = append(out, sourceLineShare{ID: lineNoID(i + 1), Weight: max(w, 0)})
	}
	return out
}

// glSourceLineColumn caches, per schema, whether gl_postings has the column
// yet - a lagging migration must never block a posting.
var glSourceLineColumn sync.Map

type glSourceLineCheck struct {
	ready     bool
	checkedAt time.Time
}

// rowQueryer is a *sql.Tx or the *sql.DB.
type rowQueryer interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

func glSourceLineReady(tx rowQueryer, schema string) bool {
	if v, ok := glSourceLineColumn.Load(schema); ok {
		c := v.(glSourceLineCheck)
		if c.ready || time.Since(c.checkedAt) < time.Minute {
			return c.ready
		}
	}
	var ready bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'gl_postings' AND column_name = 'source_line_id')`, schema).Scan(&ready); err != nil {
		return false
	}
	glSourceLineColumn.Store(schema, glSourceLineCheck{ready: ready, checkedAt: time.Now()})
	return ready
}

// salesInvoiceLineShares weights an invoice's lines by amount, numbered by
// position (1-based), for withSourceLines.
func salesInvoiceLineShares(dataStr string) []sourceLineShare {
	var out []sourceLineShare
	for i, l := range salesInvoiceLineWeights(dataStr) {
		out = append(out, sourceLineShare{ID: lineNoID(i + 1), Weight: l.Weight})
	}
	return out
}

// grnSKULineNo is the GRN line (1-based) each SKU first appears on - receipt
// costing works per SKU. Nil when the setting is off.
func grnSKULineNo(tx *sql.Tx, schema, grnID string) map[string]int {
	if !sourceLinesOn(schema) {
		return nil
	}
	var raw string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'GRN' AND id = $1`, schema), grnID).Scan(&raw); err != nil {
		return nil
	}
	var data map[string]interface{}
	if json.Unmarshal([]byte(raw), &data) != nil {
		return nil
	}
	out := map[string]int{}
	for i, l := range grnLineMaps(data) {
		if sku := strField(l, "sku"); sku != "" && out[sku] == 0 {
			out[sku] = i + 1
		}
	}
	return out
}
