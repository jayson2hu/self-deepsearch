package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidChallenge   = errors.New("invalid or expired challenge")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrRateLimited        = errors.New("rate limited")
	ErrAuthBusy           = errors.New("password computation busy")
	ErrEmailUnavailable   = errors.New("email delivery unavailable")
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrInvalidInput       = errors.New("invalid input")
	ErrForbidden          = errors.New("forbidden")
	ErrConflict           = errors.New("conflict")
	ErrNotFound           = errors.New("not found")
)

const (
	PurposeSignup        = "signup"
	PurposePasswordReset = "password_reset"
	PurposeAccountClose  = "account_close"
	PurposeInvitation    = "invitation"
	SessionCookieName    = "sd_session"
	ReauthCookieName     = "sd_reauth"
	// Reauthentication is deliberately short-lived. It confirms a password
	// shortly before a destructive operator action, while leaving the normal
	// operator session duration unchanged.
	ReauthDuration   = 5 * time.Minute
	reauthWindow     = time.Hour
	reauthSessionMax = 20
	reauthIPMax      = 60
)

type User struct {
	ID              string
	Email           string
	PasswordHash    string
	Role            string
	AccountStatus   string
	EmailVerifiedAt time.Time
}

type ChallengeRequest struct {
	Email     string
	Purpose   string
	UserID    *string
	CodeHash  string
	EmailHash string
	IPHash    string
	SentAt    time.Time
	ExpiresAt time.Time
	Deliver   bool
}

type SignupRequest struct {
	Email        string
	CodeHash     string
	PasswordHash string
	SessionHash  string
	IPHash       string
	Now          time.Time
	SessionUntil time.Time
}

// SessionRequest carries the account snapshot whose password was verified.
// The repository must atomically recheck it while issuing the session.
type SessionRequest struct {
	UserID               string
	ExpectedEmail        string
	ExpectedPasswordHash string
	ExpectedRole         string
	TokenHash            string
	CreatedAt            time.Time
	ExpiresAt            time.Time
}

type PasswordResetRequest struct {
	Email        string
	CodeHash     string
	PasswordHash string
	Now          time.Time
}

type CloseAccountRequest struct {
	UserID        string
	CodeHash      string
	NoticeVersion string
	RequestID     string
	Now           time.Time
}

