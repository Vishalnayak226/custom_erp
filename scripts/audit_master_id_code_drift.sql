-- Stage 51.1a - READ-ONLY audit: Master records stranded with id <> code.
--
-- WHAT THIS IS FOR
-- ----------------
-- Stage 51.1 restored the invariant that a Master record's `id` equals its
-- `code`. Every Link field in this system validates by *id*
-- (verifyDocumentExists checks WHERE id = $2), never by code, so a Master row
-- whose id does not equal its code can never be selected by any Link field -
-- a Vendor that exists but which no Purchase Order can reference.
--
-- Records created BEFORE that fix shipped still carry a stray generated id.
-- This script counts them, per doctype, so the backfill decision can be made
-- against real numbers instead of a guess.
--
-- SAFETY
-- ------
-- Every statement here is a SELECT. Nothing is written, nothing is locked
-- beyond a normal read. It is deliberately NOT in db/ because db/migrate.go
-- executes every .sql file in that directory - this file must never run as a
-- migration.
--
-- HOW TO RUN
-- ----------
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f scripts/audit_master_id_code_drift.sql
--
-- That audits tenant_minn, the default below. To audit a different tenant,
-- pass it in rather than editing this file:
--
--   psql "$DATABASE_URL" -v schema=tenant_default -f scripts/audit_master_id_code_drift.sql
--
-- Section 6 gives a headline for every tenant in one pass regardless.

\if :{?schema}
\else
  \set schema tenant_minn
\endif

\echo ''
\echo '=== 1. Scale: how many Master rows are stranded, per doctype ==========='
\echo ''

SELECT
    d.doctype,
    COUNT(*)                                                   AS stranded_rows,
    MIN(d.created_at)::date                                    AS oldest,
    MAX(d.created_at)::date                                    AS newest
FROM :schema.documents d
JOIN :schema.doctype_meta m ON m.name = d.doctype
WHERE m.document_type = 'Master'
  AND d.deleted_at IS NULL
  AND COALESCE(d.data->>'code', '') <> ''
  AND d.id <> d.data->>'code'
GROUP BY d.doctype
ORDER BY stranded_rows DESC, d.doctype;

\echo ''
\echo '=== 2. Totals: stranded vs. healthy, so the ratio is visible ==========='
\echo ''

SELECT
    COUNT(*) FILTER (WHERE d.id <> d.data->>'code')            AS stranded,
    COUNT(*) FILTER (WHERE d.id =  d.data->>'code')            AS healthy,
    COUNT(*) FILTER (WHERE COALESCE(d.data->>'code','') = '')  AS no_code_at_all,
    COUNT(*)                                                   AS master_rows_total
FROM :schema.documents d
JOIN :schema.doctype_meta m ON m.name = d.doctype
WHERE m.document_type = 'Master'
  AND d.deleted_at IS NULL;

\echo ''
\echo '=== 3. Collision check - MUST be empty before any backfill ============='
\echo ''
\echo '    A backfill would set id = code. That is only safe where no other'
\echo '    row of the same doctype ALREADY occupies that id. Any row listed'
\echo '    here cannot be backfilled by a plain UPDATE and needs a decision'
\echo '    of its own: two records are claiming one identity.'
\echo ''

SELECT
    d.doctype,
    d.id                      AS stranded_id,
    d.data->>'code'           AS wanted_id,
    other.id                  AS already_taken_by,
    other.created_at::date    AS taken_since
FROM :schema.documents d
JOIN :schema.doctype_meta m ON m.name = d.doctype
JOIN :schema.documents other
      ON other.doctype = d.doctype
     AND other.id = d.data->>'code'
     AND other.id <> d.id
WHERE m.document_type = 'Master'
  AND d.deleted_at IS NULL
  AND COALESCE(d.data->>'code', '') <> ''
  AND d.id <> d.data->>'code'
ORDER BY d.doctype, d.id;

\echo ''
\echo '=== 4. Blast radius - which transactions already point at a stray id ==='
\echo ''
\echo '    These are the references a backfill would have to carry along. An'
\echo '    UPDATE that changes a Master id WITHOUT rewriting these leaves the'
\echo '    transaction pointing at an id that no longer exists - trading one'
\echo '    broken reference for another, on documents that currently work.'
\echo '    Counted by scanning transaction data blobs for each stray id.'
\echo ''

WITH stranded AS (
    SELECT d.doctype, d.id AS stray_id, d.data->>'code' AS wanted_id
    FROM :schema.documents d
    JOIN :schema.doctype_meta m ON m.name = d.doctype
    WHERE m.document_type = 'Master'
      AND d.deleted_at IS NULL
      AND COALESCE(d.data->>'code', '') <> ''
      AND d.id <> d.data->>'code'
)
SELECT
    s.doctype                         AS master_doctype,
    COUNT(DISTINCT s.stray_id)        AS stray_ids_referenced,
    COUNT(*)                          AS referencing_documents
FROM stranded s
JOIN :schema.documents t
      ON t.deleted_at IS NULL
     AND t.data::text LIKE '%' || s.stray_id || '%'
     AND t.id <> s.stray_id
GROUP BY s.doctype
ORDER BY referencing_documents DESC, s.doctype;

\echo ''
\echo '=== 5. A sample of the stranded rows, to eyeball before deciding ======='
\echo ''

SELECT
    d.doctype,
    d.id                    AS stray_id,
    d.data->>'code'         AS wanted_id,
    d.data->>'name'         AS name,
    d.status,
    d.created_at::date      AS created
FROM :schema.documents d
JOIN :schema.doctype_meta m ON m.name = d.doctype
WHERE m.document_type = 'Master'
  AND d.deleted_at IS NULL
  AND COALESCE(d.data->>'code', '') <> ''
  AND d.id <> d.data->>'code'
ORDER BY d.doctype, d.created_at
LIMIT 50;

\echo ''
\echo '=== 6. Every tenant schema at once (scale only) ========================'
\echo ''
\echo '    Section 1 audits one schema. This gives a per-tenant headline so'
\echo '    you can see whether minn is the only affected tenant.'
\echo ''

-- Built with \gexec rather than a DO block on purpose: a DO block can only
-- report through RAISE NOTICE, which lands on stderr and so interleaves
-- unpredictably with the result sets above when the output is redirected to
-- a file. \gexec emits one ordinary result set per tenant, and keeps this
-- section as read-only as the rest of the script.

SELECT format(
    'SELECT %L AS tenant,'
    ' COUNT(*) FILTER (WHERE d.id <> d.data->>''code'''
    '                    AND COALESCE(d.data->>''code'','''') <> '''') AS stranded,'
    ' COUNT(*) AS master_rows_total'
    ' FROM %I.documents d'
    ' JOIN %I.doctype_meta m ON m.name = d.doctype'
    ' WHERE m.document_type = ''Master'' AND d.deleted_at IS NULL',
    nspname, nspname, nspname)
FROM pg_namespace
WHERE nspname LIKE 'tenant\_%'
ORDER BY nspname
\gexec

\echo ''
\echo '=== Done. Nothing was modified. ========================================'
\echo ''
\echo '    Next step is a decision, not a script: if section 3 is empty and'
\echo '    section 4 is small, a backfill is a contained UPDATE. If section 4'
\echo '    is large, the backfill has to rewrite those references in the same'
\echo '    transaction, which is a different and bigger piece of work.'
\echo '    Either way it goes out with the batched deploy, not on its own.'
\echo ''
