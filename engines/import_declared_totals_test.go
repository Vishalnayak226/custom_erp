package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
)

// 47.13.3 / 49.8.2: stated totals gate approval, and a reconciled staged
// batch writes one ImportJob with its control totals and file lineage.

func stageWithTotals(t *testing.T, tenantID, file, stated string) *ImportBatchView {
	t.Helper()
	ctx := context.Background()
	v, err := StageImportBatch(ctx, tenantID, stagingDoctype, strings.NewReader(file), "open.csv", "stg_maker", RoleSuperAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ValidateImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	if stated != "" {
		if v, err = SetImportBatchDeclaredTotals(tenantID, v.ID, stated); err != nil {
			t.Fatalf("declare: %v", err)
		}
	}
	if v, err = PreviewImportBatch(ctx, tenantID, v.ID); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestStage4713StatedTotalMismatchRefusedAtApproval(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	ctx := context.Background()
	v := stageWithTotals(t, tenantID, stagingCSV("D1-A,C1,SKU1,2,100.50,Open", "D1-B,C1,SKU2,3,200.25,Open"), "sale_value=999")
	if _, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0487" {
		t.Fatalf("approve with a total that does not tie: %v", err)
	}
	if countDocs(t, schema) != 0 {
		t.Fatal("a refused batch wrote records")
	}
	if _, err := SetImportBatchDeclaredTotals(tenantID, v.ID, "nosuchcol=1"); stagingCode(err) != "DATAIM-0487" {
		t.Fatalf("unknown column: %v", err)
	}
	if _, err := SetImportBatchDeclaredTotals(tenantID, v.ID, "sale_value=abc"); stagingCode(err) != "DATAIM-0487" {
		t.Fatalf("not a number: %v", err)
	}
	// Corrected, it goes through.
	if _, err := SetImportBatchDeclaredTotals(tenantID, v.ID, "sale_value=300.75"); err != nil {
		t.Fatal(err)
	}
	if _, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatalf("approve after the total ties: %v", err)
	}
	if _, err := SetImportBatchDeclaredTotals(tenantID, v.ID, "sale_value=1"); stagingCode(err) != "DATAIM-0481" {
		t.Fatalf("totals changed after approval: %v", err)
	}
}

func TestStage4713StagedBatchWritesOneImportJob(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	ctx := context.Background()
	v := stageWithTotals(t, tenantID, stagingCSV("D2-A,C1,SKU1,2,100.50,Open", "D2-B,C1,SKU2,3,200.25,Open"), "sale_value=300.75")
	if _, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	done, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin)
	if err != nil || !done.Reconcile.Balanced || done.ImportJobID == "" {
		t.Fatalf("commit: %v / %+v / job %q", err, done.Reconcile, done.ImportJobID)
	}
	if again, err := ReconcileImportBatch(ctx, tenantID, v.ID, "stg_maker"); err != nil || again.ImportJobID != done.ImportJobID {
		t.Fatalf("reconcile again: %v / %q vs %q", err, again.ImportJobID, done.ImportJobID)
	}
	var n int
	var sha, batch, totals, rules string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ImportJob' AND data->>'batch_id' = $1`, schema), v.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d import jobs for the batch (%v), want 1", n, err)
	}
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->'source'->>'sha256', data->>'batch_id', data->'control_totals'->>'rows_accepted', data->>'rules_version'
		FROM %s.documents WHERE doctype = 'ImportJob' AND id = $1`, schema), done.ImportJobID).Scan(&sha, &batch, &totals, &rules); err != nil {
		t.Fatal(err)
	}
	if sha != done.SHA256 || batch != v.ID || totals != "2" || rules != ImportRulesVersion {
		t.Fatalf("job lineage: sha %q batch %q accepted %q rules %q", sha, batch, totals, rules)
	}
}

func TestStage4713RejectedRowBreaksStatedTotalAtReconcile(t *testing.T) {
	tenantID, _ := stagingTenant(t)
	ctx := context.Background()
	// The file really holds 300.75 + 50 = 350.75, stated as such, but one row
	// is refused (qty is not a number), so the committed total falls short.
	v := stageWithTotals(t, tenantID, stagingCSV("D3-A,C1,SKU1,2,100.50,Open", "D3-B,C1,SKU2,3,200.25,Open", "D3-C,C1,SKU3,abc,50,Open"), "sale_value=350.75")
	if _, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	done, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if done.Reconcile.Balanced || done.Stage != ImportStageCommitted {
		t.Fatalf("a short committed total reconciled: %+v", done.Reconcile)
	}
	if !strings.Contains(strings.Join(done.Reconcile.Issues, "|"), "stated 350.75, committed 300.75") {
		t.Fatalf("issues %v", done.Reconcile.Issues)
	}
}
