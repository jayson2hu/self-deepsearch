package httpapi

import (
	"context"
	"net/http"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (s *Server) adminTraffic(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	period := r.URL.Query().Get("range")
	if period == "" {
		period = "30d"
	}
	if !oneOf(period, "7d", "30d", "all") {
		writeError(w, r, http.StatusBadRequest, "INVALID_RANGE", "请选择7天、30天或累计范围")
		return
	}
	repository, ok := s.operations.(operations.TrafficRepository)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "TRAFFIC_UNAVAILABLE", "访问统计暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	report, err := repository.TrafficReport(ctx, period, s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	report.VisitorCollectionEnabled = s.visitorMetricsEnabled
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, report)
}
