package engines

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 170.3: bank statement file import.
//
// A CSV or MT940 file becomes BankStatementLine documents on one bank account.
// Each line's id comes from a stable hash of account + date + amount + reference
// + running balance (+ how many identical lines came before it in the file), so
// loading the same file again - or an overlapping one - skips what is already
// there and never doubles a line.

// BankImportResult is what one import loaded and skipped.
type BankImportResult struct {
	Format  string           `json:"format"`
	Loaded  int              `json:"loaded"`
	Skipped int              `json:"skipped"`
	Errors  []string         `json:"errors"`
	Lines   []BankImportLine `json:"lines"`
}

// BankImportLine is one file line and what happened to it.
type BankImportLine struct {
	Row       int     `json:"row"`
	ID        string  `json:"id"`
	TxnDate   string  `json:"txn_date"`
	Reference string  `json:"reference_number"`
	Amount    float64 `json:"amount"`
	DrCr      string  `json:"dr_cr"`
	Result    string  `json:"result"` // Loaded | Skipped
}

type parsedStatementLine struct {
	row         int
	date        string
	description string
	reference   string
	amountPaise int64
	drCr        string
	balance     string
}

func importErr(code, msg string) error { return &ValidationError{Code: code, Message: msg} }

func parseStatementDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02", "02/01/2006", "02-01-2006", "2/1/2006", "02-Jan-2006", "2 Jan 2006", "02 Jan 2006", "02/01/06", "20060102"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}

// parseAmountPaise reads 1,234.50 / 1234,50 (MT940) / (12.00) as paise, >= 0.
func parseAmountPaise(s string, commaDecimal bool) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	s = strings.NewReplacer("Rs.", "", "INR", "", " ", "", "-", "", "(", "", ")", "").Replace(s)
	if commaDecimal {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.ReplaceAll(s, ",", ".")
	} else {
		s = strings.ReplaceAll(s, ",", "")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0, false
	}
	return RupeesToPaise(f), true
}

func csvHeaderIndex(header []string) map[string]int {
	idx := map[string]int{}
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\xef\xbb\xbf")))
		h = strings.NewReplacer(" ", "_", "/", "_", ".", "").Replace(h)
		idx[h] = i
	}
	return idx
}

func pickCol(idx map[string]int, rec []string, names ...string) string {
	for _, n := range names {
		if i, ok := idx[n]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
	}
	return ""
}

// parseStatementCSV reads a bank CSV with a header row. Columns (any order):
// date, description / narration, reference, debit + credit or amount (+ dr_cr),
// balance.
func parseStatementCSV(content string) ([]parsedStatementLine, []string, error) {
	r := csv.NewReader(strings.NewReader(content))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		return nil, nil, importErr("FIN-1704", "the file is empty or not a CSV")
	}
	idx := csvHeaderIndex(header)
	hasDate := false
	for _, n := range []string{"date", "txn_date", "transaction_date", "value_date"} {
		if _, ok := idx[n]; ok {
			hasDate = true
		}
	}
	if !hasDate {
		return nil, nil, importErr("FIN-1704", "the CSV needs a date column")
	}
	var out []parsedStatementLine
	var errs []string
	row := 1
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		row++
		if err != nil {
			errs = append(errs, fmt.Sprintf("row %d: unreadable", row))
			continue
		}
		blank := true
		for _, c := range rec {
			if strings.TrimSpace(c) != "" {
				blank = false
			}
		}
		if blank {
			continue
		}
		date, ok := parseStatementDate(pickCol(idx, rec, "date", "txn_date", "transaction_date", "value_date"))
		if !ok {
			errs = append(errs, fmt.Sprintf("row %d: date not understood", row))
			continue
		}
		var paise int64
		var drCr string
		debit, okD := parseAmountPaise(pickCol(idx, rec, "debit", "withdrawal", "withdrawals", "dr"), false)
		credit, okC := parseAmountPaise(pickCol(idx, rec, "credit", "deposit", "deposits", "cr"), false)
		if !okD || !okC {
			errs = append(errs, fmt.Sprintf("row %d: amount not understood", row))
			continue
		}
		switch {
		case debit > 0 && credit > 0:
			errs = append(errs, fmt.Sprintf("row %d: both debit and credit filled", row))
			continue
		case debit > 0:
			paise, drCr = debit, "Debit"
		case credit > 0:
			paise, drCr = credit, "Credit"
		default:
			raw := pickCol(idx, rec, "amount")
			amt, ok := parseAmountPaise(raw, false)
			if !ok || amt == 0 {
				errs = append(errs, fmt.Sprintf("row %d: no amount", row))
				continue
			}
			paise = amt
			switch strings.ToLower(pickCol(idx, rec, "dr_cr", "type", "drcr")) {
			case "cr", "credit", "c":
				drCr = "Credit"
			case "dr", "debit", "d":
				drCr = "Debit"
			default:
				if strings.Contains(raw, "-") {
					drCr = "Debit"
				} else {
					errs = append(errs, fmt.Sprintf("row %d: say Debit or Credit", row))
					continue
				}
			}
		}
		out = append(out, parsedStatementLine{row: row, date: date,
			description: pickCol(idx, rec, "description", "narration", "particulars", "details"),
			reference:   pickCol(idx, rec, "reference", "reference_number", "ref", "chq_no", "cheque_no", "utr"),
			amountPaise: paise, drCr: drCr, balance: strings.ReplaceAll(pickCol(idx, rec, "balance", "running_balance"), ",", "")})
	}
	return out, errs, nil
}

