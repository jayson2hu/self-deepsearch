package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/account"
	"self-deepsearch/services/platform-api/internal/catalog"
	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/logsafe"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type historyItemResponse struct {
	Item     homeItem `json:"item"`
	ViewedAt string   `json:"viewed_at"`
}

type historyResponse struct {
	Items []historyItemResponse `json:"items"`
}

type itemListResponse struct {
	Items      []homeItem `json:"items"`
	NextCursor *string    `json:"next_cursor"`
}

type clearHistoryResponse struct {
	HiddenCount int64 `json:"hidden_count"`
}

type feedbackRequest struct {
	ContentType  *string `json:"content_type"`
	ContentID    *string `json:"content_id"`
	FeedbackType string  `json:"feedback_type"`
	Message      string  `json:"message"`
	EvidenceURL  *string `json:"evidence_url"`
}

type feedbackResponse struct {
	ID           string  `json:"id"`
	ContentType  *string `json:"content_type"`
	ContentID    *string `json:"content_id"`
	FeedbackType string  `json:"feedback_type"`
	Message      string  `json:"message"`
	EvidenceURL  *string `json:"evidence_url"`
	ReviewStatus string  `json:"review_status"`
	CreatedAt    string  `json:"created_at"`
}

type feedbackListResponse struct {
	Items []feedbackResponse `json:"items"`
}

type adminFeedbackResponse struct {
	feedbackResponse
	Submitter  string  `json:"submitter"`
	Reviewer   *string `json:"reviewer"`
	ReviewedAt *string `json:"reviewed_at"`
}

type adminFeedbackListResponse struct {
	Items []adminFeedbackResponse `json:"items"`
}

type reviewFeedbackRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type pageViewRequest struct {
	ContentType string `json:"content_type"`
	ContentID   string `json:"content_id"`
}

