package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

type adminUserResponse struct {
	ID                 string  `json:"id"`
	Email              string  `json:"email"`
	Role               string  `json:"role"`
	AccountStatus      string  `json:"account_status"`
	CreatedAt          string  `json:"created_at"`
	LastLoginAt        *string `json:"last_login_at"`
	EmailVerifiedAt    *string `json:"email_verified_at"`
	RegistrationOrigin string  `json:"registration_origin"`
	IsTest             bool    `json:"is_test"`
	RegisteredAt       *string `json:"registered_at"`
}

type adminUserListResponse struct {
	Items      []adminUserResponse `json:"items"`
	Total      int64               `json:"total"`
	NextCursor *string             `json:"next_cursor"`
	ObservedAt string              `json:"observed_at"`
}

type adminUserSummaryResponse struct {
	ObservedAt                   string  `json:"observed_at"`
	Environment                  string  `json:"environment"`
	DefinitionVersion            string  `json:"definition_version"`
	AccountTotal                 int64   `json:"account_total"`
	OrdinaryTotal                int64   `json:"ordinary_total"`
	ActiveOrdinary               int64   `json:"active_ordinary"`
	VerifiedOrdinary             int64   `json:"verified_ordinary"`
	ManagementTotal              int64   `json:"management_total"`
	ClosedTotal                  int64   `json:"closed_total"`
	PublicRegistrations          int64   `json:"public_registrations"`
	PublicRegistrationsToday     int64   `json:"public_registrations_today"`
	PublicRegistrationsSevenDays int64   `json:"public_registrations_seven_days"`
	UnknownOrigin                int64   `json:"unknown_origin"`
	CoverageStartedAt            *string `json:"coverage_started_at"`
}

type adminUserCursor struct {
	Version    int    `json:"v"`
	Scope      string `json:"s"`
	SnapshotAt string `json:"n"`
	CreatedAt  string `json:"c"`
	ID         string `json:"i"`
}

type roleChangeRequest struct {
	Role string `json:"role"`
}

type userStatusChangeRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func (s *Server) adminListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	repository, ok := s.operations.(operations.UserManagementRepository)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "USER_MANAGEMENT_UNAVAILABLE", "用户管理服务暂时不可用")
		return
	}
	query, scope, ok := readAdminUserQuery(w, r, s.now().UTC())
	if !ok {
		return
	}
	page, err := repository.ListUsersPage(r.Context(), query)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := adminUserListResponse{
		Items: make([]adminUserResponse, 0, len(page.Items)), Total: page.Total,
		ObservedAt: query.SnapshotAt.UTC().Format(time.RFC3339),
	}
	for _, item := range page.Items {
		response.Items = append(response.Items, adminUserItem(item))
	}
	if page.HasMore && len(page.Items) > 0 {
		cursor := adminUserCursor{Version: 1, Scope: scope,
			SnapshotAt: query.SnapshotAt.UTC().Format(time.RFC3339Nano),
			CreatedAt:  page.Items[len(page.Items)-1].CreatedAt.UTC().Format(time.RFC3339Nano),
			ID:         page.Items[len(page.Items)-1].ID}
		encoded, _ := json.Marshal(cursor)
		next := base64.RawURLEncoding.EncodeToString(encoded)
		response.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminUserSummary(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	repository, ok := s.operations.(operations.UserManagementRepository)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "USER_MANAGEMENT_UNAVAILABLE", "用户管理服务暂时不可用")
		return
	}
	result, err := repository.UserSummary(r.Context(), s.now().UTC(), s.environment)
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, adminUserSummaryResponse{
		ObservedAt: result.ObservedAt.UTC().Format(time.RFC3339), Environment: result.Environment,
		DefinitionVersion: result.DefinitionVersion, AccountTotal: result.AccountTotal,
		OrdinaryTotal: result.OrdinaryTotal, ActiveOrdinary: result.ActiveOrdinary,
		VerifiedOrdinary: result.VerifiedOrdinary, ManagementTotal: result.ManagementTotal,
		ClosedTotal: result.ClosedTotal, PublicRegistrations: result.PublicRegistrations,
		PublicRegistrationsToday:     result.PublicRegistrationsToday,
		PublicRegistrationsSevenDays: result.PublicRegistrationsSevenDays,
		UnknownOrigin:                result.UnknownOrigin, CoverageStartedAt: formatRFC3339(result.CoverageStartedAt),
	})
}

func (s *Server) adminGetUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	userID := chi.URLParam(r, "userID")
	if !uuidPattern.MatchString(userID) {
		writeError(w, r, http.StatusNotFound, "USER_NOT_FOUND", "未找到用户")
		return
	}
	repository, ok := s.operations.(operations.UserManagementRepository)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "USER_MANAGEMENT_UNAVAILABLE", "用户管理服务暂时不可用")
		return
	}
	item, err := repository.GetUser(r.Context(), userID)
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, adminUserItem(item))
}

