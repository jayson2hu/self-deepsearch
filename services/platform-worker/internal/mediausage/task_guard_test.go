package mediausage

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type taskStateFunc func(context.Context) (State, error)

func (f taskStateFunc) ReadMetricsState(ctx context.Context) (State, error) { return f(ctx) }

func allowedTaskState() State {
	return State{HighWater: Counts{ClassA: 100, ClassB: 200}, LastSuccessAt: testNow, LastUntil: testNow.Add(-time.Minute),
		UpdatedAt: testNow, LastAssessment: Assessment{"low_estimate", "operations_below_warning", "observe_only"}}
}

func TestTaskAdmissionRequiresFreshNonHighSamePeriodEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*State, *Schedule)
		allowed bool
	}{
		{"low", func(*State, *Schedule) {}, true},
		{"warning", func(s *State, _ *Schedule) {
			s.HighWater.ClassA = 700000
			s.LastAssessment = Assessment{"warning", "operations_at_70_percent", "review_usage"}
		}, true},
		{"just-below-high", func(s *State, _ *Schedule) { s.HighWater.ClassA = 849999 }, true},
		{"high-water-with-low-label", func(s *State, _ *Schedule) { s.HighWater.ClassA = 850000 }, false},
		{"high-b", func(s *State, _ *Schedule) { s.HighWater.ClassB = 8500000 }, false},
		{"reserve", func(s *State, _ *Schedule) { s.HighWater.ClassA = 950000 }, false},
		{"overflow", func(s *State, _ *Schedule) { s.HighWater.ClassA = math.MaxUint64 }, false},
		{"custom-reserve", func(s *State, p *Schedule) { p.Policy.ClassAReserve = 500000; s.HighWater.ClassA = 500000 }, false},
		{"stop-latch", func(s *State, _ *Schedule) { s.StopRecommended = true }, false},
		{"review-latch", func(s *State, _ *Schedule) { s.ReviewRequired = true }, false},
		{"unknown-label", func(s *State, _ *Schedule) { s.LastAssessment.Status = "unknown" }, false},
		{"unexpected-recommendation", func(s *State, _ *Schedule) { s.LastAssessment.Recommendation = "unrecognized" }, false},
		{"missing-receipt", func(s *State, _ *Schedule) { s.LastSuccessAt = time.Time{} }, false},
		{"missing-horizon", func(s *State, _ *Schedule) { s.LastUntil = time.Time{} }, false},
		{"missing-update", func(s *State, _ *Schedule) { s.UpdatedAt = time.Time{} }, false},
		{"stale-receipt", func(s *State, _ *Schedule) { s.LastSuccessAt = testNow.Add(-31 * time.Minute) }, false},
		{"stale-horizon", func(s *State, _ *Schedule) { s.LastUntil = testNow.Add(-31 * time.Minute) }, false},
		{"future-receipt", func(s *State, _ *Schedule) { s.LastSuccessAt = testNow.Add(time.Second) }, false},
		{"future-update", func(s *State, _ *Schedule) { s.UpdatedAt = testNow.Add(time.Second) }, false},
		{"future-horizon", func(s *State, _ *Schedule) { s.LastUntil = testNow.Add(time.Second) }, false},
		{"period-ended", func(_ *State, p *Schedule) { p.PeriodEnd = testNow }, false},
		{"period-future", func(_ *State, p *Schedule) { p.PeriodStart = testNow.Add(time.Second) }, false},
		{"before-period", func(s *State, p *Schedule) { s.LastUntil = p.PeriodStart.Add(-time.Second) }, false},
		{"invalid-schedule", func(_ *State, p *Schedule) { p.Every = time.Second }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p := allowedTaskState(), testSchedule()
			tc.change(&s, &p)
			if got := taskAdmissionAllowed(s, testNow, p); got != tc.allowed {
				t.Fatalf("admission=%v want=%v", got, tc.allowed)
			}
		})
	}
}

