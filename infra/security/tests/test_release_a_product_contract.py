from __future__ import annotations

import json
import re
import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


class ReleaseAProductContractTests(unittest.TestCase):
    def test_primary_replacement_contract_requires_cas_and_truthful_retirement(self) -> None:
        spec = yaml.safe_load((ROOT / "packages/api-contracts/openapi.yaml").read_text(encoding="utf-8"))
        operation = spec["paths"]["/admin/v1/media/primary/replace"]["post"]
        self.assertEqual(operation["security"], [{"sessionCookie": [], "recentAuthCookie": []}])
        for text in ("expected_asset_id", "commit in one transaction", "Shared old assets", "30 days", "Rights takedown"):
            self.assertIn(text, operation["description"])
        self.assertIn("remote deletion is not yet confirmed", operation["responses"]["201"]["description"])
        self.assertIn("never automatically overwrite", operation["responses"]["409"]["description"])
        self.assertIn("uncertain commit", operation["responses"]["503"]["description"])
        for status in ("400", "409"):
            self.assertEqual(set(operation["responses"][status]), {"description"})
        read = spec["paths"]["/admin/v1/media/primary"]["get"]
        self.assertEqual(read["security"], [{"sessionCookie": []}])
        self.assertIn("Does not expose private storage locations", read["description"])

    def test_takedown_contract_distinguishes_queue_completion_from_storage_deletion(self) -> None:
        spec = yaml.safe_load((ROOT / "packages/api-contracts/openapi.yaml").read_text(encoding="utf-8"))
        operation = spec["paths"]["/admin/v1/takedowns/{takedown_id}/complete"]["post"]
        self.assertIn("immutable media object ID", operation["description"])
        self.assertIn("never postpones work or resets running/dead tasks", operation["description"])
        self.assertIn("remote deletion is not yet confirmed", operation["responses"]["200"]["description"])
        self.assertIn("503", operation["responses"])
        self.assertEqual(operation["security"], [{"sessionCookie": [], "recentAuthCookie": []}])

    def test_media_registration_contract_requires_durable_cache_refresh_and_state_guard(self) -> None:
        spec = yaml.safe_load((ROOT / "packages/api-contracts/openapi.yaml").read_text(encoding="utf-8"))
        operation = spec["paths"]["/admin/v1/media/manifests"]["post"]
        self.assertIn("commit in one transaction", operation["description"])
        self.assertIn("does not replace an existing primary image", operation["description"])
        self.assertIn("withdrawn or merged", operation["responses"]["409"]["description"])
        self.assertIn("could not be confirmed", operation["responses"]["503"]["description"])
        self.assertEqual(operation["security"], [{"sessionCookie": [], "recentAuthCookie": []}])

    def test_role_change_contract_requires_fresh_login_and_safe_noop(self) -> None:
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        operation = spec["paths"]["/admin/v1/users/{user_id}/role"]["post"]
        self.assertIn("atomically revokes all prior sessions", operation["description"])
        self.assertIn("409", operation["responses"])
        self.assertIn("503", operation["responses"])
        self.assertEqual(operation["security"], [{"sessionCookie": [], "recentAuthCookie": []}])
        manager = (ROOT / "apps/ops-web/components/user-manager.tsx").read_text(encoding="utf-8")
        self.assertIn("修改角色会使该账号在所有设备上的旧登录会话失效", manager)
        self.assertIn("角色已更新，该账号的旧登录会话已失效，需要重新登录。", manager)

    def test_password_work_overload_contract_is_retryable_for_all_expensive_routes(self) -> None:
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        for suffix in ("signup/verify", "invitations/accept", "login", "reauth", "password/reset"):
            with self.subTest(route=suffix):
                response = spec["paths"][f"/api/v1/auth/{suffix}"]["post"]["responses"]["503"]
                self.assertIn("AUTH_BUSY", response["description"])
                self.assertEqual(response["headers"]["Retry-After"]["schema"]["enum"], ["1"])
                self.assertEqual(response["content"]["application/json"]["schema"]["$ref"], "#/components/schemas/ErrorResponse")

    def test_invitation_sql_contract_uses_safe_repeatable_restricted_fixtures(self) -> None:
        contract = (ROOT / "services" / "platform-api" / "internal" / "database" / "invitation_store_test.go").read_text(encoding="utf-8")
        self.assertIn("validIdentityContractURL(databaseURL)", contract)
        self.assertIn("schemaVersion != 21", contract)
        self.assertIn("NOT rolsuper AND NOT rolbypassrls", contract)
        self.assertIn("identity.NewSessionToken()", contract)
        self.assertNotIn("DELETE FROM", contract)

    def test_go_router_and_openapi_operations_have_exact_bidirectional_parity(self) -> None:
        server = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "server.go").read_text(encoding="utf-8")
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))

        def normalize(path: str) -> str:
            return re.sub(r"\{[^{}]+\}", "{param}", path)

        router_operations = {
            (method.upper(), normalize(path))
            for method, path in re.findall(r'router\.(Get|Post|Put|Patch|Delete)\("([^"]+)"', server)
        }
        openapi_operations = {
            (method.upper(), normalize(path))
            for path, definition in spec["paths"].items()
            for method in definition
            if method.lower() in {"get", "post", "put", "patch", "delete"}
        }

        self.assertEqual(len(router_operations), 87)
        self.assertSetEqual(router_operations, openapi_operations)

        def resolve_local_reference(value: dict) -> dict:
            resolved = value
            while "$ref" in resolved:
                reference = resolved["$ref"]
                self.assertTrue(reference.startswith("#/"), f"unsupported non-local reference: {reference}")
                resolved = spec
                for segment in reference[2:].split("/"):
                    resolved = resolved[segment.replace("~1", "/").replace("~0", "~")]
            return resolved

        operation_ids = []
        for path, definition in spec["paths"].items():
            expected_parameters = set(re.findall(r"\{([^{}]+)\}", path))
            common_parameters = definition.get("parameters", [])
            for method, operation in definition.items():
                if method.lower() not in {"get", "post", "put", "patch", "delete"}:
                    continue
                operation_ids.append(operation.get("operationId"))
                declared_parameters = {
                    parameter["name"]
                    for raw_parameter in common_parameters + operation.get("parameters", [])
                    for parameter in [resolve_local_reference(raw_parameter)]
                    if parameter.get("in") == "path" and parameter.get("required") is True
                }
                self.assertSetEqual(
                    expected_parameters,
                    declared_parameters,
                    f"{method.upper()} {path} path parameters do not match",
                )

        self.assertEqual(len(operation_ids), 87)
        self.assertNotIn(None, operation_ids)
        self.assertEqual(len(set(operation_ids)), 87)

    def test_openapi_recent_auth_contract_matches_go_high_risk_policy(self) -> None:
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))

        recent_auth_operations = {
            (method.upper(), path)
            for path, definition in spec["paths"].items()
            for method, operation in definition.items()
            if method.lower() in {"get", "post", "put", "patch", "delete"}
            and any("recentAuthCookie" in requirement for requirement in operation.get("security", []))
        }
        expected = {
            ("POST", "/admin/v1/editorial-recommendations"),
            ("PUT", "/admin/v1/editorial-recommendations/{recommendation_id}"),
            ("DELETE", "/admin/v1/editorial-recommendations/{recommendation_id}"),
            ("PUT", "/admin/v1/site-settings/discovery-mix"),
            ("POST", "/admin/v1/review-tasks/{task_id}/reassign"),
            ("POST", "/admin/v1/review-tasks/{task_id}/approve"),
            ("POST", "/admin/v1/review-tasks/{task_id}/reject"),
            ("POST", "/admin/v1/conflicts/{conflict_id}/resolve"),
            ("POST", "/admin/v1/entities/{entity_id}/publish"),
            ("POST", "/admin/v1/entities/{entity_id}/hide"),
            ("POST", "/admin/v1/entities/{entity_id}/merge"),
            ("POST", "/admin/v1/users/{user_id}/role"),
            ("POST", "/admin/v1/users/{user_id}/status"),
            ("POST", "/admin/v1/invitations"),
            ("POST", "/admin/v1/invitations/{invitation_id}/revoke"),
            ("POST", "/admin/v1/media/manifests"),
            ("POST", "/admin/v1/media/primary/replace"),
            ("POST", "/admin/v1/media/usage/reviews"),
            ("POST", "/admin/v1/media/upload-control/commands"),
            ("POST", "/admin/v1/takedowns/{takedown_id}/complete"),
        }
        self.assertSetEqual(recent_auth_operations, expected)

        go_policy_tests = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "auth_handlers_test.go").read_text(encoding="utf-8")
        self.assertIn('/admin/v1/takedowns/10000000-0000-4000-8000-000000000001/complete", true', go_policy_tests)
        self.assertIn('/admin/v1/conflicts/10000000-0000-4000-8000-000000000001/resolve", true', go_policy_tests)
        self.assertIn('http.MethodPut, "/admin/v1/editorial-recommendations/10000000-0000-4000-8000-000000000001", true', go_policy_tests)
        self.assertIn('http.MethodPost, "/admin/v1/media/upload-control/commands", true', go_policy_tests)

    def test_account_close_notice_version_is_consistent_across_all_clients(self) -> None:
        version = "2026-08-07"
        api = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "auth_handlers.go").read_text(encoding="utf-8")
        frontend = (ROOT / "apps" / "display-web" / "components" / "close-account-form.tsx").read_text(encoding="utf-8")
        email_e2e = (ROOT / "scripts" / "release_a_email_e2e.py").read_text(encoding="utf-8")
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))

        self.assertIn(f'accountCloseNoticeVersion = "{version}"', api)
        self.assertIn(f'noticeVersion = "{version}"', frontend)
        self.assertIn(f'"notice_version": "{version}"', email_e2e)
        self.assertEqual(spec["components"]["schemas"]["CloseAccountRequest"]["properties"]["notice_version"]["const"], version)
        self.assertIn("部分操作记录、审计记录和隐私说明约定的信息会继续保存", frontend)

    def test_record_timestamps_use_shared_second_precision_formatters(self) -> None:
        for app in ("display-web", "ops-web"):
            formatter = (ROOT / "apps" / app / "lib" / "date-time.ts").read_text(encoding="utf-8")
            self.assertIn('timeStyle: "medium"', formatter)
            self.assertIn('timeZone: "Asia/Shanghai"', formatter)

            application = ROOT / "apps" / app
            timestamp_sources = "\n".join(
                path.read_text(encoding="utf-8")
                for path in application.rglob("*.tsx")
            )
            self.assertNotIn("toLocaleString", timestamp_sources)
            self.assertNotIn('timeStyle: "short"', timestamp_sources)

        history = (ROOT / "apps" / "display-web" / "app" / "account" / "history" / "page.tsx").read_text(encoding="utf-8")
        review = (ROOT / "apps" / "ops-web" / "components" / "review-queue.tsx").read_text(encoding="utf-8")
        self.assertIn('dateTime={entry.viewed_at}', history)
        self.assertIn('dateTime={task.created_at}', review)

    def test_operations_dashboard_exposes_release_a_operational_state(self) -> None:
        dashboard = (ROOT / "apps" / "ops-web" / "app" / "page.tsx").read_text(encoding="utf-8")
        for field in (
            "pending_outbox_events",
            "media_bytes",
            "verified_backup_age_seconds",
            "media_reconcile_age_seconds",
            "media_reconcile_issues",
            "failed_media_reconciles",
            "media_inspection_age_seconds",
            "media_publication_issues",
            "default_image_failures",
            "failed_media_inspections",
        ):
            self.assertIn(field, dashboard)
        self.assertIn('user.role !== "editor"', dashboard)
        self.assertIn("停止新图片上传", dashboard)

        alerts = (ROOT / "infra" / "monitoring" / "alerts.yml").read_text(encoding="utf-8")
        for name, threshold in (
            ("PlatformMediaStorageReview", "7516192768"),
            ("PlatformMediaStorageConstrained", "9126805504"),
            ("PlatformMediaStorageCritical", "10200547328"),
        ):
            self.assertIn(name, alerts)
            self.assertIn(threshold, alerts)

    def test_performer_aliases_are_editable_searchable_and_public(self) -> None:
        handler = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "operations_handlers.go").read_text(encoding="utf-8")
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "operations_store.go").read_text(encoding="utf-8")
        public_store = (ROOT / "services" / "platform-api" / "internal" / "database" / "store.go").read_text(encoding="utf-8")
        create_form = (ROOT / "apps" / "ops-web" / "components" / "new-performer-form.tsx").read_text(encoding="utf-8")
        revision_form = (ROOT / "apps" / "ops-web" / "components" / "revision-editor.tsx").read_text(encoding="utf-8")
        detail = (ROOT / "apps" / "display-web" / "app" / "performers" / "[slug]" / "page.tsx").read_text(encoding="utf-8")

        # Keep this contract resilient to gofmt's alignment changes while
        # still requiring the public aliases field and its JSON name.
        self.assertIn("Aliases", handler)
        self.assertIn("[]string", handler)
        self.assertIn('json:"aliases"', handler)
        self.assertIn("normalizePerformerAliases", handler)
        self.assertIn("replacePerformerAliases", store)
        self.assertIn("platform.performer_aliases", store)
        self.assertIn("queryPerformerAliases", public_store)
        self.assertIn('name="aliases"', create_form)
        self.assertIn('name="aliases"', revision_form)
        self.assertIn("performer.aliases", detail)

        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        self.assertIn("aliases", spec["components"]["schemas"]["CreatePerformerRequest"]["properties"])
        self.assertIn("aliases", spec["components"]["schemas"]["PerformerDetailResponse"]["required"])
        self.assertIn("discovery", spec["components"]["schemas"]["HomeSection"]["properties"]["key"]["enum"])

    def test_standalone_frontends_use_their_generated_server_and_distinct_ports(self) -> None:
        display_package = (ROOT / "apps" / "display-web" / "package.json").read_text(encoding="utf-8")
        display_dockerfile = (ROOT / "apps" / "display-web" / "Dockerfile").read_text(encoding="utf-8")
        ops_package = (ROOT / "apps" / "ops-web" / "package.json").read_text(encoding="utf-8")
        ops_dockerfile = (ROOT / "apps" / "ops-web" / "Dockerfile").read_text(encoding="utf-8")

        self.assertIn('"start": "node ../../scripts/start-next-standalone.mjs .next/standalone/apps/display-web/server.js 3000"', display_package)
        self.assertIn('ENV PORT=3000', display_dockerfile)
        self.assertIn('CMD ["node", "apps/display-web/server.js"]', display_dockerfile)
        self.assertIn('"start": "node ../../scripts/start-next-standalone.mjs .next/standalone/apps/ops-web/server.js 3001"', ops_package)
        self.assertIn('ENV PORT=3001', ops_dockerfile)
        self.assertIn('CMD ["node", "apps/ops-web/server.js"]', ops_dockerfile)

    def test_local_mailpit_only_exposes_web_and_smtp_on_loopback(self) -> None:
        compose = (ROOT / "compose.yaml").read_text(encoding="utf-8")
        self.assertIn('"127.0.0.1:1025:1025"', compose)
        self.assertIn('"127.0.0.1:8025:8025"', compose)
        self.assertNotIn('"0.0.0.0:1025:1025"', compose)

    def test_home_renders_real_performer_and_trending_sections(self) -> None:
        page = (ROOT / "apps" / "display-web" / "app" / "page.tsx").read_text(encoding="utf-8")
        self.assertIn('section.key === "performers"', page)
        self.assertIn('section.key === "trending"', page)
        self.assertIn('section.key === "most_viewed"', page)
        self.assertIn('section.key === "editorial"', page)
        self.assertIn('section.key === "discovery"', page)
        self.assertIn("<PerformerCard", page)
        self.assertIn("<PersonalizedWorkGrid", page)
        self.assertNotIn("统计将在详情页请求上报接入后显示", page)
        self.assertNotIn("人物资料将在人工审核发布后显示", page)

    def test_home_discovery_mix_is_configurable_and_audited(self) -> None:
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "store.go").read_text(encoding="utf-8")
        routes = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "server.go").read_text(encoding="utf-8")
        handlers = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "handlers.go").read_text(encoding="utf-8")
        self.assertIn("DiscoveryMixRule", store)
        self.assertIn("mixDiscoveryItems", handlers)
        self.assertIn('Key: "discovery"', handlers)
        self.assertIn('router.Put("/admin/v1/site-settings/discovery-mix"', routes)
        site_store = (ROOT / "services" / "platform-api" / "internal" / "database" / "site_rules_store.go").read_text(encoding="utf-8")
        self.assertIn('"site_content_rule.update"', site_store)
        self.assertIn("'cache_purge'", site_store)
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        self.assertIn("put", spec["paths"]["/admin/v1/site-settings/discovery-mix"])

    def test_editorial_recommendations_are_published_only_and_audited(self) -> None:
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        paths = spec["paths"]
        self.assertIn("get", paths["/api/v1/site/editorial"])
        self.assertIn("post", paths["/admin/v1/editorial-recommendations"])
        self.assertIn("put", paths["/admin/v1/editorial-recommendations/{recommendation_id}"])
        self.assertIn("delete", paths["/admin/v1/editorial-recommendations/{recommendation_id}"])

        public_store = (ROOT / "services" / "platform-api" / "internal" / "database" / "store.go").read_text(encoding="utf-8")
        operations_store = (ROOT / "services" / "platform-api" / "internal" / "database" / "editorial_store.go").read_text(encoding="utf-8")
        handlers = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "editorial_handlers.go").read_text(encoding="utf-8")
        migration = (ROOT / "db" / "migrations" / "00011_editorial_recommendations.sql").read_text(encoding="utf-8")
        self.assertIn("JOIN platform.public_published_works", public_store)
        self.assertIn("requirePublishedWork", operations_store)
        self.assertIn('"editorial_recommendation.create"', operations_store)
        self.assertIn('"editorial_recommendation.update"', operations_store)
        self.assertIn('"editorial_recommendation.remove"', operations_store)
        self.assertIn("enqueueEditorialCacheInvalidation", operations_store)
        self.assertIn("'cache_purge'", operations_store)
        self.assertIn('requireAdminRole(w, r, "admin", "owner")', handlers)
        self.assertNotIn("DELETE FROM platform.editorial_recommendations", operations_store)
        self.assertIn("status IN ('active', 'paused', 'removed')", migration)

        alerts = (ROOT / "infra" / "monitoring" / "alerts.yml").read_text(encoding="utf-8")
        self.assertIn("self_deepsearch_schema_version != 21", alerts)
        self.assertNotIn("self_deepsearch_schema_version != 10", alerts)

    def test_hidden_work_contract_is_private_and_complete(self) -> None:
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        paths = spec["paths"]
        self.assertIn("get", paths["/api/v1/me/hidden-works"])
        self.assertIn("post", paths["/api/v1/me/hidden-works/{work_id}"])
        self.assertIn("delete", paths["/api/v1/me/hidden-works/{work_id}"])

        routes = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "server.go").read_text(encoding="utf-8")
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "account_store.go").read_text(encoding="utf-8")
        client = (ROOT / "apps" / "display-web" / "components" / "personalized-work-grid.tsx").read_text(encoding="utf-8")
        self.assertIn('router.Get("/api/v1/me/hidden-works"', routes)
        self.assertIn("platform.hidden_preferences", store)
        self.assertIn("preference.status = 'active'", store)
        self.assertIn('cache: "no-store"', client)
        self.assertIn('credentials: "same-origin"', client)

    def test_latest_and_search_follow_confirmed_product_semantics(self) -> None:
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "store.go").read_text(encoding="utf-8")
        publication = (ROOT / "services" / "platform-api" / "internal" / "database" / "operations_store.go").read_text(encoding="utf-8")
        handler = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "handlers.go").read_text(encoding="utf-8")
        client = (ROOT / "apps" / "display-web" / "lib" / "api.ts").read_text(encoding="utf-8")
        self.assertIn("WHERE work.release_date IS NOT NULL", store)
        self.assertIn("JOIN platform.performer_aliases", publication)
        self.assertIn("normalizeSearchQuery", handler)
        self.assertIn("character - 0xfee0", handler)
        search_client = client.split("export async function searchWorks", 1)[1].split("export async function getPopularWorks", 1)[0]
        self.assertIn("fetchPlatformNoStore<WorkSearchResponse>", search_client)
        self.assertIn("async function fetchPlatformNoStore", client)
        no_store_helper = client.split("async function fetchPlatformNoStore", 1)[1].split("function platformRequest", 1)[0]
        self.assertIn('cache: "no-store"', no_store_helper)

        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        popular = (ROOT / "apps" / "display-web" / "app" / "popular" / "page.tsx").read_text(encoding="utf-8")
        self.assertIn("/api/v1/works/popular", spec["paths"])
        self.assertIn('{ key: "7d"', popular)
        self.assertIn('{ key: "30d"', popular)
        self.assertIn('{ key: "all"', popular)
        recent = (ROOT / "apps" / "display-web" / "app" / "recent" / "page.tsx").read_text(encoding="utf-8")
        self.assertIn("/api/v1/works/recent", spec["paths"])
        self.assertIn("getRecentlyAddedWorks", recent)

    def test_media_display_slots_are_enforced_across_public_and_admin_reads(self) -> None:
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "store.go").read_text(encoding="utf-8")
        account_store = (ROOT / "services" / "platform-api" / "internal" / "database" / "account_store.go").read_text(encoding="utf-8")
        primary_store = (ROOT / "services" / "platform-api" / "internal" / "database" / "media_primary.go").read_text(encoding="utf-8")
        page = (ROOT / "apps" / "display-web" / "app" / "works" / "[slug]" / "page.tsx").read_text(encoding="utf-8")
        self.assertIn("WITH eligible_media AS", store)
        self.assertIn("selected_assets AS (", store)
        self.assertIn("purpose = 'cover' AND is_primary AND position = 0", store)
        self.assertIn("purpose = 'gallery' AND NOT is_primary AND position BETWEEN 1 AND 3", store)
        self.assertIn("purpose = 'avatar' AND is_primary AND position = 0", store)
        self.assertIn("EXISTS (SELECT 1 FROM ranked_assets AS primary_asset WHERE primary_asset.is_primary)", store)
        self.assertIn("LIMIT 4", store)
        self.assertGreaterEqual(store.count("AND purpose = 'cover'"), 1)
        self.assertGreaterEqual(store.count("AND purpose = 'avatar'"), 1)
        self.assertGreaterEqual(account_store.count("purpose = 'cover' AND is_primary AND position = 0"), 1)
        self.assertGreaterEqual(account_store.count("purpose = 'avatar' AND is_primary AND position = 0"), 2)
        self.assertIn("link.purpose = 'cover' AND link.position = 0", primary_store)
        self.assertIn("link.purpose = 'avatar' AND link.position = 0", primary_store)
        self.assertIn("work.images.slice(0, 4)", page)
        self.assertIn("images.slice(1)", page)
        self.assertNotIn("work.images.slice(0, 3)", page)

    def test_public_images_use_api_provided_responsive_renditions(self) -> None:
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "store.go").read_text(encoding="utf-8")
        safe_image = (ROOT / "apps" / "display-web" / "components" / "safe-image.tsx").read_text(encoding="utf-8")
        work_card = (ROOT / "apps" / "display-web" / "components" / "work-card.tsx").read_text(encoding="utf-8")
        performer_card = (ROOT / "apps" / "display-web" / "components" / "performer-card.tsx").read_text(encoding="utf-8")
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))

        self.assertIn("jsonb_agg", store)
        self.assertIn("imageFromRenditions", store)
        self.assertIn("srcSet={srcSet}", safe_image)
        self.assertIn("sizes={srcSet ? sizes : undefined}", safe_image)
        self.assertNotIn("replace(\"w640\"", safe_image)
        self.assertIn("renditions={image?.renditions}", work_card)
        self.assertIn("renditions={image?.renditions}", performer_card)
        responsive = spec["components"]["schemas"]["ResponsiveImage"]
        self.assertEqual(responsive["allOf"][1]["properties"]["renditions"]["maxItems"], 3)

    def test_work_detail_recommends_only_other_published_works(self) -> None:
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "store.go").read_text(encoding="utf-8")
        handlers = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "handlers.go").read_text(encoding="utf-8")
        page = (ROOT / "apps" / "display-web" / "app" / "works" / "[slug]" / "page.tsx").read_text(encoding="utf-8")
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))

        self.assertIn("func queryRelatedWorks", store)
        self.assertIn("FROM platform.public_published_works AS work", store)
        self.assertIn("work.work_id <> source.work_id", store)
        self.assertIn("queryRelatedWorks(ctx, tx, detail.ID, 8)", store)
        self.assertIn("RelatedWorks: workItems(detail.RelatedWorks)", handlers)
        self.assertIn("work.related_works", page)
        related = spec["components"]["schemas"]["WorkDetailResponse"]["properties"]["related_works"]
        self.assertEqual(related["maxItems"], 8)

    def test_sitemaps_are_split_by_public_entity_type(self) -> None:
        index = (ROOT / "apps" / "display-web" / "app" / "sitemap.xml" / "route.ts").read_text(encoding="utf-8")
        entities = (ROOT / "apps" / "display-web" / "app" / "sitemaps" / "[entity]" / "route.ts").read_text(encoding="utf-8")
        handler = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "handlers.go").read_text(encoding="utf-8")
        self.assertIn('"works", "performers", "studios"', index)
        self.assertIn("getSitemapEntries(entityType)", entities)
        self.assertIn('oneOf(entityType, "all", "work", "performer", "studio")', handler)

    def test_public_detail_pages_emit_safe_json_ld(self) -> None:
        component = (ROOT / "apps" / "display-web" / "components" / "json-ld.tsx").read_text(encoding="utf-8")
        self.assertIn('type="application/ld+json"', component)
        self.assertIn('.replace(/</g, "\\\\u003c")', component)
        self.assertIn("dangerouslySetInnerHTML", component)

        expected_types = {
            "works": '"@type": "CreativeWork"',
            "performers": '"@type": "Person"',
            "studios": '"@type": "Organization"',
        }
        for directory, schema_type in expected_types.items():
            page = (ROOT / "apps" / "display-web" / "app" / directory / "[slug]" / "page.tsx").read_text(encoding="utf-8")
            self.assertIn("<JsonLD", page)
            self.assertIn(schema_type, page)
            self.assertIn("mainEntityOfPage", page)

    def test_public_lists_use_scoped_keyset_cursors(self) -> None:
        pagination = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "pagination.go").read_text(encoding="utf-8")
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "store.go").read_text(encoding="utf-8")
        self.assertIn("type pageCursor", pagination)
        self.assertIn("Scope", pagination)
        self.assertIn("After", pagination)
        self.assertNotIn("OFFSET", store)
        for page in ("search", "latest", "recent", "popular", "performers", "studios"):
            content = (ROOT / "apps" / "display-web" / "app" / page / "page.tsx").read_text(encoding="utf-8")
            self.assertIn("next_cursor", content)
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        self.assertIn("/api/v1/works/latest", spec["paths"])

    def test_public_cache_boundary_keeps_session_out_of_shared_html(self) -> None:
        layout = (ROOT / "apps" / "display-web" / "app" / "layout.tsx").read_text(encoding="utf-8")
        detail = (ROOT / "apps" / "display-web" / "app" / "works" / "[slug]" / "page.tsx").read_text(encoding="utf-8")
        header = (ROOT / "apps" / "display-web" / "components" / "site-header.tsx").read_text(encoding="utf-8")
        account_actions = (ROOT / "apps" / "display-web" / "components" / "account-actions.tsx").read_text(encoding="utf-8")
        work_actions = (ROOT / "apps" / "display-web" / "components" / "work-actions.tsx").read_text(encoding="utf-8")

        self.assertNotIn("getCurrentUser", layout)
        self.assertNotIn("getCurrentUser", detail)
        self.assertIn("<AccountActions />", header)
        self.assertIn("<WorkActions", detail)
        for client in (account_actions, work_actions):
            self.assertIn('fetch("/api/v1/me"', client)
            self.assertIn('credentials: "same-origin"', client)
            self.assertIn('cache: "no-store"', client)

        # Shared public HTML must expose the guest entry points immediately.
        # The private session lookup upgrades this UI after hydration, but an
        # API outage must not make login and registration navigation disappear.
        self.assertIn('if (session.status === "loading") return <GuestActions', account_actions)
        self.assertIn('if (session.status === "unavailable") return <GuestActions', account_actions)
        self.assertIn('href="/login"', account_actions)
        self.assertIn('href="/register"', account_actions)

    def test_turnstile_uses_runtime_site_key_and_never_silently_bypasses_production(self) -> None:
        config = (ROOT / "apps" / "display-web" / "lib" / "turnstile-config.ts").read_text(encoding="utf-8")
        field = (ROOT / "apps" / "display-web" / "components" / "turnstile-field.tsx").read_text(encoding="utf-8")
        register = (ROOT / "apps" / "display-web" / "app" / "register" / "page.tsx").read_text(encoding="utf-8")
        password = (ROOT / "apps" / "display-web" / "app" / "forgot-password" / "page.tsx").read_text(encoding="utf-8")
        register_form = (ROOT / "apps" / "display-web" / "components" / "register-form.tsx").read_text(encoding="utf-8")
        password_form = (ROOT / "apps" / "display-web" / "components" / "password-reset-form.tsx").read_text(encoding="utf-8")
        api_config = (ROOT / "services" / "platform-api" / "internal" / "config" / "config.go").read_text(encoding="utf-8")
        verifier = (ROOT / "services" / "platform-api" / "internal" / "security" / "turnstile.go").read_text(encoding="utf-8")

        self.assertIn('process.env.TURNSTILE_SITE_KEY', config)
        self.assertNotIn("NEXT_PUBLIC_TURNSTILE_SITE_KEY", field)
        self.assertIn('appEnv !== "production"', config)
        self.assertIn('allowDevelopmentBypass', field)
        self.assertIn("人机验证配置不可用", field)
        for page in (register, password):
            self.assertIn('dynamic = "force-dynamic"', page)
            self.assertIn("turnstilePublicConfig()", page)
        self.assertIn('TURNSTILE_SITE_KEY is required in production', api_config)
        self.assertIn('TURNSTILE_EXPECTED_HOSTNAME must be a plain DNS hostname in production', api_config)
        self.assertIn('action="signup_code"', register_form)
        self.assertIn('action="password_code"', password_form)
        self.assertIn('result.Action != action', verifier)
        self.assertIn('result.Hostname', verifier)

        local_env = (ROOT / ".env.example").read_text(encoding="utf-8")
        japan_env = (ROOT / "infra" / "compose" / "japan" / ".env.example").read_text(encoding="utf-8")
        compose = (ROOT / "compose.yaml").read_text(encoding="utf-8")
        self.assertIn("TURNSTILE_BYPASS=true", local_env)
        self.assertIn("TURNSTILE_BYPASS=false", japan_env)
        self.assertIn("TURNSTILE_SITE_KEY=replace-with-turnstile-site-key", japan_env)
        self.assertIn("TURNSTILE_EXPECTED_HOSTNAME=display.example.invalid", japan_env)
        self.assertIn("TURNSTILE_SITE_KEY: ${TURNSTILE_SITE_KEY:-}", compose)
        self.assertIn("TURNSTILE_SECRET_KEY: ${TURNSTILE_SECRET_KEY:-}", compose)
        self.assertIn("TURNSTILE_EXPECTED_HOSTNAME: ${TURNSTILE_EXPECTED_HOSTNAME:-127.0.0.1}", compose)

    def test_public_catalog_and_site_identity_are_not_baked_from_build_time_fallbacks(self) -> None:
        api = (ROOT / "apps" / "display-web" / "lib" / "api.ts").read_text(encoding="utf-8")
        site = (ROOT / "apps" / "display-web" / "lib" / "site.ts").read_text(encoding="utf-8")
        layout = (ROOT / "apps" / "display-web" / "app" / "layout.tsx").read_text(encoding="utf-8")
        home = (ROOT / "apps" / "display-web" / "app" / "page.tsx").read_text(encoding="utf-8")
        robots = (ROOT / "apps" / "display-web" / "app" / "robots.ts").read_text(encoding="utf-8")
        sitemap = (ROOT / "apps" / "display-web" / "app" / "sitemap.xml" / "route.ts").read_text(encoding="utf-8")
        smoke = (ROOT / "scripts" / "release_a_frontend_smoke.mjs").read_text(encoding="utf-8")

        self.assertNotIn("fallbackHome", api)
        self.assertNotIn('code: "TEST-', api)
        self.assertNotIn("当前展示本地合成数据", api + home)
        self.assertIn("目录服务暂时不可用", home)
        for source in (home, robots, sitemap):
            self.assertIn('dynamic = "force-dynamic"', source)
        self.assertNotIn("metadataBase: siteURL()", layout)
        self.assertIn('process.env.NODE_ENV === "production"', site)
        self.assertIn("production ? undefined : process.env.NEXT_PUBLIC_SITE_URL", site)
        self.assertIn("SITE_URL must be configured as an HTTP(S) origin in production", site)
        self.assertIn('extraEnv: { SITE_URL: displayOrigin, NEXT_PUBLIC_SITE_URL: displayOrigin }', smoke)
        self.assertIn('NEXT_PUBLIC_SITE_URL: "https://build-time.invalid"', smoke)
        self.assertIn("expected HTTP 500", smoke)

    def test_work_csv_error_download_neutralizes_spreadsheet_formulas(self) -> None:
        component = (ROOT / "apps" / "ops-web" / "components" / "work-csv-preflight.tsx").read_text(encoding="utf-8")
        exporter = (ROOT / "apps" / "ops-web" / "lib" / "work-csv-errors.mjs").read_text(encoding="utf-8")
        declarations = (ROOT / "apps" / "ops-web" / "lib" / "work-csv-errors.d.mts").read_text(encoding="utf-8")
        package = json.loads((ROOT / "package.json").read_text(encoding="utf-8"))

        self.assertIn("workImportErrorCSV(report)", component)
        self.assertNotIn("function csvCell", component)
        self.assertIn("spreadsheetFormulaPrefix", exporter)
        self.assertIn("[=+\\-@]", exporter)
        self.assertIn("? `'${value}` : value", exporter)
        self.assertIn("WorkCSVPreflightResponse", declarations)
        self.assertIn("work_csv_errors.test.mjs", package["scripts"]["test:frontend:smoke"])

    def test_production_compose_uses_service_scoped_environment_variables(self) -> None:
        japan = (ROOT / "infra" / "compose" / "japan" / "compose.yaml").read_text(encoding="utf-8")
        beijing = (ROOT / "infra" / "compose" / "beijing" / "compose.yaml").read_text(encoding="utf-8")

        self.assertNotIn("env_file:", japan)
        self.assertNotIn("env_file:", beijing)
        self.assertIn("TURNSTILE_SITE_KEY: ${TURNSTILE_SITE_KEY:?set TURNSTILE_SITE_KEY}", japan)
        self.assertIn("TURNSTILE_SECRET_KEY: ${TURNSTILE_SECRET_KEY:?set TURNSTILE_SECRET_KEY}", japan)
        self.assertIn("TURNSTILE_EXPECTED_HOSTNAME: ${TURNSTILE_EXPECTED_HOSTNAME:?set TURNSTILE_EXPECTED_HOSTNAME}", japan)
        self.assertIn("S3_PRIVATE_BUCKET: ${S3_PRIVATE_BUCKET:?set S3_PRIVATE_BUCKET}", beijing)
        self.assertIn("AWS_SECRET_ACCESS_KEY: ${AWS_SECRET_ACCESS_KEY:?set AWS_SECRET_ACCESS_KEY}", beijing)
        self.assertNotIn("PLATFORM_API_DATABASE_URL", beijing)

        japan_services = yaml.safe_load(japan)["services"]
        beijing_services = yaml.safe_load(beijing)["services"]
        self.assertEqual(
            set(japan_services["display-web"]["environment"]),
            {
                "APP_ENV", "CACHE_HMAC_SECRET", "NEXT_PUBLIC_SITE_URL", "PLATFORM_API_URL",
                "RIGHTS_CONTACT_EMAIL", "SITE_URL", "TURNSTILE_BYPASS", "TURNSTILE_SITE_KEY", "MEDIA_DELIVERY_MODE",
                "MEDIA_DELIVERY_DYNAMIC_MODE",
            },
        )
        self.assertEqual(set(japan_services["ops-web"]["environment"]), {"APP_ENV", "PLATFORM_API_URL"})
        self.assertEqual(
            set(japan_services["postgres"]["environment"]),
            {"POSTGRES_DB", "POSTGRES_PASSWORD", "POSTGRES_USER"},
        )
        self.assertNotIn("AUTH_HMAC_SECRET", japan_services["platform-worker-japan"]["environment"])
        self.assertNotIn("SMTP_URL", japan_services["display-web"]["environment"])
        self.assertNotIn("DATABASE_URL", japan_services["display-web"]["environment"])
        self.assertNotIn("TURNSTILE_SECRET_KEY", japan_services["display-web"]["environment"])
        self.assertNotIn("DATABASE_URL", beijing_services["platform-worker-beijing"]["environment"])
        self.assertNotIn("AWS_SECRET_ACCESS_KEY", beijing_services["platform-worker-beijing"]["environment"])
        self.assertNotIn("INGEST_API_URL", beijing_services["media-python"]["environment"])

    def test_production_compose_variables_are_documented_in_env_examples(self) -> None:
        deployments = (
            ("japan", ROOT / "infra" / "compose" / "japan" / "compose.yaml", ROOT / "infra" / "compose" / "japan" / ".env.example"),
            ("beijing", ROOT / "infra" / "compose" / "beijing" / "compose.yaml", ROOT / "infra" / "compose" / "beijing" / ".env.example"),
        )

        for deployment, compose_path, env_path in deployments:
            with self.subTest(deployment=deployment):
                compose = compose_path.read_text(encoding="utf-8")
                referenced = set(re.findall(r"\$\{([A-Z][A-Z0-9_]*)[^}]*\}", compose))
                keys = [
                    line.split("=", 1)[0]
                    for line in env_path.read_text(encoding="utf-8").splitlines()
                    if re.match(r"^[A-Z][A-Z0-9_]*=", line)
                ]

                self.assertEqual(len(keys), len(set(keys)), f"{deployment} .env.example contains duplicate keys")
                self.assertEqual(
                    referenced - set(keys),
                    set(),
                    f"{deployment} .env.example is missing Compose variables",
                )

    def test_documented_operations_deployment_matches_japan_compose(self) -> None:
        architecture = (ROOT / "ARCHITECTURE.md").read_text(encoding="utf-8")
        platform_architecture = (ROOT / "PLATFORM_ARCHITECTURE.md").read_text(encoding="utf-8")
        implementation = (ROOT / "IMPLEMENTATION_PLAN.md").read_text(encoding="utf-8")
        japan = yaml.safe_load((ROOT / "infra" / "compose" / "japan" / "compose.yaml").read_text(encoding="utf-8"))
        beijing = yaml.safe_load((ROOT / "infra" / "compose" / "beijing" / "compose.yaml").read_text(encoding="utf-8"))

        self.assertIn("ops-web", japan["services"])
        self.assertNotIn("ops-web", beijing["services"])
        self.assertIn("`ops-web` | 1 | Next.js + TypeScript | 日本", architecture)
        self.assertIn("Release A 将 `ops-web` 与 API 一起放在日本", architecture)
        self.assertIn("`ops.example.com` 与日本 `ops-web`、Go API 位于同一部署网络", platform_architecture)
        self.assertIn("不承载浏览器运营后台", implementation)
        for source in (architecture, platform_architecture, implementation):
            self.assertNotIn("北京反向代理转发到日本", source)
            self.assertNotIn("生产推荐把 `ops-web` 放在北京", source)

    def test_release_a_worker_fails_closed_when_collection_is_enabled(self) -> None:
        worker_config = (ROOT / "services" / "platform-worker" / "internal" / "config" / "config.go").read_text(encoding="utf-8")
        worker_main = (ROOT / "services" / "platform-worker" / "cmd" / "worker" / "main.go").read_text(encoding="utf-8")
        local_env = (ROOT / ".env.example").read_text(encoding="utf-8")
        local_compose = (ROOT / "compose.yaml").read_text(encoding="utf-8")
        japan_compose = (ROOT / "infra" / "compose" / "japan" / "compose.yaml").read_text(encoding="utf-8")
        beijing_compose = (ROOT / "infra" / "compose" / "beijing" / "compose.yaml").read_text(encoding="utf-8")
        go_live_record = (ROOT / "docs" / "RELEASE_A_GO_LIVE_RECORD.md").read_text(encoding="utf-8")

        self.assertIn('envOr("COLLECTION_ENABLED", "false")', worker_config)
        self.assertIn("if configuration.CollectionEnabled", worker_config)
        self.assertIn("COLLECTION_ENABLED must be false", worker_config)
        self.assertIn('"collection_enabled", configuration.CollectionEnabled', worker_main)
        self.assertIn("COLLECTION_ENABLED=false", local_env)
        for compose in (local_compose, japan_compose, beijing_compose):
            self.assertIn('COLLECTION_ENABLED: "false"', compose)
        self.assertIn("默认结论：**no-go", go_live_record)
        self.assertIn("自动采集（Release B）", go_live_record)
        self.assertIn("广告联盟脚本或主动广告行为（Release C）", go_live_record)
        self.assertIn("头像/人脸识别", go_live_record)
        self.assertIn("`COLLECTION_ENABLED=false`", go_live_record)
        self.assertIn("页面只有静态广告占位", go_live_record)
        self.assertIn("任一 P0 门槛未通过时保持 no-go", go_live_record)

    def test_review_queue_navigation_targets_the_dashboard_queue(self) -> None:
        shell = (ROOT / "apps" / "ops-web" / "components" / "admin-shell.tsx").read_text(encoding="utf-8")
        dashboard = (ROOT / "apps" / "ops-web" / "app" / "page.tsx").read_text(encoding="utf-8")
        self.assertIn('{ href: "/#review-title", label: "审核队列"', shell)
        self.assertIn('id="review-title"', dashboard)

    def test_invitation_contract_keeps_role_and_code_boundaries(self) -> None:
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        self.assertIn("/api/v1/auth/invitations/accept", spec["paths"])
        self.assertIn("/admin/v1/invitations", spec["paths"])
        self.assertIn("/admin/v1/invitations/{invitation_id}/revoke", spec["paths"])
        invitation = spec["components"]["schemas"]["Invitation"]
        self.assertEqual(invitation["properties"]["status"]["enum"], ["pending", "accepted", "revoked", "expired"])
        self.assertNotIn("code", invitation["properties"])
        identity = (ROOT / "services" / "platform-api" / "internal" / "identity" / "identity.go").read_text(encoding="utf-8")
        self.assertIn('actorRole == "admin" && role == "user"', identity)
        self.assertIn("ExpiresAt: now.Add(48 * time.Hour)", identity)
        page = (ROOT / "apps" / "display-web" / "app" / "invite" / "page.tsx").read_text(encoding="utf-8")
        self.assertIn("InviteAcceptForm", page)

    def test_api_mutation_responses_are_never_cacheable(self) -> None:
        server = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "server.go").read_text(encoding="utf-8")
        tests = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "server_test.go").read_text(encoding="utf-8")

        self.assertIn("r.Method != http.MethodGet && r.Method != http.MethodHead", server)
        self.assertIn('w.Header().Set("Cache-Control", "no-store")', server)
        self.assertIn("TestPublicReadDoesNotForcePrivateCachePolicy", tests)
        self.assertIn("mutation responses must not be cached", tests)

    def test_performer_follow_feature_has_a_complete_frontend_path(self) -> None:
        detail = (ROOT / "apps" / "display-web" / "app" / "performers" / "[slug]" / "page.tsx").read_text(encoding="utf-8")
        follows = (ROOT / "apps" / "display-web" / "app" / "account" / "follows" / "page.tsx").read_text(encoding="utf-8")
        button = (ROOT / "apps" / "display-web" / "components" / "follow-button.tsx").read_text(encoding="utf-8")

        self.assertIn("<PerformerActions", detail)
        self.assertIn("<PerformerCard", follows)
        self.assertNotIn("<WorkCard", follows)
        self.assertIn('fetch("/api/v1/me/follows"', button)
        self.assertIn("/api/v1/me/follows/${encodeURIComponent(performerID)}", button)

        home = (ROOT / "apps" / "display-web" / "app" / "page.tsx").read_text(encoding="utf-8")
        followed = (ROOT / "apps" / "display-web" / "components" / "followed-works-section.tsx").read_text(encoding="utf-8")
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "account_store.go").read_text(encoding="utf-8")
        self.assertIn("<FollowedWorksSection />", home)
        self.assertIn('fetch("/api/v1/me/followed-works"', followed)
        self.assertIn("platform.work_performers", store)
        self.assertIn("platform.hidden_preferences", store)

    def test_admin_can_hide_a_published_entity_from_the_catalog(self) -> None:
        catalog = (ROOT / "apps" / "ops-web" / "app" / "catalog" / "page.tsx").read_text(encoding="utf-8")
        control = (ROOT / "apps" / "ops-web" / "components" / "hide-entity-button.tsx").read_text(encoding="utf-8")

        self.assertIn('operator.role !== "editor" && item.status === "published"', catalog)
        self.assertIn("<HideEntityButton", catalog)
        self.assertIn("/admin/v1/entities/${encodeURIComponent(entity.id)}/hide", control)
        self.assertIn("填写隐藏理由", control)

    def test_admin_entity_merge_is_transactional_audited_and_redirected(self) -> None:
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        routes = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "server.go").read_text(encoding="utf-8")
        handler = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "merge_handlers.go").read_text(encoding="utf-8")
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "merge_store.go").read_text(encoding="utf-8")
        migration = (ROOT / "db" / "migrations" / "00012_entity_merges.sql").read_text(encoding="utf-8")
        catalog = (ROOT / "apps" / "ops-web" / "app" / "catalog" / "page.tsx").read_text(encoding="utf-8")

        self.assertIn("post", spec["paths"]["/admin/v1/entities/{entity_id}/merge"])
        self.assertIn('router.Post("/admin/v1/entities/{entityID}/merge"', routes)
        self.assertIn('requireAdminRole(w, r, "admin", "owner")', handler)
        self.assertIn("pg_advisory_xact_lock", store)
        self.assertIn("UPDATE platform.publications", store)
        self.assertIn("publication_status = 'hidden'", store)
        self.assertIn("INSERT INTO platform.redirects", store)
        self.assertIn("status_code = 301", store)
        self.assertIn('"entity.merge"', store)
        self.assertIn("'cache_purge'", store)
        self.assertIn("INSERT INTO platform.entity_merges", store)
        self.assertNotIn("DELETE FROM platform.works", store)
        self.assertNotIn("DELETE FROM platform.performers", store)
        self.assertNotIn("DELETE FROM platform.studios", store)
        self.assertIn("UNIQUE (entity_type, source_entity_id)", migration)
        self.assertIn("<MergeEntityButton", catalog)

    def test_feedback_has_a_manual_review_queue_and_audit_transition(self) -> None:
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        routes = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "server.go").read_text(encoding="utf-8")
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "account_store.go").read_text(encoding="utf-8")
        page = (ROOT / "apps" / "ops-web" / "app" / "feedback" / "page.tsx").read_text(encoding="utf-8")

        self.assertIn("/admin/v1/feedback", spec["paths"])
        self.assertIn("/admin/v1/feedback/{feedback_id}/review", spec["paths"])
        self.assertIn('router.Get("/admin/v1/feedback"', routes)
        self.assertIn('router.Post("/admin/v1/feedback/{feedbackID}/review"', routes)
        self.assertIn('insertAudit(ctx, tx, reviewerID, "feedback.review"', store)
        self.assertIn("证据链接仅展示，不由后台自动访问", page)

    def test_admin_user_management_revokes_sessions_without_bypassing_verification(self) -> None:
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "operations_store.go").read_text(encoding="utf-8")
        manager = (ROOT / "apps" / "ops-web" / "components" / "user-manager.tsx").read_text(encoding="utf-8")

        self.assertIn("/admin/v1/users/{user_id}/status", spec["paths"])
        self.assertIn("previousStatus != \"active\"", store)
        self.assertIn("UPDATE platform.sessions SET revoked_at", store)
        self.assertIn('insertAudit(ctx, tx, actorID, "user.status_change"', store)
        self.assertIn('operator.role === "admin" && user.role === "user"', manager)

    def test_frontend_navigation_and_private_pages_expose_accessible_failure_states(self) -> None:
        main_nav = (ROOT / "apps" / "display-web" / "components" / "main-nav.tsx").read_text(encoding="utf-8")
        header = (ROOT / "apps" / "display-web" / "components" / "site-header.tsx").read_text(encoding="utf-8")
        self.assertIn("usePathname", main_nav)
        self.assertIn('aria-current={active ? "page" : undefined}', main_nav)
        self.assertIn("<MainNav />", header)

        shell = (ROOT / "apps" / "ops-web" / "components" / "admin-shell.tsx").read_text(encoding="utf-8")
        review = (ROOT / "apps" / "ops-web" / "components" / "review-queue.tsx").read_text(encoding="utf-8")
        feedback = (ROOT / "apps" / "ops-web" / "components" / "feedback-review-queue.tsx").read_text(encoding="utf-8")
        self.assertIn("aria-label={label}", shell)
        self.assertIn('aria-label="领取任务"', review)
        self.assertIn('aria-label="开始处理"', feedback)

        for page, phrase in (
            ("favorites", "收藏服务暂时不可用"),
            ("follows", "关注服务暂时不可用"),
            ("hidden", "隐藏偏好暂时不可用"),
            ("history", "浏览历史暂时不可用"),
            ("feedback", "反馈记录暂时不可用"),
        ):
            content = (ROOT / "apps" / "display-web" / "app" / "account" / page / "page.tsx").read_text(encoding="utf-8")
            self.assertIn("data === null", content)
            self.assertIn(phrase, content)

        clear_history = (ROOT / "apps" / "display-web" / "components" / "clear-history-button.tsx").read_text(encoding="utf-8")
        self.assertIn("catch (cause)", clear_history)
        self.assertIn('role="alert"', clear_history)

    def test_frontend_review_session_challenge_and_disclosure_fail_closed(self) -> None:
        review_page = (ROOT / "apps" / "ops-web" / "app" / "catalog" / "[entityType]" / "[entityID]" / "page.tsx").read_text(encoding="utf-8")
        review_actions = (ROOT / "apps" / "ops-web" / "components" / "review-task-actions.tsx").read_text(encoding="utf-8")
        self.assertIn('revision.status === "reviewing"', review_page)
        self.assertIn("reviewHasSources", review_page)
        self.assertIn("canInspectRevision={reviewRevisionAvailable}", review_page)
        self.assertIn('disabled={busy || !canInspectRevision || !canApprove}', review_actions)
        self.assertIn("没有来源证据，不能通过", review_actions)

        for component in ("account-actions", "work-actions", "performer-actions"):
            content = (ROOT / "apps" / "display-web" / "components" / f"{component}.tsx").read_text(encoding="utf-8")
            self.assertIn('response.status === 401 || response.status === 403', content)
            self.assertIn('status: "unavailable"', content)
        layout = (ROOT / "apps" / "display-web" / "app" / "layout.tsx").read_text(encoding="utf-8")
        self.assertNotIn("getCurrentUser", layout)

        turnstile = (ROOT / "apps" / "display-web" / "components" / "turnstile-field.tsx").read_text(encoding="utf-8")
        for callback in ('"error-callback"', '"expired-callback"', '"timeout-callback"'):
            self.assertIn(callback, turnstile)
        self.assertIn("function retry()", turnstile)
        self.assertIn("onError", turnstile)

        footer = (ROOT / "apps" / "display-web" / "components" / "site-footer.tsx").read_text(encoding="utf-8")
        self.assertIn("隶属、授权、推荐或背书关系", footer)
        self.assertIn('endsWith(".invalid")', footer)
        self.assertNotIn('"rights@example.invalid"', footer)

    def test_page_view_reporter_deduplicates_effect_replay_without_time_window(self) -> None:
        reporter = (ROOT / "apps" / "display-web" / "components" / "page-view-reporter.tsx").read_text(encoding="utf-8")
        self.assertIn('import { useEffect, useRef } from "react"', reporter)
        self.assertIn("const reportedKey = useRef<string | null>(null)", reporter)
        self.assertIn("if (reportedKey.current === key) return", reporter)
        self.assertIn("reportedKey.current = key", reporter)
        self.assertNotIn("STRICT_MODE_DEDUPE_WINDOW_MS", reporter)
        self.assertNotIn("recentReports", reporter)
        self.assertNotIn("Date.now()", reporter)
        self.assertNotIn("setTimeout", reporter)
        self.assertIn('"/api/v1/metrics/page-view"', reporter)

    def test_review_task_reassignment_is_explicit_audited_and_reauthenticated(self) -> None:
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        reassignment = spec["paths"]["/admin/v1/review-tasks/{task_id}/reassign"]["post"]
        self.assertEqual(reassignment["security"], [{"sessionCookie": [], "recentAuthCookie": []}])
        self.assertEqual(
            reassignment["requestBody"]["content"]["application/json"]["schema"]["$ref"],
            "#/components/schemas/ReviewReassignmentRequest",
        )

        server = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "server.go").read_text(encoding="utf-8")
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "operations_store.go").read_text(encoding="utf-8")
        page = (ROOT / "apps" / "ops-web" / "app" / "catalog" / "[entityType]" / "[entityID]" / "page.tsx").read_text(encoding="utf-8")
        actions = (ROOT / "apps" / "ops-web" / "components" / "review-task-actions.tsx").read_text(encoding="utf-8")
        self.assertIn('router.Post("/admin/v1/review-tasks/{taskID}/reassign"', server)
        self.assertIn('strings.HasSuffix(path, "/reassign")', server)
        self.assertIn('"review_task.reassign"', store)
        self.assertIn('"assignee_id": *previousAssigneeID', store)
        self.assertIn('"assignee_id": assigneeID', store)
        self.assertIn("getOperationsUsers()", page)
        self.assertIn('action === "reassign"', actions)
        self.assertIn("adminFetch", actions)

    def test_audit_lookup_is_exact_bounded_and_minimally_disclosed(self) -> None:
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        operation = spec["paths"]["/admin/v1/audit-logs"]["get"]
        self.assertEqual(operation["security"], [{"sessionCookie": []}])
        request_id = next(parameter for parameter in operation["parameters"] if parameter["name"] == "request_id")
        self.assertTrue(request_id["required"])
        self.assertEqual(request_id["schema"]["maxLength"], 128)
        self.assertEqual(request_id["schema"]["pattern"], "^[A-Za-z0-9._:-]+$")

        properties = spec["components"]["schemas"]["AuditLog"]["properties"]
        self.assertNotIn("before_version", properties)
        self.assertNotIn("after_version", properties)
        self.assertNotIn("metadata", properties)
        self.assertEqual(spec["components"]["schemas"]["AuditLogListResponse"]["properties"]["items"]["maxItems"], 200)

        server = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "server.go").read_text(encoding="utf-8")
        handler = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "operations_handlers.go").read_text(encoding="utf-8")
        page = (ROOT / "apps" / "ops-web" / "app" / "audit" / "page.tsx").read_text(encoding="utf-8")
        navigation = (ROOT / "apps" / "ops-web" / "components" / "admin-shell.tsx").read_text(encoding="utf-8")
        mock_upstream = (ROOT / "scripts" / "release_a_frontend_smoke.mjs").read_text(encoding="utf-8")
        self.assertIn('router.Get("/admin/v1/audit-logs"', server)
        self.assertIn('requireAdminRole(w, r, "admin", "owner")', handler)
        self.assertIn("ListAuditLogs(r.Context(), requestID, 201)", handler)
        self.assertIn("不返回 before/after 原始载荷或 metadata", page)
        self.assertIn('roles: ["admin", "owner"]', navigation)
        self.assertIn('url.pathname === "/admin/v1/audit-logs"', mock_upstream)


if __name__ == "__main__":
    unittest.main()
