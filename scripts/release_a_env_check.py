from __future__ import annotations

import argparse
import ipaddress
import re
import sys
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from pathlib import Path
from urllib.parse import SplitResult, unquote, urlsplit

KEY_PATTERN = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")
DURATION_PATTERN = re.compile(r"^[1-9][0-9]*(?:\.[0-9]+)?(?:ns|us|µs|ms|s|m|h)$")
EMAIL_PATTERN = re.compile(r"^[^@\s]+@[^@\s]+\.[^@\s]+$")
PLACEHOLDER_MARKERS = (
    "replace-with",
    "example.invalid",
    "changeme",
    "change-me",
    "todo",
    "<",
    ">",
)


@dataclass
class Report:
    errors: list[str] = field(default_factory=list)
    warnings: list[str] = field(default_factory=list)

    def error(self, scope: str, key: str, message: str) -> None:
        self.errors.append(f"{scope}:{key}: {message}")

    def warning(self, scope: str, key: str, message: str) -> None:
        self.warnings.append(f"{scope}:{key}: {message}")


def parse_env_file(path: Path) -> tuple[dict[str, str], list[str]]:
    values: dict[str, str] = {}
    errors: list[str] = []
    try:
        lines = path.read_text(encoding="utf-8-sig").splitlines()
    except OSError as exc:
        return {}, [f"env-file:{path}: cannot read file ({exc.__class__.__name__})"]

    for line_number, raw_line in enumerate(lines, start=1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[7:].lstrip()
        if "=" not in line:
            errors.append(f"env-file:{path}:{line_number}: expected KEY=VALUE")
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip()
        if not KEY_PATTERN.fullmatch(key):
            errors.append(f"env-file:{path}:{line_number}: invalid key")
            continue
        if key in values:
            errors.append(f"env-file:{path}:{line_number}: duplicate key {key}")
            continue
        if len(value) >= 2 and value[0] == value[-1] and value[0] in {'"', "'"}:
            value = value[1:-1]
        values[key] = value
    return values, errors


def is_placeholder(value: str) -> bool:
    lowered = value.strip().lower()
    return not lowered or any(marker in lowered for marker in PLACEHOLDER_MARKERS)


def require_real(report: Report, env: dict[str, str], scope: str, keys: tuple[str, ...]) -> None:
    for key in keys:
        if key not in env:
            report.error(scope, key, "missing")
        elif is_placeholder(env[key]):
            report.error(scope, key, "still empty or contains a documented placeholder")


def require_value(report: Report, env: dict[str, str], scope: str, key: str, expected: str) -> None:
    if env.get(key, "").strip().lower() != expected.lower():
        report.error(scope, key, f"must be {expected}")


def parse_url(
    report: Report,
    env: dict[str, str],
    scope: str,
    key: str,
    schemes: tuple[str, ...],
    *,
    origin_only: bool = False,
    allow_credentials: bool = False,
) -> SplitResult | None:
    value = env.get(key, "").strip()
    if is_placeholder(value):
        return None
    try:
        parsed = urlsplit(value)
        parsed.port
    except ValueError:
        report.error(scope, key, f"must be an absolute {'/'.join(schemes)} URL with a valid host and port")
        return None
    if parsed.scheme not in schemes or not parsed.hostname:
        report.error(scope, key, f"must be an absolute {'/'.join(schemes)} URL")
        return None
    if not allow_credentials and (parsed.username is not None or parsed.password is not None):
        report.error(scope, key, "must not contain embedded credentials")
    if origin_only and (parsed.path not in ("", "/") or parsed.query or parsed.fragment):
        report.error(scope, key, "must be an origin without path, query, or fragment")
    return parsed


def url_origin(parsed: SplitResult) -> str:
    host = parsed.hostname or ""
    default_port = (parsed.scheme == "https" and parsed.port == 443) or (parsed.scheme == "http" and parsed.port == 80)
    port = "" if parsed.port is None or default_port else f":{parsed.port}"
    return f"{parsed.scheme}://{host}{port}"


def validate_plain_hostname(report: Report, env: dict[str, str], scope: str, key: str) -> str | None:
    raw = env.get(key, "")
    value = raw.strip()
    if is_placeholder(value):
        return None
    try:
        ipaddress.ip_address(value)
        is_ip_address = True
    except ValueError:
        is_ip_address = False
    labels = value.split(".")
    valid = (
        raw == value
        and not is_ip_address
        and len(value) <= 253
        and len(labels) >= 2
        and not value.endswith(".")
        and all(
            1 <= len(label) <= 63
            and label[0] != "-"
            and label[-1] != "-"
            and all(character.isascii() and (character.isalnum() or character == "-") for character in label)
            for label in labels
        )
    )
    if not valid:
        report.error(scope, key, "must be a plain DNS hostname without scheme, port, path, or wildcard")
        return None
    return value.lower()


def check_image(
    report: Report, env: dict[str, str], scope: str, key: str, *, expected_version: str | None = None
) -> None:
    value = env.get(key, "").strip()
    if is_placeholder(value):
        return
    if "@sha256:" not in value and ":" not in value.rsplit("/", 1)[-1]:
        report.error(scope, key, "must use an immutable digest or an explicit version tag")
    if value.endswith(":latest") or value == "latest":
        report.error(scope, key, "must not use the latest tag")
    if expected_version and "@sha256:" not in value:
        image_name = value.rsplit("/", 1)[-1]
        tag = image_name.rsplit(":", 1)[1] if ":" in image_name else ""
        if tag != expected_version:
            report.error(scope, key, "tag must match BUILD_VERSION or use an immutable sha256 digest")


def check_secret(
    report: Report, env: dict[str, str], scope: str, key: str, *, min_length: int = 32
) -> str | None:
    value = env.get(key, "").strip()
    if is_placeholder(value):
        return None
    if len(value) < min_length:
        report.error(scope, key, f"must contain at least {min_length} characters")
        return None
    return value


def check_distinct(report: Report, scope: str, named_values: dict[str, str | None]) -> None:
    by_value: dict[str, list[str]] = {}
    for key, value in named_values.items():
        if value is not None:
            by_value.setdefault(value, []).append(key)
    for keys in by_value.values():
        if len(keys) > 1:
            report.error(scope, ",".join(sorted(keys)), "values must be different and must not be reused")


def check_alertmanager_config(report: Report, path: Path, scope: str) -> None:
    try:
        source = path.read_text(encoding="utf-8")
    except OSError:
        report.error(scope, "ALERTMANAGER_CONFIG_FILE", "cannot read file")
        return
    lowered = source.lower()
    if "example.invalid" in lowered or "replace-with" in lowered:
        report.error(scope, "ALERTMANAGER_CONFIG_FILE", "still contains documented placeholder values")
    required_patterns = (
        r"(?m)^\s*smtp_auth_password_file\s*:\s*['\"]?/run/secrets/alertmanager_smtp_password['\"]?\s*$",
        r"severity\s*=\s*['\"]critical['\"]",
        r"severity\s*=~\s*['\"]warning\|info['\"]",
        r"(?m)^\s*send_resolved\s*:\s*true\s*$",
    )
    for pattern in required_patterns:
        if re.search(pattern, source) is None:
            report.error(scope, "ALERTMANAGER_CONFIG_FILE", "missing required Release A routing or secret-file setting")
            break
    if re.search(r"(?m)^\s*smtp_auth_password\s*:", source):
        report.error(scope, "ALERTMANAGER_CONFIG_FILE", "must use smtp_auth_password_file instead of an inline secret")


def check_integer(
    report: Report, env: dict[str, str], scope: str, key: str, minimum: int, maximum: int
) -> int | None:
    try:
        value = int(env.get(key, ""))
    except ValueError:
        report.error(scope, key, "must be an integer")
        return None
    if value < minimum or value > maximum:
        report.error(scope, key, f"must be between {minimum} and {maximum}")
        return None
    return value


def check_duration(report: Report, env: dict[str, str], scope: str, key: str) -> None:
    if not DURATION_PATTERN.fullmatch(env.get(key, "").strip()):
        report.error(scope, key, "must be a positive Go duration such as 2s, 15m, or 24h")


def validate_usage_monitor(env: dict[str, str], report: Report) -> None:
    scope = "japan"
    mode = env.get("MEDIA_USAGE_MONITOR_MODE", "off").strip()
    guard = env.get("MEDIA_RECONCILE_USAGE_GUARD", "off").strip()
    if guard not in {"off", "enforce"}:
        report.error(scope, "MEDIA_RECONCILE_USAGE_GUARD", "must be off or enforce")
    if guard == "enforce" and (mode != "observe" or env.get("MEDIA_RECONCILE_ENABLED") != "true"):
        report.error(scope, "MEDIA_RECONCILE_USAGE_GUARD", "requires observe mode and enabled reconciliation; does not control uploads or image delivery")
    if mode == "off":
        return
    if mode != "observe":
        report.error(scope, "MEDIA_USAGE_MONITOR_MODE", "must be off or observe")
        return
    for key, pattern in (
        ("R2_ANALYTICS_ACCOUNT_ID", r"[a-fA-F0-9]{32}"),
        ("R2_ANALYTICS_API_TOKEN", r"[A-Za-z0-9_-]{20,256}"),
    ):
        if not re.fullmatch(pattern, env.get(key, "")):
            report.error(scope, key, "must be a valid account-scoped read-only analytics setting")
    period: list[datetime] = []
    for key in ("MEDIA_USAGE_PERIOD_START", "MEDIA_USAGE_PERIOD_END"):
        value = env.get(key, "")
        try:
            if not re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})", value):
                raise ValueError
            period.append(datetime.fromisoformat(value.replace("Z", "+00:00")))
        except ValueError:
            report.error(scope, key, "must explicitly identify the confirmed period in whole-second RFC3339")
    if len(period) == 2 and not timedelta(0) < period[1] - period[0] <= timedelta(days=31):
        report.error(scope, "MEDIA_USAGE_PERIOD_END", "must be after start and within 31 days; no implicit rollover")
    duration = env.get("MEDIA_USAGE_INTERVAL", "15m").strip()
    match = re.fullmatch(r"([1-9][0-9]*(?:\.[0-9]+)?)(ns|us|µs|ms|s|m|h)", duration)
    units = {"ns": 1e-9, "us": 1e-6, "µs": 1e-6, "ms": 1e-3, "s": 1, "m": 60, "h": 3600}
    if not match or not 300 <= float(match[1]) * units[match[2]] <= 900:
        report.error(scope, "MEDIA_USAGE_INTERVAL", "must be 5 to 15 minutes")


