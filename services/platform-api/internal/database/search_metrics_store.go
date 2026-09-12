package database

import (
	"context"
	"fmt"

	"self-deepsearch/services/platform-api/internal/catalog"
)

func (store *Store) RecordSearchOutcome(ctx context.Context, zeroResult bool) error {
	zeroIncrement := 0
	if zeroResult {
		zeroIncrement = 1
	}
	_, err := store.pool.Exec(ctx, `
INSERT INTO platform.search_metrics_hourly (hour_bucket, search_requests, zero_result_requests)
VALUES (date_trunc('hour', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC', 1, $1)
ON CONFLICT (hour_bucket) DO UPDATE SET
  search_requests = platform.search_metrics_hourly.search_requests + 1,
  zero_result_requests = platform.search_metrics_hourly.zero_result_requests + EXCLUDED.zero_result_requests,
  updated_at = now()`, zeroIncrement)
	if err != nil {
		return fmt.Errorf("record search outcome: %w", err)
	}
	return nil
}

var _ catalog.Repository = (*Store)(nil)
