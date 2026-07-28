#!/usr/bin/env bash
# Apply db/*.sql migrations in filename order, skipping any already recorded in
# public.schema_migrations. This is the Linux port of promote.ps1's
# Invoke-PendingMigrations -- same ledger table, same bootstrap-order rule
# (run everything in filename order; the ledger table itself is created by an
# early migration, so pre-ledger files just run and get back-recorded).
#
# Usage (sources the env file for DATABASE_URL):
#   source /etc/erp/erp.env && bash /opt/erp/deploy/migrate.sh
set -euo pipefail

: "${DATABASE_URL:?set DATABASE_URL (e.g. 'source /etc/erp/erp.env') before running}"

DB_DIR="$(cd "$(dirname "$0")/../db" && pwd)"

find "$DB_DIR" -maxdepth 1 -name '*.sql' | sort | while read -r f; do
	name="$(basename "$f")"
	already="$(psql "$DATABASE_URL" -tAc \
		"SELECT 1 FROM public.schema_migrations WHERE migration_file = '$name'" 2>/dev/null || true)"
	if [ "$already" = "1" ]; then
		echo "  [skip]  $name"
		continue
	fi
	echo "  [apply] $name"
	psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$f"
	# migrations_stage14b_versioning.sql inserts its own ledger row via its
	# bootstrap INSERT; this ON CONFLICT DO NOTHING back-records any file
	# applied outside that bootstrap list without double-recording.
	psql "$DATABASE_URL" -c \
		"INSERT INTO public.schema_migrations (migration_file, description) VALUES ('$name', 'applied by migrate.sh') ON CONFLICT (migration_file) DO NOTHING" \
		>/dev/null 2>&1 || true
done

echo "migrations up to date."