def validate_japan(env: dict[str, str], report: Report, *, mode: str, check_files: bool) -> None:
    scope = "japan"
    admission_mode = env.get("MEDIA_UPLOAD_ADMISSION_MODE", "off")
    if admission_mode not in {"off", "enforce"}:
        report.error(scope, "MEDIA_UPLOAD_ADMISSION_MODE", "must be off or enforce")
    if admission_mode == "enforce":
        if env.get("MEDIA_USAGE_MONITOR_MODE") != "observe" or env.get("MEDIA_UPLOAD_QUEUE_MODE") != "dispatch":
            report.error(scope, "MEDIA_UPLOAD_ADMISSION_MODE", "requires observe and dispatch; endpoint alone does not pause idle uploaders or image delivery")
        validate_admission_secret(env, report, scope)
    queue_mode = env.get("MEDIA_UPLOAD_QUEUE_MODE", "off")
    if queue_mode not in {"off", "dispatch"}:
        report.error(scope, "MEDIA_UPLOAD_QUEUE_MODE", "must be off or dispatch")
    if env.get("MEDIA_UPLOAD_CONTROL_ALLOW_HTTP", "false") not in {"false", "true"}:
        report.error(scope, "MEDIA_UPLOAD_CONTROL_ALLOW_HTTP", "must be false or true")
    if queue_mode == "dispatch":
        require_real(report, env, scope, ("MEDIA_UPLOAD_CONTROL_URL", "MEDIA_UPLOAD_CONTROL_SECRET"))
        if env.get("MEDIA_UPLOAD_CONTROL_URL", "").startswith("http:") and env.get("MEDIA_UPLOAD_CONTROL_ALLOW_HTTP") != "true":
            report.error(scope, "MEDIA_UPLOAD_CONTROL_ALLOW_HTTP", "explicit HTTP opt-in is required; prefer VPN/TLS")
        if env.get("MEDIA_USAGE_MONITOR_MODE", "off") != "observe":
            report.warning(scope, "MEDIA_UPLOAD_QUEUE_MODE", "pause can execute, but resume will be rejected without the usage observer guard")
    if env.get("MEDIA_UPLOAD_CONTROL_URL") or env.get("MEDIA_UPLOAD_CONTROL_SECRET"):
        require_real(report, env, scope, ("MEDIA_UPLOAD_CONTROL_URL", "MEDIA_UPLOAD_CONTROL_SECRET"))
        parse_url(report, env, scope, "MEDIA_UPLOAD_CONTROL_URL", ("http", "https"), origin_only=True)
        check_secret(report, env, scope, "MEDIA_UPLOAD_CONTROL_SECRET")
        if env.get("MEDIA_UPLOAD_CONTROL_SECRET", "").strip() == env.get("MEDIA_HMAC_SECRET", "").strip():
            report.error(scope, "MEDIA_UPLOAD_CONTROL_SECRET", "must be separate from MEDIA_HMAC_SECRET")
    if env.get("MEDIA_DELIVERY_MODE") not in {"normal", "default_only"}:
        report.error(scope, "MEDIA_DELIVERY_MODE", "must be normal or default_only")
    dynamic_delivery_mode = env.get("MEDIA_DELIVERY_DYNAMIC_MODE", "off")
    if dynamic_delivery_mode not in {"off", "enforce"}:
        report.error(scope, "MEDIA_DELIVERY_DYNAMIC_MODE", "must be off or enforce")
    if dynamic_delivery_mode == "enforce" and env.get("MEDIA_USAGE_MONITOR_MODE") != "observe":
        report.error(scope, "MEDIA_DELIVERY_DYNAMIC_MODE", "requires MEDIA_USAGE_MONITOR_MODE=observe")
    edge_policy_mode = env.get("MEDIA_EDGE_POLICY_MODE", "off")
    if edge_policy_mode not in {"off", "enforce"}:
        report.error(scope, "MEDIA_EDGE_POLICY_MODE", "must be off or enforce")
    edge_policy_token: str | None = None
    if edge_policy_mode == "enforce":
        require_real(report, env, scope, ("MEDIA_EDGE_POLICY_TOKEN",))
        edge_policy_token = check_secret(report, env, scope, "MEDIA_EDGE_POLICY_TOKEN")
    core_real = (
        "BUILD_VERSION",
        "PLATFORM_API_IMAGE",
        "PLATFORM_WORKER_IMAGE",
        "DISPLAY_WEB_IMAGE",
        "OPS_WEB_IMAGE",
        "POSTGRES_DB",
        "POSTGRES_USER",
        "POSTGRES_PASSWORD",
        "ADMIN_DATABASE_URL",
        "PLATFORM_API_DB_PASSWORD",
        "PLATFORM_WORKER_DB_PASSWORD",
        "PLATFORM_API_DATABASE_URL",
        "PLATFORM_WORKER_DATABASE_URL",
        "WORKER_ID",
        "ALLOWED_ORIGINS",
        "SITE_URL",
        "NEXT_PUBLIC_SITE_URL",
        "RIGHTS_CONTACT_EMAIL",
        "S3_PUBLIC_BASE_URL",
        "AUTH_HMAC_SECRET",
        "CACHE_HMAC_SECRET",
        "DISPLAY_REVALIDATE_URL",
        "MEDIA_DELETE_URL",
        "MEDIA_HMAC_SECRET",
        "MEDIA_RECONCILE_URL",
        "MEDIA_INSPECT_DISPLAY_ORIGIN",
        "SMTP_URL",
        "TURNSTILE_SITE_KEY",
        "TURNSTILE_SECRET_KEY",
        "TURNSTILE_EXPECTED_HOSTNAME",
        "METRICS_TOKEN",
    )
    require_real(report, env, scope, core_real)
    require_value(report, env, scope, "APP_ENV", "production")
    require_value(report, env, scope, "POSTGRES_USER", "platform")
    require_value(report, env, scope, "COLLECTION_ENABLED", "false")
    require_value(report, env, scope, "TURNSTILE_BYPASS", "false")
    require_value(report, env, scope, "MEDIA_INSPECT_ENABLED", "true")
    validate_usage_monitor(env, report)

    fixture_flag = env.get("ALLOW_SYNTHETIC_FIXTURES", "false").strip().lower()
    if fixture_flag not in {"true", "false"}:
        report.error(scope, "ALLOW_SYNTHETIC_FIXTURES", "must be true or false")
    if fixture_flag == "true" and env.get("CONFIRM_SYNTHETIC_FIXTURES", "").strip() != "release-a-test-only":
        report.error(
            scope,
            "CONFIRM_SYNTHETIC_FIXTURES",
            "must be release-a-test-only while synthetic fixture loading is enabled",
        )

    build_version = env.get("BUILD_VERSION", "").strip()
    for key in ("PLATFORM_API_IMAGE", "PLATFORM_WORKER_IMAGE", "DISPLAY_WEB_IMAGE", "OPS_WEB_IMAGE"):
        check_image(report, env, scope, key, expected_version=build_version)
    if env.get("BUILD_VERSION", "").strip().lower() in {"dev", "latest"}:
        report.error(scope, "BUILD_VERSION", "must identify an immutable release")

    site = parse_url(report, env, scope, "SITE_URL", ("https", "http"), origin_only=True)
    turnstile_hostname = validate_plain_hostname(report, env, scope, "TURNSTILE_EXPECTED_HOSTNAME")
    next_site = parse_url(report, env, scope, "NEXT_PUBLIC_SITE_URL", ("https", "http"), origin_only=True)
    media_base = parse_url(report, env, scope, "S3_PUBLIC_BASE_URL", ("https",), origin_only=True)
    smtp = parse_url(report, env, scope, "SMTP_URL", ("smtp", "smtps"), allow_credentials=True)
    revalidate = parse_url(report, env, scope, "DISPLAY_REVALIDATE_URL", ("http", "https"))
    media_delete = parse_url(report, env, scope, "MEDIA_DELETE_URL", ("http", "https"))
    media_reconcile = parse_url(report, env, scope, "MEDIA_RECONCILE_URL", ("http", "https"))
    inspection_origin = parse_url(report, env, scope, "MEDIA_INSPECT_DISPLAY_ORIGIN", ("http", "https"), origin_only=True)
    if mode == "full" and site and inspection_origin and url_origin(inspection_origin) != url_origin(site):
        report.error(scope, "MEDIA_INSPECT_DISPLAY_ORIGIN", "must match SITE_URL for public-path inspection in full mode")
    if edge_policy_mode == "enforce" and media_base:
        host = (media_base.hostname or "").lower()
        if host.endswith(".r2.dev") or host.endswith(".r2.cloudflarestorage.com"):
            report.error(scope, "S3_PUBLIC_BASE_URL", "must use the controlled media gateway origin, not a direct R2 endpoint")
    del media_base, smtp

    if site and next_site and url_origin(site) != url_origin(next_site):
        report.error(scope, "SITE_URL,NEXT_PUBLIC_SITE_URL", "origins must match")
    if site and turnstile_hostname and turnstile_hostname != (site.hostname or "").lower():
        report.error(scope, "TURNSTILE_EXPECTED_HOSTNAME,SITE_URL", "hosts must match")
    if site and site.scheme != "https" and site.hostname not in {"127.0.0.1", "localhost"}:
        report.error(scope, "SITE_URL", "must use HTTPS outside localhost")
    if revalidate and revalidate.path != "/api/internal/revalidate":
        report.error(scope, "DISPLAY_REVALIDATE_URL", "must use /api/internal/revalidate")
    if media_delete and media_delete.path != "/v1/delete":
        report.error(scope, "MEDIA_DELETE_URL", "must use /v1/delete")
    if media_reconcile and media_reconcile.path != "/v1/reconcile":
        report.error(scope, "MEDIA_RECONCILE_URL", "must use /v1/reconcile")

    allowed_origins: set[str] = set()
    for index, raw_origin in enumerate(env.get("ALLOWED_ORIGINS", "").split(","), start=1):
        temporary = {f"origin_{index}": raw_origin.strip()}
        parsed = parse_url(report, temporary, scope, f"origin_{index}", ("https", "http"), origin_only=True)
        if parsed:
            allowed_origins.add(url_origin(parsed))
    if site and url_origin(site) not in allowed_origins:
        report.error(scope, "ALLOWED_ORIGINS", "must include SITE_URL")

    rights_email = env.get("RIGHTS_CONTACT_EMAIL", "").strip()
    if not is_placeholder(rights_email) and not EMAIL_PATTERN.fullmatch(rights_email):
        report.error(scope, "RIGHTS_CONTACT_EMAIL", "must be a valid email address")

    bootstrap_email = env.get("BOOTSTRAP_OWNER_EMAIL", "").strip()
    bootstrap_password = env.get("BOOTSTRAP_OWNER_PASSWORD", "")
    bootstrap_password_secret: str | None = None
    if bootstrap_email or bootstrap_password:
        if is_placeholder(bootstrap_email):
            report.error(scope, "BOOTSTRAP_OWNER_EMAIL", "must be a real email when owner bootstrap is requested")
        elif not EMAIL_PATTERN.fullmatch(bootstrap_email):
            report.error(scope, "BOOTSTRAP_OWNER_EMAIL", "must be a valid email address")
        if is_placeholder(bootstrap_password):
            report.error(scope, "BOOTSTRAP_OWNER_PASSWORD", "must be set when owner bootstrap is requested")
        elif not 12 <= len(bootstrap_password) <= 128:
            report.error(scope, "BOOTSTRAP_OWNER_PASSWORD", "must contain 12 to 128 characters")
        else:
            bootstrap_password_secret = bootstrap_password

    auth_secret = check_secret(report, env, scope, "AUTH_HMAC_SECRET")
    cache_secret = check_secret(report, env, scope, "CACHE_HMAC_SECRET")
    media_secret = check_secret(report, env, scope, "MEDIA_HMAC_SECRET")
    metrics_token = check_secret(report, env, scope, "METRICS_TOKEN")
    owner_password = check_secret(report, env, scope, "POSTGRES_PASSWORD", min_length=16)
    api_password = check_secret(report, env, scope, "PLATFORM_API_DB_PASSWORD", min_length=16)
    worker_password = check_secret(report, env, scope, "PLATFORM_WORKER_DB_PASSWORD", min_length=16)
    check_distinct(
        report,
        scope,
        {
            "AUTH_HMAC_SECRET": auth_secret,
            "CACHE_HMAC_SECRET": cache_secret,
            "MEDIA_HMAC_SECRET": media_secret,
            "MEDIA_EDGE_POLICY_TOKEN": edge_policy_token,
            "METRICS_TOKEN": metrics_token,
            "POSTGRES_PASSWORD": owner_password,
            "PLATFORM_API_DB_PASSWORD": api_password,
            "PLATFORM_WORKER_DB_PASSWORD": worker_password,
            "BOOTSTRAP_OWNER_PASSWORD": bootstrap_password_secret,
        },
    )

    database_expectations = {
        "ADMIN_DATABASE_URL": ("platform", "POSTGRES_PASSWORD"),
        "PLATFORM_API_DATABASE_URL": ("platform_api_login", "PLATFORM_API_DB_PASSWORD"),
        "PLATFORM_WORKER_DATABASE_URL": ("platform_worker_login", "PLATFORM_WORKER_DB_PASSWORD"),
    }
    for key, (expected_user, password_key) in database_expectations.items():
        parsed = parse_url(report, env, scope, key, ("postgres", "postgresql"), allow_credentials=True)
        if parsed and parsed.username != expected_user:
            report.error(scope, key, f"must use the {expected_user} login")
        if parsed and (not parsed.password or not parsed.path.strip("/")):
            report.error(scope, key, "must contain a password and database name")
        if parsed and parsed.password and unquote(parsed.password) != env.get(password_key, ""):
            report.error(scope, key, f"password must match {password_key}")
        if parsed and parsed.path.strip("/") != env.get("POSTGRES_DB", "").strip():
            report.error(scope, key, "database name must match POSTGRES_DB")

    for key in (
        "EMAIL_DAILY_LIMIT",
        "EMAIL_FAILURE_THRESHOLD",
        "HISTORY_ARCHIVE_BATCH_SIZE",
    ):
        check_integer(report, env, scope, key, 1, 10_000 if key != "EMAIL_DAILY_LIMIT" else 10_000_000)
    for key in (
        "WORKER_POLL_INTERVAL",
        "WORKER_LEASE_DURATION",
        "MEDIA_RECONCILE_INTERVAL",
        "EMAIL_CIRCUIT_COOLDOWN",
        "EMAIL_SEND_TIMEOUT",
        "HISTORY_ARCHIVE_INTERVAL",
        "HISTORY_ARCHIVE_MIN_AGE",
    ):
        check_duration(report, env, scope, key)

    if mode == "full":
        edge_real = (
            "EDGE_IMAGE",
            "DISPLAY_HOST",
            "API_HOST",
            "OPS_HOST",
        )
        require_real(report, env, scope, edge_real)
        check_image(report, env, scope, "EDGE_IMAGE")
        hosts = [env.get(key, "").strip().lower() for key in ("DISPLAY_HOST", "API_HOST", "OPS_HOST")]
        if len(set(host for host in hosts if host)) != len([host for host in hosts if host]):
            report.error(scope, "DISPLAY_HOST,API_HOST,OPS_HOST", "hosts must be different")
        if site and env.get("DISPLAY_HOST", "").strip().lower() != (site.hostname or "").lower():
            report.error(scope, "DISPLAY_HOST,SITE_URL", "hosts must match")
        if env.get("TRUST_PROXY_HEADERS", "").strip().lower() != "true":
            report.error(scope, "TRUST_PROXY_HEADERS", "must be true behind the trusted Japan edge")
        expected_origins = {
            f"https://{env.get('DISPLAY_HOST', '').strip().lower()}",
            f"https://{env.get('OPS_HOST', '').strip().lower()}",
        }
        if allowed_origins != expected_origins:
            report.error(scope, "ALLOWED_ORIGINS", "must contain exactly the display and ops HTTPS origins")

    private_files = (
        "ORIGIN_CERT_FILE",
        "ORIGIN_KEY_FILE",
        "METRICS_TOKEN_FILE",
        "ALERTMANAGER_CONFIG_FILE",
        "ALERTMANAGER_SMTP_PASSWORD_FILE",
    )
    if mode == "full" or check_files:
        require_real(report, env, scope, private_files)

    if check_files:
        for key in private_files:
            path = Path(env.get(key, ""))
            if not path.is_file():
                report.error(scope, key, "file does not exist on this host")
        token_path = Path(env.get("METRICS_TOKEN_FILE", ""))
        if token_path.is_file():
            try:
                file_token = token_path.read_text(encoding="utf-8").strip()
            except OSError:
                report.error(scope, "METRICS_TOKEN_FILE", "cannot read file")
            else:
                if metrics_token is not None and file_token != metrics_token:
                    report.error(scope, "METRICS_TOKEN_FILE", "content does not match METRICS_TOKEN")
        alertmanager_config = Path(env.get("ALERTMANAGER_CONFIG_FILE", ""))
        if alertmanager_config.is_file():
            check_alertmanager_config(report, alertmanager_config, scope)
        alertmanager_password = Path(env.get("ALERTMANAGER_SMTP_PASSWORD_FILE", ""))
        if alertmanager_password.is_file():
            try:
                password_size = alertmanager_password.stat().st_size
            except OSError:
                report.error(scope, "ALERTMANAGER_SMTP_PASSWORD_FILE", "cannot inspect file")
            else:
                if password_size < 12 or password_size > 4096:
                    report.error(scope, "ALERTMANAGER_SMTP_PASSWORD_FILE", "must contain a 12 to 4096 byte secret")


