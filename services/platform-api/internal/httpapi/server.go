package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/account"
	"self-deepsearch/services/platform-api/internal/catalog"
	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/operations"
	"self-deepsearch/services/platform-api/internal/security"
)

type DatabaseChecker interface {
	Ping(context.Context) error
	SchemaVersion(context.Context) (int, error)
}

type EmailDeliveryMetricsProvider interface {
	EmailDeliveryMetrics(context.Context, time.Time) (identity.EmailDeliveryMetrics, error)
}

type IdentityService interface {
	RequestCode(context.Context, string, string, *string, string) error
	Signup(context.Context, string, string, string, string) (identity.User, string, error)
	Login(context.Context, string, string, string, string) (identity.User, string, error)
	Authenticate(context.Context, string) (identity.User, error)
	Logout(context.Context, string) error
	ResetPassword(context.Context, string, string, string) error
	CloseAccount(context.Context, string, string, string, string, string) error
	CreateInvitation(context.Context, string, string, string, string, string, string, string) (identity.Invitation, error)
	ListInvitations(context.Context, string, int) ([]identity.Invitation, error)
	RevokeInvitation(context.Context, string, string, string, string, time.Time) (identity.Invitation, error)
	AcceptInvitation(context.Context, string, string, string, string, string) (identity.User, string, error)
}

type Options struct {
	Service                  string
	Version                  string
	Database                 DatabaseChecker
	Catalog                  catalog.Repository
	Account                  account.Repository
	Identity                 IdentityService
	Operations               operations.Repository
	HumanVerifier            security.HumanVerifier
	AllowedOrigins           []string
	SecureCookies            bool
	TrustProxyHeaders        bool
	Logger                   *slog.Logger
	MetricsToken             string
	EmailMetrics             EmailDeliveryMetricsProvider
	EmailDailyLimit          int
	MediaPublicBaseURL       string
	MediaDeliveryMode        string
	MediaDeliveryDynamicMode string
	MediaEdgePolicyMode      string
	MediaEdgePolicyToken     string
	// RequireRecentAuth enables the short-lived password confirmation gate for
	// high-risk operator mutations. When omitted, a concrete identity service
	// that implements identity.ReauthenticationService enables it automatically.
	RequireRecentAuth bool
	Now               func() time.Time
}

type Server struct {
	service              string
	version              string
	database             DatabaseChecker
	catalog              catalog.Repository
	account              account.Repository
	identity             IdentityService
	operations           operations.Repository
	humanVerifier        security.HumanVerifier
	allowedOrigins       []string
	secureCookies        bool
	trustProxyHeaders    bool
	logger               *slog.Logger
	metricsToken         string
	emailMetrics         EmailDeliveryMetricsProvider
	emailDailyLimit      int
	mediaPublicBaseURL   string
	mediaDefaultOnly     bool
	mediaDeliveryDynamic bool
	mediaEdgePolicy      bool
	mediaEdgePolicyToken string
	requireRecentAuth    bool
	metrics              *requestMetrics
	now                  func() time.Time
}

type contextKey string

const (
	requestIDKey    contextKey = "request_id"
	routePatternKey contextKey = "route_pattern"
)

type routePatternState struct {
	pattern string
}

