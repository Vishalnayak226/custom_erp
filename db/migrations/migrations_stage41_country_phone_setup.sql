-- Stage 41: country-aware phone data, and the optional Location short code.
--
-- Additive and idempotent throughout (the ON CONFLICT DO NOTHING convention
-- every doctype_fields migration in this repo uses), so it is safe to re-run
-- and changes nothing about existing records.

-- ---------------------------------------------------------------------------
-- 1. Customer.phone_country - which country a customer's phone number belongs
--    to, written by engines/master_data_validation.go at save time.
--
-- Optional and never typed by hand: the phone engine resolves it from the
-- number itself (an explicit +<code>) or from the tenant's configured home
-- country, and stamps it on the record. It exists so "customers outside our
-- home market" is a field a campaign/report can filter on, instead of
-- something that would have to be recomputed by re-parsing every stored
-- number. mandatory = FALSE so every existing Customer stays valid.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Customer', 'phone_country', 'Phone Country', 'Data', FALSE, NULL, 60)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 2. SalesOrder customer contact + origin country.
--
-- The Order Engine had customer_name and shipping_address but nowhere to put
-- a phone number, so a channel order's contact detail was simply dropped on
-- import. Both new fields are written by engines/orders.go after cleaning:
--
--   customer_phone   - digits only, dial code and trunk prefix resolved.
--   customer_country - ISO2 of the country that number belongs to.
--
-- customer_country is the "order came from a different country" signal the
-- business targets on. Note the deliberate asymmetry with the Customer master:
-- an order is NEVER rejected for a bad phone number - it is cleaned, tagged
-- and saved - because refusing a real, paid order over a contact-field format
-- would be the wrong trade every single time.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SalesOrder', 'customer_phone', 'Customer Phone', 'Data', FALSE, NULL, 11),
('SalesOrder', 'customer_country', 'Customer Country', 'Data', FALSE, NULL, 12)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 3. Location.short_code - an optional shorthand for a location.
--
-- Explicitly NOT mandatory, and explicitly not a second identity: `code`
-- remains the key every transaction, reservation and stock row references,
-- and nothing keys off short_code. It exists so a store can be referred to by
-- the two or three letters staff actually say out loud ("BKC", "LDH2") while
-- `name` stays the full human name shown on screen and `code` stays the
-- system identifier.
--
-- Indexed for search because the Location typeahead matches against it (103
-- Location records today, and the picker is the most-reused control in the
-- app - 15 screens select a location).
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Location', 'short_code', 'Short Code', 'Data', FALSE, NULL, 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_documents_location_short_code
  ON tenant_default.documents ((data->>'short_code'))
  WHERE doctype = 'Location';
