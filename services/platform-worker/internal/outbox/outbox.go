package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"self-deepsearch/services/platform-worker/internal/logsafe"
)

type Event struct {
	ID             string
	AggregateType  string
	AggregateID    string
	Type           string
	Payload        json.RawMessage
	AttemptCount   int
	LeaseExpiresAt time.Time
}

var ErrLeaseLost = errors.New("outbox lease is no longer valid")

type Repository interface {
	Claim(context.Context, string, time.Time, time.Duration, int) ([]Event, error)
	Complete(context.Context, string, string, int, time.Time) error
	Fail(context.Context, string, string, int, string, time.Time) error
}

type Processor interface {
	Process(context.Context, Event) error
}

type ProcessorFunc func(context.Context, Event) error

func (process ProcessorFunc) Process(ctx context.Context, event Event) error {
	return process(ctx, event)
}

type Telemetry interface {
	Heartbeat(time.Time)
	SetActiveLeases(int)
	PollSucceeded(time.Time)
	PollFailed(time.Time)
	EventCompleted(time.Time)
	EventFailed(time.Time)
}

type RoutingProcessor struct {
	Revalidation Processor
	MediaDelete  Processor
}

func (processor RoutingProcessor) Process(ctx context.Context, event Event) error {
	if event.Type == "media_delete" {
		if processor.MediaDelete == nil {
			return processingError{code: "media_delete_unconfigured", err: errors.New("media deletion processor is not configured")}
		}
		return processor.MediaDelete.Process(ctx, event)
	}
	if processor.Revalidation == nil {
		return processingError{code: "revalidate_unconfigured", err: errors.New("revalidation processor is not configured")}
	}
	return processor.Revalidation.Process(ctx, event)
}

type processingError struct {
	code string
	err  error
}

func (failure processingError) Error() string { return failure.err.Error() }
func (failure processingError) Unwrap() error { return failure.err }

type Dispatcher struct {
	Repository Repository
	WorkerID   string
	PollEvery  time.Duration
	LeaseFor   time.Duration
	BatchSize  int
	Processor  Processor
	Logger     *slog.Logger
	Now        func() time.Time
	Telemetry  Telemetry
}

func (dispatcher Dispatcher) Run(ctx context.Context) error {
	if dispatcher.Repository == nil || dispatcher.WorkerID == "" {
		return errors.New("outbox repository and worker id are required")
	}
	if dispatcher.Processor == nil {
		return errors.New("outbox processor is required")
	}
	if dispatcher.PollEvery <= 0 {
		dispatcher.PollEvery = 2 * time.Second
	}
	if dispatcher.LeaseFor <= 0 {
		dispatcher.LeaseFor = 30 * time.Second
	}
	if dispatcher.BatchSize <= 0 {
		dispatcher.BatchSize = 20
	}
	if dispatcher.Logger == nil {
		dispatcher.Logger = slog.Default()
	}
	if dispatcher.Now == nil {
		dispatcher.Now = time.Now
	}
	if err := dispatcher.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		dispatcher.Logger.ErrorContext(ctx, "outbox_poll_failed", "error_class", logsafe.ErrorClass(err))
	}
	ticker := time.NewTicker(dispatcher.PollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := dispatcher.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				dispatcher.Logger.ErrorContext(ctx, "outbox_poll_failed", "error_class", logsafe.ErrorClass(err))
			}
		}
	}
}

