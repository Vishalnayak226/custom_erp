package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// Blanket / rate contracts.

func contractFix(t *testing.T) *wsFix {
	t.Helper()
	f := newWSFix(t, false)
	f.vendor("PC-V")
	return f
}

func (f *wsFix) contract(id, status string, capValue float64) {
	f.t.Helper()
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": "WS-A", "rate": 100.0}, {"sku": "WS-B", "rate": 50.0}})
	f.insert(id, "PurchaseContract", status, "system", map[string]interface{}{"code": id, "vendor": "PC-V", "contract_kind": "Blanket",
		"valid_from": "2020-01-01", "valid_to": "2099-12-31", "cap_value": capValue, "lines": string(lines), "status": status})
}

func (f *wsFix) releasePO(id, contract string, qty float64, rate float64) map[string]interface{} {
	f.t.Helper()
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "WS-A", "qty": qty, "rate": rate}})
	data := map[string]interface{}{"code": id, "vendor_id": "PC-V", "location": "WS-HQ", "order_date": BusinessToday(), "items": string(items),
		"total_amount": qty * rate, "grand_total": qty * rate, "contract": contract, "status": "Draft"}
	f.insert(id, "PurchaseOrder", "Draft", "system", data)
	return data
}

func (f *wsFix) setStatus(doctype, id, status string) {
	f.t.Helper()
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = $1 WHERE doctype = $2 AND id = $3`, f.schema), status, doctype, id); err != nil {
		f.t.Fatal(err)
	}
}

func TestPurchaseContractRateComesFromTheContractAndAboveItIsRefused(t *testing.T) {
	f := contractFix(t)
	f.contract("PC-1", "Active", 100000)
	blank := f.releasePO("PO-C1", "PC-1", 2, 0)
	if err := applyPOContract(f.tenantID, "", "", nil, blank); err != nil {
		t.Fatalf("blank rate: %v", err)
	}
	if got := docTableOf(blank["items"])[0]["rate"]; numFromInterface(got) != 100 {
		t.Fatalf("rate not taken from the contract: %v", got)
	}
	wantCode(t, applyPOContract(f.tenantID, "", "", nil, f.releasePO("PO-C2", "PC-1", 2, 120)), codeContractRate)
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "NOT-ON", "qty": 1, "rate": 1}})
	wantCode(t, applyPOContract(f.tenantID, "", "", nil, map[string]interface{}{"contract": "PC-1", "vendor_id": "PC-V", "items": string(items)}), codeContractUnusable)
	// a PO that names no contract is untouched
	plain := f.releasePO("PO-C3", "", 2, 500)
	if err := applyPOContract(f.tenantID, "", "", nil, plain); err != nil {
		t.Fatalf("plain PO: %v", err)
	}
}

func TestPurchaseContractRefusesDraftOtherVendorAndOutsideTheWindow(t *testing.T) {
	f := contractFix(t)
	f.contract("PC-D", "Draft", 1000)
	wantCode(t, applyPOContract(f.tenantID, "", "", nil, f.releasePO("PO-D1", "PC-D", 1, 100)), codeContractUnusable)
	f.contract("PC-A", "Active", 1000)
	other := f.releasePO("PO-D2", "PC-A", 1, 100)
	other["vendor_id"] = "SOMEONE-ELSE"
	wantCode(t, applyPOContract(f.tenantID, "", "", nil, other), codeContractUnusable)
	late := f.releasePO("PO-D3", "PC-A", 1, 100)
	late["order_date"] = "2100-01-01"
	wantCode(t, applyPOContract(f.tenantID, "", "", nil, late), codeContractUnusable)
}

func TestPurchaseContractCapRefusesCancelReturnsAndReplayDoesNotDoubleCount(t *testing.T) {
	f := contractFix(t)
	f.contract("PC-2", "Active", 1000)
	f.releasePO("PO-1", "PC-2", 10, 100) // exactly the cap
	f.releasePO("PO-2", "PC-2", 1, 100)
	if err := SubmitForApproval(f.tenantID, "PurchaseOrder", "PO-1", "system", "Super Admin"); err != nil {
		t.Fatalf("first release: %v", err)
	}
	// asking again for the same commit must not count it twice
	if err := checkContractCap(f.tenantID, "PC-2", "PO-1", f.doc("PurchaseOrder", "PO-1")); err != nil {
		t.Fatalf("replay double-counted: %v", err)
	}
	wantCode(t, SubmitForApproval(f.tenantID, "PurchaseOrder", "PO-2", "system", "Super Admin"), codeContractCap)
	f.setStatus("PurchaseOrder", "PO-1", "Cancelled")
	if err := SubmitForApproval(f.tenantID, "PurchaseOrder", "PO-2", "system", "Super Admin"); err != nil {
		t.Fatalf("cancel did not give the release back: %v", err)
	}
	s, err := PurchaseContractSummary(f.tenantID, "PC-2")
	if err != nil || s.ConsumedValue != 100 {
		t.Fatalf("summary %+v %v", s, err)
	}
}

func TestPurchaseContractConcurrentReleasesCannotPassTheCap(t *testing.T) {
	f := contractFix(t)
	f.contract("PC-3", "Active", 1000)
	for i := 0; i < 6; i++ {
		f.releasePO(fmt.Sprintf("PO-K%d", i), "PC-3", 4, 100) // 400 each
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if SubmitForApproval(f.tenantID, "PurchaseOrder", fmt.Sprintf("PO-K%d", i), "system", "Super Admin") == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if ok != 2 {
		t.Fatalf("%d releases got through a cap that holds two", ok)
	}
}

func TestPurchaseContractActivationNeedsThePOApproverAndTheOwnTenant(t *testing.T) {
	f := contractFix(t)
	f.contract("PC-4", "Draft", 1000)
	wantCode(t, ActivatePurchaseContract(f.tenantID, "PC-4", "cashier1", "Cashier"), codeContractAction)
	if err := ActivatePurchaseContract(f.tenantID, "PC-4", "mgr", "Store Manager"); err != nil {
		t.Fatalf("approver: %v", err)
	}
	wantCode(t, ActivatePurchaseContract(f.tenantID, "PC-4", "mgr", "Store Manager"), codeContractAction) // replay
	other := contractFix(t)
	if _, err := PurchaseContractSummary(other.tenantID, "PC-4"); err == nil {
		t.Fatal("another client read this client's contract")
	}
	// terms are locked once Active
	wantCode(t, validatePurchaseContractSave(f.tenantID, "PC-4", "Active", f.doc("PurchaseContract", "PC-4"),
		map[string]interface{}{"vendor": "PC-V", "cap_value": 999999.0, "lines": f.doc("PurchaseContract", "PC-4")["lines"]}), codeContractSave)
}
