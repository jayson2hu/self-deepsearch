package visitorretention

import (
	"context"
	"log/slog"
	"self-deepsearch/services/platform-api/internal/logsafe"
	"time"
)

type Repository interface {
	PurgeVisitorMetrics(context.Context, time.Time) (int64, error)
}

func Run(ctx context.Context, repo Repository, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		attempt, cancel := context.WithTimeout(ctx, 30*time.Second)
		count, err := repo.PurgeVisitorMetrics(attempt, time.Now().UTC())
		cancel()
		if err != nil && ctx.Err() == nil {
			logger.Warn("visitor_retention_failed", "error_class", logsafe.ErrorClass(err))
		}
		if count > 0 && err == nil {
			logger.Info("visitor_retention_completed", "expired_rows", count)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
