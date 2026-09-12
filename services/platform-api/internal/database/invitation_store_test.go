package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"self-deepsearch/services/platform-api/internal/identity"
)

// These tests exercise the invitation repository against a migrated PostgreSQL
// database. They are deliberately opt-in so the regular unit-test suite does
// not require Docker or a local database.
type invitationContractHarness struct {
	prefix        string
	store         *Store
	ctx           context.Context
	actorID       string
	userIDs       []string
	invitationIDs []string
	rateHashes    []string
	requestIDs    []string
	sequence      uint64
}

var invitationTestSequence uint64

func newInvitationContractHarness(t *testing.T) *invitationContractHarness {
	t.Helper()
	databaseURL := os.Getenv("PLATFORM_API_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PLATFORM_API_TEST_DATABASE_URL is not set")
	}
	if !validIdentityContractURL(databaseURL) {
		t.Fatal("invitation contract requires the restricted loopback migration test database")
	}

	openContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := Open(openContext, databaseURL)
	if err != nil {
		cancel()
		t.Fatalf("open invitation contract database: %v", err)
	}
	var schemaVersion int
	err = store.pool.QueryRow(openContext, `SELECT schema_version FROM platform.system_metadata WHERE singleton`).Scan(&schemaVersion)
	if err != nil {
		store.Close()
		t.Fatalf("read invitation contract schema version: %v", err)
	}
	if schemaVersion != 21 {
		store.Close()
		t.Fatal("invitation contract requires migrated schema 21")
	}
	var restricted bool
	if err := store.pool.QueryRow(openContext, `SELECT current_user = 'platform_api_login' AND NOT rolsuper AND NOT rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&restricted); err != nil || !restricted {
		store.Close()
		t.Fatal("invitation contract must use the restricted API login")
	}
	_, prefix, err := identity.NewSessionToken()
	if err != nil {
		store.Close()
		t.Fatal("cannot generate invitation fixture namespace")
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Minute)
	// Retain synthetic rows and append-only audit evidence in this disposable
	// database. Never grant DELETE or disable audit triggers for cleanup.
	harness := &invitationContractHarness{store: store, ctx: ctx, prefix: prefix[:24]}
	t.Cleanup(func() { stop(); store.Close() })
	harness.actorID = harness.insertUser(t, harness.newEmail("actor"), "owner", "active")
	return harness
}

func (harness *invitationContractHarness) newEmail(label string) string {
	sequence := atomic.AddUint64(&invitationTestSequence, 1)
	return fmt.Sprintf("invite-%s-%s-%d@example.test", harness.prefix, label, sequence)
}

func (harness *invitationContractHarness) newHash() string {
	harness.sequence++
	sequence := atomic.AddUint64(&invitationTestSequence, 1)
	return identity.SessionTokenHash(fmt.Sprintf("%s-%d-%d", harness.prefix, sequence, harness.sequence))
}

func (harness *invitationContractHarness) insertUser(t *testing.T, email, role, status string) string {
	t.Helper()
	now := contractNow()
	var userID string
	err := harness.store.pool.QueryRow(harness.ctx, `
INSERT INTO platform.users (normalized_email, email_verified_at, password_hash, role, account_status)
VALUES ($1, $2, $3, $4, $5)
RETURNING user_id::text`, email, now, strings.Repeat("p", 32), role, status).Scan(&userID)
	if err != nil {
		t.Fatalf("insert invitation contract user %s/%s: %v", email, status, err)
	}
	harness.userIDs = append(harness.userIDs, userID)
	return userID
}

func (harness *invitationContractHarness) newInvitation(email, role string, sentAt, expiresAt time.Time) identity.InvitationRequest {
	requestID := fmt.Sprintf("invitation-contract-%s-%d", harness.prefix, atomic.AddUint64(&invitationTestSequence, 1))
	emailHash := harness.newHash()
	ipHash := harness.newHash()
	harness.rateHashes = append(harness.rateHashes, emailHash, ipHash)
	harness.requestIDs = append(harness.requestIDs, requestID)
	return identity.InvitationRequest{
		Email: email, Role: role, CodeHash: harness.newHash(),
		EmailHash: emailHash, IPHash: ipHash, InvitedBy: harness.actorID,
		RequestID: requestID, Reason: "invitation contract test",
		SentAt: sentAt, ExpiresAt: expiresAt,
	}
}

func (harness *invitationContractHarness) newAccept(email, codeHash string, now time.Time) identity.InvitationAcceptRequest {
	requestID := fmt.Sprintf("invitation-accept-contract-%s-%d", harness.prefix, atomic.AddUint64(&invitationTestSequence, 1))
	ipHash := harness.newHash()
	sessionHash := harness.newHash()
	harness.rateHashes = append(harness.rateHashes, ipHash)
	harness.requestIDs = append(harness.requestIDs, requestID)
	return identity.InvitationAcceptRequest{
		Email: email, CodeHash: codeHash, PasswordHash: strings.Repeat("q", 64),
		SessionHash: sessionHash, IPHash: ipHash, RequestID: requestID,
		Now: now, UserSessionUntil: now.Add(30 * 24 * time.Hour),
		OperatorSessionUntil: now.Add(8 * time.Hour),
	}
}

func (harness *invitationContractHarness) rememberInvitation(invitation identity.Invitation) identity.Invitation {
	harness.invitationIDs = append(harness.invitationIDs, invitation.ID)
	return invitation
}

func contractNow() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func TestInvitationCreateRejectsOpenAccountStatuses(t *testing.T) {
	harness := newInvitationContractHarness(t)
	now := contractNow()
	for _, status := range []string{"active", "locked", "suspended"} {
		email := harness.newEmail(status)
		harness.insertUser(t, email, "user", status)
		request := harness.newInvitation(email, "user", now, now.Add(48*time.Hour))
		if _, err := harness.store.CreateInvitation(harness.ctx, request); !errors.Is(err, identity.ErrConflict) {
			t.Fatalf("create invitation for %s account error = %v, want conflict", status, err)
		}
		var count int
		if err := harness.store.pool.QueryRow(harness.ctx,
			`SELECT count(*) FROM platform.user_invitations WHERE normalized_email = $1`, email).Scan(&count); err != nil {
			t.Fatalf("count invitation for %s account: %v", status, err)
		}
		if count != 0 {
			t.Fatalf("create invitation for %s account left %d rows", status, count)
		}
	}
}

func TestInvitationPendingEmailIsUniqueAtRepositoryAndDatabase(t *testing.T) {
	harness := newInvitationContractHarness(t)
	now := contractNow()
	email := harness.newEmail("pending")
	firstRequest := harness.newInvitation(email, "user", now, now.Add(48*time.Hour))
	first, err := harness.store.CreateInvitation(harness.ctx, firstRequest)
	if err != nil {
		t.Fatalf("create first pending invitation: %v", err)
	}
	harness.rememberInvitation(first)
	secondRequest := harness.newInvitation(email, "editor", now.Add(time.Second), now.Add(48*time.Hour+time.Second))
	if _, err := harness.store.CreateInvitation(harness.ctx, secondRequest); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("create duplicate pending invitation error = %v, want conflict", err)
	}

	duplicateHash := harness.newHash()
	duplicateIPHash := harness.newHash()
	_, err = harness.store.pool.Exec(harness.ctx, `
INSERT INTO platform.user_invitations (
    normalized_email, role, invitation_code_hash, invited_by, request_ip_hash, sent_at, expires_at
) VALUES ($1, 'user', $2, $3::uuid, $4, $5, $6)`,
		email, duplicateHash, harness.actorID, duplicateIPHash, now, now.Add(48*time.Hour))
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
		t.Fatalf("database duplicate pending invitation error = %v, want unique violation", err)
	}
}

func TestInvitationExpiryAndRevocationRejectAcceptance(t *testing.T) {
	harness := newInvitationContractHarness(t)
	now := contractNow()

	expiredEmail := harness.newEmail("expired")
	expiredRequest := harness.newInvitation(expiredEmail, "user", now.Add(-72*time.Hour), now.Add(-time.Hour))
	expired := harness.rememberInvitation(mustCreateInvitation(t, harness, expiredRequest))
	if _, err := harness.store.AcceptInvitation(harness.ctx, harness.newAccept(expiredEmail, expiredRequest.CodeHash, now)); !errors.Is(err, identity.ErrInvalidChallenge) {
		t.Fatalf("accept expired invitation error = %v, want invalid challenge", err)
	}
	items, err := harness.store.ListInvitations(harness.ctx, "expired", 50)
	if err != nil {
		t.Fatalf("list expired invitations: %v", err)
	}
	var foundExpired bool
	for _, item := range items {
		if item.ID == expired.ID {
			foundExpired = true
			if item.Status != "expired" {
				t.Fatalf("expired invitation status = %q, want expired", item.Status)
			}
		}
	}
	if !foundExpired {
		t.Fatalf("expired invitation %s was not returned by expired filter", expired.ID)
	}

	revokedEmail := harness.newEmail("revoked")
	revokedRequest := harness.newInvitation(revokedEmail, "user", now, now.Add(48*time.Hour))
	revoked := harness.rememberInvitation(mustCreateInvitation(t, harness, revokedRequest))
	result, err := harness.store.RevokeInvitation(harness.ctx, revoked.ID, harness.actorID, "operator requested revoke", revokedRequest.RequestID, now)
	if err != nil {
		t.Fatalf("revoke invitation: %v", err)
	}
	if result.Status != "revoked" || result.ConsumedAt == nil {
		t.Fatalf("revoked invitation result = %#v, want consumed revoked row", result)
	}
	if _, err := harness.store.AcceptInvitation(harness.ctx, harness.newAccept(revokedEmail, revokedRequest.CodeHash, now)); !errors.Is(err, identity.ErrInvalidChallenge) {
		t.Fatalf("accept revoked invitation error = %v, want invalid challenge", err)
	}
}

func TestInvitationWrongCodeLocksAfterFiveAttempts(t *testing.T) {
	harness := newInvitationContractHarness(t)
	now := contractNow()
	email := harness.newEmail("attempts")
	request := harness.newInvitation(email, "user", now, now.Add(48*time.Hour))
	harness.rememberInvitation(mustCreateInvitation(t, harness, request))
	wrongCodeHash := strings.Repeat("f", 64)
	for attempt := 1; attempt <= 5; attempt++ {
		if _, err := harness.store.AcceptInvitation(harness.ctx, harness.newAccept(email, wrongCodeHash, now)); !errors.Is(err, identity.ErrInvalidChallenge) {
			t.Fatalf("wrong invitation code attempt %d error = %v, want invalid challenge", attempt, err)
		}
	}
	var attempts int
	if err := harness.store.pool.QueryRow(harness.ctx,
		`SELECT attempt_count FROM platform.user_invitations WHERE normalized_email = $1`, email).Scan(&attempts); err != nil {
		t.Fatalf("read invitation attempts: %v", err)
	}
	if attempts != 5 {
		t.Fatalf("invitation attempt_count = %d, want 5", attempts)
	}
	if _, err := harness.store.AcceptInvitation(harness.ctx, harness.newAccept(email, wrongCodeHash, now)); !errors.Is(err, identity.ErrInvalidChallenge) {
		t.Fatalf("sixth wrong invitation code error = %v, want invalid challenge", err)
	}
	if _, err := harness.store.AcceptInvitation(harness.ctx, harness.newAccept(email, request.CodeHash, now)); !errors.Is(err, identity.ErrInvalidChallenge) {
		t.Fatalf("correct code after lock error = %v, want invalid challenge", err)
	}
	var users int
	if err := harness.store.pool.QueryRow(harness.ctx,
		`SELECT count(*) FROM platform.users WHERE normalized_email = $1`, email).Scan(&users); err != nil {
		t.Fatalf("count users after locked invitation: %v", err)
	}
	if users != 0 {
		t.Fatalf("locked invitation created %d users, want 0", users)
	}
}

func TestAcceptInvitationCreatesRoleSessionAndAudit(t *testing.T) {
	harness := newInvitationContractHarness(t)
	now := contractNow()
	for _, test := range []struct {
		role     string
		duration time.Duration
	}{
		{role: "user", duration: 30 * 24 * time.Hour},
		{role: "editor", duration: 8 * time.Hour},
	} {
		email := harness.newEmail(test.role)
		invitationRequest := harness.newInvitation(email, test.role, now, now.Add(48*time.Hour))
		invitation := harness.rememberInvitation(mustCreateInvitation(t, harness, invitationRequest))
		acceptRequest := harness.newAccept(email, invitationRequest.CodeHash, now)
		user, err := harness.store.AcceptInvitation(harness.ctx, acceptRequest)
		if err != nil {
			t.Fatalf("accept %s invitation: %v", test.role, err)
		}
		harness.userIDs = append(harness.userIDs, user.ID)
		if user.Email != email || user.Role != test.role || user.AccountStatus != "active" || user.EmailVerifiedAt.IsZero() {
			t.Fatalf("accepted %s user = %#v", test.role, user)
		}

		var status string
		var consumedAt *time.Time
		if err := harness.store.pool.QueryRow(harness.ctx,
			`SELECT status, consumed_at FROM platform.user_invitations WHERE invitation_id = $1::uuid`, invitation.ID).Scan(&status, &consumedAt); err != nil {
			t.Fatalf("read accepted %s invitation: %v", test.role, err)
		}
		if status != "accepted" || consumedAt == nil {
			t.Fatalf("accepted %s invitation state = %q/%v", test.role, status, consumedAt)
		}

		var expiresAt time.Time
		var sessionCount int
		if err := harness.store.pool.QueryRow(harness.ctx, `
SELECT count(*), max(expires_at)
FROM platform.sessions
WHERE user_id = $1::uuid AND token_hash = $2`, user.ID, acceptRequest.SessionHash).Scan(&sessionCount, &expiresAt); err != nil {
			t.Fatalf("read accepted %s session: %v", test.role, err)
		}
		if sessionCount != 1 || !expiresAt.Equal(now.Add(test.duration)) {
			t.Fatalf("accepted %s session = count %d, expiry %s; want one and %s", test.role, sessionCount, expiresAt, now.Add(test.duration))
		}

		var auditCount int
		if err := harness.store.pool.QueryRow(harness.ctx, `
SELECT count(*) FROM audit.audit_logs
WHERE action = 'user.invitation_accept' AND object_type = 'user'
  AND object_id = $1::uuid AND request_id = $2`, user.ID, acceptRequest.RequestID).Scan(&auditCount); err != nil {
			t.Fatalf("read accepted %s audit: %v", test.role, err)
		}
		if auditCount != 1 {
			t.Fatalf("accepted %s audit rows = %d, want 1", test.role, auditCount)
		}
	}
}

func mustCreateInvitation(t *testing.T, harness *invitationContractHarness, request identity.InvitationRequest) identity.Invitation {
	t.Helper()
	invitation, err := harness.store.CreateInvitation(harness.ctx, request)
	if err != nil {
		t.Fatalf("create invitation %s: %v", request.Email, err)
	}
	return invitation
}
