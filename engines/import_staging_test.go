package engines

import (
	"bytes"
	"context"
	"custom_erp/db"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"
)

// Stage 47.13.1 - the staged import. Every test runs in its own provisioned
// client, so the approval rule and the self-approval setting it changes are
// never the shared default client's.

const stagingDoctype = "POSCostingGap"

func stagingTenant(t *testing.T) (tenantID, schema string) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema = uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	for _, u := range []string{"stg_maker", "stg_checker"} {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.users (id, username, password_hash, role, status, location_code)
			VALUES ($1, $1, 'x', $2, 'Active', 'HO') ON CONFLICT (id) DO NOTHING`, schema), u, RoleSuperAdmin); err != nil {
			t.Fatalf("seed user %s: %v", u, err)
		}
	}
	return tenantID, schema
}

func stagingCSV(rows ...string) string {
	return "id,cart_number,sku,qty,sale_value,status\n" + strings.Join(rows, "\n") + "\n"
}

func stagingCode(err error) string {
	var verr *ValidationError
	if errors.As(err, &verr) {
		return verr.Code
	}
	return ""
}

// stageAndCheck stages a file and takes it to previewed.
func stageAndCheck(t *testing.T, tenantID, file string) *ImportBatchView {
	t.Helper()
	ctx := context.Background()
	v, err := StageImportBatch(ctx, tenantID, stagingDoctype, strings.NewReader(file), "f.csv", "stg_maker", RoleSuperAdmin)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, err = ValidateImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if v, err = PreviewImportBatch(ctx, tenantID, v.ID); err != nil {
		t.Fatalf("preview: %v", err)
	}
	return v
}

func countDocs(t *testing.T, schema string) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL`, schema), stagingDoctype).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStage471311StagedImportEndToEnd(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	ctx := context.Background()
	v := stageAndCheck(t, tenantID, stagingCSV(
		"S1-A,C1,SKU1,2,100.50,Open",
		"S1-B,C1,SKU2,3,200.25,Open",
		"S1-C,C1,SKU3,abc,50,Open", // qty is not a number
		"S1-A,C1,SKU9,1,1,Open"))   // repeats S1-A
	if v.Stage != ImportStagePreviewed {
		t.Fatalf("stage %s, want previewed", v.Stage)
	}
	c := v.Counts
	if c.Rows != 4 || c.Valid != 2 || c.Invalid != 1 || c.Duplicate != 1 || c.New != 2 {
		t.Fatalf("counts %+v, want rows 4, valid 2, invalid 1, duplicate 1, new 2", c)
	}
	if v.Preview == nil || len(v.Preview.NewRows) != 2 || len(v.Preview.Errors) != 1 || len(v.Preview.Duplicates) != 1 {
		t.Fatalf("preview %+v", v.Preview)
	}
	if countDocs(t, schema) != 0 {
		t.Fatal("staging and previewing wrote records")
	}

	// A client with no approval rule approves directly.
	if v, _ = ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); v.Status != "Approved" || v.Stage != ImportStageApproved {
		t.Fatalf("approve: %s / %s", v.Status, v.Stage)
	}
	done, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if done.Stage != ImportStageReconciled || done.Status != "Reconciled" || done.Reconcile == nil || !done.Reconcile.Balanced {
		t.Fatalf("after commit: %s / %s, reconcile %+v", done.Stage, done.Status, done.Reconcile)
	}
	if done.Reconcile.Committed != 2 || done.Reconcile.Rejected != 2 || done.Reconcile.Created != 2 {
		t.Fatalf("reconcile %+v", done.Reconcile)
	}
	if n := countDocs(t, schema); n != 2 {
		t.Fatalf("%d records, want 2", n)
	}

	// A second file naming S1-A changes it: matched as an update, and the
	// preview shows what would change.
	up := stageAndCheck(t, tenantID, stagingCSV("S1-A,,,,999,"))
	if up.Counts.Update != 1 || len(up.Preview.Updates) != 1 {
		t.Fatalf("update batch %+v / %+v", up.Counts, up.Preview)
	}
	var sawChange bool
	for _, ch := range up.Preview.Updates[0].Changes {
		if ch.Field == "sale_value" && ch.To == "999" && ch.From != "999" {
			sawChange = true
		}
	}
	if !sawChange {
		t.Fatalf("the preview did not show sale_value changing: %+v", up.Preview.Updates[0])
	}
	if _, err := ApproveImportBatch(ctx, tenantID, up.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	if up, err = CommitImportBatch(ctx, tenantID, up.ID, "stg_maker", RoleSuperAdmin); err != nil || up.Reconcile == nil || !up.Reconcile.Balanced || up.Reconcile.Updated != 1 {
		t.Fatalf("update commit: %v / %+v", err, up.Reconcile)
	}
	if n := countDocs(t, schema); n != 2 {
		t.Fatalf("an update added a record: %d", n)
	}

	// The same file again is the batch already made.
	again, err := StageImportBatch(ctx, tenantID, stagingDoctype, strings.NewReader(stagingCSV("S1-A,,,,999,")), "g.csv", "stg_maker", RoleSuperAdmin)
	if err != nil || !again.AlreadyStaged || again.ID != up.ID {
		t.Fatalf("restaging: %v / %+v", err, again)
	}
}

