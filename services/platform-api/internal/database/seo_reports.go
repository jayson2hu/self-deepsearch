package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (store *Store) ImportSEOReport(ctx context.Context, input operations.SEOReportImportInput, actorID, requestID string, now time.Time) (operations.SEOReportImport, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.SEOReportImport{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO platform.seo_report_imports
(report_type,dataset_kind,property_uri,period_start,period_end,exported_at,file_sha256,row_count,imported_by,created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::uuid,$10)
ON CONFLICT (report_type,dataset_kind,property_uri,file_sha256) DO NOTHING RETURNING import_id::text`,
		input.ReportType, input.DatasetKind, input.PropertyURI, input.PeriodStart, input.PeriodEnd, input.ExportedAt,
		input.FileSHA256, len(input.Rows), actorID, now).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		item, loadErr := loadSEOReportByKey(ctx, tx, input.ReportType, input.DatasetKind, input.PropertyURI, input.FileSHA256)
		if loadErr != nil {
			return operations.SEOReportImport{}, loadErr
		}
		if err := tx.Commit(ctx); err != nil {
			return operations.SEOReportImport{}, err
		}
		return item, nil
	}
	if err != nil {
		return operations.SEOReportImport{}, mapConstraintError(err)
	}
	for index, row := range input.Rows {
		dimensions, err := json.Marshal(row.Dimensions)
		if err != nil {
			return operations.SEOReportImport{}, err
		}
		metrics, err := json.Marshal(row.Metrics)
		if err != nil {
			return operations.SEOReportImport{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO platform.seo_report_rows(import_id,row_number,dimensions,metrics)
VALUES ($1::uuid,$2,$3::jsonb,$4::jsonb)`, id, index+1, dimensions, metrics); err != nil {
			return operations.SEOReportImport{}, mapConstraintError(err)
		}
	}
	if err := insertAudit(ctx, tx, actorID, "seo.report_import", "seo_report_import", id, nil,
		map[string]any{"report_type": input.ReportType, "dataset_kind": input.DatasetKind, "property_uri": input.PropertyURI, "row_count": len(input.Rows)},
		"import official SEO export", requestID, now); err != nil {
		return operations.SEOReportImport{}, err
	}
	item, err := loadSEOReport(ctx, tx, id)
	if err != nil {
		return operations.SEOReportImport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.SEOReportImport{}, err
	}
	return item, nil
}

func (store *Store) ListSEOReports(ctx context.Context, limit int) ([]operations.SEOReportImport, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := store.pool.Query(ctx, `SELECT report.import_id::text,report.report_type,report.dataset_kind,report.property_uri,
report.period_start,report.period_end,report.exported_at,report.file_sha256,report.row_count,report.status,
report.error_summary,account.normalized_email,report.created_at
FROM platform.seo_report_imports report JOIN platform.users account ON account.user_id=report.imported_by
ORDER BY report.created_at DESC,report.import_id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list seo reports: %w", err)
	}
	defer rows.Close()
	items := make([]operations.SEOReportImport, 0)
	for rows.Next() {
		item, err := scanSEOReport(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type seoReportScanner interface{ Scan(...any) error }

func scanSEOReport(row seoReportScanner) (operations.SEOReportImport, error) {
	var item operations.SEOReportImport
	err := row.Scan(&item.ID, &item.ReportType, &item.DatasetKind, &item.PropertyURI, &item.PeriodStart,
		&item.PeriodEnd, &item.ExportedAt, &item.FileSHA256, &item.RowCount, &item.Status,
		&item.ErrorSummary, &item.ImportedBy, &item.CreatedAt)
	return item, err
}

func loadSEOReport(ctx context.Context, tx pgx.Tx, id string) (operations.SEOReportImport, error) {
	return scanSEOReport(tx.QueryRow(ctx, `SELECT report.import_id::text,report.report_type,report.dataset_kind,report.property_uri,
report.period_start,report.period_end,report.exported_at,report.file_sha256,report.row_count,report.status,
report.error_summary,account.normalized_email,report.created_at
FROM platform.seo_report_imports report JOIN platform.users account ON account.user_id=report.imported_by
WHERE report.import_id=$1::uuid`, id))
}

func loadSEOReportByKey(ctx context.Context, tx pgx.Tx, reportType, datasetKind, propertyURI, hash string) (operations.SEOReportImport, error) {
	return scanSEOReport(tx.QueryRow(ctx, `SELECT report.import_id::text,report.report_type,report.dataset_kind,report.property_uri,
report.period_start,report.period_end,report.exported_at,report.file_sha256,report.row_count,report.status,
report.error_summary,account.normalized_email,report.created_at
FROM platform.seo_report_imports report JOIN platform.users account ON account.user_id=report.imported_by
WHERE report.report_type=$1 AND report.dataset_kind=$2 AND report.property_uri=$3 AND report.file_sha256=$4`, reportType, datasetKind, propertyURI, hash))
}
