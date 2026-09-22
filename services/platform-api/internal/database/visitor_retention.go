package database

import (
	"context"
	"time"
)

func (store *Store) PurgeVisitorMetrics(ctx context.Context, now time.Time) (int64, error) {
	local := now.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	cutoff := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()).AddDate(0, 0, -29)
	result, err := store.pool.Exec(ctx, `DELETE FROM platform.visitor_metrics_daily WHERE visit_day < $1::date`, cutoff.Format(time.DateOnly))
	return result.RowsAffected(), err
}