func TestStage471311UploadSafety(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	ctx := context.Background()
	stage := func(b []byte) (*ImportBatchView, error) {
		return StageImportBatch(ctx, tenantID, stagingDoctype, bytes.NewReader(b), "f.csv", "stg_maker", RoleSuperAdmin)
	}

	// Oversize.
	if _, err := stage(bytes.Repeat([]byte("a,b,c\n"), ImportStagingMaxBytes/6+10)); stagingCode(err) != "DATAIM-0480" {
		t.Fatalf("oversize: %v, want DATAIM-0480", err)
	}
	// Wrong content: a PNG named .csv.
	if _, err := stage(append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00"), bytes.Repeat([]byte{0}, 64)...)); err == nil {
		t.Fatal("a PNG was staged as a CSV")
	}
	// Nothing but a header.
	if _, err := stage([]byte("id,cart_number\n")); stagingCode(err) != "DATAIM-0163" {
		t.Fatalf("empty: %v, want DATAIM-0163", err)
	}
	// A missing mandatory column.
	if _, err := stage([]byte("cart_number\nC1\n")); stagingCode(err) != "DATAIM-0164" {
		t.Fatalf("missing column: %v, want DATAIM-0164", err)
	}
	// A type that is not importable.
	if _, err := StageImportBatch(ctx, tenantID, "NoSuchType", strings.NewReader("a\n1\n"), "f.csv", "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0481" {
		t.Fatalf("unknown type: %v", err)
	}
	if countDocs(t, schema) != 0 {
		t.Fatal("a refused upload left records")
	}

	// Encodings Excel writes: UTF-8 with BOM, UTF-16 with BOM, Windows-1252.
	want := "S2-é"
	file := stagingCSV(want + ",C1,SKU1,1,1,Open")
	u16 := utf16.Encode([]rune(file))
	utf16Bytes := []byte{0xFF, 0xFE}
	for _, u := range u16 {
		utf16Bytes = append(utf16Bytes, byte(u), byte(u>>8))
	}
	cp := bytes.ReplaceAll([]byte(file), []byte("é"), []byte{0xE9}) // é in Windows-1252
	for name, body := range map[string][]byte{
		"utf-8-bom":    append([]byte{0xEF, 0xBB, 0xBF}, []byte(file)...),
		"utf-16":       utf16Bytes,
		"windows-1252": cp,
	} {
		v, err := stage(body)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v.Encoding != name {
			t.Errorf("%s detected as %s", name, v.Encoding)
		}
		rows, _ := ListImportBatchRows(tenantID, v.ID, "", 10, 0)
		if len(rows) != 1 || rows[0].Data["id"] != want {
			t.Errorf("%s: row %+v, want id %q", name, rows, want)
		}
		if _, err := DiscardImportBatch(tenantID, v.ID, "stg_maker"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStage471311ApprovalAndRefusals(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	ctx := context.Background()
	withSelfApproval(t, tenantID, SelfApprovalNobody)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.approval_rules (doctype, min_amount, required_role) VALUES ('ImportBatch', 0, 'HR/Admin') ON CONFLICT (doctype, min_amount) DO NOTHING`, schema)); err != nil {
		t.Fatal(err)
	}
	v := stageAndCheck(t, tenantID, stagingCSV("S3-A,C1,SKU1,1,1,Open"))

	// Not approved yet: no commit.
	if _, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0483" {
		t.Fatalf("commit before approval: %v, want DATAIM-0483", err)
	}
	// Sent for approval; sent again changes nothing.
	sent, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin)
	if err != nil || sent.Status != "Pending Approval" || sent.Stage != ImportStagePendingApproval {
		t.Fatalf("send: %v / %+v", err, sent)
	}
	if again, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil || again.Status != "Pending Approval" {
		t.Fatalf("send again: %v", err)
	}
	// Still no commit, and no re-check once it is out for approval.
	if _, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0483" {
		t.Fatalf("commit while pending: %v", err)
	}
	if _, err := ValidateImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0481" {
		t.Fatalf("validate while pending: %v", err)
	}
	// Self-approval: the maker may not decide their own batch.
	if code := sodCode(DecideApproval(tenantID, ImportBatchDoctype, v.ID, "stg_maker", RoleSuperAdmin, "HO", "Approved", "")); code != "APPROV-0840" {
		t.Fatalf("the maker approved their own batch: %q, want APPROV-0840", code)
	}
	if countDocs(t, schema) != 0 {
		t.Fatal("records written before approval")
	}
	// Someone else approves; now it commits.
	if err := DecideApproval(tenantID, ImportBatchDoctype, v.ID, "stg_checker", RoleSuperAdmin, "HO", "Approved", ""); err != nil {
		t.Fatalf("checker approve: %v", err)
	}
	done, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin)
	if err != nil || done.Stage != ImportStageReconciled {
		t.Fatalf("commit: %v / %+v", err, done)
	}
	if countDocs(t, schema) != 1 {
		t.Fatalf("%d records, want 1", countDocs(t, schema))
	}

	// A rejected batch is not committed and its file may be staged again.
	r := stageAndCheck(t, tenantID, stagingCSV("S3-R,C1,SKU1,1,1,Open"))
	if _, err := ApproveImportBatch(ctx, tenantID, r.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(tenantID, ImportBatchDoctype, r.ID, "stg_checker", RoleSuperAdmin, "HO", "Rejected", "no"); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitImportBatch(ctx, tenantID, r.ID, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0483" {
		t.Fatalf("commit of a rejected batch: %v", err)
	}
	if re, err := StageImportBatch(ctx, tenantID, stagingDoctype, strings.NewReader(stagingCSV("S3-R,C1,SKU1,1,1,Open")), "f.csv", "stg_maker", RoleSuperAdmin); err != nil || re.AlreadyStaged {
		t.Fatalf("restaging a rejected file: %v / %+v", err, re)
	}
}

func TestStage471311NothingValidAndStaleRules(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	ctx := context.Background()

	none := stageAndCheck(t, tenantID, stagingCSV("S4-X,C1,SKU1,abc,1,Open"))
	if _, err := ApproveImportBatch(ctx, tenantID, none.ID, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0485" {
		t.Fatalf("approve with nothing valid: %v, want DATAIM-0485", err)
	}

	// A batch checked under older rules is refused at preview, approve and
	// commit, and checking it again brings it up to date.
	v := stageAndCheck(t, tenantID, stagingCSV("S4-A,C1,SKU1,1,1,Open"))
	old := func(id string) {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || '{"rules_version":"2020.01.01"}'::jsonb WHERE id = $1`, schema), id); err != nil {
			t.Fatal(err)
		}
	}
	old(v.ID)
	if _, err := PreviewImportBatch(ctx, tenantID, v.ID); stagingCode(err) != "DATAIM-0482" {
		t.Fatalf("preview under old rules: %v, want DATAIM-0482", err)
	}
	if _, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0482" {
		t.Fatalf("approve under old rules: %v, want DATAIM-0482", err)
	}
	healed, err := ValidateImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin)
	if err != nil || !healed.RulesCurrent || healed.RulesVersion != ImportRulesVersion {
		t.Fatalf("re-check: %v / %+v", err, healed)
	}
	if _, err := PreviewImportBatch(ctx, tenantID, v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	old(v.ID) // the rules move on after approval
	if _, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0482" {
		t.Fatalf("commit under old rules: %v, want DATAIM-0482", err)
	}
	if countDocs(t, schema) != 0 {
		t.Fatal("a stale batch wrote records")
	}
}

