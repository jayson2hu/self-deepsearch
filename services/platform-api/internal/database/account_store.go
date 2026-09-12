package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/account"
	"self-deepsearch/services/platform-api/internal/catalog"
	"self-deepsearch/services/platform-api/internal/identity"
)

func (store *Store) SetFavorite(ctx context.Context, userID, workID string, active bool, now time.Time) error {
	if active {
		result, err := store.pool.Exec(ctx, `
INSERT INTO platform.favorites (user_id, work_id, created_at)
SELECT $1::uuid, work_id, $3 FROM platform.public_published_works WHERE work_id = $2::uuid
ON CONFLICT (user_id, work_id) DO UPDATE
SET is_deleted = false, deleted_at = NULL, created_at = EXCLUDED.created_at`, userID, workID, now)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return account.ErrNotFound
		}
		return nil
	}
	_, err := store.pool.Exec(ctx, `
UPDATE platform.favorites SET is_deleted = true, deleted_at = $3
WHERE user_id = $1::uuid AND work_id = $2::uuid AND NOT is_deleted`, userID, workID, now)
	return err
}

func (store *Store) ListFavorites(ctx context.Context, userID string, limit int) ([]catalog.WorkSummary, error) {
	query := `SELECT ` + workSummaryColumns + `
FROM platform.favorites AS favorite
JOIN platform.public_published_works AS work ON work.work_id = favorite.work_id
` + workSummaryJoins + `
WHERE favorite.user_id = $1::uuid AND NOT favorite.is_deleted
ORDER BY favorite.created_at DESC
LIMIT $2`
	return store.queryWorkSummaries(ctx, query, userID, limit)
}

func (store *Store) SetFollow(ctx context.Context, userID, performerID string, active bool, now time.Time) error {
	if active {
		result, err := store.pool.Exec(ctx, `
INSERT INTO platform.follows (user_id, performer_id, created_at)
SELECT $1::uuid, performer_id, $3 FROM platform.public_published_performers WHERE performer_id = $2::uuid
ON CONFLICT (user_id, performer_id) DO UPDATE
SET is_deleted = false, deleted_at = NULL, created_at = EXCLUDED.created_at`, userID, performerID, now)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return account.ErrNotFound
		}
		return nil
	}
	_, err := store.pool.Exec(ctx, `
UPDATE platform.follows SET is_deleted = true, deleted_at = $3
WHERE user_id = $1::uuid AND performer_id = $2::uuid AND NOT is_deleted`, userID, performerID, now)
	return err
}

