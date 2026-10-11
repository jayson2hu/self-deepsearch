package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"self-deepsearch/services/platform-api/internal/operations"
)

func lockEntity(ctx context.Context, tx pgx.Tx, entityType, entityID string) error {
	table, idColumn, err := entityTable(entityType)
	if err != nil {
		return err
	}
	var id string
	query := fmt.Sprintf("SELECT %s::text FROM platform.%s WHERE %s = $1::uuid FOR UPDATE", idColumn, table, idColumn)
	if err := tx.QueryRow(ctx, query, entityID).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return operations.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock %s: %w", entityType, err)
	}
	return nil
}

func setEntityStatus(ctx context.Context, tx pgx.Tx, entityType, entityID, status string) error {
	table, idColumn, err := entityTable(entityType)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("UPDATE platform.%s SET publication_status = $2 WHERE %s = $1::uuid", table, idColumn)
	command, err := tx.Exec(ctx, query, entityID, status)
	if err != nil {
		return fmt.Errorf("set %s status: %w", entityType, err)
	}
	if command.RowsAffected() == 0 {
		return operations.ErrNotFound
	}
	return nil
}

func readEntityStatus(ctx context.Context, tx pgx.Tx, entityType, entityID string) (string, error) {
	table, idColumn, err := entityTable(entityType)
	if err != nil {
		return "", err
	}
	query := fmt.Sprintf("SELECT publication_status FROM platform.%s WHERE %s = $1::uuid", table, idColumn)
	var status string
	if err := tx.QueryRow(ctx, query, entityID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return "", operations.ErrNotFound
	} else if err != nil {
		return "", fmt.Errorf("read %s status: %w", entityType, err)
	}
	return status, nil
}

func entityTable(entityType string) (string, string, error) {
	switch entityType {
	case "work":
		return "works", "work_id", nil
	case "performer":
		return "performers", "performer_id", nil
	case "studio":
		return "studios", "studio_id", nil
	default:
		return "", "", operations.ErrInvalidInput
	}
}

func mapConstraintError(err error) error {
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch databaseError.Code {
		case "23505":
			return operations.ErrConflict
		case "23503", "23514", "22P02", "22007":
			return operations.ErrInvalidInput
		}
	}
	return err
}
