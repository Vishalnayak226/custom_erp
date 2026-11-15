-- 146.4.1: add Unicommerce to the Channel platform list (additive, safe to run twice).
UPDATE tenant_default.doctype_fields
SET options = options || ',Unicommerce'
WHERE doctype_name = 'Channel' AND fieldname = 'platform'
  AND options NOT LIKE '%Unicommerce%';
