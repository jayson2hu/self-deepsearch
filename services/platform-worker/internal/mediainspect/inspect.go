// Package mediainspect audits registered publication state and the two fixed
// display fallback assets. It never changes catalog/media state or fetches a
// URL supplied by a catalog record, source page, or user.
package mediainspect

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"self-deepsearch/services/platform-worker/internal/logsafe"
)

const Every = 24 * time.Hour

var (
	ErrNotDue            = errors.New("media inspection is not due")
	ErrInventoryTooLarge = errors.New("media inspection inventory exceeds limit")
)

type Report struct {
	RunID                                     string
	ObjectCount, LinkCount, PublicationIssues int
	DefaultFailures                           int
	IssueSamples                              map[string][]string
	Defaults                                  []DefaultResult
}

type Repository interface {
	Prepare(context.Context, time.Time, string) (string, error)
	Snapshot(context.Context) (Snapshot, error)
	Complete(context.Context, Report, time.Time) error
	Fail(context.Context, string, string, time.Time) error
}

type DefaultChecker interface {
	Origin() string
	Check(context.Context) []DefaultResult
}

type Runner struct {
	Repository Repository
	Defaults   DefaultChecker
	Logger     *slog.Logger
	Now        func() time.Time
}

func (r Runner) Run(ctx context.Context) error {
	if r.Repository == nil || r.Defaults == nil {
		return errors.New("media inspection repository and default checker are required")
	}
	if r.Logger == nil {
		r.Logger = slog.Default()
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	r.runOnce(ctx)
	// Poll cheaply so a restarted/second worker does not delay a due daily run
	// for another full day. PostgreSQL, not the ticker, owns the schedule.
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r Runner) runOnce(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	id, err := r.Repository.Prepare(ctx, r.Now().UTC(), r.Defaults.Origin())
	if errors.Is(err, ErrNotDue) {
		return
	}
	if err != nil {
		r.Logger.ErrorContext(ctx, "media_inspection_prepare_failed", "error_class", logsafe.ErrorClass(err))
		return
	}
	snapshot, err := r.Repository.Snapshot(ctx)
	if err != nil {
		code := "snapshot_unavailable"
		if errors.Is(err, ErrInventoryTooLarge) {
			code = "inventory_too_large"
		}
		r.fail(parent, id, code)
		return
	}
	report := Inspect(snapshot)
	report.RunID = id
	report.Defaults = r.Defaults.Check(ctx)
	if !validDefaultResults(report.Defaults) {
		r.fail(parent, id, "invalid_default_report")
		return
	}
	for _, result := range report.Defaults {
		if result.ErrorCode != "" {
			report.DefaultFailures++
		}
	}
	if err := r.Repository.Complete(ctx, report, r.Now().UTC()); err != nil {
		r.Logger.ErrorContext(ctx, "media_inspection_complete_failed", "run_id", id, "error_class", logsafe.ErrorClass(err))
		r.fail(parent, id, "completion_unavailable")
		return
	}
	r.Logger.InfoContext(ctx, "media_inspection_completed", "run_id", id,
		"publication_issues", report.PublicationIssues, "default_failures", report.DefaultFailures)
}

func (r Runner) fail(parent context.Context, id, code string) {
	// Preserve failure evidence after a bounded scan/probe timeout, but do not
	// keep working after process shutdown. Never log remote URLs or bodies.
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	if err := r.Repository.Fail(ctx, id, code, r.Now().UTC()); err != nil {
		r.Logger.ErrorContext(ctx, "media_inspection_fail_record_failed", "run_id", id, "error_class", logsafe.ErrorClass(err))
	}
	r.Logger.ErrorContext(ctx, "media_inspection_failed", "run_id", id, "error_code", code)
}
