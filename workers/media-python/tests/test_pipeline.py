from __future__ import annotations

import io
import sys
import tempfile
import threading
import unittest
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from pathlib import Path

from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from media_worker.pipeline import MediaRejected, inspect_image, prepare_manifest  # noqa: E402


class FakeStore:
    def __init__(self, fail_on: str | None = None, usage_bytes: int = 0) -> None:
        self.objects: dict[str, tuple[bytes, str]] = {}
        self.fail_on = fail_on
        self._usage_bytes = usage_bytes
        self._upload_lock = threading.Lock()

    @contextmanager
    def upload_guard(self):
        if not self._upload_lock.acquire(blocking=False):
            raise OSError("media upload is already running")
        try:
            yield
        finally:
            self._upload_lock.release()

    def usage_bytes(self) -> int:
        return self._usage_bytes

    def put(self, key: str, body: bytes, *, content_type: str, public: bool, sha256: str) -> None:
        del content_type, public
        if self.fail_on and self.fail_on in key:
            raise OSError("synthetic upload failure")
        self.objects[key] = (body, sha256)

    def verify(self, key: str, *, byte_size: int, sha256: str) -> None:
        body, stored_hash = self.objects[key]
        if len(body) != byte_size or stored_hash != sha256:
            raise ValueError("verification failed")

    def delete(self, key: str) -> None:
        self.objects.pop(key, None)

    def delete_and_verify(self, key: str) -> None:
        self.delete(key)


def write_image(path: Path, *, size: tuple[int, int] = (1200, 800), image_format: str = "JPEG") -> None:
    image = Image.new("RGB", size, "#8a4d3f")
    if image_format == "JPEG":
        image.save(path, format=image_format, comment=b"metadata-to-strip")
    else:
        image.save(path, format=image_format)
    image.close()


