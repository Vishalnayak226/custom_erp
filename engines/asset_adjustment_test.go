package engines

import (
	"custom_erp/db"
	"math"
	"testing"
	"time"
)

// Ask 170.7: impairment and revaluation.

func cleanupAssetAdjustmentTest(schema string) {
	db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type IN ('Asset', 'Asset-Disposal') AND document_id LIKE 'T170A-%'")
	db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T170A-%'")
}

func adjNear(a, b float64) bool { return math.Abs(a-b) < 0.011 }

func TestAssetImpairmentPostsTheLossOnceAndResetsDepreciation(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanupAssetAdjustmentTest(schema)
	t.Cleanup(func() { cleanupAssetAdjustmentTest(schema) })

	// 2400 over 24 months; six have ended, so book value is 1800.
	capitalisedTestAsset(t, schema, "T170A-1", 2400, 2, monthsAgo(6))
	if _, err := AdjustAsset("default", "T170A-1", "t", AssetAdjustmentInput{Kind: "Impairment", Amount: 300}); validationCode(err) != "ASSET-1725" {
		t.Fatalf("no reason: %v", err)
	}
	if _, err := AdjustAsset("default", "T170A-1", "t", AssetAdjustmentInput{Kind: "Impairment", Amount: 5000, Reason: "x"}); validationCode(err) != "ASSET-1725" {
		t.Fatalf("a loss above the book value: %v", err)
	}
	in := AssetAdjustmentInput{Kind: "Impairment", Amount: 300, Reason: "Fire damage", IdempotencyKey: "T170A-K1"}
	res, err := AdjustAsset("default", "T170A-1", "t", in)
	if err != nil || !adjNear(res.CarryingBefore, 1800) || !adjNear(res.CarryingAfter, 1500) || !adjNear(res.Loss, 300) {
		t.Fatalf("impair: %+v %v", res, err)
	}
	if !adjNear(res.MonthlyCharge, 1500.0/18) {
		t.Fatalf("the remaining 18 months share 1500: %v", res.MonthlyCharge)
	}
	gl := assetGLNet(t, schema, "T170A-1")
	if gl["5313"][0] != 30000 || gl["1410"][1] != 60000+30000 {
		t.Fatalf("loss entry: %v", gl)
	}
	// The same key again changes nothing.
	again, err := AdjustAsset("default", "T170A-1", "t", in)
	if err != nil || !again.Replayed {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if gl2 := assetGLNet(t, schema, "T170A-1"); gl2["5313"][0] != 30000 {
		t.Fatalf("replay posted again: %v", gl2)
	}
	// Two months on, depreciation runs on the new base.
	data, _, _ := fetchAssetData("default", "T170A-1")
	if _, err := depreciateAsset("default", schema, "T170A-1", data, time.Now().AddDate(0, 2, 0)); err != nil {
		t.Fatalf("depreciate: %v", err)
	}
	gl = assetGLNet(t, schema, "T170A-1")
	want := int64(60000 + 2*(150000/18))
	if gl["5310"][0] < want-1 || gl["5310"][0] > want+1 {
		t.Fatalf("expense after two more months = %d, want about %d", gl["5310"][0], want)
	}
	// A disposal clears both asset accounts to nought.
	if _, err := DisposeAssetWithProceeds("default", "T170A-1", "Scrap", 0, ""); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	gl = assetGLNet(t, schema, "T170A-1")
	if gl["1400"][0]-gl["1400"][1] != 0 || gl["1410"][0]-gl["1410"][1] != 0 {
		t.Fatalf("disposal left balances: %v", gl)
	}
}

func TestAssetRevaluationUsesTheReserveFirst(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanupAssetAdjustmentTest(schema)
	t.Cleanup(func() { cleanupAssetAdjustmentTest(schema) })

	capitalisedTestAsset(t, schema, "T170A-2", 2400, 2, monthsAgo(6)) // book value 1800
	up, err := AdjustAsset("default", "T170A-2", "t", AssetAdjustmentInput{Kind: "Revaluation", NewValue: 2000, Reason: "Valuer's report"})
	if err != nil || !adjNear(up.ToReserve, 200) || !adjNear(up.Reserve, 200) || up.Loss != 0 {
		t.Fatalf("up: %+v %v", up, err)
	}
	gl := assetGLNet(t, schema, "T170A-2")
	if gl["3500"][1] != 20000 || gl["1400"][0] != 240000+20000 || gl["5313"][0] != 0 {
		t.Fatalf("up entry: %v", gl)
	}
	// Down by 50: all of it comes off the reserve; no loss.
	d1, err := AdjustAsset("default", "T170A-2", "t", AssetAdjustmentInput{Kind: "Revaluation", NewValue: 1950, Reason: "Market fell"})
	if err != nil || !adjNear(d1.FromReserve, 50) || d1.Loss != 0 || !adjNear(d1.Reserve, 150) {
		t.Fatalf("down 1: %+v %v", d1, err)
	}
	// Down by 250 more: 150 from the reserve, 100 is a loss.
	d2, err := AdjustAsset("default", "T170A-2", "t", AssetAdjustmentInput{Kind: "Revaluation", NewValue: 1700, Reason: "Market fell again"})
	if err != nil || !adjNear(d2.FromReserve, 150) || !adjNear(d2.Loss, 100) || d2.Reserve != 0 {
		t.Fatalf("down 2: %+v %v", d2, err)
	}
	gl = assetGLNet(t, schema, "T170A-2")
	if gl["3500"][0] != 20000 || gl["3500"][0] != gl["3500"][1] || gl["5313"][0] != 10000 {
		t.Fatalf("reserve must net to nought and the loss be 100: %v", gl)
	}
	if _, err := AdjustAsset("default", "T170A-2", "t", AssetAdjustmentInput{Kind: "Revaluation", NewValue: 1700, Reason: "Same"}); validationCode(err) != "ASSET-1725" {
		t.Fatalf("no change: %v", err)
	}
	// A disposal after all this leaves nothing on 1400 or 1410.
	if _, err := DisposeAssetWithProceeds("default", "T170A-2", "Scrap", 0, ""); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	gl = assetGLNet(t, schema, "T170A-2")
	if gl["1400"][0]-gl["1400"][1] != 0 || gl["1410"][0]-gl["1410"][1] != 0 {
		t.Fatalf("disposal left balances: %v", gl)
	}
}

func TestAssetAdjustmentRefusesAnAssetNotCapitalised(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanupAssetAdjustmentTest(schema)
	t.Cleanup(func() { cleanupAssetAdjustmentTest(schema) })
	insertTestDoc(t, schema, "T170A-3", "Asset", map[string]interface{}{"code": "T170A-3", "cost": 1000, "useful_life_years": 2})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Draft' WHERE id = 'T170A-3'")
	if _, err := AdjustAsset("default", "T170A-3", "t", AssetAdjustmentInput{Kind: "Impairment", Amount: 10, Reason: "x"}); validationCode(err) != "ASSET-1726" {
		t.Fatalf("draft asset: %v", err)
	}
}

// Ask 176: disposing of a revalued asset moves what is left of its reserve to
// retained earnings (Dr 3500 / Cr 3100) once; the lifecycle schedule carries
// the revaluation in its year so the last closing is nought.
func TestAssetDisposalReleasesTheReserveAndTheScheduleCarriesAdjustments(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanupAssetAdjustmentTest(schema)
	t.Cleanup(func() { cleanupAssetAdjustmentTest(schema) })

	capitalisedTestAsset(t, schema, "T170A-4", 2400, 2, monthsAgo(6)) // book value 1800
	if _, err := AdjustAsset("default", "T170A-4", "t", AssetAdjustmentInput{Kind: "Revaluation", NewValue: 2100, Reason: "Valuer's report"}); err != nil {
		t.Fatalf("revalue: %v", err)
	}
	lc, err := GetAssetLifecycle("default", "T170A-4")
	if err != nil || len(lc.Schedule) == 0 {
		t.Fatalf("lifecycle: %v", err)
	}
	if !adjNear(lc.Schedule[0].Opening, 2400) {
		t.Fatalf("the schedule must open at the cost before revaluation: %+v", lc.Schedule[0])
	}
	var adj float64
	for _, s := range lc.Schedule {
		adj += s.Adjustment
	}
	if !adjNear(adj, 300) {
		t.Fatalf("the schedule carries %.2f of adjustment, want 300", adj)
	}
	if last := lc.Schedule[len(lc.Schedule)-1]; !adjNear(last.Closing, 0) {
		t.Fatalf("the last year must close at nought: %+v", last)
	}

	preview, err := PreviewAssetDisposal("default", "T170A-4", 0, "")
	if err != nil || !adjNear(preview.ReserveReleased, 300) {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	res, err := DisposeAssetWithProceeds("default", "T170A-4", "Scrap", 0, "")
	if err != nil || !adjNear(res.ReserveReleased, 300) {
		t.Fatalf("dispose: %+v %v", res, err)
	}
	gl := assetGLNet(t, schema, "T170A-4")
	if gl["3500"][0] != 30000 || gl["3500"][1] != 30000 || gl["3100"][1] != 30000 {
		t.Fatalf("the reserve must move to 3100 once: %v", gl)
	}
	if gl["1400"][0]-gl["1400"][1] != 0 || gl["1410"][0]-gl["1410"][1] != 0 {
		t.Fatalf("disposal left balances: %v", gl)
	}
	if _, err := DisposeAssetWithProceeds("default", "T170A-4", "Scrap", 0, ""); err == nil {
		t.Fatal("a second disposal must be refused")
	}
	if gl2 := assetGLNet(t, schema, "T170A-4"); gl2["3100"][1] != 30000 {
		t.Fatalf("the release posted twice: %v", gl2)
	}
	data, _, _ := fetchAssetData("default", "T170A-4")
	if numFromInterface(data["revaluation_reserve_paise"]) != 0 || !adjNear(numFromInterface(data["disposal_reserve_released"]), 300) {
		t.Fatalf("asset data after release: reserve %v released %v", data["revaluation_reserve_paise"], data["disposal_reserve_released"])
	}
}
