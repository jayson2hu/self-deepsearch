package mediainspect

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type memoryRepository struct {
	prepareErr, snapshotErr, completeErr error
	snapshot                             Snapshot
	completed                            *Report
	failure                              string
	prepared, inspected, completeCalls   int
}

func (r *memoryRepository) Prepare(ctx context.Context, now time.Time, origin string) (string, error) {
	r.prepared++
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 45*time.Second {
		return "", errors.New("missing run deadline")
	}
	return "run-a", r.prepareErr
}
func (r *memoryRepository) Snapshot(context.Context) (Snapshot, error) {
	r.inspected++
	return r.snapshot, r.snapshotErr
}
func (r *memoryRepository) Complete(_ context.Context, report Report, _ time.Time) error {
	r.completeCalls++
	if r.completeErr != nil {
		return r.completeErr
	}
	r.completed = &report
	return nil
}
func (r *memoryRepository) Fail(_ context.Context, id, code string, _ time.Time) error {
	r.failure = code
	return nil
}

type fakeDefaults struct {
	results []DefaultResult
	calls   int
}

func (*fakeDefaults) Origin() string                          { return "https://display.example.test" }
func (c *fakeDefaults) Check(context.Context) []DefaultResult { c.calls++; return c.results }
func healthyDefaults() []DefaultResult {
	return []DefaultResult{{Path: defaultAssets[0].Path, HTTPStatus: 200}, {Path: defaultAssets[1].Path, HTTPStatus: 200}}
}

func TestInspectionRunnerFailureAndRecovery(t *testing.T) {
	for _, test := range []struct{ name, failure string }{
		{"healthy", ""}, {"default-fails", ""}, {"not-due", ""}, {"snapshot-error", "snapshot_unavailable"},
		{"oversized", "inventory_too_large"}, {"incomplete-probes", "invalid_default_report"}, {"complete-error", "completion_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &memoryRepository{snapshot: preparedSnapshot()}
			checker := &fakeDefaults{results: healthyDefaults()}
			switch test.name {
			case "not-due":
				repo.prepareErr = ErrNotDue
			case "snapshot-error":
				repo.snapshotErr = errors.New("private details never logged")
			case "oversized":
				repo.snapshotErr = ErrInventoryTooLarge
			case "incomplete-probes":
				checker.results = checker.results[:1]
			case "default-fails":
				checker.results[0].HTTPStatus, checker.results[0].ErrorCode = 404, "http_status"
			case "complete-error":
				repo.completeErr = errors.New("write failed")
			}
			runner := Runner{Repository: repo, Defaults: checker, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now}
			runner.runOnce(context.Background())
			if repo.failure != test.failure {
				t.Fatalf("failure %q != %q", repo.failure, test.failure)
			}
			if test.failure != "" && repo.completed != nil {
				t.Fatal("failure recorded as completed")
			}
			if test.name == "not-due" && (repo.inspected != 0 || checker.calls != 0 || repo.completeCalls != 0) {
				t.Fatal("not-due run performed work")
			}
			if test.name == "default-fails" && (repo.completed == nil || repo.completed.DefaultFailures != 1) {
				t.Fatal("failed probe counted as healthy")
			}
			if test.name == "healthy" && (repo.completed == nil || repo.completed.PublicationIssues != 0) {
				t.Fatal("healthy inspection not completed")
			}
		})
	}
}

func TestInspectionRunnerRejectsMissingDependencies(t *testing.T) {
	if err := (Runner{}).Run(context.Background()); err == nil {
		t.Fatal("missing checker/repository accepted")
	}
}
