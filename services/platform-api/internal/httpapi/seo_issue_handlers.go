package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/operations"
)

type seoScanResponse struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Status       string  `json:"status"`
	ScannedCount int     `json:"scanned_count"`
	IssueCount   int     `json:"issue_count"`
	SkippedCount int     `json:"skipped_count"`
	Truncated    bool    `json:"truncated"`
	ErrorSummary *string `json:"error_summary"`
	StartedAt    string  `json:"started_at"`
	CompletedAt  *string `json:"completed_at"`
}

type seoIssueResponse struct {
	ID             string         `json:"id"`
	RuleCode       string         `json:"rule_code"`
	EntityType     string         `json:"entity_type"`
	EntityID       string         `json:"entity_id"`
	URLPath        string         `json:"url_path"`
	Priority       string         `json:"priority"`
	Status         string         `json:"status"`
	Evidence       map[string]any `json:"evidence"`
	AssigneeID     *string        `json:"assignee_id"`
	Assignee       *string        `json:"assignee"`
	DueAt          *string        `json:"due_at"`
	ResolutionNote *string        `json:"resolution_note"`
	FirstSeenAt    string         `json:"first_seen_at"`
	LastSeenAt     string         `json:"last_seen_at"`
	ClosedAt       *string        `json:"closed_at"`
	UpdatedAt      string         `json:"updated_at"`
}

type seoIssueUpdateRequest struct {
	Status     string  `json:"status"`
	AssigneeID *string `json:"assignee_id"`
	DueAt      *string `json:"due_at"`
	Note       string  `json:"note"`
}

func (s *Server) adminRunSEOScan(w http.ResponseWriter, r *http.Request) {
	user, repository, ok := s.requireSEOIssues(w, r)
	if !ok {
		return
	}
	if r.ContentLength > 0 {
		var request struct{}
		if !s.decodeJSON(w, r, &request) {
			return
		}
	}
	item, err := repository.RunSEOScan(r.Context(), user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, seoScanItem(item))
}

func (s *Server) adminListSEOScans(w http.ResponseWriter, r *http.Request) {
	_, repository, ok := s.requireSEOIssues(w, r)
	if !ok {
		return
	}
	items, err := repository.ListSEOScans(r.Context(), 20)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := make([]seoScanResponse, 0, len(items))
	for _, item := range items {
		response = append(response, seoScanItem(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func (s *Server) adminListSEOIssues(w http.ResponseWriter, r *http.Request) {
	_, repository, ok := s.requireSEOIssues(w, r)
	if !ok {
		return
	}
	status := defaultString(strings.TrimSpace(r.URL.Query().Get("status")), "all")
	if !oneOf(status, "all", "open", "in_progress", "retest", "closed") {
		writeError(w, r, http.StatusBadRequest, "INVALID_SEO_ISSUE_QUERY", "SEO 问题状态不符合要求")
		return
	}
	items, err := repository.ListSEOIssues(r.Context(), status, 100)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := make([]seoIssueResponse, 0, len(items))
	for _, item := range items {
		response = append(response, seoIssueItem(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func (s *Server) adminUpdateSEOIssue(w http.ResponseWriter, r *http.Request) {
	user, repository, ok := s.requireSEOIssues(w, r)
	if !ok {
		return
	}
	issueID := chi.URLParam(r, "issueID")
	if !uuidPattern.MatchString(issueID) {
		writeError(w, r, http.StatusNotFound, "SEO_ISSUE_NOT_FOUND", "未找到 SEO 问题")
		return
	}
	var request seoIssueUpdateRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Note = strings.TrimSpace(request.Note)
	if !oneOf(request.Status, "open", "in_progress", "retest", "closed") || len(request.Note) < 2 || len(request.Note) > 2000 ||
		(request.AssigneeID != nil && !uuidPattern.MatchString(*request.AssigneeID)) {
		writeError(w, r, http.StatusBadRequest, "INVALID_SEO_ISSUE_UPDATE", "状态、负责人或处理说明不符合要求")
		return
	}
	var dueAt *time.Time
	if request.DueAt != nil && *request.DueAt != "" {
		value, err := time.Parse(time.RFC3339, *request.DueAt)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_SEO_ISSUE_UPDATE", "截止时间必须使用 RFC3339")
			return
		}
		dueAt = &value
	}
	item, err := repository.UpdateSEOIssue(r.Context(), issueID, operations.SEOIssueUpdate{Status: request.Status,
		AssigneeID: request.AssigneeID, DueAt: dueAt, Note: request.Note}, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, seoIssueItem(item))
}

func (s *Server) requireSEOIssues(w http.ResponseWriter, r *http.Request) (identity.User, operations.SEOIssueRepository, bool) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return identity.User{}, nil, false
	}
	repository, ok := s.operations.(operations.SEOIssueRepository)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "SEO_ISSUES_UNAVAILABLE", "SEO 扫描与问题服务暂时不可用")
		return identity.User{}, nil, false
	}
	return user, repository, true
}

func seoScanItem(item operations.SEOScan) seoScanResponse {
	return seoScanResponse{ID: item.ID, Source: item.Source, Status: item.Status, ScannedCount: item.ScannedCount,
		IssueCount: item.IssueCount, SkippedCount: item.SkippedCount, Truncated: item.Truncated,
		ErrorSummary: item.ErrorSummary, StartedAt: item.StartedAt.UTC().Format(time.RFC3339), CompletedAt: formatRFC3339(item.CompletedAt)}
}

func seoIssueItem(item operations.SEOIssue) seoIssueResponse {
	return seoIssueResponse{ID: item.ID, RuleCode: item.RuleCode, EntityType: item.EntityType, EntityID: item.EntityID,
		URLPath: item.URLPath, Priority: item.Priority, Status: item.Status, Evidence: item.Evidence,
		AssigneeID: item.AssigneeID, Assignee: item.Assignee, DueAt: formatRFC3339(item.DueAt), ResolutionNote: item.ResolutionNote,
		FirstSeenAt: item.FirstSeenAt.UTC().Format(time.RFC3339), LastSeenAt: item.LastSeenAt.UTC().Format(time.RFC3339),
		ClosedAt: formatRFC3339(item.ClosedAt), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339)}
}
