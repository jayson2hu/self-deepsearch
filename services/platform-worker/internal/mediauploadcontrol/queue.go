package mediauploadcontrol

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

var ErrNoWork = errors.New("media_upload_queue_no_work")
var ErrLeaseLost = errors.New("media_upload_queue_lease_lost")

type QueuedCommand struct {
	Command
	ActorID    string
	Dispatched bool
}

type Lease struct {
	Token   string
	Until   time.Time
	Command *QueuedCommand
}

type Outcome struct {
	Status  string
	Code    string
	Receipt *Receipt
}

// Implementations must fence every write with the lease token and both the
// caller/database clocks. No transaction/row lock may span a remote HTTP call.
type QueueRepository interface {
	Claim(context.Context, time.Time) (Lease, error)
	Observe(context.Context, Lease, Receipt, time.Time) error
	AuthorizeDispatch(context.Context, Lease, time.Time) (bool, error)
	Finish(context.Context, Lease, Outcome, time.Time) error
}

type ResumeGuard interface{ Allow(context.Context) error }

type QueueRunner struct {
	Repository  QueueRepository
	Client      controlClient
	ResumeGuard ResumeGuard
	Now         func() time.Time
	Logger      *slog.Logger
}

func (r *QueueRunner) clock() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *QueueRunner) Run(ctx context.Context) error {
	if r.Repository == nil || r.Client == nil {
		return ErrInvalid
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := r.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) && r.Logger != nil {
			r.Logger.Warn("media_upload_control_queue_unconfirmed")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *QueueRunner) RunOnce(ctx context.Context) error {
	if r.Repository == nil || r.Client == nil {
		return ErrInvalid
	}
	claimCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	lease, err := r.Repository.Claim(claimCtx, r.clock())
	cancel()
	if errors.Is(err, ErrNoWork) {
		return nil
	}
	if err != nil {
		return err
	}
	if lease.Token == "" || !r.clock().Before(lease.Until.Add(-time.Second)) {
		return ErrLeaseLost
	}
	work, stop := context.WithDeadline(ctx, lease.Until.Add(-time.Second))
	defer stop()
	finish := func(out Outcome) error {
		if work.Err() != nil || !r.clock().Before(lease.Until.Add(-time.Second)) {
			return ErrLeaseLost
		}
		bounded, cancel := context.WithTimeout(work, 2*time.Second)
		defer cancel()
		return r.Repository.Finish(bounded, lease, out, r.clock())
	}
	remote, err := r.Client.Status(work)
	if err != nil || !remote.valid(r.clock()) || remote.Status != "observed" {
		return finish(Outcome{Status: "uncertain", Code: "control_status_unavailable"})
	}
	bounded, cancel := context.WithTimeout(work, 2*time.Second)
	err = r.Repository.Observe(bounded, lease, remote, r.clock())
	cancel()
	if err != nil {
		return err
	}
	if lease.Command == nil {
		return finish(Outcome{})
	}
	command := *lease.Command
	issued, expires, timed := command.Window()
	if !timed || !command.valid() {
		return finish(Outcome{Status: "uncertain", Code: "control_command_invalid", Receipt: &remote})
	}
	if command.Dispatched && command.Matches(remote) {
		return finish(Outcome{Status: "applied", Receipt: &remote})
	}
	if remote.Epoch != command.ExpectedEpoch || remote.Generation != command.ExpectedGeneration {
		status := "rejected"
		if command.Dispatched {
			status = "superseded"
		}
		return finish(Outcome{Status: status, Code: "control_snapshot_changed", Receipt: &remote})
	}
	now := r.clock()
	if now.Before(issued) || !now.Before(expires) {
		if command.Dispatched && now.Before(expires.Add(310*time.Second)) {
			// A request authenticated by Beijing can be up to 300 seconds
			// behind our clock; add the bounded HTTP/read allowance. Do not
			// infer non-application merely from Japan's command expiry.
			return finish(Outcome{Status: "uncertain", Code: "control_expiry_confirmation_pending", Receipt: &remote})
		}
		return finish(Outcome{Status: "rejected", Code: "control_request_expired", Receipt: &remote})
	}
	if expires.Sub(now) < 8*time.Second || lease.Until.Sub(now) < 11*time.Second {
		return finish(Outcome{Status: "uncertain", Code: "control_dispatch_window_short", Receipt: &remote})
	}
	if command.Mode == "enabled" {
		if r.ResumeGuard == nil || r.ResumeGuard.Allow(work) != nil {
			status := "rejected"
			if command.Dispatched {
				status = "uncertain"
			}
			return finish(Outcome{Status: status, Code: "control_resume_guard_denied", Receipt: &remote})
		}
	}
	bounded, cancel = context.WithTimeout(work, 2*time.Second)
	allowed, err := r.Repository.AuthorizeDispatch(bounded, lease, r.clock())
	cancel()
	if err != nil {
		return err
	}
	if !allowed {
		status := "rejected"
		if command.Dispatched {
			status = "uncertain"
		}
		return finish(Outcome{Status: status, Code: "control_actor_or_window_invalid", Receipt: &remote})
	}
	// AuthorizeDispatch durably marks the attempt before any outbound apply.
	// Any later failure is uncertain, even when the HTTP call did not return.
	ack, err := r.Client.Apply(work, command.Command)
	if err != nil || !ack.valid(r.clock()) || !command.Matches(ack) || (ack.Status != "applied" && ack.Status != "replayed") {
		return finish(Outcome{Status: "uncertain", Code: "control_apply_unconfirmed", Receipt: &remote})
	}
	return finish(Outcome{Status: "applied", Receipt: &ack})
}
