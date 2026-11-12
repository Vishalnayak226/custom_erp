package engines

import (
	"custom_erp/db"
	"errors"
	"sort"
	"strings"
	"testing"
)

// 49.5.3 (rest): the money / stock-adjacent doctypes the first pass left
// failing open are strict, every status their enum declares is covered (a
// generic rule in or out, the create status, or engine-only), and the moves
// that would re-open money or stock are refused on a plain save.
// db/migrations/migrations_stage130_16_4_strict_money_stock_transitions.sql.

var stage4953bMatrix = map[string]struct {
	rules      map[string]string // "from>to" -> requires_reason_code
	engineOnly []string
	start      string // the status a document is created in
}{
	"LoyaltyRedemptionRequest": {approval4953(), []string{"Redeemed"}, "Draft"},
	"PriceListVersion":         {approval4953(), []string{"Superseded"}, "Draft"},
	"PostingProfile":           {approval4953("Draft>Inactive:No", "Approved>Inactive:No", "Rejected>Inactive:No", "Inactive>Draft:No"), nil, "Draft"},
	"Budget":                   {approval4953(), nil, "Draft"},
	"PrepaidExpenseSchedule":   {approval4953(), []string{"Completed"}, "Draft"},
	"CapturedCharge":           {rules4953("Captured>Cancelled:No"), []string{"Billed"}, "Captured"},
	"Voucher": {rules4953("Active>Inactive:No", "Inactive>Active:No", "Active>Expired:No", "Inactive>Expired:No", "Expired>Active:No", "Expired>Inactive:No"),
		[]string{"Exhausted"}, "Active"},
	"SubcontractOrder":        {rules4953("Draft>Cancelled:No"), []string{"Sent", "Challan Pending", "Partly Received", "Received"}, "Draft"},
	"PhysicalInventory":       {rules4953(), []string{"Reconciling", "Closed", "Cancelled"}, "Counting"},
	"BundleAssembly":          {rules4953(), []string{"Completed", "Failed"}, "Draft"},
	"ExchangeCredit":          {rules4953(), []string{"Used"}, "Open"},
	"DeferredRevenueSchedule": {rules4953(), []string{"Completed"}, "Active"},
	"OMSBill":                 {rules4953(), []string{"Billed"}, "Issued"},
	"LogisticsBooking":        {rules4953(), []string{"Manifested", "Handed Over", "In-Transit", "Delivered", "RTO"}, "AWB Assigned"},
}

