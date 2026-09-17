-- +goose Up
CREATE TABLE platform.ad_campaigns (
    campaign_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    advertiser text NOT NULL CHECK (length(btrim(advertiser)) BETWEEN 1 AND 120),
    published_version_id uuid,
    paused_at timestamptz,
    created_by uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER ad_campaigns_set_updated_at
BEFORE UPDATE ON platform.ad_campaigns
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TABLE platform.ad_campaign_versions (
    version_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id uuid NOT NULL REFERENCES platform.ad_campaigns(campaign_id) ON DELETE RESTRICT,
    version integer NOT NULL CHECK (version > 0),
    version_state text NOT NULL DEFAULT 'draft' CHECK (version_state IN ('draft', 'published')),
    slot_code text NOT NULL CHECK (slot_code ~ '^[a-z][a-z0-9_.]{2,79}$'),
    placement text NOT NULL CHECK (placement IN ('banner', 'rail', 'infeed')),
    page_scope text NOT NULL CHECK (page_scope IN ('home', 'work', 'performer', 'studio', 'latest', 'recent', 'popular')),
    device_scope text NOT NULL CHECK (device_scope IN ('all', 'desktop', 'mobile')),
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 120),
    sponsor text NOT NULL CHECK (length(btrim(sponsor)) BETWEEN 1 AND 120),
    image_url text NOT NULL CHECK (length(image_url) BETWEEN 1 AND 2000),
    image_alt text NOT NULL CHECK (length(image_alt) <= 160),
    target_url text CHECK (target_url IS NULL OR length(target_url) BETWEEN 1 AND 2000),
    width integer NOT NULL CHECK (width > 0),
    height integer NOT NULL CHECK (height > 0),
    priority integer NOT NULL DEFAULT 0 CHECK (priority BETWEEN -1000 AND 1000),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    created_by uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    UNIQUE (campaign_id, version),
    CHECK (ends_at > starts_at),
    CHECK ((version_state = 'draft' AND published_at IS NULL) OR (version_state = 'published' AND published_at IS NOT NULL))
);

ALTER TABLE platform.ad_campaigns
  ADD CONSTRAINT ad_campaigns_published_version_fk
  FOREIGN KEY (published_version_id) REFERENCES platform.ad_campaign_versions(version_id) ON DELETE RESTRICT;

CREATE INDEX ad_campaign_versions_effective_idx
  ON platform.ad_campaign_versions (slot_code, starts_at, ends_at, priority DESC)
  WHERE version_state = 'published';
CREATE INDEX ad_campaign_versions_campaign_idx
  ON platform.ad_campaign_versions (campaign_id, version DESC);

GRANT SELECT, INSERT, UPDATE ON platform.ad_campaigns, platform.ad_campaign_versions TO platform_api;

UPDATE platform.system_metadata SET schema_version = 24 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 23 WHERE singleton;

ALTER TABLE platform.ad_campaigns DROP CONSTRAINT ad_campaigns_published_version_fk;
DROP TABLE platform.ad_campaign_versions;
DROP TRIGGER ad_campaigns_set_updated_at ON platform.ad_campaigns;
DROP TABLE platform.ad_campaigns;
