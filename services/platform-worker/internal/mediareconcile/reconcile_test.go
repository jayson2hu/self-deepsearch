package mediareconcile

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeRepository struct {
	run       Run
	completed *Report
	failed    string
}

func (repository *fakeRepository) Prepare(context.Context, time.Time, time.Duration) (Run, error) {
	return repository.run, nil
}
func (repository *fakeRepository) Complete(_ context.Context, report Report, _ time.Time) error {
	repository.completed = &report
	return nil
}
func (repository *fakeRepository) Fail(_ context.Context, _ string, code string, _ time.Time) error {
	repository.failed = code
	return nil
}

type fakeClient struct{ report Report }

func (client fakeClient) Reconcile(context.Context, Run) (Report, error) { return client.report, nil }

func TestRunOnceCompletesMatchingReport(t *testing.T) {
	run := Run{ID: "10000000-0000-4000-8000-000000000001", Objects: []Object{{StorageScope: "private", StorageKey: "media-master/a", BackupPath: "media-master/a", SHA256: "a", ByteSize: 1}}}
	repository := &fakeRepository{run: run}
	report := completeReport(run)
	report.MissingS3Count = 1
	report.IssueSamples["missing_s3"] = []string{run.Objects[0].StorageKey}
	runner := Runner{Repository: repository, Client: fakeClient{report: report}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return time.Unix(1, 0) }}
	runner.runOnce(context.Background())
	if repository.completed == nil || repository.completed.IssueCount() != 1 || repository.failed != "" {
		t.Fatalf("matching report was not completed: completed=%v failed=%s", repository.completed, repository.failed)
	}
}

func TestRunOnceRejectsMismatchedReport(t *testing.T) {
	run := Run{ID: "10000000-0000-4000-8000-000000000001", Objects: []Object{{}}}
	repository := &fakeRepository{run: run}
	runner := Runner{Repository: repository, Client: fakeClient{report: Report{RunID: "different", ExpectedCount: 1}}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now}
	runner.runOnce(context.Background())
	if repository.failed != "invalid_report" || repository.completed != nil {
		t.Fatalf("mismatched report must fail: completed=%v failed=%s", repository.completed, repository.failed)
	}
}
