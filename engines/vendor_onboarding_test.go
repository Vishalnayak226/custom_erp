package engines

import (
	"encoding/json"
	"strings"
	"testing"

	"custom_erp/db"
)

// Supplier onboarding and the approved vendor list (168.3): a vendor that is
// not Approved is refused on a new PO, an approver's decision makes it usable,
// the list refuses an unlisted vendor and names the listed ones, and with the
// settings off everything behaves as it always did.

func onboardingPO(vendor string, skus ...string) map[string]interface{} {
	var lines []map[string]interface{}
	for _, s := range skus {
		lines = append(lines, map[string]interface{}{"sku": s, "qty": 1, "rate": 10})
	}
	b, _ := json.Marshal(lines)
	return map[string]interface{}{"vendor": vendor, "status": "Draft", "items": string(b)}
}

func onboardingSetting(t *testing.T, tenantID, key, value string) {
	t.Helper()
	if err := SetSetting(tenantID, key, value, "test"); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

func (f *creditFixture) vendorStatus(id string) string {
	return strField(f.loadVendor(id), "onboarding_status")
}

func (f *creditFixture) tickChecklist(id string) {
	f.t.Helper()
	if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET data = data || '{\"doc_gstin_checked\":\"Yes\",\"doc_pan_checked\":\"Yes\",\"doc_bank_proof_checked\":\"Yes\"}'::jsonb WHERE doctype = 'Vendor' AND id = $1", id); err != nil {
		f.t.Fatal(err)
	}
}

func TestVendorOnboardingBlocksNewPO(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("V-APP", map[string]interface{}{"onboarding_status": "Applied"})
	f.vendor("V-OLD", nil) // an existing vendor: no status at all
	f.vendor("V-OK", map[string]interface{}{"onboarding_status": "Approved"})

	// setting off: the old behaviour, whatever the status says
	if err := checkPOVendorHold(f.tenantID, onboardingPO("V-APP", "ITEM-CC")); err != nil {
		t.Fatalf("setting off must not refuse: %v", err)
	}
	onboardingSetting(t, f.tenantID, SettingVendorOnboarding, "true")
	err := checkPOVendorHold(f.tenantID, onboardingPO("V-APP", "ITEM-CC"))
	if codeOf(err) != VendorNotApprovedCode || !strings.Contains(err.Error(), "Applied") {
		t.Fatalf("an Applied vendor must be refused with %s and its status, got %v", VendorNotApprovedCode, err)
	}
	for _, st := range []string{"Rejected", "Blocked"} {
		f.vendor("V-"+st, map[string]interface{}{"onboarding_status": st})
		if codeOf(checkPOVendorHold(f.tenantID, onboardingPO("V-"+st))) != VendorNotApprovedCode {
			t.Fatalf("a %s vendor must be refused", st)
		}
	}
	if err := checkPOVendorHold(f.tenantID, onboardingPO("V-OLD", "ITEM-CC")); err != nil {
		t.Fatalf("a vendor with no status counts as Approved: %v", err)
	}
	if err := checkPOVendorHold(f.tenantID, onboardingPO("V-OK", "ITEM-CC")); err != nil {
		t.Fatalf("an Approved vendor: %v", err)
	}
	// an order already past Draft is not touched (it can still be received)
	po := onboardingPO("V-APP", "ITEM-CC")
	po["status"] = "Approved"
	if err := checkPOVendorHold(f.tenantID, po); err != nil {
		t.Fatalf("an order past Draft must not be re-checked: %v", err)
	}
}

