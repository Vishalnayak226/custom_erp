package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestStage379QualityMaintenance(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system') "+
			"ON CONFLICT (id) DO UPDATE SET doctype = $2, data = $3, status = $4", id, doctype, raw, status)
	}
	cleanupIDs := func(ids ...string) {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}

	t.Run("ValidateInspectionPlanDocument rejects a non-positive sample_size", func(t *testing.T) {
		if err := ValidateInspectionPlanDocument(tenantID, map[string]interface{}{"sample_size": 0.0}); err == nil {
			t.Fatalf("expected a non-positive sample_size to be rejected")
		}
		if err := ValidateInspectionPlanDocument(tenantID, map[string]interface{}{"sample_size": 5.0}); err != nil {
			t.Fatalf("expected a valid sample_size to be accepted, got %v", err)
		}
	})

	t.Run("CertificateOfAnalysis: overall result derivation, release refused on Fail, reject quarantines every Good bin holding", func(t *testing.T) {
		const sku, binA, binB, location, batchNo = "TEST379A-SKU", "TEST379A-BINA", "TEST379A-BINB", "TEST379A-LOC", "TEST379A-BATCH"
		cleanupBins := func() {
			db.DB.Exec("DELETE FROM " + schema + ".bin_stock_batch WHERE batch_no = '" + batchNo + "'")
			db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE sku = '" + sku + "'")
			db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku = '" + sku + "'")
		}
		cleanupBins()
		defer cleanupBins()

		// TransitionBinStockCondition (called by QuarantineBatch) requires a
		// real bin_stock row for (bin, sku, 'Good') - it is the parent
		// aggregate bin_stock_batch is a further breakdown of (Stage 42.1).
		for _, h := range []struct {
			bin string
			qty int
		}{{binA, 10}, {binB, 5}} {
			if _, err := db.DB.Exec(fmt.Sprintf(
				"INSERT INTO %s.bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1,$2,$3,'Good',$4)", schema),
				h.bin, sku, location, h.qty); err != nil {
				t.Fatalf("seed bin_stock %s: %v", h.bin, err)
			}
			if _, err := db.DB.Exec(fmt.Sprintf(
				"INSERT INTO %s.bin_stock_batch (bin_code, sku, batch_no, condition, location_code, qty) VALUES ($1,$2,$3,'Good',$4,$5)", schema),
				h.bin, sku, batchNo, location, h.qty); err != nil {
				t.Fatalf("seed bin_stock_batch %s: %v", h.bin, err)
			}
		}
		db.DB.Exec(fmt.Sprintf(
			"INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) VALUES ($1,$2,15,15)", schema),
			sku, location)

		passID, err := CreateCertificateOfAnalysis(tenantID, batchNo, sku, "", []map[string]interface{}{
			{"parameter_name": "Purity", "actual_value": "99.9%", "pass_fail": "Pass"},
		}, "system")
		if err != nil {
			t.Fatalf("CreateCertificateOfAnalysis (pass case): %v", err)
		}
		defer cleanupIDs(passID)
		data, _, err := fetchCoA(schema, passID)
		if err != nil {
			t.Fatalf("fetchCoA: %v", err)
		}
		if data["overall_result"] != "Pass" {
			t.Fatalf("expected overall_result=Pass, got %v", data["overall_result"])
		}
		if err := ReleaseCertificateOfAnalysis(tenantID, passID); err != nil {
			t.Fatalf("ReleaseCertificateOfAnalysis: %v", err)
		}

		failID, err := CreateCertificateOfAnalysis(tenantID, batchNo, sku, "", []map[string]interface{}{
			{"parameter_name": "Purity", "actual_value": "99.9%", "pass_fail": "Pass"},
			{"parameter_name": "Moisture", "actual_value": "12%", "pass_fail": "Fail"},
		}, "system")
		if err != nil {
			t.Fatalf("CreateCertificateOfAnalysis (fail case): %v", err)
		}
		defer cleanupIDs(failID)
		data, _, err = fetchCoA(schema, failID)
		if err != nil {
			t.Fatalf("fetchCoA: %v", err)
		}
		if data["overall_result"] != "Fail" {
			t.Fatalf("expected overall_result=Fail (one failed parameter fails the whole CoA), got %v", data["overall_result"])
		}
		if err := ReleaseCertificateOfAnalysis(tenantID, failID); err == nil {
			t.Fatalf("expected releasing a Fail CoA to be refused")
		}

		binsAffected, err := RejectCertificateOfAnalysis(tenantID, failID, "system")
		if err != nil {
			t.Fatalf("RejectCertificateOfAnalysis: %v", err)
		}
		if binsAffected != 2 {
			t.Fatalf("expected both bins holding the batch to be quarantined, got %d", binsAffected)
		}
		var conditionA, conditionB string
		db.DB.QueryRow("SELECT condition FROM "+schema+".bin_stock_batch WHERE bin_code=$1 AND batch_no=$2 AND qty > 0", binA, batchNo).Scan(&conditionA)
		db.DB.QueryRow("SELECT condition FROM "+schema+".bin_stock_batch WHERE bin_code=$1 AND batch_no=$2 AND qty > 0", binB, batchNo).Scan(&conditionB)
		if conditionA != "QC-Hold" || conditionB != "QC-Hold" {
			t.Fatalf("expected both bins' batch rows to move to QC-Hold, got A=%q B=%q", conditionA, conditionB)
		}

		if _, err := RejectCertificateOfAnalysis(tenantID, failID, "system"); err == nil {
			t.Fatalf("expected re-rejecting an already-Rejected CoA to be refused")
		}
	})

	t.Run("NonConformanceReport: root cause requires a real Active Quality ReasonCode, full lifecycle enforced in order", func(t *testing.T) {
		const reasonID = "TEST379B-REASON"
		cleanupIDs(reasonID)
		defer cleanupIDs(reasonID)
		insert(reasonID, "ReasonCode", "Active", map[string]interface{}{"code": reasonID, "description": "Supplier defect", "category": "Quality", "status": "Active"})

		ncrID, err := CreateNonConformanceReport(tenantID, "Wrong dimensions on batch", "CertificateOfAnalysis", "SOME-COA", "system")
		if err != nil {
			t.Fatalf("CreateNonConformanceReport: %v", err)
		}
		defer cleanupIDs(ncrID)

		if err := PlanCorrectiveAction(tenantID, ncrID, "re-train supplier"); err == nil {
			t.Fatalf("expected planning a corrective action on a Draft NCR (not yet Investigating) to be refused")
		}
		if err := InvestigateNonConformanceReport(tenantID, ncrID, ""); err == nil {
			t.Fatalf("expected an empty root_cause_reason_code to be refused")
		}
		if err := InvestigateNonConformanceReport(tenantID, ncrID, "TEST379B-NO-SUCH-REASON"); err == nil {
			t.Fatalf("expected an unregistered reason code to be refused")
		}
		if err := InvestigateNonConformanceReport(tenantID, ncrID, reasonID); err != nil {
			t.Fatalf("InvestigateNonConformanceReport: %v", err)
		}

		if err := CloseNonConformanceReport(tenantID, ncrID); err == nil {
			t.Fatalf("expected closing an Investigating (not yet CorrectiveActionPlanned) NCR to be refused")
		}
		if err := PlanCorrectiveAction(tenantID, ncrID, ""); err == nil {
			t.Fatalf("expected an empty corrective_action to be refused")
		}
		if err := PlanCorrectiveAction(tenantID, ncrID, "re-train supplier, add incoming QC step"); err != nil {
			t.Fatalf("PlanCorrectiveAction: %v", err)
		}
		if err := CloseNonConformanceReport(tenantID, ncrID); err != nil {
			t.Fatalf("CloseNonConformanceReport: %v", err)
		}
		_, status, err := fetchNCR(schema, ncrID)
		if err != nil {
			t.Fatalf("fetchNCR: %v", err)
		}
		if status != "Closed" {
			t.Fatalf("expected status=Closed, got %s", status)
		}
	})

	t.Run("MaintenanceSchedule validation and the worker spawning a Scheduled MaintenanceOrder", func(t *testing.T) {
		const assetID, scheduleID = "TEST379C-ASSET", "TEST379C-SCHED"
		cleanupIDs(assetID, scheduleID)
		defer cleanupIDs(assetID, scheduleID)

		if err := ValidateMaintenanceScheduleDocument(tenantID, map[string]interface{}{"interval_days": 0.0}); err == nil {
			t.Fatalf("expected a non-positive interval_days to be rejected")
		}

		insert(assetID, "Asset", "Capitalised", map[string]interface{}{"code": assetID, "category": "Equipment", "status": "Capitalised"})
		yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		insert(scheduleID, "MaintenanceSchedule", "Active", map[string]interface{}{
			"code": scheduleID, "asset": assetID, "description": "Quarterly service",
			"interval_days": 90.0, "next_due_date": yesterday,
		})

		runMaintenanceSchedulingForSchema(schema)

		var orderID string
		var scheduledDate string
		if err := db.DB.QueryRow("SELECT id, data->>'scheduled_date' FROM "+schema+".documents WHERE doctype='MaintenanceOrder' AND data->>'maintenance_schedule_id'=$1", scheduleID).Scan(&orderID, &scheduledDate); err != nil {
			t.Fatalf("expected the worker to have spawned a MaintenanceOrder: %v", err)
		}
		defer cleanupIDs(orderID)
		if scheduledDate != yesterday {
			t.Fatalf("expected scheduled_date=%s, got %s", yesterday, scheduledDate)
		}

		var newNextDueDate string
		db.DB.QueryRow("SELECT data->>'next_due_date' FROM "+schema+".documents WHERE id=$1", scheduleID).Scan(&newNextDueDate)
		if newNextDueDate <= yesterday {
			t.Fatalf("expected next_due_date to have advanced past %s, got %s", yesterday, newNextDueDate)
		}

		if err := CompleteMaintenanceOrder(tenantID, orderID, ""); err == nil {
			t.Fatalf("expected completing with no completion_notes to be refused")
		}
		if err := CompleteMaintenanceOrder(tenantID, orderID, "serviced"); err == nil {
			t.Fatalf("expected completing a Scheduled (not yet InProgress) order to be refused")
		}
		if err := StartMaintenanceOrder(tenantID, orderID); err != nil {
			t.Fatalf("StartMaintenanceOrder: %v", err)
		}
		if err := CompleteMaintenanceOrder(tenantID, orderID, "serviced, replaced filter"); err != nil {
			t.Fatalf("CompleteMaintenanceOrder: %v", err)
		}
		if err := CancelMaintenanceOrder(tenantID, orderID); err == nil {
			t.Fatalf("expected cancelling an already-Completed (terminal) order to be refused")
		}
	})
}
