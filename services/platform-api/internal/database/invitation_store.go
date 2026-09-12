package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"self-deepsearch/services/platform-api/internal/identity"
)

func (store *Store) CreateInvitation(ctx context.Context, request identity.InvitationRequest) (identity.Invitation, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.Invitation{}, fmt.Errorf("begin invitation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	emailCount, err := incrementRate(ctx, tx, "invitation_code", "email_hash", request.EmailHash, request.SentAt.Truncate(time.Hour), time.Hour)
	if err != nil {
		return identity.Invitation{}, err
	}
	ipCount, err := incrementRate(ctx, tx, "invitation_code", "ip_hash", request.IPHash, request.SentAt.Truncate(time.Hour), time.Hour)
	if err != nil {
		return identity.Invitation{}, err
	}
	if emailCount > 3 || ipCount > 20 {
		if err := tx.Commit(ctx); err != nil {
			return identity.Invitation{}, err
		}
		return identity.Invitation{}, identity.ErrRateLimited
	}
	rows, err := tx.Query(ctx, `
UPDATE platform.user_invitations
SET status = 'expired', consumed_at = expires_at, updated_at = $2
WHERE normalized_email = $1 AND status = 'pending' AND expires_at <= $2
RETURNING invitation_id::text`, request.Email, request.SentAt)
	if err != nil {
		return identity.Invitation{}, fmt.Errorf("expire old invitations: %w", err)
	}
	for rows.Next() {
		var expiredID string
		if err := rows.Scan(&expiredID); err != nil {
			rows.Close()
			return identity.Invitation{}, fmt.Errorf("scan expired invitation: %w", err)
		}
		if err := insertAudit(ctx, tx, request.InvitedBy, "user.invitation_expire", "invitation", expiredID,
			map[string]any{"status": "pending"}, map[string]any{"status": "expired"}, "invitation expired before replacement", request.RequestID, request.SentAt); err != nil {
			rows.Close()
			return identity.Invitation{}, err
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return identity.Invitation{}, fmt.Errorf("iterate expired invitations: %w", err)
	}
	rows.Close()

	// Invitations are for accounts that do not exist yet. Check the account
	// table before sending mail so an operator gets an immediate conflict
	// instead of creating an invitation that can never be accepted. The unique
	// account index remains the final race-safe guard during acceptance.
	var activeUserID string
	err = tx.QueryRow(ctx, `
SELECT user_id::text
FROM platform.users
WHERE normalized_email = $1 AND account_status <> 'closed'
LIMIT 1 FOR KEY SHARE`, request.Email).Scan(&activeUserID)
	if err == nil {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return identity.Invitation{}, commitErr
		}
		return identity.Invitation{}, identity.ErrConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return identity.Invitation{}, fmt.Errorf("check active account: %w", err)
	}

	var existingID string
	err = tx.QueryRow(ctx, `
SELECT invitation_id::text
FROM platform.user_invitations
WHERE normalized_email = $1 AND status = 'pending' AND expires_at > $2
ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, request.Email, request.SentAt).Scan(&existingID)
	if err == nil {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return identity.Invitation{}, commitErr
		}
		return identity.Invitation{}, identity.ErrConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return identity.Invitation{}, fmt.Errorf("check active invitation: %w", err)
	}

	var invitation identity.Invitation
	err = tx.QueryRow(ctx, `
INSERT INTO platform.user_invitations (
    normalized_email, role, invitation_code_hash, invited_by, request_ip_hash, sent_at, expires_at
) VALUES ($1, $2, $3, $4::uuid, $5, $6, $7)
RETURNING invitation_id::text, normalized_email, role, status, invited_by::text, sent_at, expires_at, consumed_at, created_at`,
		request.Email, request.Role, request.CodeHash, request.InvitedBy, request.IPHash, request.SentAt, request.ExpiresAt).Scan(
		&invitation.ID, &invitation.Email, &invitation.Role, &invitation.Status, &invitation.InvitedBy,
		&invitation.SentAt, &invitation.ExpiresAt, &invitation.ConsumedAt, &invitation.CreatedAt)
	if err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return identity.Invitation{}, identity.ErrConflict
		}
		return identity.Invitation{}, fmt.Errorf("insert invitation: %w", err)
	}
	if err := insertAudit(ctx, tx, request.InvitedBy, "user.invitation_create", "invitation", invitation.ID,
		map[string]any{"status": "none"}, map[string]any{"role": request.Role, "status": "pending"},
		request.Reason, request.RequestID, request.SentAt); err != nil {
		return identity.Invitation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Invitation{}, fmt.Errorf("commit invitation: %w", err)
	}
	return invitation, nil
}

func (store *Store) ListInvitations(ctx context.Context, status string, limit int) ([]identity.Invitation, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := store.pool.Query(ctx, `
SELECT invitation.invitation_id::text, invitation.normalized_email, invitation.role,
       CASE WHEN invitation.status = 'pending' AND invitation.expires_at <= now() THEN 'expired' ELSE invitation.status END,
       invitation.invited_by::text, coalesce(inviter.normalized_email, ''), invitation.sent_at,
       invitation.expires_at, invitation.consumed_at, invitation.created_at
FROM platform.user_invitations invitation
LEFT JOIN platform.users inviter ON inviter.user_id = invitation.invited_by
WHERE ($1 = 'all'
    OR ($1 = 'pending' AND invitation.status = 'pending' AND invitation.expires_at > now())
    OR ($1 = 'expired' AND (invitation.status = 'expired' OR (invitation.status = 'pending' AND invitation.expires_at <= now())))
    OR ($1 IN ('accepted', 'revoked') AND invitation.status = $1))
ORDER BY invitation.created_at DESC, invitation.invitation_id
LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	defer rows.Close()
	items := make([]identity.Invitation, 0)
	for rows.Next() {
		var item identity.Invitation
		if err := rows.Scan(&item.ID, &item.Email, &item.Role, &item.Status, &item.InvitedBy, &item.Inviter,
			&item.SentAt, &item.ExpiresAt, &item.ConsumedAt, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan invitation: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) RevokeInvitation(ctx context.Context, invitationID, actorID, reason, requestID string, now time.Time) (identity.Invitation, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.Invitation{}, fmt.Errorf("begin revoke invitation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item identity.Invitation
	err = tx.QueryRow(ctx, `
SELECT invitation_id::text, normalized_email, role, status, invited_by::text, sent_at, expires_at, consumed_at, created_at
FROM platform.user_invitations WHERE invitation_id = $1::uuid FOR UPDATE`, invitationID).Scan(
		&item.ID, &item.Email, &item.Role, &item.Status, &item.InvitedBy, &item.SentAt, &item.ExpiresAt, &item.ConsumedAt, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Invitation{}, identity.ErrNotFound
	}
	if err != nil {
		return identity.Invitation{}, fmt.Errorf("load invitation: %w", err)
	}
	if item.Status != "pending" || !item.ExpiresAt.After(now) {
		return identity.Invitation{}, identity.ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.user_invitations SET status = 'revoked', consumed_at = $2, updated_at = $2 WHERE invitation_id = $1::uuid`, invitationID, now); err != nil {
		return identity.Invitation{}, fmt.Errorf("revoke invitation: %w", err)
	}
	item.Status = "revoked"
	item.ConsumedAt = &now
	if err := insertAudit(ctx, tx, actorID, "user.invitation_revoke", "invitation", invitationID,
		map[string]any{"status": "pending"}, map[string]any{"status": "revoked"}, reason, requestID, now); err != nil {
		return identity.Invitation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Invitation{}, fmt.Errorf("commit revoke invitation: %w", err)
	}
	return item, nil
}

// CheckInvitation commits invalid-attempt accounting but neither consumes a
// valid invitation nor reserves quota. No transaction remains open during KDF
// work. AcceptInvitation must revalidate under its own final transaction.
func (store *Store) CheckInvitation(ctx context.Context, email, codeHash, ipHash string, now time.Time) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin invitation precheck: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, _, err = lockInvitation(ctx, tx, email, codeHash, now)
	if errors.Is(err, identity.ErrInvalidChallenge) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return commitErr
		}
		return err
	}
	if err != nil {
		return err
	}
	var count int64
	if err := tx.QueryRow(ctx, `
SELECT COALESCE((SELECT request_count FROM platform.security_rate_limits
 WHERE action = 'invitation_accept' AND dimension_type = 'ip_hash'
   AND dimension_hash = $1 AND window_start = $2 AND window_seconds = 86400), 0)`,
		ipHash, now.UTC().Truncate(24*time.Hour)).Scan(&count); err != nil {
		return fmt.Errorf("precheck invitation quota: %w", err)
	}
	if count >= 10 {
		return identity.ErrRateLimited
	}
	return tx.Commit(ctx)
}

func lockInvitation(ctx context.Context, tx pgx.Tx, email, codeHash string, now time.Time) (string, string, error) {
	var invitationID, role string
	err := tx.QueryRow(ctx, `
SELECT invitation_id::text, role
FROM platform.user_invitations
WHERE normalized_email = $1 AND invitation_code_hash = $2 AND status = 'pending'
  AND expires_at > $3 AND attempt_count < 5
ORDER BY sent_at DESC LIMIT 1 FOR UPDATE`, email, codeHash, now).Scan(&invitationID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		_, updateErr := tx.Exec(ctx, `
UPDATE platform.user_invitations
SET attempt_count = least(attempt_count + 1, 5), updated_at = $2
WHERE invitation_id = (
    SELECT invitation_id FROM platform.user_invitations
    WHERE normalized_email = $1 AND status = 'pending' AND expires_at > $2
    ORDER BY sent_at DESC LIMIT 1
)`, email, now)
		if updateErr != nil {
			return "", "", updateErr
		}
		return "", "", identity.ErrInvalidChallenge
	}
	if err != nil {
		return "", "", fmt.Errorf("load invitation code: %w", err)
	}
	return invitationID, role, nil
}

func (store *Store) AcceptInvitation(ctx context.Context, request identity.InvitationAcceptRequest) (identity.User, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.User{}, fmt.Errorf("begin accept invitation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	invitationID, role, err := lockInvitation(ctx, tx, request.Email, request.CodeHash, request.Now)
	if errors.Is(err, identity.ErrInvalidChallenge) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return identity.User{}, commitErr
		}
		return identity.User{}, err
	}
	if err != nil {
		return identity.User{}, err
	}

	dayStart := request.Now.Truncate(24 * time.Hour)
	count, err := incrementRate(ctx, tx, "invitation_accept", "ip_hash", request.IPHash, dayStart, 24*time.Hour)
	if err != nil {
		return identity.User{}, err
	}
	if count > 10 {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return identity.User{}, commitErr
		}
		return identity.User{}, identity.ErrRateLimited
	}

	var user identity.User
	err = tx.QueryRow(ctx, `
INSERT INTO platform.users (normalized_email, email_verified_at, password_hash, role, account_status)
VALUES ($1, $2, $3, $4, 'active')
RETURNING user_id::text, normalized_email, password_hash, role, account_status, email_verified_at`,
		request.Email, request.Now, request.PasswordHash, role).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.AccountStatus, &user.EmailVerifiedAt)
	if err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return identity.User{}, identity.ErrConflict
		}
		return identity.User{}, fmt.Errorf("create invited user: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.user_invitations SET status = 'accepted', consumed_at = $2, updated_at = $2 WHERE invitation_id = $1::uuid`, invitationID, request.Now); err != nil {
		return identity.User{}, fmt.Errorf("consume invitation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.sessions (user_id, token_hash, created_at, expires_at, last_used_at)
VALUES ($1::uuid, $2, $3, CASE WHEN $4 = 'user' THEN $5::timestamptz ELSE $6::timestamptz END, $3)`, user.ID, request.SessionHash, request.Now, role, request.UserSessionUntil, request.OperatorSessionUntil); err != nil {
		return identity.User{}, fmt.Errorf("create invited session: %w", err)
	}
	if err := insertAudit(ctx, tx, user.ID, "user.invitation_accept", "user", user.ID,
		map[string]any{"invitation_id": invitationID}, map[string]any{"role": role, "account_status": "active"},
		"accept email invitation", request.RequestID, request.Now); err != nil {
		return identity.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.User{}, fmt.Errorf("commit invited user: %w", err)
	}
	return user, nil
}
