package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"self-deepsearch/services/platform-api/internal/operations"
)

type usageControlRow func(...any) error

func (f usageControlRow) Scan(dest ...any) error { return f(dest...) }

type usageControlTx struct {
	pgx.Tx
	t                                *testing.T
	steps                            []string
	fail                             string
	allowed, replay, matches, active bool
	digest                           string
	now                              time.Time
	input                            operations.MediaUsageReviewInput
	options                          pgx.TxOptions
}

func (tx *usageControlTx) step(n string) error {
	tx.steps = append(tx.steps, n)
	if tx.fail == n {
		return errors.New("synthetic private database detail")
	}
	return nil
}
func (tx *usageControlTx) begin(_ context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx.options = o
	return tx, tx.step("begin")
}
func (tx *usageControlTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	n := "lock"
	if strings.Contains(sql, "INSERT INTO audit.audit_logs") {
		n = "audit"
	}
	return pgconn.NewCommandTag("INSERT 1"), tx.step(n)
}
func (tx *usageControlTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	return usageControlRow(func(dest ...any) error {
		switch {
		case strings.Contains(sql, "lock_media_usage_reviewer"):
			if e := tx.step("authorize"); e != nil {
				return e
			}
			*dest[0].(*bool) = tx.allowed
		case strings.Contains(sql, "idempotency_key=$2"):
			if e := tx.step("replay"); e != nil {
				return e
			}
			if !tx.replay {
				return pgx.ErrNoRows
			}
			*dest[0].(*string) = tx.input.IdempotencyKey
			*dest[1].(*bool) = tx.matches
		case strings.Contains(sql, "platform.lock_media_usage_review_state()"):
			if e := tx.step("state"); e != nil {
				return e
			}
			*dest[0].(*string) = tx.digest
			*dest[1].(*time.Time) = tx.now
			*dest[2].(*time.Time) = tx.now.Add(-time.Hour)
			*dest[3].(*bool) = tx.active
		default:
			n := "load-replay"
			if strings.Contains(sql, "INSERT INTO audit.media_usage_reviews") {
				n = "insert"
				if len(args) != 11 || args[10].(time.Time).Sub(args[9].(time.Time)) != 5*time.Minute {
					tx.t.Error("request expiry or immutable arguments missing")
				}
			}
			if e := tx.step(n); e != nil {
				return e
			}
			*dest[0].(*string) = tx.input.IdempotencyKey
			*dest[1].(*string) = tx.input.Action
			*dest[2].(*string) = "pending"
			*dest[3].(*time.Time) = tx.now
			*dest[4].(*time.Time) = tx.now.Add(5 * time.Minute)
			*dest[5].(**time.Time) = nil
			*dest[6].(**string) = nil
			*dest[7].(*string) = tx.input.Reason
			*dest[8].(**time.Time) = tx.input.NextPeriodStart
			*dest[9].(**time.Time) = tx.input.NextPeriodEnd
		}
		return nil
	})
}
func (tx *usageControlTx) Commit(context.Context) error   { return tx.step("commit") }
func (tx *usageControlTx) Rollback(context.Context) error { return tx.step("rollback") }
func usageInput(now time.Time) operations.MediaUsageReviewInput {
	return operations.MediaUsageReviewInput{IdempotencyKey: "90000000-0000-4000-8000-000000000001", Action: "acknowledge", ExpectedDigest: strings.Repeat("a", 64), ExpectedUpdatedAt: now, Reason: "人工确认", Confirmed: true}
}
func TestUsageReviewQueueRollbackCASAndIdempotency(t *testing.T) {
	now := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	for _, failure := range []string{"", "begin", "lock", "authorize", "replay", "state", "insert", "audit", "commit"} {
		t.Run(failure, func(t *testing.T) {
			input := usageInput(now)
			tx := &usageControlTx{t: t, allowed: true, fail: failure, digest: input.ExpectedDigest, now: now, input: input}
			item, err := submitMediaUsageReview(context.Background(), tx.begin, input, input.IdempotencyKey, "review-test", now)
			if (err != nil) != (failure != "") || (err != nil && item.ID != "") {
				t.Fatalf("uncertain request reported success: %v", err)
			}
			if failure == "" && (tx.options.IsoLevel != pgx.ReadCommitted || strings.Join(tx.steps, ",") != "begin,lock,authorize,replay,state,insert,audit,commit,rollback") {
				t.Fatalf("unsafe ordering: %v", tx.steps)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*usageControlTx)
		want   error
	}{
		{"inactive", func(tx *usageControlTx) { tx.allowed = false }, operations.ErrForbidden},
		{"stale", func(tx *usageControlTx) { tx.now = tx.now.Add(time.Second) }, operations.ErrConflict},
		{"different-config", func(tx *usageControlTx) { tx.digest = strings.Repeat("b", 64) }, operations.ErrConflict},
		{"busy", func(tx *usageControlTx) { tx.active = true }, operations.ErrConflict},
		{"changed-replay", func(tx *usageControlTx) { tx.replay = true }, operations.ErrConflict},
		{"same-replay", func(tx *usageControlTx) { tx.replay = true; tx.matches = true }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := usageInput(now)
			tx := &usageControlTx{t: t, allowed: true, digest: input.ExpectedDigest, now: now, input: input}
			tc.change(tx)
			item, err := submitMediaUsageReview(context.Background(), tx.begin, input, input.IdempotencyKey, "review-test", now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("wrong guard result: %v", err)
			}
			if strings.Contains(strings.Join(tx.steps, ","), "insert") {
				t.Fatal("guard/replay created another request")
			}
			if tc.name == "same-replay" && !item.IdempotentReplay {
				t.Fatal("replay missing")
			}
		})
	}
}
