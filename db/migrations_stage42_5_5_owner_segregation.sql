-- ---------------------------------------------------------------------------
-- Stage 42.5.5 - Multi-owner stock segregation.
--
-- Closes the last open item of Phase 42.5. 42.D2 ("is 3PL/multi-owner a real
-- target?") was resolved 2026-08-24: build it, same call already made for
-- 42.6.6-42.6.9.
--
-- Today `Bin.owner_id` (Stage 26.5's migrations_stage26_5_wms_p2.sql) is the
-- ONLY place ownership lives: a bin is either unowned or wholly one owner's,
-- so a bin holding two clients' stock has no way to say which units are
-- whose. 26.5.15's storage billing and 42.6's task-completion billing both
-- approximate around that absence by attributing a bin's entire qty to its
-- one owner_id.
--
-- Same design decision as 42.1.3 (batch) and 26.5.4 (LPN): owner segregation
-- is a NEW breakdown table, not an owner_id column added to bin_stock.
-- Adding the column would force owner_id into bin_stock's PRIMARY KEY (two
-- owners' stock of one SKU in one bin must not collide), which means
-- DROP CONSTRAINT bin_stock_pkey on a table holding live droplet stock plus
-- rewriting every existing ON CONFLICT (bin_code, sku, condition) in
-- engines/wms.go and wms_putaway_ext.go - a destructive rework of an
-- existing table, which this repo's first principle forbids. bin_stock_owner
-- is a breakdown of bin_stock exactly the way bin_stock_batch already is:
--     SUM(bin_stock_owner.qty) for (bin, sku, condition)
--         <= bin_stock.qty for that same (bin, sku, condition)
-- i.e. the breakdown may be incomplete (most bins will never use it) but can
-- never claim more than the bin holds. engines/wms_owner_stock.go enforces
-- this the same way RecordBatchPutaway enforces the batch invariant.
--
-- Backward compatibility: Bin.owner_id is left exactly as it is and remains
-- meaningful - it is the fallback attribution for any (bin, sku, condition)
-- slice that has no explicit bin_stock_owner row, so a single-owner warehouse
-- (or a 3PL tenant that hasn't segregated a given bin/SKU down to the unit
-- level yet) keeps working unchanged. Only a bin/SKU an operator has
-- explicitly split with RecordOwnerStock stops using the whole-bin
-- approximation. See engines/wms_owner_stock.go's ownerStockQty for the
-- exact combined query billing now runs.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenant_default.bin_stock_owner (
    bin_code VARCHAR(100) NOT NULL,
    sku VARCHAR(100) NOT NULL,
    condition VARCHAR(20) NOT NULL DEFAULT 'Good',
    owner_id VARCHAR(100) NOT NULL,
    location_code VARCHAR(100) NOT NULL,
    qty INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (bin_code, sku, condition, owner_id),
    CONSTRAINT bin_stock_owner_condition_check CHECK (condition IN ('Good', 'Damaged', 'QC-Hold', 'RTV'))
);

-- The billing/inquiry access path: "this owner's stock of this SKU at this
-- location". Mirrors idx_bin_stock_batch_sku_location.
CREATE INDEX IF NOT EXISTS idx_bin_stock_owner_sku_location
    ON tenant_default.bin_stock_owner (sku, location_code, condition);

-- The aggregate billing access path: "everything this owner holds at this
-- location, across SKUs".
CREATE INDEX IF NOT EXISTS idx_bin_stock_owner_owner_location
    ON tenant_default.bin_stock_owner (owner_id, location_code);
