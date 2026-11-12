-- Stage 26.12.3: Fulfillment Pick/Pack - ShortPickLine (engines/fulfillment_pickpack.go)
-- requires a mandatory, category-matched ReasonCode (Stage 26.12.9) the same
-- way the Order Engine's Hold/Cancel actions already do. "Short Pick" is a
-- new category value appended to the existing Select options string -
-- additive, idempotent (safe to re-run), no new column/table.
UPDATE tenant_default.doctype_fields
SET options = 'Cancellation,Hold,Return,Allocation Exception,Short Pick,Other'
WHERE doctype_name = 'ReasonCode' AND fieldname = 'category';
