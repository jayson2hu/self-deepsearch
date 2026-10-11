package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (store *Store) CreateRevision(ctx context.Context, entityID string, input operations.RevisionInput, actorID, requestID string, now time.Time) (operations.Entity, error) {
	if len(input.Sources) == 0 {
		return operations.Entity{}, operations.ErrInvalidInput
	}
	patch, err := json.Marshal(input.Payload)
	if err != nil {
		return operations.Entity{}, operations.ErrInvalidInput
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.Entity{}, fmt.Errorf("begin create revision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockEntity(ctx, tx, input.EntityType, entityID); err != nil {
		return operations.Entity{}, err
	}
	currentStatus, err := readEntityStatus(ctx, tx, input.EntityType, entityID)
	if err != nil {
		return operations.Entity{}, err
	}
	if currentStatus == "takedown" || currentStatus == "merged" {
		return operations.Entity{}, operations.ErrForbidden
	}
	var reviewingExists bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM platform.content_revisions revision
  LEFT JOIN platform.publications publication
    ON publication.entity_type = revision.entity_type AND publication.entity_id = revision.entity_id AND publication.site = 'main'
  WHERE revision.entity_type = $1 AND revision.entity_id = $2::uuid
    AND (
      revision.revision_status = 'reviewing'
      OR (revision.revision_status = 'approved'
        AND revision.version = (SELECT max(latest.version) FROM platform.content_revisions latest WHERE latest.entity_type = $1 AND latest.entity_id = $2::uuid)
        AND publication.current_revision_id IS DISTINCT FROM revision.revision_id)
    )
)`, input.EntityType, entityID).Scan(&reviewingExists); err != nil {
		return operations.Entity{}, fmt.Errorf("check pending revision: %w", err)
	}
	if reviewingExists {
		return operations.Entity{}, operations.ErrConflict
	}
	var previousRevisionID string
	if err := tx.QueryRow(ctx, `
SELECT revision_id::text FROM platform.content_revisions
WHERE entity_type = $1 AND entity_id = $2::uuid
ORDER BY version DESC LIMIT 1`, input.EntityType, entityID).Scan(&previousRevisionID); errors.Is(err, pgx.ErrNoRows) {
		return operations.Entity{}, operations.ErrNotFound
	} else if err != nil {
		return operations.Entity{}, fmt.Errorf("read previous revision: %w", err)
	}
	data, err := snapshotRevisionPayload(ctx, tx, input.EntityType, entityID, patch)
	if err != nil {
		return operations.Entity{}, err
	}
	var entity operations.Entity
	entity.ID, entity.EntityType, entity.Status, entity.RevisionStatus = entityID, input.EntityType, currentStatus, "reviewing"
	if currentStatus != "published" && currentStatus != "hidden" {
		entity.Status = "reviewing"
	}
	err = tx.QueryRow(ctx, `
INSERT INTO platform.content_revisions (entity_type, entity_id, version, payload, revision_status, author_id, reason, submitted_at)
SELECT $1, $2::uuid, coalesce(max(version), 0) + 1, $3::jsonb, 'reviewing', $4::uuid, $5, $6
FROM platform.content_revisions WHERE entity_type = $1 AND entity_id = $2::uuid
RETURNING revision_id::text, created_at`, input.EntityType, entityID, data, actorID, input.Reason, now).
		Scan(&entity.RevisionID, &entity.CreatedAt)
	if err != nil {
		return operations.Entity{}, fmt.Errorf("create revision: %w", err)
	}
	changedFields := sortedPayloadFields(input.Payload)
	if err := copyRevisionProvenance(ctx, tx, previousRevisionID, entity.RevisionID, changedFields); err != nil {
		return operations.Entity{}, err
	}
	if err := insertRevisionProvenance(ctx, tx, input.EntityType, entityID, entity.RevisionID, input.Payload, input.Sources, actorID); err != nil {
		return operations.Entity{}, err
	}
	if currentStatus != "published" && currentStatus != "hidden" {
		if err := setEntityStatus(ctx, tx, input.EntityType, entityID, "reviewing"); err != nil {
			return operations.Entity{}, err
		}
	}
	if err := createReviewTask(ctx, tx, input.EntityType, entityID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := insertAudit(ctx, tx, actorID, "revision.submit", input.EntityType, entityID, nil, input.Payload, input.Reason, requestID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.Entity{}, fmt.Errorf("commit create revision: %w", err)
	}
	return entity, nil
}

func (store *Store) ListRevisions(ctx context.Context, entityID, entityType string, limit int) ([]operations.Revision, error) {
	rows, err := store.pool.Query(ctx, `
SELECT revision.revision_id::text, revision.entity_type, revision.entity_id::text, revision.version,
       revision.payload, revision.revision_status, author.normalized_email,
       reviewer.normalized_email, revision.reason, revision.created_at, revision.reviewed_at,
	       coalesce((
	         SELECT jsonb_agg(jsonb_build_object(
	           'field_name', provenance.field_name,
	           'source_type', provenance.source_type,
	           'source_url', provenance.source_url,
	           'source_title', provenance.source_title,
	           'checked_at', provenance.checked_at,
	           'operator', operator.normalized_email,
	           'confidence', provenance.confidence,
	           'rights_status', provenance.rights_status
	         ) ORDER BY provenance.field_name, provenance.checked_at DESC, provenance.provenance_id)
	         FROM platform.field_provenance provenance
	         LEFT JOIN platform.users operator ON operator.user_id = provenance.operator_id
	         WHERE provenance.revision_id = revision.revision_id
	       ), '[]'::jsonb),
	       coalesce(publication.current_revision_id = revision.revision_id, false), publication.canonical_slug
FROM platform.content_revisions revision
JOIN platform.users author ON author.user_id = revision.author_id
LEFT JOIN platform.users reviewer ON reviewer.user_id = revision.reviewer_id
LEFT JOIN platform.publications publication
  ON publication.entity_type = revision.entity_type AND publication.entity_id = revision.entity_id AND publication.site = 'main'
WHERE revision.entity_type = $1 AND revision.entity_id = $2::uuid
ORDER BY revision.version DESC LIMIT $3`, entityType, entityID, limit)
	if err != nil {
		return nil, fmt.Errorf("list revisions: %w", err)
	}
	defer rows.Close()
	items := make([]operations.Revision, 0)
	for rows.Next() {
		var item operations.Revision
		var payload, sources []byte
		if err := rows.Scan(&item.ID, &item.EntityType, &item.EntityID, &item.Version, &payload,
			&item.Status, &item.Author, &item.Reviewer, &item.Reason, &item.CreatedAt,
			&item.ReviewedAt, &sources, &item.IsCurrent, &item.CanonicalSlug); err != nil {
			return nil, fmt.Errorf("scan revision: %w", err)
		}
		if err := json.Unmarshal(payload, &item.Payload); err != nil {
			return nil, fmt.Errorf("decode revision payload: %w", err)
		}
		if err := json.Unmarshal(sources, &item.Sources); err != nil {
			return nil, fmt.Errorf("decode revision provenance: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func snapshotRevisionPayload(ctx context.Context, tx pgx.Tx, entityType, entityID string, patch []byte) ([]byte, error) {
	if _, _, err := entityTable(entityType); err != nil {
		return nil, operations.ErrInvalidInput
	}
	var snapshot []byte
	if err := tx.QueryRow(ctx, `
SELECT payload || $3::jsonb
FROM platform.content_revisions
WHERE entity_type = $1 AND entity_id = $2::uuid
ORDER BY version DESC LIMIT 1`, entityType, entityID, patch).Scan(&snapshot); errors.Is(err, pgx.ErrNoRows) {
		return nil, operations.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("snapshot %s revision: %w", entityType, err)
	}
	return snapshot, nil
}

func applyRevision(ctx context.Context, tx pgx.Tx, entityType, entityID string, payload []byte) error {
	var rowsAffected int64
	switch entityType {
	case "work":
		command, err := tx.Exec(ctx, `
UPDATE platform.works SET
 canonical_code = coalesce($2::jsonb->>'canonical_code', canonical_code),
 compact_code = coalesce($2::jsonb->>'compact_code', compact_code),
 title = coalesce($2::jsonb->>'title', title),
 title_original = CASE WHEN $2::jsonb ? 'title_original' THEN nullif($2::jsonb->>'title_original', '') ELSE title_original END,
 release_date = CASE WHEN $2::jsonb ? 'release_date' THEN nullif($2::jsonb->>'release_date', '')::date ELSE release_date END,
 studio_id = CASE WHEN $2::jsonb ? 'studio_id' THEN nullif($2::jsonb->>'studio_id', '')::uuid ELSE studio_id END,
 summary = CASE WHEN $2::jsonb ? 'summary' THEN nullif($2::jsonb->>'summary', '') ELSE summary END,
 publication_status = 'published'
WHERE work_id = $1::uuid`, entityID, payload)
		if err != nil {
			return mapConstraintError(err)
		}
		rowsAffected = command.RowsAffected()
		if rowsAffected > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM platform.work_performers WHERE work_id = $1::uuid AND $2::jsonb ? 'performer_ids'`, entityID, payload); err != nil {
				return fmt.Errorf("replace work performers: %w", err)
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO platform.work_performers (work_id, performer_id, role, position)
SELECT $1::uuid, value::uuid, 'performer', ordinal - 1
FROM jsonb_array_elements_text(coalesce($2::jsonb->'performer_ids', '[]'::jsonb)) WITH ORDINALITY entry(value, ordinal)`, entityID, payload); err != nil {
				return mapConstraintError(err)
			}
		}
	case "performer":
		command, err := tx.Exec(ctx, `
UPDATE platform.performers SET
 display_name = coalesce($2::jsonb->>'display_name', display_name),
 name_original = CASE WHEN $2::jsonb ? 'name_original' THEN nullif($2::jsonb->>'name_original', '') ELSE name_original END,
 romanized_name = CASE WHEN $2::jsonb ? 'romanized_name' THEN nullif($2::jsonb->>'romanized_name', '') ELSE romanized_name END,
 adult_status = coalesce($2::jsonb->>'adult_status', adult_status),
 activity_status = coalesce($2::jsonb->>'activity_status', activity_status),
 agency = CASE WHEN $2::jsonb ? 'agency' THEN nullif($2::jsonb->>'agency', '') ELSE agency END,
 birth_year = CASE WHEN $2::jsonb ? 'birth_year' THEN nullif($2::jsonb->>'birth_year', '')::smallint ELSE birth_year END,
 height_cm = CASE WHEN $2::jsonb ? 'height_cm' THEN nullif($2::jsonb->>'height_cm', '')::smallint ELSE height_cm END,
 measurements = CASE WHEN $2::jsonb ? 'measurements' THEN nullif($2::jsonb->>'measurements', '') ELSE measurements END,
 debut_year = CASE WHEN $2::jsonb ? 'debut_year' THEN nullif($2::jsonb->>'debut_year', '')::smallint ELSE debut_year END,
 publication_status = 'published'
WHERE performer_id = $1::uuid`, entityID, payload)
		if err != nil {
			return mapConstraintError(err)
		}
		rowsAffected = command.RowsAffected()
		if rowsAffected > 0 {
			var aliases []string
			var hasAliases bool
			var patch map[string]json.RawMessage
			if err := json.Unmarshal(payload, &patch); err != nil {
				return operations.ErrInvalidInput
			}
			if rawAliases, exists := patch["aliases"]; exists {
				hasAliases = true
				if err := json.Unmarshal(rawAliases, &aliases); err != nil {
					return operations.ErrInvalidInput
				}
			}
			if hasAliases {
				if err := replacePerformerAliases(ctx, tx, entityID, aliases); err != nil {
					return err
				}
			}
		}
	case "studio":
		command, err := tx.Exec(ctx, `
UPDATE platform.studios SET
 name = coalesce($2::jsonb->>'name', name),
 normalized_name = coalesce($2::jsonb->>'normalized_name', normalized_name),
 publication_status = 'published'
WHERE studio_id = $1::uuid`, entityID, payload)
		if err != nil {
			return mapConstraintError(err)
		}
		rowsAffected = command.RowsAffected()
	default:
		return operations.ErrInvalidInput
	}
	if rowsAffected == 0 {
		return operations.ErrNotFound
	}
	return nil
}

func replacePerformerAliases(ctx context.Context, tx pgx.Tx, performerID string, aliases []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM platform.performer_aliases WHERE performer_id = $1::uuid`, performerID); err != nil {
		return fmt.Errorf("replace performer aliases: %w", err)
	}
	for _, alias := range aliases {
		normalized := strings.ToLower(strings.Join(strings.Fields(alias), " "))
		if _, err := tx.Exec(ctx, `
INSERT INTO platform.performer_aliases (performer_id, alias_type, raw_value, normalized_value)
VALUES ($1::uuid, 'stage_name', $2, $3)`, performerID, alias, normalized); err != nil {
			return mapConstraintError(err)
		}
	}
	return nil
}

func sortedPayloadFields(payload map[string]any) []string {
	fields := make([]string, 0, len(payload))
	for field := range payload {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func stringPointer(value string) *string {
	return &value
}