type Invitation struct {
	ID         string
	Email      string
	Role       string
	Status     string
	InvitedBy  string
	Inviter    string
	SentAt     time.Time
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

type InvitationRequest struct {
	Email     string
	Role      string
	CodeHash  string
	EmailHash string
	IPHash    string
	InvitedBy string
	RequestID string
	Reason    string
	SentAt    time.Time
	ExpiresAt time.Time
}

type InvitationAcceptRequest struct {
	Email                string
	CodeHash             string
	PasswordHash         string
	SessionHash          string
	IPHash               string
	RequestID            string
	Now                  time.Time
	UserSessionUntil     time.Time
	OperatorSessionUntil time.Time
}

type Repository interface {
	EmailDeliveryGuard
	ChallengeTarget(context.Context, string, string, *string) (User, bool, error)
	CreateChallenge(context.Context, ChallengeRequest) error
	CheckChallenge(context.Context, string, string, string, *string, time.Time) (bool, error)
	CreateVerifiedUser(context.Context, SignupRequest) (User, error)
	AllowLogin(context.Context, string, string, time.Time) error
	RecordLogin(context.Context, *string, string, string, time.Time) error
	ActiveUserByEmail(context.Context, string) (User, error)
	CreateSession(context.Context, SessionRequest) error
	UserBySession(context.Context, string, time.Time) (User, error)
	RevokeSession(context.Context, string, string, time.Time) error
	ResetPassword(context.Context, PasswordResetRequest) error
	CloseAccount(context.Context, CloseAccountRequest) error
	CreateInvitation(context.Context, InvitationRequest) (Invitation, error)
	ListInvitations(context.Context, string, int) ([]Invitation, error)
	RevokeInvitation(context.Context, string, string, string, string, time.Time) (Invitation, error)
	CheckInvitation(context.Context, string, string, string, time.Time) error
	AcceptInvitation(context.Context, InvitationAcceptRequest) (User, error)
}

// ReauthenticationService is implemented by the identity service and kept
// separate from the broad account interface so lightweight HTTP test doubles
// and read-only deployments do not need to implement password confirmation.
type ReauthenticationService interface {
	Reauthenticate(context.Context, string, string, string, string) (string, error)
	ValidateReauthentication(string, string) error
}

type EmailSender interface {
	SendCode(context.Context, string, string, string) error
}

type Service struct {
	repository        Repository
	email             EmailSender
	emailPolicy       EmailDeliveryPolicy
	pepper            []byte
	now               func() time.Time
	dummyPasswordHash string
	passwords         *passwordWorker
	reauthMu          sync.Mutex
	reauthAttempts    map[string]reauthAttemptBucket
}

type reauthAttemptBucket struct {
	windowStart time.Time
	count       int
}

func NewService(repository Repository, email EmailSender, pepper string, now func() time.Time) (*Service, error) {
	return NewServiceWithEmailPolicy(repository, email, pepper, DefaultEmailDeliveryPolicy(), now)
}

func NewServiceWithEmailPolicy(repository Repository, email EmailSender, pepper string, emailPolicy EmailDeliveryPolicy, now func() time.Time) (*Service, error) {
	if repository == nil {
		return nil, errors.New("identity repository is required")
	}
	if email == nil {
		return nil, errors.New("email sender is required")
	}
	if len(pepper) < 32 {
		return nil, errors.New("identity HMAC secret must be at least 32 characters")
	}
	if err := emailPolicy.validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	dummyPasswordHash, err := HashPassword("invalid-account-password-placeholder")
	if err != nil {
		return nil, fmt.Errorf("create dummy password hash: %w", err)
	}
	return &Service{
		repository: repository, email: email, emailPolicy: emailPolicy, pepper: []byte(pepper), now: now,
		dummyPasswordHash: dummyPasswordHash, passwords: processPasswords, reauthAttempts: make(map[string]reauthAttemptBucket),
	}, nil
}

func (service *Service) RequestCode(ctx context.Context, email, purpose string, userID *string, remoteAddress string) error {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if purpose != PurposeSignup && purpose != PurposePasswordReset && purpose != PurposeAccountClose {
		return errors.New("unsupported challenge purpose")
	}
	target, deliver, err := service.repository.ChallengeTarget(ctx, normalized, purpose, userID)
	if err != nil {
		return err
	}
	if deliver {
		normalized = target.Email
		userID = &target.ID
		if purpose == PurposeSignup {
			userID = nil
		}
	}

	code, err := randomDigits(6)
	if err != nil {
		return fmt.Errorf("generate email challenge: %w", err)
	}
	now := service.now().UTC()
	request := ChallengeRequest{
		Email: normalized, Purpose: purpose, UserID: userID,
		CodeHash:  service.challengeHash(normalized, purpose, code),
		EmailHash: service.riskHash("email", normalized),
		IPHash:    service.riskHash("ip", remoteAddress),
		SentAt:    now, ExpiresAt: now.Add(10 * time.Minute), Deliver: deliver,
	}
	if err := service.repository.CreateChallenge(ctx, request); err != nil {
		return err
	}
	if deliver {
		if err := service.deliverCode(ctx, normalized, purpose, code, userID); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) Signup(ctx context.Context, email, code, password, remoteAddress string) (User, string, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return User{}, "", err
	}
	if err := validateCode(code); err != nil {
		return User{}, "", err
	}
	if err := validatePassword(password); err != nil {
		return User{}, "", err
	}
	now := service.now().UTC()
	valid, err := service.repository.CheckChallenge(ctx, normalized, PurposeSignup, service.challengeHash(normalized, PurposeSignup, code), nil, now)
	if err != nil {
		return User{}, "", err
	}
	if !valid {
		return User{}, "", ErrInvalidChallenge
	}
	passwordHash, err := service.passwords.hash(ctx, password)
	if err != nil {
		return User{}, "", err
	}
	token, tokenHash, err := NewSessionToken()
	if err != nil {
		return User{}, "", err
	}
	now = service.now().UTC()
	user, err := service.repository.CreateVerifiedUser(ctx, SignupRequest{
		Email: normalized, CodeHash: service.challengeHash(normalized, PurposeSignup, code),
		PasswordHash: passwordHash, SessionHash: tokenHash,
		IPHash: service.riskHash("ip", remoteAddress), Now: now, SessionUntil: now.Add(30 * 24 * time.Hour),
	})
	if err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

func (service *Service) Login(ctx context.Context, email, password, remoteAddress, requestID string) (User, string, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return User{}, "", ErrInvalidCredentials
	}
	now := service.now().UTC()
	if err := service.repository.AllowLogin(ctx, service.riskHash("email", normalized), service.riskHash("ip", remoteAddress), now); err != nil {
		_ = service.repository.RecordLogin(ctx, nil, "rate_limited", requestID, now)
		return User{}, "", err
	}
	user, lookupErr := service.repository.ActiveUserByEmail(ctx, normalized)
	passwordHash := user.PasswordHash
	if lookupErr != nil {
		passwordHash = service.dummyPasswordHash
	}
	passwordMatches, err := service.passwords.verify(ctx, password, passwordHash)
	if err != nil {
		return User{}, "", err
	}
	now = service.now().UTC()
	if lookupErr != nil || !passwordMatches {
		var userID *string
		if user.ID != "" {
			userID = &user.ID
		}
		_ = service.repository.RecordLogin(ctx, userID, "invalid_credentials", requestID, now)
		return User{}, "", ErrInvalidCredentials
	}
	token, tokenHash, err := NewSessionToken()
	if err != nil {
		return User{}, "", err
	}
	duration := 30 * 24 * time.Hour
	if user.Role != "user" {
		duration = 8 * time.Hour
	}
	if err := service.repository.CreateSession(ctx, SessionRequest{
		UserID: user.ID, ExpectedEmail: user.Email, ExpectedPasswordHash: user.PasswordHash,
		ExpectedRole: user.Role, TokenHash: tokenHash, CreatedAt: now, ExpiresAt: now.Add(duration),
	}); err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			_ = service.repository.RecordLogin(ctx, &user.ID, "invalid_credentials", requestID, now)
		}
		return User{}, "", err
	}
	_ = service.repository.RecordLogin(ctx, &user.ID, "success", requestID, now)
	return user, token, nil
}

