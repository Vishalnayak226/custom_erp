package engines

import (
	"custom_erp/db"
	"errors"
	"sort"
	"strings"
	"testing"
)

// 49.5.3 (sixth batch): 10 more doctypes (money / stock) are strict and every status their
// enum declares is covered. db/migrations/migrations_stage148_99_strict_transitions_49_5_3f.sql.

var stage4953fMatrix = map[string]struct {
	rules      map[string]string
	engineOnly []string
	start      string
}{
	"CreditHoldRelease":      {approval4953(), nil, "Draft"},
	"GLAccountChange":        {approval4953(), nil, "Draft"},
	"VendorChange":           {approval4953(), nil, "Draft"},
	"Customer":               {rules4953("Active>Inactive:No", "Inactive>Active:No"), []string{"Merged"}, "Active"},
	"VendorAdvance":          {rules4953(), []string{"Part Applied", "Applied", "Refunded", "Cancelled"}, "Open"},
	"TradeChargeEntry":       {rules4953(), []string{"Posted", "Trued up", "Reversed", "Skipped"}, "Estimated"},
	"MaterialReturn":         {rules4953(), nil, "Posted"},
	"POSCashMovement":        {rules4953(), nil, "Posted"},
	"ConsumptionEntry":       {rules4953(), nil, "Posted"},
	"StorageBalanceSnapshot": {rules4953(), nil, "Captured"},
}

func TestStage4953fSixthBatchIsStrictAndCovered(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var seeded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.documents WHERE id = 'STR-VendorChange-Draft-Pending Approval')`).Scan(&seeded)
	if !seeded {
		t.Skip("db/migrations/migrations_stage148_99_strict_transitions_49_5_3f.sql (Stage 49.5.3) has not been applied")
	}
	names := make([]string, 0, len(stage4953fMatrix))
	for n := range stage4953fMatrix {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, doctype := range names {
		want := stage4953fMatrix[doctype]
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
		for k, reason := range want.rules {
			ft := strings.SplitN(k, ">", 2)
			payload := map[string]interface{}{}
			if reason == "Yes" {
				if err := ValidateStatusTransition("default", doctype, ft[0], ft[1], payload); err == nil {
					t.Errorf("%s %s without a reason must be refused", doctype, k)
				}
				payload["reason_code"] = "TEST-4953f"
			}
			if err := ValidateStatusTransition("default", doctype, ft[0], ft[1], payload); err != nil {
				t.Errorf("%s %s: %v", doctype, k, err)
			}
		}
		for _, s := range want.engineOnly {
			for from := range declared {
				if from == s {
					continue
				}
				if _, isRule := want.rules[from+">"+s]; isRule {
					continue
				}
				err := ValidateStatusTransition("default", doctype, from, s, map[string]interface{}{"reason_code": "TEST-4953f"})
				var verr *ValidationError
				if !errors.As(err, &verr) || verr.Code != "GLOBAL-0019" {
					t.Errorf("%s: a plain save %s -> %s must be refused with GLOBAL-0019, got %v", doctype, from, s, err)
				}
			}
		}
	}
}

// The moves that would redo a finished event are refused on a plain save.
func TestStage4953fRedoMovesAreRefused(t *testing.T) {
	spInitDB()
	var seeded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM tenant_default.documents WHERE id = 'STR-VendorChange-Draft-Pending Approval')`).Scan(&seeded)
	if !seeded {
		t.Skip("Stage 49.5.3 sixth migration has not been applied")
	}
	for _, m := range [][3]string{
		{"CreditHoldRelease", "Approved", "Draft"},
		{"GLAccountChange", "Approved", "Draft"},
		{"VendorChange", "Rejected", "Approved"},
		{"Customer", "Merged", "Active"},
		{"Customer", "Active", "Merged"},
		{"VendorAdvance", "Applied", "Open"},
		{"VendorAdvance", "Open", "Refunded"},
		{"TradeChargeEntry", "Reversed", "Estimated"},
		{"TradeChargeEntry", "Estimated", "Posted"},
	} {
		err := ValidateStatusTransition("default", m[0], m[1], m[2], map[string]interface{}{"reason_code": "TEST-4953f"})
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != "GLOBAL-0019" {
			t.Errorf("%s %s -> %s by a save: %v, want GLOBAL-0019", m[0], m[1], m[2], err)
		}
	}
}
