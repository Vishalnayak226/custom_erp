package engines

import (
	"custom_erp/db"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
)

// Ask 170.6: capital work in progress.

func (e *exp19) cwipAsset(budget float64) string {
	e.t.Helper()
	id := e.id("CWIP")
	e.doc(id, "Asset", "Draft", map[string]interface{}{"code": id, "category": "Test", "location": "HQ", "useful_life_years": 5.0})
	if err := MoveAssetToCWIP(e.tenant, id, "t", budget); err != nil {
		e.t.Fatalf("start: %v", err)
	}
	return id
}

func TestAssetCWIPCollectsBillsAndCapitalisesOnce(t *testing.T) {
	e := newExp19(t)
	asset := e.cwipAsset(5000)
	bill := e.create(e.input("CWIP1",
		ExpenseBillLine{Description: "Civil works", Amount: 1000, GSTRate: 18, ITC: "Eligible", CwipAsset: asset},
		ExpenseBillLine{Description: "Fit-out", Amount: 500, GSTRate: 18, ITC: "Blocked", CwipAsset: asset},
		ExpenseBillLine{Account: "5100", Amount: 200}))
	// The charge lands in 1450, never in the expense account; blocked GST joins the cost.
	if got := e.gl(bill, "1450"); got != 100000+59000 {
		t.Fatalf("1450 = %d, want 159000", got)
	}
	if got := e.gl(bill, "5100"); got != 20000 {
		t.Fatalf("the plain line stays in 5100: %d", got)
	}
	e.wantBalanced(bill)
	if n, err := CWIPCollectedPaise(e.tenant, asset); err != nil || n != 159000 {
		t.Fatalf("collected = %d %v", n, err)
	}
	reg, _ := GetAssetRegister("default")
	for _, r := range reg {
		if r.ID == asset && (r.CwipCollected != 1590 || r.Status != "CWIP") {
			t.Fatalf("register: %+v", r)
		}
	}

	// A cancelled bill no longer counts.
	cancelled := e.create(e.input("CWIP2", ExpenseBillLine{Description: "More", Amount: 100, CwipAsset: asset}))
	if n, _ := CWIPCollectedPaise(e.tenant, asset); n != 169000 {
		t.Fatalf("collected with the second bill = %d", n)
	}
	db.DB.Exec("UPDATE "+e.schema+".documents SET status = 'Cancelled' WHERE id = $1", cancelled)
	if n, _ := CWIPCollectedPaise(e.tenant, asset); n != 159000 {
		t.Fatalf("a cancelled bill still counted: %d", n)
	}

	res, err := CapitalizeCWIPAsset(e.tenant, asset, "t", 0, "2026-10-01")
	if err != nil || res.Amount != 1590 {
		t.Fatalf("capitalise: %+v %v", res, err)
	}
	gl := assetGLNet(t, e.schema, asset)
	if gl["1400"][0] != 159000 || gl["1450"][1] != 159000 {
		t.Fatalf("capitalise entry: %v", gl)
	}
	if _, err := CapitalizeCWIPAsset(e.tenant, asset, "t", 0, "2026-10-01"); validationCode(err) != "ASSET-1723" {
		t.Fatalf("a second capitalise must be refused: %v", err)
	}
	if gl2 := assetGLNet(t, e.schema, asset); gl2["1400"][0] != 159000 {
		t.Fatalf("second capitalise posted: %v", gl2)
	}
	// Depreciation now starts from the capitalisation date.
	data, status, _ := fetchAssetData(e.tenant, asset)
	if status != "Capitalised" || strField(data, "capitalisation_date") != "2026-10-01" {
		t.Fatalf("asset: %s %v", status, data["capitalisation_date"])
	}
	if _, err := depreciateAsset(e.tenant, e.schema, asset, data, time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("depreciate: %v", err)
	}
	if gl3 := assetGLNet(t, e.schema, asset); gl3["5310"][0] == 0 {
		t.Fatalf("no depreciation after capitalising: %v", gl3)
	}
}

