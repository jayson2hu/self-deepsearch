package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

type fakeTraffic struct {
	*fakeOperations
	period string
}

func (f *fakeTraffic) TrafficReport(_ context.Context, period string, now time.Time) (operations.TrafficReport, error) {
	f.period = period
	return operations.TrafficReport{Range: period, CheckedAt: now, Timezone: "Asia/Shanghai", PageViews: 12, Daily: []operations.TrafficDay{}, HotContent: []operations.TrafficContent{}, Users: []operations.TrafficUser{}}, f.operationErr
}

func TestTrafficAuthorizationAndRanges(t *testing.T) {
	for _, role := range []string{"owner", "admin", "editor", "user"} {
		for _, period := range []string{"", "7d", "30d", "all", "bad"} {
			t.Run(role+"/"+period, func(t *testing.T) {
				repo := &fakeTraffic{fakeOperations: &fakeOperations{}}
				response := httptest.NewRecorder()
				adminHandler(role, repo).ServeHTTP(response, authenticatedAdminRequest(http.MethodGet, "/admin/v1/traffic?range="+period, ""))
				expected := http.StatusOK
				if role == "editor" || role == "user" {
					expected = http.StatusForbidden
				} else if period == "bad" {
					expected = http.StatusBadRequest
				}
				if response.Code != expected {
					t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
				}
				if expected != http.StatusOK {
					if repo.period != "" {
						t.Fatal("unauthorized/invalid request queried data")
					}
					return
				}
				if period == "" {
					period = "30d"
				}
				if repo.period != period {
					t.Fatalf("range=%s", repo.period)
				}
				var body map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				uv, exists := body["unique_visitors"]
				if !exists || uv != nil {
					t.Fatal("UV must be explicit null")
				}
				if body["page_views"] != float64(12) || response.Header().Get("Cache-Control") != "private, no-store" {
					t.Fatal("wrong report/cache policy")
				}
			})
		}
	}
}

func TestTrafficUnavailableAndAnonymous(t *testing.T) {
	for _, repo := range []operations.Repository{&fakeOperations{}, &fakeTraffic{fakeOperations: &fakeOperations{operationErr: errors.New("database unavailable")}}} {
		response := httptest.NewRecorder()
		adminHandler("owner", repo).ServeHTTP(response, authenticatedAdminRequest(http.MethodGet, "/admin/v1/traffic", ""))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("failed reads must not become zero reports: %d", response.Code)
		}
	}
	response := httptest.NewRecorder()
	adminHandler("owner", &fakeTraffic{fakeOperations: &fakeOperations{}}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/v1/traffic", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", response.Code)
	}
}
