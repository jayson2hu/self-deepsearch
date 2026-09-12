package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/logsafe"
	"self-deepsearch/services/platform-api/internal/security"
)

const accountCloseNoticeVersion = "2026-08-07"

type codeRequest struct {
	Email          string `json:"email"`
	TurnstileToken string `json:"turnstile_token"`
}

type signupRequest struct {
	Email    string `json:"email"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type reauthenticateRequest struct {
	Password string `json:"password"`
}

type resetPasswordRequest struct {
	Email    string `json:"email"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

type closeAccountRequest struct {
	Code          string `json:"code"`
	NoticeVersion string `json:"notice_version"`
}

type invitationAcceptRequest struct {
	Email    string `json:"email"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

type messageResponse struct {
	Message string `json:"message"`
}

type userResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type sessionResponse struct {
	User userResponse `json:"user"`
}

func (s *Server) signupCode(w http.ResponseWriter, r *http.Request) {
	var request codeRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	if !s.verifyHuman(w, r, request.TurnstileToken, security.TurnstileActionSignupCode) {
		return
	}
	s.requestCode(w, r, request.Email, identity.PurposeSignup, nil)
}

func (s *Server) passwordCode(w http.ResponseWriter, r *http.Request) {
	var request codeRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	if !s.verifyHuman(w, r, request.TurnstileToken, security.TurnstileActionPasswordCode) {
		return
	}
	s.requestCode(w, r, request.Email, identity.PurposePasswordReset, nil)
}

func (s *Server) closeAccountCode(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok {
		return
	}
	s.requestCode(w, r, user.Email, identity.PurposeAccountClose, &user.ID)
}

func (s *Server) requestCode(w http.ResponseWriter, r *http.Request, email, purpose string, userID *string) {
	if s.identity == nil {
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	err := s.identity.RequestCode(ctx, email, purpose, userID, s.remoteIP(r))
	if purpose != identity.PurposeAccountClose {
		if errors.Is(err, identity.ErrInvalidInput) {
			s.writeIdentityError(w, r, err)
			return
		}
		if err != nil {
			s.logger.WarnContext(r.Context(), "email_challenge_not_delivered",
				"request_id", requestIDFromContext(r.Context()),
				"purpose", purpose,
				"error_class", logsafe.ErrorClass(err),
			)
		}
		writeJSON(w, http.StatusAccepted, messageResponse{Message: "如果信息有效，验证码将发送到对应邮箱"})
		return
	}
	if s.writeIdentityError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusAccepted, messageResponse{Message: "如果信息有效，验证码将发送到对应邮箱"})
}

func (s *Server) signupVerify(w http.ResponseWriter, r *http.Request) {
	var request signupRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	if s.identity == nil {
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
		return
	}
	user, token, err := s.identity.Signup(r.Context(), request.Email, request.Code, request.Password, s.remoteIP(r))
	if s.writeIdentityError(w, r, err) {
		return
	}
	s.setSessionCookie(w, token, sessionDuration(user))
	writeJSON(w, http.StatusCreated, sessionResponse{User: publicUser(user)})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	if s.identity == nil {
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
		return
	}
	user, token, err := s.identity.Login(r.Context(), request.Email, request.Password, s.remoteIP(r), requestIDFromContext(r.Context()))
	if s.writeIdentityError(w, r, err) {
		return
	}
	s.setSessionCookie(w, token, sessionDuration(user))
	writeJSON(w, http.StatusOK, sessionResponse{User: publicUser(user)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(identity.SessionCookieName); err == nil && cookie.Value != "" {
		if s.identity == nil {
			writeError(w, r, http.StatusServiceUnavailable, "LOGOUT_UNAVAILABLE", "退出尚未确认，请稍后重试")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		err := s.identity.Logout(ctx, cookie.Value)
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			s.logger.WarnContext(r.Context(), "session_revocation_unconfirmed",
				"request_id", requestIDFromContext(r.Context()), "error_class", logsafe.ErrorClass(err))
			writeError(w, r, http.StatusServiceUnavailable, "LOGOUT_UNAVAILABLE", "退出尚未确认，请稍后重试")
			return
		}
	}
	s.clearSessionCookie(w)
	s.clearReauthCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// reauthenticate confirms the password for the current server-side session.
// The response never contains the signed token; it is delivered as an
// HttpOnly cookie and is only accepted for the exact session that requested it.
func (s *Server) reauthenticate(w http.ResponseWriter, r *http.Request) {
	reauthenticator, ok := s.identity.(identity.ReauthenticationService)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "REAUTH_UNAVAILABLE", "高风险操作确认服务暂时不可用")
		return
	}
	sessionCookie, err := r.Cookie(identity.SessionCookieName)
	if err != nil || strings.TrimSpace(sessionCookie.Value) == "" {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "请先登录")
		return
	}
	var request reauthenticateRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	token, err := reauthenticator.Reauthenticate(r.Context(), sessionCookie.Value, request.Password, s.remoteIP(r), requestIDFromContext(r.Context()))
	if s.writeIdentityError(w, r, err) {
		return
	}
	s.setReauthCookie(w, token)
	writeJSON(w, http.StatusOK, messageResponse{Message: "高风险操作确认成功，5 分钟内有效"})
}

func (s *Server) passwordReset(w http.ResponseWriter, r *http.Request) {
	var request resetPasswordRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	if s.identity == nil {
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
		return
	}
	if s.writeIdentityError(w, r, s.identity.ResetPassword(r.Context(), request.Email, request.Code, request.Password)) {
		return
	}
	writeJSON(w, http.StatusOK, messageResponse{Message: "密码已重置，请重新登录"})
}

func (s *Server) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var request invitationAcceptRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	if s.identity == nil {
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
		return
	}
	user, token, err := s.identity.AcceptInvitation(r.Context(), request.Email, request.Code, request.Password, s.remoteIP(r), requestIDFromContext(r.Context()))
	if s.writeIdentityError(w, r, err) {
		return
	}
	s.setSessionCookie(w, token, sessionDuration(user))
	writeJSON(w, http.StatusCreated, sessionResponse{User: publicUser(user)})
}

func (s *Server) closeAccount(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok {
		return
	}
	var request closeAccountRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	if request.NoticeVersion != accountCloseNoticeVersion {
		writeError(w, r, http.StatusBadRequest, "NOTICE_REQUIRED", "请确认最新的关闭账号说明")
		return
	}
	err := s.identity.CloseAccount(r.Context(), user.ID, user.Email, request.Code, request.NoticeVersion, requestIDFromContext(r.Context()))
	if s.writeIdentityError(w, r, err) {
		return
	}
	s.clearSessionCookie(w)
	s.clearReauthCookie(w)
	writeJSON(w, http.StatusOK, messageResponse{Message: "账号已关闭"})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse{User: publicUser(user)})
}

func (s *Server) authenticatedUser(w http.ResponseWriter, r *http.Request) (identity.User, bool) {
	if s.identity == nil {
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
		return identity.User{}, false
	}
	cookie, err := r.Cookie(identity.SessionCookieName)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "请先登录")
		return identity.User{}, false
	}
	user, err := s.identity.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "登录状态已失效")
		return identity.User{}, false
	}
	return user, true
}

func (s *Server) verifyHuman(w http.ResponseWriter, r *http.Request, token, action string) bool {
	if s.humanVerifier == nil {
		writeError(w, r, http.StatusServiceUnavailable, "HUMAN_VERIFICATION_UNAVAILABLE", "验证服务暂时不可用")
		return false
	}
	if err := s.humanVerifier.Verify(r.Context(), token, s.remoteIP(r), action); err != nil {
		writeError(w, r, http.StatusBadRequest, "HUMAN_VERIFICATION_FAILED", "请完成人机验证")
		return false
	}
	return true
}

func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		writeError(w, r, http.StatusUnsupportedMediaType, "JSON_REQUIRED", "请求必须使用 JSON")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "请求内容格式不正确")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "请求只能包含一个 JSON 对象")
		return false
	}
	return true
}

func (s *Server) writeIdentityError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, identity.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "当前账号无权执行此操作")
	case errors.Is(err, identity.ErrConflict):
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", "邀请状态已变化，请刷新后重试")
	case errors.Is(err, identity.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "INVITATION_NOT_FOUND", "未找到邀请")
	case errors.Is(err, identity.ErrInvalidCredentials):
		writeError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "邮箱或密码不正确")
	case errors.Is(err, identity.ErrInvalidChallenge):
		writeError(w, r, http.StatusBadRequest, "INVALID_CODE", "验证码错误或已过期")
	case errors.Is(err, identity.ErrRateLimited):
		writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "请求过于频繁，请稍后再试")
	case errors.Is(err, identity.ErrAuthBusy):
		w.Header().Set("Retry-After", "1")
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_BUSY", "账号服务繁忙，请稍后重试")
	case errors.Is(err, identity.ErrEmailUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "EMAIL_UNAVAILABLE", "邮件服务暂时不可用")
	case errors.Is(err, identity.ErrUnauthenticated):
		writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "请先登录")
	case errors.Is(err, identity.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", "邮箱或密码不符合要求")
	default:
		s.logger.ErrorContext(r.Context(), "identity_operation_failed", "request_id", requestIDFromContext(r.Context()), "error_class", logsafe.ErrorClass(err))
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
	}
	return true
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, duration time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: identity.SessionCookieName, Value: token, Path: "/",
		HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode,
		MaxAge: int(duration.Seconds()), Expires: s.now().Add(duration),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: identity.SessionCookieName, Value: "", Path: "/",
		HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode,
		MaxAge: -1, Expires: time.Unix(1, 0),
	})
}

func (s *Server) setReauthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: identity.ReauthCookieName, Value: token, Path: "/",
		HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode,
		MaxAge: int(identity.ReauthDuration.Seconds()), Expires: s.now().Add(identity.ReauthDuration),
	})
}

func (s *Server) clearReauthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: identity.ReauthCookieName, Value: "", Path: "/",
		HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode,
		MaxAge: -1, Expires: time.Unix(1, 0),
	})
}

func (s *Server) remoteIP(r *http.Request) string {
	if s.trustProxyHeaders {
		if value := net.ParseIP(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); value != nil {
			return value.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func publicUser(user identity.User) userResponse {
	return userResponse{ID: user.ID, Email: user.Email, Role: user.Role}
}

func sessionDuration(user identity.User) time.Duration {
	if user.Role != "user" {
		return 8 * time.Hour
	}
	return 30 * 24 * time.Hour
}
