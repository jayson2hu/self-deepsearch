package health

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadyWorker(t *testing.T) {
	handler := Handler{
		Service: "platform-worker", Version: "test", Region: "japan", Capabilities: []string{"publish"},
		Now: func() time.Time { return time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC) },
	}.Routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
}

func TestWorkerWithoutCapabilitiesIsNotReady(t *testing.T) {
	handler := Handler{Service: "platform-worker", Version: "test"}.Routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.Code)
	}
}

func TestWorkerMetricsRequireToken(t *testing.T) {
	handler := Handler{Service: "platform-worker", Version: "test", Region: "beijing", Capabilities: []string{"backup"}, MetricsToken: "metrics-test-token"}.Routes()
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauthorized.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer metrics-test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `region="beijing"`) {
		t.Fatalf("unexpected metrics response %d: %s", response.Code, response.Body.String())
	}
}

func TestWorkerReadinessRejectsStaleDispatcherHeartbeat(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	tracker := NewTracker(now.Add(-time.Minute))
	tracker.Heartbeat(now.Add(-31 * time.Second))
	handler := Handler{
		Service: "platform-worker", Version: "test", Region: "japan", Capabilities: []string{"publish"},
		Now: func() time.Time { return now }, Telemetry: tracker, RequireTelemetry: true, HeartbeatMaxAge: 30 * time.Second,
	}.Routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"status":"not_ready"`) {
		t.Fatalf("expected stale heartbeat to fail readiness, got %d: %s", response.Code, response.Body.String())
	}
}

func TestWorkerReadinessRejectsThreeConsecutivePollFailuresAndRecovers(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	tracker := NewTracker(now.Add(-time.Minute))
	tracker.Heartbeat(now.Add(-time.Second))
	tracker.PollFailed(now.Add(-3 * time.Second))
	tracker.PollFailed(now.Add(-2 * time.Second))
	tracker.PollFailed(now.Add(-time.Second))
	handler := Handler{
		Service: "platform-worker", Version: "test", Region: "japan", Capabilities: []string{"publish"},
		Now: func() time.Time { return now }, Telemetry: tracker, RequireTelemetry: true, HeartbeatMaxAge: 30 * time.Second,
	}.Routes()

	failed := httptest.NewRecorder()
	handler.ServeHTTP(failed, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if failed.Code != http.StatusServiceUnavailable || !strings.Contains(failed.Body.String(), `"consecutive_poll_failures":3`) {
		t.Fatalf("expected repeated poll failures to fail readiness, got %d: %s", failed.Code, failed.Body.String())
	}

	tracker.PollSucceeded(now)
	recovered := httptest.NewRecorder()
	handler.ServeHTTP(recovered, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recovered.Code != http.StatusOK || !strings.Contains(recovered.Body.String(), `"consecutive_poll_failures":0`) {
		t.Fatalf("expected successful poll to restore readiness, got %d: %s", recovered.Code, recovered.Body.String())
	}
}

func TestWorkerMetricsExposeHeartbeatLeaseAndOutcomeState(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	tracker := NewTracker(now.Add(-time.Hour))
	tracker.Heartbeat(now.Add(-5 * time.Second))
	tracker.PollFailed(now.Add(-7 * time.Second))
	tracker.PollSucceeded(now.Add(-6 * time.Second))
	tracker.SetActiveLeases(2)
	tracker.EventCompleted(now.Add(-10 * time.Second))
	tracker.EventFailed(now.Add(-8 * time.Second))
	handler := Handler{
		Service: "platform-worker", Version: "test", Region: "japan", Capabilities: []string{"publish"},
		Now: func() time.Time { return now }, MetricsToken: "metrics-test-token",
		Telemetry: tracker, RequireTelemetry: true, HeartbeatMaxAge: 30 * time.Second,
	}.Routes()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer metrics-test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	for _, expected := range []string{
		`self_deepsearch_worker_ready{region="japan"} 1`,
		`self_deepsearch_worker_last_heartbeat_age_seconds{region="japan"} 5.000`,
		`self_deepsearch_worker_last_poll_success_age_seconds{region="japan"} 6.000`,
		`self_deepsearch_worker_poll_failures_total{region="japan"} 1`,
		`self_deepsearch_worker_consecutive_poll_failures{region="japan"} 0`,
		`self_deepsearch_worker_last_success_age_seconds{region="japan"} 10.000`,
		`self_deepsearch_worker_active_leases{region="japan"} 2`,
		`self_deepsearch_worker_polls_total{region="japan"} 1`,
		`self_deepsearch_worker_events_total{region="japan",outcome="completed"} 1`,
		`self_deepsearch_worker_events_total{region="japan",outcome="failed"} 1`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("expected %q in metrics: %s", expected, response.Body.String())
		}
	}
}
