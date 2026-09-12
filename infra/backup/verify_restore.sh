#!/bin/sh
set -eu

: "${BACKUP_FILE:?BACKUP_FILE is required}"
: "${VERIFY_DATABASE_URL:?VERIFY_DATABASE_URL must point to a new empty restore-test database}"
: "${AUDIT_DATABASE_URL:?AUDIT_DATABASE_URL is required}"
: "${BACKUP_RUN_ID:?BACKUP_RUN_ID is required}"

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)"
audit_tool="$script_dir/audit_backup.sh"

if [ ! -f "$BACKUP_FILE" ]; then
  echo "backup file does not exist" >&2
  exit 2
fi

checksum="$BACKUP_FILE.sha256"
[ -f "$checksum" ] || { echo "backup checksum file does not exist" >&2; exit 2; }
(cd "$(dirname "$BACKUP_FILE")" && sha256sum -c "$(basename "$checksum")")
pg_restore --list "$BACKUP_FILE" >/dev/null

# The target must be a dedicated empty database. This script never drops or cleans it.
existing_objects="$(psql "$VERIFY_DATABASE_URL" -v ON_ERROR_STOP=1 -Atc \
  "SELECT count(*) FROM pg_class object JOIN pg_namespace schema ON schema.oid = object.relnamespace WHERE schema.nspname NOT IN ('pg_catalog','information_schema') AND schema.nspname !~ '^pg_toast' AND object.relkind IN ('r','p','v','m','S','f');")"
if [ "$existing_objects" != "0" ]; then
  echo "restore-test database is not empty" >&2
  exit 2
fi

pg_restore --exit-on-error --no-owner --no-acl --dbname="$VERIFY_DATABASE_URL" "$BACKUP_FILE"
schema_version="$(psql "$VERIFY_DATABASE_URL" -v ON_ERROR_STOP=1 -Atc \
  "SELECT schema_version FROM platform.system_metadata WHERE singleton;")"
case "$schema_version" in *[!0-9]*|'') echo "restored schema version is invalid" >&2; exit 1;; esac

schemas="$(psql "$VERIFY_DATABASE_URL" -v ON_ERROR_STOP=1 -Atc \
  "SELECT string_agg(schema_name, ',' ORDER BY schema_name) FROM information_schema.schemata WHERE schema_name IN ('collector','platform','audit');")"
if [ "$schemas" != "audit,collector,platform" ]; then
  echo "restored database is missing required schemas" >&2
  exit 1
fi

restored_sha256="$(sha256sum "$BACKUP_FILE" | cut -d' ' -f1)"
sh "$audit_tool" mark-verified "$BACKUP_RUN_ID" "$restored_sha256" "$schema_version"
printf 'backup_run_id=%s\nschema_version=%s\nschemas=%s\nsha256=%s\nstatus=verified\n' \
  "$BACKUP_RUN_ID" "$schema_version" "$schemas" "$restored_sha256"