func (s *Server) pageView(w http.ResponseWriter, r *http.Request) {
	var request pageViewRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	if !oneOf(request.ContentType, "work", "performer", "studio") || !uuidPattern.MatchString(request.ContentID) {
		writeError(w, r, http.StatusBadRequest, "INVALID_CONTENT", "上报对象不符合要求")
		return
	}
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "统计服务暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.catalog.RecordPageView(ctx, request.ContentType, request.ContentID); err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "CONTENT_NOT_FOUND", "未找到已发布资料")
		} else {
			s.logger.WarnContext(r.Context(), "record_page_view_failed", "request_id", requestIDFromContext(r.Context()), "error_class", logsafe.ErrorClass(err))
			writeError(w, r, http.StatusServiceUnavailable, "METRICS_UNAVAILABLE", "统计服务暂时不可用")
		}
		return
	}
	if s.account != nil {
		if user, ok := s.optionalUser(r); ok {
			if err := s.account.RecordHistory(ctx, user.ID, request.ContentType, request.ContentID, s.now().UTC()); err != nil {
				s.logger.WarnContext(r.Context(), "record_view_history_failed", "request_id", requestIDFromContext(r.Context()), "error_class", logsafe.ErrorClass(err))
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addFavorite(w http.ResponseWriter, r *http.Request)    { s.setFavorite(w, r, true) }
func (s *Server) removeFavorite(w http.ResponseWriter, r *http.Request) { s.setFavorite(w, r, false) }

func (s *Server) setFavorite(w http.ResponseWriter, r *http.Request, active bool) {
	user, ok := s.authenticatedUser(w, r)
	if !ok || !s.requireAccountRepository(w, r) {
		return
	}
	workID := chi.URLParam(r, "workID")
	if !uuidPattern.MatchString(workID) {
		writeError(w, r, http.StatusNotFound, "WORK_NOT_FOUND", "未找到该作品资料")
		return
	}
	if err := s.account.SetFavorite(r.Context(), user.ID, workID, active, s.now().UTC()); err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listFavorites(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok || !s.requireAccountRepository(w, r) {
		return
	}
	items, err := s.account.ListFavorites(r.Context(), user.ID, 100)
	if err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	s.writeMediaJSON(w, r, http.StatusOK, itemListResponse{Items: workItems(items)})
}

func (s *Server) addFollow(w http.ResponseWriter, r *http.Request)    { s.setFollow(w, r, true) }
func (s *Server) removeFollow(w http.ResponseWriter, r *http.Request) { s.setFollow(w, r, false) }

func (s *Server) setFollow(w http.ResponseWriter, r *http.Request, active bool) {
	user, ok := s.authenticatedUser(w, r)
	if !ok || !s.requireAccountRepository(w, r) {
		return
	}
	performerID := chi.URLParam(r, "performerID")
	if !uuidPattern.MatchString(performerID) {
		writeError(w, r, http.StatusNotFound, "PERFORMER_NOT_FOUND", "未找到该人物资料")
		return
	}
	if err := s.account.SetFollow(r.Context(), user.ID, performerID, active, s.now().UTC()); err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listFollows(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok || !s.requireAccountRepository(w, r) {
		return
	}
	items, err := s.account.ListFollows(r.Context(), user.ID, 100)
	if err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	s.writeMediaJSON(w, r, http.StatusOK, itemListResponse{Items: performerItems(items)})
}

func (s *Server) listFollowedWorks(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok || !s.requireAccountRepository(w, r) {
		return
	}
	items, err := s.account.ListFollowedWorks(r.Context(), user.ID, 12)
	if err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	s.writeMediaJSON(w, r, http.StatusOK, itemListResponse{Items: workItems(items)})
}

func (s *Server) hideWork(w http.ResponseWriter, r *http.Request)    { s.setHiddenWork(w, r, true) }
func (s *Server) restoreWork(w http.ResponseWriter, r *http.Request) { s.setHiddenWork(w, r, false) }

func (s *Server) setHiddenWork(w http.ResponseWriter, r *http.Request, active bool) {
	user, ok := s.authenticatedUser(w, r)
	if !ok || !s.requireAccountRepository(w, r) {
		return
	}
	workID := chi.URLParam(r, "workID")
	if !uuidPattern.MatchString(workID) {
		writeError(w, r, http.StatusNotFound, "WORK_NOT_FOUND", "未找到该作品资料")
		return
	}
	if err := s.account.SetHiddenWork(r.Context(), user.ID, workID, active, s.now().UTC()); err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listHiddenWorks(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok || !s.requireAccountRepository(w, r) {
		return
	}
	items, err := s.account.ListHiddenWorks(r.Context(), user.ID, 100)
	if err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	s.writeMediaJSON(w, r, http.StatusOK, itemListResponse{Items: workItems(items)})
}

func (s *Server) listHistory(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok || !s.requireAccountRepository(w, r) {
		return
	}
	items, err := s.account.ListHistory(r.Context(), user.ID, 100)
	if err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	response := historyResponse{Items: make([]historyItemResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, historyItemResponse{
			Item: homeItem{
				ID: item.ID, Type: item.ContentType, Code: item.Code, Title: item.Title,
				Subtitle: item.Subtitle, Href: item.Href, ImageURL: item.ImageURL,
			},
			ViewedAt: item.ViewedAt.UTC().Format(time.RFC3339),
		})
	}
	s.writeMediaJSON(w, r, http.StatusOK, response)
}

func (s *Server) clearHistory(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok || !s.requireAccountRepository(w, r) {
		return
	}
	count, err := s.account.ClearHistory(r.Context(), user.ID, s.now().UTC())
	if err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	s.writeMediaJSON(w, r, http.StatusOK, clearHistoryResponse{HiddenCount: count})
}

func (s *Server) createFeedback(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok || !s.requireAccountRepository(w, r) {
		return
	}
	var request feedbackRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Message = strings.TrimSpace(request.Message)
	if len(request.Message) < 1 || len(request.Message) > 4000 || !validFeedbackType(request.FeedbackType) {
		writeError(w, r, http.StatusBadRequest, "INVALID_FEEDBACK", "反馈内容不符合要求")
		return
	}
	if !validFeedbackTarget(request.ContentType, request.ContentID) {
		writeError(w, r, http.StatusBadRequest, "INVALID_FEEDBACK", "反馈对象不符合要求")
		return
	}
	if request.EvidenceURL != nil {
		value := strings.TrimSpace(*request.EvidenceURL)
		parsed, err := url.ParseRequestURI(value)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || len(value) > 2048 {
			writeError(w, r, http.StatusBadRequest, "INVALID_FEEDBACK", "证据链接不符合要求")
			return
		}
		request.EvidenceURL = &value
	}
	item, err := s.account.CreateFeedback(r.Context(), user.ID, request.ContentType, request.ContentID, request.FeedbackType, request.Message, request.EvidenceURL, s.now().UTC())
	if err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	s.writeMediaJSON(w, r, http.StatusCreated, feedbackItem(item))
}

func (s *Server) listFeedback(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok || !s.requireAccountRepository(w, r) {
		return
	}
	items, err := s.account.ListFeedback(r.Context(), user.ID, 100)
	if err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	response := feedbackListResponse{Items: make([]feedbackResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, feedbackItem(item))
	}
	s.writeMediaJSON(w, r, http.StatusOK, response)
}

func (s *Server) adminListFeedback(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok || !s.requireAccountRepository(w, r) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "all"
	}
	if !oneOf(status, "all", "pending", "reviewing", "accepted", "rejected", "closed") {
		writeError(w, r, http.StatusBadRequest, "INVALID_FEEDBACK_FILTER", "反馈筛选条件不符合要求")
		return
	}
	items, err := s.account.ListFeedbackForReview(r.Context(), status, 200)
	if err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	response := adminFeedbackListResponse{Items: make([]adminFeedbackResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, adminFeedbackItem(item))
	}
	s.writeMediaJSON(w, r, http.StatusOK, response)
}

func (s *Server) adminReviewFeedback(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "editor", "admin", "owner")
	if !ok || !s.requireAccountRepository(w, r) {
		return
	}
	feedbackID := chi.URLParam(r, "feedbackID")
	var request reviewFeedbackRequest
	if !uuidPattern.MatchString(feedbackID) || !s.decodeJSON(w, r, &request) {
		if !uuidPattern.MatchString(feedbackID) {
			writeError(w, r, http.StatusNotFound, "FEEDBACK_NOT_FOUND", "未找到反馈")
		}
		return
	}
	request.Status, request.Reason = strings.TrimSpace(request.Status), strings.TrimSpace(request.Reason)
	if !oneOf(request.Status, "reviewing", "accepted", "rejected", "closed") || len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_FEEDBACK_REVIEW", "反馈处理参数不符合要求")
		return
	}
	if user.Role == "editor" && request.Status != "reviewing" {
		writeError(w, r, http.StatusForbidden, "ROLE_FORBIDDEN", "当前角色不能完成反馈审核")
		return
	}
	item, err := s.account.ReviewFeedback(r.Context(), feedbackID, request.Status, user.ID, request.Reason,
		requestIDFromContext(r.Context()), s.now().UTC())
	if errors.Is(err, account.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "FEEDBACK_NOT_FOUND", "未找到反馈")
		return
	}
	if err != nil {
		s.writeAccountError(w, r, err)
		return
	}
	s.writeMediaJSON(w, r, http.StatusOK, adminFeedbackItem(item))
}

func (s *Server) optionalUser(r *http.Request) (identity.User, bool) {
	if s.identity == nil {
		return identity.User{}, false
	}
	cookie, err := r.Cookie(identity.SessionCookieName)
	if err != nil {
		return identity.User{}, false
	}
	user, err := s.identity.Authenticate(r.Context(), cookie.Value)
	return user, err == nil
}

func (s *Server) requireAccountRepository(w http.ResponseWriter, r *http.Request) bool {
	if s.account == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ACCOUNT_UNAVAILABLE", "用户数据服务暂时不可用")
		return false
	}
	return true
}

func (s *Server) writeAccountError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, account.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "CONTENT_NOT_FOUND", "未找到已发布资料")
	case errors.Is(err, account.ErrConflict):
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", "当前状态已变化，请刷新后重试")
	case errors.Is(err, identity.ErrRateLimited):
		writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "提交过于频繁，请稍后再试")
	default:
		s.logger.ErrorContext(r.Context(), "account_operation_failed", "request_id", requestIDFromContext(r.Context()), "error_class", logsafe.ErrorClass(err))
		writeError(w, r, http.StatusServiceUnavailable, "ACCOUNT_UNAVAILABLE", "用户数据服务暂时不可用")
	}
}

func validFeedbackType(value string) bool {
	return value == "correction" || value == "source_suggestion" || value == "rights" || value == "other"
}

func validFeedbackTarget(contentType, contentID *string) bool {
	if (contentType == nil) != (contentID == nil) {
		return false
	}
	if contentType == nil {
		return true
	}
	return oneOf(*contentType, "work", "performer", "studio") && uuidPattern.MatchString(*contentID)
}

func feedbackItem(item account.Feedback) feedbackResponse {
	return feedbackResponse{
		ID: item.ID, ContentType: item.ContentType, ContentID: item.ContentID,
		FeedbackType: item.FeedbackType, Message: item.Message, EvidenceURL: item.EvidenceURL,
		ReviewStatus: item.ReviewStatus, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func adminFeedbackItem(item account.Feedback) adminFeedbackResponse {
	var reviewedAt *string
	if item.ReviewedAt != nil {
		value := item.ReviewedAt.UTC().Format(time.RFC3339)
		reviewedAt = &value
	}
	return adminFeedbackResponse{
		feedbackResponse: feedbackItem(item), Submitter: item.Submitter,
		Reviewer: item.Reviewer, ReviewedAt: reviewedAt,
	}
}
