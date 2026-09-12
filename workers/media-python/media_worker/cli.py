from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

from botocore.exceptions import BotoCoreError, ClientError

from .pipeline import DEFAULT_STORAGE_LIMIT_BYTES, MediaRejected, inspect_image, prepare_manifest
from .server import CloudflarePurger, DeleteService, ReconcileService, serve
from .storage import S3ObjectStore
from .upload_control import UploadControlBlocked, configured_control
from .upload_guard import UploadBusy, UploadRecoveryRequired, acknowledge_upload, upload_status, validate_lock_path


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Release A controlled media processor")
    subcommands = parser.add_subparsers(dest="command", required=True)
    inspect_parser = subcommands.add_parser("inspect", help="decode and validate one local image")
    inspect_parser.add_argument("--input", type=Path, required=True)
    prepare = subcommands.add_parser("prepare", help="create derivatives, upload them and emit an API manifest")
    prepare.add_argument("--input", type=Path, required=True)
    prepare.add_argument("--entity-type", choices=("work", "performer"), required=True)
    prepare.add_argument("--entity-id", required=True)
    prepare.add_argument("--purpose", choices=("cover", "gallery", "avatar"), required=True)
    prepare.add_argument("--source-type", default="manual")
    prepare.add_argument("--source-url")
    prepare.add_argument("--reason", required=True)
    prepare.add_argument("--position", type=int, default=0)
    prepare.add_argument("--primary", action="store_true")
    prepare.add_argument("--confidence", type=float, default=1.0)
    prepare.add_argument("--asset-id")
    prepare.add_argument("--backup-root", type=Path, required=True)
    prepare.add_argument("--output", type=Path, required=True)
    prepare.add_argument("--site-id", default="default")
    prepare.add_argument("--endpoint", default=os.getenv("S3_ENDPOINT_URL"))
    prepare.add_argument("--region", default=os.getenv("S3_REGION", "auto"))
    prepare.add_argument("--private-bucket", default=os.getenv("S3_PRIVATE_BUCKET"))
    prepare.add_argument("--public-bucket", default=os.getenv("S3_PUBLIC_BUCKET"))
    prepare.add_argument("--public-base-url", default=os.getenv("S3_PUBLIC_BASE_URL"))
    prepare.add_argument("--upload-lock-file", type=Path, default=os.getenv("MEDIA_UPLOAD_LOCK_FILE"),
                         help="absolute lock path shared by every uploader on the single Beijing writer host")
    prepare.add_argument(
        "--storage-limit-bytes", type=int,
        default=os.getenv("MEDIA_STORAGE_LIMIT_BYTES", str(DEFAULT_STORAGE_LIMIT_BYTES)),
        help="reject the entire batch before upload when private plus public media would exceed this limit",
    )
    server = subcommands.add_parser("serve", help="run the private signed media deletion service")
    server.add_argument("--host", default=os.getenv("MEDIA_HOST", "0.0.0.0"))
    server.add_argument("--port", type=int, default=int(os.getenv("MEDIA_PORT", "8090")))
    server.add_argument("--backup-root", type=Path, default=Path(os.getenv("MEDIA_BACKUP_ROOT", "/var/lib/self-deepsearch/media")))
    server.add_argument("--endpoint", default=os.getenv("S3_ENDPOINT_URL"))
    server.add_argument("--region", default=os.getenv("S3_REGION", "auto"))
    server.add_argument("--private-bucket", default=os.getenv("S3_PRIVATE_BUCKET"))
    server.add_argument("--public-bucket", default=os.getenv("S3_PUBLIC_BUCKET"))
    server.add_argument("--secret", default=os.getenv("MEDIA_HMAC_SECRET"))
    server.add_argument("--cloudflare-zone-id", default=os.getenv("CLOUDFLARE_ZONE_ID"))
    server.add_argument("--cloudflare-api-token", default=os.getenv("CLOUDFLARE_API_TOKEN"))
    server.add_argument("--upload-lock-file", type=Path, default=os.getenv("MEDIA_UPLOAD_LOCK_FILE"))
    status = subcommands.add_parser("upload-status", help="inspect the local pending upload fence; does not contact S3")
    status.add_argument("--upload-lock-file", type=Path, default=os.getenv("MEDIA_UPLOAD_LOCK_FILE"))
    acknowledge = subcommands.add_parser("acknowledge-upload", help="acknowledge a manually reconciled pending batch; does not verify or modify S3")
    acknowledge.add_argument("--upload-lock-file", type=Path, default=os.getenv("MEDIA_UPLOAD_LOCK_FILE"))
    acknowledge.add_argument("--run-id", required=True)
    acknowledge.add_argument("--reason", required=True)
    acknowledge.add_argument("--confirm-storage-reconciled", action="store_true")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    try:
        if args.command == "inspect":
            result = inspect_image(args.input)
        elif args.command == "prepare":
            # Check before SDK construction, decoding, listing or local writes.
            mode = os.getenv("MEDIA_DELIVERY_MODE", "normal")
            if mode != "normal":
                raise MediaRejected("MEDIA_DELIVERY_MODE disables new image preparation; inspection, deletion and recovery remain available")
            lock_file = validate_lock_path(args.upload_lock_file)
            control = configured_control(lock_file)
            if control is not None:
                control.require_enabled()
            if not args.private_bucket or not args.public_bucket or not args.public_base_url:
                raise MediaRejected("private/public S3 buckets and public base URL are required")
            if args.output.exists():
                raise MediaRejected("output manifest already exists")
            result = prepare_manifest(
                input_path=args.input, entity_type=args.entity_type, entity_id=args.entity_id,
                purpose=args.purpose, source_type=args.source_type, source_url=args.source_url,
                reason=args.reason, position=args.position, is_primary=args.primary,
                confidence=args.confidence, asset_id=args.asset_id, site_id=args.site_id,
                backup_root=args.backup_root, public_base_url=args.public_base_url,
                object_store=S3ObjectStore(endpoint=args.endpoint, region=args.region, private_bucket=args.private_bucket,
                                           public_bucket=args.public_bucket, upload_lock_file=lock_file),
                storage_limit_bytes=args.storage_limit_bytes,
                manifest_path=args.output,
            )
        elif args.command == "upload-status":
            result = upload_status(validate_lock_path(args.upload_lock_file))
        elif args.command == "acknowledge-upload":
            result = acknowledge_upload(validate_lock_path(args.upload_lock_file), expected_run_id=args.run_id,
                                        reason=args.reason, confirmed=args.confirm_storage_reconciled)
        else:
            control = configured_control(args.upload_lock_file)
            if not args.private_bucket or not args.public_bucket or not args.secret or not args.cloudflare_zone_id or not args.cloudflare_api_token:
                raise MediaRejected("S3 buckets, media HMAC secret and Cloudflare purge credentials are required")
            store = S3ObjectStore(endpoint=args.endpoint, region=args.region, private_bucket=args.private_bucket, public_bucket=args.public_bucket,
                                  upload_lock_file=args.upload_lock_file)
            purger = CloudflarePurger(zone_id=args.cloudflare_zone_id, api_token=args.cloudflare_api_token)
            serve(host=args.host, port=args.port,
                  service=DeleteService(store=store, backup_root=args.backup_root, purger=purger),
                  reconcile_service=ReconcileService(store=store, backup_root=args.backup_root), secret=args.secret,
                  upload_control=control, control_secret=os.getenv("MEDIA_UPLOAD_CONTROL_SECRET", ""))
            return 0
        print(json.dumps(result, ensure_ascii=False))
        return 0
    except (BotoCoreError, ClientError):
        # SDK errors can embed endpoints, request identifiers or credentials.
        # A failed PUT remains fenced; callers should inspect local status.
        print(json.dumps({"level": "error", "code": "MEDIA_STORAGE_UNAVAILABLE",
                          "message": "storage request failed; inspect upload-status before retrying"}), file=sys.stderr)
        return 2
    except (OSError, ValueError, MediaRejected) as exc:
        code = "MEDIA_UPLOAD_BUSY" if isinstance(exc, UploadBusy) else "MEDIA_UPLOAD_RECOVERY_REQUIRED" if isinstance(exc, UploadRecoveryRequired) else "MEDIA_REJECTED"
        if isinstance(exc, UploadControlBlocked):
            code = "MEDIA_UPLOAD_CONTROL_BLOCKED"
        print(json.dumps({"level": "error", "code": code, "message": str(exc)}, ensure_ascii=False), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
