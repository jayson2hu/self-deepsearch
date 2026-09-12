package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"self-deepsearch/services/platform-api/internal/catalog"
	"self-deepsearch/services/platform-api/internal/observability"
)

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	configuration, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	configuration.MaxConns = 10
	configuration.MinConns = 1
	configuration.MaxConnLifetime = 30 * time.Minute
	configuration.MaxConnIdleTime = 5 * time.Minute
	configuration.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (store *Store) Close() {
	store.pool.Close()
}

func (store *Store) Ping(ctx context.Context) error {
	if err := store.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

func (store *Store) DatabasePoolStats() observability.DatabasePoolStats {
	stats := store.pool.Stat()
	return observability.DatabasePoolStats{
		AcquiredConnections:  stats.AcquiredConns(),
		IdleConnections:      stats.IdleConns(),
		TotalConnections:     stats.TotalConns(),
		MaxConnections:       stats.MaxConns(),
		AcquireCount:         stats.AcquireCount(),
		EmptyAcquireCount:    stats.EmptyAcquireCount(),
		CanceledAcquireCount: stats.CanceledAcquireCount(),
	}
}

func (store *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := store.pool.QueryRow(ctx, `
SELECT schema_version
FROM platform.system_metadata
WHERE singleton`).Scan(&version); err != nil {
		return 0, fmt.Errorf("read database schema version: %w", err)
	}
	return version, nil
}

const workSummaryColumns = `
    work.work_id::text,
    work.canonical_code,
    work.title,
    work.release_date,
    coalesce(studio.name, ''),
    work.canonical_slug,
    media.renditions`

const workSummaryJoins = `
LEFT JOIN platform.public_published_studios AS studio
  ON studio.studio_id = work.studio_id
LEFT JOIN LATERAL (
    SELECT jsonb_agg(
      jsonb_build_object(
        'url', public_url,
        'rendition', rendition,
        'width', width,
        'height', height,
        'mime_type', mime_type
      )
      ORDER BY CASE rendition WHEN 'w320' THEN 1 WHEN 'w640' THEN 2 WHEN 'w960' THEN 3 ELSE 4 END
    ) AS renditions
    FROM (
      SELECT DISTINCT ON (rendition) public_url, rendition, width, height, mime_type
      FROM platform.public_entity_media
      WHERE entity_type = 'work'
        AND entity_id = work.work_id
        AND purpose = 'cover'
        AND is_primary
        AND position = 0
      ORDER BY rendition, public_url
    ) AS public_rendition
) AS media ON true`

const performerSummaryColumns = `
    performer.performer_id::text,
    performer.display_name,
    performer.canonical_slug,
    media.renditions`

const performerSummaryJoins = `
LEFT JOIN LATERAL (
    SELECT jsonb_agg(
      jsonb_build_object(
        'url', public_url,
        'rendition', rendition,
        'width', width,
        'height', height,
        'mime_type', mime_type
      )
      ORDER BY CASE rendition WHEN 'w320' THEN 1 WHEN 'w640' THEN 2 WHEN 'w960' THEN 3 ELSE 4 END
    ) AS renditions
    FROM (
      SELECT DISTINCT ON (rendition) public_url, rendition, width, height, mime_type
      FROM platform.public_entity_media
      WHERE entity_type = 'performer'
        AND entity_id = performer.performer_id
        AND purpose = 'avatar'
        AND is_primary
        AND position = 0
      ORDER BY rendition, public_url
    ) AS public_rendition
) AS media ON true`

func (store *Store) LatestWorks(ctx context.Context, afterID string, limit int) ([]catalog.WorkSummary, error) {
	query := `SELECT ` + workSummaryColumns + `
FROM platform.public_published_works AS work
` + workSummaryJoins + `
WHERE work.release_date IS NOT NULL
  AND ($1 = '' OR (work.release_date, work.published_at, work.work_id) < (
    SELECT cursor.release_date, cursor.published_at, cursor.work_id
    FROM platform.public_published_works AS cursor WHERE cursor.work_id = nullif($1, '')::uuid
  ))
ORDER BY work.release_date DESC NULLS LAST, work.published_at DESC, work.work_id DESC
LIMIT $2`
	return store.queryWorkSummaries(ctx, query, afterID, limit)
}

func (store *Store) RecentlyAddedWorks(ctx context.Context, afterID string, limit int) ([]catalog.WorkSummary, error) {
	query := `SELECT ` + workSummaryColumns + `
FROM platform.public_published_works AS work
` + workSummaryJoins + `
WHERE $1 = '' OR (work.published_at, work.work_id) < (
    SELECT cursor.published_at, cursor.work_id
    FROM platform.public_published_works AS cursor WHERE cursor.work_id = nullif($1, '')::uuid
)
ORDER BY work.published_at DESC, work.work_id DESC
LIMIT $2`
	return store.queryWorkSummaries(ctx, query, afterID, limit)
}

func (store *Store) EditorialWorks(ctx context.Context, limit int) ([]catalog.WorkSummary, error) {
	query := `SELECT ` + workSummaryColumns + `
FROM platform.editorial_recommendations AS recommendation
JOIN platform.public_published_works AS work ON work.work_id = recommendation.work_id
` + workSummaryJoins + `
WHERE recommendation.status = 'active'
  AND recommendation.starts_at <= now()
  AND (recommendation.ends_at IS NULL OR recommendation.ends_at > now())
ORDER BY recommendation.position, recommendation.starts_at DESC, recommendation.recommendation_id
LIMIT $1`
	return store.queryWorkSummaries(ctx, query, limit)
}

func (store *Store) SitemapEntries(ctx context.Context, entityType string, limit int) ([]catalog.SitemapEntry, error) {
	rows, err := store.pool.Query(ctx, `
SELECT entity_type, canonical_slug, updated_at FROM (
  SELECT 'work'::text AS entity_type, canonical_slug, updated_at FROM platform.public_published_works
  UNION ALL
  SELECT 'performer'::text, canonical_slug, updated_at FROM platform.public_published_performers
  UNION ALL
  SELECT 'studio'::text, canonical_slug, updated_at FROM platform.public_published_studios
) entries
WHERE $1 = 'all' OR entity_type = $1
ORDER BY updated_at DESC, entity_type, canonical_slug
LIMIT $2`, entityType, limit)
	if err != nil {
		return nil, fmt.Errorf("query sitemap entries: %w", err)
	}
	defer rows.Close()
	items := make([]catalog.SitemapEntry, 0)
	for rows.Next() {
		var item catalog.SitemapEntry
		if err := rows.Scan(&item.EntityType, &item.Slug, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan sitemap entry: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) RedirectTarget(ctx context.Context, sourcePath string) (catalog.RedirectTarget, error) {
	var target catalog.RedirectTarget
	err := store.pool.QueryRow(ctx, `
SELECT target_path, status_code
FROM platform.redirects
WHERE source_path = $1 AND active`, sourcePath).Scan(&target.TargetPath, &target.StatusCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.RedirectTarget{}, catalog.ErrNotFound
	}
	if err != nil {
		return catalog.RedirectTarget{}, fmt.Errorf("query public redirect: %w", err)
	}
	return target, nil
}

func (store *Store) LatestPerformers(ctx context.Context, limit int) ([]catalog.PerformerSummary, error) {
	rows, err := store.pool.Query(ctx, `SELECT `+performerSummaryColumns+`
FROM platform.public_published_performers AS performer
`+performerSummaryJoins+`
ORDER BY performer.published_at DESC, performer.performer_id
LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("query latest performers: %w", err)
	}
	defer rows.Close()

	items := make([]catalog.PerformerSummary, 0, limit)
	for rows.Next() {
		item, err := scanPerformerSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan latest performer: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest performers: %w", err)
	}
	return items, nil
}

func (store *Store) DiscoveryMixRule(ctx context.Context) (catalog.DiscoveryMixRule, error) {
	var rule catalog.DiscoveryMixRule
	err := store.pool.QueryRow(ctx, `
SELECT (value->>'work_slots')::integer,
       (value->>'performer_slots')::integer,
       (value->>'repeat_window')::integer
FROM platform.site_content_rules
WHERE rule_key = 'home.discovery_mix' AND enabled`).Scan(
		&rule.WorkSlots, &rule.PerformerSlots, &rule.RepeatWindow,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.DiscoveryMixRule{WorkSlots: 4, PerformerSlots: 1, RepeatWindow: 10}, nil
	}
	if err != nil {
		return catalog.DiscoveryMixRule{}, fmt.Errorf("query discovery mix rule: %w", err)
	}
	if rule.WorkSlots < 1 || rule.PerformerSlots < 1 || rule.RepeatWindow < 2 {
		return catalog.DiscoveryMixRule{}, fmt.Errorf("query discovery mix rule: invalid persisted value")
	}
	return rule, nil
}

func (store *Store) TrendingWorks(ctx context.Context, window time.Duration, afterID string, limit int) ([]catalog.WorkSummary, error) {
	query := `WITH ranked AS (
SELECT work.*, metric.views,
       CASE WHEN work.release_date IS NULL THEN 0 ELSE -extract(epoch FROM work.release_date) END AS release_sort
FROM platform.public_published_works AS work
JOIN (
    SELECT content_id, sum(page_views) AS views
    FROM platform.content_metrics_hourly
    WHERE content_type = 'work' AND hour_bucket >= now() - $1::interval
    GROUP BY content_id
) AS metric ON metric.content_id = work.work_id
), cursor_anchor AS (
  SELECT views, release_date IS NULL AS release_is_null, release_sort, work_id
  FROM ranked WHERE work_id = nullif($2, '')::uuid
)
SELECT ` + workSummaryColumns + ` FROM ranked AS work
` + workSummaryJoins + `
WHERE $2 = '' OR (-work.views, work.release_date IS NULL, work.release_sort, work.work_id) > (
  SELECT -views, release_is_null, release_sort, work_id FROM cursor_anchor
)
ORDER BY work.views DESC, work.release_date DESC NULLS LAST, work.work_id
LIMIT $3`
	return store.queryWorkSummaries(ctx, query, intervalLiteral(window), afterID, limit)
}

func (store *Store) MostViewedWorks(ctx context.Context, window *time.Duration, afterID string, limit int) ([]catalog.WorkSummary, error) {
	query := `WITH ranked AS (
SELECT work.*, metric.views,
       CASE WHEN work.release_date IS NULL THEN 0 ELSE -extract(epoch FROM work.release_date) END AS release_sort
FROM platform.public_published_works AS work
JOIN (
    SELECT content_id, sum(page_views) AS views
    FROM platform.content_metrics_hourly
    WHERE content_type = 'work'
      AND ($1::interval IS NULL OR hour_bucket >= now() - $1::interval)
    GROUP BY content_id
) AS metric ON metric.content_id = work.work_id
), cursor_anchor AS (
  SELECT views, release_date IS NULL AS release_is_null, release_sort, work_id
  FROM ranked WHERE work_id = nullif($2, '')::uuid
)
SELECT ` + workSummaryColumns + ` FROM ranked AS work
` + workSummaryJoins + `
WHERE $2 = '' OR (-work.views, work.release_date IS NULL, work.release_sort, work.work_id) > (
  SELECT -views, release_is_null, release_sort, work_id FROM cursor_anchor
)
ORDER BY work.views DESC, work.release_date DESC NULLS LAST, work.work_id
LIMIT $3`
	var interval any
	if window != nil {
		interval = intervalLiteral(*window)
	}
	return store.queryWorkSummaries(ctx, query, interval, afterID, limit)
}

func (store *Store) SearchWorks(ctx context.Context, queryText, compactCode, afterID string, limit int) ([]catalog.WorkSummary, error) {
	query := `WITH ranked AS (
SELECT work.*,
    CASE
      WHEN lower(document.canonical_code) = lower($1) THEN 0
      WHEN document.compact_code = $2 THEN 1
      WHEN document.compact_code LIKE $2 || '%' THEN 2
      WHEN document.search_text ILIKE '%' || $1 || '%' THEN 3
      ELSE 4
    END AS search_rank,
    similarity(document.search_text, $1) AS search_similarity,
    CASE WHEN work.release_date IS NULL THEN 0 ELSE -extract(epoch FROM work.release_date) END AS search_release
FROM platform.public_published_works AS work
JOIN platform.public_search_documents AS document
  ON document.entity_type = 'work' AND document.entity_id = work.work_id
WHERE lower(document.canonical_code) = lower($1)
   OR document.compact_code = $2
   OR document.compact_code LIKE $2 || '%'
   OR document.search_vector @@ plainto_tsquery('simple'::regconfig, $1)
   OR document.search_text ILIKE '%' || $1 || '%'
   OR similarity(document.search_text, $1) >= 0.15
), cursor_anchor AS (
    SELECT search_rank, search_similarity, release_date IS NULL AS release_is_null, search_release, work_id
    FROM ranked WHERE work_id = nullif($3, '')::uuid
)
SELECT ` + workSummaryColumns + `
FROM ranked AS work
` + workSummaryJoins + `
WHERE $3 = '' OR (work.search_rank, -work.search_similarity, work.release_date IS NULL,
                   work.search_release, work.work_id) > (
    SELECT search_rank, -search_similarity, release_is_null, search_release, work_id
    FROM cursor_anchor
)
ORDER BY work.search_rank,
    work.search_similarity DESC,
    work.release_date DESC NULLS LAST,
    work.work_id
LIMIT $4`
	return store.queryWorkSummaries(ctx, query, queryText, compactCode, afterID, limit)
}

func (store *Store) ListPerformers(ctx context.Context, afterID string, limit int) ([]catalog.PerformerSummary, error) {
	return store.queryPerformerSummaries(ctx, `SELECT `+performerSummaryColumns+`
FROM platform.public_published_performers performer
`+performerSummaryJoins+`
WHERE $1 = '' OR (performer.published_at, performer.performer_id) < (
  SELECT cursor.published_at, cursor.performer_id
  FROM platform.public_published_performers AS cursor WHERE cursor.performer_id = nullif($1, '')::uuid
)
ORDER BY performer.published_at DESC, performer.performer_id DESC LIMIT $2`, afterID, limit)
}

func (store *Store) ListStudios(ctx context.Context, afterID string, limit int) ([]catalog.StudioSummary, error) {
	rows, err := store.pool.Query(ctx, `
SELECT studio_id::text, name, canonical_slug FROM platform.public_published_studios
WHERE $1 = '' OR (published_at, studio_id) < (
  SELECT cursor.published_at, cursor.studio_id
  FROM platform.public_published_studios AS cursor WHERE cursor.studio_id = nullif($1, '')::uuid
)
ORDER BY published_at DESC, studio_id DESC LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list studios: %w", err)
	}
	defer rows.Close()
	items := make([]catalog.StudioSummary, 0)
	for rows.Next() {
		var item catalog.StudioSummary
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug); err != nil {
			return nil, fmt.Errorf("scan studio: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) StudioBySlug(ctx context.Context, slug string) (catalog.StudioDetail, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return catalog.StudioDetail{}, fmt.Errorf("begin studio detail: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var detail catalog.StudioDetail
	if err := tx.QueryRow(ctx, `SELECT studio_id::text, name, canonical_slug FROM platform.public_published_studios WHERE canonical_slug = $1`, slug).Scan(&detail.ID, &detail.Name, &detail.Slug); errors.Is(err, pgx.ErrNoRows) {
		return catalog.StudioDetail{}, catalog.ErrNotFound
	} else if err != nil {
		return catalog.StudioDetail{}, fmt.Errorf("query studio detail: %w", err)
	}
	query := `SELECT ` + workSummaryColumns + ` FROM platform.public_published_works work ` + workSummaryJoins + `
WHERE work.studio_id = $1::uuid ORDER BY work.release_date DESC NULLS LAST, work.published_at DESC, work.work_id LIMIT 200`
	detail.Works, err = queryWorkSummariesWith(ctx, tx, query, detail.ID)
	if err != nil {
		return catalog.StudioDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return catalog.StudioDetail{}, fmt.Errorf("commit studio detail: %w", err)
	}
	return detail, nil
}

func (store *Store) PerformerBySlug(ctx context.Context, slug string) (catalog.PerformerDetail, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return catalog.PerformerDetail{}, fmt.Errorf("begin performer detail transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var detail catalog.PerformerDetail
	var primaryRenditions []byte
	err = tx.QueryRow(ctx, `SELECT `+performerSummaryColumns+`,
       performer.name_original, performer.romanized_name,
       performer.activity_status, performer.agency, performer.birth_year,
       performer.height_cm, performer.measurements, performer.debut_year
FROM platform.public_published_performers performer
`+performerSummaryJoins+`
WHERE performer.canonical_slug = $1`, slug).Scan(&detail.ID, &detail.Name, &detail.Slug, &primaryRenditions,
		&detail.NameOriginal, &detail.RomanizedName, &detail.ActivityStatus, &detail.Agency,
		&detail.BirthYear, &detail.HeightCM, &detail.Measurements, &detail.DebutYear)
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.PerformerDetail{}, catalog.ErrNotFound
	}
	if err != nil {
		return catalog.PerformerDetail{}, fmt.Errorf("query performer detail: %w", err)
	}
	detail.Image, err = imageFromJSON(primaryRenditions)
	if err != nil {
		return catalog.PerformerDetail{}, fmt.Errorf("decode performer primary image: %w", err)
	}
	detail.ImageURL = imageURLForRendition(detail.Image, "w320")
	detail.Aliases, err = queryPerformerAliases(ctx, tx, detail.ID)
	if err != nil {
		return catalog.PerformerDetail{}, err
	}
	detail.Works, err = queryPerformerWorks(ctx, tx, detail.ID)
	if err != nil {
		return catalog.PerformerDetail{}, err
	}
	detail.Images, err = queryEntityImages(ctx, tx, "performer", detail.ID)
	if err != nil {
		return catalog.PerformerDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return catalog.PerformerDetail{}, fmt.Errorf("commit performer detail transaction: %w", err)
	}
	return detail, nil
}

func queryPerformerAliases(ctx context.Context, tx pgx.Tx, performerID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
SELECT raw_value FROM platform.performer_aliases
WHERE performer_id = $1::uuid
ORDER BY raw_value, performer_alias_id`, performerID)
	if err != nil {
		return nil, fmt.Errorf("query performer aliases: %w", err)
	}
	defer rows.Close()
	aliases := make([]string, 0)
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			return nil, fmt.Errorf("scan performer alias: %w", err)
		}
		aliases = append(aliases, alias)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate performer aliases: %w", err)
	}
	return aliases, nil
}

func (store *Store) WorkBySlug(ctx context.Context, slug string) (catalog.WorkDetail, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return catalog.WorkDetail{}, fmt.Errorf("begin work detail transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var detail catalog.WorkDetail
	var primaryRenditions []byte
	err = tx.QueryRow(ctx, `
SELECT work.work_id::text, work.canonical_code, work.title, work.release_date,
       coalesce(studio.name, ''), work.canonical_slug, media.renditions,
       work.title_original, work.summary
FROM platform.public_published_works AS work
`+workSummaryJoins+`
WHERE work.canonical_slug = $1`, slug).Scan(
		&detail.ID, &detail.Code, &detail.Title, &detail.ReleaseDate,
		&detail.StudioName, &detail.Slug, &primaryRenditions,
		&detail.TitleOriginal, &detail.Summary,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.WorkDetail{}, catalog.ErrNotFound
	}
	if err != nil {
		return catalog.WorkDetail{}, fmt.Errorf("query work detail: %w", err)
	}
	detail.Image, err = imageFromJSON(primaryRenditions)
	if err != nil {
		return catalog.WorkDetail{}, fmt.Errorf("decode work primary image: %w", err)
	}
	detail.ImageURL = imageURL(detail.Image)

	detail.Performers, err = queryPerformers(ctx, tx, detail.ID)
	if err != nil {
		return catalog.WorkDetail{}, err
	}
	detail.Images, err = queryImages(ctx, tx, detail.ID)
	if err != nil {
		return catalog.WorkDetail{}, err
	}
	detail.RelatedWorks, err = queryRelatedWorks(ctx, tx, detail.ID, 8)
	if err != nil {
		return catalog.WorkDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return catalog.WorkDetail{}, fmt.Errorf("commit work detail transaction: %w", err)
	}
	return detail, nil
}

func queryRelatedWorks(ctx context.Context, tx pgx.Tx, workID string, limit int) ([]catalog.WorkSummary, error) {
	query := `
WITH source AS (
    SELECT work_id, studio_id, release_date
    FROM platform.public_published_works
    WHERE work_id = $1::uuid
)
SELECT ` + workSummaryColumns + `
FROM platform.public_published_works AS work
CROSS JOIN source
` + workSummaryJoins + `
WHERE work.work_id <> source.work_id
ORDER BY (
    CASE WHEN EXISTS (
        SELECT 1
        FROM platform.work_performers AS candidate
        JOIN platform.work_performers AS original
          ON original.performer_id = candidate.performer_id
        WHERE candidate.work_id = work.work_id
          AND original.work_id = source.work_id
    ) THEN 40 ELSE 0 END
    + CASE WHEN source.studio_id IS NOT NULL AND work.studio_id = source.studio_id THEN 25 ELSE 0 END
    + CASE WHEN EXISTS (
        SELECT 1
        FROM platform.work_tags AS candidate
        JOIN platform.work_tags AS original ON original.tag_id = candidate.tag_id
        WHERE candidate.work_id = work.work_id
          AND original.work_id = source.work_id
    ) THEN 20 ELSE 0 END
    + CASE
        WHEN source.release_date IS NOT NULL AND work.release_date IS NOT NULL
          AND abs(work.release_date - source.release_date) <= 90 THEN 10
        WHEN source.release_date IS NOT NULL AND work.release_date IS NOT NULL
          AND abs(work.release_date - source.release_date) <= 365 THEN 5
        ELSE 0
      END
    + CASE WHEN EXISTS (
        SELECT 1
        FROM platform.editorial_recommendations AS recommendation
        WHERE recommendation.work_id = work.work_id
          AND recommendation.status = 'active'
          AND recommendation.starts_at <= now()
          AND (recommendation.ends_at IS NULL OR recommendation.ends_at > now())
    ) THEN 5 ELSE 0 END
) DESC,
work.release_date DESC NULLS LAST,
work.published_at DESC,
work.work_id
LIMIT $2`
	items, err := queryWorkSummariesWith(ctx, tx, query, workID, limit)
	if err != nil {
		return nil, fmt.Errorf("query related works: %w", err)
	}
	return items, nil
}

func (store *Store) RecordPageView(ctx context.Context, contentType, contentID string) error {
	// Keep the entity type whitelist explicit. Besides preventing SQL identifier
	// injection, this ensures page views are only accepted for currently public
	// records in the corresponding publication view.
	var query string
	switch contentType {
	case "work":
		query = `
INSERT INTO platform.content_metrics_hourly (content_type, content_id, hour_bucket, page_views)
SELECT 'work', work_id, date_trunc('hour', now()), 1
FROM platform.public_published_works WHERE work_id = $1::uuid
ON CONFLICT (content_type, content_id, hour_bucket)
DO UPDATE SET page_views = platform.content_metrics_hourly.page_views + 1, updated_at = now()`
	case "performer":
		query = `
INSERT INTO platform.content_metrics_hourly (content_type, content_id, hour_bucket, page_views)
SELECT 'performer', performer_id, date_trunc('hour', now()), 1
FROM platform.public_published_performers WHERE performer_id = $1::uuid
ON CONFLICT (content_type, content_id, hour_bucket)
DO UPDATE SET page_views = platform.content_metrics_hourly.page_views + 1, updated_at = now()`
	case "studio":
		query = `
INSERT INTO platform.content_metrics_hourly (content_type, content_id, hour_bucket, page_views)
SELECT 'studio', studio_id, date_trunc('hour', now()), 1
FROM platform.public_published_studios WHERE studio_id = $1::uuid
ON CONFLICT (content_type, content_id, hour_bucket)
DO UPDATE SET page_views = platform.content_metrics_hourly.page_views + 1, updated_at = now()`
	default:
		return catalog.ErrNotFound
	}
	result, err := store.pool.Exec(ctx, query, contentID)
	if err != nil {
		return fmt.Errorf("record page view: %w", err)
	}
	if result.RowsAffected() == 0 {
		return catalog.ErrNotFound
	}
	return nil
}

func (store *Store) queryWorkSummaries(ctx context.Context, query string, args ...any) ([]catalog.WorkSummary, error) {
	return queryWorkSummariesWith(ctx, store.pool, query, args...)
}

func queryWorkSummariesWith(ctx context.Context, queryer rowQuerier, query string, args ...any) ([]catalog.WorkSummary, error) {
	rows, err := queryer.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query work summaries: %w", err)
	}
	defer rows.Close()

	items := make([]catalog.WorkSummary, 0)
	for rows.Next() {
		item, err := scanWorkSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan work summary: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate work summaries: %w", err)
	}
	return items, nil
}

func (store *Store) queryPerformerSummaries(ctx context.Context, query string, args ...any) ([]catalog.PerformerSummary, error) {
	rows, err := store.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query performer summaries: %w", err)
	}
	defer rows.Close()
	items := make([]catalog.PerformerSummary, 0)
	for rows.Next() {
		item, err := scanPerformerSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan performer summary: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func queryPerformerWorks(ctx context.Context, tx pgx.Tx, performerID string) ([]catalog.WorkSummary, error) {
	query := `SELECT ` + workSummaryColumns + `
FROM platform.public_published_works work
` + workSummaryJoins + `
JOIN platform.work_performers link ON link.work_id = work.work_id
WHERE link.performer_id = $1::uuid
ORDER BY work.release_date DESC NULLS LAST, work.published_at DESC, work.work_id LIMIT 100`
	rows, err := tx.Query(ctx, query, performerID)
	if err != nil {
		return nil, fmt.Errorf("query performer works: %w", err)
	}
	defer rows.Close()
	items := make([]catalog.WorkSummary, 0)
	for rows.Next() {
		item, err := scanWorkSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan performer work: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type rowQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func queryPerformers(ctx context.Context, queryer rowQuerier, workID string) ([]catalog.PerformerSummary, error) {
	rows, err := queryer.Query(ctx, `SELECT `+performerSummaryColumns+`
FROM platform.work_performers AS link
JOIN platform.public_published_performers AS performer ON performer.performer_id = link.performer_id
`+performerSummaryJoins+`
WHERE link.work_id = $1::uuid
ORDER BY link.position, performer.performer_id`, workID)
	if err != nil {
		return nil, fmt.Errorf("query work performers: %w", err)
	}
	defer rows.Close()

	items := make([]catalog.PerformerSummary, 0)
	for rows.Next() {
		item, err := scanPerformerSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan work performer: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func queryImages(ctx context.Context, queryer rowQuerier, workID string) ([]catalog.Image, error) {
	return queryEntityImages(ctx, queryer, "work", workID)
}

const entityImagesQuery = `
WITH eligible_media AS (
  SELECT *
  FROM platform.public_entity_media
  WHERE entity_type = $1 AND entity_id = $2::uuid
    AND (
      ($1 = 'work' AND (
        (purpose = 'cover' AND is_primary AND position = 0)
        OR (purpose = 'gallery' AND NOT is_primary AND position BETWEEN 1 AND 3)
      ))
      OR ($1 = 'performer' AND purpose = 'avatar' AND is_primary AND position = 0)
    )
), ranked_assets AS (
  SELECT asset_id, bool_or(is_primary) AS is_primary,
         bool_or(purpose = 'gallery') AS is_gallery, min(position) AS position
  FROM eligible_media
  GROUP BY asset_id
), selected_assets AS (
  SELECT asset_id, is_primary, position
  FROM ranked_assets
  WHERE (is_primary OR is_gallery)
    AND EXISTS (SELECT 1 FROM ranked_assets AS primary_asset WHERE primary_asset.is_primary)
  ORDER BY is_primary DESC, position, asset_id
  LIMIT 4
), selected_renditions AS (
  SELECT DISTINCT ON (media.asset_id, media.rendition)
         media.asset_id, media.public_url, media.rendition, media.width, media.height, media.mime_type
  FROM eligible_media AS media
  JOIN selected_assets AS asset ON asset.asset_id = media.asset_id
  WHERE media.entity_type = $1 AND media.entity_id = $2::uuid
  ORDER BY media.asset_id, media.rendition, media.public_url
)
SELECT rendition.asset_id::text, rendition.public_url, rendition.rendition,
       rendition.width, rendition.height, rendition.mime_type
FROM selected_renditions AS rendition
JOIN selected_assets AS asset ON asset.asset_id = rendition.asset_id
ORDER BY asset.is_primary DESC, asset.position, asset.asset_id,
         CASE rendition.rendition WHEN 'w320' THEN 1 WHEN 'w640' THEN 2 WHEN 'w960' THEN 3 ELSE 4 END`

func queryEntityImages(ctx context.Context, queryer rowQuerier, entityType, entityID string) ([]catalog.Image, error) {
	rows, err := queryer.Query(ctx, entityImagesQuery, entityType, entityID)
	if err != nil {
		return nil, fmt.Errorf("query %s images: %w", entityType, err)
	}
	defer rows.Close()

	items := make([]catalog.Image, 0, 4)
	currentAssetID := ""
	renditions := make([]catalog.ImageRendition, 0, 3)
	appendCurrent := func() {
		if image := imageFromRenditions(renditions); image != nil {
			items = append(items, *image)
		}
	}
	for rows.Next() {
		var assetID string
		var rendition catalog.ImageRendition
		if err := rows.Scan(&assetID, &rendition.URL, &rendition.Rendition, &rendition.Width, &rendition.Height, &rendition.MimeType); err != nil {
			return nil, fmt.Errorf("scan %s image: %w", entityType, err)
		}
		if currentAssetID != "" && currentAssetID != assetID {
			appendCurrent()
			renditions = renditions[:0]
		}
		currentAssetID = assetID
		renditions = append(renditions, rendition)
	}
	if currentAssetID != "" {
		appendCurrent()
	}
	return items, rows.Err()
}

type rowScanner interface {
	Scan(...any) error
}

func scanWorkSummary(row rowScanner) (catalog.WorkSummary, error) {
	var item catalog.WorkSummary
	var renditions []byte
	if err := row.Scan(&item.ID, &item.Code, &item.Title, &item.ReleaseDate, &item.StudioName, &item.Slug, &renditions); err != nil {
		return catalog.WorkSummary{}, err
	}
	image, err := imageFromJSON(renditions)
	if err != nil {
		return catalog.WorkSummary{}, fmt.Errorf("decode primary image: %w", err)
	}
	item.Image = image
	item.ImageURL = imageURL(image)
	return item, nil
}

func scanPerformerSummary(row rowScanner) (catalog.PerformerSummary, error) {
	var item catalog.PerformerSummary
	var renditions []byte
	if err := row.Scan(&item.ID, &item.Name, &item.Slug, &renditions); err != nil {
		return catalog.PerformerSummary{}, err
	}
	image, err := imageFromJSON(renditions)
	if err != nil {
		return catalog.PerformerSummary{}, fmt.Errorf("decode primary image: %w", err)
	}
	item.Image = image
	item.ImageURL = imageURLForRendition(image, "w320")
	return item, nil
}

func imageFromJSON(value []byte) (*catalog.Image, error) {
	if len(value) == 0 || string(value) == "null" {
		return nil, nil
	}
	var renditions []catalog.ImageRendition
	if err := json.Unmarshal(value, &renditions); err != nil {
		return nil, err
	}
	return imageFromRenditions(renditions), nil
}

func imageFromRenditions(renditions []catalog.ImageRendition) *catalog.Image {
	if len(renditions) == 0 {
		return nil
	}
	preferred := renditions[0]
	for _, candidate := range renditions[1:] {
		if preferredRendition(candidate.Rendition, preferred.Rendition) {
			preferred = candidate
		}
	}
	return &catalog.Image{
		URL: preferred.URL, Rendition: preferred.Rendition, Width: preferred.Width,
		Height: preferred.Height, MimeType: preferred.MimeType,
		Renditions: append([]catalog.ImageRendition(nil), renditions...),
	}
}

func preferredRendition(candidate, current string) bool {
	rank := func(value string) int {
		switch value {
		case "w640":
			return 0
		case "w320":
			return 1
		case "w960":
			return 2
		default:
			return 3
		}
	}
	return rank(candidate) < rank(current)
}

func imageURL(image *catalog.Image) *string {
	if image == nil {
		return nil
	}
	value := image.URL
	return &value
}

func imageURLForRendition(image *catalog.Image, rendition string) *string {
	if image == nil {
		return nil
	}
	for _, candidate := range image.Renditions {
		if candidate.Rendition == rendition {
			value := candidate.URL
			return &value
		}
	}
	return imageURL(image)
}

func intervalLiteral(value time.Duration) string {
	hours := int(value.Hours())
	if hours < 1 {
		hours = 1
	}
	return fmt.Sprintf("%d hours", hours)
}