// parseStatementMT940 reads the :60F:, :61: and :86: tags of an MT940 file.
func parseStatementMT940(content string) ([]parsedStatementLine, []string, error) {
	content = strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	if !strings.Contains(content, ":61:") {
		return nil, nil, importErr("FIN-1704", "no :61: transaction lines found - is this an MT940 file?")
	}
	var out []parsedStatementLine
	var errs []string
	var balance int64
	haveBalance := false
	lines := strings.Split(content, "\n")
	row := 0
	for i := 0; i < len(lines); i++ {
		ln := strings.TrimRight(lines[i], " ")
		if strings.HasPrefix(ln, ":60F:") || strings.HasPrefix(ln, ":60M:") {
			v := ln[5:]
			if len(v) > 10 {
				if p, ok := parseAmountPaise(v[10:], true); ok {
					balance, haveBalance = p, true
					if v[0] == 'D' {
						balance = -p
					}
				}
			}
			continue
		}
		if !strings.HasPrefix(ln, ":61:") {
			continue
		}
		row++
		v := ln[4:]
		if len(v) < 8 {
			errs = append(errs, fmt.Sprintf("entry %d: :61: too short", row))
			continue
		}
		date, ok := parseStatementDate("20" + v[:6])
		if !ok {
			errs = append(errs, fmt.Sprintf("entry %d: date not understood", row))
			continue
		}
		rest := v[6:]
		if len(rest) >= 4 && rest[0] >= '0' && rest[0] <= '9' { // optional entry date MMDD
			rest = rest[4:]
		}
		drCr := "Credit"
		sign := int64(1)
		switch {
		case strings.HasPrefix(rest, "RC"):
			rest = rest[2:]
			drCr, sign = "Debit", -1
		case strings.HasPrefix(rest, "RD"):
			rest = rest[2:]
		case strings.HasPrefix(rest, "C"):
			rest = rest[1:]
		case strings.HasPrefix(rest, "D"):
			rest = rest[1:]
			drCr, sign = "Debit", -1
		default:
			errs = append(errs, fmt.Sprintf("entry %d: no debit / credit mark", row))
			continue
		}
		if len(rest) > 0 && rest[0] >= 'A' && rest[0] <= 'Z' { // funds code letter
			rest = rest[1:]
		}
		j := 0
		for j < len(rest) && (rest[j] == ',' || (rest[j] >= '0' && rest[j] <= '9')) {
			j++
		}
		paise, ok := parseAmountPaise(rest[:j], true)
		if !ok || paise == 0 {
			errs = append(errs, fmt.Sprintf("entry %d: amount not understood", row))
			continue
		}
		after := rest[j:]
		ref := ""
		if len(after) > 4 { // transaction type (N + 3 chars), then the reference up to //
			ref = after[4:]
			if k := strings.Index(ref, "//"); k >= 0 {
				ref = ref[:k]
			}
		}
		desc := ""
		for i+1 < len(lines) && !strings.HasPrefix(lines[i+1], ":61:") && !strings.HasPrefix(lines[i+1], ":62") && !strings.HasPrefix(lines[i+1], "-") {
			i++
			nx := strings.TrimSpace(lines[i])
			if strings.HasPrefix(nx, ":86:") {
				desc = strings.TrimSpace(nx[4:])
				continue
			}
			if strings.HasPrefix(nx, ":") {
				continue
			}
			if desc != "" && nx != "" {
				desc += " " + nx
			}
		}
		bal := ""
		if haveBalance {
			balance += sign * paise
			bal = strconv.FormatFloat(PaiseToRupees(balance), 'f', 2, 64)
		}
		out = append(out, parsedStatementLine{row: row, date: date, description: desc, reference: strings.TrimSpace(ref),
			amountPaise: paise, drCr: drCr, balance: bal})
	}
	return out, errs, nil
}