func TestAssetCWIPRefusesAChargeToAnAssetNotInCWIP(t *testing.T) {
	e := newExp19(t)
	plain := e.id("PLAIN")
	e.doc(plain, "Asset", "Draft", map[string]interface{}{"code": plain, "category": "Test"})
	_, err := CreateExpenseBill(e.tenant, "system", e.input("CWIPX", ExpenseBillLine{Description: "x", Amount: 100, CwipAsset: plain}))
	if validationCode(err) != "ASSET-1724" {
		t.Fatalf("a Draft asset: %v", err)
	}
	empty := e.cwipAsset(0)
	if _, err := CapitalizeCWIPAsset(e.tenant, empty, "t", 0, ""); validationCode(err) != "ASSET-1723" {
		t.Fatalf("nothing collected: %v", err)
	}
}

// Ask 176: a bill and a capitalisation racing for the same asset queue on the
// asset's row lock, so what is capitalised is exactly what was charged before
// it, and 1450 nets to nought for the asset.
func TestAssetCWIPBillsRacingACapitaliseReconcile(t *testing.T) {
	e := newExp19(t)
	asset := e.cwipAsset(0)
	e.create(e.input("RACE0", ExpenseBillLine{Description: "Groundwork", Amount: 1000, CwipAsset: asset}))

	const bills = 6
	var mu sync.Mutex
	var wg sync.WaitGroup
	posted, refused := 0, 0
	var capErr error
	var capAmount float64
	for i := 0; i < bills; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := CreateExpenseBill(e.tenant, "system", e.input(fmt.Sprintf("RACE%d", i+1), ExpenseBillLine{Description: "Works", Amount: 100, CwipAsset: asset}))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				posted++
				e.docs = append(e.docs, res.InvoiceID)
			case validationCode(err) == "ASSET-1724":
				refused++
			default:
				t.Errorf("bill %d: %v", i+1, err)
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		res, err := CapitalizeCWIPAsset(e.tenant, asset, "t", 0, "2026-10-01")
		mu.Lock()
		defer mu.Unlock()
		capErr = err
		if res != nil {
			capAmount = res.Amount
		}
	}()
	wg.Wait()
	if capErr != nil {
		t.Fatalf("capitalise: %v", capErr)
	}
	if posted+refused != bills {
		t.Fatalf("posted %d + refused %d != %d", posted, refused, bills)
	}
	collected, err := CWIPCollectedPaise(e.tenant, asset)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(100000 + posted*10000); collected != want {
		t.Fatalf("collected %d, want %d (posted %d)", collected, want, posted)
	}
	if RupeesToPaise(capAmount) != collected {
		t.Fatalf("capitalised %.2f but %d was charged to the asset", capAmount, collected)
	}
	var net int64
	if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit - credit), 0) FROM "+e.schema+".gl_postings WHERE account_code = '1450' AND (document_id = $1 OR document_id = ANY($2))",
		asset, pq.Array(e.docs)).Scan(&net); err != nil {
		t.Fatal(err)
	}
	if net != 0 {
		t.Fatalf("1450 left %d for the asset's bills and its capitalisation", net)
	}
}

// Ask 176: a bill held for approval that charges a CWIP asset is refused at
// approval once the asset has been capitalised.
func TestAssetCWIPBillApprovedAfterCapitaliseIsRefused(t *testing.T) {
	e := newExp19(t)
	rule, err := UpsertApprovalRule(e.tenant, "VendorInvoice", 50000, nil, "Finance Manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DeleteApprovalRule(e.tenant, rule) })
	asset := e.cwipAsset(0)
	e.create(e.input("PRE", ExpenseBillLine{Description: "Groundwork", Amount: 1000, CwipAsset: asset}))
	held := e.create(e.input("HELD", ExpenseBillLine{Description: "Big works", Amount: 60000, CwipAsset: asset}))
	if _, st, _ := fetchDocData(e.tenant, "VendorInvoice", held); st != "Pending Approval" {
		t.Fatalf("the big bill is %s, want Pending Approval", st)
	}
	if _, err := CapitalizeCWIPAsset(e.tenant, asset, "t", 0, "2026-10-01"); err != nil {
		t.Fatalf("capitalise: %v", err)
	}
	db.DB.Exec("UPDATE "+e.schema+".documents SET status = 'Approved' WHERE id = $1", held)
	if _, err := FinalizeVendorInvoiceOverridePayment(e.tenant, held, "checker"); validationCode(err) != "ASSET-1724" {
		t.Fatalf("approving a charge to a capitalised asset must be refused with ASSET-1724: %v", err)
	}
	if e.gl(held, "1450") != 0 {
		t.Fatal("the refused bill booked to 1450")
	}
}
