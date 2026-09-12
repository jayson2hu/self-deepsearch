-- +goose Up
CREATE TABLE platform.entity_merges (
    merge_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type text NOT NULL CHECK (entity_type IN ('work', 'performer', 'studio')),
    source_entity_id uuid NOT NULL,
    target_entity_id uuid NOT NULL,
    redirect_id uuid NOT NULL UNIQUE REFERENCES platform.redirects(redirect_id) ON DELETE RESTRICT,
    merged_by uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 2 AND 1000),
    merged_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entity_type, source_entity_id),
    CHECK (source_entity_id <> target_entity_id)
);

CREATE INDEX entity_merges_target_idx
    ON platform.entity_merges (entity_type, target_entity_id, merged_at DESC);

GRANT SELECT, INSERT ON platform.entity_merges TO platform_api;
GRANT DELETE ON platform.work_performers, platform.search_documents, platform.content_metrics_hourly TO platform_api;
UPDATE platform.system_metadata SET schema_version = 12 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 11 WHERE singleton;
REVOKE DELETE ON platform.work_performers, platform.search_documents, platform.content_metrics_hourly FROM platform_api;
REVOKE ALL ON platform.entity_merges FROM platform_api;
DROP TABLE IF EXISTS platform.entity_merges;
