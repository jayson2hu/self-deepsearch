package httpapi

import (
	"net/http"
	"strings"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

type discoveryMixRuleRequest struct {
	WorkSlots      int    `json:"work_slots"`
	PerformerSlots int    `json:"performer_slots"`
	RepeatWindow   int    `json:"repeat_window"`
	Enabled        bool   `json:"enabled"`
	Reason         string `json:"reason"`
}

type discoveryMixRuleResponse struct {
	WorkSlots      int    `json:"work_slots"`
	PerformerSlots int    `json:"performer_slots"`
	RepeatWindow   int    `json:"repeat_window"`
	Enabled        bool   `json:"enabled"`
	UpdatedAt      string `json:"updated_at"`
}

func (s *Server) adminGetDiscoveryMixRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	item, err := s.operations.GetDiscoveryMixRule(r.Context())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, discoveryMixRuleItem(item))
}

func (s *Server) adminUpdateDiscoveryMixRule(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	var request discoveryMixRuleRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.WorkSlots < 1 || request.WorkSlots > 8 || request.PerformerSlots < 1 || request.PerformerSlots > 4 ||
		request.RepeatWindow < 5 || request.RepeatWindow > 40 || len([]rune(request.Reason)) < 2 || len([]rune(request.Reason)) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_DISCOVERY_MIX", "混排比例、窗口或修改理由不符合要求")
		return
	}
	item, err := s.operations.UpdateDiscoveryMixRule(r.Context(), operations.DiscoveryMixRuleInput{
		WorkSlots: request.WorkSlots, PerformerSlots: request.PerformerSlots, RepeatWindow: request.RepeatWindow,
		Enabled: request.Enabled, Reason: request.Reason,
	}, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, discoveryMixRuleItem(item))
}

func discoveryMixRuleItem(item operations.DiscoveryMixRule) discoveryMixRuleResponse {
	updatedAt := item.UpdatedAt.UTC().Format(time.RFC3339)
	return discoveryMixRuleResponse{WorkSlots: item.WorkSlots, PerformerSlots: item.PerformerSlots,
		RepeatWindow: item.RepeatWindow, Enabled: item.Enabled, UpdatedAt: updatedAt}
}