def validate_admission_secret(env: dict[str, str], report: Report, scope: str) -> None:
    key = "MEDIA_UPLOAD_ADMISSION_SECRET"
    require_real(report, env, scope, (key,))
    secret = env.get(key, "").strip()
    if not 32 <= len(secret.encode("utf-8")) <= 4096:
        report.error(scope, key, "must contain 32 to 4096 bytes")
    for other in ("MEDIA_HMAC_SECRET", "MEDIA_UPLOAD_CONTROL_SECRET", "METRICS_TOKEN", "CACHE_HMAC_SECRET", "AUTH_HMAC_SECRET"):
        if secret and secret == env.get(other, "").strip():
            report.error(scope, key, f"must be separate from {other}")


def validate_beijing_admission(env: dict[str, str], japan: dict[str, str], report: Report) -> None:
    scope = "beijing"
    mode = env.get("MEDIA_UPLOAD_ADMISSION_MODE", "off")
    if mode not in {"off", "enforce"}:
        report.error(scope, "MEDIA_UPLOAD_ADMISSION_MODE", "must be off or enforce")
    if mode != japan.get("MEDIA_UPLOAD_ADMISSION_MODE", "off"):
        report.error(scope, "MEDIA_UPLOAD_ADMISSION_MODE", "must match Japan; both sides must explicitly enforce")
    if env.get("MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP", "false") not in {"false", "true"}:
        report.error(scope, "MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP", "must be false or true")
    if mode != "enforce":
        return
    if env.get("MEDIA_UPLOAD_CONTROL_MODE") != "enforce":
        report.error(scope, "MEDIA_UPLOAD_ADMISSION_MODE", "requires Beijing upload control enforce with shared durable state")
    validate_admission_secret(env, report, scope)
    if env.get("MEDIA_UPLOAD_ADMISSION_SECRET", "").strip() != japan.get("MEDIA_UPLOAD_ADMISSION_SECRET", "").strip():
        report.error(scope, "MEDIA_UPLOAD_ADMISSION_SECRET", "must match Japan")
    key = "MEDIA_UPLOAD_ADMISSION_URL"
    require_real(report, env, scope, (key,))
    parsed = parse_url(report, env, scope, key, ("https", "http"), origin_only=True)
    if parsed is None:
        return
    raw = env.get(key, "")
    try:
        address = ipaddress.ip_address(parsed.hostname or "")
        if address.is_unspecified or address.is_multicast or "%" in (parsed.hostname or "") or parsed.port == 0:
            raise ValueError
    except ValueError:
        report.error(scope, key, "requires a fixed unicast IP and nonzero port, no DNS names or IPv6 zones")
    if "?" in raw or "#" in raw or any(character.isspace() for character in raw):
        report.error(scope, key, "must not contain query/fragment delimiters or whitespace")
    if parsed.scheme == "http":
        if env.get("MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP") != "true":
            report.error(scope, "MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP", "explicit HTTP opt-in is required; use a trusted encrypted tunnel")
        report.warning(scope, key, "HTTP must travel through an encrypted private tunnel; HMAC alone does not provide confidentiality")
    else:
        report.warning(scope, key, "HTTPS must validate a certificate for the configured IP; no insecure TLS bypass is supported")


