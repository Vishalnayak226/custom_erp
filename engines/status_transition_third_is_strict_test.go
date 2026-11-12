package engines

import (
	"custom_erp/db"
	"errors"
	"sort"
	"strings"
	"testing"
)

// 49.5.3 (third batch): 21 more doctypes are strict and every status their
// enum declares is covered. db/migrations/migrations_stage140_13_strict_transitions_49_5_3c.sql.

var stage4953cMatrix = map[string]struct {
	rules      map[string]string
	engineOnly []string
	start      string
}{
	"EmployeeLoan":            {rules4953(), []string{"Active", "Closed"}, "Draft"},
	"RecurringSalesContract":  {rules4953("Active>Paused:No", "Paused>Active:No", "Active>Cancelled:No", "Paused>Cancelled:No"), nil, "Active"},
	"POSHeldBill":             {rules4953(), []string{"Resumed", "Discarded"}, "Held"},
	"ServiceContract":         {rules4953("Active>Expired:No", "Active>Cancelled:No", "Expired>Active:No"), nil, "Active"},
	"POSPriceOverride":        {rules4953(), []string{"Approved", "Rejected", "Consumed"}, "Pending Approval"},
	"BackdatedPostingRequest": {approval4953(), nil, "Draft"},
	"PeriodReopenRequest":     {approval4953(), nil, "Draft"},
	"GRNExcessApproval":       {approval4953(), nil, "Draft"},
	"HoldReleaseRequest":      {approval4953(), nil, "Draft"},
	"QualityInspection":       {approval4953(), nil, "Draft"},
	"SalesQuotation":          {approval4953(), []string{"Converted", "Expired"}, "Draft"},
	"ReprintRequest":          {approval4953(), []string{"Used"}, "Draft"},
	"ItemTreatmentCorrection": {approval4953(), []string{"Applied"}, "Draft"},
	"CustomerMerge":           {approval4953(), []string{"Merged"}, "Draft"},
	// Already had rules; now strict. The matrix pins what the seed says.
	"Batch": {rules4953("Active>Quarantined:No", "Active>Blocked:No", "Active>Expired:No", "Active>Consumed:No", "Quarantined>Active:Yes",
		"Quarantined>Blocked:No", "Quarantined>Expired:No", "Quarantined>Consumed:Yes", "Blocked>Active:Yes", "Blocked>Expired:No",
		"Blocked>Consumed:Yes", "Expired>Blocked:No", "Expired>Consumed:No"), nil, "Active"},
	"SerialNumber": {rules4953("InStock>Allocated:No", "Allocated>InStock:No", "Allocated>Shipped:No", "Shipped>Returned:Yes", "Returned>InStock:No",
		"Returned>Scrapped:Yes", "InStock>Scrapped:Yes", "Allocated>Scrapped:Yes"), nil, "InStock"},
	"ShippingPackage": {rules4953("Draft>Invoiced:No", "Invoiced>Cancelled:Yes", "Invoiced>Shipped:No", "Draft>Cancelled:Yes"), nil, "Draft"},
	"GatePass":        {rules4953("Draft>Issued:No", "Issued>Completed:No", "Draft>Discarded:Yes", "Issued>Discarded:Yes"), nil, "Draft"},
	"Wave":            {rules4953("Planned>Released:No", "Released>In Progress:No", "In Progress>Complete:No", "Complete>Closed:No"), nil, "Planned"},
	"WarehouseTask": {rules4953("Pending>Assigned:No", "Assigned>Pending:No", "Assigned>In Progress:No", "In Progress>Completed:No", "In Progress>Exception:No",
		"Exception>Assigned:No", "Pending>Cancelled:Yes", "Assigned>Cancelled:Yes", "In Progress>Cancelled:Yes", "Exception>Cancelled:Yes"), nil, "Pending"},
	"LoadingTask": {rules4953("Planned>Loading:No", "Loading>Loaded:No", "Loaded>Departed:No"), nil, "Planned"},
}

func TestStage4953cThirdBatchIsStrictAndCovered(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var seeded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.documents WHERE id = 'STR-ServiceContract-Active-Expired')`).Scan(&seeded)
	if !seeded {
		t.Skip("db/migrations/migrations_stage140_13_strict_transitions_49_5_3c.sql (Stage 49.5.3) has not been applied")
	}
	names := make([]string, 0, len(stage4953cMatrix))
	for n := range stage4953cMatrix {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, doctype := range names {
		want := stage4953cMatrix[doctype]
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
				payload["reason_code"] = "TEST-4953c"
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
				err := ValidateStatusTransition("default", doctype, from, s, map[string]interface{}{"reason_code": "TEST-4953c"})
				var verr *ValidationError
				if !errors.As(err, &verr) || verr.Code != "GLOBAL-0019" {
					t.Errorf("%s: a plain save %s -> %s must be refused with GLOBAL-0019, got %v", doctype, from, s, err)
				}
			}
		}
	}
}

// The moves that would redo a finished event are refused on a plain save.
func TestStage4953cRedoMovesAreRefused(t *testing.T) {
	spInitDB()
	var seeded bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM tenant_default.documents WHERE id = 'STR-ServiceContract-Active-Expired')`).Scan(&seeded)
	if !seeded {
		t.Skip("Stage 49.5.3's third migration has not been applied")
	}
	for _, m := range [][3]string{
		{"EmployeeLoan", "Closed", "Active"},
		{"EmployeeLoan", "Draft", "Active"},
		{"RecurringSalesContract", "Cancelled", "Active"},
		{"POSHeldBill", "Resumed", "Held"},
		{"POSHeldBill", "Discarded", "Held"},
		{"SalesQuotation", "Converted", "Draft"},
		{"ReprintRequest", "Used", "Approved"},
		{"POSPriceOverride", "Consumed", "Approved"},
		{"Batch", "Consumed", "Active"},
		{"SerialNumber", "Scrapped", "InStock"},
		{"GatePass", "Completed", "Draft"},
		{"Wave", "Closed", "Planned"},
		{"CustomerMerge", "Merged", "Draft"},
		{"ItemTreatmentCorrection", "Applied", "Draft"},
	} {
		err := ValidateStatusTransition("default", m[0], m[1], m[2], map[string]interface{}{"reason_code": "TEST-4953c"})
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != "GLOBAL-0019" {
			t.Errorf("%s %s -> %s by a save: %v, want GLOBAL-0019", m[0], m[1], m[2], err)
		}
	}
}
