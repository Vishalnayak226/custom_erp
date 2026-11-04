package engines

import (
	"custom_erp/db"
	"errors"
	"sort"
	"strings"
	"testing"
)

// 49.5.3 (fifth batch): 10 more doctypes are strict and every status their
// enum declares is covered. db/migrations_stage146_100_strict_transitions_49_5_3e.sql.

var stage4953eMatrix = map[string]struct {
	rules      map[string]string
	engineOnly []string
	start      string
}{
	"GateEntry":              {rules4953("Open>Cancelled:No"), []string{"GRN Made"}, "Open"},
	"POSCostingGap":          {rules4953("Open>Resolved:No"), nil, "Open"},
	"POSOfflineQueueGap":     {rules4953("Open>Reviewed:No"), nil, "Open"},
	"POSOfflineSyncVariance": {rules4953("Open>Reviewed:No"), nil, "Open"},
	"FloorAssistRequest":     {rules4953("Open>Acknowledged:No", "Open>Resolved:No", "Acknowledged>Resolved:No"), nil, "Open"},
	"ItemException":          {rules4953("Open>Resolved:No", "Logged>Resolved:No"), []string{"Logged"}, "Open"},
	"ChannelSKUException":    {rules4953("Open>Resolved:No"), nil, "Open"},
	"StoreLabelRequest":      {rules4953("Requested>Cancelled:No"), []string{"Printed"}, "Requested"},
	"Hold":                   {rules4953(), []string{"Released"}, "Active"},
}

func TestStage4953eFifthBatchIsStrictAndCovered(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var seeded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.documents WHERE id = 'STR-FloorAssistRequest-Open-Acknowledged')`).Scan(&seeded)
	if !seeded {
		t.Skip("db/migrations_stage146_100_strict_transitions_49_5_3e.sql (Stage 49.5.3) has not been applied")
	}
	names := make([]string, 0, len(stage4953eMatrix))
	for n := range stage4953eMatrix {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, doctype := range names {
		want := stage4953eMatrix[doctype]
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
				payload["reason_code"] = "TEST-4953e"
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
				err := ValidateStatusTransition("default", doctype, from, s, map[string]interface{}{"reason_code": "TEST-4953e"})
				var verr *ValidationError
				if !errors.As(err, &verr) || verr.Code != "GLOBAL-0019" {
					t.Errorf("%s: a plain save %s -> %s must be refused with GLOBAL-0019, got %v", doctype, from, s, err)
				}
			}
		}
	}
}

// The moves that would redo a finished event are refused on a plain save.
func TestStage4953eRedoMovesAreRefused(t *testing.T) {
	spInitDB()
	var seeded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM tenant_default.documents WHERE id = 'STR-FloorAssistRequest-Open-Acknowledged')`).Scan(&seeded)
	if !seeded {
		t.Skip("Stage 49.5.3 fifth migration has not been applied")
	}
	for _, m := range [][3]string{
		{"GateEntry", "Cancelled", "Open"},
		{"GateEntry", "GRN Made", "Open"},
		{"GateEntry", "Open", "GRN Made"},
		{"POSCostingGap", "Resolved", "Open"},
		{"POSOfflineQueueGap", "Reviewed", "Open"},
		{"POSOfflineSyncVariance", "Reviewed", "Open"},
		{"FloorAssistRequest", "Resolved", "Open"},
		{"FloorAssistRequest", "Resolved", "Acknowledged"},
		{"FloorAssistRequest", "Acknowledged", "Open"},
		{"ItemException", "Resolved", "Open"},
		{"ChannelSKUException", "Resolved", "Open"},
		{"StoreLabelRequest", "Cancelled", "Requested"},
		{"StoreLabelRequest", "Printed", "Requested"},
		{"StoreLabelRequest", "Requested", "Printed"},
		{"Hold", "Released", "Active"},
		{"Hold", "Active", "Released"},
	} {
		err := ValidateStatusTransition("default", m[0], m[1], m[2], map[string]interface{}{"reason_code": "TEST-4953e"})
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != "GLOBAL-0019" {
			t.Errorf("%s %s -> %s by a save: %v, want GLOBAL-0019", m[0], m[1], m[2], err)
		}
	}
}