func TestStage4953bMoneyStockDoctypesAreStrictAndCovered(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var seeded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.documents WHERE id = 'STR-Voucher-Active-Inactive')`).Scan(&seeded)
	if !seeded {
		t.Skip("db/migrations/migrations_stage130_16_4_strict_money_stock_transitions.sql (Stage 49.5.3) has not been applied")
	}
	names := make([]string, 0, len(stage4953bMatrix))
	for n := range stage4953bMatrix {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, doctype := range names {
		want := stage4953bMatrix[doctype]
		var strict bool
		var options string
		if err := db.DB.QueryRow(`SELECT m.strict_status_transitions, f.options FROM `+schema+`.doctype_meta m JOIN `+schema+`.doctype_fields f ON f.doctype_name = m.name AND f.fieldname = 'status' WHERE m.name = $1`, doctype).Scan(&strict, &options); err != nil {
			t.Fatalf("%s: %v", doctype, err)
		}
		if !strict {
			t.Errorf("%s is not strict", doctype)
		}
		rows, err := db.DB.Query(`SELECT data->>'from_status', data->>'to_status', COALESCE(data->>'requires_reason_code', 'No') FROM `+schema+`.documents
			WHERE doctype = 'StatusTransitionRule' AND status = 'Active' AND deleted_at IS NULL AND data->>'entity' = $1`, doctype)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for rows.Next() {
			var from, to, reason string
			_ = rows.Scan(&from, &to, &reason)
			got[from+">"+to] = reason
		}
		rows.Close()
		for k, v := range want.rules {
			if got[k] != v {
				t.Errorf("%s: rule %s wants reason=%s, got %q", doctype, k, v, got[k])
			}
		}
		for k := range got {
			if _, ok := want.rules[k]; !ok {
				t.Errorf("%s: unexpected rule %s", doctype, k)
			}
		}
		// Every declared status is covered, so no live document is stuck.
		covered := map[string]bool{want.start: true}
		for k := range want.rules {
			ft := strings.SplitN(k, ">", 2)
			covered[ft[0]], covered[ft[1]] = true, true
		}
		for _, s := range want.engineOnly {
			covered[s] = true
		}
		declared := map[string]bool{}
		for _, s := range strings.Split(options, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			declared[s] = true
			if !covered[s] {
				t.Errorf("%s: declared status %q has no rule, is not the create status and is not engine-only - a document there would be stuck", doctype, s)
			}
		}
		for s := range covered {
			if !declared[s] {
				t.Errorf("%s: the matrix names %q, which its status field does not declare (%s)", doctype, s, options)
			}
		}
		// Every rule passes (with a reason where one is needed).
		for k, reason := range want.rules {
			ft := strings.SplitN(k, ">", 2)
			payload := map[string]interface{}{}
			if reason == "Yes" {
				if err := ValidateStatusTransition("default", doctype, ft[0], ft[1], payload); err == nil {
					t.Errorf("%s %s without a reason must be refused", doctype, k)
				}
				payload["reason_code"] = "TEST-4953b"
			}
			if err := ValidateStatusTransition("default", doctype, ft[0], ft[1], payload); err != nil {
				t.Errorf("%s %s: %v", doctype, k, err)
			}
		}
		// A plain save cannot claim an engine-only status, from any declared one.
		for _, s := range want.engineOnly {
			for from := range declared {
				if from == s {
					continue
				}
				err := ValidateStatusTransition("default", doctype, from, s, map[string]interface{}{"reason_code": "TEST-4953b"})
				var verr *ValidationError
				if !errors.As(err, &verr) || verr.Code != "GLOBAL-0019" {
					t.Errorf("%s: a plain save %s -> %s must be refused with GLOBAL-0019, got %v", doctype, from, s, err)
				}
			}
		}
	}
}

// The moves that would put money or stock back in play are refused.
func TestStage4953bReopeningMovesAreRefused(t *testing.T) {
	spInitDB()
	var seeded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM tenant_default.documents WHERE id = 'STR-Voucher-Active-Inactive')`).Scan(&seeded)
	if !seeded {
		t.Skip("Stage 49.5.3's second migration has not been applied")
	}
	for _, m := range [][3]string{
		{"Voucher", "Exhausted", "Active"},                 // a used-up single-use code used again
		{"CapturedCharge", "Billed", "Captured"},           // a billed charge billed again
		{"DeferredRevenueSchedule", "Completed", "Active"}, // revenue recognised twice
		{"PrepaidExpenseSchedule", "Completed", "Approved"},
		{"PostingProfile", "Inactive", "Approved"}, // back to live without approval
		{"ExchangeCredit", "Used", "Open"},         // an exchange credit spent twice
		{"LoyaltyRedemptionRequest", "Redeemed", "Approved"},
		{"SubcontractOrder", "Sent", "Cancelled"}, // goods out with the job worker
		{"PhysicalInventory", "Closed", "Counting"},
		{"LogisticsBooking", "Delivered", "AWB Assigned"},
	} {
		err := ValidateStatusTransition("default", m[0], m[1], m[2], map[string]interface{}{"reason_code": "TEST-4953b"})
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != "GLOBAL-0019" {
			t.Errorf("%s %s -> %s by a save: %v, want GLOBAL-0019", m[0], m[1], m[2], err)
		}
	}
}
