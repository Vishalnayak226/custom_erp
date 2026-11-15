package engines

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"custom_erp/db"
)

// UPI provider statements (185.2, ask 185). BharatPe, PhonePe and Paytm have
// no public merchant API for a small store, but each lets the owner download
// a statement: one row per customer payment, with the customer's UPI ref
// (RRN), the amount, any charge, the settlement date and the payout's UTR.
// That file goes in through the standard AcquirerSettlement import, which now
// reads each provider's own column names. A row carrying the customer's ref
// is matched to the till bill that took it (amount checked) and the rows of
// one payout are summed and matched to the bank credit, as a payout file's
// single row always was (acquirer_settlement.go).

// acquirerColumnAliases is a statement column, reduced to lower-case words,
// and the AcquirerSettlement field it fills. The field names themselves are
// in it, so the file the template gives keeps working.
var acquirerColumnAliases = map[string]string{
	"settlement date": "settlement_date", "settled date": "settlement_date", "settled on": "settlement_date",
	"payout date": "settlement_date", "settlement dt": "settlement_date", "credit date": "settlement_date",
	"sales date": "sales_date", "transaction date": "sales_date", "txn date": "sales_date", "date": "sales_date",
	"payment date": "sales_date", "order date": "sales_date", "transaction date time": "sales_date",
	"txn date time": "sales_date", "transaction time": "sales_date", "date time": "sales_date", "created date": "sales_date",
	"gross": "gross", "amount": "gross", "transaction amount": "gross", "txn amount": "gross", "gross amount": "gross",
	"order amount": "gross", "payment amount": "gross", "amount rs": "gross", "amount inr": "gross",
	"fee": "fee", "fees": "fee", "commission": "fee", "mdr": "fee", "mdr amount": "fee", "charges": "fee",
	"total charges": "fee", "commission gst": "fee", "platform fee": "fee", "total fee": "fee",
	"net": "net", "net paid": "net", "net amount": "net", "settled amount": "net", "settlement amount": "net",
	"payout amount": "net", "net settlement amount": "net", "amount settled": "net",
	"utr": "utr", "utr no": "utr", "utr number": "utr", "bank utr": "utr", "settlement utr": "utr",
	"settlement utr no": "utr", "payout utr": "utr", "settlement reference": "utr", "settlement ref": "utr",
	"payment ref": "payment_ref", "rrn": "payment_ref", "rrn no": "payment_ref", "upi ref": "payment_ref",
	"upi ref no": "payment_ref", "upi reference": "payment_ref", "upi reference no": "payment_ref",
	"upi transaction id": "payment_ref", "upi txn id": "payment_ref", "customer utr": "payment_ref",
	"upi utr": "payment_ref", "transaction utr": "payment_ref", "txn utr": "payment_ref", "bank txn id": "payment_ref",
	"bank reference no": "payment_ref", "txn ref": "payment_ref", "txn ref no": "payment_ref",
	"terminal": "terminal", "terminal id": "terminal", "tid": "terminal", "mid": "terminal", "merchant id": "terminal",
	"terminal mid": "terminal",
	"mode":         "mode", "payment mode": "mode", "pay mode": "mode",
	"match status": "match_status", "exception": "exception", "bank line": "bank_line", "bill": "bill", "bill check": "bill_check",
}

// statementStatusColumns hold a payment's state; a failed, pending or
// refunded payment never reached the store and is left out.
var statementStatusColumns = map[string]bool{"status": true, "transaction status": true, "txn status": true, "payment status": true}

// statementTypeColumns say what a row is; a refund or chargeback row is left
// out the same way.
var statementTypeColumns = map[string]bool{"type": true, "transaction type": true, "txn type": true, "payment type": true}

// payoutUTRColumns name the payout's UTR outright - in a file that has one, a
// bare "UTR" column is the customer's payment reference.
var payoutUTRColumns = map[string]bool{"settlement utr": true, "settlement utr no": true, "payout utr": true, "settlement reference": true, "settlement ref": true}

func statementColumnKey(h string) string {
	h = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(h), "\xef\xbb\xbf"))
	var b strings.Builder
	gap := false
	for _, r := range h {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if gap && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			gap = false
		} else {
			gap = true
		}
	}
	return b.String()
}

