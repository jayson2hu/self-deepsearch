package database

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/operations"
)

type roleContractEmail struct{}

func (roleContractEmail) SendCode(context.Context, string, string, string) error { return nil }

// Reuse the restricted, randomized, append-only disposable identity fixtures.
// No production database URL or privileged cleanup is permitted.
func TestPostgresRoleChangeRevocationContract(t *testing.T) {
	h := newInvitationContractHarness(t)
	email := h.newEmail("roles")
	id := h.insertUser(t, email, "user", "active")
	password := "synthetic-role-contract-password"
	hash, err := identity.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.pool.Exec(h.ctx, `UPDATE platform.users SET password_hash = $2 WHERE user_id = $1::uuid`, id, hash); err != nil {
		t.Fatal(err)
	}
	service, err := identity.NewService(h.store, roleContractEmail{}, strings.Repeat("role-contract-only-", 2), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	var ownerEmail string
	if err := h.store.pool.QueryRow(h.ctx, `SELECT normalized_email FROM platform.users WHERE user_id = $1::uuid`, h.actorID).Scan(&ownerEmail); err != nil {
		t.Fatal(err)
	}
	ownerSnapshot, err := h.store.ActiveUserByEmail(h.ctx, ownerEmail)
	if err != nil {
		t.Fatal(err)
	}
	ownerSession := roleContractSession(ownerSnapshot, h.newHash(), contractNow(), 8*time.Hour)
	if err := h.store.CreateSession(h.ctx, ownerSession); err != nil {
		t.Fatal(err)
	}

	for _, nextRole := range []string{"editor", "admin", "user"} {
		t.Run("to_"+nextRole, func(t *testing.T) {
			user, token, err := service.Login(h.ctx, email, password, h.prefix, h.newHash())
			if err != nil {
				t.Fatal(err)
			}
			previous := roleContractSession(user, h.newHash(), contractNow(), time.Hour)
			if err := h.store.CreateSession(h.ctx, previous); err != nil {
				t.Fatal(err)
			}
			if err := h.store.RevokeSession(h.ctx, previous.TokenHash, "logout", previous.CreatedAt); err != nil {
				t.Fatal(err)
			}
			// A request can begin before a concurrent session is created.
			now := contractNow()
			future := roleContractSession(user, h.newHash(), now.Add(time.Second), 30*24*time.Hour)
			if err := h.store.CreateSession(h.ctx, future); err != nil {
				t.Fatal(err)
			}
			_, err = service.Reauthenticate(h.ctx, token, password, h.prefix, h.newHash())
			if err != nil {
				t.Fatal(err)
			}
			requestID := h.newHash()
			updated, err := h.store.ChangeUserRole(h.ctx, id, nextRole, h.actorID, requestID, now)
			if err != nil || updated.Role != nextRole {
				t.Fatalf("change role: %v", err)
			}
			if _, err := service.Authenticate(h.ctx, token); !errors.Is(err, identity.ErrUnauthenticated) {
				t.Fatal("old session survived role change")
			}
			if _, err := service.Reauthenticate(h.ctx, token, password, h.prefix, h.newHash()); !errors.Is(err, identity.ErrUnauthenticated) {
				t.Fatal("old session renewed recent-auth after role change")
			}
			var reason string
			var revokedAt time.Time
			if err := h.store.pool.QueryRow(h.ctx, `SELECT revoke_reason, revoked_at FROM platform.sessions WHERE token_hash = $1`, future.TokenHash).Scan(&reason, &revokedAt); err != nil || reason != "role_change" || !revokedAt.Equal(future.CreatedAt) {
				t.Fatal("revocation lost reason or violated creation-time boundary")
			}
			if err := h.store.pool.QueryRow(h.ctx, `SELECT revoke_reason, revoked_at FROM platform.sessions WHERE token_hash = $1`, previous.TokenHash).Scan(&reason, &revokedAt); err != nil || reason != "logout" || !revokedAt.Equal(previous.CreatedAt) {
				t.Fatal("role change overwrote earlier revocation evidence")
			}
			if _, err := h.store.UserBySession(h.ctx, ownerSession.TokenHash, contractNow()); err != nil {
				t.Fatal("role change revoked operator session")
			}
			stale := roleContractSession(user, h.newHash(), contractNow(), 30*24*time.Hour)
			if err := h.store.CreateSession(h.ctx, stale); !errors.Is(err, identity.ErrInvalidCredentials) {
				t.Fatal("stale role snapshot issued a session")
			}
			fresh, freshToken, err := service.Login(h.ctx, email, password, h.prefix, h.newHash())
			if err != nil || fresh.Role != nextRole {
				t.Fatalf("new-role login failed: %v", err)
			}
			var lifetime float64
			if err := h.store.pool.QueryRow(h.ctx, `SELECT extract(epoch FROM (expires_at - created_at)) FROM platform.sessions WHERE token_hash = $1`, identity.SessionTokenHash(freshToken)).Scan(&lifetime); err != nil {
				t.Fatal(err)
			}
			want := 8 * time.Hour
			if nextRole == "user" {
				want = 30 * 24 * time.Hour
			}
			if lifetime != want.Seconds() {
				t.Fatal("new session has wrong role lifetime")
			}
			if _, err := h.store.ChangeUserRole(h.ctx, id, nextRole, h.actorID, requestID, now); !errors.Is(err, operations.ErrConflict) {
				t.Fatal("unchanged role must be a conflict")
			}
			if _, err := service.Authenticate(h.ctx, freshToken); err != nil {
				t.Fatal("same-role retry revoked fresh session")
			}
			var auditCount int
			if err := h.store.pool.QueryRow(h.ctx, `SELECT count(*) FROM audit.audit_logs WHERE action = 'user.role_change' AND object_id = $1::uuid AND request_id = $2`, id, requestID).Scan(&auditCount); err != nil || auditCount != 1 {
				t.Fatal("role change did not record exactly one audit")
			}
		})
	}
}

func roleContractSession(user identity.User, hash string, now time.Time, duration time.Duration) identity.SessionRequest {
	return identity.SessionRequest{UserID: user.ID, ExpectedEmail: user.Email, ExpectedPasswordHash: user.PasswordHash, ExpectedRole: user.Role, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(duration)}
}

type pausedRoleCommit struct {
	pgx.Tx
	ready   chan struct{}
	release chan struct{}
}

func (tx *pausedRoleCommit) Commit(ctx context.Context) error {
	close(tx.ready)
	select {
	case <-tx.release:
		return tx.Tx.Commit(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPostgresRoleChangeBlocksStaleConcurrentLogin(t *testing.T) {
	h := newInvitationContractHarness(t)
	email := h.newEmail("role-lock")
	h.insertUser(t, email, "user", "active")
	user, err := h.store.ActiveUserByEmail(h.ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	realTx, err := h.store.pool.Begin(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	paused := &pausedRoleCommit{Tx: realTx, ready: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	defer once.Do(func() { close(paused.release) })
	changeResult := make(chan error, 1)
	go func() {
		_, err := changeUserRole(h.ctx, func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return paused, nil }, user.ID, "editor", h.actorID, h.newHash(), contractNow())
		changeResult <- err
	}()
	select {
	case <-paused.ready:
	case err := <-changeResult:
		t.Fatalf("role mutation failed before commit: %v", err)
	case <-h.ctx.Done():
		t.Fatal("role mutation timed out")
	}
	request := roleContractSession(user, h.newHash(), contractNow(), 30*24*time.Hour)
	loginResult := make(chan error, 1)
	go func() { loginResult <- h.store.CreateSession(h.ctx, request) }()
	blocked := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := h.store.pool.QueryRow(h.ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND usename = current_user AND wait_event_type = 'Lock' AND query LIKE '%INSERT INTO platform.sessions%')`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("stale login did not wait on role-change account lock")
	}
	once.Do(func() { close(paused.release) })
	select {
	case err := <-changeResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-h.ctx.Done():
		t.Fatal("role commit timed out")
	}
	select {
	case err := <-loginResult:
		if !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatal("stale login survived committed role change")
		}
	case <-h.ctx.Done():
		t.Fatal("stale login timed out")
	}
	var count int
	if err := h.store.pool.QueryRow(h.ctx, `SELECT count(*) FROM platform.sessions WHERE token_hash = $1`, request.TokenHash).Scan(&count); err != nil || count != 0 {
		t.Fatal("stale login left a token row")
	}
}
