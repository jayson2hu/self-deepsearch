package mediausage

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type runnerRepo struct {
	t                         *testing.T
	prepareError, finishError error
	prepared, finished        int
	fetchError                error
	finishHook                func()
	run                       Run
}

func (r *runnerRepo) Prepare(ctx context.Context, _ time.Time) (Run, error) {
	r.prepared++
	checkDeadline(r.t, ctx, 3*time.Second)
	return r.run, r.prepareError
}
func (r *runnerRepo) Finish(ctx context.Context, _ Run, o *Observation, e error, now time.Time) (State, error) {
	r.finished++
	r.fetchError = e
	checkDeadline(r.t, ctx, 5*time.Second)
	if r.finishHook != nil {
		r.finishHook()
	}
	s, _ := Advance(State{}, o, e, testSchedule(), now)
	return s, r.finishError
}

type fakeFetcher struct {
	calls int
	err   error
}

func (f *fakeFetcher) Fetch(_ context.Context, w Window) (*Observation, error) {
	f.calls++
	o := observation()
	o.Window = w
	return o, f.err
}
func checkDeadline(t *testing.T, ctx context.Context, limit time.Duration) {
	t.Helper()
	until, ok := ctx.Deadline()
	if !ok || time.Until(until) > limit {
		t.Error("unbounded stage context")
	}
}

func TestRunnerOnlyFetchesClaimedRunsAndPersistsFailures(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		prepare, fetch, finish error
		calls                  int
	}{
		{"healthy", nil, nil, nil, 1}, {"not-due", ErrNotDue, nil, nil, 0}, {"wrong-config", ErrScheduleChanged, nil, nil, 0}, {"period", ErrPeriodInactive, nil, nil, 0},
		{"database", errors.New(testToken), nil, nil, 0}, {"upstream", nil, errors.New(testToken), nil, 1}, {"timeout", nil, context.DeadlineExceeded, nil, 1}, {"completion", nil, nil, errors.New(testToken), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			repo := &runnerRepo{t: t, prepareError: tc.prepare, finishError: tc.finish, run: Run{"run", shortWindow()}}
			fetcher := &fakeFetcher{err: tc.fetch}
			r := Runner{Repository: repo, Client: fetcher, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Now: func() time.Time { return testNow }}
			r.runOnce(context.Background())
			if repo.prepared != 1 || fetcher.calls != tc.calls || repo.finished != tc.calls || repo.fetchError != tc.fetch {
				t.Fatalf("unexpected invocation counts: %+v %+v", repo, fetcher)
			}
			if strings.Contains(logs.String(), testToken) || strings.Contains(logs.String(), testAccount) {
				t.Fatal("private data leaked into logs")
			}
		})
	}
}

func TestRunnerCancellationAndDependencies(t *testing.T) {
	if (Runner{}).Run(context.Background()) != ErrConfig {
		t.Fatal("missing dependencies accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &runnerRepo{t: t, run: Run{"run", shortWindow()}, finishHook: cancel}
	fetcher := &fakeFetcher{}
	r := Runner{Repository: repo, Client: fetcher, Now: func() time.Time { return testNow }}
	if err := r.Run(ctx); err != nil || repo.finished != 1 || fetcher.calls != 1 {
		t.Fatalf("runner did not stop after current attempt: %v", err)
	}
}
