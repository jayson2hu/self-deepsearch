package database

import (
	"errors"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/identity"
)

func TestInvitationPrecheckDoesNotConsumeOrDoubleCount(t *testing.T) {
	h := newInvitationContractHarness(t)
	now := contractNow()
	invitation := h.newInvitation(h.newEmail("precheck"), "user", now, now.Add(time.Hour))
	item := mustCreateInvitation(t, h, invitation)
	accept := h.newAccept(invitation.Email, invitation.CodeHash, now)
	for index := 0; index < 2; index++ {
		if err := h.store.CheckInvitation(h.ctx, accept.Email, accept.CodeHash, accept.IPHash, now); err != nil {
			t.Fatal(err)
		}
	}
	var untouched bool
	if err := h.store.pool.QueryRow(h.ctx, `SELECT status = 'pending' AND consumed_at IS NULL AND attempt_count = 0 FROM platform.user_invitations WHERE invitation_id = $1::uuid`, item.ID).Scan(&untouched); err != nil || !untouched {
		t.Fatal("precheck consumed or changed valid invitation")
	}
	var count int
	if err := h.store.pool.QueryRow(h.ctx, `SELECT count(*) FROM platform.security_rate_limits WHERE action = 'invitation_accept' AND dimension_hash = $1`, accept.IPHash).Scan(&count); err != nil || count != 0 {
		t.Fatal("precheck reserved quota")
	}
	if _, err := h.store.AcceptInvitation(h.ctx, accept); err != nil {
		t.Fatal(err)
	}
	if err := h.store.pool.QueryRow(h.ctx, `SELECT request_count FROM platform.security_rate_limits WHERE action = 'invitation_accept' AND dimension_hash = $1`, accept.IPHash).Scan(&count); err != nil || count != 1 {
		t.Fatal("acceptance quota double counted")
	}
	if err := h.store.CheckInvitation(h.ctx, accept.Email, accept.CodeHash, accept.IPHash, now); !errors.Is(err, identity.ErrInvalidChallenge) {
		t.Fatal("accepted invitation passed precheck")
	}
	if _, err := h.store.AcceptInvitation(h.ctx, accept); !errors.Is(err, identity.ErrInvalidChallenge) {
		t.Fatal("accepted invitation replayed")
	}
}

func TestInvitationPrecheckCountsWrongCodeAndLocks(t *testing.T) {
	h := newInvitationContractHarness(t)
	now := contractNow()
	invitation := h.newInvitation(h.newEmail("attempt-precheck"), "user", now, now.Add(time.Hour))
	item := mustCreateInvitation(t, h, invitation)
	accept := h.newAccept(invitation.Email, invitation.CodeHash, now)
	for index := 1; index <= 5; index++ {
		if err := h.store.CheckInvitation(h.ctx, accept.Email, h.newHash(), accept.IPHash, now); !errors.Is(err, identity.ErrInvalidChallenge) {
			t.Fatal("wrong code passed precheck")
		}
		var attempts int
		if err := h.store.pool.QueryRow(h.ctx, `SELECT attempt_count FROM platform.user_invitations WHERE invitation_id = $1::uuid`, item.ID).Scan(&attempts); err != nil || attempts != index {
			t.Fatal("precheck must count each wrong code exactly once")
		}
	}
	if err := h.store.CheckInvitation(h.ctx, accept.Email, accept.CodeHash, accept.IPHash, now); !errors.Is(err, identity.ErrInvalidChallenge) {
		t.Fatal("locked invitation passed precheck")
	}
	if _, err := h.store.AcceptInvitation(h.ctx, accept); !errors.Is(err, identity.ErrInvalidChallenge) {
		t.Fatal("final validation bypassed attempt lock")
	}
}

func TestInvitationFinalValidationRejectsPostPrecheckChanges(t *testing.T) {
	h := newInvitationContractHarness(t)
	for _, change := range []string{"expired", "revoked", "quota"} {
		t.Run(change, func(t *testing.T) {
			now := contractNow()
			invitation := h.newInvitation(h.newEmail(change), "user", now, now.Add(time.Minute))
			item := mustCreateInvitation(t, h, invitation)
			accept := h.newAccept(invitation.Email, invitation.CodeHash, now)
			if err := h.store.CheckInvitation(h.ctx, accept.Email, accept.CodeHash, accept.IPHash, now); err != nil {
				t.Fatal(err)
			}
			want := identity.ErrInvalidChallenge
			switch change {
			case "expired":
				accept.Now = invitation.ExpiresAt
			case "revoked":
				if _, err := h.store.RevokeInvitation(h.ctx, item.ID, h.actorID, "precheck contract revoke", invitation.RequestID, now); err != nil {
					t.Fatal(err)
				}
			case "quota":
				want = identity.ErrRateLimited
				if _, err := h.store.pool.Exec(h.ctx, `INSERT INTO platform.security_rate_limits (action, dimension_type, dimension_hash, window_start, window_seconds, request_count, success_count, expires_at) VALUES ('invitation_accept', 'ip_hash', $1, $2::timestamptz, 86400, 10, 0, $2::timestamptz + interval '1 day')`, accept.IPHash, now.Truncate(24*time.Hour)); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.store.CheckInvitation(h.ctx, accept.Email, accept.CodeHash, accept.IPHash, accept.Now); !errors.Is(err, want) {
				t.Fatalf("changed state passed precheck: %v", err)
			}
			if _, err := h.store.AcceptInvitation(h.ctx, accept); !errors.Is(err, want) {
				t.Fatalf("changed state passed final check: %v", err)
			}
			var count int
			if err := h.store.pool.QueryRow(h.ctx, `SELECT count(*) FROM platform.users WHERE normalized_email = $1`, accept.Email).Scan(&count); err != nil || count != 0 {
				t.Fatal("rejected invitation created an account")
			}
			if err := h.store.pool.QueryRow(h.ctx, `SELECT count(*) FROM platform.sessions WHERE token_hash = $1`, accept.SessionHash).Scan(&count); err != nil || count != 0 {
				t.Fatal("rejected invitation created a session")
			}
		})
	}
}
