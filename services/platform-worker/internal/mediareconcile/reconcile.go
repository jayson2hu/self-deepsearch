package mediareconcile

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"self-deepsearch/services/platform-worker/internal/logsafe"
)

type Object struct {
	StorageScope string `json:"storage_scope"`
	StorageKey   string `json:"storage_key"`
	BackupPath   string `json:"backup_path"`
	SHA256       string `json:"sha256"`
	ByteSize     int64  `json:"byte_size"`
}

type Run struct {
	ID      string
	Objects []Object
}

type Report struct {
	RunID              string              `json:"run_id"`
	ExpectedCount      int                 `json:"expected_count"`
	MissingS3Count     int                 `json:"missing_s3_count"`
	CorruptS3Count     int                 `json:"corrupt_s3_count"`
	MissingBackupCount int                 `json:"missing_backup_count"`
	CorruptBackupCount int                 `json:"corrupt_backup_count"`
	OrphanS3Count      int                 `json:"orphan_s3_count"`
	OrphanBackupCount  int                 `json:"orphan_backup_count"`
	IssueSamples       map[string][]string `json:"issue_samples"`
}

func (report Report) IssueCount() int64 {
	return int64(report.MissingS3Count) + int64(report.CorruptS3Count) + int64(report.MissingBackupCount) + int64(report.CorruptBackupCount) + int64(report.OrphanS3Count) + int64(report.OrphanBackupCount)
}

type Repository interface {
	Prepare(context.Context, time.Time, time.Duration) (Run, error)
	Complete(context.Context, Report, time.Time) error
	Fail(context.Context, string, string, time.Time) error
}

var (
	ErrNotDue        = errors.New("media reconciliation is not due")
	ErrInvalidReport = errors.New("media reconciliation report is invalid")
)

type Client interface {
	Reconcile(context.Context, Run) (Report, error)
}

type Admission interface {
	Allow(context.Context) error
}

type Runner struct {
	Repository Repository
	Client     Client
	Admission  Admission
	Every      time.Duration
	Logger     *slog.Logger
	Now        func() time.Time
}

func (runner Runner) Run(ctx context.Context) error {
	if runner.Repository == nil || runner.Client == nil {
		return errors.New("media reconciliation repository and client are required")
	}
	if runner.Every < time.Hour || runner.Every > 7*24*time.Hour {
		return errors.New("media reconciliation interval must be between 1h and 168h")
	}
	if runner.Logger == nil {
		runner.Logger = slog.Default()
	}
	if runner.Now == nil {
		runner.Now = time.Now
	}
	runner.runOnce(ctx)
	// The database owns the full-scan cadence. Recheck admission every minute
	// so a skipped daily job can resume promptly without waiting another day.
	pollEvery := runner.Every
	if runner.Admission != nil {
		pollEvery = time.Minute
	}
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runner.runOnce(ctx)
		}
	}
}

func (runner Runner) runOnce(ctx context.Context) {
	if runner.Admission != nil && runner.Admission.Allow(ctx) != nil {
		runner.Logger.WarnContext(ctx, "media_reconciliation_usage_deferred", "stage", "before_prepare")
		return
	}
	now := runner.Now().UTC()
	prepareCtx, prepareCancel := context.WithTimeout(ctx, 5*time.Second)
	run, err := runner.Repository.Prepare(prepareCtx, now, runner.Every)
	prepareCancel()
	if err != nil {
		if errors.Is(err, ErrNotDue) {
			return
		}
		runner.Logger.ErrorContext(ctx, "media_reconciliation_prepare_failed", "error_class", logsafe.ErrorClass(err))
		return
	}
	// Inventory preparation can wait on locks. A permit from before that wait
	// is not sufficient to authorize the cross-region/remote storage request.
	if runner.Admission != nil && runner.Admission.Allow(ctx) != nil {
		failCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := runner.Repository.Fail(failCtx, run.ID, "usage_guard_denied", runner.Now().UTC()); err != nil {
			runner.Logger.ErrorContext(ctx, "media_reconciliation_fail_record_failed", "run_id", run.ID, "error_class", logsafe.ErrorClass(err))
		}
		runner.Logger.WarnContext(ctx, "media_reconciliation_usage_deferred", "stage", "before_dispatch", "run_id", run.ID)
		return
	}
	report, err := runner.Client.Reconcile(ctx, run)
	if err != nil {
		code := "reconcile_unavailable"
		if errors.Is(err, ErrInvalidReport) {
			code = "invalid_report"
		}
		if failErr := runner.Repository.Fail(ctx, run.ID, code, runner.Now().UTC()); failErr != nil {
			runner.Logger.ErrorContext(ctx, "media_reconciliation_fail_record_failed", "run_id", run.ID, "error_class", logsafe.ErrorClass(failErr))
		}
		runner.Logger.ErrorContext(ctx, "media_reconciliation_failed", "run_id", run.ID, "error_class", logsafe.ErrorClass(err))
		return
	}
	if !report.validFor(run) {
		_ = runner.Repository.Fail(ctx, run.ID, "invalid_report", runner.Now().UTC())
		runner.Logger.ErrorContext(ctx, "media_reconciliation_report_invalid", "run_id", run.ID)
		return
	}
	if err := runner.Repository.Complete(ctx, report, runner.Now().UTC()); err != nil {
		runner.Logger.ErrorContext(ctx, "media_reconciliation_complete_failed", "run_id", run.ID, "error_class", logsafe.ErrorClass(err))
		return
	}
	runner.Logger.InfoContext(ctx, "media_reconciliation_completed", "run_id", run.ID, "expected_count", report.ExpectedCount, "issue_count", report.IssueCount())
}
