#!/bin/sh
set -eu

: "${DATABASE_URL:?DATABASE_URL is required}"
: "${BACKUP_DIR:?BACKUP_DIR is required}"

AUDIT_DATABASE_URL="${AUDIT_DATABASE_URL:-$DATABASE_URL}"
export AUDIT_DATABASE_URL
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)"
audit_tool="$script_dir/audit_backup.sh"

RETENTION_COUNT="${RETENTION_COUNT-4}"
case "$RETENTION_COUNT" in
  [1-9]|[1-9][0-9]|100) ;;
  *) echo "RETENTION_COUNT must be an integer between 1 and 100" >&2; exit 2 ;;
esac

umask 077
mkdir -p "$BACKUP_DIR"
backup_root="$(cd "$BACKUP_DIR" && pwd -P)"
if [ "$backup_root" = "/" ]; then
  echo "BACKUP_DIR must not resolve to filesystem root" >&2
  exit 2
fi
case "$backup_root" in
  *'
'*|*"$(printf '\r')"*) echo "BACKUP_DIR must not contain line breaks" >&2; exit 2 ;;
esac

lock_dir="$backup_root/.weekly-backup.lock"
if ! mkdir "$lock_dir" 2>/dev/null; then
  echo "backup directory is locked; check for a running or interrupted backup" >&2
  exit 75
fi

temporary="$lock_dir/backup.partial"
checksum_temporary="$lock_dir/checksum.partial"
retention_list="$lock_dir/retention.txt"
prune_plan="$lock_dir/prune.txt"
backup_run_id=""
audit_finished=0

cleanup() {
  status=$?
  trap - 0 HUP INT TERM
  rm -f -- "$temporary" "$checksum_temporary" "$retention_list" "$prune_plan" || true
  if [ "$status" -ne 0 ] && [ -n "$backup_run_id" ] && [ "$audit_finished" -eq 0 ]; then
    sh "$audit_tool" fail "$backup_run_id" backup_command_failed >/dev/null 2>&1 || true
  fi
  rmdir "$lock_dir" || true
  exit "$status"
}
trap cleanup 0
trap 'exit 130' HUP INT TERM

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
final="$backup_root/self-deepsearch-$timestamp.dump"
checksum="$final.sha256"
if [ -e "$final" ] || [ -L "$final" ] || [ -e "$checksum" ] || [ -L "$checksum" ]; then
  echo "backup timestamp already exists; no files were overwritten" >&2
  exit 73
fi

backup_run_id="$(sh "$audit_tool" start weekly japan beijing)"

pg_dump --dbname="$DATABASE_URL" --format=custom --compress=6 --no-owner --no-acl --file="$temporary"
pg_restore --list "$temporary" >/dev/null
bytes="$(wc -c <"$temporary" | tr -d ' ')"
hash="$(sha256sum <"$temporary")"
hash="${hash%% *}"
printf '%s  %s\n' "$hash" "${final##*/}" >"$checksum_temporary"
# Hard links fail if a destination exists, including a directory or symlink.
# Staging and final files are on the same filesystem; nothing is overwritten.
ln -T -- "$temporary" "$final"
ln -T -- "$checksum_temporary" "$checksum"
sh "$audit_tool" complete "$backup_run_id" "$final" "$bytes" "$hash"
audit_finished=1

# Only bare, controlled filenames enter the plan. Never parse arbitrary paths
# from find output as deletion targets.
for candidate in "$backup_root"/self-deepsearch-*.dump; do
  name="${candidate##*/}"
  case "$name" in
    self-deepsearch-[0-9][0-9][0-9][0-9][0-1][0-9][0-3][0-9]T[0-2][0-9][0-5][0-9][0-5][0-9]Z.dump)
      if [ -f "$candidate" ] && [ ! -L "$candidate" ]; then printf '%s\n' "$name"; fi ;;
  esac
done >"$retention_list"
LC_ALL=C sort -r "$retention_list" -o "$retention_list"

# Validate the complete plan before any deletion. Keep the newest N files,
# the newest locally intact verified recovery point, and every unverified or
# unrecognized backup. A SQL error must leave all old files untouched.
: >"$prune_plan"
rank=0
recovery_kept=0
protected_extra=0
while IFS= read -r name; do
  candidate="$backup_root/$name"
  rank=$((rank + 1))
  verification=unverified
  if [ -f "$candidate.sha256" ] && [ ! -L "$candidate.sha256" ]; then
    candidate_hash="$(sha256sum <"$candidate")"
    candidate_hash="${candidate_hash%% *}"
    candidate_bytes="$(wc -c <"$candidate" | tr -d ' ')"
    expected_checksum="$(printf '%s  %s' "$candidate_hash" "$name")"
    if [ "$(cat "$candidate.sha256")" = "$expected_checksum" ]; then
      verification="$(sh "$audit_tool" retention-check "$candidate" "$candidate_hash" "$candidate_bytes")"
    fi
  fi
  if [ "$verification" = verified ] && [ "$recovery_kept" -eq 0 ]; then
    recovery_kept=1
    if [ "$rank" -gt "$RETENTION_COUNT" ]; then protected_extra=$((protected_extra + 1)); fi
    continue
  fi
  if [ "$rank" -le "$RETENTION_COUNT" ]; then continue; fi
  if [ "$candidate" = "$final" ]; then
    protected_extra=$((protected_extra + 1))
    continue
  fi
  if [ "$verification" = verified ]; then
    printf '%s\t%s\t%s\n' "$name" "$candidate_hash" "$candidate_bytes" >>"$prune_plan"
  else
    protected_extra=$((protected_extra + 1))
  fi
done <"$retention_list"

removed=0
while IFS="$(printf '\t')" read -r name expected_hash expected_bytes; do
  candidate="$backup_root/$name"
  if [ -L "$candidate" ] || [ ! -f "$candidate" ] || [ -L "$candidate.sha256" ] || [ ! -f "$candidate.sha256" ]; then
    echo "backup changed during retention; remaining deletions stopped" >&2
    exit 1
  fi
  actual_hash="$(sha256sum <"$candidate")"
  actual_hash="${actual_hash%% *}"
  actual_bytes="$(wc -c <"$candidate" | tr -d ' ')"
  if [ "$actual_hash" != "$expected_hash" ] || [ "$actual_bytes" != "$expected_bytes" ] ||
    [ "$(cat "$candidate.sha256")" != "$(printf '%s  %s' "$actual_hash" "$name")" ]; then
    echo "backup integrity changed during retention; remaining deletions stopped" >&2
    exit 1
  fi
  rm -- "$candidate" "$candidate.sha256"
  removed=$((removed + 1))
done <"$prune_plan"

retention_status=complete
if [ "$protected_extra" -gt 0 ]; then
  retention_status=deferred
  echo "backup retention deferred: protected recovery or unverified files exceed the configured count" >&2
fi
printf 'backup_run_id=%s\nbackup=%s\nbytes=%s\nsha256=%s\nretention_count=%s\nretention_status=%s\nretention_removed_count=%s\nretention_extra_count=%s\nstatus=completed\n' \
  "$backup_run_id" "$final" "$bytes" "$hash" "$RETENTION_COUNT" "$retention_status" "$removed" "$protected_extra"
