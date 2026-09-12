from __future__ import annotations

import re
from decimal import Decimal
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


def compose(relative: str) -> dict[str, object]:
    return yaml.safe_load((ROOT / relative).read_text(encoding="utf-8"))


def memory_mib(value: object) -> int:
    match = re.fullmatch(r"([1-9][0-9]*)([mMgG])", str(value))
    assert match, f"memory limit must use an explicit MiB/GiB unit: {value}"
    amount = int(match.group(1))
    return amount if match.group(2).lower() == "m" else amount * 1024


def runtime_services(config: dict[str, object]) -> list[dict[str, object]]:
    services = config["services"]
    assert isinstance(services, dict)
    return [service for service in services.values() if "tools" not in service.get("profiles", [])]


def limits(service: dict[str, object]) -> dict[str, object]:
    result = service.get("deploy", {}).get("resources", {}).get("limits", {})
    assert {"cpus", "memory"} <= set(result), "every production service needs CPU and memory limits"
    return result


def test_japan_2c4g_runtime_budget_keeps_host_headroom() -> None:
    config = compose("infra/compose/japan/compose.yaml")
    services = runtime_services(config)
    cpu = sum((Decimal(str(limits(service)["cpus"])) for service in services), Decimal())
    memory = sum(memory_mib(limits(service)["memory"]) for service in services)
    assert cpu <= Decimal("2.00")
    assert memory <= 3072
    assert all(service.get("logging", {}).get("driver") == "json-file" for service in services)


def test_beijing_2c8g_runtime_budget_is_bounded_and_leaves_burst_capacity() -> None:
    config = compose("infra/compose/beijing/compose.yaml")
    services = config["services"]
    assert set(services) == {"platform-worker-beijing", "media-python"}
    cpu = sum((Decimal(str(limits(service)["cpus"])) for service in services.values()), Decimal())
    memory = sum(memory_mib(limits(service)["memory"]) for service in services.values())
    assert cpu <= Decimal("1.50"), "reserve at least 0.5 CPU for the host and transfer overhead"
    assert memory <= 2304, "reserve more than 5 GiB for the host, image bursts and backup tooling"
    assert all(int(limits(service)["pids"]) <= 256 for service in services.values())
    assert all(service.get("logging", {}).get("driver") == "json-file" for service in services.values())


def test_beijing_media_processing_has_more_budget_than_the_go_worker_without_becoming_unbounded() -> None:
    services = compose("infra/compose/beijing/compose.yaml")["services"]
    worker = limits(services["platform-worker-beijing"])
    media = limits(services["media-python"])
    assert Decimal(str(media["cpus"])) > Decimal(str(worker["cpus"]))
    assert memory_mib(media["memory"]) >= 1536
    assert memory_mib(media["memory"]) <= 2048
    assert memory_mib(worker["memory"]) <= 256
