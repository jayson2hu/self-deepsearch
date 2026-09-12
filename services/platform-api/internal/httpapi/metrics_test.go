package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/observability"
)

type poolMetricsDatabase struct {
	stats observability.DatabasePoolStats
}

type passwordMetricsIdentity struct{ *fakeIdentityService }

func (*passwordMetricsIdentity) PasswordWorkStats() identity.PasswordWorkStats {
	return identity.PasswordWorkStats{Active: 1, Waiting: 2, ActiveLimit: 1, WaitingLimit: 2, WaitTimeoutSeconds: .5, Rejected: 7, Canceled: 3, Completed: 11}
}

func TestPasswordMetricsArePrivateAggregatesIndependentOfDatabase(t *testing.T) {
	handler := New(Options{Identity: &passwordMetricsIdentity{&fakeIdentityService{}}, MetricsToken: "test-metrics-token"})
	for _, authorized := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		if authorized {
			request.Header.Set("Authorization", "Bearer test-metrics-token")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		body := response.Body.String()
		if !authorized {
			if response.Code != http.StatusUnauthorized || strings.Contains(body, "password_work_") {
				t.Fatal("password metrics exposed without authentication")
			}
			continue
		}
		for _, expected := range []string{"active 1", "waiting 2", "active_limit 1", "waiting_limit 2", "wait_timeout_seconds 0.5", "rejected_total 7", "canceled_total 3", "completed_total 11"} {
			if !strings.Contains(body, "self_deepsearch_password_work_"+expected+"\n") {
				t.Fatalf("missing password metric %s", expected)
			}
		}
		if response.Code != http.StatusOK || strings.Contains(body, "password_work_active{") {
			t.Fatal("password metrics must be available without SQL and contain no identifying labels")
		}
	}
}

func (database poolMetricsDatabase) Ping(context.Context) error { return nil }
func (database poolMetricsDatabase) SchemaVersion(context.Context) (int, error) {
	return requiredDatabaseSchemaVersion, nil
}
func (database poolMetricsDatabase) DatabasePoolStats() observability.DatabasePoolStats {
	return database.stats
}

func TestRequestMetricsHistogramBucketsAreCumulative(t *testing.T) {
	metrics := newRequestMetrics(time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
	metrics.observe(http.MethodGet, "/api/v1/search/works", http.StatusOK, 5*time.Millisecond)
	metrics.observe(http.MethodGet, "/api/v1/search/works", http.StatusOK, 300*time.Millisecond)
	metrics.observe(http.MethodGet, "/api/v1/search/works", http.StatusOK, 6*time.Second)

	items := metrics.snapshot()
	if len(items) != 1 {
		t.Fatalf("expected one metric series, got %d", len(items))
	}
	want := [requestDurationBucketCount]uint64{1, 1, 1, 1, 1, 2, 2, 2, 2, 2}
	if items[0].Value.DurationBucketCounts != want {
		t.Fatalf("unexpected cumulative buckets: got %v want %v", items[0].Value.DurationBucketCounts, want)
	}
	if items[0].Value.Count != 3 {
		t.Fatalf("expected +Inf/count value 3, got %d", items[0].Value.Count)
	}
}

func TestRequestMetricsBoundsUnrecognizedMethods(t *testing.T) {
	metrics := newRequestMetrics(time.Now())
	metrics.observe("CUSTOM-A", "/unmatched", http.StatusMethodNotAllowed, time.Millisecond)
	metrics.observe("CUSTOM-B", "/unmatched", http.StatusMethodNotAllowed, time.Millisecond)

	items := metrics.snapshot()
	if len(items) != 1 || items[0].Key.Method != "OTHER" || items[0].Value.Count != 2 {
		t.Fatalf("expected custom methods to share one bounded series, got %#v", items)
	}
}

func TestPrometheusIncludesDatabasePoolMetrics(t *testing.T) {
	handler := New(Options{
		Database: poolMetricsDatabase{stats: observability.DatabasePoolStats{
			AcquiredConnections:  8,
			IdleConnections:      1,
			TotalConnections:     9,
			MaxConnections:       10,
			AcquireCount:         120,
			EmptyAcquireCount:    7,
			CanceledAcquireCount: 2,
		}},
		MetricsToken: "test-metrics-token",
	})
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer test-metrics-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	body := response.Body.String()
	for _, expected := range []string{
		`self_deepsearch_database_pool_connections{state="acquired"} 8`,
		"self_deepsearch_database_pool_max_connections 10",
		"self_deepsearch_database_pool_utilization_ratio 0.800000",
		"self_deepsearch_database_pool_acquire_total 120",
		"self_deepsearch_database_pool_empty_acquire_total 7",
		"self_deepsearch_database_pool_canceled_acquire_total 2",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected database pool metric %q: %s", expected, body)
		}
	}
}
