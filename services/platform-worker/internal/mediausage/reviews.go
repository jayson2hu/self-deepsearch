package mediausage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type ReviewProcessor interface {
	ProcessReviews(context.Context, time.Time) error
}

type reviewRequest struct {
	ID, ActorID, Action, Digest      string
	ExpectedAt, CreatedAt, ExpiresAt time.Time
	NextStart, NextEnd               *time.Time
}

type reviewMetadata struct {
	AccountRef, Policy string
	Start, End         time.Time
}

// reviewPatch produces only a recommendation-state change. It cannot execute
// delivery recovery, change account/budget, or erase the previous period audit.
func reviewPatch(row storedState, meta reviewMetadata, req reviewRequest, schedule Schedule, now time.Time) (map[string]any, string) {
	if req.CreatedAt.After(now) || !now.Before(req.ExpiresAt) {
		return nil, "usage_review_expired"
	}
	if row.digest != req.Digest || !row.UpdatedAt.Equal(req.ExpectedAt) {
		return nil, "usage_review_stale"
	}
	if row.activeRun != nil {
		return nil, "usage_review_busy"
	}
	patch := map[string]any{"last_review_id": req.ID, "updated_at": now}
	switch req.Action {
	case "acknowledge":
		if row.digest != schedule.Fingerprint() {
			return nil, "usage_schedule_changed"
		}
		if !row.ReviewRequired || row.StopRecommended || (row.LastAssessment.Status != "low_estimate" && row.LastAssessment.Status != "warning") {
			return nil, "usage_review_not_recoverable"
		}
		// Recompute risk from durable high water, not merely a stored status label.
		risk := Assess(&Observation{Window: Window{schedule.PeriodStart, row.LastUntil}, ReceivedAt: row.LastSuccessAt, Segments: 1, Counts: row.HighWater}, nil, schedule.PeriodStart, now, schedule.Policy)
		if risk.Status != "low_estimate" && risk.Status != "warning" {
			return nil, "usage_review_not_recoverable"
		}
		unlatched := row.State
		unlatched.ReviewRequired = false
		if next := unlatched.EffectiveRecommendation(now, schedule); next != "observe_only" && next != "review_usage" {
			return nil, "usage_review_not_recoverable"
		}
		patch["review_required"], patch["review_since"], patch["review_reason"] = false, nil, nil
	case "rotate_period":
		if req.NextStart == nil || req.NextEnd == nil || schedule.Validate() != nil ||
			!schedule.PeriodStart.Equal(*req.NextStart) || !schedule.PeriodEnd.Equal(*req.NextEnd) ||
			schedule.AccountRef() != meta.AccountRef || schedule.PeriodStart.Before(meta.End) ||
			now.Before(schedule.PeriodStart) || !now.Before(schedule.PeriodEnd) || row.digest == schedule.Fingerprint() {
			return nil, "usage_rotation_config_mismatch"
		}
		var oldPolicy, nextPolicy any
		decode := func(value string, target *any) error {
			d := json.NewDecoder(strings.NewReader(value))
			d.UseNumber()
			return d.Decode(target)
		}
		if decode(meta.Policy, &oldPolicy) != nil || decode(schedule.PolicyDocument(), &nextPolicy) != nil {
			return nil, "usage_rotation_config_mismatch"
		}
		oldJSON, _ := json.Marshal(oldPolicy)
		nextJSON, _ := json.Marshal(nextPolicy)
		if string(oldJSON) != string(nextJSON) {
			return nil, "usage_rotation_config_mismatch"
		}
		patch["config_digest"], patch["period_start"], patch["period_end"] = schedule.Fingerprint(), schedule.PeriodStart, schedule.PeriodEnd
		patch["high_class_a"], patch["high_class_b"], patch["high_free"] = uint64(0), uint64(0), uint64(0)
		patch["last_success_at"], patch["last_until"] = nil, nil
		patch["review_required"], patch["review_since"], patch["review_reason"] = true, now, "usage_period_rotated"
		patch["stop_recommended"] = false
		patch["last_status"], patch["last_reason"], patch["last_recommendation"] = "unknown", "usage_period_rotated", "hold_for_review"
		patch["next_poll_at"], patch["active_run_id"], patch["lease_until"] = now, nil, nil
	default:
		return nil, "usage_review_invalid"
	}
	return patch, ""
}

