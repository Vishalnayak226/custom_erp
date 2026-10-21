package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 74.1.3 (BRD §3, §5, DEC-07, DEC-08, DEC-21): posting profiles - the
// accounts an economic event posts to, set by the client. A posting site asks
// ResolvePostingAccounts for its event with the accounts it has always used
// as the defaults; an Approved, in-date profile for the event overrides a side
// it names, the most specific profile winning (one naming the reason code
// beats one naming only the legal entity). With no profile nothing changes.

// Posting events (the PostingProfile.event options).
const (
	PostingEventAdvanceCollected     = "advance_collected"
	PostingEventAdvanceApplied       = "advance_applied"
	PostingEventAdvanceRefunded      = "advance_refunded"
	PostingEventStoreCreditIssued    = "store_credit_issued"
	PostingEventStoreCreditRedeemed  = "store_credit_redeemed"
	PostingEventStoreCreditExpired   = "store_credit_expired"
	PostingEventMaterialIssue        = "material_issue"
	PostingEventMaterialReturn       = "material_return"
	PostingEventAdjustmentGain       = "adjustment_gain"
	PostingEventAdjustmentLoss       = "adjustment_loss"
	PostingEventAdjustmentDamage     = "adjustment_damage"
	PostingEventAdjustmentRevalue    = "adjustment_revaluation"
	PostingEventAdjustmentExpiry     = "adjustment_expiry"
	PostingEventJobworkCharge        = "jobwork_charge"
	PostingEventJobworkLoss          = "jobwork_loss"
	PostingEventChallanNonReturnable = "challan_nonreturnable_issue"
)

var postingEvents = []string{
	PostingEventAdvanceCollected, PostingEventAdvanceApplied, PostingEventAdvanceRefunded,
	PostingEventStoreCreditIssued, PostingEventStoreCreditRedeemed, PostingEventStoreCreditExpired,
	PostingEventMaterialIssue, PostingEventMaterialReturn,
	PostingEventAdjustmentGain, PostingEventAdjustmentLoss, PostingEventAdjustmentDamage, PostingEventAdjustmentRevalue, PostingEventAdjustmentExpiry,
	PostingEventJobworkCharge, PostingEventJobworkLoss, PostingEventChallanNonReturnable,
}

// PostingContext is what a posting knows about itself for the match: blank
// fields match only a profile that leaves them blank too.
type PostingContext struct {
	LegalEntity string
	ReasonCode  string
	LossClass   string
	CostCenter  string
	On          time.Time // the posting date; zero = today
}

// PostingResolution is the accounts an event posts to and the profile that
// set them ("" = the event's own defaults).
type PostingResolution struct {
	Debit, Credit, Profile string
}

// ResolvePostingAccounts returns the debit and credit accounts for event.
func ResolvePostingAccounts(tenantID, event string, ctx PostingContext, defaultDebit, defaultCredit string) PostingResolution {
	out := PostingResolution{Debit: defaultDebit, Credit: defaultCredit}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out
	}
	on := ctx.On
	if on.IsZero() {
		on = time.Now()
	}
	day := on.Format("2006-01-02")
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		 WHERE doctype = 'PostingProfile' AND data->>'event' = $1 AND COALESCE(data->>'status', status) = 'Approved' AND deleted_at IS NULL
		 ORDER BY id`, schema), event)
	if err != nil {
		return out
	}
	defer rows.Close()
	best, bestScore, bestFrom := "", -1, ""
	var bestData map[string]interface{}
	for rows.Next() {
		var id, raw string
		if rows.Scan(&id, &raw) != nil {
			continue
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(raw), &data) != nil {
			continue
		}
		from, to := strField(data, "effective_from"), strField(data, "effective_to")
		if (from != "" && day < from) || (to != "" && day > to) {
			continue
		}
		score, ok := 0, true
		for _, m := range []struct {
			field, value string
			weight       int
		}{{"reason_code", ctx.ReasonCode, 8}, {"loss_class", ctx.LossClass, 4}, {"cost_center", ctx.CostCenter, 2}, {"legal_entity", ctx.LegalEntity, 1}} {
			switch v := strings.TrimSpace(strField(data, m.field)); {
			case v == "":
			case strings.EqualFold(v, strings.TrimSpace(m.value)):
				score += m.weight
			default:
				ok = false
			}
		}
		if !ok {
			continue
		}
		// The most specific wins; between equals, the one in force longest
		// ago gives way to the newer.
		if score > bestScore || (score == bestScore && from > bestFrom) {
			best, bestScore, bestFrom, bestData = firstNonEmpty(strField(data, "code"), id), score, from, data
		}
	}
	if bestData != nil {
		if a := strings.TrimSpace(strField(bestData, "debit_account")); a != "" {
			out.Debit = a
		}
		if a := strings.TrimSpace(strField(bestData, "credit_account")); a != "" {
			out.Credit = a
		}
		out.Profile = best
	}
	return out
}

// validatePostingProfileRules is a profile's save check: a known event, at
// least one account, every account in the chart, dates in order.
func validatePostingProfileRules(tenantID string, payload map[string]interface{}) error {
	if ev := strField(payload, "event"); !containsString(postingEvents, ev) {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Event", Message: fmt.Sprintf("%q is not a posting event", ev)}
	}
	debit, credit := strings.TrimSpace(strField(payload, "debit_account")), strings.TrimSpace(strField(payload, "credit_account"))
	if debit == "" && credit == "" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Debit account", Message: "name the debit account, the credit account or both - a profile with neither changes nothing"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	for _, a := range []struct{ label, code string }{{"Debit account", debit}, {"Credit account", credit}} {
		if a.code == "" {
			continue
		}
		var exists bool
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.gl_accounts WHERE account_code = $1)`, schema), a.code).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: a.label, Message: fmt.Sprintf("account %s is not in the chart of accounts", a.code)}
		}
	}
	if from, to := strField(payload, "effective_from"), strField(payload, "effective_to"); from != "" && to != "" && to < from {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Effective to", Message: "the profile ends before it starts"}
	}
	return nil
}

// locationLegalEntity is the legal entity a store belongs to ("" = none).
func locationLegalEntity(tenantID, location string) string {
	if strings.TrimSpace(location) == "" {
		return ""
	}
	loc, _, err := fetchDocData(tenantID, "Location", location)
	if err != nil {
		return ""
	}
	return strField(loc, "legal_entity")
}

// commonLineReason is the reason code every line of a document shares, "" when
// they differ - a document posts its total once, so a reason-specific profile
// applies only when the whole document carries that reason.
func commonLineReason(lines []map[string]interface{}) string {
	common := ""
	for i, l := range lines {
		r := strings.TrimSpace(strField(l, "reason_code"))
		if i == 0 {
			common = r
		} else if r != common {
			return ""
		}
	}
	return common
}
