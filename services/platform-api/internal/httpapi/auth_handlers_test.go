package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/security"
)

type fakeIdentityService struct {
	err             error
	purpose         string
	requestEmail    string
	requestUserID   string
	closedUserID    string
	closedEmail     string
	closedCode      string
	closedNotice    string
	closedRequestID string
	loginIP         string
	logoutCalls     int
	logoutToken     string
	logoutDeadline  time.Time
	user            identity.User
	token           string
}

type fakeRecentIdentityService struct {
	*fakeIdentityService
	reauthToken      string
	reauthErr        error
	validateErr      error
	sessionToken     string
	password         string
	validatedSession string
	validatedReauth  string
}

func (service *fakeRecentIdentityService) Reauthenticate(_ context.Context, sessionToken, password, _, _ string) (string, error) {
	service.sessionToken, service.password = sessionToken, password
	return service.reauthToken, service.reauthErr
}

func (service *fakeRecentIdentityService) ValidateReauthentication(sessionToken, token string) error {
	service.validatedSession, service.validatedReauth = sessionToken, token
	return service.validateErr
}

func (service *fakeIdentityService) RequestCode(_ context.Context, email, purpose string, userID *string, _ string) error {
	service.requestEmail, service.purpose = email, purpose
	if userID != nil {
		service.requestUserID = *userID
	}
	return service.err
}
func (service *fakeIdentityService) Signup(context.Context, string, string, string, string) (identity.User, string, error) {
	return service.user, service.token, service.err
}

func (service *fakeIdentityService) Login(_ context.Context, _, _, remoteIP, _ string) (identity.User, string, error) {
	service.loginIP = remoteIP
	return service.user, service.token, service.err
}
func (service *fakeIdentityService) Authenticate(context.Context, string) (identity.User, error) {
	return service.user, service.err
}
func (service *fakeIdentityService) Logout(ctx context.Context, token string) error {
	service.logoutCalls++
	service.logoutToken = token
	service.logoutDeadline, _ = ctx.Deadline()
	return service.err
}
func (service *fakeIdentityService) ResetPassword(context.Context, string, string, string) error {
	return service.err
}
func (service *fakeIdentityService) CloseAccount(_ context.Context, userID, email, code, noticeVersion, requestID string) error {
	service.closedUserID, service.closedEmail, service.closedCode = userID, email, code
	service.closedNotice, service.closedRequestID = noticeVersion, requestID
	return service.err
}
func (service *fakeIdentityService) CreateInvitation(context.Context, string, string, string, string, string, string, string) (identity.Invitation, error) {
	return identity.Invitation{ID: "invitation-1", Email: "person@example.com", Role: "user", Status: "pending"}, service.err
}
func (service *fakeIdentityService) ListInvitations(context.Context, string, int) ([]identity.Invitation, error) {
	return []identity.Invitation{}, service.err
}
func (service *fakeIdentityService) RevokeInvitation(context.Context, string, string, string, string, time.Time) (identity.Invitation, error) {
	return identity.Invitation{}, service.err
}
func (service *fakeIdentityService) AcceptInvitation(context.Context, string, string, string, string, string) (identity.User, string, error) {
	return service.user, service.token, service.err
}

type acceptingVerifier struct{}

type recordingVerifier struct {
	token    string
	remoteIP string
	action   string
}

