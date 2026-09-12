from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def test_review_authority_is_split_and_state_is_bound_to_immutable_receipts() -> None:
    migration = read("db/migrations/00020_media_usage_reviews.sql")
    for marker in (
        "GRANT SELECT, INSERT ON audit.media_usage_reviews TO platform_api",
        "GRANT SELECT, UPDATE ON audit.media_usage_reviews TO platform_worker",
        "SECURITY DEFINER SET search_path = pg_catalog",
        "FROM platform.users WHERE user_id=actor FOR SHARE",
        "REVOKE ALL ON FUNCTION platform.lock_media_usage_reviewer(uuid) FROM PUBLIC",
        "REVOKE ALL ON FUNCTION platform.lock_media_usage_review_state() FROM PUBLIC",
        "GRANT EXECUTE ON FUNCTION platform.lock_media_usage_review_state() TO platform_api",
        "FROM platform.media_usage_state AS state WHERE state.singleton FOR UPDATE",
        "review.before_state IS DISTINCT FROM to_jsonb(OLD)",
        "review.after_state IS DISTINCT FROM to_jsonb(NEW)",
        "review.expires_at <= clock_timestamp()",
        "NEW.period_start < OLD.period_end",
        "NEW.last_recommendation <> 'hold_for_review'",
        "OLD.last_status NOT IN ('low_estimate','warning')",
        "OLD.stop_recommended",
        "CREATE UNIQUE INDEX media_usage_one_pending_review_idx",
        "DEFERRABLE INITIALLY DEFERRED",
        "state.last_review_id=NEW.review_id AND to_jsonb(state)=NEW.after_state",
    ):
        assert marker in migration
    assert "GRANT DELETE" not in migration
    assert "GRANT SELECT ON platform.users" not in migration
    assert "TO public_reader" not in migration
    assert "GRANT UPDATE ON platform.media_usage_state TO platform_api" not in migration
    repository = read("services/platform-api/internal/database/media_usage.go")
    assert "FROM platform.lock_media_usage_review_state()" in repository
    assert "WHERE singleton FOR UPDATE" not in repository
    contract = read("services/platform-api/internal/database/media_usage_contract_test.go")
    for marker in ("CONFIRM_MEDIA_USAGE_API_CONTRACT", "store.SubmitMediaUsageReview", "store.GetMediaUsage", "FOR UPDATE NOWAIT", 'sqlError.Code != "55P03"', "has_table_privilege"):
        assert marker in contract


def test_review_path_is_authenticated_asynchronous_and_current_schema_guarded() -> None:
    spec = yaml.safe_load(read("packages/api-contracts/openapi.yaml"))
    mutation = spec["paths"]["/admin/v1/media/usage/reviews"]["post"]
    assert mutation["security"] == [{"sessionCookie": [], "recentAuthCookie": []}]
    assert {"200", "202", "400", "401", "403", "404", "409", "503"} <= mutation["responses"].keys()
    assert "confirmed" in spec["components"]["schemas"]["MediaUsageReviewInput"]["required"]
    assert spec["components"]["schemas"]["MediaUsageReviewResult"]["properties"]["enforcement"]["enum"] == ["not_connected"]
    assert "nullable" not in str(spec["components"]["schemas"]["MediaUsageOverview"])
    main = read("services/platform-worker/cmd/worker/main.go")
    assert "Reviews: usageRepository" in main
    runner = read("services/platform-worker/internal/mediausage/runner.go")
    assert runner.index("r.Reviews.ProcessReviews") < runner.index("r.Repository.Prepare")
    contract = read("services/platform-worker/internal/mediausage/reviews_contract_test.go")
    for marker in ("CONFIRM_MEDIA_USAGE_CONTRACT", "validUsageContractURL", "version != 21", "SET LOCAL ROLE platform_api", "receipt committed without matching state transition"):
        assert marker in contract


def test_operator_ui_keeps_uncertain_identity_and_never_claims_media_recovery() -> None:
    component = read("apps/ops-web/components/media-usage-manager.tsx")
    for marker in ("setUncertain(input)", "send(uncertain)", "crypto.randomUUID()", "expected_updated_at: overview.state.updated_at", "重试同一请求", "尚未处理，更不代表已恢复图片", "formatDateTime"):
        assert marker in component
    client = read("apps/ops-web/lib/media-usage-client.ts")
    assert 'body.enforcement !== "not_connected"' in client
    assert "response.status === 202" in client
    assert "body.review.idempotent_replay" in client
    page = read("apps/ops-web/app/media/usage/page.tsx")
    assert 'user.role !== "admin" && user.role !== "owner"' in page
