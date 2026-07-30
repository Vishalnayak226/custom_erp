-- Stage 29: reporting index on gl_postings (QC_EXHAUSTIVE_REPORT.md
-- observation O1 / R2 scenario 21 - "10 million gl_postings rows").
--
-- The finding: GetTrialBalance (engines/finance.go) LEFT JOINs gl_accounts to
-- gl_postings on account_code with NO date filter and aggregates SUM(debit)/
-- SUM(credit) grouped by account_code. Today the only indexes on gl_postings
-- are the posting_id PK and the partial idx_gl_postings_idempotency_key
-- (Stage 24.5), so that report is a full sequential scan of the whole table
-- every time - fine at today's row counts, slow at 10M+.
--
-- Why INCLUDE (debit, credit, cost_center) and not the bare
-- (account_code, created_at) the QC report suggested: a bare composite index
-- would NOT actually fix anything. These reports need debit/credit for every
-- matching row, so with a non-covering index the planner has to visit the heap
-- once per row - strictly worse than a seq scan, so it would simply never
-- choose the index and the migration would be a no-op that costs write
-- throughput for nothing. This was measured, not assumed: on a 1M-row
-- gl_postings the planner produced a byte-identical Seq Scan plan with the
-- bare index present and absent. Adding the payload columns (B-tree INCLUDE,
-- Postgres 11+; this repo targets postgres:16 per docker-compose.yml) makes
-- the aggregates satisfiable by an index-only scan (Heap Fetches: 0).
-- gl_postings is append-only - nothing rewrites a posted row except bank
-- reconciliation stamping matched_statement_line_id - so its visibility map
-- stays almost entirely all-visible and index-only scans stay effective.
-- cost_center is in the payload because GetCostCenterPL (gl_cost_center.go)
-- groups by it; on the same 1M-row table that report went 95ms/20,359 buffers
-- -> 8.6ms/158 buffers, for 8MB (39MB -> 47MB) of extra index against a 159MB
-- heap. `department` is deliberately NOT included - no report groups by it
-- today, so it would be dead payload on every row.
--
-- created_at is the second key column so the index also range-seeks the dated
-- account-anchored reports: GetProfitAndLoss / GetBalanceSheet / GetTaxLedger-
-- Report / GetCashFlowStatement (finance_reports_stage26.go), GetCostCenterPL,
-- glAccountNetBalance + the GST txn count (reports.go), gstReturnDrillDown
-- (report_definitions.go). Those queries used to spell the date predicate
-- `created_at::date BETWEEN $1 AND $2`; the cast wraps the indexed column in a
-- function call, so it could never be a range seek. They were rewritten to the
-- equivalent half-open form `created_at >= $1::date AND created_at < ($2::date
-- + 1)` alongside this migration - the index and that rewrite only deliver the
-- speedup together, so neither is useful shipped alone. See the "Date-range
-- convention" comment at the top of engines/finance_reports_stage26.go.
--
-- Two GL reports are deliberately NOT helped by this index, both recorded as
-- follow-ups in docs/micro_checklist.md:
--   * GetTrialBalance - aggregates the ENTIRE ledger with no date filter at
--     all, so it must touch every row by definition; no index can fix an
--     unbounded full aggregate. Bounding it to a period/as-of date is the real
--     fix and that is a product decision, not a mechanical one.
--   * GetStatutoryGLExport - filters on the date alone with no account_code,
--     so it cannot seek on an account_code-leading index. It is an async
--     background CSV export by design (CreateReportExportJob), not an
--     interactive screen, so it is left to seq-scan rather than paying for a
--     second index on every posting write.
--
-- Additive and idempotent per this repo's migration convention. This is a
-- plain (non-CONCURRENT) CREATE INDEX because CONCURRENTLY cannot run inside
-- the transaction a DO block / migrate.sh statement implies; on an existing
-- large table it takes a ShareLock that blocks writes to gl_postings for the
-- duration of the build, so apply it in a deploy window like any other
-- migration.
CREATE INDEX IF NOT EXISTS idx_gl_postings_account_created
    ON tenant_default.gl_postings (account_code, created_at)
    INCLUDE (debit, credit, cost_center);

-- Backfill every already-provisioned tenant schema. New tenants pick this up
-- automatically - engines/saas.go's ProvisionTenantSchema clones gl_postings
-- with `LIKE tenant_default.gl_postings INCLUDING ALL`, and INCLUDING ALL
-- covers INCLUDING INDEXES. Same shape as the Stage 27 backfill loop in
-- migrations_stage27_product_packaging.sql.
--
-- The guard tests for an existing index BY DEFINITION, not by name, which
-- matters here: `LIKE ... INCLUDING ALL` copies indexes but does NOT preserve
-- their names - a tenant provisioned after this migration gets the same index
-- under a server-generated name like
-- gl_postings_account_code_created_at_debit_credit_cost_cente_idx (the
-- pre-existing gl_postings_idempotency_key_idx clones the same way). A plain
-- `CREATE INDEX IF NOT EXISTS idx_gl_postings_account_created` only matches on
-- the name, so it would happily build a SECOND, redundant copy of the same
-- index on that tenant's ledger - doubling the write cost of every posting for
-- no benefit. Matching on the key columns avoids that.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    -- Guard rather than assume: a schema mid-provision may not have the table yet.
    IF to_regclass(format('%I.gl_postings', schema_rec.schema_name)) IS NOT NULL
       AND NOT EXISTS (
         SELECT 1 FROM pg_indexes
         WHERE schemaname = schema_rec.schema_name
           AND tablename  = 'gl_postings'
           AND indexdef LIKE '%(account_code, created_at)%'
       )
    THEN
      EXECUTE format(
        'CREATE INDEX idx_gl_postings_account_created ON %I.gl_postings (account_code, created_at) INCLUDE (debit, credit, cost_center)',
        schema_rec.schema_name
      );
    END IF;
  END LOOP;
END $$;
