-- +goose Up
CREATE TABLE platform.editorial_recommendations (
    recommendation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    work_id uuid NOT NULL REFERENCES platform.works(work_id) ON DELETE RESTRICT,
    position integer NOT NULL CHECK (position BETWEEN 1 AND 100),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz,
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 2 AND 1000),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'removed')),
    created_by uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX editorial_recommendations_public_idx
    ON platform.editorial_recommendations (status, starts_at, ends_at, position, recommendation_id)
    WHERE status = 'active';

CREATE INDEX editorial_recommendations_work_idx
    ON platform.editorial_recommendations (work_id, status, starts_at DESC);

CREATE TRIGGER editorial_recommendations_set_updated_at
    BEFORE UPDATE ON platform.editorial_recommendations
    FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

GRANT SELECT, INSERT, UPDATE ON platform.editorial_recommendations TO platform_api;
UPDATE platform.system_metadata SET schema_version = 11 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 10 WHERE singleton;
REVOKE ALL ON platform.editorial_recommendations FROM platform_api;
DROP TRIGGER IF EXISTS editorial_recommendations_set_updated_at ON platform.editorial_recommendations;
DROP TABLE IF EXISTS platform.editorial_recommendations;
