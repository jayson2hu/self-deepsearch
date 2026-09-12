#!/bin/sh
set -eu

# Synthetic audit rows only; run after migrations in the disposable roundtrip DB.
: "${DATABASE_URL:?DATABASE_URL is required}"
if [ "${CONFIRM_BACKUP_RETENTION_CONTRACTS:-}" != disposable-database ]; then
  echo "backup retention contracts require a disposable database confirmation" >&2
  exit 2
fi
AUDIT_DATABASE_URL="$DATABASE_URL"
export AUDIT_DATABASE_URL
audit_tool=infra/backup/audit_backup.sh
hash=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
other_hash=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
bytes=42

expect_evidence() {
  expected="$1"
  shift
  actual="$(sh "$audit_tool" retention-check "$@")"
  if [ "$actual" != "$expected" ]; then
    echo "backup retention evidence mismatch: expected $expected, got $actual" >&2
    exit 1
  fi
}

run_id="$(sh "$audit_tool" start weekly japan beijing)"
# Include spaces and an SQL quote to exercise psql value binding, not SQL text.
reference="/ci/backup fixtures/owner's-$run_id.dump"
expect_evidence unverified "$reference" "$hash" "$bytes"
sh "$audit_tool" complete "$run_id" "$reference" "$bytes" "$hash"
expect_evidence unverified "$reference" "$hash" "$bytes"
if sh "$audit_tool" mark-verified "$run_id" "$hash" 16 >/dev/null 2>&1; then
  echo "restore verification accepted a backup without copy evidence" >&2
  exit 1
fi
if sh "$audit_tool" mark-copy "$run_id" "beijing:$reference" "$other_hash" "$bytes" >/dev/null 2>&1; then
  echo "copy evidence accepted a mismatched hash" >&2
  exit 1
fi
sh "$audit_tool" mark-copy "$run_id" "beijing:$reference" "$hash" "$bytes"
expect_evidence unverified "$reference" "$hash" "$bytes"
sh "$audit_tool" mark-verified "$run_id" "$hash" 16
expect_evidence verified "$reference" "$hash" "$bytes"
expect_evidence unverified "$reference.changed" "$hash" "$bytes"
expect_evidence unverified "$reference" "$other_hash" "$bytes"
expect_evidence unverified "$reference" "$hash" 43

# Status alone must not authorize pruning if stored copy/restore hashes or
# copy size do not agree with the original evidence. Restore each tested key.
for key in copy_sha256 copy_byte_size restored_sha256; do
  psql "$DATABASE_URL" --no-psqlrc --quiet -v ON_ERROR_STOP=1 \
    -v run_id="$run_id" -v key="$key" <<'SQL'
UPDATE audit.backup_runs
SET summary = summary - :'key'
WHERE backup_run_id = :'run_id'::uuid;
SQL
  expect_evidence unverified "$reference" "$hash" "$bytes"
  psql "$DATABASE_URL" --no-psqlrc --quiet -v ON_ERROR_STOP=1 \
    -v run_id="$run_id" -v key="$key" <<'SQL'
UPDATE audit.backup_runs
SET summary = summary || jsonb_build_object(:'key', 'mismatched')
WHERE backup_run_id = :'run_id'::uuid;
SQL
  expect_evidence unverified "$reference" "$hash" "$bytes"
  psql "$DATABASE_URL" --no-psqlrc --quiet -v ON_ERROR_STOP=1 \
    -v run_id="$run_id" -v key="$key" <<'SQL'
UPDATE audit.backup_runs
SET summary = summary || jsonb_build_object(
  :'key', CASE WHEN :'key' = 'copy_byte_size' THEN byte_size::text ELSE sha256 END
)
WHERE backup_run_id = :'run_id'::uuid;
SQL
  expect_evidence verified "$reference" "$hash" "$bytes"
done

# A verified manual/daily or reverse-region run is not a weekly Japan source.
for scope in daily:japan:beijing weekly:beijing:japan; do
  backup_type="${scope%%:*}"
  regions="${scope#*:}"
  source_region="${regions%%:*}"
  destination_region="${regions#*:}"
  scoped_id="$(sh "$audit_tool" start "$backup_type" "$source_region" "$destination_region")"
  scoped_reference="/ci/$scoped_id.dump"
  sh "$audit_tool" complete "$scoped_id" "$scoped_reference" "$bytes" "$hash"
  sh "$audit_tool" mark-copy "$scoped_id" "$destination_region:$scoped_reference" "$hash" "$bytes"
  sh "$audit_tool" mark-verified "$scoped_id" "$hash" 16
  expect_evidence unverified "$scoped_reference" "$hash" "$bytes"
done

printf 'backup_retention_contracts=passed\n'
