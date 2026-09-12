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

func (store *Store) ChallengeTarget(ctx context.Context, email, purpose string, userID *string) (identity.User, bool, error) {
	var user identity.User
	var err error
	switch purpose {
	case identity.PurposeSignup:
		err = store.pool.QueryRow(ctx, `
SELECT user_id::text, normalized_email, password_hash, role, account_status, email_verified_at
FROM platform.users
WHERE normalized_email = $1 AND account_status <> 'closed'
LIMIT 1`, email).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.AccountStatus, &user.EmailVerifiedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.User{Email: email}, true, nil
		}
		return user, false, err
	case identity.PurposePasswordReset:
		err = store.pool.QueryRow(ctx, `
SELECT user_id::text, normalized_email, password_hash, role, account_status, email_verified_at
FROM platform.users
WHERE normalized_email = $1 AND account_status = 'active'
LIMIT 1`, email).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.AccountStatus, &user.EmailVerifiedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.User{Email: email}, false, nil
		}
		return user, err == nil, err
	case identity.PurposeAccountClose:
		if userID == nil {
			return identity.User{}, false, identity.ErrUnauthenticated
		}
		err = store.pool.QueryRow(ctx, `
SELECT user_id::text, normalized_email, password_hash, role, account_status, email_verified_at
FROM platform.users
WHERE user_id = $1::uuid AND account_status = 'active'`, *userID).Scan(
			&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.AccountStatus, &user.EmailVerifiedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.User{}, false, identity.ErrUnauthenticated
		}
		return user, err == nil, err
	default:
		return identity.User{}, false, errors.New("unsupported challenge purpose")
	}
}

func (store *Store) CreateChallenge(ctx context.Context, request identity.ChallengeRequest) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin challenge transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	action := challengeAction(request.Purpose)
	emailCount, err := incrementRate(ctx, tx, action, "email_hash", request.EmailHash, request.SentAt.Truncate(time.Hour), time.Hour)
	if err != nil {
		return err
	}
	ipLimit := int64(20)
	if request.Purpose == identity.PurposeSignup {
		ipLimit = 5
	}
	ipCount, err := incrementRate(ctx, tx, action, "ip_hash", request.IPHash, request.SentAt.Truncate(time.Hour), time.Hour)
	if err != nil {
		return err
	}
	limited := emailCount > 3 || ipCount > ipLimit

	var recent bool
	if request.Deliver && !limited {
		err = tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM platform.email_challenges
    WHERE normalized_email = $1 AND purpose = $2 AND consumed_at IS NULL
      AND sent_at > $3 - interval '60 seconds'
)`, request.Email, request.Purpose, request.SentAt).Scan(&recent)
		if err != nil {
			return fmt.Errorf("check challenge resend interval: %w", err)
		}
	}
	if limited || recent || !request.Deliver {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit challenge rate limit: %w", err)
		}
		if limited || recent {
			return identity.ErrRateLimited
		}
		return nil
	}

	if _, err := tx.Exec(ctx, `
UPDATE platform.email_challenges
SET consumed_at = $3
WHERE normalized_email = $1 AND purpose = $2 AND consumed_at IS NULL`, request.Email, request.Purpose, request.SentAt); err != nil {
		return fmt.Errorf("invalidate prior challenge: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.email_challenges (
    user_id, normalized_email, purpose, code_hash, sent_at, expires_at, request_ip_hash
) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)`, request.UserID, request.Email, request.Purpose, request.CodeHash, request.SentAt, request.ExpiresAt, request.IPHash); err != nil {
		return fmt.Errorf("insert email challenge: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit email challenge: %w", err)
	}
	return nil
}

