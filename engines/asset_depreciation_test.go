package engines

import (
	"context"
	"custom_erp/db"
	"math"
	"testing"
	"time"
)

// Stage 55.16 (Batch D): depreciation posted monthly, disposal with proceeds.

func cleanupAssetDepreciationTest(schema string) {
	db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type IN ('Asset', 'Asset-Disposal') AND document_id LIKE 'T5516D-%'")
	db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T5516D-%'")
	db.DB.Exec("DELETE FROM " + schema + ".accounting_periods WHERE period_name LIKE 'T5516D%'")
}

func capitalisedTestAsset(t *testing.T, schema, id string, cost, lifeYears float64, capDate string) {
	t.Helper()
	insertTestDoc(t, schema, id, "Asset", map[string]interface{}{"code": id, "category": "Test", "cost": cost,
		"useful_life_years": lifeYears, "capitalisation_date": capDate, "location": "HQ"})
	db.DB.Exec("UPDATE " + schema + ".documents SET status = 'Draft' WHERE id = '" + id + "'")
	if err := CapitalizeAsset("default", id); err != nil {
		t.Fatalf("capitalise %s: %v", id, err)
	}
}

// assetGLNet is the asset's GL by account: debit, credit (paise).
func assetGLNet(t *testing.T, schema, id string) map[string][2]int64 {
	t.Helper()
	rows, err := db.DB.Query("SELECT account_code, SUM(debit), SUM(credit) FROM "+schema+".gl_postings WHERE document_type IN ('Asset', 'Asset-Disposal') AND document_id = $1 GROUP BY account_code", id)
	if err != nil {
		t.Fatalf("gl: %v", err)
	}
	defer rows.Close()
	out := map[string][2]int64{}
	for rows.Next() {
		var code string
		var d, c int64
		rows.Scan(&code, &d, &c)
		out[code] = [2]int64{d, c}
	}
	return out
}

func outcomeFor(result *AssetDepreciationRunResult, id string) AssetDepreciationOutcome {
	for _, o := range result.Assets {
		if o.AssetID == id {
			return o
		}
	}
	return AssetDepreciationOutcome{}
}

