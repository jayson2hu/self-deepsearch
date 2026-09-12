#!/bin/sh
set -eu

: "${ADMIN_DATABASE_URL:?ADMIN_DATABASE_URL is required}"

if [ "${ALLOW_SYNTHETIC_FIXTURES:-false}" != "true" ]; then
  echo "synthetic fixture loading is disabled" >&2
  exit 2
fi

if [ "${CONFIRM_SYNTHETIC_FIXTURES:-}" != "release-a-test-only" ]; then
  echo "CONFIRM_SYNTHETIC_FIXTURES must be release-a-test-only" >&2
  exit 2
fi

seed_file=${SEED_FILE:-/seeds/development.sql}
if [ ! -f "$seed_file" ]; then
  echo "Release A fixture seed file is missing" >&2
  exit 2
fi

psql "$ADMIN_DATABASE_URL" \
  --no-psqlrc \
  --single-transaction \
  --set ON_ERROR_STOP=1 \
  --set release_a_fixture=1 \
  --file "$seed_file"

printf '%s\n' 'release_a_synthetic_fixtures=loaded'
