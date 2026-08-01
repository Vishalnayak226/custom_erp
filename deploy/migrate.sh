#!/usr/bin/env bash
# Apply pending db/*.sql migrations.
#
# Stage 30.2.2: this used to be a psql loop - the Linux port of promote.ps1's
# own psql loop - which meant separate implementations of "which migrations are
# pending, in what order, recorded how" in the deploy kit, in promote.ps1, and
# in CI, all of them ordering files lexicographically. They are now one: the
# runner compiled into the application itself (db/migrate.go), invoked here.
# Ordering is numeric-aware there (migrations_stage26_4_* before
# migrations_stage26_10_*), each file runs inside its own transaction, and the
# ledger is still public.schema_migrations - nothing about an existing
# database's recorded state changes.
#
# The migration files are embedded in the binary, so this works even when only
# the binary was shipped: db/ does not have to be present on the server.
#
# Usage (sources the env file for DATABASE_URL):
#   source /etc/erp/erp.env && bash /opt/erp/deploy/migrate.sh
#
# Useful variants, run against the binary directly:
#   erp-server -migrate-status    # list what would run, without running it
#   erp-server -migrate-baseline  # record all as applied WITHOUT running them
#                                 # (only for a database migrated by hand
#                                 #  before this runner existed)
set -euo pipefail

: "${DATABASE_URL:?set DATABASE_URL (e.g. 'source /etc/erp/erp.env') before running}"

# Deployed layout is /opt/erp/erp-server with this script at
# /opt/erp/deploy/migrate.sh; ERP_BINARY overrides it for any other layout.
BINARY="${ERP_BINARY:-$(cd "$(dirname "$0")/.." && pwd)/erp-server}"

if [ ! -x "$BINARY" ]; then
	echo "erp-server binary not found or not executable at: $BINARY" >&2
	echo "Set ERP_BINARY=/path/to/erp-server and re-run." >&2
	exit 1
fi

"$BINARY" -migrate

echo "migrations up to date."