func registerEntry(t *testing.T, id string) AssetRegisterEntry {
	t.Helper()
	register, err := GetAssetRegister("default")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	for _, e := range register {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("%s not in the register", id)
	return AssetRegisterEntry{}
}

func monthsAgo(n int) string {
	now := time.Now()
	return time.Date(now.Year(), now.Month()-time.Month(n), 1, 0, 0, 0, 0, time.Local).Format("2006-01-02")
}

func TestAssetDepreciationCatchUpPostsEachMonthOnce(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanupAssetDepreciationTest(schema)
	defer cleanupAssetDepreciationTest(schema)

	// Capitalised long ago: the whole 12-month life is due. 1000 / 12 does
	// not divide, so the last month carries the remainder.
	capitalisedTestAsset(t, schema, "T5516D-A1", 1000, 1, "2019-01-15")
	// Three months into a three-year life.
	capitalisedTestAsset(t, schema, "T5516D-A2", 1200, 3, monthsAgo(3))

	result, err := RunAssetDepreciation(context.Background(), "default")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	a1 := outcomeFor(result, "T5516D-A1")
	if len(a1.MonthsPosted) != 12 || a1.MonthsPosted[0] != "2019-01" || a1.MonthsPosted[11] != "2019-12" || a1.AmountPosted != 1000 {
		t.Fatalf("A1 outcome %+v", a1)
	}
	if a2 := outcomeFor(result, "T5516D-A2"); len(a2.MonthsPosted) != 3 {
		t.Fatalf("A2 outcome %+v", a2)
	}
	gl := assetGLNet(t, schema, "T5516D-A1")
	if gl["1410"][1] != 100000 || gl["5310"][0] != 100000 || gl["1400"][0] != 100000 {
		t.Fatalf("A1 GL after catch-up: %v", gl)
	}
	var last, march int64
	var marchDated string
	db.DB.QueryRow("SELECT credit FROM " + schema + ".gl_postings WHERE idempotency_key = 'Asset:T5516D-A1:DEP:2019-12' AND account_code = '1410'").Scan(&last)
	db.DB.QueryRow("SELECT credit, to_char(created_at, 'YYYY-MM-DD') FROM "+schema+".gl_postings WHERE idempotency_key = 'Asset:T5516D-A1:DEP:2019-03' AND account_code = '1410'").Scan(&march, &marchDated)
	if march != 8333 || last != 100000-11*8333 || marchDated != "2019-03-31" {
		t.Fatalf("monthly split: March %d dated %s, December %d", march, marchDated, last)
	}

	// A second run posts nothing more.
	again, err := RunAssetDepreciation(context.Background(), "default")
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if o := outcomeFor(again, "T5516D-A1"); len(o.MonthsPosted) != 0 {
		t.Fatalf("re-run posted again: %+v", o)
	}
	if o := outcomeFor(again, "T5516D-A2"); len(o.MonthsPosted) != 0 {
		t.Fatalf("re-run posted A2 again: %+v", o)
	}
	if gl2 := assetGLNet(t, schema, "T5516D-A1"); gl2["1410"][1] != 100000 {
		t.Fatalf("A1 GL after re-run: %v", gl2)
	}

	// Register = books: accumulated is what is posted, calculated beside it.
	if e := registerEntry(t, "T5516D-A1"); e.AccumulatedDepreciation != 1000 || e.CalculatedDepreciation != 1000 || e.NetBlock != 0 {
		t.Fatalf("A1 register %+v", e)
	}
	if e := registerEntry(t, "T5516D-A2"); e.AccumulatedDepreciation != 99.99 || e.CalculatedDepreciation != 99.99 || e.NetBlock != 1100.01 {
		t.Fatalf("A2 register %+v", e)
	}
	// Capitalised after the run: the lag shows until the next run.
	capitalisedTestAsset(t, schema, "T5516D-A3", 1200, 3, monthsAgo(2))
	if e := registerEntry(t, "T5516D-A3"); e.AccumulatedDepreciation != 0 || e.CalculatedDepreciation != 66.66 || e.NetBlock != 1200 {
		t.Fatalf("A3 register (not yet run) %+v", e)
	}

	lc, err := GetAssetLifecycle("default", "T5516D-A1")
	if err != nil {
		t.Fatalf("lifecycle: %v", err)
	}
	posted, toCome := 0.0, 0.0
	for _, s := range lc.Schedule {
		posted += s.Posted
		toCome += s.ToCome
	}
	if lc.Depreciation.MonthsPosted != 12 || len(lc.Depreciation.Postings) != 12 || math.Round(posted*100) != 100000 || toCome != 0 || len(lc.Gaps) != 0 {
		t.Fatalf("A1 lifecycle depreciation %+v schedule %+v gaps %v", lc.Depreciation, lc.Schedule, lc.Gaps)
	}
	if lc3, _ := GetAssetLifecycle("default", "T5516D-A3"); lc3 == nil || len(lc3.Depreciation.MonthsNotPosted) != 2 || len(lc3.Gaps) != 1 {
		t.Fatalf("A3 lifecycle should report a 2-month lag: %+v", lc3)
	}

	// The monthly job, through the runner: it clears A3's lag.
	jobKey := "test-5516d-" + time.Now().Format("150405.000000")
	jobID, err := EnqueueJob("default", AssetDepreciationJobType, nil, jobKey)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".async_jobs WHERE id = $1", jobID)
	runClaimedJobs(schema)
	var jobStatus string
	db.DB.QueryRow("SELECT status FROM "+schema+".async_jobs WHERE id = $1", jobID).Scan(&jobStatus)
	if e := registerEntry(t, "T5516D-A3"); jobStatus != "Succeeded" || e.AccumulatedDepreciation != 66.66 {
		t.Fatalf("job %s: A3 register %+v", jobStatus, e)
	}
}

func TestAssetDepreciationSkipsClosedMonthUntilReopened(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanupAssetDepreciationTest(schema)
	defer cleanupAssetDepreciationTest(schema)

	capitalisedTestAsset(t, schema, "T5516D-B1", 600, 1, "2018-01-10")
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".accounting_periods (period_name, start_date, end_date, status, created_by) VALUES ('T5516D Mar 2018', '2018-03-01', '2018-03-31', 'Closed', 'test')"); err != nil {
		t.Fatalf("period: %v", err)
	}
	result, err := RunAssetDepreciation(context.Background(), "default")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	b1 := outcomeFor(result, "T5516D-B1")
	if len(b1.MonthsPosted) != 11 || len(b1.Skipped) != 1 || b1.Skipped[0].Month != "2018-03" {
		t.Fatalf("closed month not skipped: %+v", b1)
	}
	var marchRows int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".gl_postings WHERE idempotency_key = 'Asset:T5516D-B1:DEP:2018-03'").Scan(&marchRows)
	if marchRows != 0 {
		t.Fatalf("a closed month was posted (%d rows)", marchRows)
	}
	lc, _ := GetAssetLifecycle("default", "T5516D-B1")
	if lc == nil || len(lc.Gaps) != 1 {
		t.Fatalf("lifecycle should name the closed month: %+v", lc)
	}
	preview, err := PreviewAssetDisposal("default", "T5516D-B1", 0, "")
	if err != nil || len(preview.DepreciationSkipped) != 1 || preview.AccumulatedDepreciation != 550 {
		t.Fatalf("preview with a closed month: %+v, %v", preview, err)
	}

	// Still closed: still skipped, nothing new.
	again, _ := RunAssetDepreciation(context.Background(), "default")
	if o := outcomeFor(again, "T5516D-B1"); len(o.MonthsPosted) != 0 || len(o.Skipped) != 1 {
		t.Fatalf("second run while closed: %+v", o)
	}

	// Reopened: the month posts, dated in its own month, and the life adds up.
	db.DB.Exec("UPDATE " + schema + ".accounting_periods SET status = 'Open' WHERE period_name = 'T5516D Mar 2018'")
	reopened, _ := RunAssetDepreciation(context.Background(), "default")
	if o := outcomeFor(reopened, "T5516D-B1"); len(o.MonthsPosted) != 1 || o.MonthsPosted[0] != "2018-03" {
		t.Fatalf("after reopen: %+v", o)
	}
	if gl := assetGLNet(t, schema, "T5516D-B1"); gl["1410"][1] != 60000 || gl["5310"][0] != 60000 {
		t.Fatalf("B1 GL after reopen: %v", gl)
	}
}

