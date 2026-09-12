package mediausage

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Run struct {
	ID     string
	Window Window
}

type Repository interface {
	Prepare(context.Context, time.Time) (Run, error)
	Finish(context.Context, Run, *Observation, error, time.Time) (State, error)
}

type PostgresRepository struct {
	begin    func(context.Context, pgx.TxOptions) (pgx.Tx, error)
	schedule Schedule
}

func NewPostgresRepository(pool *pgxpool.Pool, schedule Schedule) (*PostgresRepository, error) {
	if pool == nil || schedule.Validate() != nil {
		return nil, ErrConfig
	}
	return &PostgresRepository{pool.BeginTx, schedule}, nil
}

type storedState struct {
	State
	digest     string
	nextPoll   time.Time
	activeRun  *string
	leaseUntil *time.Time
	leaseLive  bool
}

func readState(ctx context.Context, tx pgx.Tx, lock bool) (storedState, error) {
	var row storedState
	var a, b, free string
	var success, until, review *time.Time
	var reason *string
	query := `SELECT config_digest, high_class_a::text, high_class_b::text, high_free::text,
 last_success_at,last_until,review_required,review_since,review_reason,stop_recommended,
 last_status,last_reason,last_recommendation,updated_at,next_poll_at,active_run_id::text,lease_until,
 coalesce(lease_until>clock_timestamp(),false)
 FROM platform.media_usage_state WHERE singleton`
	if lock {
		query += " FOR UPDATE"
	}
	err := tx.QueryRow(ctx, query).Scan(
		&row.digest, &a, &b, &free, &success, &until, &row.ReviewRequired, &review, &reason, &row.StopRecommended,
		&row.LastAssessment.Status, &row.LastAssessment.Reason, &row.LastAssessment.Recommendation, &row.UpdatedAt,
		&row.nextPoll, &row.activeRun, &row.leaseUntil, &row.leaseLive)
	if err != nil {
		return row, err
	}
	for _, field := range []struct {
		value       string
		destination *uint64
	}{{a, &row.HighWater.ClassA}, {b, &row.HighWater.ClassB}, {free, &row.HighWater.Free}} {
		value, err := strconv.ParseUint(field.value, 10, 64)
		if err != nil {
			return row, ErrResponse
		}
		*field.destination = value
	}
	if success != nil {
		row.LastSuccessAt = *success
	}
	if until != nil {
		row.LastUntil = *until
	}
	if review != nil {
		row.ReviewSince = *review
	}
	if reason != nil {
		row.ReviewReason = *reason
	}
	return row, nil
}

