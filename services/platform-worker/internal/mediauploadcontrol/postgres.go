package mediauploadcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresQueue struct {
	begin  func(context.Context, pgx.TxOptions) (pgx.Tx, error)
	target string
}

func (c *Client) TargetRef() string {
	digest := sha256.Sum256([]byte(c.origin))
	return hex.EncodeToString(digest[:])
}

func NewPostgresQueue(pool *pgxpool.Pool, target string) (*PostgresQueue, error) {
	if pool == nil || len(target) != 64 {
		return nil, ErrInvalid
	}
	if _, err := hex.DecodeString(target); err != nil {
		return nil, ErrInvalid
	}
	return &PostgresQueue{begin: pool.BeginTx, target: target}, nil
}

func (q *PostgresQueue) Claim(ctx context.Context, now time.Time) (Lease, error) {
	tx, err := q.begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Lease{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('self-deepsearch-media-upload-control'))`); err != nil {
		return Lease{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO platform.media_upload_control_state(singleton,target_ref,updated_at,next_poll_at)
 VALUES(true,$1,$2,least($2,clock_timestamp())) ON CONFLICT(singleton) DO NOTHING`, q.target, now); err != nil {
		return Lease{}, err
	}
	var lease Lease
	var target string
	if err = tx.QueryRow(ctx, `SELECT target_ref FROM platform.media_upload_control_state WHERE singleton`).Scan(&target); err != nil {
		return Lease{}, err
	}
	if target != q.target {
		return Lease{}, ErrInvalid
	}
	err = tx.QueryRow(ctx, `UPDATE platform.media_upload_control_state SET lease_token=gen_random_uuid(),
 lease_until=clock_timestamp()+interval '30 seconds',updated_at=greatest(updated_at,$2)
 WHERE singleton AND target_ref=$1 AND next_poll_at<=$2 AND next_poll_at<=clock_timestamp()
 AND (lease_until IS NULL OR (lease_until<=$2 AND lease_until<=clock_timestamp()))
 RETURNING lease_token::text,lease_until`, q.target, now).Scan(&lease.Token, &lease.Until)
	if errors.Is(err, pgx.ErrNoRows) {
		return Lease{}, ErrNoWork
	}
	if err != nil {
		return Lease{}, err
	}
	var queued QueuedCommand
	var issued, expires time.Time
	err = tx.QueryRow(ctx, `SELECT command_id::text,actor_id::text,expected_epoch,expected_generation,mode,reason,created_at,expires_at,
 first_dispatched_at IS NOT NULL FROM audit.media_upload_commands
 WHERE status IN ('pending','running','uncertain') AND target_ref=$1 FOR UPDATE`, q.target).Scan(
		&queued.ID, &queued.ActorID, &queued.ExpectedEpoch, &queued.ExpectedGeneration, &queued.Mode, &queued.Reason, &issued, &expires, &queued.Dispatched)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Lease{}, err
	}
	if err == nil {
		queued.IssuedAt, queued.ExpiresAt = issued.UTC().Format("2006-01-02T15:04:05Z"), expires.UTC().Format("2006-01-02T15:04:05Z")
		queued.ResumeConfirmed = queued.Mode == "enabled"
		lease.Command = &queued
	}
	if err = tx.Commit(ctx); err != nil {
		return Lease{}, err
	}
	return lease, nil
}

func (q *PostgresQueue) lock(ctx context.Context, tx pgx.Tx, lease Lease, now time.Time) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('self-deepsearch-media-upload-control'))`); err != nil {
		return err
	}
	var matched bool
	err := tx.QueryRow(ctx, `SELECT true FROM platform.media_upload_control_state WHERE singleton AND target_ref=$1
 AND lease_token=$2::uuid AND lease_until=$3 AND lease_until>$4 AND lease_until>clock_timestamp() FOR UPDATE`, q.target, lease.Token, lease.Until, now).Scan(&matched)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLeaseLost
	}
	return err
}

func (q *PostgresQueue) Observe(ctx context.Context, lease Lease, receipt Receipt, now time.Time) error {
	if !receipt.valid(now) || receipt.Status != "observed" {
		return ErrInvalid
	}
	tx, err := q.begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = q.lock(ctx, tx, lease, now); err != nil {
		return err
	}
	if err = q.saveObservation(ctx, tx, receipt, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (q *PostgresQueue) saveObservation(ctx context.Context, tx pgx.Tx, receipt Receipt, now time.Time) error {
	receipt.Status = "observed"
	body, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE platform.media_upload_control_state SET last_receipt=$2::jsonb,last_success_at=$3,
 last_error_code=NULL,updated_at=$3 WHERE singleton AND target_ref=$1 AND updated_at<=$3`, q.target, string(body), now)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (q *PostgresQueue) AuthorizeDispatch(ctx context.Context, lease Lease, now time.Time) (bool, error) {
	if lease.Command == nil {
		return false, ErrInvalid
	}
	tx, err := q.begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = q.lock(ctx, tx, lease, now); err != nil {
		return false, err
	}
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT platform.lock_media_usage_reviewer($1::uuid)`, lease.Command.ActorID).Scan(&allowed); err != nil {
		return false, err
	}
	if !allowed {
		return false, tx.Commit(ctx)
	}
	result, err := tx.Exec(ctx, `UPDATE audit.media_upload_commands SET status='running',
 first_dispatched_at=coalesce(first_dispatched_at,$2),dispatch_count=least(dispatch_count+1,1000000),error_code=NULL
 WHERE command_id=$1::uuid AND status IN ('pending','running','uncertain') AND target_ref=$3 AND created_at<=$2
 AND expires_at>$2+interval '8 seconds' AND expires_at>clock_timestamp()+interval '8 seconds'`, lease.Command.ID, now, q.target)
	if err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}

func (q *PostgresQueue) Finish(ctx context.Context, lease Lease, outcome Outcome, now time.Time) error {
	tx, err := q.begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = q.lock(ctx, tx, lease, now); err != nil {
		return err
	}
	if lease.Command != nil {
		if outcome.Status != "applied" && outcome.Status != "rejected" && outcome.Status != "superseded" && outcome.Status != "uncertain" {
			return ErrInvalid
		}
		var body *string
		if outcome.Receipt != nil {
			if !outcome.Receipt.valid(now) {
				return ErrInvalid
			}
			encoded, err := json.Marshal(outcome.Receipt)
			if err != nil {
				return err
			}
			value := string(encoded)
			body = &value
		}
		if outcome.Status == "applied" && (outcome.Receipt == nil || !lease.Command.Matches(*outcome.Receipt)) {
			return ErrInvalid
		}
		result, err := tx.Exec(ctx, `UPDATE audit.media_upload_commands SET status=$2,error_code=nullif($3,''),receipt=$4::jsonb,
 completed_at=CASE WHEN $2='uncertain' THEN NULL ELSE greatest(created_at,$5) END
 WHERE command_id=$1::uuid AND status IN ('pending','running','uncertain') AND target_ref=$6`, lease.Command.ID, outcome.Status, outcome.Code, body, now, q.target)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ErrLeaseLost
		}
		if outcome.Status == "applied" {
			if err = q.saveObservation(ctx, tx, *outcome.Receipt, now); err != nil {
				return err
			}
		}
	}
	result, err := tx.Exec(ctx, `UPDATE platform.media_upload_control_state SET lease_token=NULL,lease_until=NULL,
 last_error_code=nullif($2,''),updated_at=greatest(updated_at,$3),next_poll_at=$3+interval '1 minute'
 WHERE singleton AND target_ref=$1`, q.target, outcome.Code, now)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return tx.Commit(ctx)
}
