"""Single source for current-schema guards in cross-language static contracts."""
from pathlib import Path

MIGRATIONS = Path(__file__).resolve().parents[3] / "db" / "migrations"
SCHEMA_VERSION = max(int(path.name.split("_", 1)[0]) for path in MIGRATIONS.glob("*.sql"))
