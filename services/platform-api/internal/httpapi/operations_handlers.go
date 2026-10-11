package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/logsafe"
	"self-deepsearch/services/platform-api/internal/operations"
)

func (s *Server) requireAdminRole(w http.ResponseWriter, r *http.Request, roles ...string) (identity.User, bool) {
	user, ok := s.authenticatedUser(w, r)
	if !ok {
		return identity.User{}, false
	}
	if !oneOf(user.Role, roles...) {
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "当前账号无权执行此操作")
		return identity.User{}, false
	}
	return user, true
}

func (s *Server) requireOperations(w http.ResponseWriter, r *http.Request) bool {
	if s.operations == nil {
		writeError(w, r, http.StatusServiceUnavailable, "OPERATIONS_UNAVAILABLE", "运营服务暂时不可用")
		return false
	}
	return true
}

func (s *Server) writeOperationsError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, operations.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "OPERATIONS_OBJECT_NOT_FOUND", "未找到运营对象")
	case errors.Is(err, operations.ErrConflict):
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", "对象状态已变化，请刷新后重试")
	case errors.Is(err, operations.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "当前账号无权执行此操作")
	case errors.Is(err, operations.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", "提交的数据不符合要求")
	default:
		s.logger.ErrorContext(r.Context(), "operations_failed", "request_id", requestIDFromContext(r.Context()), "error_class", logsafe.ErrorClass(err))
		writeError(w, r, http.StatusServiceUnavailable, "OPERATIONS_UNAVAILABLE", "运营服务暂时不可用")
	}
	return true
}

func formatRFC3339(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func cleanOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
