package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepository struct {
	events          []Event
	claimErr        error
	completed       []string
	failed          []string
	failureCodes    []string
	claims          int
	settledAttempts []int
}

type fakeTelemetry struct {
	heartbeats int
	active     []int
	pollOK     int
	pollFailed int
	completed  int
	failed     int
}

func (telemetry *fakeTelemetry) Heartbeat(time.Time) { telemetry.heartbeats++ }
func (telemetry *fakeTelemetry) SetActiveLeases(value int) {
	telemetry.active = append(telemetry.active, value)
}
func (telemetry *fakeTelemetry) PollSucceeded(time.Time)  { telemetry.pollOK++ }
func (telemetry *fakeTelemetry) PollFailed(time.Time)     { telemetry.pollFailed++ }
func (telemetry *fakeTelemetry) EventCompleted(time.Time) { telemetry.completed++ }
func (telemetry *fakeTelemetry) EventFailed(time.Time)    { telemetry.failed++ }

func (repository *fakeRepository) Claim(context.Context, string, time.Time, time.Duration, int) ([]Event, error) {
	repository.claims++
	if repository.claimErr != nil {
		return nil, repository.claimErr
	}
	items := repository.events
	repository.events = nil
	return items, nil
}

func (repository *fakeRepository) Complete(_ context.Context, eventID, _ string, attempt int, _ time.Time) error {
	repository.completed = append(repository.completed, eventID)
	repository.settledAttempts = append(repository.settledAttempts, attempt)
	return nil
}

func (repository *fakeRepository) Fail(_ context.Context, eventID, _ string, attempt int, errorCode string, _ time.Time) error {
	repository.failed = append(repository.failed, eventID)
	repository.failureCodes = append(repository.failureCodes, errorCode)
	repository.settledAttempts = append(repository.settledAttempts, attempt)
	return nil
}

func TestRunOnceCompletesSupportedEvent(t *testing.T) {
	repository := &fakeRepository{events: []Event{{ID: "event-1", Type: "publication_changed", Payload: []byte(`{"entity_id":"work-1"}`)}}}
	dispatcher := Dispatcher{Repository: repository, WorkerID: "worker-1", Processor: ProcessorFunc(validateEvent), Now: func() time.Time { return time.Unix(1, 0) }}
	if err := dispatcher.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce returned error: %v", err)
	}
	if len(repository.completed) != 1 || len(repository.failed) != 0 {
		t.Fatalf("unexpected event outcome: completed=%v failed=%v", repository.completed, repository.failed)
	}
}

func TestRunOnceRetriesInvalidEvent(t *testing.T) {
	repository := &fakeRepository{events: []Event{{ID: "event-2", Type: "email_send", Payload: []byte(`{}`)}}}
	dispatcher := Dispatcher{Repository: repository, WorkerID: "worker-1", Processor: ProcessorFunc(validateEvent), Now: func() time.Time { return time.Unix(1, 0) }}
	if err := dispatcher.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce returned error: %v", err)
	}
	if len(repository.failed) != 1 || len(repository.completed) != 0 || repository.failureCodes[0] != "invalid_event" {
		t.Fatalf("unexpected event outcome: completed=%v failed=%v codes=%v", repository.completed, repository.failed, repository.failureCodes)
	}
}

func TestRunOnceUsesConfiguredProcessor(t *testing.T) {
	repository := &fakeRepository{events: []Event{{ID: "event-3", Type: "publication_changed", Payload: []byte(`{}`)}}}
	called := false
	dispatcher := Dispatcher{
		Repository: repository, WorkerID: "worker-1", Now: func() time.Time { return time.Unix(1, 0) },
		Processor: ProcessorFunc(func(_ context.Context, event Event) error {
			called = event.ID == "event-3"
			return processingError{code: "revalidate_unavailable", err: errors.New("display unavailable")}
		}),
	}
	if err := dispatcher.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called || len(repository.failed) != 1 || repository.failed[0] != "event-3" || repository.failureCodes[0] != "revalidate_unavailable" {
		t.Fatalf("processor outcome not persisted: called=%v failed=%v codes=%v", called, repository.failed, repository.failureCodes)
	}
}

