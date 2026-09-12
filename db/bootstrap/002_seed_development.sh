#!/bin/sh
set -eu

psql --no-psqlrc --single-transaction --set ON_ERROR_STOP=1 \
  --set release_a_fixture=1 \
  --username "${POSTGRES_USER}" \
  --dbname "${POSTGRES_DB}" \
  --file /seeds/development.sql
