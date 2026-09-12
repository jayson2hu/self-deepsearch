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

if psql --no-psqlrc --single-transaction --set ON_ERROR_STOP=1 \
  --set release_a_fixture=0 "${DATABASE_URL}" --file db/seeds/development.sql >/dev/null 2>&1; then
  echo "fixture seed accepted release_a_fixture=0" >&2
  exit 1
fi

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
