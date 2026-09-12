package mediausage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReadMetricsState never locks a row or changes freshness/review state. It is
// separate from scheduler telemetry, so a restarted worker sees durable flags.
func (r *PostgresRepository) ReadMetricsState(ctx context.Context) (State, error) {
	tx, err := r.begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return State{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := readState(ctx, tx, false)
	if err != nil {
		return State{}, err
	}
	if row.digest != r.schedule.Fingerprint() {
		return State{}, ErrScheduleChanged
	}
	if err := tx.Commit(ctx); err != nil {
		return State{}, err
	}
	return row.State, nil
}

func (r *PostgresRepository) WriteMetrics(ctx context.Context, w io.Writer, now time.Time) {
	s, err := r.ReadMetricsState(ctx)
	writeUsageMetrics(w, s, err, now, r.schedule)
}

func writeUsageMetrics(w io.Writer, s State, err error, now time.Time, schedule Schedule) {
	gauge := func(name, help string, value any) {
		_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_%s %s\n# TYPE self_deepsearch_%s gauge\nself_deepsearch_%s %v\n", name, help, name, name, value)
	}
	gauge("media_usage_monitor_enabled", "Account analytics observer enabled; not delivery enforcement.", 1)
	if err != nil {
		gauge("media_usage_state_available", "Durable analytics state was readable.", 0)
		return
	}
	gauge("media_usage_state_available", "Durable analytics state was readable.", 1)
	bit := func(value bool) int {
		if value {
			return 1
		}
		return 0
	}
	gauge("media_usage_review_required", "Persisted account usage review latch.", bit(s.ReviewRequired))
	gauge("media_usage_stop_recommended", "Persisted stop recommendation; not an executed stop.", bit(s.StopRecommended))
	effective := s.EffectiveRecommendation(now, schedule)
	gauge("media_usage_hold_recommended", "Review or stop is recommended including stale/missing observations.", bit(effective == "hold_for_review" || effective == "stop_media_review_required"))
	if s.LastSuccessAt.IsZero() || s.LastUntil.IsZero() {
		return
	}
	age := now.Sub(s.LastUntil).Seconds()
	if age < 0 {
		age = 0
	}
	gauge("media_usage_observation_age_seconds", "Age of requested analytics horizon, not provider completeness.", age)
	gauge("media_usage_class_a_highwater", "Conservative Class A analytics high water; not billing.", s.HighWater.ClassA)
	gauge("media_usage_class_b_highwater", "Conservative Class B analytics high water; not billing.", s.HighWater.ClassB)
}
