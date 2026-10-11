package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (store *Store) ListUsers(ctx context.Context, limit int) ([]operations.User, error) {
	rows, err := store.pool.Query(ctx, `
SELECT user_id::text, normalized_email, role, account_status, created_at, last_login_at
FROM platform.users ORDER BY created_at DESC, user_id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	items := make([]operations.User, 0)
	for rows.Next() {
		var item operations.User
		if err := rows.Scan(&item.ID, &item.Email, &item.Role, &item.AccountStatus, &item.CreatedAt, &item.LastLoginAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) ChangeUserRole(ctx context.Context, userID, role, actorID, requestID string, now time.Time) (operations.User, error) {
	return changeUserRole(ctx, store.pool.BeginTx, userID, role, actorID, requestID, now)
}

// Keep transaction orchestration testable without granting tests a live
// database. SQL and locking semantics still need the PostgreSQL contract.

func changeUserRole(ctx context.Context, begin func(context.Context, pgx.TxOptions) (pgx.Tx, error), userID, role, actorID, requestID string, now time.Time) (operations.User, error) {
	if userID == actorID || role == "owner" {
		return operations.User{}, operations.ErrForbidden
	}
	if role != "admin" && role != "editor" && role != "user" {
		return operations.User{}, operations.ErrInvalidInput
	}
	tx, err := begin(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.User{}, fmt.Errorf("begin role change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item operations.User
	var oldRole, actorRole string
	if err = tx.QueryRow(ctx, `
SELECT role FROM platform.users
WHERE user_id = $1::uuid AND account_status <> 'closed'
FOR UPDATE`, userID).Scan(&oldRole); errors.Is(err, pgx.ErrNoRows) {
		return operations.User{}, operations.ErrNotFound
	} else if err != nil {
		return operations.User{}, fmt.Errorf("load target user role: %w", err)
	}
	// Keep the owner boundary in the repository as well as the HTTP layer. This
	// prevents a future internal caller from changing an owner or using a stale
	// operator session to mutate roles.
	if err = tx.QueryRow(ctx, `
SELECT role FROM platform.users
WHERE user_id = $1::uuid AND account_status = 'active'`, actorID).Scan(&actorRole); errors.Is(err, pgx.ErrNoRows) {
		return operations.User{}, operations.ErrForbidden
	} else if err != nil {
		return operations.User{}, fmt.Errorf("load actor role: %w", err)
	}
	if !operations.CanManageUserRole(actorRole, oldRole, userID == actorID) {
		return operations.User{}, operations.ErrForbidden
	}
	if oldRole == role {
		return operations.User{}, operations.ErrConflict
	}
	err = tx.QueryRow(ctx, `
UPDATE platform.users SET role = $2
WHERE user_id = $1::uuid AND account_status <> 'closed'
RETURNING user_id::text, normalized_email, role, account_status, created_at, last_login_at,
          email_verified_at, registration_origin, is_test, registered_at`, userID, role).Scan(
		&item.ID, &item.Email, &item.Role, &item.AccountStatus, &item.CreatedAt, &item.LastLoginAt,
		&item.EmailVerifiedAt, &item.RegistrationOrigin, &item.IsTest, &item.RegisteredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.User{}, operations.ErrNotFound
	}
	if err != nil {
		return operations.User{}, mapConstraintError(err)
	}
	// CreateSession locks this same account row before verifying the role
	// snapshot and issuing a token. A login issued first is revoked here; a
	// stale login waiting behind this transaction cannot issue a new token.
	// Preserve earlier revocation evidence and the session time constraint.
	revoked, err := tx.Exec(ctx, `
UPDATE platform.sessions SET revoked_at = greatest($2, created_at), revoke_reason = 'role_change'
WHERE user_id = $1::uuid AND revoked_at IS NULL`, userID, now)
	if err != nil {
		return operations.User{}, fmt.Errorf("revoke role-change sessions: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, "user.role_change", "user", userID,
		map[string]any{"role": oldRole}, map[string]any{"role": role, "sessions_revoked": revoked.RowsAffected()}, "owner role management", requestID, now); err != nil {
		return operations.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.User{}, fmt.Errorf("commit role change: %w", err)
	}
	return item, nil
}

func (store *Store) ChangeUserStatus(ctx context.Context, userID, status, actorID, actorRole, reason, requestID string, now time.Time) (operations.User, error) {
	if userID == actorID {
		return operations.User{}, operations.ErrForbidden
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return operations.User{}, fmt.Errorf("begin user status change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item operations.User
	var previousStatus string
	if err := tx.QueryRow(ctx, `
SELECT user_id::text, normalized_email, role, account_status, created_at, last_login_at,
       email_verified_at, registration_origin, is_test, registered_at
FROM platform.users WHERE user_id = $1::uuid AND account_status <> 'closed' FOR UPDATE`, userID).Scan(
		&item.ID, &item.Email, &item.Role, &previousStatus, &item.CreatedAt, &item.LastLoginAt,
		&item.EmailVerifiedAt, &item.RegistrationOrigin, &item.IsTest, &item.RegisteredAt,
	); errors.Is(err, pgx.ErrNoRows) {
		return operations.User{}, operations.ErrNotFound
	} else if err != nil {
		return operations.User{}, err
	}
	if !operations.CanManageUserStatus(actorRole, item.Role, userID == actorID) ||
		(previousStatus != "active" && previousStatus != "locked" && previousStatus != "suspended") {
		return operations.User{}, operations.ErrForbidden
	}
	if previousStatus == status {
		return operations.User{}, operations.ErrConflict
	}
	if err := tx.QueryRow(ctx, `
UPDATE platform.users SET account_status = $2
WHERE user_id = $1::uuid
RETURNING user_id::text, normalized_email, role, account_status, created_at, last_login_at,
          email_verified_at, registration_origin, is_test, registered_at`, userID, status).Scan(
		&item.ID, &item.Email, &item.Role, &item.AccountStatus, &item.CreatedAt, &item.LastLoginAt,
		&item.EmailVerifiedAt, &item.RegistrationOrigin, &item.IsTest, &item.RegisteredAt,
	); err != nil {
		return operations.User{}, err
	}
	if status != "active" {
		if _, err := tx.Exec(ctx, `
UPDATE platform.sessions SET revoked_at = greatest($2, created_at), revoke_reason = 'account_' || $3
WHERE user_id = $1::uuid AND revoked_at IS NULL`, userID, now, status); err != nil {
			return operations.User{}, err
		}
	}
	if err := insertAudit(ctx, tx, actorID, "user.status_change", "user", userID,
		map[string]any{"account_status": previousStatus}, map[string]any{"account_status": status}, reason, requestID, now); err != nil {
		return operations.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.User{}, fmt.Errorf("commit user status change: %w", err)
	}
	return item, nil
}