func TestPasswordWorkOverloadReturnsRetryableErrorWithoutCookies(t *testing.T) {
	for _, path := range []string{"signup/verify", "invitations/accept", "login", "password/reset", "reauth"} {
		t.Run(path, func(t *testing.T) {
			service := &fakeRecentIdentityService{fakeIdentityService: &fakeIdentityService{err: identity.ErrAuthBusy, token: "must-not-be-issued"}, reauthErr: identity.ErrAuthBusy}
			body := `{"email":"person@example.test","code":"123456","password":"a-long-test-password"}`
			if path == "login" {
				body = `{"email":"person@example.test","password":"a-long-test-password"}`
			}
			if path == "reauth" {
				body = `{"password":"a-long-test-password"}`
			}
			handler := New(Options{Identity: service, HumanVerifier: acceptingVerifier{}})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/"+path, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "current-session"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" || !strings.Contains(response.Body.String(), "AUTH_BUSY") || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") || len(response.Result().Cookies()) != 0 || strings.Contains(response.Body.String(), "must-not-be-issued") {
				t.Fatalf("unsafe overload response %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func (acceptingVerifier) Verify(context.Context, string, string, string) error { return nil }

func (verifier *recordingVerifier) Verify(_ context.Context, token, remoteIP, action string) error {
	verifier.token = token
	verifier.remoteIP = remoteIP
	verifier.action = action
	return nil
}

func TestPublicCodeRoutesBindDistinctTurnstileActions(t *testing.T) {
	tests := []struct {
		path           string
		expectedAction string
	}{
		{path: "/api/v1/auth/signup/code", expectedAction: security.TurnstileActionSignupCode},
		{path: "/api/v1/auth/password/code", expectedAction: security.TurnstileActionPasswordCode},
	}
	for _, test := range tests {
		t.Run(test.expectedAction, func(t *testing.T) {
			verifier := &recordingVerifier{}
			handler := New(Options{Identity: &fakeIdentityService{}, HumanVerifier: verifier})
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(`{"email":"person@example.com","turnstile_token":"bound-token"}`))
			request.Header.Set("Content-Type", "application/json")
			request.RemoteAddr = "192.0.2.25:4321"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusAccepted {
				t.Fatalf("expected 202, got %d: %s", response.Code, response.Body.String())
			}
			if verifier.token != "bound-token" || verifier.remoteIP != "192.0.2.25" || verifier.action != test.expectedAction {
				t.Fatalf("unexpected verification binding: %#v", verifier)
			}
		})
	}
}

func TestLoginUsesCloudflareClientIPOnlyWhenProxyHeadersAreTrusted(t *testing.T) {
	tests := []struct {
		name       string
		trustProxy bool
		expectedIP string
	}{
		{name: "trusted edge", trustProxy: true, expectedIP: "203.0.113.25"},
		{name: "untrusted direct request", trustProxy: false, expectedIP: "192.0.2.10"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeIdentityService{
				user:  identity.User{ID: "user-1", Email: "person@example.com", Role: "user"},
				token: "session-token",
			}
			handler := New(Options{Identity: service, TrustProxyHeaders: test.trustProxy})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"person@example.com","password":"valid-password"}`))
			request.RemoteAddr = "192.0.2.10:43210"
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("CF-Connecting-IP", "203.0.113.25")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
			}
			if service.loginIP != test.expectedIP {
				t.Fatalf("expected remote IP %q, got %q", test.expectedIP, service.loginIP)
			}
		})
	}
}

func TestSignupCodeReturnsUniformAcceptedResponse(t *testing.T) {
	service := &fakeIdentityService{}
	handler := New(Options{Identity: service, HumanVerifier: acceptingVerifier{}, AllowedOrigins: []string{"http://127.0.0.1:3000"}})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup/code", strings.NewReader(`{"email":"person@example.com","turnstile_token":"token"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:3000")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", response.Code, response.Body.String())
	}
	if service.requestEmail != "person@example.com" || service.purpose != identity.PurposeSignup {
		t.Fatalf("unexpected code request: %q %q", service.requestEmail, service.purpose)
	}
}

func TestCloseAccountCodeUsesAuthenticatedEmailAndUserBinding(t *testing.T) {
	service := &fakeIdentityService{user: identity.User{ID: "user-1", Email: "person@example.com", Role: "user"}}
	handler := New(Options{Identity: service})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/account/close/code", nil)
	request.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "current-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", response.Code, response.Body.String())
	}
	if service.requestEmail != "person@example.com" || service.purpose != identity.PurposeAccountClose || service.requestUserID != "user-1" {
		t.Fatalf("close challenge was not bound to the authenticated user: %#v", service)
	}
}

func TestCloseAccountRejectsStaleNoticeWithoutCallingIdentity(t *testing.T) {
	service := &fakeIdentityService{user: identity.User{ID: "user-1", Email: "person@example.com", Role: "user"}}
	handler := New(Options{Identity: service})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/account/close", strings.NewReader(`{"code":"123456","notice_version":"stale"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "current-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "NOTICE_REQUIRED") {
		t.Fatalf("expected NOTICE_REQUIRED, got %d: %s", response.Code, response.Body.String())
	}
	if service.closedUserID != "" {
		t.Fatal("identity close must not run for a stale notice version")
	}
}

func TestCloseAccountClearsSessionCookiesAfterVerifiedClose(t *testing.T) {
	service := &fakeIdentityService{user: identity.User{ID: "user-1", Email: "person@example.com", Role: "user"}}
	handler := New(Options{Identity: service})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/account/close", strings.NewReader(`{"code":"123456","notice_version":"2026-08-07"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "close-request-1")
	request.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "current-session"})
	request.AddCookie(&http.Cookie{Name: identity.ReauthCookieName, Value: "confirmed-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if service.closedUserID != "user-1" || service.closedEmail != "person@example.com" || service.closedCode != "123456" ||
		service.closedNotice != "2026-08-07" || service.closedRequestID != "close-request-1" {
		t.Fatalf("unexpected close request: %#v", service)
	}
	cookies := response.Result().Cookies()
	for _, name := range []string{identity.SessionCookieName, identity.ReauthCookieName} {
		cleared := false
		for _, cookie := range cookies {
			if cookie.Name == name && cookie.Value == "" && cookie.MaxAge < 0 {
				cleared = true
			}
		}
		if !cleared {
			t.Fatalf("%s was not cleared: %#v", name, cookies)
		}
	}
}

func TestAuthRejectsCrossOriginPost(t *testing.T) {
	handler := New(Options{Identity: &fakeIdentityService{}, HumanVerifier: acceptingVerifier{}, AllowedOrigins: []string{"http://127.0.0.1:3000"}})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup/code", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.invalid")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.Code)
	}
}

