package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

type createTakedownRequest struct {
	RequesterReference string  `json:"requester_reference"`
	EntityType         string  `json:"entity_type"`
	EntityID           string  `json:"entity_id"`
	EvidenceReference  *string `json:"evidence_reference"`
}

type completeTakedownRequest struct {
	ResultSummary string `json:"result_summary"`
}

type takedownResponse struct {
	ID                 string  `json:"id"`
	RequesterReference string  `json:"requester_reference"`
	EntityType         string  `json:"entity_type"`
	EntityID           string  `json:"entity_id"`
	EvidenceReference  *string `json:"evidence_reference"`
	Status             string  `json:"status"`
	AssignedTo         *string `json:"assigned_to"`
	ReceivedAt         string  `json:"received_at"`
	DecidedAt          *string `json:"decided_at"`
	CompletedAt        *string `json:"completed_at"`
	ResultSummary      *string `json:"result_summary"`
	UpdatedAt          string  `json:"updated_at"`
}

type takedownListResponse struct {
	Items []takedownResponse `json:"items"`
}

func (s *Server) adminListTakedowns(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "all"
	}
	if !oneOf(status, "all", "received", "validating", "approved", "rejected", "completed") {
		writeError(w, r, http.StatusBadRequest, "INVALID_STATUS", "下架请求状态不符合要求")
		return
	}
	items, err := s.operations.ListTakedownRequests(r.Context(), status, 100)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := takedownListResponse{Items: make([]takedownResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, takedownItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminCreateTakedown(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	var request createTakedownRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.RequesterReference = strings.TrimSpace(request.RequesterReference)
	request.EntityType = strings.TrimSpace(request.EntityType)
	request.EntityID = strings.TrimSpace(request.EntityID)
	request.EvidenceReference = cleanOptional(request.EvidenceReference)
	if len(request.RequesterReference) < 1 || len(request.RequesterReference) > 500 ||
		!oneOf(request.EntityType, "work", "performer", "studio", "media") ||
		!uuidPattern.MatchString(request.EntityID) ||
		(request.EvidenceReference != nil && len(*request.EvidenceReference) > 2000) {
		writeError(w, r, http.StatusBadRequest, "INVALID_TAKEDOWN", "权利下架登记信息不符合要求")
		return
	}
	item, err := s.operations.CreateTakedownRequest(r.Context(), operations.TakedownInput{
		RequesterReference: request.RequesterReference, EntityType: request.EntityType,
		EntityID: request.EntityID, EvidenceReference: request.EvidenceReference,
	}, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, takedownItem(item))
}

func (s *Server) adminCompleteTakedown(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	takedownID := chi.URLParam(r, "takedownID")
	if !uuidPattern.MatchString(takedownID) {
		writeError(w, r, http.StatusNotFound, "TAKEDOWN_NOT_FOUND", "未找到权利下架请求")
		return
	}
	var request completeTakedownRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.ResultSummary = strings.TrimSpace(request.ResultSummary)
	if len(request.ResultSummary) < 2 || len(request.ResultSummary) > 2000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_TAKEDOWN_RESULT", "请填写 2 至 2000 个字符的处理结论")
		return
	}
	item, err := s.operations.CompleteTakedownRequest(r.Context(), takedownID, user.ID,
		request.ResultSummary, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, takedownItem(item))
}

func takedownItem(item operations.TakedownRequest) takedownResponse {
	return takedownResponse{
		ID: item.ID, RequesterReference: item.RequesterReference, EntityType: item.EntityType,
		EntityID: item.EntityID, EvidenceReference: item.EvidenceReference, Status: item.Status,
		AssignedTo: item.AssignedTo, ReceivedAt: item.ReceivedAt.UTC().Format(time.RFC3339),
		DecidedAt: formatRFC3339(item.DecidedAt), CompletedAt: formatRFC3339(item.CompletedAt),
		ResultSummary: item.ResultSummary, UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
