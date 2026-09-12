#!/bin/sh
set -eu

: "${DATABASE_URL:?DATABASE_URL must be the database-owner connection}"
: "${PLATFORM_API_DATABASE_URL:?PLATFORM_API_DATABASE_URL is required}"
: "${PLATFORM_WORKER_DATABASE_URL:?PLATFORM_WORKER_DATABASE_URL is required}"
: "${PLATFORM_API_DB_PASSWORD:?PLATFORM_API_DB_PASSWORD is required}"
: "${PLATFORM_WORKER_DB_PASSWORD:?PLATFORM_WORKER_DB_PASSWORD is required}"

cleanup() {
  psql "$DATABASE_URL" --quiet -v ON_ERROR_STOP=1 <<'SQL'
DROP ROLE IF EXISTS platform_worker_login;
DROP ROLE IF EXISTS platform_api_login;
SQL
}
trap cleanup EXIT

ADMIN_DATABASE_URL="$DATABASE_URL" \
  sh infra/database/provision_runtime_logins.sh

expect_allowed() {
  connection="$1"
  statement="$2"
  psql "$connection" --quiet -v ON_ERROR_STOP=1 -Atc "$statement" >/dev/null
}

expect_denied() {
  connection="$1"
  statement="$2"
  if psql "$connection" --quiet -v ON_ERROR_STOP=1 -Atc "$statement" >/dev/null 2>&1; then
    echo "statement unexpectedly succeeded: $statement" >&2
    exit 1
  fi
}

expect_allowed "$PLATFORM_API_DATABASE_URL" "SELECT count(*) FROM platform.public_published_works"
expect_allowed "$PLATFORM_API_DATABASE_URL" "SELECT count(*) FROM platform.users"
expect_denied "$PLATFORM_API_DATABASE_URL" "SELECT count(*) FROM pg_authid"
expect_denied "$PLATFORM_API_DATABASE_URL" "CREATE TABLE platform.runtime_login_escape(id integer)"

expect_allowed "$PLATFORM_WORKER_DATABASE_URL" "SELECT count(*) FROM platform.outbox_events"
expect_allowed "$PLATFORM_WORKER_DATABASE_URL" "SELECT count(*) FROM audit.media_reconciliation_runs"
expect_denied "$PLATFORM_WORKER_DATABASE_URL" "SELECT count(*) FROM platform.users"
expect_denied "$PLATFORM_WORKER_DATABASE_URL" "SELECT count(*) FROM collector.source_records"
expect_denied "$PLATFORM_WORKER_DATABASE_URL" "CREATE TABLE platform.runtime_worker_escape(id integer)"

roles="$(psql "$DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -Atc \
  "SELECT count(*) FROM pg_roles WHERE rolname IN ('platform_api_login','platform_worker_login') AND rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication")"
test "$roles" = "2"

api_statement_timeout="$(psql "$PLATFORM_API_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -Atc 'SHOW statement_timeout')"
api_lock_timeout="$(psql "$PLATFORM_API_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -Atc 'SHOW lock_timeout')"
worker_statement_timeout="$(psql "$PLATFORM_WORKER_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -Atc 'SHOW statement_timeout')"
worker_lock_timeout="$(psql "$PLATFORM_WORKER_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -Atc 'SHOW lock_timeout')"
test "$api_statement_timeout" = "15s"
test "$api_lock_timeout" = "5s"
test "$worker_statement_timeout" = "30s"
test "$worker_lock_timeout" = "5s"

printf '%s\n' 'runtime database login contracts passed'
