package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// Stage 148.20: dimensions on a journal line, and required dimensions by
// account range.
//
// A manual journal line may name its own cost centre, department, store,
// employee, tag and narration. At posting the line's value goes on its ledger
// row; a line without one keeps the voucher's, so every report that groups by
// cost centre or department already reads "the line's value, else the
// header's" from the row itself.
//
// JournalDimensionRule rows say which dimension an account range must carry
// (kind Required) and which accounts are exempt from all of them (kind Exempt,
// the one exception list). No rows = no check = today. A cost centre or
// department that is not Active (suspended, never deleted) is refused on a new
// line and stays on the rows already posted.

// PostingLine is one ledger row with its own dimensions; amounts in paise.
type PostingLine struct {
	Account    string
	Debit      int64
	Credit     int64
	CostCenter string
	Department string
	Store      string
	Employee   string
	Tag        string
	Narration  string
}

func (l JournalVoucherLine) hasOwnDimensions() bool {
	return l.CostCenter != "" || l.Department != "" || l.Store != "" || l.Employee != "" || l.Tag != "" || l.Narration != ""
}

// journalPostingLines is nil when no line carries a dimension, which keeps the
// posting on its existing one-row-per-account path.
func journalPostingLines(lines []JournalVoucherLine) []PostingLine {
	has := false
	for _, l := range lines {
		if l.hasOwnDimensions() {
			has = true
			break
		}
	}
	if !has {
		return nil
	}
	out := make([]PostingLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, PostingLine{Account: l.AccountCode, Debit: int64(l.Debit) * 100, Credit: int64(l.Credit) * 100,
			CostCenter: l.CostCenter, Department: l.Department, Store: l.Store, Employee: l.Employee, Tag: l.Tag, Narration: l.Narration})
	}
	return out
}

func dimErr(code, format string, args ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// dimDocUsable: a live document of the doctype; wantActive also asks for
// status Active.
func dimDocUsable(schema, doctype, id string, wantActive bool) bool {
	var status string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema), doctype, id).Scan(&status)
	if err != nil {
		return false
	}
	return !wantActive || status == "Active"
}

type journalDimensionRule struct {
	kind, from, to, dimension string
}

