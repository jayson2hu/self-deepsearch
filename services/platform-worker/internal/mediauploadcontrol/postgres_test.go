package mediauploadcontrol

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type queueRow func(...any) error

func (f queueRow) Scan(d ...any) error { return f(d...) }

type queueTx struct {
	pgx.Tx
	t                                *testing.T
	steps                            []string
	fail, target                     string
	now                              time.Time
	command                          *QueuedCommand
	lost, notDue, denied, zeroUpdate bool
	options                          pgx.TxOptions
}

func (x *queueTx) step(n string) error {
	x.steps = append(x.steps, n)
	if x.fail == n {
		return errors.New("synthetic SQL detail")
	}
	return nil
}
func (x *queueTx) begin(_ context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	x.options = o
	return x, x.step("begin")
}
func (x *queueTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	n := "lock"
	switch {
	case strings.Contains(sql, "INSERT INTO platform.media_upload_control_state"):
		n = "initialize"
		if !strings.Contains(sql, "least($2,clock_timestamp())") {
			x.t.Fatal("ahead app clock must not prevent initial poll")
		}
	case strings.Contains(sql, "last_receipt=$2"):
		n = "observe"
		if !strings.Contains(sql, "updated_at<=$3") {
			x.t.Fatal("observation needs monotonic clock")
		}
	case strings.Contains(sql, "status='running'"):
		n = "dispatch"
		if !strings.Contains(sql, "expires_at>clock_timestamp()+interval '8 seconds'") {
			x.t.Fatal("dispatch needs database expiry guard")
		}
	case strings.Contains(sql, "UPDATE audit.media_upload_commands"):
		n = "outcome"
	case strings.Contains(sql, "lease_token=NULL"):
		n = "release"
	}
	count := "UPDATE 1"
	if x.zeroUpdate && n != "lock" && n != "initialize" {
		count = "UPDATE 0"
	}
	return pgconn.NewCommandTag(count), x.step(n)
}
func (x *queueTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	return queueRow(func(d ...any) error {
		switch {
		case strings.Contains(sql, "SELECT target_ref FROM"):
			if e := x.step("target"); e != nil {
				return e
			}
			*d[0].(*string) = x.target
		case strings.Contains(sql, "RETURNING lease_token"):
			if e := x.step("claim"); e != nil {
				return e
			}
			if x.notDue {
				return pgx.ErrNoRows
			}
			if !strings.Contains(sql, "lease_until<=clock_timestamp()") || !strings.Contains(sql, "next_poll_at<=clock_timestamp()") {
				x.t.Fatal("claim missing database fencing")
			}
			*d[0].(*string) = testID
			*d[1].(*time.Time) = x.now.Add(30 * time.Second)
		case strings.Contains(sql, "SELECT command_id"):
			if e := x.step("command"); e != nil {
				return e
			}
			if x.command == nil {
				return pgx.ErrNoRows
			}
			c := x.command
			*d[0].(*string) = c.ID
			*d[1].(*string) = c.ActorID
			*d[2].(*string) = c.ExpectedEpoch
			*d[3].(*int64) = c.ExpectedGeneration
			*d[4].(*string) = c.Mode
			*d[5].(*string) = c.Reason
			a, b, _ := c.Window()
			*d[6].(*time.Time) = a
			*d[7].(*time.Time) = b
			*d[8].(*bool) = c.Dispatched
		case strings.Contains(sql, "SELECT true FROM"):
			if e := x.step("fence"); e != nil {
				return e
			}
			if x.lost {
				return pgx.ErrNoRows
			}
			if !strings.Contains(sql, "lease_token=$2::uuid AND lease_until=$3 AND lease_until>$4 AND lease_until>clock_timestamp() FOR UPDATE") || len(args) != 4 {
				x.t.Fatal("write lacks exact lease and two-clock fence")
			}
			*d[0].(*bool) = true
		case strings.Contains(sql, "lock_media_usage_reviewer"):
			if e := x.step("authorize"); e != nil {
				return e
			}
			*d[0].(*bool) = !x.denied
		case strings.Contains(sql, "SELECT last_success_at,last_error_code"):
			if e := x.step("metrics"); e != nil {
				return e
			}
			*d[0].(**time.Time) = &x.now
			*d[1].(**string) = nil
			*d[2].(*int) = 1
			*d[3].(*int) = 1
		default:
			x.t.Fatal("unexpected SQL", sql)
		}
		return nil
	})
}
func (x *queueTx) Commit(context.Context) error   { return x.step("commit") }
func (x *queueTx) Rollback(context.Context) error { return x.step("rollback") }
func queueRepo(t *testing.T) (*PostgresQueue, *queueTx, Lease) {
	now := time.Now().UTC().Truncate(time.Second)
	x := &queueTx{t: t, target: strings.Repeat("a", 64), now: now, command: queuedAt(now)}
	return &PostgresQueue{begin: x.begin, target: x.target}, x, Lease{Token: testID, Until: now.Add(30 * time.Second), Command: x.command}
}
func TestPostgresQueueTransactionBoundaries(t *testing.T) {
	for _, op := range []string{"claim", "observe", "dispatch", "finish"} {
		t.Run(op, func(t *testing.T) {
			for _, fail := range []string{"", "begin", "lock", "commit"} {
				t.Run(fail, func(t *testing.T) {
					q, x, l := queueRepo(t)
					x.fail = fail
					var err error
					switch op {
					case "claim":
						var got Lease
						got, err = q.Claim(context.Background(), x.now)
						if err == nil && (got.Command == nil || got.Command.IssuedAt != l.Command.IssuedAt || got.Token == "") {
							t.Fatal("lost immutable command")
						}
					case "observe":
						err = q.Observe(context.Background(), l, Receipt{Status: "observed", Mode: "paused"}, x.now)
					case "dispatch":
						var allowed bool
						allowed, err = q.AuthorizeDispatch(context.Background(), l, x.now)
						if err == nil && !allowed {
							t.Fatal("dispatch not durably authorized")
						}
					case "finish":
						ack := ackFor(l.Command.Command)
						err = q.Finish(context.Background(), l, Outcome{Status: "applied", Receipt: &ack}, x.now)
					}
					if (err != nil) != (fail != "") {
						t.Fatal("wrong failure result", err)
					}
					if fail == "" {
						want := map[string]string{"claim": "begin,lock,initialize,target,claim,command,commit,rollback", "observe": "begin,lock,fence,observe,commit,rollback", "dispatch": "begin,lock,fence,authorize,dispatch,commit,rollback", "finish": "begin,lock,fence,outcome,observe,release,commit,rollback"}[op]
						if strings.Join(x.steps, ",") != want {
							t.Fatal("unsafe SQL ordering", x.steps)
						}
					}
				})
			}
		})
	}
}
func TestPostgresQueueRefusesLeaseLossAndFailedEvidence(t *testing.T) {
	for _, fail := range []string{"fence", "outcome", "observe", "release"} {
		q, x, l := queueRepo(t)
		x.fail = fail
		ack := ackFor(l.Command.Command)
		if err := q.Finish(context.Background(), l, Outcome{Status: "applied", Receipt: &ack}, x.now); err == nil || strings.Contains(strings.Join(x.steps, ","), "commit") {
			t.Fatal("partial applied transaction committed")
		}
	}
	q, x, l := queueRepo(t)
	x.lost = true
	if err := q.Observe(context.Background(), l, Receipt{Status: "observed", Mode: "paused"}, x.now); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("old observation accepted", err)
	}
	if ok, err := q.AuthorizeDispatch(context.Background(), l, x.now); ok || !errors.Is(err, ErrLeaseLost) {
		t.Fatal("old dispatch accepted", err)
	}
	if err := q.Finish(context.Background(), l, Outcome{Status: "uncertain", Code: "control_apply_unconfirmed"}, x.now); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("old completion accepted", err)
	}
	q, x, l = queueRepo(t)
	x.denied = true
	if ok, err := q.AuthorizeDispatch(context.Background(), l, x.now); ok || err != nil || strings.Contains(strings.Join(x.steps, ","), "dispatch") {
		t.Fatal("revoked actor dispatched", err)
	}
	q, x, l = queueRepo(t)
	x.zeroUpdate = true
	if err := q.Observe(context.Background(), l, Receipt{Status: "observed", Mode: "paused"}, x.now); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("zero row write accepted", err)
	}
	for _, out := range []Outcome{{Status: "pending"}, {Status: "applied"}, {Status: "applied", Receipt: &Receipt{Status: "observed", Mode: "paused"}}} {
		q, x, l = queueRepo(t)
		if err := q.Finish(context.Background(), l, out, x.now); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid outcome accepted", err)
		}
	}
	q, x, _ = queueRepo(t)
	x.notDue = true
	if _, err := q.Claim(context.Background(), x.now); !errors.Is(err, ErrNoWork) {
		t.Fatal("busy lease stolen")
	}
	q, x, _ = queueRepo(t)
	x.target = strings.Repeat("b", 64)
	if _, err := q.Claim(context.Background(), x.now); !errors.Is(err, ErrInvalid) {
		t.Fatal("target replaced")
	}
	q, x, _ = queueRepo(t)
	x.command = nil
	if got, err := q.Claim(context.Background(), x.now); err != nil || got.Command != nil {
		t.Fatal("idle observation requires no command", err)
	}
}
func TestUploadQueueMetricsBoundedAndFailClosed(t *testing.T) {
	for _, fail := range []string{"", "begin", "metrics", "commit"} {
		q, x, _ := queueRepo(t)
		x.fail = fail
		var b bytes.Buffer
		q.WriteMetrics(context.Background(), &b, x.now)
		want := "1"
		if fail != "" {
			want = "0"
		}
		if !strings.Contains(b.String(), "self_deepsearch_media_upload_queue_state_readable "+want) || strings.Contains(b.String(), "synthetic") || strings.Contains(b.String(), x.target) {
			t.Fatal("unsafe metrics", b.String())
		}
		if x.options.AccessMode != pgx.ReadOnly {
			t.Fatal("metrics must be read only")
		}
	}
}