func saveState(ctx context.Context, tx pgx.Tx, s State) error {
	var reason any
	if s.ReviewRequired {
		reason = s.ReviewReason
	}
	result, err := tx.Exec(ctx, `UPDATE platform.media_usage_state SET
 high_class_a=$1::text::numeric,high_class_b=$2::text::numeric,high_free=$3::text::numeric,
 last_success_at=$4,last_until=$5,review_required=$6,review_since=$7,review_reason=$8,
 stop_recommended=$9,last_status=$10,last_reason=$11,last_recommendation=$12,updated_at=$13 WHERE singleton`,
		strconv.FormatUint(s.HighWater.ClassA, 10), strconv.FormatUint(s.HighWater.ClassB, 10), strconv.FormatUint(s.HighWater.Free, 10),
		nullTime(s.LastSuccessAt), nullTime(s.LastUntil), s.ReviewRequired, nullTime(s.ReviewSince), reason,
		s.StopRecommended, s.LastAssessment.Status, s.LastAssessment.Reason, s.LastAssessment.Recommendation, s.UpdatedAt)
	if err == nil && result.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return err
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func (r *PostgresRepository) Prepare(ctx context.Context, now time.Time) (Run, error) {
	if now.IsZero() || r.schedule.Validate() != nil {
		return Run{}, ErrConfig
	}
	tx, err := r.begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Run{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// READ COMMITTED + a separate post-lock read avoids stale startup snapshots
	// in two workers contending for the first run. No DB lock spans an HTTP call.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('self-deepsearch-media-usage'))`); err != nil {
		return Run{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO platform.media_usage_state
 (config_digest,account_ref,period_start,period_end,updated_at,next_poll_at,policy_snapshot)
 VALUES ($1,$2,$3,$4,$5,$5,$6::jsonb) ON CONFLICT (singleton) DO NOTHING`,
		r.schedule.Fingerprint(), r.schedule.AccountRef(), r.schedule.PeriodStart, r.schedule.PeriodEnd, now, r.schedule.PolicyDocument()); err != nil {
		return Run{}, err
	}
	row, err := readState(ctx, tx, true)
	if err != nil {
		return Run{}, err
	}
	finishWithoutRun := func(state State, result error) (Run, error) {
		if err := saveState(ctx, tx, state); err != nil {
			return Run{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Run{}, err
		}
		return Run{}, result
	}
	if row.digest != r.schedule.Fingerprint() {
		return finishWithoutRun(requireReview(row.State, now, "usage_schedule_changed"), ErrScheduleChanged)
	}
	if row.activeRun != nil {
		if row.leaseUntil != nil && row.leaseUntil.After(now) && row.leaseLive {
			if err := tx.Commit(ctx); err != nil {
				return Run{}, err
			}
			return Run{}, ErrNotDue
		}
		interrupted, err := tx.Exec(ctx, `UPDATE audit.media_usage_runs SET run_status='failed',error_code='usage_interrupted',completed_at=$2
	 WHERE run_id=$1::uuid AND run_status='running'`, *row.activeRun, now)
		if err != nil {
			return Run{}, err
		}
		if interrupted.RowsAffected() != 1 {
			return Run{}, ErrLeaseLost
		}
		if _, err = tx.Exec(ctx, `UPDATE platform.media_usage_state SET active_run_id=NULL,lease_until=NULL WHERE singleton`); err != nil {
			return Run{}, err
		}
		row.State = requireReview(row.State, now, "usage_interrupted")
	}
	if now.Before(r.schedule.PeriodStart) || !now.Before(r.schedule.PeriodEnd) {
		return finishWithoutRun(requireReview(row.State, now, "usage_period_inactive"), ErrPeriodInactive)
	}
	if !row.LastUntil.IsZero() && (now.Before(row.LastUntil) || now.Sub(row.LastUntil) > r.schedule.Policy.MaxAge) {
		row.State = requireReview(row.State, now, "observation_stale")
	}
	if row.nextPoll.After(now) {
		return finishWithoutRun(row.State, ErrNotDue)
	}
	run := Run{Window: Window{r.schedule.PeriodStart, now.UTC().Truncate(time.Second)}}
	if err = tx.QueryRow(ctx, `INSERT INTO audit.media_usage_runs
 (config_digest,run_status,window_start,window_end,started_at) VALUES ($1,'running',$2,$3,$4) RETURNING run_id::text`,
		r.schedule.Fingerprint(), run.Window.Start, run.Window.EndInclusive, now).Scan(&run.ID); err != nil {
		return Run{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE platform.media_usage_state SET next_poll_at=$1,active_run_id=$2::uuid,lease_until=$3 WHERE singleton`,
		now.Add(r.schedule.Every), run.ID, now.Add(2*time.Minute)); err != nil {
		return Run{}, err
	}
	row.UpdatedAt = now
	if err = saveState(ctx, tx, row.State); err != nil {
		return Run{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (r *PostgresRepository) Finish(ctx context.Context, run Run, o *Observation, fetchError error, now time.Time) (State, error) {
	if run.ID == "" || now.IsZero() {
		return State{}, ErrLeaseLost
	}
	tx, err := r.begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return State{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := readState(ctx, tx, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return State{}, ErrLeaseLost
	}
	if err != nil {
		return State{}, err
	}
	if row.digest != r.schedule.Fingerprint() || row.activeRun == nil || *row.activeRun != run.ID || row.leaseUntil == nil || !row.leaseUntil.After(now) || !row.leaseLive {
		return State{}, ErrLeaseLost
	}
	if o != nil && (!o.Window.Start.Equal(run.Window.Start) || !o.Window.EndInclusive.Equal(run.Window.EndInclusive)) {
		o, fetchError = nil, ErrResponse
	}
	next, accepted := Advance(row.State, o, fetchError, r.schedule, now)
	var observationJSON any
	status := "failed"
	var errorCode any = next.LastAssessment.Reason
	if accepted != nil {
		encoded, err := json.Marshal(accepted)
		if err != nil {
			return State{}, err
		}
		observationJSON, status, errorCode = string(encoded), "completed", nil
	}
	assessmentJSON, err := json.Marshal(next.LastAssessment)
	if err != nil {
		return State{}, err
	}
	result, err := tx.Exec(ctx, `UPDATE audit.media_usage_runs SET run_status=$2,completed_at=$3,
 observation=$4::jsonb,assessment=$5::jsonb,error_code=$6 WHERE run_id=$1::uuid AND run_status='running'
 AND config_digest=$7 AND window_start=$8 AND window_end=$9`,
		run.ID, status, now, observationJSON, string(assessmentJSON), errorCode, r.schedule.Fingerprint(), run.Window.Start, run.Window.EndInclusive)
	if err != nil {
		return State{}, err
	}
	if result.RowsAffected() != 1 {
		return State{}, ErrLeaseLost
	}
	if err = saveState(ctx, tx, next); err != nil {
		return State{}, err
	}
	result, err = tx.Exec(ctx, `UPDATE platform.media_usage_state SET active_run_id=NULL,lease_until=NULL
 WHERE singleton AND active_run_id=$1::uuid AND lease_until>$2 AND lease_until>clock_timestamp()`, run.ID, now)
	if err != nil {
		return State{}, err
	}
	if result.RowsAffected() != 1 {
		return State{}, ErrLeaseLost
	}
	if err = tx.Commit(ctx); err != nil {
		return State{}, err
	}
	return next, nil
}
