package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (s *Server) uploadControlRepository(w http.ResponseWriter, r *http.Request) (operations.MediaUploadRepository, bool) {
	repo, ok := s.operations.(operations.MediaUploadRepository)
	if !ok || isTypedNil(repo) {
		writeError(w, r, http.StatusServiceUnavailable, "MEDIA_UPLOAD_CONTROL_UNAVAILABLE", "上传控制服务暂时不可用")
		return nil, false
	}
	return repo, true
}

func (s *Server) adminGetMediaUploadControl(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok {
		return
	}
	repo, ok := s.uploadControlRepository(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	item, err := repo.GetMediaUploadControl(ctx, s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminSubmitMediaUploadCommand(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok {
		return
	}
	repo, ok := s.uploadControlRepository(w, r)
	if !ok {
		return
	}
	var input operations.MediaUploadInput
	if !s.decodeJSON(w, r, &input) {
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if !input.Valid() {
		writeError(w, r, http.StatusBadRequest, "INVALID_MEDIA_UPLOAD_COMMAND", "请刷新上传状态、填写原因并明确确认暂停或恢复操作")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	item, err := repo.SubmitMediaUploadCommand(ctx, input, actor.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	status := http.StatusAccepted
	if item.IdempotentReplay {
		status = http.StatusOK
	}
	writeJSON(w, status, struct {
		Command operations.MediaUploadCommand `json:"command"`
	}{item})
}
