package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRepository struct {
	signup             SignupRequest
	reset              PasswordResetRequest
	target             User
	deliver            bool
	challenge          ChallengeRequest
	invitation         InvitationRequest
	accepted           InvitationAcceptRequest
	revokeCalled       bool
	revokeReason       string
	invitationErr      error
	acceptUser         User
	reservation        EmailDeliveryReservation
	completion         EmailDeliveryCompletion
	reserveErr         error
	deliverySteps      []string
	sessionUser        User
	sessionErr         error
	loginResults       []string
	loginUser          User
	createdSession     SessionRequest
	createSessionErr   error
	invitationCheckErr error
	invitationChecked  bool
}

func (repository *fakeRepository) ReserveEmailDelivery(_ context.Context, request EmailDeliveryReservation) (EmailDeliveryTicket, error) {
	repository.reservation = request
	repository.deliverySteps = append(repository.deliverySteps, "reserve")
	if repository.reserveErr != nil {
		return EmailDeliveryTicket{}, repository.reserveErr
	}
	return EmailDeliveryTicket{ID: "10000000-0000-4000-8000-000000000001", BucketDate: request.ReservedAt.UTC().Format(time.DateOnly)}, nil
}

func (repository *fakeRepository) CompleteEmailDelivery(_ context.Context, completion EmailDeliveryCompletion) error {
	repository.completion = completion
	repository.deliverySteps = append(repository.deliverySteps, "complete")
	return nil
}

func (repository *fakeRepository) ChallengeTarget(context.Context, string, string, *string) (User, bool, error) {
	return repository.target, repository.deliver, nil
}
func (repository *fakeRepository) CreateChallenge(_ context.Context, request ChallengeRequest) error {
	repository.challenge = request
	return nil
}
func (*fakeRepository) CheckChallenge(context.Context, string, string, string, *string, time.Time) (bool, error) {
	return true, nil
}
func (repository *fakeRepository) CreateVerifiedUser(_ context.Context, request SignupRequest) (User, error) {
	repository.signup = request
	return User{}, nil
}
func (repository *fakeRepository) AllowLogin(context.Context, string, string, time.Time) error {
	return nil
}
func (repository *fakeRepository) RecordLogin(_ context.Context, _ *string, result, _ string, _ time.Time) error {
	repository.loginResults = append(repository.loginResults, result)
	return nil
}
func (repository *fakeRepository) ActiveUserByEmail(context.Context, string) (User, error) {
	if repository.loginUser.ID != "" {
		return repository.loginUser, nil
	}
	return User{}, errors.New("missing")
}
func (repository *fakeRepository) CreateSession(_ context.Context, request SessionRequest) error {
	repository.createdSession = request
	return repository.createSessionErr
}
func (repository *fakeRepository) UserBySession(context.Context, string, time.Time) (User, error) {
	return repository.sessionUser, repository.sessionErr
}
func (*fakeRepository) RevokeSession(context.Context, string, string, time.Time) error { return nil }
func (repository *fakeRepository) ResetPassword(_ context.Context, request PasswordResetRequest) error {
	repository.reset = request
	return nil
}
func (*fakeRepository) CloseAccount(context.Context, CloseAccountRequest) error { return nil }
func (repository *fakeRepository) CreateInvitation(_ context.Context, request InvitationRequest) (Invitation, error) {
	repository.invitation = request
	if repository.invitationErr != nil {
		return Invitation{}, repository.invitationErr
	}
	return Invitation{ID: "invitation-1", Email: request.Email, Role: request.Role, Status: "pending", SentAt: request.SentAt, ExpiresAt: request.ExpiresAt}, nil
}
func (*fakeRepository) ListInvitations(context.Context, string, int) ([]Invitation, error) {
	return []Invitation{}, nil
}
func (repository *fakeRepository) RevokeInvitation(_ context.Context, _, _, reason, _ string, _ time.Time) (Invitation, error) {
	repository.revokeCalled = true
	repository.revokeReason = reason
	return Invitation{}, nil
}
func (repository *fakeRepository) AcceptInvitation(_ context.Context, request InvitationAcceptRequest) (User, error) {
	repository.accepted = request
	if repository.acceptUser.ID != "" {
		return repository.acceptUser, nil
	}
	return User{ID: "user-1", Email: "person@example.com", Role: "user", AccountStatus: "active"}, nil
}

