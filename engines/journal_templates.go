package engines

import (
	"encoding/json"
	"math"
	"strings"
)

// Stage 148.21: journal templates, and the checks both new Stage 148.20/21
// record types run when saved through the generic API (the shared exit in
// currency.go's dispatcher).
//
// A JournalTemplate is a saved line layout: fixed amounts, or a percent split
// of an amount typed when the template is used. It is its own doctype and
// nothing reads it but the Journal Vouchers screen, which copies it into a
// new voucher; a template never posts and never becomes a voucher on its own.

type journalTemplateLine struct {
	AccountCode string  `json:"account_code"`
	Debit       float64 `json:"debit"`
	Credit      float64 `json:"credit"`
	DebitPct    float64 `json:"debit_pct"`
	CreditPct   float64 `json:"credit_pct"`
}

// ValidateJournalTemplateDocument: at least two lines, each with an account;
// Amount templates balance, Percent templates split 100 on each side.
func ValidateJournalTemplateDocument(payload map[string]interface{}) error {
	var lines []journalTemplateLine
	if err := json.Unmarshal([]byte(pimString(payload["lines"])), &lines); err != nil {
		return dimErr("FIN-0953", "The template lines are not readable.")
	}
	if len(lines) < 2 {
		return dimErr("FIN-0953", "A template needs at least two lines.")
	}
	var d, c, dp, cp float64
	for _, l := range lines {
		if strings.TrimSpace(l.AccountCode) == "" {
			return dimErr("FIN-0953", "Every template line needs an account.")
		}
		if l.Debit < 0 || l.Credit < 0 || l.DebitPct < 0 || l.CreditPct < 0 {
			return dimErr("FIN-0953", "Template amounts cannot be negative.")
		}
		d, c, dp, cp = d+l.Debit, c+l.Credit, dp+l.DebitPct, cp+l.CreditPct
	}
	if pimString(payload["split_mode"]) == "Percent" {
		if math.Abs(dp-100) > 0.005 || math.Abs(cp-100) > 0.005 {
			return dimErr("FIN-0953", "The percentages must add up to 100 on each side.")
		}
	} else if d != c || d == 0 {
		return dimErr("FIN-0953", "The template debits and credits must be equal.")
	}
	return nil
}

// ValidateJournalDimensionRuleDocument: a Required rule names its dimension and
// the range runs upward.
func ValidateJournalDimensionRuleDocument(payload map[string]interface{}) error {
	from, to := strings.TrimSpace(pimString(payload["account_from"])), strings.TrimSpace(pimString(payload["account_to"]))
	if from == "" {
		return nil // the mandatory-field check covers it
	}
	if pimString(payload["kind"]) == "Required" && journalDimensionLabels[pimString(payload["dimension"])] == "" {
		return dimErr("FIN-0954", "A required rule needs its dimension.")
	}
	if to != "" && !accountInRange(to, from, to) {
		return dimErr("FIN-0954", "The account range must run from low to high.")
	}
	return nil
}
