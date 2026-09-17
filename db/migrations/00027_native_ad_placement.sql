-- +goose Up
ALTER TABLE platform.ad_campaign_versions
  DROP CONSTRAINT ad_campaign_versions_placement_check;

ALTER TABLE platform.ad_campaign_versions
  ADD CONSTRAINT ad_campaign_versions_placement_check
  CHECK (placement IN ('banner', 'rail', 'infeed', 'native'));

UPDATE platform.system_metadata SET schema_version = 27 WHERE singleton;

-- +goose Down
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM platform.ad_campaign_versions WHERE placement = 'native') THEN
    RAISE EXCEPTION 'cannot remove native ad placement while native campaign versions exist';
  END IF;
END
$$;

ALTER TABLE platform.ad_campaign_versions
  DROP CONSTRAINT ad_campaign_versions_placement_check;

ALTER TABLE platform.ad_campaign_versions
  ADD CONSTRAINT ad_campaign_versions_placement_check
  CHECK (placement IN ('banner', 'rail', 'infeed'));

UPDATE platform.system_metadata SET schema_version = 26 WHERE singleton;
