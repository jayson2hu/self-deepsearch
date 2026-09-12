package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

// BootstrapOwner creates the first owner or rotates the password of the existing
// owner with the same email. It refuses to create a second owner implicitly.
func (store *Store) BootstrapOwner(ctx context.Context, email, passwordHash string, now time.Time) (string, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin owner bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('platform.bootstrap_owner'))`); err != nil {
		return "", fmt.Errorf("lock owner bootstrap: %w", err)
	}
	var existingOwnerEmail string
	err = tx.QueryRow(ctx, `
SELECT normalized_email FROM platform.users
WHERE role = 'owner' AND account_status <> 'closed'
ORDER BY created_at LIMIT 1 FOR UPDATE`).Scan(&existingOwnerEmail)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("check existing owner: %w", err)
	}
	if err == nil && existingOwnerEmail != email {
		return "", operations.ErrConflict
	}
	var userID string
	err = tx.QueryRow(ctx, `
INSERT INTO platform.users (normalized_email, email_verified_at, password_hash, role, account_status)
VALUES ($1, $2, $3, 'owner', 'active')
ON CONFLICT (normalized_email) WHERE account_status <> 'closed' DO UPDATE
SET email_verified_at = EXCLUDED.email_verified_at, password_hash = EXCLUDED.password_hash,
    role = 'owner', account_status = 'active', closed_at = NULL
RETURNING user_id::text`, email, now, passwordHash).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("upsert bootstrap owner: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.sessions SET revoked_at = greatest($2, created_at), revoke_reason = 'owner_bootstrap' WHERE user_id = $1::uuid AND revoked_at IS NULL`, userID, now); err != nil {
		return "", fmt.Errorf("revoke owner sessions: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO audit.audit_logs (actor_type, action, object_type, object_id, reason, request_id, occurred_at)
VALUES ('system', 'owner.bootstrap', 'user', $1::uuid, 'deployment bootstrap or credential rotation', 'bootstrap-owner', $2)`, userID, now); err != nil {
		return "", fmt.Errorf("audit owner bootstrap: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit owner bootstrap: %w", err)
	}
	return userID, nil
}
