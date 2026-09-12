package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

type editorialRecommendationRequest struct {
	WorkID       string  `json:"work_id"`
	Position     int     `json:"position"`
	StartsAt     string  `json:"starts_at"`
	EndsAt       *string `json:"ends_at"`
	Reason       string  `json:"reason"`
	ChangeReason string  `json:"change_reason"`
	Status       string  `json:"status"`
}

type removeEditorialRecommendationRequest struct {
	Reason string `json:"reason"`
}

type editorialRecommendationResponse struct {
	ID        string  `json:"id"`
	WorkID    string  `json:"work_id"`
	WorkCode  string  `json:"work_code"`
	WorkTitle string  `json:"work_title"`
	WorkHref  *string `json:"work_href"`
	Position  int     `json:"position"`
	StartsAt  string  `json:"starts_at"`
	EndsAt    *string `json:"ends_at"`
	Reason    string  `json:"reason"`
	Status    string  `json:"status"`
	CreatedBy string  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type editorialRecommendationListResponse struct {
	Items []editorialRecommendationResponse `json:"items"`
}

func (s *Server) adminListEditorialRecommendations(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "all"
	}
	if !oneOf(status, "all", "active", "paused", "removed") {
		writeError(w, r, http.StatusBadRequest, "INVALID_STATUS", "推荐状态不符合要求")
		return
	}
	items, err := s.operations.ListEditorialRecommendations(r.Context(), status, 200)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := editorialRecommendationListResponse{Items: make([]editorialRecommendationResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, editorialRecommendationItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminCreateEditorialRecommendation(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	var request editorialRecommendationRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	input, valid := parseEditorialRecommendationInput(request)
	if !valid {
		writeError(w, r, http.StatusBadRequest, "INVALID_RECOMMENDATION", "推荐作品、时间、顺序或理由不符合要求")
		return
	}
	item, err := s.operations.CreateEditorialRecommendation(r.Context(), input, user.ID,
		requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, editorialRecommendationItem(item))
}

func (s *Server) adminUpdateEditorialRecommendation(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	recommendationID := chi.URLParam(r, "recommendationID")
	if !uuidPattern.MatchString(recommendationID) {
		writeError(w, r, http.StatusNotFound, "RECOMMENDATION_NOT_FOUND", "未找到该推荐")
		return
	}
	var request editorialRecommendationRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	input, valid := parseEditorialRecommendationInput(request)
	if !valid {
		writeError(w, r, http.StatusBadRequest, "INVALID_RECOMMENDATION", "推荐作品、时间、顺序或理由不符合要求")
		return
	}
	item, err := s.operations.UpdateEditorialRecommendation(r.Context(), recommendationID, input, user.ID,
		requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, editorialRecommendationItem(item))
}

func (s *Server) adminRemoveEditorialRecommendation(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	recommendationID := chi.URLParam(r, "recommendationID")
	if !uuidPattern.MatchString(recommendationID) {
		writeError(w, r, http.StatusNotFound, "RECOMMENDATION_NOT_FOUND", "未找到该推荐")
		return
	}
	var request removeEditorialRecommendationRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	reason := strings.TrimSpace(request.Reason)
	if len([]rune(reason)) < 2 || len([]rune(reason)) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_REASON", "请输入 2 至 1000 个字符的移除理由")
		return
	}
	item, err := s.operations.RemoveEditorialRecommendation(r.Context(), recommendationID, user.ID, reason,
		requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, editorialRecommendationItem(item))
}

func parseEditorialRecommendationInput(request editorialRecommendationRequest) (operations.EditorialRecommendationInput, bool) {
	request.WorkID = strings.TrimSpace(request.WorkID)
	request.StartsAt = strings.TrimSpace(request.StartsAt)
	request.Reason = strings.TrimSpace(request.Reason)
	request.ChangeReason = strings.TrimSpace(request.ChangeReason)
	request.Status = strings.TrimSpace(request.Status)
	startsAt, err := time.Parse(time.RFC3339, request.StartsAt)
	if err != nil || !uuidPattern.MatchString(request.WorkID) || request.Position < 1 || request.Position > 100 ||
		len([]rune(request.Reason)) < 2 || len([]rune(request.Reason)) > 1000 ||
		len([]rune(request.ChangeReason)) < 2 || len([]rune(request.ChangeReason)) > 1000 ||
		!oneOf(request.Status, "active", "paused") {
		return operations.EditorialRecommendationInput{}, false
	}
	var endsAt *time.Time
	if request.EndsAt != nil && strings.TrimSpace(*request.EndsAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*request.EndsAt))
		if err != nil || !parsed.After(startsAt) {
			return operations.EditorialRecommendationInput{}, false
		}
		parsed = parsed.UTC()
		endsAt = &parsed
	}
	return operations.EditorialRecommendationInput{WorkID: request.WorkID, Position: request.Position,
		StartsAt: startsAt.UTC(), EndsAt: endsAt, Reason: request.Reason, ChangeReason: request.ChangeReason,
		Status: request.Status}, true
}

func editorialRecommendationItem(item operations.EditorialRecommendation) editorialRecommendationResponse {
	var href *string
	if item.WorkSlug != nil {
		value := "/works/" + *item.WorkSlug
		href = &value
	}
	return editorialRecommendationResponse{
		ID: item.ID, WorkID: item.WorkID, WorkCode: item.WorkCode, WorkTitle: item.WorkTitle, WorkHref: href,
		Position: item.Position, StartsAt: item.StartsAt.UTC().Format(time.RFC3339), EndsAt: formatRFC3339(item.EndsAt),
		Reason: item.Reason, Status: item.Status, CreatedBy: item.CreatedBy,
		CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