func TestRunOncePublishesBoundedRuntimeTelemetry(t *testing.T) {
	repository := &fakeRepository{events: []Event{
		{ID: "event-ok", Type: "publication_changed", Payload: []byte(`{"entity_id":"work-1"}`)},
		{ID: "event-failed", Type: "email_send", Payload: []byte(`{}`)},
	}}
	telemetry := &fakeTelemetry{}
	dispatcher := Dispatcher{
		Repository: repository, WorkerID: "worker-1", Now: func() time.Time { return time.Unix(1, 0) },
		Telemetry: telemetry, Processor: ProcessorFunc(validateEvent),
	}
	if err := dispatcher.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if telemetry.heartbeats != 1 || telemetry.pollOK != 1 || telemetry.pollFailed != 0 || telemetry.completed != 1 || telemetry.failed != 1 {
		t.Fatalf("unexpected telemetry outcomes: %#v", telemetry)
	}
	wantActive := []int{0, 2, 0}
	if len(telemetry.active) != len(wantActive) {
		t.Fatalf("unexpected active lease transitions: %v", telemetry.active)
	}
	for index := range wantActive {
		if telemetry.active[index] != wantActive[index] {
			t.Fatalf("unexpected active lease transitions: %v", telemetry.active)
		}
	}
}

func TestRunOnceTracksPollFailureAndRecovery(t *testing.T) {
	repository := &fakeRepository{claimErr: errors.New("claim unavailable")}
	telemetry := &fakeTelemetry{}
	dispatcher := Dispatcher{
		Repository: repository, WorkerID: "worker-1", Now: func() time.Time { return time.Unix(1, 0) },
		Telemetry: telemetry, Processor: ProcessorFunc(validateEvent),
	}
	if err := dispatcher.runOnce(context.Background()); err == nil {
		t.Fatal("expected claim failure")
	}
	if telemetry.pollFailed != 1 || telemetry.pollOK != 0 {
		t.Fatalf("expected failed poll telemetry, got %#v", telemetry)
	}

	repository.claimErr = nil
	if err := dispatcher.runOnce(context.Background()); err != nil {
		t.Fatalf("expected recovery poll to succeed: %v", err)
	}
	if telemetry.pollFailed != 1 || telemetry.pollOK != 1 {
		t.Fatalf("expected successful recovery telemetry, got %#v", telemetry)
	}
}

func TestMissingProcessorCannotClaimOrCompleteEvents(t *testing.T) {
	repository := &fakeRepository{events: []Event{{ID: "event-1", Type: "publication_changed", Payload: []byte(`{}`)}}}
	dispatcher := Dispatcher{Repository: repository, WorkerID: "worker-1"}
	if err := dispatcher.runOnce(context.Background()); err == nil {
		t.Fatal("missing processor must be rejected before claiming work")
	}
	if err := dispatcher.Run(context.Background()); err == nil {
		t.Fatal("dispatcher must not start without a processor")
	}
	if repository.claims != 0 || len(repository.completed) != 0 || len(repository.failed) != 0 {
		t.Fatalf("configuration failure must leave events untouched: %#v", repository)
	}
}

func TestExpiredBatchCannotCompleteOrStartMoreSideEffects(t *testing.T) {
	now := time.Now().UTC()
	repository := &fakeRepository{events: []Event{
		{ID: "first", Type: "publication_changed", Payload: []byte(`{}`)},
		{ID: "second", Type: "publication_changed", Payload: []byte(`{}`)},
	}}
	processed := 0
	dispatcher := Dispatcher{
		Repository: repository, WorkerID: "worker-1", LeaseFor: 30 * time.Second,
		Now: func() time.Time { return now },
		Processor: ProcessorFunc(func(context.Context, Event) error {
			processed++
			now = now.Add(31 * time.Second)
			return nil
		}),
	}
	err := dispatcher.runOnce(context.Background())
	if err == nil || processed != 1 || len(repository.completed) != 0 || len(repository.failed) != 0 {
		t.Fatalf("expired lease must not settle or start another side effect: err=%v processed=%d completed=%v failed=%v", err, processed, repository.completed, repository.failed)
	}
}

