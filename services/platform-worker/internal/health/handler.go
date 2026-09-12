package health

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Checker interface {
	Ping(context.Context) error
}

type UsageMetricsProvider interface {
	WriteMetrics(context.Context, io.Writer, time.Time)
}

const readinessPollFailureThreshold uint64 = 3

type Handler struct {
	Service                string
	Version                string
	Region                 string
	Capabilities           []string
	Now                    func() time.Time
	Checker                Checker
	RequireChecker         bool
	MetricsToken           string
	Telemetry              TelemetryProvider
	RequireTelemetry       bool
	HeartbeatMaxAge        time.Duration
	UsageMetrics           UsageMetricsProvider
	ReconcileGuardMetrics  UsageMetricsProvider
	UploadControlMetrics   UsageMetricsProvider
	UploadAdmissionHTTP    http.Handler
	UploadAdmissionMetrics UsageMetricsProvider
}

type response struct {
	Status       string          `json:"status"`
	Service      string          `json:"service"`
	Version      string          `json:"version"`
	Region       string          `json:"region"`
	Capabilities []string        `json:"capabilities"`
	Time         string          `json:"time"`
	Worker       *workerResponse `json:"worker,omitempty"`
}

type workerResponse struct {
	LastHeartbeatAt         *string `json:"last_heartbeat_at"`
	LastPollSuccessAt       *string `json:"last_poll_success_at"`
	LastSuccessAt           *string `json:"last_success_at"`
	ActiveLeases            int     `json:"active_leases"`
	ConsecutivePollFailures uint64  `json:"consecutive_poll_failures"`
}

