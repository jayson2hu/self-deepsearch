#!/bin/sh
set -eu

# The official image's temporary postmaster only exposes this Unix socket while
# docker-entrypoint-initdb.d scripts run.
export ADMIN_DATABASE_URL="postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@/${POSTGRES_DB}?host=/var/run/postgresql"
exec sh /database-tools/provision_runtime_logins.sh
