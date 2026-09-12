package httpapi

import (
	"net/http"
	"strings"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (s *Server) usageRepository(w http.ResponseWriter, r *http.Request) (operations.MediaUsageRepository, bool) {
	repo, ok := s.operations.(operations.MediaUsageRepository)
	if !ok || isTypedNil(repo) {
		writeError(w, r, http.StatusServiceUnavailable, "MEDIA_USAGE_UNAVAILABLE", "用量复核服务暂时不可用")
		return nil, false
	}
	return repo, true
}

func (s *Server) adminGetMediaUsage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok {
		return
	}
	repo, ok := s.usageRepository(w, r)
	if !ok {
		return
	}
	item, err := repo.GetMediaUsage(r.Context(), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminSubmitMediaUsageReview(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok {
		return
	}
	repo, ok := s.usageRepository(w, r)
	if !ok {
		return
	}
	var input operations.MediaUsageReviewInput
	if !s.decodeJSON(w, r, &input) {
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if !input.Valid() {
		writeError(w, r, http.StatusBadRequest, "INVALID_MEDIA_USAGE_REVIEW", "请刷新状态，填写有效账期、复核原因并明确确认")
		return
	}
	item, err := repo.SubmitMediaUsageReview(r.Context(), input, actor.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	status := http.StatusAccepted
	if item.IdempotentReplay {
		status = http.StatusOK
	}
	writeJSON(w, status, struct {
		Review      operations.MediaUsageReview `json:"review"`
		Enforcement string                      `json:"enforcement"`
	}{item, "not_connected"})
}