func TestAssetDisposalPostsProceedsAndGainOrLoss(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanupAssetDepreciationTest(schema)
	defer cleanupAssetDepreciationTest(schema)

	// 2400 over two years is 100 a month; six months have ended, nothing is
	// posted yet - the disposal posts them first. Book value 1800.
	capitalisedTestAsset(t, schema, "T5516D-C1", 2400, 2, monthsAgo(6))
	capitalisedTestAsset(t, schema, "T5516D-C2", 2400, 2, monthsAgo(6))
	capitalisedTestAsset(t, schema, "T5516D-C3", 2400, 2, monthsAgo(6))

	if _, err := DisposeAssetWithProceeds("default", "T5516D-C2", "Sale", -5, ""); validationCode(err) != "ASSET-0537" {
		t.Fatalf("negative proceeds: %v", err)
	}
	if _, err := DisposeAssetWithProceeds("default", "T5516D-C2", "Sale", 100, "4100"); validationCode(err) != "ASSET-0538" {
		t.Fatalf("proceeds into a revenue account: %v", err)
	}

	preview, err := PreviewAssetDisposal("default", "T5516D-C1", 2000, "1100")
	if err != nil || len(preview.DepreciationFirst) != 6 || preview.BookValue != 1800 || preview.GainLoss != 200 || len(preview.ReceiptAccounts) == 0 {
		t.Fatalf("preview: %+v, %v", preview, err)
	}
	if gl := assetGLNet(t, schema, "T5516D-C1"); gl["1410"][1] != 0 {
		t.Fatalf("the preview posted something: %v", gl)
	}

	// Above book value: a gain.
	sale, err := DisposeAssetWithProceeds("default", "T5516D-C1", "Sale", 2000, "")
	if err != nil || sale.GainLoss != 200 || sale.AccumulatedDepreciation != 600 || len(sale.DepreciationFirst) != 6 {
		t.Fatalf("sale: %+v, %v", sale, err)
	}
	gl := assetGLNet(t, schema, "T5516D-C1")
	if gl["1400"][0]-gl["1400"][1] != 0 || gl["1410"][0]-gl["1410"][1] != 0 || gl["1100"][0] != 200000 || gl["4300"][1] != 20000 || gl["5300"][0] != 0 || gl["5310"][0] != 60000 {
		t.Fatalf("gain entry: %v", gl)
	}

	// Below book value: a loss.
	scrap, err := DisposeAssetWithProceeds("default", "T5516D-C2", "Scrap", 1000, "1100")
	if err != nil || scrap.GainLoss != -800 {
		t.Fatalf("scrap: %+v, %v", scrap, err)
	}
	gl = assetGLNet(t, schema, "T5516D-C2")
	if gl["1400"][0]-gl["1400"][1] != 0 || gl["1410"][0]-gl["1410"][1] != 0 || gl["1100"][0] != 100000 || gl["5300"][0] != 80000 || gl["4300"][1] != 0 {
		t.Fatalf("loss entry: %v", gl)
	}

	// The old signature: no proceeds, the book value is the loss.
	if err := DisposeAsset("default", "T5516D-C3", "WriteOff"); err != nil {
		t.Fatalf("DisposeAsset: %v", err)
	}
	gl = assetGLNet(t, schema, "T5516D-C3")
	if gl["1400"][0]-gl["1400"][1] != 0 || gl["1410"][0]-gl["1410"][1] != 0 || gl["5300"][0] != 180000 {
		t.Fatalf("write-off entry: %v", gl)
	}

	// Disposed is final; the run leaves it alone.
	if _, err := DisposeAssetWithProceeds("default", "T5516D-C1", "Sale", 10, ""); validationCode(err) != "ASSET-0270" {
		t.Fatalf("disposed twice: %v", err)
	}
	result, _ := RunAssetDepreciation(context.Background(), "default")
	if o := outcomeFor(result, "T5516D-C1"); len(o.MonthsPosted) != 0 {
		t.Fatalf("a disposed asset was depreciated: %+v", o)
	}
	lc, _ := GetAssetLifecycle("default", "T5516D-C1")
	if lc == nil || len(lc.Gaps) != 0 || lc.Acquisition["disposal_gain_loss"] != 200.0 {
		t.Fatalf("disposed lifecycle: %+v", lc)
	}
}
