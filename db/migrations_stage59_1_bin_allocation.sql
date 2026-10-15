-- ---------------------------------------------------------------------------
-- Stage 59.1 - bin stock that tracks reality: bin-level allocation and
-- consumption (foundation for mixed-owner 3PL isolation, Stage 59).
--
-- What was wrong. `bin_stock` was written by putaway, cross-dock, VAS and
-- bin-to-bin moves, and NOTHING ever took stock out of it: a pick scan only
-- counted units against the FulfillmentTask, and dispatch only reduced the
-- location-level `inventory_availability`. So every shipped unit stayed on the
-- bin it came from, forever. The pick list (GenerateBinPickList /
-- GenerateWavePickList) was recomputed from those inflated quantities on every
-- call and never remembered, so two tasks could be sent to the same last unit.
--
-- What this adds. One row per (task, sku, bin, lot, owner) slice of stock that
-- a FulfillmentTask has been given - a reservation against `bin_stock` while
-- Open, and the record of exactly which bin/lot/owner each unit came out of
-- once picked. Allocation subtracts other tasks' Open reservations before
-- offering a bin, and pick scan / dispatch decrement `bin_stock` (and the lot
-- and owner breakdowns) from the reserved rows.
--
-- owner_id is carried now, empty until Stage 59.3 makes allocation
-- owner-aware, so that stage is a query change rather than a second migration.
--
-- Additive only. Existing bin_stock rows are NOT corrected here: how far each
-- bin over-states its stock depends on history this migration cannot see, and
-- guessing would move somebody's stock on paper. The bin-vs-location
-- reconciliation report (bin-stock-reconciliation) shows every drifted
-- SKU/location so a count can settle it deliberately.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS tenant_default.bin_allocation (
    id            BIGSERIAL PRIMARY KEY,
    task_id       VARCHAR(100) NOT NULL,
    sku           VARCHAR(100) NOT NULL,
    location_code VARCHAR(100) NOT NULL,
    bin_code      VARCHAR(100) NOT NULL,
    batch_no      VARCHAR(100) NOT NULL DEFAULT '',
    owner_id      VARCHAR(100) NOT NULL DEFAULT '',
    qty           INT NOT NULL CHECK (qty > 0),
    picked_qty    INT NOT NULL DEFAULT 0 CHECK (picked_qty >= 0 AND picked_qty <= qty),
    -- Open: still reserving (qty - picked_qty) units of the bin.
    -- Picked: every reserved unit came out of the bin.
    -- Released: the rest went back (short pick, rejection, dispatch of a
    --           partially picked task); picked_qty still says what left.
    status        VARCHAR(20) NOT NULL DEFAULT 'Open'
                  CHECK (status IN ('Open', 'Picked', 'Released')),
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Allocation asks "how much of this bin is already promised" on every call.
CREATE INDEX IF NOT EXISTS idx_bin_allocation_open_bin
    ON tenant_default.bin_allocation (bin_code, sku)
    WHERE status = 'Open';

CREATE INDEX IF NOT EXISTS idx_bin_allocation_task
    ON tenant_default.bin_allocation (task_id, sku);

CREATE INDEX IF NOT EXISTS idx_bin_allocation_sku_location
    ON tenant_default.bin_allocation (sku, location_code);
