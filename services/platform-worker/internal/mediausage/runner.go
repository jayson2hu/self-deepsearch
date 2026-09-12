package mediausage

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"self-deepsearch/services/platform-worker/internal/logsafe"
)

type Fetcher interface {
	Fetch(context.Context, Window) (*Observation, error)
}

type Runner struct {
	Repository Repository
	Reviews    ReviewProcessor
	Client     Fetcher
	Logger     *slog.Logger
	Now        func() time.Time
}

func (r Runner) Run(ctx context.Context) error {
	if r.Repository == nil || r.Client == nil {
		return ErrConfig
	}
	if r.Logger == nil {
		r.Logger = slog.Default()
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	r.runOnce(ctx)
	ticker := time.NewTicker(time.Minute)
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
	if r.Reviews != nil {
		reviewCtx, reviewCancel := context.WithTimeout(ctx, 3*time.Second)
		err := r.Reviews.ProcessReviews(reviewCtx, r.Now().UTC())
		reviewCancel()
		if err != nil {
			r.Logger.ErrorContext(ctx, "media_usage_review_failed", "error_class", logsafe.ErrorClass(err))
			return
		}
	}
	prepareCtx, prepareCancel := context.WithTimeout(ctx, 3*time.Second)
	run, err := r.Repository.Prepare(prepareCtx, r.Now().UTC())
	prepareCancel()
	if errors.Is(err, ErrNotDue) {
		return
	}
	if err != nil {
		if errors.Is(err, ErrScheduleChanged) || errors.Is(err, ErrPeriodInactive) {
			code := "usage_period_inactive"
			if errors.Is(err, ErrScheduleChanged) {
				code = "usage_schedule_changed"
			}
			r.Logger.WarnContext(ctx, "media_usage_review_required", "error_code", code)
		} else {
			r.Logger.ErrorContext(ctx, "media_usage_prepare_failed", "error_class", logsafe.ErrorClass(err))
		}
		return
	}
	o, fetchError := r.Client.Fetch(ctx, run.Window)
	// Finish on a short independent context so a query timeout can still be
	// recorded; shutdown cancellation is respected. A crash leaves a leased run
	// which Prepare subsequently records as interrupted, never as a zero sample.
	finishCtx, finishCancel := context.WithTimeout(parent, 5*time.Second)
	defer finishCancel()
	state, err := r.Repository.Finish(finishCtx, run, o, fetchError, r.Now().UTC())
	if err != nil {
		r.Logger.ErrorContext(ctx, "media_usage_finish_failed", "error_class", logsafe.ErrorClass(err))
		return
	}
	r.Logger.InfoContext(ctx, "media_usage_observed", "status", state.LastAssessment.Status,
		"review_required", state.ReviewRequired, "stop_recommended", state.StopRecommended, "enforcement", "not_connected")
}
