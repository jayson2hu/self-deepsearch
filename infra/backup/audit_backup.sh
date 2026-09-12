#!/bin/sh
set -eu

: "${AUDIT_DATABASE_URL:?AUDIT_DATABASE_URL is required}"

command_name="${1:-}"

require_uuid() {
  if ! printf '%s' "$1" | grep -Eq '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$'; then
    echo "backup run id must be a UUID" >&2
    exit 2
  fi
}

require_sha256() {
  if ! printf '%s' "$1" | grep -Eq '^[0-9a-f]{64}$'; then
    echo "sha256 must be 64 lowercase hexadecimal characters" >&2
    exit 2
  fi
}

require_positive_integer() {
  case "$1" in *[!0-9]*|'') echo "$2 must be a positive integer" >&2; exit 2;; esac
  if [ "$1" -le 0 ]; then
    echo "$2 must be a positive integer" >&2
    exit 2
  fi
}

require_result() {
  if [ "$1" != "ok" ]; then
    echo "backup audit transition was rejected" >&2
    exit 1
  fi
}

case "$command_name" in
  start)
    backup_type="${2:-weekly}"
    source_region="${3:-japan}"
    destination_region="${4:-beijing}"
    case "$backup_type" in daily|weekly|manual) ;; *) echo "unsupported backup type" >&2; exit 2;; esac
    case "$source_region:$destination_region" in japan:beijing|beijing:japan) ;; *) echo "backup regions must be japan and beijing" >&2; exit 2;; esac
    psql "$AUDIT_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -At \
      -v backup_type="$backup_type" -v source_region="$source_region" -v destination_region="$destination_region" <<'SQL'
INSERT INTO audit.backup_runs (backup_type, backup_status, source_region, destination_region)
VALUES (:'backup_type', 'running', :'source_region', :'destination_region')
RETURNING backup_run_id;
SQL
    ;;
  complete)
    backup_run_id="${2:-}"
    file_reference="${3:-}"
    byte_size="${4:-}"
    sha256="${5:-}"
    require_uuid "$backup_run_id"
    [ -n "$file_reference" ] || { echo "file reference is required" >&2; exit 2; }
    require_positive_integer "$byte_size" "byte size"
    require_sha256 "$sha256"
    result="$(psql "$AUDIT_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -At \
      -v backup_run_id="$backup_run_id" -v file_reference="$file_reference" \
      -v byte_size="$byte_size" -v sha256="$sha256" <<'SQL'
UPDATE audit.backup_runs
SET backup_status = 'completed', file_reference = :'file_reference',
    byte_size = :'byte_size'::bigint, sha256 = :'sha256', completed_at = clock_timestamp()
WHERE backup_run_id = :'backup_run_id'::uuid AND backup_status = 'running'
RETURNING 'ok';
SQL
)"
    require_result "$result"
    ;;
  fail)
    backup_run_id="${2:-}"
    error_code="${3:-backup_command_failed}"
    require_uuid "$backup_run_id"
    if ! printf '%s' "$error_code" | grep -Eq '^[a-z0-9_]{1,64}$'; then
      echo "invalid backup error code" >&2
      exit 2
    fi
    result="$(psql "$AUDIT_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -At \
      -v backup_run_id="$backup_run_id" -v error_code="$error_code" <<'SQL'
UPDATE audit.backup_runs
SET backup_status = 'failed', completed_at = clock_timestamp(), error_code = :'error_code'
WHERE backup_run_id = :'backup_run_id'::uuid AND backup_status = 'running'
RETURNING 'ok';
SQL
)"
    require_result "$result"
    ;;
  mark-copy)
    backup_run_id="${2:-}"
    destination_reference="${3:-}"
    observed_sha256="${4:-}"
    observed_bytes="${5:-}"
    require_uuid "$backup_run_id"
    [ -n "$destination_reference" ] || { echo "destination reference is required" >&2; exit 2; }
    require_sha256 "$observed_sha256"
    require_positive_integer "$observed_bytes" "observed bytes"
    result="$(psql "$AUDIT_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -At \
      -v backup_run_id="$backup_run_id" -v destination_reference="$destination_reference" \
      -v observed_sha256="$observed_sha256" -v observed_bytes="$observed_bytes" <<'SQL'
UPDATE audit.backup_runs
SET copied_at = clock_timestamp(),
    summary = summary || jsonb_build_object(
      'copy_reference', :'destination_reference',
      'copy_sha256', :'observed_sha256',
      'copy_byte_size', :'observed_bytes'::bigint
    )
WHERE backup_run_id = :'backup_run_id'::uuid AND backup_status = 'completed'
  AND copied_at IS NULL AND sha256 = :'observed_sha256' AND byte_size = :'observed_bytes'::bigint
RETURNING 'ok';
SQL
)"
    require_result "$result"
    ;;
  mark-verified)
    backup_run_id="${2:-}"
    restored_sha256="${3:-}"
    schema_version="${4:-}"
    require_uuid "$backup_run_id"
    require_sha256 "$restored_sha256"
    require_positive_integer "$schema_version" "schema version"
    result="$(psql "$AUDIT_DATABASE_URL" --quiet -v ON_ERROR_STOP=1 -At \
      -v backup_run_id="$backup_run_id" -v restored_sha256="$restored_sha256" \
      -v schema_version="$schema_version" <<'SQL'
UPDATE audit.backup_runs
SET backup_status = 'verified', restore_verified_at = clock_timestamp(),
    summary = summary || jsonb_build_object(
      'restored_sha256', :'restored_sha256',
      'restored_schema_version', :'schema_version'::integer
    )
WHERE backup_run_id = :'backup_run_id'::uuid AND backup_status = 'completed'
  AND copied_at IS NOT NULL AND restore_verified_at IS NULL AND sha256 = :'restored_sha256'
RETURNING 'ok';
SQL
)"
    require_result "$result"
    ;;
  retention-check)
    file_reference="${2:-}"
    observed_sha256="${3:-}"
    observed_bytes="${4:-}"
    [ -n "$file_reference" ] || { echo "file reference is required" >&2; exit 2; }
    require_sha256 "$observed_sha256"
    require_positive_integer "$observed_bytes" "observed bytes"
    result="$(psql "$AUDIT_DATABASE_URL" --no-psqlrc --quiet -v ON_ERROR_STOP=1 -At \
      -v file_reference="$file_reference" -v observed_sha256="$observed_sha256" \
      -v observed_bytes="$observed_bytes" <<'SQL'
SELECT CASE WHEN EXISTS (
  SELECT 1 FROM audit.backup_runs
  WHERE backup_type = 'weekly' AND backup_status = 'verified'
    AND source_region = 'japan' AND destination_region = 'beijing'
    AND file_reference = :'file_reference'
    AND sha256 = :'observed_sha256' AND byte_size = :'observed_bytes'::bigint
    AND copied_at IS NOT NULL AND restore_verified_at IS NOT NULL
    AND summary->>'copy_sha256' = sha256
    AND summary->>'copy_byte_size' = byte_size::text
    AND summary->>'restored_sha256' = sha256
) THEN 'verified' ELSE 'unverified' END;
SQL
)"
    case "$result" in
      verified|unverified) printf '%s\n' "$result" ;;
      *) echo "backup retention evidence check returned an invalid result" >&2; exit 1 ;;
    esac
    ;;
  *)
    echo "usage: audit_backup.sh {start|complete|fail|mark-copy|mark-verified|retention-check} ..." >&2
    exit 2
    ;;
esac
