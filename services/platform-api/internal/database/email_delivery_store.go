package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/identity"
)

func (store *Store) ReserveEmailDelivery(ctx context.Context, request identity.EmailDeliveryReservation) (identity.EmailDeliveryTicket, error) {
	if request.DailyLimit < 1 {
		return identity.EmailDeliveryTicket{}, errors.New("email daily limit must be positive")
	}
	bucketDate := request.ReservedAt.UTC().Format(time.DateOnly)
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.EmailDeliveryTicket{}, fmt.Errorf("begin email delivery reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var circuitOpenUntil *time.Time
	if err := tx.QueryRow(ctx, `
SELECT circuit_open_until
FROM platform.email_delivery_state
WHERE singleton
FOR UPDATE`).Scan(&circuitOpenUntil); err != nil {
		return identity.EmailDeliveryTicket{}, fmt.Errorf("lock email delivery state: %w", err)
	}
	if circuitOpenUntil != nil && circuitOpenUntil.After(request.ReservedAt) {
		if err := insertSuppressedEmailDelivery(ctx, tx, request, "circuit_open"); err != nil {
			return identity.EmailDeliveryTicket{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return identity.EmailDeliveryTicket{}, fmt.Errorf("commit email circuit suppression: %w", err)
		}
		return identity.EmailDeliveryTicket{}, identity.ErrEmailCircuitOpen
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO platform.email_delivery_daily (delivery_date)
VALUES ($1::date)
ON CONFLICT (delivery_date) DO NOTHING`, bucketDate); err != nil {
		return identity.EmailDeliveryTicket{}, fmt.Errorf("ensure email delivery daily bucket: %w", err)
	}
	var reservedCount int64
	err = tx.QueryRow(ctx, `
UPDATE platform.email_delivery_daily
SET reserved_count = reserved_count + 1, updated_at = $3
WHERE delivery_date = $1::date AND reserved_count < $2
RETURNING reserved_count`, bucketDate, request.DailyLimit, request.ReservedAt).Scan(&reservedCount)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := insertSuppressedEmailDelivery(ctx, tx, request, "daily_limit"); err != nil {
			return identity.EmailDeliveryTicket{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return identity.EmailDeliveryTicket{}, fmt.Errorf("commit email quota suppression: %w", err)
		}
		return identity.EmailDeliveryTicket{}, identity.ErrEmailDailyLimit
	}
	if err != nil {
		return identity.EmailDeliveryTicket{}, fmt.Errorf("reserve email daily quota: %w", err)
	}

	ticket := identity.EmailDeliveryTicket{BucketDate: bucketDate}
	if err := tx.QueryRow(ctx, `
INSERT INTO audit.notification_deliveries (
    user_id, notification_type, delivery_status, attempt_count, queued_at
) VALUES ($1::uuid, $2, 'sending', 1, $3)
RETURNING notification_delivery_id::text`, request.UserID, request.NotificationType, request.ReservedAt).Scan(&ticket.ID); err != nil {
		return identity.EmailDeliveryTicket{}, fmt.Errorf("create email delivery audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.EmailDeliveryTicket{}, fmt.Errorf("commit email delivery reservation: %w", err)
	}
	return ticket, nil
}

func insertSuppressedEmailDelivery(ctx context.Context, tx pgx.Tx, request identity.EmailDeliveryReservation, errorClass string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO audit.notification_deliveries (
    user_id, notification_type, delivery_status, error_class,
    attempt_count, queued_at, completed_at
) VALUES ($1::uuid, $2, 'suppressed', $3, 0, $4, $4)`,
		request.UserID, request.NotificationType, errorClass, request.ReservedAt)
	if err != nil {
		return fmt.Errorf("record suppressed email delivery: %w", err)
	}
	return nil
}

func (store *Store) CompleteEmailDelivery(ctx context.Context, completion identity.EmailDeliveryCompletion) error {
	if completion.Ticket.ID == "" || completion.Ticket.BucketDate == "" || completion.FailureThreshold < 1 || completion.CircuitCooldown <= 0 {
		return errors.New("invalid email delivery completion")
	}
	if !completion.Succeeded && !oneOfEmailErrorClass(completion.ErrorClass) {
		return errors.New("invalid email delivery error class")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin email delivery completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var deliveryStatus string
	if err := tx.QueryRow(ctx, `
SELECT delivery_status
FROM audit.notification_deliveries
WHERE notification_delivery_id = $1::uuid
FOR UPDATE`, completion.Ticket.ID).Scan(&deliveryStatus); err != nil {
		return fmt.Errorf("lock email delivery audit: %w", err)
	}
	if deliveryStatus != "sending" {
		return tx.Commit(ctx)
	}

	if completion.Succeeded {
		if _, err := tx.Exec(ctx, `
UPDATE platform.email_delivery_state
SET consecutive_failures = 0, circuit_open_until = NULL, updated_at = $1
WHERE singleton`, completion.CompletedAt); err != nil {
			return fmt.Errorf("close email delivery circuit: %w", err)
		}
		if err := incrementEmailDeliveryOutcome(ctx, tx, completion.Ticket.BucketDate, "sent_count", completion.CompletedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE audit.notification_deliveries
SET delivery_status = 'sent', error_class = NULL, sent_at = $2, completed_at = $2
WHERE notification_delivery_id = $1::uuid`, completion.Ticket.ID, completion.CompletedAt); err != nil {
			return fmt.Errorf("complete successful email audit: %w", err)
		}
	} else {
		cooldownSeconds := int64(completion.CircuitCooldown / time.Second)
		if _, err := tx.Exec(ctx, `
UPDATE platform.email_delivery_state
SET consecutive_failures = consecutive_failures + 1,
    circuit_open_until = CASE
        WHEN consecutive_failures + 1 >= $1
        THEN $2::timestamptz + ($3::bigint * interval '1 second')
        ELSE circuit_open_until
    END,
    updated_at = $2
WHERE singleton`, completion.FailureThreshold, completion.CompletedAt, cooldownSeconds); err != nil {
			return fmt.Errorf("update email delivery circuit: %w", err)
		}
		if err := incrementEmailDeliveryOutcome(ctx, tx, completion.Ticket.BucketDate, "failed_count", completion.CompletedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE audit.notification_deliveries
SET delivery_status = 'failed', error_class = $2, completed_at = $3
WHERE notification_delivery_id = $1::uuid`, completion.Ticket.ID, completion.ErrorClass, completion.CompletedAt); err != nil {
			return fmt.Errorf("complete failed email audit: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit email delivery completion: %w", err)
	}
	return nil
}

func incrementEmailDeliveryOutcome(ctx context.Context, tx pgx.Tx, bucketDate, column string, completedAt time.Time) error {
	var query string
	switch column {
	case "sent_count":
		query = `
UPDATE platform.email_delivery_daily
SET sent_count = sent_count + 1, updated_at = $2
WHERE delivery_date = $1::date
  AND sent_count + failed_count < reserved_count`
	case "failed_count":
		query = `
UPDATE platform.email_delivery_daily
SET failed_count = failed_count + 1, updated_at = $2
WHERE delivery_date = $1::date
  AND sent_count + failed_count < reserved_count`
	default:
		return errors.New("invalid email delivery outcome column")
	}
	result, err := tx.Exec(ctx, query, bucketDate, completedAt)
	if err != nil {
		return fmt.Errorf("increment email delivery %s: %w", column, err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("email delivery reservation is missing or already completed")
	}
	return nil
}

func oneOfEmailErrorClass(value string) bool {
	return value == "timeout" || value == "canceled" || value == "provider"
}

func (store *Store) EmailDeliveryMetrics(ctx context.Context, now time.Time) (identity.EmailDeliveryMetrics, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return identity.EmailDeliveryMetrics{}, fmt.Errorf("begin email delivery metrics: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	metrics := identity.EmailDeliveryMetrics{}
	var circuitOpenUntil *time.Time
	if err := tx.QueryRow(ctx, `
SELECT consecutive_failures, circuit_open_until
FROM platform.email_delivery_state
WHERE singleton`).Scan(&metrics.ConsecutiveFailure, &circuitOpenUntil); err != nil {
		return identity.EmailDeliveryMetrics{}, fmt.Errorf("query email delivery circuit metrics: %w", err)
	}
	metrics.CircuitOpen = circuitOpenUntil != nil && circuitOpenUntil.After(now)
	if err := tx.QueryRow(ctx, `
SELECT reserved_count, sent_count, failed_count
FROM platform.email_delivery_daily
WHERE delivery_date = $1::date`, now.UTC().Format(time.DateOnly)).Scan(
		&metrics.DailyReserved, &metrics.DailySent, &metrics.DailyFailed,
	); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return identity.EmailDeliveryMetrics{}, fmt.Errorf("query email delivery daily metrics: %w", err)
	}

	rows, err := tx.Query(ctx, `
SELECT notification_type, delivery_status, count(*)
FROM audit.notification_deliveries
WHERE notification_type IN ('signup_code', 'password_code', 'account_close_code', 'invitation_code')
  AND delivery_status IN ('sent', 'failed', 'suppressed')
GROUP BY notification_type, delivery_status
ORDER BY notification_type, delivery_status`)
	if err != nil {
		return identity.EmailDeliveryMetrics{}, fmt.Errorf("query email delivery outcome metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var notificationType string
		var count identity.EmailDeliveryCount
		if err := rows.Scan(&notificationType, &count.Outcome, &count.Count); err != nil {
			return identity.EmailDeliveryMetrics{}, fmt.Errorf("scan email delivery outcome metrics: %w", err)
		}
		count.Purpose = emailPurposeMetricLabel(notificationType)
		metrics.Counts = append(metrics.Counts, count)
	}
	if err := rows.Err(); err != nil {
		return identity.EmailDeliveryMetrics{}, fmt.Errorf("iterate email delivery outcome metrics: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return identity.EmailDeliveryMetrics{}, fmt.Errorf("commit email delivery metrics: %w", err)
	}
	return metrics, nil
}

func emailPurposeMetricLabel(notificationType string) string {
	switch notificationType {
	case "signup_code":
		return identity.PurposeSignup
	case "password_code":
		return identity.PurposePasswordReset
	case "account_close_code":
		return identity.PurposeAccountClose
	case "invitation_code":
		return identity.PurposeInvitation
	default:
		return "unknown"
	}
}
