package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

// Stage 74.1.3: an event posts to today's accounts until an Approved, in-date
// posting profile says otherwise; the most specific profile wins.
func TestStage74_1_3PostingProfiles(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, status string, data map[string]interface{}) {
		t.Helper()
		data["status"] = status
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, 'PostingProfile', $2, $3, 'system')", id, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	ctx := PostingContext{LegalEntity: "LE-1", ReasonCode: "DAMAGE"}
	if r := ResolvePostingAccounts(tenantID, PostingEventAdjustmentLoss, ctx, "5190", "1200"); r.Debit != "5190" || r.Credit != "1200" || r.Profile != "" {
		t.Fatalf("no profile: today's accounts, got %+v", r)
	}
	insert("PSP-ENTITY", "Approved", map[string]interface{}{"code": "PSP-ENTITY", "event": PostingEventAdjustmentLoss, "legal_entity": "LE-1", "debit_account": "5320"})
	insert("PSP-DAMAGE", "Approved", map[string]interface{}{"code": "PSP-DAMAGE", "event": PostingEventAdjustmentLoss, "reason_code": "DAMAGE", "debit_account": "5330"})
	insert("PSP-DRAFT", "Draft", map[string]interface{}{"code": "PSP-DRAFT", "event": PostingEventAdjustmentLoss, "reason_code": "DAMAGE", "legal_entity": "LE-1", "debit_account": "5440"})
	insert("PSP-OLD", "Approved", map[string]interface{}{"code": "PSP-OLD", "event": PostingEventAdjustmentLoss, "reason_code": "DAMAGE", "legal_entity": "LE-1", "debit_account": "5350",
		"effective_to": time.Now().AddDate(0, 0, -1).Format("2006-01-02")})

	if r := ResolvePostingAccounts(tenantID, PostingEventAdjustmentLoss, ctx, "5190", "1200"); r.Debit != "5330" || r.Credit != "1200" || r.Profile != "PSP-DAMAGE" {
		t.Errorf("the reason-code profile beats the entity one; Draft and ended ones never apply; a blank side stays: %+v", r)
	}
	if r := ResolvePostingAccounts(tenantID, PostingEventAdjustmentLoss, PostingContext{LegalEntity: "LE-1", ReasonCode: "COUNT"}, "5190", "1200"); r.Debit != "5320" || r.Profile != "PSP-ENTITY" {
		t.Errorf("another reason falls back to the entity profile: %+v", r)
	}
	if r := ResolvePostingAccounts(tenantID, PostingEventAdjustmentGain, ctx, "1200", "5190"); r.Profile != "" {
		t.Errorf("another event is untouched: %+v", r)
	}

	if err := validatePostingProfileRules(tenantID, map[string]interface{}{"event": PostingEventAdjustmentLoss, "debit_account": "9999"}); err == nil {
		t.Errorf("an account outside the chart is refused")
	}
	if err := validatePostingProfileRules(tenantID, map[string]interface{}{"event": PostingEventAdjustmentLoss}); err == nil {
		t.Errorf("a profile with no account is refused")
	}
	if err := validatePostingProfileRules(tenantID, map[string]interface{}{"event": "made_up", "debit_account": "5320"}); err == nil {
		t.Errorf("an unknown event is refused")
	}
	if err := validatePostingProfileRules(tenantID, map[string]interface{}{"event": PostingEventAdjustmentLoss, "debit_account": "5320", "credit_account": "1200"}); err != nil {
		t.Errorf("a valid profile saves: %v", err)
	}
	if gated, err := IsApprovalGated(tenantID, "PostingProfile"); err != nil || !gated {
		t.Errorf("editing a profile is maker-checker: gated=%v err=%v", gated, err)
	}
}