func (repository *fakeRepository) CheckInvitation(context.Context, string, string, string, time.Time) error {
	repository.invitationChecked = true
	return repository.invitationCheckErr
}

func TestAcceptInvitationPrechecksBeforePreparingCredentials(t *testing.T) {
	for _, precheckErr := range []error{ErrInvalidChallenge, ErrRateLimited} {
		repository := &fakeRepository{invitationCheckErr: precheckErr}
		worker := newPasswordWorker(1, 0, time.Second, func(_, _ []byte, _, _ uint32, _ uint8, _ uint32) []byte {
			panic("rejected invitation reached expensive computation")
		})
		service := &Service{repository: repository, passwords: worker, now: time.Now, pepper: []byte(strings.Repeat("p", 32))}
		user, token, err := service.AcceptInvitation(context.Background(), "person@example.test", "123456", "a-long-test-password", "127.0.0.1", "precheck-test")
		if !errors.Is(err, precheckErr) || !repository.invitationChecked || repository.accepted.PasswordHash != "" || user.ID != "" || token != "" {
			t.Fatal("invalid invitation reached account/session preparation without precheck")
		}
	}
}

type fakeEmail struct {
	code   string
	err    error
	onSend func(context.Context)
}

func (email *fakeEmail) SendCode(ctx context.Context, _, _, code string) error {
	email.code = code
	if email.onSend != nil {
		email.onSend(ctx)
	}
	return email.err
}

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("a-long-test-password", hash) || VerifyPassword("wrong-password", hash) {
		t.Fatal("password verification mismatch")
	}
}