func TestProcessorReceivesBoundedLeaseContext(t *testing.T) {
	repository := &fakeRepository{events: []Event{{ID: "first", Type: "publication_changed", Payload: []byte(`{}`)}}}
	dispatcher := Dispatcher{
		Repository: repository, WorkerID: "worker-1", LeaseFor: 5 * time.Second,
		Processor: ProcessorFunc(func(ctx context.Context, _ Event) error {
			deadline, bounded := ctx.Deadline()
			if !bounded || time.Until(deadline) > 5*time.Second || time.Until(deadline) <= 0 {
				t.Error("side effects must have a positive deadline within the remaining lease")
			}
			return nil
		}),
	}
	if err := dispatcher.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSettlementUsesCapturedClaimAttempt(t *testing.T) {
	for _, fail := range []bool{false, true} {
		repository := &fakeRepository{events: []Event{{ID: "event", AttemptCount: 7}}}
		dispatcher := Dispatcher{
			Repository: repository, WorkerID: "same-worker-name",
			Processor: ProcessorFunc(func(context.Context, Event) error {
				if fail {
					return processingError{code: "unavailable", err: errors.New("offline")}
				}
				return nil
			}),
		}
		if err := dispatcher.runOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(repository.settledAttempts) != 1 || repository.settledAttempts[0] != 7 {
			t.Fatalf("settlement must carry the original claim fencing value: %v", repository.settledAttempts)
		}
	}
}

func TestReturnedExpiredLeaseIsNotReplacedByNewLocalDeadline(t *testing.T) {
	repository := &fakeRepository{events: []Event{{ID: "expired", AttemptCount: 1, LeaseExpiresAt: time.Now().Add(-time.Second)}}}
	called := false
	dispatcher := Dispatcher{Repository: repository, WorkerID: "worker", Processor: ProcessorFunc(func(context.Context, Event) error {
		called = true
		return nil
	})}
	if err := dispatcher.runOnce(context.Background()); !errors.Is(err, ErrLeaseLost) || called {
		t.Fatalf("expired database lease must prevent side effects: called=%t err=%v", called, err)
	}
}

func TestTimedOutProcessorCannotReturnFalseSuccess(t *testing.T) {
	repository := &fakeRepository{events: []Event{{ID: "event", AttemptCount: 2, LeaseExpiresAt: time.Now().Add(1500 * time.Millisecond)}}}
	dispatcher := Dispatcher{Repository: repository, WorkerID: "worker", Processor: ProcessorFunc(func(ctx context.Context, _ Event) error {
		<-ctx.Done()
		return nil // Simulate a processor which loses the cancellation error.
	})}
	if err := dispatcher.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repository.completed) != 0 || len(repository.failureCodes) != 1 || repository.failureCodes[0] != "processing_timeout" {
		t.Fatalf("deadline must not become success: completed=%v codes=%v", repository.completed, repository.failureCodes)
	}
}

func TestShutdownDoesNotSettleUsingCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repository := &fakeRepository{events: []Event{{ID: "event", AttemptCount: 1}}}
	dispatcher := Dispatcher{Repository: repository, WorkerID: "worker", Processor: ProcessorFunc(func(context.Context, Event) error {
		cancel()
		return nil
	})}
	if err := dispatcher.runOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled dispatcher: %v", err)
	}
	if len(repository.completed) != 0 || len(repository.failed) != 0 {
		t.Fatal("shutdown must leave the running lease for bounded recovery")
	}
}

func TestPostgresSettlementRejectsMissingClaimBeforeDatabaseAccess(t *testing.T) {
	repository := &PostgresRepository{}
	if !errors.Is(repository.Complete(context.Background(), "event", "worker", 0, time.Now()), ErrLeaseLost) {
		t.Fatal("missing attempt must not settle")
	}
	if !errors.Is(repository.Fail(context.Background(), "event", "worker", -1, "failure", time.Now()), ErrLeaseLost) {
		t.Fatal("negative attempt must not settle")
	}
}