func (service *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrUnauthenticated
	}
	user, err := service.repository.UserBySession(ctx, SessionTokenHash(token), service.now().UTC())
	if err != nil {
		return User{}, ErrUnauthenticated
	}
	return user, nil
}

// Reauthenticate verifies the password for the currently valid session and
// returns a signed, short-lived token bound to that exact session token. No
// plaintext password or reauthentication token is persisted.
func (service *Service) Reauthenticate(ctx context.Context, sessionToken, password, remoteAddress, requestID string) (string, error) {
	if strings.TrimSpace(sessionToken) == "" {
		return "", ErrUnauthenticated
	}
	now := service.now().UTC()
	user, err := service.repository.UserBySession(ctx, SessionTokenHash(sessionToken), now)
	if err != nil {
		return "", ErrUnauthenticated
	}
	// Release A runs one API instance. A process-local limiter avoids counting
	// successful five-minute confirmations as normal login attempts, while
	// bounding password guessing without a schema change. The edge rate limit
	// remains an additional production control.
	if !service.allowReauthentication(sessionToken, remoteAddress, now) {
		_ = service.repository.RecordLogin(ctx, &user.ID, "rate_limited", requestID, now)
		return "", ErrRateLimited
	}
	passwordMatches, err := service.passwords.verify(ctx, password, user.PasswordHash)
	if err != nil {
		return "", err
	}
	now = service.now().UTC()
	if !passwordMatches {
		_ = service.repository.RecordLogin(ctx, &user.ID, "invalid_credentials", requestID, now)
		return "", ErrInvalidCredentials
	}
	_ = service.repository.RecordLogin(ctx, &user.ID, "success", requestID, now)
	return service.newReauthenticationToken(sessionToken, now)
}

func (service *Service) allowReauthentication(sessionToken, remoteAddress string, now time.Time) bool {
	service.reauthMu.Lock()
	defer service.reauthMu.Unlock()
	if service.reauthAttempts == nil {
		service.reauthAttempts = make(map[string]reauthAttemptBucket)
	}
	windowStart := now.UTC().Truncate(reauthWindow)
	for key, bucket := range service.reauthAttempts {
		if bucket.windowStart.Before(windowStart) {
			delete(service.reauthAttempts, key)
		}
	}
	checks := []struct {
		key   string
		limit int
	}{
		{key: "session:" + SessionTokenHash(sessionToken), limit: reauthSessionMax},
		{key: "ip:" + service.riskHash("reauth-ip", remoteAddress), limit: reauthIPMax},
	}
	for _, check := range checks {
		bucket := service.reauthAttempts[check.key]
		if bucket.windowStart.Equal(windowStart) && bucket.count >= check.limit {
			return false
		}
	}
	for _, check := range checks {
		bucket := service.reauthAttempts[check.key]
		if !bucket.windowStart.Equal(windowStart) {
			bucket = reauthAttemptBucket{windowStart: windowStart}
		}
		bucket.count++
		service.reauthAttempts[check.key] = bucket
	}
	return true
}

