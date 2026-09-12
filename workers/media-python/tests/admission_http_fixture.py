"""Invoked by the Go test only; real HTTP/files, synthetic SDK and usage state."""
from __future__ import annotations

import json
import os
import sys
import uuid
from pathlib import Path
from unittest.mock import Mock
from urllib.parse import urlsplit
from urllib.request import Request, urlopen

from botocore.exceptions import ClientError

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from media_worker.storage import S3ObjectStore  # noqa: E402
from media_worker.upload_control import UploadControl, UploadControlBlocked, UploadControlConflict  # noqa: E402
from media_worker.upload_guard import UploadRecoveryRequired, upload_status  # noqa: E402


def main() -> None:
    if os.getenv("MEDIA_ADMISSION_CONTRACT_CONFIRM") != "temporary-files-only" or len(sys.argv) != 3:
        raise ValueError("explicit isolated contract required")
    origin = sys.argv[1]
    url = urlsplit(origin)
    if url.scheme != "http" or url.hostname != "127.0.0.1" or url.path or url.query or url.fragment or url.username:
        raise ValueError("contract is loopback only")
    root = Path(sys.argv[2]).resolve()
    lock = root / "state" / "upload.lock"
    os.environ.update(MEDIA_UPLOAD_CONTROL_MODE="enforce", MEDIA_UPLOAD_ADMISSION_MODE="off", MEDIA_DELIVERY_MODE="normal",
                      MEDIA_UPLOAD_ADMISSION_URL=origin, MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP="true")
    control = UploadControl(lock)

    def command(mode: str) -> dict[str, object]:
        state = control.status()
        return {"command_id": str(uuid.uuid4()), "expected_epoch": state["epoch"], "expected_generation": state["generation"],
                "mode": mode, "reason": "cross-language synthetic operator", "resume_confirmed": mode == "enabled"}

    control.apply(command("paused"))
    control.apply(command("enabled"))
    old_resume = command("enabled")
    os.environ["MEDIA_UPLOAD_ADMISSION_MODE"] = "enforce"
    control.require_enabled()
    sdk = Mock()
    sdk.head_object.side_effect = ClientError({"Error": {"Code": "NoSuchKey"}, "ResponseMetadata": {"HTTPStatusCode": 404}}, "HeadObject")
    store = S3ObjectStore(endpoint=None, region="auto", private_bucket="private", public_bucket="public", client=sdk, upload_lock_file=lock)
    try:
        with store.upload_guard():
            store.put("media-master/test/first.webp", b"abc", content_type="image/webp", public=False, sha256="a" * 64)
            store.put("media-public/test/second.webp", b"abc", content_type="image/webp", public=True, sha256="a" * 64)
        raise AssertionError("high usage permitted second PUT")
    except UploadControlBlocked:
        pass
    assert sdk.put_object.call_count == 1
    paused = control.status()
    assert paused["mode"] == "paused" and paused["generation"] == 3
    event = json.loads(control.ledger.read_text().splitlines()[-1])
    assert event["source"] == "usage_admission" and event["decision"]["outcome"] == "denied"
    assert len(event["decision"]["response_hash"]) == 64
    pending = upload_status(lock)["pending"]
    assert len(pending["objects"]) == 1
    try:
        control.apply(old_resume)
        raise AssertionError("stale resume undid automatic pause")
    except UploadControlConflict:
        pass
    try:
        control.apply(command("enabled"))
        raise AssertionError("unsafe explicit resume accepted")
    except UploadControlBlocked:
        pass
    store.delete_and_verify("media-master/test/first.webp")
    sdk.delete_object.assert_called_once()
    assert upload_status(lock)["pending"] == pending
    # This route belongs only to Go's httptest server, never production.
    with urlopen(Request(origin + "/__test/reviewed", method="POST", data=b"{}"), timeout=2) as response:
        assert response.status == 204
    try:
        UploadControl(lock).require_enabled()
        raise AssertionError("fresh usage silently resumed the uploader")
    except UploadControlBlocked:
        pass
    control.apply(command("enabled"))
    assert upload_status(lock)["pending"] == pending
    try:
        with store.upload_guard():
            raise AssertionError("resume cleared uncertain batch")
    except UploadRecoveryRequired:
        pass
    print(json.dumps({"paused_generation": paused["generation"], "puts": sdk.put_object.call_count, "deletes": sdk.delete_object.call_count,
                      "resumed_generation": control.status()["generation"], "pending_preserved": True}))


if __name__ == "__main__":
    main()