func TestStage471311BatchIsPerClient(t *testing.T) {
	tenantA, _ := stagingTenant(t)
	tenantB, _ := stagingTenant(t)
	v := stageAndCheck(t, tenantA, stagingCSV("S5-A,C1,SKU1,1,1,Open"))
	for name, call := range map[string]func() error{
		"load": func() error { _, err := LoadImportBatch(tenantB, v.ID); return err },
		"validate": func() error {
			_, err := ValidateImportBatch(context.Background(), tenantB, v.ID, "stg_maker", RoleSuperAdmin)
			return err
		},
		"approve": func() error {
			_, err := ApproveImportBatch(context.Background(), tenantB, v.ID, "stg_maker", RoleSuperAdmin)
			return err
		},
		"commit": func() error {
			_, err := CommitImportBatch(context.Background(), tenantB, v.ID, "stg_maker", RoleSuperAdmin)
			return err
		},
		"rows": func() error { _, err := ListImportBatchRows(tenantB, v.ID, "", 10, 0); return err },
	} {
		if stagingCode(call()) != "DATAIM-0481" {
			t.Errorf("%s of another client's batch was not refused as not found", name)
		}
	}
	if list, _ := ListImportBatches(tenantB, 50); len(list) != 0 {
		t.Fatalf("client B lists %d batches of client A", len(list))
	}
}