// ValidateReauthentication checks the signature, lifetime and session binding
// of a token. The caller must authenticate the session separately, so a
// revoked or expired session cannot keep using an otherwise valid token.
func (service *Service) ValidateReauthentication(sessionToken, token string) error {
	if strings.TrimSpace(sessionToken) == "" || len(token) == 0 || len(token) > 512 {
		return ErrUnauthenticated
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ErrUnauthenticated
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(signature) != sha256.Size || !hmac.Equal(signature, service.signReauthentication(parts[0])) {
		return ErrUnauthenticated
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ErrUnauthenticated
	}
	fields := strings.Split(string(payload), "|")
	if len(fields) != 5 || fields[0] != "v1" || fields[1] != SessionTokenHash(sessionToken) {
		return ErrUnauthenticated
	}
	issuedAt, errIssued := strconv.ParseInt(fields[2], 10, 64)
	expiresAt, errExpires := strconv.ParseInt(fields[3], 10, 64)
	nonce, errNonce := base64.RawURLEncoding.DecodeString(fields[4])
	if errIssued != nil || errExpires != nil || errNonce != nil || len(nonce) != 16 || expiresAt <= issuedAt {
		return ErrUnauthenticated
	}
	maxLifetime := int64(ReauthDuration / time.Second)
	now := service.now().UTC().Unix()
	if issuedAt > now+30 || expiresAt <= now || expiresAt-issuedAt > maxLifetime {
		return ErrUnauthenticated
	}
	return nil
}

func (service *Service) newReauthenticationToken(sessionToken string, now time.Time) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate reauthentication token: %w", err)
	}
	payload := strings.Join([]string{
		"v1",
		SessionTokenHash(sessionToken),
		strconv.FormatInt(now.UTC().Unix(), 10),
		strconv.FormatInt(now.UTC().Add(ReauthDuration).Unix(), 10),
		base64.RawURLEncoding.EncodeToString(nonce),
	}, "|")
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payload))
	encodedSignature := base64.RawURLEncoding.EncodeToString(service.signReauthentication(encodedPayload))
	return encodedPayload + "." + encodedSignature, nil
}

func (service *Service) signReauthentication(encodedPayload string) []byte {
	digest := hmac.New(sha256.New, service.pepper)
	_, _ = digest.Write([]byte("reauth\x00" + encodedPayload))
	return digest.Sum(nil)
}

func (service *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return service.repository.RevokeSession(ctx, SessionTokenHash(token), "logout", service.now().UTC())
}

func (service *Service) ResetPassword(ctx context.Context, email, code, password string) error {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := validateCode(code); err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	now := service.now().UTC()
	valid, err := service.repository.CheckChallenge(ctx, normalized, PurposePasswordReset, service.challengeHash(normalized, PurposePasswordReset, code), nil, now)
	if err != nil {
		return err
	}
	if !valid {
		return ErrInvalidChallenge
	}
	passwordHash, err := service.passwords.hash(ctx, password)
	if err != nil {
		return err
	}
	return service.repository.ResetPassword(ctx, PasswordResetRequest{
		Email: normalized, CodeHash: service.challengeHash(normalized, PurposePasswordReset, code),
		PasswordHash: passwordHash, Now: service.now().UTC(),
	})
}

func (service *Service) CloseAccount(ctx context.Context, userID, email, code, noticeVersion, requestID string) error {
	if err := validateCode(code); err != nil {
		return err
	}
	return service.repository.CloseAccount(ctx, CloseAccountRequest{
		UserID: userID, CodeHash: service.challengeHash(email, PurposeAccountClose, code),
		NoticeVersion: noticeVersion, RequestID: requestID, Now: service.now().UTC(),
	})
}

