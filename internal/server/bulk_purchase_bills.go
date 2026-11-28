package server

// 316.1: the Purchase sheet's bill numbers are paired with the Bills sheet the
// way a person would read them, instead of refusing every bill typed a little
// differently. minn's November file had a bill under the wrong vendor, bill
// numbers Excel counted up row by row (gagan01, gagan02 ... gagan82), a row
// typed with another bill no than the sheet's, and rows whose bill was not on
// the Bills sheet at all. Each is now received, and the result says what was
// read how.

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// bpPairing says which bill each Purchase row is received on.
type bpPairing struct {
	bill map[*bpLine]*bpBillIn
	note map[*bpLine]string // why a row is on a bill whose no it was not typed with
	made []*bpBillIn        // bills not on the Bills sheet, made from their rows
}

// bpAlnum is a bill no with only its letters and digits, lower case:
// "SA-4733 PO" and "sa4733po" are one bill.
func bpAlnum(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

// bpSplitNo splits a bill no into its letters and its closing number:
// "gagan07" is ("gagan", 7).
func bpSplitNo(s string) (string, int, bool) {
	a := bpAlnum(s)
	i := len(a)
	for i > 0 && a[i-1] >= '0' && a[i-1] <= '9' {
		i--
	}
	if i == 0 || i == len(a) {
		return a, 0, false
	}
	n, err := strconv.Atoi(a[i:])
	if err != nil {
		return a, 0, false
	}
	return a[:i], n, true
}

// bpCountsOn: bill no b on the row after a is a's own bill continued - the
// same no, or Excel's fill counting it up by one (gagan07 after gagan06).
func bpCountsOn(a, b string) bool {
	if bpAlnum(a) == bpAlnum(b) {
		return true
	}
	pa, na, oka := bpSplitNo(a)
	pb, nb, okb := bpSplitNo(b)
	return oka && okb && pa == pb && nb == na+1
}

// bpPairBills pairs every Purchase row that has a bill no with a bill, in this
// order: (1) the vendor's bill with that no; (2) a bill typed under the wrong
// vendor, when only one other vendor's rows carry its no; (3) the vendor's
// bill with the same letters and digits; (4) Excel's count-up from the row
// above; (5) the vendor's one bill no row names, when its rows carry one other
// no; (6) a bill of its own made from its rows. today dates a made bill when
// the file has no bill date to go by.
func bpPairBills(bills []*bpBillIn, lines []*bpLine, today string) bpPairing {
	p := bpPairing{bill: map[*bpLine]*bpBillIn{}, note: map[*bpLine]string{}}
	key := func(vendor, no string) string { return bpAttrKey(vendor) + "|" + swNorm(no) }
	var rows []*bpLine
	byRow := map[int]*bpLine{}
	for _, l := range lines {
		byRow[l.Row] = l
		if strings.TrimSpace(l.Invoice) != "" {
			rows = append(rows, l)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Row < rows[j].Row })

	exact := map[string]*bpBillIn{}
	var sheet []*bpBillIn // the Bills sheet less a bill listed twice
	for _, bi := range bills {
		if k := key(bi.Vendor, bi.Invoice); exact[k] == nil {
			exact[k] = bi
			sheet = append(sheet, bi)
		}
	}
	claimed := map[*bpBillIn]int{}
	taken := map[*bpBillIn][]*bpLine{}
	put := func(l *bpLine, bi *bpBillIn, joined bool) {
		p.bill[l] = bi
		claimed[bi]++
		if joined {
			taken[bi] = append(taken[bi], l)
			p.note[l] = "received on bill " + bi.Invoice
		}
	}
	free := func(l *bpLine) bool { return p.bill[l] == nil }

	// (1) As typed.
	for _, l := range rows {
		if bi := exact[key(l.Vendor, l.Invoice)]; bi != nil {
			put(l, bi, false)
		}
	}
	// (2) Under the wrong vendor (or none): only one other vendor's rows
	// carry this no, and none of the bill's own vendor's.
	for _, bi := range sheet {
		if claimed[bi] > 0 {
			continue
		}
		owners := map[string]string{}
		var mine []*bpLine
		for _, l := range rows {
			if free(l) && swNorm(l.Invoice) == swNorm(bi.Invoice) {
				owners[bpAttrKey(l.Vendor)] = l.Vendor
				mine = append(mine, l)
			}
		}
		if len(owners) != 1 {
			continue
		}
		old := bi.Vendor
		bi.Vendor = mine[0].Vendor
		if old == "" {
			if bi.err == "missing: Vendor" {
				bi.err = ""
			}
			bi.note = "no Vendor on the Bills sheet: received for " + bi.Vendor
		} else {
			bi.note = fmt.Sprintf("under %s on the Bills sheet; its rows are %s's - received for %s", old, bi.Vendor, bi.Vendor)
		}
		for _, l := range mine {
			put(l, bi, false)
		}
	}
	ofVendor := map[string][]*bpBillIn{}
	for _, bi := range sheet {
		ofVendor[bpAttrKey(bi.Vendor)] = append(ofVendor[bpAttrKey(bi.Vendor)], bi)
	}
	// (3) The same letters and digits as one of the vendor's bills.
	for _, l := range rows {
		if !free(l) {
			continue
		}
		var hit []*bpBillIn
		for _, bi := range ofVendor[bpAttrKey(l.Vendor)] {
			if bpAlnum(bi.Invoice) == bpAlnum(l.Invoice) {
				hit = append(hit, bi)
			}
		}
		if len(hit) == 1 {
			put(l, hit[0], true)
		}
	}
	// (4) Excel's count-up: the row above is the same vendor's, already on a
	// bill, and this no continues it. A +1 step is taken only from a no that
	// is on one row, as a fill series is; a block of rows on one no followed
	// by the next no is another bill.
	runOf := func(l *bpLine) int {
		n := 1
		for up := byRow[l.Row-1]; up != nil && bpAttrKey(up.Vendor) == bpAttrKey(l.Vendor) && bpAlnum(up.Invoice) == bpAlnum(l.Invoice); up = byRow[up.Row-1] {
			n++
		}
		return n
	}
	for _, l := range rows {
		if !free(l) {
			continue
		}
		up := byRow[l.Row-1]
		if up == nil || p.bill[up] == nil || bpAttrKey(up.Vendor) != bpAttrKey(l.Vendor) || !bpCountsOn(up.Invoice, l.Invoice) {
			continue
		}
		if bpAlnum(up.Invoice) != bpAlnum(l.Invoice) && runOf(up) > 1 {
			continue
		}
		put(l, p.bill[up], true)
	}
	// (5) The vendor's one bill no row names takes the vendor's other rows,
	// when they all carry one other no (ballerinapo01 for ballerina01).
	for vk, vbills := range ofVendor {
		var empty []*bpBillIn
		for _, bi := range vbills {
			if claimed[bi] == 0 {
				empty = append(empty, bi)
			}
		}
		if len(empty) != 1 {
			continue
		}
		var left []*bpLine
		nos := map[string]bool{}
		for _, l := range rows {
			if free(l) && bpAttrKey(l.Vendor) == vk {
				left = append(left, l)
				nos[bpAlnum(l.Invoice)] = true
			}
		}
		if len(nos) != 1 {
			continue
		}
		for _, l := range left {
			put(l, empty[0], true)
		}
	}
	// (6) What is left is a bill the Bills sheet does not have: received as
	// its own, at its rows, dated like the vendor's other bills.
	vendorDate, count := map[string]string{}, map[string]int{}
	var common string
	for _, bi := range bills {
		if bi.Date == "" {
			continue
		}
		if vendorDate[bpAttrKey(bi.Vendor)] == "" {
			vendorDate[bpAttrKey(bi.Vendor)] = bi.Date
		}
		count[bi.Date]++
		if count[bi.Date] > count[common] {
			common = bi.Date
		}
	}
	whose := map[string]string{} // a bill no -> the vendor whose bill it is
	for _, bi := range sheet {
		if whose[bpAlnum(bi.Invoice)] == "" {
			whose[bpAlnum(bi.Invoice)] = bi.Vendor
		}
	}
	made := map[string]*bpBillIn{}
	madeRows := map[*bpBillIn][]int{}
	for _, l := range rows {
		if !free(l) {
			continue
		}
		k := bpAttrKey(l.Vendor) + "|" + bpAlnum(l.Invoice)
		bi := made[k]
		if bi == nil {
			bi = &bpBillIn{Invoice: l.Invoice, Vendor: l.Vendor, fromRows: true,
				Date: firstNonEmptyStr(vendorDate[bpAttrKey(l.Vendor)], common, today)}
			made[k] = bi
			p.made = append(p.made, bi)
		}
		p.bill[l] = bi
		p.note[l] = fmt.Sprintf("bill %s is not on the Bills sheet: received as its own bill", l.Invoice)
		madeRows[bi] = append(madeRows[bi], l.Row)
	}
	for _, bi := range p.made {
		bi.note = "not on the Bills sheet: made from " + bpRowsText(madeRows[bi])
		if v := whose[bpAlnum(bi.Invoice)]; v != "" && bpAttrKey(v) != bpAttrKey(bi.Vendor) {
			bi.note += fmt.Sprintf(" - %s is %s's bill no", bi.Invoice, v)
		}
	}
	for _, bi := range sheet {
		if ls := taken[bi]; len(ls) > 0 {
			var nos []string
			seen := map[string]bool{}
			for _, l := range ls {
				if !seen[l.Invoice] && len(nos) < 3 {
					nos = append(nos, l.Invoice)
				}
				seen[l.Invoice] = true
			}
			more := ""
			if len(seen) > len(nos) {
				more = " ..."
			}
			bi.note = bpJoin(bi.note, fmt.Sprintf("%d row(s) with bill no %s%s taken as this bill", len(ls), strings.Join(nos, ", "), more))
		}
	}
	return p
}

// bpRowsText: "row 3", "rows 409-432", "rows 3, 769-770".
func bpRowsText(rows []int) string {
	sort.Ints(rows)
	var parts []string
	for i := 0; i < len(rows); {
		j := i
		for j+1 < len(rows) && rows[j+1] == rows[j]+1 {
			j++
		}
		if j == i {
			parts = append(parts, strconv.Itoa(rows[i]))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", rows[i], rows[j]))
		}
		i = j + 1
	}
	if len(rows) == 1 {
		return "row " + parts[0]
	}
	return "rows " + strings.Join(parts, ", ")
}

// bpBillTotal: a bill total more than Rs 1 from its rows is received at the
// rows - a goods receipt's total must agree with its lines - and the note
// keeps what was typed.
func bpBillTotal(typed, rows float64) (float64, string) {
	diff := math.Round((typed-rows)*100) / 100
	if math.Abs(diff) <= 1 {
		return typed, ""
	}
	return rows, fmt.Sprintf("bill total %s, its rows %s: received at the rows - settle the %s with the vendor",
		swNumText(typed), swNumText(rows), swNumText(math.Abs(diff)))
}

// bpRunFix is a run's rows to fix. A run saved before 237.6 kept none, so
// its Details had no Rows to fix button and the download said every row had
// loaded while ten bills had failed (316.2): for such a run the rows are
// worked out again from its result, the way fixRows lists them.
func bpRunFix(res *bpResult) []bpFixRow {
	if len(res.Fix) > 0 {
		return res.Fix
	}
	var fix []bpFixRow
	seen := map[string]bool{}
	add := func(sheet string, row int, reason string) {
		k := sheet + "|" + strconv.Itoa(row)
		if row <= 0 || seen[k] {
			return
		}
		seen[k] = true
		fix = append(fix, bpFixRow{Sheet: sheet, Row: row, Reason: reason})
	}
	for _, v := range res.Vendors {
		if v.Action == "error" || v.Result == "failed" {
			add("vendors", v.Row, v.Message)
		}
	}
	bad := func(o *bpBillOut) bool {
		return o.Action == "error" || o.Action == "waiting" || o.Result == "failed" || o.Result == "waiting"
	}
	bill := map[string]*bpBillOut{}
	for _, b := range res.Bills {
		if k := bpAttrKey(b.Vendor) + "|" + swNorm(b.Invoice); bill[k] == nil {
			bill[k] = b
		}
	}
	// A bill of the PO + GRN template is named by its first row: its
	// problems are that row's, not a Bills sheet row's.
	fromRows := map[*bpBillOut]bool{}
	for _, po := range res.POs {
		for _, l := range po.Lines {
			if b := bill[bpAttrKey(po.Vendor)+"|"+swNorm(l.Bill)]; b != nil && b.Row == l.Row {
				fromRows[b] = true
			}
		}
	}
	for _, po := range res.POs {
		for _, l := range po.Lines {
			b := bill[bpAttrKey(po.Vendor)+"|"+swNorm(l.Bill)]
			switch {
			case po.Action == "error" || po.Result == "failed":
				add("purchase", l.Row, fmt.Sprintf("PO Ref %s: %s", po.Ref, po.Message))
			case l.Action == "error":
				add("purchase", l.Row, l.Message)
			case l.Bill == "":
			case b == nil:
				add("purchase", l.Row, firstNonEmptyStr(l.Message, "its bill is not received"))
			case bad(b):
				add("purchase", l.Row, fmt.Sprintf("ordered on PO Ref %s; bill %s not received: %s", po.Ref, l.Bill, b.Message))
			}
		}
	}
	for _, b := range res.Bills {
		if bad(b) && !fromRows[b] {
			add("bills", b.Row, b.Message)
		}
	}
	return fix
}

// bpJoin adds a note to a message without losing what is there.
func bpJoin(msg, add string) string {
	switch {
	case add == "":
		return msg
	case msg == "":
		return add
	}
	return msg + "; " + add
}
