package engines

import (
	"custom_erp/db"
	"testing"
	"time"
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
