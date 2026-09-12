package mediauploadcontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type memoryQueue struct {
	command    *QueuedCommand
	now        *time.Time
	out        Outcome
	steps      []string
	authorized bool
	fail       string
	leaseShort bool
}

func (q *memoryQueue) step(name string) error {
	q.steps = append(q.steps, name)
	if q.fail == name {
		return errors.New("synthetic SQL failure")
	}
	return nil
}
func (q *memoryQueue) Claim(context.Context, time.Time) (Lease, error) {
	err := q.step("claim")
	until := q.now.Add(30 * time.Second)
	if q.leaseShort {
		until = q.now.Add(time.Second)
	}
	return Lease{Token: testID, Until: until, Command: q.command}, err
}
func (q *memoryQueue) Observe(context.Context, Lease, Receipt, time.Time) error {
	return q.step("observe")
}
func (q *memoryQueue) AuthorizeDispatch(_ context.Context, _ Lease, _ time.Time) (bool, error) {
	if err := q.step("authorize"); err != nil {
		return false, err
	}
	if q.authorized {
		q.command.Dispatched = true
	}
	return q.authorized, nil
}
func (q *memoryQueue) Finish(_ context.Context, _ Lease, out Outcome, _ time.Time) error {
	if err := q.step("finish"); err != nil {
		return err
	}
	q.out = out
	return nil
}

type queueClient struct {
	remote                  Receipt
	statusErr, applyErr     error
	applies                 int
	afterStatus, afterApply func()
	last                    Command
}

func (c *queueClient) Status(context.Context) (Receipt, error) {
	if c.afterStatus != nil {
		c.afterStatus()
	}
	return c.remote, c.statusErr
}
func (c *queueClient) Apply(_ context.Context, command Command) (Receipt, error) {
	c.applies++
	c.last = command
	c.remote = ackFor(command)
	if c.afterApply != nil {
		c.afterApply()
	}
	ack := c.remote
	c.remote.Status = "observed"
	return ack, c.applyErr
}

type resumeGuardFunc func(context.Context) error

func (f resumeGuardFunc) Allow(ctx context.Context) error { return f(ctx) }
func queuedAt(now time.Time) *QueuedCommand {
	c := testCommand()
	c.IssuedAt = now.Add(-time.Minute).Format("2006-01-02T15:04:05Z")
	c.ExpiresAt = now.Add(4 * time.Minute).Format("2006-01-02T15:04:05Z")
	return &QueuedCommand{Command: c, ActorID: testID}
}
func setupQueue(t *testing.T) (*QueueRunner, *memoryQueue, *queueClient, *time.Time) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	q := &memoryQueue{command: queuedAt(now), now: &now, authorized: true}
	c := &queueClient{remote: Receipt{Status: "observed", Mode: "paused"}}
	r := &QueueRunner{Repository: q, Client: c, Now: func() time.Time { return now }}
	return r, q, c, &now
}

func TestQueueAppliesOnlyAfterDurableDispatchAndRecoversLostReply(t *testing.T) {
	r, q, c, _ := setupQueue(t)
	c.applyErr = ErrUncertain
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.out.Status != "uncertain" || !q.command.Dispatched || c.applies != 1 || strings.Join(q.steps, ",") != "claim,observe,authorize,finish" {
		t.Fatalf("incorrect initial outcome: %+v %v", q.out, q.steps)
	}
	c.applyErr = nil
	q.steps = nil
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.out.Status != "applied" || c.applies != 1 || strings.Join(q.steps, ",") != "claim,observe,finish" {
		t.Fatal("lost receipt caused another apply or was not reconciled")
	}
}