func (handler Handler) Routes() http.Handler {
	now := handler.Now
	if now == nil {
		now = time.Now
	}
	mux := http.NewServeMux()
	if handler.UploadAdmissionHTTP != nil {
		mux.Handle("/v1/upload-admission", handler.UploadAdmissionHTTP)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, response{
			Status:       "ok",
			Service:      handler.Service,
			Version:      handler.Version,
			Region:       handler.Region,
			Capabilities: handler.Capabilities,
			Time:         now().UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, request *http.Request) {
		current := now().UTC()
		ready, snapshot := handler.ready(request.Context(), current)
		status, state := http.StatusOK, "ready"
		if !ready {
			status, state = http.StatusServiceUnavailable, "not_ready"
		}
		writeJSON(w, status, response{
			Status:       state,
			Service:      handler.Service,
			Version:      handler.Version,
			Region:       handler.Region,
			Capabilities: handler.Capabilities,
			Time:         current.Format(time.RFC3339),
			Worker:       workerResponseFromSnapshot(snapshot),
		})
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, request *http.Request) {
		if handler.MetricsToken == "" {
			http.NotFound(w, request)
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "))
		expectedHash, providedHash := sha256.Sum256([]byte(handler.MetricsToken)), sha256.Sum256([]byte(provided))
		if provided == "" || subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		current := now().UTC()
		isReady, snapshot := handler.ready(request.Context(), current)
		ready := 0
		if isReady {
			ready = 1
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_worker_info Worker build and region information.\n# TYPE self_deepsearch_worker_info gauge\nself_deepsearch_worker_info{service=%q,version=%q,region=%q} 1\n", handler.Service, handler.Version, handler.Region)
		_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_worker_ready Worker readiness.\n# TYPE self_deepsearch_worker_ready gauge\nself_deepsearch_worker_ready{region=%q} %d\n", handler.Region, ready)
		handler.writeTelemetryMetrics(w, current, snapshot)
		if handler.UsageMetrics != nil {
			ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
			handler.UsageMetrics.WriteMetrics(ctx, w, current)
			cancel()
		} else {
			_, _ = fmt.Fprint(w, "# TYPE self_deepsearch_media_usage_monitor_enabled gauge\nself_deepsearch_media_usage_monitor_enabled 0\n")
		}
		if handler.UploadControlMetrics != nil {
			handler.UploadControlMetrics.WriteMetrics(request.Context(), w, current)
		} else {
			_, _ = fmt.Fprint(w, "# TYPE self_deepsearch_media_upload_queue_enabled gauge\nself_deepsearch_media_upload_queue_enabled 0\n")
		}
		if handler.UploadAdmissionMetrics != nil {
			handler.UploadAdmissionMetrics.WriteMetrics(request.Context(), w, current)
		} else {
			_, _ = fmt.Fprint(w, "# TYPE self_deepsearch_media_upload_admission_enabled gauge\nself_deepsearch_media_upload_admission_enabled 0\n")
		}
		if handler.ReconcileGuardMetrics != nil {
			handler.ReconcileGuardMetrics.WriteMetrics(request.Context(), w, current)
		} else {
			_, _ = fmt.Fprint(w, "# TYPE self_deepsearch_media_reconcile_usage_guard_enabled gauge\nself_deepsearch_media_reconcile_usage_guard_enabled 0\n")
		}
	})
	return mux
}

func (handler Handler) ready(ctx context.Context, current time.Time) (bool, *WorkerSnapshot) {
	ready := handler.Region != "" && len(handler.Capabilities) > 0
	var snapshot *WorkerSnapshot
	if handler.Telemetry != nil {
		value := handler.Telemetry.Snapshot()
		snapshot = &value
	}
	if handler.RequireTelemetry {
		maxAge := handler.HeartbeatMaxAge
		if maxAge <= 0 {
			maxAge = 2 * time.Minute
		}
		if snapshot == nil || snapshot.LastHeartbeatAt.IsZero() || current.Sub(snapshot.LastHeartbeatAt) > maxAge {
			ready = false
		}
		if snapshot != nil && snapshot.ConsecutivePollFailures >= readinessPollFailureThreshold {
			ready = false
		}
	}
	if handler.RequireChecker {
		if handler.Checker == nil {
			return false, snapshot
		}
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := handler.Checker.Ping(pingCtx)
		cancel()
		if err != nil {
			ready = false
		}
	}
	return ready, snapshot
}

func workerResponseFromSnapshot(snapshot *WorkerSnapshot) *workerResponse {
	if snapshot == nil {
		return nil
	}
	response := &workerResponse{
		ActiveLeases:            snapshot.ActiveLeases,
		ConsecutivePollFailures: snapshot.ConsecutivePollFailures,
	}
	if !snapshot.LastHeartbeatAt.IsZero() {
		value := snapshot.LastHeartbeatAt.UTC().Format(time.RFC3339)
		response.LastHeartbeatAt = &value
	}
	if !snapshot.LastSuccessAt.IsZero() {
		value := snapshot.LastSuccessAt.UTC().Format(time.RFC3339)
		response.LastSuccessAt = &value
	}
	if !snapshot.LastPollSuccessAt.IsZero() {
		value := snapshot.LastPollSuccessAt.UTC().Format(time.RFC3339)
		response.LastPollSuccessAt = &value
	}
	return response
}

func (handler Handler) writeTelemetryMetrics(w http.ResponseWriter, current time.Time, snapshot *WorkerSnapshot) {
	if snapshot == nil {
		return
	}
	region := fmt.Sprintf("region=%q", handler.Region)
	uptime := nonNegativeDuration(current.Sub(snapshot.StartedAt)).Seconds()
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_worker_uptime_seconds Worker process uptime.\n# TYPE self_deepsearch_worker_uptime_seconds gauge\nself_deepsearch_worker_uptime_seconds{%s} %.3f\n", region, uptime)
	maxAge := handler.HeartbeatMaxAge
	if maxAge <= 0 {
		maxAge = 2 * time.Minute
	}
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_worker_heartbeat_max_age_seconds Maximum healthy dispatcher heartbeat age.\n# TYPE self_deepsearch_worker_heartbeat_max_age_seconds gauge\nself_deepsearch_worker_heartbeat_max_age_seconds{%s} %.3f\n", region, maxAge.Seconds())
	if !snapshot.LastHeartbeatAt.IsZero() {
		age := nonNegativeDuration(current.Sub(snapshot.LastHeartbeatAt)).Seconds()
		_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_worker_last_heartbeat_age_seconds Age of the latest dispatcher poll heartbeat.\n# TYPE self_deepsearch_worker_last_heartbeat_age_seconds gauge\nself_deepsearch_worker_last_heartbeat_age_seconds{%s} %.3f\n", region, age)
	}
	if !snapshot.LastPollSuccessAt.IsZero() {
		age := nonNegativeDuration(current.Sub(snapshot.LastPollSuccessAt)).Seconds()
		_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_worker_last_poll_success_age_seconds Age of the latest successful dispatcher poll.\n# TYPE self_deepsearch_worker_last_poll_success_age_seconds gauge\nself_deepsearch_worker_last_poll_success_age_seconds{%s} %.3f\n", region, age)
	}
	if !snapshot.LastSuccessAt.IsZero() {
		age := nonNegativeDuration(current.Sub(snapshot.LastSuccessAt)).Seconds()
		_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_worker_last_success_age_seconds Age of the latest successfully completed outbox event.\n# TYPE self_deepsearch_worker_last_success_age_seconds gauge\nself_deepsearch_worker_last_success_age_seconds{%s} %.3f\n", region, age)
	}
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_worker_active_leases Current outbox events in the claimed processing batch.\n# TYPE self_deepsearch_worker_active_leases gauge\nself_deepsearch_worker_active_leases{%s} %d\n", region, snapshot.ActiveLeases)
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_worker_polls_total Dispatcher polling cycles.\n# TYPE self_deepsearch_worker_polls_total counter\nself_deepsearch_worker_polls_total{%s} %d\n", region, snapshot.PollsTotal)
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_worker_poll_failures_total Dispatcher polling cycles that returned an error.\n# TYPE self_deepsearch_worker_poll_failures_total counter\nself_deepsearch_worker_poll_failures_total{%s} %d\n", region, snapshot.PollFailuresTotal)
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_worker_consecutive_poll_failures Consecutive dispatcher polling errors since the latest successful poll.\n# TYPE self_deepsearch_worker_consecutive_poll_failures gauge\nself_deepsearch_worker_consecutive_poll_failures{%s} %d\n", region, snapshot.ConsecutivePollFailures)
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_worker_events_total Terminal outbox event outcomes.\n# TYPE self_deepsearch_worker_events_total counter\nself_deepsearch_worker_events_total{%s,outcome=%q} %d\n", region, "completed", snapshot.CompletedTotal)
	_, _ = fmt.Fprintf(w, "self_deepsearch_worker_events_total{%s,outcome=%q} %d\n", region, "failed", snapshot.FailedTotal)
}

func nonNegativeDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