func TestLoginSessionCarriesVerifiedAccountSnapshot(t *testing.T) {
	password := "a-long-test-password"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, role := range []string{"user", "editor", "admin"} {
		repository := &fakeRepository{loginUser: User{
			ID: "10000000-0000-4000-8000-000000000001", Email: "login@example.test",
			PasswordHash: hash, Role: role, AccountStatus: "active", EmailVerifiedAt: now,
		}}
		service, err := NewService(repository, &fakeEmail{}, strings.Repeat("s", 32), func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		_, token, err := service.Login(context.Background(), "LOGIN@example.test", password, "127.0.0.1", "request")
		if err != nil || token == "" {
			t.Fatal("valid login failed")
		}
		request := repository.createdSession
		if request.UserID != repository.loginUser.ID || request.ExpectedEmail != "login@example.test" || request.ExpectedPasswordHash != hash || request.ExpectedRole != role {
			t.Fatal("session issuance lost the verified account snapshot")
		}
		if request.TokenHash != SessionTokenHash(token) || request.TokenHash == token || !request.CreatedAt.Equal(now) {
			t.Fatal("session token must be hashed and timestamps preserved")
		}
		duration := 30 * 24 * time.Hour
		if role != "user" {
			duration = 8 * time.Hour
		}
		if request.ExpiresAt.Sub(request.CreatedAt) != duration {
			t.Fatal("session lifetime changed")
		}
		if len(repository.loginResults) != 1 || repository.loginResults[0] != "success" {
			t.Fatal("successful login audit missing")
		}

		repository.createSessionErr = ErrInvalidCredentials // Database rejected a stale snapshot.
		repository.loginResults = nil
		user, staleToken, err := service.Login(context.Background(), "login@example.test", password, "127.0.0.1", "stale-request")
		if !errors.Is(err, ErrInvalidCredentials) || staleToken != "" || user.ID != "" {
			t.Fatal("stale snapshot must not return an authenticated user or token")
		}
		if len(repository.loginResults) != 1 || repository.loginResults[0] != "invalid_credentials" {
			t.Fatal("stale login must be audited as invalid credentials, not success")
		}
	}
}

func TestReauthenticationTokenIsShortLivedAndBoundToSession(t *testing.T) {
	passwordHash, err := HashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	repository := &fakeRepository{sessionUser: User{
		ID: "10000000-0000-4000-8000-000000000001", Email: "operator@example.test",
		PasswordHash: passwordHash, Role: "admin", AccountStatus: "active",
	}}
	service, err := NewService(repository, &fakeEmail{}, "0123456789abcdef0123456789abcdef", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.Reauthenticate(context.Background(), "current-session", "a-long-test-password", "127.0.0.1", "request-1")
	if err != nil || token == "" || strings.Contains(token, "a-long-test-password") {
		t.Fatalf("unexpected reauthentication result token=%q err=%v", token, err)
	}
	if err := service.ValidateReauthentication("current-session", token); err != nil {
		t.Fatalf("valid reauthentication token was rejected: %v", err)
	}
	if err := service.ValidateReauthentication("different-session", token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("token was not bound to its session: %v", err)
	}
	now = now.Add(ReauthDuration)
	if err := service.ValidateReauthentication("current-session", token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired reauthentication token was accepted: %v", err)
	}
	if len(repository.loginResults) != 1 || repository.loginResults[0] != "success" {
		t.Fatalf("reauthentication audit result missing: %#v", repository.loginResults)
	}
}

func TestReauthenticationRejectsWrongPasswordAndUsesLoginLimiter(t *testing.T) {
	passwordHash, err := HashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{sessionUser: User{
		ID: "10000000-0000-4000-8000-000000000001", Email: "operator@example.test", PasswordHash: passwordHash,
	}}
	service, err := NewService(repository, &fakeEmail{}, "0123456789abcdef0123456789abcdef", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token, err := service.Reauthenticate(context.Background(), "current-session", "wrong-password", "127.0.0.1", "request-1"); token != "" || !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password result token=%q err=%v", token, err)
	}
	if len(repository.loginResults) != 1 || repository.loginResults[0] != "invalid_credentials" {
		t.Fatalf("wrong password was not audited: %#v", repository.loginResults)
	}
	now := service.now().UTC()
	repository.loginResults = nil
	service.reauthAttempts["session:"+SessionTokenHash("current-session")] = reauthAttemptBucket{
		windowStart: now.Truncate(reauthWindow), count: reauthSessionMax,
	}
	if token, err := service.Reauthenticate(context.Background(), "current-session", "a-long-test-password", "127.0.0.1", "request-2"); token != "" || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate limit result token=%q err=%v", token, err)
	}
	if repository.loginResults[len(repository.loginResults)-1] != "rate_limited" {
		t.Fatalf("rate-limited attempt was not audited: %#v", repository.loginResults)
	}
}

func TestRequestCodeStoresOnlyHash(t *testing.T) {
	repository := &fakeRepository{target: User{ID: "user-1", Email: "person@example.com"}, deliver: true}
	email := &fakeEmail{}
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	service, err := NewService(repository, email, "0123456789abcdef0123456789abcdef", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RequestCode(context.Background(), "Person@Example.com", PurposePasswordReset, nil, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if len(email.code) != 6 || repository.challenge.CodeHash == email.code || len(repository.challenge.CodeHash) != 64 {
		t.Fatalf("challenge was not hashed correctly")
	}
	if repository.challenge.ExpiresAt.Sub(repository.challenge.SentAt) != 10*time.Minute {
		t.Fatal("unexpected challenge lifetime")
	}
}

func TestSignupCodeForExistingAccountDoesNotSendEmail(t *testing.T) {
	repository := &fakeRepository{
		target:  User{ID: "user-1", Email: "person@example.com", AccountStatus: "active"},
		deliver: false,
	}
	email := &fakeEmail{}
	now := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	service, err := NewService(repository, email, "0123456789abcdef0123456789abcdef", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	if err := service.RequestCode(context.Background(), "Person@Example.com", PurposeSignup, nil, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if repository.challenge.Purpose != PurposeSignup || repository.challenge.Deliver {
		t.Fatalf("existing account challenge = %#v, want a non-delivery signup request", repository.challenge)
	}
	if email.code != "" || len(repository.deliverySteps) != 0 {
		t.Fatalf("existing account unexpectedly triggered email delivery: code=%q steps=%v", email.code, repository.deliverySteps)
	}
}

func TestNormalizeEmailRejectsDisplayName(t *testing.T) {
	if _, err := NormalizeEmail("Name <person@example.com>"); err == nil {
		t.Fatal("expected display name email to be rejected")
	}
}

func TestInvalidCodeRejected(t *testing.T) {
	if !errors.Is(validateCode("12ab56"), ErrInvalidChallenge) {
		t.Fatal("expected invalid challenge error")
	}
}

func TestCreateInvitationEnforcesRoleAndHashesCode(t *testing.T) {
	repository := &fakeRepository{}
	email := &fakeEmail{}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	service, err := NewService(repository, email, "0123456789abcdef0123456789abcdef", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	invitation, err := service.CreateInvitation(context.Background(), "Person@Example.com", "admin", "initial access", "owner-1", "owner", "request-1", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if invitation.Role != "admin" || repository.invitation.Email != "person@example.com" {
		t.Fatalf("unexpected invitation: %#v", invitation)
	}
	if len(email.code) != 6 || repository.invitation.CodeHash == email.code || len(repository.invitation.CodeHash) != 64 {
		t.Fatalf("invitation code was not hashed correctly")
	}
	if repository.invitation.ExpiresAt.Sub(repository.invitation.SentAt) != 48*time.Hour {
		t.Fatalf("unexpected invitation lifetime: %s", repository.invitation.ExpiresAt.Sub(repository.invitation.SentAt))
	}
	if _, err := service.CreateInvitation(context.Background(), "other@example.com", "admin", "initial access", "admin-1", "admin", "request-2", "127.0.0.1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected admin escalation to be forbidden, got %v", err)
	}
}

func TestCreateInvitationDeliveryFailureReleasesPendingRecord(t *testing.T) {
	repository := &fakeRepository{}
	email := &fakeEmail{err: errors.New("smtp unavailable")}
	service, err := NewService(repository, email, "0123456789abcdef0123456789abcdef", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateInvitation(context.Background(), "person@example.com", "user", "initial access", "owner-1", "owner", "request-1", "127.0.0.1")
	if !errors.Is(err, ErrEmailUnavailable) || !repository.revokeCalled || repository.revokeReason != "email delivery failed" {
		t.Fatalf("expected delivery failure to revoke invitation, err=%v revoked=%v reason=%q", err, repository.revokeCalled, repository.revokeReason)
	}
}

func TestAcceptInvitationReturnsSessionAndKeepsSecretsOutOfRepositoryFields(t *testing.T) {
	repository := &fakeRepository{}
	email := &fakeEmail{}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	service, err := NewService(repository, email, "0123456789abcdef0123456789abcdef", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := service.AcceptInvitation(context.Background(), "Person@Example.com", "123456", "a-long-test-password", "127.0.0.1", "request-1")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || repository.accepted.Email != "person@example.com" || len(repository.accepted.CodeHash) != 64 {
		t.Fatalf("unexpected invitation acceptance request: %#v", repository.accepted)
	}
	if repository.accepted.PasswordHash == "a-long-test-password" || repository.accepted.SessionHash == token {
		t.Fatal("plaintext password or session token was passed to repository")
	}
	if repository.accepted.UserSessionUntil.Sub(repository.accepted.Now) != 30*24*time.Hour || repository.accepted.OperatorSessionUntil.Sub(repository.accepted.Now) != 8*time.Hour {
		t.Fatalf("invitation session durations are not role-aware: %#v", repository.accepted)
	}
}
