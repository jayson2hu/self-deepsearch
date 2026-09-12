package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunHealthcheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/readyz" {
			t.Fatalf("unexpected healthcheck path: %s", request.URL.Path)
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	t.Setenv("HEALTHCHECK_URL", server.URL+"/readyz")
	if code := runHealthcheck("HEALTHCHECK_URL", "http://invalid.invalid"); code != 0 {
		t.Fatalf("expected successful healthcheck, got exit code %d", code)
	}

	server.Config.Handler = http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	})
	if code := runHealthcheck("HEALTHCHECK_URL", "http://invalid.invalid"); code != 1 {
		t.Fatalf("expected failed healthcheck, got exit code %d", code)
	}
}
