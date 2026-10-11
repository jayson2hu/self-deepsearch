package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

type publishEntityRequest struct {
	EntityType    string `json:"entity_type"`
	RevisionID    string `json:"revision_id"`
	CanonicalSlug string `json:"canonical_slug"`
	Reason        string `json:"reason"`
}

type hideEntityRequest struct {
	EntityType string `json:"entity_type"`
	Reason     string `json:"reason"`
}

func (s *Server) adminPublishEntity(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	entityID := chi.URLParam(r, "entityID")
	var request publishEntityRequest
	if !uuidPattern.MatchString(entityID) || !s.decodeJSON(w, r, &request) {
		if !uuidPattern.MatchString(entityID) {
			writeError(w, r, http.StatusNotFound, "ENTITY_NOT_FOUND", "未找到资料")
		}
		return
	}
	request.CanonicalSlug, request.Reason = strings.TrimSpace(request.CanonicalSlug), strings.TrimSpace(request.Reason)
	if !validEntityType(request.EntityType) || !adminSlugPattern.MatchString(request.CanonicalSlug) || len(request.CanonicalSlug) > 200 ||
		(request.RevisionID != "" && !uuidPattern.MatchString(request.RevisionID)) || len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_PUBLICATION", "发布参数不符合要求")
		return
	}
	item, err := s.operations.PublishEntity(r.Context(), entityID, operations.PublishInput{
		EntityType: request.EntityType, RevisionID: request.RevisionID, CanonicalSlug: request.CanonicalSlug, Reason: request.Reason,
	}, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, entityItem(item))
}

func (s *Server) adminHideEntity(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	entityID := chi.URLParam(r, "entityID")
	var request hideEntityRequest
	if !uuidPattern.MatchString(entityID) || !s.decodeJSON(w, r, &request) {
		if !uuidPattern.MatchString(entityID) {
			writeError(w, r, http.StatusNotFound, "ENTITY_NOT_FOUND", "未找到资料")
		}
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if !validEntityType(request.EntityType) || len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_HIDE_REQUEST", "隐藏参数不符合要求")
		return
	}
	item, err := s.operations.HideEntity(r.Context(), entityID, request.EntityType, user.ID, request.Reason,
		requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, entityItem(item))
}
