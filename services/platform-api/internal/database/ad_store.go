package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

const adColumns = `
campaign.campaign_id::text, campaign.name, campaign.advertiser,
version.version_id::text, version.version, version.version_state,
CASE
  WHEN campaign.paused_at IS NOT NULL THEN 'paused'
  WHEN version.version_state = 'draft' THEN 'draft'
  WHEN version.starts_at > $1 THEN 'scheduled'
  WHEN version.ends_at <= $1 THEN 'expired'
  ELSE 'live'
END,
version.slot_code, version.placement, version.page_scope, version.device_scope,
version.title, version.sponsor, version.image_url, version.image_alt, version.target_url,
version.width, version.height, version.priority, version.starts_at, version.ends_at,
version.published_at, campaign.paused_at, campaign.created_at, campaign.updated_at`

type adScanner interface{ Scan(...any) error }

func scanAd(row adScanner) (operations.AdCampaign, error) {
	var item operations.AdCampaign
	err := row.Scan(
		&item.ID, &item.Name, &item.Advertiser, &item.VersionID, &item.Version, &item.VersionState, &item.Status,
		&item.SlotCode, &item.Placement, &item.PageScope, &item.DeviceScope, &item.Title, &item.Sponsor,
		&item.ImageURL, &item.ImageAlt, &item.TargetURL, &item.Width, &item.Height, &item.Priority,
		&item.StartsAt, &item.EndsAt, &item.PublishedAt, &item.PausedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (store *Store) ListAdCampaigns(ctx context.Context, limit int) ([]operations.AdCampaign, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	now := time.Now().UTC()
	rows, err := store.pool.Query(ctx, `
SELECT `+adColumns+`
FROM platform.ad_campaigns campaign
JOIN LATERAL (
  SELECT candidate.* FROM platform.ad_campaign_versions candidate
  WHERE candidate.campaign_id = campaign.campaign_id
  ORDER BY candidate.version DESC LIMIT 1
) version ON true
ORDER BY campaign.updated_at DESC, campaign.campaign_id
LIMIT $2`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list ad campaigns: %w", err)
	}
	defer rows.Close()
	items := make([]operations.AdCampaign, 0)
	for rows.Next() {
		item, err := scanAd(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ad campaign: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) CreateAdCampaign(ctx context.Context, input operations.AdCampaignInput, actorID, requestID string, now time.Time) (operations.AdCampaign, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.AdCampaign{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var campaignID string
	if err := tx.QueryRow(ctx, `
INSERT INTO platform.ad_campaigns (name, advertiser, created_by, created_at, updated_at)
VALUES ($1, $2, $3::uuid, $4, $4) RETURNING campaign_id::text`, input.Name, input.Advertiser, actorID, now).Scan(&campaignID); err != nil {
		return operations.AdCampaign{}, fmt.Errorf("create ad campaign: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.ad_campaign_versions (
  campaign_id, version, slot_code, placement, page_scope, device_scope,
  title, sponsor, image_url, image_alt, target_url, width, height, priority,
  starts_at, ends_at, created_by, created_at
) VALUES ($1::uuid, 1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::uuid, $17)`,
		campaignID, input.SlotCode, input.Placement, input.PageScope, input.DeviceScope,
		input.Title, input.Sponsor, input.ImageURL, input.ImageAlt, input.TargetURL, input.Width, input.Height,
		input.Priority, input.StartsAt, input.EndsAt, actorID, now); err != nil {
		return operations.AdCampaign{}, mapConstraintError(err)
	}
	if err := insertAudit(ctx, tx, actorID, "ad.create", "ad_campaign", campaignID, nil,
		map[string]any{"version": 1, "slot": input.SlotCode}, "create ad draft", requestID, now); err != nil {
		return operations.AdCampaign{}, err
	}
	item, err := loadAdCampaign(ctx, tx, campaignID, now)
	if err != nil {
		return operations.AdCampaign{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.AdCampaign{}, err
	}
	return item, nil
}

func (store *Store) UpdateAdCampaign(ctx context.Context, campaignID string, expectedVersion int, input operations.AdCampaignInput, actorID, requestID string, now time.Time) (operations.AdCampaign, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.AdCampaign{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var latestVersion int
	var latestState string
	if err := tx.QueryRow(ctx, `
SELECT version, version_state FROM platform.ad_campaign_versions
WHERE campaign_id = $1::uuid ORDER BY version DESC LIMIT 1 FOR UPDATE`, campaignID).Scan(&latestVersion, &latestState); errors.Is(err, pgx.ErrNoRows) {
		return operations.AdCampaign{}, operations.ErrNotFound
	} else if err != nil {
		return operations.AdCampaign{}, err
	}
	if latestVersion != expectedVersion {
		return operations.AdCampaign{}, operations.ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.ad_campaigns SET name=$2, advertiser=$3, updated_at=$4 WHERE campaign_id=$1::uuid`, campaignID, input.Name, input.Advertiser, now); err != nil {
		return operations.AdCampaign{}, mapConstraintError(err)
	}
	if latestState == "draft" {
		_, err = tx.Exec(ctx, `
UPDATE platform.ad_campaign_versions SET
  slot_code=$3, placement=$4, page_scope=$5, device_scope=$6, title=$7, sponsor=$8,
  image_url=$9, image_alt=$10, target_url=$11, width=$12, height=$13, priority=$14,
  starts_at=$15, ends_at=$16
WHERE campaign_id=$1::uuid AND version=$2 AND version_state='draft'`,
			campaignID, latestVersion, input.SlotCode, input.Placement, input.PageScope, input.DeviceScope,
			input.Title, input.Sponsor, input.ImageURL, input.ImageAlt, input.TargetURL, input.Width, input.Height,
			input.Priority, input.StartsAt, input.EndsAt)
	} else {
		latestVersion++
		_, err = tx.Exec(ctx, `
INSERT INTO platform.ad_campaign_versions (
  campaign_id, version, slot_code, placement, page_scope, device_scope,
  title, sponsor, image_url, image_alt, target_url, width, height, priority,
  starts_at, ends_at, created_by, created_at
) VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::uuid,$18)`,
			campaignID, latestVersion, input.SlotCode, input.Placement, input.PageScope, input.DeviceScope,
			input.Title, input.Sponsor, input.ImageURL, input.ImageAlt, input.TargetURL, input.Width, input.Height,
			input.Priority, input.StartsAt, input.EndsAt, actorID, now)
	}
	if err != nil {
		return operations.AdCampaign{}, mapConstraintError(err)
	}
	if err := insertAudit(ctx, tx, actorID, "ad.draft_update", "ad_campaign", campaignID,
		map[string]any{"expected_version": expectedVersion}, map[string]any{"version": latestVersion, "slot": input.SlotCode},
		"update ad draft", requestID, now); err != nil {
		return operations.AdCampaign{}, err
	}
	item, err := loadAdCampaign(ctx, tx, campaignID, now)
	if err != nil {
		return operations.AdCampaign{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.AdCampaign{}, err
	}
	return item, nil
}

func (store *Store) PublishAdCampaign(ctx context.Context, campaignID string, expectedVersion int, actorID, requestID string, now time.Time) (operations.AdCampaign, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.AdCampaign{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var versionID, state string
	var version int
	if err := tx.QueryRow(ctx, `
SELECT version_id::text, version, version_state FROM platform.ad_campaign_versions
WHERE campaign_id=$1::uuid ORDER BY version DESC LIMIT 1 FOR UPDATE`, campaignID).Scan(&versionID, &version, &state); errors.Is(err, pgx.ErrNoRows) {
		return operations.AdCampaign{}, operations.ErrNotFound
	} else if err != nil {
		return operations.AdCampaign{}, err
	}
	if version != expectedVersion {
		return operations.AdCampaign{}, operations.ErrConflict
	}
	if state == "draft" {
		if _, err := tx.Exec(ctx, `UPDATE platform.ad_campaign_versions SET version_state='published', published_at=$2 WHERE version_id=$1::uuid`, versionID, now); err != nil {
			return operations.AdCampaign{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.ad_campaigns SET published_version_id=$2::uuid, paused_at=NULL, updated_at=$3 WHERE campaign_id=$1::uuid`, campaignID, versionID, now); err != nil {
		return operations.AdCampaign{}, err
	}
	if state == "draft" {
		if err := insertAudit(ctx, tx, actorID, "ad.publish", "ad_campaign", campaignID,
			map[string]any{"version": version, "state": "draft"}, map[string]any{"version": version, "state": "published"},
			"publish ad version", requestID, now); err != nil {
			return operations.AdCampaign{}, err
		}
	}
	item, err := loadAdCampaign(ctx, tx, campaignID, now)
	if err != nil {
		return operations.AdCampaign{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.AdCampaign{}, err
	}
	return item, nil
}

func (store *Store) PauseAdCampaign(ctx context.Context, campaignID, actorID, requestID string, now time.Time) (operations.AdCampaign, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.AdCampaign{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var wasPaused bool
	err = tx.QueryRow(ctx, `
UPDATE platform.ad_campaigns SET paused_at=coalesce(paused_at,$2), updated_at=$2
WHERE campaign_id=$1::uuid AND published_version_id IS NOT NULL
RETURNING paused_at < $2`, campaignID, now).Scan(&wasPaused)
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.AdCampaign{}, operations.ErrConflict
	}
	if err != nil {
		return operations.AdCampaign{}, err
	}
	if !wasPaused {
		if err := insertAudit(ctx, tx, actorID, "ad.pause", "ad_campaign", campaignID, nil,
			map[string]any{"paused_at": now}, "emergency pause", requestID, now); err != nil {
			return operations.AdCampaign{}, err
		}
	}
	item, err := loadAdCampaign(ctx, tx, campaignID, now)
	if err != nil {
		return operations.AdCampaign{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.AdCampaign{}, err
	}
	return item, nil
}

func (store *Store) EffectiveAd(ctx context.Context, slotCode, placement string, now time.Time) (operations.AdCampaign, error) {
	row := store.pool.QueryRow(ctx, `
SELECT `+adColumns+`
FROM platform.ad_campaigns campaign
JOIN platform.ad_campaign_versions version ON version.version_id=campaign.published_version_id
WHERE campaign.paused_at IS NULL AND version.slot_code=$2 AND version.placement=$3
  AND version.version_state='published' AND version.starts_at <= $1 AND version.ends_at > $1
ORDER BY version.priority DESC, version.published_at DESC, campaign.campaign_id ASC
LIMIT 1`, now, slotCode, placement)
	item, err := scanAd(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.AdCampaign{}, operations.ErrNotFound
	}
	if err != nil {
		return operations.AdCampaign{}, fmt.Errorf("resolve effective ad: %w", err)
	}
	return item, nil
}

func loadAdCampaign(ctx context.Context, tx pgx.Tx, campaignID string, now time.Time) (operations.AdCampaign, error) {
	item, err := scanAd(tx.QueryRow(ctx, `
SELECT `+adColumns+`
FROM platform.ad_campaigns campaign
JOIN LATERAL (
  SELECT candidate.* FROM platform.ad_campaign_versions candidate
  WHERE candidate.campaign_id=campaign.campaign_id ORDER BY candidate.version DESC LIMIT 1
) version ON true
WHERE campaign.campaign_id=$2::uuid`, now, campaignID))
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.AdCampaign{}, operations.ErrNotFound
	}
	return item, err
}