func (store *Store) ListFollows(ctx context.Context, userID string, limit int) ([]catalog.PerformerSummary, error) {
	rows, err := store.pool.Query(ctx, `
SELECT performer.performer_id::text, performer.display_name, performer.canonical_slug, media.public_url
FROM platform.follows AS follow
JOIN platform.public_published_performers AS performer ON performer.performer_id = follow.performer_id
LEFT JOIN LATERAL (
    SELECT public_url FROM platform.public_entity_media
    WHERE entity_type = 'performer' AND entity_id = performer.performer_id
      AND purpose = 'avatar' AND is_primary AND position = 0
    ORDER BY CASE rendition WHEN 'w320' THEN 1 WHEN 'w640' THEN 2 WHEN 'w960' THEN 3 ELSE 4 END
    LIMIT 1
) AS media ON true
WHERE follow.user_id = $1::uuid AND NOT follow.is_deleted
ORDER BY follow.created_at DESC
LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]catalog.PerformerSummary, 0)
	for rows.Next() {
		var item catalog.PerformerSummary
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.ImageURL); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) ListFollowedWorks(ctx context.Context, userID string, limit int) ([]catalog.WorkSummary, error) {
	query := `SELECT ` + workSummaryColumns + `
FROM platform.public_published_works AS work
` + workSummaryJoins + `
WHERE EXISTS (
      SELECT 1 FROM platform.work_performers link
      JOIN platform.follows follow ON follow.performer_id = link.performer_id
      WHERE link.work_id = work.work_id AND follow.user_id = $1::uuid AND NOT follow.is_deleted
  )
  AND NOT EXISTS (
      SELECT 1 FROM platform.hidden_preferences preference
      WHERE preference.user_id = $1::uuid AND preference.work_id = work.work_id AND preference.status = 'active'
  )
ORDER BY work.release_date DESC NULLS LAST, work.published_at DESC, work.work_id
LIMIT $2`
	return store.queryWorkSummaries(ctx, query, userID, limit)
}

func (store *Store) SetHiddenWork(ctx context.Context, userID, workID string, active bool, now time.Time) error {
	if active {
		result, err := store.pool.Exec(ctx, `
INSERT INTO platform.hidden_preferences (user_id, work_id, status, created_at, updated_at)
SELECT $1::uuid, work_id, 'active', $3, $3
FROM platform.public_published_works WHERE work_id = $2::uuid
ON CONFLICT (user_id, work_id) DO UPDATE
SET status = 'active', updated_at = EXCLUDED.updated_at`, userID, workID, now)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return account.ErrNotFound
		}
		return nil
	}
	_, err := store.pool.Exec(ctx, `
UPDATE platform.hidden_preferences SET status = 'removed', updated_at = $3
WHERE user_id = $1::uuid AND work_id = $2::uuid AND status = 'active'`, userID, workID, now)
	return err
}

func (store *Store) ListHiddenWorks(ctx context.Context, userID string, limit int) ([]catalog.WorkSummary, error) {
	query := `SELECT ` + workSummaryColumns + `
FROM platform.hidden_preferences AS preference
JOIN platform.public_published_works AS work ON work.work_id = preference.work_id
` + workSummaryJoins + `
WHERE preference.user_id = $1::uuid AND preference.status = 'active'
ORDER BY preference.updated_at DESC
LIMIT $2`
	return store.queryWorkSummaries(ctx, query, userID, limit)
}

func (store *Store) RecordHistory(ctx context.Context, userID, contentType, contentID string, viewedAt time.Time) error {
	var exists bool
	var query string
	switch contentType {
	case "work":
		query = `SELECT EXISTS (SELECT 1 FROM platform.public_published_works WHERE work_id = $1::uuid)`
	case "performer":
		query = `SELECT EXISTS (SELECT 1 FROM platform.public_published_performers WHERE performer_id = $1::uuid)`
	case "studio":
		query = `SELECT EXISTS (SELECT 1 FROM platform.public_published_studios WHERE studio_id = $1::uuid)`
	default:
		return account.ErrNotFound
	}
	if err := store.pool.QueryRow(ctx, query, contentID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return account.ErrNotFound
	}
	_, err := store.pool.Exec(ctx, `
INSERT INTO platform.view_history (user_id, content_type, content_id, viewed_at)
VALUES ($1::uuid, $2, $3::uuid, $4)`, userID, contentType, contentID, viewedAt)
	return err
}

func (store *Store) ListHistory(ctx context.Context, userID string, limit int) ([]account.HistoryItem, error) {
	// Keep history as a compact union of the three public entity types. A
	// hidden/taken-down entity naturally drops out because each branch joins a
	// published view; the underlying history row remains available for the
	// retention/archival workflow.
	query := `SELECT entries.content_type, entries.content_id, entries.code,
       entries.title, entries.subtitle, entries.href, entries.image_url,
       entries.viewed_at
FROM (
  SELECT history.history_id, history.content_type,
         work.work_id::text AS content_id, work.canonical_code AS code,
         work.title,
         concat_ws(' · ', to_char(work.release_date, 'YYYY-MM-DD'), studio.name) AS subtitle,
         '/works/' || work.canonical_slug AS href,
         media.public_url AS image_url, history.viewed_at
  FROM platform.view_history AS history
  JOIN platform.public_published_works AS work
    ON work.work_id = history.content_id AND history.content_type = 'work'
  LEFT JOIN platform.public_published_studios AS studio ON studio.studio_id = work.studio_id
  LEFT JOIN LATERAL (
    SELECT public_url
    FROM platform.public_entity_media
    WHERE entity_type = 'work' AND entity_id = work.work_id
      AND purpose = 'cover' AND is_primary AND position = 0
    ORDER BY CASE rendition WHEN 'w640' THEN 1 WHEN 'w320' THEN 2 WHEN 'w960' THEN 3 ELSE 4 END
    LIMIT 1
  ) AS media ON true
  WHERE history.user_id = $1::uuid AND NOT history.is_deleted
  UNION ALL
  SELECT history.history_id, history.content_type,
         performer.performer_id::text AS content_id, ''::text AS code,
         performer.display_name AS title, '人物资料'::text AS subtitle,
         '/performers/' || performer.canonical_slug AS href,
         media.public_url AS image_url, history.viewed_at
  FROM platform.view_history AS history
  JOIN platform.public_published_performers AS performer
    ON performer.performer_id = history.content_id AND history.content_type = 'performer'
  LEFT JOIN LATERAL (
    SELECT public_url
    FROM platform.public_entity_media
    WHERE entity_type = 'performer' AND entity_id = performer.performer_id
      AND purpose = 'avatar' AND is_primary AND position = 0
    ORDER BY CASE rendition WHEN 'w320' THEN 1 WHEN 'w640' THEN 2 WHEN 'w960' THEN 3 ELSE 4 END
    LIMIT 1
  ) AS media ON true
  WHERE history.user_id = $1::uuid AND NOT history.is_deleted
  UNION ALL
  SELECT history.history_id, history.content_type,
         studio.studio_id::text AS content_id, ''::text AS code,
         studio.name AS title, '厂牌资料'::text AS subtitle,
         '/studios/' || studio.canonical_slug AS href,
         NULL::text AS image_url, history.viewed_at
  FROM platform.view_history AS history
  JOIN platform.public_published_studios AS studio
    ON studio.studio_id = history.content_id AND history.content_type = 'studio'
  WHERE history.user_id = $1::uuid AND NOT history.is_deleted
) AS entries
ORDER BY entries.viewed_at DESC, entries.history_id DESC
LIMIT $2`
	rows, err := store.pool.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]account.HistoryItem, 0)
	for rows.Next() {
		var item account.HistoryItem
		if err := rows.Scan(&item.ContentType, &item.ID, &item.Code, &item.Title,
			&item.Subtitle, &item.Href, &item.ImageURL, &item.ViewedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) ClearHistory(ctx context.Context, userID string, now time.Time) (int64, error) {
	result, err := store.pool.Exec(ctx, `
UPDATE platform.view_history SET is_deleted = true, deleted_at = $2
WHERE user_id = $1::uuid AND NOT is_deleted`, userID, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (store *Store) CreateFeedback(ctx context.Context, userID string, contentType, contentID *string, feedbackType, message string, evidenceURL *string, now time.Time) (account.Feedback, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return account.Feedback{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	start := now.Truncate(24 * time.Hour)
	count, err := incrementRate(ctx, tx, "feedback", "user_id", userID, start, 24*time.Hour)
	if err != nil {
		return account.Feedback{}, err
	}
	if count > 10 {
		if err := tx.Commit(ctx); err != nil {
			return account.Feedback{}, err
		}
		return account.Feedback{}, identity.ErrRateLimited
	}
	if contentType != nil && contentID != nil {
		exists, err := publishedEntityExists(ctx, tx, *contentType, *contentID)
		if err != nil {
			return account.Feedback{}, err
		}
		if !exists {
			return account.Feedback{}, account.ErrNotFound
		}
	}
	var item account.Feedback
	err = tx.QueryRow(ctx, `
INSERT INTO platform.feedback (
    user_id, content_type, content_id, feedback_type, message, evidence_url, created_at, updated_at
) VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $7)
RETURNING feedback_id::text, content_type, content_id::text, feedback_type, message, evidence_url, review_status, created_at`,
		userID, contentType, contentID, feedbackType, message, evidenceURL, now).Scan(
		&item.ID, &item.ContentType, &item.ContentID, &item.FeedbackType, &item.Message, &item.EvidenceURL, &item.ReviewStatus, &item.CreatedAt,
	)
	if err != nil {
		return account.Feedback{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return account.Feedback{}, err
	}
	return item, nil
}

func (store *Store) ListFeedback(ctx context.Context, userID string, limit int) ([]account.Feedback, error) {
	rows, err := store.pool.Query(ctx, `
SELECT feedback_id::text, content_type, content_id::text, feedback_type, message, evidence_url, review_status, created_at
FROM platform.feedback WHERE user_id = $1::uuid
ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]account.Feedback, 0)
	for rows.Next() {
		var item account.Feedback
		if err := rows.Scan(&item.ID, &item.ContentType, &item.ContentID, &item.FeedbackType, &item.Message, &item.EvidenceURL, &item.ReviewStatus, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) ListFeedbackForReview(ctx context.Context, status string, limit int) ([]account.Feedback, error) {
	rows, err := store.pool.Query(ctx, `
SELECT feedback.feedback_id::text, feedback.content_type, feedback.content_id::text,
       feedback.feedback_type, feedback.message, feedback.evidence_url, feedback.review_status,
       submitter.normalized_email, reviewer.normalized_email, feedback.reviewed_at, feedback.created_at
FROM platform.feedback feedback
JOIN platform.users submitter ON submitter.user_id = feedback.user_id
LEFT JOIN platform.users reviewer ON reviewer.user_id = feedback.reviewer_id
WHERE ($1 = 'all' OR feedback.review_status = $1)
ORDER BY CASE feedback.review_status WHEN 'pending' THEN 1 WHEN 'reviewing' THEN 2 ELSE 3 END,
         feedback.created_at ASC
LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]account.Feedback, 0)
	for rows.Next() {
		var item account.Feedback
		if err := rows.Scan(&item.ID, &item.ContentType, &item.ContentID, &item.FeedbackType,
			&item.Message, &item.EvidenceURL, &item.ReviewStatus, &item.Submitter,
			&item.Reviewer, &item.ReviewedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) ReviewFeedback(ctx context.Context, feedbackID, status, reviewerID, reason, requestID string, now time.Time) (account.Feedback, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return account.Feedback{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var previous string
	if err := tx.QueryRow(ctx, `SELECT review_status FROM platform.feedback WHERE feedback_id = $1::uuid FOR UPDATE`, feedbackID).Scan(&previous); err == pgx.ErrNoRows {
		return account.Feedback{}, account.ErrNotFound
	} else if err != nil {
		return account.Feedback{}, err
	}
	if !account.ValidFeedbackReviewTransition(previous, status) {
		return account.Feedback{}, account.ErrConflict
	}
	reviewedAt := any(nil)
	if status != "reviewing" {
		reviewedAt = now
	}
	if _, err := tx.Exec(ctx, `
UPDATE platform.feedback
SET review_status = $2, reviewer_id = $3::uuid, reviewed_at = $4
WHERE feedback_id = $1::uuid`, feedbackID, status, reviewerID, reviewedAt); err != nil {
		return account.Feedback{}, err
	}
	if err := insertAudit(ctx, tx, reviewerID, "feedback.review", "feedback", feedbackID,
		map[string]any{"review_status": previous}, map[string]any{"review_status": status}, reason, requestID, now); err != nil {
		return account.Feedback{}, err
	}
	var item account.Feedback
	err = tx.QueryRow(ctx, `
SELECT feedback.feedback_id::text, feedback.content_type, feedback.content_id::text,
       feedback.feedback_type, feedback.message, feedback.evidence_url, feedback.review_status,
       submitter.normalized_email, reviewer.normalized_email, feedback.reviewed_at, feedback.created_at
FROM platform.feedback feedback
JOIN platform.users submitter ON submitter.user_id = feedback.user_id
LEFT JOIN platform.users reviewer ON reviewer.user_id = feedback.reviewer_id
WHERE feedback.feedback_id = $1::uuid`, feedbackID).Scan(
		&item.ID, &item.ContentType, &item.ContentID, &item.FeedbackType, &item.Message,
		&item.EvidenceURL, &item.ReviewStatus, &item.Submitter, &item.Reviewer, &item.ReviewedAt, &item.CreatedAt,
	)
	if err != nil {
		return account.Feedback{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return account.Feedback{}, err
	}
	return item, nil
}

func publishedEntityExists(ctx context.Context, tx pgx.Tx, entityType, entityID string) (bool, error) {
	var query string
	switch entityType {
	case "work":
		query = `SELECT EXISTS (SELECT 1 FROM platform.public_published_works WHERE work_id = $1::uuid)`
	case "performer":
		query = `SELECT EXISTS (SELECT 1 FROM platform.public_published_performers WHERE performer_id = $1::uuid)`
	case "studio":
		query = `SELECT EXISTS (SELECT 1 FROM platform.public_published_studios WHERE studio_id = $1::uuid)`
	default:
		return false, account.ErrNotFound
	}
	var exists bool
	err := tx.QueryRow(ctx, query, entityID).Scan(&exists)
	return exists, err
}
