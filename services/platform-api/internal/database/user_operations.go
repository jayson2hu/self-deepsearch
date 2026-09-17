package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (store *Store) ListUsersPage(ctx context.Context, query operations.UserListQuery) (operations.UserPage, error) {
	if query.Limit < 1 || query.Limit > 100 {
		query.Limit = 25
	}
	if query.SnapshotAt.IsZero() {
		query.SnapshotAt = time.Now().UTC()
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return operations.UserPage{}, fmt.Errorf("begin user list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const filters = `
FROM platform.users
WHERE created_at <= $1
  AND ($2 = '' OR normalized_email ILIKE '%' || $2 || '%' OR user_id::text = $2)
  AND ($3 = '' OR role = $3)
  AND ($4 = '' OR account_status = $4)
  AND ($5::timestamptz IS NULL OR created_at >= $5)
  AND ($6::timestamptz IS NULL OR created_at <= $6)`
	arguments := []any{query.SnapshotAt, strings.TrimSpace(strings.ToLower(query.Query)), query.Role, query.Status, query.CreatedFrom, query.CreatedTo}

	var total int64
	if err := tx.QueryRow(ctx, `SELECT count(*) `+filters, arguments...).Scan(&total); err != nil {
		return operations.UserPage{}, fmt.Errorf("count users: %w", err)
	}

	rows, err := tx.Query(ctx, `
SELECT user_id::text, normalized_email, role, account_status, created_at, last_login_at,
       email_verified_at, registration_origin, is_test, registered_at
`+filters+`
  AND ($7::timestamptz IS NULL OR created_at < $7 OR (created_at = $7 AND user_id > nullif($8, '')::uuid))
ORDER BY created_at DESC, user_id ASC
LIMIT $9`, append(arguments, query.AfterCreatedAt, query.AfterID, query.Limit+1)...)
	if err != nil {
		return operations.UserPage{}, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	items := make([]operations.User, 0, query.Limit+1)
	for rows.Next() {
		var item operations.User
		if err := rows.Scan(
			&item.ID, &item.Email, &item.Role, &item.AccountStatus, &item.CreatedAt, &item.LastLoginAt,
			&item.EmailVerifiedAt, &item.RegistrationOrigin, &item.IsTest, &item.RegisteredAt,
		); err != nil {
			return operations.UserPage{}, fmt.Errorf("scan user: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return operations.UserPage{}, err
	}
	hasMore := len(items) > query.Limit
	if hasMore {
		items = items[:query.Limit]
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.UserPage{}, fmt.Errorf("commit user list: %w", err)
	}
	return operations.UserPage{Items: items, Total: total, HasMore: hasMore}, nil
}

func (store *Store) UserSummary(ctx context.Context, now time.Time, environment string) (operations.UserSummaryMetrics, error) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return operations.UserSummaryMetrics{}, fmt.Errorf("load summary timezone: %w", err)
	}
	local := now.In(location)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).UTC()
	sevenDayStart := dayStart.AddDate(0, 0, -6)
	result := operations.UserSummaryMetrics{
		ObservedAt: now.UTC(), Environment: environment, DefinitionVersion: "users-v1",
	}
	err = store.pool.QueryRow(ctx, `
SELECT count(*),
       count(*) FILTER (WHERE role = 'user'),
       count(*) FILTER (WHERE role = 'user' AND account_status = 'active'),
       count(*) FILTER (WHERE role = 'user' AND email_verified_at IS NOT NULL),
       count(*) FILTER (WHERE role IN ('owner', 'admin', 'editor')),
       count(*) FILTER (WHERE account_status = 'closed'),
       count(*) FILTER (WHERE registration_origin = 'public_signup' AND NOT is_test AND registered_at IS NOT NULL),
       count(*) FILTER (WHERE registration_origin = 'public_signup' AND NOT is_test AND registered_at >= $1),
       count(*) FILTER (WHERE registration_origin = 'public_signup' AND NOT is_test AND registered_at >= $2),
       count(*) FILTER (WHERE registration_origin = 'unknown'),
       min(registered_at) FILTER (WHERE registration_origin = 'public_signup' AND NOT is_test AND registered_at IS NOT NULL)
FROM platform.users`, dayStart, sevenDayStart).Scan(
		&result.AccountTotal, &result.OrdinaryTotal, &result.ActiveOrdinary, &result.VerifiedOrdinary,
		&result.ManagementTotal, &result.ClosedTotal, &result.PublicRegistrations,
		&result.PublicRegistrationsToday, &result.PublicRegistrationsSevenDays,
		&result.UnknownOrigin, &result.CoverageStartedAt,
	)
	if err != nil {
		return operations.UserSummaryMetrics{}, fmt.Errorf("summarize users: %w", err)
	}
	return result, nil
}

func (store *Store) GetUser(ctx context.Context, userID string) (operations.User, error) {
	var item operations.User
	err := store.pool.QueryRow(ctx, `
SELECT user_id::text, normalized_email, role, account_status, created_at, last_login_at,
       email_verified_at, registration_origin, is_test, registered_at
FROM platform.users WHERE user_id = $1::uuid`, userID).Scan(
		&item.ID, &item.Email, &item.Role, &item.AccountStatus, &item.CreatedAt, &item.LastLoginAt,
		&item.EmailVerifiedAt, &item.RegistrationOrigin, &item.IsTest, &item.RegisteredAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.User{}, operations.ErrNotFound
	}
	if err != nil {
		return operations.User{}, fmt.Errorf("get user: %w", err)
	}
	return item, nil
}