func loadJournalDimensionRules(schema string) []journalDimensionRule {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT COALESCE(data->>'kind',''), COALESCE(data->>'account_from',''), COALESCE(data->>'account_to',''), COALESCE(data->>'dimension','')
		   FROM %s.documents WHERE doctype = 'JournalDimensionRule' AND status = 'Active' AND deleted_at IS NULL`, schema))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []journalDimensionRule
	for rows.Next() {
		var r journalDimensionRule
		if rows.Scan(&r.kind, &r.from, &r.to, &r.dimension) == nil && r.from != "" {
			out = append(out, r)
		}
	}
	return out
}

// accountInRange: numeric when both ends and the code are numbers, otherwise
// by text. An empty "to" means the one account.
func accountInRange(code, from, to string) bool {
	if to == "" {
		to = from
	}
	c, e1 := strconv.Atoi(code)
	a, e2 := strconv.Atoi(from)
	b, e3 := strconv.Atoi(to)
	if e1 == nil && e2 == nil && e3 == nil {
		return c >= a && c <= b
	}
	return code >= from && code <= to
}

var journalDimensionLabels = map[string]string{
	"cost_center": "a cost centre", "department": "a department", "store": "a store", "employee": "an employee", "tag": "a tag",
}

func (l JournalVoucherLine) dimension(name, headerCC, headerDept string) string {
	switch name {
	case "cost_center":
		if l.CostCenter != "" {
			return l.CostCenter
		}
		return headerCC
	case "department":
		if l.Department != "" {
			return l.Department
		}
		return headerDept
	case "store":
		return l.Store
	case "employee":
		return l.Employee
	case "tag":
		return l.Tag
	}
	return ""
}

// checkJournalLineDimensions refuses a line whose dimension value is not
// usable (FIN-0951) or a line missing a dimension its account range requires
// (FIN-0950). A voucher with no line dimensions and no rules costs one query.
func checkJournalLineDimensions(schema string, lines []JournalVoucherLine, headerCC, headerDept string) error {
	for i, l := range lines {
		n := i + 1
		if l.CostCenter != "" && !dimDocUsable(schema, "CostCenter", l.CostCenter, true) {
			return dimErr("FIN-0951", "Line %d: the cost centre is not usable.", n)
		}
		if l.Department != "" && !dimDocUsable(schema, "Department", l.Department, true) {
			return dimErr("FIN-0951", "Line %d: the department is not usable.", n)
		}
		if l.Store != "" && !dimDocUsable(schema, "Location", l.Store, true) {
			return dimErr("FIN-0951", "Line %d: the store is not usable.", n)
		}
		if l.Employee != "" && !dimDocUsable(schema, "Employee", l.Employee, false) {
			return dimErr("FIN-0951", "Line %d: the employee is not usable.", n)
		}
		if len(l.Tag) > 40 || len(l.Narration) > 200 {
			return dimErr("FIN-0951", "Line %d: the tag or narration is too long.", n)
		}
	}
	rules := loadJournalDimensionRules(schema)
	if len(rules) == 0 {
		return nil
	}
	for i, l := range lines {
		exempt := false
		for _, r := range rules {
			if r.kind == "Exempt" && accountInRange(l.AccountCode, r.from, r.to) {
				exempt = true
				break
			}
		}
		if exempt {
			continue
		}
		for _, r := range rules {
			if r.kind != "Required" || r.dimension == "" || !accountInRange(l.AccountCode, r.from, r.to) {
				continue
			}
			label, known := journalDimensionLabels[r.dimension]
			if !known {
				continue
			}
			if l.dimension(r.dimension, headerCC, headerDept) == "" {
				return dimErr("FIN-0950", "Line %d: account %s needs %s.", i+1, l.AccountCode, label)
			}
		}
	}
	return nil
}

// insertPostingLinesTx writes opt.Lines as one ledger row each. It takes the
// attribution and time columns PostDoubleEntryTx has already worked out; the
// positional arguments $1-$12 are fixed and the optional ones follow them.
func insertPostingLinesTx(tx *sql.Tx, schema string, opt PostingOptions, docType, docID string, keyArg interface{}, foreign bool,
	debits, credits map[string]int64, attrCols, attrVals string, attrArgs []interface{}) error {
	if foreign {
		return dimErr("FIN-0952", "Line dimensions are not available on a voucher in another currency.")
	}
	var sumD, sumC, mapD, mapC int64
	for _, l := range opt.Lines {
		sumD += l.Debit
		sumC += l.Credit
	}
	for _, v := range debits {
		mapD += v
	}
	for _, v := range credits {
		mapC += v
	}
	if sumD != mapD || sumC != mapC {
		return fmt.Errorf("posting lines do not match the debits and credits")
	}
	nullable := func(s string) interface{} {
		if s == "" {
			return nil
		}
		return s
	}
	n := 12 + len(attrArgs)
	query := fmt.Sprintf(`INSERT INTO %s.gl_postings (account_code, debit, credit, document_type, document_id, idempotency_key, cost_center, department, entity, project, location, employee%s, tag, line_narration)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12%s, $%d, $%d)`, schema, attrCols, attrVals, n+1, n+2)
	for _, l := range opt.Lines {
		if l.Debit <= 0 && l.Credit <= 0 {
			continue
		}
		cc, dep := l.CostCenter, l.Department
		if cc == "" {
			cc = opt.CostCenter
		}
		if dep == "" {
			dep = opt.Department
		}
		args := append([]interface{}{l.Account, l.Debit, l.Credit, docType, docID, keyArg, nullable(cc), nullable(dep),
			nullable(opt.Entity), nullable(opt.Project), nullable(l.Store), nullable(l.Employee)}, attrArgs...)
		args = append(args, nullable(strings.TrimSpace(l.Tag)), nullable(strings.TrimSpace(l.Narration)))
		if _, err := tx.Exec(query, args...); err != nil {
			return fmt.Errorf("error posting line for account %s: %v", l.Account, err)
		}
	}
	return nil
}
