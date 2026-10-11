package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

type reviewTaskResponse struct {
	ID          string  `json:"id"`
	TaskType    string  `json:"task_type"`
	EntityType  *string `json:"entity_type"`
	EntityID    *string `json:"entity_id"`
	Priority    int     `json:"priority"`
	Status      string  `json:"status"`
	AssigneeID  *string `json:"assignee_id"`
	Assignee    *string `json:"assignee"`
	TargetLabel string  `json:"target_label"`
	DueAt       *string `json:"due_at"`
	ClaimedAt   *string `json:"claimed_at"`
	CompletedAt *string `json:"completed_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type reviewTaskListResponse struct {
	Items []reviewTaskResponse `json:"items"`
}

type reviewDecisionRequest struct {
	Reason string `json:"reason"`
}

type reviewReassignmentRequest struct {
	AssigneeID string `json:"assignee_id"`
	Reason     string `json:"reason"`
}

func (s *Server) adminListReviewTasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "open"
	}
	if !oneOf(status, "open", "pending", "claimed", "approved", "rejected", "cancelled") {
		writeError(w, r, http.StatusBadRequest, "INVALID_STATUS", "审核状态不符合要求")
		return
	}
	items, err := s.operations.ListReviewTasks(r.Context(), status, 100)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := reviewTaskListResponse{Items: make([]reviewTaskResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, reviewTaskItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminClaimReviewTask(w http.ResponseWriter, r *http.Request) {
	// A claimed task is locked to the operator who will make the decision.
	// Editors may prepare and submit revisions, but cannot take a review lock
	// they are not authorized to complete.
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	taskID := chi.URLParam(r, "taskID")
	if !uuidPattern.MatchString(taskID) {
		writeError(w, r, http.StatusNotFound, "REVIEW_TASK_NOT_FOUND", "未找到审核任务")
		return
	}
	item, err := s.operations.ClaimReviewTask(r.Context(), taskID, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, reviewTaskItem(item))
}

func (s *Server) adminReassignReviewTask(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	taskID := chi.URLParam(r, "taskID")
	if !uuidPattern.MatchString(taskID) {
		writeError(w, r, http.StatusNotFound, "REVIEW_TASK_NOT_FOUND", "未找到审核任务")
		return
	}
	var request reviewReassignmentRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.AssigneeID = strings.TrimSpace(request.AssigneeID)
	request.Reason = strings.TrimSpace(request.Reason)
	if !uuidPattern.MatchString(request.AssigneeID) {
		writeError(w, r, http.StatusBadRequest, "INVALID_ASSIGNEE", "请选择有效的目标审核人")
		return
	}
	if len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_REASON", "请填写 2 至 1000 个字符的改派理由")
		return
	}
	item, err := s.operations.ReassignReviewTask(r.Context(), taskID, request.AssigneeID, user.ID,
		request.Reason, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, reviewTaskItem(item))
}

func (s *Server) adminApproveReviewTask(w http.ResponseWriter, r *http.Request) {
	s.adminDecideReviewTask(w, r, true)
}

func (s *Server) adminRejectReviewTask(w http.ResponseWriter, r *http.Request) {
	s.adminDecideReviewTask(w, r, false)
}

func (s *Server) adminDecideReviewTask(w http.ResponseWriter, r *http.Request, approve bool) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	taskID := chi.URLParam(r, "taskID")
	if !uuidPattern.MatchString(taskID) {
		writeError(w, r, http.StatusNotFound, "REVIEW_TASK_NOT_FOUND", "未找到审核任务")
		return
	}
	var request reviewDecisionRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_REASON", "请填写 2 至 1000 个字符的审核理由")
		return
	}
	item, err := s.operations.DecideReviewTask(r.Context(), taskID, user.ID, approve, request.Reason,
		requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, reviewTaskItem(item))
}

func reviewTaskItem(item operations.ReviewTask) reviewTaskResponse {
	return reviewTaskResponse{
		ID: item.ID, TaskType: item.TaskType, EntityType: item.EntityType, EntityID: item.EntityID,
		Priority: item.Priority, Status: item.Status, AssigneeID: item.AssigneeID, Assignee: item.Assignee,
		TargetLabel: item.TargetLabel, DueAt: formatRFC3339(item.DueAt), ClaimedAt: formatRFC3339(item.ClaimedAt),
		CompletedAt: formatRFC3339(item.CompletedAt), CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