func TestVendorOnboardingDecisionMakesVendorUsable(t *testing.T) {
	f := newCreditFixture(t)
	onboardingSetting(t, f.tenantID, SettingVendorOnboarding, "true")
	withSelfApproval(t, f.tenantID, SelfApprovalNobody)
	f.vendor("V-1", map[string]interface{}{"onboarding_status": "Applied"})

	if _, err := RequestVendorOnboarding(f.tenantID, "V-1", "Approved", "maker", "Super Admin"); codeOf(err) != VendorChecklistCode {
		t.Fatalf("approval asked with the checklist empty: want %s, got %v", VendorChecklistCode, err)
	}
	if _, err := RequestVendorOnboarding(f.tenantID, "V-1", "Maybe", "maker", "Super Admin"); codeOf(err) != VendorOnboardRequestCode {
		t.Fatalf("unknown decision: got %v", err)
	}
	f.tickChecklist("V-1")
	id, err := RequestVendorOnboarding(f.tenantID, "V-1", "Approved", "maker", "Super Admin")
	if err != nil || id == "" {
		t.Fatalf("request: %v", err)
	}
	if _, err := RequestVendorOnboarding(f.tenantID, "V-1", "Approved", "maker", "Super Admin"); codeOf(err) != VendorOnboardPendingCode {
		t.Fatalf("a second request while one waits: want %s, got %v", VendorOnboardPendingCode, err)
	}
	if got := f.vendorStatus("V-1"); got != "Applied" {
		t.Fatalf("a waiting request must not change the vendor, got %s", got)
	}
	// wrong role cannot decide; the maker cannot decide their own with self-approval off
	if err := DecideApproval(f.tenantID, VendorOnboardingDoctype, id, "cash", "Cashier", "", "Approved", "x"); err == nil {
		t.Fatal("a Cashier approved a vendor")
	}
	if err := DecideApproval(f.tenantID, VendorOnboardingDoctype, id, "maker", "Store Manager", "", "Approved", "mine"); err == nil {
		t.Fatal("the maker approved their own request with self-approval off")
	}
	if got := f.vendorStatus("V-1"); got != "Applied" {
		t.Fatalf("refused decisions changed the vendor: %s", got)
	}
	if err := checkPOVendorHold(f.tenantID, onboardingPO("V-1", "ITEM-CC")); codeOf(err) != VendorNotApprovedCode {
		t.Fatalf("still Applied, the PO must be refused, got %v", err)
	}
	if err := DecideApproval(f.tenantID, VendorOnboardingDoctype, id, "checker", "Store Manager", "", "Approved", "ok"); err != nil {
		t.Fatalf("checker approval: %v", err)
	}
	if got := f.vendorStatus("V-1"); got != "Approved" {
		t.Fatalf("after approval the vendor is %q, want Approved", got)
	}
	if err := checkPOVendorHold(f.tenantID, onboardingPO("V-1", "ITEM-CC")); err != nil {
		t.Fatalf("an approved vendor must be usable: %v", err)
	}
	// it can be blocked the same way, and the block holds the vendor off orders
	bid, err := RequestVendorOnboarding(f.tenantID, "V-1", "Blocked", "maker", "Super Admin")
	if err != nil {
		t.Fatalf("block request: %v", err)
	}
	if err := DecideApproval(f.tenantID, VendorOnboardingDoctype, bid, "checker", "Store Manager", "", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	if codeOf(checkPOVendorHold(f.tenantID, onboardingPO("V-1", "ITEM-CC"))) != VendorNotApprovedCode {
		t.Fatal("a blocked vendor must be refused")
	}
}

func TestVendorOnboardingRejectedAndSelfApproval(t *testing.T) {
	f := newCreditFixture(t)
	onboardingSetting(t, f.tenantID, SettingVendorOnboarding, "true")
	f.vendor("V-2", map[string]interface{}{"onboarding_status": "Applied"})
	f.tickChecklist("V-2")
	id, err := RequestVendorOnboarding(f.tenantID, "V-2", "Approved", "maker", "Super Admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(f.tenantID, VendorOnboardingDoctype, id, "checker", "Store Manager", "", "Rejected", ""); err == nil {
		t.Fatal("a rejection without a comment must be refused")
	}
	if err := DecideApproval(f.tenantID, VendorOnboardingDoctype, id, "checker", "Store Manager", "", "Rejected", "bad GSTIN"); err != nil {
		t.Fatal(err)
	}
	if got := f.vendorStatus("V-2"); got != "Rejected" {
		t.Fatalf("after rejection the vendor is %q", got)
	}
	// self-approval anyone: the maker may decide their own request
	withSelfApproval(t, f.tenantID, SelfApprovalAnyone)
	id2, err := RequestVendorOnboarding(f.tenantID, "V-2", "Approved", "maker", "Super Admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(f.tenantID, VendorOnboardingDoctype, id2, "maker", "Store Manager", "", "Approved", "ok"); err != nil {
		t.Fatalf("self-approval is allowed here: %v", err)
	}
	if got := f.vendorStatus("V-2"); got != "Approved" {
		t.Fatalf("vendor is %q, want Approved", got)
	}
}

func TestStampVendorOnboarding(t *testing.T) {
	f := newCreditFixture(t)
	p := map[string]interface{}{"name": "N", "onboarding_status": "Approved"}
	StampVendorOnboarding(f.tenantID, nil, p)
	if _, has := p["onboarding_status"]; has {
		t.Fatalf("setting off: a caller's status must be dropped, got %v", p)
	}
	onboardingSetting(t, f.tenantID, SettingVendorOnboarding, "true")
	p = map[string]interface{}{"name": "N", "onboarding_status": "Approved"}
	StampVendorOnboarding(f.tenantID, nil, p)
	if p["onboarding_status"] != "Applied" {
		t.Fatalf("a new vendor starts Applied, got %v", p["onboarding_status"])
	}
	edit := map[string]interface{}{"onboarding_status": "Approved"}
	StampVendorOnboarding(f.tenantID, map[string]interface{}{"onboarding_status": "Applied"}, edit)
	if edit["onboarding_status"] != "Applied" {
		t.Fatalf("an edit must keep the stored status, got %v", edit["onboarding_status"])
	}
	legacy := map[string]interface{}{"onboarding_status": "Approved"}
	StampVendorOnboarding(f.tenantID, map[string]interface{}{"name": "old"}, legacy)
	if _, has := legacy["onboarding_status"]; has {
		t.Fatal("an old vendor with no status must stay without one")
	}
}

func TestApprovedVendorList(t *testing.T) {
	f := newCreditFixture(t)
	f.seed("ITEM-PK", "Item", "Active", map[string]interface{}{"code": "ITEM-PK", "name": "Tape", "category": "Packaging"})
	f.seed("ITEM-FREE", "Item", "Active", map[string]interface{}{"code": "ITEM-FREE", "name": "Free", "category": "Misc"})
	f.vendor("V-A", map[string]interface{}{"name": "Alpha Traders"})
	f.vendor("V-B", map[string]interface{}{"name": "Beta Supplies"})
	f.vendor("V-C", map[string]interface{}{"name": "Gamma Co"})
	f.seed("AVL-1", ApprovedVendorDoctype, "Active", map[string]interface{}{"item_id": "ITEM-CC", "vendor_id": "V-A"})
	f.seed("AVL-2", ApprovedVendorDoctype, "Active", map[string]interface{}{"category": "Packaging", "vendor_id": "V-B"})

	// setting off: unlisted vendors are fine
	if err := checkPOVendorHold(f.tenantID, onboardingPO("V-C", "ITEM-CC")); err != nil {
		t.Fatalf("setting off must not refuse: %v", err)
	}
	onboardingSetting(t, f.tenantID, SettingApprovedVendorOnl, "true")
	err := checkPOVendorHold(f.tenantID, onboardingPO("V-C", "ITEM-CC"))
	if codeOf(err) != VendorNotOnAVLCode || !strings.Contains(err.Error(), "Alpha Traders") {
		t.Fatalf("an unlisted vendor must be refused naming the approved one, got %v", err)
	}
	if err := checkPOVendorHold(f.tenantID, onboardingPO("V-A", "ITEM-CC")); err != nil {
		t.Fatalf("a listed vendor: %v", err)
	}
	// by category: V-B is listed for Packaging, so V-C is refused and both are named for the tape
	err = checkPOVendorHold(f.tenantID, onboardingPO("V-C", "ITEM-PK"))
	if codeOf(err) != VendorNotOnAVLCode || !strings.Contains(err.Error(), "Beta Supplies") {
		t.Fatalf("category entry must apply, got %v", err)
	}
	if err := checkPOVendorHold(f.tenantID, onboardingPO("V-B", "ITEM-PK")); err != nil {
		t.Fatalf("category-listed vendor: %v", err)
	}
	// an item with no entries is unaffected, and a mixed order is refused on the listed line
	if err := checkPOVendorHold(f.tenantID, onboardingPO("V-C", "ITEM-FREE")); err != nil {
		t.Fatalf("an item with no entries is unaffected: %v", err)
	}
	if codeOf(checkPOVendorHold(f.tenantID, onboardingPO("V-C", "ITEM-FREE", "ITEM-CC"))) != VendorNotOnAVLCode {
		t.Fatal("a mixed order must be refused on the listed line")
	}
}

func TestApprovedVendorRowRules(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("V-A", nil)
	row := func(item, cat string) map[string]interface{} {
		return map[string]interface{}{"item_id": item, "category": cat, "vendor_id": "V-A"}
	}
	if codeOf(ValidateTransactionalRules(f.tenantID, ApprovedVendorDoctype, "", "", nil, row("", ""))) != ApprovedVendorRowCode {
		t.Fatal("neither item nor category must be refused")
	}
	if codeOf(ValidateTransactionalRules(f.tenantID, ApprovedVendorDoctype, "", "", nil, row("ITEM-CC", "Packaging"))) != ApprovedVendorRowCode {
		t.Fatal("both item and category must be refused")
	}
	if err := ValidateTransactionalRules(f.tenantID, ApprovedVendorDoctype, "AVL-9", "", nil, row("ITEM-CC", "")); err != nil {
		t.Fatalf("a valid entry: %v", err)
	}
	f.seed("AVL-9", ApprovedVendorDoctype, "Active", row("ITEM-CC", ""))
	if codeOf(ValidateTransactionalRules(f.tenantID, ApprovedVendorDoctype, "AVL-10", "", nil, row("ITEM-CC", ""))) != ApprovedVendorDupCode {
		t.Fatal("a duplicate must be refused")
	}
	if err := ValidateTransactionalRules(f.tenantID, ApprovedVendorDoctype, "AVL-9", "Active", map[string]interface{}{}, row("ITEM-CC", "")); err != nil {
		t.Fatalf("saving the same entry again is not a duplicate: %v", err)
	}
}