func TestProductionRejectsPostWithoutOrigin(t *testing.T) {
	handler := New(Options{
		Identity: &fakeIdentityService{}, HumanVerifier: acceptingVerifier{},
		AllowedOrigins: []string{"https://display.example.test"}, SecureCookies: true,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"person@example.com","password":"not-the-password"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "ORIGIN_NOT_ALLOWED") {
		t.Fatalf("expected missing production Origin to return 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCORSPreflightAllowsIdempotencyKey(t *testing.T) {
	handler := New(Options{AllowedOrigins: []string{"https://ops.example.test"}})
	request := httptest.NewRequest(http.MethodOptions, "/admin/v1/imports/works", nil)
	request.Header.Set("Origin", "https://ops.example.test")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "content-type,idempotency-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !strings.Contains(response.Header().Get("Access-Control-Allow-Headers"), "Idempotency-Key") {
		t.Fatalf("unexpected preflight response %d: %#v", response.Code, response.Header())
	}
}

func TestCORSPreflightAllowsPutForAdminConfiguration(t *testing.T) {
	handler := New(Options{AllowedOrigins: []string{"https://ops.example.test"}})
	request := httptest.NewRequest(http.MethodOptions, "/admin/v1/site-settings/discovery-mix", nil)
	request.Header.Set("Origin", "https://ops.example.test")
	request.Header.Set("Access-Control-Request-Method", http.MethodPut)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !strings.Contains(response.Header().Get("Access-Control-Allow-Methods"), "PUT") {
		t.Fatalf("expected PUT in preflight methods, got %d: %q", response.Code, response.Header().Get("Access-Control-Allow-Methods"))
	}
}

func TestAuthRequiresJSON(t *testing.T) {
	handler := New(Options{Identity: &fakeIdentityService{}, HumanVerifier: acceptingVerifier{}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("email=x")))
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d", response.Code)
	}
}

func TestLoginUsesGenericCredentialError(t *testing.T) {
	handler := New(Options{Identity: &fakeIdentityService{err: identity.ErrInvalidCredentials, token: "must-not-be-issued"}})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"person@example.com","password":"not-the-password"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "INVALID_CREDENTIALS") {
		t.Fatalf("unexpected login response: %d %s", response.Code, response.Body.String())
	}
	if len(response.Result().Cookies()) != 0 || strings.Contains(response.Body.String(), "must-not-be-issued") {
		t.Fatal("rejected credentials must never issue a session cookie or token")
	}
}