// statementDay reads a statement's date, with or without a time after it.
func statementDay(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) > 10 && s[4] == '-' && (s[10] == 'T' || s[10] == ' ') {
		s = s[:10]
	}
	if d, ok := parseStatementDate(s); ok {
		return d, true
	}
	f := strings.Fields(s)
	for len(f) > 1 {
		last := f[len(f)-1]
		if !strings.Contains(last, ":") && !strings.EqualFold(last, "AM") && !strings.EqualFold(last, "PM") {
			break
		}
		f = f[:len(f)-1]
	}
	s = strings.Join(f, " ")
	if d, ok := parseStatementDate(s); ok {
		return d, true
	}
	for _, layout := range []string{"Jan 2, 2006", "Jan 02, 2006", "2 January 2006", "02-Jan-06"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}

// normalizeImportColumns is the per-doctype column step of the standard CSV
// import (import.go). For AcquirerSettlement it maps a provider statement's
// columns onto the fields, leaves out failed / refunded payments and columns
// it does not use, reads its dates and amounts, and fills what a UPI
// statement leaves implied: the mode (UPI), the net (gross less charges) and
// the settlement date (the payment's own day). Other doctypes pass through.
func normalizeImportColumns(doctype string, records [][]string) ([][]string, []string) {
	if doctype != AcquirerSettlementDoctype || len(records) == 0 {
		return records, nil
	}
	keys := make([]string, len(records[0]))
	payoutUTR := false
	for i, h := range records[0] {
		keys[i] = statementColumnKey(h)
		if payoutUTRColumns[keys[i]] {
			payoutUTR = true
		}
	}
	col := map[string]int{}
	var fields []string
	var src []int
	status, kind := -1, -1
	var unused []string
	for i, k := range keys {
		f, ok := acquirerColumnAliases[k]
		if ok && payoutUTR && f == "utr" && !payoutUTRColumns[k] {
			f = "payment_ref"
		}
		switch {
		case statementStatusColumns[k] && status < 0:
			status = i
		case statementTypeColumns[k] && kind < 0:
			kind = i
		case ok && f != "":
			if _, taken := col[f]; !taken {
				col[f] = len(fields)
				fields = append(fields, f)
				src = append(src, i)
			}
		case k != "":
			unused = append(unused, strings.TrimSpace(records[0][i]))
		}
	}
	_, hasRef := col["payment_ref"]
	add := func(f string) {
		if _, ok := col[f]; !ok {
			col[f] = len(fields)
			fields = append(fields, f)
			src = append(src, -1)
		}
	}
	if hasRef {
		add("mode")
	}
	_, hasSales := col["sales_date"]
	if hasSales {
		add("settlement_date")
	}
	_, hasGross := col["gross"]
	if hasGross {
		add("net")
	}
	out := [][]string{fields}
	var warnings []string
	if len(unused) > 0 {
		warnings = append(warnings, "Columns not used: "+strings.Join(unused, ", "))
	}
	dropped := 0
	get := func(rec []string, i int) string {
		if i >= 0 && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	for _, rec := range records[1:] {
		blank := true
		for _, c := range rec {
			if strings.TrimSpace(c) != "" {
				blank = false
				break
			}
		}
		if blank || strings.HasPrefix(strings.ToLower(get(rec, 0)), "total") {
			continue
		}
		st := strings.ToUpper(get(rec, status) + " " + get(rec, kind))
		bad := false
		for _, w := range []string{"FAIL", "PEND", "REFUND", "REVERS", "CANCEL", "DECLIN", "EXPIR", "INITIAT", "CHARGEBACK"} {
			if strings.Contains(st, w) {
				bad = true
			}
		}
		if j, ok := col["gross"]; ok {
			if g := get(rec, src[j]); strings.HasPrefix(g, "-") || strings.HasPrefix(g, "(") {
				bad = true // a refund listed as a negative payment
			}
		}
		if bad {
			dropped++
			continue
		}
		row := make([]string, len(fields))
		for j, f := range fields {
			row[j] = get(rec, src[j])
			switch f {
			case "sales_date", "settlement_date":
				if d, ok := statementDay(row[j]); ok {
					row[j] = d
				}
			case "gross", "fee", "net":
				if p, ok := parseAmountPaise(strings.NewReplacer("₹", "", "Rs.", "", "Rs", "").Replace(row[j]), false); ok && row[j] != "" {
					row[j] = fmt.Sprintf("%.2f", PaiseToRupees(p))
				}
			}
		}
		if hasRef && src[col["mode"]] < 0 {
			row[col["mode"]] = "UPI"
		}
		if hasSales && src[col["settlement_date"]] < 0 {
			row[col["settlement_date"]] = row[col["sales_date"]]
		}
		if hasGross && src[col["net"]] < 0 {
			fee := 0.0
			if j, ok := col["fee"]; ok {
				fee = numFromInterface(row[j])
			}
			row[col["net"]] = fmt.Sprintf("%.2f", round2(numFromInterface(row[col["gross"]])-fee))
		}
		out = append(out, row)
	}
	if dropped > 0 {
		warnings = append(warnings, fmt.Sprintf("%d failed, pending or refunded payment(s) left out", dropped))
	}
	return out, warnings
}

// --- matching ---------------------------------------------------------------

// upiBillRef is a till bill that took a UPI payment under a reference.
type upiBillRef struct {
	bill   string
	amount float64
}

// cartKindRupees is what a paid bill took by one money kind, from its
// allocation (the lines' tenders, in paise).
func cartKindRupees(data map[string]interface{}, kind string) float64 {
	var paise int64
	if lines, ok := data["allocation"].([]interface{}); ok {
		for _, l := range lines {
			if m, ok := l.(map[string]interface{}); ok {
				if tenders, ok := m["tenders"].(map[string]interface{}); ok {
					paise += int64(numFromInterface(tenders[kind]))
				}
			}
		}
	}
	return PaiseToRupees(paise)
}

// upiBillRefs is every UPI reference keyed on the till's paid bills between
// two days, with what each took: a split part its own amount, the Payment
// Mode part the bill's UPI money less the split UPI parts.
func upiBillRefs(schema, from, to string) (map[string][]upiBillRef, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND deleted_at IS NULL
		AND created_at >= public.erp_day_start($1::date) AND created_at < public.erp_day_start($2::date + 1)
		AND (data->>'payment_mode' = 'UPI' OR data->'tenders' @> '[{"mode":"UPI"}]'::jsonb)`, schema), from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]upiBillRef{}
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal(raw, &d) != nil {
			continue
		}
		split := 0.0
		if parts, ok := d["tenders"].([]interface{}); ok {
			for _, p := range parts {
				m, ok := p.(map[string]interface{})
				if !ok || strField(m, "mode") != "UPI" {
					continue
				}
				amt := numFromInterface(m["amount"])
				split += amt
				if ref := strings.ToUpper(strings.TrimSpace(strField(m, "reference"))); ref != "" {
					out[ref] = append(out[ref], upiBillRef{id, round2(amt)})
				}
			}
		}
		if strField(d, "payment_mode") == "UPI" {
			if ref := strings.ToUpper(strings.TrimSpace(strField(d, "payment_reference"))); ref != "" {
				out[ref] = append(out[ref], upiBillRef{id, round2(cartKindRupees(d, "UPI") - split)})
			}
		}
	}
	return out, rows.Err()
}

// rowNet is a statement row's net: as given, else gross less the charge.
func rowNet(d map[string]interface{}) float64 {
	net := numFromInterface(d["net"])
	if gross := numFromInterface(d["gross"]); net <= 0 && gross > 0 {
		net = round2(gross - numFromInterface(d["fee"]))
	}
	return net
}

// matchUPIStatementRows matches the pending rows that carry a customer's UPI
// ref: each to its till bill, then each payout (rows of one mode, settlement
// day and UTR) to its bank credit.
func matchUPIStatementRows(tenantID, schema string, list []acquirerRow, actor string, out *AcquirerMatchResult) error {
	if len(list) == 0 {
		return nil
	}
	from, to := "", ""
	widen := func(day string, before, after int) {
		t, err := time.Parse("2006-01-02", day)
		if err != nil {
			return
		}
		if f := t.AddDate(0, 0, -before).Format("2006-01-02"); from == "" || f < from {
			from = f
		}
		if e := t.AddDate(0, 0, after).Format("2006-01-02"); to == "" || e > to {
			to = e
		}
	}
	for _, r := range list {
		widen(strings.TrimSpace(strField(r.data, "sales_date")), 1, 1)
		widen(strings.TrimSpace(strField(r.data, "settlement_date")), 5, 1)
	}
	refs := map[string][]upiBillRef{}
	if from != "" {
		var err error
		if refs, err = upiBillRefs(schema, from, to); err != nil {
			return err
		}
	}
	patches := map[string]map[string]interface{}{}
	groups := map[string][]acquirerRow{}
	var order []string
	seen := map[string]string{}
	for _, r := range list {
		ref := strings.ToUpper(strings.TrimSpace(strField(r.data, "payment_ref")))
		patch := map[string]interface{}{}
		patches[r.id] = patch
		// The same payment twice - the file imported again, or a row
		// repeated - is never booked twice.
		var other string
		if prev, ok := seen[ref]; ok {
			other = prev
		} else {
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT o.id FROM %[1]s.documents o JOIN %[1]s.documents me ON me.id = $2
				WHERE o.doctype = $3 AND o.deleted_at IS NULL AND o.id <> me.id AND upper(btrim(COALESCE(o.data->>'payment_ref',''))) = $1
				  AND (o.data->>'match_status' = 'Matched' OR o.created_at < me.created_at OR (o.created_at = me.created_at AND o.id < me.id))
				ORDER BY o.created_at, o.id LIMIT 1`, schema), ref, r.id, AcquirerSettlementDoctype).Scan(&other)
		}
		if other != "" {
			patch["match_status"], patch["exception"] = "Exception", "the same UPI ref is on row "+other
			out.Exceptions = append(out.Exceptions, map[string]interface{}{"id": r.id, "settlement_date": r.data["settlement_date"], "mode": r.data["mode"],
				"net": rowNet(r.data), "utr": r.data["payment_ref"], "reason": patch["exception"]})
			continue
		}
		seen[ref] = r.id
		amount := numFromInterface(r.data["gross"])
		if amount <= 0 {
			amount = round2(rowNet(r.data) + numFromInterface(r.data["fee"]))
		}
		check := "OK"
		bills := refs[ref]
		switch {
		case len(bills) == 0:
			check, patch["bill"] = "no till bill has this UPI ref", ""
		case len(bills) > 1:
			check, patch["bill"] = fmt.Sprintf("this UPI ref is on bills %s and %s", bills[0].bill, bills[1].bill), bills[0].bill
		default:
			patch["bill"] = bills[0].bill
			if bills[0].amount > 0 && math.Abs(bills[0].amount-amount) > 0.005 {
				check = fmt.Sprintf("bill %s took %.2f by UPI; the statement says %.2f", bills[0].bill, bills[0].amount, amount)
			}
		}
		patch["bill_check"] = check
		if check != "OK" {
			out.BillExceptions = append(out.BillExceptions, map[string]interface{}{"id": r.id, "payment_ref": r.data["payment_ref"],
				"gross": amount, "bill": patch["bill"], "reason": check})
		}
		key := strings.ToUpper(strField(r.data, "mode")) + "|" + strings.TrimSpace(strField(r.data, "settlement_date")) + "|" + strings.TrimSpace(strField(r.data, "utr"))
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], r)
	}
	sort.Strings(order)
	for _, key := range order {
		rows := groups[key]
		first := rows[0].data
		net, fee := 0.0, 0.0
		for _, r := range rows {
			net += rowNet(r.data)
			fee += numFromInterface(r.data["fee"])
		}
		net, fee = round2(net), round2(fee)
		reason, lineID := matchPayoutToBank(tenantID, schema, strField(first, "mode"), strField(first, "settlement_date"), net, fee, actor)
		if reason != "" {
			out.Exceptions = append(out.Exceptions, map[string]interface{}{"id": rows[0].id, "settlement_date": first["settlement_date"], "mode": first["mode"],
				"net": net, "fee": fee, "utr": first["utr"], "reason": fmt.Sprintf("%s (%d payment(s))", reason, len(rows))})
		} else {
			out.Matched++
		}
		for _, r := range rows {
			p := patches[r.id]
			if reason != "" {
				p["match_status"], p["exception"] = "Exception", reason
			} else {
				p["match_status"], p["exception"], p["bank_line"] = "Matched", "", lineID
			}
		}
	}
	for _, r := range list {
		p := patches[r.id]
		raw, _ := json.Marshal(p)
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, status = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3`, schema),
			raw, p["match_status"], r.id); err != nil {
			return err
		}
	}
	return nil
}
