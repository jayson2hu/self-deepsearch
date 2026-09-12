from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def test_dynamic_delivery_is_read_only_private_and_fail_closed() -> None:
    evaluator = read("services/platform-api/internal/operations/media_delivery.go")
    repository = read("services/platform-api/internal/database/media_delivery.go")
    api = read("services/platform-api/internal/httpapi/media_delivery.go")
    runtime = read("apps/display-web/lib/media-runtime.ts")
    for marker in (
        "ReviewRequired",
        "StopRecommended",
        "30*time.Minute",
        'LastStatus != "low_estimate"',
        'LastStatus != "warning"',
    ):
        assert marker in evaluator
    assert "SELECT period_start" in repository
    assert "INSERT" not in repository and "UPDATE" not in repository and "DELETE" not in repository
    assert "context.WithTimeout(ctx, 750*time.Millisecond)" in api
    assert "return true, false" in api
    assert 'const policyPath = "/internal/v1/media-delivery-policy"' in runtime
    assert "AbortSignal.timeout(1000)" in runtime
    assert "defaultOnly: true, available: false" in runtime


def test_dynamic_delivery_config_only_reaches_japan_api_and_display() -> None:
    for path in ("compose.yaml", "infra/compose/japan/compose.yaml"):
        services = yaml.safe_load(read(path))["services"]
        holders = {
            name
            for name, service in services.items()
            if "MEDIA_DELIVERY_DYNAMIC_MODE" in service.get("environment", {})
        }
        assert holders == {"platform-api", "display-web"}
    beijing = yaml.safe_load(read("infra/compose/beijing/compose.yaml"))["services"]
    assert all("MEDIA_DELIVERY_DYNAMIC_MODE" not in service.get("environment", {}) for service in beijing.values())


def test_dynamic_delivery_internal_contract_is_not_public() -> None:
    nginx = read("infra/reverse-proxy/nginx.conf.template")
    api_server = nginx.split("server_name ${API_HOST};", 1)[1].split("server_name ${OPS_HOST};", 1)[0]
    assert "location ^~ /internal/" in api_server and "return 404;" in api_server
    openapi = yaml.safe_load(read("packages/api-contracts/openapi.yaml"))
    assert "/internal/v1/media-delivery-policy" not in openapi["paths"]
    alerts = read("infra/monitoring/alerts.yml")
    assert "PlatformMediaDeliveryPolicyUnavailable" in alerts
    assert "self_deepsearch_media_delivery_policy_available == 0" in alerts