func TestLogoutRequiresRevocationConfirmation(t *testing.T) {
	for _, test := range []struct {
		name      string
		token     string
		noService bool
		canceled  bool
		err       error
		status    int
	}{
		{name: "absent cookie", status: http.StatusNoContent},
		{name: "absent service and cookie", noService: true, status: http.StatusNoContent},
		{name: "confirmed revocation", token: "test-session", status: http.StatusNoContent},
		{name: "service unavailable", token: "test-session", noService: true, status: http.StatusServiceUnavailable},
		{name: "database failure", token: "test-session", err: errors.New("database-private-detail"), status: http.StatusServiceUnavailable},
		{name: "deadline exceeded", token: "test-session", err: context.DeadlineExceeded, status: http.StatusServiceUnavailable},
		{name: "request canceled", token: "test-session", err: context.Canceled, status: http.StatusServiceUnavailable},
		{name: "expired context cannot confirm success", token: "test-session", canceled: true, status: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeIdentityService{err: test.err}
			options := Options{Identity: service}
			if test.noService {
				options.Identity = nil
			}
			handler := New(options)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
			if test.canceled {
				ctx, cancel := context.WithCancel(request.Context())
				cancel()
				request = request.WithContext(ctx)
			}
			if test.token != "" {
				request.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: test.token})
			}
			request.AddCookie(&http.Cookie{Name: identity.ReauthCookieName, Value: "test-reauth"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("expected HTTP %d, got %d", test.status, response.Code)
			}
			if !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("logout must not be cached")
			}
			if test.status == http.StatusNoContent {
				cookies := response.Result().Cookies()
				if len(cookies) != 2 || response.Body.Len() != 0 {
					t.Fatal("confirmed logout must clear both cookies without a body")
				}
				for _, cookie := range cookies {
					if cookie.Value != "" || cookie.MaxAge != -1 || !cookie.HttpOnly || cookie.Path != "/" {
						t.Fatal("logout cookie is not expired safely")
					}
				}
				if cookies[0].Name != identity.SessionCookieName || cookies[1].Name != identity.ReauthCookieName {
					t.Fatal("logout must clear session and recent-auth cookies")
				}
			} else if len(response.Result().Cookies()) != 0 || !strings.Contains(response.Body.String(), "LOGOUT_UNAVAILABLE") {
				t.Fatal("unconfirmed logout must keep cookies for retry and return an explicit error")
			}
			if strings.Contains(response.Body.String(), "database-private-detail") || strings.Contains(response.Body.String(), "test-session") {
				t.Fatal("private logout details leaked")
			}
			if test.token != "" && !test.noService {
				if service.logoutCalls != 1 || service.logoutToken != test.token || service.logoutDeadline.IsZero() || time.Until(service.logoutDeadline) > 2*time.Second {
					t.Fatal("revocation requires the exact token and a bounded two-second context")
				}
			} else if service.logoutCalls != 0 {
				t.Fatal("absent session must not invoke revocation")
			}
		})
	}
}

func TestReauthenticateSetsShortLivedHTTPOnlyCookie(t *testing.T) {
	service := &fakeRecentIdentityService{
		fakeIdentityService: &fakeIdentityService{},
		reauthToken:         "signed-reauth-token",
	}
	handler := New(Options{Identity: service})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reauth", strings.NewReader(`{"password":"a-long-test-password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "current-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || service.sessionToken != "current-session" || service.password != "a-long-test-password" {
		t.Fatalf("unexpected reauthentication response %d: %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != identity.ReauthCookieName || cookies[0].Value != "signed-reauth-token" ||
		!cookies[0].HttpOnly || cookies[0].MaxAge != int(identity.ReauthDuration.Seconds()) {
		t.Fatalf("unexpected reauthentication cookie: %#v", cookies)
	}
	if strings.Contains(response.Body.String(), "signed-reauth-token") {
		t.Fatal("reauthentication token leaked into JSON response")
	}
}

func TestReauthenticateRejectsInvalidPasswordWithoutCookie(t *testing.T) {
	service := &fakeRecentIdentityService{
		fakeIdentityService: &fakeIdentityService{},
		reauthErr:           identity.ErrInvalidCredentials,
	}
	handler := New(Options{Identity: service})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reauth", strings.NewReader(`{"password":"wrong-password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "current-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "INVALID_CREDENTIALS") || len(response.Result().Cookies()) != 0 {
		t.Fatalf("unexpected invalid-password response %d: %s", response.Code, response.Body.String())
	}
}

