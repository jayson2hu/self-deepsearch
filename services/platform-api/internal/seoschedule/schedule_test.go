package seoschedule

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"self-deepsearch/services/platform-api/internal/operations"
	"testing"
	"time"
)

type testRepo struct {
	calls  int
	cancel context.CancelFunc
}

func (r *testRepo) RunScheduledSEOScan(ctx context.Context, actor string, _ time.Time) (operations.SEOScan, error) {
	r.calls++
	if actor != "configured-owner" {
		panic("wrong actor")
	}
	if _, ok := ctx.Deadline(); !ok {
		panic("unbounded scan")
	}
	if r.calls == 2 {
		r.cancel()
		return operations.SEOScan{}, nil
	}
	return operations.SEOScan{}, errors.New("retryable failure")
}
func TestRetriesAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &testRepo{cancel: cancel}
	run(ctx, r, "configured-owner", slog.New(slog.NewTextHandler(io.Discard, nil)), time.Millisecond)
	if r.calls != 2 {
		t.Fatalf("calls=%d", r.calls)
	}
	run(ctx, r, "configured-owner", slog.Default(), time.Millisecond)
	if r.calls != 2 {
		t.Fatal("canceled scheduler must not start a job")
	}
}