func (store *Store) CreateVerifiedUser(ctx context.Context, request identity.SignupRequest) (identity.User, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return identity.User{}, fmt.Errorf("begin signup transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	challengeID, valid, err := lockChallenge(ctx, tx, request.Email, identity.PurposeSignup, request.CodeHash, nil, request.Now)
	if err != nil {
		return identity.User{}, err
	}
	if !valid {
		if err := tx.Commit(ctx); err != nil {
			return identity.User{}, err
		}
		return identity.User{}, identity.ErrInvalidChallenge
	}

	dayStart := request.Now.Truncate(24 * time.Hour)
	count, err := incrementRate(ctx, tx, "signup", "ip_hash", request.IPHash, dayStart, 24*time.Hour)
	if err != nil {
		return identity.User{}, err
	}
	if count > 10 {
		if err := tx.Commit(ctx); err != nil {
			return identity.User{}, err
		}
		return identity.User{}, identity.ErrRateLimited
	}

	var user identity.User
	err = tx.QueryRow(ctx, `
INSERT INTO platform.users (
    normalized_email, email_verified_at, password_hash, role, account_status
) VALUES ($1, $2, $3, 'user', 'active')
RETURNING user_id::text, normalized_email, password_hash, role, account_status, email_verified_at`,
		request.Email, request.Now, request.PasswordHash,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.AccountStatus, &user.EmailVerifiedAt)
	if err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return identity.User{}, identity.ErrInvalidChallenge
		}
		return identity.User{}, fmt.Errorf("create verified user: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.email_challenges SET consumed_at = $2 WHERE challenge_id = $1`, challengeID, request.Now); err != nil {
		return identity.User{}, fmt.Errorf("consume signup challenge: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.sessions (user_id, token_hash, created_at, expires_at, last_used_at)
VALUES ($1::uuid, $2, $3, $4, $3)`, user.ID, request.SessionHash, request.Now, request.SessionUntil); err != nil {
		return identity.User{}, fmt.Errorf("create signup session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.User{}, fmt.Errorf("commit signup: %w", err)
	}
	return user, nil
}

func (store *Store) CheckChallenge(ctx context.Context, email, purpose, codeHash string, userID *string, now time.Time) (bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, valid, err := lockChallenge(ctx, tx, email, purpose, codeHash, userID, now)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return valid, nil
}

func (store *Store) ActiveUserByEmail(ctx context.Context, email string) (identity.User, error) {
	var user identity.User
	err := store.pool.QueryRow(ctx, `
SELECT user_id::text, normalized_email, password_hash, role, account_status, email_verified_at
FROM platform.users
WHERE normalized_email = $1 AND account_status = 'active'`, email).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.AccountStatus, &user.EmailVerifiedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, identity.ErrInvalidCredentials
	}
	return user, err
}

func (store *Store) AllowLogin(ctx context.Context, emailHash, ipHash string, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	start := now.Truncate(time.Hour)
	emailCount, err := incrementRate(ctx, tx, "login", "email_hash", emailHash, start, time.Hour)
	if err != nil {
		return err
	}
	ipCount, err := incrementRate(ctx, tx, "login", "ip_hash", ipHash, start, time.Hour)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if emailCount > 10 || ipCount > 30 {
		return identity.ErrRateLimited
	}
	return nil
}

func (store *Store) RecordLogin(ctx context.Context, userID *string, result, requestID string, now time.Time) error {
	_, err := store.pool.Exec(ctx, `
INSERT INTO audit.login_events (user_id, result, request_id, occurred_at)
VALUES ($1::uuid, $2, $3, $4)`, userID, result, requestID, now)
	return err
}

func (store *Store) CreateSession(ctx context.Context, request identity.SessionRequest) error {
	command, err := store.pool.Exec(ctx, `
WITH verified_account AS MATERIALIZED (
    SELECT user_id FROM platform.users
    WHERE user_id = $1::uuid AND normalized_email = $5 AND password_hash = $6
      AND role = $7 AND account_status = 'active' AND email_verified_at IS NOT NULL
    FOR UPDATE
)
INSERT INTO platform.sessions (user_id, token_hash, created_at, expires_at, last_used_at)
SELECT user_id, $2, $3, $4, $3 FROM verified_account`,
		request.UserID, request.TokenHash, request.CreatedAt, request.ExpiresAt,
		request.ExpectedEmail, request.ExpectedPasswordHash, request.ExpectedRole)
	if err != nil {
		return fmt.Errorf("create authenticated session: %w", err)
	}
	if command.RowsAffected() != 1 {
		return identity.ErrInvalidCredentials
	}
	return nil
}

func (store *Store) UserBySession(ctx context.Context, tokenHash string, now time.Time) (identity.User, error) {
	var user identity.User
	err := store.pool.QueryRow(ctx, `
WITH refreshed AS (
    UPDATE platform.sessions AS session
    SET last_used_at = $2
    FROM platform.users AS account
    WHERE session.token_hash = $1
      AND session.revoked_at IS NULL
      AND session.expires_at > $2
      AND account.user_id = session.user_id
      AND account.account_status = 'active'
    RETURNING account.user_id, account.normalized_email, account.password_hash,
              account.role, account.account_status, account.email_verified_at
)
SELECT user_id::text, normalized_email, password_hash, role, account_status, email_verified_at
FROM refreshed`, tokenHash, now).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.AccountStatus, &user.EmailVerifiedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, identity.ErrUnauthenticated
	}
	return user, err
}

func (store *Store) RevokeSession(ctx context.Context, tokenHash, reason string, now time.Time) error {
	_, err := store.pool.Exec(ctx, `
UPDATE platform.sessions SET revoked_at = greatest($2, created_at), revoke_reason = $3
WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash, now, reason)
	return err
}

func (store *Store) ResetPassword(ctx context.Context, request identity.PasswordResetRequest) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	challengeID, valid, err := lockChallenge(ctx, tx, request.Email, identity.PurposePasswordReset, request.CodeHash, nil, request.Now)
	if err != nil {
		return err
	}
	if !valid {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return identity.ErrInvalidChallenge
	}
	var userID string
	if err := tx.QueryRow(ctx, `
UPDATE platform.users SET password_hash = $2
WHERE normalized_email = $1 AND account_status = 'active'
  AND user_id = (SELECT user_id FROM platform.email_challenges WHERE challenge_id = $3::uuid)
RETURNING user_id::text`, request.Email, request.PasswordHash, challengeID).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrInvalidChallenge
		}
		return fmt.Errorf("update verified password: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.email_challenges SET consumed_at = $2 WHERE challenge_id = $1`, challengeID, request.Now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE platform.sessions SET revoked_at = greatest($2, created_at), revoke_reason = 'password_reset'
WHERE user_id = $1::uuid AND revoked_at IS NULL`, userID, request.Now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) CloseAccount(ctx context.Context, request identity.CloseAccountRequest) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	challengeID, valid, err := lockChallenge(ctx, tx, "", identity.PurposeAccountClose, request.CodeHash, &request.UserID, request.Now)
	if err != nil {
		return err
	}
	if !valid {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return identity.ErrInvalidChallenge
	}
	result, err := tx.Exec(ctx, `
UPDATE platform.users SET account_status = 'closed', closed_at = $2
WHERE user_id = $1::uuid AND account_status = 'active'`, request.UserID, request.Now)
	if err != nil || result.RowsAffected() != 1 {
		return identity.ErrUnauthenticated
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.email_challenges SET consumed_at = $2 WHERE challenge_id = $1`, challengeID, request.Now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE platform.sessions SET revoked_at = greatest($2, created_at), revoke_reason = 'account_closed'
WHERE user_id = $1::uuid AND revoked_at IS NULL`, request.UserID, request.Now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO audit.account_closure_events (
    user_id, challenge_id, notice_version, actor_id, request_id, closed_at
) VALUES ($1::uuid, $2::uuid, $3, $1::uuid, $4, $5)`,
		request.UserID, challengeID, request.NoticeVersion, request.RequestID, request.Now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func challengeAction(purpose string) string {
	switch purpose {
	case identity.PurposeSignup:
		return "signup_code"
	case identity.PurposePasswordReset:
		return "password_code"
	default:
		return "account_close"
	}
}

func incrementRate(ctx context.Context, tx pgx.Tx, action, dimensionType, dimensionHash string, start time.Time, window time.Duration) (int64, error) {
	var count int64
	err := tx.QueryRow(ctx, `
INSERT INTO platform.security_rate_limits (
    action, dimension_type, dimension_hash, window_start, window_seconds,
    request_count, success_count, expires_at
) VALUES ($1, $2, $3, $4, $5, 1, 0, $4 + ($5 * interval '1 second'))
ON CONFLICT (action, dimension_type, dimension_hash, window_start, window_seconds)
DO UPDATE SET request_count = platform.security_rate_limits.request_count + 1
RETURNING request_count`, action, dimensionType, dimensionHash, start, int(window.Seconds())).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("increment security rate: %w", err)
	}
	return count, nil
}

