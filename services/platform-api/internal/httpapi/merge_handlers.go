package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

type mergeEntityRequest struct {
	EntityType     string `json:"entity_type"`
	TargetEntityID string `json:"target_entity_id"`
	Reason         string `json:"reason"`
}

type entityMergeResponse struct {
	ID             string `json:"id"`
	EntityType     string `json:"entity_type"`
	SourceEntityID string `json:"source_entity_id"`
	TargetEntityID string `json:"target_entity_id"`
	SourcePath     string `json:"source_path"`
	TargetPath     string `json:"target_path"`
	MergedAt       string `json:"merged_at"`
}

func (s *Server) adminMergeEntity(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	sourceID := chi.URLParam(r, "entityID")
	var request mergeEntityRequest
	if !uuidPattern.MatchString(sourceID) || !s.decodeJSON(w, r, &request) {
		if !uuidPattern.MatchString(sourceID) {
			writeError(w, r, http.StatusNotFound, "ENTITY_NOT_FOUND", "未找到资料")
		}
		return
	}
	request.EntityType = strings.TrimSpace(request.EntityType)
	request.TargetEntityID = strings.TrimSpace(request.TargetEntityID)
	request.Reason = strings.TrimSpace(request.Reason)
	if !validEntityType(request.EntityType) || !uuidPattern.MatchString(request.TargetEntityID) ||
		sourceID == request.TargetEntityID || len([]rune(request.Reason)) < 2 || len([]rune(request.Reason)) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_MERGE_REQUEST", "合并目标、类型或理由不符合要求")
		return
	}
	item, err := s.operations.MergeEntity(r.Context(), sourceID, operations.MergeEntityInput{
		EntityType: request.EntityType, TargetEntityID: request.TargetEntityID, Reason: request.Reason,
	}, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, entityMergeResponse{ID: item.ID, EntityType: item.EntityType,
		SourceEntityID: item.SourceEntityID, TargetEntityID: item.TargetEntityID,
		SourcePath: item.SourcePath, TargetPath: item.TargetPath, MergedAt: item.MergedAt.UTC().Format(time.RFC3339)})
}
