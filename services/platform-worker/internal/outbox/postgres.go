package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func OpenPostgres(ctx context.Context, databaseURL string) (*PostgresRepository, error) {
	configuration, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse worker DATABASE_URL: %w", err)
	}
	configuration.MaxConns = 3
	configuration.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		return nil, fmt.Errorf("create worker database pool: %w", err)
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) Close() { repository.pool.Close() }

func (repository *PostgresRepository) Pool() *pgxpool.Pool { return repository.pool }

func (repository *PostgresRepository) Ping(ctx context.Context) error {
	return repository.pool.Ping(ctx)
}

func (repository *PostgresRepository) Claim(ctx context.Context, workerID string, now time.Time, leaseFor time.Duration, limit int) ([]Event, error) {
	rows, err := repository.pool.Query(ctx, `
WITH candidates AS (
  SELECT event_id, attempt_count FROM platform.outbox_events
  WHERE event_type IN ('publication_changed', 'publication_hidden', 'publication_takedown', 'search_refresh', 'cache_purge', 'sitemap_refresh', 'media_delete')
    AND ((status IN ('pending', 'retry') AND available_at <= $2)
      OR (status = 'running' AND lease_expires_at < $2))
  ORDER BY available_at, created_at
  FOR UPDATE SKIP LOCKED LIMIT $4
), exhausted AS (
  UPDATE platform.outbox_events event
  SET status = 'dead', lease_owner = NULL, lease_expires_at = NULL,
      last_error_code = 'attempts_exhausted'
  FROM candidates
  WHERE event.event_id = candidates.event_id AND candidates.attempt_count >= 10
  RETURNING event.event_id
), claimed AS (
  UPDATE platform.outbox_events event
  SET status = 'running', lease_owner = $1, lease_expires_at = $2 + $3::interval,
      attempt_count = event.attempt_count + 1, last_error_code = NULL
  FROM candidates WHERE event.event_id = candidates.event_id AND candidates.attempt_count < 10
  RETURNING event.event_id::text, event.aggregate_type, event.aggregate_id::text,
            event.event_type, event.payload, event.attempt_count, event.lease_expires_at
)
SELECT * FROM claimed`, workerID, now, intervalLiteral(leaseFor), limit)
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	defer rows.Close()
	items := make([]Event, 0)
	for rows.Next() {
		var item Event
		if err := rows.Scan(&item.ID, &item.AggregateType, &item.AggregateID, &item.Type, &item.Payload, &item.AttemptCount, &item.LeaseExpiresAt); err != nil {
			return nil, fmt.Errorf("scan outbox event: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (repository *PostgresRepository) Complete(ctx context.Context, eventID, workerID string, attemptCount int, now time.Time) error {
	if attemptCount < 1 {
		return ErrLeaseLost
	}
	command, err := repository.pool.Exec(ctx, `
WITH target AS MATERIALIZED (
  SELECT event_type, payload FROM platform.outbox_events
  WHERE event_id = $1::uuid AND status = 'running' AND lease_owner = $2
    AND attempt_count = $4 AND lease_expires_at > $3 AND lease_expires_at > clock_timestamp()
  FOR UPDATE
), finalized_media AS (
  UPDATE platform.media_objects object
  SET object_status = 'deleted', deleted_at = coalesce(object.deleted_at, $3)
  FROM target
  WHERE target.event_type = 'media_delete'
	AND object.storage_provider = 's3'
	AND object.storage_scope = target.payload->>'storage_scope'
    AND object.storage_key = target.payload->>'storage_key'
    AND object.object_status IN ('hidden', 'deleted')
  RETURNING object.media_object_id
)
UPDATE platform.outbox_events event
SET status = 'completed', completed_at = $3, lease_owner = NULL, lease_expires_at = NULL
FROM target
WHERE event.event_id = $1::uuid AND event.status = 'running' AND event.lease_owner = $2
  AND event.attempt_count = $4
  AND (target.event_type <> 'media_delete' OR EXISTS (SELECT 1 FROM finalized_media))`, eventID, workerID, now, attemptCount)
	if err != nil {
		return fmt.Errorf("complete outbox event: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (repository *PostgresRepository) Fail(ctx context.Context, eventID, workerID string, attemptCount int, errorCode string, now time.Time) error {
	if attemptCount < 1 {
		return ErrLeaseLost
	}
	command, err := repository.pool.Exec(ctx, `
UPDATE platform.outbox_events
SET status = CASE WHEN attempt_count >= 10 THEN 'dead' ELSE 'retry' END,
    available_at = $3 + make_interval(secs => least(3600, (power(2, least(attempt_count, 10)) * 5)::int)),
    lease_owner = NULL, lease_expires_at = NULL, last_error_code = $4
WHERE event_id = $1::uuid AND status = 'running' AND lease_owner = $2
  AND attempt_count = $5 AND lease_expires_at > $3 AND lease_expires_at > clock_timestamp()`, eventID, workerID, now, errorCode, attemptCount)
	if err != nil {
		return fmt.Errorf("retry outbox event: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func intervalLiteral(value time.Duration) string {
	seconds := int(value.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("%d seconds", seconds)
}