func New(options Options) http.Handler {
	server := &Server{
		service:              defaultString(options.Service, "platform-api"),
		version:              defaultString(options.Version, "dev"),
		database:             options.Database,
		catalog:              options.Catalog,
		account:              options.Account,
		identity:             options.Identity,
		operations:           options.Operations,
		humanVerifier:        options.HumanVerifier,
		allowedOrigins:       options.AllowedOrigins,
		secureCookies:        options.SecureCookies,
		trustProxyHeaders:    options.TrustProxyHeaders,
		logger:               options.Logger,
		metricsToken:         strings.TrimSpace(options.MetricsToken),
		emailMetrics:         options.EmailMetrics,
		emailDailyLimit:      options.EmailDailyLimit,
		mediaPublicBaseURL:   strings.TrimRight(strings.TrimSpace(options.MediaPublicBaseURL), "/"),
		mediaDefaultOnly:     options.MediaDeliveryMode != "" && options.MediaDeliveryMode != "normal",
		mediaDeliveryDynamic: options.MediaDeliveryDynamicMode == "enforce",
		mediaEdgePolicy:      options.MediaEdgePolicyMode == "enforce",
		mediaEdgePolicyToken: strings.TrimSpace(options.MediaEdgePolicyToken),
		requireRecentAuth:    options.RequireRecentAuth,
		now:                  options.Now,
	}
	if isTypedNil(server.database) {
		server.database = nil
	}
	if isTypedNil(server.catalog) {
		server.catalog = nil
	}
	if isTypedNil(server.account) {
		server.account = nil
	}
	if isTypedNil(server.identity) {
		server.identity = nil
	}
	if isTypedNil(server.operations) {
		server.operations = nil
	}
	if isTypedNil(server.emailMetrics) {
		server.emailMetrics = nil
	}
	if isTypedNil(server.humanVerifier) {
		server.humanVerifier = nil
	}
	if !server.requireRecentAuth {
		// Keep existing lightweight test doubles and degraded page-shell mode
		// compatible, while enabling the gate automatically for the real identity
		// service used by the API process.
		_, server.requireRecentAuth = server.identity.(identity.ReauthenticationService)
	}
	if server.logger == nil {
		server.logger = slog.Default()
	}
	if server.now == nil {
		server.now = time.Now
	}
	if server.emailDailyLimit < 1 {
		server.emailDailyLimit = identity.DefaultEmailDeliveryPolicy().DailyLimit
	}
	server.metrics = newRequestMetrics(server.now())

	core := server.internalMediaDeliveryPolicy(server.edgeMediaDeliveryPolicy(server.routes()))
	return server.requestIDMiddleware(
		server.recoverMiddleware(
			server.routePatternMiddleware(
				server.metricsMiddleware(server.accessLogMiddleware(
					server.securityHeadersMiddleware(server.corsMiddleware(server.originMiddleware(core))),
				)),
			),
		),
	)
}

func isTypedNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (s *Server) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.secureCookies {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		isMutation := r.Method != http.MethodGet && r.Method != http.MethodHead
		if isMutation || strings.HasPrefix(r.URL.Path, "/api/v1/me") || strings.HasPrefix(r.URL.Path, "/admin/") || strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() http.Handler {
	router := chi.NewRouter()
	router.Use(s.captureRoutePatternMiddleware)
	router.Use(s.recentAuthMiddleware)
	router.Get("/healthz", s.health)
	router.Get("/readyz", s.readiness)
	router.Get("/metrics", s.prometheusMetrics)
	router.Get("/api/v1/site/home", s.home)
	router.Get("/api/v1/site/editorial", s.editorialWorks)
	router.Get("/api/v1/site/sitemap", s.sitemap)
	router.Get("/api/v1/site/redirect", s.publicRedirect)
	router.Get("/api/v1/search/works", s.searchWorks)
	router.Get("/api/v1/works/popular", s.popularWorks)
	router.Get("/api/v1/works/latest", s.latestWorks)
	router.Get("/api/v1/works/recent", s.recentlyAddedWorks)
	router.Get("/api/v1/works/{slug}", s.workDetail)
	router.Get("/api/v1/performers", s.performerList)
	router.Get("/api/v1/performers/{slug}", s.performerDetail)
	router.Get("/api/v1/studios", s.studioList)
	router.Get("/api/v1/studios/{slug}", s.studioDetail)
	router.Post("/api/v1/metrics/page-view", s.pageView)
	router.Post("/api/v1/auth/signup/code", s.signupCode)
	router.Post("/api/v1/auth/signup/verify", s.signupVerify)
	router.Post("/api/v1/auth/login", s.login)
	router.Post("/api/v1/auth/logout", s.logout)
	router.Post("/api/v1/auth/reauth", s.reauthenticate)
	router.Post("/api/v1/auth/password/code", s.passwordCode)
	router.Post("/api/v1/auth/password/reset", s.passwordReset)
	router.Post("/api/v1/auth/invitations/accept", s.acceptInvitation)
	router.Post("/api/v1/account/close/code", s.closeAccountCode)
	router.Post("/api/v1/account/close", s.closeAccount)
	router.Get("/api/v1/me", s.me)
	router.Get("/api/v1/me/favorites", s.listFavorites)
	router.Post("/api/v1/me/favorites/{workID}", s.addFavorite)
	router.Delete("/api/v1/me/favorites/{workID}", s.removeFavorite)
	router.Get("/api/v1/me/follows", s.listFollows)
	router.Get("/api/v1/me/followed-works", s.listFollowedWorks)
	router.Post("/api/v1/me/follows/{performerID}", s.addFollow)
	router.Delete("/api/v1/me/follows/{performerID}", s.removeFollow)
	router.Get("/api/v1/me/hidden-works", s.listHiddenWorks)
	router.Post("/api/v1/me/hidden-works/{workID}", s.hideWork)
	router.Delete("/api/v1/me/hidden-works/{workID}", s.restoreWork)
	router.Get("/api/v1/me/history", s.listHistory)
	router.Delete("/api/v1/me/history", s.clearHistory)
	router.Post("/api/v1/feedback", s.createFeedback)
	router.Get("/api/v1/me/feedback", s.listFeedback)
	router.Get("/admin/v1/feedback", s.adminListFeedback)
	router.Post("/admin/v1/feedback/{feedbackID}/review", s.adminReviewFeedback)
	router.Get("/admin/v1/editorial-recommendations", s.adminListEditorialRecommendations)
	router.Post("/admin/v1/editorial-recommendations", s.adminCreateEditorialRecommendation)
	router.Put("/admin/v1/editorial-recommendations/{recommendationID}", s.adminUpdateEditorialRecommendation)
	router.Delete("/admin/v1/editorial-recommendations/{recommendationID}", s.adminRemoveEditorialRecommendation)
	router.Get("/admin/v1/site-settings/discovery-mix", s.adminGetDiscoveryMixRule)
	router.Put("/admin/v1/site-settings/discovery-mix", s.adminUpdateDiscoveryMixRule)
	router.Get("/admin/v1/review-tasks", s.adminListReviewTasks)
	router.Post("/admin/v1/review-tasks/{taskID}/claim", s.adminClaimReviewTask)
	router.Post("/admin/v1/review-tasks/{taskID}/reassign", s.adminReassignReviewTask)
	router.Post("/admin/v1/review-tasks/{taskID}/approve", s.adminApproveReviewTask)
	router.Post("/admin/v1/review-tasks/{taskID}/reject", s.adminRejectReviewTask)
	router.Get("/admin/v1/conflicts", s.adminListConflicts)
	router.Post("/admin/v1/conflicts/{conflictID}/resolve", s.adminResolveConflict)
	router.Post("/admin/v1/imports/works/preflight", s.adminPreflightWorkCSV)
	router.Post("/admin/v1/imports/works", s.adminImportWorkCSV)
	router.Get("/admin/v1/imports/works", s.adminListWorkImports)
	router.Post("/admin/v1/works", s.adminCreateWork)
	router.Post("/admin/v1/performers", s.adminCreatePerformer)
	router.Post("/admin/v1/studios", s.adminCreateStudio)
	router.Post("/admin/v1/entities/{entityID}/revisions", s.adminCreateRevision)
	router.Post("/admin/v1/entities/{entityID}/publish", s.adminPublishEntity)
	router.Post("/admin/v1/entities/{entityID}/hide", s.adminHideEntity)
	router.Post("/admin/v1/entities/{entityID}/merge", s.adminMergeEntity)
	router.Get("/admin/v1/entities", s.adminListEntities)
	router.Get("/admin/v1/entities/{entityID}/revisions", s.adminListRevisions)
	router.Get("/admin/v1/takedowns", s.adminListTakedowns)
	router.Post("/admin/v1/takedowns", s.adminCreateTakedown)
	router.Post("/admin/v1/takedowns/{takedownID}/complete", s.adminCompleteTakedown)
	router.Get("/admin/v1/users", s.adminListUsers)
	router.Post("/admin/v1/users/{userID}/role", s.adminChangeUserRole)
	router.Post("/admin/v1/users/{userID}/status", s.adminChangeUserStatus)
	router.Get("/admin/v1/invitations", s.adminListInvitations)
	router.Post("/admin/v1/invitations", s.adminCreateInvitation)
	router.Post("/admin/v1/invitations/{invitationID}/revoke", s.adminRevokeInvitation)
	router.Get("/admin/v1/system/health", s.adminSystemHealth)
	router.Get("/admin/v1/audit-logs", s.adminListAuditLogs)
	router.Post("/admin/v1/media/manifests", s.adminRegisterMediaManifest)
	router.Get("/admin/v1/media/primary", s.adminGetPrimaryMedia)
	router.Get("/admin/v1/media/usage", s.adminGetMediaUsage)
	router.Post("/admin/v1/media/usage/reviews", s.adminSubmitMediaUsageReview)
	router.Get("/admin/v1/media/upload-control", s.adminGetMediaUploadControl)
	router.Post("/admin/v1/media/upload-control/commands", s.adminSubmitMediaUploadCommand)
	router.Post("/admin/v1/media/primary/replace", s.adminReplacePrimaryMedia)
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "请求的接口不存在")
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "请求方法不被允许")
	})
	return router
}

