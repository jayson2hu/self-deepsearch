package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (store *Store) CreateWork(ctx context.Context, input operations.WorkInput, actorID, requestID string, now time.Time) (operations.Entity, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.Entity{}, fmt.Errorf("begin create work: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	entity, _, err := createWorkTx(ctx, tx, input, actorID, requestID, now)
	if err != nil {
		return operations.Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.Entity{}, fmt.Errorf("commit create work: %w", err)
	}
	return entity, nil
}

func createWorkTx(ctx context.Context, tx pgx.Tx, input operations.WorkInput, actorID, requestID string, now time.Time) (operations.Entity, map[string]any, error) {
	if len(input.Sources) == 0 {
		return operations.Entity{}, nil, operations.ErrInvalidInput
	}
	payload := map[string]any{"canonical_code": input.Code, "compact_code": compactIdentifier(input.Code), "title": input.Title, "performer_ids": input.PerformerIDs}
	putOptional(payload, "title_original", input.TitleOriginal)
	putOptional(payload, "summary", input.Summary)
	putOptional(payload, "studio_id", input.StudioID)
	if input.ReleaseDate != nil {
		payload["release_date"] = input.ReleaseDate.Format(time.DateOnly)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return operations.Entity{}, nil, fmt.Errorf("encode work revision: %w", err)
	}
	var entity operations.Entity
	err = tx.QueryRow(ctx, `
INSERT INTO platform.works (canonical_code, compact_code, title, title_original, release_date, studio_id, summary, publication_status)
VALUES ($1, $2, $3, $4, $5, $6::uuid, $7, 'reviewing')
RETURNING work_id::text, created_at`, input.Code, compactIdentifier(input.Code), input.Title,
		input.TitleOriginal, input.ReleaseDate, input.StudioID, input.Summary).Scan(&entity.ID, &entity.CreatedAt)
	if err != nil {
		return operations.Entity{}, nil, mapConstraintError(err)
	}
	entity.EntityType, entity.Status, entity.RevisionStatus = "work", "reviewing", "reviewing"
	if err := tx.QueryRow(ctx, `
INSERT INTO platform.content_revisions (entity_type, entity_id, version, payload, revision_status, author_id, reason, submitted_at)
VALUES ('work', $1::uuid, 1, $2::jsonb, 'reviewing', $3::uuid, $4, $5)
RETURNING revision_id::text`, entity.ID, data, actorID, input.Reason, now).Scan(&entity.RevisionID); err != nil {
		return operations.Entity{}, nil, fmt.Errorf("create work revision: %w", err)
	}
	if err := insertRevisionProvenance(ctx, tx, "work", entity.ID, entity.RevisionID, payload, input.Sources, actorID); err != nil {
		return operations.Entity{}, nil, err
	}
	for position, performerID := range input.PerformerIDs {
		if _, err := tx.Exec(ctx, `
INSERT INTO platform.work_performers (work_id, performer_id, role, position)
VALUES ($1::uuid, $2::uuid, 'performer', $3)`, entity.ID, performerID, position); err != nil {
			return operations.Entity{}, nil, mapConstraintError(err)
		}
	}
	if err := createReviewTask(ctx, tx, "work", entity.ID, now); err != nil {
		return operations.Entity{}, nil, err
	}
	if err := insertAudit(ctx, tx, actorID, "work.create", "work", entity.ID, nil, payload, input.Reason, requestID, now); err != nil {
		return operations.Entity{}, nil, err
	}
	return entity, payload, nil
}

func (store *Store) CreatePerformer(ctx context.Context, input operations.PerformerInput, actorID, requestID string, now time.Time) (operations.Entity, error) {
	if len(input.Sources) == 0 {
		return operations.Entity{}, operations.ErrInvalidInput
	}
	payload := map[string]any{"display_name": input.DisplayName, "adult_status": input.AdultStatus, "activity_status": input.ActivityStatus}
	payload["aliases"] = input.Aliases
	putOptional(payload, "name_original", input.NameOriginal)
	putOptional(payload, "romanized_name", input.RomanizedName)
	putOptional(payload, "agency", input.Agency)
	putOptional(payload, "birth_year", input.BirthYear)
	putOptional(payload, "height_cm", input.HeightCM)
	putOptional(payload, "measurements", input.Measurements)
	putOptional(payload, "debut_year", input.DebutYear)
	data, err := json.Marshal(payload)
	if err != nil {
		return operations.Entity{}, fmt.Errorf("encode performer revision: %w", err)
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.Entity{}, fmt.Errorf("begin create performer: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var entity operations.Entity
	err = tx.QueryRow(ctx, `
WITH identity AS (SELECT gen_random_uuid() AS id)
INSERT INTO platform.performers (
    performer_id, display_name, name_original, romanized_name, canonical_slug, adult_status,
    activity_status, publication_status, agency, birth_year, height_cm, measurements, debut_year
)
SELECT id, $1, $2, $3, 'performer-' || replace(id::text, '-', ''), $4, $5, 'reviewing', $6, $7, $8, $9, $10 FROM identity
RETURNING performer_id::text, canonical_slug, created_at`, input.DisplayName, input.NameOriginal, input.RomanizedName,
		input.AdultStatus, input.ActivityStatus, input.Agency, input.BirthYear, input.HeightCM, input.Measurements, input.DebutYear).
		Scan(&entity.ID, &entity.CanonicalSlug, &entity.CreatedAt)
	if err != nil {
		return operations.Entity{}, mapConstraintError(err)
	}
	entity.EntityType, entity.Status, entity.RevisionStatus = "performer", "reviewing", "reviewing"
	if err := tx.QueryRow(ctx, `
INSERT INTO platform.content_revisions (entity_type, entity_id, version, payload, revision_status, author_id, reason, submitted_at)
VALUES ('performer', $1::uuid, 1, $2::jsonb, 'reviewing', $3::uuid, $4, $5)
RETURNING revision_id::text`, entity.ID, data, actorID, input.Reason, now).Scan(&entity.RevisionID); err != nil {
		return operations.Entity{}, fmt.Errorf("create performer revision: %w", err)
	}
	if err := insertRevisionProvenance(ctx, tx, "performer", entity.ID, entity.RevisionID, payload, input.Sources, actorID); err != nil {
		return operations.Entity{}, err
	}
	if err := replacePerformerAliases(ctx, tx, entity.ID, input.Aliases); err != nil {
		return operations.Entity{}, err
	}
	if err := createReviewTask(ctx, tx, "performer", entity.ID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := insertAudit(ctx, tx, actorID, "performer.create", "performer", entity.ID, nil, payload, input.Reason, requestID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.Entity{}, fmt.Errorf("commit create performer: %w", err)
	}
	return entity, nil
}

func (store *Store) CreateStudio(ctx context.Context, input operations.StudioInput, actorID, requestID string, now time.Time) (operations.Entity, error) {
	if len(input.Sources) == 0 {
		return operations.Entity{}, operations.ErrInvalidInput
	}
	payload := map[string]any{"name": input.Name, "normalized_name": strings.ToLower(strings.TrimSpace(input.Name))}
	data, err := json.Marshal(payload)
	if err != nil {
		return operations.Entity{}, operations.ErrInvalidInput
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.Entity{}, fmt.Errorf("begin create studio: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var entity operations.Entity
	err = tx.QueryRow(ctx, `
INSERT INTO platform.studios (name, normalized_name, canonical_slug, publication_status)
VALUES ($1, $2, $3, 'reviewing')
RETURNING studio_id::text, created_at`, input.Name, payload["normalized_name"], input.CanonicalSlug).Scan(&entity.ID, &entity.CreatedAt)
	if err != nil {
		return operations.Entity{}, mapConstraintError(err)
	}
	entity.EntityType, entity.CanonicalSlug, entity.Status, entity.RevisionStatus = "studio", input.CanonicalSlug, "reviewing", "reviewing"
	if err := tx.QueryRow(ctx, `
INSERT INTO platform.content_revisions (entity_type, entity_id, version, payload, revision_status, author_id, reason, submitted_at)
VALUES ('studio', $1::uuid, 1, $2::jsonb, 'reviewing', $3::uuid, $4, $5)
RETURNING revision_id::text`, entity.ID, data, actorID, input.Reason, now).Scan(&entity.RevisionID); err != nil {
		return operations.Entity{}, fmt.Errorf("create studio revision: %w", err)
	}
	if err := insertRevisionProvenance(ctx, tx, "studio", entity.ID, entity.RevisionID, payload, input.Sources, actorID); err != nil {
		return operations.Entity{}, err
	}
	if err := createReviewTask(ctx, tx, "studio", entity.ID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := insertAudit(ctx, tx, actorID, "studio.create", "studio", entity.ID, nil, payload, input.Reason, requestID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.Entity{}, fmt.Errorf("commit create studio: %w", err)
	}
	return entity, nil
}

func (store *Store) ListEntities(ctx context.Context, entityType, status string, limit int) ([]operations.CatalogEntity, error) {
	rows, err := store.pool.Query(ctx, `
WITH entities AS (
  SELECT work_id AS entity_id, 'work'::text AS entity_type,
         canonical_code || ' / ' || title AS label, publication_status, updated_at
  FROM platform.works
  UNION ALL
  SELECT performer_id, 'performer', display_name, publication_status, updated_at FROM platform.performers
  UNION ALL
  SELECT studio_id, 'studio', name, publication_status, updated_at FROM platform.studios
)
SELECT entity.entity_id::text, entity.entity_type, entity.label, entity.publication_status,
       publication.canonical_slug, publication.current_revision_id::text, entity.updated_at
FROM entities entity
LEFT JOIN platform.publications publication
  ON publication.entity_type = entity.entity_type AND publication.entity_id = entity.entity_id AND publication.site = 'main'
WHERE ($1 = 'all' OR entity.entity_type = $1)
  AND ($2 = 'all' OR entity.publication_status = $2)
ORDER BY entity.updated_at DESC, entity.entity_type, entity.entity_id
LIMIT $3`, entityType, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	defer rows.Close()
	items := make([]operations.CatalogEntity, 0)
	for rows.Next() {
		var item operations.CatalogEntity
		if err := rows.Scan(&item.ID, &item.EntityType, &item.Label, &item.Status,
			&item.CanonicalSlug, &item.CurrentRevisionID, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan entity: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func compactIdentifier(value string) string {
	var result strings.Builder
	for _, character := range strings.ToUpper(value) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			result.WriteRune(character)
		}
	}
	return result.String()
}

func putOptional[T any](target map[string]any, key string, value *T) {
	if value != nil {
		target[key] = *value
	}
}