class MediaPipelineTests(unittest.TestCase):
    def test_prepare_rejects_invalid_display_slot_before_decode_backup_or_upload(self) -> None:
        cases = [
            ("work", "cover", False, 0),
            ("work", "cover", True, 1),
            ("work", "gallery", True, 1),
            ("work", "gallery", False, 0),
            ("work", "gallery", False, 4),
            ("performer", "avatar", False, 0),
            ("performer", "avatar", True, 1),
        ]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            missing = root / "not-decoded.jpg"
            for entity_type, purpose, is_primary, position in cases:
                with self.subTest(entity_type=entity_type, purpose=purpose, is_primary=is_primary, position=position):
                    store = FakeStore()
                    with self.assertRaisesRegex(MediaRejected, "valid display slot"):
                        prepare_manifest(
                            input_path=missing, entity_type=entity_type,
                            entity_id="11111111-1111-4111-8111-111111111111", purpose=purpose,
                            source_type="manual", source_url=None, reason="reviewed source",
                            position=position, is_primary=is_primary, confidence=1.0, asset_id=None,
                            site_id="default", backup_root=root / "backup",
                            public_base_url="https://media.example.test", object_store=store,
                        )
                    self.assertEqual(store.objects, {})
                    self.assertFalse((root / "backup").exists())

    def test_capacity_admission_serializes_usage_read_through_last_upload(self) -> None:
        class RacingStore(FakeStore):
            def __init__(self) -> None:
                super().__init__()
                self.read_started = threading.Event()
                self.release_first = threading.Event()
                self.read_count = 0

            def usage_bytes(self) -> int:
                observed = sum(len(body) for body, _ in self.objects.values())
                self.read_count += 1
                if self.read_count == 1:
                    self.read_started.set()
                    if not self.release_first.wait(5):
                        raise TimeoutError("test did not release first usage read")
                return observed

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "candidate.jpg"
            write_image(path, size=(60, 40))

            def prepare(store, output, limit=100000):
                return prepare_manifest(
                    input_path=path, entity_type="work", entity_id="11111111-1111-4111-8111-111111111111",
                    purpose="cover", source_type="manual", source_url=None, reason="reviewed source",
                    position=0, is_primary=True, confidence=1.0, asset_id=None, site_id="default",
                    backup_root=root / output, public_base_url="https://media.example.test", object_store=store,
                    storage_limit_bytes=limit,
                )

            baseline = FakeStore()
            prepare(baseline, "baseline")
            limit = sum(len(body) for body, _ in baseline.objects.values())
            store = RacingStore()
            with ThreadPoolExecutor(max_workers=2) as pool:
                first = pool.submit(prepare, store, "first", limit)
                self.assertTrue(store.read_started.wait(3))
                second = pool.submit(prepare, store, "second", limit)
                succeeded = 0
                try:
                    try:
                        second.result(timeout=3)
                        succeeded += 1
                    except OSError:
                        pass
                finally:
                    store.release_first.set()
                first.result(timeout=3)
                succeeded += 1
            self.assertEqual(succeeded, 1, "two uploads passed the same stale capacity snapshot")
            self.assertLessEqual(sum(len(body) for body, _ in store.objects.values()), limit)

    def test_inspect_decodes_image_instead_of_trusting_extension(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "candidate.bin"
            write_image(path)
            result = inspect_image(path)
        self.assertEqual(result["status"], "decoded")
        self.assertEqual((result["width"], result["height"]), (1200, 800))

    def test_rejects_malformed_jpeg(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "candidate.jpg"
            path.write_bytes(b"not-an-image")
            with self.assertRaises(MediaRejected):
                inspect_image(path)

    def test_rejects_animated_webp(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "candidate.webp"
            frames = [Image.new("RGB", (10, 10), "red"), Image.new("RGB", (10, 10), "blue")]
            frames[0].save(path, format="WEBP", save_all=True, append_images=frames[1:], duration=100, loop=0)
            for frame in frames:
                frame.close()
            with self.assertRaises(MediaRejected):
                inspect_image(path)

    def test_prepare_generates_master_three_derivatives_and_backups(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "candidate.jpg"
            write_image(path)
            store = FakeStore()
            manifest = prepare_manifest(
                input_path=path, entity_type="work", entity_id="11111111-1111-4111-8111-111111111111",
                purpose="cover", source_type="manual", source_url=None,
                reason="manual rights review complete", position=0, is_primary=True, confidence=1.0,
                asset_id="22222222-2222-4222-8222-222222222222", site_id="default",
                backup_root=root / "backup", public_base_url="https://media.example.test", object_store=store,
            )
            objects = manifest["objects"]
            self.assertEqual([item["rendition"] for item in objects], ["master", "w320", "w640", "w960"])
            self.assertIsNone(objects[0]["public_url"])
            self.assertEqual([item["width"] for item in objects[1:]], [320, 640, 960])
            self.assertEqual(len(store.objects), 4)
            for item in objects:
                self.assertTrue((root / "backup" / item["backup_path"]).is_file())
                with Image.open(io.BytesIO(store.objects[item["storage_key"]][0])) as generated:
                    self.assertEqual(generated.format, "WEBP")
                    self.assertFalse(generated.info.get("exif"))

    def test_prepare_rolls_back_on_failure(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "candidate.png"
            write_image(path, image_format="PNG")
            store = FakeStore(fail_on="w640")
            with self.assertRaises(OSError):
                prepare_manifest(
                    input_path=path, entity_type="performer", entity_id="11111111-1111-4111-8111-111111111111",
                    purpose="avatar", source_type="manual", source_url=None, reason="reviewed source",
                    position=0, is_primary=True, confidence=1.0, asset_id=None, site_id="default",
                    backup_root=root / "backup", public_base_url="https://media.example.test", object_store=store,
                )
            self.assertEqual(store.objects, {})
            retained = list((root / "backup").rglob("*.webp"))
            self.assertEqual(len(retained), 1)
            self.assertIn("w640", retained[0].name)

    def test_prepare_refuses_to_overwrite_existing_backup(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "candidate.jpg"
            write_image(path)
            asset_id = "22222222-2222-4222-8222-222222222222"
            existing = root / "backup" / "media-master" / asset_id / "v1" / "master.webp"
            existing.parent.mkdir(parents=True)
            existing.write_bytes(b"existing")
            store = FakeStore()
            with self.assertRaises(MediaRejected):
                prepare_manifest(
                    input_path=path, entity_type="work", entity_id="11111111-1111-4111-8111-111111111111",
                    purpose="cover", source_type="manual", source_url=None, reason="reviewed source",
                    position=0, is_primary=True, confidence=1.0, asset_id=asset_id, site_id="default",
                    backup_root=root / "backup", public_base_url="https://media.example.test", object_store=store,
                )
            self.assertEqual(existing.read_bytes(), b"existing")
            self.assertEqual(store.objects, {})

    def test_prepare_rejects_batch_before_backup_or_upload_when_storage_limit_would_be_exceeded(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "candidate.jpg"
            write_image(path)
            store = FakeStore(usage_bytes=999)
            with self.assertRaisesRegex(MediaRejected, "storage limit would be exceeded"):
                prepare_manifest(
                    input_path=path, entity_type="work", entity_id="11111111-1111-4111-8111-111111111111",
                    purpose="cover", source_type="manual", source_url=None, reason="reviewed source",
                    position=0, is_primary=True, confidence=1.0, asset_id=None, site_id="default",
                    backup_root=root / "backup", public_base_url="https://media.example.test", object_store=store,
                    storage_limit_bytes=1000,
                )
            self.assertEqual(store.objects, {})
            self.assertFalse((root / "backup").exists())


if __name__ == "__main__":
    unittest.main()
