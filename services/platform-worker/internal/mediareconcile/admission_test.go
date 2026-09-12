package mediareconcile

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type admissionFunc func(context.Context) error

func (f admissionFunc) Allow(ctx context.Context) error { return f(ctx) }

type reconcileFunc func(context.Context, Run) (Report, error)

func (f reconcileFunc) Reconcile(ctx context.Context, r Run) (Report, error) { return f(ctx, r) }

type admissionRepo struct {
	fakeRepository
	prepares int
	failErr  error
	notDue   bool
}

func (r *admissionRepo) Prepare(ctx context.Context, _ time.Time, _ time.Duration) (Run, error) {
	r.prepares++
	if _, ok := ctx.Deadline(); !ok {
		return Run{}, errors.New("unbounded prepare")
	}
	if r.notDue {
		return Run{}, ErrNotDue
	}
	return r.run, nil
}
func (r *admissionRepo) Fail(ctx context.Context, _ string, code string, _ time.Time) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("unbounded failure receipt")
	}
	r.failed = code
	return r.failErr
}

func TestAdmissionDenialDoesNotDispatchOrInventSuccessfulReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		denyAt                            int
		notDue, failReceipt               bool
		wantPrepare, wantChecks, wantHTTP int
		failed                            string
	}{
		{"before-prepare", 1, false, false, 0, 1, 0, ""},
		{"before-dispatch", 2, false, false, 1, 2, 0, "usage_guard_denied"},
		{"failure-receipt-error", 2, false, true, 1, 2, 0, "usage_guard_denied"},
		{"allowed", 0, false, false, 1, 2, 1, ""},
		{"not-due", 0, true, false, 1, 1, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &admissionRepo{fakeRepository: fakeRepository{run: reportRun()}, notDue: tc.notDue}
			if tc.failReceipt {
				r.failErr = errors.New("private-state-detail")
			}
			checks, calls := 0, 0
			var log strings.Builder
			runner := Runner{Repository: r, Every: 24 * time.Hour, Now: time.Now, Logger: slog.New(slog.NewTextHandler(&log, nil)),
				Admission: admissionFunc(func(context.Context) error {
					checks++
					if checks == tc.denyAt {
						return errors.New("private-account-detail")
					}
					return nil
				}),
				Client: reconcileFunc(func(_ context.Context, run Run) (Report, error) { calls++; return completeReport(run), nil })}
			runner.runOnce(context.Background())
			if checks != tc.wantChecks || r.prepares != tc.wantPrepare || calls != tc.wantHTTP || r.failed != tc.failed {
				t.Fatalf("unexpected effects: %d/%d/%d/%s", checks, r.prepares, calls, r.failed)
			}
			if (r.completed != nil) != (tc.wantHTTP == 1) || strings.Contains(log.String(), "private-") {
				t.Fatal("false completion or sensitive log")
			}
		})
	}
}

func TestDeferredAdmissionReevaluatesWithoutRestart(t *testing.T) {
	blocked := true
	r := &admissionRepo{fakeRepository: fakeRepository{run: reportRun()}}
	calls := 0
	runner := Runner{Repository: r, Every: 24 * time.Hour, Now: time.Now, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Admission: admissionFunc(func(context.Context) error {
			if blocked {
				return errors.New("paused")
			}
			return nil
		}),
		Client: reconcileFunc(func(_ context.Context, run Run) (Report, error) { calls++; return completeReport(run), nil })}
	runner.runOnce(context.Background())
	blocked = false
	runner.runOnce(context.Background())
	if r.prepares != 1 || calls != 1 || r.completed == nil {
		t.Fatal("deferred admission consumed scan cadence or required restart")
	}
}
