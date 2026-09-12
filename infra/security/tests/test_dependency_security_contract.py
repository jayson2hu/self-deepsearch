from __future__ import annotations

import json
import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


class DependencySecurityContractTests(unittest.TestCase):
    def test_nanoid_advisory_is_pinned_and_ci_blocks_high_production_findings(self) -> None:
        package = json.loads((ROOT / "package.json").read_text(encoding="utf-8"))
        lock = json.loads((ROOT / "package-lock.json").read_text(encoding="utf-8"))
        workflow = yaml.safe_load((ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8"))

        self.assertEqual(package["overrides"]["nanoid"], "3.3.18")
        self.assertEqual(lock["packages"]["node_modules/nanoid"]["version"], "3.3.18")
        self.assertEqual(package["scripts"]["audit:production"], "npm audit --omit=dev --audit-level=high")

        web_steps = workflow["jobs"]["web"]["steps"]
        commands = [step.get("run") for step in web_steps if isinstance(step, dict)]
        self.assertIn("npm run audit:production", commands)

    def test_next_and_sharp_versions_are_outside_known_advisory_ranges(self) -> None:
        package = json.loads((ROOT / "package.json").read_text(encoding="utf-8"))
        lock = json.loads((ROOT / "package-lock.json").read_text(encoding="utf-8"))
        for workspace in ("apps/display-web/package.json", "apps/ops-web/package.json"):
            dependencies = json.loads((ROOT / workspace).read_text(encoding="utf-8"))
            self.assertEqual(dependencies["dependencies"]["next"], "16.3.3")
            self.assertEqual(dependencies["dependencies"]["sharp"], "0.35.4")
            self.assertEqual(dependencies["devDependencies"]["eslint-config-next"], "16.3.3")

        self.assertEqual(lock["packages"]["apps/display-web/node_modules/next"]["version"], "16.3.3")
        self.assertEqual(lock["packages"]["apps/display-web/node_modules/sharp"]["version"], "0.35.4")
        self.assertEqual(lock["packages"]["apps/ops-web/node_modules/next"]["version"], "16.3.3")
        self.assertEqual(lock["packages"]["apps/ops-web/node_modules/sharp"]["version"], "0.35.4")
        self.assertEqual(package["scripts"]["audit:production"], "npm audit --omit=dev --audit-level=high")

    def test_frontend_images_install_the_locked_dependency_tree(self) -> None:
        for dockerfile in ("apps/display-web/Dockerfile", "apps/ops-web/Dockerfile"):
            source = (ROOT / dockerfile).read_text(encoding="utf-8")
            self.assertIn("COPY package.json package-lock.json ./", source)
            self.assertIn("RUN npm ci", source)
            self.assertNotIn("RUN npm install", source)

        dockerignore = (ROOT / ".dockerignore").read_text(encoding="utf-8").splitlines()
        for excluded in (
            ".git",
            ".env",
            ".env.*",
            "**/.env",
            "**/.env.*",
            "*.key",
            "*.pem",
            "*.crt",
            "**/node_modules",
            ".venv",
            "runtime",
            "spool",
            "backups",
            "infra/monitoring/dev-metrics-token.txt",
            "infra/monitoring/alertmanager.yml",
            "infra/monitoring/alertmanager_smtp_password",
        ):
            self.assertIn(excluded, dockerignore)

        dockerfiles = (
            "services/platform-api/Dockerfile",
            "services/platform-worker/Dockerfile",
            "apps/display-web/Dockerfile",
            "apps/ops-web/Dockerfile",
            "workers/media-python/Dockerfile",
        )
        for dockerfile in dockerfiles:
            source = (ROOT / dockerfile).read_text(encoding="utf-8")
            self.assertIn("org.opencontainers.image.version", source)
            self.assertIn("org.opencontainers.image.revision", source)

        api_image = (ROOT / "services/platform-api/Dockerfile").read_text(encoding="utf-8")
        worker_image = (ROOT / "services/platform-worker/Dockerfile").read_text(encoding="utf-8")
        self.assertIn("internal/config.DefaultBuildVersion=${BUILD_VERSION}", api_image)
        self.assertIn("internal/config.DefaultBuildVersion=${BUILD_VERSION}", worker_image)
        self.assertIn("-o /out/create-owner ./cmd/create-owner", api_image)
        self.assertIn("COPY --from=build /out/create-owner /create-owner", api_image)

        bake = (ROOT / "docker-bake.hcl").read_text(encoding="utf-8")
        for target in ("platform-api", "platform-worker", "display-web", "ops-web", "media-python"):
            self.assertIn(f'target "{target}"', bake)
        self.assertIn('group "release-a"', bake)

        workflow = yaml.safe_load((ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8"))
        image_job = workflow["jobs"]["images"]
        self.assertEqual(set(image_job["needs"]), {"go", "python", "migrations", "core-e2e", "web", "compose"})
        image_steps = image_job["steps"]
        image_commands = [step.get("run", "") for step in image_steps if isinstance(step, dict)]
        self.assertIn("docker buildx bake release-a --load", image_commands)
        self.assertTrue(any(step.get("uses") == "actions/upload-artifact@v4" for step in image_steps))

    def test_runtime_images_define_local_healthchecks(self) -> None:
        expected = {
            "services/platform-api/Dockerfile": '["/platform-api", "--healthcheck"]',
            "services/platform-worker/Dockerfile": '["/platform-worker", "--healthcheck"]',
            "apps/display-web/Dockerfile": 'http://127.0.0.1:3000/',
            "apps/ops-web/Dockerfile": 'http://127.0.0.1:3001/login',
            "workers/media-python/Dockerfile": 'http://127.0.0.1:8090/health/live',
        }
        for dockerfile, signal in expected.items():
            source = (ROOT / dockerfile).read_text(encoding="utf-8")
            self.assertIn("HEALTHCHECK --interval=30s", source)
            self.assertIn(signal, source)

        api_main = (ROOT / "services/platform-api/cmd/api/main.go").read_text(encoding="utf-8")
        worker_main = (ROOT / "services/platform-worker/cmd/worker/main.go").read_text(encoding="utf-8")
        self.assertIn('"http://127.0.0.1:8080/readyz"', api_main)
        self.assertIn('"http://127.0.0.1:8081/readyz"', worker_main)

    def test_frontends_and_edge_wait_for_healthy_dependencies(self) -> None:
        local = yaml.safe_load((ROOT / "compose.yaml").read_text(encoding="utf-8"))["services"]
        japan = yaml.safe_load((ROOT / "infra/compose/japan/compose.yaml").read_text(encoding="utf-8"))["services"]
        workflow = yaml.safe_load((ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8"))
        for services in (local, japan):
            self.assertEqual(services["display-web"]["depends_on"]["platform-api"]["condition"], "service_healthy")
            self.assertEqual(services["ops-web"]["depends_on"]["platform-api"]["condition"], "service_healthy")
            self.assertEqual(services["platform-worker-japan"]["depends_on"]["postgres"]["condition"], "service_healthy")
            self.assertEqual(services["prometheus"]["depends_on"]["platform-api"]["condition"], "service_healthy")
            self.assertEqual(services["prometheus"]["depends_on"]["platform-worker-japan"]["condition"], "service_healthy")
            self.assertEqual(services["prometheus"]["depends_on"]["alertmanager"]["condition"], "service_started")
            self.assertEqual(services["prometheus"]["depends_on"]["node-exporter"]["condition"], "service_started")
            self.assertEqual(services["alertmanager"]["profiles"], ["monitoring"])
            self.assertEqual(services["alertmanager"]["ports"], ["127.0.0.1:9093:9093"])
            self.assertEqual(services["alertmanager"]["user"], "65534:65534")
            self.assertIn("no-new-privileges:true", services["alertmanager"]["security_opt"])
            node_exporter = services["node-exporter"]
            self.assertEqual(node_exporter["profiles"], ["monitoring"])
            self.assertEqual(node_exporter["ports"], ["127.0.0.1:9100:9100"])
            self.assertEqual(node_exporter["user"], "65534:65534")
            self.assertTrue(node_exporter["read_only"])
            self.assertEqual(node_exporter["cap_drop"], ["ALL"])
            self.assertIn("no-new-privileges:true", node_exporter["security_opt"])
            self.assertIn("--collector.disable-defaults", node_exporter["command"])
            self.assertIn("--collector.cpu", node_exporter["command"])
            self.assertIn("--collector.meminfo", node_exporter["command"])
            self.assertIn("--collector.filesystem", node_exporter["command"])
            node_volumes = "\n".join(node_exporter["volumes"])
            self.assertIn("/proc:/host/proc:ro", node_volumes)
            self.assertIn("/sys:/host/sys:ro", node_volumes)
            self.assertIn("/:/host/root:ro,rslave", node_volumes)
        for dependency in ("platform-api", "display-web", "ops-web"):
            self.assertEqual(japan["edge"]["depends_on"][dependency]["condition"], "service_healthy")

        owner_bootstrap = japan["owner-bootstrap"]
        self.assertEqual(owner_bootstrap["profiles"], ["tools"])
        self.assertEqual(owner_bootstrap["image"], "${PLATFORM_API_IMAGE:?set PLATFORM_API_IMAGE}")
        self.assertEqual(owner_bootstrap["entrypoint"], ["/create-owner"])
        self.assertEqual(
            set(owner_bootstrap["environment"]),
            {"DATABASE_URL", "BOOTSTRAP_OWNER_EMAIL", "BOOTSTRAP_OWNER_PASSWORD"},
        )
        self.assertEqual(
            owner_bootstrap["depends_on"]["database-logins"]["condition"],
            "service_completed_successfully",
        )
        fixture_loader = japan["database-fixtures"]
        self.assertEqual(fixture_loader["profiles"], ["tools"])
        self.assertEqual(fixture_loader["entrypoint"], ["sh", "/database-tools/load_release_a_fixtures.sh"])
        self.assertEqual(
            set(fixture_loader["environment"]),
            {"ADMIN_DATABASE_URL", "ALLOW_SYNTHETIC_FIXTURES", "CONFIRM_SYNTHETIC_FIXTURES", "SEED_FILE"},
        )
        self.assertEqual(
            fixture_loader["depends_on"]["database-logins"]["condition"],
            "service_completed_successfully",
        )

        alertmanager = japan["alertmanager"]
        volumes = "\n".join(alertmanager["volumes"])
        self.assertIn("ALERTMANAGER_CONFIG_FILE", volumes)
        self.assertIn("ALERTMANAGER_SMTP_PASSWORD_FILE", volumes)
        self.assertNotIn("SMTP_URL", volumes)

        prometheus_config = (ROOT / "infra" / "monitoring" / "prometheus.yml").read_text(encoding="utf-8")
        alertmanager_example = (ROOT / "infra" / "monitoring" / "alertmanager.example.yml").read_text(
            encoding="utf-8"
        )
        self.assertIn("targets: [alertmanager:9093]", prometheus_config)
        self.assertIn("job_name: node-exporter", prometheus_config)
        self.assertIn("targets: [node-exporter:9100]", prometheus_config)
        self.assertIn("smtp_auth_password_file: /run/secrets/alertmanager_smtp_password", alertmanager_example)
        self.assertNotRegex(alertmanager_example, r"(?m)^\s*smtp_auth_password\s*:")
        self.assertIn('severity="critical"', alertmanager_example)
        self.assertIn('severity=~"warning|info"', alertmanager_example)
        self.assertGreaterEqual(alertmanager_example.count("send_resolved: true"), 2)

        compose_commands = [
            step.get("run", "") for step in workflow["jobs"]["compose"]["steps"] if isinstance(step, dict)
        ]
        japan_validation = next(command for command in compose_commands if "infra/compose/japan/compose.yaml" in command)
        self.assertIn(
            "--profile edge --profile monitoring --profile tools config --quiet",
            " ".join(japan_validation.split()),
        )

        workflow_source = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
        self.assertIn("branches: [main, master]", workflow_source)

    def test_ci_runs_release_a_playwright_browser_e2e(self) -> None:
        package = json.loads((ROOT / "package.json").read_text(encoding="utf-8"))
        workflow = yaml.safe_load((ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8"))
        browser_suite = (ROOT / "tests" / "e2e" / "release-a.spec.mjs").read_text(encoding="utf-8")

        self.assertIn("@playwright/test", package["devDependencies"])
        self.assertEqual(package["scripts"]["test:e2e:release-a"], "playwright test")
        commands = [step.get("run") for step in workflow["jobs"]["web"]["steps"] if isinstance(step, dict)]
        self.assertIn("npx playwright install --with-deps chromium", commands)
        self.assertIn("npm run test:e2e:release-a", commands)
        self.assertTrue(any("npm run capture:visual:release-a" in (command or "") for command in commands))
        self.assertTrue(any("--browser-channel chromium" in (command or "") for command in commands))
        self.assertTrue(any("npm run probe:performance:release-a" in (command or "") for command in commands))
        self.assertTrue(any("npm run probe:http-latency:release-a" in (command or "") for command in commands))
        web_steps = workflow["jobs"]["web"]["steps"]
        evidence_upload = next(
            step
            for step in web_steps
            if isinstance(step, dict) and step.get("name") == "Upload Release A web evidence"
        )
        self.assertEqual(evidence_upload["uses"], "actions/upload-artifact@v4")
        self.assertEqual(evidence_upload["if"], "always()")
        self.assertIn("release-a-visual-ci", evidence_upload["with"]["path"])
        self.assertIn("release-a-performance-ci.json", evidence_upload["with"]["path"])
        self.assertIn("release-a-http-latency-ci.json", evidence_upload["with"]["path"])
        self.assertIn("匿名用户可以按番号搜索并安全查看作品详情", browser_suite)
        self.assertIn("注册验证码流程建立会话，并可退出登录", browser_suite)
        self.assertIn("忘记密码必须完成邮箱验证码后才能重置", browser_suite)
        self.assertIn("关闭账号展示保留说明并要求邮箱验证码", browser_suite)
        self.assertIn("注册用户可以收藏关注隐藏、查看历史并提交反馈", browser_suite)
        self.assertIn("运营账号错误密码被拒绝，正确密码进入后台", browser_suite)
        self.assertIn("两个运营账号完成人工录入、审核、发布并公开搜索", browser_suite)
        self.assertIn("owner 邀请管理员并由受邀邮箱验证码建立后台账号", browser_suite)
        self.assertIn("owner 修改普通用户角色，admin 只能管理普通用户状态", browser_suite)
        self.assertIn("用户反馈由 editor 领取并由 admin 完成审核", browser_suite)
        self.assertIn("权利下架限制 editor 操作并从公开搜索撤下作品", browser_suite)
        mock_upstream = (ROOT / "scripts" / "release_a_frontend_smoke.mjs").read_text(encoding="utf-8")
        self.assertIn('/__test/reset-invitations', mock_upstream)
        self.assertIn('/__test/reset-user-management', mock_upstream)
        self.assertIn('/__test/reset-feedback', mock_upstream)
        self.assertIn('/__test/reset-takedowns', mock_upstream)
        self.assertIn('/api/v1/auth/invitations/accept', mock_upstream)
        self.assertIn('/admin/v1/invitations', mock_upstream)
        self.assertIn('/admin/v1/users/${MOCK_USER.id}/role', mock_upstream)
        self.assertIn('/admin/v1/users/${MOCK_USER.id}/status', mock_upstream)
        self.assertIn('/admin/v1/feedback/50000000-0000-4000-8000-000000000001/review', mock_upstream)
        self.assertIn('/admin/v1/takedowns/91000000-0000-4000-8000-000000000001/complete', mock_upstream)
        self.assertIn('const invitationRevokeMatch = url.pathname.match', mock_upstream)
        self.assertIn('invitation.status = "revoked"', mock_upstream)
        self.assertIn("撤销浏览器测试邀请", browser_suite)
        self.assertIn('/api/v1/auth/password/code', mock_upstream)
        self.assertIn('/api/v1/auth/password/reset', mock_upstream)
        self.assertIn('/api/v1/account/close/code', mock_upstream)
        self.assertIn('/api/v1/account/close', mock_upstream)
        for path in (
            '/api/v1/me/favorites',
            '/api/v1/me/follows',
            '/api/v1/me/hidden-works',
            '/api/v1/me/history',
            '/api/v1/feedback',
            '/api/v1/me/feedback',
        ):
            self.assertIn(path, mock_upstream)
        for path in (
            '/admin/v1/works',
            '/admin/v1/review-tasks',
            '/admin/v1/users',
            '/publish',
        ):
            self.assertIn(path, mock_upstream)
        ops_css = (ROOT / "apps" / "ops-web" / "app" / "globals.css").read_text(encoding="utf-8")
        self.assertIn("th:nth-child(n+4):not(:last-child), td:nth-child(n+4):not(:last-child)", ops_css)
        self.assertIn("th:last-child, td:last-child { display: table-cell", ops_css)

    def test_ci_runs_real_go_postgres_mailpit_flows_before_image_delivery(self) -> None:
        workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8"))
        job = workflow["jobs"]["core-e2e"]
        self.assertEqual(job["services"]["postgres"]["image"], "postgres:16-alpine")
        self.assertEqual(job["services"]["mailpit"]["image"], "axllent/mailpit:v1.27")
        self.assertEqual(job["services"]["postgres"]["env"]["POSTGRES_DB"], "self_deepsearch_core_e2e_test")
        self.assertEqual(job["env"]["CONFIRM_RELEASE_A_CORE_E2E"], "disposable-database")
        self.assertEqual(job["env"]["MIGRATION_DIR"], "db/migrations")
        self.assertIn("postgres://platform_api_login:", job["env"]["CORE_E2E_DATABASE_URL"])
        self.assertNotIn("continue-on-error", job)
        commands = "\n".join(step.get("run", "") for step in job["steps"])
        for command in (
            "sh infra/database/migrate.sh", "sh infra/database/provision_runtime_logins.sh",
            "go build -trimpath -o .cache/core-e2e/platform-api ./services/platform-api/cmd/api",
            "go build -trimpath -o .cache/core-e2e/create-owner ./services/platform-api/cmd/create-owner",
            "python scripts/release_a_core_e2e.py",
        ):
            self.assertIn(command, commands)
        self.assertNotIn("frontend_smoke", commands)
        upload = next(step for step in job["steps"] if step.get("uses") == "actions/upload-artifact@v4")
        self.assertEqual(upload["if"], "always()")
        self.assertEqual(upload["with"]["path"], "docs/evidence/release-a-core-e2e-ci.json")
        self.assertIn("core-e2e", workflow["jobs"]["images"]["needs"])

    def test_ci_enables_real_go_python_media_acknowledgement_contract(self) -> None:
        workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8"))
        job = workflow["jobs"]["go"]
        self.assertEqual(job["env"]["MEDIA_CONTRACT_PYTHON"], "python3")
        setup = next(step for step in job["steps"] if step.get("uses") == "actions/setup-python@v5")
        self.assertEqual(setup["with"]["python-version"], "3.12")
        contract = (ROOT / "services/platform-worker/internal/outbox/media_delete_contract_test.go").read_text(encoding="utf-8")
        self.assertIn("TestMediaDeletionPythonHTTPContractRetryAndRecovery", contract)
        self.assertIn("http_delete_fixture.py", contract)
        self.assertIn("media_delete_unavailable", contract)
        fixture = (ROOT / "workers/media-python/tests/http_delete_fixture.py").read_text(encoding="utf-8")
        self.assertIn("handler(service, reconcile", fixture)
        self.assertNotIn("boto3", fixture)

    def test_ci_runs_reconcile_http_and_post_lock_snapshot_contracts(self) -> None:
        workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8"))
        steps = workflow["jobs"]["migrations"]["steps"]
        step = next(step for step in steps if "TestPostgresReconciliationScheduleContract" in step.get("run", ""))
        self.assertLess(step["run"].index("TestPostgresOutboxLeaseContract"), step["run"].index("TestPostgresReconciliationScheduleContract"))
        self.assertEqual(step["env"]["CONFIRM_MEDIA_RECONCILE_CONTRACT"], "disposable-database")
        self.assertEqual(step["env"]["MEDIA_RECONCILE_CONTRACT_DATABASE_URL"], step["env"]["WORKER_CONTRACT_DATABASE_URL"])
        self.assertEqual(step["env"]["MEDIA_RECONCILE_CONTRACT_ADMIN_DATABASE_URL"], step["env"]["WORKER_CONTRACT_ADMIN_DATABASE_URL"])
        self.assertNotIn("continue-on-error", step)
        contract = (ROOT / "services/platform-worker/internal/mediareconcile/postgres_contract_test.go").read_text(encoding="utf-8")
        for marker in ("validReconcileContractURL", "NOT rolsuper AND NOT rolbypassrls", "waiting == 2", "version != 21", "winners != 1 || notDue != 1"):
            self.assertIn(marker, contract)
        http_contract = (ROOT / "services/platform-worker/internal/mediareconcile/python_contract_test.go").read_text(encoding="utf-8")
        self.assertIn("MEDIA_CONTRACT_PYTHON", http_contract)
        self.assertIn("--enable-reconciliation", http_contract)
        self.assertIn("reconcile_unavailable", http_contract)
        self.assertIn("Proxy: nil", http_contract)

    def test_ci_requires_real_go_next_cache_invalidation_before_delivery(self) -> None:
        workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8"))
        job = workflow["jobs"]["web"]
        steps = job["steps"]
        self.assertTrue(any(step.get("uses") == "actions/setup-go@v5" for step in steps))
        command = "npm run test:cache:release-a -- --output docs/evidence/release-a-cache-contract-ci.json"
        index = next(index for index, step in enumerate(steps) if step.get("run") == command)
        build_index = next(index for index, step in enumerate(steps) if step.get("run") == "npm run build")
        self.assertGreater(index, build_index)
        self.assertNotIn("continue-on-error", steps[index])
        self.assertNotIn("if", steps[index])
        self.assertNotIn("continue-on-error", job)
        upload = next(step for step in steps if step.get("uses") == "actions/upload-artifact@v4")
        self.assertEqual(upload["if"], "always()")
        self.assertIn("docs/evidence/release-a-cache-contract-ci.json", upload["with"]["path"])
        self.assertIn("web", workflow["jobs"]["images"]["needs"])

    def test_ci_requires_isolated_worker_postgres_contract_with_restricted_login(self) -> None:
        workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8"))
        job = workflow["jobs"]["migrations"]
        step = next(step for step in job["steps"] if "TestPostgresOutboxLeaseContract" in step.get("run", ""))
        self.assertNotIn("continue-on-error", step)
        self.assertNotIn("if", step)
        self.assertEqual(step["env"]["CONFIRM_WORKER_OUTBOX_CONTRACT"], "disposable-database")
        self.assertIn("platform_worker_login:", step["env"]["WORKER_CONTRACT_DATABASE_URL"])
        self.assertTrue(step["env"]["WORKER_CONTRACT_DATABASE_URL"].endswith("/self_deepsearch_worker_test"))
        self.assertIn("CREATE DATABASE self_deepsearch_worker_test", step["run"])
        self.assertIn("infra/database/migrate.sh", step["run"])
        self.assertIn("infra/database/provision_runtime_logins.sh", step["run"])
        self.assertNotIn("load_release_a_fixtures", step["run"])
        self.assertIn("migrations", workflow["jobs"]["images"]["needs"])

    def test_ci_includes_identity_lifecycle_contract_with_restricted_login(self) -> None:
        workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8"))
        step = next(step for step in workflow["jobs"]["migrations"]["steps"]
                    if "go test -count=1 ./services/platform-api/internal/database" in step.get("run", ""))
        self.assertNotIn("continue-on-error", step)
        self.assertNotIn("if", step)
        url = step["env"]["PLATFORM_API_TEST_DATABASE_URL"]
        self.assertIn("platform_api_login:", url)
        self.assertTrue(url.endswith("@127.0.0.1:5432/self_deepsearch_migrate_test"))
        contract = (ROOT / "services/platform-api/internal/database/identity_store_contract_test.go").read_text(encoding="utf-8")
        self.assertIn("TestPostgresIdentityLifecycleContract", contract)
        self.assertIn("concurrent_password_change_is_rechecked_after_row_lock", contract)
        self.assertIn("old_account_reset_code_cannot_cross_email_reuse", contract)
        self.assertNotIn("DELETE FROM audit.", contract)
        self.assertNotIn("DISABLE TRIGGER", contract)


    def test_logout_confirmation_is_required_across_api_and_frontends(self) -> None:
        spec = yaml.safe_load((ROOT / "packages/api-contracts/openapi.yaml").read_text(encoding="utf-8"))
        responses = spec["paths"]["/api/v1/auth/logout"]["post"]["responses"]
        self.assertIn("204", responses)
        self.assertIn("503", responses)
        self.assertIn("cookies remain unchanged", responses["503"]["description"])
        for app in ("display-web", "ops-web"):
            route = (ROOT / f"apps/{app}/app/auth/logout/route.ts").read_text(encoding="utf-8")
            for guard in ('upstream.status !== 204', 'redirect: "error"', 'AbortSignal.timeout(3000)',
                          'request.headers.get("origin") !== origin', 'request.headers.get("host")',
                          'request.headers.get("sec-fetch-site") === "same-origin"',
                          'request.headers.get("sec-fetch-mode") === "navigate"',
                          'private, no-store', '/logout?error=unconfirmed'):
                self.assertIn(guard, route)
            page = (ROOT / f"apps/{app}/app/logout/page.tsx").read_text(encoding="utf-8")
            for signal in ('"force-dynamic"', 'index: false', 'role="alert"', '重试退出', '关闭页面不代表已退出登录'):
                self.assertIn(signal, page)
        e2e = (ROOT / "tests/e2e/release-a.spec.mjs").read_text(encoding="utf-8")
        for scenario in ("公开站和后台退出失败保留重试，确认后才清除会话",
                         "两个退出入口拒绝假成功回执、重定向和无界等待",
                         "两个退出入口拒绝跨站请求，无会话退出无需依赖 API",
                         "普通账号误入后台时退出失败也必须明确提示"):
            self.assertIn(scenario, e2e)


if __name__ == "__main__":
    unittest.main()