// A crash between chunks leaves the written chunks marked; the next commit
// writes only the rest. Nothing is written twice.
func TestStage471311CommitSurvivesACrashBetweenChunks(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	ctx := context.Background()
	var rows []string
	for i := 0; i < 450; i++ {
		rows = append(rows, fmt.Sprintf("S6-%04d,C1,SKU%d,1,2,Open", i, i))
	}
	v := stageAndCheck(t, tenantID, stagingCSV(rows...))
	if v.Counts.Valid != 450 {
		t.Fatalf("valid %d, want 450", v.Counts.Valid)
	}
	if _, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}

	boom := errors.New("injected crash")
	importCommitAfterChunk = func(chunk int) error {
		if chunk == 0 {
			return boom
		}
		return nil
	}
	_, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin)
	importCommitAfterChunk = nil
	if !errors.Is(err, boom) {
		t.Fatalf("commit: %v, want the injected crash", err)
	}
	mid, _ := LoadImportBatch(tenantID, v.ID)
	if mid.Status != "Approved" || mid.CommitCreated != importCommitChunk {
		t.Fatalf("after the crash: %s, %d created; want Approved and %d", mid.Status, mid.CommitCreated, importCommitChunk)
	}
	if n := countDocs(t, schema); n != importCommitChunk {
		t.Fatalf("%d records after the crash, want %d", n, importCommitChunk)
	}
	if cnt, _ := importRowCounts(schema, v.ID); cnt.Committed != importCommitChunk || cnt.Valid != 250 {
		t.Fatalf("rows after the crash: %+v", cnt)
	}

	done, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if n := countDocs(t, schema); n != 450 {
		t.Fatalf("%d records after resuming, want 450", n)
	}
	if done.CommitCreated != 450 || done.Reconcile == nil || !done.Reconcile.Balanced {
		t.Fatalf("resumed batch: created %d, reconcile %+v", done.CommitCreated, done.Reconcile)
	}

	// A double commit: the recorded result, nothing written again.
	twice, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin)
	if err != nil || !twice.AlreadyCommitted || twice.CommitCreated != 450 {
		t.Fatalf("second commit: %v / %+v", err, twice)
	}
	if n := countDocs(t, schema); n != 450 {
		t.Fatalf("%d records after a double commit, want 450", n)
	}
}

