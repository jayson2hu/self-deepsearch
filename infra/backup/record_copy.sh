#!/bin/sh
set -eu

: "${AUDIT_DATABASE_URL:?AUDIT_DATABASE_URL is required}"
: "${BACKUP_RUN_ID:?BACKUP_RUN_ID is required}"
: "${DESTINATION_REFERENCE:?DESTINATION_REFERENCE is required}"
: "${OBSERVED_SHA256:?OBSERVED_SHA256 is required}"
: "${OBSERVED_BYTES:?OBSERVED_BYTES is required}"

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)"
sh "$script_dir/audit_backup.sh" mark-copy "$BACKUP_RUN_ID" "$DESTINATION_REFERENCE" "$OBSERVED_SHA256" "$OBSERVED_BYTES"
printf 'backup_run_id=%s\nstatus=copy_recorded\n' "$BACKUP_RUN_ID"
