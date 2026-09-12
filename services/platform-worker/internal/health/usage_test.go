package health

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type usageProbe struct {
	calls int
	t     *testing.T
}

type reconciliationMetricsProbe struct{ calls int }

func (p *reconciliationMetricsProbe) WriteMetrics(_ context.Context, w io.Writer, _ time.Time) {
	p.calls++
	_, _ = io.WriteString(w, "self_deepsearch_media_reconcile_usage_guard_enabled 1\n")
}
func TestReconciliationGuardMetricsStayPrivateAndIndependent(t *testing.T) {
	p := &reconciliationMetricsProbe{}
	h := Handler{Region: "japan", Capabilities: []string{"metrics"}, MetricsToken: "synthetic", ReconcileGuardMetrics: p}.Routes()
	for _, path := range []string{"/metrics", "/readyz", "/healthz"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if p.calls != 0 {
			t.Fatal("admission telemetry leaked through public route")
		}
		if path != "/metrics" && w.Code != 200 {
			t.Fatal("scan protection blocked independent services")
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.Header.Set("Authorization", "Bearer synthetic")
	h.ServeHTTP(w, r)
	if p.calls != 1 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "media_reconcile_usage_guard_enabled 1") {
		t.Fatal("private admission telemetry missing")
	}
	h = Handler{Region: "beijing", Capabilities: []string{"backup"}, MetricsToken: "synthetic"}.Routes()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "media_reconcile_usage_guard_enabled 0") {
		t.Fatal("disabled admission was ambiguous")
	}
}

func (p *usageProbe) WriteMetrics(ctx context.Context, w io.Writer, _ time.Time) {
	p.calls++
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 2*time.Second {
		p.t.Error("metrics DB read must be bounded")
	}
	_, _ = io.WriteString(w, "self_deepsearch_media_usage_monitor_enabled 1\n")
}
func TestUsageMetricsArePrivateAndDoNotReplaceWorkerReadiness(t *testing.T) {
	p := &usageProbe{t: t}
	h := Handler{Region: "japan", Capabilities: []string{"metrics"}, MetricsToken: "synthetic-token", UsageMetrics: p}.Routes()
	for _, path := range []string{"/metrics", "/healthz", "/readyz"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if p.calls != 0 {
			t.Fatal("usage state read from unprotected/non-metrics route")
		}
		if path == "/metrics" && w.Code != http.StatusUnauthorized {
			t.Fatal("metrics not protected")
		}
		if path != "/metrics" && w.Code != 200 {
			t.Fatal("observer failure must not block independent worker jobs")
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.Header.Set("Authorization", "Bearer synthetic-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if p.calls != 1 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "media_usage_monitor_enabled 1") {
		t.Fatal("protected metric missing")
	}
	h = Handler{Region: "beijing", Capabilities: []string{"backup"}, MetricsToken: "synthetic-token"}.Routes()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "media_usage_monitor_enabled 0") {
		t.Fatal("disabled observer ambiguous")
	}
}
