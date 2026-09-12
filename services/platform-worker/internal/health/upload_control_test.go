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

type uploadMetricsProbe struct{ calls int }

func (p *uploadMetricsProbe) WriteMetrics(_ context.Context, w io.Writer, _ time.Time) {
	p.calls++
	_, _ = io.WriteString(w, "self_deepsearch_media_upload_queue_enabled 1\n")
}
func TestUploadMetricsPrivateAndIndependent(t *testing.T) {
	p := &uploadMetricsProbe{}
	h := Handler{Region: "japan", Capabilities: []string{"metrics"}, MetricsToken: "synthetic", UploadControlMetrics: p}.Routes()
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		res := httptest.NewRecorder()
		h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if p.calls != 0 {
			t.Fatal("unprotected request queried upload control")
		}
		if path != "/metrics" && res.Code != 200 {
			t.Fatal("upload controls blocked independent health")
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer synthetic")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if p.calls != 1 || res.Header().Get("Cache-Control") != "no-store" || !strings.Contains(res.Body.String(), "media_upload_queue_enabled 1") {
		t.Fatal("private upload metrics missing")
	}
	h = Handler{Region: "beijing", Capabilities: []string{"backup"}, MetricsToken: "synthetic"}.Routes()
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if !strings.Contains(res.Body.String(), "media_upload_queue_enabled 0") {
		t.Fatal("disabled queue ambiguous")
	}
}