// ProcessReviews serializes with observation claims, snapshots the old state,
// and commits the immutable command outcome and exact transition together.
func (r *PostgresRepository) ProcessReviews(ctx context.Context, now time.Time) error {
	if now.IsZero() || r.schedule.Validate() != nil {
		return ErrConfig
	}
	tx, err := r.begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('self-deepsearch-media-usage'))`); err != nil {
		return err
	}
	row, err := readState(ctx, tx, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	var req reviewRequest
	err = tx.QueryRow(ctx, `SELECT review_id::text,actor_id::text,action,expected_digest,expected_updated_at,
 created_at,expires_at,next_period_start,next_period_end FROM audit.media_usage_reviews WHERE status='pending' FOR UPDATE`).Scan(
		&req.ID, &req.ActorID, &req.Action, &req.Digest, &req.ExpectedAt, &req.CreatedAt, &req.ExpiresAt, &req.NextStart, &req.NextEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	var meta reviewMetadata
	if err = tx.QueryRow(ctx, `SELECT account_ref,policy_snapshot::text,period_start,period_end FROM platform.media_usage_state WHERE singleton`).Scan(
		&meta.AccountRef, &meta.Policy, &meta.Start, &meta.End); err != nil {
		return err
	}
	var authorized bool
	if err = tx.QueryRow(ctx, `SELECT platform.lock_media_usage_reviewer($1::uuid)`, req.ActorID).Scan(&authorized); err != nil {
		return err
	}
	patch, code := reviewPatch(row, meta, req, r.schedule, now)
	if !authorized {
		code = "usage_reviewer_inactive"
	}
	if code != "" {
		result, err := tx.Exec(ctx, `UPDATE audit.media_usage_reviews SET status='rejected',completed_at=greatest($2,created_at),error_code=$3 WHERE review_id=$1::uuid AND status='pending'`, req.ID, now, code)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ErrLeaseLost
		}
		return tx.Commit(ctx)
	}
	encoded, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	// jsonb_populate_record normalizes timestamps/numerics to the exact SQL row
	// representation checked by the state trigger, without float conversions.
	result, err := tx.Exec(ctx, `UPDATE audit.media_usage_reviews review SET status='applied',completed_at=$2,
 before_state=to_jsonb(state),after_state=to_jsonb(jsonb_populate_record(state,$3::jsonb))
 FROM platform.media_usage_state state WHERE state.singleton AND review.review_id=$1::uuid AND review.status='pending'`, req.ID, now, string(encoded))
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	result, err = tx.Exec(ctx, `UPDATE platform.media_usage_state SET
 config_digest=next.config_digest,period_start=next.period_start,period_end=next.period_end,
 high_class_a=next.high_class_a,high_class_b=next.high_class_b,high_free=next.high_free,
 last_success_at=next.last_success_at,last_until=next.last_until,review_required=next.review_required,
 review_since=next.review_since,review_reason=next.review_reason,stop_recommended=next.stop_recommended,
 last_status=next.last_status,last_reason=next.last_reason,last_recommendation=next.last_recommendation,
 updated_at=next.updated_at,next_poll_at=next.next_poll_at,active_run_id=next.active_run_id,lease_until=next.lease_until,last_review_id=next.last_review_id
 FROM audit.media_usage_reviews review,
 LATERAL jsonb_populate_record(NULL::platform.media_usage_state,review.after_state) next
 WHERE platform.media_usage_state.singleton AND review.review_id=$1::uuid AND review.status='applied'`, req.ID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return tx.Commit(ctx)
}
