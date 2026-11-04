package engines

import (
	"custom_erp/db"
	"errors"
	"sort"
	"strings"
	"testing"
)

// 49.5.3 (fourth batch): 10 more doctypes are strict and every status their
// enum declares is covered. db/migrations_stage146_99_strict_transitions_49_5_3d.sql.

var stage4953dMatrix = map[string]struct {
	rules      map[string]string
	engineOnly []string
	start      string
}{
	"MRPChange":             {rules4953(), []string{"Scheduled", "Applied", "Cancelled"}, "Draft"},
	"CertificateOfAnalysis": {rules4953(), []string{"Released", "Rejected"}, "Draft"},
	"NonConformanceReport":  {rules4953(), []string{"Investigating", "CorrectiveActionPlanned", "Closed"}, "Draft"},
	"MaintenanceOrder":      {rules4953(), []string{"Scheduled", "InProgress", "Completed", "Cancelled"}, "Draft"},
	"FulfillmentTask":       {rules4953(), []string{"Picking", "Packed", "Dispatched", "Rejected"}, "Pending"},
	"SupplyProposal":        {rules4953(), []string{"Converted", "Rejected"}, "Proposed"},
	"CrossDockPlan":         {rules4953("Planned>Cancelled:No"), []string{"Fulfilled"}, "Planned"},
	"NDRCase":               {rules4953(), []string{"Reattempt Scheduled", "Closed", "RTO"}, "Open"},
	"YardCheckIn":           {rules4953("InYard>AtDoor:No", "AtDoor>Departed:No", "InYard>Departed:No"), nil, "InYard"},
	"LoyaltyPointTransfer":  {approval4953(), []string{"Posted"}, "Awaiting OTP"},
}

func TestStage4953dFourthBatchIsStrictAndCovered(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var seeded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.documents WHERE id = 'STR-YardCheckIn-InYard-AtDoor')`).Scan(&seeded)
	if !seeded {
		t.Skip("db/migrations_stage146_99_strict_transitions_49_5_3d.sql (Stage 49.5.3) has not been applied")
	}
	names := make([]string, 0, len(stage4953dMatrix))
	for n := range stage4953dMatrix {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, doctype := range names {
		want := stage4953dMatrix[doctype]
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
				payload["reason_code"] = "TEST-4953d"
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
				err := ValidateStatusTransition("default", doctype, from, s, map[string]interface{}{"reason_code": "TEST-4953d"})
				var verr *ValidationError
				if !errors.As(err, &verr) || verr.Code != "GLOBAL-0019" {
					t.Errorf("%s: a plain save %s -> %s must be refused with GLOBAL-0019, got %v", doctype, from, s, err)
				}
			}
		}
	}
}

// The moves that would redo a finished event are refused on a plain save.
func TestStage4953dRedoMovesAreRefused(t *testing.T) {
	spInitDB()
	var seeded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM tenant_default.documents WHERE id = 'STR-YardCheckIn-InYard-AtDoor')`).Scan(&seeded)
	if !seeded {
		t.Skip("Stage 49.5.3 fourth migration has not been applied")
	}
	for _, m := range [][3]string{
		{"MRPChange", "Applied", "Draft"},
		{"MRPChange", "Cancelled", "Scheduled"},
		{"CertificateOfAnalysis", "Released", "Draft"},
		{"NonConformanceReport", "Closed", "Draft"},
		{"MaintenanceOrder", "Completed", "Draft"},
		{"FulfillmentTask", "Dispatched", "Pending"},
		{"SupplyProposal", "Converted", "Proposed"},
		{"CrossDockPlan", "Fulfilled", "Planned"},
		{"CrossDockPlan", "Cancelled", "Planned"},
		{"NDRCase", "Closed", "Open"},
		{"YardCheckIn", "Departed", "InYard"},
		{"LoyaltyPointTransfer", "Posted", "Approved"},
	} {
		err := ValidateStatusTransition("default", m[0], m[1], m[2], map[string]interface{}{"reason_code": "TEST-4953d"})
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != "GLOBAL-0019" {
			t.Errorf("%s %s -> %s by a save: %v, want GLOBAL-0019", m[0], m[1], m[2], err)
		}
	}
}
