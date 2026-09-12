"""Real media HTTP handler with temporary-file substitutes for S3 and purge."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import sys
from http.server import ThreadingHTTPServer
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from media_worker.server import DeleteService, ReconcileService, handler  # noqa: E402
from media_worker.upload_control import UploadControl  # noqa: E402


class FileStoreFixture:
    def __init__(self, root: Path, *, enable_reconciliation: bool = False) -> None:
        self.root = root.resolve()
        self.enable_reconciliation = enable_reconciliation

    def delete_and_verify(self, key: str) -> None:
        target = (self.root / key).resolve()
        if self.root not in target.parents:
            raise ValueError("fixture path is outside its root")
        target.unlink(missing_ok=True)
        if target.exists():
            raise OSError("fixture object still exists")

    def inspect(self, key: str) -> tuple[int, str] | None:
        if not self.enable_reconciliation:
            raise AssertionError("reconciliation is outside this deletion contract")
        if (self.root.parent / "reconcile-fails").exists():
            raise OSError("intentional fixture inspection failure")
        target = (self.root / key).resolve()
        if self.root not in target.parents:
            raise ValueError("fixture path is outside its root")
        if not target.is_file():
            return None
        body = target.read_bytes()
        return len(body), hashlib.sha256(body).hexdigest()

    def list_media_keys(self) -> list[tuple[str, str]]:
        if not self.enable_reconciliation:
            raise AssertionError("reconciliation is outside this deletion contract")
        result = []
        for scope, prefix in (("private", "media-master"), ("public", "media-public")):
            for path in (self.root / prefix).rglob("*.webp"):
                if self.root not in path.resolve().parents:
                    raise ValueError("fixture path is outside its root")
                if path.is_file():
                    result.append((scope, path.relative_to(self.root).as_posix()))
        return result


class PurgerFixture:
    def __init__(self, root: Path) -> None:
        self.root = root

    def purge(self, public_url: str) -> None:
        if (self.root / "purge-fails").exists():
            raise OSError("intentional fixture purge failure")
        (self.root / "purge-completed").write_text("completed\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--enable-reconciliation", action="store_true")
    parser.add_argument("--enable-upload-control", action="store_true")
    args = parser.parse_args()
    if os.environ.get("MEDIA_DELETE_CONTRACT_CONFIRM") != "temporary-files-only":
        raise SystemExit("this fixture requires a temporary-files-only confirmation")
    root = args.root.resolve(strict=True)
    store = FileStoreFixture(root / "objects", enable_reconciliation=args.enable_reconciliation)
    service = DeleteService(store=store, backup_root=root / "backups", purger=PurgerFixture(root))
    reconcile = ReconcileService(store=store, backup_root=root / "backups")
    server = ThreadingHTTPServer(
        ("127.0.0.1", 0), handler(service, reconcile, os.environ["MEDIA_HMAC_SECRET"],
                                 upload_control=UploadControl(root / "state" / "upload.lock") if args.enable_upload_control else None,
                                 control_secret=os.environ.get("MEDIA_UPLOAD_CONTROL_SECRET", "")),
    )
    print(json.dumps({"port": server.server_port}), flush=True)
    try:
        server.serve_forever()
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