func (dispatcher Dispatcher) runOnce(ctx context.Context) (runErr error) {
	if dispatcher.Processor == nil {
		return errors.New("outbox processor is required")
	}
	if dispatcher.Logger == nil {
		dispatcher.Logger = slog.Default()
	}
	if dispatcher.Now == nil {
		dispatcher.Now = time.Now
	}
	if dispatcher.LeaseFor <= 0 {
		dispatcher.LeaseFor = 30 * time.Second
	}
	if dispatcher.BatchSize <= 0 {
		dispatcher.BatchSize = 20
	}
	now := dispatcher.Now().UTC()
	if dispatcher.Telemetry != nil {
		dispatcher.Telemetry.Heartbeat(now)
		dispatcher.Telemetry.SetActiveLeases(0)
		defer func() {
			finishedAt := dispatcher.Now().UTC()
			if runErr == nil {
				dispatcher.Telemetry.PollSucceeded(finishedAt)
				return
			}
			dispatcher.Telemetry.PollFailed(finishedAt)
		}()
	}
	events, err := dispatcher.Repository.Claim(ctx, dispatcher.WorkerID, now, dispatcher.LeaseFor, dispatcher.BatchSize)
	if err != nil {
		return err
	}
	if dispatcher.Telemetry != nil {
		dispatcher.Telemetry.SetActiveLeases(len(events))
		defer dispatcher.Telemetry.SetActiveLeases(0)
	}
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		expiresAt := event.LeaseExpiresAt
		if expiresAt.IsZero() {
			// Repositories should return the claimed deadline. Keep in-memory repositories bounded too.
			expiresAt = now.Add(dispatcher.LeaseFor)
		}
		remaining := expiresAt.Sub(dispatcher.Now().UTC())
		// Reserve time to commit the result; never start a queued side effect after its lease budget.
		if remaining <= time.Second {
			return ErrLeaseLost
		}
		processCtx, cancelProcess := context.WithTimeout(ctx, remaining-time.Second)
		processErr := dispatcher.Processor.Process(processCtx, event)
		deadlineErr := processCtx.Err()
		cancelProcess()
		if err := ctx.Err(); err != nil {
			return err
		}
		finishedAt := dispatcher.Now().UTC()
		if !finishedAt.Before(expiresAt) {
			return ErrLeaseLost
		}
		if processErr == nil && deadlineErr != nil {
			processErr = processingError{code: "processing_timeout", err: deadlineErr}
		}
		settleCtx, cancelSettle := context.WithTimeout(ctx, expiresAt.Sub(finishedAt))
		if processErr != nil {
			errorCode := "invalid_event"
			var coded processingError
			if errors.As(processErr, &coded) && coded.code != "" {
				errorCode = coded.code
			}
			failErr := dispatcher.Repository.Fail(settleCtx, event.ID, dispatcher.WorkerID, event.AttemptCount, errorCode, finishedAt)
			cancelSettle()
			if failErr != nil {
				return fmt.Errorf("fail event %s: %w", event.ID, failErr)
			}
			if dispatcher.Telemetry != nil {
				dispatcher.Telemetry.EventFailed(dispatcher.Now().UTC())
			}
			dispatcher.Logger.WarnContext(ctx, "outbox_event_failed", "event_id", event.ID, "event_type", event.Type, "error_class", logsafe.ErrorClass(processErr))
			continue
		}
		completeErr := dispatcher.Repository.Complete(settleCtx, event.ID, dispatcher.WorkerID, event.AttemptCount, finishedAt)
		cancelSettle()
		if completeErr != nil {
			return fmt.Errorf("complete event %s: %w", event.ID, completeErr)
		}
		if dispatcher.Telemetry != nil {
			dispatcher.Telemetry.EventCompleted(dispatcher.Now().UTC())
		}
		dispatcher.Logger.InfoContext(ctx, "outbox_event_completed", "event_id", event.ID, "event_type", event.Type)
	}
	return nil
}

func validateEvent(_ context.Context, event Event) error {
	switch event.Type {
	case "publication_changed", "publication_hidden", "publication_takedown", "search_refresh", "cache_purge", "sitemap_refresh":
		var payload map[string]any
		if len(event.Payload) == 0 || json.Unmarshal(event.Payload, &payload) != nil {
			return errors.New("event payload is not a JSON object")
		}
		return nil
	default:
		return fmt.Errorf("unsupported event type %q", event.Type)
	}
}
