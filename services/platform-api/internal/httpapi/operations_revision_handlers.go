package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

type createRevisionRequest struct {
	EntityType string                  `json:"entity_type"`
	Payload    map[string]any          `json:"payload"`
	Sources    []sourceEvidenceRequest `json:"sources"`
	Reason     string                  `json:"reason"`
}

type revisionResponse struct {
	ID            string                      `json:"id"`
	EntityType    string                      `json:"entity_type"`
	EntityID      string                      `json:"entity_id"`
	Version       int                         `json:"version"`
	Payload       map[string]any              `json:"payload"`
	Status        string                      `json:"status"`
	Author        string                      `json:"author"`
	Reviewer      *string                     `json:"reviewer"`
	Reason        string                      `json:"reason"`
	CreatedAt     string                      `json:"created_at"`
	ReviewedAt    *string                     `json:"reviewed_at"`
	Sources       []operations.SourceEvidence `json:"sources"`
	IsCurrent     bool                        `json:"is_current"`
	CanonicalSlug *string                     `json:"canonical_slug"`
}

type revisionListResponse struct {
	Items []revisionResponse `json:"items"`
}

func (s *Server) adminCreateRevision(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "editor", "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	entityID := chi.URLParam(r, "entityID")
	var request createRevisionRequest
	if !uuidPattern.MatchString(entityID) || !s.decodeJSON(w, r, &request) {
		if !uuidPattern.MatchString(entityID) {
			writeError(w, r, http.StatusNotFound, "ENTITY_NOT_FOUND", "未找到资料")
		}
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	now := s.now().UTC()
	sources, sourcesOK := normalizeSourceEvidence(request.Sources, now)
	if request.EntityType == "work" {
		if code, ok := request.Payload["canonical_code"].(string); ok {
			request.Payload["canonical_code"] = strings.TrimSpace(code)
			request.Payload["compact_code"] = compactCode(code)
		}
	}
	if request.EntityType == "studio" {
		if name, ok := request.Payload["name"].(string); ok {
			name = strings.TrimSpace(name)
			request.Payload["name"] = name
			request.Payload["normalized_name"] = strings.ToLower(name)
		}
	}
	aliasesOK := request.EntityType != "performer" || normalizePerformerAliasRevisionPayload(request.Payload)
	if !validEntityType(request.EntityType) || len(request.Payload) == 0 || len(request.Payload) > 24 ||
		len(request.Reason) < 2 || len(request.Reason) > 1000 || !sourcesOK || !aliasesOK || !validRevisionPayload(request.EntityType, request.Payload) ||
		!validRevisionPayloadValues(request.EntityType, request.Payload) {
		writeError(w, r, http.StatusBadRequest, "INVALID_REVISION", "修订内容不符合要求")
		return
	}
	item, err := s.operations.CreateRevision(r.Context(), entityID, operations.RevisionInput{
		EntityType: request.EntityType, Payload: request.Payload, Sources: sources, Reason: request.Reason,
	}, user.ID, requestIDFromContext(r.Context()), now)
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, entityItem(item))
}

func (s *Server) adminListRevisions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	entityID := chi.URLParam(r, "entityID")
	entityType := strings.TrimSpace(r.URL.Query().Get("entity_type"))
	if !uuidPattern.MatchString(entityID) || !validEntityType(entityType) {
		writeError(w, r, http.StatusBadRequest, "INVALID_ENTITY", "资料类型或 ID 不符合要求")
		return
	}
	items, err := s.operations.ListRevisions(r.Context(), entityID, entityType, 100)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := revisionListResponse{Items: make([]revisionResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, revisionResponse{ID: item.ID, EntityType: item.EntityType,
			EntityID: item.EntityID, Version: item.Version, Payload: item.Payload, Status: item.Status,
			Author: item.Author, Reviewer: item.Reviewer, Reason: item.Reason,
			CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339), ReviewedAt: formatRFC3339(item.ReviewedAt),
			Sources: item.Sources, IsCurrent: item.IsCurrent, CanonicalSlug: item.CanonicalSlug})
	}
	writeJSON(w, http.StatusOK, response)
}
