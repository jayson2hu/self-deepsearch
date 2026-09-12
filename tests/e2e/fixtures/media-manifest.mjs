// Synthetic metadata only. No real media or storage requests are involved.
export function mediaManifest() {
  const asset = "22000000-0000-4000-8000-000000000001";
  const entity = "11000000-0000-4000-8000-000000000001";
  return {
    asset_id: asset, entity_type: "work", entity_id: entity, asset_type: "work_image",
    source_type: "manual", source_url: null, checked_at: "2026-09-10T12:00:00Z", confidence: 1,
    purpose: "cover", position: 0, is_primary: true, reason: "Synthetic primary replacement", tool_version: "media-python/1",
    objects: ["master", "w320", "w640", "w960"].map((rendition) => {
      const storage_key = rendition === "master" ? `media-master/${asset}/v1/master.webp` : `media-public/default/works/${entity}/${asset}/v1/cover-${rendition}.webp`;
      return { rendition, storage_scope: rendition === "master" ? "private" : "public", storage_key, backup_path: storage_key,
        public_url: rendition === "master" ? null : `https://media.example.test/${storage_key}`,
        sha256: "a".repeat(64), mime_type: "image/webp", width: rendition === "master" ? 960 : Number(rendition.slice(1)), height: rendition === "master" ? 600 : Number(rendition.slice(1)) * 5 / 8, byte_size: 1200 };
    }),
  };
}
