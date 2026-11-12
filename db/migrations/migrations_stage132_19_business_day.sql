-- ---------------------------------------------------------------------------
-- Stage 132.19: a day is the business's day, whatever the database's clock.
--
-- Documents, GL lines and stock ledger rows keep naive TIMESTAMPs written by
-- CURRENT_TIMESTAMP in the session's TimeZone: Asia/Kolkata on a developer's
-- machine, UTC on the production server. Reports filtered a day with
-- `created_at >= $1::date AND created_at < ($2::date + 1)`, so on production
-- a "day" ran 05:30 to 05:30 IST and what happened after midnight landed on
-- the previous day - and month, for GST (Guide, on app, 2026-10-26). The user
-- chose to keep every stored value and count days in the business's zone
-- (decision 2026-10-26, option 2). These read the session's own clock, so they
-- are right on both.
--
--   erp_business_tz()        the business's IANA zone: the database setting
--                            erp.business_tz (ALTER DATABASE ... SET), else
--                            Asia/Kolkata - every client today is in India
--   erp_day_start(d)         the session-clock moment business day d begins;
--                            compare a naive timestamp with it:
--                              created_at >= erp_day_start($1::date)
--                              AND created_at < erp_day_start($2::date + 1)
--   erp_today()              the business's date now (in place of CURRENT_DATE)
--   erp_local_date(ts)       the business date of a naive session-clock timestamp
--
-- Additive: functions only, nothing stored changes. STABLE, so a filter
-- against erp_day_start(...) of a parameter is worked out once per query and
-- an index on the column is still used.
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION public.erp_business_tz() RETURNS text
LANGUAGE sql STABLE AS $$
  SELECT COALESCE(NULLIF(current_setting('erp.business_tz', true), ''), 'Asia/Kolkata')
$$;

CREATE OR REPLACE FUNCTION public.erp_day_start(d date) RETURNS timestamp
LANGUAGE sql STABLE AS $$
  SELECT (d::timestamp AT TIME ZONE public.erp_business_tz()) AT TIME ZONE current_setting('TimeZone')
$$;

CREATE OR REPLACE FUNCTION public.erp_today() RETURNS date
LANGUAGE sql STABLE AS $$
  SELECT (CURRENT_TIMESTAMP AT TIME ZONE public.erp_business_tz())::date
$$;

CREATE OR REPLACE FUNCTION public.erp_local_date(ts timestamp) RETURNS date
LANGUAGE sql STABLE AS $$
  SELECT ((ts AT TIME ZONE current_setting('TimeZone')) AT TIME ZONE public.erp_business_tz())::date
$$;
