-- ---------------------------------------------------------------------------
-- Stage 47.5.1 - enforced single-owner-per-warehouse (audit finding A-05).
--
-- THE DECISION THIS IMPLEMENTS (user, 2026-09-09). A-05 offered two closures:
-- build real mixed-owner isolation across the whole inventory lifecycle, or
-- "make the unsupported configuration impossible". The second was chosen, on
-- evidence: `bin_stock_owner` held exactly two rows in the entire development
-- database and both were the A-05 red-team fixture itself (OWNER-A-TEST and
-- OWNER-B-TEST in WH-OWN-TEST, one of them at qty 0). No tenant has ever
-- segregated real stock by owner, so the honest closure is to stop claiming
-- an isolation the code does not enforce, rather than to build the isolation
-- for nobody.
--
-- What was actually unenforced, from engines/wms_owner_stock.go's own header:
-- "allocation/picking does not filter by owner ... there is no way for any
-- caller to ask allocation to respect an owner boundary." `bin_stock_owner`
-- is a BILLING breakdown; `AllocateFromStock` reads `bin_stock` and never
-- looks at it. So one client's demand could be satisfied out of another
-- client's segregated goods, silently. Restricting a warehouse to a single
-- owner removes the boundary that was going unenforced - there is no longer a
-- second owner in the building to take stock from.
--
-- WHY A TABLE RATHER THAN A CHECK CONSTRAINT. "At most one distinct owner_id
-- per location_code" is not expressible as a row-local CHECK, and a trigger
-- would put per-row procedural code in the write path. A warehouse_owner
-- table with location_code as the PRIMARY KEY makes the rule structural: a
-- location can hold exactly one owner row, and every owner-stock write is
-- verified against it. The database enforces the cardinality; the engine
-- enforces the reference.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS tenant_default.warehouse_owner (
    -- One row per location. The PRIMARY KEY *is* the guard: a second owner
    -- for the same warehouse cannot be inserted, by construction.
    location_code VARCHAR(100) PRIMARY KEY,
    owner_id      VARCHAR(100) NOT NULL,
    assigned_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    assigned_by   VARCHAR(100),
    -- Free-text note for the case a 3PL relationship ends and the warehouse is
    -- reassigned, so the change is explained rather than merely observed.
    note TEXT
);

CREATE INDEX IF NOT EXISTS idx_warehouse_owner_owner
  ON tenant_default.warehouse_owner (owner_id);

-- ---------------------------------------------------------------------------
-- Backfill, and the deliberate refusal to break a database that is already
-- mixed.
--
-- This migration must be safe to run against production, which this session
-- could not inspect. So it does NOT assume the "no real owner data" finding
-- holds there. It seeds warehouse_owner from whatever each location already
-- holds - but ONLY for locations that hold exactly one owner. A location that
-- is genuinely mixed is left unseeded and reported, so an operator resolves it
-- deliberately instead of the migration silently picking a winner and
-- misattributing somebody's stock.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.warehouse_owner (location_code, owner_id, assigned_by, note)
SELECT location_code, MIN(owner_id), 'stage47.5.1-backfill',
       'Backfilled from existing bin_stock_owner rows; this location already held exactly one owner.'
  FROM tenant_default.bin_stock_owner
 WHERE COALESCE(location_code, '') <> ''
 GROUP BY location_code
HAVING COUNT(DISTINCT owner_id) = 1
ON CONFLICT (location_code) DO NOTHING;

-- Report - never fail - on any location that is already mixed. A migration
-- that aborts here would block every later migration on a database whose data
-- predates the rule; a migration that silently "fixed" it would rewrite
-- somebody's stock ownership without being asked. Naming it is the only
-- honest option, and engines.ListMixedOwnerLocations surfaces the same list
-- through the API for whoever has to resolve it.
DO $$
DECLARE
    mixed_count INT;
    mixed_list  TEXT;
BEGIN
    SELECT COUNT(*), STRING_AGG(location_code, ', ')
      INTO mixed_count, mixed_list
      FROM (
        SELECT location_code
          FROM tenant_default.bin_stock_owner
         WHERE COALESCE(location_code, '') <> ''
         GROUP BY location_code
        HAVING COUNT(DISTINCT owner_id) > 1
      ) m;

    IF mixed_count > 0 THEN
        RAISE WARNING 'Stage 47.5.1: % location(s) already hold stock for more than one owner and were NOT assigned a single owner: %. These predate the single-owner rule. Resolve them (move or re-attribute the stock) and then assign the warehouse explicitly; until then owner isolation is NOT enforced for them and they are listed by GET /api/v1/wms/owner/mixed-locations.', mixed_count, mixed_list;
    END IF;
END $$;
