package database

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"self-deepsearch/services/platform-api/internal/operations"
)

// MediaDeliveryDefaultOnly is a read-only projection of the existing usage
// singleton. Absence is a valid fail-closed state; database errors are returned
// so the API can expose policy availability separately from the chosen mode.
func (store *Store) MediaDeliveryDefaultOnly(ctx context.Context, now time.Time) (bool, error) {
	var state operations.MediaUsageState
	err := store.pool.QueryRow(ctx, `SELECT period_start,period_end,last_success_at,last_until,
 review_required,stop_recommended,last_status FROM platform.media_usage_state WHERE singleton`).Scan(
		&state.PeriodStart, &state.PeriodEnd, &state.LastSuccessAt, &state.LastUntil,
		&state.ReviewRequired, &state.StopRecommended, &state.LastStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	return operations.MediaDeliveryDefaultOnly(&state, now), nil
}