func TestHighRiskAdminMutationRequiresRecentAuthentication(t *testing.T) {
	service := &fakeRecentIdentityService{
		fakeIdentityService: &fakeIdentityService{user: identity.User{
			ID: "90000000-0000-4000-8000-000000000001", Email: "operator@example.test", Role: "admin",
		}},
	}
	repository := &fakeOperations{}
	handler := New(Options{Identity: service, Operations: repository})
	const sourceID = "10000000-0000-4000-8000-000000000001"
	const targetID = "10000000-0000-4000-8000-000000000002"
	body := `{"entity_type":"work","target_entity_id":"` + targetID + `","reason":"duplicate record"}`

	request := authenticatedAdminRequest(http.MethodPost, "/admin/v1/entities/"+sourceID+"/merge", body)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "REAUTH_REQUIRED") || repository.mergeSource != "" {
		t.Fatalf("unguarded high-risk mutation response %d: %s", response.Code, response.Body.String())
	}

	confirmedRequest := authenticatedAdminRequest(http.MethodPost, "/admin/v1/entities/"+sourceID+"/merge", body)
	confirmedRequest.AddCookie(&http.Cookie{Name: identity.ReauthCookieName, Value: "confirmed-token"})
	confirmedResponse := httptest.NewRecorder()
	handler.ServeHTTP(confirmedResponse, confirmedRequest)
	if confirmedResponse.Code != http.StatusOK || repository.mergeSource != sourceID ||
		service.validatedSession != "session" || service.validatedReauth != "confirmed-token" {
		t.Fatalf("confirmed high-risk mutation response %d: %s", confirmedResponse.Code, confirmedResponse.Body.String())
	}
}

func TestRecentAuthenticationRoutePolicy(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodPost, "/admin/v1/entities/10000000-0000-4000-8000-000000000001/publish", true},
		{http.MethodPost, "/admin/v1/entities/10000000-0000-4000-8000-000000000001/hide", true},
		{http.MethodPost, "/admin/v1/entities/10000000-0000-4000-8000-000000000001/merge", true},
		{http.MethodPost, "/admin/v1/review-tasks/10000000-0000-4000-8000-000000000001/approve", true},
		{http.MethodPost, "/admin/v1/review-tasks/10000000-0000-4000-8000-000000000001/reject", true},
		{http.MethodPost, "/admin/v1/review-tasks/10000000-0000-4000-8000-000000000001/reassign", true},
		{http.MethodPost, "/admin/v1/users/10000000-0000-4000-8000-000000000001/status", true},
		{http.MethodPost, "/admin/v1/users/10000000-0000-4000-8000-000000000001/role", true},
		{http.MethodPut, "/admin/v1/site-settings/discovery-mix", true},
		{http.MethodPost, "/admin/v1/invitations", true},
		{http.MethodPost, "/admin/v1/invitations/10000000-0000-4000-8000-000000000001/revoke", true},
		{http.MethodPost, "/admin/v1/takedowns/10000000-0000-4000-8000-000000000001/complete", true},
		{http.MethodPost, "/admin/v1/media/manifests", true},
		{http.MethodPost, "/admin/v1/media/primary/replace", true},
		{http.MethodGet, "/admin/v1/media/primary", false},
		{http.MethodGet, "/admin/v1/media/usage", false},
		{http.MethodPost, "/admin/v1/media/usage/reviews", true},
		{http.MethodGet, "/admin/v1/media/upload-control", false},
		{http.MethodPost, "/admin/v1/media/upload-control/commands", true},
		{http.MethodPost, "/admin/v1/editorial-recommendations", true},
		{http.MethodPut, "/admin/v1/editorial-recommendations/10000000-0000-4000-8000-000000000001", true},
		{http.MethodDelete, "/admin/v1/editorial-recommendations/10000000-0000-4000-8000-000000000001", true},
		{http.MethodPost, "/admin/v1/conflicts/10000000-0000-4000-8000-000000000001/resolve", true},
		{http.MethodPost, "/admin/v1/review-tasks/10000000-0000-4000-8000-000000000001/claim", false},
		{http.MethodPost, "/admin/v1/takedowns", false},
		{http.MethodPost, "/admin/v1/works", false},
		{http.MethodPost, "/admin/v1/imports/works", false},
		{http.MethodGet, "/admin/v1/users/10000000-0000-4000-8000-000000000001/status", false},
		{http.MethodPost, "/api/v1/auth/reauth", false},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			if got := recentAuthRequired(request); got != test.want {
				t.Fatalf("recentAuthRequired=%v, want %v", got, test.want)
			}
		})
	}
}