// Two commits at once: one runs, the other is told so.
func TestStage471311ConcurrentCommitIsRefused(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	ctx := context.Background()
	v := stageAndCheck(t, tenantID, stagingCSV("S7-A,C1,SKU1,1,1,Open"))
	if _, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('commit_lease_until', to_char((now() + interval '5 minutes') AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')) WHERE id = $1`, schema), v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0484" {
		t.Fatalf("commit while another runs: %v, want DATAIM-0484", err)
	}
	if countDocs(t, schema) != 0 {
		t.Fatal("a refused commit wrote records")
	}
}

// A row the importer refuses at commit (the data moved since the check) is
// marked Failed; the rest go in.
func TestStage471311RowRefusedAtCommitDoesNotBlockTheRest(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	ctx := context.Background()
	v := stageAndCheck(t, tenantID, stagingCSV("S8-A,C1,SKU1,1,1,Open", "S8-B,C1,SKU2,1,1,Open", "S8-C,C1,SKU3,1,1,Open"))
	if _, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	// After the check, a row's staged cell goes bad (as if the rules or the data moved).
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.import_staging_rows SET normalized = jsonb_set(normalized, '{qty}', '"abc"') WHERE batch_id = $1 AND row_no = 3`, schema), v.ID); err != nil {
		t.Fatal(err)
	}
	done, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if done.Counts.Committed != 2 || done.Counts.Failed != 1 || countDocs(t, schema) != 2 {
		t.Fatalf("counts %+v, %d records; want 2 committed, 1 failed, 2 records", done.Counts, countDocs(t, schema))
	}
	failed, _ := ListImportBatchRows(tenantID, v.ID, "Failed", 10, 0)
	if len(failed) != 1 || failed[0].RowNo != 3 || len(failed[0].Errors) == 0 {
		t.Fatalf("failed rows %+v", failed)
	}
}

// Reconcile goes red when the books do not tie out.
func TestStage471311ReconcileCatchesDifferences(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	ctx := context.Background()
	v := stageAndCheck(t, tenantID, stagingCSV("S9-A,C1,SKU1,1,10,Open", "S9-B,C1,SKU2,2,20,Open"))
	if _, err := ApproveImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	done, err := CommitImportBatch(ctx, tenantID, v.ID, "stg_maker", RoleSuperAdmin)
	if err != nil || !done.Reconcile.Balanced {
		t.Fatalf("commit: %v / %+v", err, done.Reconcile)
	}
	if _, err := ReconcileImportBatch(ctx, tenantID, v.ID, "stg_maker"); err != nil {
		t.Fatalf("reconciling again: %v", err)
	}

	// A committed record removed behind the batch's back.
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET deleted_at = NOW() WHERE doctype = $1 AND id = 'S9-A'`, schema), stagingDoctype); err != nil {
		t.Fatal(err)
	}
	r, err := ReconcileImportBatch(ctx, tenantID, v.ID, "stg_maker")
	if err != nil || r.Reconcile.Balanced || r.Reconcile.KeysMissing != 1 || r.Stage != ImportStageCommitted || r.Status != "Committed" {
		t.Fatalf("a missing record was not caught: %v / %s / %+v", err, r.Stage, r.Reconcile)
	}
	// Put it back; it ties out again.
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET deleted_at = NULL WHERE doctype = $1 AND id = 'S9-A'`, schema), stagingDoctype); err != nil {
		t.Fatal(err)
	}
	if r, err = ReconcileImportBatch(ctx, tenantID, v.ID, "stg_maker"); err != nil || !r.Reconcile.Balanced {
		t.Fatalf("restored: %v / %+v", err, r.Reconcile)
	}

	// A staged row lost: the file's rows and totals no longer add up.
	if _, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.import_staging_rows WHERE batch_id = $1 AND row_no = 3`, schema), v.ID); err != nil {
		t.Fatal(err)
	}
	if r, err = ReconcileImportBatch(ctx, tenantID, v.ID, "stg_maker"); err != nil || r.Reconcile.Balanced || len(r.Reconcile.Issues) < 2 {
		t.Fatalf("a lost row was not caught: %v / %+v", err, r.Reconcile)
	}
}

