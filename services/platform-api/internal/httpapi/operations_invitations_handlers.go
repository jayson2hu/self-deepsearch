package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/identity"
)

type createInvitationRequest struct {
	Email  string `json:"email"`
	Role   string `json:"role"`
	Reason string `json:"reason"`
}

type revokeInvitationRequest struct {
	Reason string `json:"reason"`
}

type invitationResponse struct {
	ID         string  `json:"id"`
	Email      string  `json:"email"`
	Role       string  `json:"role"`
	Status     string  `json:"status"`
	InvitedBy  string  `json:"invited_by"`
	Inviter    string  `json:"inviter"`
	SentAt     string  `json:"sent_at"`
	ExpiresAt  string  `json:"expires_at"`
	ConsumedAt *string `json:"consumed_at"`
	CreatedAt  string  `json:"created_at"`
}

type invitationListResponse struct {
	Items []invitationResponse `json:"items"`
}

func (s *Server) adminListInvitations(w http.ResponseWriter, r *http.Request) {
	if s.identity == nil {
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
		return
	}
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "all"
	}
	if !oneOf(status, "all", "pending", "accepted", "revoked", "expired") {
		writeError(w, r, http.StatusBadRequest, "INVALID_INVITATION_FILTER", "邀请筛选条件不符合要求")
		return
	}
	items, err := s.identity.ListInvitations(r.Context(), status, 100)
	if s.writeIdentityError(w, r, err) {
		return
	}
	response := invitationListResponse{Items: make([]invitationResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, invitationItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminCreateInvitation(w http.ResponseWriter, r *http.Request) {
	if s.identity == nil {
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
		return
	}
	actor, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok {
		return
	}
	var request createInvitationRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Email, request.Role, request.Reason = strings.TrimSpace(request.Email), strings.TrimSpace(request.Role), strings.TrimSpace(request.Reason)
	if !oneOf(request.Role, "admin", "editor", "user") || len(request.Reason) < 2 || len(request.Reason) > 500 {
		writeError(w, r, http.StatusBadRequest, "INVALID_INVITATION", "邀请角色或理由不符合要求")
		return
	}
	item, err := s.identity.CreateInvitation(r.Context(), request.Email, request.Role, request.Reason, actor.ID, actor.Role,
		requestIDFromContext(r.Context()), s.remoteIP(r))
	if s.writeIdentityError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, invitationItem(item))
}

func (s *Server) adminRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	if s.identity == nil {
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
		return
	}
	actor, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok {
		return
	}
	invitationID := chi.URLParam(r, "invitationID")
	if !uuidPattern.MatchString(invitationID) {
		writeError(w, r, http.StatusNotFound, "INVITATION_NOT_FOUND", "未找到邀请")
		return
	}
	var request revokeInvitationRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if len(request.Reason) < 2 || len(request.Reason) > 500 {
		writeError(w, r, http.StatusBadRequest, "INVALID_INVITATION_REASON", "撤销理由不符合要求")
		return
	}
	item, err := s.identity.RevokeInvitation(r.Context(), invitationID, actor.ID, request.Reason,
		requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeIdentityError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, invitationItem(item))
}

func invitationItem(item identity.Invitation) invitationResponse {
	return invitationResponse{
		ID: item.ID, Email: item.Email, Role: item.Role, Status: item.Status,
		InvitedBy: item.InvitedBy, Inviter: item.Inviter,
		SentAt: item.SentAt.UTC().Format(time.RFC3339), ExpiresAt: item.ExpiresAt.UTC().Format(time.RFC3339),
		ConsumedAt: formatRFC3339(item.ConsumedAt), CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339),
	}
}