def validate_beijing(env: dict[str, str], japan: dict[str, str], report: Report) -> None:
    scope = "beijing"
    validate_beijing_admission(env, japan, report)
    control_mode = env.get("MEDIA_UPLOAD_CONTROL_MODE", "off")
    if control_mode not in {"off", "enforce"}:
        report.error(scope, "MEDIA_UPLOAD_CONTROL_MODE", "must be off or enforce")
    if japan.get("MEDIA_UPLOAD_QUEUE_MODE", "off") == "dispatch" and control_mode != "enforce":
        report.error(scope, "MEDIA_UPLOAD_CONTROL_MODE", "Japan dispatch requires Beijing enforce with matching private control configuration")
    if control_mode == "enforce":
        check_secret(report, env, scope, "MEDIA_UPLOAD_CONTROL_SECRET")
        if not japan.get("MEDIA_UPLOAD_CONTROL_URL"):
            report.error(scope, "MEDIA_UPLOAD_CONTROL_URL", "Japan must configure the private control origin")
        if env.get("MEDIA_UPLOAD_CONTROL_SECRET", "").strip() != japan.get("MEDIA_UPLOAD_CONTROL_SECRET", "").strip():
            report.error(scope, "MEDIA_UPLOAD_CONTROL_SECRET", "must match Japan")
        if env.get("MEDIA_UPLOAD_CONTROL_SECRET", "").strip() == env.get("MEDIA_HMAC_SECRET", "").strip():
            report.error(scope, "MEDIA_UPLOAD_CONTROL_SECRET", "must be separate from MEDIA_HMAC_SECRET")
    if env.get("MEDIA_DELIVERY_MODE") not in {"normal", "default_only"} or env.get("MEDIA_DELIVERY_MODE") != japan.get("MEDIA_DELIVERY_MODE"):
        report.error(scope, "MEDIA_DELIVERY_MODE", "must be normal or default_only and match Japan")
    real_keys = (
        "BUILD_VERSION",
        "WORKER_ID",
        "METRICS_TOKEN",
        "PLATFORM_WORKER_IMAGE",
        "MEDIA_PYTHON_IMAGE",
        "INGEST_API_URL",
        "MEDIA_HMAC_SECRET",
        "S3_ENDPOINT_URL",
        "S3_REGION",
        "S3_PRIVATE_BUCKET",
        "S3_PUBLIC_BUCKET",
        "S3_PUBLIC_BASE_URL",
        "AWS_ACCESS_KEY_ID",
        "AWS_SECRET_ACCESS_KEY",
        "CLOUDFLARE_ZONE_ID",
        "CLOUDFLARE_API_TOKEN",
    )
    require_real(report, env, scope, real_keys)
    require_value(report, env, scope, "APP_ENV", "production")
    build_version = env.get("BUILD_VERSION", "").strip()
    for key in ("PLATFORM_WORKER_IMAGE", "MEDIA_PYTHON_IMAGE"):
        check_image(report, env, scope, key, expected_version=build_version)
    parse_url(report, env, scope, "INGEST_API_URL", ("https",))
    parse_url(report, env, scope, "S3_ENDPOINT_URL", ("https",), origin_only=True)
    parse_url(report, env, scope, "S3_PUBLIC_BASE_URL", ("https",), origin_only=True)
    check_secret(report, env, scope, "METRICS_TOKEN")
    check_secret(report, env, scope, "MEDIA_HMAC_SECRET")
    check_secret(report, env, scope, "AWS_SECRET_ACCESS_KEY", min_length=16)
    check_secret(report, env, scope, "CLOUDFLARE_API_TOKEN", min_length=16)
    check_duration(report, env, scope, "WORKER_POLL_INTERVAL")
    check_duration(report, env, scope, "WORKER_LEASE_DURATION")
    check_integer(report, env, scope, "MEDIA_STORAGE_LIMIT_BYTES", 1, 10 * 1024**3)
    require_value(report, env, scope, "MEDIA_UPLOAD_LOCK_FILE", "/var/lib/self-deepsearch/media-state/upload.lock")

    if env.get("S3_PRIVATE_BUCKET", "").strip() == env.get("S3_PUBLIC_BUCKET", "").strip():
        report.error(scope, "S3_PRIVATE_BUCKET,S3_PUBLIC_BUCKET", "private and public buckets must be different")
    if env.get("MEDIA_HMAC_SECRET", "").strip() != japan.get("MEDIA_HMAC_SECRET", "").strip():
        report.error(scope, "MEDIA_HMAC_SECRET", "must match the Japan MEDIA_HMAC_SECRET")
    if env.get("S3_PUBLIC_BASE_URL", "").rstrip("/") != japan.get("S3_PUBLIC_BASE_URL", "").rstrip("/"):
        report.error(scope, "S3_PUBLIC_BASE_URL", "must match the Japan public media base URL")
    if env.get("BUILD_VERSION", "").strip() != japan.get("BUILD_VERSION", "").strip():
        report.error(scope, "BUILD_VERSION", "must match the Japan release version")
    if env.get("WORKER_ID", "").strip() == japan.get("WORKER_ID", "").strip():
        report.error(scope, "WORKER_ID", "must be different from the Japan worker ID")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Validate Release A deployment env files without printing any secret values"
    )
    parser.add_argument("--japan-env", type=Path, required=True)
    parser.add_argument("--beijing-env", type=Path)
    parser.add_argument("--mode", choices=("core", "full"), default="core")
    parser.add_argument(
        "--check-files",
        action="store_true",
        help="require Japan TLS, metrics, Alertmanager config, and SMTP password files on this host",
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    japan, parse_errors = parse_env_file(args.japan_env)
    if args.mode == "full" and args.beijing_env is None:
        parse_errors.append("arguments:--beijing-env: required when --mode full is used")
    beijing: dict[str, str] = {}
    if args.beijing_env is not None:
        beijing, beijing_errors = parse_env_file(args.beijing_env)
        parse_errors.extend(beijing_errors)
    if parse_errors:
        for error in parse_errors:
            print(f"ERROR {error}", file=sys.stderr)
        return 2

    report = Report()
    validate_japan(japan, report, mode=args.mode, check_files=args.check_files)
    if args.mode == "full":
        validate_beijing(beijing, japan, report)

    for warning in report.warnings:
        print(f"WARNING {warning}", file=sys.stderr)
    for error in report.errors:
        print(f"ERROR {error}", file=sys.stderr)
    if report.errors:
        print(f"FAILED: {len(report.errors)} Release A environment issue(s); secret values were not displayed.")
        return 1
    print(f"OK: Release A {args.mode} environment passed; secret values were not displayed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
