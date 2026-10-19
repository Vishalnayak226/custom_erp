-- ---------------------------------------------------------------------------
-- Stage 114.6 (user, 2026-10-19: "I need a mrp logic as marigin cal. multiple
-- margin cal you bring. I will choose or build new in that. One with wright
-- wise pp wise mrp. only pp range wise mrp with a multillier single
-- multiplier etc."; decision the same day: a suggestion at the goods
-- receipt's MRP step, set per Product type with one default).
--
-- MRPRule - one way to suggest an MRP from the purchase price incl. GST:
--   Multiplier               price x multiplier
--   Purchase price bands     price x the multiplier of its band (bands:
--                            "500=3, 2000=2.5, above=2")
--   Weight + purchase price  weight (g) x rate per gram + price x multiplier
-- ProductType.mrp_rule picks one; else the Active rule ticked Default; else
-- the markup the MRP step always used. engines/mrp_rules.go reads them.
--
-- Additive and idempotent, replayed into every tenant schema. No existing
-- row changes, so every suggestion stays as it is until a rule is made.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL
               OR to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL
               OR to_regclass(format('%I.role_permissions', schema_rec.schema_name)) IS NULL;

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('MRPRule', 'Master Data', 'master_data', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('MRPRule', 'code', 'Code', 'Data', TRUE, NULL, 1),
        ('MRPRule', 'name', 'Rule name', 'Data', TRUE, NULL, 2),
        ('MRPRule', 'method', 'Method', 'Select', TRUE, 'Multiplier,Purchase price bands,Weight + purchase price', 3),
        ('MRPRule', 'multiplier', 'Multiplier (x purchase price incl. GST)', 'Number', FALSE, NULL, 4),
        ('MRPRule', 'bands', 'Price bands (up to amount = multiplier, e.g. 500=3, 2000=2.5, above=2)', 'Data', FALSE, NULL, 5),
        ('MRPRule', 'rate_per_gram', 'Rate per gram', 'Number', FALSE, NULL, 6),
        ('MRPRule', 'is_default', 'Default for product types with no rule', 'Check', FALSE, NULL, 7),
        ('MRPRule', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 8),
        ('ProductType', 'mrp_rule', 'MRP rule', 'Link', FALSE, 'MRPRule', 12)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- Whoever manages product types manages their MRP rules.
    EXECUTE format($sql$
      INSERT INTO %1$I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT p.role, 'MRPRule', p.allow_read, p.allow_create, p.allow_update, p.allow_delete
        FROM %1$I.role_permissions p
       WHERE p.doctype_name = 'ProductType'
         AND NOT EXISTS (SELECT 1 FROM %1$I.role_permissions q
                          WHERE q.role = p.role AND q.doctype_name = 'MRPRule')
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
