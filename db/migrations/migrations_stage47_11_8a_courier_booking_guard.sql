-- 47.11.8a: nothing is booked twice with a courier.
--
-- engines/courier.go claims a booking and records a stable reference before
-- it calls the courier, keeps the claim when the answer never came, and asks
-- the courier by that reference before booking again. These two indexes are
-- the database's own backstop under that code:
--
--   * one courier AWB belongs to one live booking - an AWB adopted from the
--     courier, or typed in from its portal, cannot also land on another
--     booking;
--   * one parcel (shipping package) holds at most one live courier AWB - a
--     booking that is cancelled at the courier (courier_cancelled_at), RTO or
--     Cancelled no longer counts, so the parcel can be booked again.
--
-- Additive: partial unique indexes only, no column or row is changed. A
-- client schema whose existing rows already break a rule keeps working
-- without that index (a WARNING names it) instead of failing the release;
-- the engine's own check still applies there. tenant_default carries both
-- indexes into every client provisioned later (CREATE TABLE ... LIKE
-- tenant_default.documents INCLUDING ALL).

DO $$
DECLARE
  schema_rec RECORD;
  dup BOOLEAN;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF to_regclass(format('%I.documents', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$SELECT EXISTS (
        SELECT 1 FROM %I.documents
        WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL AND COALESCE(data->>'provider_awb', '') <> ''
        GROUP BY LOWER(COALESCE(data->>'carrier', '')), data->>'provider_awb' HAVING COUNT(*) > 1)$f$,
      schema_rec.schema_name) INTO dup;
    IF dup THEN
      RAISE WARNING '47.11.8a: %.documents has one courier AWB on two live bookings - idx_logistics_booking_provider_awb_unique not created', schema_rec.schema_name;
    ELSE
      EXECUTE format($f$CREATE UNIQUE INDEX IF NOT EXISTS idx_logistics_booking_provider_awb_unique
        ON %I.documents (LOWER(COALESCE(data->>'carrier', '')), (data->>'provider_awb'))
        WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL AND COALESCE(data->>'provider_awb', '') <> ''$f$,
        schema_rec.schema_name);
    END IF;

    EXECUTE format($f$SELECT EXISTS (
        SELECT 1 FROM %I.documents
        WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL AND COALESCE(data->>'provider_awb', '') <> ''
          AND COALESCE(data->>'shipping_package_id', '') <> '' AND status NOT IN ('Cancelled', 'RTO')
          AND NOT (data ? 'courier_cancelled_at')
        GROUP BY data->>'shipping_package_id' HAVING COUNT(*) > 1)$f$,
      schema_rec.schema_name) INTO dup;
    IF dup THEN
      RAISE WARNING '47.11.8a: %.documents has a parcel with two live courier AWBs - idx_logistics_booking_package_live_awb not created', schema_rec.schema_name;
    ELSE
      EXECUTE format($f$CREATE UNIQUE INDEX IF NOT EXISTS idx_logistics_booking_package_live_awb
        ON %I.documents ((data->>'shipping_package_id'))
        WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL AND COALESCE(data->>'provider_awb', '') <> ''
          AND COALESCE(data->>'shipping_package_id', '') <> '' AND status NOT IN ('Cancelled', 'RTO')
          AND NOT (data ? 'courier_cancelled_at')$f$,
        schema_rec.schema_name);
    END IF;
  END LOOP;
END $$;
