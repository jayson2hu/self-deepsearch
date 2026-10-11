package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

type conflictReviewResponse struct {
	ID             string  `json:"id"`
	ReviewTaskID   string  `json:"review_task_id"`
	EntityType     string  `json:"entity_type"`
	EntityID       string  `json:"entity_id"`
	FieldName      string  `json:"field_name"`
	CurrentValue   any     `json:"current_value"`
	CandidateValue any     `json:"candidate_value"`
	SourceID       *string `json:"source_id"`
	SourceName     *string `json:"source_name"`
	SourcePriority *int    `json:"source_priority"`
	Resolution     *string `json:"resolution"`
	ResolvedBy     *string `json:"resolved_by"`
	ResolvedAt     *string `json:"resolved_at"`
	CreatedAt      string  `json:"created_at"`
}

type conflictReviewListResponse struct {
	Items []conflictReviewResponse `json:"items"`
}

type resolveConflictRequest struct {
	Resolution string `json:"resolution"`
	Reason     string `json:"reason"`
}

func (s *Server) adminListConflicts(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "open"
	}
	if !oneOf(status, "open", "resolved", "all") {
		writeError(w, r, http.StatusBadRequest, "INVALID_STATUS", "冲突状态不符合要求")
		return
	}
	items, err := s.operations.ListConflictReviews(r.Context(), status, 200)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := conflictReviewListResponse{Items: make([]conflictReviewResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, conflictReviewItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminResolveConflict(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	conflictID := chi.URLParam(r, "conflictID")
	if !uuidPattern.MatchString(conflictID) {
		writeError(w, r, http.StatusNotFound, "CONFLICT_NOT_FOUND", "未找到该冲突记录")
		return
	}
	var request resolveConflictRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Resolution = strings.TrimSpace(request.Resolution)
	request.Reason = strings.TrimSpace(request.Reason)
	if !oneOf(request.Resolution, "keep_current", "accept_candidate", "mark_unknown", "merge", "reject") ||
		len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_RESOLUTION", "裁决类型或理由不符合要求")
		return
	}
	item, err := s.operations.ResolveConflictReview(r.Context(), conflictID, request.Resolution, user.ID,
		request.Reason, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, conflictReviewItem(item))
}

func conflictReviewItem(item operations.ConflictReview) conflictReviewResponse {
	return conflictReviewResponse{
		ID: item.ID, ReviewTaskID: item.ReviewTaskID, EntityType: item.EntityType, EntityID: item.EntityID,
		FieldName: item.FieldName, CurrentValue: item.CurrentValue, CandidateValue: item.CandidateValue,
		SourceID: item.SourceID, SourceName: item.SourceName, SourcePriority: item.SourcePriority,
		Resolution: item.Resolution, ResolvedBy: item.ResolvedBy, ResolvedAt: formatRFC3339(item.ResolvedAt),
		CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339),
	}
}
