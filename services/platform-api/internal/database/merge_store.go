package database

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (store *Store) MergeEntity(ctx context.Context, sourceID string, input operations.MergeEntityInput, actorID, requestID string, now time.Time) (operations.EntityMerge, error) {
	if sourceID == input.TargetEntityID {
		return operations.EntityMerge{}, operations.ErrInvalidInput
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.EntityMerge{}, fmt.Errorf("begin entity merge: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	lockIDs := []string{sourceID, input.TargetEntityID}
	sort.Strings(lockIDs)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, input.EntityType+":"+lockIDs[0]+":"+lockIDs[1]); err != nil {
		return operations.EntityMerge{}, fmt.Errorf("lock entity merge: %w", err)
	}
	sourceStatus, sourceSlug, err := mergeEntityState(ctx, tx, input.EntityType, sourceID)
	if err != nil {
		return operations.EntityMerge{}, err
	}
	targetStatus, targetSlug, err := mergeEntityState(ctx, tx, input.EntityType, input.TargetEntityID)
	if err != nil {
		return operations.EntityMerge{}, err
	}
	if sourceStatus != "published" || targetStatus != "published" {
		return operations.EntityMerge{}, operations.ErrConflict
	}
	if err := mergeEntityRelations(ctx, tx, input.EntityType, sourceID, input.TargetEntityID, now); err != nil {
		return operations.EntityMerge{}, err
	}
	if err := refreshMergedSearchDocuments(ctx, tx, input.EntityType, input.TargetEntityID, now); err != nil {
		return operations.EntityMerge{}, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE platform.publications
SET publication_status = 'hidden', hidden_at = $3, published_at = NULL, takedown_at = NULL, updated_at = $3
WHERE entity_type = $1 AND entity_id = $2::uuid AND site = 'main'`, input.EntityType, sourceID, now); err != nil {
		return operations.EntityMerge{}, fmt.Errorf("hide merged publication: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM platform.search_documents WHERE entity_type = $1 AND entity_id = $2::uuid`, input.EntityType, sourceID); err != nil {
		return operations.EntityMerge{}, fmt.Errorf("remove merged search document: %w", err)
	}
	if err := setEntityStatus(ctx, tx, input.EntityType, sourceID, "merged"); err != nil {
		return operations.EntityMerge{}, err
	}
	prefix := map[string]string{"work": "works", "performer": "performers", "studio": "studios"}[input.EntityType]
	sourcePath, targetPath := "/"+prefix+"/"+sourceSlug, "/"+prefix+"/"+targetSlug
	var redirectID string
	err = tx.QueryRow(ctx, `
INSERT INTO platform.redirects (source_path, target_path, status_code, active, reason, created_at)
VALUES ($1, $2, 301, true, left($3, 500), $4)
ON CONFLICT (source_path) DO UPDATE SET target_path = EXCLUDED.target_path, status_code = 301,
    active = true, reason = EXCLUDED.reason
RETURNING redirect_id::text`, sourcePath, targetPath, input.Reason, now).Scan(&redirectID)
	if err != nil {
		return operations.EntityMerge{}, mapConstraintError(err)
	}
	var mergeID string
	err = tx.QueryRow(ctx, `
INSERT INTO platform.entity_merges
    (entity_type, source_entity_id, target_entity_id, redirect_id, merged_by, reason, merged_at)
VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7)
RETURNING merge_id::text`, input.EntityType, sourceID, input.TargetEntityID, redirectID, actorID, input.Reason, now).Scan(&mergeID)
	if err != nil {
		return operations.EntityMerge{}, mapConstraintError(err)
	}
	if err := insertAudit(ctx, tx, actorID, "entity.merge", input.EntityType, sourceID,
		map[string]any{"status": sourceStatus, "path": sourcePath},
		map[string]any{"status": "merged", "target_entity_id": input.TargetEntityID, "target_path": targetPath},
		input.Reason, requestID, now); err != nil {
		return operations.EntityMerge{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, event_type, payload, dedupe_key)
VALUES ($1, $2::uuid, 'cache_purge',
        jsonb_build_object('reason', 'entity_merged', 'source_path', $3::text, 'target_path', $4::text), $5)
ON CONFLICT (event_type, dedupe_key) DO NOTHING`, input.EntityType, sourceID, sourcePath, targetPath, mergeID); err != nil {
		return operations.EntityMerge{}, fmt.Errorf("enqueue entity merge cache invalidation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.EntityMerge{}, fmt.Errorf("commit entity merge: %w", err)
	}
	return operations.EntityMerge{ID: mergeID, EntityType: input.EntityType, SourceEntityID: sourceID,
		TargetEntityID: input.TargetEntityID, SourcePath: sourcePath, TargetPath: targetPath, MergedAt: now}, nil
}

func mergeEntityState(ctx context.Context, tx pgx.Tx, entityType, entityID string) (string, string, error) {
	table, idColumn, err := entityTable(entityType)
	if err != nil {
		return "", "", operations.ErrInvalidInput
	}
	query := fmt.Sprintf(`SELECT entity.publication_status, publication.canonical_slug, publication.publication_status
FROM platform.%s entity
JOIN platform.publications publication ON publication.entity_type = $1 AND publication.entity_id = entity.%s AND publication.site = 'main'
WHERE entity.%s = $2::uuid FOR UPDATE OF entity, publication`, table, idColumn, idColumn)
	var status, slug, publicationStatus string
	if err := tx.QueryRow(ctx, query, entityType, entityID).Scan(&status, &slug, &publicationStatus); errors.Is(err, pgx.ErrNoRows) {
		return "", "", operations.ErrNotFound
	} else if err != nil {
		return "", "", fmt.Errorf("read entity merge state: %w", err)
	}
	if publicationStatus != "published" || status != "published" {
		return status, slug, operations.ErrConflict
	}
	return status, slug, nil
}

func mergeEntityRelations(ctx context.Context, tx pgx.Tx, entityType, sourceID, targetID string, now time.Time) error {
	statements := map[string][]string{
		"performer": {
			`INSERT INTO platform.work_performers (work_id, performer_id, role, position, created_at)
             SELECT work_id, $2::uuid, role, position, created_at FROM platform.work_performers WHERE performer_id = $1::uuid
             ON CONFLICT (work_id, performer_id, role) DO NOTHING`,
			`DELETE FROM platform.work_performers WHERE performer_id = $1::uuid`,
			`INSERT INTO platform.follows (user_id, performer_id, created_at, is_deleted, deleted_at)
             SELECT user_id, $2::uuid, created_at, is_deleted, deleted_at FROM platform.follows WHERE performer_id = $1::uuid
			 ON CONFLICT (user_id, performer_id) DO UPDATE
			 SET is_deleted = platform.follows.is_deleted AND EXCLUDED.is_deleted,
			     deleted_at = CASE WHEN platform.follows.is_deleted AND EXCLUDED.is_deleted
			                       THEN coalesce(platform.follows.deleted_at, EXCLUDED.deleted_at) ELSE NULL END`,
			`INSERT INTO platform.performer_aliases (performer_id, alias_type, raw_value, normalized_value, language_tag, created_at)
			 SELECT $2::uuid, alias_type, raw_value, normalized_value, language_tag, created_at
			 FROM platform.performer_aliases WHERE performer_id = $1::uuid
			 ON CONFLICT (performer_id, alias_type, normalized_value) DO NOTHING`,
			`UPDATE platform.view_history SET content_id = $2::uuid WHERE content_type = 'performer' AND content_id = $1::uuid`,
			`UPDATE platform.performers SET merged_into = $2::uuid WHERE performer_id = $1::uuid`,
		},
		"studio": {
			`UPDATE platform.works SET studio_id = $2::uuid, updated_at = $3 WHERE studio_id = $1::uuid`,
			`UPDATE platform.view_history SET content_id = $2::uuid WHERE content_type = 'studio' AND content_id = $1::uuid`,
		},
		"work": {
			`INSERT INTO platform.work_performers (work_id, performer_id, role, position, created_at)
             SELECT $2::uuid, performer_id, role, position, created_at FROM platform.work_performers WHERE work_id = $1::uuid
             ON CONFLICT (work_id, performer_id, role) DO NOTHING`,
			`INSERT INTO platform.work_tags (work_id, tag_id, created_at)
             SELECT $2::uuid, tag_id, created_at FROM platform.work_tags WHERE work_id = $1::uuid
             ON CONFLICT (work_id, tag_id) DO NOTHING`,
			`INSERT INTO platform.favorites (user_id, work_id, created_at, is_deleted, deleted_at)
             SELECT user_id, $2::uuid, created_at, is_deleted, deleted_at FROM platform.favorites WHERE work_id = $1::uuid
			 ON CONFLICT (user_id, work_id) DO UPDATE
			 SET is_deleted = platform.favorites.is_deleted AND EXCLUDED.is_deleted,
			     deleted_at = CASE WHEN platform.favorites.is_deleted AND EXCLUDED.is_deleted
			                       THEN coalesce(platform.favorites.deleted_at, EXCLUDED.deleted_at) ELSE NULL END`,
			`INSERT INTO platform.hidden_preferences (user_id, work_id, status, created_at, updated_at)
             SELECT user_id, $2::uuid, status, created_at, updated_at FROM platform.hidden_preferences WHERE work_id = $1::uuid
			 ON CONFLICT (user_id, work_id) DO UPDATE
			 SET status = CASE WHEN platform.hidden_preferences.status = 'active' OR EXCLUDED.status = 'active'
			                   THEN 'active' ELSE 'removed' END, updated_at = $3`,
			`INSERT INTO platform.work_aliases (work_id, alias_type, raw_value, normalized_value, language_tag, created_at)
			 SELECT $2::uuid, alias_type, raw_value, normalized_value, language_tag, created_at
			 FROM platform.work_aliases WHERE work_id = $1::uuid
			 ON CONFLICT (work_id, alias_type, normalized_value) DO NOTHING`,
			`UPDATE platform.view_history SET content_id = $2::uuid WHERE content_type = 'work' AND content_id = $1::uuid`,
			`INSERT INTO platform.content_metrics_hourly (content_type, content_id, hour_bucket, page_views, updated_at)
			 SELECT content_type, $2::uuid, hour_bucket, page_views, $3 FROM platform.content_metrics_hourly
			 WHERE content_type = 'work' AND content_id = $1::uuid
			 ON CONFLICT (content_type, content_id, hour_bucket) DO UPDATE
			 SET page_views = platform.content_metrics_hourly.page_views + EXCLUDED.page_views, updated_at = $3`,
			`DELETE FROM platform.content_metrics_hourly WHERE content_type = 'work' AND content_id = $1::uuid`,
		},
	}
	for _, statement := range statements[entityType] {
		args := []any{sourceID}
		if strings.Contains(statement, "$2") {
			args = append(args, targetID)
		}
		if strings.Contains(statement, "$3") {
			args = append(args, now)
		}
		if _, err := tx.Exec(ctx, statement, args...); err != nil {
			return fmt.Errorf("merge %s relations: %w", entityType, err)
		}
	}
	if entityType == "work" {
		if _, err := tx.Exec(ctx, `UPDATE platform.editorial_recommendations SET status = 'removed', updated_at = $2
WHERE work_id = $1::uuid AND status <> 'removed'`, sourceID, now); err != nil {
			return fmt.Errorf("remove merged work recommendations: %w", err)
		}
	}
	if entityType != "work" {
		if _, err := tx.Exec(ctx, `
INSERT INTO platform.content_metrics_hourly (content_type, content_id, hour_bucket, page_views, updated_at)
SELECT content_type, $2::uuid, hour_bucket, page_views, $3
FROM platform.content_metrics_hourly
WHERE content_type = $4 AND content_id = $1::uuid
ON CONFLICT (content_type, content_id, hour_bucket) DO UPDATE
SET page_views = platform.content_metrics_hourly.page_views + EXCLUDED.page_views, updated_at = $3`,
			sourceID, targetID, now, entityType); err != nil {
			return fmt.Errorf("merge %s metrics: %w", entityType, err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM platform.content_metrics_hourly WHERE content_type = $2 AND content_id = $1::uuid`, sourceID, entityType); err != nil {
			return fmt.Errorf("remove merged %s metrics: %w", entityType, err)
		}
	}
	return nil
}

