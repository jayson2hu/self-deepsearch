from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


def test_delivery_mode_is_shared_only_by_api_display_and_beijing_media() -> None:
    for filename, services in (
        ("compose.yaml", ("platform-api", "display-web", "media-python")),
        ("infra/compose/japan/compose.yaml", ("platform-api", "display-web")),
        ("infra/compose/beijing/compose.yaml", ("media-python",)),
    ):
        config = yaml.safe_load((ROOT / filename).read_text(encoding="utf-8"))
        for name in services:
            assert config["services"][name]["environment"]["MEDIA_DELIVERY_MODE"] == "${MEDIA_DELIVERY_MODE:-normal}"
    for filename in (".env.example", "infra/compose/japan/.env.example", "infra/compose/beijing/.env.example"):
        assert "MEDIA_DELIVERY_MODE=normal" in (ROOT / filename).read_text(encoding="utf-8").splitlines()


def test_all_public_and_account_catalog_serializers_use_delivery_projection() -> None:
    for filename in ("handlers.go", "performer_handlers.go", "studio_handlers.go", "account_handlers.go"):
        source = (ROOT / "services/platform-api/internal/httpapi" / filename).read_text(encoding="utf-8")
        assert "\twriteJSON(w," not in source
        assert "s.writeMediaJSON(w," in source
    # Stop mode is a projection, not a modification of operator deletion evidence.
    media = (ROOT / "services/platform-api/internal/httpapi/media_handlers.go").read_text(encoding="utf-8")
    assert "writeJSON(w, http.StatusCreated, mediaManifestDTO(item))" in media
