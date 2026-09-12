package database

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/identity"
)

func validIdentityContractURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "postgres" && parsed.Hostname() == "127.0.0.1" && parsed.Port() != "" &&
		parsed.Path == "/self_deepsearch_migrate_test" && parsed.User != nil && parsed.User.Username() == "platform_api_login" &&
		(parsed.RawQuery == "" || parsed.RawQuery == "sslmode=disable") && !parsed.ForceQuery && parsed.Fragment == "" && !strings.Contains(value, "#")
}

func TestIdentityContractDatabaseGuard(t *testing.T) {
	valid := "postgres://platform_api_login:test-password@127.0.0.1:5432/self_deepsearch_migrate_test"
	if !validIdentityContractURL(valid) {
		t.Fatal("restricted disposable database rejected")
	}
	for _, value := range []string{
		strings.Replace(valid, "platform_api_login", "platform", 1),
		strings.Replace(valid, "127.0.0.1", "example.test", 1),
		strings.Replace(valid, "self_deepsearch_migrate_test", "production", 1),
		valid + "?host=example.test", valid + "?", valid + "#",
	} {
		if validIdentityContractURL(value) {
			t.Fatal("unsafe identity contract URL accepted")
		}
	}
}

func TestPostgresIdentityLifecycleContract(t *testing.T) {
	databaseURL := os.Getenv("PLATFORM_API_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PLATFORM_API_TEST_DATABASE_URL is not set")
	}
	if !validIdentityContractURL(databaseURL) {
		t.Fatal("identity lifecycle contract requires the restricted loopback migration test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal("cannot configure identity contract database")
	}
	defer store.Close()
	var version int
	if err := store.pool.QueryRow(ctx, `SELECT schema_version FROM platform.system_metadata WHERE singleton`).Scan(&version); err != nil || version != 21 {
		t.Fatal("identity contract requires migrated schema 21")
	}
	var restricted bool
	if err := store.pool.QueryRow(ctx, `SELECT current_user = 'platform_api_login' AND NOT rolsuper AND NOT rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&restricted); err != nil || !restricted {
		t.Fatal("identity contract must use the restricted API login")
	}
	newHash := func(t *testing.T) string {
		t.Helper()
		_, hash, err := identity.NewSessionToken()
		if err != nil {
			t.Fatal("cannot generate synthetic token hash")
		}
		return hash
	}
	newUser := func(t *testing.T, email string) identity.User {
		t.Helper()
		if email == "" {
			email = "identity-contract-" + newHash(t)[:24] + "@example.test"
		}
		user := identity.User{Email: email, PasswordHash: strings.Repeat("p", 32), Role: "user", AccountStatus: "active"}
		if err := store.pool.QueryRow(ctx, `INSERT INTO platform.users (normalized_email, password_hash, role, account_status, email_verified_at) VALUES ($1, $2, 'user', 'active', now()) RETURNING user_id::text, email_verified_at`, user.Email, user.PasswordHash).Scan(&user.ID, &user.EmailVerifiedAt); err != nil {
			t.Fatal("cannot insert synthetic user")
		}
		// Keep synthetic rows and append-only audit evidence in this disposable DB.
		// The runtime role must not gain DELETE privileges or disable audit triggers for tests.
		return user
	}
	challenge := func(t *testing.T, user identity.User, purpose string) string {
		t.Helper()
		hash := newHash(t)
		if _, err := store.pool.Exec(ctx, `INSERT INTO platform.email_challenges (user_id, normalized_email, purpose, code_hash, sent_at, expires_at) VALUES ($1::uuid, $2, $3, $4, now() - interval '1 minute', now() + interval '10 minutes')`, user.ID, user.Email, purpose, hash); err != nil {
			t.Fatal("cannot insert synthetic challenge")
		}
		return hash
	}
	session := func(t *testing.T, user identity.User) identity.SessionRequest {
		t.Helper()
		now := time.Now().UTC()
		return identity.SessionRequest{
			UserID: user.ID, ExpectedEmail: user.Email, ExpectedPasswordHash: user.PasswordHash, ExpectedRole: user.Role,
			TokenHash: newHash(t), CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		}
	}
	assertNoSession := func(t *testing.T, hash string) {
		t.Helper()
		var count int
		if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM platform.sessions WHERE token_hash = $1`, hash).Scan(&count); err != nil || count != 0 {
			t.Fatal("rejected login left a session row")
		}
	}

	t.Run("reset_and_close_revoke_sessions_and_reject_stale_issuance", func(t *testing.T) {
		user := newUser(t, "")
		request := session(t, user)
		if err := store.CreateSession(ctx, request); err != nil {
			t.Fatal(err)
		}
		if _, err := store.UserBySession(ctx, request.TokenHash, time.Now()); err != nil {
			t.Fatal("new session is not usable")
		}
		resetHash := challenge(t, user, identity.PurposePasswordReset)
		reset := identity.PasswordResetRequest{Email: user.Email, CodeHash: resetHash, PasswordHash: strings.Repeat("n", 32), Now: request.CreatedAt.Add(-time.Second)}
		// The reset request began before this concurrent login was issued.
		if err := store.ResetPassword(ctx, reset); err != nil {
			t.Fatalf("reset failed for a session created after request start: %v", err)
		}
		if _, err := store.UserBySession(ctx, request.TokenHash, time.Now()); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatal("reset did not revoke the prior session")
		}
		request.TokenHash = newHash(t)
		if err := store.CreateSession(ctx, request); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatal("old password snapshot issued a session after reset")
		}
		assertNoSession(t, request.TokenHash)
		if err := store.ResetPassword(ctx, reset); !errors.Is(err, identity.ErrInvalidChallenge) {
			t.Fatal("consumed reset code was replayed")
		}
		user.PasswordHash = reset.PasswordHash
		current := session(t, user)
		if err := store.CreateSession(ctx, current); err != nil {
			t.Fatal("new credentials failed to issue a session")
		}
		closeHash := challenge(t, user, identity.PurposeAccountClose)
		if err := store.CloseAccount(ctx, identity.CloseAccountRequest{UserID: user.ID, CodeHash: closeHash, NoticeVersion: "2026-08-07", RequestID: "identity-contract-close", Now: current.CreatedAt.Add(-time.Second)}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.UserBySession(ctx, current.TokenHash, time.Now()); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatal("closed account session remains usable")
		}
		current.TokenHash = newHash(t)
		if err := store.CreateSession(ctx, current); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatal("closed account issued a new session")
		}
		assertNoSession(t, current.TokenHash)
	})

	t.Run("logout_is_idempotent_and_preserves_revocation_evidence", func(t *testing.T) {
		user := newUser(t, "")
		request := session(t, user)
		if err := store.CreateSession(ctx, request); err != nil {
			t.Fatal(err)
		}
		if err := store.RevokeSession(ctx, newHash(t), "logout", time.Now()); err != nil {
			t.Fatal("absent token cannot be logged out idempotently")
		}
		if _, err := store.UserBySession(ctx, request.TokenHash, time.Now()); err != nil {
			t.Fatal("logging out an absent token changed another session")
		}
		if err := store.RevokeSession(ctx, request.TokenHash, "logout", request.CreatedAt.Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
		var revokedAt time.Time
		if err := store.pool.QueryRow(ctx, `SELECT revoked_at FROM platform.sessions WHERE token_hash = $1 AND revoked_at = created_at AND revoke_reason = 'logout'`, request.TokenHash).Scan(&revokedAt); err != nil {
			t.Fatal("logout failed to preserve the timestamp constraint")
		}
		if err := store.RevokeSession(ctx, request.TokenHash, "repeat-must-not-overwrite", time.Now().Add(time.Second)); err != nil {
			t.Fatal("repeat logout failed")
		}
		var unchanged bool
		if err := store.pool.QueryRow(ctx, `SELECT revoked_at = $2 AND revoke_reason = 'logout' FROM platform.sessions WHERE token_hash = $1`, request.TokenHash, revokedAt).Scan(&unchanged); err != nil || !unchanged {
			t.Fatal("repeat logout changed the original revocation evidence")
		}
		if _, err := store.UserBySession(ctx, request.TokenHash, time.Now()); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatal("revoked token remains usable")
		}
	})

	t.Run("changed_account_snapshot_is_rejected", func(t *testing.T) {
		for _, mutation := range []string{
			`role = 'admin'`, `normalized_email = 'changed-' || normalized_email`,
			`account_status = 'locked'`, `account_status = 'suspended'`, `account_status = 'pending', email_verified_at = NULL`,
		} {
			user := newUser(t, "")
			request := session(t, user)
			// Only fixed, test-owned SQL fragments above are used here.
			if _, err := store.pool.Exec(ctx, `UPDATE platform.users SET `+mutation+` WHERE user_id = $1::uuid`, user.ID); err != nil {
				t.Fatal(err)
			}
			if err := store.CreateSession(ctx, request); !errors.Is(err, identity.ErrInvalidCredentials) {
				t.Fatal("changed account snapshot accepted")
			}
			assertNoSession(t, request.TokenHash)
		}
	})

	t.Run("old_account_reset_code_cannot_cross_email_reuse", func(t *testing.T) {
		old := newUser(t, "")
		oldResetHash := challenge(t, old, identity.PurposePasswordReset)
		closeHash := challenge(t, old, identity.PurposeAccountClose)
		if err := store.CloseAccount(ctx, identity.CloseAccountRequest{UserID: old.ID, CodeHash: closeHash, NoticeVersion: "2026-08-07", RequestID: "identity-contract-reuse", Now: time.Now()}); err != nil {
			t.Fatal(err)
		}
		current := newUser(t, old.Email)
		if valid, err := store.CheckChallenge(ctx, current.Email, identity.PurposePasswordReset, oldResetHash, nil, time.Now()); err != nil || valid {
			t.Fatal("old identity code passed the pre-hash check")
		}
		if err := store.ResetPassword(ctx, identity.PasswordResetRequest{Email: current.Email, CodeHash: oldResetHash, PasswordHash: strings.Repeat("x", 32), Now: time.Now()}); !errors.Is(err, identity.ErrInvalidChallenge) {
			t.Fatal("old identity code changed the new account password")
		}
		var hash string
		if err := store.pool.QueryRow(ctx, `SELECT password_hash FROM platform.users WHERE user_id = $1::uuid`, current.ID).Scan(&hash); err != nil || hash != current.PasswordHash {
			t.Fatal("new account password was modified")
		}
	})

	t.Run("concurrent_password_change_is_rechecked_after_row_lock", func(t *testing.T) {
		user := newUser(t, "")
		request := session(t, user)
		transaction, err := store.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer transaction.Rollback(context.Background())
		if _, err := transaction.Exec(ctx, `UPDATE platform.users SET password_hash = $2 WHERE user_id = $1::uuid`, user.ID, strings.Repeat("r", 32)); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() { result <- store.CreateSession(ctx, request) }()
		blocked := false
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := store.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND usename = current_user AND wait_event_type = 'Lock' AND query LIKE '%INSERT INTO platform.sessions%')`).Scan(&blocked); err != nil {
				t.Fatal(err)
			}
			if blocked {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !blocked {
			t.Fatal("session issuance did not lock the account row")
		}
		if err := transaction.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-result; !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatal("concurrent stale password snapshot issued a session")
		}
		assertNoSession(t, request.TokenHash)
	})
}
