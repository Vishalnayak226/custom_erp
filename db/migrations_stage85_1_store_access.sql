-- ---------------------------------------------------------------------------
-- Stage 85.A1/85.A2 (user, 2026-10-18): which stores a user may work at.
--
-- "User wise Admin will give the store allocation. First role than user ...
-- In User, There will a section to allocate store. Dynamically it should pull
-- to user if I add a new store."
--
-- Until now a user had exactly one store (users.location_code), and the POS
-- store box let anyone bill at any sellable store. This adds the allocation:
--
--   role_store_access   a role's store access: All (every store, including
--                       ones added later - evaluated live, never copied),
--                       Selected (the listed stores) or Home (the user's
--                       own store only). A role with no row is Home.
--   users.store_access  Inherit (the role decides, plus users.stores as
--                       extra stores), All, Selected (home + users.stores)
--                       or Home. Super Admin always has every store.
--   users.stores        the user's own store list.
--
-- users.location_code stays the home store; nothing about it changes.
--
-- No lockout at deploy: a till that worked yesterday keeps working. Each user
-- who has opened a POS session at a store other than their home store is
-- given that store in users.stores - what the business actually did is the
-- evidence, the same rule 53.1 used for Location.sellable. Users keep
-- "Inherit", so the stores add to whatever their role gives them.
--
-- Loops every tenant schema in the style of 53.18; every statement is
-- idempotent, so re-running changes nothing.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                    WHERE table_schema = schema_rec.schema_name AND table_name = 'users') THEN
      CONTINUE;
    END IF;

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.role_store_access (
        role       VARCHAR(100) PRIMARY KEY,
        access     VARCHAR(20)  NOT NULL DEFAULT 'Home'
                   CONSTRAINT role_store_access_access_check CHECK (access IN ('All', 'Selected', 'Home')),
        stores     TEXT[]       NOT NULL DEFAULT '{}',
        updated_by VARCHAR(100),
        updated_at TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
      )
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      ALTER TABLE %I.users ADD COLUMN IF NOT EXISTS store_access VARCHAR(20) NOT NULL DEFAULT 'Inherit'
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      ALTER TABLE %I.users ADD COLUMN IF NOT EXISTS stores TEXT[] NOT NULL DEFAULT '{}'
    $sql$, schema_rec.schema_name);

    -- The POS-history grant. Only stores that still exist as a Location, and
    -- never the home store itself (it is always allowed).
    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'documents') THEN
      EXECUTE format($sql$
        UPDATE %1$I.users u
           SET stores = (
                 SELECT ARRAY(
                   SELECT DISTINCT s FROM unnest(u.stores || ARRAY(
                     SELECT DISTINCT d.data->>'location'
                       FROM %1$I.documents d
                      WHERE d.doctype = 'POSSession'
                        AND d.data->>'cashier' = u.username
                        AND COALESCE(d.data->>'location', '') NOT IN ('', u.location_code)
                        AND EXISTS (SELECT 1 FROM %1$I.documents l
                                     WHERE l.doctype = 'Location' AND l.id = d.data->>'location'
                                       AND l.deleted_at IS NULL)
                   )) AS s ORDER BY s))
         WHERE EXISTS (
                 SELECT 1 FROM %1$I.documents d
                  WHERE d.doctype = 'POSSession'
                    AND d.data->>'cashier' = u.username
                    AND COALESCE(d.data->>'location', '') NOT IN ('', u.location_code)
                    AND NOT (d.data->>'location' = ANY (u.stores)))
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