func lockChallenge(ctx context.Context, tx pgx.Tx, email, purpose, codeHash string, userID *string, now time.Time) (string, bool, error) {
	var challengeID string
	query := `
SELECT challenge_id::text
FROM platform.email_challenges
WHERE purpose = $1 AND code_hash = $2 AND consumed_at IS NULL
  AND expires_at > $3 AND attempt_count < 5`
	args := []any{purpose, codeHash, now}
	if purpose == identity.PurposePasswordReset {
		query += ` AND EXISTS (
    SELECT 1 FROM platform.users account
    WHERE account.user_id = platform.email_challenges.user_id
      AND account.normalized_email = platform.email_challenges.normalized_email
      AND account.account_status = 'active' AND account.email_verified_at IS NOT NULL
)`
	}
	if userID != nil {
		query += ` AND user_id = $4::uuid`
		args = append(args, *userID)
	} else {
		query += ` AND normalized_email = $4`
		args = append(args, email)
	}
	query += ` ORDER BY sent_at DESC LIMIT 1 FOR UPDATE`
	err := tx.QueryRow(ctx, query, args...).Scan(&challengeID)
	if err == nil {
		return challengeID, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}

	update := `
UPDATE platform.email_challenges
SET attempt_count = least(attempt_count + 1, 5)
WHERE challenge_id = (
    SELECT challenge_id FROM platform.email_challenges
    WHERE purpose = $1 AND consumed_at IS NULL AND expires_at > $2`
	updateArgs := []any{purpose, now}
	if userID != nil {
		update += ` AND user_id = $3::uuid`
		updateArgs = append(updateArgs, *userID)
	} else {
		update += ` AND normalized_email = $3`
		updateArgs = append(updateArgs, email)
	}
	update += ` ORDER BY sent_at DESC LIMIT 1 FOR UPDATE)`
	if _, updateErr := tx.Exec(ctx, update, updateArgs...); updateErr != nil {
		return "", false, updateErr
	}
	return "", false, nil
}
