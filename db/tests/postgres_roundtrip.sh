#!/bin/sh
set -eu

apply_section() {
  migration="$1"
  section="$2"

  awk -v requested_section="${section}" '
    /^-- \+goose Up/ { current_section = "up"; next }
    /^-- \+goose Down/ { current_section = "down"; next }
    current_section == requested_section { print }
  ' "${migration}" | psql --single-transaction --set ON_ERROR_STOP=1 "${DATABASE_URL}"
}

load_release_a_fixtures() {
  ALLOW_SYNTHETIC_FIXTURES=true \
    CONFIRM_SYNTHETIC_FIXTURES=release-a-test-only \
    ADMIN_DATABASE_URL="${DATABASE_URL}" \
    SEED_FILE=db/seeds/development.sql \
    sh infra/database/load_release_a_fixtures.sh
}

for migration in db/migrations/*.sql; do
  apply_section "${migration}" up
done

actual_version="$(psql --tuples-only --no-align "${DATABASE_URL}" --command 'SELECT schema_version FROM platform.system_metadata WHERE singleton')"
expected_version="$(find db/migrations -maxdepth 1 -name '*.sql' | wc -l | tr -d ' ')"
test "${actual_version}" = "${expected_version}"

for rejected_confirmation in 0 true; do
  if psql --no-psqlrc --single-transaction --set ON_ERROR_STOP=1 \
    --set "release_a_fixture=$rejected_confirmation" "${DATABASE_URL}" --file db/seeds/development.sql >/dev/null 2>&1; then
    echo "fixture seed accepted an invalid confirmation" >&2
    exit 1
  fi
done
if psql --no-psqlrc --single-transaction --set ON_ERROR_STOP=1 \
  "${DATABASE_URL}" --file db/seeds/development.sql >/dev/null 2>&1; then
  echo "fixture seed accepted a missing confirmation" >&2
  exit 1
fi
test "$(psql --tuples-only --no-align "${DATABASE_URL}" --command 'SELECT count(*) FROM platform.works')" = "0"

load_release_a_fixtures
published_works="$(psql --tuples-only --no-align "${DATABASE_URL}" --command 'SELECT count(*) FROM platform.public_published_works')"
test "${published_works}" = "99"
test "$(psql --tuples-only --no-align "${DATABASE_URL}" --command 'SELECT count(*) FROM platform.works')" = "100"
test "$(psql --tuples-only --no-align "${DATABASE_URL}" --command 'SELECT count(*) FROM platform.performers')" = "30"

# A second production-loader invocation must be a stable no-op from the
# catalog consumer's perspective.
load_release_a_fixtures
test "$(psql --tuples-only --no-align "${DATABASE_URL}" --command 'SELECT count(*) FROM platform.works')" = "100"
test "$(psql --tuples-only --no-align "${DATABASE_URL}" --command 'SELECT count(*) FROM platform.performers')" = "30"

# Once any non-fixture catalog record exists, the production loader must stop
# before it can mix synthetic and real source data.
psql --no-psqlrc --set ON_ERROR_STOP=1 "${DATABASE_URL}" --command \
  "INSERT INTO collector.sources (source_id, name, source_type) VALUES ('80000000-0000-4000-8000-000000000099', 'roundtrip real-source sentinel', 'manual')"
if load_release_a_fixtures >/dev/null 2>&1; then
  echo "fixture loader accepted a database containing non-fixture catalog data" >&2
  exit 1
fi
psql --no-psqlrc --set ON_ERROR_STOP=1 "${DATABASE_URL}" --command \
  "DELETE FROM collector.sources WHERE source_id = '80000000-0000-4000-8000-000000000099'"

# The migration-owned manual source is allowed only while unused. Its fixed
# UUID must not hide real ingestion that has not reached the catalog yet.
psql --no-psqlrc --set ON_ERROR_STOP=1 "${DATABASE_URL}" <<'SQL'
INSERT INTO collector.publication_batches
  (batch_id, source_id, region, idempotency_key, request_hash)
VALUES ('82000000-0000-4000-8000-000000000099',
        '81000000-0000-4000-8000-000000000001', 'japan',
        'roundtrip-manual-batch', 'sha256:' || repeat('a', 64));
SQL
if load_release_a_fixtures >/dev/null 2>&1; then
  echo "fixture loader accepted existing manual ingestion" >&2
  exit 1
fi
psql --no-psqlrc --set ON_ERROR_STOP=1 "${DATABASE_URL}" <<'SQL'
DELETE FROM collector.publication_batches WHERE batch_id = '82000000-0000-4000-8000-000000000099';
INSERT INTO collector.source_records
  (source_record_id, source_id, external_id, entity_type, connector_version,
   idempotency_key, content_hash, payload)
VALUES ('83000000-0000-4000-8000-000000000099',
        '81000000-0000-4000-8000-000000000001', 'roundtrip-manual-record',
        'work', 'roundtrip/1', 'roundtrip-manual-record',
        'sha256:' || repeat('b', 64), '{}');
SQL
if load_release_a_fixtures >/dev/null 2>&1; then
  echo "fixture loader accepted an unbatched manual source record" >&2
  exit 1
fi
psql --no-psqlrc --set ON_ERROR_STOP=1 "${DATABASE_URL}" --command \
  "DELETE FROM collector.source_records WHERE source_record_id = '83000000-0000-4000-8000-000000000099'"
load_release_a_fixtures
test "$(psql --tuples-only --no-align "${DATABASE_URL}" --command 'SELECT count(*) FROM platform.works')" = "100"

psql "${DATABASE_URL}" --file db/tests/postgres_contracts.sql
psql --no-psqlrc "${DATABASE_URL}" --file db/tests/media_visibility_contracts.sql

CONFIRM_BACKUP_RETENTION_CONTRACTS=disposable-database \
  sh db/tests/backup_retention_contracts.sh

if [ -n "${PLATFORM_API_DATABASE_URL:-}" ] || [ -n "${PLATFORM_WORKER_DATABASE_URL:-}" ]; then
  sh db/tests/runtime_login_contracts.sh
fi

find db/migrations -name '*.sql' -print | sort -r | while IFS= read -r migration; do
  apply_section "${migration}" down
done

remaining_schemas="$(psql --tuples-only --no-align "${DATABASE_URL}" --command "SELECT count(*) FROM information_schema.schemata WHERE schema_name IN ('collector', 'platform', 'audit')")"
test "${remaining_schemas}" = "0"