func readAdminUserQuery(w http.ResponseWriter, r *http.Request, now time.Time) (operations.UserListQuery, string, bool) {
	values := r.URL.Query()
	query := strings.TrimSpace(values.Get("q"))
	role := strings.TrimSpace(values.Get("role"))
	status := strings.TrimSpace(values.Get("status"))
	if len(query) > 320 || (role != "" && !oneOf(role, "owner", "admin", "editor", "user")) ||
		(status != "" && !oneOf(status, "pending", "active", "locked", "suspended", "closed")) {
		writeError(w, r, http.StatusBadRequest, "INVALID_USER_FILTER", "用户筛选条件不符合要求")
		return operations.UserListQuery{}, "", false
	}
	limit := 25
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, r, http.StatusBadRequest, "INVALID_USER_FILTER", "分页数量必须为 1 至 100")
			return operations.UserListQuery{}, "", false
		}
		limit = parsed
	}
	createdFrom, ok := readOptionalRFC3339(w, r, "created_from")
	if !ok {
		return operations.UserListQuery{}, "", false
	}
	createdTo, ok := readOptionalRFC3339(w, r, "created_to")
	if !ok || (createdFrom != nil && createdTo != nil && createdFrom.After(*createdTo)) {
		if ok {
			writeError(w, r, http.StatusBadRequest, "INVALID_USER_FILTER", "创建时间范围不符合要求")
		}
		return operations.UserListQuery{}, "", false
	}
	scopeSource := strings.Join([]string{strings.ToLower(query), role, status, values.Get("created_from"), values.Get("created_to"), strconv.Itoa(limit)}, "\x00")
	digest := sha256.Sum256([]byte(scopeSource))
	scope := "admin.users." + hex.EncodeToString(digest[:8])
	result := operations.UserListQuery{Query: query, Role: role, Status: status, CreatedFrom: createdFrom, CreatedTo: createdTo, SnapshotAt: now, Limit: limit}
	rawCursor := strings.TrimSpace(values.Get("cursor"))
	if rawCursor == "" {
		return result, scope, true
	}
	if len(rawCursor) > 1024 {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return operations.UserListQuery{}, "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(rawCursor)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return operations.UserListQuery{}, "", false
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor adminUserCursor
	if err := decoder.Decode(&cursor); err != nil || cursor.Version != 1 || cursor.Scope != scope || !uuidPattern.MatchString(cursor.ID) {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return operations.UserListQuery{}, "", false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return operations.UserListQuery{}, "", false
	}
	snapshot, snapshotErr := time.Parse(time.RFC3339Nano, cursor.SnapshotAt)
	after, afterErr := time.Parse(time.RFC3339Nano, cursor.CreatedAt)
	if snapshotErr != nil || afterErr != nil || snapshot.After(now.Add(time.Minute)) {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return operations.UserListQuery{}, "", false
	}
	result.SnapshotAt = snapshot.UTC()
	result.AfterCreatedAt = &after
	result.AfterID = strings.ToLower(cursor.ID)
	return result, scope, true
}

func readOptionalRFC3339(w http.ResponseWriter, r *http.Request, key string) (*time.Time, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_USER_FILTER", "创建时间必须使用 RFC3339 格式")
		return nil, false
	}
	parsed = parsed.UTC()
	return &parsed, true
}

func (s *Server) adminChangeUserRole(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	userID := chi.URLParam(r, "userID")
	var request roleChangeRequest
	if !uuidPattern.MatchString(userID) || !s.decodeJSON(w, r, &request) {
		if !uuidPattern.MatchString(userID) {
			writeError(w, r, http.StatusNotFound, "USER_NOT_FOUND", "未找到用户")
		}
		return
	}
	if !oneOf(request.Role, "admin", "editor", "user") {
		writeError(w, r, http.StatusBadRequest, "INVALID_ROLE", "角色不符合要求")
		return
	}
	item, err := s.operations.ChangeUserRole(r.Context(), userID, request.Role, user.ID,
		requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, adminUserItem(item))
}

func (s *Server) adminChangeUserStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	userID := chi.URLParam(r, "userID")
	var request userStatusChangeRequest
	if !uuidPattern.MatchString(userID) || !s.decodeJSON(w, r, &request) {
		if !uuidPattern.MatchString(userID) {
			writeError(w, r, http.StatusNotFound, "USER_NOT_FOUND", "未找到用户")
		}
		return
	}
	request.Status, request.Reason = strings.TrimSpace(request.Status), strings.TrimSpace(request.Reason)
	if !oneOf(request.Status, "active", "locked", "suspended") || len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_USER_STATUS", "账号状态参数不符合要求")
		return
	}
	item, err := s.operations.ChangeUserStatus(r.Context(), userID, request.Status, user.ID, user.Role,
		request.Reason, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, adminUserItem(item))
}

func adminUserItem(item operations.User) adminUserResponse {
	return adminUserResponse{ID: item.ID, Email: item.Email, Role: item.Role, AccountStatus: item.AccountStatus,
		CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339), LastLoginAt: formatRFC3339(item.LastLoginAt),
		EmailVerifiedAt: formatRFC3339(item.EmailVerifiedAt), RegistrationOrigin: item.RegistrationOrigin,
		IsTest: item.IsTest, RegisteredAt: formatRFC3339(item.RegisteredAt)}
}