func TestSignupSetsHTTPOnlySessionCookie(t *testing.T) {
	service := &fakeIdentityService{
		user:  identity.User{ID: "user-1", Email: "person@example.com", Role: "user"},
		token: "session-token",
	}
	handler := New(Options{Identity: service})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup/verify", strings.NewReader(`{"email":"person@example.com","code":"123456","password":"a-long-test-password"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != identity.SessionCookieName || !cookies[0].HttpOnly {
		t.Fatalf("missing secure session cookie: %#v", cookies)
	}
}

func TestAcceptInvitationSetsRoleSessionCookie(t *testing.T) {
	service := &fakeIdentityService{
		user:  identity.User{ID: "user-1", Email: "person@example.com", Role: "editor"},
		token: "session-token",
	}
	handler := New(Options{Identity: service})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/invitations/accept", strings.NewReader(`{"email":"person@example.com","code":"123456","password":"a-long-test-password"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"role":"editor"`) {
		t.Fatalf("expected invited role in response: %s", response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != identity.SessionCookieName || !cookies[0].HttpOnly {
		t.Fatalf("missing invitation session cookie: %#v", cookies)
	}
}

func TestPublicCodeRateLimitUsesUniformAcceptedResponse(t *testing.T) {
	handler := New(Options{Identity: &fakeIdentityService{err: identity.ErrRateLimited}, HumanVerifier: acceptingVerifier{}})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/code", strings.NewReader(`{"email":"person@example.com","turnstile_token":"token"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), "如果信息有效") {
		t.Fatalf("expected uniform 202, got %d: %s", response.Code, response.Body.String())
	}
}

func TestPublicCodeEmailGuardFailuresRemainUniformAcceptedResponses(t *testing.T) {
	for _, guardedError := range []error{identity.ErrEmailDailyLimit, identity.ErrEmailCircuitOpen, identity.ErrEmailUnavailable} {
		handler := New(Options{Identity: &fakeIdentityService{err: guardedError}, HumanVerifier: acceptingVerifier{}})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/code", strings.NewReader(`{"email":"person@example.com","turnstile_token":"token"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), "如果信息有效") {
			t.Fatalf("guard error %v leaked through public response: %d %s", guardedError, response.Code, response.Body.String())
		}
	}
}

func TestPublicCodeInfrastructureErrorDoesNotLeakAccountOrErrorDetails(t *testing.T) {
	const secret = "smtp failure for known-person@example.test"
	var output bytes.Buffer
	handler := New(Options{
		Identity: &fakeIdentityService{err: errors.New(secret)}, HumanVerifier: acceptingVerifier{},
		Logger: slog.New(slog.NewJSONHandler(&output, nil)),
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/code", strings.NewReader(`{"email":"known-person@example.test","turnstile_token":"token"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), "如果信息有效") {
		t.Fatalf("expected uniform 202, got %d: %s", response.Code, response.Body.String())
	}
	logLine := output.String()
	if strings.Contains(logLine, secret) || strings.Contains(logLine, "known-person@example.test") || !strings.Contains(logLine, `"error_class":"internal"`) {
		t.Fatalf("unsafe challenge failure log: %s", logLine)
	}
}

func TestPublicCodeStillRejectsMalformedEmail(t *testing.T) {
	handler := New(Options{Identity: &fakeIdentityService{err: identity.ErrInvalidInput}, HumanVerifier: acceptingVerifier{}})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup/code", strings.NewReader(`{"email":"not-an-email","turnstile_token":"token"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "INVALID_INPUT") {
		t.Fatalf("expected invalid email 400, got %d: %s", response.Code, response.Body.String())
	}
}
