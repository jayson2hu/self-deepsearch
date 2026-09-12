package mediausage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// The gate is consumed by reconciliation, explicit upload resume checks and
// private per-operation upload admission. A permit does not itself resume
// anything. It is never used by necessary rights deletion or image delivery.
var ErrTaskAdmission = errors.New("usage_task_admission_denied")

type TaskStateReader interface {
	ReadMetricsState(context.Context) (State, error)
}

type TaskGuard struct {
	reader                       TaskStateReader
	schedule                     Schedule
	now                          func() time.Time
	mu                           sync.Mutex
	checked                      time.Time
	blocked                      bool
	allowed, denied, unavailable uint64
}

func NewTaskGuard(reader TaskStateReader, schedule Schedule, now func() time.Time) (*TaskGuard, error) {
	if reader == nil || schedule.Validate() != nil {
		return nil, ErrConfig
	}
	if now == nil {
		now = time.Now
	}
	return &TaskGuard{reader: reader, schedule: schedule, now: now, blocked: true}, nil
}

func taskAdmissionAllowed(s State, now time.Time, schedule Schedule) bool {
	if schedule.Validate() != nil || s.UpdatedAt.IsZero() || now.Before(s.UpdatedAt) ||
		s.LastUntil.Before(schedule.PeriodStart) || !s.LastUntil.Before(schedule.PeriodEnd) {
		return false
	}
	effective := s.EffectiveRecommendation(now, schedule)
	if effective != "observe_only" && effective != "review_usage" {
		return false
	}
	if s.LastAssessment.Status != "low_estimate" && s.LastAssessment.Status != "warning" {
		return false
	}
	// Recompute from durable integer high water, rather than trusting a stored
	// low/warning label. Assess also checks missing/future/stale clock evidence.
	a := Assess(&Observation{Counts: s.HighWater, Window: Window{Start: schedule.PeriodStart, EndInclusive: s.LastUntil},
		ReceivedAt: s.LastSuccessAt, Segments: 1}, nil, schedule.PeriodStart, now, schedule.Policy)
	return a.Recommendation == "observe_only" || a.Recommendation == "review_usage"
}

// Allow does not cache a permit or query Analytics. The repository verifies the
// active schedule digest; each check reads the existing local PostgreSQL state.
// Use a fresh clock AFTER that bounded read so time spent waiting is not free.
func (g *TaskGuard) Allow(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	s, err := g.reader.ReadMetricsState(bounded)
	now := g.now().UTC()
	allowed := err == nil && bounded.Err() == nil && taskAdmissionAllowed(s, now, g.schedule)
	g.mu.Lock()
	g.checked, g.blocked = now, !allowed
	if allowed {
		g.allowed++
	} else if err != nil || bounded.Err() != nil {
		g.unavailable++
	} else {
		g.denied++
	}
	g.mu.Unlock()
	if !allowed {
		// Never expose database errors, account IDs or provider data to logs.
		return ErrTaskAdmission
	}
	return nil
}

func (g *TaskGuard) WriteMetrics(_ context.Context, w io.Writer, now time.Time) {
	g.mu.Lock()
	checked, blocked, allowed, denied, unavailable := g.checked, g.blocked, g.allowed, g.denied, g.unavailable
	g.mu.Unlock()
	value := 0
	if blocked || checked.IsZero() || now.Before(checked) || now.Sub(checked) > 2*time.Minute {
		value = 1
	}
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_media_reconcile_usage_guard_enabled Scheduled reconciliation admission only; not upload or image delivery control.\n# TYPE self_deepsearch_media_reconcile_usage_guard_enabled gauge\nself_deepsearch_media_reconcile_usage_guard_enabled 1\n"+
		"# HELP self_deepsearch_media_reconcile_usage_guard_blocked Admission denied or decision missing/stale; not evidence that all media stopped.\n# TYPE self_deepsearch_media_reconcile_usage_guard_blocked gauge\nself_deepsearch_media_reconcile_usage_guard_blocked %d\n"+
		"# TYPE self_deepsearch_media_reconcile_usage_guard_checks_total counter\nself_deepsearch_media_reconcile_usage_guard_checks_total{outcome=\"allowed\"} %d\nself_deepsearch_media_reconcile_usage_guard_checks_total{outcome=\"blocked\"} %d\nself_deepsearch_media_reconcile_usage_guard_checks_total{outcome=\"unavailable\"} %d\n", value, allowed, denied, unavailable)
	if !checked.IsZero() {
		_, _ = fmt.Fprintf(w, "# TYPE self_deepsearch_media_reconcile_usage_guard_last_check_timestamp_seconds gauge\nself_deepsearch_media_reconcile_usage_guard_last_check_timestamp_seconds %d\n", checked.Unix())
	}
}