func statementLineID(account string, l parsedStatementLine, occurrence int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%s|%s|%s|%d", account, l.date, l.amountPaise, l.drCr, l.reference, l.balance, occurrence)))
	return "BSL-" + strings.ToUpper(hex.EncodeToString(sum[:8]))
}

// ImportBankStatement loads a CSV or MT940 statement onto a bank account.
// Lines already loaded (same hash) are skipped; unreadable rows are listed in
// Errors and do not stop the rest.
func ImportBankStatement(tenantID, bankAccountID, format, content, actor string) (*BankImportResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if _, err := bankAccountGLAccount(schema, bankAccountID); err != nil {
		return nil, err
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		if strings.Contains(content, ":61:") {
			format = "mt940"
		} else {
			format = "csv"
		}
	}
	var parsed []parsedStatementLine
	var errs []string
	switch format {
	case "csv":
		parsed, errs, err = parseStatementCSV(content)
	case "mt940", "sta":
		format = "mt940"
		parsed, errs, err = parseStatementMT940(content)
	default:
		return nil, importErr("FIN-1704", "the format must be CSV or MT940")
	}
	if err != nil {
		return nil, err
	}
	if len(parsed) == 0 && len(errs) == 0 {
		return nil, importErr("FIN-1704", "the file has no statement lines")
	}
	res := &BankImportResult{Format: format, Errors: errs, Lines: []BankImportLine{}}
	if res.Errors == nil {
		res.Errors = []string{}
	}
	batch := "IMP-" + time.Now().Format("20060102-150405")
	seen := map[string]int{}
	for _, l := range parsed {
		key := fmt.Sprintf("%s|%s|%d|%s|%s", l.date, l.drCr, l.amountPaise, l.reference, l.balance)
		occ := seen[key]
		seen[key]++
		id := statementLineID(bankAccountID, l, occ)
		data := map[string]interface{}{
			"bank_account": bankAccountID, "txn_date": l.date, "description": l.description, "reference_number": l.reference,
			"amount": PaiseToRupees(l.amountPaise), "dr_cr": l.drCr, "match_status": "Unmatched",
			"import_batch": batch, "import_format": format,
		}
		if l.balance != "" {
			data["running_balance"] = l.balance
		}
		b, _ := json.Marshal(data)
		tag, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'BankStatementLine', $2, 'Active', $3)
			ON CONFLICT (id) DO NOTHING`, schema), id, string(b), actor)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("row %d: %v", l.row, err))
			continue
		}
		n, _ := tag.RowsAffected()
		line := BankImportLine{Row: l.row, ID: id, TxnDate: l.date, Reference: l.reference, Amount: PaiseToRupees(l.amountPaise), DrCr: l.drCr, Result: "Loaded"}
		if n == 0 {
			line.Result = "Skipped"
			res.Skipped++
		} else {
			res.Loaded++
		}
		res.Lines = append(res.Lines, line)
	}
	LogAuditEvent(tenantID, actor, "BANK_STATEMENT_IMPORTED", "SUCCESS", fmt.Sprintf("Bank statement %s on %s: %d loaded, %d skipped, %d errors", format, bankAccountID, res.Loaded, res.Skipped, len(res.Errors)))
	return res, nil
}
