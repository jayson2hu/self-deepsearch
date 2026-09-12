#!/bin/sh
set -eu

for migration in /migrations/*.sql; do
  echo "applying development bootstrap migration: ${migration}"
  awk '
    /^-- \+goose Up/ { in_up = 1; next }
    /^-- \+goose Down/ { in_up = 0; next }
    in_up { print }
  ' "${migration}" | psql --single-transaction --set ON_ERROR_STOP=1 --username "${POSTGRES_USER}" --dbname "${POSTGRES_DB}"
done
