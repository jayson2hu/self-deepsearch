package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

const seoInventoryCTE = `
WITH inventory AS (
  SELECT publication.entity_type, publication.entity_id, publication.canonical_slug,
    publication.publication_status, publication.content_classification, publication.seo_quality_status,
    publication.updated_at, work.canonical_code || ' ' || work.title AS title,
    work.canonical_code || ' 的公开发行与人物资料。' AS description
  FROM platform.publications publication
  JOIN platform.works work ON publication.entity_type='work' AND work.work_id=publication.entity_id
  WHERE publication.site='main'
  UNION ALL
  SELECT publication.entity_type, publication.entity_id, publication.canonical_slug,
    publication.publication_status, publication.content_classification, publication.seo_quality_status,
    publication.updated_at, performer.display_name,
    performer.display_name || ' 的公开资料与作品列表。'
  FROM platform.publications publication
  JOIN platform.performers performer ON publication.entity_type='performer' AND performer.performer_id=publication.entity_id
  WHERE publication.site='main'
  UNION ALL
  SELECT publication.entity_type, publication.entity_id, publication.canonical_slug,
    publication.publication_status, publication.content_classification, publication.seo_quality_status,
    publication.updated_at, studio.name,
    studio.name || ' 的公开厂牌资料与作品列表。'
  FROM platform.publications publication
  JOIN platform.studios studio ON publication.entity_type='studio' AND studio.studio_id=publication.entity_id
  WHERE publication.site='main'
), classified AS (
  SELECT inventory.*,
    CASE
      WHEN publication_status <> 'published' THEN 'not_published'
      WHEN content_classification = 'unknown' THEN 'classification_unknown'
      WHEN seo_quality_status = 'unknown' THEN 'quality_unknown'
      WHEN seo_quality_status = 'ineligible' THEN 'quality_ineligible'
      ELSE NULL
    END AS exclusion_reason,
    publication_status='published' AND content_classification <> 'unknown' AND seo_quality_status='eligible' AS indexable,
    CASE WHEN EXISTS (
      SELECT 1 FROM platform.entity_media link
      JOIN platform.media_assets asset ON asset.asset_id=link.asset_id
      WHERE link.entity_type=inventory.entity_type AND link.entity_id=inventory.entity_id
        AND link.is_primary AND link.publication_status='published'
        AND asset.asset_status='published' AND asset.rights_status='allowed'
        AND asset.content_classification='non_explicit' AND asset.external_preview_permission='allow'
    ) THEN 'preview_allowed'
    WHEN EXISTS (
      SELECT 1 FROM platform.entity_media link
      WHERE link.entity_type=inventory.entity_type AND link.entity_id=inventory.entity_id
        AND link.is_primary AND link.publication_status='published'
    ) THEN 'preview_blocked'
    ELSE 'missing' END AS media_policy
  FROM inventory
)
`

func (store *Store) SEOInventorySummary(ctx context.Context, now time.Time) (operations.SEOInventorySummary, error) {
	var result operations.SEOInventorySummary
	var reasonsJSON []byte
	err := store.pool.QueryRow(ctx, seoInventoryCTE+`
SELECT count(*), count(*) FILTER (WHERE indexable), count(*) FILTER (WHERE NOT indexable),
  count(*) FILTER (WHERE content_classification='unknown' OR seo_quality_status='unknown'),
  count(*) FILTER (WHERE indexable), count(*) FILTER (WHERE publication_status='published'),
  count(*) FILTER (WHERE publication_status='hidden'), count(*) FILTER (WHERE publication_status='takedown'),
  coalesce((SELECT jsonb_object_agg(reason, amount) FROM (
    SELECT exclusion_reason reason, count(*) amount FROM classified
    WHERE exclusion_reason IS NOT NULL GROUP BY exclusion_reason
  ) grouped), '{}'::jsonb)
FROM classified`).Scan(&result.Total, &result.Indexable, &result.Excluded, &result.Unknown,
		&result.SitemapCandidates, &result.Published, &result.Hidden, &result.Takedown, &reasonsJSON)
	if err != nil {
		return operations.SEOInventorySummary{}, fmt.Errorf("summarize seo inventory: %w", err)
	}
	result.ObservedAt = now
	result.Reasons = map[string]int64{}
	if err := json.Unmarshal(reasonsJSON, &result.Reasons); err != nil {
		return operations.SEOInventorySummary{}, fmt.Errorf("decode seo exclusion reasons: %w", err)
	}
	return result, nil
}

func (store *Store) ListSEOPages(ctx context.Context, query operations.SEOPageQuery) (operations.SEOPageResult, error) {
	if query.Limit < 1 || query.Limit > 100 {
		query.Limit = 25
	}
	if query.Offset < 0 || query.Offset > 1000000 {
		query.Offset = 0
	}
	rows, err := store.pool.Query(ctx, seoInventoryCTE+`
SELECT entity_type, entity_id::text, '/' || entity_type || 's/' || canonical_slug,
  title, description, '/' || entity_type || 's/' || canonical_slug,
  CASE WHEN indexable THEN 'index,follow' ELSE 'noindex,nofollow' END,
  publication_status, content_classification, seo_quality_status, media_policy,
  indexable, indexable, exclusion_reason, updated_at, count(*) OVER()
FROM classified
WHERE ($1='' OR entity_type=$1) AND ($2='' OR publication_status=$2)
  AND ($3='' OR title ILIKE '%' || $3 || '%' OR canonical_slug ILIKE '%' || $3 || '%')
ORDER BY updated_at DESC, entity_type, entity_id
OFFSET $4 LIMIT $5`, query.EntityType, query.Status, strings.TrimSpace(query.Query), query.Offset, query.Limit)
	if err != nil {
		return operations.SEOPageResult{}, fmt.Errorf("list seo inventory: %w", err)
	}
	defer rows.Close()
	result := operations.SEOPageResult{Items: make([]operations.SEOPage, 0)}
	for rows.Next() {
		var item operations.SEOPage
		if err := rows.Scan(&item.EntityType, &item.EntityID, &item.URLPath, &item.Title, &item.Description,
			&item.Canonical, &item.Robots, &item.PublicationStatus, &item.ContentClassification, &item.QualityStatus,
			&item.MediaPolicy, &item.Indexable, &item.SitemapEligible, &item.ExclusionReason, &item.UpdatedAt, &result.Total); err != nil {
			return operations.SEOPageResult{}, fmt.Errorf("scan seo inventory: %w", err)
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}
