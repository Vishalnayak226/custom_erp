package engines

import (
	"database/sql"
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
	if !sourceLinesOn(schema) || len(shares) == 0 {
		return opts
	}
	var o PostingOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	if len(o.Lines) > 0 || o.Currency != "" {
		return opts
	}
	lines := sourceLinePostingLines(debits, credits, shares)
	if lines == nil {
		return opts
	}
	o.Lines = lines
	return []PostingOptions{o}
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
