package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/identity"
)

type emailMetricsDatabase struct{}

func (emailMetricsDatabase) Ping(context.Context) error { return nil }
func (emailMetricsDatabase) SchemaVersion(context.Context) (int, error) {
	return requiredDatabaseSchemaVersion, nil
}

type fakeEmailMetricsProvider struct {
	metrics identity.EmailDeliveryMetrics
}

func (provider fakeEmailMetricsProvider) EmailDeliveryMetrics(context.Context, time.Time) (identity.EmailDeliveryMetrics, error) {
	return provider.metrics, nil
}

func TestPrometheusIncludesEmailQuotaCircuitAndOutcomeMetrics(t *testing.T) {
	handler := New(Options{
		Database:        emailMetricsDatabase{},
		MetricsToken:    "test-metrics-token",
		EmailDailyLimit: 10,
		EmailMetrics: fakeEmailMetricsProvider{metrics: identity.EmailDeliveryMetrics{
			DailyReserved: 8, DailySent: 6, DailyFailed: 2,
			ConsecutiveFailure: 3, CircuitOpen: true,
			Counts: []identity.EmailDeliveryCount{{Purpose: "signup", Outcome: "sent", Count: 6}},
		}},
	})
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer test-metrics-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	for _, expected := range []string{
		`self_deepsearch_email_delivery_total{purpose="signup",outcome="sent"} 6`,
		"self_deepsearch_email_daily_reserved 8",
		"self_deepsearch_email_daily_usage_ratio 0.800000",
		"self_deepsearch_email_consecutive_failures 3",
		"self_deepsearch_email_circuit_open 1",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in metrics response: %s", expected, body)
		}
	}
}
