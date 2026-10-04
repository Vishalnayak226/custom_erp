-- ---------------------------------------------------------------------------
-- Stage 53.1: Location.sellable - is this a place a POS till may sell from?
--
-- Stage 17.9 gave Location a `type` (Store/Warehouse/HO) but nothing that said
-- whether stock there may be rung up at a counter. Nothing stopped a cashier
-- opening a POS session, or completing a sale, against a warehouse or against
-- HO - and the live screenshot that opened Stage 53 is exactly that: a till
-- sitting on HO.
--
-- `type` is deliberately NOT reused as the answer. The two are different
-- questions: a Warehouse with a trade counter does sell, and a Store being
-- fitted out does not. Type is what the place IS; sellable is what it may DO.
--
-- Additive and idempotent in the Stage 17.9 style (ON CONFLICT DO NOTHING),
-- so re-running changes nothing.
--
-- mandatory = FALSE on purpose. Making it mandatory would reject a save of
-- every Location record that predates this migration the next time anyone
-- edited one - the exact "destructive rework of an existing table" the repo's
-- first principle rules out. Instead the backfill below gives every EXISTING
-- row an explicit value, and engines.LocationIsSellable derives an answer for
-- the blank case (a row created later by someone who skipped the field):
--
--   no Location record at all   -> permitted. Location codes are still free
--                                  text on most doctypes (17.9's own decision),
--                                  so an unregistered code must keep working
--                                  rather than start failing at the till.
--   sellable = 'Yes' / 'No'     -> honoured as written.
--   sellable blank/absent       -> derived from type: Store sells, anything
--                                  else does not.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Location', 'sellable', 'Sellable (may a POS till sell from here?)', 'Select', FALSE, 'Yes,No', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Backfill every existing Location with an explicit value, so the flag is
-- real data a user can see and correct on the Location master rather than an
-- invisible default buried in Go.
--
-- The obvious rule - "Store sells, everything else does not" - was written
-- first and then thrown away, because running it against a real database
-- showed what it actually does. Stage 17.9 seeded a Location row for every
-- code already in use with a HARDCODED type of 'Warehouse' (see that file's
-- own INSERT), so on a tenant whose shops were never re-typed by hand, every
-- single Location is a 'Warehouse' and that rule leaves the tenant with ZERO
-- sellable locations: every till in the chain refuses every sale the moment
-- this migration lands. That is not an additive, backward-compatible change,
-- which is what this repo requires of a schema change - it is an outage.
--
-- So the rule has a second half: a location that has DEMONSTRABLY sold before
-- keeps selling. POSCart and POSSession are the record of that, and they are
-- better evidence than `type` precisely because a human never typed them -
-- they are what the business did, not what someone remembered to classify.
--
--   sells today (a POSCart or POSSession exists there)  -> Yes
--   typed as a Store                                    -> Yes
--   anything else                                       -> No
--
-- The net effect is that this migration changes nothing for any till that was
-- working the day before it ran, and closes the hole for everywhere else -
-- which is the whole point of the flag.
UPDATE tenant_default.documents loc
   SET data = jsonb_set(loc.data, '{sellable}',
         to_jsonb(CASE
           WHEN loc.data->>'type' = 'Store' THEN 'Yes'
           WHEN EXISTS (
             SELECT 1 FROM tenant_default.documents d
              WHERE d.doctype IN ('POSCart', 'POSSession')
                AND d.data->>'location' = loc.id
           ) THEN 'Yes'
           ELSE 'No'
         END))
 WHERE loc.doctype = 'Location'
   AND (loc.data->>'sellable' IS NULL OR loc.data->>'sellable' = '');
