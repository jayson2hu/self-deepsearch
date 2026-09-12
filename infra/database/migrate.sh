#!/bin/sh
set -eu

: "${ADMIN_DATABASE_URL:?ADMIN_DATABASE_URL is required}"
MIGRATION_DIR="${MIGRATION_DIR:-/migrations}"

if [ ! -d "$MIGRATION_DIR" ]; then
  echo "migration directory does not exist: $MIGRATION_DIR" >&2
  exit 2
fi

metadata="$(psql "$ADMIN_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -Atc \
  "SELECT to_regclass('platform.system_metadata') IS NOT NULL")"
if [ "$metadata" = "t" ]; then
  current_version="$(psql "$ADMIN_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -Atc \
    "SELECT schema_version FROM platform.system_metadata WHERE singleton")"
else
  current_version=0
fi
case "$current_version" in *[!0-9]*|'') echo "current schema version is invalid" >&2; exit 1;; esac

temporary="$(mktemp "${TMPDIR:-/tmp}/self-deepsearch-migrate.XXXXXX.sql")"
trap 'rm -f "$temporary"' EXIT HUP INT TERM
chmod 600 "$temporary"

printf '%s\n' "SELECT pg_advisory_lock(hashtextextended('self-deepsearch-schema-migrations', 0));" >"$temporary"
if [ "$current_version" -eq 0 ]; then
  cat >>"$temporary" <<'SQL'
DO $$
BEGIN
  IF to_regclass('platform.system_metadata') IS NOT NULL THEN
    RAISE EXCEPTION 'schema appeared while waiting for the migration lock';
  END IF;
END
$$;
SQL
else
  printf '%s\n' \
    'DO $$' \
    'DECLARE actual integer;' \
    'BEGIN' \
    '  SELECT schema_version INTO actual FROM platform.system_metadata WHERE singleton;' \
    "  IF actual <> $current_version THEN" \
    "    RAISE EXCEPTION 'schema changed while waiting for the migration lock: expected $current_version, got %', actual;" \
    '  END IF;' \
    'END' \
    '$$;' >>"$temporary"
fi

expected=1
latest=0
pending=0
for migration in "$MIGRATION_DIR"/*.sql; do
  if [ ! -f "$migration" ]; then
    echo "no migration files found in $MIGRATION_DIR" >&2
    exit 2
  fi
  name="$(basename "$migration")"
  prefix="${name%%_*}"
  version="$(printf '%s' "$prefix" | sed 's/^0*//')"
  [ -n "$version" ] || version=0
  case "$version" in *[!0-9]*|'') echo "invalid migration version: $name" >&2; exit 2;; esac
  if [ "$version" -ne "$expected" ]; then
    echo "migration sequence gap: expected $expected, got $version ($name)" >&2
    exit 2
  fi
  latest="$version"
  expected=$((expected + 1))
  if [ "$version" -le "$current_version" ]; then
    continue
  fi
  pending=$((pending + 1))
  printf '%s\n' 'BEGIN;' >>"$temporary"
  awk '
    /^-- \+goose Up/ { in_up = 1; next }
    /^-- \+goose Down/ { in_up = 0; next }
    in_up { print }
  ' "$migration" >>"$temporary"
  printf '%s\n' \
    'DO $$' \
    'DECLARE actual integer;' \
    'BEGIN' \
    '  SELECT schema_version INTO actual FROM platform.system_metadata WHERE singleton;' \
    "  IF actual <> $version THEN" \
    "    RAISE EXCEPTION 'migration $name did not set schema version $version (got %)', actual;" \
    '  END IF;' \
    'END' \
    '$$;' \
    'COMMIT;' >>"$temporary"
done

if [ "$current_version" -gt "$latest" ]; then
  echo "database schema version $current_version is newer than available migration $latest" >&2
  exit 1
fi

printf '%s\n' "SELECT pg_advisory_unlock(hashtextextended('self-deepsearch-schema-migrations', 0));" >>"$temporary"
psql "$ADMIN_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 --file "$temporary"

actual="$(psql "$ADMIN_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -Atc \
  "SELECT schema_version FROM platform.system_metadata WHERE singleton")"
if [ "$actual" != "$latest" ]; then
  echo "schema migration finished at version $actual, expected $latest" >&2
  exit 1
fi

printf 'schema_version=%s\napplied=%s\nstatus=current\n' "$actual" "$pending"
