from __future__ import annotations

from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


def read(relative: str) -> str:
    return (ROOT / relative).read_text(encoding="utf-8")


def test_media_gateway_is_bound_to_private_r2_and_has_no_committed_secret() -> None:
    worker = read("infra/cloudflare/media-gateway/src/index.mjs")
    example = read("infra/cloudflare/media-gateway/wrangler.toml.example")
    assert 'env.MEDIA_EDGE_GATEWAY_MODE !== "enforce"' in worker
    assert "configuration.bucket.get(key)" in worker
    assert "policy.mode !== \"normal\"" in worker
    assert "cache.match(objectCacheKey)" in worker
    assert worker.index('policy.mode !== "normal"') < worker.index("cache.match(objectCacheKey)")
    assert "MEDIA_BUCKET" in example
    assert "MEDIA_EDGE_POLICY_TOKEN =" not in example
    assert "wrangler secret put" in example


def test_media_gateway_has_strict_public_key_and_no_external_origin_fallback() -> None:
    worker = read("infra/cloudflare/media-gateway/src/index.mjs")
    assert "media-public/" in worker
    assert "media-master/" not in worker
    assert "cover|gallery|avatar" in worker
    assert "w(320|640|960)" in worker
    assert "Cloudflare-CDN-Cache-Control" in worker
    assert "private, no-store" in worker
    assert "defaultImage(request.method, key" in worker
    assert "fetch(configuration.publicOrigin" not in worker


def test_edge_policy_credential_is_only_in_the_japan_api_service() -> None:
    for path in ("compose.yaml", "infra/compose/japan/compose.yaml"):
        services = yaml.safe_load(read(path))["services"]
        holders = {
            name
            for name, service in services.items()
            if "MEDIA_EDGE_POLICY_TOKEN" in service.get("environment", {})
        }
        assert holders == {"platform-api"}
    beijing = yaml.safe_load(read("infra/compose/beijing/compose.yaml"))["services"]
    assert all("MEDIA_EDGE_POLICY_TOKEN" not in service.get("environment", {}) for service in beijing.values())


def test_api_edge_exposes_one_exact_policy_path_and_hides_the_rest() -> None:
    nginx = read("infra/reverse-proxy/nginx.conf.template")
    api = nginx.split("server_name ${API_HOST};", 1)[1].split("server_name ${OPS_HOST};", 1)[0]
    assert "location = /edge/v1/media-delivery-policy" in api
    assert "location ^~ /edge/" in api
    assert "return 404;" in api.split("location ^~ /edge/", 1)[1]
    assert "location ^~ /internal/" in api
    assert "return 404;" in api.split("location ^~ /internal/", 1)[1]


def test_node_contract_is_part_of_the_existing_release_a_unit_gate() -> None:
    package = read("package.json")
    assert "scripts/tests/media_edge_gateway.test.mjs" in package