// Raw-file expiry: staged rows of settled batches go after the retention;
// a batch waiting on a person never does; and a retention of 0 sweeps nothing.
func TestStage471311ExpirySweep(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	ctx := context.Background()
	settled := stageAndCheck(t, tenantID, stagingCSV("S10-A,C1,SKU1,1,1,Open"))
	if _, err := ApproveImportBatch(ctx, tenantID, settled.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitImportBatch(ctx, tenantID, settled.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	waiting := stageAndCheck(t, tenantID, stagingCSV("S10-B,C1,SKU1,1,1,Open"))
	if _, err := ApproveImportBatch(ctx, tenantID, waiting.ID, "stg_maker", RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	abandoned := stageAndCheck(t, tenantID, stagingCSV("S10-C,C1,SKU1,1,1,Open"))
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET created_at = NOW() - interval '30 days' WHERE doctype = 'ImportBatch'`, schema)); err != nil {
		t.Fatal(err)
	}
	rowsOf := func(id string) int {
		var n int
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.import_staging_rows WHERE batch_id = $1`, schema), id).Scan(&n)
		return n
	}

	// Retention off (the default): nothing is swept.
	if res, err := SweepImportFileRetention(tenantID); err != nil || res.Batches != 0 || rowsOf(settled.ID) == 0 {
		t.Fatalf("sweep with retention off: %v / %+v", err, res)
	}
	if err := SetSetting(tenantID, importFileRetentionSetting, "7", "test"); err != nil {
		t.Fatal(err)
	}
	res, err := SweepImportFileRetention(tenantID)
	if err != nil || res.Batches != 2 {
		t.Fatalf("sweep: %v / %+v, want 2 batches", err, res)
	}
	if rowsOf(settled.ID) != 0 || rowsOf(abandoned.ID) != 0 {
		t.Fatal("staged rows of settled / abandoned batches survived the sweep")
	}
	if rowsOf(waiting.ID) == 0 {
		t.Fatal("the sweep removed rows of a batch that is approved and waiting")
	}
	b, _ := LoadImportBatch(tenantID, settled.ID)
	if b.Status != "Expired" || b.Stage != ImportStageExpired || b.ExpiredAt == "" || b.Reconcile == nil || b.Counts.Committed != 1 {
		t.Fatalf("expired batch %s / %s / %+v: counts and reconciliation must survive", b.Status, b.Stage, b)
	}
	if n := countDocs(t, schema); n != 1 {
		t.Fatalf("expiry touched the imported records: %d", n)
	}
	// An expired batch cannot be acted on, and its file may be staged again.
	if _, err := CommitImportBatch(ctx, tenantID, settled.ID, "stg_maker", RoleSuperAdmin); stagingCode(err) != "DATAIM-0483" {
		t.Fatalf("commit of an expired batch: %v", err)
	}
}