func refreshMergedSearchDocuments(ctx context.Context, tx pgx.Tx, entityType, targetID string, now time.Time) error {
	condition := map[string]string{
		"work":      "work.work_id = $1::uuid",
		"performer": "EXISTS (SELECT 1 FROM platform.work_performers affected WHERE affected.work_id = work.work_id AND affected.performer_id = $1::uuid)",
		"studio":    "work.studio_id = $1::uuid",
	}[entityType]
	if condition == "" {
		return operations.ErrInvalidInput
	}
	query := fmt.Sprintf(`
UPDATE platform.search_documents document
SET names = coalesce((SELECT string_agg(performer.display_name, ' ')
                      FROM platform.work_performers link
                      JOIN platform.performers performer ON performer.performer_id = link.performer_id
                      WHERE link.work_id = work.work_id), ''),
    aliases = concat_ws(' ',
        coalesce((SELECT string_agg(alias.raw_value, ' ') FROM platform.work_aliases alias WHERE alias.work_id = work.work_id), ''),
        coalesce((SELECT string_agg(alias.raw_value, ' ')
                  FROM platform.work_performers link
                  JOIN platform.performer_aliases alias ON alias.performer_id = link.performer_id
                  WHERE link.work_id = work.work_id), '')),
    studio_name = coalesce((SELECT studio.name FROM platform.studios studio WHERE studio.studio_id = work.studio_id), ''),
    updated_at = $2
FROM platform.works work
WHERE document.entity_type = 'work' AND document.entity_id = work.work_id AND %s`, condition)
	if _, err := tx.Exec(ctx, query, targetID, now); err != nil {
		return fmt.Errorf("refresh merged work search documents: %w", err)
	}
	if entityType == "performer" {
		if _, err := tx.Exec(ctx, `
UPDATE platform.search_documents document
SET aliases = coalesce((SELECT string_agg(alias.raw_value, ' ')
                        FROM platform.performer_aliases alias WHERE alias.performer_id = $1::uuid), ''),
    updated_at = $2
WHERE document.entity_type = 'performer' AND document.entity_id = $1::uuid`, targetID, now); err != nil {
			return fmt.Errorf("refresh merged performer search document: %w", err)
		}
	}
	return nil
}
