#!/bin/sh
set -eu

: "${BACKUP_FILE:?BACKUP_FILE is required}"
: "${SOURCE_SHA256:?SOURCE_SHA256 is required}"
: "${DESTINATION_REFERENCE:?DESTINATION_REFERENCE is required}"

if [ ! -f "$BACKUP_FILE" ]; then
  echo "copied backup file does not exist" >&2
  exit 2
fi
if ! printf '%s' "$SOURCE_SHA256" | grep -Eq '^[0-9a-f]{64}$'; then
  echo "SOURCE_SHA256 must be 64 lowercase hexadecimal characters" >&2
  exit 2
fi

observed_sha256="$(sha256sum "$BACKUP_FILE" | cut -d' ' -f1)"
if [ "$observed_sha256" != "$SOURCE_SHA256" ]; then
  echo "copied backup checksum does not match source" >&2
  exit 1
fi
observed_bytes="$(wc -c <"$BACKUP_FILE" | tr -d ' ')"

printf 'destination_reference=%s\nobserved_bytes=%s\nobserved_sha256=%s\n' \
  "$DESTINATION_REFERENCE" "$observed_bytes" "$observed_sha256"