// CreateInvitation creates a time-limited, single-use email invitation. The
// plaintext code is only passed to the mail sender and is never persisted.
func (service *Service) CreateInvitation(ctx context.Context, email, role, reason, actorID, actorRole, requestID, remoteAddress string) (Invitation, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return Invitation{}, err
	}
	if role != "admin" && role != "editor" && role != "user" {
		return Invitation{}, fmt.Errorf("%w: unsupported invitation role", ErrInvalidInput)
	}
	if actorRole != "owner" && !(actorRole == "admin" && role == "user") {
		return Invitation{}, ErrForbidden
	}
	if len(strings.TrimSpace(reason)) < 2 || len(strings.TrimSpace(reason)) > 500 {
		return Invitation{}, ErrInvalidInput
	}
	code, err := randomDigits(6)
	if err != nil {
		return Invitation{}, fmt.Errorf("generate invitation code: %w", err)
	}
	now := service.now().UTC()
	invitation, err := service.repository.CreateInvitation(ctx, InvitationRequest{
		Email: normalized, Role: role, CodeHash: service.challengeHash(normalized, PurposeInvitation, code),
		EmailHash: service.riskHash("email", normalized), IPHash: service.riskHash("ip", remoteAddress),
		InvitedBy: actorID, RequestID: requestID, Reason: strings.TrimSpace(reason), SentAt: now, ExpiresAt: now.Add(48 * time.Hour),
	})
	if err != nil {
		return Invitation{}, err
	}
	if err := service.deliverCode(ctx, normalized, PurposeInvitation, code, nil); err != nil {
		// Do not leave an undeliverable pending invitation that blocks a retry.
		// The database operation is best-effort; the delivery error remains the
		// primary result returned to the caller.
		_, _ = service.repository.RevokeInvitation(ctx, invitation.ID, actorID, "email delivery failed", requestID, now)
		return invitation, err
	}
	return invitation, nil
}

func (service *Service) ListInvitations(ctx context.Context, status string, limit int) ([]Invitation, error) {
	return service.repository.ListInvitations(ctx, status, limit)
}

func (service *Service) RevokeInvitation(ctx context.Context, invitationID, actorID, reason, requestID string, now time.Time) (Invitation, error) {
	if invitationID == "" || actorID == "" || len(strings.TrimSpace(reason)) < 2 {
		return Invitation{}, ErrInvalidInput
	}
	return service.repository.RevokeInvitation(ctx, invitationID, actorID, reason, requestID, now.UTC())
}

func (service *Service) AcceptInvitation(ctx context.Context, email, code, password, remoteAddress, requestID string) (User, string, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return User{}, "", err
	}
	if err := validateCode(code); err != nil {
		return User{}, "", err
	}
	if err := validatePassword(password); err != nil {
		return User{}, "", err
	}
	if err := service.repository.CheckInvitation(ctx, normalized, service.challengeHash(normalized, PurposeInvitation, code), service.riskHash("ip", remoteAddress), service.now().UTC()); err != nil {
		return User{}, "", err
	}
	passwordHash, err := service.passwords.hash(ctx, password)
	if err != nil {
		return User{}, "", err
	}
	token, tokenHash, err := NewSessionToken()
	if err != nil {
		return User{}, "", err
	}
	now := service.now().UTC()
	user, err := service.repository.AcceptInvitation(ctx, InvitationAcceptRequest{
		Email: normalized, CodeHash: service.challengeHash(normalized, PurposeInvitation, code), PasswordHash: passwordHash,
		SessionHash: tokenHash, IPHash: service.riskHash("ip", remoteAddress), RequestID: requestID, Now: now,
		UserSessionUntil: now.Add(30 * 24 * time.Hour), OperatorSessionUntil: now.Add(8 * time.Hour),
	})
	if err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

func NormalizeEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 3 || len(value) > 320 || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("%w: invalid email", ErrInvalidInput)
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || !strings.EqualFold(parsed.Address, value) || !strings.Contains(parsed.Address, "@") {
		return "", fmt.Errorf("%w: invalid email", ErrInvalidInput)
	}
	return strings.ToLower(parsed.Address), nil
}

func validatePassword(password string) error {
	if len(password) < 12 || len(password) > 128 {
		return fmt.Errorf("%w: password must contain 12 to 128 characters", ErrInvalidInput)
	}
	return nil
}

func NewSessionToken() (string, string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	return token, SessionTokenHash(token), nil
}

func SessionTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (service *Service) challengeHash(email, purpose, code string) string {
	return service.riskHash("challenge", purpose+"\x00"+strings.ToLower(email)+"\x00"+code)
}

func (service *Service) riskHash(dimension, value string) string {
	digest := hmac.New(sha256.New, service.pepper)
	_, _ = digest.Write([]byte(dimension + "\x00" + value))
	return hex.EncodeToString(digest.Sum(nil))
}

func randomDigits(length int) (string, error) {
	result := make([]byte, length)
	for index := range result {
		var value [1]byte
		for {
			if _, err := rand.Read(value[:]); err != nil {
				return "", err
			}
			if value[0] < 250 {
				result[index] = '0' + value[0]%10
				break
			}
		}
	}
	return string(result), nil
}

func validateCode(code string) error {
	if len(code) != 6 {
		return ErrInvalidChallenge
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return ErrInvalidChallenge
		}
	}
	return nil
}
