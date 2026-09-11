-- Stage 49.7.1/49.7.4: least-privilege PostgreSQL roles for this deployment.
--
-- engines/tenant_lifecycle.go's TenantDatabasePrivilegeReport (`tenantctl
-- db-privilege`) already REPORTS if the application connects as a superuser
-- or holds CREATEROLE/CREATEDB - its own doc comment says fixing that is
-- deployment state and belongs here. This is that fix.
--
-- deploy/README.md Part A2 (as shipped before this file existed) has the app
-- own the database it runs in: `CREATE ROLE erp LOGIN ...; CREATE DATABASE
-- custom_erp OWNER erp;`. That single role then does everything - runs the
-- server, runs migrations, and (implicitly, since nothing stopped it) could
-- run backups too. Risk register R-07: "Runtime, migration and backup
-- identities are the same account" - compromise of the running server
-- process is then the difference between a data breach and schema-modify +
-- full-dump ability. This script splits that one role into three:
--
--   erp_app      - what /etc/erp/erp.env's DATABASE_URL should name from now
--                  on. LOGIN, no CREATEROLE/CREATEDB/SUPERUSER, DML only
--                  (SELECT/INSERT/UPDATE/DELETE) on every schema, including
--                  ones provisioned after this script ran (ALTER DEFAULT
--                  PRIVILEGES below). Cannot CREATE TABLE, cannot CREATE
--                  SCHEMA, cannot DROP anything.
--   erp_migrate  - owns the database and every schema/table in it (via
--                  REASSIGN OWNED BY below). Used by deploy/migrate.sh and by
--                  `tenantctl` (tenant provisioning/deprovisioning/purge is a
--                  DDL operation - CREATE/DROP SCHEMA - so it needs this
--                  role, not erp_app; see docs/security/README.md's "operator
--                  commands, not routes" note on why tenantctl already runs
--                  as a separate invocation from the server).
--   erp_backup   - SELECT only, everywhere, forever (default privileges
--                  again). Enough for pg_dump (deploy/backup.sh); cannot
--                  write, cannot see anything pg_dump does not also need.
--
-- Idempotent: every statement below is safe to run again (DO blocks check
-- pg_roles first; grants are additive; ALTER DATABASE/SCHEMA OWNER is a
-- no-op once already owned). It is NOT safe to run unattended against a
-- database with connections from other roles mid-transaction - REASSIGN
-- OWNED BY takes the same kind of lock ALTER TABLE OWNER does. Run it in a
-- short maintenance window, not from a CI job or an unattended cron.
--
-- What this script deliberately does NOT do:
--   - Does not touch pg_hba.conf/postgresql.conf (out of a database-level
--     script's reach; see deploy/README.md's new "PostgreSQL baseline" note
--     for the file-level settings 49.7.4 also asks for).
--   - Does not fix search_path or add a DB-enforced tenant boundary beyond
--     what schema-per-tenant already gives - that is Stage 49.3.3's
--     "Database isolation defense", a related but broader item this script's
--     role split is a prerequisite for, not a substitute for.
--   - Does not create or manage OS-level accounts. `erp_app`/`erp_migrate`/
--     `erp_backup` are PostgreSQL roles, independent of the single Unix
--     `erp` user the systemd unit already runs as (deploy/erp.service) -
--     that separation is a much larger change (49.7.1's "controlled
--     migration and backup identities separate from runtime" is read here as
--     the database identity, which is the credential that actually gates
--     what a compromised process can do to data; a second OS account buys
--     comparatively little extra on a single-box deployment where the
--     database is reached over a network credential either way).
--
-- Usage (run once, by whichever role currently owns the database - the
-- existing `erp` role, or a cluster superuser during initial provisioning):
--
--   psql "$DATABASE_URL" -v current_owner=erp -v app_password='CHANGE_ME' \
--        -v migrate_password='CHANGE_ME' -v backup_password='CHANGE_ME' \
--        -f deploy/postgres_harden.sql
--
-- Then, and only after confirming the new roles work (see the verification
-- block at the end of this file):
--   1. Point /etc/erp/erp.env's DATABASE_URL at erp_app.
--   2. Point deploy/backup.sh's DATABASE_URL (wherever the backup cron's
--      environment sources it from) at erp_backup.
--   3. Run `tenantctl`/`deploy/migrate.sh` with DATABASE_URL pointed at
--      erp_migrate instead of the old shared `erp` credential.
--   4. Restart the erp service so it reconnects as erp_app, then confirm
--      `tenantctl db-privilege` reports superuser=no, createrole=no,
--      createdb=no and zero findings.

\set ON_ERROR_STOP on

-- current_owner: the role that owns the database/schemas TODAY, whose
-- objects get reassigned to erp_migrate. Defaults to 'erp' (the role
-- deploy/README.md's Part A2 has always created) if not passed with -v.
\if :{?current_owner}
\else
	\set current_owner erp
\endif

-- The three passwords are REQUIRED (no default) - an empty or placeholder
-- password on a role that can log in is exactly SB-004's "looks like a
-- placeholder" finding, and this script does not gate startup the way the
-- 49.1.3 baseline does, so it fails loudly here instead.
\if :{?app_password}
\else
	\warn 'Missing -v app_password=... - refusing to create erp_app with no password.'
	\quit
\endif
\if :{?migrate_password}
\else
	\warn 'Missing -v migrate_password=... - refusing to create erp_migrate with no password.'
	\quit
\endif
\if :{?backup_password}
\else
	\warn 'Missing -v backup_password=... - refusing to create erp_backup with no password.'
	\quit
\endif

-- --- 1. Create the three roles (idempotent) ---------------------------------
--
-- Deliberately plain top-level CREATE ROLE statements, each guarded by its
-- own \if, rather than one DO $$ ... $$ block: psql does NOT perform :'var'
-- substitution inside a dollar-quoted string, so a password variable used
-- that way would reach the server as the literal, unsubstituted text
-- ":'app_password'" - a real, tested bug in an earlier draft of this file,
-- not a hypothetical one.
SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'erp_app') AS need_erp_app \gset
\if :need_erp_app
CREATE ROLE erp_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
	CONNECTION LIMIT 80 PASSWORD :'app_password';
\else
\echo 'erp_app already exists - leaving its password as-is (ALTER ROLE erp_app PASSWORD ... to rotate it deliberately).'
\endif

SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'erp_migrate') AS need_erp_migrate \gset
\if :need_erp_migrate
CREATE ROLE erp_migrate LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
	CONNECTION LIMIT 5 PASSWORD :'migrate_password';
\else
\echo 'erp_migrate already exists - leaving its password as-is (ALTER ROLE erp_migrate PASSWORD ... to rotate it deliberately).'
\endif

SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'erp_backup') AS need_erp_backup \gset
\if :need_erp_backup
CREATE ROLE erp_backup LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
	CONNECTION LIMIT 3 PASSWORD :'backup_password';
\else
\echo 'erp_backup already exists - leaving its password as-is (ALTER ROLE erp_backup PASSWORD ... to rotate it deliberately).'
\endif

-- 49.7.4 "safe timeouts/connection caps" - erp_app is the role every ordinary
-- HTTP request runs as, so a runaway or malicious query cannot hold a
-- connection (and its locks) forever. 120s is a generous upper bound, not a
-- performance target - the app's own HTTP WriteTimeout is 60s (routes.go),
-- so a query still running at 120s has already failed the request; this only
-- bounds how long the abandoned server-side work continues after that.
-- erp_migrate and erp_backup are deliberately NOT given a statement_timeout:
-- a large ALTER TABLE or a full pg_dump legitimately runs longer, and both
-- roles are used interactively/by a single script, never by a request pool.
ALTER ROLE erp_app SET statement_timeout = '120s';
ALTER ROLE erp_app SET idle_in_transaction_session_timeout = '60s';
ALTER ROLE erp_migrate SET idle_in_transaction_session_timeout = '600s';
ALTER ROLE erp_backup SET idle_in_transaction_session_timeout = '600s';

-- --- 2. Reassign ownership from the current single role to erp_migrate -----
--
-- Plain top-level statements again, for the same reason as step 1: psql does
-- not substitute :'var'/: "var" references inside a dollar-quoted DO $$
-- block body, so the conditional has to be decided client-side (via \gset)
-- and the DDL itself issued as an ordinary statement, not wrapped in PL/pgSQL.
SELECT (:'current_owner' <> 'erp_migrate') AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'current_owner') AS need_reassign \gset
\if :need_reassign
REASSIGN OWNED BY :"current_owner" TO erp_migrate;
\else
\echo 'Skipping REASSIGN OWNED BY: current_owner is already erp_migrate, or that role does not exist on this database.'
\endif

-- The database itself (not just the objects inside it) has an owner too -
-- REASSIGN OWNED BY does not change that.
SELECT current_database() AS dbname \gset
SELECT pg_get_userbyid(datdba) IS DISTINCT FROM 'erp_migrate' AS need_db_owner_change FROM pg_database WHERE datname = :'dbname' \gset
\if :need_db_owner_change
ALTER DATABASE :"dbname" OWNER TO erp_migrate;
\endif

-- --- 3. Grant erp_app least-privilege DML on every schema, present and future

DO $$
DECLARE
	s record;
BEGIN
	FOR s IN
		SELECT nspname FROM pg_namespace
		WHERE nspname = 'public' OR nspname LIKE 'tenant\_%' ESCAPE '\'
	LOOP
		EXECUTE format('GRANT USAGE ON SCHEMA %I TO erp_app', s.nspname);
		EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO erp_app', s.nspname);
		EXECUTE format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %I TO erp_app', s.nspname);
		EXECUTE format('GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA %I TO erp_app', s.nspname);

		EXECUTE format('GRANT USAGE ON SCHEMA %I TO erp_backup', s.nspname);
		EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA %I TO erp_backup', s.nspname);
		EXECUTE format('GRANT SELECT ON ALL SEQUENCES IN SCHEMA %I TO erp_backup', s.nspname);

		-- Covers every table/sequence/function CREATEd from now on by
		-- erp_migrate in this schema - including a brand-new tenant_<x>
		-- schema a future `tenantctl provision` creates - without this
		-- script needing to run again per tenant. Scoped "FOR ROLE
		-- erp_migrate" because erp_migrate is the only role that will ever
		-- CREATE anything after this script runs.
		EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE erp_migrate IN SCHEMA %I GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO erp_app', s.nspname);
		EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE erp_migrate IN SCHEMA %I GRANT USAGE, SELECT ON SEQUENCES TO erp_app', s.nspname);
		EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE erp_migrate IN SCHEMA %I GRANT EXECUTE ON FUNCTIONS TO erp_app', s.nspname);
		EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE erp_migrate IN SCHEMA %I GRANT SELECT ON TABLES TO erp_backup', s.nspname);
		EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE erp_migrate IN SCHEMA %I GRANT SELECT ON SEQUENCES TO erp_backup', s.nspname);
	END LOOP;
END
$$;

-- :dbname was \gset above (step 2) from current_database() - this script
-- always runs "psql $DATABASE_URL -f ..." against the one database being
-- hardened, so there is nothing a second passed-in variable would let an
-- operator get right that the connection string itself does not already say.
GRANT CONNECT ON DATABASE :"dbname" TO erp_app, erp_backup;
-- erp_migrate needs schema-creation rights at the DATABASE level (provisioning
-- a tenant is CREATE SCHEMA, a database-level, not per-schema, privilege) -
-- it already has this implicitly as the database owner from step 2 above;
-- this GRANT only matters if that reassignment was skipped (current_owner
-- already was erp_migrate on a re-run).
GRANT CREATE, CONNECT ON DATABASE :"dbname" TO erp_migrate;

-- --- 4. Standard cluster hardening ------------------------------------------

-- A low-privilege role could otherwise CREATE objects directly in `public`
-- with no schema-level grant at all - PostgreSQL grants CREATE on `public`
-- to PUBLIC (every role) by default. erp_app/erp_backup were never granted
-- CREATE on anything above, so this closes the one path that default left
-- open for any OTHER role that might ever connect to this database.
REVOKE CREATE ON SCHEMA public FROM PUBLIC;

-- --- 5. Verify -------------------------------------------------------------

\echo '--- role posture (expect erp_app/erp_backup: no/no/no) ---'
SELECT rolname, rolsuper AS superuser, rolcreaterole AS createrole, rolcreatedb AS createdb, rolconnlimit AS conn_limit
FROM pg_roles WHERE rolname IN ('erp_app', 'erp_migrate', 'erp_backup') ORDER BY rolname;

\echo '--- schema ownership (expect erp_migrate everywhere) ---'
SELECT nspname, pg_get_userbyid(nspowner) AS owner FROM pg_namespace
WHERE nspname = 'public' OR nspname LIKE 'tenant\_%' ESCAPE '\' ORDER BY nspname;

\echo 'Review both tables above, then rotate DATABASE_URL for erp-server/tenantctl/backup.sh to the matching role (see the file header) and restart each.'
\echo 'Dangerous extensions/functions (49.7.4): confirm only expected extensions are installed - dblink, postgres_fdw, adminpack, plpythonu/plperlu/plperl and file_fdw have no use in this codebase and must never appear here.'
SELECT extname FROM pg_extension ORDER BY extname;
