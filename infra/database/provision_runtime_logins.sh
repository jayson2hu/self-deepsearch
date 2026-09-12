#!/bin/sh
set -eu

: "${ADMIN_DATABASE_URL:?ADMIN_DATABASE_URL is required}"
: "${PLATFORM_API_DB_PASSWORD:?PLATFORM_API_DB_PASSWORD is required}"
: "${PLATFORM_WORKER_DB_PASSWORD:?PLATFORM_WORKER_DB_PASSWORD is required}"

if [ "${#PLATFORM_API_DB_PASSWORD}" -lt 16 ] || [ "${#PLATFORM_WORKER_DB_PASSWORD}" -lt 16 ]; then
  echo "runtime database passwords must contain at least 16 characters" >&2
  exit 2
fi
if [ "$PLATFORM_API_DB_PASSWORD" = "$PLATFORM_WORKER_DB_PASSWORD" ]; then
  echo "API and Worker database passwords must be different" >&2
  exit 2
fi

psql "$ADMIN_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 \
  -v api_password="$PLATFORM_API_DB_PASSWORD" \
  -v worker_password="$PLATFORM_WORKER_DB_PASSWORD" <<'SQL'
SELECT format(
  'CREATE ROLE platform_api_login LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION CONNECTION LIMIT 20',
  :'api_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'platform_api_login')
\gexec

SELECT format(
  'CREATE ROLE platform_worker_login LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION CONNECTION LIMIT 5',
  :'worker_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'platform_worker_login')
\gexec

ALTER ROLE platform_api_login WITH LOGIN PASSWORD :'api_password'
  NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION CONNECTION LIMIT 20;
ALTER ROLE platform_worker_login WITH LOGIN PASSWORD :'worker_password'
  NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION CONNECTION LIMIT 5;

GRANT platform_api TO platform_api_login;
GRANT platform_worker TO platform_worker_login;

ALTER ROLE platform_api_login SET statement_timeout = '15s';
ALTER ROLE platform_api_login SET lock_timeout = '5s';
ALTER ROLE platform_worker_login SET statement_timeout = '30s';
ALTER ROLE platform_worker_login SET lock_timeout = '5s';
SQL

printf '%s\n' 'runtime database logins provisioned'
