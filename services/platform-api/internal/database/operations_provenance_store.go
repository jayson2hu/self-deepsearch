package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

func insertRevisionProvenance(
	ctx context.Context,
	tx pgx.Tx,
	entityType, entityID, revisionID string,
	payload map[string]any,
	sources []operations.SourceEvidenceInput,
	actorID string,
) error {
	fields := sortedPayloadFields(payload)
	if len(fields) == 0 || len(sources) == 0 {
		return operations.ErrInvalidInput
	}
	for _, field := range fields {
		for _, source := range sources {
			if _, err := tx.Exec(ctx, `
INSERT INTO platform.field_provenance (
  entity_type, entity_id, revision_id, field_name, source_id, source_type,
  source_url, source_title, checked_at, operator_id, confidence, rights_status
) VALUES ($1, $2::uuid, $3::uuid, $4, $5::uuid, $6, $7, $8, $9, $10::uuid, 1, 'needs_review')`,
				entityType, entityID, revisionID, field, source.SourceID, source.SourceType,
				source.SourceURL, source.SourceTitle, source.CheckedAt, actorID); err != nil {
				return mapConstraintError(err)
			}
		}
	}
	complete, err := revisionProvenanceComplete(ctx, tx, revisionID)
	if err != nil {
		return err
	}
	if !complete {
		return operations.ErrInvalidInput
	}
	return nil
}

func copyRevisionProvenance(ctx context.Context, tx pgx.Tx, sourceRevisionID, targetRevisionID string, excludedFields []string) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.field_provenance (
  entity_type, entity_id, revision_id, field_name, source_id, source_type,
  source_url, source_title, checked_at, operator_id, confidence, rights_status
)
SELECT entity_type, entity_id, $2::uuid, field_name, source_id, source_type,
       source_url, source_title, checked_at, operator_id, confidence, rights_status
FROM platform.field_provenance
WHERE revision_id = $1::uuid
  AND NOT (field_name = ANY(coalesce($3::text[], ARRAY[]::text[])))`, sourceRevisionID, targetRevisionID, excludedFields); err != nil {
		return fmt.Errorf("copy revision provenance: %w", err)
	}
	return nil
}

func revisionProvenanceComplete(ctx context.Context, tx pgx.Tx, revisionID string) (bool, error) {
	var complete bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM platform.content_revisions WHERE revision_id = $1::uuid
) AND NOT EXISTS (
  SELECT 1
  FROM platform.content_revisions revision
  CROSS JOIN LATERAL jsonb_object_keys(revision.payload) field(field_name)
  WHERE revision.revision_id = $1::uuid
    AND NOT EXISTS (
      SELECT 1 FROM platform.field_provenance provenance
      WHERE provenance.revision_id = revision.revision_id
        AND provenance.field_name = field.field_name
    )
)`, revisionID).Scan(&complete); err != nil {
		return false, fmt.Errorf("check revision provenance: %w", err)
	}
	return complete, nil
}