func TestTaskGuardReadIsBoundedUncachedAndUsesPostReadClock(t *testing.T) {
	now := testNow
	s := allowedTaskState()
	calls := 0
	readErr := error(nil)
	g, err := NewTaskGuard(taskStateFunc(func(ctx context.Context) (State, error) {
		calls++
		checkDeadline(t, ctx, 2*time.Second)
		return s, readErr
	}), testSchedule(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err = g.Allow(context.Background()); err != nil {
		t.Fatal("fresh state rejected")
	}
	readErr = errors.New("private-account/token/database-detail")
	if err = g.Allow(context.Background()); !errors.Is(err, ErrTaskAdmission) || strings.Contains(err.Error(), "private") {
		t.Fatal("failed read reused old permit or leaked details")
	}
	readErr = ErrScheduleChanged
	if err = g.Allow(context.Background()); !errors.Is(err, ErrTaskAdmission) {
		t.Fatal("changed schedule admitted")
	}
	readErr = nil
	g.reader = taskStateFunc(func(context.Context) (State, error) { now = now.Add(31 * time.Minute); return s, nil })
	if err = g.Allow(context.Background()); !errors.Is(err, ErrTaskAdmission) {
		t.Fatal("pre-read time reused")
	}
	now = testNow
	g.reader = taskStateFunc(func(context.Context) (State, error) { return s, nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = g.Allow(ctx); !errors.Is(err, ErrTaskAdmission) {
		t.Fatal("canceled read admitted")
	}
	if calls != 3 {
		t.Fatal("admission cached an allow response")
	}
	if _, err = NewTaskGuard(nil, testSchedule(), nil); err == nil {
		t.Fatal("nil reader accepted")
	}
	if _, err = NewTaskGuard(g.reader, Schedule{}, nil); err == nil {
		t.Fatal("invalid schedule accepted")
	}
}

func TestTaskGuardMetricsAreBoundedConcurrentAndNotDeliveryState(t *testing.T) {
	g, _ := NewTaskGuard(taskStateFunc(func(context.Context) (State, error) { return allowedTaskState(), nil }), testSchedule(), func() time.Time { return testNow })
	var initial bytes.Buffer
	g.WriteMetrics(context.Background(), &initial, testNow)
	if !strings.Contains(initial.String(), "media_reconcile_usage_guard_blocked 1") {
		t.Fatal("unchecked guard claimed an allow")
	}
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = g.Allow(context.Background())
			g.WriteMetrics(context.Background(), &bytes.Buffer{}, testNow)
		}()
	}
	wg.Wait()
	for _, tc := range []struct {
		at      time.Time
		blocked string
	}{{testNow, "0"}, {testNow.Add(-time.Second), "1"}, {testNow.Add(3 * time.Minute), "1"}} {
		var b bytes.Buffer
		g.WriteMetrics(context.Background(), &b, tc.at)
		text := b.String()
		if !strings.Contains(text, "media_reconcile_usage_guard_blocked "+tc.blocked) || !strings.Contains(text, `checks_total{outcome="allowed"} 30`) {
			t.Fatal("wrong concurrent/freshness metric")
		}
		for _, private := range []string{testAccount, "media_default_only", "media_usage_stop_recommended"} {
			if strings.Contains(text, private) {
				t.Fatal("admission metric leaked identity or impersonated global delivery")
			}
		}
	}
}

func TestTaskGuardRepositoryReadIsReadOnlyAndRejectsUncommittedState(t *testing.T) {
	for _, failure := range []string{"", "begin", "read-only", "commit", "no-row", "changed-schedule"} {
		t.Run(failure, func(t *testing.T) {
			row := initialStored()
			row.State = allowedTaskState()
			tx := &usageTx{t: t, row: row, fail: failure}
			if failure == "changed-schedule" {
				tx.row.digest = strings.Repeat("f", 64)
			}
			repo := &PostgresRepository{tx.begin, testSchedule()}
			g, err := NewTaskGuard(repo, testSchedule(), func() time.Time { return testNow })
			if err != nil {
				t.Fatal(err)
			}
			err = g.Allow(context.Background())
			if (err == nil) != (failure == "") {
				t.Fatal("uncommitted/missing/mismatched state became a permit")
			}
			if tx.options.AccessMode != pgx.ReadOnly || strings.Contains(strings.Join(tx.steps, ","), "save") {
				t.Fatal("admission mutated or locked usage state")
			}
			if failure == "" && strings.Join(tx.steps, ",") != "begin,read-only,commit,rollback" {
				t.Fatal(tx.steps)
			}
		})
	}
}