func TestQueueGuardsExpiryRoleResumeAndSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*QueueRunner, *memoryQueue, *queueClient, *time.Time)
		want   string
	}{
		{"pause", func(*QueueRunner, *memoryQueue, *queueClient, *time.Time) {}, "applied"},
		{"actor_revoked", func(_ *QueueRunner, q *memoryQueue, _ *queueClient, _ *time.Time) { q.authorized = false }, "rejected"},
		{"attempted_actor_revoked", func(_ *QueueRunner, q *memoryQueue, _ *queueClient, _ *time.Time) {
			q.authorized = false
			q.command.Dispatched = true
		}, "uncertain"},
		{"expired_unsent", func(_ *QueueRunner, q *memoryQueue, _ *queueClient, now *time.Time) {
			*now = now.Add(4 * time.Minute)
			q.command.Dispatched = false
		}, "rejected"},
		{"expired_attempt_waits_clock_skew", func(_ *QueueRunner, q *memoryQueue, _ *queueClient, now *time.Time) {
			q.command.Dispatched = true
			*now = now.Add(4 * time.Minute)
		}, "uncertain"},
		{"expired_attempt_confirmed_unchanged", func(_ *QueueRunner, q *memoryQueue, _ *queueClient, now *time.Time) {
			q.command.Dispatched = true
			*now = now.Add(4*time.Minute + 310*time.Second)
		}, "rejected"},
		{"remote_unknown", func(_ *QueueRunner, _ *memoryQueue, c *queueClient, _ *time.Time) { c.statusErr = ErrUncertain }, "uncertain"},
		{"stale_snapshot", func(_ *QueueRunner, _ *memoryQueue, c *queueClient, _ *time.Time) {
			c.remote = ackFor(testCommand())
			c.remote.Status = "observed"
		}, "rejected"},
		{"superseded_attempt", func(_ *QueueRunner, q *memoryQueue, c *queueClient, _ *time.Time) {
			q.command.Dispatched = true
			c.remote = ackFor(testCommand())
			c.remote.Generation = 2
			c.remote.CommandID = "20000000-0000-4000-8000-000000000002"
			c.remote.Status = "observed"
		}, "superseded"},
		{"unbounded_legacy_request", func(_ *QueueRunner, q *memoryQueue, _ *queueClient, _ *time.Time) {
			q.command.IssuedAt = ""
			q.command.ExpiresAt = ""
		}, "uncertain"},
		{"too_little_dispatch_time", func(_ *QueueRunner, q *memoryQueue, _ *queueClient, now *time.Time) {
			q.command.ExpiresAt = now.Add(7 * time.Second).Format("2006-01-02T15:04:05Z")
		}, "uncertain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, q, c, now := setupQueue(t)
			tc.change(r, q, c, now)
			if err := r.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if q.out.Status != tc.want {
				t.Fatalf("got %+v", q.out)
			}
			if tc.want != "applied" && c.applies != 0 {
				t.Fatal("unsafe apply sent")
			}
		})
	}
	for _, allowed := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume_denied", true: "resume_allowed"}[allowed], func(t *testing.T) {
			r, q, c, _ := setupQueue(t)
			q.command.ExpectedEpoch = testID
			q.command.ExpectedGeneration = 1
			q.command.Mode = "enabled"
			q.command.ResumeConfirmed = true
			q.command.ID = "20000000-0000-4000-8000-000000000002"
			c.remote = ackFor(testCommand())
			c.remote.Status = "observed"
			if allowed {
				r.ResumeGuard = resumeGuardFunc(func(context.Context) error { return nil })
			}
			if err := r.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if allowed && q.out.Status != "applied" || !allowed && q.out.Status != "rejected" {
				t.Fatalf("resume guard ignored: %+v", q.out)
			}
		})
	}
}

func TestQueueNeverCompletesWithLostLeaseOrFailedDatabaseWrite(t *testing.T) {
	for _, failure := range []string{"claim", "observe", "authorize", "finish"} {
		t.Run(failure, func(t *testing.T) {
			r, q, c, _ := setupQueue(t)
			q.fail = failure
			if err := r.RunOnce(context.Background()); err == nil || q.out.Status != "" {
				t.Fatal("failed SQL reported completion")
			}
			if failure != "finish" && c.applies != 0 {
				t.Fatal("apply before durable dispatch")
			}
		})
	}
	r, q, c, now := setupQueue(t)
	c.afterApply = func() { *now = now.Add(time.Minute) }
	if err := r.RunOnce(context.Background()); !errors.Is(err, ErrLeaseLost) || q.out.Status != "" {
		t.Fatal("expired lease wrote completion")
	}
	r, q, c, now = setupQueue(t)
	c.afterStatus = func() { *now = now.Add(time.Minute) }
	if err := r.RunOnce(context.Background()); !errors.Is(err, ErrLeaseLost) || c.applies != 0 {
		t.Fatal("expired status lease dispatched command")
	}
}