// recentAuthMiddleware protects the small set of operator mutations whose
// consequences are difficult to reverse. It intentionally runs before the
// individual handlers so a newly added handler cannot accidentally omit the
// password-confirmation check when its path is included below.
func (s *Server) recentAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.requireRecentAuth || !recentAuthRequired(r) {
			next.ServeHTTP(w, r)
			return
		}
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
		if _, err := s.identity.Authenticate(r.Context(), sessionCookie.Value); err != nil {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "登录状态已失效")
			return
		}
		reauthCookie, err := r.Cookie(identity.ReauthCookieName)
		if err != nil || strings.TrimSpace(reauthCookie.Value) == "" ||
			reauthenticator.ValidateReauthentication(sessionCookie.Value, reauthCookie.Value) != nil {
			writeError(w, r, http.StatusUnauthorized, "REAUTH_REQUIRED", "高风险操作需要近期密码确认")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// recentAuthRequired is deliberately an allow-list. Routine editorial data
// entry and read-only queue operations remain usable with the normal operator
// session; destructive publication, rights, identity and configuration
// changes require a fresh password confirmation.
func recentAuthRequired(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return false
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	if !strings.HasPrefix(path, "/admin/v1/") {
		return false
	}
	if path == "/admin/v1/editorial-recommendations" {
		return r.Method == http.MethodPost
	}
	if strings.HasPrefix(path, "/admin/v1/editorial-recommendations/") {
		return r.Method == http.MethodPut || r.Method == http.MethodDelete
	}
	if path == "/admin/v1/site-settings/discovery-mix" {
		return r.Method == http.MethodPut
	}
	if path == "/admin/v1/media/usage/reviews" || path == "/admin/v1/media/upload-control/commands" {
		return r.Method == http.MethodPost
	}
	if strings.HasPrefix(path, "/admin/v1/review-tasks/") {
		return r.Method == http.MethodPost && (strings.HasSuffix(path, "/approve") ||
			strings.HasSuffix(path, "/reject") || strings.HasSuffix(path, "/reassign"))
	}
	if strings.HasPrefix(path, "/admin/v1/conflicts/") && strings.HasSuffix(path, "/resolve") {
		return r.Method == http.MethodPost
	}
	if strings.HasPrefix(path, "/admin/v1/entities/") {
		return r.Method == http.MethodPost && (strings.HasSuffix(path, "/publish") ||
			strings.HasSuffix(path, "/hide") || strings.HasSuffix(path, "/merge"))
	}
	if strings.HasPrefix(path, "/admin/v1/takedowns/") && strings.HasSuffix(path, "/complete") {
		return r.Method == http.MethodPost
	}
	if strings.HasPrefix(path, "/admin/v1/users/") {
		return r.Method == http.MethodPost && (strings.HasSuffix(path, "/role") || strings.HasSuffix(path, "/status"))
	}
	if path == "/admin/v1/invitations" {
		return r.Method == http.MethodPost
	}
	if strings.HasPrefix(path, "/admin/v1/invitations/") && strings.HasSuffix(path, "/revoke") {
		return r.Method == http.MethodPost
	}
	return (path == "/admin/v1/media/manifests" || path == "/admin/v1/media/primary/replace") && r.Method == http.MethodPost
}

func (s *Server) originMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if (s.secureCookies && origin == "") || (origin != "" && !slices.Contains(s.allowedOrigins, origin)) {
				writeError(w, r, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "请求来源不被允许")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !validRequestID(requestID) {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("-_.:", character) {
			continue
		}
		return false
	}
	return true
}

func (s *Server) routePatternMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := &routePatternState{}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), routePatternKey, state)))
	})
}

func (s *Server) captureRoutePatternMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			state, _ := r.Context().Value(routePatternKey).(*routePatternState)
			if state != nil {
				state.pattern = chi.RouteContext(r.Context()).RoutePattern()
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func routePatternFromContext(ctx context.Context) string {
	state, _ := ctx.Value(routePatternKey).(*routePatternState)
	if state == nil || state.pattern == "" {
		return "unmatched"
	}
	return state.pattern
}

func (s *Server) accessLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		writer := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(writer, r)
		s.logger.InfoContext(r.Context(), "http_request",
			"request_id", requestIDFromContext(r.Context()),
			"method", r.Method,
			"route", routePatternFromContext(r.Context()),
			"status", writer.status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.ErrorContext(r.Context(), "http_panic",
					"request_id", requestIDFromContext(r.Context()),
					"error_class", "internal",
				)
				writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "服务暂时不可用")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := origin != "" && slices.Contains(s.allowedOrigins, origin)
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key, X-Request-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			if !allowed {
				writeError(w, r, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "请求来源不被允许")
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

func newRequestID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buffer)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
