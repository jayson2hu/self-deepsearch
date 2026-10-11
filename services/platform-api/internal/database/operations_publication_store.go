package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (store *Store) PublishEntity(ctx context.Context, entityID string, input operations.PublishInput, actorID, requestID string, now time.Time) (operations.Entity, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.Entity{}, fmt.Errorf("begin publish entity: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockEntity(ctx, tx, input.EntityType, entityID); err != nil {
		return operations.Entity{}, err
	}
	table, idColumn, _ := entityTable(input.EntityType)
	statusQuery := fmt.Sprintf("SELECT publication_status FROM platform.%s WHERE %s = $1::uuid", table, idColumn)
	var entityStatus string
	if err := tx.QueryRow(ctx, statusQuery, entityID).Scan(&entityStatus); err != nil {
		return operations.Entity{}, fmt.Errorf("read publication status: %w", err)
	}
	if entityStatus == "takedown" {
		return operations.Entity{}, operations.ErrForbidden
	}
	var revisionID string
	var payload []byte
	var revisionAuthorID string
	query := `SELECT revision_id::text, payload FROM platform.content_revisions
WHERE entity_type = $1 AND entity_id = $2::uuid AND revision_status = 'approved'`
	args := []any{input.EntityType, entityID}
	if input.RevisionID != "" {
		query += ` AND revision_id = $3::uuid`
		args = append(args, input.RevisionID)
	}
	query = strings.Replace(query, `SELECT revision_id::text, payload`, `SELECT revision_id::text, payload, author_id::text`, 1)
	query += ` ORDER BY version DESC LIMIT 1 FOR UPDATE`
	if err := tx.QueryRow(ctx, query, args...).Scan(&revisionID, &payload, &revisionAuthorID); errors.Is(err, pgx.ErrNoRows) {
		return operations.Entity{}, operations.ErrConflict
	} else if err != nil {
		return operations.Entity{}, fmt.Errorf("lock approved revision: %w", err)
	}
	complete, err := revisionProvenanceComplete(ctx, tx, revisionID)
	if err != nil {
		return operations.Entity{}, err
	}
	if !complete {
		return operations.Entity{}, operations.ErrConflict
	}
	if err := applyRevision(ctx, tx, input.EntityType, entityID, payload); err != nil {
		return operations.Entity{}, err
	}
	contentClassification, seoQualityStatus, err := seoPolicyFromRevision(payload)
	if err != nil {
		return operations.Entity{}, err
	}
	if input.EntityType == "performer" || input.EntityType == "studio" {
		table, idColumn, _ := entityTable(input.EntityType)
		query := fmt.Sprintf("UPDATE platform.%s SET canonical_slug = $2 WHERE %s = $1::uuid", table, idColumn)
		if _, err := tx.Exec(ctx, query, entityID, input.CanonicalSlug); err != nil {
			return operations.Entity{}, mapConstraintError(err)
		}
	}
	var previousRevisionID *string
	if err := tx.QueryRow(ctx, `
SELECT current_revision_id::text FROM platform.publications
WHERE entity_type = $1 AND entity_id = $2::uuid AND site = 'main'`, input.EntityType, entityID).Scan(&previousRevisionID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return operations.Entity{}, fmt.Errorf("read current publication revision: %w", err)
	}
	rollbackSourceRevisionID := ""
	isRollback := input.RevisionID != "" && previousRevisionID != nil && *previousRevisionID != revisionID
	if isRollback {
		// A rollback creates a new immutable approved revision. Preserve the
		// selected revision's author as provenance and record the operator as
		// reviewer. The schema intentionally forbids author == reviewer, so do
		// not let an operator approve their own historical revision.
		if revisionAuthorID == actorID {
			return operations.Entity{}, operations.ErrForbidden
		}
		rollbackSourceRevisionID = revisionID
		err = tx.QueryRow(ctx, `
INSERT INTO platform.content_revisions (
  entity_type, entity_id, version, payload, revision_status, author_id, reviewer_id,
  reason, submitted_at, reviewed_at
)
SELECT $1, $2::uuid, coalesce(max(version), 0) + 1, $3::jsonb, 'approved',
       $4::uuid, $5::uuid, $6, $7, $7
FROM platform.content_revisions WHERE entity_type = $1 AND entity_id = $2::uuid
RETURNING revision_id::text`, input.EntityType, entityID, payload, revisionAuthorID, actorID, input.Reason, now).Scan(&revisionID)
		if err != nil {
			return operations.Entity{}, fmt.Errorf("create rollback revision: %w", err)
		}
		if err := copyRevisionProvenance(ctx, tx, rollbackSourceRevisionID, revisionID, nil); err != nil {
			return operations.Entity{}, err
		}
	}
	var publicationID string
	err = tx.QueryRow(ctx, `
INSERT INTO platform.publications (
    entity_type, entity_id, site, current_revision_id, publication_status, canonical_slug, published_at,
    content_classification, seo_quality_status, seo_reviewed_at, seo_reviewed_by
) VALUES ($1, $2::uuid, 'main', $3::uuid, 'published', $4, $5, $6, $7,
          CASE WHEN $6 = 'unknown' THEN NULL ELSE $5::timestamptz END,
          CASE WHEN $6 = 'unknown' THEN NULL ELSE $8::uuid END)
ON CONFLICT (entity_type, entity_id, site) DO UPDATE
SET current_revision_id = EXCLUDED.current_revision_id,
    publication_status = 'published', canonical_slug = EXCLUDED.canonical_slug,
    published_at = EXCLUDED.published_at, hidden_at = NULL, takedown_at = NULL,
    content_classification = EXCLUDED.content_classification,
    seo_quality_status = EXCLUDED.seo_quality_status,
    seo_reviewed_at = EXCLUDED.seo_reviewed_at,
    seo_reviewed_by = EXCLUDED.seo_reviewed_by
RETURNING publication_id::text`, input.EntityType, entityID, revisionID, input.CanonicalSlug, now,
		contentClassification, seoQualityStatus, actorID).Scan(&publicationID)
	if err != nil {
		return operations.Entity{}, mapConstraintError(err)
	}
	if err := refreshSearchDocument(ctx, tx, input.EntityType, entityID, publicationID, now); err != nil {
		return operations.Entity{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, event_type, payload, dedupe_key)
VALUES ($1::text, $2::uuid, 'publication_changed', jsonb_build_object('entity_type', $1::text, 'entity_id', $2::uuid, 'slug', $3::text), $4)
ON CONFLICT (event_type, dedupe_key) DO NOTHING`, input.EntityType, entityID, input.CanonicalSlug, revisionID); err != nil {
		return operations.Entity{}, fmt.Errorf("enqueue publication side effects: %w", err)
	}
	action := "entity.publish"
	if isRollback {
		action = "entity.rollback"
	}
	if err := insertAudit(ctx, tx, actorID, action, input.EntityType, entityID,
		map[string]any{"revision_id": previousRevisionID},
		map[string]any{"revision_id": revisionID, "rollback_source_revision_id": rollbackSourceRevisionID, "canonical_slug": input.CanonicalSlug,
			"content_classification": contentClassification, "seo_quality_status": seoQualityStatus}, input.Reason, requestID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.Entity{}, fmt.Errorf("commit publish entity: %w", err)
	}
	return operations.Entity{ID: entityID, EntityType: input.EntityType, CanonicalSlug: input.CanonicalSlug,
		Status: "published", RevisionID: revisionID, RevisionStatus: "approved", CreatedAt: now}, nil
}

func (store *Store) HideEntity(ctx context.Context, entityID, entityType, actorID, reason, requestID string, now time.Time) (operations.Entity, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.Entity{}, fmt.Errorf("begin hide entity: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var revisionID, slug string
	err = tx.QueryRow(ctx, `
UPDATE platform.publications
SET publication_status = 'hidden', hidden_at = $3, published_at = NULL, takedown_at = NULL
WHERE entity_type = $1 AND entity_id = $2::uuid AND site = 'main' AND publication_status = 'published'
RETURNING current_revision_id::text, canonical_slug`, entityType, entityID, now).Scan(&revisionID, &slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.Entity{}, operations.ErrConflict
	}
	if err != nil {
		return operations.Entity{}, fmt.Errorf("hide publication: %w", err)
	}
	if err := setEntityStatus(ctx, tx, entityType, entityID, "hidden"); err != nil {
		return operations.Entity{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, event_type, payload, dedupe_key)
VALUES ($1::text, $2::uuid, 'publication_hidden', jsonb_build_object('entity_type', $1::text, 'entity_id', $2::uuid), $3)
ON CONFLICT (event_type, dedupe_key) DO NOTHING`, entityType, entityID, revisionID+":"+now.Format(time.RFC3339Nano)); err != nil {
		return operations.Entity{}, fmt.Errorf("enqueue hide side effects: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, "entity.hide", entityType, entityID,
		map[string]any{"status": "published"}, map[string]any{"status": "hidden"}, reason, requestID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.Entity{}, fmt.Errorf("commit hide entity: %w", err)
	}
	return operations.Entity{ID: entityID, EntityType: entityType, CanonicalSlug: slug,
		Status: "hidden", RevisionID: revisionID, RevisionStatus: "approved", CreatedAt: now}, nil
}

func seoPolicyFromRevision(payload []byte) (string, string, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(payload, &values); err != nil {
		return "", "", operations.ErrInvalidInput
	}
	classification, quality := "unknown", "unknown"
	if raw, ok := values["content_classification"]; ok {
		if err := json.Unmarshal(raw, &classification); err != nil {
			return "", "", operations.ErrInvalidInput
		}
	}
	if raw, ok := values["seo_quality_status"]; ok {
		if err := json.Unmarshal(raw, &quality); err != nil {
			return "", "", operations.ErrInvalidInput
		}
	}
	validUnknown := classification == "unknown" && quality == "unknown"
	validReviewed := (classification == "non_explicit" || classification == "explicit") && (quality == "eligible" || quality == "ineligible")
	if !validUnknown && !validReviewed {
		return "", "", operations.ErrInvalidInput
	}
	return classification, quality, nil
}

func refreshSearchDocument(ctx context.Context, tx pgx.Tx, entityType, entityID, publicationID string, now time.Time) error {
	var query string
	switch entityType {
	case "work":
		query = `
INSERT INTO platform.search_documents (entity_type, entity_id, canonical_code, compact_code, title, names, aliases, studio_name, publication_id, updated_at)
SELECT 'work', work.work_id, work.canonical_code, work.compact_code, work.title,
       coalesce((SELECT string_agg(performer.display_name, ' ') FROM platform.work_performers link JOIN platform.performers performer ON performer.performer_id = link.performer_id WHERE link.work_id = work.work_id), ''),
       concat_ws(' ',
         coalesce((SELECT string_agg(alias.raw_value, ' ') FROM platform.work_aliases alias WHERE alias.work_id = work.work_id), ''),
         coalesce((SELECT string_agg(alias.raw_value, ' ')
                   FROM platform.work_performers link
                   JOIN platform.performer_aliases alias ON alias.performer_id = link.performer_id
                   WHERE link.work_id = work.work_id), '')
       ),
       coalesce(studio.name, ''), $2::uuid, $3
FROM platform.works work LEFT JOIN platform.studios studio ON studio.studio_id = work.studio_id
WHERE work.work_id = $1::uuid
ON CONFLICT (entity_type, entity_id) DO UPDATE SET
 canonical_code = EXCLUDED.canonical_code, compact_code = EXCLUDED.compact_code, title = EXCLUDED.title,
 names = EXCLUDED.names, aliases = EXCLUDED.aliases, studio_name = EXCLUDED.studio_name,
 publication_id = EXCLUDED.publication_id, updated_at = EXCLUDED.updated_at`
	case "performer":
		query = `
INSERT INTO platform.search_documents (entity_type, entity_id, title, names, aliases, publication_id, updated_at)
SELECT 'performer', performer.performer_id, performer.display_name,
       concat_ws(' ', performer.display_name, performer.name_original, performer.romanized_name),
       coalesce((SELECT string_agg(alias.raw_value, ' ') FROM platform.performer_aliases alias WHERE alias.performer_id = performer.performer_id), ''),
       $2::uuid, $3 FROM platform.performers performer WHERE performer.performer_id = $1::uuid
ON CONFLICT (entity_type, entity_id) DO UPDATE SET title = EXCLUDED.title, names = EXCLUDED.names,
 aliases = EXCLUDED.aliases, publication_id = EXCLUDED.publication_id, updated_at = EXCLUDED.updated_at`
	case "studio":
		query = `
INSERT INTO platform.search_documents (entity_type, entity_id, title, studio_name, publication_id, updated_at)
SELECT 'studio', studio_id, name, name, $2::uuid, $3 FROM platform.studios WHERE studio_id = $1::uuid
ON CONFLICT (entity_type, entity_id) DO UPDATE SET title = EXCLUDED.title, studio_name = EXCLUDED.studio_name,
 publication_id = EXCLUDED.publication_id, updated_at = EXCLUDED.updated_at`
	default:
		return operations.ErrInvalidInput
	}
	if _, err := tx.Exec(ctx, query, entityID, publicationID, now); err != nil {
		return fmt.Errorf("refresh search document: %w", err)
	}
	return nil
}
