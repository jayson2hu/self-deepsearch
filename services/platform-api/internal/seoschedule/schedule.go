// Package seoschedule runs opt-in internal scans; never makes external HTTP requests.
package seoschedule

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"self-deepsearch/services/platform-api/internal/logsafe"
	"self-deepsearch/services/platform-api/internal/operations"
)

type Repository interface {
	RunScheduledSEOScan(context.Context, string, time.Time) (operations.SEOScan, error)
}

func Run(ctx context.Context, repo Repository, actor string, logger *slog.Logger) {
	run(ctx, repo, actor, logger, time.Minute)
}

func run(ctx context.Context, repo Repository, actor string, logger *slog.Logger, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		attempt, cancel := context.WithTimeout(ctx, 2*time.Minute)
		scan, err := repo.RunScheduledSEOScan(attempt, actor, time.Now().UTC())
		cancel()
		if err != nil && !errors.Is(err, operations.ErrConflict) && ctx.Err() == nil {
			logger.Warn("seo_daily_scan_failed", "error_class", logsafe.ErrorClass(err))
		}
		if err == nil && scan.ID != "" {
			logger.Info("seo_daily_scan_completed", "cycle_id", scan.CycleID, "batches", scan.BatchCount, "scanned", scan.TotalScannedCount)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
