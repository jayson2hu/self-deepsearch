package database

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"self-deepsearch/services/platform-api/internal/operations"
)

type uploadTx struct {
	pgx.Tx
	t                                         *testing.T
	steps                                     []string
	fail                                      string
	allowed, replay, matches, active, missing bool
	target                                    string
	receipt                                   []byte
	observed                                  *time.Time
	code                                      *string
	input                                     operations.MediaUploadInput
	now                                       time.Time
}

func (x *uploadTx) step(n string) error {
	x.steps = append(x.steps, n)
	if x.fail == n {
		return errors.New("synthetic transaction failure")
	}
	return nil
}
func (x *uploadTx) begin(_ context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	if o.IsoLevel != pgx.ReadCommitted {
		x.t.Fatal("wrong isolation")
	}
	return x, x.step("begin")
}
func (x *uploadTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	n := "lock"
	if strings.Contains(sql, "INSERT INTO audit.audit_logs") {
		n = "audit"
	}
	return pgconn.NewCommandTag("INSERT 1"), x.step(n)
}
func (x *uploadTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	return usageControlRow(func(d ...any) error {
		switch {
		case strings.Contains(sql, "lock_media_usage_reviewer"):
			if e := x.step("authorize"); e != nil {
				return e
			}
			*d[0].(*bool) = x.allowed
		case strings.Contains(sql, "idempotency_key=$2"):
			if e := x.step("replay"); e != nil {
				return e
			}
			if !x.replay {
				return pgx.ErrNoRows
			}
			*d[0].(*string) = x.input.IdempotencyKey
			*d[1].(*bool) = x.matches
		case strings.Contains(sql, "lock_media_upload_control_state"):
			if e := x.step("state"); e != nil {
				return e
			}
			if x.missing {
				return pgx.ErrNoRows
			}
			*d[0].(*string) = x.target
			*d[1].(*[]byte) = x.receipt
			*d[2].(**time.Time) = x.observed
			*d[3].(**string) = x.code
			*d[4].(*bool) = x.active
		default:
			n := "load_replay"
			if strings.Contains(sql, "INSERT INTO audit.media_upload_commands") {
				n = "insert"
				if len(args) != 11 || !args[9].(time.Time).Equal(x.now) || !args[10].(time.Time).Equal(x.now.Truncate(time.Second).Add(5*time.Minute)) {
					x.t.Fatal("wrong immutable validity window")
				}
			}
			if e := x.step(n); e != nil {
				return e
			}
			*d[0].(*string) = x.input.IdempotencyKey
			*d[1].(*string) = x.input.IdempotencyKey
			*d[2].(*string) = x.input.Mode
			*d[3].(*string) = x.input.Reason
			*d[4].(*string) = "pending"
			*d[5].(*time.Time) = x.now
			*d[6].(*time.Time) = x.now.Truncate(time.Second).Add(5 * time.Minute)
			*d[7].(**time.Time) = nil
			*d[8].(**time.Time) = nil
			*d[9].(*int) = 0
			*d[10].(**string) = nil
			*d[11].(*json.RawMessage) = nil
		}
		return nil
	})
}
func (x *uploadTx) Commit(context.Context) error   { return x.step("commit") }
func (x *uploadTx) Rollback(context.Context) error { return x.step("rollback") }
func uploadInput(now time.Time) operations.MediaUploadInput {
	g := int64(0)
	return operations.MediaUploadInput{IdempotencyKey: "90000000-0000-4000-8000-000000000001", TargetRef: strings.Repeat("a", 64), ExpectedGeneration: &g, ExpectedObservedAt: now, Mode: "paused", Reason: "人工暂停", Confirmed: true}
}
func newUploadTx(t *testing.T, now time.Time) *uploadTx {
	in := uploadInput(now)
	return &uploadTx{t: t, allowed: true, target: in.TargetRef, input: in, now: now, observed: &now, receipt: []byte(`{"status":"observed","epoch":"","generation":0,"mode":"paused","command_id":"","applied_at":""}`)}
}
func TestUploadTransactionRollbackAndCommitFailure(t *testing.T) {
	now := time.Date(2026, 9, 11, 0, 0, 0, 123456000, time.UTC)
	for _, fail := range []string{"", "begin", "lock", "authorize", "replay", "state", "insert", "audit", "commit"} {
		t.Run(fail, func(t *testing.T) {
			x := newUploadTx(t, now)
			x.fail = fail
			item, err := submitMediaUploadCommand(context.Background(), x.begin, x.input, x.input.IdempotencyKey, "test-upload", now)
			if (err != nil) != (fail != "") || (err != nil && item.ID != "") {
				t.Fatal("failed transaction reported a request", err)
			}
			if fail == "" && strings.Join(x.steps, ",") != "begin,lock,authorize,replay,state,insert,audit,commit,rollback" {
				t.Fatal("unsafe transaction ordering", x.steps)
			}
		})
	}
}
func TestUploadCASAndIdempotency(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, tc := range []struct {
		name   string
		change func(*uploadTx)
		want   error
	}{
		{"revoked", func(x *uploadTx) { x.allowed = false }, operations.ErrForbidden},
		{"absent", func(x *uploadTx) { x.missing = true }, operations.ErrNotFound},
		{"not_observed", func(x *uploadTx) { x.observed = nil }, operations.ErrConflict},
		{"future", func(x *uploadTx) { v := now.Add(time.Second); x.observed = &v; x.input.ExpectedObservedAt = v }, operations.ErrConflict},
		{"stale", func(x *uploadTx) { v := now.Add(-121 * time.Second); x.observed = &v; x.input.ExpectedObservedAt = v }, operations.ErrConflict},
		{"snapshot_changed", func(x *uploadTx) { v := now.Add(-time.Microsecond); x.observed = &v }, operations.ErrConflict},
		{"wrong_target", func(x *uploadTx) { x.target = strings.Repeat("b", 64) }, operations.ErrConflict},
		{"generation_changed", func(x *uploadTx) { x.receipt = []byte(`{"status":"observed","epoch":"","generation":1}`) }, operations.ErrConflict},
		{"invalid_receipt", func(x *uploadTx) { x.receipt = []byte(`{`) }, operations.ErrConflict},
		{"unhealthy", func(x *uploadTx) { s := "control_apply_unconfirmed"; x.code = &s }, operations.ErrConflict},
		{"live_lease", func(x *uploadTx) { x.active = true }, operations.ErrConflict},
		{"same_replay", func(x *uploadTx) { x.replay = true; x.matches = true; x.missing = true }, nil},
		{"changed_replay", func(x *uploadTx) { x.replay = true }, operations.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newUploadTx(t, now)
			tc.change(x)
			item, err := submitMediaUploadCommand(context.Background(), x.begin, x.input, x.input.IdempotencyKey, "test-upload", now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("wrong guard outcome %v", err)
			}
			if strings.Contains(strings.Join(x.steps, ","), "insert") {
				t.Fatal("guard or replay inserted another request")
			}
			if tc.name == "same_replay" && (!item.IdempotentReplay || strings.Contains(strings.Join(x.steps, ","), "state")) {
				t.Fatal("replay consulted changed live state")
			}
		})
	}
	x := newUploadTx(t, now)
	x.input.Confirmed = false
	if _, err := submitMediaUploadCommand(context.Background(), x.begin, x.input, "", "", now); !errors.Is(err, operations.ErrInvalidInput) || len(x.steps) > 0 {
		t.Fatal("invalid input opened a transaction")
	}
}
